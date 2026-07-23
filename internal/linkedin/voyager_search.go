package linkedin

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/store"
)

// voyagerJobCardsURL is the authenticated search XHR the browser fires when you
// scroll the left panel of /jobs/search/ while signed in. It returns a larger,
// more complete result set than the anonymous guest endpoint
// (seeMoreJobPostings) and exposes a paging.total so we know when to stop.
const voyagerJobCardsURL = "https://www.linkedin.com/voyager/api/voyagerJobsDashJobCards"

// voyagerSearchDecoration is the decorationId the browser requests for the full
// job-card collection. It controls which fields the normalized JSON includes
// (title, company, location, logo, …). If LinkedIn bumps the version the
// request will 4xx/5xx and SearchURL falls back to the guest endpoint.
const voyagerSearchDecoration = "com.linkedin.voyager.dash.deco.jobs.search.JobSearchCardsCollection-220"

// textVM is a LinkedIn Voyager TextViewModel — a normalized-JSON object whose
// only field we care about here is the rendered text.
type textVM struct {
	Text string `json:"text"`
}

// jobsFromVoyagerSearch paginates the authenticated Voyager job-card API — the
// same XHR the signed-in browser fires when scrolling /jobs/search/. It returns
// the full result set the signed-in user sees (the anonymous guest endpoint
// returns a smaller/different set and caps early), so --top can pull more than
// the first page. Requires a CSRF-bearing session; SearchURL only calls this
// when HasSession(). maxJobs <= 0 means pull until the last page.
func (c *Client) jobsFromVoyagerSearch(rawURL string, maxJobs int) ([]*models.JobPosting, error) {
	query, ok := voyagerSearchQuery(rawURL)
	if !ok {
		return nil, errf("voyager search requires a keywords filter")
	}
	var out []*models.JobPosting
	seen := map[string]bool{}
	const pageSize = 25
	total := -1
	for start := 0; ; start += pageSize {
		if maxJobs > 0 && start >= maxJobs {
			break
		}
		params := url.Values{}
		params.Set("decorationId", voyagerSearchDecoration)
		params.Set("count", itoa(pageSize))
		params.Set("q", "jobSearch")
		params.Set("query", query)
		params.Set("start", itoa(start))
		apiURL := voyagerJobCardsURL + "?" + params.Encode()

		hdr := http.Header{}
		hdr.Set("Accept", "application/vnd.linkedin.normalized+json+2.1")
		hdr.Set("Csrf-Token", c.session.CSRFToken)
		hdr.Set("X-Restli-Protocol-Version", "2.0.0")
		hdr.Set("X-Li-Lang", "en_US")
		hdr.Set("Referer", "https://www.linkedin.com/jobs/search/")

		body, _, status, err := c.get(apiURL, true, hdr)
		if err != nil {
			return out, err
		}
		if status != 200 {
			return out, errf("voyager jobCards API returned status %d", status)
		}
		cards, pageTotal, err := parseVoyagerJobCards(body)
		if err != nil {
			return out, err
		}
		if total < 0 {
			total = pageTotal
		}
		for _, j := range cards {
			if seen[j.ID] {
				continue
			}
			seen[j.ID] = true
			j.Source = "url"
			out = append(out, j)
		}
		// Stop on last page (fewer than a full page), when we've reached the
		// reported total, or when a page yields nothing new (defensive guard).
		if len(cards) < pageSize {
			break
		}
		if total >= 0 && len(out) >= total {
			break
		}
		if maxJobs > 0 && len(out) >= maxJobs {
			break
		}
	}
	return out, nil
}

