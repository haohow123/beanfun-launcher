package beanfun

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

const (
	// Chrome 130 UA — keeps us aligned with the browser shape Beanfun's
	// portal expects. Some endpoints fingerprint the UA shape.
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"
	defaultTimeout   = 30 * time.Second
	// 16 MiB cap on any response body. Defends the process against a
	// hostile server streaming forever.
	maxResponseBodyBytes int64 = 16 << 20
)

// Endpoints holds the base URLs the Beanfun login flow touches. Tests
// swap these for an httptest.Server via NewBeanfunClientWithEndpoints.
type Endpoints struct {
	LoginBase    *url.URL // https://login.beanfun.com/
	PortalBase   *url.URL // https://tw.beanfun.com/
	NewloginBase *url.URL // https://tw.newlogin.beanfun.com/
}

// DefaultEndpoints returns the production TW endpoints.
func DefaultEndpoints() Endpoints {
	must := func(s string) *url.URL {
		u, err := url.Parse(s)
		if err != nil {
			panic(fmt.Sprintf("invalid default endpoint %q: %v", s, err))
		}
		return u
	}
	return Endpoints{
		LoginBase:    must("https://login.beanfun.com/"),
		PortalBase:   must("https://tw.beanfun.com/"),
		NewloginBase: must("https://tw.newlogin.beanfun.com/"),
	}
}

// BeanfunClient wraps a redirect-following http.Client plus a cookie
// jar for a single Beanfun login session. A new instance starts a
// fresh jar — no leftover cookies from a prior attempt.
//
// Pungin/Beanfun maintains a second no-redirect client variant for
// flows that need to read `Set-Cookie` directly from a 302 response.
// Our QR-only flow doesn't: step 6 of finalize discards the response,
// and step 7 reads `bfWebToken` from the jar after the redirect chain
// settles. So one client is enough. See docs/beanfun-login-protocol.md.
type BeanfunClient struct {
	endpoints Endpoints
	http      *http.Client
	jar       *cookiejar.Jar
	userAgent string
}

// NewBeanfunClient builds a client pointed at production TW endpoints.
func NewBeanfunClient() (*BeanfunClient, error) {
	return NewBeanfunClientWithEndpoints(DefaultEndpoints())
}

// NewBeanfunClientWithEndpoints builds a client with caller-provided
// endpoints. Tests use this to point at httptest.NewServer.
func NewBeanfunClientWithEndpoints(endpoints Endpoints) (*BeanfunClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, ErrHTTP(fmt.Errorf("cookiejar.New: %w", err))
	}
	return &BeanfunClient{
		endpoints: endpoints,
		http: &http.Client{
			Timeout: defaultTimeout,
			Jar:     jar,
		},
		jar:       jar,
		userAgent: defaultUserAgent,
	}, nil
}

// newRequest is the centralised request constructor. It injects the
// User-Agent header; other headers are set per call site since they
// vary by endpoint.
func (c *BeanfunClient) newRequest(ctx context.Context, method string, u *url.URL) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, ErrHTTP(fmt.Errorf("NewRequestWithContext: %w", err))
	}
	req.Header.Set("User-Agent", c.userAgent)
	return req, nil
}

// boundedRead reads at most maxResponseBodyBytes from resp.Body, then
// closes the body. Returns ErrBodyTooLarge if the limit was hit.
func (c *BeanfunClient) boundedRead(resp *http.Response) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()
	lr := io.LimitReader(resp.Body, maxResponseBodyBytes+1)
	body, err := io.ReadAll(lr)
	if err != nil {
		return nil, ErrHTTP(fmt.Errorf("read body: %w", err))
	}
	if int64(len(body)) > maxResponseBodyBytes {
		return nil, ErrBodyTooLarge(maxResponseBodyBytes)
	}
	return body, nil
}

// loginURL joins path onto LoginBase.
func (c *BeanfunClient) loginURL(path string) (*url.URL, error) {
	u, err := c.endpoints.LoginBase.Parse(path)
	if err != nil {
		return nil, ErrHTTP(fmt.Errorf("loginURL.Parse(%q): %w", path, err))
	}
	return u, nil
}

// loginURLWithSKey is loginURL plus a pSKey query parameter.
func (c *BeanfunClient) loginURLWithSKey(path, skey string) (*url.URL, error) {
	u, err := c.loginURL(path)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("pSKey", skey)
	u.RawQuery = q.Encode()
	return u, nil
}

// portalURL joins path onto PortalBase.
func (c *BeanfunClient) portalURL(path string) (*url.URL, error) {
	u, err := c.endpoints.PortalBase.Parse(path)
	if err != nil {
		return nil, ErrHTTP(fmt.Errorf("portalURL.Parse(%q): %w", path, err))
	}
	return u, nil
}

// newloginURL joins path onto NewloginBase. Used by the OTP flow's
// step 2 (TW's get_cookies.ashx lives on the newlogin host, not the
// regular login host).
func (c *BeanfunClient) newloginURL(path string) (*url.URL, error) {
	u, err := c.endpoints.NewloginBase.Parse(path)
	if err != nil {
		return nil, ErrHTTP(fmt.Errorf("newloginURL.Parse(%q): %w", path, err))
	}
	return u, nil
}
