package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linkedin-jobs/internal/models"
)

func iptr(i int) *int { return &i }

// fakeCompletions returns a provider pointing at a test server that replies
// with the given content (or status != 200). The handler records call count via
// *calls.
func fakeCompletions(t *testing.T, content string, status int, calls *int) (*httptest.Server, *Provider) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if status != 200 {
			// Body > 256 bytes with the token near the end so truncation must cut it.
			padding := strings.Repeat("x", 280)
			http.Error(w, padding+" token-abcdef123456 should-be-cut", status)
			return
		}
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": content}},
			},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	return srv, &Provider{BaseURL: srv.URL, APIKey: "k", Model: "m"}
}

func TestScore_HappyPathJSON(t *testing.T) {
	content := `{"company_overview":"Makes dev tools","industry":"DevTools","tech_stack":"Go,K8s","seniority":"staff","employment_type":"full-time","years_experience":7,"company_size_band":"11-50","company_stage":"early","is_founding_role":true,"visa_sponsorship":"yes","work_arrangement":"remote","fit_score":85,"fit_reason":"Strong Go fit"}`
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Title: "Staff Eng", Description: "build stuff"}
	e, err := Score(j, nil, p, 70)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if e.CompanyOverview != "Makes dev tools" {
		t.Errorf("company_overview=%q", e.CompanyOverview)
	}
	if e.TechStack != "Go,K8s" {
		t.Errorf("tech_stack=%q", e.TechStack)
	}
	if e.Seniority != "staff" {
		t.Errorf("seniority=%q", e.Seniority)
	}
	if !e.IsFoundingRole {
		t.Errorf("is_founding_role should be true")
	}
	if e.WorkArrangement != "remote" {
		t.Errorf("work_arrangement=%q", e.WorkArrangement)
	}
	if e.FitScore == nil || *e.FitScore != 85 {
		t.Errorf("fit_score=%+v want 85", e.FitScore)
	}
	if e.FitReason == "" {
		t.Errorf("fit_reason should be populated above threshold")
	}
}

func TestScore_JSONWrappedInFence(t *testing.T) {
	content := "```json\n{\"industry\":\"AI\",\"fit_score\":90,\"fit_reason\":\"great\"}\n```"
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "d"}
	e, err := Score(j, nil, p, 70)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if e.Industry != "AI" {
		t.Errorf("industry=%q", e.Industry)
	}
}

func TestScore_DelimiterFallback(t *testing.T) {
	content := "Company: Acme || Stack: Go,React || Score: 65 || Reason: ok match || Remote: onsite"
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "d"}
	e, err := Score(j, nil, p, 70)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if e.CompanyOverview != "Acme" {
		t.Errorf("company_overview=%q want Acme", e.CompanyOverview)
	}
	if e.TechStack != "Go,React" {
		t.Errorf("tech_stack=%q", e.TechStack)
	}
	if e.WorkArrangement != "onsite" {
		t.Errorf("work_arrangement=%q want onsite", e.WorkArrangement)
	}
	if e.FitScore == nil || *e.FitScore != 65 {
		t.Errorf("fit_score=%+v want 65", e.FitScore)
	}
	// Score below threshold -> reason should be cleared.
	if e.FitReason != "" {
		t.Errorf("fit_reason should be cleared below threshold, got %q", e.FitReason)
	}
}

func TestScore_UnparseableYieldsPartialWithoutError(t *testing.T) {
	content := "Just some random prose about a job with no labels or JSON structure at all."
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "d"}
	e, err := Score(j, nil, p, 70)
	if err != nil {
		t.Errorf("unparseable response should not error, got %v", err)
	}
	if e.FitScore != nil {
		t.Errorf("expected no score from unstructured prose, got %+v", e.FitScore)
	}
}

func TestScore_EmptyDescriptionMakesNoCall(t *testing.T) {
	calls := 0
	srv, p := fakeCompletions(t, "{}", 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: ""}
	e, err := Score(j, nil, p, 70)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if calls != 0 {
		t.Errorf("empty description should make no API call, got %d", calls)
	}
	_ = e
}

func TestScore_TransportFailureReturnsError(t *testing.T) {
	calls := 0
	srv, p := fakeCompletions(t, "", 500, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "d"}
	_, err := Score(j, nil, p, 70)
	if err == nil {
		t.Fatalf("want error on HTTP 500")
	}
	// Error body must be truncated and single-line (no reflected token/newline leak).
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("error should be single-line: %q", err.Error())
	}
	if strings.Contains(err.Error(), "abcdef123456") {
		t.Errorf("full upstream body leaked into error: %q", err.Error())
	}
}

func TestNormalizeArrangement(t *testing.T) {
	cases := map[string]string{
		"Remote": "remote", "ON-SITE": "onsite", "in-office": "onsite",
		"Hybrid": "hybrid", "": "", "nonsense": "",
	}
	for in, want := range cases {
		if got := normalizeArrangement(in); got != want {
			t.Errorf("normalizeArrangement(%q)=%q want %q", in, got, want)
		}
	}
}
