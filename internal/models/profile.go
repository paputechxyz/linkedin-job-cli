package models

// Profile is the in-memory candidate context assembled at load time from the
// structured preference knobs in the settings.yaml profile: section. The Pref*
// fields drive the deterministic hard filter (work arrangement, salary floor,
// locations) and the rubric scorer (preferred_tech, avoided_tech). Fit scoring
// relies entirely on these knobs.
type Profile struct {
	PrefWorkArrangement   []string `json:"pref_work_arrangement,omitempty"`  // remote|hybrid|onsite; any subset
	PrefMinSalary         *float64 `json:"pref_min_salary,omitempty"`
	PrefMinSalaryCurrency string   `json:"pref_min_salary_currency,omitempty"` // ISO 4217 (e.g. CAD); "" = raw numeric compare
	PrefLocations         []string `json:"pref_locations,omitempty"`           // preferred location tokens
	PrefPreferredTech     []string `json:"pref_preferred_tech,omitempty"`      // drives tech_overlap dimension
	PrefAvoidedTech       []string `json:"pref_avoided_tech,omitempty"`        // drives deal-breaker cap alongside scoring.deal_breakers
	UpdatedAt             string   `json:"updated_at,omitempty"`
}
