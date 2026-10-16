//go:build windows

package launcher

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() {
	verifyGameSignatureFn = verifySignature
}

// Win32 procs we need but aren't exported by golang.org/x/sys/windows.
var (
	crypt32              = windows.NewLazySystemDLL("crypt32.dll")
	procCryptMsgGetParam = crypt32.NewProc("CryptMsgGetParam")
	procCryptMsgClose    = crypt32.NewProc("CryptMsgClose")
)

const (
	cmsgSignerCertInfoParam = 7 // CMSG_SIGNER_CERT_INFO_PARAM
	oidOrganization         = "2.5.4.10"
	oidSerialNumber         = "2.5.4.5"
)

// verifySignature checks the Authenticode chain offline, then compares the signer subject against NEXON's.
func verifySignature(path string) signatureVerdict {
	if err := winVerifyTrust(path); err != nil {
		if errors.Is(err, syscall.Errno(windows.TRUST_E_NOSIGNATURE)) {
			return signatureVerdict{Reason: "unsigned"}
		}
		return signatureVerdict{Reason: "invalid"}
	}
	org, serial, err := signerSubject(path)
	if err != nil {
		return signatureVerdict{Reason: "unreadable"}
	}
	if !signerMatches(org, serial) {
		return signatureVerdict{Reason: "wrong signer", Org: org, Serial: serial}
	}
	return signatureVerdict{OK: true, Org: org, Serial: serial}
}

// winVerifyTrust wraps WinVerifyTrustEx (wintrust.dll) with revocation and network retrieval disabled.
// https://learn.microsoft.com/en-us/windows/win32/api/wintrust/nf-wintrust-winverifytrust
func winVerifyTrust(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	data := &windows.WinTrustData{
		Size:             uint32(unsafe.Sizeof(windows.WinTrustData{})),
		UIChoice:         windows.WTD_UI_NONE,
		RevocationChecks: windows.WTD_REVOKE_NONE,
		UnionChoice:      windows.WTD_CHOICE_FILE,
		StateAction:      windows.WTD_STATEACTION_VERIFY,
		ProvFlags:        windows.WTD_CACHE_ONLY_URL_RETRIEVAL,
		FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&windows.WinTrustFileInfo{
			Size:     uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})),
			FilePath: p,
		}),
	}
	verifyErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	// CLOSE must run even on failure or the provider state leaks.
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	_ = windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, data)
	return verifyErr
}

// signerSubject returns the O and SERIALNUMBER attributes of the embedded signature's signer certificate.
// https://learn.microsoft.com/en-us/windows/win32/api/wincrypt/nf-wincrypt-cryptqueryobject
func signerSubject(path string) (org, serial string, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", "", err
	}
	var store, msg windows.Handle
	err = windows.CryptQueryObject(
		windows.CERT_QUERY_OBJECT_FILE,
		unsafe.Pointer(p),
		windows.CERT_QUERY_CONTENT_FLAG_PKCS7_SIGNED_EMBED,
		windows.CERT_QUERY_FORMAT_FLAG_BINARY,
		0, nil, nil, nil, &store, &msg, nil,
	)
	if err != nil {
		return "", "", fmt.Errorf("CryptQueryObject: %w", err)
	}
	defer func() { _ = windows.CertCloseStore(store, 0) }()
	defer func() { _, _, _ = procCryptMsgClose.Call(uintptr(msg)) }()

	var size uint32
	r, _, e := procCryptMsgGetParam.Call(uintptr(msg), cmsgSignerCertInfoParam, 0, 0, uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return "", "", fmt.Errorf("CryptMsgGetParam size: %w", e)
	}
	// uint64 backing keeps the CERT_INFO struct 8-byte aligned.
	buf := make([]uint64, (size+7)/8)
	r, _, e = procCryptMsgGetParam.Call(uintptr(msg), cmsgSignerCertInfoParam, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return "", "", fmt.Errorf("CryptMsgGetParam: %w", e)
	}
	certInfo := (*windows.CertInfo)(unsafe.Pointer(&buf[0]))

	cert, err := windows.CertFindCertificateInStore(store,
		windows.X509_ASN_ENCODING|windows.PKCS_7_ASN_ENCODING, 0,
		windows.CERT_FIND_SUBJECT_CERT, unsafe.Pointer(certInfo), nil)
	if err != nil {
		return "", "", fmt.Errorf("CertFindCertificateInStore: %w", err)
	}
	defer func() { _ = windows.CertFreeCertificateContext(cert) }()

	if org, err = certNameAttr(cert, oidOrganization); err != nil {
		return "", "", err
	}
	if serial, err = certNameAttr(cert, oidSerialNumber); err != nil {
		return "", "", err
	}
	return org, serial, nil
}

// certNameAttr wraps CertGetNameStringW with CERT_NAME_ATTR_TYPE.
func certNameAttr(cert *windows.CertContext, oid string) (string, error) {
	// typePara for CERT_NAME_ATTR_TYPE is a NUL-terminated ANSI OID string, not UTF-16.
	oidPtr, err := windows.BytePtrFromString(oid)
	if err != nil {
		return "", err
	}
	n := windows.CertGetNameString(cert, windows.CERT_NAME_ATTR_TYPE, 0, unsafe.Pointer(oidPtr), nil, 0)
	if n <= 1 {
		return "", fmt.Errorf("certificate has no attribute %s", oid)
	}
	buf := make([]uint16, n)
	n = windows.CertGetNameString(cert, windows.CERT_NAME_ATTR_TYPE, 0, unsafe.Pointer(oidPtr), &buf[0], n)
	return windows.UTF16ToString(buf[:n]), nil
}
