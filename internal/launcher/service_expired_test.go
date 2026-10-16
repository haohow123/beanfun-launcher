package launcher

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

// swappableLogin hands out client and, like LoginService, only ends the session when the caller's client is still current.
type swappableLogin struct {
	mu      sync.Mutex
	current *beanfun.BeanfunClient
	ended   []*beanfun.BeanfunClient
	cleared bool
}

func (l *swappableLogin) Snapshot() (*beanfun.BeanfunClient, *beanfun.Session) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.current, &beanfun.Session{ServiceCode: "x", ServiceRegion: "y", WebToken: "z"}
}

func (l *swappableLogin) EndSessionIfCurrent(c *beanfun.BeanfunClient) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ended = append(l.ended, c)
	if l.current != c {
		return false
	}
	l.current = nil
	l.cleared = true
	return true
}

func (l *swappableLogin) swap(c *beanfun.BeanfunClient) {
	l.mu.Lock()
	l.current = c
	l.mu.Unlock()
}

// TestLauncherService_SessionExpired_EndsOnlyTheSnapshotClient pins all three FetchOTP call sites passing the Snapshot client, so a login that replaces it mid-request survives.
func TestLauncherService_SessionExpired_EndsOnlyTheSnapshotClient(t *testing.T) {
	withEnv(t, gameExeEnvVar, "")
	withFakeRegistry(t, "", false)
	withFakeSpawn(t)
	exe := withLocalGameExe(t)
	withWindowImage(t, func(uintptr) (uint32, string, error) { return 7, "MapleStory.exe", nil })
	orig := findGameWindowFn
	t.Cleanup(func() { findGameWindowFn = orig })

	calls := map[string]func(*LauncherService, beanfun.Account) error{
		"GetOTP": func(s *LauncherService, a beanfun.Account) error { _, err := s.GetOTP(a); return err },
		"Launch": func(s *LauncherService, a beanfun.Account) error { _, err := s.Launch(a); return err },
		"SpawnGame": func(s *LauncherService, a beanfun.Account) error {
			return s.SpawnGame(a)
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			findGameWindowFn = func() uintptr {
				if name == "Launch" {
					return 1
				}
				return 0
			}
			var login *swappableLogin
			var newer *beanfun.BeanfunClient
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				login.swap(newer)
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(`<div id="divMsg">尚未登入，請重新登入</div>`))
			}))
			t.Cleanup(srv.Close)
			base, err := url.Parse(srv.URL + "/")
			if err != nil {
				t.Fatal(err)
			}
			eps := beanfun.Endpoints{LoginBase: base, PortalBase: base, NewloginBase: base}
			older, err := beanfun.NewBeanfunClientWithEndpoints(eps)
			if err != nil {
				t.Fatal(err)
			}
			newer, err = beanfun.NewBeanfunClientWithEndpoints(eps)
			if err != nil {
				t.Fatal(err)
			}
			login = &swappableLogin{current: older}
			svc := NewLauncherService(login, bgtask.New(), &fakeGamePathStore{path: exe})

			err = call(svc, beanfun.Account{SID: "s", SSN: "1", SName: "n"})
			var le *beanfun.LoginError
			if !errors.As(err, &le) || le.Kind != beanfun.KindLoginRequired {
				t.Fatalf("err = %v, want ErrLoginRequired", err)
			}
			if len(login.ended) != 1 || login.ended[0] != older {
				t.Errorf("EndSessionIfCurrent got %v, want exactly the Snapshot client", login.ended)
			}
			if login.cleared || login.current != newer {
				t.Error("the newer login was cleared")
			}
		})
	}
}
