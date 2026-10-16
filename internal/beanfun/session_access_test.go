package beanfun

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

// Every exported method is bound to the frontend WebView.
func TestLoginService_BoundMethodSet(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(&LoginService{})
	var got []string
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	slices.Sort(got)
	want := []string{"GetAccounts", "QRStatusNow", "Reset", "StartQRLogin"}
	if !slices.Equal(got, want) {
		t.Errorf("exported methods = %v, want exactly %v", got, want)
	}
}

func TestEndSessionIfCurrent_NilClientIsNoOp(t *testing.T) {
	t.Parallel()
	s := NewLoginService(bgtask.New())
	calls := 0
	s.OnSessionEnded = func(SessionEnded) { calls++ }
	before := s.qrGeneration

	if s.endSessionIfCurrent(nil) {
		t.Error("endSessionIfCurrent(nil) = true, want false")
	}
	if s.qrGeneration != before {
		t.Errorf("qrGeneration = %d, want unchanged %d", s.qrGeneration, before)
	}
	if calls != 0 {
		t.Errorf("OnSessionEnded called %d times, want 0", calls)
	}
}

func TestSessionAccess_DelegatesToService(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NewServeMux())
	t.Cleanup(srv.Close)
	eps := stubEndpoints(t, srv)
	s := NewLoginServiceWithEndpoints(eps, bgtask.New())
	client, err := NewBeanfunClientWithEndpoints(eps)
	if err != nil {
		t.Fatal(err)
	}
	sess := &Session{}
	s.mu.Lock()
	s.client = client
	s.session = sess
	s.mu.Unlock()
	ended := 0
	s.OnSessionEnded = func(SessionEnded) { ended++ }
	acc := NewSessionAccess(s)

	if cl, se := acc.Snapshot(); cl != client || se != sess {
		t.Errorf("Snapshot = (%v, %v), want the service's client and session", cl, se)
	}
	if !acc.EndSessionIfCurrent(client) {
		t.Error("EndSessionIfCurrent(current) = false, want true")
	}
	if ended != 1 {
		t.Errorf("OnSessionEnded called %d times, want 1", ended)
	}
	if cl, se := s.snapshot(); cl != nil || se != nil {
		t.Errorf("snapshot after end = (%v, %v), want cleared", cl, se)
	}
}
