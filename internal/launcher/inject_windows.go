//go:build windows

package launcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() {
	findGameWindowFn = findGameWindow
	waitForGameWindowFn = waitForGameWindow
	injectFn = injectCredentials
}

// Win32 procs we need but aren't exported by golang.org/x/sys/windows.
var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procGetClassNameW       = user32.NewProc("GetClassNameW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procSetWinEventHook     = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent      = user32.NewProc("UnhookWinEvent")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
)

const (
	eventObjectCreate    = 0x8000
	winEventOutOfContext = 0x0000
	winEventSkipOwnProc  = 0x0002
	objidWindow          = 0x00000000
	wmQuit               = 0x0012
)

// win32Msg mirrors Win32's MSG struct for use with GetMessageW.
type win32Msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

const (
	wmChar    = 0x0102 // WM_CHAR
	wmKeyDown = 0x0100 // WM_KEYDOWN

	vkBack   = 0x08 // VK_BACK
	vkTab    = 0x09 // VK_TAB
	vkReturn = 0x0D // VK_RETURN
	vkEnd    = 0x23 // VK_END

	// Two-tier window class fallback. The TW build's class diverges
	// in suffix from time to time, so we probe both before giving up.
	// If both miss, the caller falls back to clipboard copy for
	// manual paste.
	classMapleStory   = "MapleStoryClass"
	classMapleStoryTW = "MapleStoryClassTW"

	// Backspace counts when clearing the login fields. Chosen
	// generously so they reliably empty whatever's already there:
	// 64 covers account-id-sized fields, 20 covers OTP-sized.
	clearAccountKeyCount  = 64
	clearPasswordKeyCount = 20
)

// findGameWindow tries the known MapleStory window classes and
// returns the first non-zero HWND, or 0 if none are open.
func findGameWindow() uintptr {
	for _, class := range []string{classMapleStory, classMapleStoryTW} {
		classW, err := windows.UTF16PtrFromString(class)
		if err != nil {
			continue
		}
		h, _, _ := procFindWindowW.Call(
			uintptr(unsafe.Pointer(classW)),
			0, // lpWindowName — NULL matches any window title
		)
		if h != 0 {
			return h
		}
	}
	return 0
}

// waitForGameWindow registers a system-wide WinEvent hook for
// EVENT_OBJECT_CREATE and returns the first MapleStory window's
// HWND, or an error on context cancel / timeout.
//
// Architecture (Win32 hooks fundamentally require thread-local
// state + a message pump):
//
//  1. Run a goroutine locked to one OS thread (Win32 hooks are
//     thread-bound; cross-thread hook delivery is not allowed).
//  2. That thread calls SetWinEventHook with WINEVENT_OUTOFCONTEXT
//     — the hook callback runs in our process, no DLL injection
//     into the target.
//  3. The thread runs a GetMessage / TranslateMessage /
//     DispatchMessage pump. SetWinEventHook delivers its callbacks
//     via this pump.
//  4. The callback checks the new window's class name; matches
//     publish to a `found` channel.
//  5. The caller goroutine waits on `found` / ctx / timeout; on
//     any exit it posts WM_QUIT to wake up the pump and the hook
//     thread terminates cleanly (deferred UnhookWinEvent + pump
//     return).
//
// Why event-based instead of polling: 200ms poll worked, but
// SetWinEventHook fires within ~milliseconds of window creation —
// no wasted FindWindowW calls, cleaner pattern, and the same
// approach lets us later subscribe to "game window closed"
// (EVENT_OBJECT_DESTROY) events if we want.
func waitForGameWindow(ctx context.Context, timeout time.Duration) (uintptr, error) {
	// Immediate probe — game may already be running.
	if h := findGameWindow(); h != 0 {
		return h, nil
	}

	found := make(chan uintptr, 1)
	threadIDCh := make(chan uint32, 1)
	pumpDone := make(chan struct{})

	go runHookPump(found, threadIDCh, pumpDone)

	// Wait for the hook thread to publish its thread ID; we need it
	// to PostThreadMessageW(WM_QUIT) on cleanup. The pump should be
	// up within milliseconds of the goroutine starting.
	var threadID uint32
	select {
	case threadID = <-threadIDCh:
	case <-ctx.Done():
		<-pumpDone
		return 0, ctx.Err()
	case <-time.After(2 * time.Second):
		return 0, errors.New("hook thread failed to start")
	}

	defer func() {
		_, _, _ = procPostThreadMessageW.Call(uintptr(threadID), wmQuit, 0, 0)
		<-pumpDone
	}()

	select {
	case h := <-found:
		return h, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(timeout):
		return 0, errors.New("game window did not appear within timeout")
	}
}

