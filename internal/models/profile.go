package models

import "strings"

// Profile is the in-memory candidate context assembled at load time from the
// structured preference knobs in the settings.yaml profile: section. The Pref*
// fields drive the system rubrics (work arrangement, salary floor) and feed the
// LLM enrich prompt (preferred_tech, avoided_tech). Dynamic rubrics, including
// location, are LLM-rated from the rubric description and do not use these knobs.
type Profile struct {
	PrefWorkArrangement   []string `json:"pref_work_arrangement,omitempty"` // remote|hybrid|onsite; any subset
	PrefMinSalary         *float64 `json:"pref_min_salary,omitempty"`
	PrefMinSalaryCurrency string   `json:"pref_min_salary_currency,omitempty"` // ISO 4217 (e.g. CAD); "" = raw numeric compare
	PrefPreferredTech     []string `json:"pref_preferred_tech,omitempty"`      // surfaced as a dynamic rubric via setup
	PrefAvoidedTech       []string `json:"pref_avoided_tech,omitempty"`        // surfaced as a dynamic rubric via setup
	UpdatedAt             string   `json:"updated_at,omitempty"`
}

// HasWorkArrangementPreference reports whether the user has expressed a genuine
// work arrangement preference. Returns false when the list is empty or contains
// all three canonical arrangements (remote, hybrid, onsite) — both cases mean
// "no preference" and should not affect scoring or filtering.
func (p *Profile) HasWorkArrangementPreference() bool {
	normalized := normalizedArrangementSet(p.PrefWorkArrangement)
	if len(normalized) == 0 {
		return false
	}
	_, hasRemote := normalized["remote"]
	_, hasHybrid := normalized["hybrid"]
	_, hasOnsite := normalized["onsite"]
	return !(hasRemote && hasHybrid && hasOnsite)
}

// PrefersArrangement reports whether the given canonical arrangement token
// ("remote", "hybrid", "onsite") is in the profile's preferred set. Uses the
// same normalization as HasWorkArrangementPreference. Returns false for empty
// or unknown tokens.
func (p *Profile) PrefersArrangement(arrangement string) bool {
	if arrangement == "" {
		return false
	}
	normalized := normalizedArrangementSet(p.PrefWorkArrangement)
	return normalized[normalizeArrangement(arrangement)]
}

// normalizeArrangement converts a work arrangement token to its canonical
// lowercase form: "on-site" and "on site" become "onsite".
func normalizeArrangement(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "on-site", "on site":
		return "onsite"
	}
	return s
}

// normalizedArrangementSet builds a set of canonical arrangement tokens from
// a raw preference list.
func normalizedArrangementSet(arrangements []string) map[string]bool {
	set := make(map[string]bool)
	for _, a := range arrangements {
		n := normalizeArrangement(a)
		if n != "" {
			set[n] = true
		}
	}
	return set
}
