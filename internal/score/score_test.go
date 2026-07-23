package score

import (
	"strings"
	"testing"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/models"
)

// --- helpers ---

func floatPtr(f float64) *float64 { return &f }

// dynRubric builds a dynamic rubric with the given id and weight.
func dynRubric(id string, weight int) config.Rubric {
	return config.Rubric{ID: id, Kind: "dynamic", Weight: weight}
}

// sysRubric builds a system rubric with the given id and weight.
func sysRubric(id string, weight int) config.Rubric {
	return config.Rubric{ID: id, Kind: "system", Weight: weight}
}

// nDynRubrics builds n dynamic rubrics ("r0".."r{n-1}") all at the given weight.
func nDynRubrics(n, weight int) []config.Rubric {
	out := make([]config.Rubric, n)
	for i := 0; i < n; i++ {
		out[i] = dynRubric("r"+itoa(i), weight)
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// ratingsFor builds a dynamicRatings map giving every id in rubrics the same
// rating.
func ratingsFor(rubrics []config.Rubric, rating int) map[string]models.DynamicRating {
	m := make(map[string]models.DynamicRating, len(rubrics))
	for _, r := range rubrics {
		m[r.ID] = models.DynamicRating{Rating: rating}
	}
	return m
}

// --- weighted average math ---

func TestCompute_WeightedAverage(t *testing.T) {
	// Two dynamic rubrics: w5 r4 and w5 r5 → avg = (20+25)/10 = 4.5 → 90.
	rubrics := []config.Rubric{
		dynRubric("a", 5),
		dynRubric("b", 5),
	}
	ratings := map[string]models.DynamicRating{"a": {Rating: 4}, "b": {Rating: 5}}
	r := Compute(&models.JobPosting{}, &models.Profile{}, rubrics, ratings)
	if r.Score != 90 {
		t.Errorf("Score=%d want 90 (w5·r4 + w5·r5 → avg 4.5)", r.Score)
	}
	if len(r.Rubrics) != 2 {
		t.Errorf("expected 2 rubric scores, got %d", len(r.Rubrics))
	}
}

func TestCompute_ScaleStability(t *testing.T) {
	// All-4 ratings must map to 80 regardless of how many rubrics exist.
	cases := []struct {
		name string
		n    int
	}{
		{"three_rubrics", 3},
		{"ten_rubrics", 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rubrics := nDynRubrics(tc.n, 5)
			r := Compute(&models.JobPosting{}, &models.Profile{}, rubrics, ratingsFor(rubrics, 4))
			if r.Score != 80 {
				t.Errorf("n=%d Score=%d want 80 (rubric count must not distort scale)", tc.n, r.Score)
			}
		})
	}
}

// --- system salary rubric ---

func TestCompute_SalaryRating(t *testing.T) {
	// Single salary system rubric at weight 5; same currency so no FX conversion.
	floor := 200000.0
	prof := &models.Profile{
		PrefMinSalary:         &floor,
		PrefMinSalaryCurrency: "USD",
	}
	rubric := sysRubric(config.RubricSalary, 5)

	cases := []struct {
		name       string
		amount     float64
		wantRating int
		wantScore  int
	}{
		{"at_floor_rating3", 200000, RatingNeutral, 60},
		{"plus_30pct_rating5", 260000, RatingStrong, 100},
		{"well_under_rating1", 100000, RatingMiss, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			amount := tc.amount
			j := &models.JobPosting{SalaryHigh: &amount, SalaryCurrency: "USD"}
			r := Compute(j, prof, []config.Rubric{rubric}, nil)
			if r.Score != tc.wantScore {
				t.Errorf("amount=%.0f Score=%d want %d", tc.amount, r.Score, tc.wantScore)
			}
			if len(r.Rubrics) != 1 || r.Rubrics[0].Rating != tc.wantRating {
				t.Errorf("amount=%.0f rating=%d want %d", tc.amount, r.Rubrics[0].Rating, tc.wantRating)
			}
		})
	}
}

