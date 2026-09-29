//go:build !windows

package alertsound

func init() {
	playFn = func(string, bool) error { return ErrUnsupported }
	mediaDirFn = func() (string, error) { return "", ErrUnsupported }
}
