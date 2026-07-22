package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/store"
)

func TestCompanyOrDash(t *testing.T) {
	if got := companyOrDash(""); got != "—" {
		t.Errorf("empty company: got %q, want '—'", got)
	}
	if got := companyOrDash("Acme"); got != "Acme" {
		t.Errorf("non-empty: got %q", got)
	}
}

// --- enrichAndScoreBatch (parallel batching) ---

// tmpCmdStore opens a store at a temp path for cmd-package tests.
func tmpCmdStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// TestEnrichAndScoreBatch_AllProcessed verifies every job gets a callback
// (success or error) and the scored count is correct. Uses empty-description
// jobs so llm.Enrich returns ErrEmptyDescription without any HTTP call, making
// the test fast and provider-free. Run with -race to verify goroutine safety.
func TestEnrichAndScoreBatch_AllProcessed(t *testing.T) {
	st := tmpCmdStore(t)
	const n = 7 // exercises a partial final batch (7 > batch 5)
	jobs := make([]*models.JobPosting, n)
	for i := 0; i < n; i++ {
		j := &models.JobPosting{
			ID:         fmt.Sprintf("job-%d", i),
			Title:      fmt.Sprintf("Job %d", i),
			URL:        "https://example.com",
			SearchedAt: "now",
			// Description left empty → ErrEmptyDescription, no HTTP call.
		}
		if err := st.Upsert(j); err != nil {
			t.Fatalf("Upsert: %v", err)
		}
		jobs[i] = j
	}

	var mu sync.Mutex
	var processed []string
	scored := enrichAndScoreBatch(st, jobs, nil, nil, nil, 5, 0, func(idx, total int, j *models.JobPosting, err error) {
		mu.Lock() // redundant (callback already serialized) but demonstrates safety
		processed = append(processed, j.ID)
		mu.Unlock()
	})

	if scored != 0 {
		t.Errorf("scored = %d, want 0 (all empty descriptions error)", scored)
	}
	if len(processed) != n {
		t.Errorf("callbacks = %d, want %d", len(processed), n)
	}
	// Every job ID should appear exactly once.
	seen := map[string]int{}
	for _, id := range processed {
		seen[id]++
	}
	if len(seen) != n {
		t.Errorf("unique jobs processed = %d, want %d", len(seen), n)
	}
}

// TestEnrichAndScoreBatch_SequentialFallback verifies concurrency=1 processes
// all jobs (reverts to sequential, one goroutine per batch).
func TestEnrichAndScoreBatch_SequentialFallback(t *testing.T) {
	st := tmpCmdStore(t)
	jobs := make([]*models.JobPosting, 3)
	for i := 0; i < 3; i++ {
		j := &models.JobPosting{
			ID:         fmt.Sprintf("seq-%d", i),
			Title:      fmt.Sprintf("Seq %d", i),
			URL:        "https://example.com",
			SearchedAt: "now",
		}
		st.Upsert(j)
		jobs[i] = j
	}

	var count int
	enrichAndScoreBatch(st, jobs, nil, nil, nil, 1, 0, func(idx, total int, j *models.JobPosting, err error) {
		count++
	})
	if count != 3 {
		t.Errorf("callbacks = %d, want 3 (concurrency=1)", count)
	}
}

func TestScoringProvider_MissingProviderReturnsSetupError(t *testing.T) {
	_, err := scoringProvider(nil, llm.ErrNoProvider)
	if err == nil {
		t.Fatal("expected an error when no provider resolves, got nil")
	}
	// The error must steer the user at the same setup knobs doctor/setup advertise.
	for _, knob := range []string{"OPENAI_API_KEY", "LJ_LLM", "ANTHROPIC_API_KEY"} {
		if !strings.Contains(err.Error(), knob) {
			t.Errorf("error %q should mention setup knob %q", err.Error(), knob)
		}
	}
}

func TestScoringProvider_NilProviderNeverPassesSilently(t *testing.T) {
	// A nil provider with no resolve error must still yield a setup error — the
	// policy never lets a nil provider through to the scoring loop.
	_, err := scoringProvider(nil, nil)
	if err == nil {
		t.Fatal("expected an error for a nil provider even when resolve returned no error")
	}
}

func TestScoringProvider_ResolvedProviderPassesThrough(t *testing.T) {
	p := &llm.Provider{BaseURL: "https://example.com", Model: "x", Source: "test"}
	got, err := scoringProvider(p, nil)
	if err != nil {
		t.Fatalf("expected no error for a resolved provider, got %v", err)
	}
	if got != p {
		t.Error("expected the same provider pointer to be returned unchanged")
	}
}
