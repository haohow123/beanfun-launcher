package launcher

import (
	"context"
	"log/slog"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// windowPIDFn returns the PID of the process that owns the given HWND.
// Bound in watcher_windows.go (Win32 GetWindowThreadProcessId) and
// watcher_other.go (stub returning ErrPlatformUnsupported).
var windowPIDFn func(hwnd uintptr) (uint32, error)

// waitForProcessExitFn blocks until the process with the given PID
// exits or ctx is cancelled. Bound in watcher_windows.go (Win32
// OpenProcess + WaitForSingleObject) and watcher_other.go (stub).
var waitForProcessExitFn func(ctx context.Context, pid uint32) error

// eventEmitFn forwards events to the live Wails application. Default
// hits application.Get().Event.Emit with a nil-app guard so tests
// that exercise this code path without standing up the runtime don't
// panic. Tests override this var directly to capture emits.
//
// Same indirection shape as spawnFn / injectFn / findGameWindowFn —
// platform code binds the default, tests swap-and-restore.
var eventEmitFn = func(name string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data)
	}
}

// Tunables for runGameWatcher. Tests override gameWindowAppearTimeout
// to a small value so the phase-1 timeout case finishes in
// milliseconds instead of waiting an actual minute.
var (
	gameWindowPollInterval  = 500 * time.Millisecond
	gameWindowAppearTimeout = 60 * time.Second
)

// gameExitedEvent is the Wails event name the frontend subscribes to
// in HomePage.tsx to reset spawn.isSuccess when the game closes.
const gameExitedEvent = "game:exited"

// runGameWatcher learns when the spawned MapleStory process exits and
// emits "game:exited" so the frontend can reset spawn.isSuccess. One
// watcher per active spawn — LauncherService cancels the prior
// watcher when a new SpawnGame call lands.
//
// Two phases:
//
//	Phase 1 (initialHwnd == 0): poll findGameWindowFn every 500ms
//	  for up to gameWindowAppearTimeout waiting for cold-start.
//	  Timeout → emit anyway with PID=0 so the frontend doesn't stay
//	  stuck in success state (we don't know what went wrong — AV
//	  blocked, slow disk, crash mid-launch — but we know the user
//	  shouldn't keep seeing "✓ 已啟動" indefinitely).
//	Phase 2: derive PID from hwnd, then waitForProcessExitFn blocks
//	  until the kernel signals process exit → emit "game:exited"
//	  with the PID.
//
// ctx cancel (new SpawnGame supersedes, or service shutdown) →
// return without emitting; the new watcher (if any) takes over.
func runGameWatcher(ctx context.Context, initialHwnd uintptr) {
	hwnd := initialHwnd
	if hwnd == 0 {
		var found bool
		hwnd, found = pollForWindow(ctx)
		if !found {
			// pollForWindow handled the emit (timeout) or stayed
			// silent (ctx cancel). Either way, nothing more here.
			return
		}
	}

	pid, err := windowPIDFn(hwnd)
	if err != nil {
		slog.Error("gameWatcher: windowPIDFn failed", "err", err, "hwnd", hwnd)
		return
	}
	slog.Info("gameWatcher: tracking process", "pid", pid, "hwnd", hwnd)

	if err := waitForProcessExitFn(ctx, pid); err != nil {
		if ctx.Err() != nil {
			slog.Info("gameWatcher: cancelled mid-wait", "pid", pid)
			return
		}
		// Unexpected error from the wait. Fall through to emit so
		// the frontend's reset path still runs — a premature reset
		// is better UX than a permanently stuck "✓ 已啟動" button.
		slog.Error("gameWatcher: waitForProcessExitFn failed",
			"err", err, "pid", pid)
	}
	slog.Info("gameWatcher: emitting game:exited", "pid", pid)
	eventEmitFn(gameExitedEvent, pid)
}

// pollForWindow polls findGameWindowFn until the game window appears,
// the appear-timeout elapses, or ctx is cancelled. Returns (hwnd,
// true) on success. On timeout it emits the synthetic "game:exited"
// signal (PID=0) so the frontend resets; on ctx cancel it returns
// silently for the new watcher to take over.
func pollForWindow(ctx context.Context) (uintptr, bool) {
	deadline := time.Now().Add(gameWindowAppearTimeout)
	ticker := time.NewTicker(gameWindowPollInterval)
	defer ticker.Stop()
	for {
		if hwnd := findGameWindowFn(); hwnd != 0 {
			return hwnd, true
		}
		if time.Now().After(deadline) {
			slog.Warn("gameWatcher: window never appeared, emitting timeout signal",
				"timeout", gameWindowAppearTimeout)
			eventEmitFn(gameExitedEvent, uint32(0))
			return 0, false
		}
		select {
		case <-ctx.Done():
			return 0, false
		case <-ticker.C:
		}
	}
}
