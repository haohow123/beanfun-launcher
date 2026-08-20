package maple

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// withFakeNet swaps the package-level network primitives with
// caller-controlled fakes and restores them on test cleanup.
// Matches the swap-and-restore pattern fakeSpawn uses in
// internal/launcher.
type fakeNet struct {
	dial   func(ctx context.Context, addr string) error
	canary func(ctx context.Context, c *http.Client) error
}

func withFakeNet(t *testing.T, f *fakeNet) {
	t.Helper()
	origDial := dialerFn
	origCanary := canaryFn
	origDelay := offlineConfirmDelay

	dialerFn = func(ctx context.Context, addr string) error {
		return f.dial(ctx, addr)
	}
	canaryFn = func(ctx context.Context, c *http.Client) error {
		return f.canary(ctx, c)
	}
	offlineConfirmDelay = 5 * time.Millisecond

	t.Cleanup(func() {
		dialerFn = origDial
		canaryFn = origCanary
		offlineConfirmDelay = origDelay
	})
}

func TestCheckStatus_AnyHostOnline_MarksOnline(t *testing.T) {
	withFakeNet(t, &fakeNet{
		dial: func(_ context.Context, addr string) error {
			if addr == gameServerHosts[0] {
				return nil // success
			}
			return errors.New("timeout")
		},
		canary: func(context.Context, *http.Client) error {
			t.Fatal("canary should NOT run when probe succeeds")
			return nil
		},
	})

	c := NewChecker(nil)
	got := c.CheckStatus(context.Background())
	if !got.Online {
		t.Fatalf("Online = false, want true (host 0 succeeded)")
	}
	if got.LastChecked.IsZero() {
		t.Errorf("LastChecked is zero, want set")
	}
}

func TestCheckStatus_AllFail_CanaryFail_PreservesLast(t *testing.T) {
	// Simulate: launcher's network is broken. Don't flip Status —
	// preserve last known good.
	withFakeNet(t, &fakeNet{
		dial: func(context.Context, string) error {
			return errors.New("timeout")
		},
		canary: func(context.Context, *http.Client) error {
			return errors.New("our wifi is down")
		},
	})

	c := NewChecker(nil)
	// Seed an "online" state to verify it's preserved.
	c.status = Status{Online: true, CheckedSince: time.Now()}
	c.everChecked = true

	got := c.CheckStatus(context.Background())
	if !got.Online {
		t.Errorf("Online = false, want true (canary failed → preserve last status)")
	}
}

func TestCheckStatus_AllFail_CanaryOK_FlipsToOffline(t *testing.T) {
	var dialCalls int32
	withFakeNet(t, &fakeNet{
		dial: func(context.Context, string) error {
			atomic.AddInt32(&dialCalls, 1)
			return errors.New("timeout") // always fail
		},
		canary: func(context.Context, *http.Client) error {
			return nil // our network is fine
		},
	})

	c := NewChecker(nil)
	got := c.CheckStatus(context.Background())
	if got.Online {
		t.Errorf("Online = true, want false (all probes failed, canary OK)")
	}
	// Re-verify pass should have doubled the dial count vs a
	// single-pass probe (2 hosts × 2 passes = 4 calls).
	if calls := atomic.LoadInt32(&dialCalls); calls != int32(len(gameServerHosts))*2 {
		t.Errorf("dial calls = %d, want %d (probe + re-verify)",
			calls, len(gameServerHosts)*2)
	}
}

func TestCheckStatus_AllFail_ReverifyRecovers_StaysOnline(t *testing.T) {
	// Simulate: first probe pass times out, but re-verify succeeds
	// (the transient blip the offlineConfirmDelay is designed to
	// absorb).
	var pass int32
	withFakeNet(t, &fakeNet{
		dial: func(_ context.Context, _ string) error {
			p := atomic.AddInt32(&pass, 1)
			// First pass (calls 1, 2) fails; second pass succeeds.
			if p <= int32(len(gameServerHosts)) {
				return errors.New("transient")
			}
			return nil
		},
		canary: func(context.Context, *http.Client) error { return nil },
	})

	c := NewChecker(nil)
	got := c.CheckStatus(context.Background())
	if !got.Online {
		t.Errorf("Online = false, want true (re-verify recovered)")
	}
}

func TestCheckStatus_StateChangeUpdatesCheckedSince(t *testing.T) {
	var fail int32 = 1 // first call: 1 (fail), then 0 (success)
	withFakeNet(t, &fakeNet{
		dial: func(context.Context, string) error {
			if atomic.LoadInt32(&fail) == 1 {
				return errors.New("offline")
			}
			return nil
		},
		canary: func(context.Context, *http.Client) error { return nil },
	})

	c := NewChecker(nil)
	first := c.CheckStatus(context.Background())
	if first.Online {
		t.Fatal("first check should have been offline")
	}
	firstSince := first.CheckedSince

	time.Sleep(2 * time.Millisecond)
	atomic.StoreInt32(&fail, 0) // server "comes online"

	second := c.CheckStatus(context.Background())
	if !second.Online {
		t.Fatal("second check should have been online")
	}
	if !second.CheckedSince.After(firstSince) {
		t.Errorf("CheckedSince = %v, want after %v (state flipped)",
			second.CheckedSince, firstSince)
	}
}

