package localpath

import (
	"errors"
	"os"
	"testing"
	"time"
)

// withFakeRemoteDrive swaps IsRemoteDriveFn, restoring it in t.Cleanup.
func withFakeRemoteDrive(t *testing.T, remote bool) {
	t.Helper()
	orig := IsRemoteDriveFn
	IsRemoteDriveFn = func(string) bool { return remote }
	t.Cleanup(func() { IsRemoteDriveFn = orig })
}

// withFakeLstat swaps LstatFn, restoring it in t.Cleanup.
func withFakeLstat(t *testing.T, fn func(string) (os.FileInfo, error)) {
	t.Helper()
	orig := LstatFn
	LstatFn = fn
	t.Cleanup(func() { LstatFn = orig })
}

// fakeFileInfo is a minimal os.FileInfo for tests that need RejectLink to
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
		remote  bool // fake IsRemoteDriveFn return value
		wantErr error
	}{
		{name: "relative path", path: "x.wav", wantErr: ErrNotLocal},
		{name: "empty string", path: "", wantErr: ErrNotLocal},
		{name: "backslash UNC", path: `\\host\share\x.wav`, wantErr: ErrNotLocal},
		{name: "forward-slash UNC", path: `//host/share/x.wav`, wantErr: ErrNotLocal},
		{name: "device path \\\\?\\", path: `\\?\C:\x.wav`, wantErr: ErrNotLocal},
		{name: "device path \\\\.\\", path: `\\.\C:\x.wav`, wantErr: ErrNotLocal},
		{name: "NT native path \\??\\", path: `\??\C:\x.wav`, wantErr: ErrNotLocal},
		{name: "NT native UNC \\??\\UNC\\", path: `\??\UNC\host\share\x.wav`, wantErr: ErrNotLocal},
		{name: "drive-relative path", path: `C:x.wav`, wantErr: ErrNotLocal},
		{name: "drive-absolute backslash", path: `C:\Users\a\ding.wav`},
		{name: "drive-absolute forward slash", path: `c:/x.wav`},
		{name: "mapped network drive", path: `C:\Users\a\ding.wav`, remote: true, wantErr: ErrNotLocal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withFakeRemoteDrive(t, tt.remote)

			err := CheckWindowsPath(tt.path)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("CheckWindowsPath(%q) = %v, want nil", tt.path, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CheckWindowsPath(%q) = %v, want %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

// TestRejectLink exercises RejectLink directly via the LstatFn seam: a
// symlink-mode FileInfo is rejected, a regular-mode one passes, and a
// lstat error is passed through unwrapped.
func TestRejectLink(t *testing.T) {
	withFakeLstat(t, func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil })
	if err := RejectLink("whatever"); err != nil {
		t.Fatalf("RejectLink() = %v, want nil for a regular file", err)
	}

	withFakeLstat(t, func(string) (os.FileInfo, error) { return fakeFileInfo{mode: os.ModeSymlink}, nil })
	if err := RejectLink("whatever"); !errors.Is(err, ErrNotLocal) {
		t.Fatalf("RejectLink() = %v, want %v for a symlink", err, ErrNotLocal)
	}

	wantErr := errors.New("stat failed")
	withFakeLstat(t, func(string) (os.FileInfo, error) { return nil, wantErr })
	if err := RejectLink("whatever"); !errors.Is(err, wantErr) {
		t.Fatalf("RejectLink() = %v, want %v passed through", err, wantErr)
	}
}

// TestCheck_WindowsRulesOnAnyHost checks that Check simply delegates to
// whatever CheckFn is bound to, using CheckWindowsPath as a stand-in so the
// assertion runs on any host OS.
func TestCheck_WindowsRulesOnAnyHost(t *testing.T) {
	orig := CheckFn
	CheckFn = CheckWindowsPath
	t.Cleanup(func() { CheckFn = orig })
	withFakeRemoteDrive(t, false)

	if err := Check(`\\host\share\x.wav`); !errors.Is(err, ErrNotLocal) {
		t.Fatalf("Check(UNC) = %v, want %v", err, ErrNotLocal)
	}
	if err := Check(`C:\Users\a\ding.wav`); err != nil {
		t.Fatalf("Check(drive-absolute) = %v, want nil", err)
	}
}
