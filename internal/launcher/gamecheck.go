package launcher

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/haohow123/beanfun-launcher/internal/localpath"
)

// gameStatFn is the test seam for checkGameExe's os.Stat probes.
var gameStatFn = os.Stat

// gameExeName, gameDataDirName and gameDataWzName are the MapleStory install-layout shapes checkGameExe looks for.
const (
	gameExeName     = "MapleStory.exe"
	gameDataDirName = "Data"
	gameDataWzName  = "Data.wz"
)

// Problem codes set on a failed GameCheck, shared with errFromGameCheck and the LauncherError constructors in launcher.go so every caller reports the same string for the same cause.
const (
	problemNoGamePath   = "no game path"
	problemNotLocalFile = "not a local file"
	problemNotAFile     = "game path is not a file"
	problemNotExe       = "game path is not an exe"
	problemExeMissing   = "game executable not found"
)

// Warning codes appended to a passing GameCheck for soft issues that don't affect OK.
const (
	warnUnexpectedName  = "unexpected executable name"
	warnGameDataMissing = "game data not found"
)

// GameCheck is the result of checking a candidate game executable path,
// shared by the settings page's test button and the pre-launch guard in
// SpawnGame / SpawnGameClean.
type GameCheck struct {
	Path     string   `json:"path"`
	OK       bool     `json:"ok"`
	Problem  string   `json:"problem,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// checkGameExe validates path as a launchable game executable, setting
// OK=false with a stable Problem code (see errFromGameCheck) for hard
// failures (empty, not local, missing, a directory or symlink) while soft
// issues (unexpected file name, no game data found) are Warnings that
// don't affect OK.
func checkGameExe(path string) GameCheck {
	check := GameCheck{Path: path}
	if path == "" {
		check.Problem = problemNoGamePath
		return check
	}
	if err := localpath.Check(path); err != nil {
		check.Problem = problemNotLocalFile
		return check
	}
	if !strings.EqualFold(filepath.Ext(path), ".exe") {
		check.Problem = problemNotExe
		return check
	}
	// RejectLink runs before any Stat so a symlink to a UNC share is never followed.
	if err := localpath.RejectLink(path); err != nil {
		if os.IsNotExist(err) {
			check.Problem = problemExeMissing
		} else {
			check.Problem = problemNotAFile
		}
		return check
	}
	info, err := gameStatFn(path)
	if err != nil {
		check.Problem = problemExeMissing
		return check
	}
	if info.IsDir() {
		check.Problem = problemNotAFile
		return check
	}

	check.OK = true
	if !strings.EqualFold(filepath.Base(path), gameExeName) {
		check.Warnings = append(check.Warnings, warnUnexpectedName)
	}
	if !gameDataPresent(filepath.Dir(path)) {
		check.Warnings = append(check.Warnings, warnGameDataMissing)
	}
	return check
}

// gameDataPresent reports whether either known MapleStory data-file shape
// sits in dir: a Data/ directory, or a Data.wz archive.
func gameDataPresent(dir string) bool {
	if fi, err := gameStatFn(filepath.Join(dir, gameDataDirName)); err == nil && fi.IsDir() {
		return true
	}
	_, err := gameStatFn(filepath.Join(dir, gameDataWzName))
	return err == nil
}

// errFromGameCheck turns a failed GameCheck's Problem into the matching
// *LauncherError, so Spawn*, the settings page's test button, and the
// pre-launch guard all surface the same error shape for the same cause.
func errFromGameCheck(check GameCheck) *LauncherError {
	switch check.Problem {
	case problemNotLocalFile:
		return ErrGamePathNotLocal()
	case problemNotAFile:
		return ErrGamePathNotAFile()
	case problemNotExe:
		return ErrGamePathNotExe()
	case problemExeMissing:
		return ErrGameExeMissing()
	case problemNoGamePath:
		return ErrNoGamePath()
	default:
		slog.Warn("errFromGameCheck: unrecognized problem code, falling back to ErrNoGamePath", "problem", check.Problem)
		return ErrNoGamePath()
	}
}
