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
	// Drain the body to release the connection back to the pool, even
	// though we only care about resp.Request.URL (the final URL after
	// redirects).
	if _, err := c.boundedRead(resp); err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", ErrHTTP(fmt.Errorf("portal default.aspx returned HTTP %d", resp.StatusCode))
	}

	finalURL := resp.Request.URL.String()
	slog.Info("getSessionKey: portal redirect resolved",
		"final_host", resp.Request.URL.Hostname(),
		"status", resp.StatusCode)
	key, ok := sessionKeyFromURL(finalURL)
	if !ok {
		slog.Warn("getSessionKey: regex did not match final URL")
		return "", ErrMissingSessionKey()
	}
	return key, nil
}
