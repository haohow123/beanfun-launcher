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
