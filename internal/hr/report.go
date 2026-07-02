// Package hr researches the best person to reach out to about a job posting.
// Given a job's company + context, it identifies the highest-leverage contact
// role (and, when the LLM is confident, a named person), explains why, and
// generates ready-to-click LinkedIn people-search URLs scoped to that company.
package hr

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"

	"linkedin-jobs/internal/linkedin"
	"linkedin-jobs/internal/llm"
)

// Contact is one recommended person to reach out to. Name is set only when the
// LLM is confident from its training data; otherwise Role is the target and
// SearchURL finds matching people at the company.
type Contact struct {
	Name        string `json:"name,omitempty"`
	Role        string `json:"role"`
	SearchTerms string `json:"search_terms,omitempty"`
	Priority    int    `json:"priority"`
	Why         string `json:"why,omitempty"`
	SearchURL   string `json:"search_url,omitempty"`
}

// Report is the full outreach-research result for one job.
type Report struct {
	JobID          string    `json:"job_id"`
	JobURL         string    `json:"job_url"`
	Title          string    `json:"title"`
	Company        string    `json:"company"`
	CompanyID      string    `json:"company_id,omitempty"`
	CompanySlug    string    `json:"company_slug,omitempty"`
	CompanyURL     string    `json:"company_url,omitempty"`
	CompanyAbout   string    `json:"company_about,omitempty"`
	TargetRole     string    `json:"target_role"`
	Reasoning      string    `json:"reasoning"`
	Contacts       []Contact `json:"contacts"`
	OutreachAngle  string    `json:"outreach_angle,omitempty"`
	ApplicantCount string    `json:"applicant_count,omitempty"`
	// LLMUsed reports whether the recommendation came from the LLM (true) or
	// the deterministic heuristic fallback (false).
	LLMUsed bool `json:"llm_used"`
}

const hrSystem = "You are an expert at landing startup and tech roles by reaching out to the right person. " +
	"Given a job posting, identify the single best person to contact to get the role noticed, " +
	"rank the top alternatives, and explain the reasoning. Be concrete and specific to THIS company and role."

const hrPromptTmpl = `A candidate wants to apply for the job below and reach out to the best person at the company to maximize their chances. Recommend who to contact.

Job title: %s
Company: %s
Location: %s
Seniority: %s
Employment type: %s
Applicants so far: %s

Company profile:
%s

Job description:
%s

Decide who the best person to reach out to is. For a small startup or founding role that is almost always a founder / CTO / hiring manager who makes the decision (there is no recruiter gate). For a larger company it is more often the hiring manager for the team or the recruiter/talent partner coordinating the role. Use the job description as the strongest signal (e.g. "work directly with the CTO" -> target the CTO).

Return ONLY a JSON object (no prose, no code fences) with EXACTLY these keys:
"target_role": the role/title of the single best person to contact,
"reasoning": 1-3 sentences on why that is the best contact for THIS role,
"outreach_angle": a concrete one-sentence hook the candidate can lead with, tied to the company or role,
"contacts": an array of 2-4 objects, best first, each with:
  "name": a specific person's name ONLY if you are highly confident from your training data (e.g. well-known founders/execs); otherwise empty string,
  "role": the target role/title to look for,
  "search_terms": a short LinkedIn-people-search keyword to plug into the title facet for this role (e.g. "CTO", "Head of Talent", "Recruiter", "Engineering Manager"),
  "priority": integer, 1 for the best contact,
  "why": one short sentence on why this contact.

Do not invent or guess names you are not sure about — leave "name" empty and the candidate will use the search link to find the exact person.`

// Generate researches the best contact using the LLM. It never returns an error
// for a soft LLM miss (unparseable JSON) — it falls back to the heuristic so the
// command always produces a usable report. A transport/HTTP failure returns the
// error so the caller can decide whether to fall back.
func Generate(ctx *linkedin.JobContext, co *linkedin.CompanyProfile, provider *llm.Provider) (*Report, error) {
	r := baseReport(ctx, co)
	content, err := llm.Chat(provider, hrSystem, buildPrompt(ctx, co), 2048, 0.3)
	if err != nil {
		return nil, err
	}
	if parsed := parseReport(content); parsed != nil {
		applyParsed(r, parsed)
		r.LLMUsed = true
		return r, nil
	}
	// Unparseable: keep the base report but enrich with the heuristic targets
	// so the output is still actionable.
	h := heuristicContacts(ctx)
	r.Contacts = attachURLs(h, r.CompanyID, r.CompanySlug)
	r.TargetRole = h[0].Role
	r.Reasoning = heuristicReasoning(ctx)
	r.LLMUsed = false
	return r, nil
}

