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

// injectFn types the account + OTP into the game's login form via
// PostMessage. Both byte slices are zeroed by the caller after Launch
// returns; injectFn must consume them synchronously (no goroutines).
var injectFn func(hwnd uintptr, account, otp []byte) error

// postWindowSettleDelay is how long Launch waits after the window
// first appears before sending keystrokes. The early frames are
// often a patcher / "loading" screen with the same class as the
// login window; the controls aren't ready to receive input yet.
// 3 seconds is enough on warm disks for the login form to take
// focus on a previously-patched install. If the user is mid-patch
// the inject still misses, but they can click 啟動遊戲 again once
// the login screen is visible — the detect-first path then lands
// cleanly into the now-ready window.
const postWindowSettleDelay = 3 * time.Second

// gameWindowWaitTimeout is the upper bound on how long Launch
// blocks waiting for the game window to appear after spawn. Covers
// SSD installs (5-15s including launch + small patch). First-ever
// installs with multi-minute patch downloads will hit this and fall
// back to the manual-paste path.
const gameWindowWaitTimeout = 60 * time.Second
