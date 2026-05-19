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

// Beanfun launch protocol — positional argv passed to MapleStory.exe.
//
// Verified 2026-05-19 against the running official Beanfun client via
//
//	wmic process where name="MapleStory.exe" get commandline /value
//
// The game spawns with five positional arguments, NOT slash-prefixed
// flags. Earlier docs (and the pungin reference) described
// `/hb /u:<SID> /p:<OTP>` which Gamania has since replaced. The exact
// format is:
//
//	MapleStory.exe <host> <port> BeanFun <SID> <OTP>
//
// On a fresh spawn the game.exe contacts <host>:<port> directly with
// the OTP, validates server-side, and proceeds straight to character
// select — no login form, no WM_CHAR injection needed. This replaces
// the M8 PostMessage path for fresh spawns; M8's Launch method is
// retained as a fallback for users who already have the game open
// (where argv can't be retroactively supplied).
const (
	gameServerHost   = "tw.login.maplestory.beanfun.com"
	gameServerPort   = "8484"
	gameLaunchMarker = "BeanFun"
)

// gameStateChangedEvent is the Wails event name the frontend's
// useGameStateQuery hook subscribes to. The payload is a GameState
// struct serialized to JSON. Single event for both "started" and
// "exited" transitions — the Running field discriminates.
const gameStateChangedEvent = "game:state-changed"

// GameState mirrors the FE's gameStateAtom shape. JSON-tagged for
// Wails event marshalling + GetGameState's RPC return.
//
//   - Running=true with Hwnd>0 — watcher confirmed window present.
//   - Running=true with Hwnd=0 — SpawnGame's spawnFn returned but
//     window not yet detected (race window of ~0.5-4s before the
//     game's first paint). FE treats this the same as the
//     hwnd-known case.
//   - Running=false — watcher detected exit, or app start probe
//     found no game window.
type GameState struct {
	Running bool    `json:"running"`
	Hwnd    uintptr `json:"hwnd,omitempty"`
}

// errGameAlreadyRunning is returned by SpawnGame when a MapleStory
// window already exists. The argv path requires a fresh process —
// you can't retroactively pass credentials. The FE button should
// hide 啟動遊戲 when GameState.Running is true, switching to the
// Launch (帶入帳密) action instead; this error is the defensive
// backstop if the FE somehow calls SpawnGame anyway.
var errGameAlreadyRunning = errors.New("game already running; use Launch to inject into the existing window")

// LauncherService is the Wails-bound facade for "click an account →
// game launches with credentials filled". Two flows:
//
//   - SpawnGame(account): fresh launch via argv (host port BeanFun
//     SID OTP). Game auto-logs in to character select. Primary path.
//   - Launch(account): inject credentials into an already-running
//     game window via WM_CHAR. M8 path retained as a fallback for
//     users who started the game outside our launcher.
//
// State changes (started/exited) are reported asynchronously via the
// gameStateChangedEvent Wails event.
type LauncherService struct {
	mu    sync.Mutex
	login *beanfun.LoginService
	mgr   *bgtask.Manager
}

// NewLauncherService wires the service and, if a game window already
// exists at startup, attaches the exit watcher to it. This handles
// the launcher-restart-while-game-running case so closing the game
// later still produces a clean state-changed event. The watcher's
// emit is a no-op until application.New() runs (see
// watcher_emit_windows.go's init), so this early registration is
// safe: the watcher will block on WaitForSingleObject until the
// game exits, then emit once the Wails app is live.
func NewLauncherService(login *beanfun.LoginService, mgr *bgtask.Manager) *LauncherService {
	s := &LauncherService{login: login, mgr: mgr}
	if hwnd := findGameWindowFn(); hwnd != 0 {
		slog.Info("launcher: game already running at startup, attaching watcher",
			"hwnd", hwnd)
		s.restartWatcher(hwnd)
	}
	return s
}

// GetGameState is the initial-state RPC the FE's useGameStateQuery
// calls on mount. Subsequent updates arrive via the
// gameStateChangedEvent push event, so this is read once per FE
// lifecycle (per the queryClient cache). Probes findGameWindowFn
// synchronously; cheap.
func (s *LauncherService) GetGameState() GameState {
	hwnd := findGameWindowFn()
	return GameState{Running: hwnd != 0, Hwnd: hwnd}
}

