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

// walkNodes invokes visit on n and every descendant. Subtrees rooted
// at <script> or <style> are skipped — their text content shouldn't
// be parsed as markup even when it visually contains tag-like
// substrings (e.g. the JS template that builds account rows in
// game_server_account_list.aspx).
func walkNodes(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == "script" || c.Data == "style") {
			continue
		}
		walkNodes(c, visit)
	}
}

// extractHiddenInputs walks every <input> in body and returns the
// non-submit ones that carry both name and value attributes, as
// url.Values ready for x-www-form-urlencoded encoding.
//
// url.Values sorts keys alphabetically on Encode(); the server accepts
// arbitrary field order, so the simpler Go API wins.
func extractHiddenInputs(body string) url.Values {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return url.Values{}
	}
	out := url.Values{}
	walkNodes(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "input" {
			return
		}
		var name, value, typ string
		hasValue := false
		for _, a := range n.Attr {
			switch a.Key {
			case "name":
				name = a.Val
			case "value":
				value = a.Val
				hasValue = true
			case "type":
				typ = a.Val
			}
		}
		if name == "" || !hasValue {
			return
		}
		if strings.EqualFold(typ, "submit") {
			return
		}
		out.Add(name, value)
	})
	return out
}

// extractAccounts walks game_server_account_list.aspx HTML and returns
// one Account per <div> carrying id + sn + name attributes. Names
// arrive HTML-entity decoded by the parser; the result slice is sorted
// ascending by SSN (fixed-width digit strings sort lexicographically
// equal to numerically).
//
// Enabled is best-effort: it reads the onclick attribute of the
// nearest ancestor (typically the wrapping <li>). Current production
// HTML always renders visible rows with a non-empty onclick, so this
// stays true in practice; the field is exposed in case the portal
// regresses to using empty onclick for disabled rows (the heuristic
// pungin's HTML used).
func extractAccounts(body string) []Account {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil
	}
	var out []Account
	walkNodes(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "div" {
			return
		}
		var sid, ssn, sname string
		hasSN := false
		for _, a := range n.Attr {
			switch a.Key {
			case "id":
				sid = a.Val
			case "sn":
				ssn = a.Val
				hasSN = true
			case "name":
				sname = a.Val
			}
		}
		if !hasSN || sid == "" || ssn == "" || sname == "" {
			return
		}
		out = append(out, Account{
			SID:     sid,
			SSN:     ssn,
			SName:   sname,
			Enabled: ancestorOnclickIsNonEmpty(n),
		})
	})
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].SSN < out[j].SSN
	})
	return out
}

// ancestorOnclickIsNonEmpty walks up the DOM looking for the nearest
// ancestor with an onclick attribute. Returns:
//   - true  if onclick is non-empty (rendered, clickable row)
//   - false if onclick is "" (disabled — pungin's heuristic)
//   - true  if no ancestor has onclick (assume enabled — defensive)
func ancestorOnclickIsNonEmpty(n *html.Node) bool {
	for p := n.Parent; p != nil; p = p.Parent {
		for _, a := range p.Attr {
			if a.Key == "onclick" {
				return a.Val != ""
			}
		}
	}
	return true
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
