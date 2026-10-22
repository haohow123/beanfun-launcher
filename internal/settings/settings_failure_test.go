package settings

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/alertsound"
)

const goodSettingsJSON = `{"alertSound":{"selected":{"kind":"builtin","name":"Alarm01.wav"},"custom":["/u/a.wav"]},"gameExePath":"/g/MapleStory.exe","loginNames":{"acc1":"Main"}}`

var errReadDenied = errors.New("read: permission denied")

type saveCase struct {
	name string
	save func(f *File) error
	want func() Settings
}

func saveCases() []saveCase {
	return []saveCase{
		{
			name: "SavePrefs",
			save: func(f *File) error {
				return f.SavePrefs(alertsound.Prefs{Selected: alertsound.Sound{Kind: alertsound.KindNone}})
			},
			want: func() Settings {
				s := Defaults()
				s.AlertSound = alertsound.Prefs{Selected: alertsound.Sound{Kind: alertsound.KindNone}}
				return s
			},
		},
		{
			name: "SaveGameExePath",
			save: func(f *File) error { return f.SaveGameExePath("/x/MapleStory.exe") },
			want: func() Settings {
				s := Defaults()
				s.GameExePath = "/x/MapleStory.exe"
				return s
			},
		},
		{
			name: "SaveLoginName",
			save: func(f *File) error { return f.SaveLoginName("acc9", "Alt") },
			want: func() Settings {
				s := Defaults()
				s.LoginNames = map[string]string{"acc9": "Alt"}
				return s
			},
		},
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
}

func readBytes(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) = %v", path, err)
	}
	return string(b)
}

func unreadableFile(t *testing.T) (*File, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.json")
	writeFile(t, path, goodSettingsJSON)
	f := NewFile(path)
	f.readFile = func(string) ([]byte, error) { return nil, errReadDenied }
	return f, path
}

func TestFile_Save_UnreadableFileIsNotOverwritten(t *testing.T) {
	for _, tc := range saveCases() {
		t.Run(tc.name, func(t *testing.T) {
			f, path := unreadableFile(t)

			err := tc.save(f)
			if !errors.Is(err, ErrUnreadable) {
				t.Fatalf("%s() = %v, want ErrUnreadable", tc.name, err)
			}
			if !strings.HasPrefix(err.Error(), "settings: file unreadable") {
				t.Fatalf("error = %q, want prefix %q", err.Error(), "settings: file unreadable")
			}
			if got := readBytes(t, path); got != goodSettingsJSON {
				t.Fatalf("file changed to %q", got)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatalf("ReadDir() = %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("dir has %d entries, want only settings.json", len(entries))
			}
		})
	}
}

func TestFile_Save_CorruptFileIsBackedUp(t *testing.T) {
	const bad = "{not json"
	for _, tc := range saveCases() {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			writeFile(t, path, bad)
			f := NewFile(path)

			if err := tc.save(f); err != nil {
				t.Fatalf("%s() = %v, want nil", tc.name, err)
			}
			backups, err := filepath.Glob(path + ".corrupt-*")
			if err != nil || len(backups) != 1 {
				t.Fatalf("backups = %v (err %v), want exactly one", backups, err)
			}
			if got := readBytes(t, backups[0]); got != bad {
				t.Fatalf("backup bytes = %q, want %q", got, bad)
			}
			if got, want := f.Load(), tc.want(); !reflect.DeepEqual(got, want) {
				t.Fatalf("Load() = %+v, want %+v", got, want)
			}
			if got := f.Status(); got.Problem != ProblemNone || got.BackupPath != backups[0] {
				t.Fatalf("Status() = %+v, want no problem and BackupPath %q", got, backups[0])
			}
		})
	}
}

func TestFile_LoadPrefs_ErrorOnlyWhenUnreadable(t *testing.T) {
	t.Run("unreadable", func(t *testing.T) {
		f, _ := unreadableFile(t)
		if _, err := f.LoadPrefs(); !errors.Is(err, ErrUnreadable) {
			t.Fatalf("LoadPrefs() err = %v, want ErrUnreadable", err)
		}
	})
	t.Run("missing", func(t *testing.T) {
		f := NewFile(filepath.Join(t.TempDir(), "settings.json"))
		if _, err := f.LoadPrefs(); err != nil {
			t.Fatalf("LoadPrefs() err = %v, want nil", err)
		}
	})
	t.Run("corrupt", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.json")
		writeFile(t, path, "{not json")
		if _, err := NewFile(path).LoadPrefs(); err != nil {
			t.Fatalf("LoadPrefs() err = %v, want nil", err)
		}
	})
}

func TestFile_Status(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		f := NewFile(filepath.Join(t.TempDir(), "settings.json"))
		if got := f.Status(); got != (Status{}) {
			t.Fatalf("Status() = %+v, want zero", got)
		}
	})
	t.Run("empty path", func(t *testing.T) {
		if got := NewFile("").Status(); got != (Status{}) {
			t.Fatalf("Status() = %+v, want zero", got)
		}
	})
	t.Run("healthy", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.json")
		writeFile(t, path, goodSettingsJSON)
		if got := NewFile(path).Status(); got != (Status{}) {
			t.Fatalf("Status() = %+v, want zero", got)
		}
	})
	t.Run("unreadable", func(t *testing.T) {
		f, _ := unreadableFile(t)
		if got := f.Status(); got.Problem != ProblemUnreadable || got.BackupPath != "" {
			t.Fatalf("Status() = %+v, want unreadable", got)
		}
	})
	t.Run("corrupt", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.json")
		writeFile(t, path, "{not json")
		if got := NewFile(path).Status(); got.Problem != ProblemCorrupt || got.BackupPath != "" {
			t.Fatalf("Status() = %+v, want corrupt", got)
		}
	})
}

func TestFile_Save_MissingFileStillWorks(t *testing.T) {
	for _, tc := range saveCases() {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			f := NewFile(path)
			if err := tc.save(f); err != nil {
				t.Fatalf("%s() = %v, want nil", tc.name, err)
			}
			if got, want := f.Load(), tc.want(); !reflect.DeepEqual(got, want) {
				t.Fatalf("Load() = %+v, want %+v", got, want)
			}
			if backups, _ := filepath.Glob(path + ".corrupt-*"); len(backups) != 0 {
				t.Fatalf("unexpected backups %v", backups)
			}
		})
	}
}