// --- no preference → strong (never drag the score) ---

func TestCompute_SalaryNoFloorStrong(t *testing.T) {
	// No salary floor → any salary (or none) rates strong so it can't pull the
	// weighted average down. A lone salary rubric must score 100.
	rubric := sysRubric(config.RubricSalary, 5)
	cases := []struct {
		name    string
		amount  *float64
		prof    *models.Profile
	}{
		{"nil_profile_job_has_salary", floatPtr(120000), nil},
		{"no_floor_job_has_salary", floatPtr(120000), &models.Profile{}},
		{"no_floor_job_no_salary", nil, &models.Profile{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := &models.JobPosting{SalaryHigh: tc.amount}
			r := Compute(j, tc.prof, []config.Rubric{rubric}, nil)
			if r.Score != 100 {
				t.Errorf("Score=%d want 100 (no floor → strong)", r.Score)
			}
			if len(r.Rubrics) != 1 || r.Rubrics[0].Rating != RatingStrong {
				t.Errorf("rating=%d want %d (strong)", r.Rubrics[0].Rating, RatingStrong)
			}
		})
	}
}

// --- system work_arrangement rubric ---

func TestCompute_WorkArrangementRating(t *testing.T) {
	rubric := sysRubric(config.RubricArrangement, 5)

	cases := []struct {
		name       string
		prefs      []string
		loc        string
		remote     string
		wantRating int
		wantScore  int
	}{
		{"remote_pref_remote_job", []string{"remote"}, "Remote, Canada", "Remote", RatingStrong, 100},
		{"remote_pref_onsite_job", []string{"remote"}, "San Francisco, CA", "On-site", RatingMiss, 20},
		{"no_pref_strong", nil, "Remote, Canada", "Remote", RatingStrong, 100},
		{"all_three_prefs_strong", []string{"remote", "hybrid", "onsite"}, "Hybrid · Toronto", "Hybrid", RatingStrong, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := &models.JobPosting{Location: tc.loc, RemoteType: tc.remote}
			prof := &models.Profile{PrefWorkArrangement: tc.prefs}
			r := Compute(j, prof, []config.Rubric{rubric}, nil)
			if r.Score != tc.wantScore {
				t.Errorf("Score=%d want %d", r.Score, tc.wantScore)
			}
			if len(r.Rubrics) != 1 || r.Rubrics[0].Rating != tc.wantRating {
				t.Errorf("rating=%d want %d", r.Rubrics[0].Rating, tc.wantRating)
			}
		})
	}
}

// --- dynamic rubric rating handling ---

func TestCompute_MissingDynamicRatingDefaultsNeutral(t *testing.T) {
	// "team_fit" is absent from dynamicRatings → rating 3 → score 60.
	rubrics := []config.Rubric{dynRubric("team_fit", 5)}
	r := Compute(&models.JobPosting{}, &models.Profile{}, rubrics, map[string]models.DynamicRating{})
	if r.Score != 60 {
		t.Errorf("Score=%d want 60 (missing dynamic rating defaults neutral)", r.Score)
	}
	if len(r.Rubrics) != 1 || r.Rubrics[0].Rating != RatingNeutral {
		t.Errorf("rating=%d want %d (neutral)", r.Rubrics[0].Rating, RatingNeutral)
	}
}

func TestCompute_DynamicRatingClamped(t *testing.T) {
	cases := []struct {
		name       string
		rating     int
		wantRating int
		wantScore  int
	}{
		{"clamp_high_9_to_5", 9, RatingStrong, 100},
		{"clamp_low_neg1_to_1", -1, RatingMiss, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rubrics := []config.Rubric{dynRubric("x", 5)}
			r := Compute(&models.JobPosting{}, &models.Profile{}, rubrics, map[string]models.DynamicRating{"x": {Rating: tc.rating}})
			if r.Score != tc.wantScore {
				t.Errorf("input=%d Score=%d want %d", tc.rating, r.Score, tc.wantScore)
			}
			if len(r.Rubrics) != 1 || r.Rubrics[0].Rating != tc.wantRating {
				t.Errorf("input=%d rating=%d want %d", tc.rating, r.Rubrics[0].Rating, tc.wantRating)
			}
		})
	}
}

