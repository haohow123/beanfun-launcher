package beanfun

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// sidLogRE captures the expression logged under the "sid" slog key.
var sidLogRE = regexp.MustCompile(`"sid",\s*([^,)\n]+)`)

// sidGuardDirs are the package directories scanned; a new package that logs
// an account identifier has to be added here.
var sidGuardDirs = []string{".", "../launcher"}

// wantMinSites keeps a stale regex or directory list from passing vacuously.
const wantMinSites = 15

// TestSIDNeverLoggedRaw guards the "sid" key only — an account identifier
// logged under some other key still slips past.
func TestSIDNeverLoggedRaw(t *testing.T) {
	t.Parallel()
	found := 0
	for _, dir := range sidGuardDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("ReadDir(%s): %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%s): %v", path, err)
			}
			for _, m := range sidLogRE.FindAllStringSubmatch(string(src), -1) {
				found++
				expr := strings.TrimSpace(m[1])
				if !strings.HasPrefix(expr, "MaskSID(") && !strings.HasPrefix(expr, "beanfun.MaskSID(") {
					t.Errorf(`%s: "sid" logged as %s; wrap it in MaskSID`, path, expr)
				}
			}
		}
	}
	if found < wantMinSites {
		t.Fatalf("scanned only %d \"sid\" log sites, want >= %d — the regex or the directory list is stale", found, wantMinSites)
	}
}
