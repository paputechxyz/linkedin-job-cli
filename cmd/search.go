package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/models"
)

var (
	searchTop    int
	searchForceOW bool
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search LinkedIn's public job board (anonymous, no session required)",
	Args:  cobra.MinimumNArgs(1),
	Long: `Searches LinkedIn's public (logged-out) job board and ingests results through
the same pipeline as 'recommended'. Works without a session; no login needed.

The query is a single string split into keywords + location on the FIRST comma:
everything before the comma is the keyword search; everything after is the
location. Locations often contain commas themselves ("Remote, US", "Toronto,
Ontario, Canada"), while keywords rarely do, so the first-comma split keeps
multi-comma locations intact. Omit the comma for a keywords-only search.

--top N caps the number of jobs processed end-to-end (detail fetch + LLM score).
Jobs already in the DB (by LinkedIn ID) are skipped entirely — only brand-new
jobs are detail-fetched, scored, and displayed. Pass --force-overwrite to
re-process existing jobs (bypasses the new-only pre-filter and content-hash
dedup; overwrites stored values). Every fetched job is persisted and scored;
no ingest-time filters — use 'list --remote --min-salary ...' or the 'serve'
filters to exclude at view time.

Examples:
  linkedin-jobs search "Staff Engineer, Toronto"
  linkedin-jobs search "Senior Developer, Remote, US" --top 3`,
	RunE: func(cmd *cobra.Command, args []string) error {
		provider := mustResolveProvider()
		keywords, location := splitSearchQuery(strings.Join(args, " "))
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

		// New-only pre-filter: drop jobs whose LinkedIn ID is already stored so
		// we skip detail-fetch, scoring, and display for jobs seen before. The
		// content-hash dedup inside ingest still guards against structural
		// duplicates that slip in under new IDs. --force-overwrite bypasses
		// this pre-filter (and the ingest dedup) to re-process everything.
		target := jobs
		if !searchForceOW {
			target = filterNewIDs(jobs)
		}

		ingest(target, provider, ingestOptions{
			forceOverwrite: searchForceOW,
			detailDelay:    resolveDetailDelay(),
			scoreDelay:     resolveLLMDelay(),
			jsonOut:        jsonOut,
		})
		return nil
	},
}

func init() {
	searchCmd.Flags().IntVar(&searchTop, "top", 20, "cap on number of jobs to fetch + process end-to-end (each is LLM-scored; raise to burn more tokens)")
	searchCmd.Flags().BoolVar(&searchForceOW, "force-overwrite", false, "re-parse and re-score jobs already in the DB (bypass the new-only pre-filter and dedup; overwrites existing values)")
	rootCmd.AddCommand(searchCmd)
}

// filterNewIDs returns only the jobs whose LinkedIn ID is not already stored.
// Used by 'search' to skip existing jobs entirely — no detail fetch, no score,
// no display — so re-running a query shows only what's new since the last run.
func filterNewIDs(jobs []*models.JobPosting) []*models.JobPosting {
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
	return fresh
}

// splitSearchQuery splits a single search string into keywords + location on
// the FIRST comma. Locations frequently contain commas themselves ("Remote, US",
// "Toronto, Ontario, Canada"), while job keywords rarely do, so the first-comma
// split keeps multi-comma locations intact. A query with no comma is keywords-
// only. Examples:
//
//	"Staff Engineer, Toronto"            → ("Staff Engineer", "Toronto")
//	"Senior Developer, Remote, US"       → ("Senior Developer", "Remote, US")
//	"Staff Engineer"                     → ("Staff Engineer", "")
func splitSearchQuery(q string) (keywords, location string) {
	q = strings.TrimSpace(q)
	if i := strings.Index(q, ","); i >= 0 {
		return strings.TrimSpace(q[:i]), strings.TrimSpace(q[i+1:])
	}
	return q, ""
}
