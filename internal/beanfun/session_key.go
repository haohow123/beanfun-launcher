package beanfun

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// getSessionKey performs the portal handshake: GET the portal
// default.aspx, follow redirects, scrape the session key from the
// final URL. See docs/beanfun-login-protocol.md § Step 0.
func (c *BeanfunClient) getSessionKey(ctx context.Context) (string, error) {
	u, err := c.portalURL("beanfun_block/bflogin/default.aspx?service=999999_T0")
	if err != nil {
		return "", ErrHTTP(fmt.Errorf("portal URL build: %w", err))
	}
	req, err := c.newRequest(ctx, "GET", u)
	if err != nil {
		return "", err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", ErrHTTP(err)
	}
	// Keep the body around — if the session-key regex misses we want
	// to log a preview so the operator can tell "Beanfun changed the
	// redirect target" apart from "we hit a 200 page directly".
	body, err := c.boundedRead(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", ErrHTTP(fmt.Errorf("portal default.aspx returned HTTP %d", resp.StatusCode))
	}

	finalURL := resp.Request.URL.String()
	key, ok := sessionKeyFromURL(finalURL)
	if !ok {
		slog.Warn("getSessionKey: regex did not match final URL",
			"final_url", redactedURL(finalURL),
			"status", resp.StatusCode,
			"body_preview", truncate(string(body), 500))
		return "", ErrMissingSessionKey()
	}
	slog.Info("getSessionKey: portal redirect resolved",
		"final_url", redactedURL(finalURL),
		"status", resp.StatusCode,
		"body_bytes", len(body),
		"skey_len", len(key))
	return key, nil
}

// redactedURL drops the query string, which is where the portal carries the session key.
func redactedURL(raw string) string {
	base, _, found := strings.Cut(raw, "?")
	if !found {
		return base
	}
	return base + "?<redacted>"
}
