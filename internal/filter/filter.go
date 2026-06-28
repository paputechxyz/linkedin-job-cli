package filter

import (
	"strings"

	"linkedin-jobs/internal/models"
)

// PassesHardFilter reports whether a job is NOT an obvious mismatch for the
// profile's hard constraints. It uses only pre-LLM fields (work arrangement
// derived from location/remote_type, salary, location) so it can gate an LLM
// call. A nil/empty profile passes everything. Unknown fields (no salary, empty
// location) are never treated as mismatches — only clear mismatches filter.
func PassesHardFilter(job *models.JobPosting, p *models.Profile) bool {
	if p == nil {
		return true
	}
	blob := strings.ToLower(job.Location + " " + job.RemoteType)

	// Work arrangement: a remote-required preference rejects jobs with no remote signal.
	if p.PrefWorkArrangement == "remote" && !strings.Contains(blob, "remote") {
		return false
	}

	// Salary floor: only reject when the job actually has a salary below it.
	if p.PrefMinSalary != nil && *p.PrefMinSalary > 0 && job.HasSalary() {
		if job.SalaryMax() < *p.PrefMinSalary {
			return false
		}
	}

	// Preferred locations: reject only when the job's location is known and
	// matches none of the preferred tokens.
	if p.PrefLocations != "" && strings.TrimSpace(job.Location) != "" {
		if !locationMatches(blob, p.PrefLocations) {
			return false
		}
	}
	return true
}

func locationMatches(jobBlob, prefLocations string) bool {
	for _, tok := range strings.Split(prefLocations, ",") {
		t := strings.TrimSpace(tok)
		if t == "" {
			continue
		}
		if strings.Contains(jobBlob, strings.ToLower(t)) {
			return true
		}
	}
	return false
}
