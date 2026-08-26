package beanfun

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// LoginErrorKind enumerates the classes of failure surfaced by the
// Beanfun login flow. The frontend can switch on Kind to render
// localised strings without parsing message text.
type LoginErrorKind int

const (
	KindUnknown LoginErrorKind = iota
	KindHTTP
	KindJSON
	KindBodyTooLarge
	KindMissingSessionKey
	KindQRInitResult
	KindServerMessage
	KindSendLoginNoFormData
	KindMissingWebToken
	KindLoginRequired
	KindOTPInit
	KindOTPServerRejected
	KindOTPDecrypt
	KindSessionExpired
	// Append new kinds here only; inserting renumbers the values the
	// frontend receives.
	KindLaunchDataDecode
	KindIPBlocked
)

// LoginError is the typed error returned by every Beanfun login step.
// Inspect via `errors.As(err, &beanfun.LoginError{})`.
type LoginError struct {
	Kind  LoginErrorKind
	Msg   string
	Cause error
}

func (e *LoginError) Error() string {
	if e.Cause != nil {
		return scrubCredentialParams(fmt.Sprintf("beanfun: %s: %v", e.Msg, e.Cause))
	}
	return scrubCredentialParams(fmt.Sprintf("beanfun: %s", e.Msg))
}

func (e *LoginError) Unwrap() error { return e.Cause }

func ErrHTTP(cause error) *LoginError {
	return &LoginError{Kind: KindHTTP, Msg: "http transport error", Cause: redactURLError(cause)}
}

// redactURLError strips the query string from a transport error, because Go stores the full request URL — session key included — in url.Error and renders it in the message.
func redactURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		ue.URL = redactedURL(ue.URL)
	}
	return err
}

// credentialParamRE matches a credential-bearing query parameter and its
// value. Deliberately wider and more case-tolerant than sessionKeyRE in
// parser.go: an extractor that misses finds no key, but a scrubber that
// misses ships one to the log file.
var credentialParamRE = sync.OnceValue(func() *regexp.Regexp {
	return regexp.MustCompile(`(?i)((?:[sp][sp]?key|web_?token)=)[^&"\s]+`)
})

// ScrubCredentials applies the same backstop to a message produced
// outside this package — errors from dependencies reach launcher.log
// through internal/diag and nothing else scrubs them.
func ScrubCredentials(msg string) string {
	return scrubCredentialParams(msg)
}

// scrubCredentialParams is the render-time backstop for redactURLError, which cannot reach a message fmt.Errorf already snapshotted.
func scrubCredentialParams(msg string) string {
	return credentialParamRE().ReplaceAllString(msg, "${1}<redacted>")
}

func ErrJSON(cause error) *LoginError {
	return &LoginError{Kind: KindJSON, Msg: "JSON decode failed", Cause: cause}
}

func ErrBodyTooLarge(limitBytes int64) *LoginError {
	return &LoginError{Kind: KindBodyTooLarge, Msg: fmt.Sprintf("response body exceeded %d bytes", limitBytes)}
}

func ErrMissingSessionKey() *LoginError {
	return &LoginError{Kind: KindMissingSessionKey, Msg: "session key not found in redirect URL"}
}

func ErrQRInitResult(msg string) *LoginError {
	return &LoginError{Kind: KindQRInitResult, Msg: "QR init failed: " + msg}
}

// ErrServerMessage is returned when CheckLoginStatus replies with an
// unrecognised ResultMessage (or none). Body preview included for
// operator diagnostics.
func ErrServerMessage(rawBody string) *LoginError {
	return &LoginError{Kind: KindServerMessage, Msg: "unexpected server message: " + truncate(rawBody, 200)}
}

// ErrSendLoginNoFormData is returned when Login/SendLogin's HTML body
// has zero usable hidden inputs (typical when an anti-bot interstitial
// or error page was served instead).
func ErrSendLoginNoFormData() *LoginError {
	return &LoginError{Kind: KindSendLoginNoFormData, Msg: "Login/SendLogin returned no form data"}
}

// ErrMissingWebToken is returned when the canonical bfWebToken cookie
// is absent from the jar after the finalize step 4 redirect chain
// settles. Fatal — login cannot complete without it.
func ErrMissingWebToken() *LoginError {
	return &LoginError{Kind: KindMissingWebToken, Msg: "bfWebToken cookie missing after finalize"}
}

// ErrLoginRequired is returned by post-login methods (e.g. GetAccounts)
// when no session is active. The frontend's expected response is to
// route back to the login page.
func ErrLoginRequired() *LoginError {
	return &LoginError{Kind: KindLoginRequired, Msg: "login required: no active session"}
}

