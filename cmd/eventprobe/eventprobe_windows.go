//go:build windows

package main

// eventprobe_windows.go — Win32 plumbing for the eventprobe diagnostic tool.
//
// All golang.org/x/sys/windows and unsafe usage is confined here.
// main_windows.go calls only the six exported-ish functions and postQuit.
//
// References:
//   https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwineventhook
//   https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-unhookwinevent
//   https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-enumwindows
//   https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-findwindoww
//   https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getclassnamew
//   https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getmessage
//   https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-postthreadmessagew

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------------------------------------------------------------------------
// Win32 proc bindings
// ---------------------------------------------------------------------------

var (
	// user32.dll is already declared in inject_windows.go inside the
	// launcher package; here we are in package main so we declare our own.
	modUser32 = windows.NewLazySystemDLL("user32.dll")

	// EnumWindows wraps Win32 EnumWindows (user32.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-enumwindows
	procEnumWindows = modUser32.NewProc("EnumWindows")

	// FindWindowW wraps Win32 FindWindowW (user32.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-findwindoww
	procFindWindowW = modUser32.NewProc("FindWindowW")

	// GetWindowThreadProcessId wraps Win32 GetWindowThreadProcessId (user32.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getwindowthreadprocessid
	procGetWindowThreadProcessId = modUser32.NewProc("GetWindowThreadProcessId")

	// GetClassNameW wraps Win32 GetClassNameW (user32.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getclassnamew
	procGetClassNameW = modUser32.NewProc("GetClassNameW")

	// SetWinEventHook wraps Win32 SetWinEventHook (user32.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwineventhook
	procSetWinEventHook = modUser32.NewProc("SetWinEventHook")

	// UnhookWinEvent wraps Win32 UnhookWinEvent (user32.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-unhookwinevent
	procUnhookWinEvent = modUser32.NewProc("UnhookWinEvent")

	// GetMessageW wraps Win32 GetMessageW (user32.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getmessage
	procGetMessageW = modUser32.NewProc("GetMessageW")

	// TranslateMessage wraps Win32 TranslateMessage (user32.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-translatemessage
	procTranslateMessage = modUser32.NewProc("TranslateMessage")

	// DispatchMessageW wraps Win32 DispatchMessageW (user32.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-dispatchmessage
	procDispatchMessageW = modUser32.NewProc("DispatchMessageW")

	// PostThreadMessageW wraps Win32 PostThreadMessageW (user32.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-postthreadmessagew
	procPostThreadMessageW = modUser32.NewProc("PostThreadMessageW")

	// GetCurrentThreadId wraps Win32 GetCurrentThreadId (kernel32.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-getcurrentthreadid
	modKernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentThread = modKernel32.NewProc("GetCurrentThreadId")
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// WinEvent range — subscribe to everything.
	eventMIN uint32 = 0x00000001
	eventMAX uint32 = 0x7FFFFFFF

	// WINEVENT_OUTOFCONTEXT: callback on the hook-installing thread via
	// the message loop, not injected into the target process.
	winEventOutOfContext uintptr = 0x0000

	// MSG layout offsets (bytes) in the Win32 MSG struct on amd64:
	//   hwnd     uintptr  (8)
	//   message  uint32   (4)
	//   wParam   uintptr  (8)
	//   lParam   uintptr  (8)
	//   time     uint32   (4)
	//   pt       POINT    (8)
	//   lPrivate uint32   (4)  — present in newer SDK, may be absent on older
	// We use a concrete struct below instead.

	wmQuit uint32 = 0x0012
)

// msg mirrors Win32 MSG for GetMessageW / TranslateMessage / DispatchMessageW.
// https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-msg
type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
}

// ---------------------------------------------------------------------------
// Package-level state for the WinEvent callback and message-loop thread ID.
//
// syscall.NewCallback produces a function pointer whose signature is fixed
// at compile time; it cannot close over arbitrary Go variables.  The
// user-supplied callback is therefore stored here and read from the shim.
// This is safe because eventprobe installs exactly one hook at a time.
// ---------------------------------------------------------------------------

