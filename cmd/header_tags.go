package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/linkedin"
	"linkedin-jobs/internal/render"
)

// headerTagsCmd fetches the authoritative workplace-type "header tag" (the
// Remote/Hybrid/On-site badge) for a job directly from LinkedIn's Voyager
// jobPostings API. The critics agent uses this to verify the stored
// remote_type against LinkedIn's structured badge, catching cases where the
// parser's DetectRemote heuristic overrode the authoritative value.
var headerTagsCmd = &cobra.Command{
	Use:   "header-tags <job-id>",
	Short: "Fetch the workplace-type header tag from LinkedIn's Voyager API",
	Args:  cobra.ExactArgs(1),
	Long: `Fetches the authoritative workplace-type badge (Remote/Hybrid/On-site)
for a job directly from LinkedIn's Voyager jobPostings API — the same data the
detail page renders as the header tag next to the title.

This is distinct from the stored remote_type, which the parser derives from
description prose via DetectRemote. When the two disagree, the parser overrode
LinkedIn's badge; the critics agent uses this command to detect that.

Requires an authenticated LinkedIn session (set LJ_COOKIES_FILE or LJ_COOKIE).
With --json, emits a machine-readable HeaderTags object; otherwise prints a
human-readable summary.

Example:
  linkedin-jobs header-tags 4259504707 --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		c, err := newClient(true)
		if err != nil {
			die("failed to resolve LinkedIn session: %v", err)
		}
		if !c.HasSession() {
			die("no session: header-tags requires LinkedIn auth (set LJ_COOKIES_FILE or LJ_COOKIE)")
		}
		ht, err := c.FetchHeaderTags(id)
		if err != nil {
			die("header-tags fetch failed: %v", err)
		}
		if ht.Source == "" {
			fmt.Fprintf(os.Stderr, "No workplace-type data returned for job %s (API soft-miss).\n", id)
		}
		if jsonOut {
			render.AsJSON(os.Stdout, ht)
		} else {
			renderHeaderTags(os.Stdout, ht)
		}
		return nil
	},
}

// renderHeaderTags writes a human-readable summary of the workplace-type
// header tag to w. Kept minimal since the primary consumer is `--json` (the
// critics agent); the text form is for ad-hoc interactive use.
func renderHeaderTags(w *os.File, ht linkedin.HeaderTags) {
	if ht.Source == "" {
		fmt.Fprintf(w, "Job %s: no workplace-type data (API soft-miss).\n", ht.JobID)
		return
	}
	fmt.Fprintf(w, "Job %s:\n", ht.JobID)
	fmt.Fprintf(w, "  Remote type:       %s\n", orNA2(ht.RemoteType))
	if len(ht.WorkplaceTypeURNs) > 0 {
		fmt.Fprintf(w, "  URNs:              %v\n", ht.WorkplaceTypeURNs)
	}
	fmt.Fprintf(w, "  workRemoteAllowed: %v\n", ht.WorkRemoteAllowed)
	fmt.Fprintf(w, "  Source:            %s\n", ht.Source)
}

func init() {
	rootCmd.AddCommand(headerTagsCmd)
}
