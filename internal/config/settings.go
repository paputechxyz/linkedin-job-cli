package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Settings holds tunable runtime settings loaded from a YAML file. Zero values
// are replaced by DefaultSettings so callers always get usable numbers.
type Settings struct {
	Stats   StatsSettings   `yaml:"stats"`
	Filter  FilterSettings  `yaml:"filter"`
	Scoring ScoringSettings `yaml:"scoring"`
	Enrich  EnrichSettings  `yaml:"enrich"`
}

type StatsSettings struct {
	TopCompaniesLimit int `yaml:"top_companies_limit"`
}

type FilterSettings struct {
	AutoFilter bool `yaml:"auto_filter"`
}

type ScoringSettings struct {
	ReasonThreshold int `yaml:"reason_threshold"`
}

type EnrichSettings struct {
	AutoEnrichOnSave bool `yaml:"auto_enrich_on_save"`
}

// DefaultSettings returns the built-in defaults used when the YAML file is
// absent or a key is omitted.
func DefaultSettings() Settings {
	return Settings{
		Stats:   StatsSettings{TopCompaniesLimit: 50},
		Filter:  FilterSettings{AutoFilter: true},
		Scoring: ScoringSettings{ReasonThreshold: 70},
		Enrich:  EnrichSettings{AutoEnrichOnSave: false},
	}
}

// ConfigDir returns the directory holding secret/sensitive config that should
// stay global per user: $LJ_CONFIG_DIR if set, otherwise ~/.linkedin-jobs.
// Used for config.json (LLM provider credentials).
func ConfigDir() string {
	if d := os.Getenv("LJ_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".linkedin-jobs")
}

// ProjectDir returns the directory holding project-local, hand-editable user
// content (settings.yaml, RESUME.md, JOB_PREFERENCE.md): $LJ_CONFIG_DIR if
// set, otherwise the current working directory. These files describe *this*
// job-search project (your resume, your preferences, your tunables) and so
// travel with the repo/folder you run the CLI from, unlike secrets in
// ConfigDir() which stay global.
func ProjectDir() string {
	if d := os.Getenv("LJ_CONFIG_DIR"); d != "" {
		return d
	}
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

// SettingsPath returns the resolved path to settings.yaml (project-local).
func SettingsPath() string {
	return filepath.Join(ProjectDir(), "settings.yaml")
}

// LoadSettings reads settings.yaml from ProjectDir, overlaying it on
// DefaultSettings. A missing file yields defaults with no error.
func LoadSettings() (Settings, error) {
	s := DefaultSettings()
	data, err := os.ReadFile(SettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	// yaml.Unmarshal preserves existing struct fields not present in the
	// document, so defaults survive for keys the file omits.
	if err := yaml.Unmarshal(data, &s); err != nil {
		return DefaultSettings(), err
	}
	// Guard: an explicit zero limit would hide all companies; fall back to default.
	if s.Stats.TopCompaniesLimit <= 0 {
		s.Stats.TopCompaniesLimit = DefaultSettings().Stats.TopCompaniesLimit
	}
	if s.Scoring.ReasonThreshold <= 0 || s.Scoring.ReasonThreshold > 100 {
		s.Scoring.ReasonThreshold = DefaultSettings().Scoring.ReasonThreshold
	}
	return s, nil
}
