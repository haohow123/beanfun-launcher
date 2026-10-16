package beanfun

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/haohow123/beanfun-launcher/internal/bgtask"
)

// fakeNameStore is an in-memory LoginNameStore that records every save.
type fakeNameStore struct {
	mu      sync.Mutex
	names   map[string]string
	saves   int
	saveErr error
}

func (f *fakeNameStore) LoadLoginName(mainID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.names[mainID]
}

func (f *fakeNameStore) SaveLoginName(mainID, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.saveErr != nil {
		return f.saveErr
	}
	if f.names == nil {
		f.names = map[string]string{}
	}
	if name == "" {
		delete(f.names, mainID)
	} else {
		f.names[mainID] = name
	}
	return nil
}

func (f *fakeNameStore) snapshot() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.names)
}

func newNamedService(t *testing.T, store *fakeNameStore, mainIDs ...string) *LoginService {
	t.Helper()
	s := NewLoginService(bgtask.New())
	s.NameStore = store
	for _, id := range mainIDs {
		addTestLogin(t, s, &BeanfunClient{}, id)
	}
	return s
}

func TestRenameLogin_ShowsAndStoresName(t *testing.T) {
	t.Parallel()
	store := &fakeNameStore{}
	s := newNamedService(t, store, "AAAAAA1", "BBBBBB2")
	var changes [][]LoginInfo
	s.OnLoginsChanged = func(infos []LoginInfo) { changes = append(changes, infos) }

	if err := s.RenameLogin("1", "  主帳號  "); err != nil {
		t.Fatalf("RenameLogin = %v, want nil", err)
	}

	want := []LoginInfo{{ID: "1", Label: "主帳號", Name: "主帳號"}, {ID: "2", Label: "BB***B2"}}
	if got := s.ListLogins(); !slices.Equal(got, want) {
		t.Errorf("ListLogins = %+v, want %+v", got, want)
	}
	if len(changes) != 1 || !slices.Equal(changes[0], want) {
		t.Errorf("OnLoginsChanged = %+v, want one call with %+v", changes, want)
	}
	if got := store.snapshot(); !maps.Equal(got, map[string]string{"AAAAAA1": "主帳號"}) {
		t.Errorf("stored names = %v, want AAAAAA1 → 主帳號", got)
	}
}

func TestRenameLogin_EmptyNameRestoresDefaultAndForgets(t *testing.T) {
	t.Parallel()
	store := &fakeNameStore{}
	s := newNamedService(t, store, "AAAAAA1")
	if err := s.RenameLogin("1", "主帳號"); err != nil {
		t.Fatalf("RenameLogin = %v", err)
	}

	if err := s.RenameLogin("1", "   "); err != nil {
		t.Fatalf("RenameLogin(blank) = %v, want nil", err)
	}

	want := []LoginInfo{{ID: "1", Label: "AA***A1"}}
	if got := s.ListLogins(); !slices.Equal(got, want) {
		t.Errorf("ListLogins = %+v, want %+v", got, want)
	}
	if got := store.snapshot(); len(got) != 0 {
		t.Errorf("stored names = %v, want none", got)
	}
}

func TestRenameLogin_LengthLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"twelve multibyte runes", strings.Repeat("楓", 12), false},
		{"thirteen runes", strings.Repeat("楓", 13), true},
		{"twelve after trimming", "  " + strings.Repeat("a", 12) + "  ", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &fakeNameStore{}
			s := newNamedService(t, store, "AAAAAA1")

			err := s.RenameLogin("1", tc.input)

			var le *LoginError
			if tc.wantErr {
				if !errors.As(err, &le) || le.Kind != KindLoginNameTooLong || le.Msg != "login name too long" {
					t.Fatalf("RenameLogin = %v, want KindLoginNameTooLong with message %q", err, "login name too long")
				}
				if store.saves != 0 || s.ListLogins()[0].Name != "" {
					t.Errorf("saves = %d, name = %q, want nothing saved or applied", store.saves, s.ListLogins()[0].Name)
				}
				return
			}
			if err != nil {
				t.Fatalf("RenameLogin = %v, want nil", err)
			}
			if got := s.ListLogins()[0].Name; got != strings.TrimSpace(tc.input) {
				t.Errorf("Name = %q, want %q", got, strings.TrimSpace(tc.input))
			}
		})
	}
}

func TestRenameLogin_StoreFailureAppliesNothing(t *testing.T) {
	t.Parallel()
	store := &fakeNameStore{saveErr: errors.New("disk full")}
	s := newNamedService(t, store, "AAAAAA1")
	changes := 0
	s.OnLoginsChanged = func([]LoginInfo) { changes++ }

	err := s.RenameLogin("1", "主帳號")

	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindLoginNameSave || le.Msg != "login name not saved" {
		t.Fatalf("RenameLogin = %v, want KindLoginNameSave with message %q", err, "login name not saved")
	}
	if got := s.ListLogins()[0].Label; got != "AA***A1" {
		t.Errorf("Label = %q, want the default kept", got)
	}
	if changes != 0 {
		t.Errorf("OnLoginsChanged called %d times, want 0", changes)
	}
}

