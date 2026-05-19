package launcher

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

// gameExeEnvVar is the override env var for the game executable
// path. If set, it wins over the registry lookup — handy for dev,
// non-default installs, or when the registry value is stale.
const gameExeEnvVar = "BEANFUN_GAME_EXE"

// gameExitWatcherTaskName is the bgtask registry key for the
// game-exit watcher. Re-registering under the same name supersedes
// the previous goroutine — a new SpawnGame cancels the prior
// game's watcher cleanly without needing a manual cancel field.
const gameExitWatcherTaskName = "launcher-game-exit"

// LauncherService is the Wails-bound facade for "click an account →
// game window opens". Its single method, Launch, runs the post-login
// OTP flow and spawns the game process with the OTP on the command
// line. The OTP byte slice is zeroed immediately after spawn.
type LauncherService struct {
	mu    sync.Mutex
	login *beanfun.LoginService
	// mgr owns the game-exit watcher goroutine (registered under
	// gameExitWatcherTaskName). Constructor injection so tests can
	// plug a fresh manager; main.go shares a single instance across
	// all services so app shutdown can StopAll() them in one go.
	mgr *bgtask.Manager
}

func NewLauncherService(login *beanfun.LoginService, mgr *bgtask.Manager) *LauncherService {
	return &LauncherService{login: login, mgr: mgr}
}

// LaunchResult signals the outcome reported back to the frontend.
//
//   - AutoFilled=true → the game's login form received the
//     credentials. OTP empty (never exposed to frontend).
//
//   - NoWindow=true → no game window is open. Frontend should
//     prompt the user to press 啟動遊戲 first. OTP is not fetched
//     in this path (saves a wasted single-use token).
//
//   - AutoFilled=false (NoWindow=false) → window was found but
//     inject failed mid-sequence. OTP is populated so the frontend
//     can offer the user manual-paste.
//
// Hard errors (no session, OTP fetch failure) come back as a plain
// Go error and never produce a LaunchResult.
type LaunchResult struct {
	AutoFilled bool   `json:"autoFilled"`
	NoWindow   bool   `json:"noWindow,omitempty"`
	OTP        string `json:"otp,omitempty"`
}

// SpawnGame opens the configured MapleStory.exe but does not wait
// for the login form. Returns nil when the game has been spawned
// (or was already running).
//
// Split out from Launch so the frontend can drive an explicit
// two-step flow: user clicks 啟動遊戲 (this method) → waits visually
// for the login form → clicks 帶入帳密 per account (Launch). That
// design sidesteps the +20s "form input-ready" gap between window
// visibility and the textbox actually accepting WM_CHAR; once the
// user can see the form they're guaranteed it's ready, and inject
// lands within ~100ms.
//
// Requires:
//   - An active session (so we know there's a logged-in user
//     intending to play; resolveGameExe is independent of session).
//   - Either BEANFUN_GAME_EXE env var or the registry value to
//     locate the game executable.
func (s *LauncherService) SpawnGame() error {
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

	if hwnd := findGameWindowFn(); hwnd != 0 {
		slog.Info("SpawnGame: game already running, attaching watcher")
		s.restartWatcher(hwnd)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := spawnFn(ctx, gameExe, nil); err != nil {
		slog.Error("SpawnGame: spawnFn failed", "err", err)
		return err
	}
	slog.Info("SpawnGame: game spawned", "exe", gameExe)
	s.restartWatcher(0)
	return nil
}

// restartWatcher registers (or re-registers) the game-exit watcher
// against s.mgr. SpawnGame calls this in both paths: already-running
// passes the known hwnd so the watcher skips its window-appears
// phase; fresh-spawn passes 0 so the watcher polls for the
// cold-start window. The watcher emits "game:exited" when the game
// process terminates, so the frontend can reset its post-launch
// button state (issue #62). bgtask auto-supersedes the previous
// registration under the same name — no manual cancel field needed.
func (s *LauncherService) restartWatcher(hwnd uintptr) {
	s.mgr.Watcher(gameExitWatcherTaskName, func(ctx context.Context) {
		runGameWatcher(ctx, hwnd)
	})
}

// Launch finds the running MapleStory window and injects the given
// account's credentials into its login form. Does NOT spawn the
// game — see SpawnGame for that. Returns:
//
//   - LaunchResult{NoWindow: true} when no game window is open.
//     Frontend should prompt the user to press 啟動遊戲 first.
//   - LaunchResult{AutoFilled: true} on successful inject.
//   - LaunchResult{AutoFilled: false, OTP: ...} when the window
//     exists but inject failed mid-sequence — frontend shows the
//     OTP for manual paste.
//
// The OTP byte slice is zeroed before this method returns.
func (s *LauncherService) Launch(account beanfun.Account) (LaunchResult, error) {
	s.mu.Lock()
	client, session := s.login.Snapshot()
	s.mu.Unlock()

	if client == nil || session == nil {
		return LaunchResult{}, beanfun.ErrLoginRequired()
	}

	hwnd := findGameWindowFn()
	if hwnd == 0 {
		slog.Info("Launch: no game window found", "sid", account.SID)
		return LaunchResult{NoWindow: true}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	otp, err := client.FetchOTP(ctx, session, account)
	if err != nil {
		if isSessionExpired(err) {
			slog.Warn("Launch: session expired, clearing local state",
				"sid", account.SID)
			s.login.Reset()
			return LaunchResult{}, beanfun.ErrLoginRequired()
		}
		slog.Error("Launch: FetchOTP failed", "err", err, "sid", account.SID)
		return LaunchResult{}, err
	}
	defer beanfun.Zero(otp.Token)

	if ierr := injectFn(hwnd, []byte(account.SID), otp.Token); ierr != nil {
		slog.Error("Launch: inject failed, falling back to manual paste",
			"err", ierr, "sid", account.SID)
		return LaunchResult{AutoFilled: false, OTP: string(otp.Token)}, nil
	}
	slog.Info("Launch: credentials injected", "sid", account.SID)
	return LaunchResult{AutoFilled: true}, nil
}

// isSessionExpired reports whether err signals that the Beanfun
// server-side session has been invalidated and the user must
// re-login. Used by Launch + GetOTP to fold session-expired errors
// into the same "login required" shape the rest of the service
// uses.
func isSessionExpired(err error) bool {
	var le *beanfun.LoginError
	return errors.As(err, &le) && le.Kind == beanfun.KindSessionExpired
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
		if isSessionExpired(err) {
			slog.Warn("GetOTP: session expired, clearing local state",
				"sid", account.SID)
			s.login.Reset()
			return "", beanfun.ErrLoginRequired()
		}
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
