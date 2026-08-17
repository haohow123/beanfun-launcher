package beanfun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// OTPResult is the decrypted one-time game-launch credential. Token
// is held as []byte (not string) so the launcher can Zero it after
// the spawn call consumes it.
type OTPResult struct {
	Token []byte
}

// otpStep1 carries the handoff literal scraped from
// game_start_step2.aspx.
type otpStep1 struct {
	handoff launchHandoff
}

// FetchOTP fetches game_start_step2.aspx, decodes the LaunchTicket out
// of that page's handoff blob, and exchanges it for an OTP at
// get_webstart_otp_v2.ashx. The session's bfWebToken cookie must
// already be in the client's jar. See
// docs/beanfun-login-protocol.md § 9.
func (c *BeanfunClient) FetchOTP(ctx context.Context, sess *Session, acc Account) (OTPResult, error) {
	if sess == nil {
		return OTPResult{}, ErrLoginRequired()
	}

	step1, err := c.otpStep1(ctx, sess, acc)
	if err != nil {
		return OTPResult{}, err
	}
	info, err := decodeLaunchData(step1.handoff.Data)
	if err != nil {
		return OTPResult{}, err
	}
	defer Zero(info.LaunchTicket)
	payload, err := c.otpFetchV2(ctx, step1.handoff.SN, info.LaunchTicket)
	if err != nil {
		return OTPResult{}, err
	}
	token, err := decryptOTPPayload(payload)
	if err != nil {
		return OTPResult{}, err
	}
	slog.Info("FetchOTP: token acquired", "len", len(token), "sid", acc.SID)
	return OTPResult{Token: token}, nil
}

// otpStep1: GET game_start_step2.aspx; scrape the m_objData handoff.
func (c *BeanfunClient) otpStep1(ctx context.Context, sess *Session, acc Account) (otpStep1, error) {
	u, err := c.portalURL("beanfun_block/game_zone/game_start_step2.aspx")
	if err != nil {
		return otpStep1{}, err
	}
	q := u.Query()
	q.Set("service_code", sess.ServiceCode)
	q.Set("service_region", sess.ServiceRegion)
	q.Set("sotp", acc.SSN)
	q.Set("dt", time.Now().UTC().Format("20060102150405"))
	u.RawQuery = q.Encode()

	req, err := c.newRequest(ctx, "GET", u)
	if err != nil {
		return otpStep1{}, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return otpStep1{}, ErrHTTP(err)
	}
	body, err := c.boundedRead(resp)
	if err != nil {
		return otpStep1{}, err
	}
	if resp.StatusCode >= 400 {
		return otpStep1{}, ErrHTTP(fmt.Errorf("game_start_step2.aspx returned HTTP %d", resp.StatusCode))
	}

	bodyStr := string(body)
	if isSessionExpiredBody(bodyStr) {
		return otpStep1{}, ErrSessionExpired()
	}
	// No withBody: the page carries m_objData, whose blob decodes to a
	// live LaunchTicket with nothing but the tables in this package and
	// the key embedded in the blob itself.
	handoff, ok := extractLaunchHandoff(bodyStr)
	if !ok {
		return otpStep1{}, ErrOTPInit(fmt.Sprintf(
			"m_objData not found in game_start_step2.aspx (body %d bytes)", len(bodyStr)))
	}
	slog.Info("FetchOTP step 1: game_start_step2.aspx",
		"handoff_data_len", len(handoff.Data))
	return otpStep1{handoff: handoff}, nil
}

// buildOTPV2Body renders the body get_webstart_otp_v2.ashx expects:
// {"SN":…,"LaunchTicket":…,"CV":…,"Hash":…,"arch":…}. Only SN survives
// from the retired query-string form; CV, Hash and arch are the
// client-integrity claim (see client_integrity.go).
//
// Hand-rolled rather than marshalled from a struct so the launch ticket
// never becomes an unzeroable Go string. The ticket needs no escaping
// because decodeLaunchData has already verified it is 64 hex characters.
func buildOTPV2Body(sn string, launchTicket []byte) ([]byte, error) {
	snJSON, err := json.Marshal(sn)
	if err != nil {
		return nil, ErrJSON(err)
	}
	var b bytes.Buffer
	b.Grow(len(snJSON) + len(launchTicket) + len(ggmCV) + len(ggmDLLSHA256) + 96)
	b.WriteString(`{"SN":`)
	b.Write(snJSON)
	b.WriteString(`,"LaunchTicket":"`)
	b.Write(launchTicket)
	b.WriteString(`","CV":"` + ggmCV + `","Hash":"` + ggmDLLSHA256 + `","arch":"` + ggmArch() + `"}`)
	return b.Bytes(), nil
}

// otpV2Response mirrors the reply envelope. Pointer fields tell
// "missing" apart from a zero value.
type otpV2Response struct {
	Result  *int    `json:"result"`
	Data    *string `json:"data"`
	Message *string `json:"message"`
}

// otpFetchV2 POSTs the launch ticket to get_webstart_otp_v2.ashx and
// returns the still-encrypted data field.
func (c *BeanfunClient) otpFetchV2(ctx context.Context, sn string, launchTicket []byte) (string, error) {
	u, err := c.portalURL("beanfun_block/generic_handlers/get_webstart_otp_v2.ashx")
	if err != nil {
		return "", err
	}
	body, err := buildOTPV2Body(sn, launchTicket)
	if err != nil {
		return "", err
	}
	defer Zero(body)
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return "", ErrHTTP(fmt.Errorf("NewRequestWithContext: %w", err))
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", ErrHTTP(err)
	}
	respBody, err := c.boundedRead(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", ErrHTTP(fmt.Errorf("get_webstart_otp_v2.ashx returned HTTP %d", resp.StatusCode))
	}
	var env otpV2Response
	if err := json.Unmarshal(respBody, &env); err != nil {
		return "", ErrJSON(err)
	}
	if env.Result == nil {
		return "", ErrOTPServerRejected("missing result field")
	}
	if *env.Result != 1 {
		if env.Message != nil && *env.Message != "" {
			return "", ErrOTPServerRejected(*env.Message)
		}
		return "", ErrOTPServerRejected(fmt.Sprintf("result = %d", *env.Result))
	}
	if env.Data == nil || *env.Data == "" {
		return "", ErrOTPServerRejected("missing data field")
	}
	slog.Info("FetchOTP: get_webstart_otp_v2.ashx",
		"status", resp.StatusCode,
		"data_len", len(*env.Data))
	return *env.Data, nil
}
