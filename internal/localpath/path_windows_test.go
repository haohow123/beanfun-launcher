//go:build windows

package localpath

import (
	"errors"
	"testing"
)

// TestCheck_WindowsHost exercises Check with the real IsRemoteDriveFn
// (unlike TestCheckWindowsPath, which fakes it to run on any host OS).
func TestCheck_WindowsHost(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{name: "backslash UNC", path: `\\host\s\x.wav`, wantErr: ErrNotLocal},
		{name: "NT native UNC", path: `\??\UNC\host\s\x.wav`, wantErr: ErrNotLocal},
		{name: "local media path", path: `C:\Windows\Media\x.wav`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Check(tt.path)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Check(%q) = %v, want nil", tt.path, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Check(%q) = %v, want %v", tt.path, err, tt.wantErr)
			}
		})
	}
}
