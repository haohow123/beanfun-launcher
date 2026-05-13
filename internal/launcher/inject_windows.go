//go:build windows

package launcher

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() {
	findGameWindowFn = findGameWindow
	injectFn = injectCredentials
}

// Win32 procs we need but aren't exported by golang.org/x/sys/windows.
var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procPostMessageW        = user32.NewProc("PostMessageW")
)

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
