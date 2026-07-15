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
	Scoring ScoringSettings `yaml:"scoring"`
	Profile ProfileSettings `yaml:"profile"`
}

// ProfileSettings holds the structured candidate preferences that drive the
// deterministic system rubrics (salary floor, work arrangement) and the LLM
// enrich prompt (preferred/avoided tech). These flow into models.Profile.Pref*
// at load time. Pointer types let users express "unset" by deleting the key.
type ProfileSettings struct {
	WorkArrangement   []string `yaml:"work_arrangement,omitempty"`
	MinSalary         *float64 `yaml:"min_salary,omitempty"`
	MinSalaryCurrency string   `yaml:"min_salary_currency,omitempty"`
	PreferredTech     []string `yaml:"preferred_tech,omitempty"`
	AvoidedTech       []string `yaml:"avoided_tech,omitempty"`
}

type ScoringSettings struct {
	Rubrics []Rubric `yaml:"rubrics"`
}

// Rubric is one scored criterion. System rubrics (salary, work_arrangement)
// are computed deterministically in Go; dynamic rubrics are rated 1–5 by the
// LLM at enrichment time. Weight is user-tunable (1–10, default 5).
// Items lets one rubric carry a list (e.g. preferred/avoided tech) whose rating
// reflects aggregate match across the whole list.
type Rubric struct {
	ID          string   `yaml:"id" json:"id"`
	Kind        string   `yaml:"kind" json:"kind"` // "system" | "dynamic"
	Weight      int      `yaml:"weight" json:"weight"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Items       []string `yaml:"items,omitempty" json:"items,omitempty"`
}

// System rubric IDs — always present, computed deterministically in Go.
const (
	RubricSalary      = "salary"
	RubricArrangement = "work_arrangement"
)

// DefaultSettings returns the built-in defaults used when the YAML file is
// absent or a key is omitted.
func DefaultSettings() Settings {
	return Settings{
		Scoring: DefaultScoringSettings(),
	}
}

// DefaultScoringSettings returns the rubric defaults: two system rubrics
// (salary, work_arrangement) at weight 5. Dynamic rubrics (including location)
// are added by the setup flow from the user's preferences paragraph.
func DefaultScoringSettings() ScoringSettings {
	return ScoringSettings{
		Rubrics: []Rubric{
			{ID: RubricSalary, Kind: "system", Weight: 5, Description: "Salary level relative to your floor"},
			{ID: RubricArrangement, Kind: "system", Weight: 5, Description: "Remote / hybrid / onsite match"},
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
	// Rubric weights must land in [1,10]; clamp anything invalid to the default 5.
	for i := range s.Scoring.Rubrics {
		if s.Scoring.Rubrics[i].Weight < 1 || s.Scoring.Rubrics[i].Weight > 10 {
			s.Scoring.Rubrics[i].Weight = 5
		}
	}
	return s, nil
}

// defaultSettingsTemplate is the YAML written when settings.yaml doesn't exist
// yet. Profile starts empty so the user can fill it via `linkedin-jobs setup`.
const defaultSettingsTemplate = `# linkedin-jobs settings — edit freely, delete a key to fall back to its default.
# Docs: README → Settings

scoring:
  # Rubrics drive the weighted-average score. System rubrics are computed in Go;
  # dynamic rubrics are rated 1-5 by the LLM. Run 'linkedin-jobs setup' to
  # (re)generate the dynamic rubrics from a preferences paragraph. Weight: 1-10.
  rubrics:
    - id: salary
      kind: system
      weight: 5
      description: Salary level relative to your floor
    - id: work_arrangement
      kind: system
      weight: 5
      description: Remote / hybrid / onsite match

profile:
  work_arrangement: []          # remote, hybrid, onsite (any subset)
  min_salary: 0                 # 0 = no salary floor
  min_salary_currency: USD      # ISO 4217 (USD, CAD, EUR, GBP)
  preferred_tech: []            # tech tokens (also surfaced as a dynamic rubric via setup)
  avoided_tech: []              # tech tokens to penalize (surfaced as a dynamic rubric via setup)
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

// SaveRubrics writes the scoring.rubrics section into settings.yaml, preserving
// the rest of the file. Used by setup/amend/reset to persist a generated or
// edited rubric set.
func SaveRubrics(rubrics []Rubric) error {
	path := SettingsPath()
	var root map[string]any
	data, err := os.ReadFile(path)
	if err == nil {
		_ = yaml.Unmarshal(data, &root)
	}
	if root == nil {
		root = map[string]any{}
	}
	scoring, _ := root["scoring"].(map[string]any)
	if scoring == nil {
		scoring = map[string]any{}
	}
	raw, err := yaml.Marshal(ScoringSettings{Rubrics: rubrics})
	if err != nil {
		return fmt.Errorf("marshal rubrics: %w", err)
	}
	var scored map[string]any
	if err := yaml.Unmarshal(raw, &scored); err != nil {
		return fmt.Errorf("remarshal rubrics: %w", err)
	}
	if rubricsMap, ok := scored["rubrics"]; ok {
		scoring["rubrics"] = rubricsMap
	}
	root["scoring"] = scoring
	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	return os.WriteFile(path, out, 0o644)
}
