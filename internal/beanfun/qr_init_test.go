package beanfun

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// initStubMux creates a mux with /Login/Index and /Login/InitLogin
// handlers wired up. Either may be nil to fall through to 404.
func initStubMux(indexHandler, initLoginHandler http.HandlerFunc) *http.ServeMux {
	mux := http.NewServeMux()
	if indexHandler != nil {
		mux.HandleFunc("/Login/Index", indexHandler)
	}
	if initLoginHandler != nil {
		mux.HandleFunc("/Login/InitLogin", initLoginHandler)
	}
	return mux
}

func TestInitQRLogin_HappyPath(t *testing.T) {
	t.Parallel()
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("TKN_A")) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, happyInitBody("https://app.example/auth")) },
	)
	c, _ := newTestClient(t, mux)

	got, err := c.initQRLogin(context.Background(), "SKEY_X")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.SKey != "SKEY_X" {
		t.Errorf("SKey = %q, want SKEY_X", got.SKey)
	}
	if got.BitmapBase64 != sampleQRBase64 {
		t.Errorf("BitmapBase64 mismatch")
	}
	if got.Deeplink != "https://app.example/auth" {
		t.Errorf("Deeplink = %q", got.Deeplink)
	}
	if got.VerificationToken != "TKN_A" {
		t.Errorf("VerificationToken = %q", got.VerificationToken)
	}
}

func TestInitQRLogin_MissingVerificationTokenLenient(t *testing.T) {
	t.Parallel()
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, `<html><body>no token here</body></html>`) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, happyInitBody("")) },
	)
	c, _ := newTestClient(t, mux)

	got, err := c.initQRLogin(context.Background(), "SK")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.VerificationToken != "" {
		t.Errorf("expected empty token, got %q", got.VerificationToken)
	}
}

func TestInitQRLogin_DeeplinkUnwrap(t *testing.T) {
	t.Parallel()
	wrapped := "https://play.games.gamania.com/foo/deeplink/?url=https%3A%2F%2Fapp.example%2Fauth%3Ft%3D1"
	expectedInner := "https://app.example/auth?t=1"

	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("X")) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, happyInitBody(wrapped)) },
	)
	c, _ := newTestClient(t, mux)

	got, err := c.initQRLogin(context.Background(), "SK")
	if err != nil {
		t.Fatal(err)
	}
	if got.Deeplink != expectedInner {
		t.Errorf("Deeplink = %q, want %q", got.Deeplink, expectedInner)
	}
}

func TestInitQRLogin_DeeplinkPlainPassthrough(t *testing.T) {
	t.Parallel()
	plain := "https://elsewhere.example/path"
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("X")) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, happyInitBody(plain)) },
	)
	c, _ := newTestClient(t, mux)

	got, err := c.initQRLogin(context.Background(), "SK")
	if err != nil {
		t.Fatal(err)
	}
	if got.Deeplink != plain {
		t.Errorf("Deeplink = %q, want passthrough %q", got.Deeplink, plain)
	}
}

func TestInitQRLogin_DeeplinkOmitted(t *testing.T) {
	t.Parallel()
	body, _ := json.Marshal(map[string]any{
		"Result":     0,
		"ResultData": map[string]any{"QRImage": sampleQRBase64},
	})
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("X")) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, string(body)) },
	)
	c, _ := newTestClient(t, mux)

	got, err := c.initQRLogin(context.Background(), "SK")
	if err != nil {
		t.Fatal(err)
	}
	if got.Deeplink != "" {
		t.Errorf("Deeplink = %q, want empty when omitted", got.Deeplink)
	}
}

func TestInitQRLogin_DeeplinkEmptyString(t *testing.T) {
	t.Parallel()
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("X")) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, happyInitBody("")) },
	)
	c, _ := newTestClient(t, mux)

	got, err := c.initQRLogin(context.Background(), "SK")
	if err != nil {
		t.Fatal(err)
	}
	if got.Deeplink != "" {
		t.Errorf("Deeplink = %q, want empty when blank", got.Deeplink)
	}
}

