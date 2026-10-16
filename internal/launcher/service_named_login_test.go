package launcher

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

type endedCall struct {
	id     string
	client *beanfun.BeanfunClient
}

// recordingLogin serves a distinct client per login id and records every EndSessionIfCurrent call.
type recordingLogin struct {
	byID  map[string]*beanfun.BeanfunClient
	ended []endedCall
}

func (r *recordingLogin) Snapshot(id string) (*beanfun.BeanfunClient, *beanfun.Session) {
	return r.byID[id], &beanfun.Session{ServiceCode: "610074", ServiceRegion: "T9", WebToken: "z"}
}

func (r *recordingLogin) EndSessionIfCurrent(id string, c *beanfun.BeanfunClient) bool {
	r.ended = append(r.ended, endedCall{id, c})
	return true
}

func (r *recordingLogin) HasLogin() bool { return true }

func expiredOTPServer(t *testing.T) *beanfun.BeanfunClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<div id="divMsg">尚未登入，請重新登入</div>`))
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

func TestLauncherService_SpawnGameAndGetOTP_UseTheNamedLoginsClient(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, "", false)
	errSpawn := errors.New("spawn stubbed")
	withFakeSpawn(t).err = errSpawn
	exe := withLocalGameExe(t)
	withWindowImage(t, func(uintptr) (uint32, string, error) { return 7, "MapleStory.exe", nil })
	origFind := findGameWindowFn
	findGameWindowFn = func() uintptr { return 0 }
	t.Cleanup(func() { findGameWindowFn = origFind })

	calls := map[string]func(*LauncherService) error{
		"SpawnGame": func(s *LauncherService) error { return s.SpawnGame("2", testAccount) },
		"GetOTP":    func(s *LauncherService) error { _, err := s.GetOTP("2", testAccount); return err },
	}
	for name, call := range calls {
		t.Run(name+"/fetches with login 2", func(t *testing.T) {
			var hits1, hits2 atomic.Int32
			login := &recordingLogin{byID: map[string]*beanfun.BeanfunClient{
				"1": countingOTPServer(t, &hits1),
				"2": countingOTPServer(t, &hits2),
			}}
			svc := NewLauncherService(login, bgtask.New(), &fakeGamePathStore{path: exe})

			// SpawnGame stops at the stubbed spawn, after the OTP fetch, so no watcher goroutine outlives the test.
			if err := call(svc); err != nil && !errors.Is(err, errSpawn) {
				t.Fatalf("%s = %v, want nil or the stubbed spawn error", name, err)
			}
			if hits1.Load() != 0 || hits2.Load() == 0 {
				t.Errorf("hits login1 = %d, login2 = %d, want 0 and >0", hits1.Load(), hits2.Load())
			}
		})
		t.Run(name+"/expiry ends login 2", func(t *testing.T) {
			login := &recordingLogin{byID: map[string]*beanfun.BeanfunClient{
				"1": expiredOTPServer(t),
				"2": expiredOTPServer(t),
			}}
			svc := NewLauncherService(login, bgtask.New(), &fakeGamePathStore{path: exe})

			err := call(svc)
			var le *beanfun.LoginError
			if !errors.As(err, &le) || le.Kind != beanfun.KindLoginRequired {
				t.Fatalf("err = %v, want ErrLoginRequired", err)
			}
			if len(login.ended) != 1 || login.ended[0].id != "2" || login.ended[0].client != login.byID["2"] {
				t.Errorf("EndSessionIfCurrent got %+v, want exactly login 2 with its own client", login.ended)
			}
		})
	}
}
