//go:build windows

package alertsound

import (
	"golang.org/x/sys/windows"
)

func init() {
	isRemoteDriveFn = isRemoteDrive
	checkLocalPathFn = checkWindowsPath
}

// p must already match driveAbsolute.
func isRemoteDrive(p string) bool {
	root, err := windows.UTF16PtrFromString(p[:2] + `\`)
	if err != nil {
		return false
	}
	return windows.GetDriveType(root) == windows.DRIVE_REMOTE
}
