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
