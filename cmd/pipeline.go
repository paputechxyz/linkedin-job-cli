package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"linkedin-jobs/internal/config"
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

// ingestOptions controls the shared fetch → gate → dedup → hard-filter → score → display pipeline.
// Gates (--remote/--hybrid/--onsite/--min-salary) run AFTER detail fetch (so salary and
// RemoteType are populated) but BEFORE persist+score: failing jobs are dropped
// in-memory, never stored, and never sent to the LLM.
type ingestOptions struct {
	minSalary         float64
	minSalaryCurrency string // "" = legacy raw numeric compare; else ISO 4217 (e.g. CAD) for FX-aware filtering
	remote            bool
	hybrid            bool
	onsite            bool
	forceOverwrite    bool // bypass dedup: re-parse + re-score + overwrite jobs already in the DB
	detailDelay       float64
	scoreDelay        float64 // pause between successive LLM scoring calls (avoids 429s)
	jsonOut           bool
}

// scoringProvider is the pure policy for the mandatory-LLM precondition: it
// returns a setup-guidance error when the provider is nil or resolution failed,
// otherwise the provider unchanged. Split from mustResolveProvider so the
// "missing provider → actionable error, never a silent nil" rule is unit-
// testable without depending on host opencode credential state (which
// llm.Resolve also reads and would make a forced-failure test flaky).
func scoringProvider(p *llm.Provider, err error) (*llm.Provider, error) {
	if p == nil && err == nil {
		err = llm.ErrNoProvider
	}
	if err != nil {
		return nil, fmt.Errorf("LLM provider required for scoring — run 'linkedin-jobs doctor' to diagnose: %w", err)
	}
	return p, nil
}

// mustResolveProvider resolves the LLM provider or dies with a setup prompt.
// Each fetch+score command calls it at the top of its RunE, before any LinkedIn
// fetch, so a missing provider fails fast with no network calls.
func mustResolveProvider() *llm.Provider {
	p, err := scoringProvider(llm.Resolve(loadCfg()))
	if err != nil {
		die("%v", err)
	}
	return p
}

// ingest runs the pipeline on a batch of job cards and returns the display set
// (all fetched jobs are persisted to the store regardless). The caller resolves
// the LLM provider and passes it in — scoring is mandatory, so provider is
// always non-nil. Gate order keeps token use minimal: dedup and the hard filter
// are deterministic and free; every genuine new candidate reaches the LLM (one
// combined enrichment+score call).
func ingest(jobs []*models.JobPosting, provider *llm.Provider, opts ingestOptions) []*models.JobPosting {
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
	fmt.Fprintln(os.Stderr, "Fetching job details (salary + description)…")
	c := linkedin.New(cfg)
	// Best-effort: a session enables the Voyager-API fallbacks (description
	// body + workplace type) that the anonymous guest page omits. Non-fatal
	// when no session is configured — search still works anonymously.
	attachSession(c)
	c.FetchDetailsBatch(jobs, opts.detailDelay, func(done, total int) {
		fmt.Fprintf(os.Stderr, "\r  %d/%d", done, total)
	})
	fmt.Fprintln(os.Stderr)

	// 1b. Apply user gates (--remote/--hybrid/--onsite/--min-salary). Runs after the detail
	// fetch so salary and RemoteType are populated, but before persist + score.
	// Jobs failing any active gate are dropped in-memory: never stored, never LLM'd.
	beforeGate := len(jobs)
	jobs = applyGates(jobs, opts)
	if beforeGate != len(jobs) {
		fmt.Fprintf(os.Stderr, "Gates: %d/%d passed (remote/hybrid/onsite/salary); %d dropped pre-store.\n", len(jobs), beforeGate, beforeGate-len(jobs))
	}

	// 2. Compute dedup hash + persist ALL surviving jobs (save-all; dedup memory).
	for _, j := range jobs {
		j.ContentHash = store.ContentHash(j.Company, j.Title, j.Description, j.ListedAt)
		if err := st.Upsert(j); err != nil {
			fmt.Fprintf(os.Stderr, "  ! %s: %v\n", j.Title, err)
		}
	}

	// 3. Score every surviving job. The provider is resolved by the caller
	// before any fetch (LLM is a hard requirement; --no-score is gone), so every
	// non-duplicate survivor runs through enrich → compose → persist.
	profileData, _ := profile.Load(settings.Profile)
	fmt.Fprintln(os.Stderr, profileStatus(profileData))
	rubrics := settings.Scoring.Rubrics
	// Partition into jobs that need LLM scoring vs. dedup-skipped.
	var toScore []*models.JobPosting
	dupsN := 0
	for _, j := range jobs {
		if !opts.forceOverwrite && st.IsDuplicateEnriched(j.ContentHash) {
			dupsN++
			continue
		}
		toScore = append(toScore, j)
	}
	scoredN := 0
	if len(toScore) > 0 {
		scoredN = enrichAndScoreBatch(st, toScore, profileData, provider, rubrics, resolveLLMConcurrency(), opts.scoreDelay, func(idx, total int, j *models.JobPosting, err error) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ! %s: %v\n", j.Title, err)
			}
		})
	}
	if scoredN > 0 || dupsN > 0 {
		fmt.Fprintf(os.Stderr, "Processed: %d scored, %d duplicates skipped.\n", scoredN, dupsN)
	}

	// 4. Output. No further filtering here — the user gates already ran in
	// step 1b and trimmed the slice pre-store. What remains is the shown set.
	if opts.jsonOut {
		if err := render.AsJSON(os.Stdout, jobs); err != nil {
			die("json output failed: %v", err)
		}
	} else {
		render.Table(os.Stdout, jobs)
	}
	return jobs
}

