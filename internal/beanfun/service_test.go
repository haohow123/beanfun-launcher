package beanfun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haohow123/beanfun-launcher/internal/bgtask"
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
			w.WriteHeader(http.StatusOK)
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
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())

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
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())

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
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())

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
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())

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
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())

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
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())

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
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())

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

// withShortCooldown shrinks the mint cooldown so tests finish in
// milliseconds; the technique mirrors offlineConfirmDelay in
// internal/maple. Callers must not be parallel — qrMintCooldown is
// package state.
func withShortCooldown(t *testing.T, d time.Duration) {
	t.Helper()
	orig := qrMintCooldown
	qrMintCooldown = d
	t.Cleanup(func() { qrMintCooldown = orig })
}

func TestMintCooldown(t *testing.T) {
	withShortCooldown(t, 50*time.Millisecond)
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())

	if got := s.mintCooldownRemaining(); got != 0 {
		t.Fatalf("fresh service: remaining = %v, want 0", got)
	}

	s.armCooldownIfBlocked(ErrIPBlocked())
	if got := s.mintCooldownRemaining(); got <= 0 {
		t.Fatalf("after arming: remaining = %v, want > 0", got)
	}
	time.Sleep(60 * time.Millisecond)
	if got := s.mintCooldownRemaining(); got != 0 {
		t.Errorf("after the window: remaining = %v, want 0", got)
	}
}

// TestArmCooldownIgnoresOtherErrors is the guard against locking the
// button on every ordinary failure.
func TestArmCooldownIgnoresOtherErrors(t *testing.T) {
	withShortCooldown(t, time.Minute)
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())

	for _, err := range []error{
		ErrMissingSessionKey(),
		ErrLoginRequired(),
		ErrSessionExpired(),
		errors.New("some transport blip"),
		nil,
	} {
		s.armCooldownIfBlocked(err)
		if got := s.mintCooldownRemaining(); got != 0 {
			t.Fatalf("%v armed the cooldown (remaining %v), want untouched", err, got)
		}
	}
}

func TestStartQRLoginInCooldownMakesNoRequest(t *testing.T) {
	withShortCooldown(t, time.Minute)

	var requests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())

	s.armCooldownIfBlocked(ErrIPBlocked())

	_, err := s.StartQRLogin()
	// Errorf, not Fatalf: the request-count assertion below is the one
	// this test exists for, and a Fatalf here would skip it.
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindIPBlocked {
		t.Errorf("got %v, want KindIPBlocked", err)
	}
	// The point of this test: refusing locally is worthless if the
	// request still went out.
	if got := requests.Load(); got != 0 {
		t.Errorf("StartQRLogin issued %d request(s) while in cooldown, want 0", got)
	}
}

// TestCheckQRLoginInCooldownMakesNoRequest covers the poll path, which is
// the likelier way to trip the lock than the button: the frontend polls
// every 2s and refetchInterval does not stop on error, so an unguarded
// poll re-arms the cooldown forever and the block never clears.
func TestCheckQRLoginInCooldownMakesNoRequest(t *testing.T) {
	withShortCooldown(t, time.Minute)

	var pollRequests atomic.Int32
	mux := fullInitMux()
	mux.HandleFunc("/QRLogin/CheckLoginStatus", func(w http.ResponseWriter, r *http.Request) {
		pollRequests.Add(1)
		http.Redirect(w, r, "/TW/BlockIPMessage.htm", http.StatusFound)
	})
	mux.HandleFunc("/TW/BlockIPMessage.htm", func(w http.ResponseWriter, r *http.Request) {
		writeHTML(w, "<html>locked</html>")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())

	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}

	// First poll discovers the block and arms the cooldown.
	if _, err := s.CheckQRLogin(); err == nil {
		t.Fatal("first poll: expected the block error")
	}
	if armed := s.mintCooldownRemaining(); armed <= 0 {
		t.Fatalf("cooldown not armed after the first blocked poll (got %v)", armed)
	}

	for i := 0; i < 3; i++ {
		_, err := s.CheckQRLogin()
		var le *LoginError
		if !errors.As(err, &le) || le.Kind != KindIPBlocked {
			t.Errorf("poll %d: got %v, want KindIPBlocked", i+2, err)
		}
	}

	if got := pollRequests.Load(); got != 1 {
		t.Errorf("poll endpoint hit %d times, want 1 — later polls must be refused locally", got)
	}
}

// TestKeepAliveTick covers the delay decision for all three response
// shapes. The cooldown column is the point: true for a block and false
// for a 500 is the difference between the block being noticed and the
// block informing anything.
func TestKeepAliveTick(t *testing.T) {
	const echoPath = "/beanfun_block/generic_handlers/echo_token.ashx"
	tests := []struct {
		name         string
		install      func(*http.ServeMux)
		wantDelay    time.Duration
		wantLog      string
		wantNoLog    string
		wantCooldown bool
	}{
		{
			name: "ping ok",
			install: func(mux *http.ServeMux) {
				mux.HandleFunc(echoPath, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				})
			},
			wantDelay: keepAliveIntervalOK,
			wantLog:   "keep-alive: ping ok",
		},
		{
			name: "server error",
			install: func(mux *http.ServeMux) {
				mux.HandleFunc(echoPath, func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				})
			},
			wantDelay: keepAliveIntervalFail,
			wantLog:   "keep-alive: ping failed",
			wantNoLog: "keep-alive: ping ok",
		},
		{
			name: "ip blocked",
			install: func(mux *http.ServeMux) {
				mux.HandleFunc(echoPath, func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, "/TW/BlockIPMessage.htm", http.StatusFound)
				})
				mux.HandleFunc("/TW/BlockIPMessage.htm", func(w http.ResponseWriter, r *http.Request) {
					writeHTML(w, "<html>locked</html>")
				})
			},
			wantDelay:    keepAliveIntervalFail,
			wantLog:      "ip blocked, session was not refreshed",
			wantNoLog:    "keep-alive: ping ok",
			wantCooldown: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withShortCooldown(t, time.Minute)
			logs := captureLogs(t)

			mux := http.NewServeMux()
			tt.install(mux)
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)

			endpoints := stubEndpoints(t, srv)
			c, err := NewBeanfunClientWithEndpoints(endpoints)
			if err != nil {
				t.Fatal(err)
			}
			s := NewLoginServiceWithEndpoints(endpoints, bgtask.New())

			got := s.keepAliveTick(context.Background(), c)

			if got != tt.wantDelay {
				t.Errorf("delay = %v, want %v", got, tt.wantDelay)
			}
			out := logs.String()
			if !strings.Contains(out, tt.wantLog) {
				t.Errorf("log missing %q:\n%s", tt.wantLog, out)
			}
			if tt.wantNoLog != "" && strings.Contains(out, tt.wantNoLog) {
				t.Errorf("log should not contain %q:\n%s", tt.wantNoLog, out)
			}
			if armed := s.mintCooldownRemaining() > 0; armed != tt.wantCooldown {
				t.Errorf("cooldown armed = %v, want %v", armed, tt.wantCooldown)
			}
		})
	}
}
