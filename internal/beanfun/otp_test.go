package beanfun

import (
	"context"
	"crypto/des"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// happyStep1Body returns the inline-JS-bearing HTML that step 1
// scrapes the long-polling key, unk_data, and createTime from.
func happyStep1Body(key, unkK, unkV, createTime string) string {
	return fmt.Sprintf(`<html><body><script>
var stuff = "GetResultByLongPolling&key=%s";
var foo = MyAccountData.ServiceAccountCreateTime + "%s=%s";
var bar = ServiceAccountCreateTime: "%s";
</script></body></html>`, key, unkK, unkV, createTime)
}

func happyStep2Body(secret string) string {
	return fmt.Sprintf(`<html><body><script>var m_strSecretCode = '%s';</script></body></html>`, secret)
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

func TestDecryptOTP(t *testing.T) {
	t.Parallel()
	const key = "ABCD1234"
	const plain = "XYZ12345"
	cipher := encryptForFixture(t, plain, key)

	cases := []struct {
		name     string
		envelope string
		want     string
		wantKind LoginErrorKind
	}{
		{
			name:     "happy path",
			envelope: "1;" + key + cipher,
			want:     plain,
		},
		{
			name:     "empty envelope",
			envelope: "",
			wantKind: KindOTPServerRejected,
		},
		{
			name:     "missing semicolon",
			envelope: "1ABC",
			wantKind: KindOTPServerRejected,
		},
		{
			name:     "status not 1",
			envelope: "0;something went wrong",
			wantKind: KindOTPServerRejected,
		},
		{
			name:     "payload too short",
			envelope: "1;abc",
			wantKind: KindOTPDecrypt,
		},
		{
			name:     "non-hex cipher",
			envelope: "1;ABCD1234ZZZZ",
			wantKind: KindOTPDecrypt,
		},
		{
			name:     "cipher not block aligned",
			envelope: "1;ABCD1234DEADBEEF12", // hex => 5 bytes after 8-byte key
			wantKind: KindOTPDecrypt,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := decryptOTP(tc.envelope)
			if tc.wantKind != 0 {
				var le *LoginError
				if !errors.As(err, &le) || le.Kind != tc.wantKind {
					t.Errorf("got err %v, want Kind=%d", err, tc.wantKind)
				}
				return
			}
			if err != nil {
				t.Fatalf("decryptOTP: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("plaintext = %q, want %q", got, tc.want)
			}
		})
	}
}

// otpMux stubs all 6 routes for a happy-path OTP fetch. Per-route
// hits are recorded so callers can assert ordering / request shape.
type otpMuxRecorder struct {
	step1Hits, step2Hits, step3Hits, step4Hits, step5Hits int
	step3Body                                             string
	step5RawQuery                                         string
}

func happyOTPMux(t *testing.T, rec *otpMuxRecorder, otpKey, otpPlain string) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/game_zone/game_start_step2.aspx", func(w http.ResponseWriter, _ *http.Request) {
		rec.step1Hits++
		writeHTML(w, happyStep1Body("LONGKEY123", "u_k", "u_v", "2024-01-15 12:34:56"))
	})
	mux.HandleFunc("/generic_handlers/get_cookies.ashx", func(w http.ResponseWriter, _ *http.Request) {
		rec.step2Hits++
		writeHTML(w, happyStep2Body("SECRET_XYZ"))
	})
	mux.HandleFunc("/beanfun_block/generic_handlers/record_service_start.ashx", func(w http.ResponseWriter, r *http.Request) {
		rec.step3Hits++
		bs, _ := io.ReadAll(r.Body)
		rec.step3Body = string(bs)
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/generic_handlers/get_result.ashx", func(w http.ResponseWriter, _ *http.Request) {
		rec.step4Hits++
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/beanfun_block/generic_handlers/get_webstart_otp.ashx", func(w http.ResponseWriter, r *http.Request) {
		rec.step5Hits++
		rec.step5RawQuery = r.URL.RawQuery
		cipher := encryptForFixture(t, otpPlain, otpKey)
		_, _ = fmt.Fprintf(w, "1;%s%s", otpKey, cipher)
	})
	return mux
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
	if rec.step1Hits != 1 || rec.step2Hits != 1 || rec.step3Hits != 1 ||
		rec.step4Hits != 1 || rec.step5Hits != 1 {
		t.Errorf("step hits = (1,2,3,4,5) = (%d,%d,%d,%d,%d), want all 1",
			rec.step1Hits, rec.step2Hits, rec.step3Hits, rec.step4Hits, rec.step5Hits)
	}
	// Step 3 form should include the unk_data key
	if !strings.Contains(rec.step3Body, "u_k=u_v") {
		t.Errorf("step3 body missing unk_data: %q", rec.step3Body)
	}
	if !strings.Contains(rec.step3Body, "service_account_id=T9abc123") {
		t.Errorf("step3 body missing service_account_id: %q", rec.step3Body)
	}
	// Step 5 query must carry the literal ppppp value, the SN=key,
	// and the %20-encoded CreateTime.
	if !strings.Contains(rec.step5RawQuery, "ppppp="+pppppLiteral) {
		t.Errorf("step5 missing ppppp literal: %q", rec.step5RawQuery)
	}
	if !strings.Contains(rec.step5RawQuery, "SN=LONGKEY123") {
		t.Errorf("step5 missing SN: %q", rec.step5RawQuery)
	}
	if !strings.Contains(rec.step5RawQuery, "CreateTime=2024-01-15%2012:34:56") {
		t.Errorf("step5 CreateTime not %%20-encoded: %q", rec.step5RawQuery)
	}
	if !strings.Contains(rec.step5RawQuery, "WebToken=WEBTKN") {
		t.Errorf("step5 missing WebToken: %q", rec.step5RawQuery)
	}
}

func TestBeanfunClient_FetchOTP_Step1MissingLongPollingKey(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/game_zone/game_start_step2.aspx", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(w, "<html>no key here</html>")
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
	if !errors.As(err, &le) || le.Kind != KindOTPInit {
		t.Errorf("got %v, want KindOTPInit", err)
	}
}

func TestBeanfunClient_FetchOTP_Step5ServerRejects(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/game_zone/game_start_step2.aspx", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(w, happyStep1Body("KEY", "u_k", "u_v", "2024-01-15 12:34:56"))
	})
	mux.HandleFunc("/generic_handlers/get_cookies.ashx", func(w http.ResponseWriter, _ *http.Request) {
		writeHTML(w, happyStep2Body("SECRET"))
	})
	mux.HandleFunc("/beanfun_block/generic_handlers/record_service_start.ashx", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/generic_handlers/get_result.ashx", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/beanfun_block/generic_handlers/get_webstart_otp.ashx", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "0;denied by server")
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
