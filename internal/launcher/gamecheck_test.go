package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

// withSignature swaps verifyGameSignatureFn for a fixed verdict, restoring the original via t.Cleanup.
func withSignature(t *testing.T, verdict signatureVerdict) {
	t.Helper()
	orig := verifyGameSignatureFn
	verifyGameSignatureFn = func(string) signatureVerdict { return verdict }
	t.Cleanup(func() { verifyGameSignatureFn = orig })
}

var signatureOK = signatureVerdict{OK: true}

func TestCheckGameExe_Unsigned(t *testing.T) {
	withSignature(t, signatureVerdict{Reason: "unsigned"})
	dir := t.TempDir()
	path := filepath.Join(dir, "MapleStory.exe")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	got := checkGameExe(path)
	if got.OK {
		t.Fatalf("checkGameExe(unsigned) OK = true, want false")
	}
	if got.Problem != problemUnsigned {
		t.Errorf("Problem = %q, want %q", got.Problem, problemUnsigned)
	}
}

func TestCheckGameExe_NoPath(t *testing.T) {
	got := checkGameExe("")
	if got.OK {
		t.Fatalf("checkGameExe(\"\") OK = true, want false")
	}
	if got.Problem != "no game path" {
		t.Errorf("Problem = %q, want %q", got.Problem, "no game path")
	}
}

func TestCheckGameExe_NotLocal(t *testing.T) {
	got := checkGameExe("relative/MapleStory.exe")
	if got.OK {
		t.Fatalf("checkGameExe(relative) OK = true, want false")
	}
	if got.Problem != "not a local file" {
		t.Errorf("Problem = %q, want %q", got.Problem, "not a local file")
	}
}

func TestCheckGameExe_Missing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MapleStory.exe")

	got := checkGameExe(path)
	if got.OK {
		t.Fatalf("checkGameExe(missing) OK = true, want false")
	}
	if got.Problem != "game executable not found" {
		t.Errorf("Problem = %q, want %q", got.Problem, "game executable not found")
	}
}

// TestCheckGameExe_Directory uses a .exe-suffixed directory name so the
// extension check (which runs first) doesn't shadow the directory check
// this test pins.
func TestCheckGameExe_Directory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MapleStory.exe")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("Mkdir() = %v", err)
	}

	got := checkGameExe(path)
	if got.OK {
		t.Fatalf("checkGameExe(dir) OK = true, want false")
	}
	if got.Problem != "game path is not a file" {
		t.Errorf("Problem = %q, want %q", got.Problem, "game path is not a file")
	}
}

func TestCheckGameExe_Symlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.exe")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	link := filepath.Join(dir, "MapleStory.exe")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("os.Symlink unsupported: %v", err)
	}

	got := checkGameExe(link)
	if got.OK {
		t.Fatalf("checkGameExe(symlink) OK = true, want false")
	}
	if got.Problem != "game path is not a file" {
		t.Errorf("Problem = %q, want %q", got.Problem, "game path is not a file")
	}
}

// TestCheckGameExe_Symlink_RejectsBeforeStat pins RejectLink running before
// any Stat call, so a symlink to a UNC share never triggers an implicit SMB
// connection.
func TestCheckGameExe_Symlink_RejectsBeforeStat(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.exe")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	link := filepath.Join(dir, "MapleStory.exe")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("os.Symlink unsupported: %v", err)
	}

	origStat := gameStatFn
	calls := 0
	gameStatFn = func(path string) (os.FileInfo, error) {
		calls++
		return origStat(path)
	}
	t.Cleanup(func() { gameStatFn = origStat })

	got := checkGameExe(link)
	if got.OK {
		t.Fatalf("checkGameExe(symlink) OK = true, want false")
	}
	if got.Problem != problemNotAFile {
		t.Errorf("Problem = %q, want %q", got.Problem, problemNotAFile)
	}
	if calls != 0 {
		t.Errorf("gameStatFn called %d times, want 0", calls)
	}
}

// TestCheckGameExe_NotExe pins the extension gate: anything but .exe
// (case-insensitive) is a hard failure, checked before any disk access.
func TestCheckGameExe_NotExe(t *testing.T) {
	withSignature(t, signatureOK)
	dir := t.TempDir()
	tests := []struct {
		file string
		ok   bool
	}{
		{file: "MapleStory.lnk", ok: false},
		{file: "MapleStory.url", ok: false},
		{file: "MapleStory.bat", ok: false},
		{file: "MapleStory.EXE", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			path := filepath.Join(dir, tt.file)
			if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
				t.Fatalf("WriteFile() = %v", err)
			}

			got := checkGameExe(path)
			if got.OK != tt.ok {
				t.Fatalf("checkGameExe(%q) OK = %v, want %v (Problem=%q)", tt.file, got.OK, tt.ok, got.Problem)
			}
			if !tt.ok && got.Problem != problemNotExe {
				t.Errorf("Problem = %q, want %q", got.Problem, problemNotExe)
			}
		})
	}
}

