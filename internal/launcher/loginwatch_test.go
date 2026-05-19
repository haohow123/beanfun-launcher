package launcher

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeLoginWatch captures the Win32-layer install call and exposes
// hooks for tests to fire synthetic events. Matches the swap-and-
// restore pattern fakeSpawn uses in service_test.go.
type fakeLoginWatch struct {
	mu          sync.Mutex
	installed   bool
	pid         uint32
	loginHwnd   uintptr
	onCaret     func()
	onLoggedIn  func()
	uninstalled bool
}

func (f *fakeLoginWatch) install(
	_ context.Context,
	pid uint32,
	loginHwnd uintptr,
	onCaret func(),
	onLoggedIn func(),
) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installed = true
	f.pid = pid
	f.loginHwnd = loginHwnd
	f.onCaret = onCaret
	f.onLoggedIn = onLoggedIn
	return func() {
		f.mu.Lock()
		f.uninstalled = true
		f.mu.Unlock()
	}, nil
}

func (f *fakeLoginWatch) fireCaret() {
	f.mu.Lock()
	cb := f.onCaret
	f.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (f *fakeLoginWatch) fireLoggedIn() {
	f.mu.Lock()
	cb := f.onLoggedIn
	f.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// withFakeLoginWatch installs the test fake + shrinks formReadyDelay
// so tests don't wait the real 7-second TSF quiet zone. Restores
// everything on test cleanup.
func withFakeLoginWatch(t *testing.T) *fakeLoginWatch {
	t.Helper()
	origInstall := installLoginWatchFn
	origDelay := formReadyDelay

	fake := &fakeLoginWatch{}
	installLoginWatchFn = fake.install
	formReadyDelay = 10 * time.Millisecond

	t.Cleanup(func() {
		installLoginWatchFn = origInstall
		formReadyDelay = origDelay
	})
	return fake
}

// waitFor polls a condition until true or deadline; fails the test if
// the deadline expires. Avoids fixed sleeps that flake on slow CI.
func waitFor(t *testing.T, deadline time.Duration, cond func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", deadline)
}

// recvWithTimeout returns the next channel value or fails the test
// with a descriptive message.
func recvWithTimeout(t *testing.T, ch <-chan loginWatchEvent, timeout time.Duration, what string) loginWatchEvent {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatalf("%s: channel closed without event", what)
		}
		return ev
	case <-time.After(timeout):
		t.Fatalf("%s: no event within %s", what, timeout)
		return 0
	}
}

// noEventWithin asserts no event arrives on ch for the given duration.
func noEventWithin(t *testing.T, ch <-chan loginWatchEvent, duration time.Duration, what string) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			return // channel closed = no more events, fine
		}
		t.Fatalf("%s: unexpected event %v", what, ev)
	case <-time.After(duration):
		// expected
	}
}

func TestRunLoginWatcher_HappyPath(t *testing.T) {
	fake := withFakeLoginWatch(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := runLoginWatcher(ctx, 0x1234, 5555)

	// Wait until install completes (fake.install runs synchronously
	// inside the watcher goroutine).
	waitFor(t, 100*time.Millisecond, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.installed
	})

	// Wait past the formReadyDelay quiet zone, then fire a burst.
	time.Sleep(15 * time.Millisecond)
	for range caretBurstThreshold {
		fake.fireCaret()
	}

	if ev := recvWithTimeout(t, events, 100*time.Millisecond, "formReady"); ev != formReady {
		t.Errorf("first event = %v, want formReady", ev)
	}

	fake.fireLoggedIn()
	if ev := recvWithTimeout(t, events, 100*time.Millisecond, "loggedIn"); ev != loggedIn {
		t.Errorf("second event = %v, want loggedIn", ev)
	}
}

