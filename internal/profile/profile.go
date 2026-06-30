// Package profile stores the user's resume and job preferences as plain
// markdown files in the config directory, so they are trivial to edit by hand:
//
//	$LJ_CONFIG_DIR/RESUME.md          — resume body (free text)
//	$LJ_CONFIG_DIR/JOB_PREFERENCE.md  — preferences body + YAML front-matter knobs
//
// JOB_PREFERENCE.md layout (front-matter is optional; body is free text):
//
//	---
//	work_arrangement: remote
//	min_salary: 200000
//	locations: Remote,US
//	---
//
//	I want staff/founding roles at startups…
//
// The free-text fields feed LLM fit scoring; the front-matter knobs drive the
// deterministic hard filter (see internal/filter).
package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/models"
)

// ResumeFile is the filename holding the resume body.
const ResumeFile = "RESUME.md"

// PrefsFile is the filename holding preferences + front-matter knobs.
const PrefsFile = "JOB_PREFERENCE.md"

// ResumePath returns the resolved path to RESUME.md.
func ResumePath() string { return filepath.Join(config.ConfigDir(), ResumeFile) }

// PrefsPath returns the resolved path to JOB_PREFERENCE.md.
func PrefsPath() string { return filepath.Join(config.ConfigDir(), PrefsFile) }

// prefsFrontmatter mirrors the YAML block at the top of JOB_PREFERENCE.md.
// Pointer types let users express "unset" by deleting the key.
type prefsFrontmatter struct {
	WorkArrangement string   `yaml:"work_arrangement,omitempty"`
	MinSalary       *float64 `yaml:"min_salary,omitempty"`
	MinSalaryCurrency string `yaml:"min_salary_currency,omitempty"`
	Locations       string   `yaml:"locations,omitempty"`
}

// Load reads both files and returns a merged Profile. Returns (nil, nil) when
// neither file exists (no profile set yet).
func Load() (*models.Profile, error) {
	resumeBytes, resumeErr := os.ReadFile(ResumePath())
	if resumeErr != nil && !os.IsNotExist(resumeErr) {
		return nil, fmt.Errorf("read %s: %w", ResumeFile, resumeErr)
	}
	prefsBytes, prefsErr := os.ReadFile(PrefsPath())
	if prefsErr != nil && !os.IsNotExist(prefsErr) {
		return nil, fmt.Errorf("read %s: %w", PrefsFile, prefsErr)
	}
	if resumeErr != nil && prefsErr != nil {
		return nil, nil
	}

	p := &models.Profile{}
	if len(resumeBytes) > 0 {
		p.ResumeText = strings.TrimSpace(string(resumeBytes))
	}
	if len(prefsBytes) > 0 {
		fmText, body, err := splitFrontmatter(string(prefsBytes))
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", PrefsFile, err)
		}
		var fm prefsFrontmatter
		if strings.TrimSpace(fmText) != "" {
			if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
				return nil, fmt.Errorf("parse %s front-matter: %w", PrefsFile, err)
			}
		}
		p.PrefWorkArrangement = fm.WorkArrangement
		p.PrefMinSalary = fm.MinSalary
		p.PrefMinSalaryCurrency = fm.MinSalaryCurrency
		p.PrefLocations = fm.Locations
		p.PreferencesText = strings.TrimSpace(body)
	}
	p.UpdatedAt = nowISO()
	return p, nil
}

// SaveResume writes the resume body to RESUME.md (creating the config dir if
// needed). An empty text clears the file.
func SaveResume(text string) error {
	if err := os.MkdirAll(config.ConfigDir(), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(ResumePath(), []byte(strings.TrimSpace(text)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", ResumeFile, err)
	}
	return nil
}

// SavePrefs writes preferences (body + front-matter knobs) to JOB_PREFERENCE.md.
// All fields come from p; an empty profile yields a minimal empty file.
func SavePrefs(p *models.Profile) error {
	if err := os.MkdirAll(config.ConfigDir(), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	fm := prefsFrontmatter{
		WorkArrangement:   p.PrefWorkArrangement,
		MinSalary:         p.PrefMinSalary,
		MinSalaryCurrency: p.PrefMinSalaryCurrency,
		Locations:         p.PrefLocations,
	}
	fmBytes, err := yaml.Marshal(&fm)
	if err != nil {
		return fmt.Errorf("marshal front-matter: %w", err)
	}
	body := strings.TrimSpace(p.PreferencesText)

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmBytes)
	b.WriteString("---\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	if err := os.WriteFile(PrefsPath(), []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", PrefsFile, err)
	}
	return nil
}

// Clear removes both profile files. Missing files are not an error.
func Clear() error {
	for _, path := range []string{ResumePath(), PrefsPath()} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// splitFrontmatter separates a `---\n…\n---\n` YAML header from the body. If
// the document has no front-matter, fm is empty and body is the whole input.
func splitFrontmatter(doc string) (fm, body string, err error) {
	s := strings.TrimSpace(doc)
	if !strings.HasPrefix(s, "---") {
		return "", doc, nil
	}
	// Drop the opening fence.
	rest := strings.TrimPrefix(s, "---")
	rest = strings.TrimPrefix(rest, "\r\n")
	rest = strings.TrimPrefix(rest, "\n")
	// Find the closing fence on its own line.
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", "", fmt.Errorf("missing closing front-matter fence (---)")
	}
	fm = rest[:idx]
	tail := rest[idx+len("\n---"):]
	tail = strings.TrimPrefix(tail, "\r\n")
	tail = strings.TrimPrefix(tail, "\n")
	return fm, tail, nil
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
