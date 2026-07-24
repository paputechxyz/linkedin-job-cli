//go:build !darwin && !windows

package auth

import "errors"

func browserCaptureSupported() bool { return false }

// Stubs: never reached because ReadChromeCookies short-circuits on unsupported
// platforms via browserCaptureSupported(). They exist to satisfy the shared
// call sites at compile time.
func chromeCookieDBPath() string { return "" }

func chromeCookieKey() ([]byte, error) {
	return nil, errors.New("not supported on this platform")
}

func decryptCookieValue(enc, key []byte, dbVersion int) (string, error) {
	return "", errors.New("not supported on this platform")
}
