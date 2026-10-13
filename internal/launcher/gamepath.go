package launcher

import (
	"log/slog"
	"os"
	"strings"
)

// GamePathSource identifies which of resolveGameExe's candidate sources
// produced the effective path, surfaced to the frontend via GamePathInfo.
type GamePathSource string

const (
	GamePathEnv      GamePathSource = "env"
	GamePathSetting  GamePathSource = "setting"
	GamePathRegistry GamePathSource = "registry"
	GamePathNone     GamePathSource = "none"
)

// readGameExeFromRegistryFn is the indirection point for tests.
// Real impl lives in gamepath_windows.go / gamepath_other.go.
var readGameExeFromRegistryFn = readGameExeFromRegistry

// resolveGameExe picks the env override, then the setting, then the registry install path, falling back to GamePathNone, and never validates what it returns — checkGameExe does that uniformly for every source.
func resolveGameExe(setting string) (string, GamePathSource) {
	if v := normalizeGamePath(os.Getenv(gameExeEnvVar)); v != "" {
		slog.Info("resolveGameExe: env var hit", "var", gameExeEnvVar)
		return v, GamePathEnv
	}
	if setting != "" {
		slog.Info("resolveGameExe: setting hit")
		return setting, GamePathSetting
	}
	if path, ok := readGameExeFromRegistryFn(); ok {
		slog.Info("resolveGameExe: registry hit", "path_len", len(path))
		return path, GamePathRegistry
	}
	slog.Warn("resolveGameExe: no source resolved", "tried", []string{
		"$" + gameExeEnvVar,
		"setting",
		gameRegistryHive + `\` + gameRegistrySubkey + `\` + gameRegistryValue,
	})
	return "", GamePathNone
}

// unquote strips one matching pair of leading/trailing double quotes, so BEANFUN_GAME_EXE set from a shell like `set BEANFUN_GAME_EXE="C:\Game.exe"` resolves to the bare path.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// normalizeGamePath trims surrounding whitespace and one matching pair of double quotes, so a path pasted from Explorer's "Copy as path" (which quotes it) is treated the same as a bare path.
func normalizeGamePath(s string) string {
	return unquote(strings.TrimSpace(s))
}
