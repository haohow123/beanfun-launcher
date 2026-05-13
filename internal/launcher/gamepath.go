package launcher

import (
	"log/slog"
	"os"
)

// gameExeEnvVar is the override env var. If set, it wins regardless
// of registry state — useful for dev / testing / non-default install
// locations. Empty = fall through to registry detection.
//
// Defined here (not service.go) so the resolver can read it
// alongside the registry probe.

// readGameExeFromRegistryFn is the indirection point for tests.
// Real impl lives in gamepath_windows.go / gamepath_other.go.
var readGameExeFromRegistryFn = readGameExeFromRegistry

// resolveGameExe finds the game.exe path via:
//
//  1. `BEANFUN_GAME_EXE` env var — explicit override; wins outright.
//  2. Windows registry at the canonical install-info key written by
//     Beanfun's installer (see gamepath_windows.go for the exact
//     hive / subkey / value).
//  3. ErrGameExeMissing — nothing usable found.
//
// The registry value is the canonical source; the env var is an
// escape hatch for dev / unusual installs / overrides.
//
// On non-Windows builds step 2 always misses (registry doesn't
// exist), so the env var is the only working path.
func resolveGameExe() (string, error) {
	if v := os.Getenv(gameExeEnvVar); v != "" {
		slog.Info("resolveGameExe: env var hit", "var", gameExeEnvVar)
		return v, nil
	}
	if path, ok := readGameExeFromRegistryFn(); ok {
		slog.Info("resolveGameExe: registry hit", "path_len", len(path))
		return path, nil
	}
	slog.Warn("resolveGameExe: no source resolved", "tried", []string{
		"$" + gameExeEnvVar,
		gameRegistryHive + `\` + gameRegistrySubkey + `\` + gameRegistryValue,
	})
	return "", ErrGameExeMissing()
}
