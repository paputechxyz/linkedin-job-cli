package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/models"
)

const enrichSystem = "You are an expert technical recruiter assistant. Analyze a job posting and extract structured facts, then rate it against the user's rubrics. Be objective — report what the posting actually says."

// enrichPromptTmpl is the base fact-extraction template. The dynamic rubric
// block (built at call time from the active rubric set) is interpolated into
// the "ratings" instruction so the LLM knows which rubrics to rate 1–5.
const enrichPromptTmpl = `Analyze this job posting and return ONLY a JSON object (no prose, no code fences) with EXACTLY these keys:
"company_overview": 1-2 sentences on what the company does,
"industry": the company's industry,
"tech_stack": comma-separated technologies required,
"seniority": one of intern|junior|mid|senior|staff|principal|lead|manager|director,
"employment_type": one of full-time|part-time|contract|temporary|internship,
"years_experience": integer minimum years required,
"company_size_band": one of 1-10|11-50|51-200|201-1000|1000+,
"company_stage": one of seed|early|growth|mature|public,
"is_founding_role": boolean (founding/founding-engineer role),
"visa_sponsorship": one of yes|no|unknown,
"work_arrangement": one of remote|hybrid|onsite|unknown,
"ratings": an object mapping each rubric id listed below to an integer 1-5, where 1 = strong miss/negative, 2 = weak, 3 = neutral or not mentioned, 4 = good, 5 = strong match. Rate every listed rubric.

Rubrics to rate (id: what to look for):
%s

Job Title: %s
Company: %s
Location: %s
Salary: %s

Job description:
%s`

// ErrEmptyDescription means the job had no description body to analyze. The
// caller (ingest) treats this as a non-fatal miss: it must NOT persist
// enriched_at/scored_at, otherwise the dedup gate would hide the job from
// future retries (a real regression risk now that LinkedIn's detail page is a
// SPA whose description sometimes fails to load).
var ErrEmptyDescription = errors.New("job description is empty; cannot enrich")

// Enrich runs the LLM extraction call for one job and returns the structured
// facts plus a per-rubric 1–5 rating map for the dynamic rubrics. System
// rubrics are NOT rated here — they are computed deterministically by
// internal/score. An empty description yields ErrEmptyDescription without any
// HTTP call so the caller can skip silently persisting a no-op result. A
// transport/HTTP failure returns an error; an unparseable response never errors
// (it yields a partial Enrichment + empty ratings).
func Enrich(j *models.JobPosting, provider *Provider, rubrics []config.Rubric) (models.Enrichment, map[string]int, error) {
	if strings.TrimSpace(j.Description) == "" {
		return models.Enrichment{}, nil, ErrEmptyDescription
	}
	content, err := requestEnrichment(j, provider, rubrics)
	if err != nil {
		return models.Enrichment{}, nil, err
	}
	e, ratings := parseEnrichment(content)
	return e, ratings, nil
}

func orNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}

// dynamicRubricBlock lists the dynamic rubrics the LLM must rate. System
// rubrics are excluded (computed in Go). Returns a placeholder line when there
// are no dynamic rubrics so the ratings key is still well-formed.
func dynamicRubricBlock(rubrics []config.Rubric) string {
	var lines []string
	for _, r := range rubrics {
		if r.Kind != "system" {
			line := r.ID + ": " + r.Description
			if len(r.Items) > 0 {
				line += " (items: " + strings.Join(r.Items, ", ") + ")"
			}
			lines = append(lines, "- "+line)
		}
	}
	if len(lines) == 0 {
		return "- (none — return an empty ratings object {})"
	}
	return strings.Join(lines, "\n")
}

func enrichPrompt(j *models.JobPosting, rubrics []config.Rubric) string {
	desc := j.Description
	if len(desc) > 4000 {
		desc = desc[:4000]
	}
	return fmt.Sprintf(enrichPromptTmpl, dynamicRubricBlock(rubrics), j.Title, orNA(j.Company), orNA(j.Location),
		j.SalaryDisplay(), desc)
}

func requestEnrichment(j *models.JobPosting, provider *Provider, rubrics []config.Rubric) (string, error) {
	return Chat(provider, enrichSystem, enrichPrompt(j, rubrics), 4096, 0.2)
}

// truncateForError bounds an error body to 256 bytes and scrubs newlines so a
// misconfigured upstream that echoes request material cannot dump a reflected
// token or PII into the terminal/logs.
func truncateForError(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 256 {
		return s[:256] + "…"
	}
	return s
}

type enrichJSON struct {
	CompanyOverview string `json:"company_overview"`
	Industry        string `json:"industry"`
	TechStack       string `json:"tech_stack"`
	Seniority       string `json:"seniority"`
	EmploymentType  string `json:"employment_type"`
	YearsExperience *int   `json:"years_experience"`
	CompanySizeBand string `json:"company_size_band"`
	CompanyStage    string `json:"company_stage"`
	IsFoundingRole  bool   `json:"is_founding_role"`
	VisaSponsorship string `json:"visa_sponsorship"`
	WorkArrangement string `json:"work_arrangement"`
	Ratings         map[string]int `json:"ratings"`
}

