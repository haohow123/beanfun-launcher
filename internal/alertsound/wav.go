package alertsound

import (
	"errors"
	"io"
	"os"
	"unicode/utf16"
)

var (
	errNotWAV      = errors.New("not a WAV file")
	errPathTooLong = errors.New("path too long")
)

// maxPathUTF16 leaves room for PlaySoundW's 256-UTF-16-unit limit including the terminator.
const maxPathUTF16 = 255

// ValidateWAV rejects a path PlaySoundW can't be trusted to play: too long
// for its buffer, unreadable, or missing the RIFF/WAVE header.
func ValidateWAV(path string) error {
	if len(utf16.Encode([]rune(path))) > maxPathUTF16 {
		return errPathTooLong
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var h [12]byte
	if _, err := io.ReadFull(f, h[:]); err != nil {
		return errNotWAV
	}
	if string(h[0:4]) != "RIFF" || string(h[8:12]) != "WAVE" {
		return errNotWAV
	}
	return nil
}
