package alertsound

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

var errTestUnreadable = errors.New("settings: file unreadable")

// scriptedStore returns loadErrs[i] on the i-th LoadPrefs call (nil past the end) and the real prefs otherwise.
type scriptedStore struct {
	fakeStore
	real     Prefs
	loadErrs []error
	loads    int
}

func (s *scriptedStore) LoadPrefs() (Prefs, error) {
	i := s.loads
	s.loads++
	if i < len(s.loadErrs) && s.loadErrs[i] != nil {
		return Prefs{}, s.loadErrs[i]
	}
	return s.real, nil
}

func TestService_MutationsKeepCustomListAfterFailedStartupRead(t *testing.T) {
	withFakeCatalog(t, []string{"Alarm01.wav"}, nil)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.wav")
	b := filepath.Join(dir, "b.wav")
	added := filepath.Join(dir, "c.wav")
	if err := os.WriteFile(added, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	origValidate := validateWAVFn
	validateWAVFn = func(string) error { return nil }
	t.Cleanup(func() { validateWAVFn = origValidate })

	tests := []struct {
		name       string
		act        func(s *Service) error
		wantCustom []string
	}{
		{
			name:       "Select",
			act:        func(s *Service) error { return s.Select(Sound{Kind: KindBuiltin, Name: "Alarm01.wav"}) },
			wantCustom: []string{a, b},
		},
		{
			name: "AddCustom",
			act: func(s *Service) error {
				_, err := s.AddCustom(added)
				return err
			},
			wantCustom: []string{a, b, added},
		},
		{
			name:       "RemoveCustom",
			act:        func(s *Service) error { return s.RemoveCustom(b) },
			wantCustom: []string{a},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &scriptedStore{
				real:     Prefs{Selected: DefaultSound, Custom: []string{a, b}},
				loadErrs: []error{errTestUnreadable},
			}
			svc := NewService(store)

			if err := tc.act(svc); err != nil {
				t.Fatalf("%s() = %v, want nil", tc.name, err)
			}
			if !reflect.DeepEqual(store.prefs.Custom, tc.wantCustom) {
				t.Fatalf("saved Custom = %v, want %v", store.prefs.Custom, tc.wantCustom)
			}
		})
	}
}

func TestService_MutationsFailWhileStoreStaysUnreadable(t *testing.T) {
	withFakeCatalog(t, []string{"Alarm01.wav"}, nil)
	added := filepath.Join(t.TempDir(), "c.wav")
	if err := os.WriteFile(added, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	origValidate := validateWAVFn
	validateWAVFn = func(string) error { return nil }
	t.Cleanup(func() { validateWAVFn = origValidate })

	tests := []struct {
		name string
		act  func(s *Service) error
	}{
		{"Select", func(s *Service) error { return s.Select(Sound{Kind: KindBuiltin, Name: "Alarm01.wav"}) }},
		{"AddCustom", func(s *Service) error {
			_, err := s.AddCustom(added)
			return err
		}},
		{"RemoveCustom", func(s *Service) error { return s.RemoveCustom(added) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &scriptedStore{loadErrs: slices.Repeat([]error{errTestUnreadable}, 10)}
			svc := NewService(store)

			err := tc.act(svc)
			if !errors.Is(err, errTestUnreadable) {
				t.Fatalf("%s() = %v, want the unreadable error", tc.name, err)
			}
			if store.saved != 0 {
				t.Fatalf("store.saved = %d, want 0", store.saved)
			}
		})
	}
}

func TestService_DoesNotReloadAfterSuccessfulStartup(t *testing.T) {
	withFakeCatalog(t, []string{"Alarm01.wav"}, nil)
	store := &scriptedStore{real: Prefs{Selected: DefaultSound}}
	svc := NewService(store)

	if _, err := svc.Options(); err != nil {
		t.Fatalf("Options() = %v", err)
	}
	svc.Selected()
	if err := svc.Select(Sound{Kind: KindBuiltin, Name: "Alarm01.wav"}); err != nil {
		t.Fatalf("Select() = %v", err)
	}
	if store.loads != 1 {
		t.Fatalf("LoadPrefs calls = %d, want 1", store.loads)
	}
}
