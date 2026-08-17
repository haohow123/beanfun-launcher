package beanfun

import (
	"bytes"
	"crypto/des"
	"encoding/hex"
	"fmt"
)

// decryptOTPPayload unpacks the `{key8}{cipherHex}` payload the v2 OTP
// reply carries in its data field. The retired endpoint wrapped the
// same construction in a `<status>;` prefix; that status now arrives as
// the reply's JSON result field instead.
//
// Returns the plaintext OTP as a []byte the caller owns — kept as bytes
// (not string) so the launcher can Zero it after the game process
// accepts it. NUL padding is trimmed.
func decryptOTPPayload(payload string) ([]byte, error) {
	if len(payload) < 8 {
		return nil, ErrOTPDecrypt(fmt.Sprintf("payload < 8 bytes (got %d)", len(payload)))
	}
	key := []byte(payload[:8])
	defer Zero(key)
	return desECBDecryptHex(key, payload[8:])
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
