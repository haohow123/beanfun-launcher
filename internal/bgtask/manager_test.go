package bgtask

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor polls cond every tick until it returns true or the
// deadline expires. Asserts on failure. Used in place of fixed
// sleeps so tests stay fast on green machines and don't flake on
// slow CI runners.
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

func TestHeartbeat_FiresImmediatelyWhenFirstDelayZero(t *testing.T) {
	mgr := New()
	var calls int32
	mgr.Heartbeat("h", 0, func(_ context.Context) time.Duration {
		atomic.AddInt32(&calls, 1)
		return 0 // stop after first call
	})
	waitFor(t, 100*time.Millisecond, func() bool {
		return atomic.LoadInt32(&calls) == 1
	})
}

func TestHeartbeat_FixedCadence(t *testing.T) {
	mgr := New()
	defer mgr.StopAll()
	var calls int32
	mgr.Heartbeat("h", 0, func(_ context.Context) time.Duration {
		atomic.AddInt32(&calls, 1)
		return 5 * time.Millisecond
	})
	// Expect at least 3 calls within ~30 ms (immediate + ~2 ticks).
	waitFor(t, 200*time.Millisecond, func() bool {
		return atomic.LoadInt32(&calls) >= 3
	})
}

func TestHeartbeat_AdaptiveDelay(t *testing.T) {
	mgr := New()
	defer mgr.StopAll()
	var (
		calls       int32
		delays      []time.Duration
		delaysMu    sync.Mutex
		nextChoices = []time.Duration{5 * time.Millisecond, 10 * time.Millisecond, 0}
	)
	mgr.Heartbeat("h", 0, func(_ context.Context) time.Duration {
		n := atomic.AddInt32(&calls, 1)
		idx := int(n) - 1
		if idx >= len(nextChoices) {
			return 0
		}
		d := nextChoices[idx]
		delaysMu.Lock()
		delays = append(delays, d)
		delaysMu.Unlock()
		return d
	})
	waitFor(t, 200*time.Millisecond, func() bool {
		return atomic.LoadInt32(&calls) >= 3
	})
	delaysMu.Lock()
	defer delaysMu.Unlock()
	if got := len(delays); got != 3 {
		t.Fatalf("delays recorded = %d, want 3", got)
	}
}

func TestHeartbeat_StopByName(t *testing.T) {
	mgr := New()
	var calls int32
	mgr.Heartbeat("h", 0, func(_ context.Context) time.Duration {
		atomic.AddInt32(&calls, 1)
		return 5 * time.Millisecond
	})
	waitFor(t, 100*time.Millisecond, func() bool {
		return atomic.LoadInt32(&calls) >= 2
	})
	mgr.Stop("h")
	got := atomic.LoadInt32(&calls)
	time.Sleep(30 * time.Millisecond)
	if grew := atomic.LoadInt32(&calls); grew > got+1 {
		// +1 tolerance: a tick may already be in flight when Stop fires.
		t.Fatalf("Stop didn't halt heartbeat: was %d, now %d", got, grew)
	}
	if names := mgr.List(); len(names) != 0 {
		t.Errorf("List after Stop = %v, want empty", names)
	}
}

func TestHeartbeat_ReturningZeroStops(t *testing.T) {
	mgr := New()
	calls := 0
	mgr.Heartbeat("h", 0, func(_ context.Context) time.Duration {
		calls++
		return 0 // stop immediately after first call
	})
	waitFor(t, 100*time.Millisecond, func() bool {
		// goroutine should have exited and self-removed from registry
		return len(mgr.List()) == 0
	})
	if calls != 1 {
		t.Errorf("calls = %d, want 1", calls)
	}
}

func TestWatcher_RunsOnceUntilExit(t *testing.T) {
	mgr := New()
	done := make(chan struct{})
	var fired int32
	mgr.Watcher("w", func(_ context.Context) {
		atomic.AddInt32(&fired, 1)
		<-done
	})
	waitFor(t, 100*time.Millisecond, func() bool {
		return atomic.LoadInt32(&fired) == 1
	})
	// Active in registry while running.
	if names := mgr.List(); len(names) != 1 || names[0] != "w" {
		t.Errorf("List = %v, want [\"w\"]", names)
	}
	close(done)
	waitFor(t, 100*time.Millisecond, func() bool {
		return len(mgr.List()) == 0
	})
}

func TestWatcher_StopCancelsContext(t *testing.T) {
	mgr := New()
	gotCancel := make(chan struct{})
	mgr.Watcher("w", func(ctx context.Context) {
		<-ctx.Done()
		close(gotCancel)
	})
	// give the watcher a moment to install
	time.Sleep(5 * time.Millisecond)
	mgr.Stop("w")
	select {
	case <-gotCancel:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("watcher didn't observe ctx cancellation within 100 ms")
	}
}

func TestReregister_CancelsPrevious(t *testing.T) {
	mgr := New()
	defer mgr.StopAll()

	firstCancelled := make(chan struct{})
	mgr.Watcher("w", func(ctx context.Context) {
		<-ctx.Done()
		close(firstCancelled)
	})
	time.Sleep(5 * time.Millisecond)

	// Re-register same name with a different fn — old should be cancelled.
	mgr.Watcher("w", func(ctx context.Context) {
		<-ctx.Done()
	})

	select {
	case <-firstCancelled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("first watcher wasn't cancelled by re-registration")
	}

	if names := mgr.List(); len(names) != 1 {
		t.Errorf("List after re-register = %v, want exactly 1 entry", names)
	}
}

func TestStopAll_CancelsEverything(t *testing.T) {
	mgr := New()

	hbDone := make(chan struct{})
	mgr.Heartbeat("hb", 0, func(ctx context.Context) time.Duration {
		<-ctx.Done()
		close(hbDone)
		return 0
	})

	wDone := make(chan struct{})
	mgr.Watcher("w", func(ctx context.Context) {
		<-ctx.Done()
		close(wDone)
	})

	time.Sleep(5 * time.Millisecond)
	if got := len(mgr.List()); got != 2 {
		t.Fatalf("List before StopAll = %d entries, want 2", got)
	}

	mgr.StopAll()

	select {
	case <-hbDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("heartbeat not cancelled by StopAll")
	}
	select {
	case <-wDone:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("watcher not cancelled by StopAll")
	}

	if got := len(mgr.List()); got != 0 {
		t.Errorf("List after StopAll = %d, want 0", got)
	}
}

func TestCleanup_DoesNotEvictNewerRegistration(t *testing.T) {
	// Guards the pointer-compare cleanup logic: when an old
	// goroutine finishes naturally AFTER a newer same-name
	// registration has installed itself, the old defer must NOT
	// delete the newer entry.
	mgr := New()
	defer mgr.StopAll()

	letFirstFinish := make(chan struct{})
	mgr.Watcher("w", func(_ context.Context) {
		<-letFirstFinish
	})
	time.Sleep(5 * time.Millisecond)

	// Re-register before the first one finishes. The first
	// goroutine will hit its defer AFTER this newer entry is
	// installed.
	mgr.Watcher("w", func(ctx context.Context) {
		<-ctx.Done()
	})
	time.Sleep(5 * time.Millisecond)

	// Now let the first goroutine finish — its defer must NOT
	// evict the newer registration.
	close(letFirstFinish)
	time.Sleep(20 * time.Millisecond)

	if names := mgr.List(); len(names) != 1 {
		t.Errorf("List after first watcher's natural exit = %v, want 1 entry (the newer one)", names)
	}
}

func TestStop_UnknownNameNoop(t *testing.T) {
	mgr := New()
	mgr.Stop("does-not-exist") // should not panic
}
