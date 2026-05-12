package beanfun

import "fmt"

// Session holds the credentials acquired after a successful login.
// SKey and WebToken are session bearers — never log the struct
// directly with %+v; use the redacted Stringer below.
type Session struct {
	SKey          string
	WebToken      string
	AccountID     string // empty for QR until GetAccounts runs (Milestone 5+)
	ServiceCode   string
	ServiceRegion string
}

const (
	// Default service info for the TW MapleStory portal — matches
	// pungin's LoginRegion::TW::default_service_code / service_region
	// at session.rs:159-170.
	twDefaultServiceCode   = "610074"
	twDefaultServiceRegion = "T9"
)

// String returns a representation that redacts SKey and WebToken,
// mirroring pungin's Debug impl for Session (session.rs:115-126). This
// is what slog will pick up if the Session is logged as a key value.
func (s *Session) String() string {
	if s == nil {
		return "Session<nil>"
	}
	return fmt.Sprintf(
		"Session{SKey:***, WebToken:***, AccountID:%q, ServiceCode:%q, ServiceRegion:%q}",
		s.AccountID, s.ServiceCode, s.ServiceRegion,
	)
}
