//go:build windows

package launcher

import (
	"context"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() {
	spawnFn = osSpawn
}

// osSpawn invokes CreateProcessW with the given executable and args.
// The new process is detached — the launcher closes the parent's
// handles to the process + main thread immediately. We don't track
// game lifetime in Milestone 6.
//
// path is the absolute path to game.exe; args is the argv tail (NOT
// including argv[0] — Windows expects the full command line to start
// with the program name, so we re-emit `path` as the first token of
// CommandLine).
func osSpawn(_ context.Context, path string, args []string) error {
	cmdLine := buildCommandLine(path, args)

	pathW, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ErrSpawnFailed(fmt.Errorf("UTF16PtrFromString(path): %w", err))
	}
	cmdW, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		return ErrSpawnFailed(fmt.Errorf("UTF16PtrFromString(cmdLine): %w", err))
	}

	si := &windows.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(*si))
	pi := &windows.ProcessInformation{}

	if err := windows.CreateProcess(
		pathW,
		cmdW,
		nil,   // process security attributes
		nil,   // thread security attributes
		false, // inherit handles
		0,     // creation flags
		nil,   // environment (inherit parent)
		nil,   // current directory (inherit parent)
		si,
		pi,
	); err != nil {
		return ErrSpawnFailed(fmt.Errorf("CreateProcessW: %w", err))
	}

	_ = windows.CloseHandle(pi.Process)
	_ = windows.CloseHandle(pi.Thread)
	return nil
}

// buildCommandLine assembles the Win32 lpCommandLine string. Each
// token containing whitespace gets double-quoted with embedded `"`
// escaped per Microsoft's parsing rules
// (https://learn.microsoft.com/en-us/cpp/cpp/main-function-command-line-args#parsing-c-command-line-arguments).
// Beanfun's game args (e.g. `/u:T9abc123 /p:OTP56789`) have no spaces
// or quotes themselves, so the path is the only token that typically
// needs quoting.
func buildCommandLine(path string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, quoteIfNeeded(path))
	for _, a := range args {
		parts = append(parts, quoteIfNeeded(a))
	}
	return joinSpace(parts)
}

func quoteIfNeeded(s string) string {
	needsQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '"' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return s
	}
	out := make([]byte, 0, len(s)+4)
	out = append(out, '"')
	backslashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			backslashes++
		case '"':
			// Double the run of backslashes preceding ", then escape the ".
			for j := 0; j < backslashes*2; j++ {
				out = append(out, '\\')
			}
			out = append(out, '\\', '"')
			backslashes = 0
		default:
			for j := 0; j < backslashes; j++ {
				out = append(out, '\\')
			}
			out = append(out, c)
			backslashes = 0
		}
	}
	for j := 0; j < backslashes*2; j++ {
		out = append(out, '\\')
	}
	out = append(out, '"')
	return string(out)
}

func joinSpace(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	total := len(parts) - 1
	for _, p := range parts {
		total += len(p)
	}
	out := make([]byte, 0, total)
	out = append(out, parts[0]...)
	for _, p := range parts[1:] {
		out = append(out, ' ')
		out = append(out, p...)
	}
	return string(out)
}
