package cmd

import (
	"bufio"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Wipe all rubrics and restart setup from scratch",
	Long: `Forgets every rubric (the system defaults — salary, work arrangement,
location — are re-added automatically) and restarts the setup paragraph flow
so you can regenerate your rubric set from a fresh preferences paragraph.
Structured profile values (salary floor, locations) are preserved unless the
new paragraph overrides them.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		stdin := bufio.NewReader(os.Stdin)
		if !confirm(stdin, "This wipes ALL rubrics and restarts setup. Continue?") {
			fmt.Println("Aborted — rubrics unchanged.")
			return nil
		}
		if err := config.SaveRubrics(config.DefaultScoringSettings().Rubrics); err != nil {
			return fmt.Errorf("reset rubrics: %w", err)
		}
		fmt.Println("Rubrics wiped to system defaults. Restarting setup…")
		fmt.Println()
		return runSetup(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(resetCmd)
}
