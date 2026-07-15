package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
)

var amendCmd = &cobra.Command{
	Use:   "amend",
	Short: "Add or change a few rubrics from a follow-up paragraph",
	Long: `Takes a follow-up paragraph describing rubrics to add or change.

Only the rubrics you mention are created or updated — every other rubric and
its weight is preserved untouched. Use this to fine-tune your rubric set
without re-running the full setup. After saving, run 'rescore-all' to re-score
existing jobs against the new rubric set.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		settings, _ := config.LoadSettings()
		existing := settings.Scoring.Rubrics
		if len(existing) == 0 {
			existing = config.DefaultScoringSettings().Rubrics
		}

		stdin := bufio.NewReader(os.Stdin)
		fmt.Println("Describe the rubrics to add or change (end with a blank line).")
		fmt.Println("Only named rubrics are touched; everything else is preserved.")
		paragraph := promptParagraph(stdin)
		if strings.TrimSpace(paragraph) == "" {
			fmt.Println("Nothing entered — no changes.")
			return nil
		}

		cfg := loadCfg()
		provider, err := llm.Resolve(cfg)
		if err != nil {
			return fmt.Errorf("LLM provider required for amend: %w", err)
		}
		fmt.Println("Proposing changes…")
		changes, err := llm.GenerateAmend(existing, paragraph, provider)
		if err != nil {
			return fmt.Errorf("amend rubrics: %w", err)
		}

		rubricChanges := make([]config.Rubric, 0, len(changes))
		for _, c := range changes {
			rubricChanges = append(rubricChanges, config.Rubric{
				ID: c.ID, Weight: c.Weight, Description: c.Description, Items: c.Items,
			})
		}
		merged := config.MergeRubrics(existing, rubricChanges)

		fmt.Println("\nUpdated rubric set:")
		printRubrics(merged)
		if !confirm(stdin, "Save these changes?") {
			fmt.Println("Aborted — nothing saved.")
			return nil
		}
		if err := config.SaveRubrics(merged); err != nil {
			return fmt.Errorf("save rubrics: %w", err)
		}
		fmt.Println("\nSaved. Run 'linkedin-jobs rescore-all' to re-score existing jobs.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(amendCmd)
}
