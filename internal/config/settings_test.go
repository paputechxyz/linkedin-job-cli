package config

import (
	"os"
	"path/filepath"
	"testing"
)

// settingsFile points LoadSettings at an isolated temp path for the duration of
// the test (so the real settings.yaml in the repo root is never read). The path
// need not exist — a missing file yields defaults.
func settingsFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "settings.yaml")
}

func TestLoadSettings_DefaultWhenAbsent(t *testing.T) {
	t.Setenv("LJ_SETTINGS_FILE", settingsFile(t))
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if !s.Filter.AutoFilter {
		t.Errorf("default auto_filter should be true")
	}
	if s.Scoring.ReasonThreshold != 70 {
		t.Errorf("default reason_threshold = %d, want 70", s.Scoring.ReasonThreshold)
	}
}

func TestLoadSettings_FileOverridesKeepsDefaults(t *testing.T) {
	p := settingsFile(t)
	os.WriteFile(p,
		[]byte("scoring:\n  reason_threshold: 80\n"), 0o644)
	t.Setenv("LJ_SETTINGS_FILE", p)
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Scoring.ReasonThreshold != 80 {
		t.Errorf("reason_threshold = %d, want 80", s.Scoring.ReasonThreshold)
	}
	// Keys omitted from the file keep their defaults.
	if !s.Filter.AutoFilter {
		t.Errorf("auto_filter should keep default true")
	}
}
