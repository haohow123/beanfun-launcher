// Package beanfun is the Gamania Beanfun API client.
//
// Day 3: real init flow lands (getSessionKey + Login/Index + InitLogin).
// CheckQRLogin stays mocked (3 polls → Approved) until Day 4 wires up
// the real poll + finalize handshake.
package beanfun

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// QRStart is the payload returned to the frontend when QR login begins.
// BitmapBase64 is the QR PNG bytes encoded as base64 (no data: prefix).
// Deeplink is the Beanfun mobile-app URL the QR encodes; surfaced so the
// frontend can offer a "copy / open on phone" action.
type QRStart struct {
	BitmapBase64 string `json:"bitmapBase64"`
	Deeplink     string `json:"deeplink"`
}

// QRStatus mirrors the four outcomes pungin/Beanfun observed from
// CheckLoginStatus, mapped to lower-case string tags so the TS side gets
// a clean discriminated union.
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
	polls     int // mock state for CheckQRLogin (Day 4 will replace)
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

// StartQRLogin runs the full init flow (getSessionKey → initQRLogin)
// and returns the QR + deeplink for the frontend to render.
//
// A fresh BeanfunClient (clean cookie jar) is minted on every call —
// pungin observed, and we confirmed via dev testing, that re-using a
// jar across attempts causes the portal to route subsequent requests
// differently and sometimes land on a URL with no session key at all.
//
// The internal session state (skey, verification token) is stashed
// for Day 4's poll + finalize to consume.
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
	s.polls = 0
	s.mu.Unlock()

	return QRStart{
		BitmapBase64: init.BitmapBase64,
		Deeplink:     init.Deeplink,
	}, nil
}

// CheckQRLogin is still mocked: returns Pending for the first two
// polls, then Approved on the third. Day 4 replaces this with the real
// POST /QRLogin/CheckLoginStatus + ResultMessage dispatch.
func (s *LoginService) CheckQRLogin() (QRStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls++
	if s.polls >= 3 {
		return QRStatusApproved, nil
	}
	return QRStatusPending, nil
}
