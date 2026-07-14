package filter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"linkedin-jobs/internal/fx"
	"linkedin-jobs/internal/models"
)

func fptr(f float64) *float64 { return &f }

func job(loc, remoteType string, salaryHigh *float64) *models.JobPosting {
	j := &models.JobPosting{Location: loc, RemoteType: remoteType}
	j.SalaryHigh = salaryHigh
	return j
}

func TestPassesHardFilter_NilProfilePassesAll(t *testing.T) {
	if !PassesHardFilter(job("New York, NY", "onsite", nil), nil) {
		t.Errorf("nil profile should pass everything")
	}
}

func TestPassesHardFilter_RemoteRequired(t *testing.T) {
	p := &models.Profile{PrefWorkArrangement: []string{"remote"}}
	cases := []struct {
		name string
		j    *models.JobPosting
		want bool
	}{
		{"remote job", job("Remote, US", "", nil), true},
		{"remote_type set", job("NYC", "remote", nil), true},
		{"hybrid mentions remote-ish", job("Hybrid - NYC", "", nil), false}, // no "remote" substring
		{"fully onsite", job("New York, NY (On-site)", "", nil), false},
		{"empty location", job("", "", nil), false},
	}
	for _, c := range cases {
		if got := PassesHardFilter(c.j, p); got != c.want {
			t.Errorf("%s: got %v want %v (blob=%q)", c.name, got, c.want, c.j.Location+" "+c.j.RemoteType)
		}
	}
}

func TestPassesHardFilter_SalaryFloor(t *testing.T) {
	p := &models.Profile{PrefMinSalary: fptr(200000)}
	if !PassesHardFilter(job("Remote", "", fptr(250000)), p) {
		t.Errorf("salary above floor should pass")
	}
	if PassesHardFilter(job("Remote", "", fptr(150000)), p) {
		t.Errorf("salary below floor should be filtered")
	}
	// No salary data -> unknown, do not filter.
	if !PassesHardFilter(job("Remote", "", nil), p) {
		t.Errorf("unknown salary should pass (not a clear mismatch)")
	}
}

func TestPassesHardFilter_Locations(t *testing.T) {
	p := &models.Profile{PrefLocations: []string{"Toronto", "Remote", "US"}}
	if !PassesHardFilter(job("Toronto, Canada", "", nil), p) {
		t.Errorf("Toronto should match")
	}
	if !PassesHardFilter(job("Remote", "", nil), p) {
		t.Errorf("Remote should match")
	}
	if PassesHardFilter(job("London, UK", "", nil), p) {
		t.Errorf("London should be filtered")
	}
	// Empty location -> unknown, pass.
	if !PassesHardFilter(job("", "", nil), p) {
		t.Errorf("empty location should pass")
	}
}

func TestPassesHardFilter_Combined(t *testing.T) {
	p := &models.Profile{PrefWorkArrangement: []string{"remote"}, PrefMinSalary: fptr(180000), PrefLocations: []string{"Remote", "US"}}
	// Passes all three.
	if !PassesHardFilter(job("Remote, US", "", fptr(200000)), p) {
		t.Errorf("should pass all constraints")
	}
	// Fails salary only.
	if PassesHardFilter(job("Remote, US", "", fptr(100000)), p) {
		t.Errorf("should fail salary")
	}
}

func TestPassesHardFilter_NoPreference(t *testing.T) {
	// Empty or all-three prefs = no preference → all jobs pass regardless of arrangement.
	noPrefProfiles := []struct {
		name  string
		prefs []string
	}{
		{"empty", nil},
		{"all_three", []string{"remote", "hybrid", "onsite"}},
	}
	jobs := []struct {
		name    string
		loc     string
		remote  string
	}{
		{"remote", "Remote, US", ""},
		{"hybrid", "Hybrid - NYC", ""},
		{"onsite", "SF", "On-site"},
		{"no_signal", "Unknown location", ""},
	}
	for _, np := range noPrefProfiles {
		t.Run(np.name, func(t *testing.T) {
			p := &models.Profile{PrefWorkArrangement: np.prefs}
			for _, jc := range jobs {
				if !PassesHardFilter(job(jc.loc, jc.remote, nil), p) {
					t.Errorf("%s prefs + %s job should pass (no preference)", np.name, jc.name)
				}
			}
		})
	}
}

