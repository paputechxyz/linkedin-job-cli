package filter

import (
	"strings"

	"linkedin-jobs/internal/fx"
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

	// Work arrangement: reject when the job matches none of the preferred
	// arrangements. A preference containing "remote" rejects jobs with no
	// "remote"/"hybrid" signal; preferences are matched as case-insensitive
	// substrings of the job blob.
	if len(p.PrefWorkArrangement) > 0 && !arrangementMatches(blob, p.PrefWorkArrangement) {
		return false
	}

	// Salary floor: only reject when the job actually has a salary below it.
	// When the profile declares a currency, convert the job's max salary into
	// that currency first so a CAD floor isn't compared raw against USD pay.
	if p.PrefMinSalary != nil && *p.PrefMinSalary > 0 && job.HasSalary() {
		if !meetsSalaryFloor(job.SalaryMax(), job.SalaryCurrency, *p.PrefMinSalary, p.PrefMinSalaryCurrency) {
			return false
		}
	}

	// Preferred locations: reject only when the job's location is known and
	// matches none of the preferred tokens.
	if len(p.PrefLocations) > 0 && strings.TrimSpace(job.Location) != "" {
		if !locationMatches(blob, p.PrefLocations) {
			return false
		}
	}
	return true
}

// arrangementMatches reports whether the job blob contains any of the preferred
// work-arrangement tokens (e.g. remote, hybrid). Case-insensitive substring
// match, mirroring locationMatches.
func arrangementMatches(jobBlob string, prefs []string) bool {
	for _, pref := range prefs {
		t := strings.ToLower(strings.TrimSpace(pref))
		if t == "" {
			continue
		}
		if strings.Contains(jobBlob, t) {
			return true
		}
	}
	return false
}

func locationMatches(jobBlob string, prefLocations []string) bool {
	for _, tok := range prefLocations {
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

// meetsSalaryFloor reports whether a job's salary figure (in its own currency)
// meets the minimum threshold (expressed in floorCurrency). When no currency is
// set it falls back to a raw numeric compare (legacy behavior). Conversion
// failures (unknown currency) are treated as a pass — only clear mismatches are
// filtered, mirroring the "unknown is not a mismatch" rule.
func meetsSalaryFloor(salary float64, jobCurrency string, floor float64, floorCurrency string) bool {
	if floorCurrency == "" {
		return salary >= floor
	}
	jobCur := strings.TrimSpace(jobCurrency)
	if jobCur == "" {
		jobCur = "USD"
	}
	conv, err := fx.Convert(salary, jobCur, floorCurrency)
	if err != nil {
		return salary >= floor // can't convert: fall back to raw compare, don't over-filter
	}
	return conv >= floor
}
