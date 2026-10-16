package launcher

import (
	"crypto/des"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

// desECBHex DES-ECB encrypts plaintext (NUL-padded to a block) with an 8-byte key and returns lowercase hex.
func desECBHex(t *testing.T, key, plaintext string) string {
	t.Helper()
	padded := []byte(plaintext)
	for len(padded)%des.BlockSize != 0 {
		padded = append(padded, 0)
	}
	block, err := des.NewCipher([]byte(key))
	if err != nil {
		t.Fatalf("des.NewCipher: %v", err)
	}
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += des.BlockSize {
		block.Encrypt(out[i:i+des.BlockSize], padded[i:i+des.BlockSize])
	}
	return hex.EncodeToString(out)
}

// launchBlob builds the step-1 handoff blob with selector 0, whose substitution table is the first of beanfun's four.
func launchBlob(t *testing.T) string {
	t.Helper()
	const table = "bac987d65e432f10"
	const key = "0a1b2c3d"
	plain := "LaunchTicket=" + strings.Repeat("ab", 32) + "&ServiceCode=610074&ServiceRegion=T9&ServiceAccount=T9abc123"
	cipher := desECBHex(t, key, plain)
	norm := cipher[:1] + key + cipher[1:]
	var enc strings.Builder
	for i := 0; i < len(norm); i++ {
		enc.WriteByte(table[strings.IndexByte("0123456789abcdef", norm[i])])
	}
	return "0" + enc.String()
}

// otpServer serves the two routes a successful FetchOTP touches.
func otpServer(t *testing.T) *beanfun.BeanfunClient {
	t.Helper()
	return countingOTPServer(t, new(atomic.Int32))
}

// countingOTPServer is otpServer that also counts every request it receives.
func countingOTPServer(t *testing.T, hits *atomic.Int32) *beanfun.BeanfunClient {
	t.Helper()
	const otpKey = "ABCD1234"
	blob := launchBlob(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/beanfun_block/game_zone/game_start_step2.aspx", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<script>var m_objData = {"region": "TW;Production", "sn": "SN1", "data": "%s"};</script>`, blob)
	})
	mux.HandleFunc("/beanfun_block/generic_handlers/get_webstart_otp_v2.ashx", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"result":1,"data":"%s%s","message":null}`, otpKey, desECBHex(t, otpKey, "OTP56789"))
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	client, err := beanfun.NewBeanfunClientWithEndpoints(beanfun.Endpoints{LoginBase: base, PortalBase: base, NewloginBase: base})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// TestLauncherService_SpawnGame_SignatureRejectedAfterOTP_NeverCallsSpawnFn pins the second verification that runs once the OTP is in hand.
func TestLauncherService_SpawnGame_SignatureRejectedAfterOTP_NeverCallsSpawnFn(t *testing.T) {
	fake := withFakeSpawn(t)
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, "", false)
	withCountingFindGameWindow(t)
	exe := withLocalGameExe(t)

	calls := 0
	orig := verifyGameSignatureFn
	verifyGameSignatureFn = func(string) signatureVerdict {
		calls++
		if calls == 1 {
			return signatureOK
		}
		return signatureVerdict{Reason: "invalid"}
	}
	t.Cleanup(func() { verifyGameSignatureFn = orig })

	login := fakeLoginSnapshotter{client: otpServer(t), session: &beanfun.Session{ServiceCode: "610074", ServiceRegion: "T9", WebToken: "z"}}
	svc := NewLauncherService(login, bgtask.New(), &fakeGamePathStore{path: exe})

	err := svc.SpawnGame(beanfun.Account{SID: "T9abc123", SSN: "1", SName: "n"})
	var le *LauncherError
	if !errors.As(err, &le) || le.Kind != KindGamePathInvalid || le.Msg != problemUnsigned {
		t.Fatalf("got %v, want *LauncherError{KindGamePathInvalid, %q}", err, problemUnsigned)
	}
	if calls != 2 {
		t.Errorf("verifyGameSignatureFn called %d times, want 2", calls)
	}
	if fake.called {
		t.Errorf("spawnFn called = true, want false")
	}
}
