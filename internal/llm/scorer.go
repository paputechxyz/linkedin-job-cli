package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"linkedin-jobs/internal/models"
)

const enrichSystem = "You are an expert technical recruiter assistant. Analyze a job posting for an engineering candidate and extract structured facts plus a fit score against the candidate's resume and preferences."

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
"fit_score": integer 0-100 reflecting how well this role fits the candidate's resume and stated preferences,
"fit_reason": one concise sentence explaining the fit (include ONLY when fit_score >= %d; otherwise empty string).

Job Title: %s
Company: %s
Location: %s
Salary: %s

Candidate resume:
%s

Candidate preferences:
%s

Job description:
%s`

// Score runs the combined enrichment + fit-scoring LLM call for one job and
// returns the structured result. An empty description yields a zero result
// without any HTTP call. A transport/HTTP failure returns an error so the caller
// can warn and persist the job unenriched; an unparseable response never errors
// (it yields a partial Enrichment) per KTD2.
func Score(j *models.JobPosting, p *models.Profile, provider *Provider, reasonThreshold int) (models.Enrichment, error) {
	if strings.TrimSpace(j.Description) == "" {
		return models.Enrichment{}, nil
	}
	if reasonThreshold <= 0 || reasonThreshold > 100 {
		reasonThreshold = 70
	}
	content, err := requestEnrichment(j, p, provider, reasonThreshold)
	if err != nil {
		return models.Enrichment{}, err
	}
	return parseEnrichment(content, reasonThreshold), nil
}

func enrichPrompt(j *models.JobPosting, p *models.Profile, reasonThreshold int) string {
	desc := j.Description
	if len(desc) > 4000 {
		desc = desc[:4000]
	}
	resume, prefs := "", ""
	if p != nil {
		resume, prefs = p.ResumeText, p.PreferencesText
	}
	if len(resume) > 2000 {
		resume = resume[:2000]
	}
	if len(prefs) > 1000 {
		prefs = prefs[:1000]
	}
	return fmt.Sprintf(enrichPromptTmpl, reasonThreshold, j.Title, orNA(j.Company), orNA(j.Location),
		j.SalaryDisplay(), orNA(resume), orNA(prefs), desc)
}

func requestEnrichment(j *models.JobPosting, p *models.Profile, provider *Provider, reasonThreshold int) (string, error) {
	reqBody := map[string]interface{}{
		"model":       provider.Model,
		"max_tokens":  700,
		"temperature": 0.2,
		"messages": []map[string]string{
			{"role": "system", "content": enrichSystem},
			{"role": "user", "content": enrichPrompt(j, p, reasonThreshold)},
		},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(provider.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	provider.Apply(req)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("LLM API status %d: %s", resp.StatusCode, truncateForError(string(body)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("LLM returned no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
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
	CompanyOverview  string `json:"company_overview"`
	Industry         string `json:"industry"`
	TechStack        string `json:"tech_stack"`
	Seniority        string `json:"seniority"`
	EmploymentType   string `json:"employment_type"`
	YearsExperience  *int   `json:"years_experience"`
	CompanySizeBand  string `json:"company_size_band"`
	CompanyStage     string `json:"company_stage"`
	IsFoundingRole   bool   `json:"is_founding_role"`
	VisaSponsorship  string `json:"visa_sponsorship"`
	WorkArrangement  string `json:"work_arrangement"`
	FitScore         *int   `json:"fit_score"`
	FitReason        string `json:"fit_reason"`
}

func parseEnrichment(content string, reasonThreshold int) models.Enrichment {
	var ej enrichJSON
	if jstr := extractJSON(content); jstr != "" {
		if err := json.Unmarshal([]byte(jstr), &ej); err == nil {
			return toEnrichment(ej, reasonThreshold)
		}
	}
	// Delimiter/labeled-prose fallback.
	return toEnrichment(parseDelimiter(content), reasonThreshold)
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

func toEnrichment(ej enrichJSON, reasonThreshold int) models.Enrichment {
	e := models.Enrichment{
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
		FitScore:        ej.FitScore,
		FitReason:       strings.TrimSpace(ej.FitReason),
	}
	if e.FitScore != nil && *e.FitScore < reasonThreshold {
		e.FitReason = "" // only keep reason above threshold
	}
	return e
}

var (
	seniorityVals   = []string{"intern", "junior", "mid", "senior", "staff", "principal", "lead", "manager", "director"}
	employmentVals  = []string{"full-time", "part-time", "contract", "temporary", "internship"}
	sizeVals        = []string{"1-10", "11-50", "51-200", "201-1000", "1000+"}
	stageVals       = []string{"seed", "early", "growth", "mature", "public"}
	visaVals        = []string{"yes", "no", "unknown"}
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
// left zero-valued so partial extraction still works.
func parseDelimiter(content string) enrichJSON {
	var ej enrichJSON
	segs := splitDelimiters(content)
	set := map[string]*string{
		"company_overview": &ej.CompanyOverview, "company": &ej.CompanyOverview,
		"industry": &ej.Industry,
		"tech_stack": &ej.TechStack, "stack": &ej.TechStack, "tech stack": &ej.TechStack,
		"seniority":        &ej.Seniority,
		"employment_type":  &ej.EmploymentType, "employment": &ej.EmploymentType,
		"company_size_band": &ej.CompanySizeBand, "size": &ej.CompanySizeBand, "company size": &ej.CompanySizeBand,
		"company_stage": &ej.CompanyStage, "stage": &ej.CompanyStage,
		"visa_sponsorship": &ej.VisaSponsorship, "visa": &ej.VisaSponsorship,
		"work_arrangement": &ej.WorkArrangement, "remote": &ej.WorkArrangement, "arrangement": &ej.WorkArrangement,
		"fit_reason": &ej.FitReason, "reason": &ej.FitReason,
	}
	var foundScore, foundYears, foundFounding bool
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
		case "fit_score", "score":
			if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
				ej.FitScore = &n
				foundScore = true
			}
		case "years_experience", "years":
			if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
				ej.YearsExperience = &n
				foundYears = true
			}
		case "is_founding_role", "founding":
			b := parseBool(val)
			ej.IsFoundingRole = b
			foundFounding = true
		}
	}
	_ = foundScore
	_ = foundYears
	_ = foundFounding
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
