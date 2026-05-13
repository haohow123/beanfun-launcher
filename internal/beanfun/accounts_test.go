package beanfun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func accountsListHTML(rows ...string) string {
	var b strings.Builder
	b.WriteString("<html><body>")
	for _, r := range rows {
		b.WriteString(r)
	}
	b.WriteString("</body></html>")
	return b.String()
}

// accountRow renders one rendered account row using the live HTML
// shape — <li> with several attributes before onclick, then an inner
// <div id=... sn=... name=...>. See real-world dump captured during
// Milestone 5 dev: /tmp/beanfun-account-list-dump.html line 56.
func accountRow(onclick, sid, ssn, name string) string {
	return fmt.Sprintf(
		`<li class="" title="使用這個帳戶啟動遊戲" onclick="%s">`+
			`<div id="%s" sn="%s" name="%s" inherited="false" visible="1" class="Account">%s</div>`+
			`</li>`,
		onclick, sid, ssn, name, name,
	)
}

func TestExtractAccounts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		html string
		want []Account
	}{
		{
			name: "multi-row sort by ssn",
			html: accountsListHTML(
				accountRow("x", "bbb", "222", "Bravo"),
				accountRow("x", "aaa", "111", "Alpha"),
				accountRow("x", "ccc", "333", "Charlie"),
			),
			want: []Account{
				{SID: "aaa", SSN: "111", SName: "Alpha"},
				{SID: "bbb", SSN: "222", SName: "Bravo"},
				{SID: "ccc", SSN: "333", SName: "Charlie"},
			},
		},
		{
			name: "single row",
			html: accountsListHTML(accountRow("onclick(...)", "abc", "000111", "Solo")),
			want: []Account{{SID: "abc", SSN: "000111", SName: "Solo"}},
		},
		{
			name: "empty list",
			html: "<html><body><div>No accounts</div></body></html>",
			want: []Account{},
		},
		{
			name: "named HTML entity in name",
			html: accountsListHTML(accountRow("x", "abc", "111", "Tom &amp; Jerry")),
			want: []Account{{SID: "abc", SSN: "111", SName: "Tom & Jerry"}},
		},
		{
			name: "numeric HTML entity in name (chinese)",
			// &#23567;&#27193; → 小樹 (traditional). Confirms the
			// tokenizer decodes bare decimal numeric character references.
			html: accountsListHTML(accountRow("x", "abc", "111", "&#23567;&#27193;")),
			want: []Account{{SID: "abc", SSN: "111", SName: "小樹"}},
		},
		{
			name: "malformed row is skipped",
			html: `<a onclick="x"><div id="" sn="" name=""></div></a>` +
				accountsListHTML(accountRow("x", "abc", "111", "Real")),
			want: []Account{{SID: "abc", SSN: "111", SName: "Real"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractAccounts(tc.html)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d rows, want %d: got=%+v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("row %d: got %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestExtractAccounts_RealBeanfunShape regression-tests against the
// exact production HTML shape captured 2026-05-13. The fixture mirrors
// the live page's ul → li → div structure AND the JS template inside a
// <script> block — the latter exists to confirm the regex does NOT
// match templated rows where attribute values are JS concatenations.
func TestExtractAccounts_RealBeanfunShape(t *testing.T) {
	t.Parallel()
	fixture := `<ul id="ulServiceAccountList" class="ServiceAccountList">
<li class="" title="使用這個帳戶啟動遊戲" onclick="GameAccount.StartGame('1234567'); return false;"><div id="T9abcdef0123456789ab" sn="1234567" name="TestUser" inherited="false" visible="1" class="Account" title="編輯帳戶" onclick="GameAccount.ShowEditAcountDialog(event, 'T9abcdef0123456789ab'); return false;">TestUser</div><span class="StartButtonSmall"><input type="button" value="開始遊戲" /></span></li></ul>
<script type="text/javascript">
function AddServiceAccountToList(strServiceAccountSN, strServiceAccountID, strServiceAccountDisplayName, strServiceAccountCurtailName) {
  $('#ulServiceAccountList').prepend('<li class="" title="t" onclick="GameAccount.StartGame(' + strServiceAccountSN + '); return false;"><div id="' + strServiceAccountID + '" sn="' + strServiceAccountSN + '" name="' + strServiceAccountDisplayName + '" inherited="false" visible="1" class="Account">' + strServiceAccountCurtailName + '</div></li>');
}
</script>`
	got := extractAccounts(fixture)
	want := []Account{
		{SID: "T9abcdef0123456789ab", SSN: "1234567", SName: "TestUser"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// happyAccountListBody is the canonical HTML fixture for the integration
// tests — three rows, mixed enable state, one with a named HTML entity.
func happyAccountListBody() string {
	// Real Beanfun onclick handlers look like
	//   onclick="javascript:return CheckGameStart('610074', 'T9')"
	// Single quotes inside the double-quoted attribute so the regex's
	// [^"]* matches the whole handler.
	return accountsListHTML(
		accountRow(`javascript:CheckGameStart('active1')`, "aaa", "111", "Hero"),
		accountRow("", "frozen", "222", "Stuck"),
		accountRow(`javascript:CheckGameStart('active2')`, "ccc", "333", "Tom &amp; Jerry"),
	)
}

func TestBeanfunClient_GetAccounts_HappyPath(t *testing.T) {
	t.Parallel()
	var authHits, listHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/auth.aspx", func(w http.ResponseWriter, r *http.Request) {
		authHits++
		q := r.URL.Query()
		if q.Get("channel") != "game_zone" {
			t.Errorf("channel = %q, want game_zone", q.Get("channel"))
		}
		if q.Get("web_token") != "TKN" {
			t.Errorf("web_token = %q, want TKN", q.Get("web_token"))
		}
		if want := "service_code_and_region=610074_T9"; !strings.Contains(q.Get("page_and_query"), want) {
			t.Errorf("page_and_query = %q, want substring %q", q.Get("page_and_query"), want)
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/beanfun_block/game_zone/game_server_account_list.aspx", func(w http.ResponseWriter, r *http.Request) {
		listHits++
		q := r.URL.Query()
		if q.Get("sc") != "610074" {
			t.Errorf("sc = %q, want 610074", q.Get("sc"))
		}
		if q.Get("sr") != "T9" {
			t.Errorf("sr = %q, want T9", q.Get("sr"))
		}
		if len(q.Get("dt")) != 14 {
			t.Errorf("dt = %q (want 14 chars YYYYMMDDHHMMSS)", q.Get("dt"))
		}
		_, _ = io.WriteString(w, happyAccountListBody())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{
		WebToken:      "TKN",
		ServiceCode:   twDefaultServiceCode,
		ServiceRegion: twDefaultServiceRegion,
	}
	got, err := c.GetAccounts(context.Background(), sess)
	if err != nil {
		t.Fatalf("GetAccounts: %v", err)
	}
	if authHits != 1 {
		t.Errorf("auth.aspx hit %d times, want 1", authHits)
	}
	if listHits != 1 {
		t.Errorf("game_server_account_list.aspx hit %d times, want 1", listHits)
	}
	want := []Account{
		{SID: "aaa", SSN: "111", SName: "Hero"},
		{SID: "frozen", SSN: "222", SName: "Stuck"},
		{SID: "ccc", SSN: "333", SName: "Tom & Jerry"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d, want %d: %+v", len(got), len(want), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestBeanfunClient_GetAccounts_NilSession(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.GetAccounts(context.Background(), nil)
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindLoginRequired {
		t.Errorf("got %v, want KindLoginRequired", err)
	}
}

func TestBeanfunClient_GetAccounts_AuthRefreshFails(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/auth.aspx", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{ServiceCode: "x", ServiceRegion: "y", WebToken: "z"}
	_, err = c.GetAccounts(context.Background(), sess)
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindHTTP {
		t.Errorf("got %v, want KindHTTP", err)
	}
}

func TestBeanfunClient_GetAccounts_EmptyList(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/auth.aspx", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/beanfun_block/game_zone/game_server_account_list.aspx", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html><body>No accounts found.</body></html>")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{ServiceCode: "610074", ServiceRegion: "T9", WebToken: "TKN"}
	got, err := c.GetAccounts(context.Background(), sess)
	if err != nil {
		t.Fatalf("GetAccounts: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %d rows: %+v", len(got), got)
	}
}

func TestLoginService_GetAccounts_NoSession(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv))
	_, err := s.GetAccounts()
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindLoginRequired {
		t.Errorf("got %v, want KindLoginRequired", err)
	}
}

func TestLoginService_GetAccounts_HappyPath(t *testing.T) {
	t.Parallel()
	mux := serviceTestMux("Success", true)
	mux.HandleFunc("/beanfun_block/auth.aspx", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/beanfun_block/game_zone/game_server_account_list.aspx", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, happyAccountListBody())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	s := NewLoginServiceWithEndpoints(stubEndpoints(t, srv))

	if _, err := s.StartQRLogin(); err != nil {
		t.Fatalf("StartQRLogin: %v", err)
	}
	if _, err := s.CheckQRLogin(); err != nil {
		t.Fatalf("CheckQRLogin: %v", err)
	}
	got, err := s.GetAccounts()
	if err != nil {
		t.Fatalf("GetAccounts: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got %d rows, want 3: %+v", len(got), got)
	}
}
