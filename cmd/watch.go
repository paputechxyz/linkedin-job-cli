package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/models"
)

var (
	watchTop       int
	watchMinSalary string
	watchRemote    bool
	watchExclude   []string
	watchNoDetail  bool
)

var watchCmd = &cobra.Command{
	Use:   "watch <keywords> <location>",
	Short: "Run a search and show only NEW jobs not seen before",
	Args:  cobra.ExactArgs(2),
	Long: `Re-runs an anonymous search and compares against jobs already stored. Only
brand-new job IDs (never seen) are fetched, summarized, stored, and displayed —
handy as a recurring "what's new" check. --top N caps how many jobs are pulled
from LinkedIn each run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		keywords, location := args[0], args[1]
		c, err := newClient(false)
		if err != nil {
			die("%v", err)
		}
		pages := (watchTop + 24) / 25
		if pages < 1 {
			pages = 1
		}
		fmt.Fprintf(os.Stderr, "Searching %q @ %q…\n", keywords, location)
		jobs, err := c.Search(keywords, location, pages)
		if err != nil {
			die("search failed: %v", err)
		}
		if watchTop > 0 && len(jobs) > watchTop {
			jobs = jobs[:watchTop]
		}
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()

		ids := make([]string, len(jobs))
		for i, j := range jobs {
			ids[i] = j.ID
		}
		existing, err := st.ExistingIDs(ids)
		if err != nil {
			die("lookup failed: %v", err)
		}
		var fresh []*models.JobPosting
		for _, j := range jobs {
			if !existing[j.ID] {
				fresh = append(fresh, j)
			}
		}
		fmt.Fprintf(os.Stderr, "Found %d jobs, %d new since last run.\n", len(jobs), len(fresh))

		// Run fresh jobs through the standard dedup -> hard-filter -> score pipeline.
		ingest(fresh, ingestOptions{
			minSalary:        parseMinSalary(watchMinSalary),
			excludeCompanies: watchExclude,
			remote:           watchRemote,
			noDetail:         watchNoDetail,
			detailDelay:      resolveDetailDelay(),
			scoreDelay:       resolveLLMDelay(),
			jsonOut:          jsonOut,
		})
		return nil
	},
}

func init() {
	watchCmd.Flags().IntVar(&watchTop, "top", 25, "cap on number of jobs to pull from LinkedIn each run")
	watchCmd.Flags().StringVar(&watchMinSalary, "min-salary", "", "only keep jobs paying at or above this")
	watchCmd.Flags().BoolVar(&watchRemote, "remote", false, "only remote-friendly jobs")
	watchCmd.Flags().StringSliceVar(&watchExclude, "exclude-company", nil, "drop jobs whose company matches")
	watchCmd.Flags().BoolVar(&watchNoDetail, "no-detail", false, "skip detail page fetching")
	rootCmd.AddCommand(watchCmd)
}
