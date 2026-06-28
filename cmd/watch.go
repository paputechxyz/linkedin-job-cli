package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-job-cli/internal/linkedin"
	"linkedin-job-cli/internal/models"
	"linkedin-job-cli/internal/render"
)

var (
	watchPages     int
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
handy as a recurring "what's new" check.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		keywords, location := args[0], args[1]
		c, err := newClient(false)
		if err != nil {
			die("%v", err)
		}
		fmt.Fprintf(os.Stderr, "Searching %q @ %q…\n", keywords, location)
		jobs, err := c.Search(keywords, location, watchPages)
		if err != nil {
			die("search failed: %v", err)
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

		if !watchNoDetail && len(fresh) > 0 {
			linkedin.New(loadCfg()).FetchDetailsBatch(fresh, resolveDetailDelay(), nil)
		}
		if !jsonOut && len(fresh) > 0 {
			for _, j := range fresh {
				j.LLMSummary = llmSum(j)
			}
		} else if len(fresh) > 0 {
			for _, j := range fresh {
				j.LLMSummary = llmSum(j)
			}
		}
		opts := ingestOptions{
			minSalary:        parseMinSalary(watchMinSalary),
			excludeCompanies: watchExclude,
			remote:           watchRemote,
			noDetail:         watchNoDetail,
			noSummarize:      true, // summarized inline above
			detailDelay:      resolveDetailDelay(),
			jsonOut:          jsonOut,
		}
		filtered := filterJobs(fresh, opts)
		for _, j := range fresh {
			st.Upsert(j)
		}
		if jsonOut {
			render.AsJSON(os.Stdout, filtered)
		} else if len(filtered) == 0 {
			fmt.Fprintln(os.Stderr, "No new jobs matched your filters.")
		} else {
			render.Table(os.Stdout, filtered)
		}
		return nil
	},
}

func init() {
	watchCmd.Flags().IntVar(&watchPages, "pages", 1, "number of result pages to fetch")
	watchCmd.Flags().StringVar(&watchMinSalary, "min-salary", "", "only keep jobs paying at or above this")
	watchCmd.Flags().BoolVar(&watchRemote, "remote", false, "only remote-friendly jobs")
	watchCmd.Flags().StringSliceVar(&watchExclude, "exclude-company", nil, "drop jobs whose company matches")
	watchCmd.Flags().BoolVar(&watchNoDetail, "no-detail", false, "skip detail page fetching")
	rootCmd.AddCommand(watchCmd)
}
