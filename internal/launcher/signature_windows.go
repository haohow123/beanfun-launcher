//go:build windows

package launcher

import (
	"encoding/binary"
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

	imagehlp                    = windows.NewLazySystemDLL("imagehlp.dll")
	procImageGetCertificateData = imagehlp.NewProc("ImageGetCertificateData")
)

const (
	cmsgSignerCertInfoParam = 7 // CMSG_SIGNER_CERT_INFO_PARAM
	oidOrganization         = "2.5.4.10"
	oidSerialNumber         = "2.5.4.5"

	winCertHeaderSize         = 8       // WIN_CERTIFICATE: dwLength, wRevision, wCertificateType
	winCertTypePKCSSignedData = 0x0002  // WIN_CERT_TYPE_PKCS_SIGNED_DATA
	maxCertificateSize        = 1 << 20 // the file declares the entry size, so it is capped before allocating
)

// openForVerify opens path with write and delete sharing denied, so the file stays unchanged until the handle closes.
func openForVerify(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	h, err := windows.CreateFile(p,
		windows.GENERIC_READ|windows.FILE_EXECUTE|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("CreateFile(%s): %w", path, err)
	}
	return h, nil
}

// verifySignature checks the Authenticode chain offline, then compares the signer subject against NEXON's.
func verifySignature(path string) signatureVerdict {
	h, err := openForVerify(path)
	if err != nil {
		return signatureVerdict{Reason: "unreadable"}
	}
	defer func() { _ = windows.CloseHandle(h) }()
	return verifySignatureHandle(h, path)
}

// verifySignatureHandle runs both checks against the already-open h, passing path only because WinVerifyTrust requires it.
func verifySignatureHandle(h windows.Handle, path string) signatureVerdict {
	if err := winVerifyTrust(h, path); err != nil {
		if errors.Is(err, syscall.Errno(windows.TRUST_E_NOSIGNATURE)) {
			return signatureVerdict{Reason: "unsigned"}
		}
		var errno syscall.Errno
		if errors.As(err, &errno) {
			return signatureVerdict{Reason: fmt.Sprintf("invalid (0x%08X)", uint32(errno))}
		}
		return signatureVerdict{Reason: "invalid"}
	}
	org, serial, err := signerSubject(h)
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
func winVerifyTrust(h windows.Handle, path string) error {
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
			File:     h,
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
func signerSubject(h windows.Handle) (org, serial string, err error) {
	pkcs7, err := embeddedPKCS7(h)
	if err != nil {
		return "", "", err
	}
	blob := windows.CryptDataBlob{Size: uint32(len(pkcs7)), Data: &pkcs7[0]}
	var store, msg windows.Handle
	err = windows.CryptQueryObject(
		windows.CERT_QUERY_OBJECT_BLOB,
		unsafe.Pointer(&blob),
		windows.CERT_QUERY_CONTENT_FLAG_PKCS7_SIGNED,
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
	if size == 0 {
		return "", "", errors.New("CryptMsgGetParam: empty signer info")
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
	// A missing SERIALNUMBER is left empty for signerMatches to reject, not reported as a read failure.
	serial, _ = certNameAttr(cert, oidSerialNumber)
	return org, serial, nil
}

// embeddedPKCS7 reads entry 0 of the PE certificate table through h.
// https://learn.microsoft.com/en-us/windows/win32/api/imagehlp/nf-imagehlp-imagegetcertificatedata
func embeddedPKCS7(h windows.Handle) ([]byte, error) {
	var size uint32
	r, _, e := procImageGetCertificateData.Call(uintptr(h), 0, 0, uintptr(unsafe.Pointer(&size)))
	if r == 0 && !errors.Is(e, windows.ERROR_INSUFFICIENT_BUFFER) {
		return nil, fmt.Errorf("ImageGetCertificateData size: %w", e)
	}
	if size <= winCertHeaderSize {
		return nil, errors.New("ImageGetCertificateData: no certificate")
	}
	if size > maxCertificateSize {
		return nil, fmt.Errorf("certificate entry of %d bytes exceeds %d", size, maxCertificateSize)
	}
	// uint64 backing keeps the WIN_CERTIFICATE header aligned.
	buf := make([]uint64, (size+7)/8)
	r, _, e = procImageGetCertificateData.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return nil, fmt.Errorf("ImageGetCertificateData: %w", e)
	}
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&buf[0])), len(buf)*8)
	length := binary.LittleEndian.Uint32(raw[0:4])
	certType := binary.LittleEndian.Uint16(raw[6:8])
	if certType != winCertTypePKCSSignedData {
		return nil, fmt.Errorf("certificate type 0x%04X is not PKCS#7", certType)
	}
	if length <= winCertHeaderSize || length > size {
		return nil, fmt.Errorf("certificate length %d out of range", length)
	}
	return append([]byte(nil), raw[winCertHeaderSize:length]...), nil
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
