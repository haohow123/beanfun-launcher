package beanfun

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
)

// qrLoginInit is the internal state after Steps 1 + 2 succeed. The
// public QRStart only exposes BitmapBase64 and Deeplink; SKey and
// VerificationToken are stashed by LoginService for the Day 4 poll +
// finalize stages.
type qrLoginInit struct {
	SKey              string
	BitmapBase64      string // raw base64, no "data:" prefix
	Deeplink          string // "" if server omitted
	VerificationToken string
}

// initQRLoginResponse mirrors the InitLogin JSON envelope.
// Pungin reference: qr_init.rs:291-308. Pointer fields let us tell
// "missing" apart from "zero value".
type initQRLoginResponse struct {
	Result     *int                   `json:"Result"`
	ResultData *initQRLoginResultData `json:"ResultData"`
}

type initQRLoginResultData struct {
	QRImage  *string `json:"QRImage"`
	DeepLink *string `json:"DeepLink"`
}

// initQRLogin performs Step 1 (Login/Index) + Step 2 (Login/InitLogin)
// of pungin's QR-login state machine. Pungin reference:
// qr_init.rs:129-225.
func (c *BeanfunClient) initQRLogin(ctx context.Context, skey string) (*qrLoginInit, error) {
	// ---- Step 1: GET Login/Index ----
	indexURL, err := c.loginURLWithSKey("Login/Index", skey)
	if err != nil {
		return nil, err
	}
	req1, err := c.newRequest(ctx, "GET", indexURL)
	if err != nil {
		return nil, err
	}
	req1.Header.Set("Accept", "text/html")

	resp1, err := c.http.Do(req1)
	if err != nil {
		return nil, ErrHTTP(err)
	}
	indexBody, err := c.boundedRead(resp1)
	if err != nil {
		return nil, err
	}
	if resp1.StatusCode >= 400 {
		return nil, ErrHTTP(fmt.Errorf("Login/Index returned HTTP %d", resp1.StatusCode))
	}
	slog.Info("initQRLogin step 1: Login/Index",
		"status", resp1.StatusCode,
		"body_bytes", len(indexBody))

	token := extractVerificationToken(string(indexBody))
	if token == "" {
		slog.Warn("initQRLogin: __RequestVerificationToken not found; continuing with empty token (pungin parity)")
	}

	// ---- Step 2: GET Login/InitLogin ----
	initURL, err := c.loginURLWithSKey("Login/InitLogin", skey)
	if err != nil {
		return nil, err
	}
	req2, err := c.newRequest(ctx, "GET", initURL)
	if err != nil {
		return nil, err
	}
	req2.Header.Set("Accept", "application/json, text/plain, */*")
	req2.Header.Set("X-Requested-With", "XMLHttpRequest")
	req2.Header.Set("Referer", indexURL.String())
	// Origin = scheme + "://" + host (no path, no trailing slash).
	req2.Header.Set("Origin", c.endpoints.LoginBase.Scheme+"://"+c.endpoints.LoginBase.Host)

	resp2, err := c.http.Do(req2)
	if err != nil {
		return nil, ErrHTTP(err)
	}
	initBody, err := c.boundedRead(resp2)
	if err != nil {
		return nil, err
	}
	if resp2.StatusCode >= 400 {
		return nil, ErrHTTP(fmt.Errorf("Login/InitLogin returned HTTP %d", resp2.StatusCode))
	}
	slog.Info("initQRLogin step 2: Login/InitLogin",
		"status", resp2.StatusCode,
		"body_bytes", len(initBody))

	// ---- Parse response ----
	var env initQRLoginResponse
	if err := json.Unmarshal(initBody, &env); err != nil {
		return nil, ErrJSON(err)
	}
	if env.Result == nil {
		return nil, ErrQRInitResult("missing Result field")
	}
	if *env.Result != 0 {
		return nil, ErrQRInitResult(fmt.Sprintf("Result = %d (expected 0)", *env.Result))
	}
	if env.ResultData == nil {
		return nil, ErrQRInitResult("missing ResultData field")
	}
	if env.ResultData.QRImage == nil || *env.ResultData.QRImage == "" {
		return nil, ErrQRInitResult("missing or empty QRImage")
	}

	deeplink := ""
	if env.ResultData.DeepLink != nil && *env.ResultData.DeepLink != "" {
		deeplink = normalizeDeeplink(*env.ResultData.DeepLink)
	}

	return &qrLoginInit{
		SKey:              skey,
		BitmapBase64:      *env.ResultData.QRImage,
		Deeplink:          deeplink,
		VerificationToken: token,
	}, nil
}
