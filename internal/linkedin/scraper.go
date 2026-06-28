package linkedin

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"linkedin-job-cli/internal/models"
	"linkedin-job-cli/internal/salary"
	"linkedin-job-cli/internal/store"
)

const guestSearchURL = "https://www.linkedin.com/jobs-guest/jobs/api/seeMoreJobPostings/search"

var jobIDRE = regexp.MustCompile(`jobPosting:(\d+)`)

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

	j.RemoteType = DetectRemote(j.Location + " " + j.Description)
	j.FetchedAt = store.NowISO()
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
