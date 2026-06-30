package linkedin

import (
	"encoding/json"
	"net/http"
	"strings"

	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/store"
)

const (
	recommendedGraphQL = "https://www.linkedin.com/voyager/api/graphql"
	recommendedQueryID = "voyagerJobsDashJobCards.e5b6b761ede078dabe8ad857aa42c220"
	jobPostingCardType = "com.linkedin.voyager.dash.jobs.JobPostingCard"
)

// Recommended fetches the authenticated "Recommended for you" job collection.
// maxJobs caps how many cards are fetched (pagination is 25/page). Requires a
// session; returns card-level data only (id/title/company/location). Fetch
// detail pages separately for salary + description.
func (c *Client) Recommended(maxJobs int) ([]*models.JobPosting, error) {
	if !c.HasSession() {
		return nil, ErrAuthRequired
	}
	// HasSession only guarantees *some* cookies are present. The Voyager
	// GraphQL endpoint additionally requires a csrf-token header derived from
	// the JSESSIONID cookie. If that's missing, LinkedIn returns 403
	// "CSRF check failed" — fail early with an actionable message instead.
	if c.session.CSRFToken == "" {
		return nil, errf("session is missing JSESSIONID (cannot derive csrf-token); re-run `linkedin-jobs auth login`")
	}
	var out []*models.JobPosting
	seen := map[string]bool{}
	pageSize := 25
	for start := 0; maxJobs <= 0 || len(out) < maxJobs; start += pageSize {
		q := "includeWebMetadata=true&variables=(count:" + itoa(pageSize) +
			",jobCollectionSlug:recommended,query:(origin:GENERIC_JOB_COLLECTIONS_LANDING),start:" +
			itoa(start) + ")&queryId=" + recommendedQueryID
		hdr := http.Header{
			"Referer":                   {"https://www.linkedin.com/jobs/collections/recommended/"},
			"X-Restli-Protocol-Version": {"2.0.0"},
			"Csrf-Token":                {c.session.CSRFToken},
		}
		body, status, err := c.getJSON(recommendedGraphQL+"?"+q, true, hdr)
		if err != nil {
			return out, err
		}
		if status == 401 || status == 403 {
			// 403 with "CSRF check failed" means the csrf-token didn't match
			// the JSESSIONID (or the session expired). Distinguish it so the
			// user knows their captured session is stale, not just rejected.
			hint := "re-run `linkedin-jobs auth login`"
			if strings.Contains(strings.ToLower(body), "csrf") {
				hint = "csrf-token rejected (stale/incomplete session) — re-run `linkedin-jobs auth login`"
			}
			return out, errf("LinkedIn rejected the session (status %d) — %s", status, hint)
		}
		if status != 200 {
			return out, errf("recommended request returned status %d", status)
		}
		jobs := parseRecommended(body)
		if len(jobs) == 0 {
			break
		}
		for _, j := range jobs {
			if seen[j.ID] {
				continue
			}
			seen[j.ID] = true
			out = append(out, j)
			if maxJobs > 0 && len(out) >= maxJobs {
				break
			}
		}
		if len(jobs) < pageSize {
			break
		}
	}
	return out, nil
}

type graphqlResp struct {
	Data struct {
		Data struct {
			Jobs struct {
				Elements []struct {
					JobCard map[string]json.RawMessage `json:"jobCard"`
				} `json:"elements"`
			} `json:"jobsDashJobCardsByJobCollections"`
		} `json:"data"`
	} `json:"data"`
	Included []map[string]interface{} `json:"included"`
}

