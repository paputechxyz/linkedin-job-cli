package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var purgeYes bool

var purgeCmd = &cobra.Command{
	Use:   "purge",
	Short: "Delete all jobs from the local database",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadCfg()
		if !purgeYes {
			fmt.Fprintf(os.Stderr, "Delete ALL jobs from %s? [y/N] ", cfg.DBPath)
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
		n, err := st.DeleteAll()
		if err != nil {
			die("delete failed: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Deleted %d jobs.\n", n)
		return nil
	},
}

func init() {
	purgeCmd.Flags().BoolVar(&purgeYes, "yes", false, "skip the confirmation prompt")
	rootCmd.AddCommand(purgeCmd)
}
