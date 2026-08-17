package beanfun

import (
	"context"
	"crypto/des"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// leakMarker is planted in fake page bodies; it must never appear in an
// error message.
const leakMarker = "SENSITIVE-BLOB-MARKER"

// Synthetic launch ticket for the step-1 page fixture — 64 hex
// characters in an obvious repeating pattern.
const fixtureLaunchTicket = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fixtureLaunchBlob builds the m_objData `data` value the step-1 page
// fixture carries, wrapping fixtureLaunchTicket.
func fixtureLaunchBlob(t *testing.T, serviceAccount string) string {
	t.Helper()
	return buildLaunchBlob(t, 8, "0a1b2c3d",
		"LaunchTicket="+fixtureLaunchTicket+
			"&ServiceCode=610074&ServiceRegion=T9&ServiceAccount="+serviceAccount)
}

// happyStep1Body returns the inline-JS-bearing HTML that step 1
// scrapes the long-polling key, unk_data, and createTime from.
func happyStep1Body(key, unkK, unkV, createTime, blob string) string {
	return fmt.Sprintf(`<html><body><script>
var stuff = "GetResultByLongPolling&key=%s";
var foo = MyAccountData.ServiceAccountCreateTime + "%s=%s";
var bar = ServiceAccountCreateTime: "%s";
var m_objData = {"region": "TW;Production", "sn": "%s", "data": "%s"};
</script></body></html>`, key, unkK, unkV, createTime, key, blob)
}

// encryptForFixture takes a plaintext OTP and a key, ECB-encrypts the
// padded plaintext to produce a step-5 cipher hex string. Used by the
// happy-path test to build a fixture without hardcoding magic bytes.
func encryptForFixture(t *testing.T, plaintext, key string) string {
	t.Helper()
	// Pad plaintext to 8-byte block with trailing NULs (Beanfun's WPF
	// uses NoPadding — sender appends zero bytes).
	padded := []byte(plaintext)
	for len(padded)%des.BlockSize != 0 {
		padded = append(padded, 0)
	}
	block, err := des.NewCipher([]byte(key))
	if err != nil {
		t.Fatalf("des.NewCipher: %v", err)
	}
	cipher := make([]byte, len(padded))
	for i := 0; i < len(padded); i += des.BlockSize {
		block.Encrypt(cipher[i:i+des.BlockSize], padded[i:i+des.BlockSize])
	}
	return strings.ToUpper(hex.EncodeToString(cipher))
}

func TestDecryptOTPPayload(t *testing.T) {
	t.Parallel()
	const key = "ABCD1234"
	const plain = "XYZ12345"
	cipher := encryptForFixture(t, plain, key)

	cases := []struct {
		name     string
		payload  string
		want     string
		wantKind LoginErrorKind
	}{
		{
			name:    "happy path",
			payload: key + cipher,
			want:    plain,
		},
		{
			name:     "empty payload",
			payload:  "",
			wantKind: KindOTPDecrypt,
		},
		{
			name:     "payload too short",
			payload:  "abc",
			wantKind: KindOTPDecrypt,
		},
		{
			name:     "non-hex cipher",
			payload:  "ABCD1234ZZZZ",
			wantKind: KindOTPDecrypt,
		},
		{
			name:     "cipher not block aligned",
			payload:  "ABCD1234DEADBEEF12", // hex => 5 bytes after 8-byte key
			wantKind: KindOTPDecrypt,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := decryptOTPPayload(tc.payload)
			if tc.wantKind != 0 {
				var le *LoginError
				if !errors.As(err, &le) || le.Kind != tc.wantKind {
					t.Errorf("got err %v, want Kind=%d", err, tc.wantKind)
				}
				return
			}
			if err != nil {
				t.Fatalf("decryptOTPPayload: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("plaintext = %q, want %q", got, tc.want)
			}
		})
	}
}

// otpMuxRecorder records the two routes a happy-path OTP fetch touches,
// plus a total request count so a revived priming step is caught.
type otpMuxRecorder struct {
	step1Hits, v2Hits int
	// totalRequests counts every request the mux sees, including paths
	// the flow should no longer touch — a revived step would show up
	// here even without its own handler.
	totalRequests int
	v2Body        []byte
	v2ContentType string
}

func happyOTPMux(t *testing.T, rec *otpMuxRecorder, otpKey, otpPlain string) http.Handler {
	t.Helper()
	blob := fixtureLaunchBlob(t, "T9abc123")
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/game_zone/game_start_step2.aspx", func(w http.ResponseWriter, _ *http.Request) {
		rec.step1Hits++
		writeHTML(w, happyStep1Body("LONGKEY123", "u_k", "u_v", "2024-01-15 12:34:56", blob))
	})
	mux.HandleFunc("/beanfun_block/generic_handlers/get_webstart_otp_v2.ashx", func(w http.ResponseWriter, r *http.Request) {
		rec.v2Hits++
		rec.v2Body, _ = io.ReadAll(r.Body)
		rec.v2ContentType = r.Header.Get("Content-Type")
		cipher := encryptForFixture(t, otpPlain, otpKey)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"result":1,"data":"%s%s","message":null}`, otpKey, cipher)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.totalRequests++
		mux.ServeHTTP(w, r)
	})
}

func TestBeanfunClient_FetchOTP_HappyPath(t *testing.T) {
	t.Parallel()
	rec := &otpMuxRecorder{}
	srv := httptest.NewServer(happyOTPMux(t, rec, "ABCD1234", "OTP56789"))
	t.Cleanup(srv.Close)

	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{
		SKey:          "SK",
		WebToken:      "WEBTKN",
		ServiceCode:   twDefaultServiceCode,
		ServiceRegion: twDefaultServiceRegion,
	}
	acc := Account{SID: "T9abc123", SSN: "7777777", SName: "Hero"}

	got, err := c.FetchOTP(context.Background(), sess, acc)
	if err != nil {
		t.Fatalf("FetchOTP: %v", err)
	}
	if string(got.Token) != "OTP56789" {
		t.Errorf("Token = %q, want OTP56789", got.Token)
	}
	if rec.step1Hits != 1 || rec.v2Hits != 1 {
		t.Errorf("hits = (step1 %d, v2 %d), want (1, 1)", rec.step1Hits, rec.v2Hits)
	}
	// Exactly two requests: the page fetch and the v2 POST. A revived
	// priming step, a retry, or a new call would push this above 2.
	if rec.totalRequests != 2 {
		t.Errorf("total requests = %d, want exactly 2", rec.totalRequests)
	}
	if rec.v2ContentType != "application/json" {
		t.Errorf("v2 Content-Type = %q, want application/json", rec.v2ContentType)
	}
	var sent map[string]any
	if err := json.Unmarshal(rec.v2Body, &sent); err != nil {
		t.Fatalf("v2 body is not JSON: %v", err)
	}
	// Exactly the five contract fields — a sixth would mean we are
	// sending something the endpoint did not ask for.
	if len(sent) != 5 {
		t.Errorf("v2 body has %d keys, want 5: %v", len(sent), sent)
	}
	// Literal expected values, so a changed constant fails here rather
	// than agreeing with itself.
	for _, f := range []struct{ key, want string }{
		{"SN", "LONGKEY123"},
		{"LaunchTicket", fixtureLaunchTicket},
		{"CV", "1.5.0.2"},
		{"Hash", "dfd568a69d87abcd8f4a93d1a4481ebb57712d1d28ab0b6fc018fcf140101e06"},
	} {
		if got, _ := sent[f.key].(string); got != f.want {
			t.Errorf("v2 body %s = %q, want %q", f.key, got, f.want)
		}
	}
	if arch, _ := sent["arch"].(string); arch != "x64" && arch != "x86" {
		t.Errorf("v2 body arch = %q, want x64 or x86", arch)
	}
}

func TestBeanfunClient_FetchOTP_Step1HandoffFailures(t *testing.T) {
	t.Parallel()
	// Both bodies plant leakMarker. The page carries m_objData, whose
	// blob decodes to a live LaunchTicket using only this package's
	// tables and a key embedded in the blob — so no part of the body may
	// reach an error message.
	tests := []struct {
		name     string
		body     string
		wantKind LoginErrorKind
	}{
		{
			name:     "handoff absent",
			body:     "<html>" + leakMarker + " no m_objData here</html>",
			wantKind: KindOTPInit,
		},
		{
			name: "handoff undecodable",
			body: "<html>" + leakMarker + `<script>var m_objData = ` +
				`{"region": "TW;Production", "sn": "SN1", "data": "0zzzzyyyyxxxx"};</script></html>`,
			wantKind: KindLaunchDataDecode,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/beanfun_block/game_zone/game_start_step2.aspx", func(w http.ResponseWriter, _ *http.Request) {
				writeHTML(w, tc.body)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.FetchOTP(context.Background(),
				&Session{ServiceCode: "x", ServiceRegion: "y", WebToken: "z"},
				Account{SID: "s", SSN: "1", SName: "n"})
			var le *LoginError
			if !errors.As(err, &le) || le.Kind != tc.wantKind {
				t.Fatalf("got %v, want Kind=%d", err, tc.wantKind)
			}
			if strings.Contains(le.Msg, leakMarker) {
				t.Errorf("error message echoes the page body: %q", le.Msg)
			}
			if strings.Contains(le.Msg, "body=") {
				t.Errorf("error message carries a body preview: %q", le.Msg)
			}
		})
	}
}

func TestBeanfunClient_FetchOTP_Step1SessionExpired(t *testing.T) {
	// Captured from real launcher-v0.1.0-alpha.17.log: when the
	// Beanfun session has timed out server-side, game_start_step2
	// returns a 200 OK with a "Messge Page" HTML stub whose
	// divMsg literally says "尚未登入，請重新登入". FetchOTP must
	// surface that as KindSessionExpired so the launcher resets
	// state and the frontend routes back to QR login.
	t.Parallel()
	const expiredBody = `<!DOCTYPE html><html><body>` +
		`<div id="divMsg">尚未登入，請重新登入</div>` +
		`</body></html>`
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/game_zone/game_start_step2.aspx", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(w, expiredBody)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.FetchOTP(context.Background(),
		&Session{ServiceCode: "x", ServiceRegion: "y", WebToken: "z"},
		Account{SID: "s", SSN: "1", SName: "n"})
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindSessionExpired {
		t.Errorf("got %v, want KindSessionExpired", err)
	}
}

func TestBeanfunClient_FetchOTP_V2ServerRejects(t *testing.T) {
	t.Parallel()
	blob := fixtureLaunchBlob(t, "s")
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/game_zone/game_start_step2.aspx", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(w, happyStep1Body("KEY", "u_k", "u_v", "2024-01-15 12:34:56", blob))
	})
	mux.HandleFunc("/beanfun_block/generic_handlers/get_webstart_otp_v2.ashx", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"result":0,"data":null,"message":"denied by server"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.FetchOTP(context.Background(),
		&Session{ServiceCode: "x", ServiceRegion: "y", WebToken: "z"},
		Account{SID: "s", SSN: "1", SName: "n"})
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindOTPServerRejected {
		t.Errorf("got %v, want KindOTPServerRejected", err)
	}
}

func TestBeanfunClient_FetchOTP_NilSession(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	c, err := NewBeanfunClientWithEndpoints(stubEndpoints(t, srv))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.FetchOTP(context.Background(), nil, Account{})
	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindLoginRequired {
		t.Errorf("got %v, want KindLoginRequired", err)
	}
}

func TestZero(t *testing.T) {
	t.Parallel()
	b := []byte{1, 2, 3, 4, 5}
	Zero(b)
	for i, v := range b {
		if v != 0 {
			t.Errorf("b[%d] = %d, want 0", i, v)
		}
	}
}
