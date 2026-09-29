package alertsound

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// statFn checks whether a resolved sound target exists; overridden in tests.
var statFn = os.Stat

// Prefs is the persisted alert-sound state: the current selection plus the
// custom-file list (Phase 3 populates Custom; Phase 2 always saves it empty).
type Prefs struct {
	Selected Sound    `json:"selected"`
	Custom   []string `json:"custom,omitempty"`
}

// PrefsStore loads and saves Prefs; internal/settings.File implements it.
type PrefsStore interface {
	LoadPrefs() Prefs
	SavePrefs(Prefs) error
}

// Option is one selectable row in the settings list.
type Option struct {
	Sound   Sound  `json:"sound"`
	Label   string `json:"label"`
	Missing bool   `json:"missing"`
}

// Selection is the currently selected sound plus whether it is playable.
type Selection struct {
	Sound   Sound `json:"sound"`
	Missing bool  `json:"missing"`
}

// Service is the Wails-bound facade over the alert-sound preference: it
// composes the player (Play), the built-in catalogue (listBuiltinFn) and a
// PrefsStore into the Options/Select/Preview/PlaySelected API the settings
// page and the offline→online hook use.
type Service struct {
	mu    sync.Mutex
	store PrefsStore
	prefs Prefs
}

// NewService loads the initial Prefs from store.
func NewService(store PrefsStore) *Service {
	return &Service{store: store, prefs: store.LoadPrefs()}
}

// Options lists none, Windows default, then the built-in catalogue.
func (s *Service) Options() ([]Option, error) {
	opts := []Option{
		{Sound: Sound{Kind: KindNone}, Label: "無聲"},
		{Sound: Sound{Kind: KindDefault}, Label: "Windows 預設"},
	}
	names, err := listBuiltinFn()
	if err != nil {
		slog.Warn("alertsound: list built-ins failed", "err", err)
		return opts, nil
	}
	for _, name := range names {
		opts = append(opts, Option{
			Sound: Sound{Kind: KindBuiltin, Name: name},
			Label: builtinLabel(name),
		})
	}
	s.mu.Lock()
	custom := append([]string(nil), s.prefs.Custom...)
	s.mu.Unlock()
	for _, path := range custom {
		opts = append(opts, Option{
			Sound:   Sound{Kind: KindCustom, Path: path},
			Label:   filepath.Base(path),
			Missing: !statOK(path),
		})
	}
	return opts, nil
}

// builtinLabel strips the file extension for display.
func builtinLabel(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// Selected returns the current selection and whether it is playable.
func (s *Service) Selected() Selection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Selection{Sound: s.prefs.Selected, Missing: !playable(s.prefs.Selected)}
}

// Select saves snd as the new selection, rejecting one Options() would not show.
func (s *Service) Select(snd Sound) error {
	if err := s.checkSelectable(snd); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.prefs
	next.Selected = snd
	if err := s.store.SavePrefs(next); err != nil {
		return err
	}
	s.prefs = next
	return nil
}

// checkSelectable rejects a sound that Options() would not offer: an
// unlisted built-in, or a custom path missing from the saved list.
func (s *Service) checkSelectable(snd Sound) error {
	switch snd.Kind {
	case KindNone, KindDefault:
		return nil
	case KindBuiltin:
		return s.checkBuiltinSelectable(snd)
	case KindCustom:
		return s.checkCustomSelectable(snd)
	}
	return errUnknownKind(snd.Kind)
}

func (s *Service) checkBuiltinSelectable(snd Sound) error {
	names, err := listBuiltinFn()
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == snd.Name {
			return nil
		}
	}
	return errNotSelectable(snd)
}

func (s *Service) checkCustomSelectable(snd Sound) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.prefs.Custom {
		if p == snd.Path {
			return nil
		}
	}
	return errNotSelectable(snd)
}

// AddCustom validates path as a WAV, adds it to the custom list (deduping by absolute path), and selects it as the current sound.
func (s *Service) AddCustom(path string) (Sound, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return Sound{}, fmt.Errorf("custom sound path %q is not absolute", path)
	}
	if err := ValidateWAV(clean); err != nil {
		return Sound{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.prefs
	if !containsPath(next.Custom, clean) {
		next.Custom = append(append([]string(nil), next.Custom...), clean)
	}
	next.Selected = Sound{Kind: KindCustom, Path: clean}
	if err := s.store.SavePrefs(next); err != nil {
		return Sound{}, err
	}
	s.prefs = next
	return next.Selected, nil
}

// RemoveCustom drops path from the custom list without touching the file on disk, falling the selection back to KindDefault if path was selected.
func (s *Service) RemoveCustom(path string) error {
	clean := filepath.Clean(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.prefs
	next.Custom = removePath(next.Custom, clean)
	if next.Selected.Kind == KindCustom && next.Selected.Path == clean {
		next.Selected = Sound{Kind: KindDefault}
	}
	if err := s.store.SavePrefs(next); err != nil {
		return err
	}
	s.prefs = next
	return nil
}

func containsPath(paths []string, path string) bool {
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}

func removePath(paths []string, path string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p != path {
			out = append(out, p)
		}
	}
	return out
}

// Preview plays snd once without changing the selection.
func (s *Service) Preview(snd Sound) error {
	return Play(snd)
}

// PlaySelected plays the current selection, falling back to KindDefault (and
// logging a warning) when it is missing.
func (s *Service) PlaySelected() {
	sel := s.Selected()
	target := sel.Sound
	if sel.Missing {
		slog.Warn("alertsound: selected sound missing, falling back to default", "sound", sel.Sound)
		target = Sound{Kind: KindDefault}
	}
	if err := Play(target); err != nil {
		slog.Warn("alertsound: play failed", "err", err)
	}
}

// playable reports whether s resolves to a target that exists on disk;
// none/default are always playable.
func playable(s Sound) bool {
	switch s.Kind {
	case KindNone, KindDefault:
		return true
	case KindBuiltin:
		dir, err := mediaDirFn()
		if err != nil {
			return false
		}
		return statOK(filepath.Join(dir, s.Name))
	case KindCustom:
		return statOK(s.Path)
	}
	return false
}

func statOK(path string) bool {
	_, err := statFn(path)
	return err == nil
}

func errUnknownKind(k Kind) error {
	return fmt.Errorf("unknown sound kind %q", k)
}

func errNotSelectable(snd Sound) error {
	return fmt.Errorf("sound %+v is not in the current option list", snd)
}
