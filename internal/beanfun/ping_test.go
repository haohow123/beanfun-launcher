package beanfun

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
)

// Reply shapes copied verbatim from the live endpoint.
const (
	echoLoggedIn        = `BeanFunBlock.EchoTokenResult({ResultCode:1, ResultDesc: "User is logged in.", MainAccountID : "X" });`
	echoLoggedOut       = `BeanFunBlock.EchoTokenResult({ResultCode:0, ResultDesc: "User is logged out.", MainAccountID : "" });`
	echoLoggedOutWithID = `BeanFunBlock.EchoTokenResult({ResultCode:0, ResultDesc: "User is logged out.", MainAccountID : "X" });`
)

func writeJS(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/x-javascript; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func TestBeanfunClient_Ping(t *testing.T) {
	t.Parallel()
	const echoPath = "/beanfun_block/generic_handlers/echo_token.ashx"
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		closeSrv bool
		wantKind LoginErrorKind
		wantNil  bool
	}{
		{name: "logged in", handler: func(w http.ResponseWriter, _ *http.Request) { writeJS(w, echoLoggedIn) }, wantNil: true},
		{name: "logged out", handler: func(w http.ResponseWriter, _ *http.Request) { writeJS(w, echoLoggedOut) }, wantKind: KindSessionExpired},
		{name: "logged out first reply still carries the id", handler: func(w http.ResponseWriter, _ *http.Request) { writeJS(w, echoLoggedOutWithID) }, wantKind: KindSessionExpired},
		{name: "unrelated 200 page is not a logout", handler: func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, "<html>unrelated</html>") }, wantKind: KindHTTP},
		{name: "ResultCode outside EchoTokenResult is not a logout", handler: func(w http.ResponseWriter, _ *http.Request) { writeHTML(w, "<script>var x={ResultCode:0}</script>") }, wantKind: KindHTTP},
		{name: "unknown result code is not a logout", handler: func(w http.ResponseWriter, _ *http.Request) {
			writeJS(w, `BeanFunBlock.EchoTokenResult({ResultCode:-1, ResultDesc: "x", MainAccountID : "" });`)
		}, wantKind: KindHTTP},
		{name: "server error", handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }, wantKind: KindHTTP},
		{name: "ip blocked", handler: func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/TW/BlockIPMessage.htm", http.StatusFound)
		}, wantKind: KindIPBlocked},
		{name: "server closed", handler: func(http.ResponseWriter, *http.Request) {}, closeSrv: true, wantKind: KindHTTP},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var hits int32
			var sawQuery atomic.Value
			mux := http.NewServeMux()
			mux.HandleFunc(echoPath, func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&hits, 1)
				sawQuery.Store(r.URL.RawQuery)
				tt.handler(w, r)
			})
			mux.HandleFunc("/TW/BlockIPMessage.htm", func(w http.ResponseWriter, _ *http.Request) {
				writeHTML(w, "<html>locked</html>")
			})
			c, srv := newTestClient(t, mux)
			if tt.closeSrv {
				srv.Close()
			}

			err := c.Ping(context.Background())
			if tt.wantNil {
				if err != nil {
					t.Fatalf("Ping err = %v, want nil", err)
				}
			} else {
				var le *LoginError
				if !errors.As(err, &le) || le.Kind != tt.wantKind {
					t.Fatalf("Ping err = %v, want kind %v", err, tt.wantKind)
				}
			}
			if tt.closeSrv {
				return
			}
			if got := atomic.LoadInt32(&hits); got != 1 {
				t.Errorf("hits = %d, want 1", got)
			}
			if q, _ := sawQuery.Load().(string); q != "webtoken=1" {
				t.Errorf("RawQuery = %q, want webtoken=1", q)
			}
		})
	}
}

// TestEchoTokenResultRE pins the pattern against the captured shapes plus the spacing variants it tolerates.
func TestEchoTokenResultRE(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{echoLoggedIn, "1"},
		{echoLoggedOut, "0"},
		{`EchoTokenResult({ResultCode : -1})`, "-1"},
		{`EchoTokenResult({ResultCode:12})`, "12"},
		{`<html>unrelated</html>`, ""},
		{`<script>var x={ResultCode:0}</script>`, ""},
		{`EchoTokenResult( { ResultCode : 1 })`, "1"},
	}
	for _, tt := range tests {
		got := ""
		if m := echoTokenResultRE.FindStringSubmatch(tt.in); m != nil {
			got = m[1]
		}
		if got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.in, got, tt.want)
		}
	}
}
