package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"linkedin-jobs/internal/config"
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

// dynamicRubrics builds a slice of config.Rubric with the given dynamic ids
// (Kind != "system") plus a system rubric to verify system rubrics are not
// rated by the LLM.
func dynamicRubrics(ids ...string) []config.Rubric {
	out := []config.Rubric{{ID: "salary", Kind: "system", Weight: 5, Description: "comp"}}
	for _, id := range ids {
		out = append(out, config.Rubric{ID: id, Kind: "dynamic", Weight: 5, Description: id + " fit"})
	}
	return out
}

func TestEnrich_ExtractsFacts(t *testing.T) {
	content := `{"company_overview":"Makes dev tools","industry":"DevTools","tech_stack":"Go,K8s","seniority":"staff","employment_type":"full-time","years_experience":7,"company_size_band":"11-50","company_stage":"early","is_founding_role":true,"visa_sponsorship":"yes","work_arrangement":"remote"}`
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Title: "Staff Eng", Description: "build stuff"}
	e, _, err := Enrich(j, p, dynamicRubrics("ai_intensity"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if e.CompanyOverview != "Makes dev tools" {
		t.Errorf("company_overview=%q", e.CompanyOverview)
	}
	if e.Industry != "DevTools" {
		t.Errorf("industry=%q", e.Industry)
	}
	if e.TechStack != "Go,K8s" {
		t.Errorf("tech_stack=%q", e.TechStack)
	}
	if e.Seniority != "staff" {
		t.Errorf("seniority=%q", e.Seniority)
	}
	if e.EmploymentType != "full-time" {
		t.Errorf("employment_type=%q", e.EmploymentType)
	}
	if e.YearsExperience == nil || *e.YearsExperience != 7 {
		t.Errorf("years_experience=%v want 7", e.YearsExperience)
	}
	if e.CompanySizeBand != "11-50" {
		t.Errorf("company_size_band=%q", e.CompanySizeBand)
	}
	if e.CompanyStage != "early" {
		t.Errorf("company_stage=%q", e.CompanyStage)
	}
	if !e.IsFoundingRole {
		t.Errorf("is_founding_role should be true")
	}
	if e.VisaSponsorship != "yes" {
		t.Errorf("visa_sponsorship=%q", e.VisaSponsorship)
	}
	if e.WorkArrangement != "remote" {
		t.Errorf("work_arrangement=%q", e.WorkArrangement)
	}
}

func TestEnrich_ReturnsRatings(t *testing.T) {
	content := `{"company_overview":"Acme","ratings":{"free_snacks":5,"ai_intensity":4}}`
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "d"}
	_, ratings, err := Enrich(j, p, dynamicRubrics("free_snacks", "ai_intensity"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if len(ratings) != 2 {
		t.Fatalf("ratings len=%d want 2: %v", len(ratings), ratings)
	}
	if ratings["free_snacks"] != 5 {
		t.Errorf("free_snacks=%d want 5", ratings["free_snacks"])
	}
	if ratings["ai_intensity"] != 4 {
		t.Errorf("ai_intensity=%d want 4", ratings["ai_intensity"])
	}
	// System rubric (salary) must never appear in the rating map.
	if _, ok := ratings["salary"]; ok {
		t.Errorf("system rubric 'salary' must not be rated by LLM")
	}
}

func TestEnrich_ClampsOutOfRangeRatings(t *testing.T) {
	content := `{"company_overview":"Acme","ratings":{"free_snacks":9,"ai_intensity":0}}`
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "d"}
	_, ratings, err := Enrich(j, p, dynamicRubrics("free_snacks", "ai_intensity"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if ratings["free_snacks"] != 5 {
		t.Errorf("free_snacks=%d want 5 (clamped)", ratings["free_snacks"])
	}
	if ratings["ai_intensity"] != 1 {
		t.Errorf("ai_intensity=%d want 1 (clamped)", ratings["ai_intensity"])
	}
}

func TestEnrich_NoRatingsKey(t *testing.T) {
	content := `{"company_overview":"Acme","industry":"DevTools"}`
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "d"}
	e, ratings, err := Enrich(j, p, dynamicRubrics("free_snacks"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if ratings != nil {
		t.Errorf("ratings should be nil when no ratings key, got %v", ratings)
	}
	if e.CompanyOverview != "Acme" {
		t.Errorf("company_overview=%q want Acme", e.CompanyOverview)
	}
	if e.Industry != "DevTools" {
		t.Errorf("industry=%q want DevTools", e.Industry)
	}
}

func TestEnrich_EmptyDescription(t *testing.T) {
	calls := 0
	srv, p := fakeCompletions(t, "{}", 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: ""}
	_, _, err := Enrich(j, p, dynamicRubrics("ai_intensity"))
	if err != ErrEmptyDescription {
		t.Fatalf("err = %v, want ErrEmptyDescription", err)
	}
	if calls != 0 {
		t.Errorf("empty description should make no API call, got %d", calls)
	}
}

func TestEnrich_DelimiterFallback(t *testing.T) {
	content := "company_overview: Foo || tech_stack: Go"
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "d"}
	e, ratings, err := Enrich(j, p, dynamicRubrics("ai_intensity"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if e.CompanyOverview != "Foo" {
		t.Errorf("company_overview=%q want Foo", e.CompanyOverview)
	}
	if e.TechStack != "Go" {
		t.Errorf("tech_stack=%q want Go", e.TechStack)
	}
	if ratings != nil {
		t.Errorf("ratings should be nil via delimiter fallback, got %v", ratings)
	}
}

func TestEnrich_FenceStripped(t *testing.T) {
	content := "```json\n{\"industry\":\"AI\",\"tech_stack\":\"Go\"}\n```"
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "d"}
	e, _, err := Enrich(j, p, dynamicRubrics("ai_intensity"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if e.Industry != "AI" {
		t.Errorf("industry=%q want AI", e.Industry)
	}
	if e.TechStack != "Go" {
		t.Errorf("tech_stack=%q want Go", e.TechStack)
	}
}

// TestEnrich_ExtractsSalaryRange confirms the LLM-extracted salary range
// survives the JSON path and lands on the Enrichment. Mirrors the real EvenUp
// posting shape: bare "$" range with a "Compensation Range:" label that the
// strict text regex missed because there was no badge currency to inherit.
func TestEnrich_ExtractsSalaryRange(t *testing.T) {
	content := `{"company_overview":"x","industry":"x","tech_stack":"x","seniority":"senior","employment_type":"full-time","years_experience":5,"company_size_band":"201-1000","company_stage":"growth","is_founding_role":false,"visa_sponsorship":"unknown","work_arrangement":"hybrid","salary_low":184728,"salary_high":249926,"salary_currency":"USD"}`
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "Compensation Range: $184,728 - $249,926"}
	e, _, err := Enrich(j, p, dynamicRubrics("ai_intensity"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if e.SalaryLow == nil || *e.SalaryLow != 184728 {
		t.Errorf("salary_low=%v want 184728", e.SalaryLow)
	}
	if e.SalaryHigh == nil || *e.SalaryHigh != 249926 {
		t.Errorf("salary_high=%v want 249926", e.SalaryHigh)
	}
	if e.SalaryCurrency != "USD" {
		t.Errorf("salary_currency=%q want USD", e.SalaryCurrency)
	}
}

// TestEnrich_NullSalaryStaysNil ensures "salary_low": null from the LLM
// (when the posting has no stated figure) does not overwrite an existing
// description-sourced salary downstream — i.e. the Enrichment comes back with
// both fields nil so the pipeline's "only override when LLM found something"
// guard skips the write.
func TestEnrich_NullSalaryStaysNil(t *testing.T) {
	content := `{"company_overview":"x","industry":"x","tech_stack":"x","seniority":"senior","salary_low":null,"salary_high":null,"salary_currency":""}`
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "competitive comp"}
	e, _, err := Enrich(j, p, dynamicRubrics("ai_intensity"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if e.SalaryLow != nil || e.SalaryHigh != nil {
		t.Errorf("nil LLM salary should stay nil; got low=%v high=%v", e.SalaryLow, e.SalaryHigh)
	}
	if e.SalaryCurrency != "" {
		t.Errorf("currency should be empty when no salary; got %q", e.SalaryCurrency)
	}
}

// TestEnrich_RejectsTinyLLMSalary guards against the LLM hallucinating tiny
// figures (e.g. an hourly rate misread as annual). Anything below 1000 is
// dropped so it can't pollute real salary data.
func TestEnrich_RejectsTinyLLMSalary(t *testing.T) {
	content := `{"salary_low":50,"salary_high":120,"salary_currency":"USD"}`
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "d"}
	e, _, err := Enrich(j, p, dynamicRubrics("ai_intensity"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if e.SalaryLow != nil || e.SalaryHigh != nil {
		t.Errorf("tiny LLM salary (<1000) should be dropped; got low=%v high=%v", e.SalaryLow, e.SalaryHigh)
	}
}

// TestEnrich_SinglePointSalaryMirroredToRange covers the case where the LLM
// returns only one side (e.g. "salary: $200,000" with no upper bound). The
// parser mirrors it so the caller has both a low and a high to persist.
func TestEnrich_SinglePointSalaryMirroredToRange(t *testing.T) {
	content := `{"salary_low":200000,"salary_high":null,"salary_currency":"USD"}`
	calls := 0
	srv, p := fakeCompletions(t, content, 200, &calls)
	defer srv.Close()
	j := &models.JobPosting{Description: "d"}
	e, _, err := Enrich(j, p, dynamicRubrics("ai_intensity"))
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if e.SalaryLow == nil || *e.SalaryLow != 200000 {
		t.Errorf("low=%v want 200000", e.SalaryLow)
	}
	if e.SalaryHigh == nil || *e.SalaryHigh != 200000 {
		t.Errorf("high=%v want 200000 (mirrored)", e.SalaryHigh)
	}
}