// activeCallback is the Go-level handler installed via installEventHook.
// Written before the hook is set; read only from the WinEventProc shim
// which runs on the same OS thread, so no lock is needed for the callback
// field itself.  We use an atomic pointer so the uninstall path can nil it
// out safely without a mutex.
var activeCallback atomic.Pointer[func(event uint32, hwnd uintptr, idObject, idChild int32)]

// messageLoopThreadID is set by runMessageLoop before entering GetMessageW.
// postQuit reads it from any goroutine, hence the atomic.
var messageLoopThreadID atomic.Uint32

// winEventShim is the raw WinEventProc passed to SetWinEventHook.
//
// WinEventProc signature (winuser.h):
//
//	void WINAPI WinEventProc(
//	    HWINEVENTHOOK hWinEventHook,
//	    DWORD         event,
//	    HWND          hwnd,
//	    LONG          idObject,
//	    LONG          idChild,
//	    DWORD         idEventThread,
//	    DWORD         dwmsEventTime
//	);
//
// syscall.NewCallback requires the Go func to return uintptr; we return 0.
// Parameters are passed as uintptr regardless of their C type; we truncate
// to the correct width when forwarding to the Go callback.
var winEventShim = syscall.NewCallback(func(
	hHook uintptr, // HWINEVENTHOOK
	event uintptr, // DWORD
	hwnd uintptr, // HWND
	idObject uintptr, // LONG (sign-extended from 32-bit slot on amd64)
	idChild uintptr, // LONG
	idEventThread uintptr, // DWORD — unused
	dwmsEventTime uintptr, // DWORD — unused
) uintptr {
	if cb := activeCallback.Load(); cb != nil {
		(*cb)(
			uint32(event),
			hwnd,
			int32(idObject),
			int32(idChild),
		)
	}
	return 0
})

// ---------------------------------------------------------------------------
// findTargetWindow
// ---------------------------------------------------------------------------

// pollInterval is the gap between successive window-search attempts
// in findTargetWindow's wait loop. 500 ms matches the launcher's other
// polling cadences.
const pollInterval = 500 * time.Millisecond

// pollTimeout caps how long findTargetWindow will wait for the target
// window to appear before giving up. 2 min covers cold-start of a
// game executable plus a UAC dance; longer than that is a user
// problem (game didn't actually launch).
const pollTimeout = 2 * time.Minute

// findTargetWindow locates the target HWND by window class name(s)
// or PID, polling until one is found or until pollTimeout elapses.
// The polling loop lets the user run eventprobe BEFORE launching the
// game — the tool waits, latches the window the moment it appears,
// and starts capturing events from the very first one (including
// EVENT_OBJECT_CREATE on the main window).
//
//   - classNames non-empty: try FindWindowW for each class per tick;
//     first match wins. Lets the caller pass a fallback list
//     (e.g. "MapleStoryClass,MapleStoryClassTW") without having to
//     rerun the tool when the first guess misses.
//   - pid non-zero: enumerate top-level windows per tick.
//   - Both empty: error.
func findTargetWindow(classNames []string, pid uint32) (hwnd uintptr, foundPID uint32, err error) {
	if len(classNames) == 0 && pid == 0 {
		return 0, 0, errors.New("findTargetWindow: must provide --class or --pid")
	}

	deadline := time.Now().Add(pollTimeout)
	announced := false

	for time.Now().Before(deadline) {
		var (
			h      uintptr
			outPID uint32
			found  bool
			sysErr error
		)
		switch {
		case len(classNames) > 0:
			for _, c := range classNames {
				h, outPID, found, sysErr = findWindowByClass(c)
				if sysErr != nil || found {
					break
				}
			}
		default:
			h, outPID, found, sysErr = findWindowByPID(pid)
		}
		if sysErr != nil {
			// Real Win32 system error (not "not found yet") — fail
			// fast, polling more won't help.
			return 0, 0, sysErr
		}
		if found {
			return h, outPID, nil
		}

		if !announced {
			if len(classNames) > 0 {
				fmt.Fprintf(os.Stderr, "waiting for window matching --class=%v (Ctrl-C to abort, %s timeout)…\n", classNames, pollTimeout)
			} else {
				fmt.Fprintf(os.Stderr, "waiting for window owned by --pid=%d (Ctrl-C to abort, %s timeout)…\n", pid, pollTimeout)
			}
			announced = true
		}
		time.Sleep(pollInterval)
	}

	if len(classNames) > 0 {
		return 0, 0, fmt.Errorf("no window matching any of --class=%v found within %s", classNames, pollTimeout)
	}
	return 0, 0, fmt.Errorf("no window owned by --pid=%d found within %s", pid, pollTimeout)
}

