package alertsound

import (
	"errors"
	"os"
	"path/filepath"
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

// writeWAV writes a minimal valid RIFF/WAVE file under t.TempDir() and
// returns its absolute path.
func writeWAV(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	header := append([]byte("RIFF"), append([]byte{0, 0, 0, 0}, []byte("WAVE")...)...)
	if err := os.WriteFile(path, header, 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	return path
}

func TestService_AddCustom_Valid(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	store := &fakeStore{}
	svc := NewService(store)
	path := writeWAV(t, "ding.wav")

	got, err := svc.AddCustom(path)
	if err != nil {
		t.Fatalf("AddCustom() = %v, want nil", err)
	}
	want := Sound{Kind: KindCustom, Path: path}
	if got != want {
		t.Fatalf("AddCustom() = %+v, want %+v", got, want)
	}
	if store.saved != 1 {
		t.Fatalf("store.saved = %d, want 1", store.saved)
	}
	if got := svc.Selected().Sound; got != want {
		t.Fatalf("Selected().Sound = %+v, want %+v", got, want)
	}
	opts, err := svc.Options()
	if err != nil {
		t.Fatalf("Options() = %v, want nil", err)
	}
	last := opts[len(opts)-1]
	if last.Sound != want {
		t.Fatalf("last Options() entry = %+v, want %+v", last.Sound, want)
	}
}

func TestService_AddCustom_UNC(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	origCheck := checkLocalPathFn
	checkLocalPathFn = checkWindowsPath
	t.Cleanup(func() { checkLocalPathFn = origCheck })
	withFakeRemoteDrive(t, false)

	store := &fakeStore{}
	svc := NewService(store)

	_, err := svc.AddCustom(`\\host\share\x.wav`)
	if !errors.Is(err, errNotLocal) {
		t.Fatalf("AddCustom() = %v, want %v", err, errNotLocal)
	}
	if store.saved != 0 {
		t.Fatalf("store.saved = %d, want 0", store.saved)
	}
}

func TestService_AddCustom_NotWAV(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	store := &fakeStore{}
	svc := NewService(store)
	path := filepath.Join(t.TempDir(), "not-wav.wav")
	if err := os.WriteFile(path, []byte("ID3garbage"), 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}

	_, err := svc.AddCustom(path)
	if !errors.Is(err, errNotWAV) {
		t.Fatalf("AddCustom() = %v, want %v", err, errNotWAV)
	}
	if store.saved != 0 {
		t.Fatalf("store.saved = %d, want 0", store.saved)
	}
}

func TestService_AddCustom_DuplicatePath(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	svc := NewService(&fakeStore{})
	path := writeWAV(t, "ding.wav")

	if _, err := svc.AddCustom(path); err != nil {
		t.Fatalf("AddCustom() #1 = %v, want nil", err)
	}
	if _, err := svc.AddCustom(path); err != nil {
		t.Fatalf("AddCustom() #2 = %v, want nil", err)
	}

	opts, err := svc.Options()
	if err != nil {
		t.Fatalf("Options() = %v, want nil", err)
	}
	count := 0
	for _, o := range opts {
		if o.Sound.Kind == KindCustom && o.Sound.Path == path {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("custom entries for %q = %d, want 1", path, count)
	}
}

func TestService_RemoveCustom_Selected(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	svc := NewService(&fakeStore{})
	path := writeWAV(t, "ding.wav")
	if _, err := svc.AddCustom(path); err != nil {
		t.Fatalf("AddCustom() = %v, want nil", err)
	}

	if err := svc.RemoveCustom(path); err != nil {
		t.Fatalf("RemoveCustom() = %v, want nil", err)
	}
	if got, want := svc.Selected().Sound, (Sound{Kind: KindDefault}); got != want {
		t.Fatalf("Selected().Sound = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file removed from disk: Stat() = %v", err)
	}
}

func TestService_AddCustom_RejectsSymlink(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	store := &fakeStore{}
	svc := NewService(store)
	target := writeWAV(t, "real.wav")
	link := filepath.Join(filepath.Dir(target), "link.wav")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("os.Symlink unsupported: %v", err)
	}

	_, err := svc.AddCustom(link)
	if !errors.Is(err, errNotLocal) {
		t.Fatalf("AddCustom() = %v, want %v", err, errNotLocal)
	}
	if store.saved != 0 {
		t.Fatalf("store.saved = %d, want 0", store.saved)
	}
}

// TestService_CustomSymlink_Missing asserts a symlink already in the saved
// Custom list (bypassing AddCustom) is reported Missing without statFn ever
// resolving through it.
func TestService_CustomSymlink_Missing(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	target := writeWAV(t, "real.wav")
	link := filepath.Join(filepath.Dir(target), "link.wav")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("os.Symlink unsupported: %v", err)
	}
	store := &fakeStore{prefs: Prefs{Custom: []string{link}}}
	svc := NewService(store)

	opts, err := svc.Options()
	if err != nil {
		t.Fatalf("Options() = %v, want nil", err)
	}
	var found bool
	for _, o := range opts {
		if o.Sound.Kind == KindCustom && o.Sound.Path == link {
			found = true
			if !o.Missing {
				t.Fatalf("Options() entry Missing = false, want true (symlink)")
			}
		}
	}
	if !found {
		t.Fatal("symlink custom entry not found in Options()")
	}
}

func TestService_CustomMissing(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	calls := withFakePlayer(t, "/media/dir")
	const missingPath = "/does/not/exist.wav"
	store := &fakeStore{prefs: Prefs{
		Selected: Sound{Kind: KindCustom, Path: missingPath},
		Custom:   []string{missingPath},
	}}
	svc := NewService(store)

	opts, err := svc.Options()
	if err != nil {
		t.Fatalf("Options() = %v, want nil", err)
	}
	var found bool
	for _, o := range opts {
		if o.Sound.Kind == KindCustom && o.Sound.Path == missingPath {
			found = true
			if !o.Missing {
				t.Fatalf("Options() entry Missing = false, want true")
			}
		}
	}
	if !found {
		t.Fatal("missing custom entry not found in Options()")
	}

	if !svc.Selected().Missing {
		t.Fatal("Selected().Missing = false, want true")
	}

	svc.PlaySelected()
	want := call{target: "SystemDefault", alias: true}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Fatalf("PlaySelected() calls = %v, want [%v]", *calls, want)
	}
}

