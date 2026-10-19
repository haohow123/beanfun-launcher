package launcher

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

type windowImageFn = func(hwnd uintptr) (uint32, string, error)

// withWindowImage drives both window seams from fn, checking the path with verifyGameSignatureFn (set to accept any signature), and restores them via t.Cleanup.
func withWindowImage(t *testing.T, fn windowImageFn) {
	t.Helper()
	origPath, origVerdict := windowImagePathFn, windowImageVerdictFn
	windowImagePathFn = fn
	windowImageVerdictFn = func(hwnd uintptr) (uint32, string, signatureVerdict, error) {
		pid, path, err := fn(hwnd)
		if err != nil {
			return 0, "", signatureVerdict{}, err
		}
		return pid, path, verifyGameSignatureFn(path), nil
	}
	t.Cleanup(func() { windowImagePathFn, windowImageVerdictFn = origPath, origVerdict })
	withSignature(t, signatureOK)
}

// launchHarness wires Launch to a window with hwnd 1 and records inject calls and OTP endpoint hits.
type launchHarness struct {
	svc        *LauncherService
	otpHits    atomic.Int32
	injects    int
	injectHwnd uintptr
}

func newLaunchHarness(t *testing.T, fn windowImageFn) *launchHarness {
	t.Helper()
	withWindowImage(t, fn)
	h := &launchHarness{}
	origFind, origInject := findGameWindowFn, injectFn
	findGameWindowFn = func() uintptr { return 1 }
	injectFn = func(hwnd uintptr, _, _ []byte) error {
		h.injects++
		h.injectHwnd = hwnd
		return nil
	}
	t.Cleanup(func() { findGameWindowFn, injectFn = origFind, origInject })
	login := fakeLoginSnapshotter{client: countingOTPServer(t, &h.otpHits), session: &beanfun.Session{ServiceCode: "610074", ServiceRegion: "T9", WebToken: "z"}}
	h.svc = NewLauncherService(login, bgtask.New(), &fakeGamePathStore{})
	return h
}

var testAccount = beanfun.Account{SID: "T9abc123", SSN: "1", SName: "n"}

func requireUntrusted(t *testing.T, res LaunchResult, err error) {
	t.Helper()
	var le *LauncherError
	if !errors.As(err, &le) || le.Msg != ErrUntrustedGameWindow().Msg {
		t.Fatalf("err = %v, want ErrUntrustedGameWindow", err)
	}
	if res != (LaunchResult{}) {
		t.Errorf("result = %+v, want zero value (no OTP handed out)", res)
	}
}

func TestLauncherService_Launch_UntrustedSignature_NeverFetchesOTPOrInjects(t *testing.T) {
	h := newLaunchHarness(t, func(uintptr) (uint32, string, error) { return 7, "evil.exe", nil })
	withSignature(t, signatureVerdict{Reason: "unsigned"})

	res, err := h.svc.Launch("1", testAccount)
	requireUntrusted(t, res, err)
	if h.otpHits.Load() != 0 {
		t.Errorf("OTP server hit %d times, want 0", h.otpHits.Load())
	}
	if h.injects != 0 {
		t.Errorf("injectFn called %d times, want 0", h.injects)
	}
}

func requireUnreadable(t *testing.T, res LaunchResult, err error) {
	t.Helper()
	var le *LauncherError
	if !errors.As(err, &le) || le.Msg != ErrGameWindowUnreadable().Msg {
		t.Fatalf("err = %v, want ErrGameWindowUnreadable", err)
	}
	if res != (LaunchResult{}) {
		t.Errorf("result = %+v, want zero value (no OTP handed out)", res)
	}
}

func TestLauncherService_Launch_UnreadableWindowImage_Rejected(t *testing.T) {
	h := newLaunchHarness(t, func(uintptr) (uint32, string, error) { return 0, "", errors.New("access denied") })

	res, err := h.svc.Launch("1", testAccount)
	requireUnreadable(t, res, err)
	if h.otpHits.Load() != 0 || h.injects != 0 {
		t.Errorf("otp hits = %d, injects = %d, want 0, 0", h.otpHits.Load(), h.injects)
	}
}

func TestLauncherService_Launch_SecondImageReadFails_NoInjectNoOTP(t *testing.T) {
	calls := 0
	h := newLaunchHarness(t, func(uintptr) (uint32, string, error) {
		calls++
		if calls == 2 {
			return 0, "", errors.New("access denied")
		}
		return 7, "MapleStory.exe", nil
	})

	res, err := h.svc.Launch("1", testAccount)
	requireUnreadable(t, res, err)
	if h.otpHits.Load() == 0 {
		t.Error("OTP never fetched, so the second read was not what stopped the inject")
	}
	if h.injects != 0 {
		t.Errorf("injectFn called %d times, want 0", h.injects)
	}
}

