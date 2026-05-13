// Package launcher spawns the Beanfun game executable with a fresh
// OTP. The Wails-bound LauncherService glues the Beanfun OTP flow
// (internal/beanfun) to the platform-specific process creation.
//
// Win32 spawn lives in launcher_windows.go via CreateProcessW. The
// macOS build uses a stub that returns ErrPlatformUnsupported so the
// dev box can still compile and run unit tests; production binaries
// only target Windows.
package launcher

import (
	"context"
	"fmt"
)

// spawnFn is the platform-specific process-spawn function, set in
// launcher_{windows,other}.go. A test can override this var to record
// arguments without touching the real OS.
var spawnFn func(ctx context.Context, path string, args []string) error

// LauncherError signals a launch-time failure. Wrapped errors carry
// the underlying cause (e.g. syscall errno).
type LauncherError struct {
	Kind  LauncherErrorKind
	Msg   string
	Cause error
}

func (e *LauncherError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("launcher: %s: %v", e.Msg, e.Cause)
	}
	return fmt.Sprintf("launcher: %s", e.Msg)
}

func (e *LauncherError) Unwrap() error { return e.Cause }

// LauncherErrorKind classifies the launch-time failure for frontend
// dispatch (different copy per kind).
type LauncherErrorKind int

const (
	KindUnknown LauncherErrorKind = iota
	KindPlatformUnsupported
	KindGameExeMissing
	KindSpawnFailed
)

// ErrPlatformUnsupported is returned by the non-Windows stub: the
// macOS dev binary will not spawn Windows game executables.
func ErrPlatformUnsupported() *LauncherError {
	return &LauncherError{Kind: KindPlatformUnsupported, Msg: "game launch is only supported on Windows"}
}

// ErrGameExeMissing is returned when the game executable can't be
// located via either source: the BEANFUN_GAME_EXE env var override
// or the HKCU\SOFTWARE\Gamania\MapleStory\ExecPath registry value
// that Beanfun's installer writes. The user typically resolves this
// by reinstalling MapleStory (which repopulates the registry value)
// or by setting BEANFUN_GAME_EXE manually.
func ErrGameExeMissing() *LauncherError {
	return &LauncherError{
		Kind: KindGameExeMissing,
		Msg:  `game executable not found (checked $BEANFUN_GAME_EXE and HKCU\SOFTWARE\Gamania\MapleStory\ExecPath)`,
	}
}

// ErrSpawnFailed wraps a CreateProcessW failure with the syscall
// error.
func ErrSpawnFailed(cause error) *LauncherError {
	return &LauncherError{Kind: KindSpawnFailed, Msg: "failed to spawn game process", Cause: cause}
}
