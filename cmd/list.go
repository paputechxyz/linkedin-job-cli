package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"linkedin-jobs/internal/render"
	"linkedin-jobs/internal/store"
)

var (
	listMinSalary       string
	listCompany         string
	listTitle           string
	listLocation        string
	listRemote          bool
	listStatus          string
	listSource          string
	listLimit           int
	listIncludeFiltered bool
	listMinScore        int
	listSortScore       bool
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved jobs from the local database",
	RunE: func(cmd *cobra.Command, args []string) error {
		st, err := openStore()
		if err != nil {
			die("failed to open DB: %v", err)
		}
		defer st.Close()
		jobs, err := st.List(store.Filters{
			MinSalary:       parseMinSalary(listMinSalary),
			Company:         listCompany,
			Title:           listTitle,
			Location:        listLocation,
			Remote:          listRemote,
			Status:          listStatus,
			Source:          listSource,
			MinScore:        listMinScore,
			IncludeFiltered: listIncludeFiltered,
			SortByScore:     listSortScore,
			Limit:           listLimit,
		})
		if err != nil {
			die("query failed: %v", err)
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
	listCmd.Flags().StringVar(&listMinSalary, "min-salary", "", "filter by minimum salary (e.g. 200k)")
	listCmd.Flags().StringVar(&listCompany, "company", "", "filter by company name (substring)")
	listCmd.Flags().StringVar(&listTitle, "title", "", "filter by title (substring)")
	listCmd.Flags().StringVar(&listLocation, "location", "", "filter by location (substring)")
	listCmd.Flags().BoolVar(&listRemote, "remote", false, "only remote-friendly jobs")
	listCmd.Flags().StringVar(&listStatus, "status", "", "filter by status (new/saved/applied/rejected/filtered)")
	listCmd.Flags().StringVar(&listSource, "source", "", "filter by source (recommended/search)")
	listCmd.Flags().IntVar(&listLimit, "limit", 50, "max results")
	listCmd.Flags().BoolVar(&listIncludeFiltered, "include-filtered", false, "include jobs tagged filtered (hidden by default)")
	listCmd.Flags().IntVar(&listMinScore, "min-score", 0, "only jobs with fit_score >= N")
	listCmd.Flags().BoolVar(&listSortScore, "sort-score", false, "sort by fit_score descending (default: salary)")
	rootCmd.AddCommand(listCmd)
}
