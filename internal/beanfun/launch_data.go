package beanfun

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

const hexDigits = "0123456789abcdef"

// launchDataTables are the four hex-digit permutations the handoff
// blob's leading selector chooses between.
var launchDataTables = [4]string{
	"bac987d65e432f10",
	"3bc4d5e6f2a79108",
	"cdbeaf9012456378",
	"4e6fb81a3c5d7092",
}

// launchInfo is the subset of the decoded handoff the OTP request
// needs. LaunchTicket stays []byte so the caller can Zero it — it is a
// live credential.
type launchInfo struct {
	LaunchTicket   []byte
	ServiceCode    string
	ServiceRegion  string
	ServiceAccount string
}

// decodeLaunchData unpacks the m_objData `data` blob: a leading hex
// digit selects a substitution table, the eight raw characters at
// offset selector+1 are the DES key, and every remaining character
// maps through the table into the ciphertext hex.
func decodeLaunchData(data string) (launchInfo, error) {
	const keyLen = 8
	if len(data) < keyLen+2 {
		return launchInfo{}, ErrLaunchDataDecode(fmt.Sprintf("data too short (%d chars)", len(data)))
	}
	selector, err := strconv.ParseUint(data[:1], 16, 8)
	if err != nil {
		return launchInfo{}, ErrLaunchDataDecode("leading selector is not a hex digit")
	}
	// The whole blob after the selector is normalised first; the DES key
	// is then lifted out of the normalised text, which is why the key is
	// always eight hex characters.
	norm, err := normalizeLaunchHex(data[1:], launchDataTables[selector%4])
	if err != nil {
		return launchInfo{}, err
	}
	keyStart := int(selector) + 1
	if keyStart+keyLen > len(norm) {
		return launchInfo{}, ErrLaunchDataDecode(fmt.Sprintf(
			"selector %d puts the key past the end of %d chars", selector, len(norm)))
	}
	key := []byte(norm[keyStart : keyStart+keyLen])
	defer Zero(key)
	plain, err := desECBDecryptHex(key, norm[:keyStart]+norm[keyStart+keyLen:])
	if err != nil {
		return launchInfo{}, err
	}
	defer Zero(plain)
	return parseLaunchFields(plain)
}

// normalizeLaunchHex rewrites each character as its index within the
// table, yielding the ciphertext hex.
func normalizeLaunchHex(s, table string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		idx := strings.IndexByte(table, s[i])
		if idx < 0 {
			return "", ErrLaunchDataDecode(fmt.Sprintf(
				"character at offset %d is outside the substitution table", i))
		}
		b.WriteByte(hexDigits[idx])
	}
	return b.String(), nil
}

// parseLaunchFields reads the decoded `k=v` plaintext. Rejecting a
// malformed LaunchTicket here is what makes a wrong decode fail at the
// decode instead of at the server.
func parseLaunchFields(plain []byte) (launchInfo, error) {
	if cut := bytes.IndexByte(plain, ';'); cut >= 0 {
		plain = plain[:cut]
	}
	var info launchInfo
	// The separator is `&`; newlines are tolerated because the field
	// order and joiner are not guaranteed by any first-party spec.
	fields := bytes.FieldsFunc(plain, func(r rune) bool {
		return r == '&' || r == '\n' || r == '\r'
	})
	for _, pair := range fields {
		k, v, ok := bytes.Cut(pair, []byte("="))
		if !ok {
			continue
		}
		switch string(k) {
		case "LaunchTicket":
			// Copied so the caller zeroing the plaintext cannot wipe it.
			info.LaunchTicket = append([]byte(nil), v...)
		case "ServiceCode":
			info.ServiceCode = string(v)
		case "ServiceRegion":
			info.ServiceRegion = string(v)
		case "ServiceAccount":
			info.ServiceAccount = string(v)
		}
	}
	if !isHexBytes(info.LaunchTicket, 64) {
		Zero(info.LaunchTicket)
		return launchInfo{}, ErrLaunchDataDecode(fmt.Sprintf(
			"LaunchTicket missing or malformed (%d chars)", len(info.LaunchTicket)))
	}
	return info, nil
}

func isHexBytes(s []byte, want int) bool {
	if len(s) != want {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
