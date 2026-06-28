package models

// Enrichment is the structured result of the combined LLM enrichment + fit-score
// call. It is produced by internal/llm (U7) and persisted by store.SetEnrichmentAndScore.
// Zero/empty values mean "not extracted"; FitScore nil means "unscored".
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
	FitScore        *int   // 0-100
	FitReason       string // populated when FitScore >= reason threshold
}