// profileStatus renders a one-line summary of what profile context scoring will
// use: a count of the structured preference knobs active in settings.yaml.
// Reports the project dir the loader looked in.
func profileStatus(p *models.Profile) string {
	dir := profileDir()
	if profile.IsEmpty(p) {
		return fmt.Sprintf("Profile: no profile knobs in %s (scoring without candidate context).", dir)
	}
	knobs := 0
	if len(p.PrefWorkArrangement) > 0 {
		knobs++
	}
	if p.PrefMinSalary != nil {
		knobs++
	}
	if len(p.PrefPreferredTech) > 0 {
		knobs++
	}
	if len(p.PrefAvoidedTech) > 0 {
		knobs++
	}
	return fmt.Sprintf("Profile: loaded %d preference knob(s) from settings.yaml", knobs)
}

// profileDir returns the directory the profile loader looked in, for display.
func profileDir() string {
	return config.ProjectDir()
}

// enrichAndScoreJob runs the LLM enrichment call (which also rates the dynamic
// rubrics), persists the extracted facts, then composes the weighted-average
// rubric score and persists that. System rubrics are computed in Go; dynamic
// rubrics take their rating from the LLM response. Shared by ingest, the enrich
// command, and rescore-all.
func enrichAndScoreJob(st *store.Store, j *models.JobPosting, prof *models.Profile, provider *llm.Provider, rubrics []config.Rubric) error {
	e, ratings, err := llm.Enrich(j, provider, rubrics, prof)
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

	// Salary cross-check: the text-extraction regex (run in the scraper) is the
	// high-confidence path — when it produced a description-sourced salary we
	// keep it as-is. When text-extraction missed but the LLM extracted a range
	// from the description body, the LLM result is authoritative (description
	// overrides badge). We do NOT override an existing description-sourced
	// salary with the LLM's, because the regex's currency-strict rules are more
	// reliable than LLM currency inference.
	if (e.SalaryLow != nil || e.SalaryHigh != nil) && j.SalarySource != models.SalarySourceDescription {
		raw := ""
		if e.SalaryLow != nil && e.SalaryHigh != nil {
			raw = fmt.Sprintf("%.0f - %.0f", *e.SalaryLow, *e.SalaryHigh)
		}
		if err := st.SetSalaryFromDescription(j.ID, e.SalaryLow, e.SalaryHigh, e.SalaryCurrency, raw); err != nil {
			return err
		}
		j.SalaryLow = e.SalaryLow
		j.SalaryHigh = e.SalaryHigh
		if e.SalaryCurrency != "" {
			j.SalaryCurrency = e.SalaryCurrency
		}
		if raw != "" {
			j.SalaryRaw = raw
		}
		j.SalarySource = models.SalarySourceDescription
	}

	// Compose the weighted-average score from the rubric set + LLM ratings.
	res := score.Compute(j, prof, rubrics, ratings)
	reason := score.FitReason(res)
	rubricJSON, _ := json.Marshal(res.Rubrics)
	if err := st.SetScore(j.ID, res.Score, reason, string(rubricJSON)); err != nil {
		return err
	}
	j.FitScore = &res.Score
	j.FitReason = reason
	j.RubricScores = string(rubricJSON)
	j.ScoredAt = "set"
	return nil
}

