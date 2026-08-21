// Package beanfun is the Gamania Beanfun API client.
//
// The QR-login flow is documented in docs/beanfun-login-protocol.md.
package beanfun

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

// QRStart is the payload returned to the frontend when QR login begins.
// BitmapBase64 is the QR PNG bytes encoded as base64 (no data: prefix).
// Deeplink is the Beanfun mobile-app URL the QR encodes; surfaced so
// the frontend can offer a "copy / open on phone" action.
type QRStart struct {
	BitmapBase64 string `json:"bitmapBase64"`
	Deeplink     string `json:"deeplink"`
}

// QRStatus mirrors the four outcomes Beanfun returns from
// CheckLoginStatus, mapped to lower-case string tags so the TS side
// gets a clean discriminated union.
type QRStatus string

const (
	QRStatusPending  QRStatus = "pending"
	QRStatusRetry    QRStatus = "retry"
	QRStatusExpired  QRStatus = "expired"
	QRStatusApproved QRStatus = "approved"
)

// QRState is the cached QR-login state. Error carries the rendered, credential-scrubbed message
// when a poll or finalize failed, so the frontend keeps the needle it matches on today.
type QRState struct {
	Status QRStatus `json:"status"`
	Error  string   `json:"error,omitempty"`
}

// LoginService is the Wails-bound login facade. The frontend calls
// StartQRLogin once and then reads QRStatusNow; the poll itself runs in a backend heartbeat.
type LoginService struct {
	mu        sync.Mutex
	endpoints Endpoints
	client    *BeanfunClient // minted fresh on each StartQRLogin
	pendingQR *qrLoginInit
	session   *Session // populated when finalize succeeds
	// mgr owns the keep-alive heartbeat goroutine (registered under
	// keepAliveTaskName). Constructor injection so tests can plug a
	// fresh manager; main.go shares a single instance across all
	// services so app shutdown can StopAll() them in one go.
	mgr *bgtask.Manager
	// blockedUntil is when the IP-lock cooldown expires; the zero value
	// means no cooldown is active.
	blockedUntil time.Time
	// qrState is the latest state the background poll produced; guarded by mu.
	qrState QRState
	// qrGeneration increments on every StartQRLogin. bgtask.Stop cancels without waiting, so a
	// superseded tick can still be mid-flight; it compares generations before writing.
	qrGeneration uint64

	// OnQRStateChanged, if non-nil, receives every new QRState. Install once before the first
	// StartQRLogin — it is read by the poll goroutine and assumed immutable from then on.
	OnQRStateChanged func(QRState)
}

// Cadence of the background ping that keeps the Beanfun portal
// from reaping the session as idle. WPF (and pungin/Beanfun's Rust
// port) drive `echo_token.ashx` every 60 seconds.
//
// Adaptive on failure: a successful ping arms the next tick at
// keepAliveIntervalOK (60s); a failed ping arms at
// keepAliveIntervalFail (10s) so a transient blip doesn't burn
// most of the 55-minute server-side reap window before we try
// again. Once the next ping succeeds we drop back to 60s. An
// IP-blocked ping backs off to keepAliveIntervalBlocked instead:
// retrying sooner cannot refresh anything and only re-arms our own
// cooldown, but giving up entirely would lose the session even
// though beanfun clears the block in minutes.
const (
	keepAliveIntervalOK   = 60 * time.Second
	keepAliveIntervalFail = 10 * time.Second
	// keepAliveIntervalBlocked must stay longer than qrMintCooldown, so the cooldown lapses between
	// two blocked pings instead of being pushed out forever.
	keepAliveIntervalBlocked = 5 * time.Minute
	// keepAliveTaskName is the bgtask registry key for the keep-alive
	// heartbeat. Re-registering under the same name supersedes the
	// previous loop (used when a fresh QR login replaces an active
	// session); Reset() calls mgr.Stop(keepAliveTaskName) to halt.
	keepAliveTaskName = "beanfun-keepalive"
)

// qrMintCooldown is how long StartQRLogin refuses after beanfun reports
// an IP lock; the lock itself clears in 2-3 minutes, and tests override
// this to keep runs in milliseconds.
var qrMintCooldown = 60 * time.Second

const (
	qrPollInterval = 2 * time.Second
	qrPollTimeout  = 30 * time.Second
	// qrPollTaskName is the bgtask registry key; re-registering supersedes the previous loop, and
	// Reset() stops it.
	qrPollTaskName = "beanfun-qr-poll"
)

