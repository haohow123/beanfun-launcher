//go:build !windows

package launcher

import "context"

// Non-Windows binding for the watcher indirection vars. macOS dev
// never actually reaches phase 2 — findGameWindowFn returns 0
// always on non-Windows, so the watcher times out in phase 1 and
// emits the PID=0 signal without these stubs being called. They
// exist purely to satisfy the var bindings.
func init() {
	windowPIDFn = func(_ uintptr) (uint32, error) {
		return 0, ErrPlatformUnsupported()
	}
	waitForProcessExitFn = func(_ context.Context, _ uint32) error {
		return ErrPlatformUnsupported()
	}
}
