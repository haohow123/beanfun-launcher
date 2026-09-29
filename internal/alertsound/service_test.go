package alertsound

import (
	"errors"
	"os"
	"testing"
)

// fakeStore is an in-memory PrefsStore for Service tests.
type fakeStore struct {
	prefs   Prefs
	saveErr error
	saved   int
}

func (f *fakeStore) LoadPrefs() Prefs { return f.prefs }

func (f *fakeStore) SavePrefs(p Prefs) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.prefs = p
	f.saved++
	return nil
}

// withFakeCatalog swaps listBuiltinFn, restoring it in t.Cleanup.
func withFakeCatalog(t *testing.T, names []string, err error) {
	t.Helper()
	orig := listBuiltinFn
	listBuiltinFn = func() ([]string, error) { return names, err }
	t.Cleanup(func() { listBuiltinFn = orig })
}

// withAllStatsMissing swaps statFn to report every path as not found,
// restoring it in t.Cleanup.
func withAllStatsMissing(t *testing.T) {
	t.Helper()
	orig := statFn
	statFn = func(string) (os.FileInfo, error) { return nil, errStatMissing }
	t.Cleanup(func() { statFn = orig })
}

var errStatMissing = errors.New("stat: no such file")

func TestService_Options(t *testing.T) {
	withFakeCatalog(t, []string{"Alarm01.wav", "Ring05.wav"}, nil)
	svc := NewService(&fakeStore{})

	got, err := svc.Options()
	if err != nil {
		t.Fatalf("Options() err = %v, want nil", err)
	}
	want := []Option{
		{Sound: Sound{Kind: KindNone}, Label: "無聲"},
		{Sound: Sound{Kind: KindDefault}, Label: "Windows 預設"},
		{Sound: Sound{Kind: KindBuiltin, Name: "Alarm01.wav"}, Label: "Alarm01"},
		{Sound: Sound{Kind: KindBuiltin, Name: "Ring05.wav"}, Label: "Ring05"},
	}
	if len(got) != len(want) {
		t.Fatalf("Options() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Options()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestService_Select_BuiltinNotListed(t *testing.T) {
	withFakeCatalog(t, []string{"Alarm01.wav"}, nil)
	store := &fakeStore{}
	svc := NewService(store)

	err := svc.Select(Sound{Kind: KindBuiltin, Name: "NotThere.wav"})
	if err == nil {
		t.Fatal("Select() = nil, want error")
	}
	if store.saved != 0 {
		t.Fatalf("store.saved = %d, want 0", store.saved)
	}
}

func TestService_Select_BuiltinListed(t *testing.T) {
	withFakeCatalog(t, []string{"Alarm01.wav"}, nil)
	store := &fakeStore{}
	svc := NewService(store)

	snd := Sound{Kind: KindBuiltin, Name: "Alarm01.wav"}
	if err := svc.Select(snd); err != nil {
		t.Fatalf("Select() = %v, want nil", err)
	}
	if store.saved != 1 {
		t.Fatalf("store.saved = %d, want 1", store.saved)
	}
	if got := svc.Selected().Sound; got != snd {
		t.Fatalf("Selected().Sound = %+v, want %+v", got, snd)
	}
}

func TestService_Select_SaveError(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	wantErr := errors.New("disk full")
	store := &fakeStore{saveErr: wantErr}
	svc := NewService(store)

	err := svc.Select(Sound{Kind: KindDefault})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Select() = %v, want %v", err, wantErr)
	}
}

func TestService_Preview(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	calls := withFakePlayer(t, "/media/dir")
	svc := NewService(&fakeStore{})

	if err := svc.Preview(Sound{Kind: KindDefault}); err != nil {
		t.Fatalf("Preview() = %v, want nil", err)
	}
	want := call{target: "SystemDefault", alias: true}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Fatalf("Preview() calls = %v, want [%v]", *calls, want)
	}
}

func TestService_PlaySelected_MissingFallsBackToDefault(t *testing.T) {
	withFakeCatalog(t, []string{"Alarm01.wav"}, nil)
	calls := withFakePlayer(t, "/media/dir")
	withAllStatsMissing(t)

	store := &fakeStore{prefs: Prefs{Selected: Sound{Kind: KindBuiltin, Name: "Alarm01.wav"}}}
	svc := NewService(store)

	svc.PlaySelected()

	want := call{target: "SystemDefault", alias: true}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Fatalf("PlaySelected() calls = %v, want [%v]", *calls, want)
	}
}
