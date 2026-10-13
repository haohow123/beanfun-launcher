package settings

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/alertsound"
)

func TestFile_Load_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	f := NewFile(path)

	got := f.Load()
	want := Defaults()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
	if got.AlertSound.Selected != alertsound.DefaultSound {
		t.Fatalf("Load().AlertSound.Selected = %+v, want %+v", got.AlertSound.Selected, alertsound.DefaultSound)
	}
}

func TestFile_SaveLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	f := NewFile(path)

	gameExe := filepath.Join(t.TempDir(), "MapleStory.exe")
	want := Settings{
		AlertSound: alertsound.Prefs{
			Selected: alertsound.Sound{Kind: alertsound.KindBuiltin, Name: "Alarm01.wav"},
			Custom:   []string{"/u/a.wav", "/u/b.wav"},
		},
		GameExePath: gameExe,
	}
	if err := f.Save(want); err != nil {
		t.Fatalf("Save() = %v, want nil", err)
	}

	got := f.Load()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

// TestFile_SaveLoadGameExePath_RoundTrip exercises LoadGameExePath /
// SaveGameExePath directly (launcher.GamePathStore's interface), as
// opposed to TestFile_SaveLoad_RoundTrip which goes through Settings as a
// whole.
func TestFile_SaveLoadGameExePath_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	f := NewFile(path)
	want := filepath.Join(t.TempDir(), "MapleStory.exe")

	if err := f.SaveGameExePath(want); err != nil {
		t.Fatalf("SaveGameExePath() = %v, want nil", err)
	}
	if got := f.LoadGameExePath(); got != want {
		t.Fatalf("LoadGameExePath() = %q, want %q", got, want)
	}
}

// TestFile_SaveGameExePath_OtherSectionUntouched asserts SaveGameExePath
// re-reads the on-disk document and only replaces GameExePath, mirroring
// SavePrefs's contract for AlertSound.
func TestFile_SaveGameExePath_OtherSectionUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	f := NewFile(path)
	wantAlert := alertsound.Prefs{Selected: alertsound.Sound{Kind: alertsound.KindBuiltin, Name: "Alarm01.wav"}}
	if err := f.Save(Settings{AlertSound: wantAlert}); err != nil {
		t.Fatalf("Save() = %v, want nil", err)
	}

	gameExe := filepath.Join(t.TempDir(), "MapleStory.exe")
	if err := f.SaveGameExePath(gameExe); err != nil {
		t.Fatalf("SaveGameExePath() = %v, want nil", err)
	}

	got := f.Load()
	if !reflect.DeepEqual(got.AlertSound, wantAlert) {
		t.Fatalf("Load().AlertSound = %+v, want %+v (untouched by SaveGameExePath)", got.AlertSound, wantAlert)
	}
	if got.GameExePath != gameExe {
		t.Fatalf("Load().GameExePath = %q, want %q", got.GameExePath, gameExe)
	}
}

// TestFile_Load_InvalidGameExePath_Retained asserts a persisted relative
// path (which fails localpath.Check on every platform) is kept on load, not
// silently dropped — checkGameExe is what surfaces the problem downstream.
func TestFile_Load_InvalidGameExePath_Retained(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	// alertSound.selected.kind must be valid ("none" here) so isValidKind
	// doesn't short-circuit to Defaults() before the GameExePath check
	// below ever runs.
	const doc = `{"alertSound":{"selected":{"kind":"none"}},"gameExePath":"relative/MapleStory.exe"}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	f := NewFile(path)

	got := f.Load()
	if got.GameExePath != "relative/MapleStory.exe" {
		t.Fatalf("Load().GameExePath = %q, want unchanged %q", got.GameExePath, "relative/MapleStory.exe")
	}
	if got.AlertSound.Selected.Kind != alertsound.KindNone {
		t.Fatalf("Load().AlertSound.Selected.Kind = %q, want %q (sanity: this path shouldn't have fallen through to Defaults())", got.AlertSound.Selected.Kind, alertsound.KindNone)
	}
}

// TestFile_Load_GameExePath_Trimmed asserts surrounding whitespace is
// stripped on load, same as SetGamePath does on save.
func TestFile_Load_GameExePath_Trimmed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	const doc = `{"alertSound":{"selected":{"kind":"none"}},"gameExePath":"  C:\\Game\\MapleStory.exe  "}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	f := NewFile(path)

	got := f.Load()
	if want := `C:\Game\MapleStory.exe`; got.GameExePath != want {
		t.Fatalf("Load().GameExePath = %q, want %q", got.GameExePath, want)
	}
}

func TestFile_Load_CorruptJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	f := NewFile(path)

	got := f.Load()
	if want := Defaults(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

func TestFile_Load_UnknownKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"alertSound":{"selected":{"kind":"bogus"}}}`), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	f := NewFile(path)

	got := f.Load()
	if want := Defaults(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

// TestFile_Load_UnknownKind_KeepsGameExePath asserts an invalid AlertSound
// kind only resets AlertSound, not the whole document — GameExePath (set
// independently from the settings page's game-path section) must survive.
func TestFile_Load_UnknownKind_KeepsGameExePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	const doc = `{"alertSound":{"selected":{"kind":"bogus"}},"gameExePath":"C:\\Game\\MapleStory.exe"}`
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	f := NewFile(path)

	got := f.Load()
	if want := Defaults().AlertSound; !reflect.DeepEqual(got.AlertSound, want) {
		t.Fatalf("Load().AlertSound = %+v, want defaults %+v", got.AlertSound, want)
	}
	const wantPath = `C:\Game\MapleStory.exe`
	if got.GameExePath != wantPath {
		t.Fatalf("Load().GameExePath = %q, want retained %q", got.GameExePath, wantPath)
	}

	// A subsequent SavePrefs round trip must not drop GameExePath either —
	// it reads loadLocked() before overwriting AlertSound.
	if err := f.SavePrefs(got.AlertSound); err != nil {
		t.Fatalf("SavePrefs() = %v, want nil", err)
	}
	if reloaded := f.Load(); reloaded.GameExePath != wantPath {
		t.Fatalf("after SavePrefs, Load().GameExePath = %q, want retained %q", reloaded.GameExePath, wantPath)
	}
}

func TestFile_Save_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes don't apply on Windows")
	}
	// The settings subdirectory must not exist yet — MkdirAll's mode only
	// takes effect when it creates the directory, not on an existing one
	// (t.TempDir() itself would already exist with the runner's default mode).
	dir := filepath.Join(t.TempDir(), "beanfun-launcher")
	path := filepath.Join(dir, "settings.json")
	f := NewFile(path)

	if err := f.Save(Defaults()); err != nil {
		t.Fatalf("Save() = %v, want nil", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(file) = %v", err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode = %o, want 0600", mode)
	}

	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat(dir) = %v", err)
	}
	if mode := di.Mode().Perm(); mode != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", mode)
	}
}

func TestFile_Save_NoLeftoverTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	f := NewFile(path)

	if err := f.Save(Defaults()); err != nil {
		t.Fatalf("Save() = %v, want nil", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() = %v", err)
	}
	for _, e := range entries {
		if e.Name() != "settings.json" {
			t.Fatalf("unexpected leftover entry %q in %s", e.Name(), dir)
		}
	}
}
