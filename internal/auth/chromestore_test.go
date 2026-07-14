package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// encryptForTest encrypts plaintext the same way Chrome does (v10 prefix,
// AES-128-CBC, IV of 16 spaces, PKCS7 padding) for round-trip decrypt tests.
func encryptForTest(plaintext, key []byte) []byte {
	pad := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := append(plaintext, bytes.Repeat([]byte{byte(pad)}, pad)...)
	block, _ := aes.NewCipher(key)
	iv := bytes.Repeat([]byte{0x20}, 16)
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)
	return append([]byte("v10"), ct...)
}

func TestDeriveAESKey(t *testing.T) {
	key := deriveAESKey([]byte("test-passphrase"))
	if len(key) != chromeKeyLen {
		t.Fatalf("key length = %d, want %d", len(key), chromeKeyLen)
	}
	// Deterministic: same passphrase → same key.
	key2 := deriveAESKey([]byte("test-passphrase"))
	if !bytes.Equal(key, key2) {
		t.Error("deriveAESKey is not deterministic")
	}
	// Different passphrase → different key.
	key3 := deriveAESKey([]byte("other-passphrase"))
	if bytes.Equal(key, key3) {
		t.Error("different passphrase produced the same key")
	}
}

func TestDecryptCookieValueRoundTrip(t *testing.T) {
	key := deriveAESKey([]byte("test-passphrase"))
	cases := []string{
		"ajax:1234567890123456",
		"li_at_value_here",
		"x",
	}
	for _, want := range cases {
		enc := encryptForTest([]byte(want), key)
		got, err := decryptCookieValue(enc, key, 0)
		if err != nil {
			t.Errorf("decrypt(%q): %v", want, err)
			continue
		}
		if got != want {
			t.Errorf("round-trip = %q, want %q", got, want)
		}
	}
}

func TestDecryptCookieValueDBv24PrefixStripping(t *testing.T) {
	key := deriveAESKey([]byte("test-passphrase"))
	plaintext := "ajax:abc"
	// Simulate the v24+ SHA256(host_key) prefix: 32 bytes prepended.
	prefixed := append(bytes.Repeat([]byte{0xAA}, 32), []byte(plaintext)...)
	enc := encryptForTest(prefixed, key)
	got, err := decryptCookieValue(enc, key, 24)
	if err != nil {
		t.Fatalf("decrypt v24: %v", err)
	}
	if got != plaintext {
		t.Errorf("v24 decrypt = %q, want %q", got, plaintext)
	}
}

func TestDecryptCookieValueUnencrypted(t *testing.T) {
	key := deriveAESKey([]byte("test-passphrase"))
	got, err := decryptCookieValue([]byte("plain_value"), key, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain_value" {
		t.Errorf("unencrypted = %q, want plain_value", got)
	}
}

func TestDecryptCookieValueEmpty(t *testing.T) {
	key := deriveAESKey([]byte("test-passphrase"))
	got, err := decryptCookieValue(nil, key, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("empty = %q, want empty", got)
	}
}

func TestFetchJSESSIONIDFromURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "li_at=mytoken" {
			t.Errorf("request Cookie = %q, want li_at=mytoken", r.Header.Get("Cookie"))
		}
		w.Header().Set("Set-Cookie", `JSESSIONID="ajax:abc123"; Path=/; HttpOnly; Secure`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got, err := fetchJSESSIONIDFromURL("mytoken", srv.URL)
	if err != nil {
		t.Fatalf("fetchJSESSIONIDFromURL: %v", err)
	}
	if got != "ajax:abc123" {
		t.Errorf("JSESSIONID = %q, want ajax:abc123", got)
	}
}

func TestFetchJSESSIONIDAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "other=val; Path=/")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got, err := fetchJSESSIONIDFromURL("mytoken", srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("JSESSIONID = %q, want empty (not in response)", got)
	}
}

func TestQueryCookiesFixtureDB(t *testing.T) {
	key := deriveAESKey([]byte("test-passphrase"))

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "Cookies")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE cookies (host_key TEXT, name TEXT, value TEXT, encrypted_value BLOB)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE meta (key TEXT UNIQUE, value)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO meta VALUES ('version', '0')`)
	if err != nil {
		t.Fatal(err)
	}

	enc := encryptForTest([]byte("secret_li_at"), key)
	_, err = db.Exec(`INSERT INTO cookies (host_key, name, encrypted_value, value) VALUES (?, ?, ?, ?)`,
		".linkedin.com", "li_at", enc, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO cookies (host_key, name, encrypted_value, value) VALUES (?, ?, ?, ?)`,
		".linkedin.com", "bcookie", nil, "plain_bcookie")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO cookies (host_key, name, encrypted_value, value) VALUES (?, ?, ?, ?)`,
		".example.com", "tracker", enc, "")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cookies, err := queryCookies(db, key, 0)
	if err != nil {
		t.Fatalf("queryCookies: %v", err)
	}
	if cookies["li_at"] != "secret_li_at" {
		t.Errorf("li_at = %q, want secret_li_at", cookies["li_at"])
	}
	if cookies["bcookie"] != "plain_bcookie" {
		t.Errorf("bcookie = %q, want plain_bcookie", cookies["bcookie"])
	}
	if _, ok := cookies["tracker"]; ok {
		t.Error("example.com cookie should be filtered out")
	}
}
