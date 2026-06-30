package cmd

import (
	"fmt"
	"os"
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
	Short: "Manage your LinkedIn browser session",
	Long: `Manage the LinkedIn session used by the 'recommended' command.

The session is captured from your logged-in Chrome via the press-auth
companion (one-time), which stores it encrypted in the macOS Keychain. As a
fallback you can set LJ_COOKIES_FILE (a file with a raw Cookie header) or
LJ_COOKIE.`,
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Capture your LinkedIn session via press-auth (one-time, in Chrome)",
	Long: `Opens a controlled Chrome window (not your daily profile) where you sign in
to LinkedIn once. press-auth captures the session cookies, stores them
encrypted in the macOS Keychain, and serves them to this CLI at runtime.

Install press-auth first if needed:
    go install github.com/mvanhorn/cli-printing-press/v4/cmd/press-auth@latest`,
		RunE: func(cmd *cobra.Command, args []string) error {
		if err := runShell("press-auth", "login", "linkedin.com",
			"--login-url", "https://www.linkedin.com/login",
			// .global-nav only renders once LinkedIn considers you signed in;
			// "a[href*=feed]" matches too early (marketing/footer links) and
			// causes press-auth to capture cookies before li_at/JSESSIONID are set.
			"--complete-selector", ".global-nav",
			// --force overwrites any prior (possibly incomplete) capture without
			// an interactive prompt, so re-running after a bad capture just works.
			"--force"); err != nil {
			fmt.Fprintln(os.Stderr, "press-auth login failed:", err)
			fmt.Fprintln(os.Stderr, "Install press-auth:")
			fmt.Fprintln(os.Stderr, "  go install github.com/mvanhorn/cli-printing-press/v4/cmd/press-auth@latest")
			fmt.Fprintln(os.Stderr, "Or export a HAR cookie header to LJ_COOKIES_FILE as a manual fallback.")
			os.Exit(1)
		}
		fmt.Println("Session captured. Try: linkedin-jobs recommended")
		return nil
	},
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
			fmt.Println("No session. Run: linkedin-jobs auth login")
			return nil
		}
		// HasSession only checks that *some* cookies were captured. A usable
		// Voyager session also needs the li_at cookie and a JSESSIONID-derived
		// csrf-token; without those, `recommended` will 403 with "CSRF check
		// failed". Surface that here so the user knows to re-login instead of
		// discovering it at fetch time.
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
			fmt.Println("This usually means the login capture raced — re-run: linkedin-jobs auth login")
			// Regression net: with the current priority order, an explicit
			// LJ_COOKIE/LJ_COOKIES_FILE always beats press-auth. If we ever
			// regress that, this hint is the user's clue. It also helps when
			// press-auth holds a stale capture and the user forgot they set
			// the env var to compensate.
			cfg := loadCfg()
			if src == "press-auth" && (cfg.CookieHeader != "" || cfg.CookiesFile != "") {
				fmt.Println("Note: LJ_COOKIE/LJ_COOKIES_FILE is set but press-auth is shadowing it.")
				fmt.Println("      Run `linkedin-jobs auth logout` to forget the stale press-auth capture.")
			}
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

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Forget the captured session (press-auth)",
	RunE: func(cmd *cobra.Command, args []string) error {
		// --yes skips press-auth's interactive confirmation prompt, which
		// would otherwise refuse to delete state in non-TTY contexts
		// (scripts, CI, this CLI running piped/redirected).
		if err := runShell("press-auth", "forget", "linkedin.com", "--yes"); err != nil {
			fmt.Fprintln(os.Stderr, "press-auth forget failed:", err)
			os.Exit(1)
		}
		fmt.Println("Session forgotten.")
		return nil
	},
}

func init() {
	authCmd.AddCommand(authLoginCmd, authStatusCmd, authLogoutCmd)
	rootCmd.AddCommand(authCmd)
}

// runShell runs a command, streaming its output to the terminal.
func runShell(name string, args ...string) error {
	cmd := newCmd(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
