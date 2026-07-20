package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/score"
	"linkedin-jobs/internal/store"
)

func openTempStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func fakeScoreServer(t *testing.T, scoreJSON string) (*httptest.Server, *llm.Provider) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": scoreJSON}},
			},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	return srv, &llm.Provider{BaseURL: srv.URL, APIKey: "k", Model: "m"}
}

// TestEnrichAndScoreJob_PersistsWeightedScore runs one job through the new
// pipeline with a system salary rubric + a dynamic rubric, faking an LLM
// response that returns facts and a rating for the dynamic rubric. The fit
// score is the weighted-average of the Go-computed salary rating and the
// LLM-supplied dynamic rating.
func TestEnrichAndScoreJob_PersistsWeightedScore(t *testing.T) {
	st := openTempStore(t)
	enrichJSON := `{"company_overview":"Makes dev tools","industry":"DevTools","tech_stack":"Go","seniority":"staff","work_arrangement":"remote","ratings":{"growth":4}}`
	srv, provider := fakeScoreServer(t, enrichJSON)
	defer srv.Close()

	rubrics := []config.Rubric{
		{ID: config.RubricSalary, Kind: "system", Weight: 5, Description: "Salary level relative to your floor"},
		{ID: "growth", Kind: "dynamic", Weight: 5, Description: "Career growth potential"},
	}
	floor := 100000.0
	high := 150000.0
	prof := &models.Profile{PrefMinSalary: &floor, PrefMinSalaryCurrency: "USD"}
	j := &models.JobPosting{
		ID: "x", Title: "Staff Eng", Company: "Acme", Location: "Remote",
		Description: "Build Go services", SearchedAt: "2026-06-28",
		SalaryCurrency: "USD", SalaryHigh: &high,
	}
	if err := st.Upsert(j); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := enrichAndScoreJob(st, j, prof, provider, rubrics); err != nil {
		t.Fatalf("enrichAndScoreJob: %v", err)
	}

	if j.FitScore == nil {
		t.Fatalf("FitScore should be set; got nil")
	}
	if *j.FitScore < 0 || *j.FitScore > 100 {
		t.Errorf("FitScore=%d out of [0,100]", *j.FitScore)
	}

	if j.RubricScores == "" {
		t.Fatalf("RubricScores should be non-empty JSON")
	}
	var rs []score.RubricScore
	if err := json.Unmarshal([]byte(j.RubricScores), &rs); err != nil {
		t.Fatalf("unmarshal RubricScores: %v", err)
	}
	if len(rs) != len(rubrics) {
		t.Errorf("RubricScores length=%d want %d (one entry per rubric)", len(rs), len(rubrics))
	}

	if !strings.Contains(j.FitReason, "growth") {
		t.Errorf("FitReason=%q should contain the dynamic rubric id 'growth'", j.FitReason)
	}
	if !strings.Contains(j.FitReason, "total") {
		t.Errorf("FitReason=%q should contain 'total'", j.FitReason)
	}

	got, err := st.Get("x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FitScore == nil {
		t.Errorf("persisted FitScore should be set")
	}
}

// TestEnrichAndScoreJob_EmptyDescriptionSkipped verifies that a job with no
// description body is rejected by llm.Enrich before any HTTP call or DB write,
// surfacing llm.ErrEmptyDescription and leaving the score unset.
func TestEnrichAndScoreJob_EmptyDescriptionSkipped(t *testing.T) {
	st := openTempStore(t)
	srv, provider := fakeScoreServer(t, "{}")
	defer srv.Close()

	rubrics := []config.Rubric{
		{ID: config.RubricSalary, Kind: "system", Weight: 5},
	}
	j := &models.JobPosting{ID: "empty", Title: "No Desc", Company: "Acme", Description: ""}
	if err := st.Upsert(j); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := enrichAndScoreJob(st, j, nil, provider, rubrics); err != llm.ErrEmptyDescription {
		t.Fatalf("err=%v want llm.ErrEmptyDescription", err)
	}
	if j.FitScore != nil {
		t.Errorf("FitScore should stay nil; got %d", *j.FitScore)
	}
}

