package cmd

import (
	"bytes"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"linkedin-jobs/internal/models"
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

// TestRenderFitReasonStars checks the fit-reason reskin: when structured
// RubricScores is present, the expandable block shows the full per-rubric
// breakdown (id + star bar + rating/weight + reason). When absent, it falls
// back to the flat FitReason.
func TestRenderFitReasonStars(t *testing.T) {
	tpl, err := newPageTemplate()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	withStars := scoreForTest(95, "new")
	withStars.FitReason = "salary 5/5 (w5) CAD297864 vs CAD200000 floor, +49%, work_arrangement 5/5 (w5) remote | total 95"
	withStars.Rubrics = []rubricView{
		{ID: "salary", Stars: "★★★★★", Rating: 5, Weight: 5, Reason: "CAD297864 vs CAD200000 floor, +49%"},
		{ID: "work_arrangement", Stars: "★★★★★", Rating: 5, Weight: 5, Reason: "remote"},
		{ID: "preferred_tech", Stars: "★★★★☆", Rating: 4, Weight: 5},
	}
	// Legacy job: no RubricScores, only the flat one-liner — must fall back.
	legacy := scoreForTest(63, "new")
	legacy.FitReason = "salary 3/5 (w5) no floor/salary | total 63"

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, pageData{CSRF: "t", F: formVals{Sort: "score"}, Jobs: []jobView{withStars, legacy}}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()

	// Full breakdown: rubric list with id + star bar + rating/weight annotation.
	for _, want := range []string{
		`class="rubric-list"`,
		`class="rubric-id">salary<`,
		`class="rubric-stars"`,
		`>★★★★★</span>`,                      // salary star bar
		`>5/5 · w5 · CAD297864`,               // salary rating/weight/reason
		`class="rubric-id">work_arrangement<`, // long id rendered, not truncated
		`>4/5 · w5<`,                          // preferred_tech has no reason → no trailing " · "
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
	// A rubric with no reason must not emit a dangling " · ".
	if strings.Contains(out, `>4/5 · w5 · <`) {
		t.Errorf(`rubric without reason rendered dangling " · " (got "4/5 · w5 · ")`)
	}
	// Legacy fallback: flat FitReason rendered in the details block.
	if !strings.Contains(out, `salary 3/5 (w5) no floor/salary | total 63`) {
		t.Error("legacy job should fall back to the flat FitReason line in details")
	}
	// The compact score blurb is intentionally not rendered next to the badge.
	if strings.Contains(out, `class="score-blurb`) {
		t.Error("score blurb must not be rendered next to the score badge")
	}
}

func TestRenderDescriptionShortDescription(t *testing.T) {
	tpl, err := newPageTemplate()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Enriched job: ShortDescription is shown, full Description is hidden.
	enriched := scoreForTest(80, "new")
	enriched.ShortDescription = "Own the payments service.\n\n8+ years Go and Postgres."
	enriched.Description = "RAW FULL BODY THAT MUST NOT APPEAR"
	// Un-enriched job: no ShortDescription → fall back to full Description.
	legacy := scoreForTest(70, "new")
	legacy.Description = "Legacy full description excerpt as fallback."

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, pageData{CSRF: "t", F: formVals{Sort: "score"}, Jobs: []jobView{enriched, legacy}}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "Own the payments service.") {
		t.Error("enriched job should render its ShortDescription")
	}
	if strings.Contains(out, "RAW FULL BODY THAT MUST NOT APPEAR") {
		t.Error("enriched job must NOT render the full Description body when ShortDescription is present")
	}
	if !strings.Contains(out, "Legacy full description excerpt as fallback.") {
		t.Error("un-enriched job should fall back to full Description")
	}
	// The copy button was removed and must not come back.
	if strings.Contains(out, "btn-copy") || strings.Contains(out, "js-copy") {
		t.Error("copy button must not be rendered")
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

func TestHasFilterParams(t *testing.T) {
	if hasFilterParams(url.Values{}) {
		t.Error("empty query should not count as a filter submit")
	}
	if hasFilterParams(url.Values{"clear": {"1"}}) {
		t.Error(`?clear=1 alone must not be treated as an Apply (it's a reset)`)
	}
	for _, k := range filterParamKeys {
		if !hasFilterParams(url.Values{k: {"x"}}) {
			t.Errorf("param %q should count as a filter submit", k)
		}
	}
}

func TestFormValsRoundTrip(t *testing.T) {
	original := formVals{
		Q: "staff", Company: "Acme", Title: "eng", Location: "Toronto",
		Status: "saved", Source: "recommended",
		MinSalary: "200k", MinSalaryCurrency: "CAD", MinScore: "60",
		Sort: "salary", Remote: true, Hybrid: true,
		PageSize: 50,
	}
	q := original.toQueryValues()
	// Empty-default and Clear-button use defaultFormVals(); verify that path
	// round-trips too.
	if got := defaultFormVals().toQueryValues().Get("sort"); got != "score" {
		t.Errorf("defaultFormVals sort = %q, want %q", got, "score")
	}
	restored := readForm(q)
	if restored != original {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", restored, original)
	}
}

func TestFiltersFilePath(t *testing.T) {
	if got := filtersFilePath(""); got != "" {
		t.Errorf(`filtersFilePath("") = %q, want ""`, got)
	}
	got := filtersFilePath(filepath.Join("home", "me", "linkedin-jobs", "linkedin_jobs.db"))
	want := filepath.Join("home", "me", "linkedin-jobs", "web_filters.json")
	if got != want {
		t.Errorf("filtersFilePath = %q, want %q", got, want)
	}
}

func TestSavedFiltersPersistAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	ws1 := &webServer{filtersPath: filepath.Join(dir, "web_filters.json")}
	// Nothing saved yet → defaults.
	if got := ws1.loadSavedFilters(); got != defaultFormVals() {
		t.Errorf("expected default vals before any save, got %+v", got)
	}
	saved := formVals{
		Q: "founder", Company: "Globex", Sort: "salary",
		Remote: true, MinSalaryCurrency: "USD", PageSize: 100,
	}
	ws1.saveFilters(saved)
	// A brand-new server instance pointing at the same file must restore the
	// saved state — this is the "survives restart" guarantee.
	ws2 := &webServer{filtersPath: ws1.filtersPath}
	if got := ws2.loadSavedFilters(); got != saved {
		t.Errorf("restored vals mismatch:\n got  %+v\n want %+v", got, saved)
	}
	// Clearing drops back to defaults.
	ws2.clearSavedFilters()
	if got := ws2.loadSavedFilters(); got != defaultFormVals() {
		t.Errorf("expected defaults after clear, got %+v", got)
	}
}

func TestValidPageSize(t *testing.T) {
	cases := map[string]int{
		"":      defaultPageSize,
		"junk":  defaultPageSize,
		"0":     defaultPageSize,
		"10":    defaultPageSize, // not an offered size
		"-5":    defaultPageSize,
		"20":    20,
		"50":    50,
		"100":   100,
		"  50 ": 50,
	}
	for in, want := range cases {
		if got := validPageSize(in); got != want {
			t.Errorf("validPageSize(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestSoftPage(t *testing.T) {
	cases := map[string]int{
		"":     1,
		"junk": 1,
		"0":    1,
		"-3":   1,
		"1":    1,
		"4":    4,
		" 9 ":  9,
	}
	for in, want := range cases {
		if got := softPage(in); got != want {
			t.Errorf("softPage(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestNormalizeSinceSearched(t *testing.T) {
	// RFC3339 passes through and is normalized to UTC (Z suffix).
	if got := normalizeSinceSearched("2026-07-03T12:00:00+05:00"); got != "2026-07-03T07:00:00Z" {
		t.Errorf("RFC3339 tz normalize = %q, want 2026-07-03T07:00:00Z", got)
	}
	// Space-separated datetime → RFC3339 UTC (local interpretation).
	got := normalizeSinceSearched("2026-07-03 00:00:00")
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("space-datetime result %q is not RFC3339: %v", got, err)
	}
	if !strings.HasSuffix(got, "Z") {
		t.Errorf("expected UTC (Z) output, got %q", got)
	}
	// Date-only means start of that day.
	got = normalizeSinceSearched("2026-07-03")
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("date-only result %q is not RFC3339: %v", got, err)
	}
	// datetime-local 'T' form.
	got = normalizeSinceSearched("2026-07-03T15:04")
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("datetime-local result %q is not RFC3339: %v", got, err)
	}
	// Empty / junk → "" (no-op filter).
	if normalizeSinceSearched("") != "" {
		t.Error(`"" should normalize to ""`)
	}
	if normalizeSinceSearched("not a date") != "" {
		t.Error(`junk should normalize to ""`)
	}
}

func TestBuildPagination(t *testing.T) {
	ws := &webServer{}
	f := formVals{Sort: "score", PageSize: 20}

	t.Run("clamps page beyond last", func(t *testing.T) {
		// 25 jobs / 20 per page = 2 pages; ask for page 9 → clamped to 2.
		pg := ws.buildPagination(f, 9, 20, 25)
		if pg.Page != 2 {
			t.Errorf("Page = %d, want 2 (clamped)", pg.Page)
		}
		if pg.Pages != 2 {
			t.Errorf("Pages = %d, want 2", pg.Pages)
		}
		if !pg.HasPrev || pg.HasNext {
			t.Errorf("HasPrev=%v HasNext=%v, want true/false on last page", pg.HasPrev, pg.HasNext)
		}
		if pg.From != 21 || pg.To != 25 {
			t.Errorf("From/To = %d/%d, want 21/25", pg.From, pg.To)
		}
	})

	t.Run("middle page range", func(t *testing.T) {
		// 50 jobs / 20 per page = 3 pages; page 2 shows 21–40.
		pg := ws.buildPagination(f, 2, 20, 50)
		if pg.From != 21 || pg.To != 40 {
			t.Errorf("From/To = %d/%d, want 21/40", pg.From, pg.To)
		}
		if !pg.HasPrev || !pg.HasNext {
			t.Errorf("middle page must have both prev and next")
		}
	})

	t.Run("single page no prev/next", func(t *testing.T) {
		pg := ws.buildPagination(f, 1, 20, 5)
		if pg.Pages != 1 || pg.HasPrev || pg.HasNext {
			t.Errorf("single page should have no nav: %+v", pg)
		}
		if len(pg.Links) != 1 {
			t.Errorf("Links len = %d, want 1", len(pg.Links))
		}
	})

	t.Run("zero jobs empty range", func(t *testing.T) {
		pg := ws.buildPagination(f, 1, 20, 0)
		if pg.From != 0 || pg.To != 0 {
			t.Errorf("From/To = %d/%d, want 0/0 for no jobs", pg.From, pg.To)
		}
		if pg.Pages != 1 {
			t.Errorf("Pages = %d, want 1 even when empty", pg.Pages)
		}
	})

	t.Run("links render all when modest", func(t *testing.T) {
		// 240 jobs / 20 = 12 pages → every page is a link, no gaps.
		pg := ws.buildPagination(f, 5, 20, 240)
		if len(pg.Links) != 12 {
			t.Errorf("Links len = %d, want 12", len(pg.Links))
		}
		for i, l := range pg.Links {
			if l.Page != i+1 {
				t.Errorf("link[%d].Page = %d, want %d", i, l.Page, i+1)
			}
			if l.URL == "" {
				t.Errorf("link[%d] missing URL", i)
			}
			if l.Page == 5 && !l.Current {
				t.Errorf("page 5 should be Current")
			}
		}
	})

	t.Run("links windowed with gaps when large", func(t *testing.T) {
		// 2000 jobs / 20 = 100 pages → windowed with ellipses (Page==0).
		pg := ws.buildPagination(f, 50, 20, 2000)
		if pg.Pages != 100 {
			t.Fatalf("Pages = %d, want 100", pg.Pages)
		}
		hasGap := false
		hasFirst := false
		hasLast := false
		for _, l := range pg.Links {
			if l.Page == 0 {
				hasGap = true
			}
			if l.Page == 1 {
				hasFirst = true
			}
			if l.Page == 100 {
				hasLast = true
			}
		}
		if !hasGap {
			t.Error("expected an ellipsis gap link for 100 pages")
		}
		if !hasFirst || !hasLast {
			t.Error("expected first and last page links when windowed")
		}
	})
}

func TestPageURLPreservesFiltersAndSize(t *testing.T) {
	ws := &webServer{}
	f := formVals{Company: "Acme", Sort: "salary", PageSize: 50, Remote: true}
	u := ws.pageURL(f, 3)
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	if q.Get("page") != "3" {
		t.Errorf("page = %q, want 3", q.Get("page"))
	}
	if q.Get("page_size") != "50" {
		t.Errorf("page_size = %q, want 50", q.Get("page_size"))
	}
	if q.Get("company") != "Acme" {
		t.Errorf("company filter not preserved: %q", q.Get("company"))
	}
	if q.Get("remote") != "1" {
		t.Errorf("remote flag not preserved: %q", q.Get("remote"))
	}
}

func TestRenderPagerWhenMultiplePages(t *testing.T) {
	tpl, err := newPageTemplate()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 45 jobs, page 2 of 3 (page size 20): the pager, the active page, and the
	// "showing X–Y of Z" count must all render.
	pd := pageData{
		CSRF: "t",
		F:    formVals{Sort: "score", PageSize: 20},
		Mode: "list",
		N:    45,
		Pagination: pagination{
			Page: 2, PageSize: 20, Total: 45, Pages: 3,
			From: 21, To: 40, HasPrev: true, HasNext: true,
			PrevURL: "/?page=1&page_size=20", NextURL: "/?page=3&page_size=20",
			Links: []pageLink{
				{Page: 1, URL: "/?page=1&page_size=20"},
				{Page: 2, URL: "/?page=2&page_size=20", Current: true},
				{Page: 3, URL: "/?page=3&page_size=20"},
			},
		},
		Jobs: []jobView{scoreForTest(70, "new")},
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, pd); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		`class="pager"`,
		`pager-page--current`,
		`rel="prev"`,
		`rel="next"`,
		`Showing <strong>21–40</strong> of <strong>45</strong>`,
		`name="page_size"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
}

func TestRenderNoPagerWhenSinglePage(t *testing.T) {
	tpl, err := newPageTemplate()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 5 jobs, one page: no pager markup, simple count only.
	pd := pageData{
		CSRF: "t",
		F:    formVals{Sort: "score", PageSize: 20},
		Mode: "list",
		N:    5,
		Pagination: pagination{Page: 1, PageSize: 20, Total: 5, Pages: 1},
		Jobs: []jobView{scoreForTest(70, "new")},
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, pd); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, `class="pager"`) {
		t.Error("pager should not render when there is a single page")
	}
	if !strings.Contains(out, `<strong>5</strong> job`) {
		t.Error("simple count should render on a single page")
	}
}

func TestIsJobID(t *testing.T) {
	cases := map[string]bool{
		"":            false,
		"123":         false, // too short
		"1234":        false, // too short (boundary)
		"12345":       true,  // 5 digits = minimum
		"4428732008":  true,  // real LinkedIn job id
		"4428732008a": false, // trailing letter
		"staff":       false,
		"  4428732008 ": false, // spaces fail the digit check (caller TrimSpaces first)
	}
	for in, want := range cases {
		if got := isJobID(in); got != want {
			t.Errorf("isJobID(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestQueryByIDLookup verifies the search box routes a bare numeric id to a
// direct store lookup (mode "id"), falls back to FTS when the id isn't stored,
// and still runs FTS for non-numeric terms.
func TestQueryByIDLookup(t *testing.T) {
	st := openTempStore(t)
	ws := &webServer{st: st}

	// Seed two jobs: one with the id the user will type, one to pollute FTS.
	const targetID = "4428732008"
	if err := st.Upsert(&models.JobPosting{
		ID: targetID, Title: "Staff Engineer", Company: "Acme",
		Location: "Remote", Description: "Go services",
	}); err != nil {
		t.Fatalf("upsert target: %v", err)
	}
	if err := st.Upsert(&models.JobPosting{
		ID: "5000000001", Title: "Backend Dev", Company: "Globex",
		Location: "Toronto", Description: "Python services",
	}); err != nil {
		t.Fatalf("upsert other: %v", err)
	}

	t.Run("numeric id hits direct lookup", func(t *testing.T) {
		v := url.Values{"q": {targetID}}
		jobs, mode, err := ws.query(v)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if mode != "id" {
			t.Errorf("mode = %q, want %q", mode, "id")
		}
		if len(jobs) != 1 || jobs[0].ID != targetID {
			t.Errorf("got %d jobs, want exactly 1 with id %s", len(jobs), targetID)
		}
	})

	t.Run("unknown numeric id falls through to search (empty result)", func(t *testing.T) {
		v := url.Values{"q": {"9999999999"}}
		jobs, mode, err := ws.query(v)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if mode != "search" {
			t.Errorf("mode = %q, want %q (fallthrough)", mode, "search")
		}
		if len(jobs) != 0 {
			t.Errorf("got %d jobs for unknown id, want 0", len(jobs))
		}
	})

	t.Run("non-numeric term runs FTS", func(t *testing.T) {
		v := url.Values{"q": {"Python"}}
		jobs, mode, err := ws.query(v)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if mode != "search" {
			t.Errorf("mode = %q, want %q", mode, "search")
		}
		if len(jobs) != 1 || jobs[0].Company != "Globex" {
			t.Errorf("FTS expected to find the Python job, got %+v", jobs)
		}
	})

	t.Run("empty q runs list mode", func(t *testing.T) {
		v := url.Values{}
		jobs, mode, err := ws.query(v)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if mode != "list" {
			t.Errorf("mode = %q, want %q", mode, "list")
		}
		if len(jobs) != 2 {
			t.Errorf("got %d jobs, want 2", len(jobs))
		}
	})
}
