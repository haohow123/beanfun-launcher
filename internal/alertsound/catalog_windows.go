//go:build windows

package alertsound

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func init() {
	listBuiltinFn = listBuiltinWAVs
}

func listBuiltinWAVs() ([]string, error) {
	dir, err := mediaDirFn()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.EqualFold(filepath.Ext(e.Name()), ".wav") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names, nil
}
