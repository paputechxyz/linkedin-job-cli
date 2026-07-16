package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/models"
)

var (
	searchTop             int
	searchMinSalary       string
	searchSalaryCurrency  string
	searchRemote          bool
	searchHybrid          bool
	searchOnsite          bool
	searchForceOW         bool
)

var searchCmd = &cobra.Command{
	Use:   "search [keywords] [location]",
	Short: "Search LinkedIn's public job board (anonymous, no session required)",
	Args:  cobra.MinimumNArgs(1),
	Long: `Searches LinkedIn's public (logged-out) job board and ingests results through
the same pipeline as 'recommended'. Works without a session; no login needed.
--top N caps the number of jobs processed end-to-end (detail fetch + LLM score).
Jobs already in the DB (by LinkedIn ID) are skipped entirely — only brand-new
jobs are detail-fetched, scored, and displayed. Pass --force-overwrite to
re-process existing jobs (bypasses the new-only pre-filter and content-hash
dedup; overwrites stored values). Jobs failing any active user gate
(--remote/--hybrid/--onsite/--min-salary) are dropped in-memory after the detail
fetch and never stored or scored.

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
		provider := mustResolveProvider()
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
			minSalary:         minSal,
			minSalaryCurrency: currency,
			remote:            searchRemote,
			hybrid:            searchHybrid,
			onsite:            searchOnsite,
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