func TestRunLoginWatcher_TSFNoiseIgnored(t *testing.T) {
	// Fire 2 caret events INSIDE the formReadyDelay quiet zone (the
	// TSF init noise we want to ignore). Then fire a proper burst
	// AFTER the quiet zone. Only the post-zone burst should trigger
	// formReady.
	fake := withFakeLoginWatch(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := runLoginWatcher(ctx, 0x1234, 5555)
	waitFor(t, 100*time.Millisecond, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.installed
	})

	// Fire 2 caret events during the 10ms quiet zone — should be ignored.
	fake.fireCaret()
	fake.fireCaret()
	noEventWithin(t, events, 20*time.Millisecond, "quiet-zone caret events")

	// Wait past the quiet zone and fire a real burst.
	time.Sleep(15 * time.Millisecond)
	for range caretBurstThreshold {
		fake.fireCaret()
	}
	if ev := recvWithTimeout(t, events, 100*time.Millisecond, "post-zone burst"); ev != formReady {
		t.Errorf("event = %v, want formReady", ev)
	}
}

func TestRunLoginWatcher_FormNeverAppears(t *testing.T) {
	// No caret events at all — formReady never fires. Verify by
	// cancelling ctx and ensuring the channel closes cleanly without
	// emitting.
	withFakeLoginWatch(t)
	ctx, cancel := context.WithCancel(context.Background())

	events := runLoginWatcher(ctx, 0x1234, 5555)
	noEventWithin(t, events, 30*time.Millisecond, "no events expected")

	cancel()
	// After cancel, the channel should close. Pull until closed.
	closed := false
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case _, ok := <-events:
			if !ok {
				closed = true
			}
		case <-time.After(5 * time.Millisecond):
		}
		if closed {
			break
		}
	}
	if !closed {
		t.Fatal("channel didn't close after ctx cancel")
	}
}

func TestRunLoginWatcher_NoTransitionAfterFormReady(t *testing.T) {
	// formReady fires but no loggedIn — caller's responsibility to
	// timeout. We verify here that we don't emit loggedIn
	// spontaneously and the channel stays open until ctx cancel.
	fake := withFakeLoginWatch(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := runLoginWatcher(ctx, 0x1234, 5555)
	waitFor(t, 100*time.Millisecond, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.installed
	})

	time.Sleep(15 * time.Millisecond)
	for range caretBurstThreshold {
		fake.fireCaret()
	}
	if ev := recvWithTimeout(t, events, 100*time.Millisecond, "formReady"); ev != formReady {
		t.Fatalf("event = %v, want formReady", ev)
	}

	// No loggedIn callback fired; no loggedIn event.
	noEventWithin(t, events, 50*time.Millisecond, "no loggedIn without fireLoggedIn")
}

func TestRunLoginWatcher_BurstThresholdEdge(t *testing.T) {
	// caretBurstThreshold-1 events should NOT fire; the threshold-th
	// event should. Verifies the sliding window count is strict ≥.
	fake := withFakeLoginWatch(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := runLoginWatcher(ctx, 0x1234, 5555)
	waitFor(t, 100*time.Millisecond, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.installed
	})

	time.Sleep(15 * time.Millisecond)

	// Fire one less than threshold; no formReady expected.
	for range caretBurstThreshold - 1 {
		fake.fireCaret()
	}
	noEventWithin(t, events, 30*time.Millisecond, "below-threshold")

	// One more event reaches threshold → formReady.
	fake.fireCaret()
	if ev := recvWithTimeout(t, events, 100*time.Millisecond, "threshold-met"); ev != formReady {
		t.Errorf("event = %v, want formReady", ev)
	}
}

func TestRunLoginWatcher_BurstWindowExpires(t *testing.T) {
	// Events spaced wider than caretBurstWindow apart should NOT
	// count as a burst. Stretches caretBurstWindow temporarily to
	// 5 ms so the test is fast.
	fake := withFakeLoginWatch(t)
	origWindow := caretBurstWindow
	caretBurstWindow = 5 * time.Millisecond
	t.Cleanup(func() { caretBurstWindow = origWindow })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := runLoginWatcher(ctx, 0x1234, 5555)
	waitFor(t, 100*time.Millisecond, func() bool {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		return fake.installed
	})

	time.Sleep(15 * time.Millisecond)

	// Fire threshold events spaced 10 ms apart — outside the 5 ms
	// window → no burst.
	for range caretBurstThreshold {
		fake.fireCaret()
		time.Sleep(10 * time.Millisecond)
	}
	noEventWithin(t, events, 30*time.Millisecond, "spaced events shouldn't burst")
}
