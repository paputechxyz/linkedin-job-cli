package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-job-cli/internal/auth"
	"linkedin-job-cli/internal/linkedin"
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
			"--complete-selector", "a[href*=feed]"); err != nil {
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
		fmt.Println("Session available (recommended jobs enabled).")
		return nil
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Forget the captured session (press-auth)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := runShell("press-auth", "forget", "linkedin.com"); err != nil {
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
