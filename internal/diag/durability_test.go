package diag

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRecordSurvivesExit is the premise the whole package rests on: the
// record must be on disk even though go-webview2 calls os.Exit(1) on the
// line after our handler returns. Runs itself as a child process because
// os.Exit cannot be observed in-process.
func TestRecordSurvivesExit(t *testing.T) {
	if path := os.Getenv("DIAG_EXIT_CHILD"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			os.Exit(2)
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})))
		framesFn = func() []string { return []string{fatalFrame + " chromium.go:151"} }
		MarkStart()
		WebviewError(errors.New("synthetic MoveFocus failure"))
		os.Exit(1)
	}

	path := filepath.Join(t.TempDir(), "out.log")
	cmd := exec.Command(os.Args[0], "-test.run=^TestRecordSurvivesExit$")
	cmd.Env = append(os.Environ(), "DIAG_EXIT_CHILD="+path)

	var ee *exec.ExitError
	err := cmd.Run()
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("child: got %v, want exit status 1", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("child wrote no file: %v", err)
	}
	if !strings.Contains(string(b), "synthetic MoveFocus failure") {
		t.Errorf("record did not survive os.Exit; file was:\n%s", b)
	}
	if !strings.Contains(string(b), "webview fatal") {
		t.Errorf("child took the non-fatal branch; file was:\n%s", b)
	}
}
