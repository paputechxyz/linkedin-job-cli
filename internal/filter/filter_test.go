package filter

import (
	"testing"

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
	p := &models.Profile{PrefWorkArrangement: "remote"}
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
	p := &models.Profile{PrefLocations: "Toronto,Remote,US"}
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
	p := &models.Profile{PrefWorkArrangement: "remote", PrefMinSalary: fptr(180000), PrefLocations: "Remote,US"}
	// Passes all three.
	if !PassesHardFilter(job("Remote, US", "", fptr(200000)), p) {
		t.Errorf("should pass all constraints")
	}
	// Fails salary only.
	if PassesHardFilter(job("Remote, US", "", fptr(100000)), p) {
		t.Errorf("should fail salary")
	}
}
