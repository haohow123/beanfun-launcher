package beanfun

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

// finalizeQRLogin runs the 4-call handshake that swaps an approved QR
// scan for a bfWebToken session cookie. Pungin reference:
// qr_finalize.rs:149-250.
func (c *BeanfunClient) finalizeQRLogin(ctx context.Context, init *qrLoginInit) (*Session, error) {
	indexURL, err := c.loginURLWithSKey("Login/Index", init.SKey)
	if err != nil {
		return nil, err
	}

	// ---- Step 1: GET /QRLogin/QRLogin (handshake) ----
	qrLoginURL, err := c.loginURL("QRLogin/QRLogin")
	if err != nil {
		return nil, err
	}
	req1, err := c.newRequest(ctx, "GET", qrLoginURL)
	if err != nil {
		return nil, err
	}
	req1.Header.Set("Accept", "application/json, text/plain, */*")
	req1.Header.Set("Referer", indexURL.String())
	resp1, err := c.http.Do(req1)
	if err != nil {
		return nil, ErrHTTP(err)
	}
	if _, err := c.boundedRead(resp1); err != nil {
		return nil, err
	}
	if resp1.StatusCode >= 400 {
		return nil, ErrHTTP(fmt.Errorf("QRLogin/QRLogin returned HTTP %d", resp1.StatusCode))
	}
	slog.Info("finalizeQRLogin step 1: handshake ok", "status", resp1.StatusCode)

	// ---- Step 2: GET /Login/SendLogin (scrape hidden form) ----
	sendLoginURL, err := c.loginURL("Login/SendLogin")
	if err != nil {
		return nil, err
	}
	req2, err := c.newRequest(ctx, "GET", sendLoginURL)
	if err != nil {
		return nil, err
	}
	// QR-specific Accept (differs from non-QR by the image/* tokens);
	// pungin qr_finalize.rs:168.
	req2.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req2.Header.Set("Referer", indexURL.String())
	resp2, err := c.http.Do(req2)
	if err != nil {
		return nil, ErrHTTP(err)
	}
	body2, err := c.boundedRead(resp2)
	if err != nil {
		return nil, err
	}
	if resp2.StatusCode >= 400 {
		return nil, ErrHTTP(fmt.Errorf("Login/SendLogin returned HTTP %d", resp2.StatusCode))
	}
	form := extractHiddenInputs(string(body2))
	if len(form) == 0 {
		slog.Warn("finalizeQRLogin: SendLogin returned no form data",
			"body_preview", truncate(string(body2), 500))
		return nil, ErrSendLoginNoFormData()
	}
	slog.Info("finalizeQRLogin step 2: scraped form", "field_count", len(form))

	// ---- Step 3: POST return.aspx (NO REDIRECT, result discarded) ----
	// Pungin's QR finalize uses the no-redirect client so it could read
	// Set-Cookie from the 302 — but the captured value is discarded
	// because step 4 is canonical. We mirror the no-redirect call (still
	// matters because the server may not auto-handle the follow-up),
	// but skip the Set-Cookie scrape entirely.
	returnURL, err := c.portalURL("beanfun_block/bflogin/return.aspx")
	if err != nil {
		return nil, err
	}
	step3Body := form.Encode()
	req3, err := http.NewRequestWithContext(ctx, "POST", returnURL.String(), strings.NewReader(step3Body))
	if err != nil {
		return nil, ErrHTTP(fmt.Errorf("NewRequestWithContext: %w", err))
	}
	req3.Header.Set("User-Agent", c.userAgent)
	req3.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req3.Header.Set("Referer", c.endpoints.LoginBase.String())
	resp3, err := c.httpNoRedirect.Do(req3)
	if err != nil {
		return nil, ErrHTTP(err)
	}
	if _, err := c.boundedRead(resp3); err != nil {
		return nil, err
	}
	// No-redirect client surfaces 302 directly; accept any 2xx or 3xx.
	if resp3.StatusCode < 200 || resp3.StatusCode >= 400 {
		return nil, ErrHTTP(fmt.Errorf("return.aspx step 3 returned HTTP %d", resp3.StatusCode))
	}
	slog.Info("finalizeQRLogin step 3: return.aspx (no-redirect) ok", "status", resp3.StatusCode)

	// ---- Step 4: POST return.aspx with AuthKey=OK (follow redirects) ----
	// 5-field form exactly per pungin completed.rs:216-222. SessionKey
	// is the OUTER skey (not the inner one from the SendLogin form).
	step4Body := url.Values{
		"SessionKey":       {init.SKey},
		"AuthKey":          {"OK"},
		"ServiceCode":      {""},
		"ServiceRegion":    {""},
		"ServiceAccountSN": {"0"},
	}.Encode()
	req4, err := http.NewRequestWithContext(ctx, "POST", returnURL.String(), strings.NewReader(step4Body))
	if err != nil {
		return nil, ErrHTTP(fmt.Errorf("NewRequestWithContext: %w", err))
	}
	req4.Header.Set("User-Agent", c.userAgent)
	req4.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req4.Header.Set("Referer", c.endpoints.LoginBase.String())
	resp4, err := c.http.Do(req4)
	if err != nil {
		return nil, ErrHTTP(err)
	}
	if _, err := c.boundedRead(resp4); err != nil {
		return nil, err
	}
	if resp4.StatusCode >= 400 {
		return nil, ErrHTTP(fmt.Errorf("return.aspx step 4 returned HTTP %d", resp4.StatusCode))
	}
	slog.Info("finalizeQRLogin step 4: LoginCompleted ok", "status", resp4.StatusCode)

	// Read bfWebToken from the shared cookie jar (not resp4.Cookies()).
	// Pungin's 2026-04-16 hotfix (completed.rs:46-49) observed that the
	// cookie can be set on a later redirect hop, not just the immediate
	// 302 — only the jar sees all of them.
	webToken := readCookieFromJar(c.jar, c.endpoints.PortalBase, "bfWebToken")
	if webToken == "" {
		slog.Error("finalizeQRLogin: bfWebToken cookie missing after step 4")
		return nil, ErrMissingWebToken()
	}
	return &Session{
		SKey:          init.SKey,
		WebToken:      webToken,
		AccountID:     "", // empty for QR; populated by GetAccounts later
		ServiceCode:   twDefaultServiceCode,
		ServiceRegion: twDefaultServiceRegion,
	}, nil
}

// readCookieFromJar returns the value of the cookie with the given
// name visible to baseURL, or "" if absent. Case-insensitive on the
// name to defend against a future server change emitting BFWebToken /
// bfwebtoken / etc.
func readCookieFromJar(jar http.CookieJar, baseURL *url.URL, name string) string {
	for _, c := range jar.Cookies(baseURL) {
		if strings.EqualFold(c.Name, name) {
			return c.Value
		}
	}
	return ""
}