// applyGates drops jobs that fail any active user gate (--remote/--hybrid/
// --onsite/--min-salary). Runs after the detail fetch (salary + RemoteType are now
// populated) and before persist+score: failing jobs are dropped in-memory and
// never reach the DB or the LLM. --salary-currency is not itself a gate; it
// only supplies the unit for the --min-salary floor (FX-converted via fx.Convert).
// --remote, --hybrid, and --onsite OR together when more than one is set.
//
// Each dropped job is logged to stderr with its title, company, and a
// human-readable reason so the user can see WHY a given job vanished (e.g.
// "salary $150,000 below CA$200,000 floor" or "not remote/hybrid/onsite
// (remote_type=onsite)").
func applyGates(jobs []*models.JobPosting, opts ingestOptions) []*models.JobPosting {
	out := make([]*models.JobPosting, 0, len(jobs))
	for _, j := range jobs {
		if reason := gateDropReason(j, opts); reason != "" {
			fmt.Fprintf(os.Stderr, "  dropped %q @ %s: %s\n", j.Title, companyOrDash(j.Company), reason)
			continue
		}
		out = append(out, j)
	}
	return out
}

// gateDropReason returns a human-readable reason why j fails the active user
// gates, or "" when j passes every active gate. The first failing gate wins.
// Mirrors the boolean logic in meetsDisplaySalaryFloor (salary) so the stated
// reason always matches the actual decision.
func gateDropReason(j *models.JobPosting, opts ingestOptions) string {
	// Salary floor.
	if opts.minSalary > 0 {
		if !j.HasSalary() {
			return fmt.Sprintf("no salary data (floor %s)", money(opts.minSalary, opts.minSalaryCurrency))
		}
		jobCur := j.SalaryCurrency
		if jobCur == "" {
			jobCur = "USD"
		}
		jobMax := j.SalaryMax()
		if opts.minSalaryCurrency == "" {
			if jobMax < opts.minSalary {
				return fmt.Sprintf("salary %s below %s floor", money(jobMax, jobCur), money(opts.minSalary, ""))
			}
		} else {
			conv, err := fx.Convert(jobMax, jobCur, opts.minSalaryCurrency)
			switch {
			case err != nil && jobMax < opts.minSalary:
				// FX rate unavailable — meetsDisplaySalaryFloor falls back to raw compare.
				return fmt.Sprintf("salary %s below %s floor (FX %s->%s unavailable)", money(jobMax, jobCur), money(opts.minSalary, opts.minSalaryCurrency), jobCur, opts.minSalaryCurrency)
			case err == nil && conv < opts.minSalary:
				return fmt.Sprintf("salary %s ~= %s below %s floor", money(jobMax, jobCur), money(conv, opts.minSalaryCurrency), money(opts.minSalary, opts.minSalaryCurrency))
			}
		}
	}
	// Work arrangement (--remote / --hybrid / --onsite OR together).
	if opts.remote || opts.hybrid || opts.onsite {
		blob := strings.ToLower(j.Location + " " + j.RemoteType)
		matchRemote := strings.Contains(blob, "remote")
		matchHybrid := strings.Contains(blob, "hybrid")
		// RemoteType is normalized to "onsite", but raw Location text often
		// carries the hyphenated "On-site" — accept either form (mirrors
		// linkedin.DetectRemote's normalization).
		matchOnsite := strings.Contains(blob, "on-site") || strings.Contains(blob, "onsite")
		if !((opts.remote && matchRemote) || (opts.hybrid && matchHybrid) || (opts.onsite && matchOnsite)) {
			var wanted []string
			if opts.remote {
				wanted = append(wanted, "remote")
			}
			if opts.hybrid {
				wanted = append(wanted, "hybrid")
			}
			if opts.onsite {
				wanted = append(wanted, "onsite")
			}
			rt := j.RemoteType
			if rt == "" {
				rt = "unknown"
			}
			loc := j.Location
			if loc == "" {
				loc = "unknown"
			}
			return fmt.Sprintf("not %s (remote_type=%s, location=%q)", strings.Join(wanted, "/"), rt, loc)
		}
	}
	return ""
}

