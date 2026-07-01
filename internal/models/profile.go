package models

// Profile is the in-memory candidate context assembled at load time: the resume
// body (from RESUME.md) plus the structured preference knobs (from the
// settings.yaml profile: section). ResumeText feeds the LLM enrich call as
// candidate context; the structured Pref* fields drive the deterministic hard
// filter (work arrangement, salary floor, locations) and the rubric scorer
// (preferred_tech, avoided_tech). PreferencesText is vestigial (always empty
// since the JOB_PREFERENCE.md prose body was retired) and kept only for JSON
// compat.
type Profile struct {
	ResumeText          string   `json:"resume_text,omitempty"`
	PreferencesText     string   `json:"preferences_text,omitempty"`
	PrefWorkArrangement    []string `json:"pref_work_arrangement,omitempty"`     // remote|hybrid|onsite; any subset
	PrefMinSalary          *float64 `json:"pref_min_salary,omitempty"`
	PrefMinSalaryCurrency  string   `json:"pref_min_salary_currency,omitempty"`  // ISO 4217 (e.g. CAD); "" = raw numeric compare
	PrefLocations          []string `json:"pref_locations,omitempty"`            // preferred location tokens
	PrefPreferredTech      []string `json:"pref_preferred_tech,omitempty"`       // drives tech_overlap dimension
	PrefAvoidedTech        []string `json:"pref_avoided_tech,omitempty"`         // drives deal-breaker cap alongside scoring.deal_breakers
	UpdatedAt           string   `json:"updated_at,omitempty"`
}
