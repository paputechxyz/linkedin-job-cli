package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/render"
)

var tagNote string

var tagCmd = &cobra.Command{
	Use:   "tag <job-id> <status>",
	Short: "Set a job's pipeline status (new/saved/applied/rejected)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, status := args[0], args[1]
		if !validStatus(status) {
			die("invalid status %q (use: new, saved, applied, rejected)", status)
		}
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		j, err := st.Get(id)
		if err != nil {
			die("query failed: %v", err)
		}
		if j == nil {
			die("Job not found: %s", id)
		}
		if err := st.SetTag(id, status, tagNote); err != nil {
			die("update failed: %v", err)
		}
		j.Status = status
		if tagNote != "" {
			j.Notes = tagNote
		}
		fmt.Fprintf(os.Stderr, "Tagged %s as %s.\n", j.Title, status)
		if jsonOut {
			render.AsJSON(os.Stdout, j)
		}
		return nil
	},
}

func validStatus(s string) bool {
	switch s {
	case "new", "viewed", "saved", "applied", "rejected":
		return true
	}
	return false
}

func init() {
	tagCmd.Flags().StringVar(&tagNote, "note", "", "attach a note to the job")
	rootCmd.AddCommand(tagCmd)
}
