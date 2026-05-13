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
// first appears before sending keystrokes.
//
// MapleStory is a DirectX game: the login form is rendered inside
// the DirectX swap chain, not as Win32 child controls. The HWND we
// match on (MapleStoryClass / MapleStoryClassTW) is created early
// — observed ~4s after spawn — but the game keeps the same HWND
// through patcher → loading → login screen with no Win32-visible
// state change. We verified this by listening for every WinEvent
// in range 0x0003-0x800C on the match: only one CREATE fires, no
// SHOW / FOREGROUND / FOCUS / NAMECHANGE / STATECHANGE follow.
//
// So we can't use a Win32 event to mark "form input-ready" — we
// fall back to a fixed wait. Real-Beanfun smoke shows form ready
// ~27s after spawn on a warm install. 25s after detect (≈ 29s
// after spawn) gives a small margin and lands cleanly. Cold
// installs with patching will still miss; user re-clicks 啟動遊戲
// and the detect-first path injects directly within ~100ms.
const postWindowSettleDelay = 25 * time.Second

// gameWindowWaitTimeout is the upper bound on how long Launch
// blocks waiting for the game window to appear after spawn. Covers
// SSD installs (5-15s including launch + small patch). First-ever
// installs with multi-minute patch downloads will hit this and fall
// back to the manual-paste path.
const gameWindowWaitTimeout = 60 * time.Second
