package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/crypto/pbkdf2"

	_ "modernc.org/sqlite"
)

// ErrUnsupportedPlatform is returned when browser capture is attempted on a
// platform other than macOS.
var ErrUnsupportedPlatform = errors.New("browser capture is only supported on macOS with Chrome")

const (
	chromeSalt       = "saltysalt"
	chromeIterations = 1003
	chromeKeyLen     = 16
)

// ReadChromeCookies reads LinkedIn cookies from the local Chrome cookie store
// on macOS. It decrypts values using the macOS Keychain and fetches JSESSIONID
// via HTTP when it is absent from the DB (session-only cookies are not
// persisted to disk).
func ReadChromeCookies() (map[string]string, error) {
	if runtime.GOOS != "darwin" {
		return nil, ErrUnsupportedPlatform
	}

	dbPath := chromeCookieDBPath()
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("chrome cookie DB not found at %s: %w", dbPath, err)
	}

	tmpDir, err := os.MkdirTemp("", "chrome-cookies-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	tmpDB := filepath.Join(tmpDir, "Cookies")
	if err := copyFile(dbPath, tmpDB); err != nil {
		return nil, fmt.Errorf("copy cookie DB: %w", err)
	}
	for _, sfx := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(dbPath + sfx); err == nil {
			_ = copyFile(dbPath+sfx, tmpDB+sfx)
		}
	}

	db, err := sql.Open("sqlite", "file:"+tmpDB+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer db.Close()

	dbVersion := getDBVersion(db)

	pass, err := keychainPassphrase()
	if err != nil {
		return nil, fmt.Errorf("keychain: %w", err)
	}
	key := deriveAESKey(pass)

	cookies, err := queryCookies(db, key, dbVersion)
	if err != nil {
		return nil, err
	}

	if _, ok := cookies["JSESSIONID"]; !ok {
		if liAt, hasLiAt := cookies["li_at"]; hasLiAt && liAt != "" {
			if jsess, err := fetchJSESSIONID(liAt); err == nil && jsess != "" {
				cookies["JSESSIONID"] = jsess
			}
		}
	}

	return cookies, nil
}

// chromeCookieDBPath returns the path to Chrome's cookie database, checking
// both the Chrome 96+ location (under Network/) and the legacy location.
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

// keychainPassphrase retrieves the Chrome Safe Storage passphrase from the
// macOS Keychain via the `security` CLI.
func keychainPassphrase() ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-w", "-s", "Chrome Safe Storage", "-a", "Chrome").Output()
	if err != nil {
		return nil, err
	}
	return bytes.TrimRight(out, "\n"), nil
}

// deriveAESKey derives the AES-128 decryption key from the Chrome Safe Storage
// passphrase using PBKDF2-HMAC-SHA1.
func deriveAESKey(pass []byte) []byte {
	return pbkdf2.Key(pass, []byte(chromeSalt), chromeIterations, chromeKeyLen, sha1.New)
}

// decryptCookieValue decrypts a Chrome cookie encrypted_value blob. It strips
// the v10/v11 prefix, AES-128-CBC decrypts with a fixed IV (16 spaces), removes
// PKCS7 padding, and strips the SHA256(host_key) prefix on DB version >= 24.
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

// queryCookies reads all linkedin.com cookies from the database and decrypts
// their values.
func queryCookies(db *sql.DB, key []byte, dbVersion int) (map[string]string, error) {
	rows, err := db.Query(`SELECT name, encrypted_value, value FROM cookies WHERE host_key LIKE '%linkedin.com'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cookies := make(map[string]string)
	for rows.Next() {
		var name, plain string
		var enc []byte
		if err := rows.Scan(&name, &enc, &plain); err != nil {
			return nil, err
		}
		val := plain
		if len(enc) > 0 {
			decrypted, err := decryptCookieValue(enc, key, dbVersion)
			if err != nil {
				continue
			}
			val = decrypted
		}
		if val != "" {
			cookies[name] = val
		}
	}
	return cookies, rows.Err()
}

// getDBVersion reads the cookie DB format version from the meta table. Version
// >= 24 (Chrome 130+) prepends a SHA256(host_key) digest to encrypted values.
func getDBVersion(db *sql.DB) int {
	var val string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key='version'`).Scan(&val); err != nil {
		return 0
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return n
}

// fetchJSESSIONID makes a lightweight HTTP GET to linkedin.com with the li_at
// cookie and reads a fresh JSESSIONID from the Set-Cookie response. This is
// needed because JSESSIONID is often session-only and not persisted to disk.
func fetchJSESSIONID(liAt string) (string, error) {
	return fetchJSESSIONIDFromURL(liAt, "https://www.linkedin.com/")
}

func fetchJSESSIONIDFromURL(liAt, rawURL string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", "li_at="+liAt)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	for _, setCookie := range resp.Header["Set-Cookie"] {
		if strings.HasPrefix(setCookie, "JSESSIONID=") {
			val := strings.TrimPrefix(setCookie, "JSESSIONID=")
			if idx := strings.Index(val, ";"); idx >= 0 {
				val = val[:idx]
			}
			return strings.Trim(val, "\""), nil
		}
	}
	return "", nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}
