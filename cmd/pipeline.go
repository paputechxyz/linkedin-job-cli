package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/filter"
	"linkedin-jobs/internal/fx"
	"linkedin-jobs/internal/linkedin"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/models"
	"linkedin-jobs/internal/profile"
	"linkedin-jobs/internal/render"
	"linkedin-jobs/internal/salary"
	"linkedin-jobs/internal/score"
	"linkedin-jobs/internal/store"
)

// ingestOptions controls the shared fetch → dedup → hard-filter → score → display pipeline.
type ingestOptions struct {
	minSalary         float64
	minSalaryCurrency string // "" = legacy raw numeric compare; else ISO 4217 (e.g. CAD) for FX-aware filtering
	excludeCompanies  []string
	remote            bool
	noDetail          bool
	noSummarize       bool // legacy flag; treated as noScore for the combined flow
	noScore           bool
	noFilter          bool
	forceOverwrite    bool // bypass dedup: re-parse + re-score + overwrite jobs already in the DB
	detailDelay       float64
	scoreDelay        float64 // pause between successive LLM scoring calls (avoids 429s)
	jsonOut           bool
}

// ingest runs the pipeline on a batch of job cards and returns the display set
// (all fetched jobs are persisted to the store regardless). Gate order keeps
// token use minimal: dedup and the hard filter are deterministic and free; only
// genuine new candidates reach the LLM (one combined enrichment+score call).
func ingest(jobs []*models.JobPosting, opts ingestOptions) []*models.JobPosting {
	cfg := loadCfg()
	settings, _ := config.LoadSettings()
	st, err := openStore()
	if err != nil {
		die("failed to open DB: %v", err)
	}
	defer st.Close()

	if len(jobs) == 0 {
		fmt.Fprintln(os.Stderr, "No jobs found.")
		return nil
	}
	fmt.Fprintf(os.Stderr, "Found %d jobs.\n", len(jobs))

	// 1. Details (salary + full description) — ensures R1 full descriptions are saved.
	if !opts.noDetail {
		fmt.Fprintln(os.Stderr, "Fetching job details (salary + description)…")
		c := linkedin.New(cfg)
		c.FetchDetailsBatch(jobs, opts.detailDelay, func(done, total int) {
			fmt.Fprintf(os.Stderr, "\r  %d/%d", done, total)
		})
		fmt.Fprintln(os.Stderr)
	}

	// 2. Compute dedup hash + persist ALL jobs (save-all; dedup memory).
	for _, j := range jobs {
		j.ContentHash = store.ContentHash(j.Company, j.Title, j.Description, j.ListedAt)
		if err := st.Upsert(j); err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", j.Title, err)
		}
	}

	// 3. Run gates per job: dedup -> score (which internally applies caps).
	noScore := opts.noScore || opts.noSummarize
	profileData, _ := profile.Load()
	var provider *llm.Provider
	if !noScore {
		p, err := llm.Resolve(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Scoring skipped: %v\n", err)
		} else {
			provider = p
			// Surface profile state so the user can tell whether scores will
			// reflect their actual resume/preferences or run context-free.
			fmt.Fprintln(os.Stderr, profileStatus(profileData))
		}
	}
	weights := score.FromSettings(settings.Scoring)
	scoredN, cappedN, dupsN := 0, 0, 0
	for _, j := range jobs {
		if !opts.forceOverwrite && st.IsDuplicateEnriched(j.ContentHash) {
			dupsN++
			continue
		}
		// Token-frugality gate: jobs that fail the hard filter are still visible
		// (the new cap-not-hide semantics), but they skip the LLM enrich call
		// because their final score is already known (the cap).
		if !opts.noFilter && settings.Filter.AutoFilter && !filter.PassesHardFilter(j, profileData) {
			res := score.Compute(j, profileData, weights)
			reason := score.FitReason(res)
			if err := st.SetScore(j.ID, res.Score, reason, res.CapReason); err != nil {
				fmt.Fprintf(os.Stderr, "  ! %s: %v\n", j.Title, err)
			} else {
				j.FitScore = &res.Score
				j.FitReason = reason
				j.ScoreCapReason = res.CapReason
				j.ScoredAt = "set"
				cappedN++
			}
			continue
		}
		if provider != nil {
			paceLLM(opts.scoreDelay, scoredN+cappedN)
			if err := enrichAndScoreJob(st, j, profileData, provider, weights); err != nil {
				fmt.Fprintf(os.Stderr, "  ! %s: %v\n", j.Title, err)
			} else {
				scoredN++
			}
		}
	}
	if scoredN > 0 || cappedN > 0 || dupsN > 0 {
		fmt.Fprintf(os.Stderr, "Processed: %d scored, %d capped, %d duplicates skipped.\n", scoredN, cappedN, dupsN)
	}

	// 4. Display filters (CLI-level) + output.
	shown := filterJobs(jobs, opts)
	if opts.minSalary > 0 {
		fmt.Fprintf(os.Stderr, "Salary filter >= %s: %d/%d shown.\n", money(opts.minSalary, opts.minSalaryCurrency), len(shown), len(jobs))
	}
	if opts.jsonOut {
		if err := render.AsJSON(os.Stdout, shown); err != nil {
			die("json output failed: %v", err)
		}
	} else {
		render.Table(os.Stdout, shown)
	}
	return shown
}

