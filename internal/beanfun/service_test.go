package beanfun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// pollSequenceMux serves one ResultMessage per poll call so a running loop can advance through
// states; the last entry repeats. serviceTestMux serves a single fixed result and cannot express a
// transition.
func pollSequenceMux(results []string, setStep4Cookie bool) (*http.ServeMux, *atomic.Int32) {
	var calls atomic.Int32
	mux := serviceTestMux("", setStep4Cookie)
	mux.HandleFunc("/QRLogin/CheckLoginStatus", func(w http.ResponseWriter, _ *http.Request) {
		i := int(calls.Add(1)) - 1
		if i >= len(results) {
			i = len(results) - 1
		}
		writeJSON(w, pollResponseBody(results[i]))
	})
	return mux, &calls
}

// newTickFixture builds a service plus a client and pendingQR without going through StartQRLogin, so
// no heartbeat is registered and a test can drive qrPollTick with no concurrent tick racing it.
func newTickFixture(t *testing.T, srv *httptest.Server) (*LoginService, *BeanfunClient, *qrLoginInit) {
	t.Helper()
	eps := stubEndpoints(t, srv)
	s := NewLoginServiceWithEndpoints(eps, bgtask.New())
	client, err := NewBeanfunClientWithEndpoints(eps)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	init, err := client.initQRLogin(context.Background(), "SK")
	if err != nil {
		t.Fatalf("initQRLogin: %v", err)
	}
	return s, client, init
}

