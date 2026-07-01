package score

import (
	"strings"
	"testing"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/models"
)

func floatPtr(f float64) *float64 { return &f }

func defaultWeights() Weights {
	return FromSettings(config.DefaultScoringSettings())
}

// minimalProfile returns a profile with no preferences set — every cap is
// disabled and every profile-dependent dimension returns 0. Used as the base
// for dimension-isolation tests, which then set only the fields they need.
func minimalProfile() *models.Profile {
	return &models.Profile{}
}

// salaryOnlyProfile enables just the salary floor; other dimensions/caps stay disabled.
func salaryOnlyProfile() *models.Profile {
	return &models.Profile{
		PrefMinSalary:         floatPtr(200000),
		PrefMinSalaryCurrency: "CAD",
	}
}

// techOnlyProfile enables just the preferred_tech list.
func techOnlyProfile() *models.Profile {
	return &models.Profile{
		PrefPreferredTech: []string{"Java", "Python", "Go", "Kafka", "Postgres"},
	}
}

// remoteOnlyProfile enables just the remote-work preference and preferred locations.
func remoteOnlyProfile() *models.Profile {
	return &models.Profile{
		PrefWorkArrangement: "remote",
		PrefLocations:       "Remote,Toronto",
	}
}

// fullProfile enables every preference — realistic case for combined + cap tests.
func fullProfile() *models.Profile {
	return &models.Profile{
		PrefWorkArrangement:   "remote",
		PrefMinSalary:         floatPtr(200000),
		PrefMinSalaryCurrency: "CAD",
		PrefLocations:         "Remote,Toronto",
		PrefPreferredTech:     []string{"Java", "Python", "Go", "Kafka", "Postgres"},
	}
}

// --- Deal-breaker path ---

func TestCompute_DealBreakerCapsAtDefault(t *testing.T) {
	j := &models.JobPosting{TechStack: "Java, C#, Spring"}
	r := Compute(j, fullProfile(), defaultWeights())
	if r.Score != 30 {
		t.Errorf("Score=%d want 30", r.Score)
	}
	if r.CapReason != CapDealBreakerTech {
		t.Errorf("CapReason=%q want %q", r.CapReason, CapDealBreakerTech)
	}
	if want := `Deal-breaker tech "c#" in stack`; r.CapDetail != want {
		t.Errorf("CapDetail=%q want %q", r.CapDetail, want)
	}
}

func TestCompute_DealBreakerCaseInsensitive(t *testing.T) {
	j := &models.JobPosting{TechStack: "java, c#"}
	r := Compute(j, fullProfile(), defaultWeights())
	if r.Score != 30 || r.CapReason != CapDealBreakerTech {
		t.Errorf("expected deal-breaker cap; got score=%d cap=%q", r.Score, r.CapReason)
	}
}

func TestCompute_DealBreakerCustomCapAndTokens(t *testing.T) {
	w := defaultWeights()
	w.DealBreakerCap = 40
	w.DealBreakers = []string{"Salesforce", "PHP"}
	j := &models.JobPosting{TechStack: "Ruby, PHP, Rails"}
	r := Compute(j, fullProfile(), w)
	if r.Score != 40 || r.CapReason != CapDealBreakerTech {
		t.Errorf("expected custom cap 40; got score=%d cap=%q", r.Score, r.CapReason)
	}
}

// --- Hard-filter cap paths ---

func TestCompute_SalarySmallMissCapsAt60(t *testing.T) {
	hi := 185000.0 // 7.5% under 200k floor — minor band
	j := &models.JobPosting{SalaryHigh: &hi, SalaryCurrency: "CAD", Location: "Remote", RemoteType: "Remote"}
	r := Compute(j, fullProfile(), defaultWeights())
	if r.Score != 60 {
		t.Errorf("Score=%d want 60 (small miss)", r.Score)
	}
	if r.CapReason != CapSalaryUnderFloor {
		t.Errorf("CapReason=%q want %q", r.CapReason, CapSalaryUnderFloor)
	}
	if !strings.Contains(r.CapDetail, "8% under") {
		t.Errorf("CapDetail=%q want it to mention the miss %%", r.CapDetail)
	}
}

