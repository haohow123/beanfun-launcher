//go:build !windows

package launcher

import "context"

func init() {
	spawnFn = osSpawn
}

// osSpawn on non-Windows always refuses. The macOS dev box exists to
// edit + test backend logic and frontend UI; only production Windows
// binaries actually run game.exe.
func osSpawn(_ context.Context, _ string, _ []string) error {
	return ErrPlatformUnsupported()
}
