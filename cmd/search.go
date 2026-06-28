package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	searchPages     int
	searchMinSalary string
	searchRemote    bool
	searchExclude   []string
	searchNoDetail  bool
	searchNoSummar  bool
	searchNoScore   bool
	searchNoFilter  bool
)

var searchCmd = &cobra.Command{
	Use:   "search [keywords] [location]",
	Short: "Search LinkedIn's public job board (anonymous, no session required)",
	Args:  cobra.MinimumNArgs(1),
	Long: `Searches LinkedIn's public (logged-out) job board and ingests results through
the same pipeline as 'recommended'. Works without a session; no login needed.

Examples:
  linkedin-jobs search "Staff Engineer" Toronto --min-salary 200k
  linkedin-jobs search "Senior Developer" "Remote, US" --pages 2`,
	RunE: func(cmd *cobra.Command, args []string) error {
		keywords := args[0]
		location := ""
		if len(args) > 1 {
			location = args[1]
		}
		c, err := newClient(false)
		if err != nil {
			die("%v", err)
		}
		fmt.Fprintf(os.Stderr, "Searching LinkedIn Jobs: %q @ %q…\n", keywords, location)
		jobs, err := c.Search(keywords, location, searchPages)
		if err != nil {
			die("search failed: %v", err)
		}
		ingest(jobs, ingestOptions{
			minSalary:        parseMinSalary(searchMinSalary),
			excludeCompanies: searchExclude,
			remote:           searchRemote,
			noDetail:         searchNoDetail,
			noSummarize:      searchNoSummar,
			noScore:          searchNoScore,
			noFilter:         searchNoFilter,
			detailDelay:      resolveDetailDelay(),
			jsonOut:          jsonOut,
		})
		return nil
	},
}

func init() {
	searchCmd.Flags().IntVar(&searchPages, "pages", 1, "number of result pages to fetch (25 jobs/page)")
	searchCmd.Flags().StringVar(&searchMinSalary, "min-salary", "", "only keep jobs paying at or above this (e.g. 200k)")
	searchCmd.Flags().BoolVar(&searchRemote, "remote", false, "only keep remote-friendly jobs")
	searchCmd.Flags().StringSliceVar(&searchExclude, "exclude-company", nil, "drop jobs whose company matches (repeatable)")
	searchCmd.Flags().BoolVar(&searchNoDetail, "no-detail", false, "skip detail page fetching")
	searchCmd.Flags().BoolVar(&searchNoSummar, "no-summarize", false, "skip LLM scoring (alias of --no-score)")
	searchCmd.Flags().BoolVar(&searchNoScore, "no-score", false, "skip LLM enrichment+fit-scoring")
	searchCmd.Flags().BoolVar(&searchNoFilter, "no-filter", false, "skip the hard preference filter")
	rootCmd.AddCommand(searchCmd)
}
