package linkedin

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/salary"
	"linkedin-jobs/internal/store"
)

const guestSearchURL = "https://www.linkedin.com/jobs-guest/jobs/api/seeMoreJobPostings/search"

var jobIDRE = regexp.MustCompile(`jobPosting:(\d+)`)

// descriptionSalaryRE matches a compensation range stated in the job description
// body, requiring an explicit currency signal: either a non-$ currency prefix
// (CA$/CAD/US$/USD/EUR…) on the first amount, or a trailing ISO code. A bare
// "$low - $high" with no currency hint is intentionally NOT matched, since that
// is usually the same ambiguous badge figure and we only want to override the
// badge with authoritative, currency-stated data.
var descriptionSalaryRE = regexp.MustCompile(
	`(?i)(?:` +
		`(?:CA\$|C\$|CAD|US\$|USD|EUR|GBP|AUD|INR|JPY|€|£|¥)\s?[\d,]+(?:\.\d+)?[kKmM]?\s*[-–—]\s*(?:CA\$|C\$|CAD|US\$|USD|EUR|GBP|AUD|INR|JPY|€|£|¥|\$)?\s?[\d,]+(?:\.\d+)?[kKmM]?(?:\s+(?:CAD|USD|EUR|GBP|AUD|INR|JPY))?` + // explicit-prefix first amount
		`|` +
		`(?:CA\$|C\$|CAD|US\$|USD|EUR|GBP|AUD|INR|JPY|€|£|¥|\$)?\s?[\d,]+(?:\.\d+)?[kKmM]?\s*[-–—]\s*(?:CA\$|C\$|CAD|US\$|USD|EUR|GBP|AUD|INR|JPY|€|£|¥|\$)?\s?[\d,]+(?:\.\d+)?[kKmM]?\s+(?:CAD|USD|EUR|GBP|AUD|INR|JPY)` + // trailing ISO code on the range
		`)`)

// Search runs an anonymous job search and returns parsed job cards (no
// salary/description — call FetchDetail for those).
func (c *Client) Search(keywords, location string, pages int) ([]*models.JobPosting, error) {
	var out []*models.JobPosting
	seen := map[string]bool{}
	for page := 0; page < pages; page++ {
		start := page * 25
		u := guestSearchURL + "?keywords=" + urlEncode(keywords) + "&location=" + urlEncode(location)
		if start > 0 {
			u += "&start=" + itoa(start)
		}
		html, _, status, err := c.get(u, false, nil)
		if err != nil {
			return out, err
		}
		if status != 200 || strings.TrimSpace(html) == "" {
			break
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err != nil {
			return out, err
		}
		cards := doc.Find("div[data-entity-urn]")
		if cards.Length() == 0 {
			break
		}
		cards.Each(func(_ int, s *goquery.Selection) {
			j := parseCard(s)
			if j == nil || seen[j.ID] {
				return
			}
			seen[j.ID] = true
			out = append(out, j)
		})
		if cards.Length() < 25 {
			break
		}
	}
	return out, nil
}

func parseCard(s *goquery.Selection) *models.JobPosting {
	urn, _ := s.Attr("data-entity-urn")
	m := jobIDRE.FindStringSubmatch(urn)
	if m == nil {
		return nil
	}
	j := &models.JobPosting{ID: m[1], SearchedAt: store.NowISO(), Source: "search"}
	if t := s.Find(".base-search-card__title").First(); t.Length() > 0 {
		j.Title = strings.TrimSpace(t.Text())
	}
	if j.Title == "" {
		j.Title = "Unknown Title"
	}
	if co := s.Find(".base-search-card__subtitle a").First(); co.Length() > 0 {
		j.Company = strings.TrimSpace(co.Text())
	} else if co := s.Find(".base-search-card__subtitle").First(); co.Length() > 0 {
		j.Company = strings.TrimSpace(co.Text())
	}
	if loc := s.Find(".job-search-card__location").First(); loc.Length() > 0 {
		j.Location = strings.TrimSpace(loc.Text())
	}
	if link := s.Find("a.base-card__full-link").First(); link.Length() > 0 {
		if href, ok := link.Attr("href"); ok {
			j.URL = cleanURL(strings.TrimSpace(href))
		}
	}
	if j.URL == "" {
		return nil
	}
	return j
}

// FetchDetail fetches a job's detail page and fills salary + description.
func (c *Client) FetchDetail(j *models.JobPosting) error {
	html, _, status, err := c.get(j.URL, false, nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return errf("detail fetch returned status %d for %s", status, j.URL)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return err
	}

	// 1. Salary badge (HTML). Low-confidence fallback — LinkedIn's badge is
	// often a different/default band or a generic placeholder.
	var salaryText string
	doc.Find(".main-job-card__salary-info").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		t := strings.TrimSpace(s.Text())
		if t == "" {
			return true
		}
		upper := strings.ToUpper(t)
		if strings.Contains(t, "$") || strings.Contains(upper, "CAD") || strings.Contains(upper, "USD") {
			salaryText = t
			return false
		}
		if salaryText == "" {
			salaryText = t
		}
		return true
	})
	if salaryText != "" {
		if parsed := salary.Parse(salaryText); parsed != nil {
			j.SalaryRaw = parsed.Raw
			j.SalaryLow = parsed.Low
			j.SalaryHigh = parsed.High
			j.SalaryCurrency = parsed.Currency
			j.SalarySource = "badge"
		}
	}

	// 2. Description — try JSON-LD first (cleanest), then HTML fallbacks for
	// pages where LinkedIn doesn't serve a JobPosting JSON-LD block.
	j.Description = extractDescription(doc)

	// 2b. LinkedIn's detail page is now a React Server Components SPA that does
	// not include the description body in the initial HTML. When the HTML
	// extraction misses AND we have a CSRF-bearing session, fetch the
	// description from the Voyager jobPostings API (data.description.text).
	if strings.TrimSpace(j.Description) == "" {
		if desc, err := c.fetchDescriptionViaAPI(j.ID); err == nil {
			j.Description = desc
		}
	}

	// 3. Description-body salary is authoritative (carries the localized band +
	// currency). Override the badge when present and mark it high-confidence.
	if descSal := salary.InDescription(j.Description); descSal != nil {
		j.SalaryRaw = descSal.Raw
		j.SalaryLow = descSal.Low
		j.SalaryHigh = descSal.High
		j.SalaryCurrency = descSal.Currency
		j.SalarySource = models.SalarySourceDescription
	}

	j.RemoteType = DetectRemote(j.Location + " " + j.Description)
	j.FetchedAt = store.NowISO()
	return nil
}

