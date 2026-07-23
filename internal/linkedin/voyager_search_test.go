package linkedin

import (
	"os"
	"path/filepath"
	"testing"
)

// loadFixture reads a testdata fixture.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// TestParseVoyagerJobCards_start0 validates the parser against the real
// captured Voyager jobCards response (page 1, start=0, count=25). It should
// yield 25 cards and report paging.total=32.
func TestParseVoyagerJobCards_start0(t *testing.T) {
	body := loadFixture(t, "voyager_jobcards_start0.json")
	cards, total, err := parseVoyagerJobCards(body)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if total != 32 {
		t.Errorf("total: got %d, want 32", total)
	}
	if len(cards) != 25 {
		t.Fatalf("page 1 card count: got %d, want 25", len(cards))
	}
	first := cards[0]
	if first.ID != "4436542046" {
		t.Errorf("first ID: got %q, want 4436542046", first.ID)
	}
	if first.Title == "" || first.Title == "Unknown Title" {
		t.Errorf("first title empty: %+v", first)
	}
	wantCompany := "Redcan.ai"
	if first.Company != wantCompany {
		t.Errorf("first company: got %q, want %q", first.Company, wantCompany)
	}
	if first.URL != "https://www.linkedin.com/jobs/view/4436542046/" {
		t.Errorf("first URL: got %q", first.URL)
	}
	if first.Location == "" {
		t.Errorf("first location empty")
	}
	// "(Remote)" is part of the location; DetectRemote should classify it.
	if first.RemoteType != "remote" {
		t.Errorf("first remote type: got %q, want remote", first.RemoteType)
	}
	// Every card must have an ID, a non-empty title, and a canonical view URL.
	seen := map[string]bool{}
	for _, c := range cards {
		if c.ID == "" {
			t.Fatal("card with empty ID")
		}
		if seen[c.ID] {
			t.Fatalf("duplicate ID within page: %s", c.ID)
		}
		seen[c.ID] = true
		if c.Title == "" {
			t.Errorf("card %s: empty title", c.ID)
		}
		if c.URL == "" {
			t.Errorf("card %s: empty URL", c.ID)
		}
	}
}

// TestParseVoyagerJobCards_start25 validates page 2 (start=25): the remaining
// 7 cards that bring the running total to 32.
func TestParseVoyagerJobCards_start25(t *testing.T) {
	body := loadFixture(t, "voyager_jobcards_start25.json")
	cards, total, err := parseVoyagerJobCards(body)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if total != 32 {
		t.Errorf("total: got %d, want 32", total)
	}
	if len(cards) != 7 {
		t.Fatalf("page 2 card count: got %d, want 7", len(cards))
	}
}

// TestParseVoyagerJobCards_bothPages combines the two captured pages the way
// jobsFromVoyagerSearch's dedup loop does, confirming the full result set
// (32 unique jobs) is recoverable and the loop's stop conditions would fire
// correctly (page 2 returns fewer than pageSize=25 → last page).
func TestParseVoyagerJobCards_bothPages(t *testing.T) {
	const pageSize = 25
	seen := map[string]bool{}
	var all []string
	total := -1
	pages := []string{"voyager_jobcards_start0.json", "voyager_jobcards_start25.json"}
	for _, name := range pages {
		cards, pageTotal, err := parseVoyagerJobCards(loadFixture(t, name))
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if total < 0 {
			total = pageTotal
		}
		for _, c := range cards {
			if !seen[c.ID] {
				seen[c.ID] = true
				all = append(all, c.ID)
			}
		}
		if len(cards) < pageSize {
			break
		}
		if total >= 0 && len(all) >= total {
			break
		}
	}
	if total != 32 {
		t.Errorf("total: got %d, want 32", total)
	}
	if len(all) != 32 {
		t.Errorf("combined unique jobs: got %d, want 32", len(all))
	}
}

