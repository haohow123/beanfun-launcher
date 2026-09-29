package alertsound

import (
	"fmt"
	"path/filepath"
	"strings"
)

// playFn plays target asynchronously; alias selects SND_ALIAS instead of SND_FILENAME.
var playFn func(target string, alias bool) error

var mediaDirFn func() (string, error)

// Play resolves s to a PlaySoundW target and plays it.
func Play(s Sound) error {
	target, alias, err := resolve(s)
	if err != nil || target == "" {
		return err
	}
	return playFn(target, alias)
}

func resolve(s Sound) (target string, alias bool, err error) {
	switch s.Kind {
	case KindNone:
		return "", false, nil
	case KindDefault:
		return "SystemDefault", true, nil
	case KindBuiltin:
		// Reject a tampered settings file pointing a built-in outside Media.
		if s.Name == "" || filepath.Base(s.Name) != s.Name || strings.ContainsAny(s.Name, `\/`) {
			return "", false, fmt.Errorf("invalid built-in sound name %q", s.Name)
		}
		dir, err := mediaDirFn()
		if err != nil {
			return "", false, err
		}
		return filepath.Join(dir, s.Name), false, nil
	case KindCustom:
		return s.Path, false, nil
	}
	return "", false, fmt.Errorf("unknown sound kind %q", s.Kind)
}
