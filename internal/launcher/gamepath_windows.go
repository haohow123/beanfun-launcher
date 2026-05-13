//go:build windows

package launcher

import (
	"golang.org/x/sys/windows/registry"
)

const (
	// Path under HKEY_CURRENT_USER that Beanfun's installer writes
	// to when MapleStory TW is installed. Reverse-engineered by
	// pungin; we use the same key.
	gameRegistrySubkey = `SOFTWARE\Gamania\MapleStory`
	gameRegistryValue  = "ExecPath"
)

// readGameExeFromRegistry reads HKCU\{gameRegistrySubkey}\{gameRegistryValue}
// as a string value. Returns ("", false) on any failure (key missing,
// value missing, type mismatch, empty string).
//
// HKCU rather than HKLM: pungin's reverse-engineering confirmed that
// Beanfun writes per-user, not machine-wide. No Wow6432Node redirect
// dance needed because HKCU lives outside the WoW64 redirector for
// user-scope keys.
func readGameExeFromRegistry() (string, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, gameRegistrySubkey, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer func() { _ = k.Close() }()

	val, _, err := k.GetStringValue(gameRegistryValue)
	if err != nil || val == "" {
		return "", false
	}
	return val, true
}