// Heuristic builds a report without an LLM, using job-title + company-size
// signals to pick target roles. Always produces actionable search URLs.
func Heuristic(ctx *linkedin.JobContext, co *linkedin.CompanyProfile) *Report {
	r := baseReport(ctx, co)
	h := heuristicContacts(ctx)
	r.Contacts = attachURLs(h, r.CompanyID, r.CompanySlug)
	r.TargetRole = h[0].Role
	r.Reasoning = heuristicReasoning(ctx)
	r.OutreachAngle = ""
	r.LLMUsed = false
	return r
}

// baseReport fills the identity/company fields shared by both paths.
func baseReport(ctx *linkedin.JobContext, co *linkedin.CompanyProfile) *Report {
	r := &Report{
		JobID:          ctx.JobID,
		JobURL:         ctx.URL,
		Title:          ctx.Title,
		Company:        ctx.Company,
		CompanyID:      ctx.CompanyID,
		CompanySlug:    ctx.CompanySlug,
		ApplicantCount: ctx.ApplicantCount,
	}
	if ctx.CompanySlug != "" {
		r.CompanyURL = "https://www.linkedin.com/company/" + ctx.CompanySlug + "/"
	}
	if co != nil {
		r.CompanyAbout = firstNonEmpty(co.Tagline, co.About)
		if r.Company == "" {
			r.Company = co.Name
		}
	}
	return r
}

func buildPrompt(ctx *linkedin.JobContext, co *linkedin.CompanyProfile) string {
	desc := ctx.Description
	if len(desc) > 3000 {
		desc = desc[:3000]
	}
	companyBlock := "unknown"
	if co != nil {
		var b strings.Builder
		if co.Name != "" {
			fmt.Fprintf(&b, "Name: %s\n", co.Name)
		}
		if co.Tagline != "" {
			fmt.Fprintf(&b, "Tagline: %s\n", co.Tagline)
		}
		if co.About != "" {
			a := co.About
			if len(a) > 800 {
				a = a[:800]
			}
			fmt.Fprintf(&b, "About: %s\n", a)
		}
		if co.Industry != "" {
			fmt.Fprintf(&b, "Industry: %s\n", co.Industry)
		}
		if co.Size != "" {
			fmt.Fprintf(&b, "Size: %s\n", co.Size)
		}
		if co.HQ != "" {
			fmt.Fprintf(&b, "HQ: %s\n", co.HQ)
		}
		companyBlock = b.String()
	}
	seniority := ctx.Seniority
	if seniority == "" {
		seniority = "not stated"
	}
	emptype := ctx.EmploymentType
	if emptype == "" {
		emptype = "not stated"
	}
	applicants := ctx.ApplicantCount
	if applicants == "" {
		applicants = "unknown"
	}
	return fmt.Sprintf(hrPromptTmpl,
		firstNonEmpty(ctx.Title, "not stated"),
		firstNonEmpty(ctx.Company, "not stated"),
		firstNonEmpty(ctx.Location, "not stated"),
		seniority, emptype, applicants,
		companyBlock, desc)
}

// parsedReport is the JSON shape we expect from the LLM.
type parsedReport struct {
	TargetRole    string `json:"target_role"`
	Reasoning     string `json:"reasoning"`
	OutreachAngle string `json:"outreach_angle"`
	Contacts      []struct {
		Name        string `json:"name"`
		Role        string `json:"role"`
		SearchTerms string `json:"search_terms"`
		Priority    int    `json:"priority"`
		Why         string `json:"why"`
	} `json:"contacts"`
}

func parseReport(content string) *parsedReport {
	jstr := extractJSONObject(content)
	if jstr == "" {
		return nil
	}
	var pr parsedReport
	if err := json.Unmarshal([]byte(jstr), &pr); err != nil {
		return nil
	}
	if pr.TargetRole == "" && len(pr.Contacts) == 0 {
		return nil
	}
	return &pr
}

