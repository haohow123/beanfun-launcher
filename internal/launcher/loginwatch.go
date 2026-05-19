package launcher

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// loginWatchEvent is what runLoginWatcher emits on its channel.
// Each value is emitted at most once per run.
type loginWatchEvent int

const (
	// formReady = "the MapleStory login form is interactive and
	// ready to receive WM_CHAR." Detected by counting OBJID_CARET
	// LOCATIONCHANGE events in a sliding window — when the form
	// renders and the text-input caret starts blinking/animating,
	// we get a burst of caret events. See eventprobe probe3 log:
	// 21 caret events in ~100ms cluster at T+15.89s on a window
	// that appeared at T+4.57s.
	formReady loginWatchEvent = iota + 1
	// loggedIn = "credentials submitted, character-select screen
	// opened." Detected by a new EVENT_OBJECT_CREATE whose
	// hwnd != loginHwnd but class == "MapleStoryClassTW". probe3
	// captured this at T+25.34s, ~6.5s after WM_CHAR + RETURN
	// landed on the form.
	loggedIn
)

var (
	// formReadyDelay is the quiet zone after the login window
	// appears, during which caret events are ignored. The TSF
	// (Text Services Framework, MSCTFIME + CicMarshalWnd) creates
	// its caret context within ~5s of window create — those
	// events are not the form-ready signal. Real form-ready burst
	// happens ~11s+ after window create per probe3.
	formReadyDelay = 7 * time.Second

	// caretBurstThreshold + caretBurstWindow define what counts as
	// a "burst" of caret events. probe3 saw 21 events within
	// ~100ms. We require ≥5 events within a 1s sliding window —
	// generous threshold that distinguishes the form-rendered
	// burst from the 1-2 isolated TSF init events.
	caretBurstThreshold = 5
	caretBurstWindow    = 1 * time.Second
)

// installLoginWatchFn installs Win32 hooks for the spawned game's
// UI events. Bound in loginwatch_windows.go (real hooks) or
// loginwatch_other.go (stub returning ErrPlatformUnsupported).
//
// onCaret fires every time an OBJID_CARET LOCATIONCHANGE event
// lands within the target PID. The state machine in runLoginWatcher
// owns burst detection — the Win32 layer just relays raw events.
//
// onLoggedIn fires once when a new HWND with class MapleStoryClassTW
// (and != loginHwnd) is created in the target PID. The Win32 layer
// deduplicates this on its side; the orchestrator can call it more
// than once safely (idempotent).
//
// uninstall removes the hook and stops the message pump. Idempotent;
// also called automatically when ctx is cancelled.
var installLoginWatchFn func(
	ctx context.Context,
	pid uint32,
	loginHwnd uintptr,
	onCaret func(),
	onLoggedIn func(),
) (uninstall func(), err error)

// runLoginWatcher subscribes to UI events for the spawned game's PID
// and translates them into typed events on the returned channel:
//
//   - formReady fires once when a caret-event burst is observed
//     (login form is rendered + ready for WM_CHAR).
//   - loggedIn fires once when a new MapleStoryClassTW HWND is
//     created (login submitted + transitioned to character select).
//
// Channel closes on ctx done. Caller is responsible for managing
// timeouts (typically: formReadyTimeout = 60s, loggedInTimeout =
// 10s after firing inject) via the ctx and external select.
//
// Reuses the windowAppearedAt = time.Now() snapshot from caller
// scope is unnecessary — we mark it at runLoginWatcher entry
// because the caller has just polled the window in (the brief
// gap is negligible).
func runLoginWatcher(
	ctx context.Context,
	loginHwnd uintptr,
	pid uint32,
) <-chan loginWatchEvent {
	out := make(chan loginWatchEvent, 4)
	go func() {
		defer close(out)

		windowAppearedAt := time.Now()

		var (
			caretMu          sync.Mutex
			caretEvents      []time.Time
			formReadyEmitted bool

			loggedInMu      sync.Mutex
			loggedInEmitted bool
		)

		onCaret := func() {
			caretMu.Lock()
			defer caretMu.Unlock()
			if formReadyEmitted {
				return
			}
			now := time.Now()
			if now.Sub(windowAppearedAt) < formReadyDelay {
				// TSF init quiet zone — these are not the form-
				// ready signal we're looking for.
				return
			}
			// Trim sliding window: drop events older than
			// caretBurstWindow.
			cutoff := now.Add(-caretBurstWindow)
			i := 0
			for ; i < len(caretEvents); i++ {
				if caretEvents[i].After(cutoff) {
					break
				}
			}
			caretEvents = caretEvents[i:]
			caretEvents = append(caretEvents, now)
			if len(caretEvents) >= caretBurstThreshold {
				formReadyEmitted = true
				slog.Info("loginwatch: caret burst → form ready",
					"events_in_window", len(caretEvents),
					"since_window_appeared", now.Sub(windowAppearedAt))
				select {
				case out <- formReady:
				case <-ctx.Done():
				}
			}
		}

		onLoggedIn := func() {
			loggedInMu.Lock()
			defer loggedInMu.Unlock()
			if loggedInEmitted {
				return
			}
			loggedInEmitted = true
			slog.Info("loginwatch: new MapleStoryClassTW HWND → login success",
				"since_window_appeared", time.Since(windowAppearedAt))
			select {
			case out <- loggedIn:
			case <-ctx.Done():
			}
		}

		uninstall, err := installLoginWatchFn(ctx, pid, loginHwnd, onCaret, onLoggedIn)
		if err != nil {
			slog.Error("loginwatch: install hook failed", "err", err)
			return
		}
		defer uninstall()

		<-ctx.Done()
		slog.Debug("loginwatch: ctx done, cleaning up")
	}()
	return out
}
