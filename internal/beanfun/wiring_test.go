package beanfun

import (
	"os"
	"strings"
	"testing"
)

// mainWiringRequired are the lines in main.go that connect the QR state feed to the Wails event
// bus. CI scopes vet, lint and test to ./internal/... and only cross-compiles the root package, so
// a source scan is the only thing that catches the wiring being dropped.
var mainWiringRequired = []string{
	"emit(beanfun.QRStateChangedEvent",
	"loginSvc.OnQRStateChanged =",
	"emit(beanfun.SessionEndedEvent",
	"loginSvc.OnSessionEnded =",
	"readyApp.Load()",
	"readyApp.Store(app)",
}

// application.Get() is non-nil while application.New is still building the event processor, so an
// emit guarded by it can panic at startup.
const mainWiringForbidden = "application.Get()"

func TestMainEmitWiring(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Guards against a wrong relative path passing vacuously.
	if len(src) < 1000 {
		t.Fatalf("main.go is only %d bytes — the path is probably wrong", len(src))
	}
	for _, want := range mainWiringRequired {
		if !strings.Contains(string(src), want) {
			t.Errorf("main.go no longer contains %q; the QR state push is disconnected", want)
		}
	}
	if strings.Contains(string(src), mainWiringForbidden) {
		t.Errorf("main.go uses %s again; emit through readyApp instead", mainWiringForbidden)
	}
	newAt := strings.Index(string(src), "application.New(")
	storeAt := strings.Index(string(src), "readyApp.Store(app)")
	runAt := strings.Index(string(src), "app.Run()")
	if newAt < 0 || runAt < 0 || storeAt < newAt || storeAt > runAt {
		t.Errorf("readyApp.Store(app) (at %d) must sit between application.New (at %d) and app.Run (at %d)", storeAt, newAt, runAt)
	}
}