func applyParsed(r *Report, pr *parsedReport) {
	r.TargetRole = strings.TrimSpace(pr.TargetRole)
	r.Reasoning = strings.TrimSpace(pr.Reasoning)
	r.OutreachAngle = strings.TrimSpace(pr.OutreachAngle)
	contacts := make([]Contact, 0, len(pr.Contacts))
	for _, c := range pr.Contacts {
		if strings.TrimSpace(c.Role) == "" && strings.TrimSpace(c.Name) == "" {
			continue
		}
		contacts = append(contacts, Contact{
			Name:        strings.TrimSpace(c.Name),
			Role:        strings.TrimSpace(c.Role),
			SearchTerms: strings.TrimSpace(c.SearchTerms),
			Priority:    c.Priority,
			Why:         strings.TrimSpace(c.Why),
		})
	}
	sort.SliceStable(contacts, func(i, j int) bool {
		return contactLess(contacts[i], contacts[j])
	})
	for i := range contacts {
		if contacts[i].Priority == 0 {
			contacts[i].Priority = i + 1
		}
	}
	r.Contacts = attachURLs(contacts, r.CompanyID, r.CompanySlug)
}

// contactLess orders by priority (ascending, 0 sorts last) then by presence of a
// name (named contacts first), keeping the LLM's stated order otherwise.
func contactLess(a, b Contact) bool {
	ap, bp := a.Priority, b.Priority
	if ap == 0 && bp == 0 {
		return a.Name != "" && b.Name == ""
	}
	if ap == 0 {
		return false
	}
	if bp == 0 {
		return true
	}
	return ap < bp
}

// attachURLs fills the SearchURL for each contact using the company id (for a
// scoped people-search facet) or the company slug (company page) as a fallback.
func attachURLs(contacts []Contact, companyID, companySlug string) []Contact {
	for i := range contacts {
		contacts[i].SearchURL = peopleSearchURL(companyID, companySlug, contacts[i].SearchTerms, contacts[i].Role)
	}
	return contacts
}

