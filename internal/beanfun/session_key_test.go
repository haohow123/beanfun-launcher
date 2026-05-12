package beanfun

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// stubEndpoints points the BeanfunClient at the given test server for
// all 3 base URLs. Sufficient for session_key + qr_init tests because
// the stubbed routes use distinct paths.
func stubEndpoints(t *testing.T, srv *httptest.Server) Endpoints {
	t.Helper()
	base, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parse srv URL: %v", err)
	}
	return Endpoints{LoginBase: base, PortalBase: base, NewloginBase: base}
}

func TestGetSessionKey_HappyPath(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/bflogin/default.aspx", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login/id-pass.aspx?service=999999_T0&pSKey=ABCDEF123", http.StatusFound)
	})
	mux.HandleFunc("/login/id-pass.aspx", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.getSessionKey(context.Background())
	if err != nil {
		t.Fatalf("getSessionKey: %v", err)
	}
	if got != "ABCDEF123" {
		t.Errorf("got %q, want ABCDEF123", got)
	}
}

func TestGetSessionKey_MissingKeyInRedirect(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/bflogin/default.aspx", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/somewhere-else?nope=true", http.StatusFound)
	})
	mux.HandleFunc("/somewhere-else", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}

	_, err = c.getSessionKey(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindMissingSessionKey {
		t.Errorf("got %v, want LoginError{Kind: KindMissingSessionKey}", err)
	}
}
