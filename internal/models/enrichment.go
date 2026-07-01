package models

// Enrichment is the structured result of the LLM enrichment call: structured
// facts extracted from the job posting. Produced by internal/llm and persisted
// by store.SetEnrichmentAndScore. Zero/empty values mean "not extracted".
//
// Note: FitScore and FitReason here are legacy fields from when the LLM picked
// the score directly. The current architecture computes scores deterministically
// in internal/score from the extracted fields below; these fields are kept for
// backward compatibility with the SetEnrichmentAndScore signature but are
// typically zero when enrichment and scoring are split.
type Enrichment struct {
	CompanyOverview string
	Industry        string
	TechStack       string
	Seniority       string
	EmploymentType  string
	YearsExperience *int
	CompanySizeBand string
	CompanyStage    string
	IsFoundingRole  bool
	VisaSponsorship string
	WorkArrangement string // remote|hybrid|onsite|unknown; refines jobs.remote_type

	// Compensation extras (LLM-extracted booleans, used by the rubric scorer).
	HasBonus           bool
	HasEquity          bool
	HasRetirementMatch bool // RRSP match / 401k match / pension
	// AIIntensity is one of core | mentioned | none ("" = not extracted).
	AIIntensity string

	// Legacy direct-LLM score fields; unused by the rubric scorer but kept for
	// signature compatibility. Internal/score computes the real fit_score.
	FitScore  *int
	FitReason string
}