// voyagerSearchQuery builds the Restli compact-JSON `query` param the Voyager
// jobCards API expects, mapping the standard LinkedIn URL filters onto their
// Voyager field names:
//
//	keywords                 -> keywords
//	geoId                    -> locationUnion.geoId
//	sortBy                   -> selectedFilters.sortBy
//	distance                 -> selectedFilters.distance
//	f_TPR (time posted range) -> selectedFilters.timePostedRange
//
// Returns ok=false when there is no keywords filter (the endpoint requires it).
// Only the filters present in the URL are emitted; unknown filters are ignored
// so the search still resolves. The search result set is defined entirely by
// these filters, so omitting tracking context (origin/currentJobId) is safe.
func voyagerSearchQuery(rawURL string) (string, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	q := u.Query()
	keywords := strings.TrimSpace(q.Get("keywords"))
	if keywords == "" {
		return "", false
	}
	var b strings.Builder
	b.WriteString("(keywords:")
	b.WriteString(escapeRestli(keywords))
	if geoID := strings.TrimSpace(q.Get("geoId")); geoID != "" {
		// geoId may carry multiple comma-separated values (e.g.
		// "100025096,101788145" when the user picked two regions in the
		// browser). A bare comma breaks the Restli compact-JSON structure, so
		// multiple values are wrapped in List(...); a single value stays flat.
		b.WriteString(",locationUnion:(geoId:")
		parts := strings.Split(geoID, ",")
		if len(parts) == 1 {
			b.WriteString(strings.TrimSpace(parts[0]))
		} else {
			b.WriteString("List(")
			for i, p := range parts {
				if i > 0 {
					b.WriteString(",")
				}
				b.WriteString(strings.TrimSpace(p))
			}
			b.WriteString(")")
		}
		b.WriteString("))")
	}
	var filters []string
	if v := strings.TrimSpace(q.Get("sortBy")); v != "" {
		filters = append(filters, "sortBy:List("+v+")")
	}
	if v := strings.TrimSpace(q.Get("distance")); v != "" {
		filters = append(filters, "distance:List("+v+")")
	}
	if v := strings.TrimSpace(q.Get("f_TPR")); v != "" {
		filters = append(filters, "timePostedRange:List("+v+")")
	}
	if len(filters) > 0 {
		b.WriteString(",selectedFilters:(")
		b.WriteString(strings.Join(filters, ","))
		b.WriteString(")")
	}
	b.WriteString(",spellCorrectionEnabled:true)")
	return b.String(), true
}

// escapeRestli escapes a string for inclusion as a bare value in LinkedIn's
// Restli compact-JSON syntax (backslash escapes the metacharacters). Spaces,
// letters and digits are left intact.
func escapeRestli(s string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`(`, `\(`,
		`)`, `\)`,
		`,`, `\,`,
		`:`, `\:`,
	).Replace(s)
}

// parseVoyagerJobCards decodes a voyagerJobsDashJobCards normalized-JSON
// response into job postings and reports the total result count from
// data.paging.total (0 when absent). The response is LinkedIn's compact format:
// data.elements[i].jobCardUnion.*jobPostingCard holds an entityUrn that resolves
// into the top-level `included` array, where the card fields (title, company,
// location) live.
func parseVoyagerJobCards(body string) ([]*models.JobPosting, int, error) {
	var resp struct {
		Data struct {
			Elements []struct {
				JobCardUnion struct {
					JobPostingCard string `json:"*jobPostingCard"`
				} `json:"jobCardUnion"`
			} `json:"elements"`
			Paging struct {
				Total int `json:"total"`
			} `json:"paging"`
		} `json:"data"`
		Included []struct {
			EntityUrn                      string  `json:"entityUrn"`
			PreDashNormalizedJobPostingUrn string  `json:"preDashNormalizedJobPostingUrn"`
			JobPostingUrn                  string  `json:"jobPostingUrn"`
			JobPostingTitle                string  `json:"jobPostingTitle"`
			PrimaryDescription             *textVM `json:"primaryDescription"`
			SecondaryDescription           *textVM `json:"secondaryDescription"`
		} `json:"included"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, 0, err
	}
	byURN := make(map[string]int, len(resp.Included))
	for i, inc := range resp.Included {
		if inc.EntityUrn != "" {
			byURN[inc.EntityUrn] = i
		}
	}
	var out []*models.JobPosting
	for _, el := range resp.Data.Elements {
		ref := el.JobCardUnion.JobPostingCard
		if ref == "" {
			continue
		}
		idx, ok := byURN[ref]
		if !ok {
			continue
		}
		inc := resp.Included[idx]
		id := voyagerJobID(inc.PreDashNormalizedJobPostingUrn, inc.JobPostingUrn, inc.EntityUrn)
		if id == "" {
			continue
		}
		title := strings.TrimSpace(inc.JobPostingTitle)
		if title == "" {
			title = "Unknown Title"
		}
		loc := textVMText(inc.SecondaryDescription)
		out = append(out, &models.JobPosting{
			ID:         id,
			Title:      title,
			Company:    textVMText(inc.PrimaryDescription),
			Location:   loc,
			URL:        "https://www.linkedin.com/jobs/view/" + id + "/",
			Source:     "url",
			SearchedAt: store.NowISO(),
			RemoteType: DetectRemote(loc),
		})
	}
	return out, resp.Data.Paging.Total, nil
}

// voyagerJobID extracts the numeric LinkedIn job ID from the first URN that
// matches the jobPosting:<digits> shape (preDashNormalizedJobPostingUrn and
// jobPostingUrn both qualify; the card entityUrn does not).
func voyagerJobID(urns ...string) string {
	for _, u := range urns {
		if m := jobIDRE.FindStringSubmatch(u); m != nil {
			return m[1]
		}
	}
	return ""
}

// textVMText returns the trimmed .text of a TextViewModel, or "" when nil.
func textVMText(vm *textVM) string {
	if vm == nil {
		return ""
	}
	return strings.TrimSpace(vm.Text)
}
