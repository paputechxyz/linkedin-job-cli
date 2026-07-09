package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/render"
)

var countFiltered bool

var countCmd = &cobra.Command{
	Use:   "count",
	Short: "Print the number of jobs saved in the local database",
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		var n int64
		if countFiltered {
			n, err = st.CountFiltered()
		} else {
			n, err = st.Count()
		}
		if err != nil {
			die("count failed: %v", err)
		}
		if jsonOut {
			render.AsJSON(os.Stdout, map[string]int64{"count": n})
		} else if countFiltered {
			fmt.Fprintf(os.Stdout, "%d filtered jobs\n", n)
		} else {
			fmt.Fprintf(os.Stdout, "%d jobs\n", n)
		}
		return nil
	},
}

func init() {
	countCmd.Flags().BoolVar(&countFiltered, "filtered", false, "count only jobs tagged status=filtered")
	rootCmd.AddCommand(countCmd)
}
