// Package beanfun is the Gamania Beanfun API client.
//
// The QR-login flow is documented in docs/beanfun-login-protocol.md.
package beanfun

import (
	"context"
	"log/slog"
	"sync"
	"time"
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

// LoginService is the Wails-bound login facade. The frontend calls
// StartQRLogin once, then polls CheckQRLogin every 2 seconds.
type LoginService struct {
	mu        sync.Mutex
	endpoints Endpoints
	client    *BeanfunClient // minted fresh on each StartQRLogin
	pendingQR *qrLoginInit
	session   *Session // populated when finalize succeeds
}

// NewLoginService returns a LoginService configured for production TW
// endpoints. The HTTP client is minted lazily inside StartQRLogin.
func NewLoginService() *LoginService {
	return &LoginService{endpoints: DefaultEndpoints()}
}

// NewLoginServiceWithEndpoints injects caller-provided endpoints. Tests
// build them from an httptest.Server and pass them in.
func NewLoginServiceWithEndpoints(endpoints Endpoints) *LoginService {
	return &LoginService{endpoints: endpoints}
}

// StartQRLogin runs the init flow (getSessionKey → initQRLogin) and
// returns the QR + deeplink for the frontend to render. A fresh
// BeanfunClient (clean cookie jar) is minted on every call.
func (s *LoginService) StartQRLogin() (QRStart, error) {
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
		return QRStart{}, err
	}
	init, err := client.initQRLogin(ctx, skey)
	if err != nil {
		slog.Error("StartQRLogin: initQRLogin failed", "err", err)
		return QRStart{}, err
	}

	s.mu.Lock()
	s.client = client
	s.pendingQR = init
	s.session = nil
	s.mu.Unlock()

	return QRStart{
		BitmapBase64: init.BitmapBase64,
		Deeplink:     init.Deeplink,
	}, nil
}

// CheckQRLogin polls /QRLogin/CheckLoginStatus once and, on Approved,
// runs the 4-call finalize handshake synchronously to acquire
// bfWebToken. The frontend never sees the finalize step explicitly —
// from its perspective a single CheckQRLogin call either stays Pending
// or completes the login.
func (s *LoginService) CheckQRLogin() (QRStatus, error) {
	s.mu.Lock()
	pendingQR := s.pendingQR
	client := s.client
	s.mu.Unlock()

	if pendingQR == nil || client == nil {
		// No active session — caller should StartQRLogin again. Surface
		// as Expired so the frontend's retry path is the natural next
		// step.
		return QRStatusExpired, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	outcome, err := client.pollQRLoginStatus(ctx, pendingQR)
	if err != nil {
		return "", err
	}

	switch outcome {
	case pollOutcomeWaitLogin:
		return QRStatusPending, nil
	case pollOutcomeFailed:
		return QRStatusRetry, nil
	case pollOutcomeTokenExpired:
		s.mu.Lock()
		s.pendingQR = nil
		s.mu.Unlock()
		return QRStatusExpired, nil
	case pollOutcomeApproved:
		sess, err := client.finalizeQRLogin(ctx, pendingQR)
		if err != nil {
			return "", err
		}
		s.mu.Lock()
		s.session = sess
		s.pendingQR = nil
		s.mu.Unlock()
		slog.Info("LoginService: login complete", "session", sess)
		return QRStatusApproved, nil
	default:
		return "", &LoginError{Kind: KindUnknown, Msg: "unknown poll outcome"}
	}
}
