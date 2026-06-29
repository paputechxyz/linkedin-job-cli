package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/profile"
	"linkedin-jobs/internal/store"
)

var scoreAll bool

var scoreCmd = &cobra.Command{
	Use:   "score --all",
	Short: "Re-score all non-filtered jobs against your current resume + preferences",
	Long: `Re-runs fit scoring for every non-filtered job using the resume and
preferences currently stored in your profile. Use this after editing your
profile to refresh scores across the DB. (Dedup is ignored on re-score.)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !scoreAll {
			die("use: linkedin-jobs score --all")
		}
		cfg := loadCfg()
		settings, _ := config.LoadSettings()
		provider, err := llm.Resolve(cfg)
		if err != nil {
			die("%v", err)
		}
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		p, _ := profile.Load()

		jobs, err := st.List(store.Filters{}) // excludes filtered by default
		if err != nil {
			die("query failed: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Re-scoring %d job(s) via %s…\n", len(jobs), provider.Source)
		delay := resolveLLMDelay()
		scored := 0
		for _, j := range jobs {
			paceLLM(delay, scored)
			if _, err := enrichAndScoreJob(st, j, p, provider, settings.Scoring.ReasonThreshold); err != nil {
				fmt.Fprintf(os.Stderr, "  ! %s: %v\n", j.Title, err)
				continue
			}
			scored++
		}
		fmt.Fprintln(os.Stderr, "Done.")
		return nil
	},
}

func init() {
	scoreCmd.Flags().BoolVar(&scoreAll, "all", false, "re-score all non-filtered jobs")
	rootCmd.AddCommand(scoreCmd)
}
