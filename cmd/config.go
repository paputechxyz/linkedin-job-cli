package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
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
		fmt.Printf("  rubrics:             %d defined (%d system)\n", len(s.Scoring.Rubrics), countSystemRubrics(s.Scoring.Rubrics))
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the settings/db file locations",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("settings: %s\n", config.SettingsPath())
		fmt.Printf("db:       %s\n", loadCfg().DBPath)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configShowCmd, configPathCmd)
	rootCmd.AddCommand(configCmd)
}

func countSystemRubrics(rubrics []config.Rubric) int {
	n := 0
	for _, r := range rubrics {
		if r.Kind == "system" {
			n++
		}
	}
	return n
}
