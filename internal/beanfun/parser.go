package beanfun

import (
	"net/url"
	"regexp"
	"strings"
	"sync"
)

var (
	// pungin session_key.rs:117 — `[sp][Ss]?[Kk]ey=([^&]+)`. Matches
	// pSKey, sKey, ssKey, pKey, etc. Stops at & or end-of-string. The
	// permissiveness matches pungin's WPF-parity choice.
	sessionKeyRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`[sp][Ss]?[Kk]ey=([^&]+)`)
	})
	// pungin qr_init.rs:12 — extracts the `__RequestVerificationToken`
	// hidden input value from the Login/Index HTML. The pattern is
	// anchored on the name attribute appearing before the value
	// attribute, matching the production response shape.
	verificationTokenRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`__RequestVerificationToken[^>]+value="([^"]+)"`)
	})
)

// sessionKeyFromURL extracts the pSKey from a URL string. Returns
// (key, true) on match.
func sessionKeyFromURL(rawURL string) (string, bool) {
	m := sessionKeyRE().FindStringSubmatch(rawURL)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

// extractVerificationToken pulls the __RequestVerificationToken hidden
// input value out of the Login/Index HTML. Returns "" if absent —
// intentional, matching pungin's leniency at qr_init.rs:151-157.
func extractVerificationToken(html string) string {
	m := verificationTokenRE().FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// normalizeDeeplink unwraps the play.games.gamania.com deeplink wrapper,
// returning the decoded inner URL. Returns the input unchanged when:
//   - input is empty / whitespace
//   - input is not an absolute URL
//   - host is not play.games.gamania.com (case-insensitive)
//   - path does not contain "deeplink" (case-insensitive)
//   - the url= query parameter is missing or empty
//
// Mirrors pungin's normalize_beanfun_app_deeplink (qr_init.rs:227-280).
func normalizeDeeplink(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	u, err := url.Parse(trimmed)
	if err != nil || !u.IsAbs() {
		return raw
	}
	if !strings.EqualFold(u.Hostname(), "play.games.gamania.com") {
		return raw
	}
	if !strings.Contains(strings.ToLower(u.Path), "deeplink") {
		return raw
	}
	inner := u.Query().Get("url")
	if inner == "" {
		return raw
	}
	return inner
}
