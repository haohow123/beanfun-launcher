package launcher

import (
	"os"
	"strings"
	"testing"
)

// TestMainInstallsEmitterBeforeService guards the startup path where NewLauncherService attaches a
// watcher to an already-running game and that watcher emits before the app is ready.
func TestMainInstallsEmitterBeforeService(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	src := string(b)
	if len(src) < 1000 {
		t.Fatalf("main.go is only %d bytes — the path is probably wrong", len(src))
	}
	set := strings.Index(src, "launcher.SetEventEmitter(emit)")
	svc := strings.Index(src, "launcher.NewLauncherService(")
	if set < 0 || svc < 0 {
		t.Fatalf("main.go is missing SetEventEmitter (%d) or NewLauncherService (%d)", set, svc)
	}
	if set > svc {
		t.Errorf("SetEventEmitter runs after NewLauncherService, so a startup watcher would emit through the no-op")
	}
}