func TestCompute_SalarySevereMissCapsAt50(t *testing.T) {
	hi := 100000.0 // 50% under floor — severe band
	j := &models.JobPosting{SalaryHigh: &hi, SalaryCurrency: "CAD", Location: "Remote", RemoteType: "Remote"}
	r := Compute(j, fullProfile(), defaultWeights())
	if r.Score != 50 {
		t.Errorf("Score=%d want 50 (severe miss)", r.Score)
	}
	if r.CapReason != CapSalaryUnderFloorSevere {
		t.Errorf("CapReason=%q want %q", r.CapReason, CapSalaryUnderFloorSevere)
	}
	if !strings.Contains(r.CapDetail, "50% under") {
		t.Errorf("CapDetail=%q want it to mention the miss %%", r.CapDetail)
	}
}

func TestCompute_NonRemoteCapsAt55(t *testing.T) {
	j := &models.JobPosting{Location: "Toronto, ON", RemoteType: "Onsite"} // matches Toronto pref so only non_remote fires
	r := Compute(j, fullProfile(), defaultWeights())
	if r.Score != 55 {
		t.Errorf("Score=%d want 55 (non-remote)", r.Score)
	}
	if r.CapReason != CapNonRemote {
		t.Errorf("CapReason=%q want %q", r.CapReason, CapNonRemote)
	}
	if !strings.Contains(r.CapDetail, "remote") {
		t.Errorf("CapDetail=%q want it to mention remote", r.CapDetail)
	}
}

func TestCompute_LocationMissCapsAt55(t *testing.T) {
	// Job in SF with NO remote signal: location doesn't match Remote/Toronto prefs,
	// AND no remote signal triggers non_remote too. Lowest cap (55) wins either way.
	j := &models.JobPosting{Location: "San Francisco, CA", RemoteType: "Onsite"}
	r := Compute(j, fullProfile(), defaultWeights())
	if r.Score != 55 {
		t.Errorf("Score=%d want 55", r.Score)
	}
	if r.CapReason == CapNone {
		t.Errorf("expected a cap; got %q", r.CapReason)
	}
	if r.CapDetail == "" {
		t.Errorf("CapDetail empty; want a human sentence")
	}
}

func TestCompute_LowestCapWins(t *testing.T) {
	hi := 100000.0
	j := &models.JobPosting{SalaryHigh: &hi, SalaryCurrency: "CAD", Location: "New York, NY", RemoteType: "Onsite"}
	r := Compute(j, fullProfile(), defaultWeights())
	if r.Score != 50 {
		t.Errorf("Score=%d want 50 (lowest cap wins)", r.Score)
	}
	if r.CapReason != CapSalaryUnderFloorSevere {
		t.Errorf("CapReason=%q want %q (the 50-point cap)", r.CapReason, CapSalaryUnderFloorSevere)
	}
}

// --- Baseline paths ---

func TestCompute_BaselineNoSignals(t *testing.T) {
	j := &models.JobPosting{}
	r := Compute(j, minimalProfile(), defaultWeights())
	if r.Score != 60 {
		t.Errorf("Score=%d want 60 (baseline only)", r.Score)
	}
	if r.CapReason != CapNone {
		t.Errorf("CapReason=%q want none", r.CapReason)
	}
}

func TestCompute_NilJob(t *testing.T) {
	r := Compute(nil, fullProfile(), defaultWeights())
	if r.Score != 60 {
		t.Errorf("nil job Score=%d want 60 (baseline)", r.Score)
	}
}

// --- Salary dimension tiers (isolated) ---

func TestCompute_SalaryTiers(t *testing.T) {
	w := defaultWeights()
	max := w.Salary
	cases := []struct {
		name   string
		amount float64
		wantPct float64 // fraction of max salary weight
	}{
		{"at_floor", 200000, 1.0 / 3.0},
		{"plus_15pct", 230000, 2.0 / 3.0},
		{"plus_40pct", 280000, 1.0},
		// Note: a below-floor salary always triggers the hard-filter cap, so it's
		// covered by TestCompute_SalarySevereMissCapsAt50 — not a salary-dimension case.
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hi := tc.amount
			j := &models.JobPosting{SalaryHigh: &hi, SalaryCurrency: "CAD"}
			r := Compute(j, salaryOnlyProfile(), w)
			want := 60 + int(float64(max)*tc.wantPct)
			if r.Score != want {
				t.Errorf("amount=%.0f Score=%d want %d", tc.amount, r.Score, want)
			}
		})
	}
}