func TestInitQRLogin_ResultNonZero(t *testing.T) {
	t.Parallel()
	body, _ := json.Marshal(map[string]any{
		"Result":     -1,
		"ResultData": map[string]any{"QRImage": sampleQRBase64},
	})
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("X")) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, string(body)) },
	)
	c, _ := newTestClient(t, mux)

	_, err := c.initQRLogin(context.Background(), "SK")
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindQRInitResult {
		t.Errorf("got %v, want KindQRInitResult", err)
	}
}

func TestInitQRLogin_MissingResultField(t *testing.T) {
	t.Parallel()
	body, _ := json.Marshal(map[string]any{
		"ResultData": map[string]any{"QRImage": sampleQRBase64},
	})
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("X")) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, string(body)) },
	)
	c, _ := newTestClient(t, mux)
	_, err := c.initQRLogin(context.Background(), "SK")
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindQRInitResult {
		t.Errorf("got %v, want KindQRInitResult", err)
	}
}

func TestInitQRLogin_MissingResultData(t *testing.T) {
	t.Parallel()
	body, _ := json.Marshal(map[string]any{"Result": 0})
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("X")) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, string(body)) },
	)
	c, _ := newTestClient(t, mux)
	_, err := c.initQRLogin(context.Background(), "SK")
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindQRInitResult {
		t.Errorf("got %v, want KindQRInitResult", err)
	}
}

func TestInitQRLogin_MissingQRImage(t *testing.T) {
	t.Parallel()
	body, _ := json.Marshal(map[string]any{
		"Result":     0,
		"ResultData": map[string]any{},
	})
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("X")) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, string(body)) },
	)
	c, _ := newTestClient(t, mux)
	_, err := c.initQRLogin(context.Background(), "SK")
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindQRInitResult {
		t.Errorf("got %v, want KindQRInitResult", err)
	}
}

func TestInitQRLogin_EmptyQRImage(t *testing.T) {
	t.Parallel()
	body, _ := json.Marshal(map[string]any{
		"Result":     0,
		"ResultData": map[string]any{"QRImage": ""},
	})
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("X")) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, string(body)) },
	)
	c, _ := newTestClient(t, mux)
	_, err := c.initQRLogin(context.Background(), "SK")
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindQRInitResult {
		t.Errorf("got %v, want KindQRInitResult", err)
	}
}

func TestInitQRLogin_InvalidJSON(t *testing.T) {
	t.Parallel()
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("X")) },
		func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, "not-json-{") },
	)
	c, _ := newTestClient(t, mux)
	_, err := c.initQRLogin(context.Background(), "SK")
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindJSON {
		t.Errorf("got %v, want KindJSON", err)
	}
}

func TestInitQRLogin_Step2RequestHeaders(t *testing.T) {
	t.Parallel()
	var capturedHeaders http.Header
	var capturedQuery string
	mux := initStubMux(
		func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, happyIndexBody("TK")) },
		func(w http.ResponseWriter, r *http.Request) {
			capturedHeaders = r.Header.Clone()
			capturedQuery = r.URL.RawQuery
			writeJSON(w, happyInitBody("https://x"))
		},
	)
	c, srv := newTestClient(t, mux)

	if _, err := c.initQRLogin(context.Background(), "SK"); err != nil {
		t.Fatal(err)
	}

	if got, want := capturedHeaders.Get("Accept"), "application/json, text/plain, */*"; got != want {
		t.Errorf("Accept = %q, want %q", got, want)
	}
	if got, want := capturedHeaders.Get("X-Requested-With"), "XMLHttpRequest"; got != want {
		t.Errorf("X-Requested-With = %q, want %q", got, want)
	}
	if got := capturedHeaders.Get("Referer"); !strings.Contains(got, "pSKey=SK") {
		t.Errorf("Referer = %q, want to contain %q", got, "pSKey=SK")
	}
	if got, want := capturedHeaders.Get("Origin"), srv.URL; got != want {
		t.Errorf("Origin = %q, want %q", got, want)
	}
	if capturedQuery != "pSKey=SK" {
		t.Errorf("query = %q, want pSKey=SK", capturedQuery)
	}
}
