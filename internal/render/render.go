package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"linkedin-job-cli/internal/models"
	"linkedin-job-cli/internal/store"
)

// AsJSON writes any value as compact-but-readable JSON.
func AsJSON(w io.Writer, v interface{}) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// Table writes jobs as a column-aligned table.
func Table(w io.Writer, jobs []*models.JobPosting) {
	if len(jobs) == 0 {
		fmt.Fprintln(w, "No jobs found.")
		return
	}
	cols := []string{"#", "Title", "Company", "Location", "Salary", "Source"}
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = len(c)
	}
	rows := make([][]string, len(jobs))
	for i, j := range jobs {
		row := []string{
			fmt.Sprintf("%d", i+1),
			trunc(j.Title, 42),
			trunc(orDash(j.Company), 26),
			trunc(orDash(j.Location), 22),
			trunc(j.SalaryDisplay(), 18),
			orDash(j.Source),
		}
		rows[i] = row
		for c, cell := range row {
			if len(cell) > widths[c] {
				widths[c] = len(cell)
			}
		}
	}
	// header
	for c, h := range cols {
		writeCell(w, h, widths[c], c == len(cols)-1)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, strings.Repeat("-", lineW(widths)))
	for _, row := range rows {
		for c, cell := range row {
			writeCell(w, cell, widths[c], c == len(row)-1)
		}
		fmt.Fprintln(w)
	}
}

func writeCell(w io.Writer, s string, width int, last bool) {
	if last {
		fmt.Fprint(w, s)
		return
	}
	fmt.Fprintf(w, "%-*s  ", width, s)
}

func lineW(widths []int) int {
	total := 0
	for _, x := range widths {
		total += x + 2
	}
	return total
}

// Detail writes a full detail panel for one job.
func Detail(w io.Writer, j *models.JobPosting) {
	fmt.Fprintf(w, "\n%s", j.Title)
	if j.Company != "" {
		fmt.Fprintf(w, " @ %s", j.Company)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Location:   %s\n", orNA(j.Location))
	fmt.Fprintf(w, "Salary:     %s\n", j.SalaryDisplay())
	fmt.Fprintf(w, "Remote:     %s\n", orNA(strings.Title(j.RemoteType)))
	fmt.Fprintf(w, "Source:     %s\n", orNA(j.Source))
	fmt.Fprintf(w, "Status:     %s\n", orNA(j.Status))
	if j.ListedAt > 0 {
		fmt.Fprintf(w, "Listed:     %s\n", time.UnixMilli(j.ListedAt).Format("2006-01-02"))
	}
	fmt.Fprintf(w, "ID:         %s\n", j.ID)
	fmt.Fprintf(w, "URL:        %s\n", j.URL)
	if j.Notes != "" {
		fmt.Fprintf(w, "Notes:      %s\n", j.Notes)
	}
	fmt.Fprintln(w)
	if j.LLMSummary != "" {
		fmt.Fprintln(w, "Summary:")
		fmt.Fprintln(w, j.LLMSummary)
	} else if j.Description != "" {
		desc := j.Description
		if len(desc) > 800 {
			desc = desc[:800] + "..."
		}
		fmt.Fprintln(w, "Description (excerpt):")
		fmt.Fprintln(w, desc)
	}
}

// Stats writes aggregate stats.
func Stats(w io.Writer, s *store.Stats) {
	fmt.Fprintf(w, "Total jobs:        %d\n", s.Total)
	fmt.Fprintf(w, "With salary:       %d\n", s.WithSalary)
	fmt.Fprintf(w, "Remote:            %d\n", s.RemoteCount)
	fmt.Fprintln(w, "\nBy status:")
	for k, v := range s.ByStatus {
		fmt.Fprintf(w, "  %-12s %d\n", k, v)
	}
	if len(s.BySource) > 0 {
		fmt.Fprintln(w, "\nBy source:")
		for k, v := range s.BySource {
			fmt.Fprintf(w, "  %-12s %d\n", k, v)
		}
	}
	if len(s.ByCompany) > 0 {
		fmt.Fprintln(w, "\nTop companies:")
		for _, c := range s.ByCompany {
			fmt.Fprintf(w, "  %-30s %d\n", trunc(c.Label, 30), c.Count)
		}
	}
	if len(s.SalaryBands) > 0 {
		fmt.Fprintln(w, "\nSalary bands (by high end):")
		for b, c := range s.SalaryBands {
			fmt.Fprintf(w, "  $%-12s %d\n", b, c)
		}
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func orNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
