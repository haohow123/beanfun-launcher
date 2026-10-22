// Package settings persists non-credential user preferences (alert sound,
// game path, login group names) to a JSON file under the OS's per-user config dir.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/haohow123/beanfun-launcher/internal/alertsound"
)

// errNoConfigDir is returned by Save when NewFile was given an empty path
// (DefaultPath could not resolve os.UserConfigDir()).
var errNoConfigDir = errors.New("settings: no config directory available")

// ErrUnreadable means the settings file exists but could not be read, so nothing is written over it.
var ErrUnreadable = errors.New("settings: file unreadable")

var errCorrupt = errors.New("settings: file corrupt")

// Problem values reported by Status.
const (
	ProblemNone       = ""
	ProblemUnreadable = "unreadable"
	ProblemCorrupt    = "corrupt"
)

// Status describes the settings file's current health for the UI.
type Status struct {
	Problem    string `json:"problem"`
	BackupPath string `json:"backupPath"`
}

// appDirName is the subdirectory under os.UserConfigDir() (%APPDATA% on
// Windows, ~/Library/Application Support on macOS).
const appDirName = "beanfun-launcher"

// fileName is the settings file inside appDirName.
const fileName = "settings.json"

type Settings struct {
	AlertSound  alertsound.Prefs  `json:"alertSound"`
	GameExePath string            `json:"gameExePath,omitempty"`
	LoginNames  map[string]string `json:"loginNames,omitempty"`
}

// Defaults is the Settings used when no file exists or it can't be read.
func Defaults() Settings {
	return Settings{AlertSound: alertsound.Prefs{Selected: alertsound.DefaultSound}}
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
	mu         sync.Mutex
	path       string
	readFile   func(string) ([]byte, error)
	backupPath string
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
// exist or can't be parsed, or resetting just AlertSound (keeping
// GameExePath) when the file names an AlertSound Kind this binary doesn't
// understand.
func (f *File) Load() Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, _ := f.loadLocked()
	return s
}

// Status re-reads the file and reports whether it is unreadable or corrupt.
func (f *File) Status() Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := f.loadLocked()
	st := Status{BackupPath: f.backupPath}
	switch {
	case errors.Is(err, ErrUnreadable):
		st.Problem = ProblemUnreadable
	case errors.Is(err, errCorrupt):
		st.Problem = ProblemCorrupt
	}
	return st
}

func (f *File) read() ([]byte, error) {
	if f.readFile != nil {
		return f.readFile(f.path)
	}
	return os.ReadFile(f.path)
}

// loadLocked returns Defaults() alongside ErrUnreadable or errCorrupt when the file can't be used.
func (f *File) loadLocked() (Settings, error) {
	if f.path == "" {
		return Defaults(), nil
	}
	b, err := f.read()
	if err != nil {
		if os.IsNotExist(err) {
			return Defaults(), nil
		}
		slog.Warn("settings: read failed, using defaults", "path", f.path, "err", err)
		return Defaults(), fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		slog.Warn("settings: corrupt file, using defaults", "path", f.path, "err", err)
		return Defaults(), fmt.Errorf("%w: %v", errCorrupt, err)
	}
	if !isValidKind(s.AlertSound.Selected.Kind) {
		slog.Warn("settings: unknown sound kind, resetting it to defaults", "path", f.path, "kind", s.AlertSound.Selected.Kind)
		s.AlertSound = Defaults().AlertSound
	}
	s.GameExePath = strings.TrimSpace(s.GameExePath)
	s.LoginNames = cleanLoginNames(s.LoginNames)
	return s, nil
}

// loadForWriteLocked refuses to proceed on an unreadable file and moves a corrupt one aside before returning Defaults().
func (f *File) loadForWriteLocked() (Settings, error) {
	s, err := f.loadLocked()
	switch {
	case err == nil:
		return s, nil
	case errors.Is(err, ErrUnreadable):
		return Settings{}, err
	}
	backup := f.path + ".corrupt-" + time.Now().Format("20060102-150405")
	if err := os.Rename(f.path, backup); err != nil {
		return Settings{}, err
	}
	f.backupPath = backup
	slog.Warn("settings: backed up corrupt file", "backup", backup)
	return s, nil
}

func cleanLoginNames(names map[string]string) map[string]string {
	clean := make(map[string]string, len(names))
	for id, name := range names {
		id, name = strings.TrimSpace(id), strings.TrimSpace(name)
		if id != "" && name != "" {
			clean[id] = name
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
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
func (f *File) LoadPrefs() (alertsound.Prefs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.loadLocked()
	if errors.Is(err, ErrUnreadable) {
		return s.AlertSound, err
	}
	return s.AlertSound, nil
}

// SavePrefs implements alertsound.PrefsStore: it merges p into the on-disk
// document so other Settings fields aren't clobbered.
func (f *File) SavePrefs(p alertsound.Prefs) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.loadForWriteLocked()
	if err != nil {
		return err
	}
	s.AlertSound = p
	return f.saveLocked(s)
}

// LoadGameExePath implements launcher.GamePathStore.
func (f *File) LoadGameExePath() string {
	return f.Load().GameExePath
}

// SaveGameExePath implements launcher.GamePathStore: it merges p into the
// on-disk document so other Settings fields aren't clobbered.
func (f *File) SaveGameExePath(p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.loadForWriteLocked()
	if err != nil {
		return err
	}
	s.GameExePath = p
	return f.saveLocked(s)
}

// LoadLoginName implements beanfun.LoginNameStore.
func (f *File) LoadLoginName(mainID string) string {
	if mainID == "" {
		return ""
	}
	return f.Load().LoginNames[mainID]
}

// SaveLoginName implements beanfun.LoginNameStore without touching the other Settings fields.
func (f *File) SaveLoginName(mainID, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, err := f.loadForWriteLocked()
	if err != nil {
		return err
	}
	if s.LoginNames == nil {
		s.LoginNames = map[string]string{}
	}
	s.LoginNames[mainID] = name
	s.LoginNames = cleanLoginNames(s.LoginNames)
	return f.saveLocked(s)
}
