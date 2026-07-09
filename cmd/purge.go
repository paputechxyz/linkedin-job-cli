package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	purgeYes      bool
	purgeFiltered bool
)

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Delete jobs from the local database",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadCfg()
		scope := "ALL"
		if purgeFiltered {
			scope = "FILTERED"
		}
		if !purgeYes {
			fmt.Fprintf(os.Stderr, "Delete %s jobs from %s? [y/N] ", scope, cfg.DBPath)
			var resp string
			fmt.Fscanln(os.Stdin, &resp)
			if resp != "y" && resp != "Y" && resp != "yes" {
				fmt.Fprintln(os.Stderr, "Aborted.")
				return nil
			}
		}
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		var n int64
		if purgeFiltered {
			n, err = st.DeleteFiltered()
		} else {
			n, err = st.DeleteAll()
		}
		if err != nil {
			die("delete failed: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Deleted %d jobs.\n", n)
		return nil
	},
}

func init() {
	purgeCmd.Flags().BoolVar(&purgeYes, "yes", false, "skip the confirmation prompt")
	purgeCmd.Flags().BoolVar(&purgeFiltered, "filtered", false, "delete only jobs tagged status=filtered")
	rootCmd.AddCommand(purgeCmd)
}
