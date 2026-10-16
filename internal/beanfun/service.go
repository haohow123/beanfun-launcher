// Package beanfun is the Gamania Beanfun API client.
//
// The QR-login flow is documented in docs/beanfun-login-protocol.md.
package beanfun

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strconv"
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

// maxLogins caps how many Beanfun logins can be held at once.
const maxLogins = 5

// login is one signed-in Beanfun account with its own client, cookie jar and session.
type login struct {
	id, label, mainID string // mainID is never logged or sent to the frontend
	client            *BeanfunClient
	session           *Session
}

// LoginInfo is the frontend-safe view of a login.
type LoginInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// LoginService is the Wails-bound login facade. The frontend calls
// StartQRLogin once and then reads QRStatusNow; the poll itself runs in a backend heartbeat.
type LoginService struct {
	mu        sync.Mutex
	endpoints Endpoints
	logins    []*login // in login order
	nextID    int
	// pendingClient and pendingQR belong to the single in-flight QR attempt.
	pendingClient *BeanfunClient
	pendingQR     *qrLoginInit
	// mgr owns the keep-alive heartbeat goroutines (one per login, registered under
	// keepAliveTaskPrefix + id). Constructor injection so tests can plug a
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

	// OnSessionEnded, like OnQRStateChanged, must be installed before the first StartQRLogin.
	OnSessionEnded func(SessionEnded)

	// OnLoginsChanged receives the full login list after every change; install it before the first StartQRLogin.
	OnLoginsChanged func([]LoginInfo)
}