// ErrOTPInit covers the OTP flow's page fetch — a missing or unparseable m_objData handoff literal.
// cause may be nil; when it is not, Error() renders it and Unwrap() exposes it to errors.Is/As, so
// the root failure survives instead of being flattened into prose.
func ErrOTPInit(msg string, cause error) *LoginError {
	return &LoginError{Kind: KindOTPInit, Msg: "OTP init: " + msg, Cause: cause}
}

// ErrOTPServerRejected is returned when get_webstart_otp_v2.ashx
// answers with a result other than 1, or omits a required field. The
// server's own message is included verbatim for diagnostics.
func ErrOTPServerRejected(rawPayload string) *LoginError {
	return &LoginError{Kind: KindOTPServerRejected, Msg: "OTP server rejected: " + truncate(rawPayload, 200)}
}

// ErrOTPDecrypt covers all DES-ECB decryption failures: short payload,
// bad hex, non-block-aligned ciphertext, DES init failure.
func ErrOTPDecrypt(msg string) *LoginError {
	return &LoginError{Kind: KindOTPDecrypt, Msg: "OTP decrypt: " + msg}
}

// ErrSessionExpired is returned when the Beanfun portal responds to
// an authenticated request with its "尚未登入，請重新登入" notice
// (literally: "not logged in, please re-login"). The callers reset
// local state and route the user back to QR login.
func ErrSessionExpired() *LoginError {
	return &LoginError{Kind: KindSessionExpired, Msg: "beanfun session expired (尚未登入)"}
}

// ErrLaunchDataDecode is returned when the launch handoff blob cannot
// be decoded. The reason must describe structure only — the blob
// carries a live LaunchTicket.
func ErrLaunchDataDecode(reason string) *LoginError {
	return &LoginError{Kind: KindLaunchDataDecode, Msg: "launch data decode failed: " + reason}
}

// ErrIPBlocked is returned when tw.beanfun.com answers with its
// IP-frequency-lock notice; the frontend matches on "ip temporarily
// blocked" to render the Chinese message (frontend/src/lib/errors.ts).
func ErrIPBlocked() *LoginError {
	return &LoginError{Kind: KindIPBlocked, Msg: "ip temporarily blocked by beanfun; retry in a few minutes"}
}

// truncate returns up to n bytes of s with a "…" marker if it was
// shortened. Used for body previews in diagnostic logs and error
// messages — small enough to fit on one terminal line, big enough to
// identify the page.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// bodyPreviewLimit caps how much of a server response is appended
// to a parse-failure error message. Generous enough to identify the
// page type (login redirect / throttle notice / actual payload) but
// tight enough to keep launcher.log lines readable.
const bodyPreviewLimit = 500

// withBody appends a truncated response-body preview to an error
// message. Use at every parse-failure site so the log captures what
// the server actually returned — saves us from re-instrumenting each
// time a new failure mode surfaces.
//
//	return ErrOTPInit("missing key" + withBody(bodyStr))
//
// Empty body strings produce an empty suffix so callers don't have
// to guard. Bytes slices can be passed via withBodyBytes.
func withBody(body string) string {
	if body == "" {
		return ""
	}
	return " :: body=" + truncate(body, bodyPreviewLimit)
}

// handoffMissDetail describes a failed launch-blob extraction for the log. The body preview is
// attached only when the page carried no m_objData at all — every other reason means the blob was
// there, and a preview could reproduce part of it.
func handoffMissDetail(miss handoffMiss, body string) string {
	detail := fmt.Sprintf("m_objData %s in game_start_step2.aspx (%s)", miss, describeBody(body))
	if miss == handoffMissAbsent {
		return detail + withBody(body)
	}
	return detail
}

// withBodyBytes is the []byte variant of withBody.
func withBodyBytes(body []byte) string {
	return withBody(string(body))
}

// isSessionExpiredBody reports whether body looks like Beanfun's
// "session timed out" notice — the same Messge Page (sic) the portal
// returns from authenticated endpoints when bfWebToken is no longer
// valid. Captured from a real launcher.log; the marker string is the
// divMsg content rather than the title because the title is
// suspiciously misspelled and could change.
func isSessionExpiredBody(body string) bool {
	return strings.Contains(body, sessionExpiredMarker)
}

const sessionExpiredMarker = "尚未登入"

// bodyMarkers name the response shapes we have actually seen, so a log
// line can identify a page without reproducing any of its text.
var bodyMarkers = []struct {
	needle string
	name   string
}{
	{sessionExpiredMarker, "session-expired"},
	{"BlockIPMessage", "ip-blocked"},
}

func describeBody(body string) string {
	var names []string
	for _, m := range bodyMarkers {
		if strings.Contains(body, m.needle) {
			names = append(names, m.name)
		}
	}
	if len(names) == 0 {
		return fmt.Sprintf("len=%d markers=none", len(body))
	}
	return fmt.Sprintf("len=%d markers=%s", len(body), strings.Join(names, ","))
}
