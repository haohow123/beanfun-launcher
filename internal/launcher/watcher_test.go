package launcher

import (
	"context"
	"sync"
	"testing"
	"time"
)

// emittedEvent records a single eventEmitFn call so tests can assert
// the watcher emitted (or didn't) the expected payload.
type emittedEvent struct {
	name string
	data any
}

// fakeWatcher swaps the four watcher indirection vars
// (findGameWindowFn, windowPIDFn, waitForProcessExitFn, eventEmitFn)
// with test-controlled functions. Same swap-and-restore pattern as
// fakeSpawn in service_test.go.
type fakeWatcher struct {
	mu sync.Mutex

	// hwndSeq is consumed by successive findGameWindow() calls; once
	// exhausted, the last element is returned forever (so "never
	// appears" cases pass []uintptr{0}).
	hwndSeq     []uintptr
	hwndCallIdx int

	pidFn  func(hwnd uintptr) (uint32, error)
	waitFn func(ctx context.Context, pid uint32) error

	emits []emittedEvent
}

func (f *fakeWatcher) findGameWindow() uintptr {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.hwndSeq) == 0 {
		// Phase 1 isn't supposed to call findGameWindow in the
		// already-present-hwnd case; default 0 is safe if it does.
		return 0
	}
	if f.hwndCallIdx >= len(f.hwndSeq) {
		return f.hwndSeq[len(f.hwndSeq)-1]
	}
	h := f.hwndSeq[f.hwndCallIdx]
	f.hwndCallIdx++
	return h
}

func (f *fakeWatcher) emit(name string, data any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emits = append(f.emits, emittedEvent{name: name, data: data})
}

func (f *fakeWatcher) emitCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.emits)
}

func (f *fakeWatcher) firstEmit() (emittedEvent, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.emits) == 0 {
		return emittedEvent{}, false
	}
	return f.emits[0], true
}

// withFakeWatcher installs the test fakes + shortens the phase-1
// timeout/poll so timeout cases finish in milliseconds instead of
// waiting an actual minute. Restores everything on test cleanup.
func withFakeWatcher(t *testing.T, fake *fakeWatcher) {
	t.Helper()
	origWindow := findGameWindowFn
	origPID := windowPIDFn
	origWait := waitForProcessExitFn
	origEmit := eventEmitFn
	origInterval := gameWindowPollInterval
	origTimeout := gameWindowAppearTimeout

	findGameWindowFn = fake.findGameWindow
	windowPIDFn = fake.pidFn
	waitForProcessExitFn = fake.waitFn
	eventEmitFn = fake.emit
	gameWindowPollInterval = 5 * time.Millisecond
	gameWindowAppearTimeout = 50 * time.Millisecond

	t.Cleanup(func() {
		findGameWindowFn = origWindow
		windowPIDFn = origPID
		waitForProcessExitFn = origWait
		eventEmitFn = origEmit
		gameWindowPollInterval = origInterval
		gameWindowAppearTimeout = origTimeout
	})
}

func TestRunGameWatcher_HappyPath(t *testing.T) {
	done := make(chan struct{})
	fake := &fakeWatcher{
		hwndSeq: []uintptr{0, 5678}, // 0 first poll, then window appears
		pidFn: func(hwnd uintptr) (uint32, error) {
			if hwnd != 5678 {
				t.Errorf("windowPIDFn got hwnd=%d, want 5678", hwnd)
			}
			return 1234, nil
		},
		waitFn: func(_ context.Context, pid uint32) error {
			if pid != 1234 {
				t.Errorf("waitFn got pid=%d, want 1234", pid)
			}
			<-done
			return nil
		},
	}
	withFakeWatcher(t, fake)

	watcherDone := make(chan struct{})
	go func() {
		runGameWatcher(context.Background(), 0)
		close(watcherDone)
	}()

	// Give the watcher time to find the window + enter phase 2.
	time.Sleep(30 * time.Millisecond)
	close(done)
	<-watcherDone

	if got := fake.emitCount(); got != 1 {
		t.Fatalf("emitCount = %d, want 1", got)
	}
	e, _ := fake.firstEmit()
	if e.name != gameStateChangedEvent {
		t.Errorf("event name = %q, want %q", e.name, gameStateChangedEvent)
	}
	state, ok := e.data.(GameState)
	if !ok {
		t.Fatalf("event data type = %T, want GameState", e.data)
	}
	if state.Running {
		t.Errorf("event data Running = true, want false (exit emit)")
	}
}

func TestRunGameWatcher_Phase1Timeout(t *testing.T) {
	fake := &fakeWatcher{
		hwndSeq: []uintptr{0}, // never appears
		pidFn: func(uintptr) (uint32, error) {
			t.Error("windowPIDFn should not be called when window never appears")
			return 0, nil
		},
		waitFn: func(context.Context, uint32) error {
			t.Error("waitForProcessExitFn should not be called when window never appears")
			return nil
		},
	}
	withFakeWatcher(t, fake)

	runGameWatcher(context.Background(), 0)

	if got := fake.emitCount(); got != 1 {
		t.Fatalf("emitCount = %d, want 1 (timeout should still emit)", got)
	}
	e, _ := fake.firstEmit()
	if e.name != gameStateChangedEvent {
		t.Errorf("event name = %q, want %q", e.name, gameStateChangedEvent)
	}
	state, ok := e.data.(GameState)
	if !ok {
		t.Fatalf("event data type = %T, want GameState", e.data)
	}
	if state.Running {
		t.Errorf("event data Running = true, want false (timeout emit)")
	}
}

func TestRunGameWatcher_CtxCancelMidWait(t *testing.T) {
	fake := &fakeWatcher{
		hwndSeq: []uintptr{5678},
		pidFn:   func(uintptr) (uint32, error) { return 1234, nil },
		waitFn: func(ctx context.Context, _ uint32) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	withFakeWatcher(t, fake)

	ctx, cancel := context.WithCancel(context.Background())
	watcherDone := make(chan struct{})
	go func() {
		runGameWatcher(ctx, 0)
		close(watcherDone)
	}()

	// Let phase 2 begin, then cancel mid-wait.
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-watcherDone

	if got := fake.emitCount(); got != 0 {
		t.Fatalf("emitCount = %d, want 0 (ctx cancel must NOT emit)", got)
	}
}

func TestRunGameWatcher_AlreadyPresentHwnd(t *testing.T) {
	done := make(chan struct{})
	fake := &fakeWatcher{
		// Phase 1 is skipped when initialHwnd != 0; hwndSeq stays
		// empty to prove findGameWindow isn't called.
		pidFn: func(hwnd uintptr) (uint32, error) {
			if hwnd != 9999 {
				t.Errorf("windowPIDFn got hwnd=%d, want 9999 (the initialHwnd)", hwnd)
			}
			return 5555, nil
		},
		waitFn: func(_ context.Context, pid uint32) error {
			if pid != 5555 {
				t.Errorf("waitFn got pid=%d, want 5555", pid)
			}
			<-done
			return nil
		},
	}
	withFakeWatcher(t, fake)

	watcherDone := make(chan struct{})
	go func() {
		runGameWatcher(context.Background(), 9999)
		close(watcherDone)
	}()

	close(done)
	<-watcherDone

	if got := fake.emitCount(); got != 1 {
		t.Fatalf("emitCount = %d, want 1", got)
	}
	e, _ := fake.firstEmit()
	state, ok := e.data.(GameState)
	if !ok {
		t.Fatalf("event data type = %T, want GameState", e.data)
	}
	if state.Running {
		t.Errorf("event data Running = true, want false (exit emit)")
	}
}
