// Package diag records what the process knew immediately before an
// unrecoverable webview error kills it.
//
// go-webview2 calls os.Exit(1) on any MoveFocus error and prints the
// error itself with fmt.Printf, which Windows -H windowsgui builds
// detach — so the one datum that explains the crash is the one that
// never reaches launcher.log. Installing WebviewError as
// application.Options.ErrorHandler puts it there instead.
package diag

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
)

// nowFn is the clock seam; tests pin it.
var nowFn = time.Now

// framesFn is the stack-capture seam so tests can drive either
// classification branch with a synthetic list.
var framesFn = capturedFrames

var (
	mu        sync.Mutex
	startedAt time.Time
	readyAt   time.Time
)

// MarkStart records the process start instant.
func MarkStart() {
	mu.Lock()
	defer mu.Unlock()
	startedAt = nowFn()
}

// NoteRuntimeReady stamps the first time the webview reported ready.
// WindowRuntimeReady fires again after a reload; the first one is the
// interesting one, so later calls are ignored.
func NoteRuntimeReady() {
	mu.Lock()
	defer mu.Unlock()
	if readyAt.IsZero() {
		readyAt = nowFn()
	}
}

// elapsed reports time since MarkStart, or zero if it was never called.
func elapsed() time.Duration {
	if startedAt.IsZero() {
		return 0
	}
	return nowFn().Sub(startedAt)
}

// fatalFrame is go-webview2's function that calls us and then exits.
const fatalFrame = "edge.(*Chromium).errorCallback"

// capturedFrames formats the stack above its own caller.
func capturedFrames() []string {
	pcs := make([]uintptr, 64)
	n := runtime.Callers(2, pcs)
	if n == 0 {
		return nil
	}
	frames := runtime.CallersFrames(pcs[:n])
	var out []string
	for {
		f, more := frames.Next()
		out = append(out, fmt.Sprintf("%s %s:%d", f.Function, filepath.Base(f.File), f.Line))
		if !more {
			return out
		}
	}
}

// isFatal reports whether this error is the about-to-exit kind; Wails
// routes its own ordinary errors through the same ErrorHandler with
// nothing in the signature to tell them apart.
func isFatal(frames []string) bool {
	for _, f := range frames {
		if strings.Contains(f, fatalFrame) {
			return true
		}
	}
	return false
}

// WebviewError is installed as application.Options.ErrorHandler.
func WebviewError(err error) {
	// Scrubbed because this handler receives errors from outside this
	// repo: Wails routes its own through the same option, and a future
	// version's message shape is not something we can audit ahead of it.
	msg := scrubbed(err)

	frames := framesFn()
	if !isFatal(frames) {
		slog.Warn("wails error", "err", msg)
		return
	}

	mu.Lock()
	since := elapsed()
	ready := !readyAt.IsZero()
	var readyIn time.Duration
	if ready && !startedAt.IsZero() {
		readyIn = readyAt.Sub(startedAt)
	}
	mu.Unlock()

	attrs := []any{
		"err", msg,
		"since_start", since,
		"runtime_ready", ready,
		"frames", len(frames),
	}
	if ready {
		attrs = append(attrs, "runtime_ready_at", readyIn)
	}
	slog.Error("webview fatal: process will exit", attrs...)
	for i, f := range frames {
		slog.Error("  stack", "n", i+1, "frame", f)
	}
}

// scrubbed renders err through the credential backstop in
// internal/beanfun, tolerating a nil error.
func scrubbed(err error) string {
	if err == nil {
		return "<nil>"
	}
	return beanfun.ScrubCredentials(err.Error())
}
