package beanfun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

// multiLoginService serves a full QR flow whose echo_token replies carry mainIDs in order, the last one repeating.
func multiLoginService(t *testing.T, mainIDs ...string) *LoginService {
	t.Helper()
	mux := serviceTestMux("Success", true)
	var calls atomic.Int32
	mux.HandleFunc("/beanfun_block/generic_handlers/echo_token.ashx", func(w http.ResponseWriter, _ *http.Request) {
		i := min(int(calls.Add(1))-1, len(mainIDs)-1)
		writeJS(w, fmt.Sprintf(`BeanFunBlock.EchoTokenResult({ResultCode:1, ResultDesc: "User is logged in.", MainAccountID : %q });`, mainIDs[i]))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv), bgtask.New())
	t.Cleanup(s.mgr.StopAll)
	return s
}

// approveNext runs one whole QR attempt to approval and returns the client it logged in with.
func approveNext(t *testing.T, s *LoginService) *BeanfunClient {
	t.Helper()
	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}
	client, init, gen := stopLoopAndCapture(t, s)
	s.qrPollTick(context.Background(), gen, client, init)
	if st := s.cachedQRState(); st.Status != QRStatusApproved {
		t.Fatalf("state = %+v, want approved", st)
	}
	return client
}

func TestLoginService_TwoApprovalsCreateTwoLogins(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "AAAAAA1", "BBBBBB2")
	var changes [][]LoginInfo
	s.OnLoginsChanged = func(infos []LoginInfo) { changes = append(changes, infos) }

	c1 := approveNext(t, s)
	c2 := approveNext(t, s)

	want := []LoginInfo{{ID: "1", Label: "AA***A1"}, {ID: "2", Label: "BB***B2"}}
	if got := s.ListLogins(); !slices.Equal(got, want) {
		t.Errorf("ListLogins = %+v, want %+v", got, want)
	}
	names := s.mgr.List()
	for _, id := range []string{"1", "2"} {
		if !slices.Contains(names, keepAliveTaskPrefix+id) {
			t.Errorf("bgtask registry %v is missing keep-alive for login %s", names, id)
		}
	}
	if cl, _ := s.snapshot("1"); cl != c1 {
		t.Error("login 1 does not hold the first client")
	}
	if cl, _ := s.snapshot("2"); cl != c2 {
		t.Error("login 2 does not hold the second client")
	}
	if len(changes) != 2 || !slices.Equal(changes[1], want) {
		t.Errorf("OnLoginsChanged calls = %+v, want two, the last being %+v", changes, want)
	}
}

func TestStartQRLogin_KeepsExistingLogins(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "AAAAAA1")
	c1 := approveNext(t, s)

	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("second StartQRLogin: %v", err)
	}

	if got := s.ListLogins(); len(got) != 1 || got[0].ID != "1" {
		t.Errorf("ListLogins = %+v, want login 1 kept", got)
	}
	if cl, sess := s.snapshot("1"); cl != c1 || sess == nil {
		t.Errorf("snapshot(1) = (%v, %v), want the first login untouched", cl, sess)
	}
	if !slices.Contains(s.mgr.List(), keepAliveTaskPrefix+"1") {
		t.Error("StartQRLogin stopped an existing login's keep-alive")
	}
}

func TestEndSessionIfCurrent_RemovesOnlyThatLogin(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "AAAAAA1", "BBBBBB2")
	c1 := approveNext(t, s)
	approveNext(t, s)
	var ended []SessionEnded
	s.OnSessionEnded = func(ev SessionEnded) { ended = append(ended, ev) }

	if !s.endSessionIfCurrent("1", c1) {
		t.Fatal("endSessionIfCurrent(1, c1) = false, want true")
	}

	if got := s.ListLogins(); len(got) != 1 || got[0].ID != "2" {
		t.Errorf("ListLogins = %+v, want only login 2", got)
	}
	want := SessionEnded{Reason: "expired", LoginID: "1", Label: "AA***A1"}
	if len(ended) != 1 || ended[0] != want {
		t.Errorf("OnSessionEnded calls = %+v, want exactly %+v", ended, want)
	}
	names := s.mgr.List()
	if slices.Contains(names, keepAliveTaskPrefix+"1") || !slices.Contains(names, keepAliveTaskPrefix+"2") {
		t.Errorf("bgtask registry = %v, want only login 2's keep-alive", names)
	}
}

func TestEndSessionIfCurrent_OldClientAfterReplaceIsIgnored(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "SAMEID99")
	old := approveNext(t, s)
	approveNext(t, s)
	ended := 0
	s.OnSessionEnded = func(SessionEnded) { ended++ }

	if s.endSessionIfCurrent("1", old) {
		t.Error("endSessionIfCurrent with the replaced client = true, want false")
	}
	if ended != 0 || len(s.ListLogins()) != 1 {
		t.Errorf("ended = %d, logins = %+v; the replacement must survive", ended, s.ListLogins())
	}
}

