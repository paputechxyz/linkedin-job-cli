package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-job-cli/internal/llm"
)

var summarizeCmd = &cobra.Command{
	Use:   "summarize",
	Short: "Generate LLM summaries for stored jobs that lack one",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadCfg()
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		jobs, err := st.Unsummarized()
		if err != nil {
			die("query failed: %v", err)
		}
		if len(jobs) == 0 {
			fmt.Println("All jobs already have summaries.")
			return nil
		}
		if cfg.LLMAPIKey == "" {
			fmt.Fprintln(os.Stderr, "No LLM API key configured (OPENAI_API_KEY / LJ_LLM_API_KEY). Falling back to extractive summaries.")
		}
		fmt.Fprintf(os.Stderr, "Summarizing %d jobs…\n", len(jobs))
		for _, j := range jobs {
			s := llm.Summarize(j, cfg)
			if err := st.SetLLMSummary(j.ID, s); err != nil {
				fmt.Fprintf(os.Stderr, "  ! %s: %v\n", j.Title, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "  + %s @ %s\n", j.Title, orNA2(j.Company))
		}
		fmt.Fprintf(os.Stderr, "Done.\n")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(summarizeCmd)
}

func orNA2(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}
