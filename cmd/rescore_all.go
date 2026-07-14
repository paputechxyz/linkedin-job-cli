package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/filter"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/profile"
	"linkedin-jobs/internal/score"
	"linkedin-jobs/internal/store"
)

var rescoreAllCmd = &cobra.Command{
	Use:   "rescore-all",
	Short: "Re-enrich + re-score every stored job via the LLM, and re-judge filter status",
	Long: `Re-runs the full LLM enrichment + deterministic rubric score for every job
in the DB — regardless of its current status (filtered jobs included) — then
re-applies the hard filter so each job's filtered tag reflects your current
profile. Use this after editing your preference knobs or scoring weights.

This always calls the LLM (one call per job) and ignores dedup, so it costs
tokens proportional to your DB size. A job that now fails the hard filter is
tagged filtered; one that was filtered but now passes is moved back to new.
Explicit triage statuses (saved/applied/rejected) are preserved — only the
filtered tag is re-judged.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		settings, _ := config.LoadSettings()
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		p, _ := profile.Load(settings.Profile)
		weights := score.FromSettings(settings.Scoring)

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
		fmt.Fprintf(os.Stderr, "Re-scoring %d job(s) via %s…\n", total, provider.Source)
		fmt.Fprintln(os.Stderr, profileStatus(p))

		scored, refiltered, unfiltered := 0, 0, 0
		for i, j := range jobs {
			paceLLM(delay, i)
			fmt.Fprintf(os.Stderr, "  [%d/%d] %s — %s\n", i+1, total, j.Title, j.Company)
			if err := enrichAndScoreJob(st, j, p, provider, weights); err != nil {
				fmt.Fprintf(os.Stderr, "    ! %v\n", err)
				continue
			}
			scored++
			if settings.Filter.AutoFilter {
				switch {
				case !filter.PassesHardFilter(j, p) && !isManualStatus(j.Status) && j.Status != "filtered":
					if err := st.SetTag(j.ID, "filtered", ""); err != nil {
						fmt.Fprintf(os.Stderr, "    ! %v\n", err)
					} else {
						j.Status = "filtered"
						refiltered++
					}
				case filter.PassesHardFilter(j, p) && j.Status == "filtered":
					if err := st.SetTag(j.ID, "new", ""); err != nil {
						fmt.Fprintf(os.Stderr, "    ! %v\n", err)
					} else {
						j.Status = "new"
						unfiltered++
					}
				}
			}
		}
		fmt.Fprintf(os.Stderr, "Re-scored %d of %d job(s)", scored, total)
		if settings.Filter.AutoFilter {
			fmt.Fprintf(os.Stderr, "; filter re-judged: %d newly filtered, %d moved to new", refiltered, unfiltered)
		}
		fmt.Fprintln(os.Stderr, ".")
		return nil
	},
}

// isManualStatus reports whether a status represents an explicit user triage
// decision that the auto filter must not clobber. Only the filtered tag is
// ever re-judged by rescore-all; saved/applied/rejected are left as-is.
func isManualStatus(s string) bool {
	switch s {
	case "saved", "applied", "rejected":
		return true
	}
	return false
}

func init() {
	rootCmd.AddCommand(rescoreAllCmd)
}
