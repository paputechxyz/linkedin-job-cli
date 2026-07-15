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
	Short: "Interactive setup: generate rubrics from a preferences paragraph, configure LLM + LinkedIn",
	Long: `Walk through the full first-time setup interactively:

  1. Preferences — write a paragraph describing what you want in a job. The
     LLM extracts scoring rubrics from it (plus a few system defaults that
     always apply: salary, work arrangement). You confirm before
     anything is saved. Any required number the paragraph omits (e.g. a salary
     floor) is prompted for. Rubrics + weights land in settings.yaml.
  2. LLM provider — checks whether a provider is resolved; if not, prints
     guidance on how to configure one.
  3. LinkedIn session — recommends auth login (macOS + Chrome) so the
     'recommended' and 'url' commands work with your personalized feed.

Run this once after installing the CLI. Use 'amend' to change a few rubrics,
or 'reset' to start over from scratch.`,
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

	// --- Step 1: Preferences paragraph → rubrics + structured params ---
	fmt.Println("== Preferences ==")
	fmt.Println("Describe what you want in a job in a few sentences — work arrangement,")
	fmt.Println("salary, location, tech, perks, deal-breakers, etc. The LLM extracts scoring")
	fmt.Println("rubrics from it. End your paragraph with a blank line.")
	paragraph := promptParagraph(stdin)

	if strings.TrimSpace(paragraph) == "" {
		fmt.Println("  No paragraph entered — keeping current rubrics and profile.")
	} else {
		cfg := loadCfg()
		provider, err := llm.Resolve(cfg)
		if err != nil {
			return fmt.Errorf("LLM provider required for rubric setup: %w", err)
		}
		fmt.Println("  Extracting rubrics…")
		gen, err := llm.GenerateRubrics(paragraph, provider)
		if err != nil {
			return fmt.Errorf("extract rubrics: %w", err)
		}

		// Dynamic rubrics merge onto the always-present system defaults.
		changes := make([]config.Rubric, 0, len(gen.Rubrics))
		for _, r := range gen.Rubrics {
			changes = append(changes, config.Rubric{ID: r.ID, Kind: "dynamic", Weight: 5, Description: r.Description, Items: r.Items})
		}
		rubrics := config.MergeRubrics(config.DefaultScoringSettings().Rubrics, changes)

		// Structured params flow into the profile block.
		prof := settings.Profile
		if len(gen.WorkArrangement) > 0 {
			prof.WorkArrangement = gen.WorkArrangement
		}
		if gen.MinSalaryCurrency != "" {
			prof.MinSalaryCurrency = gen.MinSalaryCurrency
		}
		if gen.MinSalary != nil {
			prof.MinSalary = gen.MinSalary
		}
		if len(gen.PreferredTech) > 0 {
			prof.PreferredTech = gen.PreferredTech
		}
		if len(gen.AvoidedTech) > 0 {
			prof.AvoidedTech = gen.AvoidedTech
		}
		// A required number the paragraph omitted is prompted, never guessed.
		if prof.MinSalary == nil || *prof.MinSalary <= 0 {
			fmt.Println("\n  No salary floor detected in your paragraph.")
			prof.MinSalary = promptFloatPtr(stdin, "Minimum salary (0 = no floor)", prof.MinSalary)
		}

		fmt.Println("\n  Extracted rubrics:")
		printRubrics(rubrics)
		fmt.Println("\n  Structured params:")
		fmt.Printf("    work arrangement: %s\n", orNoneSlice(prof.WorkArrangement))
		fmt.Printf("    min salary:       %s\n", formatSalaryFloor(prof.MinSalary, prof.MinSalaryCurrency))

		if !confirm(stdin, "Save these rubrics and params?") {
			fmt.Println("  Aborted — nothing saved.")
			return nil
		}
		if err := config.SaveProfile(prof); err != nil {
			return fmt.Errorf("save profile: %w", err)
		}
		if err := config.SaveRubrics(rubrics); err != nil {
			return fmt.Errorf("save rubrics: %w", err)
		}
		settings.Scoring.Rubrics = rubrics
		fmt.Printf("\n-> rubrics + profile saved to %s\n\n", settingsPath)
	}

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

// promptParagraph reads a multi-line paragraph from stdin until a blank line
// (or EOF). Used for the preferences paragraph in setup/amend.
func promptParagraph(stdin *bufio.Reader) string {
	fmt.Println(">")
	var lines []string
	for {
		line, err := stdin.ReadString('\n')
		if err != nil {
			break
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		lines = append(lines, strings.TrimRight(line, "\n"))
	}
	return strings.Join(lines, " ")
}

// confirm asks a yes/no question; empty or y/yes returns true.
func confirm(stdin *bufio.Reader, question string) bool {
	fmt.Printf("  %s [Y/n]: ", question)
	line, _ := stdin.ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "" || line == "y" || line == "yes"
}

// printRubrics lists the rubric set for confirmation.
func printRubrics(rubrics []config.Rubric) {
	for _, r := range rubrics {
		tag := r.Kind
		if tag == "" {
			tag = "dynamic"
		}
		desc := r.Description
		if desc == "" {
			desc = "—"
		}
		fmt.Printf("    [%s] %-18s w%-2d  %s", tag, r.ID, r.Weight, desc)
		if len(r.Items) > 0 {
			fmt.Printf("  (items: %s)", strings.Join(r.Items, ", "))
		}
		if len(r.AppliesTo) > 0 {
			fmt.Printf("  (applies_to: %s)", strings.Join(r.AppliesTo, ", "))
		}
		fmt.Println()
	}
}

// formatSalaryFloor renders a salary floor for display.
func formatSalaryFloor(salary *float64, currency string) string {
	if salary == nil || *salary <= 0 {
		return "(none)"
	}
	cur := currency
	if cur == "" {
		cur = "USD"
	}
	return fmt.Sprintf("%s%.0f", cur, *salary)
}
