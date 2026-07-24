package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/store"
)

var jobCmd = &cobra.Command{
	Use:   "job <job-id>",
	Short: "Fetch + fit-score a single LinkedIn job by its numeric ID",
	Args:  cobra.ExactArgs(1),
	Long: `Fetches a single LinkedIn job posting by its numeric job ID and runs it
through the same fetch → score pipeline as 'search'. No flags: scoring context
(salary floor, work arrangement) comes from your settings.yaml profile. The
job is always (re-)fetched and (re-)scored.

The argument must be a bare integer ID (digits only), e.g. 4434368088 — the
trailing digits of a /jobs/view/<...>-<id>/ URL. A full URL or any non-integer
is rejected; for a page of many jobs use 'url <search-url>' instead.

Example:
  linkedin-jobs job 4434368088`,
	RunE: func(cmd *cobra.Command, args []string) error {
		id := parseJobIDArg(args[0])
		if id == "" {
			if hint := viewJobIDFromURL(args[0]); hint != "" {
				die("job expects a numeric LinkedIn job ID, got %q.\n"+
					"That URL points to one job — extract its id and run:\n"+
					"  linkedin-jobs job %s", args[0], hint)
			}
			die("job expects a numeric LinkedIn job ID (digits only), got %q.\n"+
				"Example: linkedin-jobs job 4434368088\n"+
				"For a search/collection page, use: linkedin-jobs url <search-url>", args[0])
		}
		provider := mustResolveProvider()
		job := &models.JobPosting{
			ID:         id,
			URL:        "https://www.linkedin.com/jobs/view/" + id + "/",
			Title:      "Unknown Title",
			Source:     "id",
			SearchedAt: store.NowISO(),
		}
		fmt.Fprintf(os.Stderr, "Fetching + scoring job %s…\n", id)
		ingest([]*models.JobPosting{job}, provider, ingestOptions{
			forceOverwrite: true,
			detailDelay:    resolveDetailDelay(),
			scoreDelay:     resolveLLMDelay(),
			jsonOut:        jsonOut,
		})
		return nil
	},
}

func init() {
	rootCmd.AddCommand(jobCmd)
}
