package beanfun

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// qrPollOutcome is the parsed result of one CheckLoginStatus call.
// Internal type; the service layer maps it to the public QRStatus
// values for the frontend.
type qrPollOutcome int

const (
	pollOutcomeFailed qrPollOutcome = iota + 1
	pollOutcomeWaitLogin
	pollOutcomeTokenExpired
	pollOutcomeApproved
)

// qrCheckResponse mirrors the CheckLoginStatus JSON envelope. Pointer
// field lets us distinguish "missing" from "empty string".
type qrCheckResponse struct {
	ResultMessage *string `json:"ResultMessage"`
}

// pollQRLoginStatus runs one POST /QRLogin/CheckLoginStatus and
// returns the dispatched outcome. The caller drives the poll cadence
// (every 2 seconds is what the frontend uses). See
// docs/beanfun-login-protocol.md § Step 3.
func (c *BeanfunClient) pollQRLoginStatus(ctx context.Context, init *qrLoginInit) (qrPollOutcome, error) {
	indexURL, err := c.loginURLWithSKey("Login/Index", init.SKey)
	if err != nil {
		return 0, err
	}
	pollURL, err := c.loginURL("QRLogin/CheckLoginStatus")
	if err != nil {
		return 0, err
	}

	// Empty body with explicit Content-Length: 0 — without that header
	// Go picks chunked transfer-encoding, which the Beanfun server
	// rejects with HTTP 411. Both the field on the request struct AND
	// the header must be set.
	req, err := http.NewRequestWithContext(ctx, "POST", pollURL.String(), strings.NewReader(""))
	if err != nil {
		return 0, ErrHTTP(fmt.Errorf("NewRequestWithContext: %w", err))
	}
	req.ContentLength = 0
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", indexURL.String())
	req.Header.Set("Origin", c.endpoints.LoginBase.Scheme+"://"+c.endpoints.LoginBase.Host)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Content-Length", "0")
	if init.VerificationToken != "" {
		req.Header.Set("RequestVerificationToken", init.VerificationToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, ErrHTTP(err)
	}
	body, err := c.boundedRead(resp)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode >= 400 {
		return 0, ErrHTTP(fmt.Errorf("CheckLoginStatus returned HTTP %d", resp.StatusCode))
	}
	slog.Info("pollQRLoginStatus", "status", resp.StatusCode, "body_bytes", len(body))

	var env qrCheckResponse
	if err := json.Unmarshal(body, &env); err != nil {
		return 0, ErrJSON(err)
	}
	if env.ResultMessage == nil {
		return 0, ErrServerMessage(string(body))
	}
	switch strings.TrimSpace(*env.ResultMessage) {
	case "Failed":
		return pollOutcomeFailed, nil
	case "Wait Login":
		return pollOutcomeWaitLogin, nil
	case "Token Expired":
		return pollOutcomeTokenExpired, nil
	case "Success":
		return pollOutcomeApproved, nil
	default:
		return 0, ErrServerMessage(string(body))
	}
}
