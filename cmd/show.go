package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"linkedin-job-cli/internal/render"
)

var showCmd = &cobra.Command{
	Use:   "show <job-id>",
	Short: "Show full details for a single saved job",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		j, err := st.Get(args[0])
		if err != nil {
			die("query failed: %v", err)
		}
		if j == nil {
			die("Job not found: %s", args[0])
		}
		if jsonOut {
			render.AsJSON(os.Stdout, j)
		} else {
			render.Detail(os.Stdout, j)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(showCmd)
}
