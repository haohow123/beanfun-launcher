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
}

func TestFile_SaveLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	f := NewFile(path)

	want := Settings{
		AlertSound: alertsound.Prefs{
			Selected: alertsound.Sound{Kind: alertsound.KindBuiltin, Name: "Alarm01.wav"},
			Custom:   []string{"/u/a.wav", "/u/b.wav"},
		},
	}
	if err := f.Save(want); err != nil {
		t.Fatalf("Save() = %v, want nil", err)
	}

	got := f.Load()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %+v, want %+v", got, want)
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