func TestCheckGameExe_OK_NoWarnings(t *testing.T) {
	withSignature(t, signatureOK)
	dir := t.TempDir()
	path := filepath.Join(dir, "MapleStory.exe")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, gameDataDirName), 0o700); err != nil {
		t.Fatalf("Mkdir() = %v", err)
	}

	got := checkGameExe(path)
	if !got.OK {
		t.Fatalf("checkGameExe() OK = false, want true (Problem=%q)", got.Problem)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("Warnings = %v, want none", got.Warnings)
	}
}

func TestCheckGameExe_OK_WithDataWz(t *testing.T) {
	withSignature(t, signatureOK)
	dir := t.TempDir()
	path := filepath.Join(dir, "MapleStory.exe")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, gameDataWzName), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	got := checkGameExe(path)
	if !got.OK || len(got.Warnings) != 0 {
		t.Fatalf("checkGameExe() = %+v, want OK with no warnings", got)
	}
}

func TestCheckGameExe_WarnUnexpectedName(t *testing.T) {
	withSignature(t, signatureOK)
	dir := t.TempDir()
	path := filepath.Join(dir, "SomeOtherGame.exe")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, gameDataDirName), 0o700); err != nil {
		t.Fatalf("Mkdir() = %v", err)
	}

	got := checkGameExe(path)
	if !got.OK {
		t.Fatalf("checkGameExe() OK = false, want true")
	}
	if !containsWarning(got.Warnings, "unexpected executable name") {
		t.Fatalf("Warnings = %v, want to contain %q", got.Warnings, "unexpected executable name")
	}
}

// TestCheckGameExe_LowercaseName_NoWarning pins the gameExeName comparison
// being case-insensitive, since Windows file systems are case-preserving
// but not case-sensitive.
func TestCheckGameExe_LowercaseName_NoWarning(t *testing.T) {
	withSignature(t, signatureOK)
	dir := t.TempDir()
	path := filepath.Join(dir, "maplestory.exe")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, gameDataDirName), 0o700); err != nil {
		t.Fatalf("Mkdir() = %v", err)
	}

	got := checkGameExe(path)
	if !got.OK {
		t.Fatalf("checkGameExe() OK = false, want true")
	}
	if containsWarning(got.Warnings, warnUnexpectedName) {
		t.Fatalf("Warnings = %v, want no %q", got.Warnings, warnUnexpectedName)
	}
}

func TestCheckGameExe_WarnNoGameData(t *testing.T) {
	withSignature(t, signatureOK)
	dir := t.TempDir()
	path := filepath.Join(dir, "MapleStory.exe")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	got := checkGameExe(path)
	if !got.OK {
		t.Fatalf("checkGameExe() OK = false, want true")
	}
	if !containsWarning(got.Warnings, "game data not found") {
		t.Fatalf("Warnings = %v, want to contain %q", got.Warnings, "game data not found")
	}
}

func containsWarning(warnings []string, want string) bool {
	for _, w := range warnings {
		if w == want {
			return true
		}
	}
	return false
}

func TestErrFromGameCheck(t *testing.T) {
	tests := []struct {
		problem  string
		wantKind LauncherErrorKind
		wantMsg  string
	}{
		{problem: "no game path", wantKind: KindGameExeMissing, wantMsg: "no game path"},
		{problem: "game executable not found", wantKind: KindGameExeMissing, wantMsg: "game executable not found"},
		{problem: "not a local file", wantKind: KindGamePathInvalid, wantMsg: "not a local file"},
		{problem: "game path is not a file", wantKind: KindGamePathInvalid, wantMsg: "game path is not a file"},
		{problem: "game path is not an exe", wantKind: KindGamePathInvalid, wantMsg: "game path is not an exe"},
		{problem: "game executable is not signed by NEXON", wantKind: KindGamePathInvalid, wantMsg: "game executable is not signed by NEXON"},
	}
	for _, tt := range tests {
		t.Run(tt.problem, func(t *testing.T) {
			err := errFromGameCheck(GameCheck{Problem: tt.problem})
			if err.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", err.Kind, tt.wantKind)
			}
			if err.Msg != tt.wantMsg {
				t.Errorf("Msg = %q, want %q", err.Msg, tt.wantMsg)
			}
		})
	}
}