// TestEnrichAndScoreJob_NoCaps documents the removal of score caps: a job whose
// stack contains an avoided tech (rated 1 on a dynamic avoid_tech rubric) is
// scored purely by the weighted average. Under the old deal-breaker-cap system
// this would have been pinned to 30; now avoid_tech just contributes its low
// rating and the average (60 here) wins.
func TestEnrichAndScoreJob_NoCaps(t *testing.T) {
	st := openTempStore(t)
	enrichJSON := `{"tech_stack":"PHP,Go","ratings":{"avoid_tech":1,"growth":5}}`
	srv, provider := fakeScoreServer(t, enrichJSON)
	defer srv.Close()

	rubrics := []config.Rubric{
		{ID: "avoid_tech", Kind: "dynamic", Weight: 5, Description: "Penalize avoided tech", Items: []string{"PHP"}},
		{ID: "growth", Kind: "dynamic", Weight: 5, Description: "Career growth"},
	}
	prof := &models.Profile{PrefAvoidedTech: []string{"PHP"}}
	j := &models.JobPosting{
		ID: "php", Title: "PHP Dev", Company: "LegacyCo", Location: "Remote",
		Description: "Maintain a PHP monolith alongside some Go services.",
	}
	if err := st.Upsert(j); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := enrichAndScoreJob(st, j, prof, provider, rubrics); err != nil {
		t.Fatalf("enrichAndScoreJob: %v", err)
	}
	if j.FitScore == nil {
		t.Fatalf("FitScore should be set")
	}
	got := *j.FitScore
	if got != 60 {
		t.Errorf("FitScore=%d want 60 (weighted average: (5*1+5*5)/10 -> 3/5*100)", got)
	}
	if got == 30 {
		t.Errorf("FitScore should NOT be pinned to the old deal-breaker cap of 30")
	}
}

// TestEnrichAndScoreJob_LLMFillsMissingSalary covers the EvenUp / Scribd
// regression: the description states a salary range that the strict text
// extractor couldn't parse (no badge currency to inherit, so labeledBareRangeRE
// never fired). The LLM extraction should fill in salary_low/high and mark the
// source as "description" so the UI treats it as authoritative, not estimated.
func TestEnrichAndScoreJob_LLMFillsMissingSalary(t *testing.T) {
	st := openTempStore(t)
	enrichJSON := `{"company_overview":"x","industry":"x","tech_stack":"x","seniority":"senior","work_arrangement":"hybrid","salary_low":184728,"salary_high":249926,"salary_currency":"USD"}`
	srv, provider := fakeScoreServer(t, enrichJSON)
	defer srv.Close()

	j := &models.JobPosting{
		ID:          "evenup",
		Title:       "Senior SE",
		Company:     "EvenUp",
		Location:    "Toronto",
		Description: "Compensation Range: $184,728 - $249,926",
		// SalaryLow/High/Source all empty, mirroring a job the scraper couldn't parse.
	}
	if err := st.Upsert(j); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := enrichAndScoreJob(st, j, nil, provider, nil); err != nil {
		t.Fatalf("enrichAndScoreJob: %v", err)
	}
	if j.SalaryLow == nil || *j.SalaryLow != 184728 {
		t.Errorf("SalaryLow=%v want 184728", j.SalaryLow)
	}
	if j.SalaryHigh == nil || *j.SalaryHigh != 249926 {
		t.Errorf("SalaryHigh=%v want 249926", j.SalaryHigh)
	}
	if j.SalaryCurrency != "USD" {
		t.Errorf("SalaryCurrency=%q want USD", j.SalaryCurrency)
	}
	if j.SalarySource != models.SalarySourceDescription {
		t.Errorf("SalarySource=%q want %q", j.SalarySource, models.SalarySourceDescription)
	}

	// Persisted to the DB, not just in-memory.
	got, err := st.Get("evenup")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SalaryHigh == nil || *got.SalaryHigh != 249926 {
		t.Errorf("persisted SalaryHigh=%v want 249926", got.SalaryHigh)
	}
	if got.SalarySource != models.SalarySourceDescription {
		t.Errorf("persisted SalarySource=%q want description", got.SalarySource)
	}
}

