//go:build !windows

package main

import "errors"

var errToastUnsupported = errors.New("toast not supported on this platform")

func pushServerOnlineToast(string) error {
	return errToastUnsupported
}
