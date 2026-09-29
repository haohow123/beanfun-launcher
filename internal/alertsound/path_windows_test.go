//go:build windows

package alertsound

import (
	"errors"
	"testing"
)

// TestCheckLocalPath_WindowsHost exercises checkLocalPath with the real
// isRemoteDriveFn (unlike TestCheckLocalPath_WindowsRulesOnAnyHost, which
// fakes it to run on any host OS).
func TestCheckLocalPath_WindowsHost(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{name: "backslash UNC", path: `\\host\s\x.wav`, wantErr: errNotLocal},
		{name: "NT native UNC", path: `\??\UNC\host\s\x.wav`, wantErr: errNotLocal},
		{name: "local media path", path: `C:\Windows\Media\x.wav`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkLocalPath(tt.path)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("checkLocalPath(%q) = %v, want nil", tt.path, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("checkLocalPath(%q) = %v, want %v", tt.path, err, tt.wantErr)
			}
		})
	}
}
