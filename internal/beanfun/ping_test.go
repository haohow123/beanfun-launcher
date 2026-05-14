package beanfun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestBeanfunClient_Ping(t *testing.T) {
	// Smoke: Ping hits portal echo_token.ashx with webtoken=1.
	// Body is discarded; success is HTTP 2xx. WPF parity per
	// bfClient.cs L193-212.
	t.Parallel()
	var hits int32
	var sawQuery string
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/generic_handlers/echo_token.ashx", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		sawQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("Ping err = %v, want nil", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("hits = %d, want 1", got)
	}
	if sawQuery != "webtoken=1" {
		t.Errorf("RawQuery = %q, want webtoken=1", sawQuery)
	}
}

func TestBeanfunClient_Ping_HTTPError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/generic_handlers/echo_token.ashx", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(context.Background()); err == nil {
		t.Error("Ping err = nil, want HTTP error")
	}
}
