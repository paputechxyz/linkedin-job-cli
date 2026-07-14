package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/auth"
	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive setup: profile preferences, LLM, and LinkedIn session",
	Long: `Walk through the full first-time setup interactively:

  1. Profile preferences — work arrangement, salary floor, locations,
     preferred tech, and avoided tech (deal-breakers). Written to
     settings.yaml under the profile: section.
  2. LLM provider — checks whether a provider is resolved; if not, prints
     guidance on how to configure one.
  3. LinkedIn session — recommends auth login (macOS + Chrome) so the
     'recommended' and 'url' commands work with your personalized feed.

Run this once after installing the CLI. You can re-run it any time to
update your preferences.`,
	RunE: runSetup,
}

func runSetup(cmd *cobra.Command, args []string) error {
	stdin := bufio.NewReader(os.Stdin)

	// --- Step 0: Ensure settings.yaml exists ---
	settingsPath, err := config.EnsureSettings()
	if err != nil {
		return fmt.Errorf("create settings file: %w", err)
	}
	fmt.Printf("linkedin-jobs setup\n")
	fmt.Printf("Settings file: %s\n\n", settingsPath)

	settings, _ := config.LoadSettings()
	prof := settings.Profile

	// --- Step 1: Profile preferences ---
	fmt.Println("== Profile Preferences ==")
	fmt.Println("(Press Enter to keep the current value shown in brackets.)")

	prof.WorkArrangement = promptList(stdin,
		"Work arrangements (comma-separated: remote, hybrid, onsite)",
		prof.WorkArrangement)

	prof.MinSalaryCurrency = promptString(stdin,
		"Salary currency (USD, CAD, EUR, GBP)",
		prof.MinSalaryCurrency, "USD")

	prof.MinSalary = promptFloatPtr(stdin,
		"Minimum salary (0 = no floor)",
		prof.MinSalary)

	prof.Locations = promptList(stdin,
		"Preferred locations (comma-separated, e.g. Remote, Toronto)",
		prof.Locations)

	prof.PreferredTech = promptList(stdin,
		"Preferred tech (comma-separated, e.g. Python, Go, AWS)",
		prof.PreferredTech)

	prof.AvoidedTech = promptList(stdin,
		"Avoided tech / deal-breakers (comma-separated, e.g. C#, .NET, Ruby)",
		prof.AvoidedTech)

	if err := config.SaveProfile(prof); err != nil {
		return fmt.Errorf("save profile: %w", err)
	}
	fmt.Printf("\n-> profile saved to %s\n\n", settingsPath)

	// --- Step 2: LLM provider ---
	fmt.Println("== LLM Provider ==")
	cfg := loadCfg()
	p, err := llm.Resolve(cfg)
	if err != nil {
		fmt.Println("  [✗] No LLM provider configured.")
		fmt.Println("      Scoring is optional — every read command works without an LLM —")
		fmt.Println("      but fit scoring needs a provider. To configure:")
		fmt.Println()
		fmt.Println("      export OPENAI_API_KEY=sk-...          # or LJ_LLM_API_KEY")
		fmt.Println("      export LJ_LLM_MODEL=gpt-4o-mini       # optional")
		fmt.Println("      export LJ_LLM_BASE_URL=https://...    # optional (Ollama/vLLM/Azure)")
		fmt.Println()
		fmt.Println("      Or set ANTHROPIC_API_KEY for Claude.")
		fmt.Println("      Inside an opencode/Hermes session, the session LLM is auto-detected.")
		fmt.Println("      Re-run 'linkedin-jobs doctor' after setting a key.")
	} else {
		fmt.Printf("  [✓] provider resolved: source=%s model=%s key=%s\n",
			p.Source, p.Model, p.Redacted())
	}
	fmt.Println()

	// --- Step 3: LinkedIn session ---
	fmt.Println("== LinkedIn Session ==")
	sess, _ := auth.Resolve(cfg)
	if sess != nil && sess.Valid() {
		fmt.Printf("  [✓] Session available [source: %s].\n", sess.Source)
		fmt.Println("      'recommended' and 'url' commands are ready to go.")
	} else {
		fmt.Println("  [~] No session captured yet.")
		fmt.Println("      'recommended' and 'url' need your LinkedIn session.")
		fmt.Println()
		if runtimeGOOS == "darwin" {
			fmt.Println("      Recommended: log in to LinkedIn in Chrome, then run:")
			fmt.Println()
			fmt.Println("          linkedin-jobs auth login")
			fmt.Println()
			fmt.Println("      It reads cookies silently from Chrome (no browser window opens")
			fmt.Println("      if you're already logged in). The first run triggers a macOS")
			fmt.Println("      keychain prompt — click 'Always Allow'.")
		} else {
			fmt.Println("      Set LJ_COOKIES_FILE or LJ_COOKIE to a raw Cookie header.")
		}
		fmt.Println()
		fmt.Println("      'search', 'hr', 'watch', and 'job' work anonymously — no session needed.")
	}
	fmt.Println()

	fmt.Println("== Setup Complete ==")
	fmt.Println("  Run 'linkedin-jobs doctor' to verify everything, or")
	fmt.Println("  'linkedin-jobs recommended' to pull your first job feed.")
	return nil
}

// promptString reads a line from stdin, returning the trimmed value or the
// default if the line is empty.
func promptString(stdin *bufio.Reader, label, current, fallback string) string {
	display := current
	if display == "" {
		display = fallback
	}
	fmt.Printf("  %s [%s]: ", label, display)
	line, _ := stdin.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		if current == "" {
			return fallback
		}
		return current
	}
	return line
}

// promptList reads a comma-separated line from stdin, returning the parsed
// trimmed tokens or the current value if the line is empty.
func promptList(stdin *bufio.Reader, label string, current []string) []string {
	display := strings.Join(current, ", ")
	fmt.Printf("  %s [%s]: ", label, display)
	line, _ := stdin.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return current
	}
	if line == "-" {
		return []string{}
	}
	parts := strings.Split(line, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// promptFloatPtr reads a numeric value from stdin, returning a pointer or the
// current value if the line is empty.
func promptFloatPtr(stdin *bufio.Reader, label string, current *float64) *float64 {
	display := "0"
	if current != nil {
		display = strconv.FormatFloat(*current, 'f', -1, 64)
	}
	fmt.Printf("  %s [%s]: ", label, display)
	line, _ := stdin.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return current
	}
	v, err := strconv.ParseFloat(line, 64)
	if err != nil {
		fmt.Printf("    (not a number, keeping %s)\n", display)
		return current
	}
	return &v
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
