//go:build !windows

package launcher

import "errors"

func init() {
	findGameWindowFn = func() uintptr { return 0 }
	injectFn = func(_ uintptr, _, _ []byte) error {
		return errors.New("window injection not supported on this platform")
	}
}
