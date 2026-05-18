//go:build windows

package launcher

import "github.com/wailsapp/wails/v3/pkg/application"

// On Windows the watcher actually runs (SpawnGame succeeds and
// restartWatcher kicks off runGameWatcher), so bind eventEmitFn to
// the live Wails app's event manager. Non-Windows leaves the no-op
// default in watcher.go.
//
// Kept in a separate file from watcher_windows.go so the syscall
// layer stays free of framework imports, and so the
// `github.com/wailsapp/wails/v3/pkg/application` import lives ONLY
// under the Windows build. That import transitively pulls in
// libgtk-3-dev + libwebkit2gtk on Linux via cgo, which the CI
// runner (ubuntu-24.04 with no apt installs) cannot satisfy — CI
// scopes `go vet ./internal/...` and `go test ./internal/...` to
// avoid main.go's Wails import for exactly this reason. Putting
// this init() in a Windows-tagged file keeps internal/launcher
// importable on Linux without any system libraries.
//
// application.Get() is nil before application.New() runs, so the
// guard matters during init/test paths that touch this code without
// a live runtime.
func init() {
	eventEmitFn = func(name string, data any) {
		if app := application.Get(); app != nil {
			app.Event.Emit(name, data)
		}
	}
}
