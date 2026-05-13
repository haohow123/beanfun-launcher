package launcher

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
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

	login := beanfun.NewLoginService()
	svc := NewLauncherService(login)

	err := svc.Launch(beanfun.Account{SID: "s", SSN: "1", SName: "n"})
	var le *beanfun.LoginError
	if !errors.As(err, &le) || le.Kind != beanfun.KindLoginRequired {
		t.Errorf("got %v, want beanfun.KindLoginRequired", err)
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

func TestLauncherService_Launch_GameExeMissing_NoSessionStillTakesPrecedence(t *testing.T) {
	// Documents the check order: no session takes precedence over
	// missing env var. A user who logs out and tries to launch
	// shouldn't see "set BEANFUN_GAME_EXE first" — they should see
	// "log in first".
	withFakeSpawn(t)
	withEnv(t, gameExeEnvVar, "")

	login := beanfun.NewLoginService()
	svc := NewLauncherService(login)

	err := svc.Launch(beanfun.Account{SID: "s", SSN: "1", SName: "n"})
	var le *beanfun.LoginError
	if !errors.As(err, &le) || le.Kind != beanfun.KindLoginRequired {
		t.Errorf("got %v, want beanfun.KindLoginRequired (check order: session before env var)", err)
	}
}