func TestLauncherService_Launch_VerifiesTheWindowImagePath(t *testing.T) {
	const want = `C:\game\MapleStory.exe`
	cases := []struct {
		name      string
		imagePath string
		wantOK    bool
	}{
		{"window image path is the verified one", want, true},
		{"window image path is some other file", `C:\other\evil.exe`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newLaunchHarness(t, func(uintptr) (uint32, string, error) { return 7, tc.imagePath, nil })
			orig := verifyGameSignatureFn
			verifyGameSignatureFn = func(p string) signatureVerdict {
				if p == want {
					return signatureOK
				}
				return signatureVerdict{Reason: "wrong signer"}
			}
			t.Cleanup(func() { verifyGameSignatureFn = orig })

			res, err := h.svc.Launch("1", testAccount)
			if tc.wantOK {
				if err != nil || !res.AutoFilled || h.injects != 1 {
					t.Fatalf("Launch = %+v, %v, injects %d; want AutoFilled with 1 inject", res, err, h.injects)
				}
				return
			}
			requireUntrusted(t, res, err)
			if h.otpHits.Load() != 0 || h.injects != 0 {
				t.Errorf("otp hits = %d, injects = %d, want 0, 0", h.otpHits.Load(), h.injects)
			}
		})
	}
}

// A window whose running image is not the file at its image path must be refused before any OTP is fetched.
func TestLauncherService_Launch_ImageMismatch_NeverFetchesOTPOrInjects(t *testing.T) {
	h := newLaunchHarness(t, func(uintptr) (uint32, string, error) { return 7, "MapleStory.exe", nil })
	windowImageVerdictFn = func(uintptr) (uint32, string, signatureVerdict, error) {
		return 7, "MapleStory.exe", signatureVerdict{Reason: "image mismatch"}, nil
	}

	res, err := h.svc.Launch("1", testAccount)
	requireUntrusted(t, res, err)
	if h.otpHits.Load() != 0 || h.injects != 0 {
		t.Errorf("otp hits = %d, injects = %d, want 0, 0", h.otpHits.Load(), h.injects)
	}
}

func TestLauncherService_Launch_WindowPIDChangesDuringOTP_NoInjectNoOTP(t *testing.T) {
	calls := 0
	h := newLaunchHarness(t, func(uintptr) (uint32, string, error) {
		calls++
		return uint32(calls), "MapleStory.exe", nil
	})

	res, err := h.svc.Launch("1", testAccount)
	requireUntrusted(t, res, err)
	if h.otpHits.Load() == 0 {
		t.Error("OTP never fetched, so the PID re-check was not what stopped the inject")
	}
	if h.injects != 0 {
		t.Errorf("injectFn called %d times, want 0", h.injects)
	}
}

func TestLauncherService_Launch_TrustedWindow_Injects(t *testing.T) {
	h := newLaunchHarness(t, func(uintptr) (uint32, string, error) { return 7, "MapleStory.exe", nil })

	res, err := h.svc.Launch("1", testAccount)
	if err != nil || !res.AutoFilled {
		t.Fatalf("Launch = %+v, %v; want AutoFilled", res, err)
	}
	if h.injects != 1 || h.injectHwnd != 1 {
		t.Errorf("injectFn calls = %d hwnd = %d, want 1 call with hwnd 1", h.injects, h.injectHwnd)
	}
}

func TestLauncherService_LaunchAccount_UntrustedWindow_NoSpawnFallback(t *testing.T) {
	fake := withFakeSpawn(t)
	h := newLaunchHarness(t, func(uintptr) (uint32, string, error) { return 7, "evil.exe", nil })
	withSignature(t, signatureVerdict{Reason: "unsigned"})

	res, err := h.svc.LaunchAccount("1", testAccount)
	requireUntrusted(t, res, err)
	if fake.called {
		t.Error("spawnFn called = true, want false")
	}
}

func TestLauncherService_Launch_UsesTheNamedLoginsClient(t *testing.T) {
	var hits1, hits2 atomic.Int32
	c1 := countingOTPServer(t, &hits1)
	c2 := countingOTPServer(t, &hits2)
	withWindowImage(t, func(uintptr) (uint32, string, error) { return 7, "MapleStory.exe", nil })
	origFind, origInject := findGameWindowFn, injectFn
	findGameWindowFn = func() uintptr { return 1 }
	injectFn = func(uintptr, []byte, []byte) error { return nil }
	t.Cleanup(func() { findGameWindowFn, injectFn = origFind, origInject })

	login := fakeLoginSnapshotter{
		byID:    map[string]*beanfun.BeanfunClient{"1": c1, "2": c2},
		session: &beanfun.Session{ServiceCode: "610074", ServiceRegion: "T9", WebToken: "z"},
	}
	svc := NewLauncherService(login, bgtask.New(), &fakeGamePathStore{})

	res, err := svc.Launch("2", testAccount)
	if err != nil || !res.AutoFilled {
		t.Fatalf("Launch = %+v, %v, want AutoFilled", res, err)
	}
	if hits1.Load() != 0 || hits2.Load() == 0 {
		t.Errorf("hits login1 = %d, login2 = %d, want 0 and >0", hits1.Load(), hits2.Load())
	}
}
