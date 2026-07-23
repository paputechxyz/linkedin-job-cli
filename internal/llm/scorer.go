package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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
"short_description": a tight summary of the role as 2-3 short paragraphs separated by a blank line (use "\n\n" between paragraphs). Paragraph 1: what the role is and what you would build or own. Paragraph 2: the must-have requirements (years, stack, seniority). Optionally a 3rd short paragraph for standout context. Keep each paragraph to 1-2 sentences. Use real line breaks ("\n\n"), never one giant paragraph. Omit company marketing, benefits, EEO statements, and compensation,
"company_overview": 1-2 sentences on what the company does,
"industry": the company's industry,
"tech_stack": comma-separated technologies required,
"seniority": one of intern|junior|mid|senior|staff|principal|lead|manager|director,
"employment_type": one of full-time|part-time|contract|temporary|internship,
"years_experience": integer minimum years required,
"company_size_band": one of 1-10|11-50|51-200|201-1000|1000+,
"company_stage": one of seed|early|growth|mature|public,
"is_founding_role": boolean (founding/founding-engineer role),
"work_arrangement": one of remote|hybrid|onsite|unknown,
"salary_low": number or null — the LOW end of the cash compensation range stated in the description body. Only set when the posting EXPLICITLY states a dollar/number figure (e.g. "$184,000 - $249,000", "$150,000 CAD", "CA$190k - CA$210k"). Set to null when no figure is given ("competitive", "DOE", equity-only, etc.). Parse the literal numeric value (strip "$", ",", "k" -> *1000, "m" -> *1000000). Do NOT infer market rates or guess.
"salary_high": number or null — the HIGH end of the same range. Same rules as salary_low. When the posting gives a single point figure rather than a range, set both salary_low and salary_high to that figure.
"salary_currency": one of USD|CAD|EUR|GBP|AUD|INR|JPY|"" — the ISO 4217 currency of the stated range. Use the posting's explicit code/symbol (CA$/CAD -> CAD, US$/USD -> USD, £ -> GBP, € -> EUR). Empty string "" when no salary range was stated. When the description uses a bare "$" with no currency signal, infer from the user's preferred location: Canada -> CAD, United States -> USD, United Kingdom -> GBP, European Union -> EUR, India -> INR, Japan -> JPY, Australia -> AUD. When the description lists MULTIPLE locale-specific ranges (e.g. "US: $200,000-$300,000 USD; Canada: $150,000-$250,000 CAD"), pick the band matching the USER'S preferred location below — NOT the job's location. If the user's location is unknown or no band matches, fall back to the job's location, then to the first listed band.
"ratings": an object mapping each rubric id listed below to an object {"rating": <integer 1-5>, "reason": "<one short sentence citing the specific evidence from the posting that drove the rating>"}, where 1 = strong miss/negative, 2 = weak, 3 = neutral or not mentioned, 4 = good, 5 = strong match. Rate EVERY listed rubric and ALWAYS include a non-empty "reason" for each — never return a bare integer or omit the reason,

Rubrics to rate (id: what to look for):
%s