// TestVoyagerSearchQuery maps the user's real search URL onto the Restli
// compact-JSON query the Voyager jobCards API expects.
func TestVoyagerSearchQuery(t *testing.T) {
	const in = "https://www.linkedin.com/jobs/search/?currentJobId=4436542046&distance=25&f_TPR=a1783329149-&geoId=105149290&keywords=Staff%20Software%20Engineer&origin=JOB_ALERT_EMAIL&originToLandingJobPostings=4436542046%2C4430644084&sortBy=R&trk=x"
	got, ok := voyagerSearchQuery(in)
	if !ok {
		t.Fatal("expected ok, got false")
	}
	// Required fields.
	for _, want := range []string{
		"(keywords:Staff Software Engineer",
		"locationUnion:(geoId:105149290)",
		"selectedFilters:(sortBy:List(R)",
		"distance:List(25)",
		"timePostedRange:List(a1783329149-)",
		"spellCorrectionEnabled:true)",
	} {
		if !contains(got, want) {
			t.Errorf("query missing %q\nfull: %s", want, got)
		}
	}
	// Tracking/context params must NOT bleed into the query.
	for _, bad := range []string{"currentJobId", "origin", "originToLandingJobPostings", "trk"} {
		if contains(got, bad) {
			t.Errorf("query should not contain %q\nfull: %s", bad, got)
		}
	}
}

// TestVoyagerSearchQuery_multiGeoId confirms that a URL carrying multiple
// comma-separated geoIds (the browser emits geoId=A,B when two regions are
// selected) is translated to a Restli List(...) rather than a bare comma,
// which would corrupt the query and silently fall back to the capped guest
// endpoint.
func TestVoyagerSearchQuery_multiGeoId(t *testing.T) {
	const in = "https://www.linkedin.com/jobs/search/?keywords=Staff%20Engineer&geoId=100025096%2C101788145"
	got, ok := voyagerSearchQuery(in)
	if !ok {
		t.Fatal("expected ok, got false")
	}
	want := "locationUnion:(geoId:List(100025096,101788145))"
	if !contains(got, want) {
		t.Errorf("multi-geoId: got %q\nwant substring %q", got, want)
	}
}

// TestVoyagerSearchQuery_workType confirms that f_WT (workplace type) is mapped
// to the Voyager selectedFilters.workType field rather than silently dropped.
// Without this mapping, a pasted URL with f_WT=2 (Remote) would lose the filter
// on the signed-in Voyager path.
func TestVoyagerSearchQuery_workType(t *testing.T) {
	const in = "https://www.linkedin.com/jobs/search/?keywords=Staff%20Engineer&geoId=105149290&f_WT=2"
	got, ok := voyagerSearchQuery(in)
	if !ok {
		t.Fatal("expected ok, got false")
	}
	want := "workType:List(2)"
	if !contains(got, want) {
		t.Errorf("workType: got %q\nwant substring %q", got, want)
	}
}

// TestVoyagerSearchQuery_workTypeMulti confirms that comma-separated f_WT
// values (e.g. f_WT=2,3 for remote OR hybrid) are preserved.
func TestVoyagerSearchQuery_workTypeMulti(t *testing.T) {
	const in = "https://www.linkedin.com/jobs/search/?keywords=Staff%20Engineer&f_WT=2,3"
	got, ok := voyagerSearchQuery(in)
	if !ok {
		t.Fatal("expected ok, got false")
	}
	want := "workType:List(2,3)"
	if !contains(got, want) {
		t.Errorf("workType multi: got %q\nwant substring %q", got, want)
	}
}

func TestEscapeRestli(t *testing.T) {
	if got := escapeRestli(`a(b)c:d,e\f`); got != `a\(b\)c\:d\,e\\f` {
		t.Errorf("escapeRestli: got %q", got)
	}
}

func TestVoyagerJobID(t *testing.T) {
	cases := []struct {
		urns []string
		want string
	}{
		{[]string{"urn:li:fs_normalized_jobPosting:4436542046"}, "4436542046"},
		{[]string{"", "urn:li:fsd_jobPosting:4430644084"}, "4430644084"},
		// The card entityUrn does NOT match jobPosting:<digits>, so it should
		// fall through to the next candidate.
		{[]string{"urn:li:fsd_jobPostingCard:(4436542046,JOBS_SEARCH)", "urn:li:fsd_jobPosting:4436542046"}, "4436542046"},
		{[]string{"urn:li:fsd_jobPostingCard:(4436542046,JOBS_SEARCH)"}, ""},
	}
	for _, c := range cases {
		if got := voyagerJobID(c.urns...); got != c.want {
			t.Errorf("voyagerJobID(%v): got %q, want %q", c.urns, got, c.want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
