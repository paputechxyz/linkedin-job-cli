package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/profile"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage your LLM provider connection and view settings",
}

var configLlmCmd = &cobra.Command{
	Use:   "llm",
	Short: "Connect an LLM provider (opencode / Claude / custom key)",
	Long: `Connect an LLM provider used for enrichment and fit scoring.

Choose one:
  1. opencode  - reuse the provider already configured in opencode (e.g. your GLM key)
  2. claude    - Anthropic Claude using an ANTHROPIC_API_KEY you paste
  3. custom    - any OpenAI-compatible base URL + key + model

The choice persists to config.json (mode 0600). You can also skip this command
entirely and set OPENAI_API_KEY / LJ_LLM_* / ANTHROPIC_API_KEY env vars.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		rd := bufio.NewReader(os.Stdin)
		fmt.Println("Connect an LLM provider:")
		fmt.Println("  1. opencode (reuse opencode's stored provider)")
		fmt.Println("  2. claude (Anthropic)")
		fmt.Println("  3. custom (OpenAI-compatible base URL + key + model)")
		fmt.Print("Choose [1-3]: ")
		choice, _ := rd.ReadString('\n')
		choice = strings.TrimSpace(choice)

		var provider *llm.Provider
		switch choice {
		case "1":
			p, ok := llm.FromOpencode()
			if !ok {
				fmt.Fprintln(os.Stderr, "No usable opencode provider found. Install/configure opencode, or pick Claude/custom.")
				return nil
			}
			provider = p
			fmt.Printf("Using opencode provider (model %s).\n", provider.Model)
		case "2":
			key := readSecret(rd, "Paste your ANTHROPIC_API_KEY: ")
			if key == "" {
				fmt.Fprintln(os.Stderr, "No key entered.")
				return nil
			}
			provider = llm.NewAnthropicProvider(key)
			fmt.Println("Using Anthropic Claude.")
		case "3":
			baseURL := strings.TrimSpace(prompt(rd, "Base URL (e.g. https://api.openai.com/v1): "))
			key := readSecret(rd, "API key: ")
			model := strings.TrimSpace(prompt(rd, "Model (e.g. gpt-4o-mini): "))
			if baseURL == "" || key == "" || model == "" {
				fmt.Fprintln(os.Stderr, "base URL, key, and model are all required.")
				return nil
			}
			provider = &llm.Provider{BaseURL: baseURL, APIKey: key, Model: model, Source: "config"}
		default:
			fmt.Fprintln(os.Stderr, "Invalid choice.")
			return nil
		}
		if err := llm.Save(provider); err != nil {
			die("failed to save config: %v", err)
		}
		fmt.Printf("Saved to %s. Model: %s.\n", llm.ConfigPath(), provider.Model)
		fmt.Println("Try: linkedin-jobs enrich <id>   or   linkedin-jobs recommended")
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the resolved LLM provider (key redacted) and settings file",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadCfg()
		p, err := llm.Resolve(cfg)
		if err != nil {
			fmt.Println("No provider resolved:", err)
			fmt.Println("Run: linkedin-jobs config llm")
			return nil
		}
		fmt.Printf("Provider: %s\n", p.Source)
		fmt.Printf("Base URL: %s\n", p.BaseURL)
		fmt.Printf("Model:    %s\n", p.Model)
		fmt.Printf("API key:  %s\n", p.Redacted())
		s, _ := config.LoadSettings()
		fmt.Printf("\nSettings: %s\n", config.SettingsPath())
		fmt.Printf("  top_companies_limit: %d\n", s.Stats.TopCompaniesLimit)
		fmt.Printf("  auto_filter:         %v\n", s.Filter.AutoFilter)
		fmt.Printf("  reason_threshold:    %d\n", s.Scoring.ReasonThreshold)
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config/settings file locations",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("provider:    %s  (LLM secrets; override with LJ_CONFIG_DIR)\n", llm.ConfigPath())
		fmt.Printf("settings:    %s\n", config.SettingsPath())
		fmt.Printf("resume:      %s\n", profile.ResumePath())
		fmt.Printf("preferences: %s\n", profile.PrefsPath())
		return nil
	},
}

func prompt(rd *bufio.Reader, p string) string {
	fmt.Print(p)
	s, _ := rd.ReadString('\n')
	return s
}

// readSecret reads a secret with no echo when stdin is a TTY (golang.org/x/term),
// falling back to a plain read otherwise (piped input / non-interactive use).
func readSecret(rd *bufio.Reader, p string) string {
	fmt.Print(p)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	s, _ := rd.ReadString('\n')
	return strings.TrimSpace(s)
}

func init() {
	configCmd.AddCommand(configLlmCmd, configShowCmd, configPathCmd)
	rootCmd.AddCommand(configCmd)
}
