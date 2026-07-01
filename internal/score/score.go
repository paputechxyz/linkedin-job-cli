// Package score implements the deterministic rubric scorer.
//
// The LLM enrichment call (internal/llm) extracts structured facts from the job
// posting — including compensation extras and AI intensity, which exist
// specifically to feed this scorer. Compute derives a 0-100 fit_score from
// those facts plus the user's profile, never asking the LLM to pick a number
// directly. This eliminates the LLM midpoint-bias failure mode (scores
// collapsing to 0/50/75) and produces a per-dimension breakdown the user can
// inspect and tune via settings.yaml.
//
// Score band semantics:
//   - 0           : unused (reserved)
//   - deal_breaker_cap (default 30): a tech deal-breaker matched
//   - hard-filter cap (50-60)       : salary/work/location preference violated
//   - baseline (default 60)         : passed hard filter, no positive signals
//   - 60-95                         : calibrated by 6 weighted dimensions
//   - 95-100                        : rare; strong on every dimension
package score

import (
	"fmt"
	"strings"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/fx"
	"linkedin-jobs/internal/models"
)

// Result is the output of Compute. Score is the final 0-100 number; Dimensions
// carries the per-dimension breakdown for the machine-generated fit_reason;
// CapReason is the stable machine code set when the score was capped;
// CapDetail is the human-readable sentence naming the specific offender
// (which deal-breaker token, how far under floor, which preferred location).
type Result struct {
	Score      int
	CapReason  string
	CapDetail  string
	Dimensions []Dimension
}

// Dimension is one scored axis. Points is what it contributed (0 to its max);
// Reason is the human-readable explanation fragment used in fit_reason.
type Dimension struct {
	Name   string
	Points int
	Reason string
}

// FromSettings lifts the YAML-configurable parts of config.ScoringSettings
// into the weights Compute uses. Exposed so the pipeline can build Weights
// from settings without re-importing config here.
func FromSettings(s config.ScoringSettings) Weights {
	return Weights{
		Baseline:            s.Baseline,
		DealBreakerCap:      s.DealBreakerCap,
		DealBreakers:        s.DealBreakers,
		Salary:              s.Weights.Salary,
		TechOverlap:         s.Weights.TechOverlap,
		Startup:             s.Weights.Startup,
		AIIntensity:         s.Weights.AIIntensity,
		CompensationExtras:  s.Weights.CompensationExtras,
		RemoteTiebreak:      s.Weights.RemoteTiebreak,
	}
}

// Weights holds the tunable knobs. Build via FromSettings for production, or
// literal in tests.
type Weights struct {
	Baseline           int      // starting score after hard filter passes
	DealBreakerCap     int      // hard floor when a deal-breaker tech matches
	DealBreakers       []string // tech tokens that cap at DealBreakerCap
	Salary             int
	TechOverlap        int
	Startup            int
	AIIntensity        int
	CompensationExtras int
	RemoteTiebreak     int
}

// capReason constants — recorded in jobs.score_cap_reason and rendered in
// fit_reason so the user sees why a score landed where it did.
const (
	CapNone                  = ""
	CapDealBreakerTech       = "deal_breaker_tech"
	CapSalaryUnderFloor      = "salary_under_floor"       // ≤10% under
	CapSalaryUnderFloorSevere = "salary_under_floor_severe" // >10% under
	CapNonRemote             = "non_remote"
	CapLocationMiss          = "location_miss"
)

// capValue is the score assigned when the corresponding cap reason fires.
// Defined here (not in Weights) because they are scoring invariants, not
// user-tunables — the user said "<60 = very bad fit", so we encode the bands.
const (
	capHardFilterMinor  = 60 // small salary miss
	capHardFilterSevere = 50 // large salary miss
	capNonRemote        = 55
	capLocationMiss     = 55
)

