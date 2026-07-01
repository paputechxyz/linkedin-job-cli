package linkedin

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestJobIDsFromURL_OriginToLandingJobPostings(t *testing.T) {
	// Real job-alert email link (the form the user pasted).
	u := "https://www.linkedin.com/jobs/search/?currentJobId=4415889466&distance=25&f_TPR=a1782726266-&geoId=101788145&keywords=Staff%20Engineer&origin=JOB_ALERT_EMAIL&originToLandingJobPostings=4415889466%2C4434154740%2C4378880839%2C4434934302%2C4383944004%2C4408101577&sortBy=R"
	got := jobIDsFromURL(u)
	want := []string{"4415889466", "4434154740", "4378880839", "4434934302", "4383944004", "4408101577"}
	if len(got) != len(want) {
		t.Fatalf("got %d IDs %v, want %d", len(got), got, len(want))
	}
	for i, id := range got {
		if id != want[i] {
			t.Errorf("idx %d: got %q, want %q", i, id, want[i])
		}
	}
}

func TestJobIDsFromURL_DedupsAndIgnoresNonNumeric(t *testing.T) {
	// Duplicates and junk should be silently dropped; order preserved.
	u := "https://www.linkedin.com/jobs/search/?originToLandingJobPostings=111%2C222%2C111%2Cabc%2C333"
	got := jobIDsFromURL(u)
	want := []string{"111", "222", "333"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, id := range got {
		if id != want[i] {
			t.Errorf("idx %d: got %q, want %q", i, id, want[i])
		}
	}
}

func TestJobIDsFromURL_FallsBackToCurrentJobId(t *testing.T) {
	// No originToLandingJobPostings: fall back to the single currentJobId.
	u := "https://www.linkedin.com/jobs/view/?currentJobId=4415889466&trk=xyz"
	got := jobIDsFromURL(u)
	if len(got) != 1 || got[0] != "4415889466" {
		t.Errorf("got %v, want [4415889466]", got)
	}
}

func TestJobIDsFromURL_OriginWinsOverCurrentJobId(t *testing.T) {
	// When both are present, originToLandingJobPostings is the full list and
	// should win; currentJobId is typically one of those anyway.
	u := "https://www.linkedin.com/jobs/search/?currentJobId=4415889466&originToLandingJobPostings=111%2C222"
	got := jobIDsFromURL(u)
	if len(got) != 2 {
		t.Errorf("got %v, want 2 IDs from origin list", got)
	}
}

func TestJobIDsFromURL_NoIDsReturnsNil(t *testing.T) {
	// Plain search URL with only keywords/location — no IDs in query string.
	cases := []string{
		"https://www.linkedin.com/jobs/search/?keywords=Staff%20Engineer&location=Toronto",
		"https://www.linkedin.com/jobs/search/",
		"https://www.linkedin.com/jobs/collections/recommended/",
		"not even a url",
		"",
	}
	for _, u := range cases {
		if got := jobIDsFromURL(u); got != nil {
			t.Errorf("u=%q: got %v, want nil", u, got)
		}
	}
}

func TestJobIDsFromURL_EmptyOriginFallsBackToCurrentJobId(t *testing.T) {
	// originToLandingJobPostings present but empty -> still fall through to currentJobId.
	u := "https://www.linkedin.com/jobs/search/?originToLandingJobPostings=&currentJobId=99999"
	got := jobIDsFromURL(u)
	if len(got) != 1 || got[0] != "99999" {
		t.Errorf("got %v, want [99999]", got)
	}
}

func TestJobsFromIDs_BuildsSkeletonPostings(t *testing.T) {
	ids := []string{"111", "222", "333"}
	jobs := jobsFromIDs(ids, "url")
	if len(jobs) != 3 {
		t.Fatalf("got %d jobs, want 3", len(jobs))
	}
	for i, j := range jobs {
		if j.ID != ids[i] {
			t.Errorf("job %d: ID=%q, want %q", i, j.ID, ids[i])
		}
		wantURL := "https://www.linkedin.com/jobs/view/" + ids[i] + "/"
		if j.URL != wantURL {
			t.Errorf("job %d: URL=%q, want %q", i, j.URL, wantURL)
		}
		if j.Source != "url" {
			t.Errorf("job %d: Source=%q, want \"url\"", i, j.Source)
		}
		if j.SearchedAt == "" {
			t.Errorf("job %d: SearchedAt empty", i)
		}
	}
}

func TestIsDigits(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"abc":   false,
		"123":   true,
		"12a3":  false,
		"0":     true,
		" 123 ": false, // spaces are not digits
		"-123":  false, // sign is not a digit
	}
	for in, want := range cases {
		if got := isDigits(in); got != want {
			t.Errorf("isDigits(%q)=%v, want %v", in, got, want)
		}
	}
}

// TestExtractJobMeta_AllFields exercises a full JobPosting JSON-LD block — the
// shape LinkedIn emits on detail pages. All four fields (title, company,
// location, description) should populate. This is what fills the "Unknown
// Title" gap for jobs built from a bare ID via the url command.
func TestExtractJobMeta_AllFields(t *testing.T) {
	html := `<html><head>
	<script type="application/ld+json">{
		"@type": "JobPosting",
		"title": "Staff Engineer",
		"hiringOrganization": {"@type": "Organization", "name": "Acme Corp"},
		"jobLocation": {"@type": "Place", "address": {
			"@type": "PostalAddress",
			"addressLocality": "Toronto",
			"addressRegion": "ON",
			"addressCountry": "CA"
		}},
		"description": "We build things."
	}</script>
	</head><body></body></html>`
	doc := docFromJSONLD(t, html)
	m := extractJobMeta(doc)
	if m.Title != "Staff Engineer" {
		t.Errorf("Title=%q", m.Title)
	}
	if m.Company != "Acme Corp" {
		t.Errorf("Company=%q", m.Company)
	}
	if m.Location != "Toronto, ON, CA" {
		t.Errorf("Location=%q, want \"Toronto, ON, CA\"", m.Location)
	}
	if !strings.Contains(m.Description, "We build things") {
		t.Errorf("Description=%q", m.Description)
	}
}

// TestExtractJobMeta_ArrayOfLocations confirms a JSON-LD jobLocation array is
// walked and the first addressful entry is used.
func TestExtractJobMeta_ArrayOfLocations(t *testing.T) {
	html := `<html><head>
		<script type="application/ld+json">{"@type":"JobPosting","title":"X","jobLocation":[
			{"@type":"Place","address":{"addressLocality":"Berlin","addressCountry":"DE"}}
		]}</script>
	</head></html>`
	m := extractJobMeta(docFromJSONLD(t, html))
	if m.Location != "Berlin, DE" {
		t.Errorf("Location=%q, want \"Berlin, DE\"", m.Location)
	}
}

// TestExtractJobMeta_NoJobPosting confirms a page with only non-JobPosting
// JSON-LD blocks yields a zero-value jobMeta (no fields populated).
func TestExtractJobMeta_NoJobPosting(t *testing.T) {
	html := `<html><head>
		<script type="application/ld+json">{"@type":"WebSite","name":"x"}</script>
	</head></html>`
	m := extractJobMeta(docFromJSONLD(t, html))
	if m != (jobMeta{}) {
		t.Errorf("expected zero-value jobMeta, got %+v", m)
	}
}

// docFromJSONLD is a tiny helper around goquery for the JSON-LD tests above.
func docFromJSONLD(t *testing.T, html string) *goquery.Document {
	t.Helper()
	d, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
