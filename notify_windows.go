//go:build windows

package main

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"

	toast "git.sr.ht/~jackmordaunt/go-toast/v2"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows/registry"
)

// pushServerOnlineToast pushes a silent toast directly via go-toast, bypassing
// the Wails notifier's SendNotification (which always plays its own audio) so
// alertsound.Play is the only thing that makes sound.
func pushServerOnlineToast(appName string) error {
	saveToastIcon(appName)
	opts := notifications.NotificationOptions{ID: serverOnlineID, Title: serverOnlineTitle, Body: serverOnlineBody}
	args, err := encodeActivation(opts)
	if err != nil {
		return err
	}
	n := toast.Notification{
		Title:               opts.Title,
		Body:                opts.Body,
		ActivationType:      toast.Foreground,
		ActivationArguments: args,
		Audio:               toast.Silent,
	}
	return n.Push()
}

// encodeActivation mirrors the Wails notifier's payload so its activation callback still decodes clicks.
func encodeActivation(opts notifications.NotificationOptions) (string, error) {
	b, err := json.Marshal(notifications.NotificationPayload{Action: notifications.DefaultActionIdentifier, Options: opts})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// saveToastIcon redoes the Wails notifier's per-send icon write, which Push alone skips.
func saveToastIcon(appName string) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `SOFTWARE\Classes\AppUserModelId\`+appName, registry.QUERY_VALUE)
	if err != nil {
		slog.Warn("notify: open AUMID key", "err", err)
		return
	}
	defer key.Close()
	path, _, err := key.GetStringValue("IconUri")
	if err != nil {
		slog.Warn("notify: read IconUri", "err", err)
		return
	}
	icon, err := application.NewIconFromResource(w32.GetModuleHandle(""), 3)
	if err == nil {
		err = w32.SaveHIconAsPNG(icon, path)
	}
	if err != nil {
		slog.Warn("notify: save toast icon", "err", err)
	}
}
