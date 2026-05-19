//go:build windows

package launcher

// loginwatch_windows.go — Win32 plumbing for the M10 login watcher.
//
// Subscribes to two narrow event types on the spawned game's PID via
// SetWinEventHook (WINEVENT_OUTOFCONTEXT) and dispatches to caller-
// provided Go callbacks:
//
//   - EVENT_OBJECT_LOCATIONCHANGE on idObject == OBJID_CARET (-9)
//     → onCaret(): form's text caret is animating → form-rendered.
//   - EVENT_OBJECT_CREATE on a new MapleStoryClassTW HWND (!= loginHwnd)
//     → onLoggedIn(): character-select window opened.
//
// WINEVENT_OUTOFCONTEXT delivers events to the SAME thread that
// called SetWinEventHook. Install + GetMessageW pump therefore both
// run inside one LockOSThread'd goroutine; the caller gets back a
// pumpDone channel to await pump exit, plus an idempotent cleanup
// closure. ctx cancellation triggers the same cleanup path.

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------------------------------------------------------------------------
// Win32 procs
//
// user32 is already declared in inject_windows.go (line 20) and shared across
// package launcher. Procs prefixed `lw` (login-watch) for grep + to avoid
// future collisions if another file later wants the same proc.
// ---------------------------------------------------------------------------

var (
	lwProcSetWinEventHook    = user32.NewProc("SetWinEventHook")
	lwProcUnhookWinEvent     = user32.NewProc("UnhookWinEvent")
	lwProcGetMessageW        = user32.NewProc("GetMessageW")
	lwProcTranslateMessage   = user32.NewProc("TranslateMessage")
	lwProcDispatchMessageW   = user32.NewProc("DispatchMessageW")
	lwProcPostThreadMessageW = user32.NewProc("PostThreadMessageW")
	lwProcGetClassNameW      = user32.NewProc("GetClassNameW")

	lwKernel32               = windows.NewLazySystemDLL("kernel32.dll")
	lwProcGetCurrentThreadID = lwKernel32.NewProc("GetCurrentThreadId")
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	eventObjectCreate         uint32 = 0x8000
	eventObjectLocationChange uint32 = 0x800B

	winEventOutOfContext uintptr = 0x0000

	objidCaret int32  = -9
	wmQuit     uint32 = 0x0012

	mapleStoryClassTW = "MapleStoryClassTW"
)

// lwMsg mirrors the Win32 MSG struct on amd64 for the GetMessageW /
// TranslateMessage / DispatchMessageW chain. Layout copied from
// cmd/eventprobe/eventprobe_windows.go where it's been exercised
// against a real MapleStory process.
type lwMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
}

// ---------------------------------------------------------------------------
// Package-level shim state.
//
// syscall.NewCallback can't close over Go state, so the user-supplied
// callbacks and the loginHwnd filter live in package-level atomics. The
// orchestrator (service.go) installs one watcher at a time, so single-slot
// state is sufficient. atomic.Pointer + atomic.Uintptr handle the
// store-before-hook / clear-after-unhook race cleanly.
// ---------------------------------------------------------------------------

var (
	lwCaretCallback    atomic.Pointer[func()]
	lwLoggedInCallback atomic.Pointer[func()]
	lwLoginHwnd        atomic.Uintptr
)

// lwShim is the WINEVENTPROC passed to SetWinEventHook. Cheap filter +
// dispatch; the burst-detection state machine lives in loginwatch.go.
var lwShim = syscall.NewCallback(func(
	_ uintptr, // hWinEventHook (unused)
	event uintptr, // DWORD
	hwnd uintptr, // HWND
	idObject uintptr, // LONG — sign-extended in 64-bit slot, cast back via int32
	_ uintptr, // idChild (unused)
	_ uintptr, // idEventThread (unused)
	_ uintptr, // dwmsEventTime (unused)
) uintptr {
	switch uint32(event) {
	case eventObjectLocationChange:
		if int32(idObject) != objidCaret {
			return 0
		}
		if cb := lwCaretCallback.Load(); cb != nil {
			(*cb)()
		}
	case eventObjectCreate:
		// Skip the login form's own CREATE — we want the NEXT
		// MapleStoryClassTW window (character-select) only.
		if hwnd == lwLoginHwnd.Load() {
			return 0
		}
		if lwGetWindowClass(hwnd) != mapleStoryClassTW {
			return 0
		}
		if cb := lwLoggedInCallback.Load(); cb != nil {
			(*cb)()
		}
	}
	return 0
})

