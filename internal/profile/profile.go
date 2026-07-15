// Package profile reads the structured preference knobs from settings.yaml to
// build the in-memory models.Profile consumed by the system rubrics (salary,
// work arrangement, location) and the LLM enrich prompt.
//
// The preference knobs (work_arrangement, min_salary, locations, preferred_tech,
// avoided_tech) live under the profile: section of settings.yaml
// ($LJ_SETTINGS_FILE) and feed the system rubrics in internal/score.
package profile

import (
	"time"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/models"
)

// Load builds the in-memory profile from the structured preference knobs in
// settings. A fully-empty profile (no knobs) means scoring/filtering will run
// context-free.
func Load(prefs config.ProfileSettings) (*models.Profile, error) {
	return &models.Profile{
		PrefWorkArrangement:   prefs.WorkArrangement,
		PrefMinSalary:         prefs.MinSalary,
		PrefMinSalaryCurrency: prefs.MinSalaryCurrency,
		PrefLocations:         prefs.Locations,
		PrefPreferredTech:     prefs.PreferredTech,
		PrefAvoidedTech:       prefs.AvoidedTech,
		UpdatedAt:             nowISO(),
	}, nil
}

// IsEmpty reports whether the profile has no preference knobs — i.e.
// scoring/filtering will run context-free.
func IsEmpty(p *models.Profile) bool {
	if p == nil {
		return true
	}
	return len(p.PrefWorkArrangement) == 0 &&
		p.PrefMinSalary == nil &&
		len(p.PrefLocations) == 0 &&
		len(p.PrefPreferredTech) == 0 &&
		len(p.PrefAvoidedTech) == 0
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
