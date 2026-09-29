package alertsound

import "testing"

func TestClassifyBuiltinGroup(t *testing.T) {
	tests := []struct {
		name string
		want Group
	}{
		{"Windows Logon.wav", GroupWindows},
		{"WINDOWS BACKGROUND.WAV", GroupWindows},
		{"windows notify.wav", GroupWindows},
		{"Alarm01.wav", GroupAlarm},
		{"ALARM.wav", GroupAlarm},
		{"Ring01.wav", GroupRing},
		{"ring.wav", GroupRing},
		{"Speech On.wav", GroupOther},
		{"tada.wav", GroupOther},
		{"Windows", GroupOther},        // no trailing space after "windows" so it does not match the "windows " prefix
		{"Windowsfoo.wav", GroupOther}, // "windows" is followed by "foo", not a space, so it does not match either
	}
	for _, tt := range tests {
		if got := classifyBuiltinGroup(tt.name); got != tt.want {
			t.Errorf("classifyBuiltinGroup(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}
