package auth

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"linkedin-jobs/internal/config"
)

// Session holds the resolved LinkedIn session credentials.
type Session struct {
	CookieHeader string
	CSRFToken    string // value for the csrf-token request header
	Source       string // "env" | "file"
}

// Valid reports whether the session is structurally complete enough to make
// authenticated Voyager API calls. LinkedIn's Voyager endpoint requires:
//   - the li_at auth cookie (proves you're signed in), and
//   - a csrf-token header equal to the JSESSIONID cookie value
//
// A session can have cookies present (HasSession returns true) but still be
// unusable if those two critical cookies are missing — typically because the
// capture raced the login flow. Callers that need a working session should
// check Valid, not just HasSession.
func (s *Session) Valid() bool {
	if s == nil || s.CookieHeader == "" || s.CSRFToken == "" {
		return false
	}
	return hasCookie(s.CookieHeader, "li_at")
}

// hasCookie reports whether the given cookie name appears in a raw "Cookie:"
// header value (case-insensitive name match).
func hasCookie(header, name string) bool {
	target := strings.ToLower(name)
	for _, part := range strings.Split(header, ";") {
		p := strings.TrimSpace(part)
		idx := strings.Index(p, "=")
		if idx < 0 {
			continue
		}
		if strings.ToLower(strings.TrimSpace(p[:idx])) == target {
			return true
		}
	}
	return false
}

// ErrNoSession is returned when no LinkedIn session could be resolved.
var ErrNoSession = errors.New("no LinkedIn session found: run `linkedin-jobs auth login` to capture one")

// Resolve resolves a LinkedIn session in priority order:
//  1. LJ_COOKIE env (raw cookie header) — explicit override, wins over everything
//  2. LJ_COOKIES_FILE (file with a raw cookie header, one line or Netscape cookies.txt)
func Resolve(cfg config.Config) (*Session, error) {
	// 1. env (raw cookie header) — highest priority
	if cfg.CookieHeader != "" {
		return &Session{
			CookieHeader: cfg.CookieHeader,
			CSRFToken:    csrfFromCookieHeader(cfg.CookieHeader),
			Source:       "env",
		}, nil
	}

	// 2. file
	if cfg.CookiesFile != "" {
		if hdr, err := readCookiesFile(cfg.CookiesFile); err == nil && hdr != "" {
			return &Session{
				CookieHeader: hdr,
				CSRFToken:    csrfFromCookieHeader(hdr),
				Source:       "file",
			}, nil
		}
	}

	// 3. default cookies file (~/.linkedin-jobs/cookies.txt) — written by `auth login`
	if hdr, err := readCookiesFile(DefaultCookiesPath()); err == nil && hdr != "" {
		return &Session{
			CookieHeader: hdr,
			CSRFToken:    csrfFromCookieHeader(hdr),
			Source:       "login",
		}, nil
	}

	return nil, ErrNoSession
}

// DefaultCookiesPath returns the default location for the cookies file written
// by `auth login`: ~/.linkedin-jobs/cookies.txt. This is the third resolution
// source in Resolve (after LJ_COOKIE and LJ_COOKIES_FILE).
func DefaultCookiesPath() string {
	return filepath.Join(config.HomeDir(), "cookies.txt")
}

// AssembleCookieHeader builds a raw "name=value; name=value" Cookie header from
// a cookie map. Keys are sorted for deterministic output.
func AssembleCookieHeader(cookies map[string]string) string {
	keys := make([]string, 0, len(cookies))
	for k := range cookies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+cookies[k])
	}
	return strings.Join(parts, "; ")
}

// WriteCookiesFile writes a raw cookie header to path, creating the parent
// directory if needed. The file is created with 0600 permissions so only the
// owner can read the session cookies.
func WriteCookiesFile(path, header string) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(header), 0o600)
}

// csrfFromCookieHeader extracts the csrf-token value from JSESSIONID. LinkedIn's
// csrf-token header equals the JSESSIONID cookie value (e.g. "ajax:1234…"). The
// cookie value may carry surrounding double quotes; the header is the unquoted
// form, so surrounding quotes are stripped.
func csrfFromCookieHeader(header string) string {
	for _, part := range strings.Split(header, ";") {
		p := strings.TrimSpace(part)
		if idx := strings.Index(p, "="); idx >= 0 {
			name := strings.ToLower(strings.TrimSpace(p[:idx]))
			if name == "jsessionid" {
				val := strings.TrimSpace(p[idx+1:])
				val = strings.Trim(val, "\"")
				return val
			}
		}
	}
	return ""
}

func readCookiesFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	// Netscape cookies.txt format: tab-separated, lines starting with # are comments
	var netscape bool
	var b strings.Builder
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			if strings.HasPrefix(t, "# Netscape") || strings.HasPrefix(t, "# HttpOnly_") {
				netscape = true
			}
			continue
		}
		if netscape {
			fields := strings.Split(t, "\t")
			if len(fields) >= 7 {
				if b.Len() > 0 {
					b.WriteString("; ")
				}
				b.WriteString(fields[5] + "=" + fields[6])
				continue
			}
		}
		// raw cookie header (one or many lines)
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString(strings.TrimSuffix(t, ";"))
	}
	return strings.TrimSpace(b.String()), nil
}