func TestPassesHardFilter_ArrangementPreferences(t *testing.T) {
	cases := []struct {
		name    string
		prefs   []string
		loc     string
		remote  string
		want    bool
	}{
		// Single-arrangement prefs
		{"remote_pref_remote_job", []string{"remote"}, "Remote, US", "", true},
		{"remote_pref_hybrid_job", []string{"remote"}, "Hybrid - NYC", "", false},
		{"remote_pref_onsite_job", []string{"remote"}, "SF", "onsite", false},
		{"onsite_pref_onsite_job", []string{"onsite"}, "SF", "onsite", true},
		{"onsite_pref_remote_job", []string{"onsite"}, "Remote, US", "", false},
		{"onsite_pref_on_site_hyphenated", []string{"onsite"}, "SF", "On-site", true},
		{"hybrid_pref_hybrid_job", []string{"hybrid"}, "Hybrid - NYC", "", true},
		{"hybrid_pref_remote_job", []string{"hybrid"}, "Remote, US", "", false},
		// Multi-arrangement prefs
		{"hybrid_onsite_pref_hybrid_job", []string{"hybrid", "onsite"}, "Hybrid - NYC", "", true},
		{"hybrid_onsite_pref_onsite_job", []string{"hybrid", "onsite"}, "SF", "onsite", true},
		{"hybrid_onsite_pref_remote_job", []string{"hybrid", "onsite"}, "Remote, US", "", false},
		{"remote_hybrid_pref_remote_hybrid_blob", []string{"remote", "hybrid"}, "Remote (Hybrid)", "", true},
		{"remote_hybrid_pref_onsite_job", []string{"remote", "hybrid"}, "SF", "onsite", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &models.Profile{PrefWorkArrangement: tc.prefs}
			if got := PassesHardFilter(job(tc.loc, tc.remote, nil), p); got != tc.want {
				t.Errorf("prefs=%v job=(%q,%q) got %v want %v", tc.prefs, tc.loc, tc.remote, got, tc.want)
			}
		})
	}
}

// seedFX writes a today-dated rate cache so the FX-aware floor is deterministic.
func seedFX(t *testing.T) {
	t.Helper()
	fx.CacheFile = filepath.Join(t.TempDir(), "fx_cache.json")
	rates := map[string]float64{"USD": 1.0, "CAD": 1.36}
	rc := struct {
		Date      string             `json:"date"`
		Base      string             `json:"base"`
		Rates     map[string]float64 `json:"rates"`
		FetchedAt string             `json:"fetched_at"`
	}{time.Now().Format("2006-01-02"), "USD", rates, "now"}
	data, _ := json.Marshal(rc)
	if err := os.WriteFile(fx.CacheFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPassesHardFilter_SalaryFloorCAD(t *testing.T) {
	seedFX(t)
	// Floor 160k CAD. 1 USD = 1.36 CAD -> 160k CAD ≈ 117647 USD.
	p := &models.Profile{PrefMinSalary: fptr(160000), PrefMinSalaryCurrency: "CAD"}

	// CA$170K -> ~125k USD -> above 160k CAD floor? 170000 CAD >= 160000 CAD: pass.
	j := job("Toronto", "", fptr(170000))
	j.SalaryCurrency = "CAD"
	if !PassesHardFilter(j, p) {
		t.Errorf("CA$170K should clear a CA$160K floor")
	}

	// CA$150K below floor: filter.
	j = job("Toronto", "", fptr(150000))
	j.SalaryCurrency = "CAD"
	if PassesHardFilter(j, p) {
		t.Errorf("CA$150K should be filtered below CA$160K floor")
	}

	// USD $200K -> ~272K CAD, clears the CAD floor (this is the bug the fix targets).
	j = job("NYC", "", fptr(200000))
	j.SalaryCurrency = "USD"
	if !PassesHardFilter(j, p) {
		t.Errorf("USD $200K (≈272K CAD) should clear a CA$160K floor")
	}

	// USD $100K -> ~136K CAD, below floor: filter (previously passed by raw compare).
	j = job("NYC", "", fptr(100000))
	j.SalaryCurrency = "USD"
	if PassesHardFilter(j, p) {
		t.Errorf("USD $100K (≈136K CAD) should be filtered below CA$160K floor")
	}
}
