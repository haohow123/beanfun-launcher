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

// Launch fetches a one-time game-launch token for the given account
// and spawns the game executable with `/hb /u:{SID} /p:{OTP}`. The
// OTP byte slice is zeroed before this method returns regardless of
// outcome.
//
// Requires:
//   - An active session (login → CheckQRLogin completed).
//   - BEANFUN_GAME_EXE env var pointing at a valid game executable.
//
// On Windows the spawn uses CreateProcessW; on non-Windows it returns
// ErrPlatformUnsupported (the macOS dev box doesn't run game.exe).
func (s *LauncherService) Launch(account beanfun.Account) error {
	s.mu.Lock()
	client, session := s.login.Snapshot()
	s.mu.Unlock()

	if client == nil || session == nil {
		return beanfun.ErrLoginRequired()
	}

	gameExe, err := resolveGameExe()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	otp, err := client.FetchOTP(ctx, session, account)
	if err != nil {
		slog.Error("Launch: FetchOTP failed", "err", err, "sid", account.SID)
		return err
	}
	defer beanfun.Zero(otp.Token)

	args := []string{
		"/hb",
		"/u:" + account.SID,
		"/p:" + string(otp.Token),
	}

	if err := spawnFn(ctx, gameExe, args); err != nil {
		slog.Error("Launch: spawnFn failed", "err", err, "sid", account.SID)
		return err
	}
	slog.Info("Launch: game spawned", "sid", account.SID, "exe", gameExe)
	return nil
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
