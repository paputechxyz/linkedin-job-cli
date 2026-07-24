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

This command is for search/collection URLs (pages with many jobs). A single
/job posting URL (/jobs/view/<id>/) is rejected — use 'job <id>' for one job.

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
		rawURL := args[0]
		// Gate: a single-job /jobs/view/<id>/ URL is not a collection. The
		// user almost certainly wants to score one job, so point them at `job`
		// with the extracted id instead of silently doing the wrong thing.
		// Run before provider/session resolution so it fails fast.
		if id := viewJobIDFromURL(rawURL); id != "" {
			die("url expects a LinkedIn search/collection URL (a page with many jobs), got a single job posting:\n"+
				"  %s\n"+
				"Score this one job with: linkedin-jobs job %s", rawURL, id)
		}
		provider := mustResolveProvider()
		// url is an authenticated command: a session exposes the full result
		// set the signed-in browser sees. Without one, search still paginates
		// but over a smaller anonymous total.
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
	urlCmd.Flags().IntVar(&urlTop, "top", 20, "cap on number of jobs to process end-to-end (each is LLM-scored; 0 = all jobs from the URL)")
	urlCmd.Flags().BoolVar(&urlForceOW, "force-overwrite", false, "re-parse and re-score jobs already in the DB (bypass dedup; overwrites existing values)")
	rootCmd.AddCommand(urlCmd)
}
