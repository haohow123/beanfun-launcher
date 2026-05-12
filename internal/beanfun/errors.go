package beanfun

import "fmt"

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
		return fmt.Sprintf("beanfun: %s: %v", e.Msg, e.Cause)
	}
	return fmt.Sprintf("beanfun: %s", e.Msg)
}

func (e *LoginError) Unwrap() error { return e.Cause }

func ErrHTTP(cause error) *LoginError {
	return &LoginError{Kind: KindHTTP, Msg: "http transport error", Cause: cause}
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
