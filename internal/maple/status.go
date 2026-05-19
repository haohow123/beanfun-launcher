// Package maple integrates with MapleStory-specific data sources
// beyond Beanfun's login API. Currently covers server status via
// TCP probe to the game-server IPs (no official Nexon open-API
// endpoint exposes "server up / maintenance" — see CLAUDE.md
// security principle 2 for the network-policy exception that
// permits the probe, and the docs comment on gameServerHosts for
// the IP-list provenance).
package maple

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// Status is the snapshot the frontend reads. Online is the current
// determination; LastChecked / CheckedSince let the UI distinguish
// "freshly probed" from "still showing last result while we wait
// for the next heartbeat tick".
type Status struct {
	Online       bool      `json:"online"`
	LastChecked  time.Time `json:"lastChecked"`
	CheckedSince time.Time `json:"checkedSince"` // when Online last flipped
}

// gameServerHosts is the subset of Gamania's MapleStory login
// servers we TCP-probe. Inherited from the TMSBug_v2 Discord bot's
// monitoring set (full range 202.80.104.24-29). Probing 2 instead
// of all 6 keeps cumulative traffic minimal — for a personal
// launcher we don't need bot-grade redundancy. If both fail and
// the self-canary is healthy, that's enough signal to call the
// server offline (after a 10 s re-verify pass to dodge transient
// blips).
//
// Update guidance: if Gamania ever rotates these IPs we'd see
// every alpha user's status indicator stuck at "offline" while
// the game itself works. Watch for that in user reports; replace
// the list and ship a patch.
var gameServerHosts = []string{
	"202.80.104.24:8484",
	"202.80.104.27:8484",
}

const (
	// dialTimeout caps each individual TCP connect attempt. 3 s
	// matches the bot's setting — long enough for transcontinental
	// latency, short enough that an all-host failure pass returns
	// within ~3 s.
	dialTimeout = 3 * time.Second
	// canaryURL is the HTTP endpoint hit to confirm the launcher's
	// own network is working when all game-server probes fail.
	// Choosing tw.beanfun.com keeps the request inside the
	// Gamania-only network policy (CLAUDE.md principle #2).
	canaryURL = "https://tw.beanfun.com/"
)

// offlineConfirmDelay is the wait between the first all-fail probe
// and the re-verify. Tests override to keep runs under 100 ms.
var offlineConfirmDelay = 10 * time.Second

// Indirection vars for the network primitives. Tests swap-and-
// restore the same way the launcher / beanfun packages do (fakeSpawn
// etc.) so unit tests don't actually open TCP sockets or hit
// Gamania.
var (
	dialerFn = realDial
	canaryFn = realCanary
)

func realDial(ctx context.Context, addr string) error {
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	_ = c.Close()
	return nil
}

func realCanary(ctx context.Context, client *http.Client) error {
	// Cap the canary call so a hung tw.beanfun.com doesn't stall
	// the whole heartbeat — caller's ctx may be unbounded (the
	// heartbeat's app-lifecycle ctx).
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, canaryURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("canary %s: HTTP %d", canaryURL, resp.StatusCode)
	}
	return nil
}

// Checker performs status probes against the game servers and
// maintains the latest cached Status. Safe for concurrent reads
// via Status(); CheckStatus serializes writes under mu.
type Checker struct {
	httpClient *http.Client

	mu          sync.RWMutex
	status      Status
	everChecked bool
}

// NewChecker returns a Checker that uses httpClient for the
// self-network canary. Pass nil to get a private &http.Client{} —
// deliberately NOT aliased to http.DefaultClient, which is a
// process-wide singleton: if any other code (current or future
// dependency) ever assigns to http.DefaultClient.Jar, the canary
// would silently start carrying cookies. A dedicated client
// guarantees the canary stays jar-less + header-less, matching
// CLAUDE.md security principle #2 (no incidental data sent).
func NewChecker(httpClient *http.Client) *Checker {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Checker{httpClient: httpClient}
}

// Status returns the latest cached snapshot. Safe for concurrent
// readers; cheap (RWMutex read lock + struct copy).
func (c *Checker) Status() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// CheckStatus performs one probe cycle and updates the cached
// Status. Returns the post-update Status. Intended to be called
// from a bgtask.Heartbeat — the heartbeat handles cadence; this
// method handles one probe pass + the anti-false-positive
// re-verify.
//
// Algorithm:
//  1. Parallel TCP connect to gameServerHosts (3 s timeout each).
//     Any success → server online.
//  2. All fail → self-canary HTTP HEAD tw.beanfun.com. Canary
//     fails too → launcher's own network is the problem; preserve
//     last cached Status (better last-known-good than a misleading
//     false offline).
//  3. Canary OK + all probes failed → wait offlineConfirmDelay,
//     re-probe. Still all-fail → confirmed offline.
func (c *Checker) CheckStatus(ctx context.Context) Status {
	online := probeOnce(ctx, gameServerHosts)

	if !online {
		// Don't flip to offline if our own network looks broken.
		if err := canaryFn(ctx, c.httpClient); err != nil {
			slog.Warn("maple.status: all probes failed AND canary failed — preserving last status",
				"canary_err", err)
			return c.Status()
		}

		// Canary OK → probable real outage. Wait + re-verify
		// before flipping, to absorb transient blips.
		select {
		case <-ctx.Done():
			return c.Status()
		case <-time.After(offlineConfirmDelay):
		}
		online = probeOnce(ctx, gameServerHosts)
		if online {
			slog.Info("maple.status: false-positive avoided, server still online after re-verify")
		}
	}

	now := time.Now()
	c.mu.Lock()
	prev := c.status
	c.status.LastChecked = now
	if !c.everChecked {
		slog.Info("maple.status: initial probe", "online", online)
		c.status.Online = online
		c.status.CheckedSince = now
	} else if c.status.Online != online {
		slog.Info("maple.status: state changed",
			"online", online, "prev_online", prev.Online)
		c.status.Online = online
		c.status.CheckedSince = now
	}
	c.everChecked = true
	c.mu.Unlock()
	return c.Status()
}

// probeOnce attempts parallel TCP connects to all hosts. Returns
// true the moment any one succeeds (early exit — no need to wait
// for the remaining hosts once we know the server is up). Returns
// false if all fail or ctx is cancelled.
//
// Waits for all spawned goroutines to exit before returning so
// no probe outlives this call — important under -race because
// dialerFn is a package var swapped by tests via withFakeNet's
// t.Cleanup, and a leaked goroutine would race the swap.
func probeOnce(ctx context.Context, hosts []string) bool {
	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	var wg sync.WaitGroup
	results := make(chan bool, len(hosts))
	for _, h := range hosts {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			ok := dialerFn(ctx, addr) == nil
			select {
			case results <- ok:
			case <-ctx.Done():
			}
		}(h)
	}
	// Cleanup order: cancel first (signals remaining goroutines
	// to stop), then wait for them to exit. Deferred LIFO so wg
	// fires after cancel.
	defer wg.Wait()

	for range hosts {
		select {
		case <-ctx.Done():
			return false
		case ok := <-results:
			if ok {
				cancel() // unblock the remaining dial goroutines
				return true
			}
		}
	}
	return false
}