User's preferred location: %s
User's preferred salary currency: %s

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
//
// The profile supplies the user's preferred location/currency so the LLM can
// pick the right salary band from postings that list several locale-specific
// ranges (e.g. US: $X USD / Canada: $Y CAD). Pass nil when no profile is
// loaded — the LLM then falls back to the job's own location for band/currency
// inference.
func Enrich(j *models.JobPosting, provider *Provider, rubrics []config.Rubric, prof *models.Profile) (models.Enrichment, map[string]models.DynamicRating, error) {
	if strings.TrimSpace(j.Description) == "" {
		return models.Enrichment{}, nil, ErrEmptyDescription
	}
	content, err := requestEnrichment(j, provider, rubrics, prof)
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
// rubrics are excluded (computed in Go), and rubrics whose applies_to list
// doesn't cover the job's arrangement are dropped so the LLM isn't asked to
// rate (and waste tokens on) something Compute will ignore. Returns a
// placeholder line when there are no applicable rubrics so the ratings key is
// still well-formed.
func dynamicRubricBlock(rubrics []config.Rubric, j *models.JobPosting) string {
	arr := j.DetectArrangement()
	var lines []string
	for _, r := range rubrics {
		if r.Kind == "system" {
			continue
		}
		if !r.AppliesToArrangement(arr) {
			continue
		}
		line := r.ID + ": " + r.Description
		if len(r.Items) > 0 {
			line += " (items: " + strings.Join(r.Items, ", ") + ")"
		}
		lines = append(lines, "- "+line)
	}
	if len(lines) == 0 {
		return "- (none — return an empty ratings object {})"
	}
	return strings.Join(lines, "\n")
}

func enrichPrompt(j *models.JobPosting, rubrics []config.Rubric, prof *models.Profile) string {
	desc := j.Description
	if len(desc) > 4000 {
		// Keep the head (responsibilities, stack, etc.) but always surface any
		// compensation lines that live past the truncation point — otherwise the
		// LLM extraction path silently misses salary bands that employers bury
		// at the end of the posting. The tail scan is cheap and only appends
		// when something matches.
		head := desc[:4000]
		tail := desc[4000:]
		extra := extractSalaryBearingLines(tail)
		if extra != "" {
			desc = head + "\n\n" + extra
		} else {
			desc = head
		}
	}
	userLoc := ""
	userCur := ""
	if prof != nil {
		userLoc = strings.TrimSpace(prof.PrefLocation)
		userCur = strings.TrimSpace(prof.PrefMinSalaryCurrency)
	}
	return fmt.Sprintf(enrichPromptTmpl,
		dynamicRubricBlock(rubrics, j),
		orNA(userLoc), orNA(userCur),
		j.Title, orNA(j.Company), orNA(j.Location),
		j.SalaryDisplay(), desc)
}

// salaryLineRE matches lines that look like a salary/compensation statement —
// either an explicit "Salary:"/"Compensation:" label or any line containing a
// currency-stated amount. Used to rescue compensation info buried past the
// 4000-char description truncation so the LLM can still extract it.
var salaryLineRE = regexp.MustCompile(`(?i)(?:^|\n)[^\n]*(?:salary|compensation|pay range|base pay|annual pay|OTE)[^\n]*|(?:^|\n)[^\n]*(?:USD|CAD|EUR|GBP|AUD|INR|JPY|CA\$|US\$|\$[\d,]+)[^\n]*`)

// extractSalaryBearingLines scans the truncated tail of a description and
// returns any compensation-related lines joined as a continuation block.
// Returns "" when the tail carries nothing salary-shaped.
func extractSalaryBearingLines(tail string) string {
	matches := salaryLineRE.FindAllString(tail, -1)
	if len(matches) == 0 {
		return ""
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		m = strings.TrimSpace(m)
		if m != "" {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "Compensation details from later in the posting:\n" + strings.Join(out, "\n")
}

func requestEnrichment(j *models.JobPosting, provider *Provider, rubrics []config.Rubric, prof *models.Profile) (string, error) {
	return Chat(provider, enrichSystem, enrichPrompt(j, rubrics, prof), 4096, 0.2)
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
	ShortDescription string `json:"short_description"`
	CompanyOverview string `json:"company_overview"`
	Industry        string `json:"industry"`
	TechStack       string `json:"tech_stack"`
	Seniority       string `json:"seniority"`
	EmploymentType  string `json:"employment_type"`
	YearsExperience *int   `json:"years_experience"`
	CompanySizeBand string `json:"company_size_band"`
	CompanyStage    string `json:"company_stage"`
	IsFoundingRole  bool   `json:"is_founding_role"`
	WorkArrangement string `json:"work_arrangement"`
	SalaryLow       *float64 `json:"salary_low"`
	SalaryHigh      *float64 `json:"salary_high"`
	SalaryCurrency  string   `json:"salary_currency"`
	Ratings         json.RawMessage `json:"ratings"`
}

// parseRatings decodes the LLM "ratings" value tolerantly. The preferred shape
// is id -> {"rating": N, "reason": "..."}; the legacy id -> int shape (with no
// reason) is still accepted so older mocks/responses keep working. Returns nil
// when the value is absent or unparseable.
func parseRatings(raw json.RawMessage) map[string]models.DynamicRating {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]models.DynamicRating
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj
	}
	var ints map[string]int
	if err := json.Unmarshal(raw, &ints); err != nil {
		return nil
	}
	out := make(map[string]models.DynamicRating, len(ints))
	for k, v := range ints {
		out[k] = models.DynamicRating{Rating: v}
	}
	return out
}

func parseEnrichment(content string) (models.Enrichment, map[string]models.DynamicRating) {
	var ej enrichJSON
	if jstr := extractJSON(content); jstr != "" {
		if err := json.Unmarshal([]byte(jstr), &ej); err == nil {
			return toEnrichment(ej), clampRatings(parseRatings(ej.Ratings))
		}
	}
	// Delimiter/labeled-prose fallback (facts only; ratings left empty).
	return toEnrichment(parseDelimiter(content)), nil
}

// clampRatings forces every rating into [1,5].
func clampRatings(r map[string]models.DynamicRating) map[string]models.DynamicRating {
	if len(r) == 0 {
		return nil
	}
	for k, v := range r {
		if v.Rating < 1 {
			v.Rating = 1
		}
		if v.Rating > 5 {
			v.Rating = 5
		}
		r[k] = v
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
	e := models.Enrichment{
		ShortDescription: strings.TrimSpace(ej.ShortDescription),
		CompanyOverview: strings.TrimSpace(ej.CompanyOverview),
		Industry:        strings.TrimSpace(ej.Industry),
		TechStack:       strings.TrimSpace(ej.TechStack),
		Seniority:       normalizeEnum(ej.Seniority, seniorityVals),
		EmploymentType:  normalizeEnum(ej.EmploymentType, employmentVals),
		YearsExperience: ej.YearsExperience,
		CompanySizeBand: normalizeEnum(ej.CompanySizeBand, sizeVals),
		CompanyStage:    normalizeEnum(ej.CompanyStage, stageVals),
		IsFoundingRole:  ej.IsFoundingRole,
		WorkArrangement: normalizeArrangement(ej.WorkArrangement),
	}
	// Salary sanity: reject zero/negative, and treat "$0" hallucinations as
	// "not extracted" so the caller doesn't override real data with junk.
	if ej.SalaryLow != nil && *ej.SalaryLow >= 1000 {
		v := *ej.SalaryLow
		e.SalaryLow = &v
	}
	if ej.SalaryHigh != nil && *ej.SalaryHigh >= 1000 {
		v := *ej.SalaryHigh
		e.SalaryHigh = &v
	}
	// If only one side is provided, mirror it so we have a usable range.
	if e.SalaryLow != nil && e.SalaryHigh == nil {
		v := *e.SalaryLow
		e.SalaryHigh = &v
	}
	if e.SalaryHigh != nil && e.SalaryLow == nil {
		v := *e.SalaryHigh
		e.SalaryLow = &v
	}
	if e.SalaryLow != nil || e.SalaryHigh != nil {
		e.SalaryCurrency = normalizeCurrency(ej.SalaryCurrency)
	}
	return e
}

// currencyVals is the set of ISO 4217 codes the rest of the pipeline
// understands (fx.Convert supports the same set). Anything outside is dropped
// to "" so the caller inherits the existing currency rather than persisting
// an unsupported code.
var currencyVals = map[string]string{
	"usd": "USD", "$": "USD",
	"cad": "CAD", "ca$": "CAD", "c$": "CAD",
	"eur": "EUR", "€": "EUR",
	"gbp": "GBP", "£": "GBP",
	"aud": "AUD", "a$": "AUD",
	"inr": "INR", "₹": "INR",
	"jpy": "JPY", "¥": "JPY",
}

// normalizeCurrency maps an LLM-returned currency code or symbol to its ISO
// 4217 uppercase form. Returns "" for unknown / empty so the caller can leave
// the existing salary_currency untouched.
func normalizeCurrency(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if c, ok := currencyVals[strings.ToLower(v)]; ok {
		return c
	}
	return ""
}

var (
	seniorityVals  = []string{"intern", "junior", "mid", "senior", "staff", "principal", "lead", "manager", "director"}
	employmentVals = []string{"full-time", "part-time", "contract", "temporary", "internship"}
	sizeVals       = []string{"1-10", "11-50", "51-200", "201-1000", "1000+"}
	stageVals      = []string{"seed", "early", "growth", "mature", "public"}
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
		"short_description": &ej.ShortDescription, "short desc": &ej.ShortDescription,
		"company_overview": &ej.CompanyOverview, "company": &ej.CompanyOverview,
		"industry":   &ej.Industry,
		"tech_stack": &ej.TechStack, "stack": &ej.TechStack, "tech stack": &ej.TechStack,
		"seniority":       &ej.Seniority,
		"employment_type": &ej.EmploymentType, "employment": &ej.EmploymentType,
		"company_size_band": &ej.CompanySizeBand, "size": &ej.CompanySizeBand, "company size": &ej.CompanySizeBand,
		"company_stage": &ej.CompanyStage, "stage": &ej.CompanyStage,
		"work_arrangement": &ej.WorkArrangement, "remote": &ej.WorkArrangement, "arrangement": &ej.WorkArrangement,
		"salary_currency": &ej.SalaryCurrency, "currency": &ej.SalaryCurrency,
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
		case "salary_low", "salary_min":
			if f, ok := parseSalaryAmount(val); ok && f >= 1000 {
				ej.SalaryLow = &f
			}
		case "salary_high", "salary_max":
			if f, ok := parseSalaryAmount(val); ok && f >= 1000 {
				ej.SalaryHigh = &f
			}
		}
	}
	return ej
}

// parseSalaryAmount parses a numeric amount that may carry "$"/","/"k"/"m"
// decoration. Returns ok=false when the value is missing or unparseable.
func parseSalaryAmount(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "$", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	if s == "" || s == "null" {
		return 0, false
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(strings.ToLower(s), "k"):
		s = s[:len(s)-1]
		mult = 1_000
	case strings.HasSuffix(strings.ToLower(s), "m"):
		s = s[:len(s)-1]
		mult = 1_000_000
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f * mult, true
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
