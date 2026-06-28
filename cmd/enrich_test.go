package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"linkedin-jobs/internal/filter"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/models"
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

// TestGateSequence exercises dedup -> filter -> score end to end against a real
// store and a fake provider.
func TestGateSequence_DedupFilterScore(t *testing.T) {
	st := openTempStore(t)
	scoreJSON := `{"industry":"DevTools","tech_stack":"Go","seniority":"staff","work_arrangement":"remote","fit_score":85,"fit_reason":"great"}`
	srv, provider := fakeScoreServer(t, scoreJSON)
	defer srv.Close()

	// 1. New candidate job -> not yet a duplicate.
	j := &models.JobPosting{ID: "x", Title: "Staff Eng", Company: "Acme", Location: "Remote",
		Description: "Build Go services", SearchedAt: "2026-06-28"}
	j.ContentHash = store.ContentHash(j.Company, j.Title, j.Description, j.ListedAt)
	st.Upsert(j)
	if st.IsDuplicateEnriched(j.ContentHash) {
		t.Fatalf("fresh job should not be an enriched duplicate")
	}

	// 2. Score it -> persists structured fields + score.
	if _, err := enrichAndScoreJob(st, j, nil, provider, 70); err != nil {
		t.Fatalf("enrichAndScoreJob: %v", err)
	}
	got, _ := st.Get("x")
	if got.Industry != "DevTools" || got.FitScore == nil || *got.FitScore != 85 {
		t.Errorf("not persisted: industry=%q score=%+v", got.Industry, got.FitScore)
	}
	if got.FitReason != "great" {
		t.Errorf("fit_reason=%q want great", got.FitReason)
	}

	// 3. Now it IS an enriched duplicate -> a re-fetch would skip the LLM.
	if !st.IsDuplicateEnriched(j.ContentHash) {
		t.Errorf("enriched job should count as a duplicate-for-skip")
	}

	// 4. A clear mismatch fails the hard filter and is tagged filtered/0.
	bad := &models.JobPosting{ID: "y", Title: "Eng", Company: "X", Location: "New York, NY (On-site)",
		Description: "On-site role", SearchedAt: "2026-06-28"}
	st.Upsert(bad)
	profile := &models.Profile{PrefWorkArrangement: "remote"}
	if filter.PassesHardFilter(bad, profile) {
		t.Errorf("on-site job should fail the remote-required hard filter")
	}
	st.SetFiltered("y")
	got2, _ := st.Get("y")
	if !got2.IsFiltered() {
		t.Errorf("expected status=filtered")
	}
	// Filtered jobs are hidden from the default list.
	listed, _ := st.List(store.Filters{})
	for _, lj := range listed {
		if lj.ID == "y" {
			t.Errorf("filtered job should be hidden from default list")
		}
	}
}
