package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	searchTop      int
	searchMinSalary string
	searchRemote   bool
	searchExclude  []string
	searchNoDetail bool
	searchNoSummar bool
	searchNoScore  bool
	searchNoFilter bool
	searchForceOW  bool
)

var searchCmd = &cobra.Command{
	Use:   "search [keywords] [location]",
	Short: "Search LinkedIn's public job board (anonymous, no session required)",
	Args:  cobra.MinimumNArgs(1),
	Long: `Searches LinkedIn's public (logged-out) job board and ingests results through
the same pipeline as 'recommended'. Works without a session; no login needed.
--top N caps the number of jobs processed end-to-end (detail fetch + LLM score).

Examples:
  linkedin-jobs search "Staff Engineer" Toronto --min-salary 200k
  linkedin-jobs search "Senior Developer" "Remote, US" --top 3`,
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
		pages := (searchTop + 24) / 25
		if pages < 1 {
			pages = 1
		}
		fmt.Fprintf(os.Stderr, "Searching LinkedIn Jobs: %q @ %q…\n", keywords, location)
		jobs, err := c.Search(keywords, location, pages)
		if err != nil {
			die("search failed: %v", err)
		}
		if searchTop > 0 && len(jobs) > searchTop {
			jobs = jobs[:searchTop]
		}
		ingest(jobs, ingestOptions{
			minSalary:        parseMinSalary(searchMinSalary),
			excludeCompanies: searchExclude,
			remote:           searchRemote,
			noDetail:         searchNoDetail,
			noSummarize:      searchNoSummar,
			noScore:          searchNoScore,
			noFilter:         searchNoFilter,
			forceOverwrite:   searchForceOW,
			detailDelay:      resolveDetailDelay(),
			scoreDelay:       resolveLLMDelay(),
			jsonOut:          jsonOut,
		})
		return nil
	},
}

func init() {
	searchCmd.Flags().IntVar(&searchTop, "top", 25, "cap on number of jobs to fetch + process end-to-end")
	searchCmd.Flags().StringVar(&searchMinSalary, "min-salary", "", "only keep jobs paying at or above this (e.g. 200k)")
	searchCmd.Flags().BoolVar(&searchRemote, "remote", false, "only keep remote-friendly jobs")
	searchCmd.Flags().StringSliceVar(&searchExclude, "exclude-company", nil, "drop jobs whose company matches (repeatable)")
	searchCmd.Flags().BoolVar(&searchNoDetail, "no-detail", false, "skip detail page fetching")
	searchCmd.Flags().BoolVar(&searchNoSummar, "no-summarize", false, "skip LLM scoring (alias of --no-score)")
	searchCmd.Flags().BoolVar(&searchNoScore, "no-score", false, "skip LLM enrichment+fit-scoring")
	searchCmd.Flags().BoolVar(&searchNoFilter, "no-filter", false, "skip the hard preference filter")
	searchCmd.Flags().BoolVar(&searchForceOW, "force-overwrite", false, "re-parse and re-score jobs already in the DB (bypass dedup; overwrites existing values)")
	rootCmd.AddCommand(searchCmd)
}
