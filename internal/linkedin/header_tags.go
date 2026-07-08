package linkedin

import (
	"encoding/json"
	"net/http"
)

// HeaderTags is the authoritative workplace metadata for a job, sourced
// directly from LinkedIn's Voyager jobPostings API. This is the structured
// "header tag" LinkedIn renders as the Remote/Hybrid/On-site badge next to the
// job title — distinct from DetectRemote, which infers remote_type from
// description prose. Critics compares this against the stored remote_type to
// catch cases where the parser's heuristic overrode LinkedIn's badge.
type HeaderTags struct {
	JobID             string   `json:"job_id"`
	WorkplaceTypeURNs []string `json:"workplace_type_urns"`
	WorkRemoteAllowed bool     `json:"work_remote_allowed"`
	// RemoteType is the derived vocabulary value (remote|hybrid|onsite|"")
	// matching the jobs.remote_type column. Empty when the API returned no
	// recognizable workplace signal.
	RemoteType string `json:"remote_type"`
	// Source is "voyager_api" on a successful parse, "" on any soft miss
	// (no session, non-200, shape mismatch) so callers can tell a genuine
	// "LinkedIn says nothing" apart from "we couldn't ask".
	Source string `json:"source"`
}

// FetchHeaderTags pulls the workplace type URN(s) and workRemoteAllowed flag
// for a job from the Voyager jobPostings API — the authoritative source for
// the remote_type field. Requires a CSRF-bearing session (authenticated calls
// only). Returns Source="" with empty fields on any soft miss so callers can
// treat it as a non-fatal miss rather than an error; a non-nil error indicates
// a transport-level failure worth surfacing.
func (c *Client) FetchHeaderTags(id string) (HeaderTags, error) {
	ht := HeaderTags{JobID: id}
	if id == "" || !c.HasSession() || c.session.CSRFToken == "" {
		return ht, nil
	}
	url := "https://www.linkedin.com/voyager/api/jobs/jobPostings/" + id
	hdr := http.Header{
		"Referer":                   {"https://www.linkedin.com/jobs/view/" + id + "/"},
		"X-Restli-Protocol-Version": {"2.0.0"},
		"Csrf-Token":                {c.session.CSRFToken},
	}
	body, status, err := c.getJSON(url, true, hdr)
	if err != nil {
		return ht, err
	}
	if status != 200 {
		return ht, errf("jobPostings API returned status %d for %s", status, id)
	}
	// Same two-shape parsing as jobPostingAPIFields (flat root + legacy
	// data-envelope). Kept inline rather than shared because HeaderTags needs
	// the raw URNs/flag for evidentiary output, not just the derived
	// remoteType. Both shapes carry the same underlying data, so the root
	// fields win and "data" only fills gaps.
	var resp struct {
		WorkplaceTypes    []string `json:"workplaceTypes"`
		WorkRemoteAllowed bool     `json:"workRemoteAllowed"`
		Data struct {
			WorkplaceTypes    []string `json:"workplaceTypes"`
			WorkRemoteAllowed bool     `json:"workRemoteAllowed"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil {
		return ht, nil
	}
	urns := resp.WorkplaceTypes
	if len(urns) == 0 {
		urns = resp.Data.WorkplaceTypes
	}
	workRemote := resp.WorkRemoteAllowed || resp.Data.WorkRemoteAllowed
	ht.WorkplaceTypeURNs = urns
	ht.WorkRemoteAllowed = workRemote
	ht.RemoteType = workplaceTypeFromURNs(urns)
	if ht.RemoteType == "" && workRemote {
		ht.RemoteType = "remote"
	}
	ht.Source = "voyager_api"
	return ht, nil
}
