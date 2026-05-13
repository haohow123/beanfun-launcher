package launcher

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
)

// gameExeEnvVar is the override env var for the game executable
// path. If set, it wins over the registry lookup — handy for dev,
// non-default installs, or when the registry value is stale.
const gameExeEnvVar = "BEANFUN_GAME_EXE"

// LauncherService is the Wails-bound facade for "click an account →
// game window opens". Its single method, Launch, runs the post-login
// OTP flow and spawns the game process with the OTP on the command
// line. The OTP byte slice is zeroed immediately after spawn.
type LauncherService struct {
	mu    sync.Mutex
	login *beanfun.LoginService
}

func NewLauncherService(login *beanfun.LoginService) *LauncherService {
	return &LauncherService{login: login}
}

// LaunchResult is the outcome reported back to the frontend after a
// successful spawn. Two states:
//
//   - AutoFilled=true → window injection succeeded; the game's
//     login form has been typed into and submitted. OTP stays "".
//   - AutoFilled=false → spawn worked but the window-injection
//     step failed (window class not found, PostMessage rejected,
//     etc.). OTP is populated so the frontend can copy SID+OTP to
//     the clipboard for manual paste.
//
// A hard error (no session, missing game exe, spawn failure, OTP
// fetch failure) is returned as a regular Go error and never
// produces a LaunchResult.
type LaunchResult struct {
	AutoFilled bool   `json:"autoFilled"`
	OTP        string `json:"otp,omitempty"`
}

// Launch fetches a one-time game-launch token for the given account,
// spawns the game executable, then injects the credentials into its
// login form via PostMessage. The OTP byte slice is zeroed before
// this method returns regardless of outcome.
//
// Requires:
//   - An active session (login → CheckQRLogin completed).
//   - Either BEANFUN_GAME_EXE env var or the
//     HKLM\SOFTWARE\Gamania\MAPLESTORY\Path registry value to locate
//     the game.
//
// On Windows the spawn uses ShellExecuteW (manifest-aware UAC) and
// injection uses FindWindowW + PostMessageW. On non-Windows the
// spawn stub returns ErrPlatformUnsupported.
func (s *LauncherService) Launch(account beanfun.Account) (LaunchResult, error) {
	s.mu.Lock()
	client, session := s.login.Snapshot()
	s.mu.Unlock()

	if client == nil || session == nil {
		return LaunchResult{}, beanfun.ErrLoginRequired()
	}

	gameExe, err := resolveGameExe()
	if err != nil {
		return LaunchResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	otp, err := client.FetchOTP(ctx, session, account)
	if err != nil {
		slog.Error("Launch: FetchOTP failed", "err", err, "sid", account.SID)
		return LaunchResult{}, err
	}
	defer beanfun.Zero(otp.Token)

	// Spawn with no command-line credentials. The current TW build
	// ignores `/hb /u:.. /p:..` args (verified during alpha.6);
	// injection via PostMessage replaces that path.
	if err := spawnFn(ctx, gameExe, nil); err != nil {
		slog.Error("Launch: spawnFn failed", "err", err, "sid", account.SID)
		return LaunchResult{}, err
	}
	slog.Info("Launch: game spawned", "sid", account.SID, "exe", gameExe)

	hwnd, werr := waitForGameWindowFn(ctx, gameWindowWaitTimeout)
	if werr != nil {
		slog.Warn("Launch: window not found within timeout, falling back to manual paste",
			"err", werr, "sid", account.SID)
		return LaunchResult{AutoFilled: false, OTP: string(otp.Token)}, nil
	}

	if ierr := injectFn(hwnd, []byte(account.SID), otp.Token); ierr != nil {
		slog.Error("Launch: inject failed, falling back to manual paste",
			"err", ierr, "sid", account.SID)
		return LaunchResult{AutoFilled: false, OTP: string(otp.Token)}, nil
	}
	slog.Info("Launch: credentials injected", "sid", account.SID)
	return LaunchResult{AutoFilled: true}, nil
}

// GetOTP runs the OTP fetch flow and returns the plaintext token for
// display + clipboard copy on the frontend. The "show credentials so
// the user can paste into another launcher" path that runs alongside
// the direct-spawn Launch method.
//
// Two callers:
//   - macOS dev verification: the spawn path returns
//     ErrPlatformUnsupported, so the user uses GetOTP + paste into a
//     Windows Beanfun client (or just to confirm the wire format).
//   - Windows users who prefer to launch the game through their own
//     tooling rather than letting us spawn it.
//
// The returned string lives in the frontend's JS heap (we can't zero
// it from Go). Per Beanfun's design the OTP is single-use and
// rotates on each call — keeping it in memory between fetch and
// paste is acceptable given that single-use lifecycle.
func (s *LauncherService) GetOTP(account beanfun.Account) (string, error) {
	s.mu.Lock()
	client, session := s.login.Snapshot()
	s.mu.Unlock()

	if client == nil || session == nil {
		return "", beanfun.ErrLoginRequired()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	otp, err := client.FetchOTP(ctx, session, account)
	if err != nil {
		slog.Error("GetOTP: FetchOTP failed", "err", err, "sid", account.SID)
		return "", err
	}
	// Copy bytes into a string, then zero the source. The string copy
	// lives in Go GC heap until reclaimed; the JS heap copy lives
	// until the frontend clears its ref. Both are acceptable given
	// the OTP's single-use lifecycle.
	otpStr := string(otp.Token)
	beanfun.Zero(otp.Token)
	return otpStr, nil
}
