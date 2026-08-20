package diag

import (
	"os"
	"strings"
	"testing"
)

// wiringRequired are the lines in main.go that install this package. CI
// scopes vet, lint and test to ./internal/... and only cross-compiles the
// root package, so a source scan is the only thing that catches the
// wiring being dropped.
var wiringRequired = []string{
	"diag.MarkStart()",
	"ErrorHandler: diag.WebviewError",
	"RegisterHook(events.Common.WindowRuntimeReady",
	"diag.NoteRuntimeReady()",
}

func TestMainWiring(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	// Guards against a wrong relative path passing vacuously.
	if len(src) < 1000 {
		t.Fatalf("main.go is only %d bytes — the path is probably wrong", len(src))
	}
	for _, want := range wiringRequired {
		if !strings.Contains(string(src), want) {
			t.Errorf("main.go no longer contains %q; the diagnostic wiring is gone", want)
		}
	}
}
