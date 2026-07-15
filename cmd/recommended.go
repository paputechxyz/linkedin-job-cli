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
	recHybrid           bool
	recOnsite           bool
	recForceOW          bool
)

var recommendedCmd = &cobra.Command{
	Use:   "recommended",
	Short: "Pull your personalized LinkedIn 'Recommended for you' job feed",
	Long: `Fetches the authenticated 'Recommended for you' job collection using your
captured browser session (see 'auth status'). Requires a session. Pulls up to
--top jobs, fetches salary + description, applies the user gates
(--remote/--hybrid/--onsite/--min-salary; jobs failing any active gate are dropped
in-memory and never stored or scored), summarizes, stores, and displays the
survivors.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		minSal := parseMinSalary(recMinSalary)
		currency := validateSalaryCurrency(recSalaryCurrency)
		if currency != "" && minSal == 0 {
			die("--salary-currency requires --min-salary.")
		}
		provider := mustResolveProvider()
		c, err := newClient(true)
		if err != nil {
			die("%v", err)
		}
		if !c.HasSession() {
			fmt.Fprintln(os.Stderr, "No LinkedIn session. Run `linkedin-jobs auth login` to capture one.")
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
		ingest(jobs, provider, ingestOptions{
			minSalary:         minSal,
			minSalaryCurrency: currency,
			remote:            recRemote,
			hybrid:            recHybrid,
			onsite:            recOnsite,
			forceOverwrite:    recForceOW,
			detailDelay:       resolveDetailDelay(),
			scoreDelay:        resolveLLMDelay(),
			jsonOut:           jsonOut,
		})
		return nil
	},
}

func init() {
	recommendedCmd.Flags().IntVar(&recLimit, "top", 50, "max number of recommended jobs to fetch")
	recommendedCmd.Flags().StringVar(&recMinSalary, "min-salary", "", "only keep jobs paying at or above this (e.g. 200k)")
	recommendedCmd.Flags().StringVar(&recSalaryCurrency, "salary-currency", "", "currency for --min-salary (ISO 4217, e.g. CAD); enables FX-aware filtering")
	recommendedCmd.Flags().BoolVar(&recRemote, "remote", false, "only keep remote-friendly jobs")
	recommendedCmd.Flags().BoolVar(&recHybrid, "hybrid", false, "only keep hybrid-friendly jobs (combine with --remote/--onsite for OR)")
	recommendedCmd.Flags().BoolVar(&recOnsite, "onsite", false, "only keep on-site jobs (combine with --remote/--hybrid for OR)")
	recommendedCmd.Flags().BoolVar(&recForceOW, "force-overwrite", false, "re-parse and re-score jobs already in the DB (bypass dedup; overwrites existing values)")
	rootCmd.AddCommand(recommendedCmd)
}
