package beanfun

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// pppppLiteral is a 64-char uppercase hex constant the Beanfun OTP
// endpoint validates as a protocol literal. Provenance unknown; the
// server treats it as opaque required input — do not modify without
// empirical verification against the production server. See
// docs/beanfun-login-protocol.md § 9.
const pppppLiteral = "1F552AEAFF976018F942B13690C990F60ED01510DDF89165F1658CCE7BC21DBA"

// OTPResult is the decrypted one-time game-launch credential. Token
// is held as []byte (not string) so the launcher can Zero it after
// the spawn call consumes it.
type OTPResult struct {
	Token []byte
}

// otpStep1 captures the three values step 1 extracts from
// game_start_step2.aspx that downstream steps need.
type otpStep1 struct {
	longPollingKey string
	unkDataKey     string // TW only — extra form field for step 3
	unkDataValue   string
	createTime     string // YYYY-MM-DD HH:MM:SS; passed through to step 3 + step 5
}

// FetchOTP runs the 6-step OTP flow (game_start_step2 → get_cookies
// → record_service_start → get_result long-poll → get_webstart_otp →
// DES-ECB decrypt) and returns the decrypted token. The session's
// bfWebToken cookie must already be in the client's jar. See
// docs/beanfun-login-protocol.md § 9.
func (c *BeanfunClient) FetchOTP(ctx context.Context, sess *Session, acc Account) (OTPResult, error) {
	if sess == nil {
		return OTPResult{}, ErrLoginRequired()
	}

	step1, err := c.otpStep1(ctx, sess, acc)
	if err != nil {
		return OTPResult{}, err
	}
	secretCode, err := c.otpStep2(ctx)
	if err != nil {
		return OTPResult{}, err
	}
	if err := c.otpStep3(ctx, sess, acc, step1); err != nil {
		return OTPResult{}, err
	}
	if err := c.otpStep4(ctx, step1.longPollingKey); err != nil {
		return OTPResult{}, err
	}
	envelope, err := c.otpStep5(ctx, sess, acc, step1, secretCode)
	if err != nil {
		return OTPResult{}, err
	}
	token, err := decryptOTP(envelope)
	if err != nil {
		return OTPResult{}, err
	}
	slog.Info("FetchOTP: token acquired", "len", len(token), "sid", acc.SID)
	return OTPResult{Token: token}, nil
}

// otpStep1: GET game_start_step2.aspx; scrape the long-polling key,
// the TW unk_data pair, and the createTime fallback.
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
	key := extractLongPollingKey(bodyStr)
	if key == "" {
		return otpStep1{}, ErrOTPInit("missing GetResultByLongPolling key in game_start_step2.aspx" + withBody(bodyStr))
	}
	uk, uv, ok := extractUnkData(bodyStr)
	if !ok {
		return otpStep1{}, ErrOTPInit("missing MyAccountData unk_data literal" + withBody(bodyStr))
	}
	createTime := extractCreateTimeFallback(bodyStr)
	if createTime == "" {
		return otpStep1{}, ErrOTPInit("missing ServiceAccountCreateTime" + withBody(bodyStr))
	}
	slog.Info("FetchOTP step 1: game_start_step2.aspx",
		"long_polling_key_len", len(key),
		"create_time", createTime)
	return otpStep1{
		longPollingKey: key,
		unkDataKey:     uk,
		unkDataValue:   uv,
		createTime:     createTime,
	}, nil
}

// otpStep2: GET get_cookies.ashx on the newlogin host; scrape
// m_strSecretCode.
func (c *BeanfunClient) otpStep2(ctx context.Context) (string, error) {
	u, err := c.newloginURL("generic_handlers/get_cookies.ashx")
	if err != nil {
		return "", err
	}
	req, err := c.newRequest(ctx, "GET", u)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", ErrHTTP(err)
	}
	body, err := c.boundedRead(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", ErrHTTP(fmt.Errorf("get_cookies.ashx returned HTTP %d", resp.StatusCode))
	}
	bodyStr := string(body)
	code := extractSecretCode(bodyStr)
	if code == "" {
		return "", ErrOTPInit("missing m_strSecretCode in get_cookies.ashx" + withBody(bodyStr))
	}
	slog.Info("FetchOTP step 2: get_cookies.ashx", "secret_code_len", len(code))
	return code, nil
}

