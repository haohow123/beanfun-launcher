//go:build !windows

package alertsound

func init() {
	listBuiltinFn = func() ([]string, error) { return nil, nil }
}
