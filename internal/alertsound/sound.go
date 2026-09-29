// Package alertsound plays the server-online alert sound through one
// PlaySoundW path shared by the automatic alert and the settings preview.
package alertsound

import "errors"

// Kind selects how a Sound resolves to a playable target.
type Kind string

const (
	KindNone    Kind = "none"
	KindDefault Kind = "default"
	KindBuiltin Kind = "builtin"
	KindCustom  Kind = "custom"
)

// Sound identifies one selectable alert sound; Name is a built-in file name, Path a custom absolute path.
type Sound struct {
	Kind Kind   `json:"kind"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// ErrUnsupported is returned by playFn/mediaDirFn on platforms without a player binding.
var ErrUnsupported = errors.New("sound playback not supported on this platform")