// lwGetWindowClass calls Win32 GetClassNameW; empty string on any failure.
// Best-effort: a class-name mismatch just means the shim drops the event.
func lwGetWindowClass(hwnd uintptr) string {
	var buf [256]uint16
	r, _, _ := lwProcGetClassNameW.Call(
		hwnd,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:r])
}

// ---------------------------------------------------------------------------
// installLoginWatch
// ---------------------------------------------------------------------------

func init() {
	installLoginWatchFn = installLoginWatch
}

// installLoginWatch spawns one LockOSThread'd goroutine that installs the
// SetWinEventHook and pumps GetMessageW. SetWinEventHook with
// WINEVENT_OUTOFCONTEXT delivers events to the thread that called it, so
// install + pump must be on the same OS thread. Returns to the caller
// after the hook is verified installed (success or error).
//
// The returned cleanup func is idempotent (sync.Once) and synchronous —
// it posts WM_QUIT to the pump thread and blocks on the pump goroutine's
// natural exit (which itself calls UnhookWinEvent + clears the shim state).
// A parallel goroutine fires cleanup on ctx.Done() so callers can rely on
// ctx-only lifecycle without needing to call cleanup explicitly.
func installLoginWatch(
	ctx context.Context,
	pid uint32,
	loginHwnd uintptr,
	onCaret func(),
	onLoggedIn func(),
) (func(), error) {
	// Wire shim state BEFORE SetWinEventHook returns so the first event
	// (which can fire immediately) finds a non-nil callback.
	lwCaretCallback.Store(&onCaret)
	lwLoggedInCallback.Store(&onLoggedIn)
	lwLoginHwnd.Store(loginHwnd)

	installResult := make(chan error, 1)
	pumpDone := make(chan struct{})
	var pumpTID atomic.Uint32

	go func() {
		runtime.LockOSThread()

		// Capture our TID so cleanup's PostThreadMessageW knows where
		// to send WM_QUIT.
		tidRaw, _, _ := lwProcGetCurrentThreadID.Call()
		pumpTID.Store(uint32(tidRaw))

		hHook, _, lastErr := lwProcSetWinEventHook.Call(
			uintptr(eventObjectCreate),
			uintptr(eventObjectLocationChange),
			0,            // hmodWinEventProc — NULL for OUTOFCONTEXT
			lwShim,       // pfnWinEventProc
			uintptr(pid), // idProcess — filter to spawned game
			0,            // idThread — all threads in the process
			winEventOutOfContext,
		)
		if hHook == 0 {
			installResult <- fmt.Errorf("SetWinEventHook(pid=%d): %w", pid, lastErr)
			close(pumpDone)
			return
		}
		installResult <- nil

		// Pump until WM_QUIT (cleanup PostThreadMessageW) or fatal error.
		var m lwMsg
		for {
			r, _, _ := lwProcGetMessageW.Call(
				uintptr(unsafe.Pointer(&m)),
				0, 0, 0,
			)
			// Win32 BOOL: 0 = WM_QUIT received, -1 (sign-extended in
			// 64-bit slot) = error. Both end the pump.
			if int64(r) <= 0 {
				break
			}
			lwProcTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			lwProcDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}

		// Tear down on this thread (same thread that installed) so the
		// hook is properly released. Clear shim state after, so any
		// late-arriving callback finds a no-op nil pointer.
		lwProcUnhookWinEvent.Call(hHook)
		lwCaretCallback.Store(nil)
		lwLoggedInCallback.Store(nil)
		lwLoginHwnd.Store(0)
		close(pumpDone)
	}()

	if err := <-installResult; err != nil {
		return nil, err
	}

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			tid := pumpTID.Load()
			if tid != 0 {
				lwProcPostThreadMessageW.Call(
					uintptr(tid), uintptr(wmQuit), 0, 0,
				)
			}
			<-pumpDone
		})
	}

	// ctx-driven auto-cleanup, so callers can rely on ctx cancellation
	// alone without remembering to defer cleanup().
	go func() {
		<-ctx.Done()
		cleanup()
	}()

	return cleanup, nil
}
