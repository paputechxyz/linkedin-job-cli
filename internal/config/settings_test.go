package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSettings_DefaultWhenAbsent(t *testing.T) {
	t.Setenv("LJ_CONFIG_DIR", t.TempDir())
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Stats.TopCompaniesLimit != 50 {
		t.Errorf("default top_companies_limit = %d, want 50", s.Stats.TopCompaniesLimit)
	}
	if !s.Filter.AutoFilter {
		t.Errorf("default auto_filter should be true")
	}
	if s.Scoring.ReasonThreshold != 70 {
		t.Errorf("default reason_threshold = %d, want 70", s.Scoring.ReasonThreshold)
	}
	if s.Enrich.AutoEnrichOnSave {
		t.Errorf("default auto_enrich_on_save should be false")
	}
}

func TestLoadSettings_FileOverridesKeepsDefaults(t *testing.T) {
	d := t.TempDir()
	t.Setenv("LJ_CONFIG_DIR", d)
	os.WriteFile(filepath.Join(d, "settings.yaml"),
		[]byte("stats:\n  top_companies_limit: 25\nscoring:\n  reason_threshold: 80\n"), 0o644)
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Stats.TopCompaniesLimit != 25 {
		t.Errorf("top_companies_limit = %d, want 25", s.Stats.TopCompaniesLimit)
	}
	if s.Scoring.ReasonThreshold != 80 {
		t.Errorf("reason_threshold = %d, want 80", s.Scoring.ReasonThreshold)
	}
	// Keys omitted from the file keep their defaults.
	if !s.Filter.AutoFilter {
		t.Errorf("auto_filter should keep default true")
	}
}

func TestLoadSettings_ZeroOrInvalidLimitFallsBack(t *testing.T) {
	d := t.TempDir()
	t.Setenv("LJ_CONFIG_DIR", d)
	os.WriteFile(filepath.Join(d, "settings.yaml"), []byte("stats:\n  top_companies_limit: 0\n"), 0o644)
	s, _ := LoadSettings()
	if s.Stats.TopCompaniesLimit != 50 {
		t.Errorf("zero limit should fall back to 50, got %d", s.Stats.TopCompaniesLimit)
	}
}