// parseRecommended extracts JobPosting cards from the GraphQL response.
//
// LinkedIn has shipped two response shapes for this query; both are handled:
//
//  1. NEW (current, denormalized): the card entity is inlined directly.
//     data.jobsDashJobCardsByJobCollections.elements[*].jobCard.jobPostingCard
//     There is no top-level `included` array.
//
//  2. OLD (normalized entity graph, captured in testdata): the card has a
//     "*jobPostingCard" reference string that points into a top-level
//     `included[]` array indexed by `entityUrn`.
//
// The card entity's fields (jobPostingTitle, primaryDescription, etc.) are
// identical across both shapes, so cardEntityToJob handles either once the
// entity is located.
func parseRecommended(body string) []*models.JobPosting {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil
	}
	data, _ := raw["data"].(map[string]interface{})
	if data == nil {
		return nil
	}

	// Locate the jobsDashJobCardsByJobCollections node. Try NEW shape first
	// (data.X), fall back to OLD shape (data.data.X).
	jcbjc, _ := data["jobsDashJobCardsByJobCollections"].(map[string]interface{})
	if jcbjc == nil {
		if inner, _ := data["data"].(map[string]interface{}); inner != nil {
			jcbjc, _ = inner["jobsDashJobCardsByJobCollections"].(map[string]interface{})
		}
	}
	if jcbjc == nil {
		return nil
	}
	elements, _ := jcbjc["elements"].([]interface{})
	if len(elements) == 0 {
		return nil
	}

	// OLD shape only: build an entityUrn -> entity index from the top-level
	// `included` array so we can resolve "*jobPostingCard" references.
	byURN := map[string]map[string]interface{}{}
	if included, _ := raw["included"].([]interface{}); included != nil {
		for _, e := range included {
			em, _ := e.(map[string]interface{})
			if em == nil {
				continue
			}
			if t, _ := em["$type"].(string); t == jobPostingCardType {
				if urn, _ := em["entityUrn"].(string); urn != "" {
					byURN[urn] = em
				}
			}
		}
	}

	var out []*models.JobPosting
	added := map[string]bool{}
	for _, el := range elements {
		elm, _ := el.(map[string]interface{})
		if elm == nil {
			continue
		}
		jobCard, _ := elm["jobCard"].(map[string]interface{})
		if jobCard == nil {
			continue
		}

		// Resolve the JobPostingCard entity for this element.
		var entity map[string]interface{}
		if jpc, _ := jobCard["jobPostingCard"].(map[string]interface{}); jpc != nil {
			// NEW shape: inline entity.
			entity = jpc
		} else if ref, _ := jobCard["*jobPostingCard"].(string); ref != "" {
			// OLD shape: URN reference into `included`.
			entity = byURN[ref]
		}
		if entity == nil {
			continue
		}

		j := cardEntityToJob(entity)
		if j == nil || added[j.ID] {
			continue
		}
		added[j.ID] = true
		out = append(out, j)
	}
	return out
}

func cardEntityToJob(e map[string]interface{}) *models.JobPosting {
	id := jobIDFromEntity(e)
	if id == "" {
		return nil
	}
	j := &models.JobPosting{
		ID:         id,
		URL:        "https://www.linkedin.com/jobs/view/" + id + "/",
		Source:     "recommended",
		SearchedAt: store.NowISO(),
	}
	if t, _ := e["jobPostingTitle"].(string); t != "" {
		j.Title = t
	} else {
		j.Title = textOf(e["title"])
	}
	j.Company = textOf(e["primaryDescription"])
	j.Location = textOf(e["secondaryDescription"])
	j.RemoteType = DetectRemote(j.Location)
	if listed, promoted := footerInfo(e); listed > 0 {
		j.ListedAt = listed
		_ = promoted
	}
	return j
}

func jobIDFromEntity(e map[string]interface{}) string {
	// *jobPosting -> "urn:li:fsd_jobPosting:ID"
	if v, ok := e["*jobPosting"].(string); ok {
		if i := strings.LastIndex(v, ":"); i >= 0 {
			return v[i+1:]
		}
	}
	// entityUrn -> "urn:li:fsd_jobPostingCard:(ID,...)"
	if v, ok := e["entityUrn"].(string); ok {
		if i := strings.Index(v, ":("); i >= 0 {
			rest := v[i+2:]
			if j := strings.IndexByte(rest, ','); j >= 0 {
				return rest[:j]
			}
			if j := strings.IndexByte(rest, ')'); j >= 0 {
				return rest[:j]
			}
		}
	}
	return ""
}

// textOf extracts ".text" from a TextViewModel-like object, or returns a bare string.
func textOf(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]interface{}:
		if s, ok := t["text"].(string); ok {
			return s
		}
	}
	return ""
}

// footerInfo reads footerItems for LISTED_DATE (epoch ms) and PROMOTED flag.
func footerInfo(e map[string]interface{}) (listedAt int64, promoted bool) {
	items, ok := e["footerItems"].([]interface{})
	if !ok {
		return
	}
	for _, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		switch m["type"] {
		case "LISTED_DATE":
			if n, ok := m["timeAt"].(float64); ok {
				listedAt = int64(n)
			}
		case "PROMOTED":
			promoted = true
		}
	}
	return
}
