package models

import "fmt"

// JobPosting mirrors the LinkedIn job record persisted in SQLite. Field names
// line up with the database columns (snake_case via explicit SQL).
type JobPosting struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Company        string   `json:"company,omitempty"`
	Location       string   `json:"location,omitempty"`
	URL            string   `json:"url"`
	SalaryRaw      string   `json:"salary_raw,omitempty"`
	SalaryLow      *float64 `json:"salary_low,omitempty"`
	SalaryHigh     *float64 `json:"salary_high,omitempty"`
	SalaryCurrency string   `json:"salary_currency,omitempty"`
	Description    string   `json:"description,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	LLMSummary     string   `json:"llm_summary,omitempty"`
	RemoteType     string   `json:"remote_type,omitempty"`
	Status         string   `json:"status,omitempty"`
	Notes          string   `json:"notes,omitempty"`
	Source         string   `json:"source,omitempty"`    // "recommended" | "search"
	ListedAt       int64    `json:"listed_at,omitempty"` // epoch ms
	SearchedAt     string   `json:"searched_at,omitempty"`
	FetchedAt      string   `json:"fetched_at,omitempty"`
}

// HasSalary reports whether any numeric salary was parsed.
func (j *JobPosting) HasSalary() bool {
	return j.SalaryHigh != nil
}

// SalaryMax returns the highest parsed salary figure, or 0 if none.
func (j *JobPosting) SalaryMax() float64 {
	if j.SalaryHigh != nil {
		return *j.SalaryHigh
	}
	if j.SalaryLow != nil {
		return *j.SalaryLow
	}
	return 0
}

// SalaryDisplay renders a human-readable salary string.
func (j *JobPosting) SalaryDisplay() string {
	cur := j.SalaryCurrency
	if cur == "" {
		cur = "USD"
	}
	if j.SalaryLow != nil && j.SalaryHigh != nil {
		return fmt.Sprintf("%s$%s – $%s", cur, comma(*j.SalaryLow), comma(*j.SalaryHigh))
	}
	if j.SalaryHigh != nil {
		return fmt.Sprintf("%s$%s", cur, comma(*j.SalaryHigh))
	}
	if j.SalaryLow != nil {
		return fmt.Sprintf("%s$%s", cur, comma(*j.SalaryLow))
	}
	if j.SalaryRaw != "" {
		return j.SalaryRaw
	}
	return "N/A"
}

func comma(f float64) string {
	n := int64(f)
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		s = s[1:]
	}
	out := ""
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += ","
		}
		out += string(c)
	}
	if n < 0 {
		out = "-" + out
	}
	return out
}
