package profile

import (
	"os"
	"path/filepath"
	"testing"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/models"
)

// setConfigDir points the profile package at a temp dir for the duration of
// the test by overriding LJ_CONFIG_DIR.
func setConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LJ_CONFIG_DIR", dir)
	return dir
}

func ptr(f float64) *float64 { return &f }

func TestLoad_NoFilesReturnsNil(t *testing.T) {
	setConfigDir(t)
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != nil {
		t.Errorf("want nil profile when no files exist, got %+v", got)
	}
}

func TestSaveResume_RoundTrip(t *testing.T) {
	setConfigDir(t)
	if err := SaveResume("  Go engineer, 10y exp  "); err != nil {
		t.Fatalf("SaveResume: %v", err)
	}
	p, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p == nil {
		t.Fatal("nil profile after save")
	}
	if p.ResumeText != "Go engineer, 10y exp" {
		t.Errorf("resume = %q", p.ResumeText)
	}
	if p.PreferencesText != "" {
		t.Errorf("prefs should be empty, got %q", p.PreferencesText)
	}
}

func TestSavePrefs_RoundTrip(t *testing.T) {
	dir := setConfigDir(t)
	in := &models.Profile{
		PreferencesText:     "Staff/founding, remote, startups",
		PrefWorkArrangement: "remote",
		PrefMinSalary:       ptr(200000),
		PrefLocations:       "Remote,US",
	}
	if err := SavePrefs(in); err != nil {
		t.Fatalf("SavePrefs: %v", err)
	}

	// The file should exist with a front-matter block + body.
	raw, _ := os.ReadFile(filepath.Join(dir, PrefsFile))
	if len(raw) == 0 {
		t.Fatal("JOB_PREFERENCE.md not written")
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.PreferencesText != "Staff/founding, remote, startups" {
		t.Errorf("prefs body = %q", got.PreferencesText)
	}
	if got.PrefWorkArrangement != "remote" {
		t.Errorf("work = %q", got.PrefWorkArrangement)
	}
	if got.PrefMinSalary == nil || *got.PrefMinSalary != 200000 {
		t.Errorf("min_salary = %+v", got.PrefMinSalary)
	}
	if got.PrefLocations != "Remote,US" {
		t.Errorf("locations = %q", got.PrefLocations)
	}
}

func TestLoad_PrefsOnlyNoResume(t *testing.T) {
	setConfigDir(t)
	in := &models.Profile{
		PreferencesText:     "body",
		PrefWorkArrangement: "hybrid",
		PrefMinSalary:       ptr(180000),
	}
	if err := SavePrefs(in); err != nil {
		t.Fatalf("SavePrefs: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.ResumeText != "" {
		t.Errorf("resume should be empty, got %q", got.ResumeText)
	}
	if got.PrefWorkArrangement != "hybrid" {
		t.Errorf("work = %q", got.PrefWorkArrangement)
	}
}

func TestLoad_BodyOnlyNoKnobs(t *testing.T) {
	setConfigDir(t)
	// User hand-edits the file with just body and empty front-matter.
	if err := SavePrefs(&models.Profile{PreferencesText: "free text only"}); err != nil {
		t.Fatalf("SavePrefs: %v", err)
	}
	got, _ := Load()
	if got.PreferencesText != "free text only" {
		t.Errorf("body = %q", got.PreferencesText)
	}
	if got.PrefWorkArrangement != "" || got.PrefMinSalary != nil || got.PrefLocations != "" {
		t.Errorf("knobs should all be empty: %+v", got)
	}
}

func TestClear(t *testing.T) {
	dir := setConfigDir(t)
	if err := SaveResume("x"); err != nil {
		t.Fatal(err)
	}
	if err := SavePrefs(&models.Profile{PreferencesText: "y"}); err != nil {
		t.Fatal(err)
	}
	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ResumeFile)); !os.IsNotExist(err) {
		t.Errorf("RESUME.md still exists after Clear")
	}
	if _, err := os.Stat(filepath.Join(dir, PrefsFile)); !os.IsNotExist(err) {
		t.Errorf("JOB_PREFERENCE.md still exists after Clear")
	}
	// Clear on missing files is not an error.
	if err := Clear(); err != nil {
		t.Errorf("Clear on missing files: %v", err)
	}
}

func TestSplitFrontmatter_NoFence(t *testing.T) {
	fm, body, err := splitFrontmatter("just body")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if fm != "" {
		t.Errorf("fm should be empty, got %q", fm)
	}
	if body != "just body" {
		t.Errorf("body = %q", body)
	}
}

func TestSplitFrontmatter_MissingCloseFence(t *testing.T) {
	_, _, err := splitFrontmatter("---\nwork: remote\n")
	if err == nil {
		t.Error("want error for missing close fence")
	}
}

func TestConfigDirEnvOverride(t *testing.T) {
	// Sanity: confirming profile package honors LJ_CONFIG_DIR (used by setConfigDir).
	t.Setenv("LJ_CONFIG_DIR", "/tmp/some-where")
	if config.ConfigDir() != "/tmp/some-where" {
		t.Errorf("ConfigDir did not pick up LJ_CONFIG_DIR")
	}
}
