package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"linkedin-jobs/internal/config"
)

func TestCSRFFromCookieHeader(t *testing.T) {
	cases := map[string]string{
		"JSESSIONID=ajax:1234567890123456":          "ajax:1234567890123456",
		"JSESSIONID=\"ajax:1234567890123456\"; b=2": "ajax:1234567890123456",
		"a=1; jsessionid=ajax:99; c=3":              "ajax:99",
		"li_at=foo; JSESSIONID=\"ajax:7\"; bsync=1": "ajax:7",
		"no_session_here=1":                         "",
	}
	for in, want := range cases {
		got := csrfFromCookieHeader(in)
		if got != want {
			t.Errorf("csrfFromCookieHeader(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHasCookie(t *testing.T) {
	cases := []struct {
		header string
		name   string
		want   bool
	}{
		{"li_at=abc; JSESSIONID=ajax:1", "li_at", true},
		{"li_at=abc; JSESSIONID=ajax:1", "LI_AT", true}, // case-insensitive
		{"bcookie=1; lidc=2", "li_at", false},
		{"", "li_at", false},
		{"x=1; jsessionid=ajax:1", "jsessionid", true},
		{"x=1", "jsessionid", false},
		// values containing '=' must not break parsing
		{"li_at=a=b; JSESSIONID=ajax:1", "li_at", true},
	}
	for _, tc := range cases {
		got := hasCookie(tc.header, tc.name)
		if got != tc.want {
			t.Errorf("hasCookie(%q, %q) = %v, want %v", tc.header, tc.name, got, tc.want)
		}
	}
}

func TestSessionValid(t *testing.T) {
	cases := []struct {
		name string
		s    *Session
		want bool
	}{
		{"complete session", &Session{
			CookieHeader: "li_at=abc; JSESSIONID=\"ajax:1234\"",
			CSRFToken:    "ajax:1234",
			Source:       "env",
		}, true},
		{"missing li_at (pre-login capture)", &Session{
			CookieHeader: "__cf_bm=x; bcookie=y; lidc=z",
			CSRFToken:    "ajax:1234",
		}, false},
		{"empty csrf (no JSESSIONID)", &Session{
			CookieHeader: "li_at=abc",
			CSRFToken:    "",
		}, false},
		{"nil session", nil, false},
		{"empty cookie header", &Session{CSRFToken: "ajax:1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Valid(); got != tc.want {
				t.Errorf("Valid() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAssembleCookieHeader(t *testing.T) {
	cases := []struct {
		name    string
		cookies map[string]string
		want    string
	}{
		{"two cookies sorted", map[string]string{"JSESSIONID": "ajax:1", "li_at": "abc"}, "JSESSIONID=ajax:1; li_at=abc"},
		{"single cookie", map[string]string{"li_at": "xyz"}, "li_at=xyz"},
		{"empty map", map[string]string{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AssembleCookieHeader(tc.cookies)
			if got != tc.want {
				t.Errorf("AssembleCookieHeader() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWriteCookiesFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "cookies.txt")
	header := "li_at=abc; JSESSIONID=\"ajax:1234\""

	if err := WriteCookiesFile(path, header); err != nil {
		t.Fatalf("WriteCookiesFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perm = %o, want 0600", perm)
	}

	got, err := readCookiesFile(path)
	if err != nil {
		t.Fatalf("readCookiesFile: %v", err)
	}
	if got != header {
		t.Errorf("round-trip = %q, want %q", got, header)
	}
}

func TestDefaultCookiesPath(t *testing.T) {
	p := DefaultCookiesPath()
	if !filepath.IsAbs(p) {
		t.Errorf("DefaultCookiesPath() = %q, want absolute", p)
	}
	if filepath.Base(p) != "cookies.txt" {
		t.Errorf("DefaultCookiesPath() base = %q, want cookies.txt", filepath.Base(p))
	}
}

func TestResolveDefaultFile(t *testing.T) {
	// Write to the real default path, then clean up.
	path := DefaultCookiesPath()
	orig, hadOrig := os.ReadFile(path)
	t.Cleanup(func() {
		if hadOrig != nil {
			_ = os.WriteFile(path, orig, 0o600)
		} else {
			_ = os.Remove(path)
		}
	})

	header := "li_at=testvalue; JSESSIONID=\"ajax:9999\""
	if err := WriteCookiesFile(path, header); err != nil {
		t.Fatalf("WriteCookiesFile: %v", err)
	}

	// No env vars set → should resolve from default file with Source "login".
	sess, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if sess.Source != "login" {
		t.Errorf("Source = %q, want login", sess.Source)
	}
	if !sess.Valid() {
		t.Errorf("session not valid: %+v", sess)
	}
}

func TestResolveEnvWinsOverDefault(t *testing.T) {
	// Set up default file.
	path := DefaultCookiesPath()
	orig, hadOrig := os.ReadFile(path)
	t.Cleanup(func() {
		if hadOrig != nil {
			_ = os.WriteFile(path, orig, 0o600)
		} else {
			_ = os.Remove(path)
		}
	})
	_ = WriteCookiesFile(path, "li_at=defaultfile; JSESSIONID=\"ajax:d\"")

	// Env override should win.
	sess, err := Resolve(config.Config{CookieHeader: "li_at=envoverride; JSESSIONID=\"ajax:e\""})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if sess.Source != "env" {
		t.Errorf("Source = %q, want env (env must win over default file)", sess.Source)
	}
	if !strings.Contains(sess.CookieHeader, "envoverride") {
		t.Errorf("expected envoverride in header, got %q", sess.CookieHeader)
	}
}
