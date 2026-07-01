package cmd

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/store"
)

var (
	exportFormat string
	exportOut    string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export saved jobs to JSON, CSV, or Markdown",
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		jobs, err := st.List(store.Filters{Limit: 100000})
		if err != nil {
			die("query failed: %v", err)
		}
		w, closer, err := sink(exportOut)
		if err != nil {
			die("%v", err)
		}
		defer closer()

		switch strings.ToLower(exportFormat) {
		case "json":
			b, _ := json.MarshalIndent(jobs, "", "  ")
			w.Write(append(b, '\n'))
		case "csv":
			cw := csv.NewWriter(w)
			cw.Write([]string{"id", "title", "company", "location", "salary_high", "currency", "remote_type", "status", "fit_score", "seniority", "tech_stack", "industry", "company_size_band", "fit_reason", "source", "url"})
			for _, j := range jobs {
				sal := ""
				if j.SalaryHigh != nil {
					sal = fmt.Sprintf("%.0f", *j.SalaryHigh)
				}
				score := ""
				if j.FitScore != nil {
					score = fmt.Sprintf("%d", *j.FitScore)
				}
				cw.Write([]string{j.ID, j.Title, j.Company, j.Location, sal, j.SalaryCurrency, j.RemoteType, j.Status, score, j.Seniority, j.TechStack, j.Industry, j.CompanySizeBand, j.FitReason, j.Source, j.URL})
			}
			cw.Flush()
		case "markdown", "md":
			fmt.Fprintln(w, "# LinkedIn Jobs Export")
			fmt.Fprintln(w)
			fmt.Fprintln(w, "| Score | Title | Company | Location | Salary | Status | URL |")
			fmt.Fprintln(w, "|-------|-------|---------|----------|--------|--------|-----|")
			for _, j := range jobs {
				score := "-"
				if j.FitScore != nil {
					score = fmt.Sprintf("%d", *j.FitScore)
				}
				fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s |\n",
					score, j.Title, j.Company, j.Location, j.SalaryDisplay(), j.Status, j.URL)
			}
		default:
			die("unknown format %q (use json|csv|markdown)", exportFormat)
		}
		fmt.Fprintf(os.Stderr, "Exported %d jobs (%s).\n", len(jobs), exportFormat)
		return nil
	},
}

func init() {
	exportCmd.Flags().StringVarP(&exportFormat, "format", "f", "json", "output format: json|csv|markdown")
	exportCmd.Flags().StringVarP(&exportOut, "out", "o", "", "output file (default: stdout)")
	rootCmd.AddCommand(exportCmd)
}

func sink(path string) (writeFlusher, func(), error) {
	if path == "" || path == "-" {
		return os.Stdout, func() {}, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { f.Close() }, nil
}

type writeFlusher interface {
	Write([]byte) (int, error)
}
