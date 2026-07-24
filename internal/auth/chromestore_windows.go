//go:build windows

package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func browserCaptureSupported() bool { return true }

// chromeCookieDBPath returns the path to Chrome's cookie database on Windows,
// checking both the Chrome 96+ location (under Network\) and the legacy one.
// Chrome stores user data under %LOCALAPPDATA%\Google\Chrome\User Data.
func chromeCookieDBPath() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "AppData", "Local")
	}
	candidates := []string{
		filepath.Join(base, "Google", "Chrome", "User Data", "Default", "Network", "Cookies"),
		filepath.Join(base, "Google", "Chrome", "User Data", "Default", "Cookies"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return candidates[0]
}

// chromeCookieKey reads the AES-256 key Chrome stores in its "Local State"
// JSON and unprotects it via Windows DPAPI (CryptUnprotectData). Chrome 80+
// uses this key for AES-256-GCM cookie encryption.
func chromeCookieKey() ([]byte, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, "AppData", "Local")
	}
	localStatePath := filepath.Join(base, "Google", "Chrome", "User Data", "Local State")
	data, err := os.ReadFile(localStatePath)
	if err != nil {
		return nil, fmt.Errorf("read Chrome Local State: %w", err)
	}
	var ls struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(data, &ls); err != nil {
		return nil, fmt.Errorf("parse Chrome Local State: %w", err)
	}
	if ls.OSCrypt.EncryptedKey == "" {
		return nil, errors.New("no os_crypt.encrypted_key in Chrome Local State")
	}
	keyBlob, err := base64.StdEncoding.DecodeString(ls.OSCrypt.EncryptedKey)
	if err != nil {
		return nil, fmt.Errorf("decode encrypted_key: %w", err)
	}
	// Chrome prefixes the DPAPI blob with the literal "DPAPI".
	if !bytes.HasPrefix(keyBlob, []byte("DPAPI")) {
		return nil, errors.New("unexpected encrypted_key prefix (expected DPAPI)")
	}
	key, err := decryptDPAPI(keyBlob[len("DPAPI"):])
	if err != nil {
		return nil, fmt.Errorf("dpapi decrypt: %w", err)
	}
	return key, nil
}

// decryptDPAPI decrypts a Windows DPAPI blob using CryptUnprotectData.
func decryptDPAPI(blob []byte) ([]byte, error) {
	if len(blob) == 0 {
		return nil, errors.New("empty dpapi blob")
	}
	var in windows.DataBlob
	in.Size = uint32(len(blob))
	in.Data = &blob[0]
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, 0, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	outBytes := unsafe.Slice(out.Data, out.Size)
	res := make([]byte, out.Size)
	copy(res, outBytes)
	return res, nil
}

// decryptCookieValue decrypts a Windows Chrome cookie encrypted_value blob.
// Chrome 80+ stores values as: "v10" || nonce(12) || ciphertext || tag(16),
// encrypted with AES-256-GCM using the DPAPI-unprotected key from Local State.
// On DB v24+ (Chrome 130) a SHA256(host_key) digest is prepended to plaintext
// and stripped here.
func decryptCookieValue(enc, key []byte, dbVersion int) (string, error) {
	if len(enc) == 0 {
		return "", nil
	}
	// Older cookies (no v10 prefix) are stored unencrypted.
	if len(enc) < 3 || enc[0] != 'v' || enc[1] != '1' || enc[2] != '0' {
		return string(enc), nil
	}
	body := enc[3:]
	if len(body) < 12+16 {
		return "", fmt.Errorf("windows encrypted value too short: %d", len(body))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := body[:12]
	// Go's GCM expects ciphertext with the tag appended, which matches Chrome's
	// nonce || ciphertext || tag layout after stripping the nonce.
	ct := body[12:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	if dbVersion >= 24 && len(pt) >= 32 {
		pt = pt[32:]
	}
	return string(pt), nil
}
