package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/render"
	"linkedin-jobs/internal/salary"
)

var (
	prefWork      string
	prefMinSalary string
	prefLocations string
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage your resume and preferences (drive fit scoring & hard filtering)",
	Long: `Store your resume and job preferences as text in the local DB.

The resume and free-text preferences are sent to your LLM provider when scoring
jobs for fit. The structured preference flags (--work, --min-salary, --locations)
drive the deterministic hard filter that tags clear mismatches without an LLM call.

Paste text on stdin; end with Ctrl-D (macOS/Linux) or Ctrl-Z then Enter (Windows):
    linkedin-jobs profile resume < resume.txt
    pbpaste | linkedin-jobs profile resume
    linkedin-jobs profile resume   # then type/paste and Ctrl-D`,
}

var profileResumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Paste your resume text (read from stdin)",
	RunE: func(cmd *cobra.Command, args []string) error {
		text, err := readPasted(os.Stdin)
		if err != nil {
			die("failed to read resume: %v", err)
		}
		if strings.TrimSpace(text) == "" {
			die("no resume text received on stdin")
		}
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		p, _ := st.GetProfile()
		if p == nil {
			p = &models.Profile{}
		}
		p.ResumeText = text
		if err := st.SetProfile(p); err != nil {
			die("failed to save resume: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Resume saved (%d chars).\n", len(text))
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
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		p, _ := st.GetProfile()
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
		if cmd.Flags().Changed("locations") {
			p.PrefLocations = prefLocations
		}
		if err := st.SetProfile(p); err != nil {
			die("failed to save preferences: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Preferences saved.\n")
		return nil
	},
}

var profileShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the stored resume and preferences",
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		p, err := st.GetProfile()
		if err != nil {
			die("query failed: %v", err)
		}
		if p == nil {
			fmt.Println("No profile set. Run: linkedin-jobs profile resume")
			return nil
		}
		if jsonOut {
			render.AsJSON(os.Stdout, p)
			return nil
		}
		fmt.Println("Resume:")
		if p.ResumeText == "" {
			fmt.Println("  (none)")
		} else {
			fmt.Println(indent(p.ResumeText))
		}
		fmt.Println("\nPreferences:")
		if p.PreferencesText == "" {
			fmt.Println("  (none)")
		} else {
			fmt.Println(indent(p.PreferencesText))
		}
		fmt.Println("\nHard-filter knobs:")
		fmt.Printf("  work arrangement: %s\n", orNone(p.PrefWorkArrangement))
		if p.PrefMinSalary != nil {
			fmt.Printf("  min salary:        $%.0f\n", *p.PrefMinSalary)
		} else {
			fmt.Println("  min salary:        (none)")
		}
		fmt.Printf("  locations:         %s\n", orNone(p.PrefLocations))
		if p.UpdatedAt != "" {
			fmt.Printf("\nUpdated: %s\n", p.UpdatedAt)
		}
		return nil
	},
}

var profileClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Delete the stored resume and preferences",
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		if err := st.ClearProfile(); err != nil {
			die("clear failed: %v", err)
		}
		fmt.Println("Profile cleared.")
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

func init() {
	profilePrefsCmd.Flags().StringVar(&prefWork, "work", "", "required work arrangement for the hard filter: remote|hybrid|onsite")
	profilePrefsCmd.Flags().StringVar(&prefMinSalary, "min-salary", "", "salary floor for the hard filter (e.g. 200k)")
	profilePrefsCmd.Flags().StringVar(&prefLocations, "locations", "", "comma-separated preferred locations for the hard filter")

	profileCmd.AddCommand(profileResumeCmd, profilePrefsCmd, profileShowCmd, profileClearCmd)
	rootCmd.AddCommand(profileCmd)
}
