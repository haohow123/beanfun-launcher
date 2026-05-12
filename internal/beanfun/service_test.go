package beanfun

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// serviceTestMux composes init + poll + finalize stubs so that one
// httptest server can drive a full CheckQRLogin sequence end-to-end.
// pollResult is the ResultMessage to return; if "" the poll route is
// not mounted (forces test to call only StartQRLogin).
func serviceTestMux(pollResult string, setStep4Cookie bool) *http.ServeMux {
	mux := fullInitMux()
	if pollResult != "" {
		mux.HandleFunc("/QRLogin/CheckLoginStatus", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, pollResponseBody(pollResult))
		})
	}
	// Finalize routes — only matter when poll says "Success".
	mux.HandleFunc("/QRLogin/QRLogin", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/Login/SendLogin", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, happySendLoginBody())
	})
	var returnCalls int
	mux.HandleFunc("/beanfun_block/bflogin/return.aspx", func(w http.ResponseWriter, _ *http.Request) {
		returnCalls++
		if returnCalls == 1 {
			w.WriteHeader(http.StatusFound)
			return
		}
		if setStep4Cookie {
			http.SetCookie(w, &http.Cookie{Name: "bfWebToken", Value: "FULL_FLOW_TOKEN", Path: "/"})
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func TestLoginService_StartQRLogin(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(fullInitMux())
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv))

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

func TestLoginService_CheckQRLogin_NoActiveSession(t *testing.T) {
	t.Parallel()
	// No StartQRLogin → pendingQR is nil → expect Expired (a graceful
	// signal for the frontend to call StartQRLogin again).
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv))

	got, err := s.CheckQRLogin()
	if err != nil {
		t.Fatalf("CheckQRLogin: %v", err)
	}
	if got != QRStatusExpired {
		t.Errorf("got %q, want %q", got, QRStatusExpired)
	}
}

func TestLoginService_CheckQRLogin_PollPending(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(serviceTestMux("Wait Login", false))
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv))

	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}
	got, err := s.CheckQRLogin()
	if err != nil {
		t.Fatalf("CheckQRLogin: %v", err)
	}
	if got != QRStatusPending {
		t.Errorf("got %q, want %q", got, QRStatusPending)
	}
}

func TestLoginService_CheckQRLogin_PollExpiredClearsState(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(serviceTestMux("Token Expired", false))
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv))

	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}
	got, err := s.CheckQRLogin()
	if err != nil {
		t.Fatalf("CheckQRLogin: %v", err)
	}
	if got != QRStatusExpired {
		t.Errorf("got %q, want %q", got, QRStatusExpired)
	}
	// Subsequent CheckQRLogin without restart → still Expired (state cleared)
	got2, err := s.CheckQRLogin()
	if err != nil {
		t.Fatalf("CheckQRLogin #2: %v", err)
	}
	if got2 != QRStatusExpired {
		t.Errorf("after Expired, got %q, want %q (state should stay cleared)", got2, QRStatusExpired)
	}
}

func TestLoginService_CheckQRLogin_PollApprovedRunsFinalize(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(serviceTestMux("Success", true))
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv))

	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}
	got, err := s.CheckQRLogin()
	if err != nil {
		t.Fatalf("CheckQRLogin: %v", err)
	}
	if got != QRStatusApproved {
		t.Fatalf("got %q, want %q", got, QRStatusApproved)
	}
	// Session must be stashed and carry the token from step 4.
	s.mu.Lock()
	sess := s.session
	pending := s.pendingQR
	s.mu.Unlock()
	if sess == nil {
		t.Fatal("session not stashed after Approved")
	}
	if sess.WebToken != "FULL_FLOW_TOKEN" {
		t.Errorf("WebToken = %q, want FULL_FLOW_TOKEN", sess.WebToken)
	}
	if pending != nil {
		t.Error("pendingQR should be cleared after Approved")
	}
	// Stringer should redact secrets.
	if strings.Contains(sess.String(), "FULL_FLOW_TOKEN") {
		t.Error("Session.String() leaked WebToken")
	}
}

func TestLoginService_CheckQRLogin_PollServerError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(serviceTestMux("Mystery Status", false))
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv))

	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}
	_, err := s.CheckQRLogin()
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindServerMessage {
		t.Errorf("got %v, want KindServerMessage", err)
	}
}

func TestLoginService_StartQRLogin_ResetsState(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(serviceTestMux("Wait Login", false))
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv))

	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("first StartQRLogin: %v", err)
	}
	// Force the service into "session" state via a manual stash (no
	// real finalize wired). Then StartQRLogin should clear it.
	s.mu.Lock()
	s.session = &Session{WebToken: "stale"}
	s.mu.Unlock()
	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("second StartQRLogin: %v", err)
	}
	s.mu.Lock()
	sess := s.session
	s.mu.Unlock()
	if sess != nil {
		t.Errorf("session should be cleared after restart, got %v", sess)
	}
}