func TestInstallLogin_SameMainIDReplaces(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "SAMEID99")
	var changes int
	s.OnLoginsChanged = func([]LoginInfo) { changes++ }

	first := approveNext(t, s)
	second := approveNext(t, s)

	got := s.ListLogins()
	if len(got) != 1 || got[0].ID != "1" || got[0].Label != "SA***99" {
		t.Fatalf("ListLogins = %+v, want one login with id 1 and label SA***99", got)
	}
	if cl, _ := s.snapshot("1"); cl != second || cl == first {
		t.Error("login 1 still holds the replaced client")
	}
	if names := s.mgr.List(); len(names) != 1 || names[0] != keepAliveTaskPrefix+"1" {
		t.Errorf("bgtask registry = %v, want just login 1's keep-alive", names)
	}
	if s.nextID != 1 {
		t.Errorf("nextID = %d, want 1 (a replace consumes no id)", s.nextID)
	}
	if changes != 2 {
		t.Errorf("OnLoginsChanged fired %d times, want 2", changes)
	}
}

func TestInstallLogin_EmptyMainIDNeverDedupes(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "")

	approveNext(t, s)
	approveNext(t, s)

	want := []LoginInfo{{ID: "1", Label: "帳號 1"}, {ID: "2", Label: "帳號 2"}}
	if got := s.ListLogins(); !slices.Equal(got, want) {
		t.Errorf("ListLogins = %+v, want %+v", got, want)
	}
}

func TestMaxLoginsIsFive(t *testing.T) {
	if maxLogins != 5 {
		t.Errorf("maxLogins = %d, want 5", maxLogins)
	}
}

func fillLogins(t *testing.T, s *LoginService, n int) {
	t.Helper()
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	eps := stubEndpoints(t, srv)
	for i := range n {
		c, err := NewBeanfunClientWithEndpoints(eps)
		if err != nil {
			t.Fatal(err)
		}
		addTestLogin(t, s, c, "FILLER-"+strconv.Itoa(i))
	}
}

func TestStartQRLogin_RefusesAtMaxLogins(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "AAAAAA1")
	fillLogins(t, s, maxLogins)

	_, err := s.StartQRLogin()

	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindTooManyLogins || le.Msg != "too many logins" {
		t.Fatalf("StartQRLogin err = %v, want KindTooManyLogins with message %q", err, "too many logins")
	}
	if got := len(s.ListLogins()); got != maxLogins {
		t.Errorf("logins = %d, want %d unchanged", got, maxLogins)
	}
	if s.pendingClient != nil {
		t.Error("a refused StartQRLogin left a pending attempt behind")
	}
}

// TestApproval_DiscardedWhenFull covers an attempt already past the StartQRLogin check when the last slot is taken.
func TestApproval_DiscardedWhenFull(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "NEWONE-1")
	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}
	fillLogins(t, s, maxLogins)
	client, init, gen := stopLoopAndCapture(t, s)

	s.qrPollTick(context.Background(), gen, client, init)

	if got := len(s.ListLogins()); got != maxLogins {
		t.Errorf("logins = %d, want %d (the sixth must be refused)", got, maxLogins)
	}
	if st := s.cachedQRState(); !strings.Contains(st.Error, "too many logins") {
		t.Errorf("state = %+v, want an error mentioning too many logins", st)
	}
}

func TestLogout_RemovesOnlyThatLoginSilently(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "AAAAAA1", "BBBBBB2")
	approveNext(t, s)
	approveNext(t, s)
	ended := 0
	s.OnSessionEnded = func(SessionEnded) { ended++ }
	var last []LoginInfo
	changes := 0
	s.OnLoginsChanged = func(infos []LoginInfo) { last = infos; changes++ }

	s.Logout("1")

	if got := s.ListLogins(); len(got) != 1 || got[0].ID != "2" {
		t.Errorf("ListLogins = %+v, want only login 2", got)
	}
	if ended != 0 {
		t.Errorf("OnSessionEnded fired %d times on a user logout, want 0", ended)
	}
	if changes != 1 || len(last) != 1 || last[0].ID != "2" {
		t.Errorf("OnLoginsChanged = %d calls, last %+v; want one call listing login 2", changes, last)
	}
	if slices.Contains(s.mgr.List(), keepAliveTaskPrefix+"1") {
		t.Error("keep-alive for the logged-out login is still registered")
	}

	s.Logout("nope")
	if changes != 1 {
		t.Error("logging out an unknown id must not emit")
	}
}

