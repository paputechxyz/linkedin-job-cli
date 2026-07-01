package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/profile"
	"linkedin-jobs/internal/score"
	"linkedin-jobs/internal/store"
)

var scoreAll bool

var scoreLocal bool

var scoreCmd = &cobra.Command{
	Use:   "score --all [--local]",
	Short: "Re-score all non-filtered jobs against your current resume + preferences",
	Long: `Re-runs fit scoring for every non-filtered job using the resume and
preferences currently stored in your profile. Use this after editing your
profile to refresh scores across the DB.

By default each job is re-enriched via the LLM (one call per job) to
re-extract facts, then the deterministic rubric re-derives the score. Pass
--local to skip the LLM entirely and re-score against the enrichment facts
already saved in the DB — instant and free. (Dedup is ignored on re-score.)`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !scoreAll {
			die("use: linkedin-jobs score --all  (add --local to skip the LLM)")
		}
		settings, _ := config.LoadSettings()
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		p, _ := profile.Load(settings.Profile)
		weights := score.FromSettings(settings.Scoring)

		jobs, err := st.List(store.Filters{}) // excludes filtered by default
		if err != nil {
			die("query failed: %v", err)
		}
		total := len(jobs)

		var provider *llm.Provider
		var delay float64
		if scoreLocal {
			fmt.Fprintf(os.Stderr, "Re-scoring %d job(s) locally (no LLM)…\n", total)
		} else {
			cfg := loadCfg()
			provider, err = llm.Resolve(cfg)
			if err != nil {
				die("%v", err)
			}
			delay = resolveLLMDelay()
			fmt.Fprintf(os.Stderr, "Re-scoring %d job(s) via %s…\n", total, provider.Source)
		}
		fmt.Fprintln(os.Stderr, profileStatus(p))

		scored := 0
		for i, j := range jobs {
			if !scoreLocal {
				paceLLM(delay, i)
			}
			fmt.Fprintf(os.Stderr, "  [%d/%d] %s — %s\n", i+1, total, j.Title, j.Company)
			if scoreLocal {
				res := score.Compute(j, p, weights)
				if err := st.SetScore(j.ID, res.Score, score.FitReason(res), res.CapReason); err != nil {
					fmt.Fprintf(os.Stderr, "    ! %v\n", err)
					continue
				}
			} else {
				if err := enrichAndScoreJob(st, j, p, provider, weights); err != nil {
					fmt.Fprintf(os.Stderr, "    ! %v\n", err)
					continue
				}
			}
			scored++
		}
		fmt.Fprintf(os.Stderr, "Re-scored %d of %d job(s).\n", scored, total)
		return nil
	},
}

func init() {
	scoreCmd.Flags().BoolVar(&scoreAll, "all", false, "re-score all non-filtered jobs")
	scoreCmd.Flags().BoolVar(&scoreLocal, "local", false, "skip the LLM; re-score against the enrichment facts already saved in the DB")
	rootCmd.AddCommand(scoreCmd)
}
