//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "eventprobe is a Windows-only diagnostic tool.")
	fmt.Fprintln(os.Stderr, "Cross-build it from any platform with:")
	fmt.Fprintln(os.Stderr, "  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o eventprobe.exe ./cmd/eventprobe")
	os.Exit(1)
}