func TestNewService_SanitizesUNCFromPrefs(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	origCheck := checkLocalPathFn
	checkLocalPathFn = checkWindowsPath
	t.Cleanup(func() { checkLocalPathFn = origCheck })
	withFakeRemoteDrive(t, false)

	const legit = `C:\Users\a\ding.wav`
	const unc = `\\host\share\x.wav`

	var statCalls []string
	origStat := statFn
	statFn = func(p string) (os.FileInfo, error) {
		statCalls = append(statCalls, p)
		return origStat(p)
	}
	t.Cleanup(func() { statFn = origStat })

	store := &fakeStore{prefs: Prefs{
		Selected: Sound{Kind: KindCustom, Path: unc},
		Custom:   []string{unc, legit},
	}}
	svc := NewService(store)

	opts, err := svc.Options()
	if err != nil {
		t.Fatalf("Options() = %v, want nil", err)
	}
	var customPaths []string
	for _, o := range opts {
		if o.Sound.Kind == KindCustom {
			customPaths = append(customPaths, o.Sound.Path)
		}
	}
	if len(customPaths) != 1 || customPaths[0] != legit {
		t.Fatalf("custom Options() = %v, want [%q]", customPaths, legit)
	}

	if got, want := svc.Selected().Sound, (Sound{Kind: KindDefault}); got != want {
		t.Fatalf("Selected().Sound = %+v, want %+v", got, want)
	}

	for _, p := range statCalls {
		if p == unc {
			t.Fatalf("statFn was called with UNC path %q", unc)
		}
	}
}

// TestNewService_SanitizesDotDotAndDedupesCustom asserts sanitizePrefs Cleans
// a "/a/../ding.wav"-shaped entry to its canonical form, that a second entry
// differing only by case dedupes against it, and that the cleaned path (not
// the raw dotted one) is what RemoveCustom and Selected agree on.
func TestNewService_SanitizesDotDotAndDedupesCustom(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	clean := writeWAV(t, "ding.wav")
	dir := filepath.Dir(clean)
	if err := os.Mkdir(filepath.Join(dir, "a"), 0o700); err != nil {
		t.Fatalf("Mkdir() = %v", err)
	}
	dotdot := filepath.Join(dir, "a", "..", "ding.wav")
	dup := filepath.Join(dir, "DING.WAV")

	store := &fakeStore{prefs: Prefs{
		Selected: Sound{Kind: KindCustom, Path: dotdot},
		Custom:   []string{dotdot, dup},
	}}
	svc := NewService(store)

	if got, want := svc.Selected().Sound, (Sound{Kind: KindCustom, Path: clean}); got != want {
		t.Fatalf("Selected().Sound = %+v, want %+v", got, want)
	}

	opts, err := svc.Options()
	if err != nil {
		t.Fatalf("Options() = %v, want nil", err)
	}
	var customPaths []string
	for _, o := range opts {
		if o.Sound.Kind == KindCustom {
			customPaths = append(customPaths, o.Sound.Path)
		}
	}
	if len(customPaths) != 1 || customPaths[0] != clean {
		t.Fatalf("custom Options() = %v, want [%q]", customPaths, clean)
	}

	if err := svc.RemoveCustom(clean); err != nil {
		t.Fatalf("RemoveCustom() = %v, want nil", err)
	}
	opts, err = svc.Options()
	if err != nil {
		t.Fatalf("Options() = %v, want nil", err)
	}
	for _, o := range opts {
		if o.Sound.Kind == KindCustom {
			t.Fatalf("Options() contains custom entry %+v after RemoveCustom", o)
		}
	}
}

