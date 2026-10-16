package launcher

import (
	"context"
	"log/slog"
	"time"
)

// windowPIDFn returns the PID of the process that owns the given HWND.
// Bound in watcher_windows.go (Win32 GetWindowThreadProcessId) and
// watcher_other.go (stub returning ErrPlatformUnsupported).
var windowPIDFn func(hwnd uintptr) (uint32, error)

// waitForProcessExitFn blocks until the process with the given PID
// exits or ctx is cancelled. Bound in watcher_windows.go (Win32
// OpenProcess + WaitForSingleObject) and watcher_other.go (stub).
var waitForProcessExitFn func(ctx context.Context, pid uint32) error

// eventEmitFn stays a no-op until main installs the app's emitter, which keeps Wails out of internal/launcher.
var eventEmitFn = func(name string, data any) {
	_ = name
	_ = data
}

// SetEventEmitter must run before NewLauncherService, which can start the exit watcher for a game that is already open.
func SetEventEmitter(fn func(name string, data any)) {
	eventEmitFn = fn
}

// Tunables for runGameWatcher. Tests override gameWindowAppearTimeout
// to a small value so the phase-1 timeout case finishes in
// milliseconds instead of waiting an actual minute.
var (
	gameWindowPollInterval  = 500 * time.Millisecond
	gameWindowAppearTimeout = 60 * time.Second
)

// runGameWatcher learns when the spawned MapleStory process exits
// and emits gameStateChangedEvent{Running:false} so the frontend's
// useGameStateQuery cache flips back to "not running." One watcher
// per active spawn — LauncherService cancels the prior watcher when
// a new SpawnGame call lands, or when NewLauncherService finds an
// already-running game at startup.
//
// Two phases:
//
//	Phase 1 (initialHwnd == 0): poll findGameWindowFn every 500ms
//	  for up to gameWindowAppearTimeout waiting for cold-start.
//	  Timeout → emit running:false so the FE doesn't stay stuck in
//	  the optimistic running:true state SpawnGame emitted (we don't
//	  know what went wrong — AV blocked, slow disk, crash mid-launch
//	  — but the FE shouldn't keep showing 帶入帳密 indefinitely).
//	Phase 2: derive PID from hwnd, then waitForProcessExitFn blocks
//	  until the kernel signals process exit → emit running:false.
//
// ctx cancel (new SpawnGame supersedes, or service shutdown) →
// return without emitting; the new watcher (if any) takes over.
//
// Note: SpawnGame emits the running:true transition itself
// (optimistically, right after spawnFn returns). The watcher does
// NOT re-emit running:true on phase-1 success — that would be a
// redundant cache write for the same logical state.
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
		// the FE reset path still runs — a premature flip to
		// running:false is better UX than a permanently stuck
		// "帶入帳密" button.
		slog.Error("gameWatcher: waitForProcessExitFn failed",
			"err", err, "pid", pid)
	}
	slog.Info("gameWatcher: emitting state-changed running:false", "pid", pid)
	eventEmitFn(gameStateChangedEvent, GameState{Running: false})
}

// pollForWindow polls findGameWindowFn until the game window appears,
// the appear-timeout elapses, or ctx is cancelled. Returns (hwnd,
// true) on success. On timeout it emits running:false so the FE
// resets; on ctx cancel it returns silently for the new watcher to
// take over.
func pollForWindow(ctx context.Context) (uintptr, bool) {
	deadline := time.Now().Add(gameWindowAppearTimeout)
	ticker := time.NewTicker(gameWindowPollInterval)
	defer ticker.Stop()
	for {
		if hwnd := findGameWindowFn(); hwnd != 0 {
			return hwnd, true
		}
		if time.Now().After(deadline) {
			slog.Warn("gameWatcher: window never appeared, emitting running:false",
				"timeout", gameWindowAppearTimeout)
			eventEmitFn(gameStateChangedEvent, GameState{Running: false})
			return 0, false
		}
		select {
		case <-ctx.Done():
			return 0, false
		case <-ticker.C:
		}
	}
}
