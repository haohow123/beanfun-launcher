//go:build !windows

package localpath

import (
	"path/filepath"
	"strings"
)

func init() {
	IsRemoteDriveFn = func(string) bool { return false }
	CheckFn = checkDevPath
}

// checkDevPath is the dev-only (non-Windows) local-path check: absolute
// and not a UNC-style path (Windows rules are covered by CheckWindowsPath).
func checkDevPath(p string) error {
	if !filepath.IsAbs(p) {
		return ErrNotLocal
	}
	if strings.HasPrefix(p, `//`) {
		return ErrNotLocal
	}
	return nil
}
