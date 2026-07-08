package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/profile"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View the resolved LLM provider and settings file locations",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the resolved LLM provider (key redacted) and settings file",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadCfg()
		p, err := llm.Resolve(cfg)
		if err != nil {
			fmt.Println("No provider resolved:", err)
			fmt.Println("Set OPENAI_API_KEY / LJ_LLM_* / ANTHROPIC_API_KEY, or rely on opencode discovery.")
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
	Short: "Print the settings/resume file locations",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("settings: %s\n", config.SettingsPath())
		fmt.Printf("resume:   %s\n", profile.ResumePath())
		fmt.Printf("db:       %s\n", loadCfg().DBPath)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configShowCmd, configPathCmd)
	rootCmd.AddCommand(configCmd)
}
