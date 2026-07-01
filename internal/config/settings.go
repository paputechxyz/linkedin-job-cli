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
	Profile ProfileSettings `yaml:"profile"`
}

// ProfileSettings holds the structured candidate preferences that drive the
// deterministic hard filter + rubric (work arrangement, salary floor, locations,
// preferred tech, avoided tech). These flow into models.Profile.Pref* at load
// time. Pointer types let users express "unset" by deleting the key.
type ProfileSettings struct {
	WorkArrangement   []string `yaml:"work_arrangement,omitempty"`
	MinSalary         *float64 `yaml:"min_salary,omitempty"`
	MinSalaryCurrency string   `yaml:"min_salary_currency,omitempty"`
	Locations         []string `yaml:"locations,omitempty"`
	PreferredTech     []string `yaml:"preferred_tech,omitempty"`
	AvoidedTech       []string `yaml:"avoided_tech,omitempty"`
}

type StatsSettings struct {
	TopCompaniesLimit int `yaml:"top_companies_limit"`
}

type FilterSettings struct {
	AutoFilter bool `yaml:"auto_filter"`
}

type ScoringSettings struct {
	ReasonThreshold int             `yaml:"reason_threshold"`
	Baseline        int             `yaml:"baseline"`
	DealBreakerCap  int             `yaml:"deal_breaker_cap"`
	DealBreakers    []string        `yaml:"deal_breakers"`
	Weights         ScoringWeights  `yaml:"weights"`
}

// ScoringWeights tunes the rubric dimensions. All default to the values in
// DefaultSettings(); any weight set to 0 disables that dimension.
type ScoringWeights struct {
	Salary              int `yaml:"salary"`
	TechOverlap         int `yaml:"tech_overlap"`
	Startup             int `yaml:"startup"`
	AIIntensity         int `yaml:"ai_intensity"`
	CompensationExtras  int `yaml:"compensation_extras"`
	RemoteTiebreak      int `yaml:"remote_tiebreak"`
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
		Scoring: DefaultScoringSettings(),
		Enrich:  EnrichSettings{AutoEnrichOnSave: false},
	}
}

// DefaultScoringSettings returns the rubric defaults: baseline 60 after passing
// the hard filter, deal-breakers cap at 30, six weighted dimensions summing to
// a ~30-point upside above baseline. Tunable via settings.yaml.
func DefaultScoringSettings() ScoringSettings {
	return ScoringSettings{
		ReasonThreshold: 70,
		Baseline:        60,
		DealBreakerCap:  30,
		DealBreakers:    []string{".NET", "C#", "Ruby on Rails"},
		Weights: ScoringWeights{
			Salary:             6,
			TechOverlap:        7,
			Startup:            5,
			AIIntensity:        5,
			CompensationExtras: 4,
			RemoteTiebreak:     3,
		},
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
// content (settings.yaml incl. the profile: knobs, RESUME.md): $LJ_CONFIG_DIR
// if set, otherwise the current working directory. These files describe *this*
// job-search project (your resume, your preference knobs, your tunables) and so
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
	// Rubric defaults: fill any missing/invalid scoring values from defaults so
	// the scorer never sees a zero baseline or an empty deal-breaker cap.
	if s.Scoring.Baseline <= 0 || s.Scoring.Baseline > 100 {
		s.Scoring.Baseline = DefaultSettings().Scoring.Baseline
	}
	if s.Scoring.DealBreakerCap <= 0 || s.Scoring.DealBreakerCap > 100 {
		s.Scoring.DealBreakerCap = DefaultSettings().Scoring.DealBreakerCap
	}
	if len(s.Scoring.DealBreakers) == 0 {
		s.Scoring.DealBreakers = DefaultSettings().Scoring.DealBreakers
	}
	applyDefaultWeight := func(current, def int) int {
		if current < 0 {
			return def
		}
		return current
	}
	dw := DefaultSettings().Scoring.Weights
	s.Scoring.Weights.Salary = applyDefaultWeight(s.Scoring.Weights.Salary, dw.Salary)
	s.Scoring.Weights.TechOverlap = applyDefaultWeight(s.Scoring.Weights.TechOverlap, dw.TechOverlap)
	s.Scoring.Weights.Startup = applyDefaultWeight(s.Scoring.Weights.Startup, dw.Startup)
	s.Scoring.Weights.AIIntensity = applyDefaultWeight(s.Scoring.Weights.AIIntensity, dw.AIIntensity)
	s.Scoring.Weights.CompensationExtras = applyDefaultWeight(s.Scoring.Weights.CompensationExtras, dw.CompensationExtras)
	s.Scoring.Weights.RemoteTiebreak = applyDefaultWeight(s.Scoring.Weights.RemoteTiebreak, dw.RemoteTiebreak)
	return s, nil
}
