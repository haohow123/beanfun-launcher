package launcher

// Indirection points for the detect → spawn flow. Set in
// inject_{windows,other}.go via init(); tests override directly.

// findGameWindowFn returns the HWND of an already-open MapleStory
// window, or 0 if no matching window is currently up. Used to
// branch Launch between "game running → inject directly" and
// "game not running → spawn only".
var findGameWindowFn func() uintptr

// injectFn types the account + OTP into the game's login form via
// PostMessage. Both byte slices are zeroed by the caller after Launch
// returns; injectFn must consume them synchronously (no goroutines).
var injectFn func(hwnd uintptr, account, otp []byte) error
