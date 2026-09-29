package alertsound

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

var statFn = os.Stat

var validateWAVFn = ValidateWAV

// Prefs is the persisted alert-sound state: the current selection plus the custom-file list.
type Prefs struct {
	Selected Sound    `json:"selected"`
	Custom   []string `json:"custom,omitempty"`
}

type PrefsStore interface {
	LoadPrefs() Prefs
	SavePrefs(Prefs) error
}

// Option is one selectable row in the settings list.
type Option struct {
	Sound   Sound  `json:"sound"`
	Label   string `json:"label"`
	Missing bool   `json:"missing"`
	Group   Group  `json:"group"`
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
	return &Service{store: store, prefs: sanitizePrefs(store.LoadPrefs())}
}

// The sanitized prefs stay in memory until the next Select, AddCustom or RemoveCustom saves them.
func sanitizePrefs(p Prefs) Prefs {
	kept := make([]string, 0, len(p.Custom))
	for _, path := range p.Custom {
		clean := filepath.Clean(path)
		if checkLocalPath(clean) != nil {
			continue
		}
		if containsPathFold(kept, clean) {
			continue
		}
		kept = append(kept, clean)
	}
	p.Custom = kept
	if p.Selected.Kind == KindCustom {
		p.Selected.Path = filepath.Clean(p.Selected.Path)
	}
	if !selectedValid(p.Selected, kept) {
		p.Selected = DefaultSound
	}
	return p
}

// Mirrors the Options() and resolve() rules, so KindDefault and anything unlisted fall through to DefaultSound.
func selectedValid(snd Sound, custom []string) bool {
	switch snd.Kind {
	case KindNone:
		return true
	case KindBuiltin:
		return validBuiltinName(snd.Name)
	case KindCustom:
		return checkLocalPath(snd.Path) == nil && containsPathFold(custom, snd.Path)
	}
	return false
}

// validBuiltinName mirrors resolve()'s guard against a tampered built-in name.
func validBuiltinName(name string) bool {
	return name != "" && !strings.ContainsAny(name, `\/`) && filepath.Base(name) == name
}

// Options lists none, then the built-in catalogue, then the custom list.
func (s *Service) Options() ([]Option, error) {
	opts := []Option{
		{Sound: Sound{Kind: KindNone}, Label: "無聲", Group: GroupNone},
	}
	names, err := listBuiltinFn()
	if err != nil {
		slog.Warn("alertsound: list built-ins failed", "err", err)
		return opts, nil
	}
	opts = append(opts, sortedBuiltinOptions(names)...)
	s.mu.Lock()
	custom := append([]string(nil), s.prefs.Custom...)
	s.mu.Unlock()
	for _, path := range custom {
		opts = append(opts, Option{
			Sound:   Sound{Kind: KindCustom, Path: path},
			Label:   filepath.Base(path),
			Missing: checkLocalPath(path) != nil || rejectLink(path) != nil || !statOK(path),
			Group:   GroupCustom,
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

// Select checks and writes under one lock so a concurrent RemoveCustom cannot invalidate the check.
func (s *Service) Select(snd Sound) error {
	if err := checkKindValid(snd); err != nil {
		return err
	}
	// listBuiltinFn does disk I/O, so it runs before the lock is taken.
	var builtinNames []string
	if snd.Kind == KindBuiltin {
		names, err := listBuiltinFn()
		if err != nil {
			return err
		}
		builtinNames = names
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	canonical, err := s.checkSelectableLocked(snd, builtinNames)
	if err != nil {
		return err
	}
	next := s.prefs
	next.Selected = canonical
	if err := s.store.SavePrefs(next); err != nil {
		return err
	}
	s.prefs = next
	return nil
}

func checkKindValid(snd Sound) error {
	switch snd.Kind {
	case KindNone, KindDefault, KindBuiltin, KindCustom:
		return nil
	}
	return errUnknownKind(snd.Kind)
}

// Callers must hold s.mu.
func (s *Service) checkSelectableLocked(snd Sound, builtinNames []string) (Sound, error) {
	switch snd.Kind {
	case KindNone:
		return snd, nil
	case KindDefault:
		return Sound{}, errNotSelectable(snd)
	case KindBuiltin:
		for _, name := range builtinNames {
			if strings.EqualFold(name, snd.Name) {
				snd.Name = name
				return snd, nil
			}
		}
		return Sound{}, errNotSelectable(snd)
	case KindCustom:
		if containsPathFold(s.prefs.Custom, snd.Path) {
			return snd, nil
		}
		return Sound{}, errNotSelectable(snd)
	}
	return Sound{}, errUnknownKind(snd.Kind)
}

// AddCustom validates path as a WAV, adds it to the custom list (deduping by absolute path), and selects it as the current sound.
func (s *Service) AddCustom(path string) (Sound, error) {
	if err := checkLocalPath(path); err != nil {
		return Sound{}, err
	}
	clean := filepath.Clean(path)
	if err := rejectLink(clean); err != nil {
		return Sound{}, err
	}
	if err := validateWAVFn(clean); err != nil {
		return Sound{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.prefs
	if !containsPathFold(next.Custom, clean) {
		next.Custom = append(append([]string(nil), next.Custom...), clean)
	}
	next.Selected = Sound{Kind: KindCustom, Path: clean}
	if err := s.store.SavePrefs(next); err != nil {
		return Sound{}, err
	}
	s.prefs = next
	return next.Selected, nil
}

// RemoveCustom drops path from the custom list without touching the file on disk, falling the selection back to DefaultSound if path was selected.
func (s *Service) RemoveCustom(path string) error {
	clean := filepath.Clean(path)
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.prefs
	next.Custom = removePathFold(next.Custom, clean)
	if next.Selected.Kind == KindCustom && strings.EqualFold(next.Selected.Path, clean) {
		next.Selected = DefaultSound
	}
	if err := s.store.SavePrefs(next); err != nil {
		return err
	}
	s.prefs = next
	return nil
}

// containsPathFold reports whether path is in paths, ignoring case (Windows
// paths are case-insensitive).
func containsPathFold(paths []string, path string) bool {
	return slices.ContainsFunc(paths, func(p string) bool { return strings.EqualFold(p, path) })
}

func removePathFold(paths []string, path string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if !strings.EqualFold(p, path) {
			out = append(out, p)
		}
	}
	return out
}

// Preview plays snd once without changing the selection.
func (s *Service) Preview(snd Sound) error {
	return Play(snd)
}

// PlaySelected falls back to DefaultSound and then to the SystemDefault alias, never replaying the same target.
func (s *Service) PlaySelected() {
	sel := s.Selected()
	target := sel.Sound
	if sel.Missing {
		slog.Warn("alertsound: selected sound missing, falling back to default", "sound", sel.Sound)
		target = DefaultSound
	}
	err := Play(target)
	if err == nil {
		return
	}
	slog.Warn("alertsound: play failed, falling back to default", "sound", target, "err", err)
	if target == DefaultSound {
		playSystemDefault()
		return
	}
	if err := Play(DefaultSound); err != nil {
		slog.Warn("alertsound: default play failed, falling back to system default", "err", err)
		playSystemDefault()
	}
}

// playSystemDefault is the last-resort fallback after DefaultSound also fails.
func playSystemDefault() {
	if err := Play(Sound{Kind: KindDefault}); err != nil {
		slog.Warn("alertsound: system default play failed", "err", err)
	}
}

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
		if checkLocalPath(s.Path) != nil || rejectLink(s.Path) != nil {
			return false
		}
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
