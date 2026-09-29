package alertsound

import (
	"errors"
	"os"
	"testing"
	"time"
)

// withFakeRemoteDrive swaps isRemoteDriveFn, restoring it in t.Cleanup.
func withFakeRemoteDrive(t *testing.T, remote bool) {
	t.Helper()
	orig := isRemoteDriveFn
	isRemoteDriveFn = func(string) bool { return remote }
	t.Cleanup(func() { isRemoteDriveFn = orig })
}

// withFakeLstat swaps lstatFn, restoring it in t.Cleanup.
func withFakeLstat(t *testing.T, fn func(string) (os.FileInfo, error)) {
	t.Helper()
	orig := lstatFn
	lstatFn = fn
	t.Cleanup(func() { lstatFn = orig })
}

// fakeFileInfo is a minimal os.FileInfo for tests that need rejectLink to
// see a real (non-symlink) file without touching disk.
type fakeFileInfo struct{ mode os.FileMode }

func (f fakeFileInfo) Name() string       { return "" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

// TestCheckWindowsPath exercises the Windows path-shape whitelist directly
// (no build tag), so the real rules are tested on any host OS, not just Windows.
func TestCheckWindowsPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		remote  bool // fake isRemoteDriveFn return value
		wantErr error
	}{
		{name: "relative path", path: "x.wav", wantErr: errNotLocal},
		{name: "empty string", path: "", wantErr: errNotLocal},
		{name: "backslash UNC", path: `\\host\share\x.wav`, wantErr: errNotLocal},
		{name: "forward-slash UNC", path: `//host/share/x.wav`, wantErr: errNotLocal},
		{name: "device path \\\\?\\", path: `\\?\C:\x.wav`, wantErr: errNotLocal},
		{name: "device path \\\\.\\", path: `\\.\C:\x.wav`, wantErr: errNotLocal},
		{name: "NT native path \\??\\", path: `\??\C:\x.wav`, wantErr: errNotLocal},
		{name: "NT native UNC \\??\\UNC\\", path: `\??\UNC\host\share\x.wav`, wantErr: errNotLocal},
		{name: "drive-relative path", path: `C:x.wav`, wantErr: errNotLocal},
		{name: "drive-absolute backslash", path: `C:\Users\a\ding.wav`},
		{name: "drive-absolute forward slash", path: `c:/x.wav`},
		{name: "mapped network drive", path: `C:\Users\a\ding.wav`, remote: true, wantErr: errNotLocal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withFakeRemoteDrive(t, tt.remote)

			err := checkWindowsPath(tt.path)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("checkWindowsPath(%q) = %v, want nil", tt.path, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("checkWindowsPath(%q) = %v, want %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// TestCheckLocalPath_WindowsRulesOnAnyHost checks that checkLocalPath simply
// delegates to whatever checkLocalPathFn is bound to, using checkWindowsPath
// as a stand-in so the assertion runs on any host OS.
func TestCheckLocalPath_WindowsRulesOnAnyHost(t *testing.T) {
	orig := checkLocalPathFn
	checkLocalPathFn = checkWindowsPath
	t.Cleanup(func() { checkLocalPathFn = orig })
	withFakeRemoteDrive(t, false)

	if err := checkLocalPath(`\\host\share\x.wav`); !errors.Is(err, errNotLocal) {
		t.Fatalf("checkLocalPath(UNC) = %v, want %v", err, errNotLocal)
	}
	if err := checkLocalPath(`C:\Users\a\ding.wav`); err != nil {
		t.Fatalf("checkLocalPath(drive-absolute) = %v, want nil", err)
	}
}