// companyOrDash renders a company for diagnostic output, falling back to the
// em dash the renderer uses when the field is empty.
func companyOrDash(c string) string {
	if c == "" {
		return "—"
	}
	return c
}

// orNA2 renders a string for diagnostic output, falling back to "N/A" when empty.
func orNA2(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
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

// resolveLLMConcurrency reads the max number of jobs enriched+scored in
// parallel per batch. Set LJ_LLM_CONCURRENCY (default 5). 1 reverts to
// sequential processing.
func resolveLLMConcurrency() int {
	return config.Load().LLMConcurrency
}

// paceLLM sleeps for delay when callIdx > 0, i.e. between successive LLM calls
// rather than before the first one. Pass the count of calls already made.
func paceLLM(delay float64, callIdx int) {
	if callIdx > 0 && delay > 0 {
		time.Sleep(time.Duration(delay * float64(time.Second)))
	}
}

// enrichAndScoreBatch enriches + scores jobs concurrently in batches of up to
// `concurrency` workers. The LLM HTTP call dominates each job's wall time;
// SQLite writes serialize safely through the store's single connection
// (SetMaxOpenConns(1)), so concurrent goroutines only contend on the network.
// paceDelay (seconds) is applied between batches, not within one — a batch of
// 5 fires 5 requests at once, then waits for all to finish, then paces. Returns
// the count of jobs that scored successfully. onResult (if non-nil) is called
// serially after each job completes (under a mutex, so stderr output is safe),
// receiving the job's index in the input slice plus any error.
func enrichAndScoreBatch(
	st *store.Store,
	jobs []*models.JobPosting,
	prof *models.Profile,
	provider *llm.Provider,
	rubrics []config.Rubric,
	concurrency int,
	paceDelay float64,
	onResult func(idx, total int, j *models.JobPosting, err error),
) int {
	if concurrency < 1 {
		concurrency = 1
	}
	total := len(jobs)
	var scored int
	var mu sync.Mutex
	for start := 0; start < total; start += concurrency {
		if start > 0 {
			paceLLM(paceDelay, 1)
		}
		end := start + concurrency
		if end > total {
			end = total
		}
		var wg sync.WaitGroup
		for i := start; i < end; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				j := jobs[idx]
				err := enrichAndScoreJob(st, j, prof, provider, rubrics)
				mu.Lock()
				if err == nil {
					scored++
				}
				if onResult != nil {
					onResult(idx, total, j, err)
				}
				mu.Unlock()
			}(i)
		}
		wg.Wait()
	}
	return scored
}
