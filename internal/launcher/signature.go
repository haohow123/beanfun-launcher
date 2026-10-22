package launcher

// nexonOrg and nexonSerial are the signer subject fields measured on a real TW MapleStory.exe.
const (
	nexonOrg    = "NEXON Korea Corporation"
	nexonSerial = "110111-3320241"
)

// signatureVerdict is the outcome of verifying a game executable's code signature.
type signatureVerdict struct {
	OK     bool
	Reason string // log-only: "unsigned", "invalid", "wrong signer", "unreadable", "image mismatch"
	Org    string
	Serial string
}

// verifyGameSignatureFn is the platform seam; each signature_*.go installs its implementation in init.
var verifyGameSignatureFn func(path string) signatureVerdict

func signerMatches(org, serial string) bool { return org == nexonOrg && serial == nexonSerial }
