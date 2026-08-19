package beanfun

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// finalizeMuxHooks lets tests override behaviour per step.
type finalizeMuxHooks struct {
	step1Status       int    // 0 = use 200
	step2Status       int    // 0 = use 200
	step2Body         string // "" = happySendLoginBody
	step3SetCookie    bool   // emit bfWebToken on step 3?
	step4SetCookie    bool   // emit bfWebToken on step 4?
	step4WebToken     string
	step3Recorder     *http.Request
	step4Recorder     *http.Request
	step3BodyRecorded *string
	step4BodyRecorded *string
}

// fullFinalizeMux stubs the 4 routes finalize touches. It allows the
// test to inject failures per step and capture each request body.
func fullFinalizeMux(t *testing.T, hooks *finalizeMuxHooks) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/QRLogin/QRLogin", func(w http.ResponseWriter, _ *http.Request) {
		status := hooks.step1Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
	})
	mux.HandleFunc("/Login/SendLogin", func(w http.ResponseWriter, _ *http.Request) {
		status := hooks.step2Status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		body := hooks.step2Body
		if body == "" {
			body = happySendLoginBody()
		}
		_, _ = io.WriteString(w, body)
	})
	var returnCalls int
	mux.HandleFunc("/beanfun_block/bflogin/return.aspx", func(w http.ResponseWriter, r *http.Request) {
		returnCalls++
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if returnCalls == 1 {
			hooks.step3Recorder = r.Clone(r.Context())
			s := string(body)
			hooks.step3BodyRecorded = &s
			if hooks.step3SetCookie {
				http.SetCookie(w, &http.Cookie{Name: "bfWebToken", Value: "step3-token", Path: "/"})
			}
			// 200 is accepted by our finalize code (it accepts 2xx-3xx).
			// Using 200 instead of 302 avoids a CI flake we observed
			// with bare WriteHeader(302) responses (no Location set).
			w.WriteHeader(http.StatusOK)
			return
		}
		hooks.step4Recorder = r.Clone(r.Context())
		s := string(body)
		hooks.step4BodyRecorded = &s
		if hooks.step4SetCookie {
			tok := hooks.step4WebToken
			if tok == "" {
				tok = "WEB_TOKEN_FROM_STEP4"
			}
			http.SetCookie(w, &http.Cookie{Name: "bfWebToken", Value: tok, Path: "/"})
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

func TestFinalizeQRLogin_HappyPath(t *testing.T) {
	t.Parallel()
	hooks := &finalizeMuxHooks{step4SetCookie: true, step4WebToken: "CANONICAL_TOKEN"}
	c, _ := newTestClient(t, fullFinalizeMux(t, hooks))

	sess, err := c.finalizeQRLogin(context.Background(), &qrLoginInit{SKey: "OUTER_SK"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sess.SKey != "OUTER_SK" {
		t.Errorf("SKey = %q", sess.SKey)
	}
	if sess.WebToken != "CANONICAL_TOKEN" {
		t.Errorf("WebToken = %q, want CANONICAL_TOKEN", sess.WebToken)
	}
	if sess.AccountID != "" {
		t.Errorf("AccountID = %q, want empty for QR", sess.AccountID)
	}
	if sess.ServiceCode != twDefaultServiceCode {
		t.Errorf("ServiceCode = %q, want %q", sess.ServiceCode, twDefaultServiceCode)
	}
	if sess.ServiceRegion != twDefaultServiceRegion {
		t.Errorf("ServiceRegion = %q, want %q", sess.ServiceRegion, twDefaultServiceRegion)
	}
}

func TestFinalizeQRLogin_Step1HandshakeFailureSkipsRest(t *testing.T) {
	t.Parallel()
	hooks := &finalizeMuxHooks{step1Status: http.StatusInternalServerError, step4SetCookie: true}
	c, _ := newTestClient(t, fullFinalizeMux(t, hooks))

	_, err := c.finalizeQRLogin(context.Background(), &qrLoginInit{SKey: "SK"})
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindHTTP {
		t.Errorf("got %v, want KindHTTP", err)
	}
	// steps 3+4 should never run after step 1 fails
	if hooks.step3Recorder != nil {
		t.Error("step 3 should have been skipped")
	}
}

func TestFinalizeQRLogin_SendLoginEmptyFormFails(t *testing.T) {
	t.Parallel()
	hooks := &finalizeMuxHooks{step2Body: `<html><body>no inputs here</body></html>`}
	c, _ := newTestClient(t, fullFinalizeMux(t, hooks))

	_, err := c.finalizeQRLogin(context.Background(), &qrLoginInit{SKey: "SK"})
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindSendLoginNoFormData {
		t.Errorf("got %v, want KindSendLoginNoFormData", err)
	}
}

func TestFinalizeQRLogin_Step3MissingCookieTolerated(t *testing.T) {
	t.Parallel()
	hooks := &finalizeMuxHooks{
		step3SetCookie: false, // step 3 does NOT set bfWebToken
		step4SetCookie: true,  // step 4 sets it (canonical)
		step4WebToken:  "step4-canonical",
	}
	c, _ := newTestClient(t, fullFinalizeMux(t, hooks))

	sess, err := c.finalizeQRLogin(context.Background(), &qrLoginInit{SKey: "SK"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if sess.WebToken != "step4-canonical" {
		t.Errorf("WebToken = %q, want step4-canonical", sess.WebToken)
	}
}

func TestFinalizeQRLogin_Step4MissingCookieFatal(t *testing.T) {
	t.Parallel()
	hooks := &finalizeMuxHooks{step3SetCookie: false, step4SetCookie: false}
	c, _ := newTestClient(t, fullFinalizeMux(t, hooks))

	_, err := c.finalizeQRLogin(context.Background(), &qrLoginInit{SKey: "SK"})
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindMissingWebToken {
		t.Errorf("got %v, want KindMissingWebToken", err)
	}
}

func TestFinalizeQRLogin_Step2Step3HeadersAndOrdering(t *testing.T) {
	t.Parallel()
	hooks := &finalizeMuxHooks{step4SetCookie: true}
	c, srv := newTestClient(t, fullFinalizeMux(t, hooks))

	_, err := c.finalizeQRLogin(context.Background(), &qrLoginInit{SKey: "OUTER_SK"})
	if err != nil {
		t.Fatal(err)
	}

	if hooks.step3Recorder == nil || hooks.step4Recorder == nil {
		t.Fatal("step 3 or step 4 not recorded — ordering may have been off")
	}
	// Step 3's Referer should be LoginBase with trailing slash.
	wantReferer := srv.URL + "/"
	if got := hooks.step3Recorder.Header.Get("Referer"); got != wantReferer {
		t.Errorf("step 3 Referer = %q, want %q", got, wantReferer)
	}
	if got := hooks.step3Recorder.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Errorf("step 3 Content-Type = %q", got)
	}
}

func TestFinalizeQRLogin_Step4FormFields(t *testing.T) {
	t.Parallel()
	hooks := &finalizeMuxHooks{step4SetCookie: true}
	c, _ := newTestClient(t, fullFinalizeMux(t, hooks))

	_, err := c.finalizeQRLogin(context.Background(), &qrLoginInit{SKey: "OUTER_SK"})
	if err != nil {
		t.Fatal(err)
	}
	if hooks.step4BodyRecorded == nil {
		t.Fatal("step 4 body not recorded")
	}
	form, err := url.ParseQuery(*hooks.step4BodyRecorded)
	if err != nil {
		t.Fatalf("parse step 4 body: %v", err)
	}
	if got := form.Get("SessionKey"); got != "OUTER_SK" {
		t.Errorf("step 4 SessionKey = %q, want OUTER_SK", got)
	}
	if got := form.Get("AuthKey"); got != "OK" {
		t.Errorf("step 4 AuthKey = %q, want OK", got)
	}
	if got := form.Get("ServiceAccountSN"); got != "0" {
		t.Errorf("step 4 ServiceAccountSN = %q, want 0", got)
	}
	// ServiceCode + ServiceRegion are present but empty
	if _, ok := form["ServiceCode"]; !ok {
		t.Error("step 4 missing ServiceCode field")
	}
	if _, ok := form["ServiceRegion"]; !ok {
		t.Error("step 4 missing ServiceRegion field")
	}
}

func TestFinalizeQRLogin_Step3FormCarriesScrapedFields(t *testing.T) {
	t.Parallel()
	hooks := &finalizeMuxHooks{step4SetCookie: true}
	c, _ := newTestClient(t, fullFinalizeMux(t, hooks))

	_, err := c.finalizeQRLogin(context.Background(), &qrLoginInit{SKey: "SK"})
	if err != nil {
		t.Fatal(err)
	}
	if hooks.step3BodyRecorded == nil {
		t.Fatal("step 3 body not recorded")
	}
	form, err := url.ParseQuery(*hooks.step3BodyRecorded)
	if err != nil {
		t.Fatalf("parse step 3 body: %v", err)
	}
	if form.Get("SessionKey") != "SKEY_INNER_123" {
		t.Errorf("step 3 SessionKey = %q, want SKEY_INNER_123 (inner from SendLogin)", form.Get("SessionKey"))
	}
	if form.Get("AuthKey") != "AUTH_INNER_456" {
		t.Errorf("step 3 AuthKey = %q, want AUTH_INNER_456 (inner from SendLogin)", form.Get("AuthKey"))
	}
	// btn_submit (type=submit) should be excluded by extractHiddenInputs
	if _, ok := form["btn_submit"]; ok {
		t.Error("step 3 form should not include submit button")
	}
}

func TestFinalizeQRLogin_Step2AcceptIsQRSpecific(t *testing.T) {
	t.Parallel()
	var step2Accept string
	mux := http.NewServeMux()
	mux.HandleFunc("/QRLogin/QRLogin", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/Login/SendLogin", func(w http.ResponseWriter, r *http.Request) {
		step2Accept = r.Header.Get("Accept")
		_, _ = io.WriteString(w, happySendLoginBody())
	})
	var calls int
	mux.HandleFunc("/beanfun_block/bflogin/return.aspx", func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "bfWebToken", Value: "T", Path: "/"})
		w.WriteHeader(http.StatusOK)
	})
	c, _ := newTestClient(t, mux)

	if _, err := c.finalizeQRLogin(context.Background(), &qrLoginInit{SKey: "SK"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(step2Accept, "image/avif") || !strings.Contains(step2Accept, "image/webp") {
		t.Errorf("step 2 Accept = %q, want QR-specific image/avif+image/webp tokens", step2Accept)
	}
}

func TestFinalizeQRLogin_NoFormData_DoesNotLogPageText(t *testing.T) {
	const planted = "PLANTEDSECRET9999"
	logs := captureLogs(t)

	hooks := &finalizeMuxHooks{step2Body: `<html><body>` + planted + `</body></html>`}
	c, _ := newTestClient(t, fullFinalizeMux(t, hooks))

	_, err := c.finalizeQRLogin(context.Background(), &qrLoginInit{SKey: "SK"})
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindSendLoginNoFormData {
		t.Fatalf("got %v, want KindSendLoginNoFormData", err)
	}

	out := logs.String()
	if !strings.Contains(out, "SendLogin returned no form data") {
		t.Fatalf("the warn branch was not exercised:\n%s", out)
	}
	if strings.Contains(out, planted) {
		t.Errorf("page text leaked into logs:\n%s", out)
	}
}
