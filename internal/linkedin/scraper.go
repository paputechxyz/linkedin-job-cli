package linkedin

import (
	"encoding/json"
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

	// Salary
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
		}
	}

	// Description via JSON-LD JobPosting
	doc.Find(`script[type="application/ld+json"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return true
		}
		var data map[string]interface{}
		if json.Unmarshal([]byte(raw), &data) != nil {
			return true
		}
		if t, _ := data["@type"].(string); t != "JobPosting" {
			return true
		}
		if desc, ok := data["description"].(string); ok {
			j.Description = cleanHTMLText(desc)
			return false
		}
		return true
	})

	// The description body carries the authoritative, localized compensation
	// band (usually with an explicit currency), which is more reliable than the
	// page's salary badge (often a different/default band). Override the badge
	// when we find such a currency-stated range in the description.
	if descSal := descriptionSalary(j.Description); descSal != nil {
		j.SalaryRaw = descSal.Raw
		j.SalaryLow = descSal.Low
		j.SalaryHigh = descSal.High
		j.SalaryCurrency = descSal.Currency
	}

	j.RemoteType = DetectRemote(j.Location + " " + j.Description)
	j.FetchedAt = store.NowISO()
	return nil
}

// descriptionSalary scans a job description for an authoritative compensation
// range and returns the first plausible one (low end >= 1000 to reject small
// non-salary figures). Returns nil when no currency-stated range is present, in
// which case callers keep the salary badge value.
func descriptionSalary(desc string) *salary.Salary {
	matches := descriptionSalaryRE.FindAllString(desc, -1)
	for _, m := range matches {
		s := salary.Parse(m)
		if s == nil || s.Low == nil || s.High == nil {
			continue
		}
		if *s.Low < 1000 || *s.High < 1000 {
			continue
		}
		return s
	}
	return nil
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
