// Package score implements the dynamic rubric scorer.
//
// The user's rubric set (system + dynamic) lives in settings.yaml. System
// rubrics (salary, work_arrangement, location) are computed deterministically
// in Go from parsed/enriched data; dynamic rubrics are rated 1–5 by the LLM at
// enrichment time. Compute combines every rubric's rating into one normalized
// 0–100 weighted average:
//
//	score = ( Σ wᵢ·rᵢ / Σ wᵢ ) / 5 × 100
//
// where rᵢ ∈ [1,5] and wᵢ ∈ [1,10]. The distribution is stable regardless of
// how many rubrics exist: a job rated 4/5 across the board scores ~80 whether
// there are 3 rubrics or 15. There are no hard caps — a job that fails a rubric
// simply gets a low rating on it.
package score

import (
	"fmt"
	"strings"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/fx"
	"linkedin-jobs/internal/models"
)

// Rating constants for the 1–5 scale.
const (
	RatingMiss    = 1 // strong negative match (e.g. avoided tech present)
	RatingLow     = 2
	RatingNeutral = 3 // unknown / can't judge / partial
	RatingGood    = 4
	RatingStrong  = 5
)

// Result is the output of Compute. Score is the final 0–100 number; Rubrics
// carries the per-rubric rating/weight breakdown used to render fit_reason and
// persisted as loose JSON.
type Result struct {
	Score   int
	Rubrics []RubricScore
}

// RubricScore is one rubric's evaluated contribution for a job.
type RubricScore struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Rating int    `json:"rating"`
	Weight int    `json:"weight"`
	Reason string `json:"reason,omitempty"`
}

// NeutralRating is the rating assigned to a dynamic rubric the LLM did not
// return (so a missing rating never silently zeroes the score).
const NeutralRating = RatingNeutral

// Compute derives the fit score from the rubric set. System rubrics are rated
// here in Go; dynamic rubrics take their rating from dynamicRatings (defaulting
// to neutral when absent). The weighted average is mapped to 0–100.
func Compute(job *models.JobPosting, profile *models.Profile, rubrics []config.Rubric, dynamicRatings map[string]int) Result {
	if job == nil || len(rubrics) == 0 {
		return Result{}
	}
	if dynamicRatings == nil {
		dynamicRatings = map[string]int{}
	}

	out := Result{Rubrics: make([]RubricScore, 0, len(rubrics))}
	var weighted, totalWeight float64
	for _, r := range rubrics {
		rating, reason := rateRubric(r, job, profile, dynamicRatings)
		w := r.Weight
		if w < 1 {
			w = 1
		}
		out.Rubrics = append(out.Rubrics, RubricScore{
			ID: r.ID, Kind: r.Kind, Rating: rating, Weight: w, Reason: reason,
		})
		weighted += float64(w) * float64(rating)
		totalWeight += float64(w)
	}
	if totalWeight == 0 {
		return out
	}
	avg := weighted / totalWeight              // ∈ [1,5]
	out.Score = int(avg/5.0*100.0 + 0.5)       // round to nearest; rating/5×100
	return out
}

// rateRubric returns the 1–5 rating and a short reason for one rubric.
func rateRubric(r config.Rubric, job *models.JobPosting, profile *models.Profile, dynamicRatings map[string]int) (int, string) {
	if r.Kind == "system" {
		switch r.ID {
		case config.RubricSalary:
			return salaryRating(job, profile)
		case config.RubricArrangement:
			return arrangementRating(job, profile)
		case config.RubricLocation:
			return locationRating(job, profile)
		}
	}
	// Dynamic rubric (or an unrecognized system id): take the LLM rating,
	// defaulting to neutral when absent.
	if v, ok := dynamicRatings[r.ID]; ok {
		if v < RatingMiss {
			v = RatingMiss
		}
		if v > RatingStrong {
			v = RatingStrong
		}
		return v, ""
	}
	return NeutralRating, "not rated"
}

