package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/auth"
	"linkedin-jobs/internal/linkedin"
)

// attachSession resolves a LinkedIn session and attaches it to the client.
func attachSession(c *linkedin.Client) (*auth.Session, error) {
	s, err := auth.Resolve(loadCfg())
	if err != nil {
		return nil, err
	}
	c.WithSession(s)
	return s, nil
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Inspect your LinkedIn session",
	Long: `Check whether a usable LinkedIn session is available for the 'recommended'
and 'url' commands. Sessions come from your own cookie header — set LJ_COOKIE
(a raw Cookie header string) or LJ_COOKIES_FILE (path to a file with one). The
csrf-token is derived from your JSESSIONID cookie.`,
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether a usable session is available",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, _ := newClient(true)
		if c == nil {
			return nil
		}
		if !c.HasSession() {
			fmt.Println("No session. Set LJ_COOKIES_FILE or LJ_COOKIE to a raw Cookie header.")
			return nil
		}
		// HasSession only checks that *some* cookies were captured. A usable
		// Voyager session also needs the li_at cookie and a JSESSIONID-derived
		// csrf-token; without those, `recommended` will 403 with "CSRF check
		// failed". Surface that here so the user knows to refresh their cookie
		// export instead of discovering it at fetch time.
		sess, _ := auth.Resolve(loadCfg())
		if sess == nil || !sess.Valid() {
			var why, src string
			if sess != nil {
				src = sess.Source
				switch {
				case sess.CSRFToken == "":
					why = "no JSESSIONID cookie (cannot derive csrf-token)"
				case !strings.Contains(strings.ToLower(sess.CookieHeader), "li_at="):
					why = "missing li_at auth cookie"
				default:
					why = "incomplete cookies"
				}
			} else {
				why = "session could not be resolved"
			}
			fmt.Printf("Session captured from %s but incomplete (%s).\n", sessionSourceLabel(src), why)
			fmt.Println("This usually means the cookie export is stale — re-export your LinkedIn cookies.")
			return nil
		}
		fmt.Printf("Session available (recommended jobs enabled) [source: %s].\n", sessionSourceLabel(sess.Source))
		return nil
	},
}

func sessionSourceLabel(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func init() {
	authCmd.AddCommand(authStatusCmd)
	rootCmd.AddCommand(authCmd)
}
