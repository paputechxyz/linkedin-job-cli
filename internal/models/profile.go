package models

// Profile is the single-row user profile: resume + preferences. The free-text
// fields feed LLM fit scoring; the structured preference fields drive the
// deterministic hard filter (work arrangement, salary floor, locations) and
// the rubric scorer (preferred_tech).
type Profile struct {
	ResumeText          string   `json:"resume_text,omitempty"`
	PreferencesText     string   `json:"preferences_text,omitempty"`
	PrefWorkArrangement    string   `json:"pref_work_arrangement,omitempty"`        // remote|hybrid|onsite|""
	PrefMinSalary          *float64 `json:"pref_min_salary,omitempty"`
	PrefMinSalaryCurrency  string   `json:"pref_min_salary_currency,omitempty"`     // ISO 4217 (e.g. CAD); "" = raw numeric compare
	PrefLocations          string   `json:"pref_locations,omitempty"`               // comma-separated
	PrefPreferredTech      []string `json:"pref_preferred_tech,omitempty"`          // drives tech_overlap dimension
	UpdatedAt           string   `json:"updated_at,omitempty"`
}
