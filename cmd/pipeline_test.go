package cmd

import (
	"strings"
	"testing"

	"linkedin-jobs/internal/models"
)

func ptrFloat(f float64) *float64 { return &f }

func TestGateDropReason_NoGatesActive(t *testing.T) {
	j := &models.JobPosting{Title: "X", Company: "Y"}
	if got := gateDropReason(j, ingestOptions{}); got != "" {
		t.Errorf("expected empty reason when no gates active, got %q", got)
	}
}

func TestGateDropReason_PassesAllActiveGates(t *testing.T) {
	low, high := 150000.0, 250000.0
	j := &models.JobPosting{
		Title:          "Staff Engineer",
		Company:        "Acme",
		Location:       "Remote, US",
		RemoteType:     "remote",
		SalaryLow:      &low,
		SalaryHigh:     &high,
		SalaryCurrency: "USD",
	}
	opts := ingestOptions{minSalary: 200000, minSalaryCurrency: "CAD", remote: true, hybrid: true}
	if got := gateDropReason(j, opts); got != "" {
		t.Errorf("expected job to pass all gates, got reason %q", got)
	}
}

func TestGateDropReason_SalaryNoData(t *testing.T) {
	j := &models.JobPosting{Title: "X", Company: "Y"}
	got := gateDropReason(j, ingestOptions{minSalary: 200000, minSalaryCurrency: "CAD"})
	if !strings.Contains(got, "no salary data") {
		t.Errorf("want mention of 'no salary data', got %q", got)
	}
	if !strings.Contains(got, "CA$200000") {
		t.Errorf("want floor in reason, got %q", got)
	}
}

func TestGateDropReason_SalaryBelowFloor_NoCurrency(t *testing.T) {
	high := 150000.0
	j := &models.JobPosting{
		Title:          "X",
		Company:        "Y",
		SalaryHigh:     &high,
		SalaryCurrency: "USD",
	}
	got := gateDropReason(j, ingestOptions{minSalary: 200000})
	if !strings.Contains(got, "below") || !strings.Contains(got, "$200000") {
		t.Errorf("want 'below $200000 floor', got %q", got)
	}
}

func TestGateDropReason_SalaryBelowFloor_WithFX(t *testing.T) {
	// USD$100,000 max converts to roughly CA$135k — well below a CA$200k floor.
	high := 100000.0
	j := &models.JobPosting{
		Title:          "X",
		Company:        "Y",
		SalaryHigh:     &high,
		SalaryCurrency: "USD",
	}
	got := gateDropReason(j, ingestOptions{minSalary: 200000, minSalaryCurrency: "CAD"})
	if !strings.Contains(got, "below") {
		t.Errorf("want 'below ... floor', got %q", got)
	}
	if !strings.Contains(got, "CA$200000") {
		t.Errorf("want floor in reason, got %q", got)
	}
	if !strings.Contains(got, "~=") && !strings.Contains(got, "≈") {
		t.Errorf("want FX-converted amount in reason, got %q", got)
	}
}

func TestGateDropReason_RemoteGateFails(t *testing.T) {
	j := &models.JobPosting{
		Title:      "X",
		Company:    "Y",
		Location:   "Berlin, DE",
		RemoteType: "onsite",
	}
	got := gateDropReason(j, ingestOptions{remote: true})
	if !strings.Contains(got, "not remote") {
		t.Errorf("want 'not remote', got %q", got)
	}
	if !strings.Contains(got, "onsite") {
		t.Errorf("want remote_type=onsite in reason, got %q", got)
	}
	if !strings.Contains(got, "Berlin, DE") {
		t.Errorf("want location in reason, got %q", got)
	}
}

func TestGateDropReason_RemoteOrHybridBothWanted(t *testing.T) {
	j := &models.JobPosting{
		Title:      "X",
		RemoteType: "onsite",
		Location:   "NY",
	}
	got := gateDropReason(j, ingestOptions{remote: true, hybrid: true})
	if !strings.Contains(got, "remote/hybrid") {
		t.Errorf("want both arrangements in reason, got %q", got)
	}
}

func TestGateDropReason_OnsiteGatePasses(t *testing.T) {
	// RemoteType is the normalized "onsite" vocabulary value.
	j := &models.JobPosting{Title: "X", Company: "Y", RemoteType: "onsite", Location: "NY"}
	if got := gateDropReason(j, ingestOptions{onsite: true}); got != "" {
		t.Errorf("onsite job should pass --onsite gate, got %q", got)
	}
}

func TestGateDropReason_OnsiteGateMatchesHyphenatedLocation(t *testing.T) {
	// Raw location text often carries the hyphenated "On-site"; the gate
	// accepts both forms even when RemoteType is unset.
	j := &models.JobPosting{Title: "X", Company: "Y", Location: "New York, NY (On-site)"}
	if got := gateDropReason(j, ingestOptions{onsite: true}); got != "" {
		t.Errorf("on-site location should pass --onsite gate, got %q", got)
	}
}

func TestGateDropReason_OnsiteGateFailsRemoteJob(t *testing.T) {
	j := &models.JobPosting{
		Title:      "X",
		Company:    "Y",
		Location:   "Remote, US",
		RemoteType: "remote",
	}
	got := gateDropReason(j, ingestOptions{onsite: true})
	if !strings.Contains(got, "not onsite") {
		t.Errorf("want 'not onsite' in reason, got %q", got)
	}
	if !strings.Contains(got, "remote_type=remote") {
		t.Errorf("want remote_type=remote in reason, got %q", got)
	}
}

func TestGateDropReason_SalaryGateTakesPrecedenceOverWorkArrangement(t *testing.T) {
	// When both salary + remote gates are active and BOTH would fail, the
	// salary reason is reported first (it's checked first in applyGates).
	high := 100000.0
	j := &models.JobPosting{
		Title:      "X",
		RemoteType: "onsite",
		Location:   "NY",
		SalaryHigh: &high,
	}
	got := gateDropReason(j, ingestOptions{minSalary: 200000, remote: true})
	if !strings.Contains(got, "salary") {
		t.Errorf("expected salary reason first, got %q", got)
	}
}

func TestApplyGates_FiltersAndKeeps(t *testing.T) {
	pass := &models.JobPosting{
		Title:      "Remote Well-Paid",
		Company:    "Good Co",
		Location:   "Remote",
		RemoteType: "remote",
		SalaryHigh: ptrFloat(250000),
	}
	fail := &models.JobPosting{
		Title:      "Onsite Underpaid",
		Company:    "Bad Co",
		Location:   "NY",
		RemoteType: "onsite",
		SalaryHigh: ptrFloat(100000),
	}
	out := applyGates([]*models.JobPosting{pass, fail}, ingestOptions{
		minSalary: 200000,
		remote:    true,
	})
	if len(out) != 1 || out[0].ID != pass.ID {
		t.Errorf("expected only the passing job to survive, got %d jobs", len(out))
	}
}

func TestCompanyOrDash(t *testing.T) {
	if got := companyOrDash(""); got != "—" {
		t.Errorf("empty company: got %q, want '—'", got)
	}
	if got := companyOrDash("Acme"); got != "Acme" {
		t.Errorf("non-empty: got %q", got)
	}
}