// --- no caps: a low dynamic rating pulls the average, never pins a cap ---

func TestCompute_NoCaps(t *testing.T) {
	// Job matches an avoided-tech list, but in the rubric model that only shows
	// up as a low rating on the "avoid_tech" rubric — no hard cap.
	prof := &models.Profile{PrefAvoidedTech: []string{"C#", ".NET"}}
	j := &models.JobPosting{TechStack: "Java, C#, Spring"}
	rubrics := []config.Rubric{
		dynRubric("avoid_tech", 5),
		dynRubric("great_fit", 5),
	}
	ratings := map[string]models.DynamicRating{"avoid_tech": {Rating: 1}, "great_fit": {Rating: 5}}
	r := Compute(j, prof, rubrics, ratings)
	// avg = (5·1 + 5·5)/10 = 3.0 → 60. Pure weighted average, no cap applied.
	if r.Score != 60 {
		t.Errorf("Score=%d want 60 (weighted average only, no cap)", r.Score)
	}
	// And critically, it must exceed the old 30-point deal-breaker cap.
	if r.Score <= 30 {
		t.Errorf("Score=%d should not be pinned to the old deal-breaker cap (30)", r.Score)
	}
}

// --- applies_to: conditional rubric skip ---

func TestCompute_AppliesToSkipsRemote(t *testing.T) {
	// location_proximity has applies_to: [hybrid, onsite]. For a REMOTE job it
	// is skipped entirely — excluded from numerator AND denominator — so a
	// perfect-5 job reaches 100 instead of being dragged toward 60.
	rubrics := []config.Rubric{
		dynRubric("a", 5),
		{ID: "location_proximity", Kind: "dynamic", Weight: 5, AppliesTo: []string{"hybrid", "onsite"}},
	}
	ratings := map[string]models.DynamicRating{"a": {Rating: 5}, "location_proximity": {Rating: 5}}
	remoteJob := &models.JobPosting{Location: "Remote"}
	r := Compute(remoteJob, &models.Profile{}, rubrics, ratings)
	if r.Score != 100 {
		t.Errorf("remote Score=%d want 100 (location_proximity skipped, only 'a' rated)", r.Score)
	}
	if len(r.Rubrics) != 1 || r.Rubrics[0].ID != "a" {
		t.Errorf("remote breakdown should omit location_proximity, got %v", r.Rubrics)
	}
}

func TestCompute_AppliesToKeepsForHybrid(t *testing.T) {
	// Same rubrics, but a HYBRID job → location_proximity IS scored.
	rubrics := []config.Rubric{
		dynRubric("a", 5),
		{ID: "location_proximity", Kind: "dynamic", Weight: 5, AppliesTo: []string{"hybrid", "onsite"}},
	}
	ratings := map[string]models.DynamicRating{"a": {Rating: 5}, "location_proximity": {Rating: 3}}
	hybridJob := &models.JobPosting{Location: "Hybrid · Toronto"}
	r := Compute(hybridJob, &models.Profile{}, rubrics, ratings)
	// avg = (5·5 + 5·3)/10 = 4.0 → 80. If location_proximity were wrongly
	// skipped, the score would be 100.
	if r.Score != 80 {
		t.Errorf("hybrid Score=%d want 80 (location_proximity rated 3, kept)", r.Score)
	}
	if len(r.Rubrics) != 2 {
		t.Errorf("hybrid breakdown should include both rubrics, got %d", len(r.Rubrics))
	}
}