// profileStatus renders a one-line summary of which profile files were loaded,
// so scoring behavior is debuggable at a glance. Reports resume + preferences
// character counts and the project dir the loader looked in.
func profileStatus(p *models.Profile) string {
	dir := profileDir()
	if p == nil || (p.ResumeText == "" && p.PreferencesText == "") {
		return fmt.Sprintf("Profile: no RESUME.md / JOB_PREFERENCE.md found in %s (scoring without candidate context).", dir)
	}
	parts := make([]string, 0, 2)
	if p.ResumeText != "" {
		parts = append(parts, fmt.Sprintf("resume (%d chars)", len(p.ResumeText)))
	} else {
		parts = append(parts, "no resume")
	}
	if p.PreferencesText != "" {
		parts = append(parts, fmt.Sprintf("preferences (%d chars)", len(p.PreferencesText)))
	} else {
		parts = append(parts, "no preferences")
	}
	return fmt.Sprintf("Profile: loaded %s from %s.", strings.Join(parts, ", "), dir)
}

// profileDir returns the directory the profile loader looked in, for display.
// Mirrors profile.ResumePath's location without re-deriving the filename.
func profileDir() string {
	if abs, err := filepath.Abs(profile.ResumePath()); err == nil {
		return filepath.Dir(abs)
	}
	return filepath.Dir(profile.ResumePath())
}

// enrichAndScoreJob runs the LLM enrichment call, persists the extracted facts,
// then computes the deterministic rubric score and persists that. The LLM never
// picks a score — it only extracts facts; score.Compute derives the number.
// Shared by ingest, the enrich command, and score --all.
func enrichAndScoreJob(st *store.Store, j *models.JobPosting, prof *models.Profile, provider *llm.Provider, weights score.Weights) error {
	e, err := llm.Enrich(j, prof, provider)
	if err != nil {
		return err
	}
	if err := st.SetEnrichmentAndScore(j.ID, e); err != nil {
		return err
	}
	// Reflect enrichment onto the in-memory job so the scorer sees fresh values.
	j.EnrichedAt = "set"
	j.CompanyOverview = e.CompanyOverview
	j.Industry = e.Industry
	j.TechStack = e.TechStack
	j.Seniority = e.Seniority
	j.EmploymentType = e.EmploymentType
	if e.YearsExperience != nil {
		j.YearsExperience = e.YearsExperience
	}
	j.CompanySizeBand = e.CompanySizeBand
	j.CompanyStage = e.CompanyStage
	j.IsFoundingRole = e.IsFoundingRole
	j.VisaSponsorship = e.VisaSponsorship
	if e.WorkArrangement != "" {
		j.RemoteType = e.WorkArrangement
	}
	j.HasBonus = e.HasBonus
	j.HasEquity = e.HasEquity
	j.HasRetirementMatch = e.HasRetirementMatch
	j.AIIntensity = e.AIIntensity

	// Compute the rubric score from the freshly-enriched facts + profile.
	res := score.Compute(j, prof, weights)
	reason := score.FitReason(res)
	if err := st.SetScore(j.ID, res.Score, reason, res.CapReason); err != nil {
		return err
	}
	j.FitScore = &res.Score
	j.FitReason = reason
	j.ScoreCapReason = res.CapReason
	j.ScoredAt = "set"
	return nil
}

