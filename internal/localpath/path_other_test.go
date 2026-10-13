//go:build !windows

package localpath

import (
	"errors"
	"testing"
)

// TestCheck_DevRules exercises the dev-only (non-Windows) build's CheckFn
// directly, unaffected by whichever OS built this test binary.
func TestCheck_DevRules(t *testing.T) {
	orig := CheckFn
	CheckFn = checkDevPath
	t.Cleanup(func() { CheckFn = orig })

	tests := []struct {
		name    string
		path    string
		wantErr error
	}{
		{name: "relative path", path: "x.wav", wantErr: ErrNotLocal},
		{name: "forward-slash UNC", path: `//host/share/x.wav`, wantErr: ErrNotLocal},
		{name: "local absolute path", path: "/tmp/ding.wav"},
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