// otpStep3: POST record_service_start.ashx with the per-account form
// payload. Response body is discarded; the call exists to prime
// server-side state for step 5.
func (c *BeanfunClient) otpStep3(ctx context.Context, sess *Session, acc Account, s1 otpStep1) error {
	u, err := c.portalURL("beanfun_block/generic_handlers/record_service_start.ashx")
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("service_code", sess.ServiceCode)
	form.Set("service_region", sess.ServiceRegion)
	form.Set("service_account_id", acc.SID)
	form.Set("sotp", acc.SSN)
	form.Set("service_account_display_name", acc.SName)
	form.Set("service_account_create_time", s1.createTime)
	form.Set(s1.unkDataKey, s1.unkDataValue)
	body := form.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), strings.NewReader(body))
	if err != nil {
		return ErrHTTP(fmt.Errorf("NewRequestWithContext: %w", err))
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return ErrHTTP(err)
	}
	if _, err := c.boundedRead(resp); err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return ErrHTTP(fmt.Errorf("record_service_start.ashx returned HTTP %d", resp.StatusCode))
	}
	slog.Info("FetchOTP step 3: record_service_start.ashx", "status", resp.StatusCode)
	return nil
}

// otpStep4: GET get_result.ashx long-poll trigger. Body discarded.
func (c *BeanfunClient) otpStep4(ctx context.Context, longPollingKey string) error {
	u, err := c.portalURL("generic_handlers/get_result.ashx")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("meth", "GetResultByLongPolling")
	q.Set("key", longPollingKey)
	q.Set("_", time.Now().UTC().Format(time.RFC3339))
	u.RawQuery = q.Encode()

	req, err := c.newRequest(ctx, "GET", u)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrHTTP(err)
	}
	if _, err := c.boundedRead(resp); err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return ErrHTTP(fmt.Errorf("get_result.ashx returned HTTP %d", resp.StatusCode))
	}
	slog.Info("FetchOTP step 4: get_result.ashx long-poll", "status", resp.StatusCode)
	return nil
}

// otpStep5: GET get_webstart_otp.ashx → return the literal envelope.
//
// The URL is built as a single format string (not via url.Values)
// because two query parameters need encoding the standard form-
// urlencoder gets wrong:
//   - CreateTime contains a literal space that must become %20 (not
//     `+`).
//   - ppppp is a 64-char uppercase-hex literal that must appear
//     verbatim — no encoding or case folding.
//
// Every other value in the template is URL-safe (cookies, sids, hex
// digits), so a literal format string suffices.
func (c *BeanfunClient) otpStep5(
	ctx context.Context,
	sess *Session,
	acc Account,
	s1 otpStep1,
	secretCode string,
) (string, error) {
	base, err := c.portalURL("beanfun_block/generic_handlers/get_webstart_otp.ashx")
	if err != nil {
		return "", err
	}
	createTimeEncoded := strings.ReplaceAll(s1.createTime, " ", "%20")
	fullURL := fmt.Sprintf(
		"%s?SN=%s&WebToken=%s&SecretCode=%s&ppppp=%s&ServiceCode=%s&ServiceRegion=%s&ServiceAccount=%s&CreateTime=%s&d=%d",
		base.String(),
		s1.longPollingKey,
		sess.WebToken,
		secretCode,
		pppppLiteral,
		sess.ServiceCode,
		sess.ServiceRegion,
		acc.SID,
		createTimeEncoded,
		time.Now().UnixMilli(),
	)
	parsed, err := url.Parse(fullURL)
	if err != nil {
		return "", ErrHTTP(fmt.Errorf("parse step-5 url: %w", err))
	}
	req, err := c.newRequest(ctx, "GET", parsed)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", ErrHTTP(err)
	}
	body, err := c.boundedRead(resp)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", ErrHTTP(fmt.Errorf("get_webstart_otp.ashx returned HTTP %d", resp.StatusCode))
	}
	envelope := strings.TrimSpace(string(body))
	slog.Info("FetchOTP step 5: get_webstart_otp.ashx",
		"status", resp.StatusCode,
		"envelope_len", len(envelope))
	return envelope, nil
}
