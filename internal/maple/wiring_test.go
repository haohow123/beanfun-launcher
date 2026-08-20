package maple

import (
	"os"
	"strings"
	"testing"
)

// mainWiringRequired are the lines in main.go that connect this package's state feed to the Wails
// event bus. CI scopes vet, lint and test to ./internal/... and only cross-compiles the root
// package, so a source scan is the only thing that catches the wiring being dropped.
var mainWiringRequired = []string{
	"maple.StatusChangedEvent",
	"application.Get()",
	"notifyServerOnline, emitStatusChanged)",
}

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
			t.Errorf("main.go no longer contains %q; the status push is disconnected", want)
		}
	}
}
