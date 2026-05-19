// Package bgtask manages the app's background goroutines via a
// single named registry. Goroutines come in two shapes:
//
//   - Heartbeat: cadence-driven action (e.g., the Beanfun
//     keep-alive ping every 60 s, MapleStory status probe every
//     2 min). The fn returns the desired delay before its next
//     invocation, which lets a single API cover both fixed
//     cadence and adaptive backoff (shorter retry on failure).
//   - Watcher: one-shot blocking on an event source (e.g., Win32
//     WaitForSingleObject on a game process handle, until exit).
//
// Both register by name; re-registering the same name cancels the
// previous goroutine first, so a name always maps to at most one
// live goroutine. main.go owns one Manager and calls StopAll() on
// app shutdown to clean up.
//
// Why two methods instead of a single Go(name, fn): the named
// distinction encodes intent at the API boundary — a future
// reader sees `mgr.Heartbeat("status", …)` and knows it's
// periodic, `mgr.Watcher("game-exit", …)` and knows it's one-shot.
// Conflating them under one function lost that signal in early
// drafts (which led to "polling a watcher as a cron" mistakes the
// `feedback-event-driven-over-polling` memory codifies).
package bgtask

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Manager owns the registry of named goroutines.
type Manager struct {
	mu     sync.Mutex
	active map[string]*entry
}

// entry tracks a single registered goroutine. Pointer comparison
// (not function comparison) is the cleanup-race guard: a goroutine
// finishing naturally only removes its own entry if the registered
// pointer still matches its own — a same-name re-registration that
// happened in between leaves the new entry untouched.
type entry struct {
	cancel context.CancelFunc
}

// New returns a fresh Manager. Cheap; one per app.
func New() *Manager {
	return &Manager{active: map[string]*entry{}}
}

// Heartbeat schedules fn to run periodically. fn returns the
// desired delay before its next invocation — returning the same
// constant each time gives fixed cadence, returning different
// values gives adaptive backoff (e.g., 60 s on success, 10 s on
// failure). Returning <= 0 stops the heartbeat.
//
// firstDelay is the wait before the first call to fn — pass 0
// for "run immediately on registration" (e.g., status probes that
// want a first reading on the page within seconds), pass an
// interval for "wait this long before the first action" (e.g.,
// keep-alive pings, where the session is fresh and idle reaping
// won't happen for a while).
//
// Re-registering with the same name cancels the previous instance
// first; runtime cleanup removes the entry from List() once the
// goroutine returns.
func (m *Manager) Heartbeat(name string, firstDelay time.Duration, fn func(context.Context) time.Duration) {
	m.start(name, func(ctx context.Context) {
		delay := firstDelay
		for {
			if delay > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
			} else if err := ctx.Err(); err != nil {
				return
			}
			delay = fn(ctx)
			if delay <= 0 {
				return
			}
		}
	})
}

// Watcher schedules fn as a one-shot background goroutine. fn is
// expected to block on an event source (kernel object, channel,
// network read, etc.) and return when the event fires or ctx is
// cancelled. Useful for "do something the moment X happens" where
// the OS or library exposes a push primitive — see
// `feedback-event-driven-over-polling` for the principle.
//
// Re-registering with the same name cancels the previous instance.
// Naturally-completed watchers (fn returns) are removed from
// List() automatically.
func (m *Manager) Watcher(name string, fn func(context.Context)) {
	m.start(name, fn)
}

// Stop cancels the named goroutine and removes it from the
// registry. Safe to call on names that aren't active (no-op).
func (m *Manager) Stop(name string) {
	m.mu.Lock()
	ent, ok := m.active[name]
	if ok {
		delete(m.active, name)
	}
	m.mu.Unlock()
	if ok {
		ent.cancel()
	}
}

// StopAll cancels every active goroutine. Intended for app
// shutdown; main.go hooks app.OnShutdown to call this so we don't
// leak background work past process exit.
//
// Does not wait for goroutines to finish — they exit cooperatively
// when their ctx is cancelled. Callers needing a synchronous wait
// should add a WaitGroup or sync.Cond on top.
func (m *Manager) StopAll() {
	m.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(m.active))
	for _, ent := range m.active {
		cancels = append(cancels, ent.cancel)
	}
	m.active = map[string]*entry{}
	m.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// List returns a snapshot of currently-active goroutine names.
// Intended for introspection / debug ("what's running right now").
func (m *Manager) List() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.active))
	for n := range m.active {
		names = append(names, n)
	}
	return names
}

// start is the shared launch path for Heartbeat + Watcher. It
// cancels any existing registration under the same name, installs
// a fresh entry, and spawns the goroutine with a cleanup defer
// that removes the entry on exit (but only if it's still ours —
// see entry doc).
func (m *Manager) start(name string, fn func(context.Context)) {
	ctx, cancel := context.WithCancel(context.Background())
	ent := &entry{cancel: cancel}

	m.mu.Lock()
	if old, exists := m.active[name]; exists {
		slog.Debug("bgtask: superseded", "name", name)
		old.cancel()
	}
	m.active[name] = ent
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			// Pointer compare — only clear if no newer registration
			// has replaced us in the meantime.
			if cur, ok := m.active[name]; ok && cur == ent {
				delete(m.active, name)
			}
			m.mu.Unlock()
			cancel()
		}()
		fn(ctx)
	}()
}