// stopLoopAndCapture halts the heartbeat StartQRLogin registered and returns what it captured, so a
// test can drive the tick itself.
func stopLoopAndCapture(t *testing.T, s *LoginService) (*BeanfunClient, *qrLoginInit, uint64) {
	t.Helper()
	s.mgr.Stop(qrPollTaskName)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client, s.pendingQR, s.qrGeneration
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
	s, client, init := newTickFixture(t, srv)

	s.qrPollTick(context.Background(), 0, client, init)

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
	s, client, init := newTickFixture(t, srv)

	if delay := s.qrPollTick(context.Background(), 0, client, init); delay != 0 {
		t.Errorf("delay = %v, want 0 (expired ends the loop)", delay)
	}
	got, err := s.CheckQRLogin()
	if err != nil {
		t.Fatalf("CheckQRLogin: %v", err)
	}
	if got != QRStatusExpired {
		t.Errorf("got %q, want %q", got, QRStatusExpired)
	}
	s.mu.Lock()
	pending := s.pendingQR
	s.mu.Unlock()
	if pending != nil {
		t.Error("pendingQR should be cleared after Expired")
	}
	// The cache keeps reporting Expired without another poll.
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
	s, client, init := newTickFixture(t, srv)

	if delay := s.qrPollTick(context.Background(), 0, client, init); delay != 0 {
		t.Errorf("delay = %v, want 0 (approved ends the loop)", delay)
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
	s, client, init := newTickFixture(t, srv)

	// An unrecognised message is not terminal: the loop keeps going and the cache carries the
	// message. The LoginError kind does not survive the cache, and never crossed IPC either.
	if delay := s.qrPollTick(context.Background(), 0, client, init); delay != qrPollInterval {
		t.Errorf("delay = %v, want %v", delay, qrPollInterval)
	}
	st := s.cachedQRState()
	if !strings.Contains(st.Error, "unexpected server message") {
		t.Errorf("Error = %q, want it to mention %q", st.Error, "unexpected server message")
	}
}

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

// TestQRPollLoopStopsOnIPBlock replaces the old CheckQRLogin-in-cooldown test. The subject moved:
// the loop, not the frontend, now owns the cadence, so what has to be true is that a blocked poll
// ends the loop after exactly one request instead of re-arming the cooldown forever.
func TestQRPollLoopStopsOnIPBlock(t *testing.T) {
	withShortCooldown(t, time.Minute)

	var pollRequests atomic.Int32
	mux := fullInitMux()
	mux.HandleFunc("/QRLogin/CheckLoginStatus", func(w http.ResponseWriter, r *http.Request) {
		pollRequests.Add(1)
		http.Redirect(w, r, "/TW/BlockIPMessage.htm", http.StatusFound)
	})
	mux.HandleFunc("/TW/BlockIPMessage.htm", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(w, "<html>locked</html>")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s, client, init := newTickFixture(t, srv)

	if delay := s.qrPollTick(context.Background(), 0, client, init); delay != 0 {
		t.Errorf("delay = %v, want 0 — a blocked poll must end the loop", delay)
	}
	if armed := s.mintCooldownRemaining(); armed <= 0 {
		t.Fatalf("cooldown not armed after the blocked poll (got %v)", armed)
	}
	if got := pollRequests.Load(); got != 1 {
		t.Errorf("poll endpoint hit %d times, want 1", got)
	}
	st := s.cachedQRState()
	if !strings.Contains(st.Error, "ip temporarily blocked") {
		t.Errorf("Error = %q, want it to carry the block message", st.Error)
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

// TestQRPollTick is the tick's contract. The delay column alone cannot tell approved from expired
// (both end the loop), so status and error are asserted separately from it.
func TestQRPollTick(t *testing.T) {
	tests := []struct {
		name        string
		pollResult  string
		step4Cookie bool
		wantDelay   time.Duration
		wantStatus  QRStatus
		wantErrPart string
	}{
		{"wait login keeps polling", "Wait Login", false, 2 * time.Second, QRStatusPending, ""},
		{"failed keeps polling", "Failed", false, 2 * time.Second, QRStatusRetry, ""},
		{"token expired ends loop", "Token Expired", false, 0, QRStatusExpired, ""},
		{"success ends loop", "Success", true, 0, QRStatusApproved, ""},
		{"unknown message keeps polling", "Mystery Status", false, 2 * time.Second, QRStatusExpired, "unexpected server message"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(serviceTestMux(tt.pollResult, tt.step4Cookie))
			t.Cleanup(srv.Close)
			s, client, init := newTickFixture(t, srv)

			got := s.qrPollTick(context.Background(), 0, client, init)
			if got != tt.wantDelay {
				t.Errorf("delay = %v, want %v", got, tt.wantDelay)
			}
			st := s.cachedQRState()
			if st.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", st.Status, tt.wantStatus)
			}
			if tt.wantErrPart == "" {
				if st.Error != "" {
					t.Errorf("Error = %q, want empty", st.Error)
				}
				return
			}
			if !strings.Contains(st.Error, tt.wantErrPart) {
				t.Errorf("Error = %q, want it to contain %q", st.Error, tt.wantErrPart)
			}
		})
	}
}

// TestQRPollTick_PendingThenApproved is the transition the old fixed-result mux could not express:
// the same loop must keep polling on Wait Login and then finish the login when the answer changes.
func TestQRPollTick_PendingThenApproved(t *testing.T) {
	t.Parallel()
	mux, polls := pollSequenceMux([]string{"Wait Login", "Success"}, true)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s, client, init := newTickFixture(t, srv)

	if delay := s.qrPollTick(context.Background(), 0, client, init); delay != qrPollInterval {
		t.Fatalf("first tick delay = %v, want %v", delay, qrPollInterval)
	}
	if st := s.cachedQRState(); st.Status != QRStatusPending {
		t.Fatalf("after first tick status = %q, want %q", st.Status, QRStatusPending)
	}

	if delay := s.qrPollTick(context.Background(), 0, client, init); delay != 0 {
		t.Errorf("second tick delay = %v, want 0", delay)
	}
	if st := s.cachedQRState(); st.Status != QRStatusApproved {
		t.Errorf("after second tick status = %q, want %q", st.Status, QRStatusApproved)
	}
	s.mu.Lock()
	sess := s.session
	s.mu.Unlock()
	if sess == nil || sess.WebToken != "FULL_FLOW_TOKEN" {
		t.Errorf("session = %v, want one carrying FULL_FLOW_TOKEN", sess)
	}
	if got := polls.Load(); got != 2 {
		t.Errorf("poll endpoint hit %d times, want 2", got)
	}
}

// TestQRPollTick_StaleGenerationDiscarded covers the window bgtask.Stop leaves open: it cancels
// without waiting, so a tick from a superseded attempt can still finish and must not write.
func TestQRPollTick_StaleGenerationDiscarded(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(serviceTestMux("Success", true))
	t.Cleanup(srv.Close)
	s, client, init := newTickFixture(t, srv)

	// A newer attempt has started since this tick was registered.
	s.mu.Lock()
	s.qrGeneration = 5
	s.mu.Unlock()

	s.qrPollTick(context.Background(), 4, client, init)

	s.mu.Lock()
	sess := s.session
	s.mu.Unlock()
	if sess != nil {
		t.Error("a superseded tick installed a session")
	}
	if st := s.cachedQRState(); st.Status != QRStatusExpired {
		t.Errorf("status = %q, want %q (the constructor value, untouched)", st.Status, QRStatusExpired)
	}
	if names := s.mgr.List(); len(names) != 0 {
		t.Errorf("bgtask registry = %v, want empty (no keep-alive from a stale login)", names)
	}
}

// TestQRErrorState_ScrubsCredentials pins the one thing on this path that could leak: a transport
// error carries the full request URL, and that URL's query is where the session key lives.
func TestQRErrorState_ScrubsCredentials(t *testing.T) {
	t.Parallel()
	prev := QRState{Status: QRStatusPending}
	err := ErrHTTP(&url.Error{
		Op:  "Post",
		URL: "https://tw.newlogin.beanfun.com/QRLogin/CheckLoginStatus?pSKey=SECRET123",
		Err: errors.New("boom"),
	})

	got := qrErrorState(prev, err)

	if got.Status != QRStatusPending {
		t.Errorf("status = %q, want %q (an error must not change the status)", got.Status, QRStatusPending)
	}
	if strings.Contains(got.Error, "SECRET123") {
		t.Fatalf("payload leaked the session key: %q", got.Error)
	}
	if !strings.Contains(got.Error, "<redacted>") {
		t.Errorf("Error = %q, want the redaction marker", got.Error)
	}

	// A bare url.Error is the case ScrubCredentials alone has to catch: ErrHTTP redacts the URL in
	// place at construction, so anything already wrapped is safe without this call.
	raw := &url.Error{
		Op:  "Post",
		URL: "https://tw.newlogin.beanfun.com/QRLogin/CheckLoginStatus?pSKey=SECRET456",
		Err: errors.New("boom"),
	}
	if bare := qrErrorState(prev, raw); strings.Contains(bare.Error, "SECRET456") {
		t.Errorf("unwrapped error leaked the session key: %q", bare.Error)
	}
}

// TestCheckQRLoginMakesNoRequest is the Phase 1 claim: the read side is a cache lookup, so the
// frontend's existing 2s refetch costs nothing.
func TestCheckQRLoginMakesNoRequest(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())
	s.publishQRState(0, QRState{Status: QRStatusPending})

	for i := 0; i < 5; i++ {
		got, err := s.CheckQRLogin()
		if err != nil {
			t.Fatalf("CheckQRLogin #%d: %v", i+1, err)
		}
		if got != QRStatusPending {
			t.Errorf("call %d: got %q, want %q", i+1, got, QRStatusPending)
		}
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("CheckQRLogin issued %d request(s), want 0", got)
	}
}