// Compute derives the fit score. Pipeline order is load-bearing:
//  1. Deal-breaker check (caps at DealBreakerCap; returns immediately).
//  2. Hard-filter caps (lowest applicable wins; positive dimensions ignored).
//  3. Baseline + 6 weighted dimensions (only when no cap fired).
//  4. Clamp to 100.
func Compute(job *models.JobPosting, profile *models.Profile, w Weights) Result {
	if job == nil {
		return Result{Score: w.baselineOrDefault()}
	}

	// 1. Deal-breaker check.
	if token := dealBreakerMatch(job.TechStack, w.DealBreakers); token != "" {
		return Result{
			Score:     w.dealBreakerCapOrDefault(),
			CapReason: CapDealBreakerTech,
			CapDetail: fmt.Sprintf("Deal-breaker tech %q in stack", token),
		}
	}

	// 2. Hard-filter caps.
	if cap := hardFilterCap(job, profile); cap != nil {
		return Result{Score: cap.score, CapReason: cap.reason, CapDetail: cap.detail}
	}

	// 3. Baseline + dimensions.
	score := w.baselineOrDefault()
	var dims []Dimension

	if d := salaryDimension(job, profile, w); d.Points > 0 {
		dims = append(dims, d)
		score += d.Points
	}
	if d := techOverlapDimension(job, profile, w); d.Points > 0 {
		dims = append(dims, d)
		score += d.Points
	}
	if d := startupDimension(job, w); d.Points > 0 {
		dims = append(dims, d)
		score += d.Points
	}
	if d := aiIntensityDimension(job, w); d.Points > 0 {
		dims = append(dims, d)
		score += d.Points
	}
	if d := compensationExtrasDimension(job, w); d.Points > 0 {
		dims = append(dims, d)
		score += d.Points
	}
	if d := remoteTiebreakDimension(job, profile, w); d.Points > 0 {
		dims = append(dims, d)
		score += d.Points
	}

	if score > 100 {
		score = 100
	}
	return Result{Score: score, CapReason: CapNone, Dimensions: dims}
}

// FitReason renders the dimension breakdown as a single-line summary. When a
// cap fired, CapDetail names the specific offender (which deal-breaker token,
// how far under floor, which preferred location); otherwise the per-dimension
// points lead. This is what the user sees in the `fit_reason` column and in the
// web UI's score caption.
func FitReason(r Result) string {
	if r.CapReason != CapNone {
		if r.CapDetail != "" {
			return fmt.Sprintf("%s → capped at %d", r.CapDetail, r.Score)
		}
		return fmt.Sprintf("capped at %d (%s)", r.Score, r.CapReason)
	}
	if len(r.Dimensions) == 0 {
		return fmt.Sprintf("no positive signals matched your profile → %d", r.Score)
	}
	parts := make([]string, 0, len(r.Dimensions))
	for _, d := range r.Dimensions {
		parts = append(parts, fmt.Sprintf("+%d %s (%s)", d.Points, d.Name, d.Reason))
	}
	return strings.Join(parts, ", ") + fmt.Sprintf(" | total %d", r.Score)
}

// --- helpers ---

func (w Weights) baselineOrDefault() int {
	if w.Baseline <= 0 {
		return 60
	}
	return w.Baseline
}

func (w Weights) dealBreakerCapOrDefault() int {
	if w.DealBreakerCap <= 0 {
		return 30
	}
	return w.DealBreakerCap
}

// dealBreakerMatch returns the matched deal-breaker token (lowercased) or "".
// Case-insensitive substring match against the extracted tech_stack.
func dealBreakerMatch(techStack string, dealBreakers []string) string {
	if techStack == "" || len(dealBreakers) == 0 {
		return ""
	}
	lowerStack := strings.ToLower(techStack)
	for _, db := range dealBreakers {
		t := strings.ToLower(strings.TrimSpace(db))
		if t == "" {
			continue
		}
		if strings.Contains(lowerStack, t) {
			return t
		}
	}
	return ""
}

type capResult struct {
	score  int
	reason string
	detail string // human-readable sentence naming the specific offender
}

