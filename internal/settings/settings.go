// Package settings persists non-credential user preferences (currently the
// chosen alert sound) to a JSON file under the OS's per-user config dir.
package settings

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/haohow123/beanfun-launcher/internal/alertsound"
)

// errNoConfigDir is returned by Save when NewFile was given an empty path
// (DefaultPath could not resolve os.UserConfigDir()).
var errNoConfigDir = errors.New("settings: no config directory available")

// appDirName is the subdirectory under os.UserConfigDir() (%APPDATA% on
// Windows, ~/Library/Application Support on macOS).
const appDirName = "beanfun-launcher"

// fileName is the settings file inside appDirName.
const fileName = "settings.json"

type Settings struct {
	AlertSound alertsound.Prefs `json:"alertSound"`
}

// Defaults is the Settings used when no file exists or it can't be read.
func Defaults() Settings {
	return Settings{AlertSound: alertsound.Prefs{Selected: alertsound.Sound{Kind: alertsound.KindDefault}}}
}

// isValidKind rejects a settings file naming a Kind the current binary
// doesn't understand (future field, hand-edited file, etc.).
func isValidKind(k alertsound.Kind) bool {
	switch k {
	case alertsound.KindNone, alertsound.KindDefault, alertsound.KindBuiltin, alertsound.KindCustom:
		return true
	}
	return false
}

// File is a settings document backed by one JSON file on disk, guarded by a
// mutex so Load/Save from concurrent Wails calls don't race.
type File struct {
	mu   sync.Mutex
	path string
}

// An empty path makes Load return Defaults() and Save fail, so the app runs on in-memory defaults.
func NewFile(path string) *File {
	return &File{path: path}
}

// DefaultPath returns os.UserConfigDir()/beanfun-launcher/settings.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appDirName, fileName), nil
}

// Load reads the settings file, falling back to Defaults() when it doesn't
// exist, can't be parsed, or names a Kind this binary doesn't understand.
func (f *File) Load() Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loadLocked()
}

func (f *File) loadLocked() Settings {
	if f.path == "" {
		return Defaults()
	}
	b, err := os.ReadFile(f.path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("settings: read failed, using defaults", "path", f.path, "err", err)
		}
		return Defaults()
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		slog.Warn("settings: corrupt file, using defaults", "path", f.path, "err", err)
		return Defaults()
	}
	if !isValidKind(s.AlertSound.Selected.Kind) {
		slog.Warn("settings: unknown sound kind, using defaults", "path", f.path, "kind", s.AlertSound.Selected.Kind)
		return Defaults()
	}
	return s
}

func (f *File) Save(s Settings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saveLocked(s)
}

func (f *File) saveLocked(s Settings) error {
	if f.path == "" {
		return errNoConfigDir
	}
	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "settings-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := writeAndClose(tmp, b); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, f.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// writeAndClose writes b to f, syncs it, and closes it — the sequence a
// crash-safe rename needs the data durably on disk before it replaces the
// old file.
func writeAndClose(f *os.File, b []byte) error {
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// LoadPrefs implements alertsound.PrefsStore.
func (f *File) LoadPrefs() alertsound.Prefs {
	return f.Load().AlertSound
}

// SavePrefs implements alertsound.PrefsStore: it merges p into the on-disk
// document so other Settings fields aren't clobbered.
func (f *File) SavePrefs(p alertsound.Prefs) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.loadLocked()
	s.AlertSound = p
	return f.saveLocked(s)
}
