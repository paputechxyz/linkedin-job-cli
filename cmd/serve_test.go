package cmd

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

// scoreForTest builds a jobView the way toJobView does, deriving the score tier
// through the real scoreClass() so the render path also exercises that mapping
// (rather than hardcoding the tier string and hiding a broken scoreClass). Pass
// a negative score to model an unscored job (Score/ScoreClass left empty).
func scoreForTest(score int, status string) jobView {
	v := jobView{
		ID:       "123",
		Title:    "Staff Engineer",
		Company:  "Vector AI",
		Location: "Toronto, ON",
		URL:      "https://example.com/job/123",
		Salary:   "CA$180-220k",
		Status:   status,
		Source:   "recommended",
		Remote:   "Remote",
		Industry: "AI/ML infra",
		Seniority: "Staff",
		EmpType:  "Full-time",
		Years:    "8+",
		Founding: "Founding",
	}
	if score >= 0 {
		v.Score = strconv.Itoa(score)
		v.ScoreClass = scoreClass(score)
	}
	return v
}

func TestScoreClass(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "low"}, {39, "low"}, {40, "mid"}, {69, "mid"},
		{70, "high"}, {92, "high"}, {100, "high"},
	}
	for _, c := range cases {
		if got := scoreClass(c.in); got != c.want {
			t.Errorf("scoreClass(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPageHTMLParses(t *testing.T) {
	if _, err := newPageTemplate(); err != nil {
		t.Fatalf("pageHTML failed to parse as html/template: %v", err)
	}
}

func TestRenderJobCardJSContract(t *testing.T) {
	tpl, err := newPageTemplate()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pd := pageData{
		CSRF: "csrf-token-abc",
		N:    4,
		F:    formVals{Sort: "score"},
		Mode: "list",
		Jobs: []jobView{
			scoreForTest(92, "new"),
			scoreForTest(47, "applied"),
			scoreForTest(-1, "new"), // unscored
			{ID: "999", Title: "Junior Dev", Status: "filtered", Source: "recommended"},
		},
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, pd); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()

	// Every required JS hook / attribute the <script> and handlers depend on
	// must survive the reskin. Renaming any of these silently breaks status
	// change, delete, and mark-viewed (KTD4). Assert each as a discrete token.
	mustContain := []string{
		`<meta name="csrf-token" content="csrf-token-abc"`,
		`class="job-head"`,          // mark-viewed: e.target.closest('.job-head a')
		`class="job job--high"`,     // high-tier card + base article.job selector
		`data-id="123"`,
		`data-id="999"`,             // data-id present on the filtered card too
		`data-status="new"`,
		`data-status="filtered"`,
		`class="status status--new js-status"`, // setStatusUI queries .js-status
		`class="js-status-select"`,
		`class="btn-delete js-delete"`,
		`class="js-delete-form"`,
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing required JS-contract token %q", want)
		}
	}

	// CSRF must round-trip into both the meta tag and one delete-form hidden
	// field per job (including the filtered card, which still has a delete form).
	if got := strings.Count(out, `name="csrf" value="csrf-token-abc"`); got != len(pd.Jobs) {
		t.Errorf("csrf hidden field count = %d, want %d (one per delete form)", got, len(pd.Jobs))
	}

	// R1: the high-fit card gets the reserved HIGH-tier treatment and the
	// larger badge; the unscored card gets the "none" badge and no job--high.
	if !strings.Contains(out, `score-badge--high`) {
		t.Error("high-score card missing score-badge--high class (R1 hierarchy)")
	}
	if !strings.Contains(out, `score-badge--none"`) {
		t.Error("unscored card missing score-badge--none badge class")
	}

	// R2: status pop is wired (status--new pill present).
	if !strings.Contains(out, `status--new`) {
		t.Error("status pill class status--new missing (R2 status pop)")
	}

	// The filtered card must NOT render a status <select> (it shows the
	// "filtered (auto)" tag instead), and mark-viewed must no-op on it.
	filteredStart := strings.Index(out, `data-status="filtered"`)
	if filteredStart < 0 {
		t.Fatal("filtered card not found")
	}
	articleOpen := strings.LastIndex(out[:filteredStart], "<article ")
	nextArticle := strings.Index(out[filteredStart:], "</article>")
	if articleOpen < 0 || nextArticle < 0 {
		t.Fatal("could not locate filtered <article> bounds")
	}
	filteredCard := out[articleOpen : filteredStart+nextArticle]
	if strings.Contains(filteredCard, "js-status-select") {
		t.Error("filtered card must not render a status <select>")
	}
	if !strings.Contains(filteredCard, "filtered (auto)") {
		t.Error(`filtered card missing "filtered (auto)" tag`)
	}
}

func TestRenderEmptyAndErrorStates(t *testing.T) {
	tpl, err := newPageTemplate()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t.Run("empty jobs renders empty state", func(t *testing.T) {
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, pageData{CSRF: "t", F: formVals{Sort: "score"}, Mode: "list"}); err != nil {
			t.Fatalf("execute: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "empty-state") {
			t.Error("empty-state markup missing when no jobs")
		}
		if !strings.Contains(out, "No jobs found.") {
			t.Error(`empty state missing "No jobs found." copy`)
		}
		if !strings.Contains(out, `<meta name="csrf-token"`) {
			t.Error("csrf meta missing")
		}
	})
	t.Run("error state renders the error", func(t *testing.T) {
		var buf bytes.Buffer
		pd := pageData{CSRF: "t", F: formVals{Q: "oops", Sort: "score"}, Mode: "search", Error: "fts syntax"}
		if err := tpl.Execute(&buf, pd); err != nil {
			t.Fatalf("execute: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "Search error: fts syntax") {
			t.Error("error state missing the rendered error text")
		}
	})
}
