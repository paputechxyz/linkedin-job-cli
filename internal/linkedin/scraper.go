package linkedin

import (
	"encoding/json"
	"net/http"
	"net/url"
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

// SearchURL extracts job postings from an arbitrary LinkedIn search/collection
// URL (e.g. a job-alert email link, a saved-search URL, or a URL pasted from
// the browser). Strategy, in priority order:
//
//  1. URL has a `keywords` query param — replay it against the paginated
//     guest seeMoreJobPostings API so `top` can pull more than the first page
//     (this is the same XHR the browser fires when you scroll the left panel).
//     geoId/distance/f_TPR filters from the URL are preserved.
//  2. URL carries explicit job IDs (originToLandingJobPostings from a job-alert
//     email, or currentJobId) and NO keywords — those IDs are used directly.
//  3. Otherwise, fetch the URL HTML and parse cards via the same selectors as
//     Search.
//
// Cards returned here carry only id/title/company/location (cases 1 and 3) or
// just id with title "Unknown Title" (case 2); FetchDetail fills the rest.
//
// Stray backslashes are stripped first: inside single quotes shells preserve
// `\?` `\=` `\&` literally (no escaping happens), so an over-escaped paste
// ('https://…/\?a\=1\&b\=2') would otherwise leave query keys with trailing
// backslashes and silently match nothing. LinkedIn URLs never contain a
// literal backslash, so the strip is always safe.
func (c *Client) SearchURL(rawURL string, top int) ([]*models.JobPosting, error) {
	rawURL = strings.ReplaceAll(rawURL, "\\", "")
	if u, err := url.Parse(rawURL); err == nil && u.Query().Has("keywords") {
		return c.jobsFromSearchURL(rawURL, top)
	}
	if ids := jobIDsFromURL(rawURL); len(ids) > 0 {
		return jobsFromIDs(ids, "url"), nil
	}
	return c.jobsFromHTMLPage(rawURL)
}

// jobsFromSearchURL paginates through a LinkedIn job search by replaying the
// URL's query params against the guest seeMoreJobPostings endpoint — the same
// XHR the browser fires when you scroll the left panel of /jobs/search/.
// maxJobs <= 0 means pull until fewer than a full page comes back.
//
// Tracking/pinning params (currentJobId, originToLandingJobPostings, trk, …)
// are stripped; everything else (keywords, geoId, distance, f_TPR, f_WT, …) is
// passed through so the user's filters are honored.
func (c *Client) jobsFromSearchURL(rawURL string, maxJobs int) ([]*models.JobPosting, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	for _, k := range []string{
		"currentJobId", "origin", "originToLandingJobPostings",
		"sortBy", "trk", "sessionId", "lipi", "refId",
	} {
		q.Del(k)
	}
	var out []*models.JobPosting
	seen := map[string]bool{}
	const pageSize = 25
	for start := 0; maxJobs <= 0 || len(out) < maxJobs; start += pageSize {
		q.Set("start", itoa(start))
		apiURL := guestSearchURL + "?" + q.Encode()
		html, _, status, err := c.get(apiURL, false, nil)
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
			j.Source = "url"
			out = append(out, j)
		})
		if cards.Length() < pageSize {
			break // last page
		}
	}
	return out, nil
}

// jobIDsFromURL extracts job IDs from a LinkedIn URL's query params. Prefers
// originToLandingJobPostings (the full list from job-alert emails); falls back
// to currentJobId (a single job). Returns nil when neither is present.
func jobIDsFromURL(rawURL string) []string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	q := u.Query()
	seen := map[string]bool{}
	var ids []string
	if list := q.Get("originToLandingJobPostings"); list != "" {
		for _, id := range strings.Split(list, ",") {
			id = strings.TrimSpace(id)
			if id != "" && isDigits(id) && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			return ids
		}
	}
	if id := strings.TrimSpace(q.Get("currentJobId")); id != "" && isDigits(id) {
		ids = append(ids, id)
	}
	return ids
}

// jobsFromIDs builds skeleton JobPosting records from a list of LinkedIn job
// IDs. Title/company/location are filled later by FetchDetail; only the ID +
// canonical view URL are known at this stage.
func jobsFromIDs(ids []string, source string) []*models.JobPosting {
	out := make([]*models.JobPosting, 0, len(ids))
	for _, id := range ids {
		out = append(out, &models.JobPosting{
			ID:         id,
			URL:        "https://www.linkedin.com/jobs/view/" + id + "/",
			Title:      "Unknown Title",
			Source:     source,
			SearchedAt: store.NowISO(),
		})
	}
	return out
}

// jobsFromHTMLPage fetches a LinkedIn search/collection URL and parses job
// cards from the HTML. Uses the same selectors as the guest Search path. Tries
// an authenticated GET when a session is available (LinkedIn returns more
// complete results to signed-in users), then falls back to anonymous.
func (c *Client) jobsFromHTMLPage(rawURL string) ([]*models.JobPosting, error) {
	authed := c.HasSession()
	html, _, status, err := c.get(rawURL, authed, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, errf("page fetch returned status %d for %s", status, rawURL)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}
	var out []*models.JobPosting
	seen := map[string]bool{}
	cards := doc.Find("div[data-entity-urn]")
	cards.Each(func(_ int, s *goquery.Selection) {
		j := parseCard(s)
		if j == nil || seen[j.ID] {
			return
		}
		seen[j.ID] = true
		j.Source = "url"
		out = append(out, j)
	})
	return out, nil
}

