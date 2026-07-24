package auth

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

// ErrUnsupportedPlatform is returned when browser capture is attempted on a
// platform other than macOS or Windows.
var ErrUnsupportedPlatform = errors.New("browser capture is only supported on macOS or Windows with Chrome")

// ReadChromeCookies reads LinkedIn cookies from the local Chrome cookie store
// on macOS or Windows. It decrypts values using the OS-native secret store
// (macOS Keychain / Windows DPAPI) and fetches JSESSIONID via HTTP when it is
// absent from the DB (session-only cookies are not persisted to disk).
func ReadChromeCookies() (map[string]string, error) {
	if !browserCaptureSupported() {
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

	key, err := chromeCookieKey()
	if err != nil {
		return nil, err
	}

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

// queryCookies reads all linkedin.com cookies from the database and decrypts
// their values. Decryption is platform-specific (see decryptCookieValue).
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
