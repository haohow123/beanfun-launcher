// Package localpath implements the local-file-path rules shared by any
// feature that lets the user pick a file from disk: reject non-local paths
// (UNC shares, mapped network drives) and symlinks.
package localpath

import (
	"errors"
	"os"
	"regexp"
)

// A UNC share or mapped network drive would turn every Stat, Open and PlaySoundW call into an implicit SMB connection.
var ErrNotLocal = errors.New("not a local file")

// IsRemoteDriveFn is the test seam for the real Windows GetDriveType probe.
var IsRemoteDriveFn func(path string) bool

// CheckFn is the test seam bound to the platform-appropriate rule in init().
var CheckFn func(p string) error

var driveAbsolute = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// CheckWindowsPath is kept free of build tags so tests exercise the Windows rules on any host OS.
func CheckWindowsPath(p string) error {
	if !driveAbsolute.MatchString(p) {
		return ErrNotLocal
	}
	if IsRemoteDriveFn(p) {
		return ErrNotLocal
	}
	return nil
}

// Check applies the platform-appropriate local-path rule bound to CheckFn.
func Check(p string) error {
	return CheckFn(p)
}

// LstatFn is the test seam for RejectLink's symlink probe.
var LstatFn = os.Lstat

// RejectLink reports ErrNotLocal if p resolves to a symlink.
func RejectLink(p string) error {
	fi, err := LstatFn(p)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return ErrNotLocal
	}
	return nil
}
