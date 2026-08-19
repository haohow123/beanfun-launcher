package beanfun

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// blockedMux answers every path with a redirect to the IP-lock notice,
// which itself returns 200 — the shape that made this failure invisible.
func blockedMux(t *testing.T) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/TW/BlockIPMessage.htm", func(w http.ResponseWriter, r *http.Request) {
		writeHTML(w, "<html>IP has been locked by the system</html>")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/TW/BlockIPMessage.htm", http.StatusFound)
	})
	return mux
}

func TestIPBlockDetectedAtEveryEntryPoint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call func(*BeanfunClient) error
	}{
		{"getSessionKey", func(c *BeanfunClient) error {
			_, err := c.getSessionKey(context.Background())
			return err
		}},
		{"otpStep1", func(c *BeanfunClient) error {
			_, err := c.otpStep1(context.Background(), &Session{}, Account{SID: "T9000011112222333344"})
			return err
		}},
		{"GetAccounts", func(c *BeanfunClient) error {
			_, err := c.GetAccounts(context.Background(), &Session{})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newTestClient(t, blockedMux(t))
			err := tt.call(c)
			var le *LoginError
			if !errors.As(err, &le) || le.Kind != KindIPBlocked {
				t.Fatalf("got %v, want KindIPBlocked", err)
			}
		})
	}
}

func TestIsIPBlockedResponse(t *testing.T) {
	t.Parallel()
	respWithPath := func(p string) *http.Response {
		u, err := url.Parse("https://tw.beanfun.com" + p)
		if err != nil {
			t.Fatalf("parse %q: %v", p, err)
		}
		return &http.Response{Request: &http.Request{URL: u}}
	}
	tests := []struct {
		name string
		resp *http.Response
		want bool
	}{
		{"block notice", respWithPath("/TW/BlockIPMessage.htm"), true},
		{"normal landing", respWithPath("/beanfun_block/bflogin/default.aspx"), false},
		{"newlogin landing", respWithPath("/login/id-pass.aspx"), false},
		{"nil response", nil, false},
		{"nil request", &http.Response{}, false},
		{"nil url", &http.Response{Request: &http.Request{}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isIPBlockedResponse(tt.resp); got != tt.want {
				t.Errorf("isIPBlockedResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBoundedRead_NormalBodyUnaffected is what stops the detection from
// being over-broad: a predicate returning true for everything would
// still satisfy TestIPBlockDetectedAtEveryEntryPoint.
func TestBoundedRead_NormalBodyUnaffected(t *testing.T) {
	t.Parallel()
	const want = "<html>ordinary page</html>"
	mux := http.NewServeMux()
	mux.HandleFunc("/plain", func(w http.ResponseWriter, r *http.Request) {
		writeHTML(w, want)
	})
	c, srv := newTestClient(t, mux)

	resp, err := c.http.Get(srv.URL + "/plain")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body, err := c.boundedRead(resp)
	if err != nil {
		t.Fatalf("boundedRead: %v", err)
	}
	if string(body) != want {
		t.Errorf("body = %q, want %q", string(body), want)
	}
}
