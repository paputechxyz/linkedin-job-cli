//go:build windows

package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"path/filepath"
	"testing"
)

// encryptGCMForTest encrypts plaintext the way Windows Chrome does (v10
// prefix, AES-256-GCM, nonce(12) || ciphertext || tag(16)) for round-trip
// decrypt tests.
func encryptGCMForTest(plaintext, key []byte) []byte {
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	return append(append([]byte("v10"), nonce...), ct...)
}

func TestDecryptCookieValueWindowsRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0x07}, 32) // AES-256 key
	cases := []string{
		"ajax:1234567890123456",
		"li_at_value_here",
		"x",
	}
	for _, want := range cases {
		enc := encryptGCMForTest([]byte(want), key)
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

func TestDecryptCookieValueWindowsDBv24PrefixStripping(t *testing.T) {
	key := bytes.Repeat([]byte{0x07}, 32)
	plaintext := "ajax:abc"
	prefixed := append(bytes.Repeat([]byte{0xAA}, 32), []byte(plaintext)...)
	enc := encryptGCMForTest(prefixed, key)
	got, err := decryptCookieValue(enc, key, 24)
	if err != nil {
		t.Fatalf("decrypt v24: %v", err)
	}
	if got != plaintext {
		t.Errorf("v24 decrypt = %q, want %q", got, plaintext)
	}
}

func TestDecryptCookieValueWindowsUnencrypted(t *testing.T) {
	key := bytes.Repeat([]byte{0x07}, 32)
	got, err := decryptCookieValue([]byte("plain_value"), key, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain_value" {
		t.Errorf("unencrypted = %q, want plain_value", got)
	}
}

func TestQueryCookiesWindowsFixtureDB(t *testing.T) {
	key := bytes.Repeat([]byte{0x07}, 32)

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

	enc := encryptGCMForTest([]byte("secret_li_at"), key)
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
}