// hardFilterCap mirrors internal/filter.PassesHardFilter but returns graduated
// cap values + reasons instead of a bool. Returns nil when no cap fires (i.e.
// the old PassesHardFilter would have returned true). A nil profile fires no
// caps.
func hardFilterCap(job *models.JobPosting, profile *models.Profile) *capResult {
	if profile == nil {
		return nil
	}
	var fired []capResult

	// Salary floor: only cap when the job actually has a salary below it.
	if profile.PrefMinSalary != nil && *profile.PrefMinSalary > 0 && job.HasSalary() {
		floor := *profile.PrefMinSalary
		converted := convertSalaryTo(job.SalaryMax(), job.SalaryCurrency, profile.PrefMinSalaryCurrency)
		if converted < floor {
			missPct := (floor - converted) / floor
			detail := fmt.Sprintf("Salary %s is %.0f%% under your %s floor",
				money(converted, profile.PrefMinSalaryCurrency), missPct*100, money(floor, profile.PrefMinSalaryCurrency))
			if missPct > 0.10 {
				fired = append(fired, capResult{capHardFilterSevere, CapSalaryUnderFloorSevere, detail})
			} else {
				fired = append(fired, capResult{capHardFilterMinor, CapSalaryUnderFloor, detail})
			}
		}
	}

	// Work arrangement: remote-required preference rejects jobs with no remote signal.
	blob := strings.ToLower(job.Location + " " + job.RemoteType)
	if profile.PrefWorkArrangement == "remote" && !strings.Contains(blob, "remote") {
		fired = append(fired, capResult{capNonRemote, CapNonRemote, "Role has no remote signal; you want fully remote"})
	}

	// Preferred locations: cap only when job location is known and matches none.
	if profile.PrefLocations != "" && strings.TrimSpace(job.Location) != "" {
		if !locationMatches(blob, profile.PrefLocations) {
			detail := fmt.Sprintf("Location %q not in your preferred (%s)", job.Location, profile.PrefLocations)
			fired = append(fired, capResult{capLocationMiss, CapLocationMiss, detail})
		}
	}

	if len(fired) == 0 {
		return nil
	}
	// Lowest applicable cap wins.
	lowest := fired[0]
	for _, c := range fired[1:] {
		if c.score < lowest.score {
			lowest = c
		}
	}
	return &lowest
}

