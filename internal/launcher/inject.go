package launcher

import (
	"context"
	"time"
)

// Indirection points for the detect → spawn → wait → inject flow.
// Set in inject_{windows,other}.go via init(); tests override
// directly.

// findGameWindowFn returns the HWND of an already-open MapleStory
// window, or 0 if no matching window is up. Used by the detect-first
// branch to skip spawn when the game is already running.
var findGameWindowFn func() uintptr

// waitForGameWindowFn polls every 200ms (after an immediate probe)
// until findGameWindow returns non-zero, ctx is cancelled, or the
// timeout fires. Used post-spawn to give the game's patcher / load
// screen time to transition into the actual login window.
var waitForGameWindowFn func(ctx context.Context, timeout time.Duration) (uintptr, error)

// waitWindowVisibleFn polls IsWindowVisible(hwnd) every 250ms
// until it returns true, ctx is cancelled, or the timeout fires.
//
// MapleStory's main HWND is created early (~4s after spawn) but
// stays invisible while DirectX initialises + the login form
// renders. Diagnostic poll-log captured the transition cleanly:
// IsWindowVisible flips false → true ~4-5s after CREATE, which
// matches the moment the login form becomes input-ready. This
// replaced the previous 25s blind settle.
var waitWindowVisibleFn func(ctx context.Context, hwnd uintptr, timeout time.Duration) error

// injectFn types the account + OTP into the game's login form via
// PostMessage. Both byte slices are zeroed by the caller after Launch
// returns; injectFn must consume them synchronously (no goroutines).
var injectFn func(hwnd uintptr, account, otp []byte) error

// formReadyTimeout caps how long Launch will poll IsWindowVisible
// after the CREATE event. Real-Beanfun observed transitions land
// within 4-5s of CREATE; 30s is generous headroom for cold caches.
const formReadyTimeout = 30 * time.Second

// formReadySettleDelay is a tiny safety margin between
// IsWindowVisible flipping true and the first PostMessage. The
// visibility flip is closely correlated with form-input-ready, but
// 500ms gives the message pump room to settle without being
// perceptible in the UX.
const formReadySettleDelay = 500 * time.Millisecond

// gameWindowWaitTimeout is the upper bound on how long Launch
// blocks waiting for the game window to appear after spawn. Covers
// SSD installs (5-15s including launch + small patch). First-ever
// installs with multi-minute patch downloads will hit this and fall
// back to the manual-paste path.
const gameWindowWaitTimeout = 60 * time.Second
