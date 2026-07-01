package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/filter"
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

// TestGateSequence exercises dedup -> score end to end against a real store and
// a fake provider. With the rubric scorer, the LLM only extracts facts; the
// fit_score is derived deterministically by score.Compute.
func TestGateSequence_DedupEnrichScore(t *testing.T) {
	st := openTempStore(t)
	enrichJSON := `{"industry":"DevTools","tech_stack":"Go","seniority":"staff","work_arrangement":"remote","ai_intensity":"core"}`
	srv, provider := fakeScoreServer(t, enrichJSON)
	defer srv.Close()

	// 1. New candidate job -> not yet a duplicate.
	j := &models.JobPosting{ID: "x", Title: "Staff Eng", Company: "Acme", Location: "Remote",
		Description: "Build Go services", SearchedAt: "2026-06-28"}
	j.ContentHash = store.ContentHash(j.Company, j.Title, j.Description, j.ListedAt)
	st.Upsert(j)
	if st.IsDuplicateEnriched(j.ContentHash) {
		t.Fatalf("fresh job should not be an enriched duplicate")
	}

	// 2. Enrich + score -> persists extracted facts + computed score.
	weights := score.FromSettings(config.DefaultScoringSettings())
	if err := enrichAndScoreJob(st, j, nil, provider, weights); err != nil {
		t.Fatalf("enrichAndScoreJob: %v", err)
	}
	got, _ := st.Get("x")
	if got.Industry != "DevTools" {
		t.Errorf("industry=%q want DevTools", got.Industry)
	}
	if got.AIIntensity != "core" {
		t.Errorf("ai_intensity=%q want core", got.AIIntensity)
	}
	if got.FitScore == nil {
		t.Errorf("FitScore should be set; got nil")
	}
	if got.ScoreCapReason != "" {
		t.Errorf("no cap expected for a passing job; got %q", got.ScoreCapReason)
	}
	// Baseline (60) + AI core (5) — no profile means salary/tech/remote/etc are 0.
	if got.FitScore == nil {
		t.Errorf("FitScore should be set; got nil")
	} else if *got.FitScore != 65 {
		t.Errorf("FitScore=%d want 65 (baseline 60 + AI core 5; no remote bonus without a profile)", *got.FitScore)
	}

	// 3. Now it IS an enriched duplicate -> a re-fetch would skip the LLM.
	if !st.IsDuplicateEnriched(j.ContentHash) {
		t.Errorf("enriched job should count as a duplicate-for-skip")
	}

	// 4. Cap-not-hide: a job that fails the hard filter is now visible (status
	// stays "new") and gets a cap reason instead of being zeroed + hidden.
	bad := &models.JobPosting{ID: "y", Title: "Eng", Company: "X", Location: "New York, NY (On-site)",
		Description: "On-site role", SearchedAt: "2026-06-28"}
	st.Upsert(bad)
	profile := &models.Profile{PrefWorkArrangement: []string{"remote"}}
	if filter.PassesHardFilter(bad, profile) {
		t.Errorf("on-site job should fail the remote-required hard filter")
	}
	// Compute + persist the cap (mirrors what the ingest loop does for filter failures).
	res := score.Compute(bad, profile, weights)
	if err := st.SetScore(bad.ID, res.Score, score.FitReason(res), res.CapReason); err != nil {
		t.Fatalf("SetScore: %v", err)
	}
	got2, _ := st.Get("y")
	if got2.IsFiltered() {
		t.Errorf("cap-not-hide: status should NOT be 'filtered'")
	}
	if got2.ScoreCapReason != score.CapNonRemote {
		t.Errorf("cap reason=%q want %q", got2.ScoreCapReason, score.CapNonRemote)
	}
	if got2.FitScore == nil || *got2.FitScore != 55 {
		t.Errorf("FitScore=%+v want 55 (non-remote cap)", got2.FitScore)
	}
}
