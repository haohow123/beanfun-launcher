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

func TestScrubCredentialParams(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, in, want string }{
		{"pSKey in quoted URL", `Get "https://h/p?a=1&pSKey=SECRETVALUE": refused`, `Get "https://h/p?a=1&pSKey=<redacted>": refused`},
		{"lowercase skey", "https://h/p?skey=SECRETVALUE&b=2", "https://h/p?skey=<redacted>&b=2"},
		{"no key untouched", "beanfun: http transport error: refused", "beanfun: http transport error: refused"},
		{"stops at ampersand", "?pSKey=SECRET&service=999999_T0", "?pSKey=<redacted>&service=999999_T0"},
		{"web_token", "?channel=game_zone&web_token=SECRETVALUE", "?channel=game_zone&web_token=<redacted>"},
		{"case tolerant", "?SKEY=SECRETVALUE", "?SKEY=<redacted>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scrubCredentialParams(tt.in); got != tt.want {
				t.Errorf("scrubCredentialParams(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDescribeBody(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, body, want string }{
		{"empty", "", "len=0 markers=none"},
		{"unknown", "<html>whatever</html>", "len=21 markers=none"},
		{"session expired", "<div>系統偵測到您尚未登入</div>", "len=41 markers=session-expired"},
		{"ip blocked", `<a href="/TW/BlockIPMessage.htm">x</a>`, "len=38 markers=ip-blocked"},
		{"both", "尚未登入 BlockIPMessage", "len=27 markers=session-expired,ip-blocked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := describeBody(tt.body); got != tt.want {
				t.Errorf("describeBody() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDescribeBody_NoPageText pins the property that matters: whatever the
// page contains, none of it is reproduced.
func TestDescribeBody_NoPageText(t *testing.T) {
	t.Parallel()
	const planted = "PLANTEDSECRET9999"
	got := describeBody(`<html><input name="SessionKey" value="` + planted + `"></html>`)
	if strings.Contains(got, planted) {
		t.Errorf("page text reproduced: %q", got)
	}
	if !strings.Contains(got, "len=") {
		t.Errorf("length missing: %q", got)
	}
}

// TestErrHTTP_RedactsWebTokenInWrappedURL covers the path redactURLError
// cannot reach: fmt.Errorf snapshots its message at construction, so only the
// render-time scrubber can strip a credential from an already-wrapped error.
func TestErrHTTP_RedactsWebTokenInWrappedURL(t *testing.T) {
	t.Parallel()
	const tok = "WEBTOKENSECRET123"
	raw := "https://tw.beanfun.com/beanfun_block/auth.aspx?channel=game_zone&web_token=" + tok
	inner := &url.Error{Op: "parse", URL: raw, Err: errors.New("invalid control character")}

	got := ErrHTTP(fmt.Errorf("NewRequestWithContext: %w", inner)).Error()
	if strings.Contains(got, tok) {
		t.Errorf("web token survived redaction: %q", got)
	}
	if !strings.Contains(got, "<redacted>") {
		t.Errorf("missing redaction marker: %q", got)
	}
}

// TestFrontendErrorNeedles pins the substrings the frontend matches on
// against errors this package owns. The contract is untyped and crosses
// two languages, so this is the only side of it that can fail a test.
// errGameAlreadyRunning lives in internal/launcher and is covered by the
// test of the same name there.
func TestFrontendErrorNeedles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		needle string
		where  string
		err    error
	}{
		{"ip temporarily blocked", "frontend/src/lib/errors.ts", ErrIPBlocked()},
		{"login required", "frontend/src/pages/HomePage.tsx", ErrLoginRequired()},
	}
	for _, tt := range tests {
		t.Run(tt.needle, func(t *testing.T) {
			if got := tt.err.Error(); !strings.Contains(got, tt.needle) {
				t.Errorf("%q does not contain %q — %s will stop matching", got, tt.needle, tt.where)
			}
		})
	}
}

// TestHandoffMissDetail_PreviewOnlyWhenAbsent is the security gate on this diagnostic: the body may
// only be reproduced when the page carried no launch blob at all. Every other reason means the blob
// was there, so a 500-byte preview could reproduce part of it.
func TestHandoffMissDetail_PreviewOnlyWhenAbsent(t *testing.T) {
	t.Parallel()
	const blob = "8SECRETBLOBVALUE"
	withToken := `<script>window.m_objData = {"data":"` + blob + `"};</script>`

	tests := []struct {
		name        string
		miss        handoffMiss
		body        string
		wantPreview bool
	}{
		{"absent gets a preview", handoffMissAbsent, "<html>maintenance notice</html>", true},
		{"unmatched does not", handoffMissUnmatched, withToken, false},
		{"malformed does not", handoffMissMalformed, withToken, false},
		{"empty fields do not", handoffMissEmpty, withToken, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := handoffMissDetail(tt.miss, tt.body)

			if !strings.Contains(got, string(tt.miss)) {
				t.Errorf("detail %q does not name the reason %q", got, tt.miss)
			}
			if hasPreview := strings.Contains(got, " :: body="); hasPreview != tt.wantPreview {
				t.Errorf("preview present = %v, want %v; detail = %q", hasPreview, tt.wantPreview, got)
			}
			if tt.wantPreview {
				return
			}
			if strings.Contains(got, blob) {
				t.Fatalf("detail leaked the launch blob: %q", got)
			}
		})
	}
}
