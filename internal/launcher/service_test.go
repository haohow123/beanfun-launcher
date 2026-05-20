package launcher

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

// fakeSpawn records the path and args last passed to spawnFn so tests
// can assert the spawn shape without invoking CreateProcessW.
type fakeSpawn struct {
	called   bool
	lastPath string
	lastArgs []string
	err      error
}

func (f *fakeSpawn) spawn(_ context.Context, path string, args []string) error {
	f.called = true
	f.lastPath = path
	f.lastArgs = append(f.lastArgs[:0], args...)
	return f.err
}

// withFakeSpawn swaps the package-level spawnFn for the test, with
// automatic restore via t.Cleanup.
func withFakeSpawn(t *testing.T) *fakeSpawn {
	t.Helper()
	orig := spawnFn
	fake := &fakeSpawn{}
	spawnFn = fake.spawn
	t.Cleanup(func() { spawnFn = orig })
	return fake
}

// withEnv sets an env var and restores it on test cleanup.
func withEnv(t *testing.T, key, value string) {
	t.Helper()
	orig, hadOrig := os.LookupEnv(key)
	if value == "" {
		_ = os.Unsetenv(key)
	} else {
		_ = os.Setenv(key, value)
	}
	t.Cleanup(func() {
		if hadOrig {
			_ = os.Setenv(key, orig)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func TestLauncherService_Launch_NoSession(t *testing.T) {
	withFakeSpawn(t)
	withEnv(t, gameExeEnvVar, "C:\\Game.exe")

	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr)

	_, err := svc.Launch(beanfun.Account{SID: "s", SSN: "1", SName: "n"})
	var le *beanfun.LoginError
	if !errors.As(err, &le) || le.Kind != beanfun.KindLoginRequired {
		t.Errorf("got %v, want beanfun.KindLoginRequired", err)
	}
}

func TestLauncherService_Launch_GameExeMissing_NoSessionStillTakesPrecedence(t *testing.T) {
	// Documents the check order: no session takes precedence over
	// missing env var. A user who logs out and tries to launch
	// shouldn't see "set BEANFUN_GAME_EXE first" — they should see
	// "log in first".
	withFakeSpawn(t)
	withEnv(t, gameExeEnvVar, "")

	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr)

	_, err := svc.Launch(beanfun.Account{SID: "s", SSN: "1", SName: "n"})
	var le *beanfun.LoginError
	if !errors.As(err, &le) || le.Kind != beanfun.KindLoginRequired {
		t.Errorf("got %v, want beanfun.KindLoginRequired (check order: session before env var)", err)
	}
}

func TestLauncherService_SpawnGame_NoSession(t *testing.T) {
	// SpawnGame is the argv-based M10.1 path. Session check fires
	// before any other validation just like Launch.
	withFakeSpawn(t)
	withEnv(t, gameExeEnvVar, "C:\\Game.exe")

	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr)

	err := svc.SpawnGame(beanfun.Account{SID: "s", SSN: "1", SName: "n"})
	var le *beanfun.LoginError
	if !errors.As(err, &le) || le.Kind != beanfun.KindLoginRequired {
		t.Errorf("got %v, want beanfun.KindLoginRequired", err)
	}
}

func TestLauncherService_SpawnGame_GameExeMissing_NoSessionStillTakesPrecedence(t *testing.T) {
	// Same check-order invariant as Launch: session error wins over
	// env-var error so the user sees "log in" not "set env var".
	withFakeSpawn(t)
	withEnv(t, gameExeEnvVar, "")

	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr)

	err := svc.SpawnGame(beanfun.Account{SID: "s", SSN: "1", SName: "n"})
	var le *beanfun.LoginError
	if !errors.As(err, &le) || le.Kind != beanfun.KindLoginRequired {
		t.Errorf("got %v, want beanfun.KindLoginRequired (check order: session before env var)", err)
	}
}

func TestLauncherService_LaunchAccount_NoSession(t *testing.T) {
	// LaunchAccount is the SpawnGame+Launch façade. On non-Windows
	// findGameWindowFn returns 0, so we route into SpawnGame's path —
	// session check must fire before any other validation, same as
	// the underlying SpawnGame/Launch tests above.
	withFakeSpawn(t)
	withEnv(t, gameExeEnvVar, "C:\\Game.exe")

	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr)

	_, err := svc.LaunchAccount(beanfun.Account{SID: "s", SSN: "1", SName: "n"})
	var le *beanfun.LoginError
	if !errors.As(err, &le) || le.Kind != beanfun.KindLoginRequired {
		t.Errorf("got %v, want beanfun.KindLoginRequired", err)
	}
}

func TestLauncherService_GetGameState_NoWindow(t *testing.T) {
	// findGameWindowFn returns 0 on macOS / Linux (the test platform)
	// because inject_other.go binds it to a stub. GetGameState should
	// reflect that as Running=false.
	mgr := bgtask.New()
	login := beanfun.NewLoginService(mgr)
	svc := NewLauncherService(login, mgr)

	state := svc.GetGameState()
	if state.Running {
		t.Errorf("GetGameState.Running = true on non-Windows, want false")
	}
	if state.Hwnd != 0 {
		t.Errorf("GetGameState.Hwnd = %d, want 0", state.Hwnd)
	}
}

// Happy-path coverage for the spawn arg construction lives in the
// beanfun package's TestBeanfunClient_FetchOTP_HappyPath — that
// exercises the OTP wire format end-to-end against an httptest
// server. The LauncherService glue is intentionally thin (snapshot
// session, fetch OTP, build args, spawn, zero) so per-piece tests
// cover it: beanfun/otp_test.go for FetchOTP, this file for
// no-session + env-var checks, and the real-Beanfun smoke (`task
// dev` → click account → game window) for the integration.
