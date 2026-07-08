package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	searchTop             int
	searchMinSalary       string
	searchSalaryCurrency  string
	searchRemote          bool
	searchHybrid          bool
	searchOnsite          bool
	searchNoDetail        bool
	searchNoScore         bool
	searchNoFilter        bool
	searchForceOW         bool
)

var searchCmd = &cobra.Command{
	Use:   "search [keywords] [location]",
	Short: "Search LinkedIn's public job board (anonymous, no session required)",
	Args:  cobra.MinimumNArgs(1),
	Long: `Searches LinkedIn's public (logged-out) job board and ingests results through
the same pipeline as 'recommended'. Works without a session; no login needed.
--top N caps the number of jobs processed end-to-end (detail fetch + LLM score).
Jobs failing any active user gate (--remote/--hybrid/--onsite/--min-salary) are dropped
in-memory after the detail fetch and never stored or scored.

Examples:
  linkedin-jobs search "Staff Engineer" Toronto --min-salary 200k
  linkedin-jobs search "Senior Developer" "Remote, US" --top 3
  linkedin-jobs search "Staff Engineer" Toronto --min-salary 160k --salary-currency CAD`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate the salary floor + currency up front so a typo fails before
		// we hit LinkedIn (and before the FX-aware filter runs).
		minSal := parseMinSalary(searchMinSalary)
		currency := validateSalaryCurrency(searchSalaryCurrency)
		if currency != "" && minSal == 0 {
			die("--salary-currency requires --min-salary.")
		}
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
			minSalary:         minSal,
			minSalaryCurrency: currency,
			remote:            searchRemote,
			hybrid:            searchHybrid,
			onsite:            searchOnsite,
			noDetail:          searchNoDetail,
			noScore:           searchNoScore,
			noFilter:          searchNoFilter,
			forceOverwrite:    searchForceOW,
			detailDelay:       resolveDetailDelay(),
			scoreDelay:        resolveLLMDelay(),
			jsonOut:           jsonOut,
		})
		return nil
	},
}

func init() {
	searchCmd.Flags().IntVar(&searchTop, "top", 25, "cap on number of jobs to fetch + process end-to-end")
	searchCmd.Flags().StringVar(&searchMinSalary, "min-salary", "", "only keep jobs paying at or above this (e.g. 200k)")
	searchCmd.Flags().StringVar(&searchSalaryCurrency, "salary-currency", "", "currency for --min-salary (ISO 4217, e.g. CAD); enables FX-aware filtering")
	searchCmd.Flags().BoolVar(&searchRemote, "remote", false, "only keep remote-friendly jobs")
	searchCmd.Flags().BoolVar(&searchHybrid, "hybrid", false, "only keep hybrid-friendly jobs (combine with --remote/--onsite for OR)")
	searchCmd.Flags().BoolVar(&searchOnsite, "onsite", false, "only keep on-site jobs (combine with --remote/--hybrid for OR)")
	searchCmd.Flags().BoolVar(&searchNoDetail, "no-detail", false, "skip detail page fetching")
	searchCmd.Flags().BoolVar(&searchNoScore, "no-score", false, "skip LLM enrichment+fit-scoring")
	searchCmd.Flags().BoolVar(&searchNoFilter, "no-filter", false, "skip the hard preference filter")
	searchCmd.Flags().BoolVar(&searchForceOW, "force-overwrite", false, "re-parse and re-score jobs already in the DB (bypass dedup; overwrites existing values)")
	rootCmd.AddCommand(searchCmd)
}
