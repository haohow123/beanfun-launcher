package launcher

import (
	"errors"
	"testing"
)

// withFakeRegistry swaps the package-level registry probe for the
// test, with automatic restore via t.Cleanup.
func withFakeRegistry(t *testing.T, value string, found bool) {
	t.Helper()
	orig := readGameExeFromRegistryFn
	readGameExeFromRegistryFn = func() (string, bool) { return value, found }
	t.Cleanup(func() { readGameExeFromRegistryFn = orig })
}

func TestResolveGameExe_EnvVarWins(t *testing.T) {
	withEnv(t, gameExeEnvVar, `C:\custom\path.exe`)
	withFakeRegistry(t, `C:\registry\path.exe`, true)

	got, err := resolveGameExe()
	if err != nil {
		t.Fatalf("resolveGameExe: %v", err)
	}
	if got != `C:\custom\path.exe` {
		t.Errorf("got %q, want env var override", got)
	}
}

func TestResolveGameExe_FallsBackToRegistry(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, `C:\registry\path.exe`, true)

	got, err := resolveGameExe()
	if err != nil {
		t.Fatalf("resolveGameExe: %v", err)
	}
	if got != `C:\registry\path.exe` {
		t.Errorf("got %q, want registry value", got)
	}
}

func TestResolveGameExe_BothMissingReturnsErrGameExeMissing(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, "", false)

	_, err := resolveGameExe()
	var le *LauncherError
	if !errors.As(err, &le) || le.Kind != KindGameExeMissing {
		t.Errorf("got %v, want KindGameExeMissing", err)
	}
}

func TestResolveGameExe_EnvVarBeatsRegistryEvenIfRegistryEmpty(t *testing.T) {
	// Sanity: env-var-set path wins even if the registry probe would
	// return false. Stops anyone "fixing" the resolver by accidentally
	// gating env-var resolution on a registry hit.
	withEnv(t, gameExeEnvVar, `D:\override.exe`)
	withFakeRegistry(t, "", false)

	got, err := resolveGameExe()
	if err != nil {
		t.Fatalf("resolveGameExe: %v", err)
	}
	if got != `D:\override.exe` {
		t.Errorf("got %q, want D:\\override.exe", got)
	}
}
