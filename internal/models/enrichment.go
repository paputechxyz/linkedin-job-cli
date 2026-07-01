package models

// Enrichment is the structured result of the LLM enrichment call: structured
// facts extracted from the job posting. Produced by internal/llm and persisted
// by store.SetEnrichmentAndScore. Zero/empty values mean "not extracted".
//
// The fit score is NOT part of enrichment — it is derived deterministically by
// internal/score from the extracted fields and persisted via store.SetScore.
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
}
