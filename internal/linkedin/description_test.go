package linkedin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"linkedin-jobs/internal/auth"
	"linkedin-jobs/internal/config"
)

func docFrom(t *testing.T, html string) *goquery.Document {
	t.Helper()
	d, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestExtractDescription_JSONLD_TypeString(t *testing.T) {
	html := `<html><head>
	<script type="application/ld+json">{"@type":"JobPosting","description":"We build things."}</script>
	</head><body></body></html>`
	got := extractDescription(docFrom(t, html))
	if got != "We build things." {
		t.Errorf("got %q", got)
	}
}

func TestExtractDescription_JSONLD_TypeArray(t *testing.T) {
	// LinkedIn sometimes emits @type as an array.
	html := `<html><head>
		<script type="application/ld+json">{"@type":["JobPosting","Organization"],"description":"Array-typed role."}</script>
	</head><body></body></html>`
	got := extractDescription(docFrom(t, html))
	if got != "Array-typed role." {
		t.Errorf("got %q", got)
	}
}

func TestExtractDescription_JSONLD_ArrayOfObjects(t *testing.T) {
	// JSON-LD may be an array of objects; pick the JobPosting one.
	html := `<html><head>
		<script type="application/ld+json">[{"@type":"WebSite","name":"x"},{"@type":"JobPosting","description":"FromArray"}]</script>
	</head><body></body></html>`
	got := extractDescription(docFrom(t, html))
	if got != "FromArray" {
		t.Errorf("got %q", got)
	}
}

func TestExtractDescription_HTMLFallback(t *testing.T) {
	// No JSON-LD at all: fall back to the rendered description container.
	html := `<html><body>
		<div class="description__text"><div class="show-more-less-html__markup">
		  About the role: you will own the platform. Salary TBD.
		</div></div>
	</body></html>`
	got := extractDescription(docFrom(t, html))
	if !strings.Contains(got, "About the role") {
		t.Errorf("expected HTML fallback to capture description, got %q", got)
	}
}

func TestExtractDescription_EmptyPage(t *testing.T) {
	if got := extractDescription(docFrom(t, `<html></html>`)); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestExtractTopCardMeta_RendersTitleCompanyLocation(t *testing.T) {
	html := `<html><body>
	  <h1 class="top-card-layout__title topcard__title">Staff Backend Software Engineer</h1>
	  <div class="topcard__flavor-row">
	    <span class="topcard__flavor">
	      <a class="topcard__org-name-link topcard__flavor--black-link" href="/company/hopper">Hopper</a>
	    </span>
	    <span class="topcard__flavor topcard__flavor--bullet">Toronto, Ontario, Canada</span>
	    <span class="topcard__flavor topcard__flavor--bullet">40 applicants</span>
	  </div>
	</body></html>`
	m := extractTopCardMeta(docFrom(t, html))
	if m.Title != "Staff Backend Software Engineer" {
		t.Errorf("title: got %q", m.Title)
	}
	if m.Company != "Hopper" {
		t.Errorf("company: got %q", m.Company)
	}
	if m.Location != "Toronto, Ontario, Canada" {
		t.Errorf("location: got %q", m.Location)
	}
}

func TestExtractTopCardMeta_SkipsApplicantBulletForLocation(t *testing.T) {
	html := `<html><body>
	  <span class="topcard__flavor topcard__flavor--bullet">40 applicants</span>
	</body></html>`
	if m := extractTopCardMeta(docFrom(t, html)); m.Location != "" {
		t.Errorf("expected empty location, got %q", m.Location)
	}
}

func TestExtractTopCardMeta_EmptyPage(t *testing.T) {
	if m := extractTopCardMeta(docFrom(t, `<html></html>`)); m != (jobMeta{}) {
		t.Errorf("expected zero-value meta, got %+v", m)
	}
}

// TestDescriptionFromJobPostingAPI validates extraction of the plain `text`
// field from a Voyager jobPostings response's data.description node. The
// AttributedText `attributes` array (bold/list/hyperlink metadata) must be
// ignored — only `text` matters.
func TestDescriptionFromJobPostingAPI(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "jobposting_api.json"))
	if err != nil {
		t.Skip("testdata/jobposting_api.json not present:", err)
	}
	got := descriptionFromJobPostingAPI(string(body))
	if !strings.Contains(got, "Responsibilities include building services") {
		t.Errorf("expected description body, got %q", got)
	}
	if !strings.Contains(got, "$118,500 to $216,500 USD") {
		t.Errorf("expected salary band to round-trip, got %q", got)
	}
}

// TestDescriptionFromJobPostingAPI_EmptyOrMalformed ensures soft-miss behavior:
// any shape mismatch yields "" rather than an error so the caller can fall
// through without escalating a parse failure into a hard error.
func TestDescriptionFromJobPostingAPI_EmptyOrMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":         ``,
		"not json":      `<html>nope</html>`,
		"missing data":  `{"included": []}`,
		"missing descr": `{"data": {"title": "x"}}`,
		"descr not obj": `{"data": {"description": "raw string"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if got := descriptionFromJobPostingAPI(body); got != "" {
				t.Errorf("expected empty for %s, got %q", name, got)
			}
		})
	}
}

// TestFetchDescriptionViaAPI_Guards covers the three soft-miss guards so the
// fallback never fires an authenticated request when it can't possibly succeed:
// no job id, no session, no cookie, or no csrf token. The HTTP transport + JSON
// parse layers are exercised separately (descriptionFromJobPostingAPI above;
// getJSON is shared with the production Recommended path).
func TestFetchDescriptionViaAPI_Guards(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		session *auth.Session
	}{
		{"empty id", "", &auth.Session{CookieHeader: "x", CSRFToken: "ajax:1"}},
		{"no session", "123", nil},
		{"empty cookie", "123", &auth.Session{CookieHeader: "", CSRFToken: "ajax:1"}},
		{"empty csrf", "123", &auth.Session{CookieHeader: "x", CSRFToken: ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(config.Load()).WithSession(tc.session)
			desc, err := c.fetchDescriptionViaAPI(tc.id)
			if err != nil {
				t.Errorf("guard %q should soft-miss without error, got err=%v", tc.name, err)
			}
			if desc != "" {
				t.Errorf("guard %q should return empty, got %q", tc.name, desc)
			}
		})
	}
}
