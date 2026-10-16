package beanfun

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
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
	LoginBase  *url.URL // https://login.beanfun.com/
	PortalBase *url.URL // https://tw.beanfun.com/
	// NewloginBase is reached only by following the portal's Step-0
	// redirect, never dialled directly — no code builds URLs from it.
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
// One client (not two) is enough for our QR-only flow: step 6 of
// finalize discards the response, and step 7 reads `bfWebToken`
// from the jar after the redirect chain settles. See
// docs/beanfun-login-protocol.md.
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
//
// The HTTP client gets its own cloned Transport so per-client
// connection-pool churn (e.g. an httptest.Server closing in one
// goroutine) can't reach in-flight requests on another client.
// Without isolation, parallel tests sharing `http.DefaultTransport`
// surface as flaky `http: CloseIdleConnections called` errors when
// one test's server cleanup kicks pooled connections out from
// under another test mid-request.
func NewBeanfunClientWithEndpoints(endpoints Endpoints) (*BeanfunClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, ErrHTTP(fmt.Errorf("cookiejar.New: %w", err))
	}
	return &BeanfunClient{
		endpoints: endpoints,
		http: &http.Client{
			Timeout:   defaultTimeout,
			Jar:       jar,
			Transport: http.DefaultTransport.(*http.Transport).Clone(),
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
	if isIPBlockedResponse(resp) {
		return nil, ErrIPBlocked()
	}
	return body, nil
}

// blockIPPathMarker is the path tw.beanfun.com redirects a
// frequency-locked IP to; the notice answers HTTP 200, so the path is
// the only thing separating it from a normal landing.
const blockIPPathMarker = "BlockIPMessage"

// isIPBlockedResponse reports whether resp is the portal's IP-lock
// notice, matching the redirect path only because the page body has
// never been captured.
func isIPBlockedResponse(resp *http.Response) bool {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return false
	}
	return strings.Contains(resp.Request.URL.Path, blockIPPathMarker)
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

var echoTokenResultRE = regexp.MustCompile(`EchoTokenResult\(\s*\{\s*ResultCode\s*:\s*(-?\d+)`)

var mainAccountIDRE = regexp.MustCompile(`MainAccountID\s*:\s*"([^"]*)"`)

// Ping reads the logged-in verdict from echo_token.ashx's body, since the endpoint answers HTTP 200 either way.
func (c *BeanfunClient) Ping(ctx context.Context) error {
	code, _, err := c.echoToken(ctx)
	if err != nil {
		return err
	}
	if code == "0" {
		return ErrSessionExpired()
	}
	return nil
}

// echoToken returns echo_token.ashx's ResultCode ("1" logged in, "0" logged out) and MainAccountID.
func (c *BeanfunClient) echoToken(ctx context.Context) (code, mainID string, err error) {
	u, err := c.portalURL("beanfun_block/generic_handlers/echo_token.ashx")
	if err != nil {
		return "", "", err
	}
	q := u.Query()
	q.Set("webtoken", "1")
	u.RawQuery = q.Encode()

	req, err := c.newRequest(ctx, "GET", u)
	if err != nil {
		return "", "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", ErrHTTP(err)
	}
	body, err := c.boundedRead(resp)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode >= 400 {
		return "", "", ErrHTTP(fmt.Errorf("echo_token.ashx returned HTTP %d", resp.StatusCode))
	}
	m := echoTokenResultRE.FindSubmatch(body)
	if m == nil || (string(m[1]) != "1" && string(m[1]) != "0") {
		return "", "", ErrHTTP(fmt.Errorf("echo_token.ashx: unrecognised reply (len=%d)", len(body)))
	}
	if id := mainAccountIDRE.FindSubmatch(body); id != nil {
		mainID = string(id[1])
	}
	return string(m[1]), mainID, nil
}
