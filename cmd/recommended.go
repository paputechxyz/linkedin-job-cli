package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	recLimit            int
	recMinSalary        string
	recSalaryCurrency   string
	recRemote           bool
	recExclude          []string
	recNoDetail         bool
	recNoSummarize      bool
	recNoScore          bool
	recNoFilter         bool
	recForceOW          bool
)

var recommendedCmd = &cobra.Command{
	Use:   "recommended",
	Short: "Pull your personalized LinkedIn 'Recommended for you' job feed",
	Long: `Fetches the authenticated 'Recommended for you' job collection using your
captured browser session (see 'auth login'). Requires a session. Pulls up to
--top jobs (alias: --limit), fetches salary + description, filters,
summarizes, stores, and displays the results.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		minSal := parseMinSalary(recMinSalary)
		currency := validateSalaryCurrency(recSalaryCurrency)
		if currency != "" && minSal == 0 {
			die("--salary-currency requires --min-salary.")
		}
		c, err := newClient(true)
		if err != nil {
			die("%v", err)
		}
		if !c.HasSession() {
			fmt.Fprintln(os.Stderr, "No LinkedIn session. Run: linkedin-jobs auth login")
			fmt.Fprintln(os.Stderr, "(or set LJ_COOKIES_FILE / LJ_COOKIE to a raw Cookie header)")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "Fetching your recommended jobs…")
		jobs, err := c.Recommended(recLimit)
		if err != nil && len(jobs) == 0 {
			die("failed to fetch recommended jobs: %v", err)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
		ingest(jobs, ingestOptions{
			minSalary:         minSal,
			minSalaryCurrency: currency,
			excludeCompanies:  recExclude,
			remote:            recRemote,
			noDetail:          recNoDetail,
			noSummarize:       recNoSummarize,
			noScore:           recNoScore,
			noFilter:          recNoFilter,
			forceOverwrite:    recForceOW,
			detailDelay:       resolveDetailDelay(),
			scoreDelay:        resolveLLMDelay(),
			jsonOut:           jsonOut,
		})
		return nil
	},
}

func init() {
	// --top is the primary name (matches the `search` command's convention);
	// --limit is kept as a backward-compat alias bound to the same variable.
	// If both are passed, the last one on the command line wins.
	recommendedCmd.Flags().IntVar(&recLimit, "top", 50, "max number of recommended jobs to fetch")
	recommendedCmd.Flags().IntVar(&recLimit, "limit", 50, "alias of --top")
	recommendedCmd.Flags().StringVar(&recMinSalary, "min-salary", "", "only keep jobs paying at or above this (e.g. 200k)")
	recommendedCmd.Flags().StringVar(&recSalaryCurrency, "salary-currency", "", "currency for --min-salary (ISO 4217, e.g. CAD); enables FX-aware filtering")
	recommendedCmd.Flags().BoolVar(&recRemote, "remote", false, "only keep remote-friendly jobs")
	recommendedCmd.Flags().StringSliceVar(&recExclude, "exclude-company", nil, "drop jobs whose company matches (repeatable)")
	recommendedCmd.Flags().BoolVar(&recNoDetail, "no-detail", false, "skip detail page fetching (faster; no salary/description)")
	recommendedCmd.Flags().BoolVar(&recNoSummarize, "no-summarize", false, "skip LLM scoring (alias of --no-score)")
	recommendedCmd.Flags().BoolVar(&recNoScore, "no-score", false, "skip LLM enrichment+fit-scoring")
	recommendedCmd.Flags().BoolVar(&recNoFilter, "no-filter", false, "skip the hard preference filter")
	recommendedCmd.Flags().BoolVar(&recForceOW, "force-overwrite", false, "re-parse and re-score jobs already in the DB (bypass dedup; overwrites existing values)")
	rootCmd.AddCommand(recommendedCmd)
}
