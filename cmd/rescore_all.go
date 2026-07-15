package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/profile"
	"linkedin-jobs/internal/store"
)

var rescoreAllCmd = &cobra.Command{
	Use:   "rescore-all",
	Short: "Re-enrich + re-score every stored job against the current rubric set",
	Long: `Re-runs the full LLM enrichment + rubric score for every job in the DB,
regardless of its current status. Use this after editing your rubrics or
weights in settings.yaml, or after re-running setup/amend.

This always calls the LLM (one call per job) and ignores dedup, so it costs
tokens proportional to your DB size. Explicit triage statuses
(saved/applied/rejected) are preserved.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		settings, _ := config.LoadSettings()
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		p, _ := profile.Load(settings.Profile)
		rubrics := settings.Scoring.Rubrics

		jobs, err := st.List(store.Filters{})
		if err != nil {
			die("query failed: %v", err)
		}
		total := len(jobs)

		cfg := loadCfg()
		provider, err := llm.Resolve(cfg)
		if err != nil {
			die("%v", err)
		}
		delay := resolveLLMDelay()
		concurrency := resolveLLMConcurrency()
		fmt.Fprintf(os.Stderr, "Re-scoring %d job(s) via %s (batch %d)…\n", total, provider.Source, concurrency)
		fmt.Fprintln(os.Stderr, profileStatus(p))

		scored := enrichAndScoreBatch(st, jobs, p, provider, rubrics, concurrency, delay, func(idx, total int, j *models.JobPosting, err error) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "  [%d/%d] ! %s — %v\n", idx+1, total, j.Title, err)
				return
			}
			fmt.Fprintf(os.Stderr, "  [%d/%d] + %s — %s\n", idx+1, total, j.Title, j.Company)
		})
		fmt.Fprintf(os.Stderr, "Re-scored %d of %d job(s).\n", scored, total)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(rescoreAllCmd)
}
