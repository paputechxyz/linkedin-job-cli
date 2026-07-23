// Package profile reads the structured preference knobs from settings.yaml to
// build the in-memory models.Profile consumed by the system rubrics (salary,
// work arrangement) and the LLM enrich prompt.
//
// The preference knobs (work_arrangement, location, min_salary, preferred_tech,
// avoided_tech) live under the profile: section of settings.yaml
// ($LJ_SETTINGS_FILE) and feed the system rubrics in internal/score plus the
// LLM enrich prompt (location drives salary-band pick + currency inference).
// The location_proximity *rubric* is LLM-rated from the rubric description and
// is separate from the location preference knob.
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
		PrefLocation:          prefs.Location,
		PrefPreferredTech:     prefs.PreferredTech,
		PrefAvoidedTech:       prefs.AvoidedTech,
		UpdatedAt:             nowISO(),
	}, nil
}

// IsEmpty reports whether the profile has no preference knobs — i.e.
// scoring/filtering will run context-free. Delegates to Profile.KnobCount so
// the "empty" decision and the status-line tally share one source of truth.
func IsEmpty(p *models.Profile) bool {
	return p == nil || p.KnobCount() == 0
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
