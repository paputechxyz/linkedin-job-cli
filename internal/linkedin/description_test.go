package linkedin

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
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