func TestCheckStatus_OnServerOnline_FiresOnOfflineToOnline(t *testing.T) {
	// First probe: offline (baseline → no callback). Second probe:
	// online (offline → online transition → callback fires exactly
	// once). Mirrors TestCheckStatus_StateChangeUpdatesCheckedSince's
	// fail-flag trick to flip dial behavior between passes.
	var fail int32 = 1
	withFakeNet(t, &fakeNet{
		dial: func(context.Context, string) error {
			if atomic.LoadInt32(&fail) == 1 {
				return errors.New("offline")
			}
			return nil
		},
		canary: func(context.Context, *http.Client) error { return nil },
	})

	var calls int32
	c := NewChecker(nil)
	c.OnServerOnline = func() { atomic.AddInt32(&calls, 1) }

	c.CheckStatus(context.Background()) // baseline: offline
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("calls after baseline = %d, want 0 (initial probe is not a transition)", got)
	}

	atomic.StoreInt32(&fail, 0)
	c.CheckStatus(context.Background()) // transition: offline → online
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls after transition = %d, want 1", got)
	}
}

func TestCheckStatus_OnServerOnline_NotFiredOnInitialProbe(t *testing.T) {
	// Initial probe that lands on online still establishes baseline,
	// not a transition. Notification users would otherwise get spammed
	// on every launcher start.
	withFakeNet(t, &fakeNet{
		dial:   func(context.Context, string) error { return nil },
		canary: func(context.Context, *http.Client) error { return nil },
	})

	var called bool
	c := NewChecker(nil)
	c.OnServerOnline = func() { called = true }

	c.CheckStatus(context.Background())
	if called {
		t.Errorf("OnServerOnline fired on initial probe; want only on state change")
	}
}

func TestCheckStatus_OnServerOnline_NotFiredOnOnlineToOffline(t *testing.T) {
	// online → offline is a real transition but in the wrong direction
	// for this hook. Caller asked specifically about "server came up".
	var fail int32 = 0
	withFakeNet(t, &fakeNet{
		dial: func(context.Context, string) error {
			if atomic.LoadInt32(&fail) == 1 {
				return errors.New("offline")
			}
			return nil
		},
		canary: func(context.Context, *http.Client) error { return nil },
	})

	var called bool
	c := NewChecker(nil)
	c.OnServerOnline = func() { called = true }

	c.CheckStatus(context.Background()) // baseline: online
	atomic.StoreInt32(&fail, 1)
	c.CheckStatus(context.Background()) // transition: online → offline

	if called {
		t.Errorf("OnServerOnline fired on online → offline; want only on offline → online")
	}
}

func TestCheckStatus_NoStateChange_KeepsCheckedSince(t *testing.T) {
	withFakeNet(t, &fakeNet{
		dial:   func(context.Context, string) error { return nil },
		canary: func(context.Context, *http.Client) error { return nil },
	})

	c := NewChecker(nil)
	first := c.CheckStatus(context.Background())
	time.Sleep(2 * time.Millisecond)
	second := c.CheckStatus(context.Background())

	if !first.CheckedSince.Equal(second.CheckedSince) {
		t.Errorf("CheckedSince changed despite stable state: first=%v second=%v",
			first.CheckedSince, second.CheckedSince)
	}
	if !second.LastChecked.After(first.LastChecked) {
		t.Errorf("LastChecked didn't advance: first=%v second=%v",
			first.LastChecked, second.LastChecked)
	}
}

func TestCheckStatus_OnStatusChanged(t *testing.T) {
	tests := []struct {
		name            string
		firstFails      bool
		secondFails     bool
		wantCalls       int32
		wantFinalOnline bool
	}{
		{"initial online then steady", false, false, 1, true},
		{"initial offline then steady", true, true, 1, false},
		{"offline to online", true, false, 2, true},
		{"online to offline", false, true, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fail int32
			if tt.firstFails {
				fail = 1
			}
			withFakeNet(t, &fakeNet{
				dial: func(context.Context, string) error {
					if atomic.LoadInt32(&fail) == 1 {
						return errors.New("offline")
					}
					return nil
				},
				canary: func(context.Context, *http.Client) error { return nil },
			})

			var calls int32
			var last Status
			c := NewChecker(nil)
			c.OnStatusChanged = func(s Status) {
				atomic.AddInt32(&calls, 1)
				last = s
			}

			c.CheckStatus(context.Background())
			if tt.secondFails != tt.firstFails {
				if tt.secondFails {
					atomic.StoreInt32(&fail, 1)
				} else {
					atomic.StoreInt32(&fail, 0)
				}
			}
			c.CheckStatus(context.Background())

			if got := atomic.LoadInt32(&calls); got != tt.wantCalls {
				t.Errorf("calls = %d, want %d", got, tt.wantCalls)
			}
			if last.Online != tt.wantFinalOnline {
				t.Errorf("payload Online = %v, want %v", last.Online, tt.wantFinalOnline)
			}
			if last.CheckedSince.IsZero() {
				t.Error("payload CheckedSince is zero; the snapshot was taken before the write")
			}
		})
	}
}

func TestStatusChangedEventLiteral(t *testing.T) {
	if StatusChangedEvent != "maple:status-changed" {
		t.Errorf("StatusChangedEvent = %q; the frontend listener hardcodes the old string",
			StatusChangedEvent)
	}
}