// isDigits reports whether s is a non-empty string of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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

	// 2. JSON-LD JobPosting — source for description AND any missing card
	// metadata (title/company/location). Cards from the listing page already
	// carry these, but jobs built from a bare ID (e.g. via the `url` command's
	// originToLandingJobPostings path) only have an ID + view URL, so the
	// JSON-LD block on the detail page is where their title comes from.
	meta := extractJobMeta(doc)
	if meta.Description != "" {
		j.Description = meta.Description
	} else {
		// HTML fallbacks for pages where LinkedIn ships no JobPosting JSON-LD.
		j.Description = extractDescriptionHTML(doc)
	}
	if (j.Title == "" || j.Title == "Unknown Title") && meta.Title != "" {
		j.Title = meta.Title
	}
	if j.Company == "" && meta.Company != "" {
		j.Company = meta.Company
	}
	if j.Location == "" && meta.Location != "" {
		j.Location = meta.Location
	}

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
// containers when the page serves no JobPosting JSON-LD. FetchDetail calls
// extractJobMeta + extractDescriptionHTML directly so it can also pick up
// title/company/location; this wrapper is kept for tests and other callers
// that only need the description.
func extractDescription(doc *goquery.Document) string {
	if m := extractJobMeta(doc); m.Description != "" {
		return m.Description
	}
	return extractDescriptionHTML(doc)
}

// extractDescriptionHTML pulls the job description body from rendered HTML
// containers. Used as a fallback when the page ships no JobPosting JSON-LD
// (the JSON-LD path lives in extractJobMeta, which the caller already tried).
func extractDescriptionHTML(doc *goquery.Document) string {
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

// jobMeta captures the structured fields available in a JSON-LD JobPosting
// block on a LinkedIn detail page. Any field may be empty if absent.
type jobMeta struct {
	Title       string
	Company     string
	Location    string
	Description string
}

// extractJobMeta scans JSON-LD <script> blocks for a JobPosting and returns its
// structured fields. Returns a zero-value jobMeta when no JobPosting is found.
func extractJobMeta(doc *goquery.Document) jobMeta {
	var meta jobMeta
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return true
		}
		if m := jobMetaFromJSONLD(raw); m != nil {
			meta = *m
			return false
		}
		return true
	})
	return meta
}

// jobMetaFromJSONLD parses a raw JSON-LD blob (single object or array) and
// returns the first JobPosting's fields, or nil if none is present.
func jobMetaFromJSONLD(raw string) *jobMeta {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if m := jobMetaFromBytes([]byte(raw)); m != nil {
		return m
	}
	if raw[0] == '[' {
		var arr []map[string]interface{}
		if json.Unmarshal([]byte(raw), &arr) == nil {
			for _, o := range arr {
				if m := jobMetaFromMap(o); m != nil {
					return m
				}
			}
		}
	}
	return nil
}

func jobMetaFromBytes(b []byte) *jobMeta {
	var o map[string]interface{}
	if json.Unmarshal(b, &o) != nil {
		return nil
	}
	return jobMetaFromMap(o)
}

// jobMetaFromMap extracts JobPosting fields from a decoded JSON-LD object.
// Returns nil if the object isn't a JobPosting or has none of the fields we
// care about, so the caller can keep scanning.
func jobMetaFromMap(o map[string]interface{}) *jobMeta {
	if !hasJobPostingType(o["@type"]) {
		return nil
	}
	m := &jobMeta{}
	if t, ok := o["title"].(string); ok {
		m.Title = strings.TrimSpace(t)
	}
	if org, ok := o["hiringOrganization"].(map[string]interface{}); ok {
		if name, ok := org["name"].(string); ok {
			m.Company = strings.TrimSpace(name)
		}
	}
	m.Location = locationStringFromJSONLD(o["jobLocation"])
	if d, ok := o["description"].(string); ok {
		m.Description = cleanHTMLText(d)
	}
	if m.Title == "" && m.Company == "" && m.Location == "" && m.Description == "" {
		return nil
	}
	return m
}

// locationStringFromJSONLD renders "City, Region, Country" from a JSON-LD
// jobLocation value (single object or array). Returns "" when no address is
// present.
func locationStringFromJSONLD(v interface{}) string {
	var locs []interface{}
	switch x := v.(type) {
	case map[string]interface{}:
		locs = []interface{}{x}
	case []interface{}:
		locs = x
	default:
		return ""
	}
	for _, l := range locs {
		lm, ok := l.(map[string]interface{})
		if !ok {
			continue
		}
		addr, _ := lm["address"].(map[string]interface{})
		if addr == nil {
			continue
		}
		var parts []string
		if s, ok := addr["addressLocality"].(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				parts = append(parts, s)
			}
		}
		if s, ok := addr["addressRegion"].(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				parts = append(parts, s)
			}
		}
		if s, ok := addr["addressCountry"].(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, ", ")
		}
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
