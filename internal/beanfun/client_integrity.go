package beanfun

import "runtime"

// The v2 OTP endpoint gates on a claim that the caller is Gamania's own
// GGMWebStart.dll: CV is that assembly's version and Hash is the
// SHA-256 of its bytes. Read from a local install of GGM 1.5.0.2
// (1,289,232 bytes) on 2026-08-18. They go stale when Gamania ships a
// new helper, and the symptom is every OTP fetch being rejected at
// once.
const (
	ggmCV        = "1.5.0.2"
	ggmDLLSHA256 = "dfd568a69d87abcd8f4a93d1a4481ebb57712d1d28ab0b6fc018fcf140101e06"
)

// ggmArch mirrors the helper's Environment.Is64BitProcess, which
// reports the calling process rather than the OS.
func ggmArch() string {
	if runtime.GOARCH == "386" {
		return "x86"
	}
	return "x64"
}
