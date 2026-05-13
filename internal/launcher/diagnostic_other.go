//go:build !windows

package launcher

import "context"

func startDiagnostic(_ context.Context)                 {}
func startEventDiagnostic(_ context.Context, _ uintptr) {}
