//go:build !windows

package launcher

// Constants exist on non-Windows builds so resolveGameExe can include
// them in the "tried" diagnostic — see resolveGameExe. Values are
// textual only here (the registry is never actually queried on macOS
// / Linux). Mirrors gamepath_windows.go.
const (
	gameRegistryHive         = "HKLM"
	gameRegistrySubkey       = `SOFTWARE\Gamania\MAPLESTORY`
	gameRegistryValue        = "Path"
	gameExeRelativeToInstall = "MapleStory.exe"
)

// readGameExeFromRegistry on non-Windows always misses — there's no
// Windows registry to read. macOS dev builds resolve game.exe paths
// exclusively via the BEANFUN_GAME_EXE env var.
func readGameExeFromRegistry() (string, bool) {
	return "", false
}
