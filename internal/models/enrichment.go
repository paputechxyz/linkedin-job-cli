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

	// LLM-extracted salary, when the description states an explicit compensation
	// range that the strict text-extraction regex missed (e.g. per-locale bands
	// with bracketed prose, or labeled bare-$ ranges without badge currency to
	// inherit). Used as a fallback by the pipeline: text-extraction wins when it
	// already produced a description-sourced salary; LLM only fills the gap.
	SalaryLow      *float64
	SalaryHigh     *float64
	SalaryCurrency string // ISO 4217; "" lets the caller inherit existing currency
}
