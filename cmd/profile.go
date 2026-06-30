package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/fx"
	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/profile"
	"linkedin-jobs/internal/render"
	"linkedin-jobs/internal/salary"
)

var (
	prefWork             string
	prefMinSalary        string
	prefMinSalaryCurrency string
	prefLocations        string
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage your resume and preferences (drive fit scoring & hard filtering)",
	Long: `Store your resume and job preferences as plain markdown files in
` + "`" + `~/.linkedin-jobs/` + "`" + ` (override with LJ_CONFIG_DIR):

    RESUME.md            — your resume (free text)
    JOB_PREFERENCE.md    — preferences (free text) + YAML front-matter knobs

The resume and free-text preferences are sent to your LLM provider when scoring
jobs for fit. The structured knobs in the front-matter (work_arrangement,
min_salary, locations) drive the deterministic hard filter that tags clear
mismatches without an LLM call.

Edit the files by hand any time, or use the subcommands below. Paste text on
stdin; end with Ctrl-D (macOS/Linux) or Ctrl-Z then Enter (Windows):

    linkedin-jobs profile resume < resume.txt
    pbpaste | linkedin-jobs profile resume
    linkedin-jobs profile resume   # then type/paste and Ctrl-D`,
}

var profileResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Paste your resume text (read from stdin), writing RESUME.md",
	RunE: func(cmd *cobra.Command, args []string) error {
		text, err := readPasted(os.Stdin)
		if err != nil {
			die("failed to read resume: %v", err)
		}
		if strings.TrimSpace(text) == "" {
			die("no resume text received on stdin")
		}
		if err := profile.SaveResume(text); err != nil {
			die("failed to save resume: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Resume saved to %s (%d chars).\n", profile.ResumePath(), len(text))
		return nil
	},
}

var profilePrefsCmd = &cobra.Command{
	Use:   "prefs",
	Short: "Paste your preferences (free text) and set hard-filter knobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		text, err := readPasted(os.Stdin)
		if err != nil {
			die("failed to read preferences: %v", err)
		}

		// Merge onto the existing on-disk profile so unset flags are preserved.
		p, _ := profile.Load()
		if p == nil {
			p = &models.Profile{}
		}
		if strings.TrimSpace(text) != "" {
			p.PreferencesText = text
		}
		if cmd.Flags().Changed("work") {
			p.PrefWorkArrangement = normalizeArrangement(prefWork)
		}
		if cmd.Flags().Changed("min-salary") {
			v, err := salary.ParseShorthand(prefMinSalary)
			if err != nil {
				die("invalid --min-salary %q: use '200k' or '200000'", prefMinSalary)
			}
			p.PrefMinSalary = &v
		}
		if cmd.Flags().Changed("min-salary-currency") {
			c := fx.Normalize(prefMinSalaryCurrency)
			if c != "" && !fx.Supported(c) {
				die("unsupported --min-salary-currency %q (e.g. USD, CAD, EUR)", prefMinSalaryCurrency)
			}
			p.PrefMinSalaryCurrency = c
		}
		if cmd.Flags().Changed("locations") {
			p.PrefLocations = prefLocations
		}
		if err := profile.SavePrefs(p); err != nil {
			die("failed to save preferences: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Preferences saved to %s.\n", profile.PrefsPath())
		return nil
	},
}

var profileShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the stored resume and preferences",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := profile.Load()
		if err != nil {
			die("failed to read profile: %v", err)
		}
		if p == nil {
			fmt.Println("No profile set. Edit these files (or run: linkedin-jobs profile resume):")
			fmt.Printf("  %s\n  %s\n", profile.ResumePath(), profile.PrefsPath())
			return nil
		}
		if jsonOut {
			render.AsJSON(os.Stdout, p)
			return nil
		}
		fmt.Printf("Resume (%s):\n", profile.ResumePath())
		if p.ResumeText == "" {
			fmt.Println("  (none)")
		} else {
			fmt.Println(indent(p.ResumeText))
		}
		fmt.Printf("\nPreferences (%s):\n", profile.PrefsPath())
		if p.PreferencesText == "" {
			fmt.Println("  (none)")
		} else {
			fmt.Println(indent(p.PreferencesText))
		}
		fmt.Println("\nHard-filter knobs:")
		fmt.Printf("  work arrangement: %s\n", orNone(p.PrefWorkArrangement))
		if p.PrefMinSalary != nil {
			fmt.Printf("  min salary:        %s%.0f\n", currencyLabel(p.PrefMinSalaryCurrency), *p.PrefMinSalary)
		} else {
			fmt.Println("  min salary:        (none)")
		}
		fmt.Printf("  locations:         %s\n", orNone(p.PrefLocations))
		if p.UpdatedAt != "" {
			fmt.Printf("\nLoaded at: %s\n", p.UpdatedAt)
		}
		return nil
	},
}

var profileClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete the stored resume and preferences files",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := profile.Clear(); err != nil {
			die("clear failed: %v", err)
		}
		fmt.Println("Profile cleared (RESUME.md and JOB_PREFERENCE.md removed).")
		return nil
	},
}

// readPasted reads all of r until EOF (the user ends paste input with Ctrl-D).
func readPasted(r io.Reader) (string, error) {
	var b strings.Builder
	sc := bufio.NewReader(r)
	for {
		line, err := sc.ReadString('\n')
		b.WriteString(line)
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
	}
	return strings.TrimSpace(b.String()), nil
}

func normalizeArrangement(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "remote":
		return "remote"
	case "hybrid":
		return "hybrid"
	case "onsite", "on-site", "on site", "office":
		return "onsite"
	case "":
		return ""
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func indent(s string) string {
	var b strings.Builder
	for _, ln := range strings.Split(s, "\n") {
		b.WriteString("  " + ln + "\n")
	}
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// currencyLabel renders a currency code as a leading symbol for display, e.g.
// "CAD" -> "CA$" , "USD" or "" -> "$".
func currencyLabel(code string) string {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "CAD":
		return "CA$"
	case "USD", "":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	}
	return strings.ToUpper(strings.TrimSpace(code)) + " "
}

func init() {
	profilePrefsCmd.Flags().StringVar(&prefWork, "work", "", "required work arrangement for the hard filter: remote|hybrid|onsite")
	profilePrefsCmd.Flags().StringVar(&prefMinSalary, "min-salary", "", "salary floor for the hard filter (e.g. 200k)")
	profilePrefsCmd.Flags().StringVar(&prefMinSalaryCurrency, "min-salary-currency", "", "currency for the salary floor (ISO 4217, e.g. CAD); enables FX-aware filtering")
	profilePrefsCmd.Flags().StringVar(&prefLocations, "locations", "", "comma-separated preferred locations for the hard filter")

	profileCmd.AddCommand(profileResumeCmd, profilePrefsCmd, profileShowCmd, profileClearCmd)
	rootCmd.AddCommand(profileCmd)
}