// --- Tech overlap tiers (isolated) ---

func TestCompute_TechOverlapCounts(t *testing.T) {
	w := defaultWeights()
	max := w.TechOverlap
	cases := []struct {
		name    string
		stack   string
		wantPct float64
	}{
		{"zero", "Ruby, Rails, PHP", 0},
		{"one", "Java, Rails", 2.0 / 5.0},
		{"three", "Java, Python, Go, Rails", 3.0 / 5.0},
		{"five_plus", "Java, Python, Go, Kafka, Postgres, Redis", 1.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := &models.JobPosting{TechStack: tc.stack}
			r := Compute(j, techOnlyProfile(), w)
			want := 60 + int(float64(max)*tc.wantPct)
			if r.Score != want {
				t.Errorf("stack=%q Score=%d want %d", tc.stack, r.Score, want)
			}
		})
	}
}

// --- Startup dimension (profile-independent, isolated via minimal profile) ---

func TestCompute_StartupStageAndSize(t *testing.T) {
	w := defaultWeights()
	max := w.Startup
	cases := []struct {
		stage string
		size  string
		want  int
	}{
		{"seed", "11-50", max},
		{"early", "1-10", max},
		{"growth", "51-200", (max * 3) / 5},
		{"mature", "1000+", 0},
		{"", "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.stage+"/"+tc.size, func(t *testing.T) {
			j := &models.JobPosting{CompanyStage: tc.stage, CompanySizeBand: tc.size}
			r := Compute(j, minimalProfile(), w)
			if got := r.Score - 60; got != tc.want {
				t.Errorf("stage=%q size=%q got %d startup pts, want %d (max=%d)", tc.stage, tc.size, got, tc.want, max)
			}
		})
	}
}

// --- AI intensity dimension ---

func TestCompute_AIIntensityEnum(t *testing.T) {
	w := defaultWeights()
	max := w.AIIntensity
	cases := []struct {
		val  string
		want int
	}{
		{"core", max},
		{"mentioned", (max * 2) / 5},
		{"none", 0},
		{"", 0},
		{"bogus", 0},
	}
	for _, tc := range cases {
		t.Run(tc.val, func(t *testing.T) {
			j := &models.JobPosting{AIIntensity: tc.val}
			r := Compute(j, minimalProfile(), w)
			if got := r.Score - 60; got != tc.want {
				t.Errorf("ai_intensity=%q got %d pts, want %d", tc.val, got, tc.want)
			}
		})
	}
}

// --- Compensation extras dimension ---

func TestCompute_CompensationExtrasSums(t *testing.T) {
	w := defaultWeights()
	max := w.CompensationExtras
	cases := []struct {
		name                   string
		bonus, eq, ret         bool
		want                   int
	}{
		{"none", false, false, false, 0},
		{"bonus_only", true, false, false, 1},
		{"two", true, true, false, 2},
		{"three_gets_bonus", true, true, true, 4}, // 3 + 1, capped at max
	}
	if max < 4 {
		t.Skip("CompensationExtras max too low for the 3+1 case; adjust test or default")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := &models.JobPosting{HasBonus: tc.bonus, HasEquity: tc.eq, HasRetirementMatch: tc.ret}
			r := Compute(j, minimalProfile(), w)
			if got := r.Score - 60; got != tc.want {
				t.Errorf("got %d comp pts, want %d", got, tc.want)
			}
		})
	}
}

// --- Remote tiebreak dimension (requires remote pref to evaluate) ---
//
// Note: jobs without a "remote" signal trigger the non_remote cap and never
// reach the remote_tiebreak dimension. So the hybrid/onsite cases are covered
// by TestCompute_NonRemoteCapsAt55. Here we test the dimension itself by using
// jobs that DO contain a "remote" signal (so no cap) but vary in hybrid-ness.

