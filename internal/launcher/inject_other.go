//go:build !windows

package launcher

import (
	"context"
	"errors"
	"time"
)

func init() {
	waitForGameWindowFn = func(_ context.Context, _ time.Duration) (uintptr, error) {
		return 0, errors.New("window injection not supported on this platform")
	}
	injectFn = func(_ uintptr, _, _ []byte) error {
		return errors.New("window injection not supported on this platform")
	}
}
