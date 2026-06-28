package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"linkedin-job-cli/internal/render"
)

var (
	queryLimit   int
	queryExclude []string
)

var queryCmd = &cobra.Command{
	Use:   "query <text>",
	Short: "Offline full-text search over stored jobs (FTS5)",
	Args:  cobra.MinimumNArgs(1),
	Long: `Runs an instant SQLite FTS5 full-text query over the title, company, and
description of every stored job — no network. Pass search terms as positional
arguments (quoted if multi-word). Use --exclude to drop unwanted terms.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		expr := ftsExpr(args, queryExclude)
		jobs, err := st.SearchFTS(expr, queryLimit)
		if err != nil {
			die("search failed: %v\n(Hint: wrap multi-word phrases in quotes, e.g. query \"staff engineer\")", err)
		}
		if jsonOut {
			render.AsJSON(os.Stdout, jobs)
		} else {
			render.Table(os.Stdout, jobs)
		}
		return nil
	},
}

func init() {
	queryCmd.Flags().IntVar(&queryLimit, "limit", 50, "max results")
	queryCmd.Flags().StringSliceVar(&queryExclude, "exclude", nil, "exclude terms (repeatable)")
	rootCmd.AddCommand(queryCmd)
}

// ftsExpr builds an FTS5 MATCH expression from positive terms and excluded terms.
func ftsExpr(include, exclude []string) string {
	out := ""
	for _, a := range include {
		piece := "\"" + a + "\""
		if out != "" {
			out += " "
		}
		out += piece
	}
	for _, ex := range exclude {
		if ex == "" {
			continue
		}
		if out != "" {
			out += " "
		}
		out += "NOT \"" + ex + "\""
	}
	return out
}
