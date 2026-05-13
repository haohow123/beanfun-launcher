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
	procGetClientRect       = user32.NewProc("GetClientRect")
	procClientToScreen      = user32.NewProc("ClientToScreen")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetCursorPos        = user32.NewProc("SetCursorPos")
)

const (
	wmChar        = 0x0102 // WM_CHAR
	wmKeyDown     = 0x0100 // WM_KEYDOWN
	wmLButtonDown = 0x0201 // WM_LBUTTONDOWN

	vkBack   = 0x08 // VK_BACK
	vkTab    = 0x09 // VK_TAB
	vkReturn = 0x0D // VK_RETURN
	vkEscape = 0x1B // VK_ESCAPE
	vkEnd    = 0x23 // VK_END

	mkLButton = 0x0001 // WM_LBUTTONDOWN wParam: left button pressed

	// Two-tier window class fallback. The TW build's class diverges
	// in suffix from time to time, so we probe both before giving up.
	// If both miss, the caller treats it as "no game window" and the
	// frontend prompts the user to press 啟動遊戲.
	classMapleStory   = "MapleStoryClass"
	classMapleStoryTW = "MapleStoryClassTW"

	// Backspace counts when clearing the login fields. Chosen
	// generously so they reliably empty whatever's already there:
	// 64 covers account-id-sized fields, 20 covers OTP-sized.
	clearAccountKeyCount  = 64
	clearPasswordKeyCount = 20

	// Click ratios for the pre-typing focus click. The login textbox
	// is roughly at the horizontal centre, slightly above vertical
	// centre — clicking there pushes keyboard focus into it before we
	// send WM_CHAR. The 0.5 / 0.4 ratios come from Beanfun WPF L2205-08.
	clickXRatio = 0.5
	clickYRatio = 0.4
)

// win32Rect mirrors Win32's RECT struct.
type win32Rect struct {
	Left, Top, Right, Bottom int32
}

// win32Point mirrors Win32's POINT struct.
type win32Point struct {
	X, Y int32
}

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
//  2. preTypingClick: ESC + click at (50%, 40%) of client area to
//     drop keyboard focus onto the login textbox.
//  3. Clear account: VK_END (caret to end) + N×VK_BACK.
//  4. Type account: per-byte WM_CHAR.
//  5. VK_TAB to password field.
//  6. Clear password: VK_END + N×VK_BACK.
//  7. Type OTP: per-byte WM_CHAR.
//  8. VK_RETURN to submit.
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

	// MapleStory TW renders its login form inside the DirectX swap
	// chain; WM_CHAR alone reaches the window but its input handler
	// won't route the chars to the account/password textboxes unless
	// the form has explicit pointer focus. Beanfun WPF L2198-2216
	// solves this by sending VK_ESCAPE (dismiss any pre-login popup)
	// then synthesising a left-click roughly over the login textbox
	// (50% × 40% of the client area). The click moves keyboard focus
	// into the textbox so subsequent WM_CHAR types into the right
	// place.
	if err := preTypingClick(hwnd); err != nil {
		return err
	}

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

// preTypingClick dismisses any pre-login popup with ESC and then
// posts a synthetic left-click at (50%, 40%) of the game's client
// area. The cursor is briefly moved to the click point to satisfy
// the game's hit-test (some controls validate against the actual
// cursor position, not just lParam) and restored afterwards.
//
// Errors on the WM_LBUTTONDOWN post abort the inject — without a
// successful click the credentials would type into a defocused
// form. Cursor save/restore failures are cosmetic and swallowed.
func preTypingClick(hwnd uintptr) error {
	if err := postKey(hwnd, vkEscape); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)

	var rect win32Rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	width := rect.Right - rect.Left
	height := rect.Bottom - rect.Top
	clickX := int32(float64(width) * clickXRatio)
	clickY := int32(float64(height) * clickYRatio)

	var savedCursor win32Point
	cursorOK, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&savedCursor)))

	origin := win32Point{X: 0, Y: 0}
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&origin)))
	procSetCursorPos.Call(uintptr(origin.X+clickX), uintptr(origin.Y+clickY))

	lparam := uintptr(uint32(clickX)&0xFFFF) | uintptr(uint32(clickY))<<16
	if err := postMessage(hwnd, wmLButtonDown, mkLButton, lparam); err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)

	if cursorOK != 0 {
		procSetCursorPos.Call(uintptr(savedCursor.X), uintptr(savedCursor.Y))
	}
	return nil
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