// peopleSearchURL builds a LinkedIn people-search URL scoped to a company. With
// a numeric company id it produces a facetCurrentCompany query (the most precise
// scope); otherwise it falls back to the company page. titleTerms, when present,
// is added as a title-facet keyword.
func peopleSearchURL(companyID, companySlug, titleTerms, role string) string {
	if companyID == "" {
		if companySlug == "" {
			return "https://www.linkedin.com/"
		}
		return "https://www.linkedin.com/company/" + companySlug + "/"
	}
	u, _ := url.Parse("https://www.linkedin.com/search/results/people/")
	q := u.Query()
	q.Set("facetCurrentCompany", companyID)
	terms := titleTerms
	if terms == "" {
		terms = role
	}
	if terms != "" {
		q.Set("title", terms)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// heuristicContacts derives target roles without an LLM from the job's seniority
// signals and whether it is a founding / small-company role.
func heuristicContacts(ctx *linkedin.JobContext) []Contact {
	if isFounding(ctx) {
		return []Contact{
			{Role: "Co-Founder & CTO / VP Engineering", SearchTerms: "CTO", Priority: 1, Why: "Founding eng roles report to and are decided by a technical founder; no recruiter gate."},
			{Role: "Co-Founder & CEO", SearchTerms: "Founder", Priority: 2, Why: "Co-founders make hiring decisions at early-stage startups."},
			{Role: "Head of Talent / Recruiter", SearchTerms: "Recruiter", Priority: 3, Why: "If a talent lead exists, they coordinate the process."},
		}
	}
	if isManagerLevel(ctx) {
		return []Contact{
			{Role: "VP Engineering / Director", SearchTerms: "VP Engineering", Priority: 1, Why: "Management roles are decided a level above the open role."},
			{Role: "Hiring Manager (team lead)", SearchTerms: "Engineering Manager", Priority: 2, Why: "The direct manager of the team you'd join."},
			{Role: "Recruiter / Talent", SearchTerms: "Recruiter", Priority: 3, Why: "Often the first screen and can route you to the manager."},
		}
	}
	return []Contact{
		{Role: "Recruiter / Talent Partner", SearchTerms: "Recruiter", Priority: 1, Why: "Most reliable first contact at mid/large companies; they route you to the hiring manager."},
		{Role: "Hiring Manager (team lead)", SearchTerms: "Engineering Manager", Priority: 2, Why: "The person you'd work for; reaching out directly stands out."},
	}
}

func heuristicReasoning(ctx *linkedin.JobContext) string {
	if isFounding(ctx) {
		return "This looks like a founding / early-stage engineering role: the hiring decision sits with the founders or a technical lead, and there is no recruiter gate. Reach out to a technical founder directly — ideally the CTO/VP-Eng the role reports to."
	}
	if isManagerLevel(ctx) {
		return "This is a management/lead role: reach out one level above the open role (VP/Director Engineering) or the team's hiring manager, with the recruiter as a parallel track."
	}
	return "For this role the recruiter/talent partner is usually the fastest first contact and can route you to the hiring manager; reaching out to the team's manager directly helps you stand out."
}

func isFounding(ctx *linkedin.JobContext) bool {
	blob := strings.ToLower(ctx.Title + " " + ctx.Description)
	return strings.Contains(blob, "founding") || strings.Contains(blob, "founding engineer") ||
		strings.Contains(blob, "engineer #") || strings.Contains(ctx.Description, "founding team")
}

func isManagerLevel(ctx *linkedin.JobContext) bool {
	t := strings.ToLower(ctx.Title + " " + ctx.Seniority)
	return strings.Contains(t, "manager") || strings.Contains(t, "lead") ||
		strings.Contains(t, "director") || strings.Contains(t, "head of")
}

// Render writes a human-readable outreach report.
func Render(w io.Writer, r *Report) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s", r.Title)
	if r.Company != "" {
		fmt.Fprintf(w, " @ %s", r.Company)
	}
	fmt.Fprintln(w)
	if r.CompanyURL != "" {
		fmt.Fprintf(w, "Company:    %s\n", r.CompanyURL)
	}
	if r.ApplicantCount != "" {
		fmt.Fprintf(w, "Applicants: %s\n", r.ApplicantCount)
	}
	fmt.Fprintf(w, "Job:        %s\n", r.JobURL)
	if r.CompanyAbout != "" {
		about := r.CompanyAbout
		if len(about) > 240 {
			about = about[:240] + "…"
		}
		fmt.Fprintf(w, "About:      %s\n", about)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Best contact: %s\n", orNA(r.TargetRole))
	if r.Reasoning != "" {
		fmt.Fprintf(w, "Why:         %s\n", wrapReason(r.Reasoning))
	}
	if r.OutreachAngle != "" {
		fmt.Fprintf(w, "Hook:        %s\n", wrapReason(r.OutreachAngle))
	}
	if !r.LLMUsed {
		fmt.Fprint(w, "(LLM unavailable — role-level guidance only. Use the links to find the exact person.)\n")
	}
	fmt.Fprintln(w, "\nRanked contacts:")
	for _, c := range r.Contacts {
		renderContact(w, c)
	}
}

func renderContact(w io.Writer, c Contact) {
	fmt.Fprintln(w)
	head := fmt.Sprintf("%d. %s", c.Priority, orNA(c.Role))
	if c.Name != "" {
		head = fmt.Sprintf("%d. %s — %s", c.Priority, c.Name, c.Role)
	}
	fmt.Fprintf(w, "   %s\n", head)
	if c.Why != "" {
		fmt.Fprintf(w, "   why:    %s\n", wrapReason(c.Why))
	}
	if c.SearchURL != "" {
		fmt.Fprintf(w, "   find:   %s\n", c.SearchURL)
	}
}

// wrapReason reflows a reason string to fit ~88-col terminals with a hanging
// indent so multi-sentence LLM output stays readable.
func wrapReason(s string) string {
	const width = 82
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var b strings.Builder
	line := ""
	for _, word := range words {
		if line == "" {
			line = word
			continue
		}
		if len(line)+1+len(word) > width {
			b.WriteString(line)
			b.WriteString("\n          ")
			line = word
		} else {
			line += " " + word
		}
	}
	b.WriteString(line)
	return b.String()
}

func orNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// extractJSONObject pulls the outermost {...} block out of content that may be
// wrapped in markdown code fences or surrounded by prose.
func extractJSONObject(content string) string {
	s := strings.TrimSpace(content)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return s[start : end+1]
}
