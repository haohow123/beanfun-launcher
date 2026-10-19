//go:build windows

package launcher

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// samePath compares two existing paths after expanding 8.3 short names, which CI temp directories use.
func samePath(t *testing.T, a, b string) bool {
	t.Helper()
	return strings.EqualFold(longPath(t, a), longPath(t, b))
}

func longPath(t *testing.T, p string) string {
	t.Helper()
	in, err := windows.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetLongPathName(in, &buf[0], uint32(len(buf)))
	if err != nil {
		t.Fatalf("GetLongPathName(%s): %v", p, err)
	}
	return windows.UTF16ToString(buf[:n])
}

// startCopyOf runs a private copy of ping.exe from dir so the test can rename the folder under it.
func startCopyOf(t *testing.T, dir string) (exe string, pid uint32) {
	t.Helper()
	src := filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE")
	if _, err := os.Stat(src); err != nil {
		skipOrFailOnCI(t, "ping.exe not found: "+err.Error())
	}
	exe = filepath.Join(dir, "game.exe")
	copyFile(t, src, exe)
	cmd := exec.Command(exe, "-n", "60", "127.0.0.1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return exe, uint32(cmd.Process.Pid)
}

// The genuine image passes the identity step, so the verdict comes from the signature step (a catalog-signed copy has no embedded signature).
func TestProcessImageVerdict_SameFile_ReachesSignatureCheck(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "game")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe, pid := startCopyOf(t, dir)

	path, v, err := processImageVerdict(pid)
	if err != nil {
		t.Fatalf("processImageVerdict: %v", err)
	}
	if !samePath(t, path, exe) {
		t.Errorf("path = %q, want %q", path, exe)
	}
	if v.Reason == "image mismatch" {
		t.Fatalf("verdict = %+v, want the identity check to pass", v)
	}
}

// Renaming the folder of a running exe and putting another copy at the old path must not pass as the running image.
func TestProcessImageVerdict_FolderRenamedAndReplaced_ImageMismatch(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "game")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	exe, pid := startCopyOf(t, dir)

	if err := os.Rename(dir, filepath.Join(base, "game-moved")); err != nil {
		t.Fatalf("rename running exe's folder: %v", err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	copyFile(t, filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE"), exe)

	path, v, err := processImageVerdict(pid)
	if err != nil {
		t.Fatalf("processImageVerdict: %v", err)
	}
	if !samePath(t, path, exe) {
		t.Fatalf("image path after the folder rename = %q, want the old path %q; the attack this test reproduces does not apply on this Windows build", path, exe)
	}
	if v.OK || v.Reason != "image mismatch" {
		t.Fatalf("verdict = %+v, want OK=false reason \"image mismatch\"", v)
	}
}