// TestEnrichAndScoreJob_TextDescriptionSalaryNotOverriddenByLLM confirms the
// cross-check rule: when the scraper already extracted a description-sourced
// salary (the high-confidence path), an LLM-extracted range does NOT overwrite
// it even if the LLM returned different numbers. The strict regex's
// currency-stated rules are more reliable than LLM currency inference.
func TestEnrichAndScoreJob_TextDescriptionSalaryNotOverriddenByLLM(t *testing.T) {
	st := openTempStore(t)
	// LLM claims a different (wrong) range — must be ignored.
	enrichJSON := `{"salary_low":99999,"salary_high":111111,"salary_currency":"EUR"}`
	srv, provider := fakeScoreServer(t, enrichJSON)
	defer srv.Close()

	low, high := 200000.0, 250000.0
	j := &models.JobPosting{
		ID:            "txt",
		Title:         "SE",
		Description:   "Salary: $200,000 - $250,000 CAD",
		SalaryLow:     &low,
		SalaryHigh:    &high,
		SalaryCurrency: "CAD",
		SalarySource:  models.SalarySourceDescription,
	}
	if err := st.Upsert(j); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := enrichAndScoreJob(st, j, nil, provider, nil); err != nil {
		t.Fatalf("enrichAndScoreJob: %v", err)
	}
	if j.SalaryLow == nil || *j.SalaryLow != 200000 {
		t.Errorf("SalaryLow=%v want 200000 (text-extraction preserved)", j.SalaryLow)
	}
	if j.SalaryHigh == nil || *j.SalaryHigh != 250000 {
		t.Errorf("SalaryHigh=%v want 250000 (text-extraction preserved)", j.SalaryHigh)
	}
	if j.SalaryCurrency != "CAD" {
		t.Errorf("SalaryCurrency=%q want CAD (text-extraction preserved)", j.SalaryCurrency)
	}
}

// TestEnrichAndScoreJob_LLMOverridesBadgeSalary checks the priority rule:
// description-stated salary overrides the LinkedIn page-chrome badge. The job
// starts with badge-sourced salary (low confidence); the LLM extraction from
// the description body should override it and re-tag the source as
// "description".
func TestEnrichAndScoreJob_LLMOverridesBadgeSalary(t *testing.T) {
	st := openTempStore(t)
	enrichJSON := `{"salary_low":184728,"salary_high":249926,"salary_currency":"USD"}`
	srv, provider := fakeScoreServer(t, enrichJSON)
	defer srv.Close()

	badgeLow, badgeHigh := 150000.0, 180000.0
	j := &models.JobPosting{
		ID:            "badge",
		Title:         "SE",
		Description:   "Range: $184,728 - $249,926 (LLM sees this, regex didn't).",
		SalaryLow:     &badgeLow,
		SalaryHigh:    &badgeHigh,
		SalaryCurrency: "USD",
		SalarySource:  models.SalarySourceBadge,
	}
	if err := st.Upsert(j); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := enrichAndScoreJob(st, j, nil, provider, nil); err != nil {
		t.Fatalf("enrichAndScoreJob: %v", err)
	}
	if j.SalaryLow == nil || *j.SalaryLow != 184728 {
		t.Errorf("SalaryLow=%v want 184728 (LLM description overrides badge)", j.SalaryLow)
	}
	if j.SalaryHigh == nil || *j.SalaryHigh != 249926 {
		t.Errorf("SalaryHigh=%v want 249926 (LLM description overrides badge)", j.SalaryHigh)
	}
	if j.SalarySource != models.SalarySourceDescription {
		t.Errorf("SalarySource=%q want description (LLM lifted source confidence)", j.SalarySource)
	}
}
