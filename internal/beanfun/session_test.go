package beanfun

import (
	"strings"
	"testing"
)

func TestSession_String_RedactsSecrets(t *testing.T) {
	t.Parallel()
	s := &Session{
		SKey:          "super-secret-skey-abc123",
		WebToken:      "super-secret-token-xyz789",
		AccountID:     "alice",
		ServiceCode:   "610074",
		ServiceRegion: "T9",
	}
	out := s.String()
	if strings.Contains(out, "super-secret") {
		t.Errorf("String() leaked a secret value: %q", out)
	}
	if !strings.Contains(out, "***") {
		t.Errorf("String() should mark redacted fields with ***: %q", out)
	}
	for _, want := range []string{"alice", "610074", "T9"} {
		if !strings.Contains(out, want) {
			t.Errorf("String() missing non-secret field %q: %q", want, out)
		}
	}
}

func TestSession_String_Nil(t *testing.T) {
	t.Parallel()
	var s *Session
	if got, want := s.String(), "Session<nil>"; got != want {
		t.Errorf("nil Session.String() = %q, want %q", got, want)
	}
}
