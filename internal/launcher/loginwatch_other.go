//go:build !windows

package launcher

import "context"

// Non-Windows binding for installLoginWatchFn. macOS dev never
// reaches this code path — SpawnGame's spawnFn stub returns
// ErrPlatformUnsupported first, so M10's SpawnAndInject errors out
// before installLoginWatchFn would be called. The stub exists only
// to satisfy the package-level var so internal/launcher builds on
// non-Windows for CI / dev.
func init() {
	installLoginWatchFn = func(_ context.Context, _ uint32, _ uintptr, _, _ func()) (func(), error) {
		return func() {}, ErrPlatformUnsupported()
	}
}
