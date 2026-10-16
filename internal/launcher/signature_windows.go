//go:build windows

package launcher

func init() {
	verifyGameSignatureFn = func(string) signatureVerdict {
		return signatureVerdict{OK: true, Reason: "not implemented"}
	}
}
