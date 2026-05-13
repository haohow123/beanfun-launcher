package main

import (
	"embed"
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
// Paths:
//
//	Windows: %LOCALAPPDATA%\beanfun-launcher\launcher.log
//	macOS:   ~/Library/Caches/beanfun-launcher/launcher.log
//	Linux:   ~/.cache/beanfun-launcher/launcher.log
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
	path := filepath.Join(dir, "launcher.log")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		slog.Error("OpenFile failed; logging to stderr only", "path", path, "err", err)
		return nil
	}
	writer := io.MultiWriter(os.Stderr, f)
	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	slog.SetDefault(slog.New(handler))
	slog.Info("beanfun-launcher starting", "log_file", path)
	return f
}
