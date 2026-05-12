package beanfun

import (
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// Compiled regexes used across the login flow. See
// docs/beanfun-login-protocol.md for the full wire spec these match.
var (
	// Conservative session-key match. Accepts the lowercase `skey=` the
	// portal currently emits and the `pSKey=` shape some endpoints
	// still echo back. Stops at `&` or end of string.
	sessionKeyRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`[sp][Ss]?[Kk]ey=([^&]+)`)
	})
	// Anti-forgery token from Login/Index's hidden input. Anchored on
	// the name attribute appearing before the value attribute, which
	// is the production response shape.
	verificationTokenRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`__RequestVerificationToken[^>]+value="([^"]+)"`)
	})

	// Hidden-input scraper for Login/SendLogin's HTML response.
	// `(?is)` = case-insensitive + dot matches newline.
	inputTagRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`(?is)<input[^>]+>`)
	})
	nameAttrRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`(?i)name\s*=\s*['"]([^'"]+)['"]`)
	})
	// `*` (not `+`) on the value group preserves empty `value=""`,
	// which is the production shape for the ServiceCode / ServiceRegion
	// fields.
	valueAttrRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`(?i)value\s*=\s*['"]([^'"]*)['"]`)
	})
	submitTypeRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`(?i)type\s*=\s*["']submit["']`)
	})
)

// sessionKeyFromURL extracts the pSKey/skey value from a URL string.
// Returns (key, true) on match.
func sessionKeyFromURL(rawURL string) (string, bool) {
	m := sessionKeyRE().FindStringSubmatch(rawURL)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

// extractVerificationToken pulls the __RequestVerificationToken hidden
// input value out of the Login/Index HTML. Returns "" if absent — the
// intended behaviour; downstream callers send the
// RequestVerificationToken header only when the value is non-empty.
func extractVerificationToken(html string) string {
	m := verificationTokenRE().FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// extractHiddenInputs scrapes every non-submit <input> tag that has
// both name= and value= attributes, returning them as url.Values
// ready to be Encode()'d into an x-www-form-urlencoded POST body.
//
// url.Values sorts keys alphabetically on Encode(); the server accepts
// arbitrary field order, so the simpler Go API wins.
func extractHiddenInputs(html string) url.Values {
	out := url.Values{}
	for _, tag := range inputTagRE().FindAllString(html, -1) {
		if submitTypeRE().MatchString(tag) {
			continue
		}
		nameM := nameAttrRE().FindStringSubmatch(tag)
		valM := valueAttrRE().FindStringSubmatch(tag)
		if len(nameM) < 2 || len(valM) < 2 {
			continue
		}
		out.Add(nameM[1], valM[1])
	}
	return out
}

// normalizeDeeplink unwraps the play.games.gamania.com deeplink
// wrapper, returning the decoded inner URL. Returns the input
// unchanged when any of these is true:
//
//   - input is empty / whitespace
//   - input is not an absolute URL
//   - host is not play.games.gamania.com (case-insensitive)
//   - path does not contain "deeplink" (case-insensitive)
//   - the url= query parameter is missing or empty
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