// QRStateChangedEvent is the Wails event carrying each new QRState; the frontend bridge in
// frontend/src/queries/qrLogin.ts must use the same string.
const QRStateChangedEvent = "beanfun:qr-state-changed"

// NewLoginService returns a LoginService configured for production TW
// endpoints. The HTTP client is minted lazily inside StartQRLogin.
// mgr is required (non-nil) — the keep-alive heartbeat registers
// against it after a successful login.
func NewLoginService(mgr *bgtask.Manager) *LoginService {
	return &LoginService{endpoints: DefaultEndpoints(), mgr: mgr,
		qrState: QRState{Status: QRStatusExpired}}
}

// NewLoginServiceWithEndpoints injects caller-provided endpoints. Tests
// build them from an httptest.Server and pass them in. mgr is required
// (non-nil) — tests typically pass a fresh `bgtask.New()`.
func NewLoginServiceWithEndpoints(endpoints Endpoints, mgr *bgtask.Manager) *LoginService {
	return &LoginService{endpoints: endpoints, mgr: mgr,
		qrState: QRState{Status: QRStatusExpired}}
}

// armCooldownIfBlocked starts the mint cooldown when err is beanfun's
// IP-lock notice.
func (s *LoginService) armCooldownIfBlocked(err error) {
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindIPBlocked {
		return
	}
	s.mu.Lock()
	// Extend only, so a caller with a shorter cooldown can never shorten a longer one.
	if until := time.Now().Add(qrMintCooldown); until.After(s.blockedUntil) {
		s.blockedUntil = until
	}
	s.mu.Unlock()
	slog.Warn("ip-block cooldown armed", "duration", qrMintCooldown)
}

// mintCooldownRemaining returns how much of the cooldown is left, or
// zero when none is active.
func (s *LoginService) mintCooldownRemaining() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if remaining := time.Until(s.blockedUntil); remaining > 0 {
		return remaining
	}
	return 0
}

func (s *LoginService) cachedQRState() QRState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.qrState
}

func (s *LoginService) publishQRState(gen uint64, st QRState) {
	s.mu.Lock()
	changed := gen == s.qrGeneration && s.qrState != st
	if changed {
		s.qrState = st
	}
	s.mu.Unlock()
	// Suppressing repeats matters: a QR sitting unscanned returns Wait Login every 2s, and emitting
	// each one would be a push-shaped poll.
	if changed && s.OnQRStateChanged != nil {
		s.OnQRStateChanged(st)
	}
}

// qrErrorState keeps the last known status and attaches the failure, matching the old behaviour
// where a failed poll left the frontend's data untouched and surfaced the error separately.
func qrErrorState(prev QRState, err error) QRState {
	return QRState{Status: prev.Status, Error: ScrubCredentials(err.Error())}
}

func isIPBlockedErr(err error) bool {
	var le *LoginError
	return errors.As(err, &le) && le.Kind == KindIPBlocked
}

