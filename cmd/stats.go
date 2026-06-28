package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"linkedin-job-cli/internal/render"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Aggregate stats over the local job database",
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		s, err := st.Stats()
		if err != nil {
			die("stats failed: %v", err)
		}
		if jsonOut {
			render.AsJSON(os.Stdout, s)
		} else {
			render.Stats(os.Stdout, s)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statsCmd)
}