// extractDescription pulls the job description body, trying JSON-LD first
// (preferred — structured and clean) and falling back to rendered HTML
// containers when the page serves no JobPosting JSON-LD. Robust to @type being
// a string or an array, and to the JSON-LD being a single object or an array.
func extractDescription(doc *goquery.Document) string {
	var fromJSONLD string
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return true
		}
		if desc := descriptionFromJSONLD(raw); desc != "" {
			fromJSONLD = desc
			return false
		}
		return true
	})
	if fromJSONLD != "" {
		return fromJSONLD
	}
	// HTML fallbacks: order matters; the most specific selector first.
	for _, sel := range []string{
		".description__text .show-more-less-html__markup",
		".description__text",
		".jobs-description__content",
		".jobs-description-content",
		".jobs-box__html-content",
	} {
		if t := strings.TrimSpace(doc.Find(sel).First().Text()); t != "" {
			return cleanHTMLText(t)
		}
	}
	return ""
}

// descriptionFromJSONLD extracts a JobPosting description from a raw JSON-LD
// blob, accepting either a single object or an array of objects.
func descriptionFromJSONLD(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if desc := jobPostingDesc([]byte(raw)); desc != "" {
		return desc
	}
	if raw[0] == '[' {
		var arr []map[string]interface{}
		if json.Unmarshal([]byte(raw), &arr) == nil {
			for _, o := range arr {
				if desc := descFromMap(o); desc != "" {
					return desc
				}
			}
		}
	}
	return ""
}

func jobPostingDesc(b []byte) string {
	var o map[string]interface{}
	if json.Unmarshal(b, &o) != nil {
		return ""
	}
	return descFromMap(o)
}

func descFromMap(o map[string]interface{}) string {
	if !hasJobPostingType(o["@type"]) {
		return ""
	}
	if d, ok := o["description"].(string); ok {
		return cleanHTMLText(d)
	}
	return ""
}

// hasJobPostingType reports whether the JSON-LD @type value(s) include JobPosting.
func hasJobPostingType(t interface{}) bool {
	switch v := t.(type) {
	case string:
		return v == "JobPosting"
	case []interface{}:
		for _, e := range v {
			if s, ok := e.(string); ok && s == "JobPosting" {
				return true
			}
		}
	}
	return false
}

// fetchDescriptionViaAPI fetches a job's description body from the Voyager
// jobPostings REST API. Used as a fallback when the detail HTML page (now a
// React Server Components SPA) ships no description in the initial HTML.
//
// Requires a CSRF-bearing session (authenticated calls only). The endpoint
// returns a normalized object whose `data.description` is a Pemberly
// AttributedText {text, attributes}; only the plain `text` is needed. Returns
// ("", nil) when the field is absent so the caller can treat it as a soft miss.
func (c *Client) fetchDescriptionViaAPI(id string) (string, error) {
	if id == "" || !c.HasSession() || c.session.CSRFToken == "" {
		return "", nil
	}
	url := "https://www.linkedin.com/voyager/api/jobs/jobPostings/" + id
	hdr := http.Header{
		"Referer":                   {"https://www.linkedin.com/jobs/view/" + id + "/"},
		"X-Restli-Protocol-Version": {"2.0.0"},
		"Csrf-Token":                {c.session.CSRFToken},
	}
	body, status, err := c.getJSON(url, true, hdr)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", errf("jobPostings API returned status %d for %s", status, id)
	}
	return descriptionFromJobPostingAPI(body), nil
}

// descriptionFromJobPostingAPI extracts the plain text from a Voyager
// jobPostings response's data.description.text field. Returns "" on any shape
// mismatch so the caller can fall through without erroring.
func descriptionFromJobPostingAPI(body string) string {
	var resp struct {
		Data struct {
			Description struct {
				Text string `json:"text"`
			} `json:"description"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil {
		return ""
	}
	return strings.TrimSpace(resp.Data.Description.Text)
}

// FetchDetailsBatch fetches detail pages for multiple jobs with a politeness
// delay. Per-job errors are recorded but do not abort the batch.
func (c *Client) FetchDetailsBatch(jobs []*models.JobPosting, delay float64, progress func(done, total int)) {
	for i, j := range jobs {
		if err := c.FetchDetail(j); err != nil {
			j.Summary = "[fetch error: " + err.Error() + "]"
		}
		if progress != nil {
			progress(i+1, len(jobs))
		}
		if i < len(jobs)-1 {
			sleep(delay)
		}
	}
}
