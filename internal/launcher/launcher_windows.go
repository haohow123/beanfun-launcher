//go:build windows

package launcher

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

func init() {
	spawnFn = osSpawn
}

// osSpawn launches `path` with the given args via ShellExecuteW.
//
// We use ShellExecuteW (verb=nil → default "open") rather than
// CreateProcessW because some game executables — notably MapleStory
// TW — embed `requireAdministrator` in their application manifest.
// Windows refuses CreateProcessW from a non-elevated parent in that
// case (ERROR_ELEVATION_REQUIRED = 740), but ShellExecuteW reads the
// manifest and transparently raises a UAC prompt to elevate.
//
// Outcome:
//   - Game doesn't require admin → ShellExecuteW launches it as-is,
//     no UAC prompt.
//   - Game requires admin (MapleStory) → UAC prompt; user clicks
//     yes → game launches elevated; clicks no → ERROR_CANCELLED.
//
// We don't track the spawned process's lifetime — the game runs
// independently, and the launcher's job is done once the shell
// accepts the request.
func osSpawn(_ context.Context, path string, args []string) error {
	pathW, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ErrSpawnFailed(fmt.Errorf("UTF16PtrFromString(path): %w", err))
	}
	var argsW *uint16
	if argStr := buildArgString(args); argStr != "" {
		argsW, err = windows.UTF16PtrFromString(argStr)
		if err != nil {
			return ErrSpawnFailed(fmt.Errorf("UTF16PtrFromString(args): %w", err))
		}
	}
	// Use the exe's directory as cwd so the game finds its data files.
	// Without this, the game inherits the launcher's cwd and fails
	// with "no data file" (MapleStory's resource loader is cwd-relative).
	// Manual double-click on the exe works because Windows shell sets
	// cwd to the exe dir automatically.
	cwdW, err := windows.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return ErrSpawnFailed(fmt.Errorf("UTF16PtrFromString(cwd): %w", err))
	}

	err = windows.ShellExecute(
		0,                     // hWnd — no parent window for UAC dialog
		nil,                   // verb=nil → "open" (manifest-aware launch)
		pathW,                 // file
		argsW,                 // params
		cwdW,                  // cwd — exe's directory
		windows.SW_SHOWNORMAL, // nShowCmd
	)
	if err != nil {
		if errors.Is(err, syscall.Errno(windows.ERROR_CANCELLED)) {
			return ErrSpawnFailed(fmt.Errorf("UAC elevation cancelled by user"))
		}
		return ErrSpawnFailed(fmt.Errorf("ShellExecuteW: %w", err))
	}
	return nil
}

// buildArgString concatenates command-line args with Windows
// argv-parsing quoting (https://learn.microsoft.com/en-us/cpp/cpp/main-function-command-line-args).
// ShellExecuteW's lpParameters takes the argv tail — the program
// itself is passed separately as lpFile, unlike CreateProcessW where
// lpCommandLine starts with the program name.
func buildArgString(args []string) string {
	parts := make([]string, 0, len(args))
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
