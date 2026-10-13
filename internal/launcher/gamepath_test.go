package launcher

import "testing"

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

	got, src := resolveGameExe(`C:\setting\path.exe`)
	if got != `C:\custom\path.exe` || src != GamePathEnv {
		t.Errorf("got (%q, %q), want (env override, %q)", got, src, GamePathEnv)
	}
}

// TestResolveGameExe_SettingBeatsRegistry pins that the settings-page path
// beats the registry, so swapping resolveGameExe's setting/registry
// branches fails it.
func TestResolveGameExe_SettingBeatsRegistry(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, `C:\registry\path.exe`, true)

	got, src := resolveGameExe(`C:\setting\path.exe`)
	if got != `C:\setting\path.exe` || src != GamePathSetting {
		t.Errorf("got (%q, %q), want (setting, %q)", got, src, GamePathSetting)
	}
}

func TestResolveGameExe_FallsBackToRegistry(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, `C:\registry\path.exe`, true)

	got, src := resolveGameExe("")
	if got != `C:\registry\path.exe` || src != GamePathRegistry {
		t.Errorf("got (%q, %q), want (registry value, %q)", got, src, GamePathRegistry)
	}
}

func TestResolveGameExe_NoneFound(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, "", false)

	got, src := resolveGameExe("")
	if got != "" || src != GamePathNone {
		t.Errorf("got (%q, %q), want (\"\", %q)", got, src, GamePathNone)
	}
}

// TestResolveGameExe_EnvVar_TrimsAndUnquotes pins resolveGameExe cleaning up
// a BEANFUN_GAME_EXE value a user set from a shell with surrounding
// whitespace or a quoted path.
func TestResolveGameExe_EnvVar_TrimsAndUnquotes(t *testing.T) {
	withEnv(t, gameExeEnvVar, `  "C:\custom\path.exe"  `)
	withFakeRegistry(t, "", false)

	got, src := resolveGameExe("")
	if want := `C:\custom\path.exe`; got != want || src != GamePathEnv {
		t.Errorf("got (%q, %q), want (%q, %q)", got, src, want, GamePathEnv)
	}
}

func TestResolveGameExe_EnvVarBeatsRegistryEvenIfRegistryEmpty(t *testing.T) {
	// Sanity: env-var-set path wins even if the registry probe would
	// return false. Stops anyone "fixing" the resolver by accidentally
	// gating env-var resolution on a registry hit.
	withEnv(t, gameExeEnvVar, `D:\override.exe`)
	withFakeRegistry(t, "", false)

	got, src := resolveGameExe("")
	if got != `D:\override.exe` || src != GamePathEnv {
		t.Errorf("got (%q, %q), want (D:\\override.exe, %q)", got, src, GamePathEnv)
	}
}
