//go:build windows

package launcher

import (
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

const (
	// Verified against an actual Beanfun TW MapleStory install
	// (haohow123's box, 2026-05-13). Pungin's reference suggested
	// HKCU\SOFTWARE\Gamania\MapleStory with value "ExecPath", but
	// the live registry on a current Beanfun install has:
	//
	//   HKLM\SOFTWARE\Gamania\MAPLESTORY (key, uppercase tail)
	//     Path  REG_SZ  D:\MapleStory
	//
	// `Path` holds the install directory, not the binary itself —
	// the executable lives at <dir>\MapleStory.exe.
	gameRegistryHive         = "HKLM"
	gameRegistrySubkey       = `SOFTWARE\Gamania\MAPLESTORY`
	gameRegistryValue        = "Path"
	gameExeRelativeToInstall = "MapleStory.exe"
)

// readGameExeFromRegistry opens HKLM\{gameRegistrySubkey}, reads the
// `Path` value (install directory), and appends MapleStory.exe.
// Returns ("", false) on any failure (key missing, value missing,
// type mismatch, empty string).
//
// HKLM\SOFTWARE on 64-bit Windows: golang.org/x/sys/windows/registry
// opens the 64-bit view by default. Beanfun's installer also writes
// to the 64-bit view, so no Wow6432Node redirect needed.
func readGameExeFromRegistry() (string, bool) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, gameRegistrySubkey, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer func() { _ = k.Close() }()

	dir, _, err := k.GetStringValue(gameRegistryValue)
	if err != nil || dir == "" {
		return "", false
	}
	return filepath.Join(dir, gameExeRelativeToInstall), true
}
