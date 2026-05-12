package beanfun

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// fullInitMux stubs the 3 routes needed for a complete StartQRLogin:
// portal default.aspx → redirects to a URL carrying pSKey, plus the
// two login.beanfun.com endpoints.
func fullInitMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/bflogin/default.aspx", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login/redirect-target?pSKey=TEST_SKEY", http.StatusFound)
	})
	mux.HandleFunc("/login/redirect-target", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/Login/Index", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(w, happyIndexBody("TKN"))
	})
	mux.HandleFunc("/Login/InitLogin", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, happyInitBody("https://app.example/auth"))
	})
	return mux
}

func TestLoginService_StartQRLogin(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(fullInitMux())
	t.Cleanup(srv.Close)
	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	s := NewLoginServiceWithClient(c)

	got, err := s.StartQRLogin()
	if err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}
	if got.BitmapBase64 != sampleQRBase64 {
		t.Errorf("BitmapBase64 mismatch")
	}
	if got.Deeplink != "https://app.example/auth" {
		t.Errorf("Deeplink = %q", got.Deeplink)
	}
}

func TestLoginService_CheckQRLogin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		// call is the 1-based index of CheckQRLogin invocations.
		call int
		want QRStatus
	}{
		{name: "first poll is pending", call: 1, want: QRStatusPending},
		{name: "second poll is pending", call: 2, want: QRStatusPending},
		{name: "third poll flips to approved", call: 3, want: QRStatusApproved},
		{name: "stays approved after threshold", call: 4, want: QRStatusApproved},
	}

	// CheckQRLogin doesn't touch pendingQR in Day 3, so we can skip
	// StartQRLogin. The httptest server is a placeholder.
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	s := NewLoginServiceWithClient(c)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.CheckQRLogin()
			if err != nil {
				t.Fatalf("CheckQRLogin: %v", err)
			}
			if got != tt.want {
				t.Errorf("call %d: got %q, want %q", tt.call, got, tt.want)
			}
		})
	}
}

func TestLoginService_StartQRLogin_ResetsPolls(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(fullInitMux())
	t.Cleanup(srv.Close)
	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	s := NewLoginServiceWithClient(c)

	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("first StartQRLogin: %v", err)
	}
	for range 5 {
		_, _ = s.CheckQRLogin()
	}
	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("second StartQRLogin: %v", err)
	}

	got, err := s.CheckQRLogin()
	if err != nil {
		t.Fatal(err)
	}
	if got != QRStatusPending {
		t.Errorf("expected restart to reset to pending, got %q", got)
	}
}
