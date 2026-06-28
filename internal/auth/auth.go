package auth

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	"linkedin-jobs/internal/config"
)

// Session holds the resolved LinkedIn session credentials.
type Session struct {
	CookieHeader string
	CSRFToken    string // value for the csrf-token request header
	Source       string // "press-auth" | "env" | "file"
}

// ErrNoSession is returned when no LinkedIn session could be resolved.
var ErrNoSession = errors.New("no LinkedIn session found: run `linkedin-jobs auth login` (installs/uses press-auth) or set LJ_COOKIES_FILE / LJ_COOKIE")

// Resolve resolves a LinkedIn session in priority order:
//  1. press-auth companion (`press-auth cookies linkedin.com`)
//  2. LJ_COOKIE env (raw cookie header)
//  3. LJ_COOKIES_FILE (file with a raw cookie header, one line or Netscape cookies.txt)
func Resolve(cfg config.Config) (*Session, error) {
	// 1. press-auth
	if path, err := exec.LookPath("press-auth"); err == nil {
		out, err := exec.Command(path, "cookies", "linkedin.com").Output()
		if err == nil {
			hdr := strings.TrimSpace(string(out))
			if hdr != "" {
				return &Session{
					CookieHeader: hdr,
					CSRFToken:    csrfFromCookieHeader(hdr),
					Source:       "press-auth",
				}, nil
			}
		}
	}

	// 2. env
	if cfg.CookieHeader != "" {
		return &Session{
			CookieHeader: cfg.CookieHeader,
			CSRFToken:    csrfFromCookieHeader(cfg.CookieHeader),
			Source:       "env",
		}, nil
	}

	// 3. file
	if cfg.CookiesFile != "" {
		if hdr, err := readCookiesFile(cfg.CookiesFile); err == nil && hdr != "" {
			return &Session{
				CookieHeader: hdr,
				CSRFToken:    csrfFromCookieHeader(hdr),
				Source:       "file",
			}, nil
		}
	}

	return nil, ErrNoSession
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