// StartQRLogin runs the init flow (getSessionKey → initQRLogin) and
// returns the QR + deeplink for the frontend to render. A fresh
// BeanfunClient (clean cookie jar) is minted on every call.
func (s *LoginService) StartQRLogin() (QRStart, error) {
	if remaining := s.mintCooldownRemaining(); remaining > 0 {
		slog.Warn("StartQRLogin: refused, ip-block cooldown active", "remaining", remaining)
		return QRStart{}, ErrIPBlocked()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := NewBeanfunClientWithEndpoints(s.endpoints)
	if err != nil {
		slog.Error("StartQRLogin: new client failed", "err", err)
		return QRStart{}, err
	}

	skey, err := client.getSessionKey(ctx)
	if err != nil {
		slog.Error("StartQRLogin: getSessionKey failed", "err", err)
		s.armCooldownIfBlocked(err)
		return QRStart{}, err
	}
	init, err := client.initQRLogin(ctx, skey)
	if err != nil {
		slog.Error("StartQRLogin: initQRLogin failed", "err", err)
		s.armCooldownIfBlocked(err)
		return QRStart{}, err
	}

	// Cancel any keep-alive loop still running from a previous
	// session — re-issuing StartQRLogin after a successful login
	// (e.g. user clicked 登出 and is logging back in) would
	// otherwise leak the old loop. Done outside the s.mu critical
	// section so we don't hold a service lock while waiting on
	// the bgtask registry's internal mutex.
	s.mgr.Stop(keepAliveTaskName)
	s.mgr.Stop(qrPollTaskName)

	s.mu.Lock()
	s.client = client
	s.pendingQR = init
	s.session = nil
	s.startQRPollLocked(client, init)
	s.mu.Unlock()

	// Boundary log — absence of this line in launcher.log when
	// "step 2" did appear means we returned successfully but the
	// frontend never saw the result, i.e. Wails IPC swallowed it.
	slog.Info("StartQRLogin: returning to frontend",
		"bitmap_b64_len", len(init.BitmapBase64),
		"has_deeplink", init.Deeplink != "")
	return QRStart{
		BitmapBase64: init.BitmapBase64,
		Deeplink:     init.Deeplink,
	}, nil
}

// QRStatusNow returns the cached QR-login state. The poll that produces it runs in the heartbeat
// StartQRLogin registers; this call touches no network.
func (s *LoginService) QRStatusNow() QRState {
	return s.cachedQRState()
}

// GetAccounts returns the list of game accounts under the active
// session. Requires StartQRLogin and an approved poll to have completed
// successfully (session != nil). See docs/beanfun-login-protocol.md § 8.
func (s *LoginService) GetAccounts() ([]Account, error) {
	s.mu.Lock()
	client := s.client
	session := s.session
	s.mu.Unlock()

	if client == nil || session == nil {
		return nil, ErrLoginRequired()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	accounts, err := client.GetAccounts(ctx, session)
	if err != nil {
		slog.Error("GetAccounts failed", "err", err)
		return nil, err
	}
	return accounts, nil
}

// Snapshot returns the current client + session pointers without a
// copy. Used by sibling services (internal/launcher) that need to
// drive post-login flows against the same cookie jar — the alternative
// would be re-minting a client, which means re-logging-in.
//
// Both returned pointers may be nil if no session is active; callers
// must nil-check before use. The pointer-sharing is safe because:
//   - BeanfunClient.http (and its cookie jar) is itself thread-safe.
//   - Session is only mutated by LoginService under s.mu; readers
//     observe a coherent snapshot of whichever Session was active at
//     call time.
func (s *LoginService) Snapshot() (*BeanfunClient, *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client, s.session
}

// Reset stops the keep-alive heartbeat and drops the cached client +
// session. Used when an authenticated request comes back as
// ErrSessionExpired so the next Snapshot returns nil and the
// launcher service rejects further calls with ErrLoginRequired.
func (s *LoginService) Reset() {
	// Stop outside the s.mu critical section — see StartQRLogin
	// comment for the same rationale.
	s.mgr.Stop(keepAliveTaskName)
	s.mgr.Stop(qrPollTaskName)

	s.mu.Lock()
	s.client = nil
	s.session = nil
	s.pendingQR = nil
	s.qrState = QRState{Status: QRStatusExpired}
	s.mu.Unlock()
}

// startKeepAliveLocked registers the keep-alive heartbeat against
// s.mgr. Re-registering (e.g. user logs out + logs in again under
// the same LoginService instance) supersedes the previous loop —
// bgtask cancels the prior goroutine on same-name re-registration.
//
// Must be called with s.mu held (matches the call site in
// advanceQRState's pollOutcomeApproved branch, which holds s.mu to
// store the session atomically with starting the loop).
//
// Adaptive cadence: 60 s after each successful ping, 10 s after
// each failure, 5 min while beanfun reports an IP block. The
// shorter retry gives a transient network blip a chance to clear
// before the server's ~55 min idle reaper kicks in, without
// spamming the endpoint when things are fine; the long one waits
// out a block without abandoning the session.
//
// Both success and failure log at INFO. Pungin-style debug-only
// success logging left users (and us) staring at hour-long logs
// with no evidence the loop was alive; an alpha cycle is worth
// the ~1 line/min of noise so a glance at launcher.log can confirm
// "keep-alive ticking" without instrumenting.
func (s *LoginService) startKeepAliveLocked(client *BeanfunClient) {
	slog.Info("keep-alive: heartbeat registered",
		"interval_ok", keepAliveIntervalOK, "interval_fail", keepAliveIntervalFail)
	s.mgr.Heartbeat(keepAliveTaskName, keepAliveIntervalOK, func(ctx context.Context) time.Duration {
		return s.keepAliveTick(ctx, client)
	})
}

// keepAliveTick runs one ping and reports how long to wait before the
// next one.
func (s *LoginService) keepAliveTick(ctx context.Context, client *BeanfunClient) time.Duration {
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := client.Ping(pingCtx)
	cancel()

	if err == nil {
		slog.Info("keep-alive: ping ok", "next_interval", keepAliveIntervalOK)
		return keepAliveIntervalOK
	}

	var le *LoginError
	if errors.As(err, &le) && le.Kind == KindIPBlocked {
		// Backing off rather than giving up: the block clears on its own, and the session is worth
		// keeping if it does.
		slog.Warn("keep-alive: ip blocked, backing off",
			"err", err, "next_interval", keepAliveIntervalBlocked)
		s.armCooldownIfBlocked(err)
		return keepAliveIntervalBlocked
	}

	slog.Warn("keep-alive: ping failed (retrying sooner)",
		"err", err, "next_interval", keepAliveIntervalFail)
	return keepAliveIntervalFail
}

// startQRPollLocked registers the QR poll heartbeat. Must be called with s.mu held, from the same
// critical section that installs pendingQR — that is what makes it impossible for the loop to poll
// a stale session.
func (s *LoginService) startQRPollLocked(client *BeanfunClient, pendingQR *qrLoginInit) {
	s.qrGeneration++
	gen := s.qrGeneration
	s.qrState = QRState{Status: QRStatusPending}
	slog.Info("qr-poll: heartbeat registered", "interval", qrPollInterval, "generation", gen)
	s.mgr.Heartbeat(qrPollTaskName, 0, func(ctx context.Context) time.Duration {
		return s.qrPollTick(ctx, gen, client, pendingQR)
	})
}

// qrPollTick runs one poll and reports how long to wait before the next, returning 0 to end the
// loop. Terminal conditions are approved, expired and IP-blocked — continuing at 2s into a blocked
// endpoint is what the cooldown was introduced to prevent.
func (s *LoginService) qrPollTick(ctx context.Context, gen uint64, client *BeanfunClient, pendingQR *qrLoginInit) time.Duration {
	pollCtx, cancel := context.WithTimeout(ctx, qrPollTimeout)
	defer cancel()

	st, stop := s.advanceQRState(pollCtx, gen, client, pendingQR)
	s.publishQRState(gen, st)
	if stop {
		slog.Info("qr-poll: loop ending", "status", st.Status, "has_error", st.Error != "")
		return 0
	}
	return qrPollInterval
}

func (s *LoginService) advanceQRState(ctx context.Context, gen uint64, client *BeanfunClient, pendingQR *qrLoginInit) (QRState, bool) {
	prev := s.cachedQRState()

	outcome, err := client.pollQRLoginStatus(ctx, pendingQR)
	if err != nil {
		slog.Warn("qr-poll: poll failed", "err", err)
		s.armCooldownIfBlocked(err)
		return qrErrorState(prev, err), isIPBlockedErr(err)
	}

	switch outcome {
	case pollOutcomeWaitLogin:
		return QRState{Status: QRStatusPending}, false
	case pollOutcomeFailed:
		return QRState{Status: QRStatusRetry}, false
	case pollOutcomeTokenExpired:
		s.mu.Lock()
		if gen == s.qrGeneration {
			s.pendingQR = nil
		}
		s.mu.Unlock()
		return QRState{Status: QRStatusExpired}, true
	case pollOutcomeApproved:
		sess, err := client.finalizeQRLogin(ctx, pendingQR)
		if err != nil {
			slog.Error("qr-poll: finalize failed", "err", err)
			s.armCooldownIfBlocked(err)
			return qrErrorState(prev, err), isIPBlockedErr(err)
		}
		s.mu.Lock()
		stale := gen != s.qrGeneration
		if !stale {
			s.session = sess
			s.pendingQR = nil
			s.startKeepAliveLocked(client)
		}
		s.mu.Unlock()
		if stale {
			slog.Warn("qr-poll: discarding a login completed by a superseded attempt", "generation", gen)
			return QRState{}, true
		}
		slog.Info("LoginService: login complete", "session", sess)
		return QRState{Status: QRStatusApproved}, true
	default:
		return QRState{Status: prev.Status, Error: "unknown poll outcome"}, false
	}
}
