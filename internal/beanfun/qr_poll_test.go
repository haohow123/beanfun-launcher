package beanfun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func pollStubMux(handler http.HandlerFunc) *http.ServeMux {
	mux := http.NewServeMux()
	if handler != nil {
		mux.HandleFunc("/QRLogin/CheckLoginStatus", handler)
	}
	return mux
}

func TestPollQRLoginStatus_ResultMessageDispatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		msg  string
		want qrPollOutcome
	}{
		{"failed → pollOutcomeFailed", "Failed", pollOutcomeFailed},
		{"wait_login → pollOutcomeWaitLogin", "Wait Login", pollOutcomeWaitLogin},
		{"token_expired → pollOutcomeTokenExpired", "Token Expired", pollOutcomeTokenExpired},
		{"success → pollOutcomeApproved", "Success", pollOutcomeApproved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, _ := newTestClient(t, pollStubMux(func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, pollResponseBody(tt.msg))
			}))
			got, err := c.pollQRLoginStatus(context.Background(), &qrLoginInit{SKey: "SK", VerificationToken: "TKN"})
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got != tt.want {
				t.Errorf("outcome = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPollQRLoginStatus_UnknownResultMessage(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t, pollStubMux(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, pollResponseBody("Some Unknown State"))
	}))
	_, err := c.pollQRLoginStatus(context.Background(), &qrLoginInit{SKey: "SK"})
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindServerMessage {
		t.Errorf("got %v, want KindServerMessage", err)
	}
}

func TestPollQRLoginStatus_MissingResultMessage(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t, pollStubMux(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, `{}`)
	}))
	_, err := c.pollQRLoginStatus(context.Background(), &qrLoginInit{SKey: "SK"})
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindServerMessage {
		t.Errorf("got %v, want KindServerMessage", err)
	}
}

func TestPollQRLoginStatus_NonJSONBody(t *testing.T) {
	t.Parallel()
	c, _ := newTestClient(t, pollStubMux(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, "not-json-{")
	}))
	_, err := c.pollQRLoginStatus(context.Background(), &qrLoginInit{SKey: "SK"})
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindJSON {
		t.Errorf("got %v, want KindJSON", err)
	}
}

func TestPollQRLoginStatus_VerificationTokenHeader(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		token     string
		wantValue string
		wantSent  bool
	}{
		{"empty token → header omitted", "", "", false},
		{"non-empty token → header set", "TKN_VAL", "TKN_VAL", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var captured string
			var sent bool
			c, _ := newTestClient(t, pollStubMux(func(w http.ResponseWriter, r *http.Request) {
				_, sent = r.Header["Requestverificationtoken"]
				// fallback: normalised lookup
				if v := r.Header.Get("RequestVerificationToken"); v != "" {
					captured = v
					sent = true
				}
				writeJSON(w, pollResponseBody("Wait Login"))
			}))
			_, err := c.pollQRLoginStatus(context.Background(), &qrLoginInit{SKey: "SK", VerificationToken: tt.token})
			if err != nil {
				t.Fatal(err)
			}
			if sent != tt.wantSent {
				t.Errorf("sent = %v, want %v", sent, tt.wantSent)
			}
			if captured != tt.wantValue {
				t.Errorf("captured = %q, want %q", captured, tt.wantValue)
			}
		})
	}
}

func TestPollQRLoginStatus_RequestShape(t *testing.T) {
	t.Parallel()
	var captured http.Header
	var capturedBody []byte
	var capturedCL int64
	c, _ := newTestClient(t, pollStubMux(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		capturedCL = r.ContentLength
		capturedBody, _ = io.ReadAll(r.Body)
		writeJSON(w, pollResponseBody("Wait Login"))
	}))
	_, err := c.pollQRLoginStatus(context.Background(), &qrLoginInit{SKey: "SK", VerificationToken: "TK"})
	if err != nil {
		t.Fatal(err)
	}

	if got := captured.Get("Accept"); got != "application/json, text/plain, */*" {
		t.Errorf("Accept = %q", got)
	}
	if got := captured.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := captured.Get("Content-Length"); got != "0" {
		t.Errorf("Content-Length header = %q, want 0", got)
	}
	if capturedCL != 0 {
		t.Errorf("req.ContentLength = %d, want 0", capturedCL)
	}
	if len(capturedBody) != 0 {
		t.Errorf("body = %q, want empty", capturedBody)
	}
	if !strings.Contains(captured.Get("Referer"), "pSKey=SK") {
		t.Errorf("Referer missing pSKey: %q", captured.Get("Referer"))
	}
	if captured.Get("Origin") == "" {
		t.Errorf("Origin not set")
	}
}
