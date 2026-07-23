package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	recLimit   int
	recForceOW bool
)

var recommendedCmd = &cobra.Command{
	Use:   "recommended",
	Short: "Pull your personalized LinkedIn 'Recommended for you' job feed",
	Long: `Fetches the authenticated 'Recommended for you' job collection using your
captured browser session (see 'auth status'). Requires a session. Pulls up to
--top jobs, fetches salary + description, summarizes, stores, and scores every
one of them. No ingest-time filters: preferences (work arrangement, salary
floor) live under profile: in settings.yaml and feed the soft system rubrics,
which lower the score on mismatches rather than dropping jobs. Use
'list --remote --min-salary ...' or the 'serve' filters to exclude at view time.`,
	RunE: func(cmd *cobra.Command, args []string) error {
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
			forceOverwrite: recForceOW,
			detailDelay:    resolveDetailDelay(),
			scoreDelay:     resolveLLMDelay(),
			jsonOut:        jsonOut,
		})
		return nil
	},
}

func init() {
	recommendedCmd.Flags().IntVar(&recLimit, "top", 20, "max number of recommended jobs to fetch (each is LLM-scored; raise to burn more tokens)")
	recommendedCmd.Flags().BoolVar(&recForceOW, "force-overwrite", false, "re-parse and re-score jobs already in the DB (bypass dedup; overwrites existing values)")
	rootCmd.AddCommand(recommendedCmd)
}
