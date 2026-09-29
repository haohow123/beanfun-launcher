//go:build windows

package alertsound

import (
	"fmt"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() {
	playFn = playSound
	mediaDirFn = func() (string, error) {
		dir, err := windows.GetWindowsDirectory()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "Media"), nil
	}
}

var (
	winmm = windows.NewLazySystemDLL("winmm.dll")

	// PlaySoundW wraps Win32 PlaySoundW (winmm.dll).
	// https://learn.microsoft.com/en-us/windows/win32/api/mmsystem/nf-mmsystem-playsoundw
	procPlaySoundW = winmm.NewProc("PlaySoundW")
)

const (
	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndAlias     = 0x00010000
	sndFilename  = 0x00020000
)

// Without SND_NOSTOP a new call stops the previous sound, so previews never overlap.
func playSound(target string, alias bool) error {
	p, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	flags := uintptr(sndAsync | sndNoDefault | sndFilename)
	if alias {
		flags = uintptr(sndAsync | sndNoDefault | sndAlias)
	}
	ret, _, callErr := procPlaySoundW.Call(uintptr(unsafe.Pointer(p)), 0, flags)
	if ret == 0 {
		return fmt.Errorf("PlaySoundW %q: %w", target, callErr)
	}
	return nil
}
