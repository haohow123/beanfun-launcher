package launcher

import (
	"context"
	"time"
)

// Indirection points for the spawn → wait → inject flow. Set in
// inject_{windows,other}.go via init(); tests override directly.

// waitForGameWindowFn polls for the MapleStory window after spawn and
// returns its HWND once found, or an error on context cancel / timeout.
var waitForGameWindowFn func(ctx context.Context, timeout time.Duration) (uintptr, error)

// injectFn types the account + OTP into the game's login form via
// PostMessage. Both byte slices are zeroed by the caller after Launch
// returns; injectFn must consume them synchronously (no goroutines).
var injectFn func(hwnd uintptr, account, otp []byte) error

// gameWindowWaitTimeout is the upper bound on how long we wait for
// the game window to appear after spawn. MapleStory cold-start to
// first window is usually <3s on SSD; 10s leaves headroom for first
// launches (antivirus scan, larger update verifications). Past this,
// the fallback path engages — SID+OTP returned to frontend for
// manual paste.
const gameWindowWaitTimeout = 10 * time.Second