func convertSalaryTo(amount float64, fromCur, toCur string) float64 {
	if toCur == "" {
		return amount // legacy raw compare
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
		return amount // unknown rate: best-effort raw compare, mirror filter.go behavior
	}
	return conv
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

// --- dimensions ---

// salaryDimension: tiered by how far the job's max salary exceeds the floor.
// Below-floor is handled by hardFilterCap; this dimension only adds points for
// at-or-above-floor jobs. Scale: at floor = ~33% of weight, +10% = ~66%, +30% = full.
func salaryDimension(job *models.JobPosting, profile *models.Profile, w Weights) Dimension {
	max := w.Salary
	if max <= 0 || profile == nil || profile.PrefMinSalary == nil || *profile.PrefMinSalary <= 0 || !job.HasSalary() {
		return Dimension{Name: "salary"}
	}
	floor := *profile.PrefMinSalary
	converted := convertSalaryTo(job.SalaryMax(), job.SalaryCurrency, profile.PrefMinSalaryCurrency)
	if converted < floor {
		return Dimension{Name: "salary"} // cap path owns the below-floor case
	}
	ratio := converted / floor
	var pts int
	var tier string
	switch {
	case ratio >= 1.30:
		pts = max
		tier = fmt.Sprintf("%s vs %s floor, +%.0f%%", money(converted, profile.PrefMinSalaryCurrency), money(floor, profile.PrefMinSalaryCurrency), (ratio-1)*100)
	case ratio >= 1.10:
		pts = (max * 2) / 3
		tier = fmt.Sprintf("%s vs %s floor, +%.0f%%", money(converted, profile.PrefMinSalaryCurrency), money(floor, profile.PrefMinSalaryCurrency), (ratio-1)*100)
	default:
		pts = max / 3
		tier = fmt.Sprintf("%s vs %s floor", money(converted, profile.PrefMinSalaryCurrency), money(floor, profile.PrefMinSalaryCurrency))
	}
	return Dimension{Name: "salary", Points: pts, Reason: tier}
}

func money(amount float64, currency string) string {
	if currency == "" {
		return fmt.Sprintf("$%.0f", amount)
	}
	return fmt.Sprintf("%s%.0f", currency, amount)
}

// techOverlapDimension: count of preferred_tech items found in the extracted
// tech_stack (case-insensitive whole-word match). Tiers: 0=0, 1-2=~30%,
// 3-4=~60%, ≥5=full.
func techOverlapDimension(job *models.JobPosting, profile *models.Profile, w Weights) Dimension {
	max := w.TechOverlap
	if max <= 0 || profile == nil || len(profile.PrefPreferredTech) == 0 || job.TechStack == "" {
		return Dimension{Name: "tech_overlap"}
	}
	stack := techStackTokens(job.TechStack)
	matched := []string{}
	for _, pref := range profile.PrefPreferredTech {
		p := strings.ToLower(strings.TrimSpace(pref))
		if p == "" {
			continue
		}
		for _, s := range stack {
			if s == p {
				matched = append(matched, pref)
				break
			}
		}
	}
	n := len(matched)
	var pts int
	switch {
	case n >= 5:
		pts = max
	case n >= 3:
		pts = (max * 3) / 5
	case n >= 1:
		pts = (max * 2) / 5
	}
	reason := fmt.Sprintf("%d of %d preferred", n, len(profile.PrefPreferredTech))
	if n > 0 && n <= 4 {
		reason += " (" + strings.Join(matched, ", ") + ")"
	}
	return Dimension{Name: "tech_overlap", Points: pts, Reason: reason}
}

// techStackTokens splits a comma-separated tech_stack into a normalized token set.
func techStackTokens(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.ToLower(strings.TrimSpace(p))
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// startupDimension: stage-based, refined by size band. Seed/early = full;
// growth = ~60%; mature/public = 0. Small size adds +1 (capped at max).
func startupDimension(job *models.JobPosting, w Weights) Dimension {
	max := w.Startup
	if max <= 0 {
		return Dimension{Name: "startup"}
	}
	var pts int
	switch strings.ToLower(strings.TrimSpace(job.CompanyStage)) {
	case "seed":
		pts = max
	case "early":
		pts = max
	case "growth":
		pts = (max * 3) / 5
	default:
		pts = 0
	}
	reason := job.CompanyStage
	if reason == "" {
		reason = "stage unknown"
	}
	// Size refinement: 1-50 employees adds +1 (capped at max).
	if pts > 0 && pts < max {
		switch job.CompanySizeBand {
		case "1-10", "11-50":
			pts++
			reason += fmt.Sprintf(", %s", job.CompanySizeBand)
		}
		if pts > max {
			pts = max
		}
	}
	return Dimension{Name: "startup", Points: pts, Reason: reason}
}

// aiIntensityDimension: core (AI is the product) = full; mentioned = ~40%; none = 0.
func aiIntensityDimension(job *models.JobPosting, w Weights) Dimension {
	max := w.AIIntensity
	if max <= 0 {
		return Dimension{Name: "ai_intensity"}
	}
	switch strings.ToLower(strings.TrimSpace(job.AIIntensity)) {
	case "core":
		return Dimension{Name: "ai_intensity", Points: max, Reason: "core"}
	case "mentioned":
		return Dimension{Name: "ai_intensity", Points: (max * 2) / 5, Reason: "mentioned"}
	default:
		return Dimension{Name: "ai_intensity", Reason: "none"}
	}
}

// compensationExtrasDimension: 1 point per enabled extra (capped at max), +1 if
// all three present.
func compensationExtrasDimension(job *models.JobPosting, w Weights) Dimension {
	max := w.CompensationExtras
	if max <= 0 {
		return Dimension{Name: "compensation_extras"}
	}
	count := 0
	var on []string
	if job.HasBonus {
		count++
		on = append(on, "bonus")
	}
	if job.HasEquity {
		count++
		on = append(on, "equity")
	}
	if job.HasRetirementMatch {
		count++
		on = append(on, "retirement")
	}
	if count == 0 {
		return Dimension{Name: "compensation_extras"}
	}
	pts := count
	if count == 3 {
		pts++ // all three is exceptional
	}
	if pts > max {
		pts = max
	}
	return Dimension{Name: "compensation_extras", Points: pts, Reason: strings.Join(on, "+")}
}

// remoteTiebreakDimension: full-remote = full; hybrid = ~33%; onsite/unknown = 0.
// Only meaningful when hardFilterCap did not fire (which it would have for the
// non-remote case). Returns 0 silently if no remote signal, or if the profile
// is nil/the user hasn't asked for remote work — no preference means no bonus.
func remoteTiebreakDimension(job *models.JobPosting, profile *models.Profile, w Weights) Dimension {
	max := w.RemoteTiebreak
	if max <= 0 || profile == nil {
		return Dimension{Name: "remote"}
	}
	// User must have expressed a remote preference to be rewarded for it.
	if profile.PrefWorkArrangement != "remote" {
		return Dimension{Name: "remote"}
	}
	blob := strings.ToLower(job.Location + " " + job.RemoteType)
	switch {
	case strings.Contains(blob, "remote") && !strings.Contains(blob, "hybrid"):
		return Dimension{Name: "remote", Points: max, Reason: "fully remote"}
	case strings.Contains(blob, "hybrid"):
		return Dimension{Name: "remote", Points: max / 3, Reason: "hybrid"}
	default:
		return Dimension{Name: "remote"}
	}
}
