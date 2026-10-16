package launcher

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

type windowImageFn = func(hwnd uintptr) (uint32, string, error)

// withWindowImage swaps windowImagePathFn and accepts any signature, restoring both via t.Cleanup.
func withWindowImage(t *testing.T, fn windowImageFn) {
	t.Helper()
	orig := windowImagePathFn
	windowImagePathFn = fn
	t.Cleanup(func() { windowImagePathFn = orig })
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

	res, err := h.svc.Launch(testAccount)
	requireUntrusted(t, res, err)
	if h.otpHits.Load() != 0 {
		t.Errorf("OTP server hit %d times, want 0", h.otpHits.Load())
	}
	if h.injects != 0 {
		t.Errorf("injectFn called %d times, want 0", h.injects)
	}
}

func TestLauncherService_Launch_UnreadableWindowImage_Rejected(t *testing.T) {
	h := newLaunchHarness(t, func(uintptr) (uint32, string, error) { return 0, "", errors.New("access denied") })

	res, err := h.svc.Launch(testAccount)
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

	res, err := h.svc.Launch(testAccount)
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

	res, err := h.svc.Launch(testAccount)
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

	res, err := h.svc.LaunchAccount(testAccount)
	requireUntrusted(t, res, err)
	if fake.called {
		t.Error("spawnFn called = true, want false")
	}
}
