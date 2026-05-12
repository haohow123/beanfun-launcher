// Package beanfun is the Gamania Beanfun API client.
//
// Day 2: this file holds a mock LoginService so the frontend QR-login UI
// shell can be wired end-to-end without hitting real Gamania endpoints.
// Day 3+ will replace the mocks with a real HTTP client following the
// 5-step flow documented in docs/qr-login.md (init → poll → finalize).
package beanfun

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"sync"
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
	mu    sync.Mutex
	polls int
}

// NewLoginService returns a LoginService ready to serve mock responses.
func NewLoginService() *LoginService {
	return &LoginService{}
}

// StartQRLogin resets the mock state machine and returns a placeholder
// QR bitmap + deeplink so the frontend has something to render.
func (s *LoginService) StartQRLogin() (QRStart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls = 0
	return QRStart{
		BitmapBase64: mockQRBitmap(),
		Deeplink:     "beanfun://mock-deeplink",
	}, nil
}

// CheckQRLogin returns Pending for the first two polls, then Approved.
// That simulates a user scanning the QR around the 6-second mark
// (3 polls × 2 s — matching pungin/Beanfun's cadence).
func (s *LoginService) CheckQRLogin() (QRStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls++
	if s.polls >= 3 {
		return QRStatusApproved, nil
	}
	return QRStatusPending, nil
}

// mockQRBitmap renders a 200×200 checkerboard PNG and returns it as
// base64. Visually it looks nothing like a real QR — the point is just
// to prove the wire format (Go → TS binding → <img src=data:...>) works
// end-to-end so Day 3 only has to swap the bytes.
func mockQRBitmap() string {
	const (
		size = 200
		cell = 10
	)
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			if ((x/cell)+(y/cell))%2 == 0 {
				img.Set(x, y, color.Black)
			} else {
				img.Set(x, y, color.White)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}