func parseEnrichment(content string) (models.Enrichment, map[string]int) {
	var ej enrichJSON
	if jstr := extractJSON(content); jstr != "" {
		if err := json.Unmarshal([]byte(jstr), &ej); err == nil {
			return toEnrichment(ej), clampRatings(ej.Ratings)
		}
	}
	// Delimiter/labeled-prose fallback (facts only; ratings left empty).
	return toEnrichment(parseDelimiter(content)), nil
}

// clampRatings forces every rating into [1,5].
func clampRatings(r map[string]int) map[string]int {
	if len(r) == 0 {
		return nil
	}
	for k, v := range r {
		if v < 1 {
			r[k] = 1
		}
		if v > 5 {
			r[k] = 5
		}
	}
	return r
}

// extractJSON pulls the outermost {...} block out of content that may be wrapped
// in markdown code fences or surrounded by prose. Returns "" if none found.
func extractJSON(content string) string {
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

func toEnrichment(ej enrichJSON) models.Enrichment {
	return models.Enrichment{
		CompanyOverview: strings.TrimSpace(ej.CompanyOverview),
		Industry:        strings.TrimSpace(ej.Industry),
		TechStack:       strings.TrimSpace(ej.TechStack),
		Seniority:       normalizeEnum(ej.Seniority, seniorityVals),
		EmploymentType:  normalizeEnum(ej.EmploymentType, employmentVals),
		YearsExperience: ej.YearsExperience,
		CompanySizeBand: normalizeEnum(ej.CompanySizeBand, sizeVals),
		CompanyStage:    normalizeEnum(ej.CompanyStage, stageVals),
		IsFoundingRole:  ej.IsFoundingRole,
		VisaSponsorship: normalizeEnum(ej.VisaSponsorship, visaVals),
		WorkArrangement: normalizeArrangement(ej.WorkArrangement),
	}
}

var (
	seniorityVals  = []string{"intern", "junior", "mid", "senior", "staff", "principal", "lead", "manager", "director"}
	employmentVals = []string{"full-time", "part-time", "contract", "temporary", "internship"}
	sizeVals       = []string{"1-10", "11-50", "51-200", "201-1000", "1000+"}
	stageVals      = []string{"seed", "early", "growth", "mature", "public"}
	visaVals       = []string{"yes", "no", "unknown"}
)

func normalizeEnum(v string, allowed []string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	for _, a := range allowed {
		if v == a {
			return a
		}
	}
	// accept "full time" -> "full-time" style spacing
	v2 := strings.ReplaceAll(v, " ", "-")
	for _, a := range allowed {
		if v2 == a {
			return a
		}
	}
	return ""
}

func normalizeArrangement(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "remote":
		return "remote"
	case "hybrid":
		return "hybrid"
	case "onsite", "on-site", "on site", "office", "in-office":
		return "onsite"
	}
	return ""
}

// parseDelimiter extracts labeled fields from a non-JSON response of the form
// "Label: value || Label: value" or newline-separated "Label: value". Misses are
// left zero-valued so partial extraction still works. Ratings are not parsed
// here (the structured fallback is facts-only).
func parseDelimiter(content string) enrichJSON {
	var ej enrichJSON
	segs := splitDelimiters(content)
	set := map[string]*string{
		"company_overview": &ej.CompanyOverview, "company": &ej.CompanyOverview,
		"industry":   &ej.Industry,
		"tech_stack": &ej.TechStack, "stack": &ej.TechStack, "tech stack": &ej.TechStack,
		"seniority":       &ej.Seniority,
		"employment_type": &ej.EmploymentType, "employment": &ej.EmploymentType,
		"company_size_band": &ej.CompanySizeBand, "size": &ej.CompanySizeBand, "company size": &ej.CompanySizeBand,
		"company_stage": &ej.CompanyStage, "stage": &ej.CompanyStage,
		"visa_sponsorship": &ej.VisaSponsorship, "visa": &ej.VisaSponsorship,
		"work_arrangement": &ej.WorkArrangement, "remote": &ej.WorkArrangement, "arrangement": &ej.WorkArrangement,
	}
	for _, seg := range segs {
		label, val := splitLabel(seg)
		if label == "" {
			continue
		}
		lbl := strings.ToLower(strings.TrimSpace(label))
		val = strings.TrimSpace(val)
		if p, ok := set[lbl]; ok {
			*p = val
		}
		switch lbl {
		case "years_experience", "years":
			if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
				ej.YearsExperience = &n
			}
		case "is_founding_role", "founding":
			ej.IsFoundingRole = parseBool(val)
		}
	}
	return ej
}

func splitDelimiters(content string) []string {
	if content == "" {
		return nil
	}
	// Prefer || separators; otherwise split on newlines.
	if strings.Contains(content, "||") {
		parts := strings.Split(content, "||")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if strings.TrimSpace(p) != "" {
				out = append(out, p)
			}
		}
		return out
	}
	var out []string
	for _, ln := range strings.Split(content, "\n") {
		if strings.TrimSpace(ln) != "" {
			out = append(out, ln)
		}
	}
	return out
}

func splitLabel(seg string) (string, string) {
	idx := strings.Index(seg, ":")
	if idx == -1 {
		return "", ""
	}
	return seg[:idx], seg[idx+1:]
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "y", "1":
		return true
	}
	return false
}
