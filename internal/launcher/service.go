package launcher

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
)

// gameExeEnvVar is the env var the user points at their game's
// executable. For Milestone 6 this is the only configuration path; a
// Settings UI (with persisted JSON in %APPDATA%) is a later milestone.
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

	gameExe := os.Getenv(gameExeEnvVar)
	if gameExe == "" {
		return ErrGameExeMissing()
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
