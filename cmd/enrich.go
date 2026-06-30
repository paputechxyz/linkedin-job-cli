package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/profile"
	"linkedin-jobs/internal/render"
)

var enrichAll bool

var enrichCmd = &cobra.Command{
	Use:   "enrich [<job-id>]",
	Short: "Enrich + fit-score one job, or all unenriched jobs (--all)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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

		var jobs []*models.JobPosting
		if enrichAll {
			jobs, err = st.Unenriched()
			if err != nil {
				die("query failed: %v", err)
			}
			if len(jobs) == 0 {
				fmt.Println("All jobs already enriched.")
				return nil
			}
		} else {
			if len(args) == 0 {
				die("provide a job id, or use --all")
			}
			j, err := st.Get(args[0])
			if err != nil {
				die("query failed: %v", err)
			}
			if j == nil {
				die("Job not found: %s", args[0])
			}
			jobs = []*models.JobPosting{j}
		}

		fmt.Fprintf(os.Stderr, "Enriching + scoring %d job(s) via %s…\n", len(jobs), provider.Source)
		fmt.Fprintln(os.Stderr, profileStatus(p))
		delay := resolveLLMDelay()
		scored := 0
		for _, j := range jobs {
			paceLLM(delay, scored)
			if _, err := enrichAndScoreJob(st, j, p, provider, settings.Scoring.ReasonThreshold); err != nil {
				fmt.Fprintf(os.Stderr, "  ! %s: %v\n", j.Title, err)
				continue
			}
			scored++
			score := "-"
			if j.FitScore != nil {
				score = fmt.Sprintf("%d", *j.FitScore)
			}
			fmt.Fprintf(os.Stderr, "  + [%s] %s @ %s\n", score, j.Title, orNA2(j.Company))
		}
		fmt.Fprintln(os.Stderr, "Done.")

		if !enrichAll && len(args) == 1 {
			j, _ := st.Get(args[0])
			if jsonOut {
				render.AsJSON(os.Stdout, j)
			} else {
				render.Detail(os.Stdout, j)
			}
		}
		return nil
	},
}

func init() {
	enrichCmd.Flags().BoolVar(&enrichAll, "all", false, "enrich all jobs that lack enrichment")
	rootCmd.AddCommand(enrichCmd)
}
