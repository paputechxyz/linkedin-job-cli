package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/profile"
)

var amendCmd = &cobra.Command{
	Use:   "amend",
	Short: "Add or change rubrics and profile fields from a follow-up paragraph",
	Long: `Takes a follow-up paragraph describing rubrics and profile preferences to add or change.

Only the rubrics and profile fields you mention are created or updated —
everything else is preserved untouched. Structured profile fields the paragraph
names (location, salary, currency, work arrangement, preferred/avoided tech)
replace the old values; rubric changes are merged onto the existing set. Use
this to fine-tune your settings without re-running the full setup. After
saving, run 'rescore-all' to re-score existing jobs against the new settings.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		settings, _ := config.LoadSettings()
		existing := settings.Scoring.Rubrics
		if len(existing) == 0 {
			existing = config.DefaultScoringSettings().Rubrics
		}
		prof := settings.Profile

		stdin := bufio.NewReader(os.Stdin)
		fmt.Println("Describe the rubrics or profile fields to add or change (end with a blank line).")
		fmt.Println("Only named rubrics and fields are touched; everything else is preserved.")
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
		result, err := llm.GenerateAmend(existing, prof, paragraph, provider)
		if err != nil {
			return fmt.Errorf("amend: %w", err)
		}

		rubricChanges := make([]config.Rubric, 0, len(result.Rubrics))
		for _, c := range result.Rubrics {
			rubricChanges = append(rubricChanges, config.Rubric{
				ID: c.ID, Weight: c.Weight, Description: c.Description, Items: c.Items, AppliesTo: c.AppliesTo,
			})
		}
		merged := config.MergeRubrics(existing, rubricChanges)

		// Structured profile fields use REPLACE semantics — if the paragraph
		// names a value, it overwrites what was there. Silent fields keep the
		// existing value. Currency precedence mirrors setup.go: explicit LLM
		// value > inferred from location > existing > USD default.
		profileChanged := false
		if len(result.WorkArrangement) > 0 {
			prof.WorkArrangement = result.WorkArrangement
			profileChanged = true
		}
		if strings.TrimSpace(result.Location) != "" {
			prof.Location = strings.TrimSpace(result.Location)
			profileChanged = true
		}
		if result.MinSalaryCurrency != "" {
			prof.MinSalaryCurrency = result.MinSalaryCurrency
			profileChanged = true
		} else if strings.TrimSpace(result.Location) != "" {
			if inferred := profile.InferCurrencyFromLocation(prof.Location); inferred != "" {
				prof.MinSalaryCurrency = inferred
				profileChanged = true
			}
		}
		if result.MinSalary != nil {
			prof.MinSalary = result.MinSalary
			profileChanged = true
		}
		if len(result.PreferredTech) > 0 {
			prof.PreferredTech = result.PreferredTech
			profileChanged = true
		}
		if len(result.AvoidedTech) > 0 {
			prof.AvoidedTech = result.AvoidedTech
			profileChanged = true
		}

		fmt.Println("\nUpdated rubric set:")
		printRubrics(merged)
		if profileChanged {
			fmt.Println("\nUpdated profile fields:")
			fmt.Printf("    work arrangement: %s\n", orNoneSlice(prof.WorkArrangement))
			fmt.Printf("    location:         %s\n", orEmptyNA(prof.Location))
			fmt.Printf("    min salary:       %s\n", formatSalaryFloor(prof.MinSalary, prof.MinSalaryCurrency))
			fmt.Printf("    preferred tech:   %s\n", orNoneSlice(prof.PreferredTech))
			fmt.Printf("    avoided tech:     %s\n", orNoneSlice(prof.AvoidedTech))
		}
		if !confirm(stdin, "Save these changes?") {
			fmt.Println("Aborted — nothing saved.")
			return nil
		}
		if profileChanged {
			if err := config.SaveProfile(prof); err != nil {
				return fmt.Errorf("save profile: %w", err)
			}
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