// runHookPump owns the SetWinEventHook + message-pump lifecycle on
// one locked OS thread.
func runHookPump(found chan<- uintptr, threadIDOut chan<- uint32, pumpDone chan<- struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(pumpDone)

	callback := syscall.NewCallback(func(
		_ uintptr, // hWinEventHook
		_ uint32, // event
		hwnd uintptr,
		idObject int32,
		_ int32, // idChild
		_ uint32, // idEventThread
		_ uint32, // dwmsEventTime
	) uintptr {
		// EVENT_OBJECT_CREATE fires for windows AND for every child
		// accessibility object inside them; idObject=OBJID_WINDOW (0)
		// is the top-level window event we care about.
		if hwnd == 0 || idObject != objidWindow {
			return 0
		}
		if !classMatchesGame(hwnd) {
			return 0
		}
		// Non-blocking — first match wins.
		select {
		case found <- hwnd:
		default:
		}
		return 0
	})

	hook, _, _ := procSetWinEventHook.Call(
		eventObjectCreate, // eventMin
		eventObjectCreate, // eventMax
		0,                 // hmodWinEventProc (NULL for out-of-context)
		callback,
		0, // idProcess (0 = all)
		0, // idThread  (0 = all)
		winEventOutOfContext|winEventSkipOwnProc,
	)
	if hook == 0 {
		slog.Error("SetWinEventHook returned 0; falling back to find-only path")
		threadIDOut <- windows.GetCurrentThreadId()
		return
	}
	defer procUnhookWinEvent.Call(hook)

	threadIDOut <- windows.GetCurrentThreadId()

	var msg win32Msg
	for {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		// GetMessageW returns 0 on WM_QUIT; -1 (^uintptr(0)) on error.
		if ret == 0 || ret == ^uintptr(0) {
			return
		}
		// We never call TranslateMessage / DispatchMessage —
		// SetWinEventHook callbacks are dispatched internally by
		// the GetMessage call itself for hooks installed on this
		// thread. Skipping the dispatch loop avoids accidentally
		// invoking other window procs on a thread that owns no
		// windows.
	}
}

// classMatchesGame reads the window class name and compares it
// against the known MapleStory classes.
func classMatchesGame(hwnd uintptr) bool {
	var buf [256]uint16
	n, _, _ := procGetClassNameW.Call(
		hwnd,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if n == 0 {
		return false
	}
	name := windows.UTF16ToString(buf[:n])
	return name == classMapleStory || name == classMapleStoryTW
}

// injectCredentials types the account + OTP into the game's currently
// focused login form. The sequence mirrors Beanfun's WPF launcher
// (MainWindow.xaml.cs L2158-2238):
//
//  1. SetForegroundWindow + 100ms settle.
//  2. Clear account: VK_END (caret to end) + N×VK_BACK.
//  3. Type account: per-byte WM_CHAR.
//  4. VK_TAB to password field.
//  5. Clear password: VK_END + N×VK_BACK.
//  6. Type OTP: per-byte WM_CHAR.
//  7. VK_RETURN to submit.
//
// Per the WPF code's lParam analysis, MapleStory dispatches on
// wParam (the VK) and ignores lParam's scan-code bits in standard
// input controls — so we emit lParam=1 (repeat count only) without
// MapVirtualKey scan-code computation. Safe for this game.
func injectCredentials(hwnd uintptr, account, otp []byte) error {
	// Foregrounding can fail (other process holds SetForegroundLock,
	// fullscreen mode, etc.) — try the typing anyway; the window
	// receives messages even without focus, but typing into the
	// right control is more reliable when foregrounded.
	procSetForegroundWindow.Call(hwnd)
	time.Sleep(100 * time.Millisecond)

	if err := clearField(hwnd, clearAccountKeyCount); err != nil {
		return err
	}
	if err := typeBytes(hwnd, account); err != nil {
		return err
	}
	if err := postKey(hwnd, vkTab); err != nil {
		return err
	}
	if err := clearField(hwnd, clearPasswordKeyCount); err != nil {
		return err
	}
	if err := typeBytes(hwnd, otp); err != nil {
		return err
	}
	return postKey(hwnd, vkReturn)
}

func clearField(hwnd uintptr, backCount int) error {
	if err := postKey(hwnd, vkEnd); err != nil {
		return err
	}
	for range backCount {
		if err := postKey(hwnd, vkBack); err != nil {
			return err
		}
	}
	return nil
}

func typeBytes(hwnd uintptr, bs []byte) error {
	for _, b := range bs {
		if err := postMessage(hwnd, wmChar, uintptr(b), 0); err != nil {
			return err
		}
	}
	return nil
}

func postKey(hwnd uintptr, vk uintptr) error {
	return postMessage(hwnd, wmKeyDown, vk, 1) // lParam=1 (repeat count only)
}

func postMessage(hwnd uintptr, msg uint32, wparam, lparam uintptr) error {
	ret, _, callErr := procPostMessageW.Call(hwnd, uintptr(msg), wparam, lparam)
	if ret == 0 {
		return fmt.Errorf("PostMessageW(hwnd=0x%X, msg=0x%X, wparam=0x%X): %w", hwnd, msg, wparam, callErr)
	}
	return nil
}