func TestRenameLogin_NoMainIDStaysInMemory(t *testing.T) {
	t.Parallel()
	store := &fakeNameStore{}
	s := newNamedService(t, store, "")

	if err := s.RenameLogin("1", "訪客"); err != nil {
		t.Fatalf("RenameLogin = %v, want nil", err)
	}

	if got := s.ListLogins()[0].Label; got != "訪客" {
		t.Errorf("Label = %q, want 訪客", got)
	}
	if store.saves != 0 {
		t.Errorf("store saves = %d, want 0 for a login without MainAccountID", store.saves)
	}
}

func TestRenameLogin_UnknownLogin(t *testing.T) {
	t.Parallel()
	store := &fakeNameStore{}
	s := newNamedService(t, store, "AAAAAA1")

	err := s.RenameLogin("9", "主帳號")

	var le *LoginError
	if !errors.As(err, &le) || le.Kind != KindLoginRequired {
		t.Fatalf("RenameLogin = %v, want KindLoginRequired", err)
	}
	if store.saves != 0 {
		t.Errorf("store saves = %d, want 0", store.saves)
	}
}

// logoutDuringSave logs the login out while its name is being written.
type logoutDuringSave struct {
	fakeNameStore
	s *LoginService
}

func (l *logoutDuringSave) SaveLoginName(mainID, name string) error {
	l.s.Logout("1")
	return l.fakeNameStore.SaveLoginName(mainID, name)
}

func TestRenameLogin_LoginGoneDuringSave(t *testing.T) {
	t.Parallel()
	s := NewLoginService(bgtask.New())
	s.NameStore = &logoutDuringSave{s: s}
	addTestLogin(t, s, &BeanfunClient{}, "AAAAAA1")

	if err := s.RenameLogin("1", "主帳號"); err != nil {
		t.Fatalf("RenameLogin = %v, want nil", err)
	}
	if got := s.ListLogins(); len(got) != 0 {
		t.Errorf("ListLogins = %+v, want empty after the mid-save logout", got)
	}
}

func TestRenameLogin_WithoutStore(t *testing.T) {
	t.Parallel()
	s := NewLoginService(bgtask.New())
	addTestLogin(t, s, &BeanfunClient{}, "AAAAAA1")

	if err := s.RenameLogin("1", "主帳號"); err != nil {
		t.Fatalf("RenameLogin = %v, want nil", err)
	}
	if got := s.ListLogins()[0].Label; got != "主帳號" {
		t.Errorf("Label = %q, want 主帳號", got)
	}
}

func TestLogin_UsesStoredNameOnLogin(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "AAAAAA1", "BBBBBB2")
	s.NameStore = &fakeNameStore{names: map[string]string{"BBBBBB2": "小號"}}

	approveNext(t, s)
	approveNext(t, s)

	want := []LoginInfo{{ID: "1", Label: "AA***A1"}, {ID: "2", Label: "小號", Name: "小號"}}
	if got := s.ListLogins(); !slices.Equal(got, want) {
		t.Errorf("ListLogins = %+v, want %+v", got, want)
	}
}

func TestLogin_SameAccountRescanKeepsName(t *testing.T) {
	t.Parallel()
	s := multiLoginService(t, "AAAAAA1")
	s.NameStore = &fakeNameStore{}
	approveNext(t, s)
	if err := s.RenameLogin("1", "主帳號"); err != nil {
		t.Fatalf("RenameLogin = %v", err)
	}
	// An unreadable settings file makes the store answer "", so the name must come from memory.
	s.NameStore = &fakeNameStore{}

	approveNext(t, s)

	want := []LoginInfo{{ID: "1", Label: "主帳號", Name: "主帳號"}}
	if got := s.ListLogins(); !slices.Equal(got, want) {
		t.Errorf("ListLogins after re-scan = %+v, want %+v", got, want)
	}
}

func TestSessionEnded_UsesCustomName(t *testing.T) {
	t.Parallel()
	s := newNamedService(t, &fakeNameStore{})
	c := &BeanfunClient{}
	addTestLogin(t, s, c, "AAAAAA1")
	if err := s.RenameLogin("1", "主帳號"); err != nil {
		t.Fatalf("RenameLogin = %v", err)
	}
	var ended []SessionEnded
	s.OnSessionEnded = func(e SessionEnded) { ended = append(ended, e) }

	s.endSessionIfCurrent("1", c)

	if len(ended) != 1 || ended[0].Label != "主帳號" {
		t.Errorf("SessionEnded = %+v, want one notice labelled 主帳號", ended)
	}
}
