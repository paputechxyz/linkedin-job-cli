package linkedin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseRecommended feeds the captured (sanitized) Voyager response through
// the parser and asserts it extracts real job cards. This validates the
// recommended-jobs parsing without needing a live authenticated session.
func TestParseRecommended(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "recommended.json"))
	if err != nil {
		t.Skip("testdata/recommended.json not present:", err)
	}
	jobs := parseRecommended(string(body))
	if len(jobs) == 0 {
		t.Fatal("parseRecommended returned 0 jobs from a fixture that should contain 24")
	}
	t.Logf("parsed %d recommended jobs", len(jobs))
	for i, j := range jobs {
		if j.ID == "" {
			t.Errorf("job %d missing id", i)
		}
		if j.Title == "" {
			t.Errorf("job %s missing title", j.ID)
		}
		if !strings.HasPrefix(j.URL, "https://www.linkedin.com/jobs/view/") {
			t.Errorf("job %s has unexpected url %q", j.ID, j.URL)
		}
		if j.Source != "recommended" {
			t.Errorf("job %s source = %q, want recommended", j.ID, j.Source)
		}
	}
	// Spot-check that at least one known job from the capture is present.
	found := false
	for _, j := range jobs {
		if j.ID == "4425877454" { // "Staff Software Engineer - MetaMask" from the capture
			found = true
			if !strings.Contains(j.Title, "MetaMask") {
				t.Errorf("expected MetaMask in title, got %q", j.Title)
			}
			if j.Company == "" {
				t.Errorf("MetaMask job missing company")
			}
		}
	}
	if !found {
		t.Errorf("expected job 4425877454 in parsed results")
	}
}

// TestParseRecommendedNewShape covers LinkedIn's current (denormalized) GraphQL
// response, where the JobPostingCard entity is inlined directly at
// data.jobsDashJobCardsByJobCollections.elements[*].jobCard.jobPostingCard and
// there is no top-level `included` array. The OLD-shape fixture above remains
// for backward compatibility in case LinkedIn reverts.
func TestParseRecommendedNewShape(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "recommended_new.json"))
	if err != nil {
		t.Skip("testdata/recommended_new.json not present:", err)
	}
	jobs := parseRecommended(string(body))
	if len(jobs) == 0 {
		t.Fatal("parseRecommended returned 0 jobs from the NEW-shape fixture that should contain 5")
	}
	t.Logf("parsed %d recommended jobs (NEW shape)", len(jobs))
	for i, j := range jobs {
		if j.ID == "" {
			t.Errorf("job %d missing id", i)
		}
		if j.Title == "" {
			t.Errorf("job %s missing title", j.ID)
		}
		if j.Company == "" {
			t.Errorf("job %s missing company", j.ID)
		}
		if j.Location == "" {
			t.Errorf("job %s missing location", j.ID)
		}
		if !strings.HasPrefix(j.URL, "https://www.linkedin.com/jobs/view/") {
			t.Errorf("job %s has unexpected url %q", j.ID, j.URL)
		}
		if j.Source != "recommended" {
			t.Errorf("job %s source = %q, want recommended", j.ID, j.Source)
		}
	}
	// Spot-check the first job from the live capture (id 4430758087).
	for _, j := range jobs {
		if j.ID == "4430758087" {
			if !strings.Contains(j.Title, "Senior Software Engineer II") {
				t.Errorf("expected 'Senior Software Engineer II' in title, got %q", j.Title)
			}
			if j.Company != "Life360" {
				t.Errorf("expected Company=Life360, got %q", j.Company)
			}
		}
	}
}
