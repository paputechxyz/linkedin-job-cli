package cmd

import (
	"net/url"
	"regexp"
	"strings"
)

// viewJobPathRE captures the trailing numeric job id from a single-job
// /jobs/view/<id>/ or /jobs/view/<slug>-<id>/ path. LinkedIn serves both forms:
// the legacy /jobs/view/4435820129/ and the modern slug-prefixed
// /jobs/view/senior-full-stack-engineer-at-acme-4431544268/. It is matched
// against a parsed URL *path* so a "/jobs/view/" substring that appears inside
// a query value of a legitimate search URL never trips the gate.
var viewJobPathRE = regexp.MustCompile(`^/jobs/view/(?:[^/]*-)?(\d+)/?$`)

// parseJobIDArg validates a "job <id>" argument. It returns the id when s is a
// bare positive integer (digits only — no sign, no spaces, no URL); otherwise it
// returns "" so the caller can reject with a helpful message.
func parseJobIDArg(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s
}

// viewJobIDFromURL returns the job id when rawURL points at a single LinkedIn
// job posting (a /jobs/view/<id>/ or /jobs/view/<slug>-<id>/ path), and ""
// otherwise. The `url` command uses it to reject individual-job URLs (the user
// should run `job <id>` instead). A bare /jobs/view/ path with no id (e.g. a
// tracking redirect carrying the id only in a query param) is NOT treated as a
// single job and returns "", since that shape is indistinguishable from a
// search URL that tracks a focused job via currentJobId.
func viewJobIDFromURL(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	m := viewJobPathRE.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	return m[1]
}
