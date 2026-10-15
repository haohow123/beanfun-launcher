package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
	"github.com/haohow123/beanfun-launcher/internal/bgtask"
	"github.com/haohow123/beanfun-launcher/internal/localpath"
)

// fakeGamePathStore is an in-memory GamePathStore for Service tests.
type fakeGamePathStore struct {
	path string
}

func (f *fakeGamePathStore) LoadGameExePath() string { return f.path }

func (f *fakeGamePathStore) SaveGameExePath(p string) error {
	f.path = p
	return nil
}

// fakeLoginSnapshotter returns a fixed, non-nil client/session pair so a
// test can reach Spawn*'s game-path guard without a real Beanfun login.
type fakeLoginSnapshotter struct {
	client  *beanfun.BeanfunClient
	session *beanfun.Session
}

func (f fakeLoginSnapshotter) Snapshot() (*beanfun.BeanfunClient, *beanfun.Session) {
	return f.client, f.session
}

func (f fakeLoginSnapshotter) EndSessionIfCurrent(*beanfun.BeanfunClient) bool { return true }

// withLoggedInSession returns a loginSnapshotter whose Snapshot() always
// reports a non-nil client and session.
func withLoggedInSession() loginSnapshotter {
	return fakeLoginSnapshotter{client: &beanfun.BeanfunClient{}, session: &beanfun.Session{}}
}

// withCountingFindGameWindow swaps findGameWindowFn for a call-counting
// stub returning 0, restoring the original via t.Cleanup.
func withCountingFindGameWindow(t *testing.T) *int {
	t.Helper()
	orig := findGameWindowFn
	calls := 0
	findGameWindowFn = func() uintptr {
		calls++
		return 0
	}
	t.Cleanup(func() { findGameWindowFn = orig })
	return &calls
}

func withLocalGameExe(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "MapleStory.exe")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	return path
}

// TestLauncherService_ResolveCheckedGameExe_Missing pins the gate firing without a live session.
func TestLauncherService_ResolveCheckedGameExe_Missing(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, "", false)

	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, nil)

	gameExe, warnings, gameErr := svc.resolveCheckedGameExe()
	if gameErr == nil {
		t.Fatalf("resolveCheckedGameExe() gameErr = nil, want *LauncherError")
	}
	if gameErr.Kind != KindGameExeMissing || gameErr.Msg != "no game path" {
		t.Errorf("gameErr = %+v, want Kind=KindGameExeMissing Msg=%q", gameErr, "no game path")
	}
	if gameExe != "" {
		t.Errorf("gameExe = %q, want empty", gameExe)
	}
	if warnings != nil {
		t.Errorf("warnings = %v, want nil", warnings)
	}
}

func TestLauncherService_ResolveCheckedGameExe_OK(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, "", false)
	path := withLocalGameExe(t)

	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, &fakeGamePathStore{path: path})

	gameExe, _, gameErr := svc.resolveCheckedGameExe()
	if gameErr != nil {
		t.Fatalf("resolveCheckedGameExe() gameErr = %v, want nil", gameErr)
	}
	if gameExe != path {
		t.Errorf("gameExe = %q, want %q", gameExe, path)
	}
}

// TestLauncherService_SpawnGame_MissingGameExe_WithSession_NeverCallsSpawnFn pins the game-path guard firing when a session is present.
func TestLauncherService_SpawnGame_MissingGameExe_WithSession_NeverCallsSpawnFn(t *testing.T) {
	fake := withFakeSpawn(t)
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, "", false)
	findCalls := withCountingFindGameWindow(t)

	mgr := bgtask.New()
	svc := NewLauncherService(withLoggedInSession(), mgr, nil)
	*findCalls = 0 // NewLauncherService's own startup probe doesn't count

	err := svc.SpawnGame(beanfun.Account{SID: "s", SSN: "1", SName: "n"})
	var le *LauncherError
	if !errors.As(err, &le) || le.Kind != KindGameExeMissing {
		t.Fatalf("got %v, want *LauncherError{Kind: KindGameExeMissing}", err)
	}
	if fake.called {
		t.Errorf("spawnFn called = true, want false")
	}
	if *findCalls != 0 {
		t.Errorf("findGameWindowFn called %d times, want 0", *findCalls)
	}
}

