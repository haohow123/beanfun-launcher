//go:build windows

package launcher

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 procs needed only by the diagnostic enumerators. Kept in a
// separate file so the production injection path stays small.
var (
	procEnumWindows     = user32.NewProc("EnumWindows")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
	procGetWindowRect   = user32.NewProc("GetWindowRect")
)

type win32Rect struct {
	Left, Top, Right, Bottom int32
}

// startDiagnostic spawns two pollers (windows + processes) that run
// until ctx is cancelled. Both emit `Diag*` slog lines we read off
// the launcher.log after a launch to map the patcher → main game
// timeline. Off by default in shipped builds — Launch wires this on
// only while the M8.x form-ready signal is still being characterised.
func startDiagnostic(ctx context.Context) {
	slog.Info("Diag: start")
	go diagWindowsLoop(ctx)
	go diagProcessesLoop(ctx)
}

func diagWindowsLoop(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("Diag: windows loop done")
			return
		case <-ticker.C:
			enumerateMapleWindows()
		}
	}
}

func diagProcessesLoop(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("Diag: process loop done")
			return
		case <-ticker.C:
			enumerateMapleProcesses()
		}
	}
}

// enumerateMapleWindows walks every top-level window and logs the
// ones whose class or title contain "maple" (case-insensitive).
// Each log line carries class, title, visibility, and dimensions —
// enough to distinguish patcher / launcher / login form by shape
// even if class names overlap.
func enumerateMapleWindows() {
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		class := className(hwnd)
		title := windowTitle(hwnd)
		lc := strings.ToLower(class)
		lt := strings.ToLower(title)
		if !strings.Contains(lc, "maple") && !strings.Contains(lt, "maple") {
			return 1 // continue enumeration
		}
		var r win32Rect
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		vis, _, _ := procIsWindowVisible.Call(hwnd)
		slog.Info("DiagWindow",
			"hwnd", fmt.Sprintf("0x%X", hwnd),
			"class", class,
			"title", title,
			"visible", vis != 0,
			"rect", fmt.Sprintf("%dx%d@%d,%d", r.Right-r.Left, r.Bottom-r.Top, r.Left, r.Top),
		)
		return 1
	})
	procEnumWindows.Call(callback, 0)
}

// className wraps GetClassNameW and returns "" on failure.
func className(hwnd uintptr) string {
	var buf [256]uint16
	n, _, _ := procGetClassNameW.Call(
		hwnd,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if n == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

// enumerateMapleProcesses snapshots the full process list and logs
// the ones whose exe name contains "maple" or "patcher"
// (case-insensitive). Includes parent PID so we can read the
// process tree (which exe spawns which) from the log.
func enumerateMapleProcesses() {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return
	}
	for {
		exe := windows.UTF16ToString(entry.ExeFile[:])
		le := strings.ToLower(exe)
		if strings.Contains(le, "maple") || strings.Contains(le, "patcher") {
			slog.Info("DiagProcess",
				"pid", entry.ProcessID,
				"ppid", entry.ParentProcessID,
				"exe", exe,
			)
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			return
		}
	}
}