func TestCompute_RemoteTiebreak(t *testing.T) {
	w := defaultWeights()
	max := w.RemoteTiebreak
	cases := []struct {
		loc, remote string
		want        int
	}{
		{"Remote, Canada", "Remote", max},        // full remote
		{"Remote (Hybrid)", "Hybrid", max / 3},    // hybrid (still passes filter since blob has "remote")
		{"Toronto", "Remote", max},               // RemoteType carries the signal; loc match passes filter
	}
	for _, tc := range cases {
		t.Run(tc.loc+"/"+tc.remote, func(t *testing.T) {
			j := &models.JobPosting{Location: tc.loc, RemoteType: tc.remote}
			r := Compute(j, remoteOnlyProfile(), w)
			if got := r.Score - 60; got != tc.want {
				t.Errorf("loc=%q remote=%q got %d pts, want %d", tc.loc, tc.remote, got, tc.want)
			}
		})
	}
}

// --- Combined + clamp ---

func TestCompute_AllSignalsCombined(t *testing.T) {
	hi := 280000.0
	j := &models.JobPosting{
		SalaryHigh:          &hi,
		SalaryCurrency:      "CAD",
		Location:            "Remote, Canada",
		RemoteType:          "Remote",
		TechStack:           "Java, Python, Go, Kafka, Postgres, Redis",
		CompanyStage:        "seed",
		CompanySizeBand:     "11-50",
		AIIntensity:         "core",
		HasBonus:            true,
		HasEquity:           true,
		HasRetirementMatch:  true,
	}
	w := defaultWeights()
	r := Compute(j, fullProfile(), w)
	// Manual: 60 baseline + 6 salary (+40%) + 7 tech (5+) + 5 startup (seed) +
	// 5 AI core + 4 comp (3+1) + 3 remote = 90.
	want := 60 + w.Salary + w.TechOverlap + w.Startup + w.AIIntensity + w.CompensationExtras + w.RemoteTiebreak
	if r.Score != want {
		t.Errorf("Score=%d want %d (combined)", r.Score, want)
	}
	if r.CapReason != CapNone {
		t.Errorf("CapReason=%q want none", r.CapReason)
	}
	if len(r.Dimensions) != 6 {
		t.Errorf("expected 6 dimensions, got %d (%+v)", len(r.Dimensions), r.Dimensions)
	}
}

// --- Weights override (disable salary) ---

func TestCompute_WeightsDisableSalary(t *testing.T) {
	w := defaultWeights()
	w.Salary = 0
	hi := 500000.0
	j := &models.JobPosting{SalaryHigh: &hi, SalaryCurrency: "CAD"}
	r := Compute(j, salaryOnlyProfile(), w)
	if r.Score != 60 {
		t.Errorf("Score=%d want 60 (salary weight 0)", r.Score)
	}
}

// --- FitReason rendering ---

func TestFitReason_CappedJob(t *testing.T) {
	// With CapDetail: human sentence leads.
	r := Result{Score: 30, CapReason: CapDealBreakerTech, CapDetail: `Deal-breaker tech "c#" in stack`}
	if got := FitReason(r); got != `Deal-breaker tech "c#" in stack → capped at 30` {
		t.Errorf("got %q", got)
	}
	// Without CapDetail (defensive fallback): still names the cap reason.
	r2 := Result{Score: 50, CapReason: CapSalaryUnderFloorSevere}
	if got := FitReason(r2); got != "capped at 50 (salary_under_floor_severe)" {
		t.Errorf("fallback got %q", got)
	}
}

func TestFitReason_DimensionBreakdown(t *testing.T) {
	r := Result{
		Score: 75,
		Dimensions: []Dimension{
			{Name: "salary", Points: 6, Reason: "CAD235k vs CAD200k floor, +17%"},
			{Name: "tech_overlap", Points: 4, Reason: "3 of 5 preferred"},
			{Name: "startup", Points: 5, Reason: "seed"},
		},
	}
	got := FitReason(r)
	for _, want := range []string{"+6 salary", "+4 tech_overlap", "+5 startup", "total 75"} {
		if !strings.Contains(got, want) {
			t.Errorf("FitReason missing %q; got: %s", want, got)
		}
	}
}