// SpawnGame fetches an OTP for the given account and spawns
// MapleStory.exe with the canonical 5-arg positional argv that
// triggers Gamania's auto-login path. Game.exe takes the OTP from
// argv, validates server-side, and lands the user directly on
// character select — no login form rendered, no WM_CHAR injection,
// no form-ready timing window to navigate.
//
// Returns when the spawn syscall has completed (typically <100ms);
// the game's loading + server handshake + character-select render
// happen afterward without the launcher's involvement. State
// transitions (window-appeared, process-exited) reach the FE via
// the gameStateChangedEvent push event, fired by the goroutine
// kicked off via restartWatcher.
//
// Errors and their FE handling:
//
//   - ErrLoginRequired: session expired or absent. FE shows QR
//     re-login.
//   - errGameAlreadyRunning: defensive — FE should route to Launch
//     when GameState.Running is true. Surface error directly.
//   - resolveGameExe errors: registry value missing or override env
//     var unset. Surface error directly.
//   - spawn / FetchOTP errors: real failures. Surface error directly.
//
// The OTP byte slice is zeroed via defer before this method returns,
// matching the discipline of [[security-principles-beanfun]]. The
// OS-side argv copy in the spawned process's memory is unavoidable
// (kernel copies before we can scrub) but bounded by the OTP's
// single-use server-side ticket lifetime.
func (s *LauncherService) SpawnGame(account beanfun.Account) error {
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
		slog.Warn("SpawnGame: game already running, refusing to spawn",
			"hwnd", hwnd, "sid", account.SID)
		return errGameAlreadyRunning
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	otp, err := client.FetchOTP(ctx, session, account)
	if err != nil {
		if isSessionExpired(err) {
			slog.Warn("SpawnGame: session expired, clearing local state",
				"sid", account.SID)
			s.login.Reset()
			return beanfun.ErrLoginRequired()
		}
		slog.Error("SpawnGame: FetchOTP failed", "err", err, "sid", account.SID)
		return err
	}
	defer beanfun.Zero(otp.Token)

	args := []string{
		gameServerHost,
		gameServerPort,
		gameLaunchMarker,
		account.SID,
		string(otp.Token),
	}

	spawnCtx, spawnCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer spawnCancel()
	if err := spawnFn(spawnCtx, gameExe, args); err != nil {
		slog.Error("SpawnGame: spawnFn failed", "err", err, "sid", account.SID)
		return err
	}
	slog.Info("SpawnGame: game spawned with credentials via argv",
		"sid", account.SID, "exe", gameExe)

	// Optimistic running:true emit — spawnFn syscall returned, the
	// game process exists. Window may not be visible yet (~0.5-4s
	// race), but Running with Hwnd=0 is the documented "spawned but
	// not yet detected" state. The watcher we kick off next is
	// responsible for the running:false transition on process exit;
	// it stays silent on window-appearance since this event already
	// covered that.
	eventEmitFn(gameStateChangedEvent, GameState{Running: true})

	s.restartWatcher(0)
	return nil
}

// restartWatcher registers (or re-registers) the game-exit watcher
// against s.mgr. SpawnGame calls this with hwnd=0 so the watcher
// runs its phase-1 window-poll; NewLauncherService calls it with a
// pre-known hwnd to skip phase 1 (game was already running). The
// watcher emits gameStateChangedEvent with Running=false when the
// game process terminates (or never appears within the phase-1
// timeout). bgtask auto-supersedes the previous registration under
// the same name — no manual cancel field needed.
func (s *LauncherService) restartWatcher(hwnd uintptr) {
	s.mgr.Watcher(gameExitWatcherTaskName, func(ctx context.Context) {
		runGameWatcher(ctx, hwnd)
	})
}

// LaunchResult signals the outcome reported back to the frontend.
//
//   - AutoFilled=true → the game's login form received the
//     credentials. OTP empty (never exposed to frontend).
//
//   - NoWindow=true → no game window is open. With M10.1's argv
//     SpawnGame path this should be rare — FE only routes to Launch
//     when GameState.Running is true. Returned anyway for defensive
//     handling.
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

// Launch finds the running MapleStory window and injects the given
// account's credentials into its login form via WM_CHAR (M8 path).
// Used as a fallback when the user already has the game open from
// outside our launcher — the argv-based SpawnGame can't help retroactively
// since credentials must be passed at spawn time.
//
// Returns:
//   - LaunchResult{NoWindow: true} when no game window is open.
//     FE handles this by re-querying GameState (the state may have
//     just transitioned away).
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
// re-login. Used by SpawnGame + Launch + GetOTP to fold session-
// expired errors into the same "login required" shape the rest of
// the service uses.
func isSessionExpired(err error) bool {
	var le *beanfun.LoginError
	return errors.As(err, &le) && le.Kind == beanfun.KindSessionExpired
}

// GetOTP runs the OTP fetch flow and returns the plaintext token for
// display + clipboard copy on the frontend. The "show credentials so
// the user can paste into another launcher" path that runs alongside
// the direct-spawn SpawnGame method.
//
// Two callers:
//   - macOS dev verification: SpawnGame errors with platform-unsupported,
//     so dev uses GetOTP + paste into a Windows Beanfun client (or
//     just to confirm the wire format).
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
	otpStr := string(otp.Token)
	beanfun.Zero(otp.Token)
	return otpStr, nil
}
