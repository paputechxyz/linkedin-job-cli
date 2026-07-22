package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	urlTop     int
	urlForceOW bool
)

var urlCmd = &cobra.Command{
	Use:   "url <linkedin-search-url>",
	Short: "Score every job on a LinkedIn search/collection URL (authenticated; paste from browser or job-alert email)",
	Args:  cobra.ExactArgs(1),
	Long: `Fetches a LinkedIn search or collection URL (e.g. a job-alert email link, a
saved-search URL, or a URL pasted from the browser) and runs the jobs it
refers to through the same pipeline as 'recommended' / 'search'.

Strategy, in priority order:
  - URL has keywords= → when signed in, replay its filters against the
    authenticated Voyager jobCards API (the XHR the browser fires when you
    scroll /jobs/search/) so --top pulls every page; otherwise replay against
    the paginated seeMoreJobPostings API. The signed-in path returns the full
    result set (the anonymous endpoint caps early, e.g. 10 of 32).
  - URL only has explicit job IDs (originToLandingJobPostings from a job-alert
    email, or currentJobId) and no keywords → those IDs are used directly.
  - Otherwise → fetch the URL HTML and parse job cards.

Title/company/location are filled from JSON-LD on the detail page when the
listing didn't provide them. Salary + description are fetched per-job, then
every job is enriched + fit-scored against your profile. No ingest-time
filters — use 'list --remote --min-salary ...' or the 'serve' filters to
exclude at view time.

Auth: authenticated via your captured browser session (see 'auth status'). A
session is recommended — without one, URL fetches fall back to a limited
anonymous endpoint that caps early.

Examples:
  linkedin-jobs url "https://www.linkedin.com/jobs/search/?keywords=Staff%20Engineer&geoId=101788145" --top 50
  linkedin-jobs url "https://www.linkedin.com/jobs/search/?currentJobId=4415889466&originToLandingJobPostings=4415889466%2C4434154740&keywords=Staff%20Engineer"
  linkedin-jobs url "https://www.linkedin.com/jobs/collections/recommended/?start=0"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := mustResolveProvider()
		rawURL := args[0]
		// url is an authenticated command: a session drives the Voyager
		// jobCards search path, which returns the full result set the signed-in
		// browser sees. Without a session it degrades to a limited anonymous
		// endpoint that caps early, so attachSession failure is non-fatal but
		// worth surfacing via auth status rather than hard-exiting.
		c, _ := newClient(true)
		fmt.Fprintf(os.Stderr, "Fetching jobs from URL…\n")
		jobs, err := c.SearchURL(rawURL, urlTop)
		if err != nil {
			die("url fetch failed: %v", err)
		}
		if urlTop > 0 && len(jobs) > urlTop {
			jobs = jobs[:urlTop]
		}
		ingest(jobs, provider, ingestOptions{
			forceOverwrite: urlForceOW,
			detailDelay:    resolveDetailDelay(),
			scoreDelay:     resolveLLMDelay(),
			jsonOut:        jsonOut,
		})
		return nil
	},
}

func init() {
	urlCmd.Flags().IntVar(&urlTop, "top", 0, "cap on number of jobs to process end-to-end (0 = all jobs from the URL)")
	urlCmd.Flags().BoolVar(&urlForceOW, "force-overwrite", false, "re-parse and re-score jobs already in the DB (bypass dedup; overwrites existing values)")
	rootCmd.AddCommand(urlCmd)
}
