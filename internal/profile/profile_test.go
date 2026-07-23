package profile

import (
	"testing"

	"linkedin-jobs/internal/config"
)

func ptr(f float64) *float64 { return &f }

// TestLoad_NoKnobs verifies the profile is empty when no knobs are passed. Load
// never returns nil now — knobs come from the settings argument, so emptiness
// is expressed via IsEmpty.
func TestLoad_NoKnobs(t *testing.T) {
	got, err := Load(config.ProfileSettings{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !IsEmpty(got) {
		t.Errorf("want empty profile, got %+v", got)
	}
}

// TestLoad_KnobsFromSettings verifies the structured knobs map straight through
// from the settings argument into the profile.
func TestLoad_KnobsFromSettings(t *testing.T) {
	prefs := config.ProfileSettings{
		WorkArrangement:   []string{"remote", "hybrid"},
		MinSalary:         ptr(200000),
		MinSalaryCurrency: "CAD",
		Location:          "Toronto, ON, Canada",
		PreferredTech:     []string{"Go", "Python"},
		AvoidedTech:       []string{"C#", ".NET"},
	}
	got, err := Load(prefs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.PrefWorkArrangement) != 2 || got.PrefWorkArrangement[0] != "remote" || got.PrefWorkArrangement[1] != "hybrid" {
		t.Errorf("work = %+v", got.PrefWorkArrangement)
	}
	if got.PrefMinSalary == nil || *got.PrefMinSalary != 200000 {
		t.Errorf("min_salary = %+v", got.PrefMinSalary)
	}
	if got.PrefMinSalaryCurrency != "CAD" {
		t.Errorf("min_salary_currency = %q", got.PrefMinSalaryCurrency)
	}
	if got.PrefLocation != "Toronto, ON, Canada" {
		t.Errorf("location = %q want Toronto, ON, Canada", got.PrefLocation)
	}
	if len(got.PrefPreferredTech) != 2 || got.PrefPreferredTech[0] != "Go" {
		t.Errorf("preferred_tech = %+v", got.PrefPreferredTech)
	}
	if len(got.PrefAvoidedTech) != 2 || got.PrefAvoidedTech[0] != "C#" || got.PrefAvoidedTech[1] != ".NET" {
		t.Errorf("avoided_tech = %+v", got.PrefAvoidedTech)
	}
	if IsEmpty(got) {
		t.Errorf("profile with knobs must not be empty")
	}
}

func TestIsEmpty(t *testing.T) {
	if !IsEmpty(nil) {
		t.Error("nil profile must be empty")
	}
	got, err := Load(config.ProfileSettings{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !IsEmpty(got) {
		t.Errorf("zero-value profile must be empty, got %+v", got)
	}
}

// TestKnobCount_CountsAllFiveKnobs is the regression for the mismatch where
// `recommended` reported "loaded 4 preference knob(s)" while `profile show`
// listed 5 — location was not counted. KnobCount must count all five knobs,
// and a location-only profile must register as non-empty (1 knob).
func TestKnobCount_CountsAllFiveKnobs(t *testing.T) {
	full, _ := Load(config.ProfileSettings{
		WorkArrangement: []string{"remote", "hybrid"},
		MinSalary:       ptr(200000),
		Location:        "Toronto, ON, Canada",
		PreferredTech:   []string{"Python", "TypeScript"},
		AvoidedTech:     []string{"C#"},
	})
	if n := full.KnobCount(); n != 5 {
		t.Errorf("full profile KnobCount=%d want 5 (incl. location)", n)
	}

	// Location-only: previously miscounted as empty/0 because IsEmpty ignored
	// location. It is a real knob (fed to the LLM enrich prompt), so it counts.
	locOnly, _ := Load(config.ProfileSettings{Location: "Toronto, ON, Canada"})
	if n := locOnly.KnobCount(); n != 1 {
		t.Errorf("location-only KnobCount=%d want 1", n)
	}
	if IsEmpty(locOnly) {
		t.Errorf("location-only profile must not be empty")
	}
}
