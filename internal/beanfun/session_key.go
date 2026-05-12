package beanfun

import (
	"context"
	"fmt"
	"log/slog"
)

// getSessionKey performs the "step 0" portal handshake: GET the portal
// default.aspx, follow redirects, scrape pSKey from the final URL.
// Pungin reference: session_key.rs:33-60 (TW path).
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
	// Keep the body around — if the pSKey regex misses we want to log
	// a preview so we can tell "Beanfun changed the redirect target"
	// apart from "we hit a 200 page directly".
	body, err := c.boundedRead(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", ErrHTTP(fmt.Errorf("portal default.aspx returned HTTP %d", resp.StatusCode))
	}

	finalURL := resp.Request.URL.String()
	slog.Info("getSessionKey: portal redirect resolved",
		"final_url", finalURL,
		"status", resp.StatusCode,
		"body_bytes", len(body))
	key, ok := sessionKeyFromURL(finalURL)
	if !ok {
		slog.Warn("getSessionKey: regex did not match final URL",
			"final_url", finalURL,
			"body_preview", truncate(string(body), 500))
		return "", ErrMissingSessionKey()
	}
	return key, nil
}

// truncate returns up to n bytes of s with a "…" marker if it was
// shortened. Used for body previews in diagnostic logs — small enough
// to fit on one terminal line, big enough to identify the page.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
