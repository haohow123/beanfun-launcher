package beanfun

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

// Compiled regexes used by the URL-string and single-input scrapers.
// See docs/beanfun-login-protocol.md for the full wire spec these
// match. Multi-attribute HTML scrapers (hidden inputs, account rows)
// switched to DOM walking in Milestone 5.5 — see extractHiddenInputs
// and extractAccounts below.
var (
	// Conservative session-key match. Accepts the lowercase `skey=` the
	// portal currently emits and the `pSKey=` shape some endpoints
	// still echo back. Stops at `&` or end of string.
	sessionKeyRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`[sp][Ss]?[Kk]ey=([^&]+)`)
	})
	// Anti-forgery token from Login/Index's hidden input. Single input,
	// single attribute — regex stays clearer than walking the DOM just
	// for one node.
	verificationTokenRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`__RequestVerificationToken[^>]+value="([^"]+)"`)
	})

	// Step-1 OTP scrapers — all match inline JS literals embedded in
	// game_start_step2.aspx's HTML body. Per Beanfun the patterns are:
	//
	//   GetResultByLongPolling&key=KEYVAL"
	//   MyAccountData.ServiceAccountCreateTime + "KEY=VALUE";
	//   ServiceAccountCreateTime: "2024-01-15 12:34:56";
	//
	// And step-2's get_cookies.ashx body has:
	//
	//   var m_strSecretCode = 'CODE';
	//
	// `.` is the byte wildcard (Go's regexp doesn't match newlines by
	// default), so each capture stops at the closing terminator on the
	// same line — exactly what the JS-literal form provides.
	longPollingKeyRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`GetResultByLongPolling&key=(.*)"`)
	})
	unkDataRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`MyAccountData.ServiceAccountCreateTime \+ "(.*)=(.*)";`)
	})
	createTimeFallbackRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`ServiceAccountCreateTime: "([^"]+)"`)
	})
	secretCodeRE = sync.OnceValue(func() *regexp.Regexp {
		return regexp.MustCompile(`var m_strSecretCode = '(.*)';`)
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
func extractVerificationToken(htmlBody string) string {
	m := verificationTokenRE().FindStringSubmatch(htmlBody)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// extractHiddenInputs streams the body's tokens and collects every
// non-submit <input> that carries both name and value attributes, as
// url.Values ready for x-www-form-urlencoded encoding.
//
// url.Values sorts keys alphabetically on Encode(); the server accepts
// arbitrary field order, so the simpler Go API wins.
func extractHiddenInputs(body string) url.Values {
	out := url.Values{}
	z := html.NewTokenizer(strings.NewReader(body))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return out
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tn, hasAttr := z.TagName()
		if !hasAttr || string(tn) != "input" {
			continue
		}
		var name, value, typ string
		hasValue := false
		for {
			k, v, more := z.TagAttr()
			switch string(k) {
			case "name":
				name = string(v)
			case "value":
				value = string(v)
				hasValue = true
			case "type":
				typ = string(v)
			}
			if !more {
				break
			}
		}
		if name == "" || !hasValue || strings.EqualFold(typ, "submit") {
			continue
		}
		out.Add(name, value)
	}
}

// extractAccounts streams the body's tokens and returns one Account
// per <div> carrying id + sn + name attributes. Names arrive
// HTML-entity decoded by the tokenizer; the result is sorted ascending
// by SSN (fixed-width digit strings sort lexicographically equal to
// numerically).
//
// <script> and <style> subtrees are skipped via an open-tag counter
// — production HTML embeds a JS template that visually contains
// matching <div sn= name= id=> markup but is string-concatenated
// content, not real elements.
func extractAccounts(body string) []Account {
	var out []Account
	z := html.NewTokenizer(strings.NewReader(body))
	inSkipped := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken:
			tn, hasAttr := z.TagName()
			name := string(tn)
			if name == "script" || name == "style" {
				inSkipped++
				continue
			}
			if inSkipped > 0 || name != "div" || !hasAttr {
				continue
			}
			var sid, ssn, sname string
			for {
				k, v, more := z.TagAttr()
				switch string(k) {
				case "id":
					sid = string(v)
				case "sn":
					ssn = string(v)
				case "name":
					sname = string(v)
				}
				if !more {
					break
				}
			}
			if sid == "" || ssn == "" || sname == "" {
				continue
			}
			out = append(out, Account{SID: sid, SSN: ssn, SName: sname})
		case html.EndTagToken:
			tn, _ := z.TagName()
			n := string(tn)
			if (n == "script" || n == "style") && inSkipped > 0 {
				inSkipped--
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].SSN < out[j].SSN
	})
	return out
}

// extractLongPollingKey scrapes the inline JS
// `GetResultByLongPolling&key=VALUE"` literal from
// game_start_step2.aspx's response body. Returns "" if absent —
// callers treat that as a fatal OTP-init failure.
func extractLongPollingKey(htmlBody string) string {
	m := longPollingKeyRE().FindStringSubmatch(htmlBody)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// extractUnkData scrapes the TW-only inline JS literal
// `MyAccountData.ServiceAccountCreateTime + "K=V";`, percent-decoding
// both halves. Returns ("", "", false) if absent — callers treat as
// a fatal TW init failure (HK never emits this literal, which is fine
// because we're TW-only anyway).
func extractUnkData(htmlBody string) (key, value string, ok bool) {
	m := unkDataRE().FindStringSubmatch(htmlBody)
	if len(m) < 3 {
		return "", "", false
	}
	k, err := url.QueryUnescape(m[1])
	if err != nil {
		return "", "", false
	}
	v, err := url.QueryUnescape(m[2])
	if err != nil {
		return "", "", false
	}
	return k, v, true
}

// extractCreateTimeFallback scrapes the `ServiceAccountCreateTime:
// "..."` literal — used as a fallback when the caller doesn't
// already hold an account's createTime. Returns "" if absent.
func extractCreateTimeFallback(htmlBody string) string {
	m := createTimeFallbackRE().FindStringSubmatch(htmlBody)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// extractSecretCode scrapes the `var m_strSecretCode = 'VALUE';`
// literal from get_cookies.ashx's response body. Returns "" if absent.
func extractSecretCode(htmlBody string) string {
	m := secretCodeRE().FindStringSubmatch(htmlBody)
	if len(m) < 2 {
		return ""
	}
	return m[1]
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