// Cadence of the keep-alive ping on echo_token.ashx: OK after success, Fail after an error, Blocked after an IP lock.
const (
	keepAliveIntervalOK   = 8 * time.Minute
	keepAliveIntervalFail = 10 * time.Second
	// keepAliveIntervalBlocked must exceed both qrMintCooldown and keepAliveIntervalOK.
	keepAliveIntervalBlocked = 10 * time.Minute
	// keepAliveTaskPrefix plus a login id is that login's bgtask registry key.
	keepAliveTaskPrefix = "beanfun-keepalive:"
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

// SessionEndedEvent must match the constant in frontend/src/queries/session.ts.
const SessionEndedEvent = "beanfun:session-ended"

// LoginsChangedEvent carries the []LoginInfo after every change; the frontend listener must use the same string.
const LoginsChangedEvent = "beanfun:logins-changed"

// SessionEnded is the payload of SessionEndedEvent.
type SessionEnded struct {
	Reason  string `json:"reason"`
	LoginID string `json:"loginId"`
	Label   string `json:"label"`
}

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
// BeanfunClient (clean cookie jar) is minted on every call; existing logins are untouched.
func (s *LoginService) StartQRLogin() (QRStart, error) {
	if remaining := s.mintCooldownRemaining(); remaining > 0 {
		slog.Warn("StartQRLogin: refused, ip-block cooldown active", "remaining", remaining)
		return QRStart{}, ErrIPBlocked()
	}

	if s.loginCount() >= maxLogins {
		return QRStart{}, ErrTooManyLogins()
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

	// Only the QR poll is superseded; existing logins and their keep-alives stay.
	s.mgr.Stop(qrPollTaskName)

	s.mu.Lock()
	s.pendingClient = client
	s.pendingQR = init
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

// GetAccounts returns the game accounts under the given login.
// See docs/beanfun-login-protocol.md § 8.
func (s *LoginService) GetAccounts(loginID string) ([]Account, error) {
	client, session := s.snapshot(loginID)

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

// ListLogins returns the current logins in login order.
func (s *LoginService) ListLogins() []LoginInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loginInfosLocked()
}

func (s *LoginService) loginInfosLocked() []LoginInfo {
	infos := make([]LoginInfo, 0, len(s.logins))
	for _, l := range s.logins {
		infos = append(infos, LoginInfo{ID: l.id, Label: l.label})
	}
	return infos
}

func (s *LoginService) loginCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.logins)
}

// hasLogin reports whether at least one login is held.
func (s *LoginService) hasLogin() bool { return s.loginCount() > 0 }

func (s *LoginService) findLoginLocked(id string) int {
	return slices.IndexFunc(s.logins, func(l *login) bool { return l.id == id })
}

// labelFor masks the middle of mainID, falling back to a numbered label when it is missing or short.
func labelFor(mainID string, n int) string {
	r := []rune(mainID)
	if len(r) >= 6 {
		return string(r[:2]) + "***" + string(r[len(r)-2:])
	}
	return "帳號 " + strconv.Itoa(n)
}

// snapshot returns the client + session pointers of one login without a
// copy. Used by sibling services (internal/launcher) that need to
// drive post-login flows against the same cookie jar — the alternative
// would be re-minting a client, which means re-logging-in.
//
// Both returned pointers are nil if loginID is unknown; callers
// must nil-check before use. The pointer-sharing is safe because:
//   - BeanfunClient.http (and its cookie jar) is itself thread-safe.
//   - Session is never mutated after install.
func (s *LoginService) snapshot(loginID string) (*BeanfunClient, *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.findLoginLocked(loginID); i >= 0 {
		return s.logins[i].client, s.logins[i].session
	}
	return nil, nil
}

// Reset logs out every login and abandons any QR attempt in progress.
func (s *LoginService) Reset() {
	s.mu.Lock()
	// Stop doesn't wait, so a QR tick already past it must find its generation stale and not install a session.
	s.qrGeneration++
	s.mgr.Stop(qrPollTaskName)
	for _, l := range s.logins {
		s.mgr.Stop(keepAliveTaskPrefix + l.id)
	}
	s.logins = nil
	s.pendingClient = nil
	s.pendingQR = nil
	s.qrState = QRState{Status: QRStatusExpired}
	s.mu.Unlock()
	s.notifyLoginsChanged([]LoginInfo{})
}

// Logout ends one login without a session-ended notice, since the user asked for it.
func (s *LoginService) Logout(loginID string) {
	s.mu.Lock()
	i := s.findLoginLocked(loginID)
	if i < 0 {
		s.mu.Unlock()
		return
	}
	s.mgr.Stop(keepAliveTaskPrefix + loginID)
	s.logins = slices.Delete(s.logins, i, i+1)
	infos := s.loginInfosLocked()
	s.mu.Unlock()
	s.notifyLoginsChanged(infos)
}

func (s *LoginService) notifyLoginsChanged(infos []LoginInfo) {
	if s.OnLoginsChanged != nil {
		s.OnLoginsChanged(infos)
	}
}

// endSessionIfCurrent removes login id only if it still belongs to client, so a stale caller can't log out a newer login.
func (s *LoginService) endSessionIfCurrent(id string, client *BeanfunClient) bool {
	if client == nil {
		return false
	}
	s.mu.Lock()
	i := s.findLoginLocked(id)
	if i < 0 || s.logins[i].client != client {
		s.mu.Unlock()
		return false
	}
	ended := s.logins[i]
	s.mgr.Stop(keepAliveTaskPrefix + id)
	s.logins = slices.Delete(s.logins, i, i+1)
	infos := s.loginInfosLocked()
	s.mu.Unlock()
	if s.OnSessionEnded != nil {
		s.OnSessionEnded(SessionEnded{Reason: "expired", LoginID: ended.id, Label: ended.label})
	}
	s.notifyLoginsChanged(infos)
	return true
}

// installLoginLocked stores an approved login, replacing the one with the same non-empty mainID and
// keeping its id and label. It reports false when a new login would exceed maxLogins.
func (s *LoginService) installLoginLocked(client *BeanfunClient, sess *Session, mainID string) bool {
	if mainID != "" {
		for i, old := range s.logins {
			if old.mainID != mainID {
				continue
			}
			s.mgr.Stop(keepAliveTaskPrefix + old.id)
			s.logins[i] = &login{id: old.id, label: old.label, mainID: mainID, client: client, session: sess}
			s.startKeepAliveLocked(old.id, client)
			return true
		}
	}
	if len(s.logins) >= maxLogins {
		return false
	}
	s.nextID++
	id := strconv.Itoa(s.nextID)
	s.logins = append(s.logins, &login{id: id, label: labelFor(mainID, s.nextID), mainID: mainID, client: client, session: sess})
	s.startKeepAliveLocked(id, client)
	return true
}

// startKeepAliveLocked registers the keep-alive heartbeat for one login
// against s.mgr. Re-registering under the same id supersedes the previous loop.
//
// Must be called with s.mu held (matches the call site in
// advanceQRState's pollOutcomeApproved branch, which holds s.mu to
// store the login atomically with starting the loop).
//
// Both success and failure log at INFO. Debug-only success
// logging would leave users (and us) staring at hour-long logs
// with no evidence the loop was alive; an alpha cycle is worth
// the ~1 line per interval of noise so a glance at launcher.log can confirm
// "keep-alive ticking" without instrumenting.
func (s *LoginService) startKeepAliveLocked(id string, client *BeanfunClient) {
	slog.Info("keep-alive: heartbeat registered", "login", id,
		"interval_ok", keepAliveIntervalOK, "interval_fail", keepAliveIntervalFail)
	s.mgr.Heartbeat(keepAliveTaskPrefix+id, keepAliveIntervalOK, func(ctx context.Context) time.Duration {
		return s.keepAliveTick(ctx, id, client)
	})
}

// keepAliveTick runs one ping and reports how long to wait before the
// next one.
func (s *LoginService) keepAliveTick(ctx context.Context, id string, client *BeanfunClient) time.Duration {
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := client.Ping(pingCtx)
	cancel()

	if err == nil {
		slog.Info("keep-alive: ping ok", "next_interval", keepAliveIntervalOK)
		return keepAliveIntervalOK
	}

	var le *LoginError
	if errors.As(err, &le) && le.Kind == KindSessionExpired {
		slog.Info("keep-alive: session logged out")
		s.endSessionIfCurrent(id, client)
		return 0
	}
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
			s.pendingClient = nil
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
		// A failed echo_token only costs the dedupe and the masked label, so it never blocks the login.
		_, mainID, echoErr := client.echoToken(ctx)
		if echoErr != nil {
			slog.Warn("qr-poll: echo_token after finalize failed", "err", echoErr)
		}
		s.mu.Lock()
		stale := gen != s.qrGeneration
		installed := false
		if !stale {
			installed = s.installLoginLocked(client, sess, mainID)
			if installed {
				s.pendingClient = nil
				s.pendingQR = nil
			}
		}
		infos := s.loginInfosLocked()
		s.mu.Unlock()
		if stale {
			slog.Warn("qr-poll: discarding a login completed by a superseded attempt", "generation", gen)
			return QRState{}, true
		}
		if !installed {
			return qrErrorState(prev, ErrTooManyLogins()), true
		}
		s.notifyLoginsChanged(infos)
		slog.Info("LoginService: login complete", "session", sess, "logins", len(infos))
		return QRState{Status: QRStatusApproved}, true
	default:
		return QRState{Status: prev.Status, Error: "unknown poll outcome"}, false
	}
}