// TestLauncherService_SpawnGameClean_MissingGameExe_WithSession_NeverCallsSpawnFn mirrors the SpawnGame test above against SpawnGameClean.
func TestLauncherService_SpawnGameClean_MissingGameExe_WithSession_NeverCallsSpawnFn(t *testing.T) {
	fake := withFakeSpawn(t)
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, "", false)
	findCalls := withCountingFindGameWindow(t)

	mgr := bgtask.New()
	svc := NewLauncherService(withLoggedInSession(), mgr, nil)
	*findCalls = 0 // NewLauncherService's own startup probe doesn't count

	err := svc.SpawnGameClean()
	var le *LauncherError
	if !errors.As(err, &le) || le.Kind != KindGameExeMissing {
		t.Fatalf("got %v, want *LauncherError{Kind: KindGameExeMissing}", err)
	}
	if fake.called {
		t.Errorf("spawnFn called = true, want false")
	}
	if *findCalls != 0 {
		t.Errorf("findGameWindowFn called %d times, want 0", *findCalls)
	}
}

func TestLauncherService_GamePath(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, `C:\registry\MapleStory.exe`, true)
	path := withLocalGameExe(t)

	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, &fakeGamePathStore{path: path})

	info := svc.GamePath()
	if info.Setting != path {
		t.Errorf("Setting = %q, want %q", info.Setting, path)
	}
	if info.Detected != `C:\registry\MapleStory.exe` {
		t.Errorf("Detected = %q, want registry value regardless of setting", info.Detected)
	}
	if info.Effective != path || info.Source != GamePathSetting {
		t.Errorf("Effective/Source = %q/%q, want %q/%q", info.Effective, info.Source, path, GamePathSetting)
	}
	if !info.Check.OK {
		t.Errorf("Check.OK = false, want true (Problem=%q)", info.Check.Problem)
	}
}

// TestLauncherService_GamePath_InvalidSetting_NotLocal_NoFallbackToRegistry
// pins that an invalid persisted setting is surfaced as a failing check
// against the setting itself, rather than silently falling back to a
// working registry detection.
func TestLauncherService_GamePath_InvalidSetting_NotLocal_NoFallbackToRegistry(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, `C:\registry\MapleStory.exe`, true)

	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, &fakeGamePathStore{path: "relative/MapleStory.exe"})

	info := svc.GamePath()
	if info.Source != GamePathSetting {
		t.Errorf("Source = %q, want %q (no fallback to registry)", info.Source, GamePathSetting)
	}
	if info.Effective != "relative/MapleStory.exe" {
		t.Errorf("Effective = %q, want the invalid setting itself", info.Effective)
	}
	if info.Check.OK || info.Check.Problem != "not a local file" {
		t.Errorf("Check = %+v, want OK=false Problem=%q", info.Check, "not a local file")
	}
}

func TestLauncherService_SetGamePath_Clears(t *testing.T) {
	store := &fakeGamePathStore{path: "stale"}
	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, store)

	if err := svc.SetGamePath(""); err != nil {
		t.Fatalf("SetGamePath(\"\") = %v, want nil", err)
	}
	if store.path != "" {
		t.Errorf("store.path = %q, want empty", store.path)
	}
}

func TestLauncherService_SetGamePath_RejectsUNC(t *testing.T) {
	origCheck := localpath.CheckFn
	localpath.CheckFn = localpath.CheckWindowsPath
	t.Cleanup(func() { localpath.CheckFn = origCheck })
	origRemote := localpath.IsRemoteDriveFn
	localpath.IsRemoteDriveFn = func(string) bool { return false }
	t.Cleanup(func() { localpath.IsRemoteDriveFn = origRemote })

	store := &fakeGamePathStore{}
	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, store)

	err := svc.SetGamePath(`\\host\share\MapleStory.exe`)
	if !errors.Is(err, localpath.ErrNotLocal) {
		t.Fatalf("SetGamePath(UNC) = %v, want %v", err, localpath.ErrNotLocal)
	}
	if store.path != "" {
		t.Errorf("store.path = %q, want untouched (save must not happen on a rejected path)", store.path)
	}
}