// FitReason renders the per-rubric breakdown as a single-line summary. This is
// what the user sees in the fit_reason column and the web UI's score caption.
func FitReason(r Result) string {
	if len(r.Rubrics) == 0 {
		return fmt.Sprintf("no rubrics scored → %d", r.Score)
	}
	parts := make([]string, 0, len(r.Rubrics))
	for _, rb := range r.Rubrics {
		s := fmt.Sprintf("%s %d/5 (w%d)", rb.ID, rb.Rating, rb.Weight)
		if rb.Reason != "" {
			s += " " + rb.Reason
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, ", ") + fmt.Sprintf(" | total %d", r.Score)
}

// --- system rubric raters ---

// salaryRating: tiers by how the job's max salary (FX-converted to the floor's
// currency) compares to the floor. No salary data or no floor → neutral.
func salaryRating(job *models.JobPosting, profile *models.Profile) (int, string) {
	if profile == nil || profile.PrefMinSalary == nil || *profile.PrefMinSalary <= 0 || !job.HasSalary() {
		return RatingNeutral, "no floor/salary"
	}
	floor := *profile.PrefMinSalary
	converted := convertSalaryTo(job.SalaryMax(), job.SalaryCurrency, profile.PrefMinSalaryCurrency)
	ratio := converted / floor
	switch {
	case ratio >= 1.30:
		return RatingStrong, fmt.Sprintf("%s vs %s floor, +%.0f%%", money(converted, profile.PrefMinSalaryCurrency), money(floor, profile.PrefMinSalaryCurrency), (ratio-1)*100)
	case ratio >= 1.10:
		return RatingGood, fmt.Sprintf("%s vs %s floor, +%.0f%%", money(converted, profile.PrefMinSalaryCurrency), money(floor, profile.PrefMinSalaryCurrency), (ratio-1)*100)
	case ratio >= 1.00:
		return RatingNeutral, fmt.Sprintf("%s vs %s floor", money(converted, profile.PrefMinSalaryCurrency), money(floor, profile.PrefMinSalaryCurrency))
	case ratio >= 0.90:
		return RatingLow, fmt.Sprintf("%s under %s floor", money(converted, profile.PrefMinSalaryCurrency), money(floor, profile.PrefMinSalaryCurrency))
	default:
		return RatingMiss, fmt.Sprintf("%s well under %s floor", money(converted, profile.PrefMinSalaryCurrency), money(floor, profile.PrefMinSalaryCurrency))
	}
}

// arrangementRating: rewards a detected arrangement that matches a preference.
// Hybrid is partial when only remote is preferred. No preference → neutral.
func arrangementRating(job *models.JobPosting, profile *models.Profile) (int, string) {
	if profile == nil || !profile.HasWorkArrangementPreference() {
		return RatingNeutral, "no arrangement preference"
	}
	arr := job.DetectArrangement()
	if arr == "" {
		return RatingNeutral, "arrangement unknown"
	}
	if profile.PrefersArrangement(arr) {
		return RatingStrong, arr
	}
	if arr == "hybrid" {
		return RatingNeutral, "hybrid (partial)"
	}
	return RatingMiss, arr
}

// locationRating: full rating when the job location matches a preferred token;
// miss otherwise. No preference or unknown location → neutral.
func locationRating(job *models.JobPosting, profile *models.Profile) (int, string) {
	if profile == nil || len(profile.PrefLocations) == 0 {
		return RatingNeutral, "no location preference"
	}
	if strings.TrimSpace(job.Location) == "" {
		return RatingNeutral, "location unknown"
	}
	blob := strings.ToLower(job.Location + " " + job.RemoteType)
	if locationMatches(blob, profile.PrefLocations) {
		return RatingStrong, job.Location
	}
	return RatingMiss, job.Location
}

// --- helpers ---

func convertSalaryTo(amount float64, fromCur, toCur string) float64 {
	if toCur == "" {
		return amount
	}
	from := strings.TrimSpace(fromCur)
	if from == "" {
		from = "USD"
	}
	if from == toCur {
		return amount
	}
	conv, err := fx.Convert(amount, from, toCur)
	if err != nil {
		return amount
	}
	return conv
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

func money(amount float64, currency string) string {
	if currency == "" {
		return fmt.Sprintf("$%.0f", amount)
	}
	return fmt.Sprintf("%s%.0f", currency, amount)
}
