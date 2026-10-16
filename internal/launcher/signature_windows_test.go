//go:build windows

package launcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifySignature_Unsigned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.exe")
	if err := os.WriteFile(path, append([]byte("MZ"), make([]byte, 512)...), 0o600); err != nil {
		t.Fatal(err)
	}
	v := verifySignature(path)
	if v.OK || (v.Reason != "unsigned" && v.Reason != "invalid") {
		t.Fatalf("verdict = %+v, want OK=false with reason unsigned or invalid", v)
	}
}

// embeddedSignedFile returns the first candidate whose signer subject is readable.
func embeddedSignedFile(t *testing.T) (path, org string) {
	t.Helper()
	candidates := []string{
		`C:\Program Files\Git\cmd\git.exe`,
		`C:\Program Files\Git\bin\git.exe`,
	}
	if goExe, err := exec.LookPath("go"); err == nil {
		candidates = append(candidates, goExe)
	}
	var tried []string
	for _, c := range candidates {
		if _, err := os.Stat(c); err != nil {
			tried = append(tried, c+": "+err.Error())
			continue
		}
		o, _, err := signerSubject(c)
		if err != nil {
			tried = append(tried, c+": "+err.Error())
			continue
		}
		if o == "" {
			tried = append(tried, c+": empty org")
			continue
		}
		return c, o
	}
	t.Skipf("no embedded-signed candidate found: %s", strings.Join(tried, "; "))
	return "", ""
}

func TestSignerSubject_EmbeddedSigned(t *testing.T) {
	path, org := embeddedSignedFile(t)
	if org == "" {
		t.Fatalf("%s: empty org", path)
	}
}

func TestVerifySignature_RejectsNonNexon(t *testing.T) {
	path, _ := embeddedSignedFile(t)
	v := verifySignature(path)
	if v.Reason == "invalid" || v.Reason == "unsigned" {
		t.Skipf("%s did not pass WinVerifyTrust on this runner (reason %q); cannot exercise the signer check", path, v.Reason)
	}
	if v.OK || v.Reason != "wrong signer" {
		t.Fatalf("verdict = %+v, want OK=false reason \"wrong signer\"", v)
	}
}
