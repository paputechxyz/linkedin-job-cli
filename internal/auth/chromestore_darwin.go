//go:build darwin

package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/crypto/pbkdf2"
)

const (
	chromeSalt       = "saltysalt"
	chromeIterations = 1003
	chromeKeyLen     = 16
)

func browserCaptureSupported() bool { return true }

// chromeCookieDBPath returns the path to Chrome's cookie database on macOS,
// checking both the Chrome 96+ location (under Network/) and the legacy one.
func chromeCookieDBPath() string {
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default", "Network", "Cookies"),
		filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Default", "Cookies"),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return candidates[0]
}

// chromeCookieKey retrieves the Chrome Safe Storage passphrase from the macOS
// Keychain via the `security` CLI and derives the AES-128 decryption key.
func chromeCookieKey() ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-w", "-s", "Chrome Safe Storage", "-a", "Chrome").Output()
	if err != nil {
		return nil, fmt.Errorf("keychain: %w", err)
	}
	pass := bytes.TrimRight(out, "\n")
	return deriveAESKey(pass), nil
}

// deriveAESKey derives the AES-128 decryption key from the Chrome Safe Storage
// passphrase using PBKDF2-HMAC-SHA1.
func deriveAESKey(pass []byte) []byte {
	return pbkdf2.Key(pass, []byte(chromeSalt), chromeIterations, chromeKeyLen, sha1.New)
}

// decryptCookieValue decrypts a macOS Chrome cookie encrypted_value blob. It
// strips the v10/v11 prefix, AES-128-CBC decrypts with a fixed IV (16 spaces),
// removes PKCS7 padding, and strips the SHA256(host_key) prefix on DB v24+.
func decryptCookieValue(enc, key []byte, dbVersion int) (string, error) {
	if len(enc) == 0 {
		return "", nil
	}
	if len(enc) < 3 || enc[0] != 'v' || enc[1] != '1' || (enc[2] != '0' && enc[2] != '1') {
		return string(enc), nil
	}
	body := enc[3:]
	if len(body) == 0 || len(body)%aes.BlockSize != 0 {
		return "", fmt.Errorf("encrypted value has invalid length %d", len(body))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	iv := bytes.Repeat([]byte{0x20}, 16)
	pt := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(pt, body)
	pt, err = pkcs7Unpad(pt)
	if err != nil {
		return "", err
	}
	if dbVersion >= 24 && len(pt) >= 32 {
		pt = pt[32:]
	}
	return string(pt), nil
}

func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty data")
	}
	p := int(data[len(data)-1])
	if p < 1 || p > aes.BlockSize || p > len(data) {
		return nil, fmt.Errorf("invalid PKCS7 padding byte %d", p)
	}
	for _, b := range data[len(data)-p:] {
		if int(b) != p {
			return nil, errors.New("invalid PKCS7 padding")
		}
	}
	return data[:len(data)-p], nil
}
