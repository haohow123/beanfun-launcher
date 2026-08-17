package beanfun

import (
	"crypto/des"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// buildLaunchBlob assembles a handoff blob the way the server would:
// DES-ECB encrypt, hex encode, map each hex digit to its table
// character, then splice the selector and the raw key back in. It is
// the inverse of decodeLaunchData, so no real credential is needed as
// a fixture.
func buildLaunchBlob(t *testing.T, selector int, key, plaintext string) string {
	t.Helper()
	if len(key) != 8 {
		t.Fatalf("key must be 8 chars, got %d", len(key))
	}
	if selector < 0 || selector > 15 {
		t.Fatalf("selector must fit one hex digit, got %d", selector)
	}
	padded := []byte(plaintext)
	for len(padded)%des.BlockSize != 0 {
		padded = append(padded, 0)
	}
	block, err := des.NewCipher([]byte(key))
	if err != nil {
		t.Fatalf("des.NewCipher: %v", err)
	}
	cipher := make([]byte, len(padded))
	for i := 0; i < len(padded); i += des.BlockSize {
		block.Encrypt(cipher[i:i+des.BlockSize], padded[i:i+des.BlockSize])
	}
	cipherHex := hex.EncodeToString(cipher)

	table := launchDataTables[selector%4]
	var enc strings.Builder
	for i := 0; i < len(cipherHex); i++ {
		idx := strings.IndexByte(hexDigits, cipherHex[i])
		if idx < 0 {
			t.Fatalf("hex.EncodeToString produced %q at %d", cipherHex[i], i)
		}
		enc.WriteByte(table[idx])
	}
	encoded := enc.String()
	if selector > len(encoded) {
		t.Fatalf("selector %d exceeds encoded length %d", selector, len(encoded))
	}
	return hexDigits[selector:selector+1] + encoded[:selector] + key + encoded[selector:]
}

// Synthetic values only. Both follow an obvious repeating pattern so a
// future reader can tell at a glance that neither was captured from a
// real session.
const (
	testTicket  = "ab12cd34ef56789012345678901234567890abcdefabcdefabcdef1234567890"
	testAccount = "T9000011112222333344"
)

func testLaunchPlaintext() string {
	return "LaunchTicket=" + testTicket +
		"&ServiceCode=610074" +
		"&ServiceRegion=T9" +
		"&ServiceAccount=" + testAccount +
		"&BeanfunUrl=https://tw.beanfun.com/" +
		";5f0a1"
}

func TestDecodeLaunchData_RoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		selector int
		key      string
	}{
		{name: "selector 0 selects table 0", selector: 0, key: "K3yA0000"},
		{name: "selector 1 selects table 1", selector: 1, key: "K3yB1111"},
		{name: "selector 2 selects table 2", selector: 2, key: "K3yC2222"},
		{name: "selector 3 selects table 3", selector: 3, key: "K3yD3333"},
		{name: "selector 8 wraps to table 0", selector: 8, key: "K3yE8888"},
		{name: "selector 13 wraps to table 1", selector: 13, key: "K3yF1313"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			blob := buildLaunchBlob(t, tc.selector, tc.key, testLaunchPlaintext())
			got, err := decodeLaunchData(blob)
			if err != nil {
				t.Fatalf("decodeLaunchData: %v", err)
			}
			// Also guards against LaunchTicket aliasing the plaintext:
			// decodeLaunchData zeroes that buffer on the way out, so an
			// alias would arrive here as NUL bytes.
			if string(got.LaunchTicket) != testTicket {
				t.Errorf("LaunchTicket = %q, want %q", got.LaunchTicket, testTicket)
			}
			if got.ServiceCode != "610074" {
				t.Errorf("ServiceCode = %q, want %q", got.ServiceCode, "610074")
			}
			if got.ServiceRegion != "T9" {
				t.Errorf("ServiceRegion = %q, want %q", got.ServiceRegion, "T9")
			}
			if got.ServiceAccount != testAccount {
				t.Errorf("ServiceAccount = %q, want %q", got.ServiceAccount, testAccount)
			}
		})
	}
}

// TestLaunchDataTables pins the four constants against a typo: each
// must be a complete permutation of the 16 hex digits, or the
// substitution is not invertible.
func TestLaunchDataTables(t *testing.T) {
	if len(launchDataTables) != 4 {
		t.Fatalf("expected 4 tables, got %d", len(launchDataTables))
	}
	for i, table := range launchDataTables {
		if len(table) != 16 {
			t.Errorf("table %d has %d characters, want 16", i, len(table))
			continue
		}
		seen := map[byte]bool{}
		for j := 0; j < len(table); j++ {
			c := table[j]
			if strings.IndexByte(hexDigits, c) < 0 {
				t.Errorf("table %d character %d (%q) is not a hex digit", i, j, c)
			}
			if seen[c] {
				t.Errorf("table %d repeats character %q", i, c)
			}
			seen[c] = true
		}
		if len(seen) != 16 {
			t.Errorf("table %d has %d distinct characters, want 16", i, len(seen))
		}
	}
}

func TestDecodeLaunchData_Rejects(t *testing.T) {
	valid := buildLaunchBlob(t, 8, "K3yE8888", testLaunchPlaintext())
	shortTicket := buildLaunchBlob(t, 8, "K3yE8888",
		"LaunchTicket="+testTicket[:63]+"&ServiceCode=610074")

	tests := []struct {
		name string
		blob string
	}{
		{name: "empty", blob: ""},
		{name: "too short", blob: "8abcdef"},
		{name: "selector not hex", blob: "zabcdefghij0123456789"},
		{name: "selector puts key past end", blob: "fabcdefgh"},
		{name: "character outside table", blob: valid[:20] + "!" + valid[21:]},
		{name: "wrong key decrypts to garbage", blob: valid[:9] + "XXXXXXXX" + valid[17:]},
		{name: "LaunchTicket too short", blob: shortTicket},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decodeLaunchData(tc.blob)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			var le *LoginError
			if !errors.As(err, &le) {
				t.Fatalf("error is not *LoginError: %v", err)
			}
			if le.Kind != KindLaunchDataDecode && le.Kind != KindOTPDecrypt {
				t.Errorf("Kind = %d, want KindLaunchDataDecode or KindOTPDecrypt", le.Kind)
			}
			if tc.blob != "" && strings.Contains(err.Error(), tc.blob) {
				t.Error("error message echoes the blob")
			}
			if strings.Contains(err.Error(), testTicket) {
				t.Error("error message leaks the LaunchTicket")
			}
		})
	}
}
