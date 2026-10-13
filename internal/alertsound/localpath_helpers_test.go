package alertsound

import (
	"os"
	"testing"
	"time"

	"github.com/haohow123/beanfun-launcher/internal/localpath"
)

// withFakeRemoteDrive swaps localpath.IsRemoteDriveFn, restoring it in t.Cleanup.
func withFakeRemoteDrive(t *testing.T, remote bool) {
	t.Helper()
	orig := localpath.IsRemoteDriveFn
	localpath.IsRemoteDriveFn = func(string) bool { return remote }
	t.Cleanup(func() { localpath.IsRemoteDriveFn = orig })
}

// withFakeLstat swaps localpath.LstatFn, restoring it in t.Cleanup.
func withFakeLstat(t *testing.T, fn func(string) (os.FileInfo, error)) {
	t.Helper()
	orig := localpath.LstatFn
	localpath.LstatFn = fn
	t.Cleanup(func() { localpath.LstatFn = orig })
}

// fakeFileInfo is a minimal os.FileInfo for tests that need localpath.RejectLink
// to see a real (non-symlink) file without touching disk.
type fakeFileInfo struct{ mode os.FileMode }

func (f fakeFileInfo) Name() string       { return "" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }
