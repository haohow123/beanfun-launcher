package beanfun

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

const skeyFixture = "SKEYFIXTURE0123456789"

// assertNoSKeyLeak fails if the pSKey parameter or any 8-character fragment of the key reached out, so a partial or truncated disclosure cannot pass.
func assertNoSKeyLeak(t *testing.T, out, skey string) {
	t.Helper()
	if out == "" {
		t.Fatal("no log output captured; the harness is not wired up")
	}
	if strings.Contains(out, "pSKey=") || strings.Contains(out, "sessionKey=") {
		t.Errorf("log carries a session-key parameter:\n%s", out)
	}
	const window = 8
	for i := 0; i+window <= len(skey); i++ {
		if frag := skey[i : i+window]; strings.Contains(out, frag) {
			t.Errorf("log carries an %d-char fragment of the session key (%q):\n%s", window, frag, out)
			return
		}
	}
}

func TestGetSessionKey_DoesNotLogSessionKey(t *testing.T) {
	logs := captureLogs(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/bflogin/default.aspx", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login/id-pass.aspx?service=999999_T0&pSKey="+skeyFixture, http.StatusFound)
	})
	mux.HandleFunc("/login/id-pass.aspx", func(w http.ResponseWriter, r *http.Request) {
		writeHTML(w, "<html>ok</html>")
	})
	c, _ := newTestClient(t, mux)

	got, err := c.getSessionKey(context.Background())
	if err != nil {
		t.Fatalf("getSessionKey: %v", err)
	}
	if got != skeyFixture {
		t.Fatalf("key = %q, want %q", got, skeyFixture)
	}

	out := logs.String()
	assertNoSKeyLeak(t, out, skeyFixture)
	if !strings.Contains(out, "?<redacted>") {
		t.Errorf("final_url was not redacted:\n%s", out)
	}
	if !strings.Contains(out, "skey_len=21") {
		t.Errorf("skey_len missing from logs:\n%s", out)
	}
}

// TestGetSessionKey_RegexMissDoesNotLogURL covers the slog.Warn branch, which
// the success-path test cannot reach.
func TestGetSessionKey_RegexMissDoesNotLogURL(t *testing.T) {
	logs := captureLogs(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/bflogin/default.aspx", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login/id-pass.aspx?service=999999_T0&sessionKey="+skeyFixture, http.StatusFound)
	})
	mux.HandleFunc("/login/id-pass.aspx", func(w http.ResponseWriter, r *http.Request) {
		// A notice page that echoes the request is the realistic shape here.
		writeHTML(w, "<html>renamed parameter: sessionKey="+skeyFixture+"</html>")
	})
	c, _ := newTestClient(t, mux)

	_, err := c.getSessionKey(context.Background())
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindMissingSessionKey {
		t.Fatalf("got %v, want KindMissingSessionKey", err)
	}

	out := logs.String()
	if !strings.Contains(out, "regex did not match") {
		t.Fatalf("the warn branch was not exercised:\n%s", out)
	}
	assertNoSKeyLeak(t, out, skeyFixture)
	if !strings.Contains(out, "?<redacted>") {
		t.Errorf("final_url was not redacted:\n%s", out)
	}
}

// TestGetSessionKey_TransportFailureDoesNotLeakKeyInError covers the error
// channel: the client follows the redirect, so a failure on the second hop
// builds a url.Error around the key-bearing URL.
func TestGetSessionKey_TransportFailureDoesNotLeakKeyInError(t *testing.T) {
	t.Parallel()

	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/bflogin/default.aspx", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, deadURL+"/login/id-pass.aspx?service=999999_T0&pSKey="+skeyFixture, http.StatusFound)
	})
	c, _ := newTestClient(t, mux)

	_, err := c.getSessionKey(context.Background())
	if err == nil {
		t.Fatal("expected a transport error from the dead redirect target")
	}
	msg := err.Error()
	if strings.Contains(msg, skeyFixture) || strings.Contains(msg, "pSKey=") {
		t.Errorf("session key leaked into the error string: %s", msg)
	}
	if !strings.Contains(msg, "?<redacted>") {
		t.Errorf("expected the redaction marker in the error string: %s", msg)
	}
}
