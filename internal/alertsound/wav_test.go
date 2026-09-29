package alertsound

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("WriteFile() = %v", err)
	}
	return path
}

func TestValidateWAV(t *testing.T) {
	validHeader := append([]byte("RIFF"), append([]byte{0, 0, 0, 0}, []byte("WAVE")...)...)

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{name: "valid PCM header", path: writeTestFile(t, "ok.wav", append(validHeader, make([]byte, 32)...))},
		{name: "mp3 renamed wav", path: writeTestFile(t, "fake.wav", append([]byte("ID3"), make([]byte, 20)...)), wantErr: errNotWAV},
		{name: "truncated", path: writeTestFile(t, "short.wav", []byte{1, 2, 3, 4, 5, 6, 7, 8}), wantErr: errNotWAV},
		{name: "riff but not wave", path: writeTestFile(t, "avi.wav", []byte("RIFF\x00\x00\x00\x00AVI ")), wantErr: errNotWAV},
		{name: "path too long", path: strings.Repeat("a", 256), wantErr: errPathTooLong},
		{name: "does not exist", path: filepath.Join(t.TempDir(), "missing.wav"), wantErr: fs.ErrNotExist},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWAV(tt.path)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateWAV() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateWAV() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
