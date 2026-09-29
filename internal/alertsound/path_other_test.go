//go:build !windows

package alertsound

import (
	"errors"
	"testing"
)

// TestCheckLocalPath_DevRules exercises the dev-only (non-Windows) build's
// checkLocalPathFn directly, unaffected by whichever OS built this test binary.
func TestCheckLocalPath_DevRules(t *testing.T) {
	orig := checkLocalPathFn
	checkLocalPathFn = checkLocalPathDev
	t.Cleanup(func() { checkLocalPathFn = orig })

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{name: "relative path", path: "x.wav", wantErr: errNotLocal},
		{name: "forward-slash UNC", path: `//host/share/x.wav`, wantErr: errNotLocal},
		{name: "local absolute path", path: "/tmp/ding.wav"},
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
