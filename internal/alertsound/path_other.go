//go:build !windows

package alertsound

import (
	"path/filepath"
	"strings"
)

func init() {
	isRemoteDriveFn = func(string) bool { return false }
	checkLocalPathFn = checkLocalPathDev
}

// checkLocalPathDev is the dev-only (non-Windows) local-path check: absolute
// and not a UNC-style path (Windows rules are covered by checkWindowsPath).
func checkLocalPathDev(p string) error {
	if !filepath.IsAbs(p) {
		return errNotLocal
	}
	if strings.HasPrefix(p, `//`) {
		return errNotLocal
	}
	return nil
}