func filterJobs(jobs []*models.JobPosting, opts ingestOptions) []*models.JobPosting {
	out := make([]*models.JobPosting, 0, len(jobs))
	for _, j := range jobs {
		// Cap-not-hide: jobs with a hard-filter cap remain visible (their score
		// communicates marginality). Only the user's CLI display filters apply.
		if opts.minSalary > 0 && !meetsDisplaySalaryFloor(j, opts.minSalary, opts.minSalaryCurrency) {
			continue
		}
		if opts.remote && !strings.Contains(strings.ToLower(j.Location+" "+j.RemoteType), "remote") {
			continue
		}
		excluded := false
		for _, ex := range opts.excludeCompanies {
			if ex != "" && strings.Contains(strings.ToLower(j.Company), strings.ToLower(ex)) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		out = append(out, j)
	}
	return out
}

// meetsDisplaySalaryFloor reports whether a job's max salary clears the floor.
// With no currency it's a raw numeric compare (legacy); with a currency the job
// salary is converted to that currency first. Jobs without salary never clear
// the floor (matching the original "0 < min" drop behavior).
func meetsDisplaySalaryFloor(j *models.JobPosting, min float64, currency string) bool {
	if !j.HasSalary() {
		return false
	}
	if currency == "" {
		return j.SalaryMax() >= min
	}
	jobCur := j.SalaryCurrency
	if jobCur == "" {
		jobCur = "USD"
	}
	conv, err := fx.Convert(j.SalaryMax(), jobCur, currency)
	if err != nil {
		return j.SalaryMax() >= min // unknown rate: best-effort raw compare, don't drop
	}
	return conv >= min
}

// filterByMinSalary applies an FX-aware salary floor to a loaded job slice. Used
// by `list`/`serve` where the DB can't do currency conversion in SQL. Jobs with
// no salary are dropped (a floor implies "only show jobs I know pay enough").
func filterByMinSalary(jobs []*models.JobPosting, min float64, currency string) []*models.JobPosting {
	if min <= 0 {
		return jobs
	}
	out := make([]*models.JobPosting, 0, len(jobs))
	for _, j := range jobs {
		if meetsDisplaySalaryFloor(j, min, currency) {
			out = append(out, j)
		}
	}
	return out
}

func money(f float64, currency string) string {
	return fmt.Sprintf("%s%.0f", currencyLabel(currency), f)
}

// parseMinSalary parses a --min-salary value ("200k"), defaulting to 0 (no filter).
func parseMinSalary(s string) float64 {
	if s == "" {
		return 0
	}
	v, err := salary.ParseShorthand(s)
	if err != nil {
		die("Invalid salary format %q: use '200k' or '200000'.", s)
	}
	return v
}

// validateSalaryCurrency normalizes a --salary-currency value and exits with a
// clear message if it isn't a known convertible currency. "" means "raw numeric
// compare" (the legacy default) and is always allowed.
func validateSalaryCurrency(s string) string {
	c := fx.Normalize(s)
	if c == "" {
		return ""
	}
	if !fx.Supported(c) {
		die("Unsupported --salary-currency %q: use an ISO 4217 code like USD, CAD, EUR.", s)
	}
	return c
}

// resolveDetailDelay reads the configured delay between detail fetches.
func resolveDetailDelay() float64 {
	return config.Load().DetailDelaySeconds
}

// resolveLLMDelay reads the configured delay between successive LLM scoring
// calls. Set LJ_LLM_DELAY_SECONDS (default 2.0) to pace bulk runs and avoid
// provider rate limits (HTTP 429). 0 disables pacing.
func resolveLLMDelay() float64 {
	return config.Load().LLMDelaySeconds
}

// paceLLM sleeps for delay when callIdx > 0, i.e. between successive LLM calls
// rather than before the first one. Pass the count of calls already made.
func paceLLM(delay float64, callIdx int) {
	if callIdx > 0 && delay > 0 {
		time.Sleep(time.Duration(delay * float64(time.Second)))
	}
}
