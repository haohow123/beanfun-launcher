package beanfun

import "testing"

func TestSessionKeyFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want string
		ok   bool
	}{
		{"capital pSKey", "https://tw.newlogin.beanfun.com/login/id-pass.aspx?service=999999_T0&pSKey=ABCDEF123", "ABCDEF123", true},
		{"stops at ampersand", "https://host/path?pSKey=TOKEN&next=foo", "TOKEN", true},
		{"lowercase skey", "?skey=LOW", "LOW", true},
		{"pKey without middle S", "?pKey=PK", "PK", true},
		{"absent returns false", "https://host/no-key-here", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := sessionKeyFromURL(tt.url)
			if ok != tt.ok {
				t.Errorf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractVerificationToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		html string
		want string
	}{
		{"happy path", `<input name="__RequestVerificationToken" type="hidden" value="TKN123" />`, "TKN123"},
		// Reordered attributes don't match (regex requires name before value);
		// this mirrors pungin's regex and is fine because production servers
		// always put name first.
		{"attributes reordered (no match)", `<input value="TKN456" name="__RequestVerificationToken" type="hidden" />`, ""},
		{"missing input", `<html><body></body></html>`, ""},
		{"different token", `<input name="__RequestVerificationToken" type="hidden" value="abc/def+ghi=" />`, "abc/def+ghi="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractVerificationToken(tt.html)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeDeeplink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"wrapped", "https://play.games.gamania.com/foo/deeplink/?url=https%3A%2F%2Fapp.example%2Fauth%3Ft%3D1", "https://app.example/auth?t=1"},
		{"plain passes through", "https://app.example/auth?token=1", "https://app.example/auth?token=1"},
		{"different host passes through", "https://other.com/foo?url=x", "https://other.com/foo?url=x"},
		{"no deeplink in path", "https://play.games.gamania.com/foo/bar/?url=x", "https://play.games.gamania.com/foo/bar/?url=x"},
		{"missing url param", "https://play.games.gamania.com/foo/deeplink/", "https://play.games.gamania.com/foo/deeplink/"},
		{"empty url param", "https://play.games.gamania.com/foo/deeplink/?url=", "https://play.games.gamania.com/foo/deeplink/?url="},
		{"whitespace input", "  ", "  "},
		{"non-absolute URL", "not-a-url", "not-a-url"},
		{"uppercase host", "https://Play.Games.Gamania.com/foo/deeplink/?url=https%3A%2F%2Finner", "https://inner"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeDeeplink(tt.in)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
