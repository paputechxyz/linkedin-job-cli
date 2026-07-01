package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/profile"
	"linkedin-jobs/internal/render"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage your resume and preference knobs (drive fit scoring & hard filtering)",
	Long: `Your candidate context lives in two places (in the project directory,
override with LJ_CONFIG_DIR):

    RESUME.md          — your resume (free text); sent to your LLM when scoring
    settings.yaml      — preference knobs under the ` + "`profile:`" + ` section

The structured knobs (work_arrangement, min_salary, locations, preferred_tech)
drive the deterministic hard filter + rubric. Edit settings.yaml by hand to
tune them. The resume is managed via the subcommand below — paste text on stdin
and end with Ctrl-D (macOS/Linux) or Ctrl-Z then Enter (Windows):

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

var profileShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the stored resume and preference knobs",
	RunE: func(cmd *cobra.Command, args []string) error {
		settings, _ := config.LoadSettings()
		p, err := profile.Load(settings.Profile)
		if err != nil {
			die("failed to read profile: %v", err)
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
		fmt.Printf("\nPreference knobs (%s → profile:):\n", config.SettingsPath())
		fmt.Printf("  work arrangement: %s\n", orNone(p.PrefWorkArrangement))
		if p.PrefMinSalary != nil {
			fmt.Printf("  min salary:        %s%.0f %s\n", currencyLabel(p.PrefMinSalaryCurrency), *p.PrefMinSalary, orNoneCurrency(p.PrefMinSalaryCurrency))
		} else {
			fmt.Println("  min salary:        (none)")
		}
		fmt.Printf("  locations:         %s\n", orNone(p.PrefLocations))
		if len(p.PrefPreferredTech) > 0 {
			fmt.Printf("  preferred tech:    %d — %s\n", len(p.PrefPreferredTech), strings.Join(p.PrefPreferredTech, ", "))
		} else {
			fmt.Println("  preferred tech:    (none)")
		}
		if p.UpdatedAt != "" {
			fmt.Printf("\nLoaded at: %s\n", p.UpdatedAt)
		}
		return nil
	},
}

var profileClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete the stored resume file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := profile.ClearResume(); err != nil {
			die("clear failed: %v", err)
		}
		fmt.Printf("Resume removed (%s).\nNote: preference knobs live in %s — edit that file by hand to clear them.\n", profile.ResumePath(), config.SettingsPath())
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

// orNoneCurrency hides an empty currency code in display when salary is set.
func orNoneCurrency(code string) string {
	if code == "" {
		return "(raw compare)"
	}
	return code
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
	profileCmd.AddCommand(profileResumeCmd, profileShowCmd, profileClearCmd)
	rootCmd.AddCommand(profileCmd)
}
