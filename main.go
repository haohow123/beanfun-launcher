package main

import (
	"embed"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/haohow123/beanfun-launcher/internal/beanfun"
	"github.com/haohow123/beanfun-launcher/internal/launcher"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is the release tag this binary was built from. The release
// workflow injects the value via `-ldflags="-X main.version=$tag"`;
// dev builds keep the default. Used to namespace the log file so
// upgrading to a new alpha leaves the previous run's log intact.
var version = "dev"

func main() {
	logFile := setupLogging()
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
	}

	loginSvc := beanfun.NewLoginService()
	launcherSvc := launcher.NewLauncherService(loginSvc)

	app := application.New(application.Options{
		Name:        "beanfun-launcher",
		Description: "Personal third-party Beanfun launcher",
		Services: []application.Service{
			application.NewService(loginSvc),
			application.NewService(launcherSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Beanfun Launcher",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// setupLogging routes slog to both stderr and a per-user log file
// under the OS's cache dir. Windows production builds use
// `-H windowsgui` so stderr is detached — the file is the only
// usable trace for end-user failure reports. Returns the open file
// handle so main() can defer-close it; nil on any error (we
// gracefully fall back to stderr-only logging).
//
// Paths (one file per release tag — append within version):
//
//	Windows: %LOCALAPPDATA%\beanfun-launcher\launcher-<version>.log
//	macOS:   ~/Library/Caches/beanfun-launcher/launcher-<version>.log
//	Linux:   ~/.cache/beanfun-launcher/launcher-<version>.log
//
// Tokens (SKey, WebToken, OTP) are already redacted at their
// log-emission sites — see beanfun.Session.String() and the OTP
// flow's `len(token)`-only logging. No new secrets exposed.
func setupLogging() *os.File {
	cache, err := os.UserCacheDir()
	if err != nil {
		slog.Error("UserCacheDir failed; logging to stderr only", "err", err)
		return nil
	}
	dir := filepath.Join(cache, "beanfun-launcher")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		slog.Error("MkdirAll failed; logging to stderr only", "dir", dir, "err", err)
		return nil
	}
	// One file per release tag (e.g. launcher-v0.1.0-alpha.13.log).
	// Same-version sessions append to the same file; a new alpha
	// build writes to its own file. Keeps logs separated by build
	// so the user doesn't have to hand-prune between version tests.
	path := filepath.Join(dir, fmt.Sprintf("launcher-%s.log", version))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		slog.Error("OpenFile failed; logging to stderr only", "path", path, "err", err)
		return nil
	}
	// Wrap stderr in a best-effort writer that always reports success.
	// On Windows production builds with -H windowsgui, os.Stderr is
	// detached — a real Write returns 0 bytes / an error, and Go's
	// io.MultiWriter propagates that error to its caller, aborting
	// the entire write chain before reaching the file. The first
	// alpha.2 build hit exactly this: log file existed but was 0
	// bytes because every slog call failed at stderr and never
	// touched the file.
	writer := io.MultiWriter(bestEffortWriter{os.Stderr}, f)
	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	slog.SetDefault(slog.New(handler))
	slog.Info("beanfun-launcher starting", "log_file", path)
	return f
}

// bestEffortWriter wraps an io.Writer and swallows its errors,
// always reporting a fully-successful write. Used to keep
// io.MultiWriter from aborting the chain when an optional sink
// (e.g. detached stderr) fails.
type bestEffortWriter struct{ w io.Writer }

func (b bestEffortWriter) Write(p []byte) (int, error) {
	_, _ = b.w.Write(p)
	return len(p), nil
}
