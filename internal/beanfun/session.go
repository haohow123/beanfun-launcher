package beanfun

import "fmt"

// Session holds the credentials acquired after a successful login.
// SKey and WebToken are session bearers — never log the struct with
// %+v; use the redacted Stringer below. See
// docs/beanfun-login-protocol.md § Token storage.
type Session struct {
	SKey          string
	WebToken      string
	AccountID     string // empty for QR until GetAccounts runs (later milestone)
	ServiceCode   string
	ServiceRegion string
}

const (
	// Default service info for the TW MapleStory portal.
	twDefaultServiceCode   = "610074"
	twDefaultServiceRegion = "T9"
)

// String returns a representation that redacts SKey and WebToken.
// This is what slog will pick up if the Session is logged as a value.
func (s *Session) String() string {
	if s == nil {
		return "Session<nil>"
	}
	return fmt.Sprintf(
		"Session{SKey:***, WebToken:***, AccountID:%q, ServiceCode:%q, ServiceRegion:%q}",
		s.AccountID, s.ServiceCode, s.ServiceRegion,
	)
}