func TestCompute_AppliesToUnknownArrangementKeeps(t *testing.T) {
	// When arrangement can't be detected (""), the rubric is NOT excluded —
	// we only drop when positively detected and not matching.
	rubrics := []config.Rubric{
		{ID: "loc", Kind: "dynamic", Weight: 5, AppliesTo: []string{"hybrid"}},
	}
	unknownJob := &models.JobPosting{Location: "New York, NY"} // no arrangement keyword
	r := Compute(unknownJob, &models.Profile{}, rubrics, map[string]models.DynamicRating{"loc": {Rating: 5}})
	if r.Score != 100 {
		t.Errorf("unknown-arrangement Score=%d want 100 (rubric kept, not excluded)", r.Score)
	}
}

func TestRubric_AppliesToArrangement(t *testing.T) {
	r := config.Rubric{AppliesTo: []string{"hybrid", "onsite"}}
	if !r.AppliesToArrangement("hybrid") {
		t.Error("hybrid should apply")
	}
	if !r.AppliesToArrangement("onsite") {
		t.Error("onsite should apply")
	}
	if r.AppliesToArrangement("remote") {
		t.Error("remote should NOT apply")
	}
	if !r.AppliesToArrangement("") {
		t.Error("unknown arrangement should apply (never exclude on uncertainty)")
	}
	empty := config.Rubric{}
	if !empty.AppliesToArrangement("remote") {
		t.Error("empty AppliesTo should apply to all")
	}
}

// --- edge cases ---

func TestCompute_EmptyRubrics(t *testing.T) {
	for _, rubrics := range [][]config.Rubric{nil, {}} {
		r := Compute(&models.JobPosting{}, &models.Profile{}, rubrics, nil)
		if r.Score != 0 {
			t.Errorf("nil/empty rubrics Score=%d want 0", r.Score)
		}
		if len(r.Rubrics) != 0 {
			t.Errorf("nil/empty rubrics should produce no rubric scores, got %d", len(r.Rubrics))
		}
	}
}

func TestCompute_NilJob(t *testing.T) {
	rubrics := []config.Rubric{dynRubric("a", 5)}
	r := Compute(nil, &models.Profile{}, rubrics, map[string]models.DynamicRating{"a": {Rating: 5}})
	if r.Score != 0 {
		t.Errorf("nil job Score=%d want 0", r.Score)
	}
	if len(r.Rubrics) != 0 {
		t.Errorf("nil job should produce no rubric scores, got %d", len(r.Rubrics))
	}
}

// TestCompute_DynamicReasonCarriedThrough verifies that an LLM-supplied reason
// for a dynamic rubric reaches the RubricScore result (so the UI/CLI can show
// why every rating landed where it did, not just the system rubrics).
func TestCompute_DynamicReasonCarriedThrough(t *testing.T) {
	rubrics := []config.Rubric{dynRubric("preferred_tech", 5)}
	ratings := map[string]models.DynamicRating{
		"preferred_tech": {Rating: 5, Reason: "stack matches Go + Postgres preferences"},
	}
	r := Compute(&models.JobPosting{}, &models.Profile{}, rubrics, ratings)
	if len(r.Rubrics) != 1 {
		t.Fatalf("want 1 rubric, got %d", len(r.Rubrics))
	}
	if r.Rubrics[0].Reason != "stack matches Go + Postgres preferences" {
		t.Errorf("dynamic reason not carried through, got %q", r.Rubrics[0].Reason)
	}
}

// --- FitReason rendering ---

func TestFitReason_RendersRatings(t *testing.T) {
	r := Result{
		Score: 75,
		Rubrics: []RubricScore{
			{ID: "salary", Kind: "system", Rating: 4, Weight: 5, Reason: "USD260k vs USD200k floor"},
			{ID: "team", Kind: "dynamic", Rating: 3, Weight: 3},
		},
	}
	got := FitReason(r)
	for _, want := range []string{
		"salary 4/5 (w5)",
		"team 3/5 (w3)",
		"| total 75",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("FitReason missing %q; got: %s", want, got)
		}
	}
}