func TestReset_ClearsEveryLogin(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "AAAAAA1", "BBBBBB2", "CCCCCC3")
	approveNext(t, s)
	approveNext(t, s)
	last := []LoginInfo{{ID: "stale"}}
	s.OnLoginsChanged = func(infos []LoginInfo) { last = infos }

	s.Reset()

	if got := s.ListLogins(); len(got) != 0 {
		t.Errorf("ListLogins = %+v, want empty", got)
	}
	if last == nil || len(last) != 0 {
		t.Errorf("OnLoginsChanged payload = %#v, want a non-nil empty slice", last)
	}
	if names := s.mgr.List(); len(names) != 0 {
		t.Errorf("bgtask registry = %v, want empty", names)
	}
	approveNext(t, s)
	if got := s.ListLogins(); len(got) != 1 || got[0].ID != "3" {
		t.Errorf("ListLogins after Reset = %+v, want id 3 (ids are never reused)", got)
	}
}

func TestLabelFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, mainID string
		n            int
		want         string
	}{
		{"long id", "ABCDEFGH12", 3, "AB***12"},
		{"exactly six", "ABCDEF", 3, "AB***EF"},
		{"five is too short", "ABCDE", 4, "帳號 4"},
		{"empty", "", 2, "帳號 2"},
		{"multibyte is not split", "帳號甲乙丙丁", 1, "帳號***丙丁"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := labelFor(tt.mainID, tt.n); got != tt.want {
				t.Errorf("labelFor(%q, %d) = %q, want %q", tt.mainID, tt.n, got, tt.want)
			}
		})
	}
}

func TestBeanfunClient_EchoToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body, wantCode, wantID string
		wantErr                      bool
	}{
		{"logged in", echoLoggedIn, "1", "X", false},
		{"logged out without id", echoLoggedOut, "0", "", false},
		{"logged out still carries id", echoLoggedOutWithID, "0", "X", false},
		{"long id", `BeanFunBlock.EchoTokenResult({ResultCode:1, ResultDesc: "ok", MainAccountID : "abc123XYZ789" });`, "1", "abc123XYZ789", false},
		{"no id field", `BeanFunBlock.EchoTokenResult({ResultCode:1, ResultDesc: "ok" });`, "1", "", false},
		{"unrecognised page", "<html>x</html>", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/beanfun_block/generic_handlers/echo_token.ashx", func(w http.ResponseWriter, _ *http.Request) { writeJS(w, tt.body) })
			c, _ := newTestClient(t, mux)

			code, id, err := c.echoToken(context.Background())

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if code != tt.wantCode || id != tt.wantID {
				t.Errorf("echoToken = (%q, %q), want (%q, %q)", code, id, tt.wantCode, tt.wantID)
			}
		})
	}
}

func TestGetAccounts_UnknownLoginIDRequiresLogin(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "AAAAAA1")
	approveNext(t, s)

	_, err := s.GetAccounts("2")

	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindLoginRequired {
		t.Errorf("GetAccounts(unknown) err = %v, want KindLoginRequired", err)
	}
}

func TestLoginsChangedEventLiteral(t *testing.T) {
	if LoginsChangedEvent != "beanfun:logins-changed" {
		t.Errorf("LoginsChangedEvent = %q; the frontend listener hardcodes the string", LoginsChangedEvent)
	}
}

func TestSessionEnded_JSONShape(t *testing.T) {
	t.Parallel()
	b, err := json.Marshal(SessionEnded{Reason: "expired", LoginID: "2", Label: "AA***A1"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"reason":"expired","loginId":"2","label":"AA***A1"}`; got != want {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}

// TestMainIDNeverLeaves pins that the raw MainAccountID reaches neither the logs nor anything the frontend receives.
func TestMainIDNeverLeaves(t *testing.T) {
	const secret = "SECRETMAINID1234"
	logs := captureLogs(t)
	s := multiLoginService(t, secret)
	var emitted []string
	s.OnLoginsChanged = func(infos []LoginInfo) {
		b, _ := json.Marshal(infos)
		emitted = append(emitted, string(b))
	}
	s.OnSessionEnded = func(ev SessionEnded) {
		b, _ := json.Marshal(ev)
		emitted = append(emitted, string(b))
	}

	c := approveNext(t, s)
	s.endSessionIfCurrent("1", c)
	listed, _ := json.Marshal(s.ListLogins())

	for name, text := range map[string]string{
		"logs":    logs.String(),
		"emitted": strings.Join(emitted, "\n"),
		"listed":  string(listed),
	} {
		if strings.Contains(text, secret) {
			t.Errorf("the raw MainAccountID leaked into %s: %s", name, text)
		}
	}
	if len(emitted) == 0 {
		t.Fatal("no events were emitted, so the leak check proved nothing")
	}
}