// findWindowByClass calls FindWindowW once, then GetWindowThreadProcessId.
// Returns found=false (no error) when no matching window exists yet —
// the polling loop in findTargetWindow handles the retry. Real Win32
// failures (UTF16 conversion, malformed input) surface as sysErr.
func findWindowByClass(className string) (hwnd uintptr, pid uint32, found bool, sysErr error) {
	classW, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return 0, 0, false, fmt.Errorf("findWindowByClass: UTF16PtrFromString(%q): %w", className, err)
	}

	// FindWindowW wraps Win32 FindWindowW (user32.dll). Returns 0
	// when no matching window exists. We treat that as "not yet,
	// keep polling" rather than a hard error — GetLastError is
	// unreliable here (Win32 doesn't consider "no match" an
	// error, so it usually leaves last-error untouched, leading
	// to the confusing "the operation completed successfully"
	// formatting).
	h, _, _ := procFindWindowW.Call(
		uintptr(unsafe.Pointer(classW)),
		0, // lpWindowName — NULL matches any title
	)
	if h == 0 {
		return 0, 0, false, nil
	}

	var outPID uint32
	procGetWindowThreadProcessId.Call(h, uintptr(unsafe.Pointer(&outPID)))
	if outPID == 0 {
		return 0, 0, false, fmt.Errorf("GetWindowThreadProcessId(hwnd=0x%X): pid=0: %w",
			h, windows.GetLastError())
	}
	return h, outPID, true, nil
}

// enumState is the carry struct for the EnumWindows callback.
// We cannot close over Go state in a syscall callback, so we use a
// package-level singleton — fine because enumeration is synchronous and
// single-threaded here.
type enumState struct {
	targetPID uint32
	result    uintptr
}

var enumCtx enumState

// enumWindowsCallback is the WNDENUMPROC passed to EnumWindows.
// Returns FALSE (0) to stop enumeration once we find a match, TRUE (1)
// to continue.
var enumWindowsCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
	var ownerPID uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&ownerPID)))
	if ownerPID == enumCtx.targetPID {
		enumCtx.result = hwnd
		return 0 // stop enumeration
	}
	return 1 // continue
})

// findWindowByPID enumerates top-level windows and returns the first
// HWND owned by the given PID. Returns found=false (no error) when no
// match exists yet — the polling loop in findTargetWindow handles the
// retry.
func findWindowByPID(pid uint32) (hwnd uintptr, foundPID uint32, found bool, sysErr error) {
	enumCtx = enumState{targetPID: pid}

	// EnumWindows wraps Win32 EnumWindows (user32.dll).
	procEnumWindows.Call(enumWindowsCallback, 0)

	if enumCtx.result == 0 {
		return 0, 0, false, nil
	}
	return enumCtx.result, pid, true, nil
}

// ---------------------------------------------------------------------------
// getWindowClass
// ---------------------------------------------------------------------------

