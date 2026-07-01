// Package profile reads the candidate's resume and combines it with the
// structured preference knobs from settings.yaml to build the in-memory
// models.Profile consumed by the deterministic scorer, the hard filter, and the
// LLM enrich prompt.
//
// Files used (in the project directory, override with $LJ_CONFIG_DIR):
//
//	$CWD/RESUME.md       — resume body (free text), sent to the LLM as context
//	$CWD/settings.yaml   — preference knobs under the `profile:` section
//
// Preference knobs (work_arrangement, min_salary, locations, preferred_tech)
// drive the deterministic hard filter (internal/filter) and the rubric
// (internal/score); the resume body feeds the LLM enrich call as candidate
// context.
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/models"
)

// ResumeFile is the filename holding the resume body.
const ResumeFile = "RESUME.md"

// ResumePath returns the resolved path to RESUME.md (project-local).
func ResumePath() string { return filepath.Join(config.ProjectDir(), ResumeFile) }

// Load builds the in-memory profile from RESUME.md (for ResumeText) plus the
// structured preference knobs from settings. A missing resume yields an empty
// ResumeText but the knobs still flow through; the caller treats a fully-empty
// profile (no resume + no knobs) as "no profile set".
func Load(prefs config.ProfileSettings) (*models.Profile, error) {
	p := &models.Profile{
		PrefWorkArrangement:   prefs.WorkArrangement,
		PrefMinSalary:         prefs.MinSalary,
		PrefMinSalaryCurrency: prefs.MinSalaryCurrency,
		PrefLocations:         prefs.Locations,
		PrefPreferredTech:     prefs.PreferredTech,
	}

	resumeBytes, err := os.ReadFile(ResumePath())
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", ResumeFile, err)
	}
	if len(resumeBytes) > 0 {
		p.ResumeText = strings.TrimSpace(string(resumeBytes))
	}
	p.UpdatedAt = nowISO()
	return p, nil
}

// IsEmpty reports whether the profile has no resume and no preference knobs —
// i.e. scoring/filtering will run context-free.
func IsEmpty(p *models.Profile) bool {
	if p == nil {
		return true
	}
	return p.ResumeText == "" &&
		p.PrefWorkArrangement == "" &&
		p.PrefMinSalary == nil &&
		p.PrefLocations == "" &&
		len(p.PrefPreferredTech) == 0
}

// SaveResume writes the resume body to RESUME.md (creating the project dir if
// needed). An empty text clears the file.
func SaveResume(text string) error {
	if err := os.MkdirAll(config.ProjectDir(), 0o755); err != nil {
		return fmt.Errorf("create project dir: %w", err)
	}
	if err := os.WriteFile(ResumePath(), []byte(strings.TrimSpace(text)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", ResumeFile, err)
	}
	return nil
}

// ClearResume removes RESUME.md. A missing file is not an error. Preference
// knobs live in settings.yaml and are cleared by hand-editing that file.
func ClearResume() error {
	if err := os.Remove(ResumePath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
