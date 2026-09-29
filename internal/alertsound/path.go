package alertsound

import (
	"errors"
	"os"
	"regexp"
)

// A UNC share or mapped network drive would turn every Stat, Open and PlaySoundW call into an implicit SMB connection.
var errNotLocal = errors.New("not a local file")

var isRemoteDriveFn func(path string) bool

var checkLocalPathFn func(p string) error

var driveAbsolute = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// Kept free of build tags so tests exercise the Windows rules on any host OS.
func checkWindowsPath(p string) error {
	if !driveAbsolute.MatchString(p) {
		return errNotLocal
	}
	if isRemoteDriveFn(p) {
		return errNotLocal
	}
	return nil
}

func checkLocalPath(p string) error {
	return checkLocalPathFn(p)
}

var lstatFn = os.Lstat

func rejectLink(p string) error {
	fi, err := lstatFn(p)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return errNotLocal
	}
	return nil
}