// getWindowClass wraps Win32 GetClassNameW (user32.dll).
// Returns empty string on failure — best-effort; diagnostic only.
func getWindowClass(hwnd uintptr) string {
	var buf [256]uint16
	r, _, _ := procGetClassNameW.Call(
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
// installEventHook
// ---------------------------------------------------------------------------

// installEventHook wraps Win32 SetWinEventHook (user32.dll).
//
// Subscribes to EVENT_MIN..EVENT_MAX for the given PID on the out-of-context
// model (WINEVENT_OUTOFCONTEXT).  The hook fires on the thread that calls
// runMessageLoop; the caller must pump a message loop on that thread.
//
// The returned uninstall func calls UnhookWinEvent and clears the package-
// level callback pointer.
//
// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setwineventhook
func installEventHook(pid uint32, callback func(event uint32, hwnd uintptr, idObject, idChild int32)) (uninstall func(), err error) {
	// Store the callback before installing the hook so the shim never
	// observes a nil pointer even if the first event fires immediately.
	activeCallback.Store(&callback)

	// SetWinEventHook(eventMin, eventMax, hmodWinEventProc, pfnWinEventProc,
	//                 idProcess, idThread, dwFlags)
	// hmodWinEventProc must be NULL for out-of-context hooks.
	hHook, _, lastErr := procSetWinEventHook.Call(
		uintptr(eventMIN),
		uintptr(eventMAX),
		0,            // hmodWinEventProc — NULL for OUTOFCONTEXT
		winEventShim, // pfnWinEventProc
		uintptr(pid), // idProcess
		0,            // idThread — 0 = all threads in the process
		winEventOutOfContext,
	)
	if hHook == 0 {
		activeCallback.Store(nil)
		return nil, fmt.Errorf("SetWinEventHook(pid=%d): %w", pid, lastErr)
	}

	uninstall = func() {
		// UnhookWinEvent wraps Win32 UnhookWinEvent (user32.dll).
		procUnhookWinEvent.Call(hHook)
		activeCallback.Store(nil)
	}
	return uninstall, nil
}

// ---------------------------------------------------------------------------
// runMessageLoop + postQuit
// ---------------------------------------------------------------------------

// runMessageLoop pumps the Win32 message loop on the current OS thread.
//
// SetWinEventHook with WINEVENT_OUTOFCONTEXT delivers events through the
// message queue of the installing thread, so the loop must run on that
// exact thread.  runtime.LockOSThread ensures the goroutine is pinned.
//
// The loop exits when GetMessageW returns 0 (WM_QUIT received) or -1
// (error, which we treat as fatal and exit silently — diagnostic tool).
//
// The stop channel parameter is accepted for API symmetry with callers that
// prefer to pass a cancellation channel; however, the canonical shutdown
// path is for the caller's goroutine to call postQuit() directly (which
// posts WM_QUIT to this thread).  A nil stop channel is valid and common.
// If a non-nil channel is provided it is watched in a separate goroutine
// that calls postQuit when it closes or receives a value.
func runMessageLoop(stop <-chan struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Capture the OS thread ID so postQuit can address this thread.
	tid, _, _ := procGetCurrentThread.Call()
	messageLoopThreadID.Store(uint32(tid))

	// If the caller supplied a stop channel, watch it in a separate
	// goroutine.  Closing the channel or sending on it triggers postQuit,
	// which posts WM_QUIT to this thread and unblocks GetMessageW.
	if stop != nil {
		go func() {
			<-stop
			postQuit()
		}()
	}

	var m msg
	for {
		// GetMessageW blocks until a message arrives or WM_QUIT is posted.
		// Return values: >0 non-WM_QUIT message, 0 WM_QUIT, -1 error.
		r, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&m)),
			0, // hWnd — NULL receives all messages for this thread
			0, // wMsgFilterMin
			0, // wMsgFilterMax
		)
		// r is uintptr; on amd64 the Win32 BOOL -1 comes back as
		// 0xFFFFFFFFFFFFFFFF.  Treat anything with the sign bit set as
		// an error and bail.
		if r == 0 || int64(r) < 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// postQuit posts WM_QUIT to the message-loop thread, unblocking
// runMessageLoop.  Safe to call from any goroutine after runMessageLoop
// has started (it stores the thread ID atomically before entering the loop).
//
// PostThreadMessageW wraps Win32 PostThreadMessageW (user32.dll).
// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-postthreadmessagew
func postQuit() {
	tid := messageLoopThreadID.Load()
	if tid == 0 {
		return // message loop not started yet; nothing to do
	}
	procPostThreadMessageW.Call(
		uintptr(tid),
		uintptr(wmQuit),
		0,
		0,
	)
}

// ---------------------------------------------------------------------------
// eventName
// ---------------------------------------------------------------------------

// eventName returns a human-readable name for a WinEvent constant.
// Unknown values fall back to "EVENT_0x%04X".
func eventName(event uint32) string {
	switch event {
	case 0x0001:
		return "EVENT_SYSTEM_SOUND / EVENT_MIN"
	case 0x0002:
		return "EVENT_SYSTEM_ALERT"
	case 0x0003:
		return "EVENT_SYSTEM_FOREGROUND"
	case 0x0004:
		return "EVENT_SYSTEM_MENUSTART"
	case 0x0005:
		return "EVENT_SYSTEM_MENUEND"
	case 0x0006:
		return "EVENT_SYSTEM_MENUPOPUPSTART"
	case 0x0007:
		return "EVENT_SYSTEM_MENUPOPUPEND"
	case 0x0008:
		return "EVENT_SYSTEM_CAPTURESTART"
	case 0x0009:
		return "EVENT_SYSTEM_CAPTUREEND"
	case 0x000A:
		return "EVENT_SYSTEM_MOVESIZESTART"
	case 0x000B:
		return "EVENT_SYSTEM_MOVESIZEEND"
	case 0x000C:
		return "EVENT_SYSTEM_CONTEXTHELPSTART"
	case 0x000D:
		return "EVENT_SYSTEM_CONTEXTHELPEND"
	case 0x000E:
		return "EVENT_SYSTEM_DRAGDROPSTART"
	case 0x000F:
		return "EVENT_SYSTEM_DRAGDROPEND"
	case 0x0010:
		return "EVENT_SYSTEM_DIALOGSTART"
	case 0x0011:
		return "EVENT_SYSTEM_DIALOGEND"
	case 0x0012:
		return "EVENT_SYSTEM_SCROLLINGSTART"
	case 0x0013:
		return "EVENT_SYSTEM_SCROLLINGEND"
	case 0x0014:
		return "EVENT_SYSTEM_SWITCHSTART"
	case 0x0015:
		return "EVENT_SYSTEM_SWITCHEND"
	case 0x0016:
		return "EVENT_SYSTEM_MINIMIZESTART"
	case 0x0017:
		return "EVENT_SYSTEM_MINIMIZEEND"
	case 0x8000:
		return "EVENT_OBJECT_CREATE"
	case 0x8001:
		return "EVENT_OBJECT_DESTROY"
	case 0x8002:
		return "EVENT_OBJECT_SHOW"
	case 0x8003:
		return "EVENT_OBJECT_HIDE"
	case 0x8004:
		return "EVENT_OBJECT_REORDER"
	case 0x8005:
		return "EVENT_OBJECT_FOCUS"
	case 0x8006:
		return "EVENT_OBJECT_SELECTION"
	case 0x8007:
		return "EVENT_OBJECT_SELECTIONADD"
	case 0x8008:
		return "EVENT_OBJECT_SELECTIONREMOVE"
	case 0x8009:
		return "EVENT_OBJECT_SELECTIONWITHIN"
	case 0x800A:
		return "EVENT_OBJECT_STATECHANGE"
	case 0x800B:
		return "EVENT_OBJECT_LOCATIONCHANGE"
	case 0x800C:
		return "EVENT_OBJECT_NAMECHANGE"
	case 0x800D:
		return "EVENT_OBJECT_DESCRIPTIONCHANGE"
	case 0x800E:
		return "EVENT_OBJECT_VALUECHANGE"
	case 0x800F:
		return "EVENT_OBJECT_PARENTCHANGE"
	case 0x8010:
		return "EVENT_OBJECT_HELPCHANGE"
	case 0x8011:
		return "EVENT_OBJECT_DEFACTIONCHANGE"
	case 0x8012:
		return "EVENT_OBJECT_ACCELERATORCHANGE"
	default:
		return fmt.Sprintf("EVENT_0x%04X", event)
	}
}
