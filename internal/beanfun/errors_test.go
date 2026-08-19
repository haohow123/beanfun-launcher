package beanfun

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestErrHTTP_RedactsSessionKeyInURL(t *testing.T) {
	t.Parallel()
	const skey = "SKEYFIXTURE0123456789"
	raw := "https://tw.newlogin.beanfun.com/login/id-pass.aspx?service=999999_T0&pSKey=" + skey

	tests := []struct {
		name string
		in   error
	}{
		{"direct url.Error", &url.Error{Op: "Get", URL: raw, Err: errors.New("connection refused")}},
		{"wrapped url.Error", fmt.Errorf("NewRequestWithContext: %w", &url.Error{Op: "parse", URL: raw, Err: errors.New("invalid control character")})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrHTTP(tt.in).Error()
			if strings.Contains(got, skey) {
				t.Errorf("session key survived redaction: %q", got)
			}
			if strings.Contains(got, "pSKey="+skey) {
				t.Errorf("pSKey value survived redaction: %q", got)
			}
			if !strings.Contains(got, "<redacted>") {
				t.Errorf("missing redaction marker: %q", got)
			}
			if !strings.Contains(got, "id-pass.aspx") {
				t.Errorf("redaction destroyed the diagnostic path: %q", got)
			}
		})
	}
}

func TestErrHTTP_PreservesErrorSemantics(t *testing.T) {
	t.Parallel()
	err := ErrHTTP(&url.Error{Op: "Get", URL: "https://tw.newlogin.beanfun.com/x?pSKey=SKEYFIXTURE0123456789", Err: context.DeadlineExceeded})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("errors.Is(context.DeadlineExceeded) broken by redaction")
	}
	var ue *url.Error
	if !errors.As(err, &ue) {
		t.Fatal("errors.As(*url.Error) broken by redaction")
	}
	if strings.Contains(ue.URL, "SKEYFIXTURE0123456789") {
		t.Errorf("url.Error.URL still carries the key: %q", ue.URL)
	}
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindHTTP {
		t.Errorf("Kind = %v, want KindHTTP", err)
	}
}

func TestErrHTTP_NonURLErrorUnchanged(t *testing.T) {
	t.Parallel()
	const want = "beanfun: http transport error: plain failure"
	if got := ErrHTTP(errors.New("plain failure")).Error(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRedactedURL(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, in, want string }{
		{"query dropped", "https://h/p?a=1&pSKey=SECRET", "https://h/p?<redacted>"},
		{"no query untouched", "https://h/p", "https://h/p"},
		{"empty", "", ""},
		{"bare question mark", "https://h/p?", "https://h/p?<redacted>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactedURL(tt.in); got != tt.want {
				t.Errorf("redactedURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestScrubSessionKeys(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, in, want string }{
		{"pSKey in quoted URL", `Get "https://h/p?a=1&pSKey=SECRETVALUE": refused`, `Get "https://h/p?a=1&pSKey=<redacted>": refused`},
		{"lowercase skey", "https://h/p?skey=SECRETVALUE&b=2", "https://h/p?skey=<redacted>&b=2"},
		{"no key untouched", "beanfun: http transport error: refused", "beanfun: http transport error: refused"},
		{"stops at ampersand", "?pSKey=SECRET&service=999999_T0", "?pSKey=<redacted>&service=999999_T0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scrubSessionKeys(tt.in); got != tt.want {
				t.Errorf("scrubSessionKeys(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