func TestService_PlaySelected_PlayErrorFallsBackToDefault(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	path := writeWAV(t, "ding.wav")

	origPlay := playFn
	origMediaDir := mediaDirFn
	var calls []call
	playFn = func(target string, alias bool) error {
		calls = append(calls, call{target: target, alias: alias})
		if target == path {
			return errors.New("playsoundw failed")
		}
		return nil
	}
	mediaDirFn = func() (string, error) { return "/media/dir", nil }
	t.Cleanup(func() {
		playFn = origPlay
		mediaDirFn = origMediaDir
	})

	store := &fakeStore{prefs: Prefs{
		Selected: Sound{Kind: KindCustom, Path: path},
		Custom:   []string{path},
	}}
	svc := NewService(store)

	svc.PlaySelected()

	want := []call{{target: path, alias: false}, {target: "SystemDefault", alias: true}}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("PlaySelected() calls = %v, want %v", calls, want)
	}
}

func TestService_AddCustom_SaveError(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	wantErr := errors.New("disk full")
	store := &fakeStore{saveErr: wantErr}
	svc := NewService(store)
	path := writeWAV(t, "ding.wav")

	if _, err := svc.AddCustom(path); !errors.Is(err, wantErr) {
		t.Fatalf("AddCustom() = %v, want %v", err, wantErr)
	}
	if got := svc.Selected().Sound; got != (Sound{Kind: KindDefault}) {
		t.Fatalf("Selected().Sound = %+v, want KindDefault (unchanged)", got)
	}
	opts, err := svc.Options()
	if err != nil {
		t.Fatalf("Options() = %v, want nil", err)
	}
	for _, o := range opts {
		if o.Sound.Kind == KindCustom {
			t.Fatalf("Options() contains custom entry %+v after failed save", o)
		}
	}
}

func TestService_RemoveCustom_SaveError(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	path := writeWAV(t, "ding.wav")
	wantErr := errors.New("disk full")
	store := &fakeStore{prefs: Prefs{
		Selected: Sound{Kind: KindCustom, Path: path},
		Custom:   []string{path},
	}}
	svc := NewService(store)
	store.saveErr = wantErr

	if err := svc.RemoveCustom(path); !errors.Is(err, wantErr) {
		t.Fatalf("RemoveCustom() = %v, want %v", err, wantErr)
	}
	want := Sound{Kind: KindCustom, Path: path}
	if got := svc.Selected().Sound; got != want {
		t.Fatalf("Selected().Sound = %+v, want %+v (unchanged)", got, want)
	}
	opts, err := svc.Options()
	if err != nil {
		t.Fatalf("Options() = %v, want nil", err)
	}
	var found bool
	for _, o := range opts {
		if o.Sound.Kind == KindCustom && o.Sound.Path == path {
			found = true
		}
	}
	if !found {
		t.Fatal("custom entry removed from Options() despite failed save")
	}
}

// TestService_AddCustom_CaseInsensitiveDuplicate swaps checkLocalPathFn,
// validateWAVFn, and lstatFn so a Windows-style drive path can be exercised
// (and deduped case-insensitively) without a matching file on disk.
func TestService_AddCustom_CaseInsensitiveDuplicate(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	origCheck := checkLocalPathFn
	checkLocalPathFn = checkWindowsPath
	origValidate := validateWAVFn
	validateWAVFn = func(string) error { return nil }
	t.Cleanup(func() {
		checkLocalPathFn = origCheck
		validateWAVFn = origValidate
	})
	withFakeRemoteDrive(t, false)
	withFakeLstat(t, func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil })

	svc := NewService(&fakeStore{})

	if _, err := svc.AddCustom(`C:\A.wav`); err != nil {
		t.Fatalf("AddCustom(#1) = %v, want nil", err)
	}
	if _, err := svc.AddCustom(`c:\a.wav`); err != nil {
		t.Fatalf("AddCustom(#2) = %v, want nil", err)
	}

	opts, err := svc.Options()
	if err != nil {
		t.Fatalf("Options() = %v, want nil", err)
	}
	count := 0
	for _, o := range opts {
		if o.Sound.Kind == KindCustom {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("custom entries = %d, want 1 (case-insensitive dedupe)", count)
	}
}

