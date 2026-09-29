//go:build !windows

package main

func pushServerOnlineToast(string) error {
	return errToastUnsupported
}
