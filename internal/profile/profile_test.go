package profile

import (
	"os"
	"path/filepath"
	"testing"

	"linkedin-jobs/internal/config"
)

// useTempResume isolates each test by pointing LJ_RESUME_FILE at a temp path so
// the real RESUME.md in the repo root is never read.
func useTempResume(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "RESUME.md")
	t.Setenv("LJ_RESUME_FILE", p)
	return p
}

func ptr(f float64) *float64 { return &f }

// TestLoad_NoResumeNoKnobs verifies the profile is empty when RESUME.md is
// absent and no knobs are passed. Load never returns nil now — knobs come from
// the settings argument, so emptiness is expressed via IsEmpty.
func TestLoad_NoResumeNoKnobs(t *testing.T) {
	useTempResume(t)
	got, err := Load(config.ProfileSettings{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !IsEmpty(got) {
		t.Errorf("want empty profile, got %+v", got)
	}
}

func TestSaveResume_RoundTrip(t *testing.T) {
	useTempResume(t)
	if err := SaveResume("  Go engineer, 10y exp  "); err != nil {
		t.Fatalf("SaveResume: %v", err)
	}
	p, err := Load(config.ProfileSettings{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.ResumeText != "Go engineer, 10y exp" {
		t.Errorf("resume = %q", p.ResumeText)
	}
}

// TestLoad_KnobsFromSettings verifies the structured knobs map straight through
// from the settings argument into the profile (the old front-matter path).
func TestLoad_KnobsFromSettings(t *testing.T) {
	useTempResume(t)
	prefs := config.ProfileSettings{
		WorkArrangement:   []string{"remote", "hybrid"},
		MinSalary:         ptr(200000),
		MinSalaryCurrency: "CAD",
		Locations:         []string{"Remote", "Toronto"},
		PreferredTech:     []string{"Go", "Python"},
		AvoidedTech:       []string{"C#", ".NET"},
	}
	got, err := Load(prefs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ResumeText != "" {
		t.Errorf("resume should be empty, got %q", got.ResumeText)
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
	if len(got.PrefLocations) != 2 || got.PrefLocations[0] != "Remote" || got.PrefLocations[1] != "Toronto" {
		t.Errorf("locations = %+v", got.PrefLocations)
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

// TestLoad_ResumePlusKnobs verifies both channels combine: RESUME.md supplies
// the free-text body while settings supplies the knobs.
func TestLoad_ResumePlusKnobs(t *testing.T) {
	useTempResume(t)
	if err := SaveResume("staff backend engineer"); err != nil {
		t.Fatalf("SaveResume: %v", err)
	}
	got, err := Load(config.ProfileSettings{WorkArrangement: []string{"hybrid"}, MinSalary: ptr(160000)})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ResumeText != "staff backend engineer" {
		t.Errorf("resume = %q", got.ResumeText)
	}
	if len(got.PrefWorkArrangement) != 1 || got.PrefWorkArrangement[0] != "hybrid" {
		t.Errorf("work = %+v", got.PrefWorkArrangement)
	}
}

func TestClearResume(t *testing.T) {
	p := useTempResume(t)
	if err := SaveResume("x"); err != nil {
		t.Fatal(err)
	}
	if err := ClearResume(); err != nil {
		t.Fatalf("ClearResume: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("RESUME.md still exists after ClearResume")
	}
	// Clear on a missing file is not an error.
	if err := ClearResume(); err != nil {
		t.Errorf("ClearResume on missing file: %v", err)
	}
}

func TestIsEmpty(t *testing.T) {
	if !IsEmpty(nil) {
		t.Error("nil profile must be empty")
	}
	// A profile built from zero-value settings + no resume is empty.
	useTempResume(t)
	got, err := Load(config.ProfileSettings{})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !IsEmpty(got) {
		t.Errorf("zero-value profile must be empty, got %+v", got)
	}
}
