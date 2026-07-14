package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Settings holds tunable runtime settings loaded from a YAML file. Zero values
// are replaced by DefaultSettings so callers always get usable numbers.
type Settings struct {
	Filter  FilterSettings  `yaml:"filter"`
	Scoring ScoringSettings `yaml:"scoring"`
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

type FilterSettings struct {
	AutoFilter bool `yaml:"auto_filter"`
}

type ScoringSettings struct {
	ReasonThreshold int            `yaml:"reason_threshold"`
	Baseline        int            `yaml:"baseline"`
	DealBreakerCap  int            `yaml:"deal_breaker_cap"`
	Weights         ScoringWeights `yaml:"weights"`
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

// DefaultSettings returns the built-in defaults used when the YAML file is
// absent or a key is omitted.
func DefaultSettings() Settings {
	return Settings{
		Filter:  FilterSettings{AutoFilter: true},
		Scoring: DefaultScoringSettings(),
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

// HomeDir returns the per-user home for all CLI state: the SQLite DB, the FX
// rate cache, settings.yaml, and the cookies file. Everything lives under
// ~/.linkedin-jobs. The directory is created lazily by callers that write into it.
func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".linkedin-jobs")
}

// ProjectDir returns the directory holding user-content files (settings.yaml).
// Always ~/.linkedin-jobs so the installed binary behaves the same regardless
// of CWD. Override via $LJ_SETTINGS_FILE for dev/testing.
func ProjectDir() string {
	return HomeDir()
}

// SettingsPath returns the resolved path to settings.yaml. An absolute path in
// $LJ_SETTINGS_FILE overrides the default ~/.linkedin-jobs/ location.
func SettingsPath() string {
	if p := os.Getenv("LJ_SETTINGS_FILE"); p != "" {
		return p
	}
	return filepath.Join(HomeDir(), "settings.yaml")
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

// defaultSettingsTemplate is the YAML written when settings.yaml doesn't exist
// yet. Profile starts empty so the user can fill it via `linkedin-jobs setup`.
const defaultSettingsTemplate = `# linkedin-jobs settings — edit freely, delete a key to fall back to its default.
# Docs: README → Settings

filter:
  auto_filter: true             # true = cap score on hard-filter mismatch (no LLM); false = always call LLM

scoring:
  reason_threshold: 70          # fit_reason only emitted at/above this score (0-100)
  baseline: 60                  # starting score after a job passes the hard filter
  deal_breaker_cap: 30          # hard floor when an avoided_tech token is matched
  weights:
    salary: 6                   # tiered by how far above your floor (0/at-floor/+10%/+30%)
    tech_overlap: 7             # count of preferred_tech items found in tech_stack
    startup: 5                  # company_stage seed/early + size 1-50
    ai_intensity: 5             # core=full, mentioned=partial, none=0
    compensation_extras: 4      # bonus + equity + retirement match (1pt each, +1 all three)
    remote_tiebreak: 3          # each preferred arrangement match = full weight

profile:
  work_arrangement: []          # remote, hybrid, onsite (any subset)
  min_salary: 0                 # 0 = no salary floor
  min_salary_currency: USD      # ISO 4217 (USD, CAD, EUR, GBP)
  locations: []                 # preferred location tokens (e.g. Remote, Toronto)
  preferred_tech: []            # tech stack tokens that boost score
  avoided_tech: []              # tech tokens that cap score at deal_breaker_cap
`

// EnsureSettings writes a default settings.yaml to SettingsPath() if the file
// does not yet exist. Returns the path either way. The directory is created
// if needed.
func EnsureSettings() (string, error) {
	p := SettingsPath()
	if _, err := os.Stat(p); err == nil {
		return p, nil // already exists
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return p, fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.WriteFile(p, []byte(defaultSettingsTemplate), 0o644); err != nil {
		return p, fmt.Errorf("write %s: %w", p, err)
	}
	return p, nil
}

// SaveProfile writes the profile section into settings.yaml, preserving the
// rest of the file. Reads the current file (or defaults), replaces the
// profile key, and writes back.
func SaveProfile(p ProfileSettings) error {
	path := SettingsPath()
	var root map[string]any
	data, err := os.ReadFile(path)
	if err == nil {
		_ = yaml.Unmarshal(data, &root)
	}
	if root == nil {
		root = map[string]any{}
	}
	raw, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshal profile: %w", err)
	}
	var profMap map[string]any
	if err := yaml.Unmarshal(raw, &profMap); err != nil {
		return fmt.Errorf("remarshal profile: %w", err)
	}
	root["profile"] = profMap
	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	return os.WriteFile(path, out, 0o644)
}