// TestService_Select_CustomCaseInsensitive asserts checkSelectableLocked's
// custom-list lookup matches regardless of case, matching Windows path semantics.
func TestService_Select_CustomCaseInsensitive(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	origCheck := checkLocalPathFn
	checkLocalPathFn = checkWindowsPath
	t.Cleanup(func() { checkLocalPathFn = origCheck })
	withFakeRemoteDrive(t, false)

	store := &fakeStore{prefs: Prefs{Custom: []string{`C:\A.wav`}}}
	svc := NewService(store)

	if err := svc.Select(Sound{Kind: KindCustom, Path: `c:\a.wav`}); err != nil {
		t.Fatalf("Select() = %v, want nil (case-insensitive match)", err)
	}
}

// TestService_RemoveCustom_SelectedCaseInsensitive asserts RemoveCustom
// clears a selection that differs from the removed path only by case.
func TestService_RemoveCustom_SelectedCaseInsensitive(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	origCheck := checkLocalPathFn
	checkLocalPathFn = checkWindowsPath
	origValidate := validateWAVFn
	validateWAVFn = func(string) error { return nil }
	t.Cleanup(func() {
		checkLocalPathFn = origCheck
		validateWAVFn = origValidate
	})
	withFakeRemoteDrive(t, false)
	withFakeLstat(t, func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil })

	svc := NewService(&fakeStore{})
	if _, err := svc.AddCustom(`C:\A.wav`); err != nil {
		t.Fatalf("AddCustom() = %v, want nil", err)
	}

	if err := svc.RemoveCustom(`c:\a.wav`); err != nil {
		t.Fatalf("RemoveCustom() = %v, want nil", err)
	}
	if got, want := svc.Selected().Sound, (Sound{Kind: KindDefault}); got != want {
		t.Fatalf("Selected().Sound = %+v, want %+v", got, want)
	}
}

// TestService_PlaySelected_DefaultFailureDoesNotRetry asserts a failed play
// of the default sound is only logged, never replayed.
func TestService_PlaySelected_DefaultFailureDoesNotRetry(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	origPlay := playFn
	var calls []call
	playFn = func(target string, alias bool) error {
		calls = append(calls, call{target: target, alias: alias})
		return errors.New("playsoundw failed")
	}
	t.Cleanup(func() { playFn = origPlay })

	store := &fakeStore{prefs: Prefs{Selected: Sound{Kind: KindDefault}}}
	svc := NewService(store)

	svc.PlaySelected()

	want := call{target: "SystemDefault", alias: true}
	if len(calls) != 1 || calls[0] != want {
		t.Fatalf("PlaySelected() calls = %v, want [%v]", calls, want)
	}
}

// TestService_CustomBecomesRemoteAfterLoad asserts the local-drive check is
// re-evaluated live (not just cached from sanitizePrefs at load time): once
// isRemoteDriveFn starts reporting the drive as remote, Options/Selected mark
// the entry Missing without ever calling statFn, and PlaySelected falls back
// to the default sound.
func TestService_CustomBecomesRemoteAfterLoad(t *testing.T) {
	withFakeCatalog(t, nil, nil)
	calls := withFakePlayer(t, "/media/dir")
	origCheck := checkLocalPathFn
	checkLocalPathFn = checkWindowsPath
	t.Cleanup(func() { checkLocalPathFn = origCheck })
	withFakeRemoteDrive(t, false)
	withFakeLstat(t, func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil })

	const path = `C:\Users\a\ding.wav`
	origStat := statFn
	var statCalls []string
	statFn = func(p string) (os.FileInfo, error) {
		statCalls = append(statCalls, p)
		return fakeFileInfo{}, nil
	}
	t.Cleanup(func() { statFn = origStat })

	store := &fakeStore{prefs: Prefs{
		Selected: Sound{Kind: KindCustom, Path: path},
		Custom:   []string{path},
	}}
	svc := NewService(store)

	// Simulate the drive turning into a mapped network drive after load.
	isRemoteDriveFn = func(string) bool { return true }

	opts, err := svc.Options()
	if err != nil {
		t.Fatalf("Options() = %v, want nil", err)
	}
	var found bool
	for _, o := range opts {
		if o.Sound.Kind == KindCustom && o.Sound.Path == path {
			found = true
			if !o.Missing {
				t.Fatalf("Options() entry Missing = false, want true")
			}
		}
	}
	if !found {
		t.Fatal("custom entry not found in Options()")
	}
	for _, p := range statCalls {
		if p == path {
			t.Fatalf("statFn was called with %q after drive became remote", path)
		}
	}

	if !svc.Selected().Missing {
		t.Fatal("Selected().Missing = false, want true")
	}

	svc.PlaySelected()
	want := call{target: "SystemDefault", alias: true}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Fatalf("PlaySelected() calls = %v, want [%v]", *calls, want)
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
