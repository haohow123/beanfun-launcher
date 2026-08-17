package beanfun

import (
	"bytes"
	"crypto/des"
	"encoding/hex"
	"fmt"
)

// decryptOTP unpacks the `1;{key8}{cipherHex}` envelope returned by
// `generic_handlers/get_webstart_otp.ashx` and decrypts the
// ciphertext with DES/ECB/NoPadding (the Beanfun WCDES wire format).
//
// Returns the plaintext OTP as a `[]byte` slice the caller owns —
// kept as bytes (not string) so the launcher can `Zero` it after the
// game process accepts it. NUL bytes at both ends are trimmed to
// match the WPF reference behaviour.
func decryptOTP(envelope string) ([]byte, error) {
	if envelope == "" {
		return nil, ErrOTPServerRejected("empty envelope")
	}
	// Server format: `<status>;<payload>` (multi-`;` payloads exist;
	// we only look at the second segment to mirror WPF's
	// response.Split(';')[1]).
	semicolonIdx := -1
	for i := 0; i < len(envelope); i++ {
		if envelope[i] == ';' {
			semicolonIdx = i
			break
		}
	}
	if semicolonIdx < 0 {
		return nil, ErrOTPServerRejected("missing ';' in envelope")
	}
	status := envelope[:semicolonIdx]
	payload := envelope[semicolonIdx+1:]
	// Strip a trailing tail segment if the server appended more `;` —
	// only the first two segments are meaningful.
	if extra := indexByte(payload, ';'); extra >= 0 {
		payload = payload[:extra]
	}
	if status != "1" {
		return nil, ErrOTPServerRejected(payload)
	}
	if len(payload) < 8 {
		return nil, ErrOTPDecrypt(fmt.Sprintf("payload < 8 bytes (got %d)", len(payload)))
	}
	return desECBDecryptHex([]byte(payload[:8]), payload[8:])
}

// desECBDecryptHex decrypts hex-encoded ciphertext with an 8-byte
// ASCII key using DES-ECB and no padding.
func desECBDecryptHex(key []byte, cipherHex string) ([]byte, error) {
	cipherBytes, err := hex.DecodeString(cipherHex)
	if err != nil {
		return nil, ErrOTPDecrypt("hex decode: " + err.Error())
	}
	if len(cipherBytes)%des.BlockSize != 0 {
		return nil, ErrOTPDecrypt(fmt.Sprintf("cipher not block-aligned (%d bytes)", len(cipherBytes)))
	}
	block, err := des.NewCipher(key)
	if err != nil {
		return nil, ErrOTPDecrypt("des.NewCipher: " + err.Error())
	}
	plain := make([]byte, len(cipherBytes))
	for i := 0; i < len(cipherBytes); i += des.BlockSize {
		block.Decrypt(plain[i:i+des.BlockSize], cipherBytes[i:i+des.BlockSize])
	}
	return bytes.Trim(plain, "\x00"), nil
}

// indexByte returns the index of the first occurrence of b in s, or
// -1. (Avoids importing strings just for IndexByte.)
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
