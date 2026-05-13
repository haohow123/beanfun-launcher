//go:build windows

package launcher

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
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

// startEventDiagnostic registers a WinEvent hook scoped to the game
// window's owning process and logs every event in the diagnostic
// range. Use after CREATE has been detected so we can answer:
// "does the form fire any event in response to PostMessage?" If yes,
// that event becomes the retry trigger; if not, we fall back to a
// blind retry interval.
//
// Lifetime tied to ctx — caller cancels to stop the hook.
func startEventDiagnostic(ctx context.Context, gameHwnd uintptr) {
	var pid uint32
	procGetWindowThreadProcessId.Call(
		gameHwnd,
		uintptr(unsafe.Pointer(&pid)),
	)
	if pid == 0 {
		slog.Error("DiagEvent: GetWindowThreadProcessId returned 0; skipping hook")
		return
	}
	slog.Info("DiagEvent: hook starting", "pid", pid)
	go runDiagEventHookPump(ctx, pid)
}

// runDiagEventHookPump owns the WinEvent hook + message pump on a
// locked OS thread (Win32 requires both). On ctx cancel a watcher
// goroutine posts WM_QUIT to wake the pump, the pump exits, and the
// deferred UnhookWinEvent cleans up.
func runDiagEventHookPump(ctx context.Context, pid uint32) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	callback := syscall.NewCallback(func(
		_ uintptr,
		event uint32,
		hwnd uintptr,
		idObject int32,
		idChild int32,
		_ uint32,
		_ uint32,
	) uintptr {
		slog.Info("DiagEvent",
			"event", fmt.Sprintf("0x%04X", event),
			"hwnd", fmt.Sprintf("0x%X", hwnd),
			"obj", idObject,
			"child", idChild,
		)
		return 0
	})

	hook, _, _ := procSetWinEventHook.Call(
		eventSystemForeground, // eventMin
		eventObjectNameChange, // eventMax
		0,                     // hmodWinEventProc (NULL for out-of-context)
		callback,
		uintptr(pid), // idProcess — scope events to the game's PID
		0,            // idThread
		winEventOutOfContext|winEventSkipOwnProc,
	)
	if hook == 0 {
		slog.Error("DiagEvent: SetWinEventHook returned 0")
		return
	}
	defer procUnhookWinEvent.Call(hook)

	threadID := windows.GetCurrentThreadId()
	go func() {
		<-ctx.Done()
		_, _, _ = procPostThreadMessageW.Call(uintptr(threadID), wmQuit, 0, 0)
	}()

	var msg win32Msg
	for {
		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0, 0, 0,
		)
		if ret == 0 || ret == ^uintptr(0) {
			slog.Info("DiagEvent: hook stopped")
			return
		}
	}
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
