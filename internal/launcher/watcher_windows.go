//go:build windows

package launcher

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() {
	windowPIDFn = getWindowPID
	waitForProcessExitFn = waitForProcessExit
}

// GetWindowThreadProcessId is not exported by golang.org/x/sys/windows,
// so we bind it here alongside the other user32 procs declared in
// inject_windows.go. user32 itself is declared there; we only need the
// new proc handle in this file.
//
// https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getwindowthreadprocessid
var procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")

// getWindowPID wraps Win32 GetWindowThreadProcessId (user32.dll).
//
// The API's return value is the thread ID of the window's message pump;
// the process ID is written into the out-param. We only need the PID —
// the thread ID is discarded.
func getWindowPID(hwnd uintptr) (uint32, error) {
	var pid uint32
	// Third return value from LazyProc.Call is the last Win32 error,
	// captured eagerly before any intervening syscall can overwrite it.
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		// pid == 0 means the call failed (invalid HWND or the window's
		// owning process has already exited). Treat it as an error so
		// the caller can fall back or abort phase 2 cleanly.
		return 0, fmt.Errorf("GetWindowThreadProcessId(hwnd=0x%X): pid=0, last error: %w",
			hwnd, windows.GetLastError())
	}
	return pid, nil
}

// waitForProcessExit wraps Win32 WaitForSingleObject (kernel32.dll).
//
// We open with SYNCHRONIZE-only access so this function requires no
// special privilege beyond what the launcher already holds — no
// PROCESS_QUERY_INFORMATION or PROCESS_VM_READ needed.
//
// Cancellation uses a 1 s WaitForSingleObject timeout rather than
// INFINITE + handle-close. Closing a handle under INFINITE from a
// second goroutine causes double-close UB that needs sync.Once
// coordination. The short-timeout loop is one goroutine with no race:
// the kernel sleep + ctx-flag check costs far less than a FindWindow
// tree walk, keeping "queue over cron" intact.
func waitForProcessExit(ctx context.Context, pid uint32) error {
	// https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-openprocess
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return fmt.Errorf("OpenProcess(pid=%d): %w", pid, err)
	}
	defer windows.CloseHandle(h)

	for {
		// Check for cancellation before each wait so a context that is
		// already cancelled on entry returns immediately without blocking.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// https://learn.microsoft.com/en-us/windows/win32/api/synchapi/nf-synchapi-waitforsingleobject
		ev, err := windows.WaitForSingleObject(h, 1000) // 1 s timeout
		if err != nil {
			return fmt.Errorf("WaitForSingleObject(pid=%d): %w", pid, err)
		}
		if ev == windows.WAIT_OBJECT_0 {
			// Handle signaled — the process has exited.
			return nil
		}
		// WAIT_TIMEOUT (0x00000102): process still running; loop and
		// recheck ctx before sleeping again.
	}
}
