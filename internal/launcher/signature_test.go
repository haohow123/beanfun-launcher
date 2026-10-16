package launcher

import "testing"

func TestSignerMatches(t *testing.T) {
	tests := []struct {
		name   string
		org    string
		serial string
		want   bool
	}{
		{name: "exact match", org: nexonOrg, serial: nexonSerial, want: true},
		{name: "org differs in case", org: "nexon korea corporation", serial: nexonSerial, want: false},
		{name: "org has trailing space", org: nexonOrg + " ", serial: nexonSerial, want: false},
		{name: "serial off by one digit", org: nexonOrg, serial: "110111-3320242", want: false},
		{name: "wrong org right serial", org: "Other Corp", serial: nexonSerial, want: false},
		{name: "both empty", org: "", serial: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := signerMatches(tt.org, tt.serial); got != tt.want {
				t.Errorf("signerMatches(%q, %q) = %v, want %v", tt.org, tt.serial, got, tt.want)
			}
		})
	}
}
