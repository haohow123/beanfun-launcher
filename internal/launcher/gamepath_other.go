//go:build !windows

package launcher

// Constants exist on non-Windows builds so the resolver can include
// them in the "tried" diagnostic — see resolveGameExe. Their values
// are textual only here (the registry is never actually queried on
// macOS / Linux).
const (
	gameRegistrySubkey = `SOFTWARE\Gamania\MapleStory`
	gameRegistryValue  = "ExecPath"
)

// readGameExeFromRegistry on non-Windows always misses — there's no
// Windows registry to read. macOS dev builds resolve game.exe paths
// exclusively via the BEANFUN_GAME_EXE env var.
func readGameExeFromRegistry() (string, bool) {
	return "", false
}
