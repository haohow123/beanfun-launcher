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
	"github.com/haohow123/beanfun-launcher/internal/bgtask"
	"github.com/haohow123/beanfun-launcher/internal/diag"
	"github.com/haohow123/beanfun-launcher/internal/launcher"
	"github.com/haohow123/beanfun-launcher/internal/maple"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is the release tag this binary was built from. The release
// workflow injects the value via `-ldflags="-X main.version=$tag"`;
// dev builds keep the default. Used to namespace the log file so
// upgrading to a new alpha leaves the previous run's log intact.
var version = "dev"

func main() {
	// Before setupLogging so the elapsed figure in a crash record
	// covers logging setup too.
	diag.MarkStart()

	logFile := setupLogging()
	if logFile != nil {
		defer func() { _ = logFile.Close() }()
	}

	// Background-task manager — shared across services so app shutdown
	// hooks one StopAll(). Each service registers its goroutines
	// (beanfun keep-alive heartbeat, launcher game-exit watcher,
	// MapleStory status heartbeat) under unique names; mgr supersedes
	// per-name on re-registration. See internal/bgtask docs.
	mgr := bgtask.New()

	loginSvc := beanfun.NewLoginService(mgr)
	launcherSvc := launcher.NewLauncherService(loginSvc, mgr)

	// notifSvc backs the offline→online server toast (see
	// maple.Checker.OnServerOnline). Wails calls ServiceStartup
	// on it during app.Run, which on Windows registers the
	// AppUserModelID + COM activator needed for toast delivery.
	// On macOS / Linux dev the SendNotification call below
	// degrades to slog.Warn — no special-casing needed here.
	notifSvc := notifications.New()
	notifyServerOnline := func() {
		if err := notifSvc.SendNotification(notifications.NotificationOptions{
			ID:    "maple-server-online",
			Title: "新楓之谷 MapleStory",
			Body:  "伺服器已開啟",
		}); err != nil {
			slog.Warn("notify: SendNotification failed", "err", err)
		}
	}

	// MapleService starts its status-probe heartbeat immediately
	// (firstDelay=0 in NewMapleService) so the Hero indicator
	// transitions from "checking…" to a real green/red dot within
	// seconds of app start, not minutes.
	mapleSvc := maple.NewMapleService(mgr, nil, notifyServerOnline)

	app := application.New(application.Options{
		Name:        "beanfun-launcher",
		Description: "Personal third-party Beanfun launcher",
		Services: []application.Service{
			application.NewService(loginSvc),
			application.NewService(launcherSvc),
			application.NewService(mapleSvc),
			application.NewService(notifSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		// go-webview2 exits the process on any MoveFocus error and prints
		// the error to a stdout that -H windowsgui detaches; this keeps it.
		ErrorHandler: diag.WebviewError,
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:         "Beanfun Launcher",
		Width:         480,
		Height:        640,
		DisableResize: true,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})

	// RegisterHook, not OnWindowEvent: hooks run synchronously while
	// listeners each get a goroutine, and the crash can land within
	// milliseconds of readiness.
	win.RegisterHook(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		diag.NoteRuntimeReady()
	})

	// Stop every background goroutine on app shutdown so we don't
	// leak watchers / heartbeats past process exit.
	app.OnShutdown(mgr.StopAll)

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
// Credentials are redacted where they are emitted: beanfun.Session.String(),
// query-stripped URLs and scrubbed error strings in the login flow,
// describeBody in place of page previews, MaskSID for account identifiers,
// and len-only logging of OTP tokens. diag.WebviewError logs whatever
// Wails and go-webview2 hand it, scrubbed through the same backstop, but
// their message shapes are not ours to audit. Not covered: raw server text
// still reaches the log inside error messages — withBodyBytes on QR-init
// parse failures, ErrServerMessage, ErrOTPServerRejected — and this file
// has no size bound, so it grows for the life of a release tag.
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
