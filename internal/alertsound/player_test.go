package alertsound

import (
	"errors"
	"path/filepath"
	"testing"
)

// call records one playFn invocation.
type call struct {
	target string
	alias  bool
}

// withFakePlayer swaps playFn/mediaDirFn with test-controlled fakes reporting
// to the returned slice, restoring the originals in t.Cleanup.
func withFakePlayer(t *testing.T, dir string) *[]call {
	t.Helper()
	origPlay := playFn
	origMediaDir := mediaDirFn

	calls := &[]call{}
	playFn = func(target string, alias bool) error {
		*calls = append(*calls, call{target: target, alias: alias})
		return nil
	}
	mediaDirFn = func() (string, error) { return dir, nil }

	t.Cleanup(func() {
		playFn = origPlay
		mediaDirFn = origMediaDir
	})
	return calls
}

func TestPlay(t *testing.T) {
	const dir = "/media/dir"

	tests := []struct {
		name    string
		sound   Sound
		want    *call
		wantErr bool
	}{
		{name: "none", sound: Sound{Kind: KindNone}, want: nil},
		{name: "default", sound: Sound{Kind: KindDefault}, want: &call{target: "SystemDefault", alias: true}},
		{name: "builtin", sound: Sound{Kind: KindBuiltin, Name: "Alarm01.wav"}, want: &call{target: filepath.Join(dir, "Alarm01.wav"), alias: false}},
		{name: "builtin path traversal", sound: Sound{Kind: KindBuiltin, Name: "../x.wav"}, wantErr: true},
		{name: "builtin embedded separator", sound: Sound{Kind: KindBuiltin, Name: `a\b.wav`}, wantErr: true},
		{name: "builtin empty name", sound: Sound{Kind: KindBuiltin, Name: ""}, wantErr: true},
		{name: "custom", sound: Sound{Kind: KindCustom, Path: "/u/ding.wav"}, want: &call{target: "/u/ding.wav", alias: false}},
		{name: "unknown kind", sound: Sound{Kind: "bogus"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := withFakePlayer(t, dir)

			err := Play(tt.sound)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Play() = nil, want error")
				}
				if len(*calls) != 0 {
					t.Fatalf("Play() called playFn %v, want no call", *calls)
				}
				return
			}
			if err != nil {
				t.Fatalf("Play() = %v, want nil", err)
			}
			if tt.want == nil {
				if len(*calls) != 0 {
					t.Fatalf("Play() called playFn %v, want no call", *calls)
				}
				return
			}
			if len(*calls) != 1 || (*calls)[0] != *tt.want {
				t.Fatalf("Play() calls = %v, want [%v]", *calls, *tt.want)
			}
		})
	}
}

func TestPlay_MediaDirError(t *testing.T) {
	origPlay := playFn
	origMediaDir := mediaDirFn
	t.Cleanup(func() {
		playFn = origPlay
		mediaDirFn = origMediaDir
	})

	wantErr := errors.New("no media dir")
	playFn = func(string, bool) error { t.Fatal("playFn should not be called"); return nil }
	mediaDirFn = func() (string, error) { return "", wantErr }

	if err := Play(Sound{Kind: KindBuiltin, Name: "x.wav"}); !errors.Is(err, wantErr) {
		t.Fatalf("Play() = %v, want %v", err, wantErr)
	}
}
