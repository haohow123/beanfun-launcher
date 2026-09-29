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

// Name holds a built-in file name and Path a custom absolute path.
type Sound struct {
	Kind Kind   `json:"kind"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// Group is the settings-list section an Option is displayed under.
type Group string

const (
	GroupNone    Group = "none"
	GroupWindows Group = "windows"
	GroupAlarm   Group = "alarm"
	GroupRing    Group = "ring"
	GroupOther   Group = "other"
	GroupCustom  Group = "custom"
)

// ErrUnsupported is returned by playFn/mediaDirFn on platforms without a player binding.
var ErrUnsupported = errors.New("sound playback not supported on this platform")

// KindDefault stays behind DefaultSound as the last-resort fallback, never as a listed option.
var DefaultSound = Sound{Kind: KindBuiltin, Name: "Windows Logon.wav"}