// TestLauncherService_SetGamePath_RejectsNonExe pins SetGamePath rejecting a
// path whose extension isn't .exe without persisting it.
func TestLauncherService_SetGamePath_RejectsNonExe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MapleStory.lnk")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	store := &fakeGamePathStore{}
	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, store)

	err := svc.SetGamePath(path)
	var le *LauncherError
	if !errors.As(err, &le) || le.Kind != KindGamePathInvalid || le.Msg != problemNotExe {
		t.Fatalf("SetGamePath(.lnk) = %v, want *LauncherError{Kind: KindGamePathInvalid, Msg: %q}", err, problemNotExe)
	}
	if store.path != "" {
		t.Errorf("store.path = %q, want untouched (save must not happen on a rejected path)", store.path)
	}
}

// TestLauncherService_SetGamePath_SavesCleaned uses a host-native dirty path so the cleanup assertion holds on every CI host.
func TestLauncherService_SetGamePath_SavesCleaned(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "MapleStory.exe")
	if err := os.WriteFile(want, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	messy := dir + string(filepath.Separator) + "." + string(filepath.Separator) + "MapleStory.exe"

	store := &fakeGamePathStore{}
	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, store)

	if err := svc.SetGamePath(messy); err != nil {
		t.Fatalf("SetGamePath() = %v, want nil", err)
	}
	if store.path != want {
		t.Errorf("store.path = %q, want cleaned %q", store.path, want)
	}
}

// TestLauncherService_SetGamePath_TrimsWhitespace pins SetGamePath trimming
// surrounding whitespace before the localpath check and save, since a
// pasted path commonly carries it.
func TestLauncherService_SetGamePath_TrimsWhitespace(t *testing.T) {
	path := withLocalGameExe(t)
	store := &fakeGamePathStore{}
	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, store)

	if err := svc.SetGamePath("  " + path + "  "); err != nil {
		t.Fatalf("SetGamePath() = %v, want nil", err)
	}
	if store.path != path {
		t.Errorf("store.path = %q, want trimmed %q", store.path, path)
	}
}

// TestLauncherService_SetGamePath_UnquotesExplorerPath pins SetGamePath
// stripping the double quotes Explorer's "Copy as path" wraps around a path,
// so the saved value is still recognised as a local file.
func TestLauncherService_SetGamePath_UnquotesExplorerPath(t *testing.T) {
	path := withLocalGameExe(t)
	store := &fakeGamePathStore{}
	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, store)

	if err := svc.SetGamePath(`  "` + path + `"  `); err != nil {
		t.Fatalf("SetGamePath() = %v, want nil", err)
	}
	if store.path != path {
		t.Errorf("store.path = %q, want unquoted %q", store.path, path)
	}
}

func TestLauncherService_TestGamePath_ExplicitPath(t *testing.T) {
	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, nil)

	check := svc.TestGamePath("relative/MapleStory.exe")
	if check.OK {
		t.Fatalf("TestGamePath(relative) OK = true, want false")
	}
	if check.Problem != "not a local file" {
		t.Errorf("Problem = %q, want %q", check.Problem, "not a local file")
	}
}

// TestLauncherService_TestGamePath_TrimsWhitespace pins TestGamePath
// trimming surrounding whitespace off an explicit candidate path before
// checking it.
func TestLauncherService_TestGamePath_TrimsWhitespace(t *testing.T) {
	path := withLocalGameExe(t)
	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, nil)

	check := svc.TestGamePath("  " + path + "  ")
	if !check.OK || check.Path != path {
		t.Fatalf("TestGamePath(padded) = %+v, want OK at %q", check, path)
	}
}

// TestLauncherService_TestGamePath_UnquotesExplorerPath pins TestGamePath
// stripping the double quotes Explorer's "Copy as path" wraps around a path.
func TestLauncherService_TestGamePath_UnquotesExplorerPath(t *testing.T) {
	path := withLocalGameExe(t)
	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, nil)

	check := svc.TestGamePath(`"` + path + `"`)
	if !check.OK || check.Path != path {
		t.Fatalf("TestGamePath(quoted) = %+v, want OK at %q", check, path)
	}
}

func TestLauncherService_TestGamePath_EmptyUsesEffective(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, "", false)
	path := withLocalGameExe(t)

	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr, &fakeGamePathStore{path: path})

	check := svc.TestGamePath("")
	if !check.OK || check.Path != path {
		t.Fatalf("TestGamePath(\"\") = %+v, want OK at %q", check, path)
	}
}
