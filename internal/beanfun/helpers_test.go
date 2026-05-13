package beanfun

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// sampleQRBase64 is a 1×1 transparent PNG used as a stand-in for the
// QR image bytes the InitLogin endpoint returns.
const sampleQRBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="

// stubEndpoints points the BeanfunClient at the given test server for
// all three base URLs. Sufficient for every test in this package
// because stubbed routes use distinct paths.
func stubEndpoints(t *testing.T, srv *httptest.Server) Endpoints {
	t.Helper()
	base, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatalf("parse srv URL: %v", err)
	}
	return Endpoints{LoginBase: base, PortalBase: base, NewloginBase: base}
}

// newTestClient stands up an httptest server with the given mux and
// returns a BeanfunClient pointed at it. The server is auto-closed
// via t.Cleanup. Returning the server lets callers inspect URL or
// recorder hooks; tests that don't need it can use `_`.
func newTestClient(t *testing.T, mux *http.ServeMux) (*BeanfunClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

// writeHTML writes body to w as text/html.
func writeHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, body)
}

// writeJSON writes body to w as application/json.
func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprint(w, body)
}

// happyIndexBody returns minimal HTML containing the verification
// token hidden input. Used to stub Login/Index responses.
func happyIndexBody(token string) string {
	return `<html><body><input name="__RequestVerificationToken" type="hidden" value="` + token + `" /></body></html>`
}

// happyInitBody returns the canonical successful InitLogin JSON.
func happyInitBody(deeplink string) string {
	b, _ := json.Marshal(map[string]any{
		"Result": 0,
		"ResultData": map[string]any{
			"QRImage":  sampleQRBase64,
			"DeepLink": deeplink,
		},
	})
	return string(b)
}

// happySendLoginBody returns HTML with 5 hidden inputs that
// extractHiddenInputs scrapes, plus one submit button to verify the
// skip-submit logic. Used to stub Login/SendLogin responses.
func happySendLoginBody() string {
	return `<html><body>
<form action="https://tw.beanfun.com/beanfun_block/bflogin/return.aspx" method="post">
  <input type="hidden" name="SessionKey" value="SKEY_INNER_123" />
  <input type="hidden" name="AuthKey" value="AUTH_INNER_456" />
  <input type="hidden" name="ServiceCode" value="" />
  <input type="hidden" name="ServiceRegion" value="" />
  <input type="hidden" name="ServiceAccountSN" value="0" />
  <input type="submit" name="btn_submit" value="Submit" />
</form>
</body></html>`
}

// pollResponseBody returns a CheckLoginStatus JSON envelope with the
// given ResultMessage.
func pollResponseBody(msg string) string {
	b, _ := json.Marshal(map[string]any{
		"ResultMessage": msg,
		"ResultData":    map[string]any{},
	})
	return string(b)
}
