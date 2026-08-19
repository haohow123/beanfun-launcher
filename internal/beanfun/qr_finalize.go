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
// scan for a bfWebToken session cookie. See
// docs/beanfun-login-protocol.md §§ Step 4–7.
func (c *BeanfunClient) finalizeQRLogin(ctx context.Context, init *qrLoginInit) (*Session, error) {
	indexURL, err := c.loginURLWithSKey("Login/Index", init.SKey)
	if err != nil {
		return nil, err
	}

	// ---- Step 4: GET /QRLogin/QRLogin (handshake) ----
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
	slog.Info("finalizeQRLogin step 4: handshake ok", "status", resp1.StatusCode)

	// ---- Step 5: GET /Login/SendLogin (scrape hidden form) ----
	sendLoginURL, err := c.loginURL("Login/SendLogin")
	if err != nil {
		return nil, err
	}
	req2, err := c.newRequest(ctx, "GET", sendLoginURL)
	if err != nil {
		return nil, err
	}
	// QR-specific Accept (differs from the non-QR variant by the
	// image/* tokens).
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
			"body", describeBody(string(body2)))
		return nil, ErrSendLoginNoFormData()
	}
	slog.Info("finalizeQRLogin step 5: scraped form", "field_count", len(form))

	// ---- Step 6: POST return.aspx (response discarded) ----
	// The bfWebToken cookie potentially set by this response is
	// intentionally not consumed — the canonical token comes from
	// step 7's redirect chain. We just need the server to accept
	// the form post and advance the session state.
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
	resp3, err := c.http.Do(req3)
	if err != nil {
		return nil, ErrHTTP(err)
	}
	if _, err := c.boundedRead(resp3); err != nil {
		return nil, err
	}
	if resp3.StatusCode >= 400 {
		return nil, ErrHTTP(fmt.Errorf("return.aspx step 6 returned HTTP %d", resp3.StatusCode))
	}
	slog.Info("finalizeQRLogin step 6: return.aspx ok", "status", resp3.StatusCode)

	// ---- Step 7: POST return.aspx with AuthKey=OK (follow redirects) ----
	// Exact 5-field body. SessionKey here is the OUTER skey from
	// getSessionKey, NOT the inner SessionKey scraped in step 5.
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
		return nil, ErrHTTP(fmt.Errorf("return.aspx step 7 returned HTTP %d", resp4.StatusCode))
	}
	slog.Info("finalizeQRLogin step 7: LoginCompleted ok", "status", resp4.StatusCode)

	// Read bfWebToken from the shared cookie jar (not resp4.Cookies()).
	// The cookie can be set on a later redirect hop rather than the
	// immediate 302, and only the jar sees all hops.
	webToken := readCookieFromJar(c.jar, c.endpoints.PortalBase, "bfWebToken")
	if webToken == "" {
		slog.Error("finalizeQRLogin: bfWebToken cookie missing after step 7")
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
