package render

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/term"

	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/score"
	"linkedin-jobs/internal/store"
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

// Table writes jobs as a column-aligned table. The title cell is wrapped in an
// OSC 8 terminal hyperlink (clickable in iTerm2/Terminal.app/kitty/Windows
// Terminal/etc.) when stdout is a TTY; when piped/redirected the raw title text
// is emitted so logs and agent-captured output stay clean (the trailing "Links:"
// list still carries the bare URLs there).
func Table(w io.Writer, jobs []*models.JobPosting) {
	if len(jobs) == 0 {
		fmt.Fprintln(w, "No jobs found.")
		return
	}
	cols := []string{"#", "Score", "Title", "Company", "Location", "Work", "Salary", "Source"}
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = visibleLen(c)
	}
	rows := make([][]string, len(jobs))
	for i, j := range jobs {
		row := []string{
			fmt.Sprintf("%d", i+1),
			scoreCell(j),
			titleCell(j),
			trunc(orDash(j.Company), 24),
			trunc(orDash(j.Location), 20),
			workCell(j),
			trunc(j.SalaryDisplay(), 26),
			orDash(j.Source),
		}
		rows[i] = row
		for c, cell := range row {
			if vw := visibleLen(cell); vw > widths[c] {
				widths[c] = vw
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

	// Links: print each job's URL keyed by row number so terminals auto-link
	// them as clickable hyperlinks. Kept separate from the table so the full
	// (long) URL is shown verbatim instead of distorting the column layout.
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Links:")
	for i, j := range jobs {
		fmt.Fprintf(w, "  %d. %s\n", i+1, orNA(j.URL))
	}
}

func scoreCell(j *models.JobPosting) string {
	if j.FitScore == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *j.FitScore)
}

// writeCell writes one left-aligned cell padded to `width` visible columns,
// then a two-space gutter. Padding is based on display width (visibleLen), not
// byte length, so cells containing OSC 8 hyperlink escapes still align. The
// last column is written without padding/gutter.
func writeCell(w io.Writer, s string, width int, last bool) {
	if last {
		fmt.Fprint(w, s)
		return
	}
	fmt.Fprint(w, s)
	if pad := width - visibleLen(s); pad > 0 {
		fmt.Fprint(w, strings.Repeat(" ", pad))
	}
	fmt.Fprint(w, "  ")
}

// titleCell returns the (possibly truncated) job title, wrapped in an OSC 8
// hyperlink to the job's URL when stdout is a TTY.
func titleCell(j *models.JobPosting) string {
	return hyperlink(j.URL, trunc(j.Title, 38))
}

// workCell renders the work arrangement (Remote/Hybrid/Onsite) for the table.
func workCell(j *models.JobPosting) string {
	if j.RemoteType == "" {
		return "—"
	}
	return strings.Title(j.RemoteType)
}

// hyperlink wraps text in an OSC 8 terminal hyperlink pointing at url. When
// stdout is not a terminal (piped, redirected, agent-captured) the plain text
// is returned so output stays clean.
func hyperlink(url, text string) string {
	if url == "" || !stdoutIsTerminal() {
		return text
	}
	// OSC 8: ESC ] 8 ; ; <url> BEL <text> ESC ] 8 ; ; BEL
	return "\x1b]8;;" + url + "\x07" + text + "\x1b]8;;\x07"
}

// stdoutIsTerminal reports whether os.Stdout is an interactive terminal.
func stdoutIsTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// osc8RE matches OSC 8 hyperlink escape sequences (open and close, BEL- or
// ST-terminated) so visibleLen can return the on-screen column width of a cell
// that contains a clickable link.
var osc8RE = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)`)

// visibleLen returns the on-screen column width of s: the byte length with all
// ANSI/OSC escape sequences stripped.
func visibleLen(s string) int {
	return len(osc8RE.ReplaceAllString(s, ""))
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
	if j.FitScore != nil {
		fmt.Fprintf(w, "Fit score:  %d/100\n", *j.FitScore)
		renderRubricBreakdown(w, j)
	}
	if hasEnrichment(j) {
		fmt.Fprintln(w, "\nStructured:")
		if j.CompanyOverview != "" {
			fmt.Fprintf(w, "  Company:     %s\n", j.CompanyOverview)
		}
		if j.Industry != "" {
			fmt.Fprintf(w, "  Industry:    %s\n", j.Industry)
		}
		if j.TechStack != "" {
			fmt.Fprintf(w, "  Tech stack:  %s\n", j.TechStack)
		}
		if j.Seniority != "" {
			fmt.Fprintf(w, "  Seniority:   %s\n", j.Seniority)
		}
		if j.EmploymentType != "" {
			fmt.Fprintf(w, "  Type:        %s\n", j.EmploymentType)
		}
		if j.YearsExperience != nil {
			fmt.Fprintf(w, "  Years exp:   %d+\n", *j.YearsExperience)
		}
		if j.CompanySizeBand != "" {
			fmt.Fprintf(w, "  Co size:     %s\n", j.CompanySizeBand)
		}
		if j.CompanyStage != "" {
			fmt.Fprintf(w, "  Co stage:    %s\n", j.CompanyStage)
		}
		if j.IsFoundingRole {
			fmt.Fprintln(w, "  Founding:    yes")
		}
		if j.VisaSponsorship != "" {
			fmt.Fprintf(w, "  Visa:        %s\n", j.VisaSponsorship)
		}
		fmt.Fprintln(w)
	}
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

func hasEnrichment(j *models.JobPosting) bool {
	return j.CompanyOverview != "" || j.Industry != "" || j.TechStack != "" ||
		j.Seniority != "" || j.EmploymentType != "" || j.CompanySizeBand != "" ||
		j.CompanyStage != "" || j.VisaSponsorship != ""
}

// renderRubricBreakdown writes the per-rubric scoring breakdown as bullet
// points with star bars, e.g.:
//
//	Rubrics:
//	  • salary             ★★★☆☆ (3/5, w5) no floor/salary
//	  • work_arrangement   ★★★★★ (5/5, w5) hybrid
//
// Falls back to the flat FitReason string when RubricScores is absent or
// unparseable (e.g. legacy jobs scored before the column existed).
func renderRubricBreakdown(w io.Writer, j *models.JobPosting) {
	if j.RubricScores == "" {
		if j.FitReason != "" {
			fmt.Fprintf(w, "Fit reason: %s\n", j.FitReason)
		}
		return
	}
	var rs []score.RubricScore
	if err := json.Unmarshal([]byte(j.RubricScores), &rs); err != nil || len(rs) == 0 {
		if j.FitReason != "" {
			fmt.Fprintf(w, "Fit reason: %s\n", j.FitReason)
		}
		return
	}
	idW := 0
	for _, r := range rs {
		if len(r.ID) > idW {
			idW = len(r.ID)
		}
	}
	fmt.Fprintln(w, "Rubrics:")
	for _, r := range rs {
		line := fmt.Sprintf("  • %-*s  %s (%d/5, w%d)", idW, r.ID, StarsFor(r.Rating), r.Rating, r.Weight)
		if r.Reason != "" {
			line += " " + r.Reason
		}
		fmt.Fprintln(w, line)
	}
}

// StarsFor renders a 5-star bar: filled stars for the rating, empty for the
// remainder. rating is clamped to [0,5]. Shared between the CLI renderer and
// the web UI so both show the skill.md star format identically.
func StarsFor(rating int) string {
	if rating < 0 {
		rating = 0
	}
	if rating > 5 {
		rating = 5
	}
	return strings.Repeat("★", rating) + strings.Repeat("☆", 5-rating)
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
