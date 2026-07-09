package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/render"
)

var countCmd = &cobra.Command{
	Use:   "count",
	Short: "Print the total number of jobs saved in the local database",
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		n, err := st.Count()
		if err != nil {
			die("count failed: %v", err)
		}
		if jsonOut {
			render.AsJSON(os.Stdout, map[string]int64{"count": n})
		} else {
			fmt.Fprintf(os.Stdout, "%d jobs\n", n)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(countCmd)
}
