package diag

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// resetForTest restores package state and the two seams; callers must
// not be parallel because both are package-level.
func resetForTest(t *testing.T) *bytes.Buffer {
	t.Helper()
	origNow, origFrames, origLogger := nowFn, framesFn, slog.Default()
	mu.Lock()
	startedAt = time.Time{}
	mu.Unlock()

	buf := &bytes.Buffer{}
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() {
		nowFn, framesFn = origNow, origFrames
		slog.SetDefault(origLogger)
		mu.Lock()
		startedAt = time.Time{}
		mu.Unlock()
	})
	return buf
}

func TestIsFatal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		frames []string
		want   bool
	}{
		{"fatal path", []string{"main.main main.go:30", "edge.(*Chromium).errorCallback chromium.go:151"}, true},
		{"ordinary wails error", []string{"application.(*App).handleError application.go:445"}, false},
		{"empty", nil, false},
		{"substring only, no match", []string{"edge.(*Chromium).Focus chromium.go:572"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFatal(tt.frames); got != tt.want {
				t.Errorf("isFatal() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestFatalFrameLiteral pins the dependency's function name against a
// literal. Every other test builds its synthetic frames from fatalFrame
// itself, so they move with it and cannot catch a typo here.
func TestFatalFrameLiteral(t *testing.T) {
	t.Parallel()
	const want = "edge.(*Chromium).errorCallback"
	if fatalFrame != want {
		t.Errorf("fatalFrame = %q, want %q (webview2 pkg/edge/chromium.go:150)", fatalFrame, want)
	}
}

func TestWebviewError_FatalRecord(t *testing.T) {
	buf := resetForTest(t)

	base := time.Date(2026, 8, 20, 3, 46, 30, 0, time.UTC)
	nowFn = func() time.Time { return base }
	MarkStart()
	nowFn = func() time.Time { return base.Add(4200 * time.Millisecond) }
	framesFn = func() []string {
		return []string{fatalFrame + " chromium.go:151", "edge.(*Chromium).Focus chromium.go:572"}
	}

	WebviewError(errors.New("MoveFocus: 0x8000FFFF"))

	out := buf.String()
	for _, want := range []string{
		"webview fatal: process will exit",
		"MoveFocus: 0x8000FFFF",
		"since_start=4.2s",
		"frames=2",
		"chromium.go:151",
		"chromium.go:572",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("record missing %q:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "msg=\"  stack\""); got != 2 {
		t.Errorf("stack lines = %d, want 2:\n%s", got, out)
	}
}

// TestWebviewError_OrdinaryError guards the classification: Wails routes
// its own errors through the same handler, and recording each as a
// multi-frame crash would bury the real one.
func TestWebviewError_OrdinaryError(t *testing.T) {
	buf := resetForTest(t)
	framesFn = func() []string {
		return []string{"application.(*App).handleError application.go:445"}
	}

	WebviewError(errors.New("some ordinary wails failure"))

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("want a WARN line:\n%s", out)
	}
	if strings.Contains(out, "webview fatal") {
		t.Errorf("ordinary error recorded as fatal:\n%s", out)
	}
	if strings.Contains(out, "stack") {
		t.Errorf("ordinary error emitted a stack:\n%s", out)
	}
	if got := strings.Count(out, "\n"); got != 1 {
		t.Errorf("emitted %d lines, want exactly 1:\n%s", got, out)
	}
}

// TestCapturedFrames_IncludesCaller pins that the real capture keeps our
// own caller, which is what isFatal relies on in production.
func TestCapturedFrames_IncludesCaller(t *testing.T) {
	t.Parallel()
	frames := capturedFrames()
	if len(frames) == 0 {
		t.Fatal("capturedFrames returned nothing")
	}
	if !strings.Contains(frames[0], "TestCapturedFrames_IncludesCaller") {
		t.Errorf("first frame = %q, want this test function", frames[0])
	}
}

// TestWebviewError_ScrubsCredentials guards the backstop: this handler is
// the one sink in the codebase that logs an error produced outside the
// repo, so nothing upstream of it can be relied on to have scrubbed it.
func TestWebviewError_ScrubsCredentials(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		absent  string
		present string
	}{
		{
			name:    "fatal branch",
			err:     errors.New(`Get "https://tw.newlogin.beanfun.com/login/id-pass.aspx?pSKey=LEAKED_KEY_9999": refused`),
			absent:  "LEAKED_KEY_9999",
			present: "pSKey=<redacted>",
		},
		{
			name:    "web_token in an ordinary wails error",
			err:     errors.New(`marshalling failed for https://tw.beanfun.com/x?web_token=LEAKED_TOKEN_8888`),
			absent:  "LEAKED_TOKEN_8888",
			present: "web_token=<redacted>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := resetForTest(t)
			if tt.name == "fatal branch" {
				framesFn = func() []string { return []string{fatalFrame + " chromium.go:151"} }
			} else {
				framesFn = func() []string { return []string{"application.(*App).error application.go:540"} }
			}

			WebviewError(tt.err)

			out := buf.String()
			if strings.Contains(out, tt.absent) {
				t.Errorf("credential reached the log:\n%s", out)
			}
			if !strings.Contains(out, tt.present) {
				t.Errorf("missing %q:\n%s", tt.present, out)
			}
		})
	}
}

func TestScrubbed_NilError(t *testing.T) {
	t.Parallel()
	if got := scrubbed(nil); got != "<nil>" {
		t.Errorf("scrubbed(nil) = %q, want %q", got, "<nil>")
	}
}
