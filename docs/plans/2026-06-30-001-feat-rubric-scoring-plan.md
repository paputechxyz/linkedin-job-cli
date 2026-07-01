---
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-brainstorm
date: 2026-06-30
---

# Rubric-Based Fit Scoring - Plan

## Goal Capsule

**Objective.** Replace the single uncalibrated LLM `fit_score` pick with a deterministic, rubric-driven scorer that computes a 0-100 fit score from six weighted dimensions sourced from enriched structured fields. Eliminates the observed 0/50 score collapse (LLM midpoint bias) and produces explainable per-dimension `fit_reason` output.

**Product authority.** Owner: patrickpu. Source: in-session brainstorm 2026-06-30.

**Open blockers.** None blocking start. Calibration of default weights needs ~20+ user-reviewed jobs after first ship to validate; defaults are opinionated and tunable in YAML.

## Product Contract

**Product Contract preservation.** Unchanged. The four open questions in the origin brainstorm are resolved below in Key Technical Decisions; no requirement, scope boundary, or band semantic was rewritten.

### Problem

Today `llm.Score` (`internal/llm/scorer.go`) asks the LLM for a single `fit_score: integer 0-100` with no rubric, anchors, or calibration. Observed result in the production DB: scores collapse to round numbers (0, 50) — the LLM picks the safe midpoint when signal is weak, and 0 doubles as the hard-filter sentinel. The user sees no useful differentiation among jobs that pass the filter.

A second, related problem: the hard filter (`internal/filter/filter.go`) treats preference misses as binary — `status=filtered, fit_score=0`, hidden from `list`. Salary misses of 5% (e.g. CA$190k vs CA$200k floor) get the same penalty as a 50% miss. This is too coarse for ranking real jobs.

### User outcome

After this ships, the user can rank their recommended jobs by a calibrated, explainable score where:
- Most jobs they'd seriously consider land in 60-100.
- Score <60 means a genuine bad fit (true deal-breaker present).
- Each score is decomposable: `fit_reason` shows the per-dimension breakdown so the user sees exactly why a job scored 72 vs 78.
- Editing weights in `settings.yaml` and re-running `score --all` recalibrates the entire DB without code changes or LLM re-calls.

### Scope boundaries

**In scope.**
- New deterministic scoring algorithm computed from enriched fields.
- Six scoring dimensions per the user's stated priorities.
- Two new enrichment extraction fields required as scoring inputs.
- Promotion of the user's preferred-tech list from free-text to YAML front-matter.
- YAML-tunable weights and band caps in `settings.yaml`.
- Hard filter semantics change: cap at 60 instead of zero+hide.
- Machine-generated `fit_reason` from the dimension breakdown.

**Deferred for later.**
- Real calibration pass: ship opinionated defaults, tune after ~20+ user-reviewed jobs.
- Replacing the legacy `--no-filter` flag (still works; semantics shift).
- Profile-driven per-dimension weights (initial ship uses global weights).

**Outside this product's identity.**
- Bayesian / learned-weight scoring from accept-reject history (out of scope; would require tracking outcomes).
- Multi-profile scoring (one user, one profile).
- Real-time re-scoring on profile edit (still requires `score --all` invocation).

### Score band semantics

| Score range | Meaning | Trigger |
|---|---|---|
| 0 | Unused | Reserved; never emitted by the new algorithm |
| 30 (cap) | Deal-breaker present | Match against `scoring.deal_breakers` (default: `.NET`, `C#`, `Ruby on Rails`) in extracted `tech_stack` |
| 60 (cap) | Marginal — hard filter violation | Salary under floor, non-remote when remote preferred, or outside preferred locations. Job stays visible; cap communicates marginality |
| 60 (baseline) | Passed hard filter, no positive signals | Default starting score the moment a job clears the hard filter |
| 60 → 95 | Calibrated by six weighted dimensions | Each dimension adds 0 to ~6 points based on enriched signals |
| 95-100 | Rare; exceptional on every dimension | Reserved by design; not the default for "good" jobs |

### The six scoring dimensions

Dimensions reflect the user's explicitly stated priorities. Four are sourced from fields the enrichment prompt already extracts; two require new extraction fields.

| # | Dimension | Sourcing | Notes |
|---|---|---|---|
| 1 | **Salary level** | `salary_high` + currency (already parsed), FX-converted against `pref_min_salary_currency` | Tiered: at floor, +20%/+50% above floor → weighted points. A small miss (≤10% under floor) is the canonical "cap at 60" case |
| 2 | **Remote / work arrangement** | `work_remote_allowed` + `remote_type` (enrichment-derived) | Already a hard-filter dimension; inside the score it's a thin tie-breaker (full-remote > hybrid > ambiguous). Onsite when remote is preferred hits the 60 cap above, not this dimension |
| 3 | **Compensation extras** (bonus / equity / RRSP-or-401k match) | **New enrichment fields** — three booleans | Plan will specify field names; brainstorm locks the semantics: presence of each contributes weighted points |
| 4 | **Tech stack match** | `tech_stack` (already extracted) + new `preferred_tech` YAML list in `JOB_PREFERENCE.md` front-matter | Deterministic: count overlap between extracted stack and preferred list; weighted tiers |
| 5 | **Startup stage** | `company_stage` + `company_size_band` (already extracted) | Seed / early-stage / 1-50 employees → strong startup signal; growth → partial; mature / public → none |
| 6 | **AI intensity** | **New enrichment field** — enum `core` / `mentioned` / `none` | Distinguishes "AI is the product" (e.g. AI-native role) from "AI mentioned in passing" from "no AI" |

### Hard filter and deal-breakers

**Hard filter semantics change.** Today a hard-filter violation produces `status=filtered, fit_score=0` and hides the job from `list`. After this ships, the same violation produces `status=new, fit_score=60` (capped) and the job remains visible. The score communicates marginality; the user decides whether to dig in. The salary miss example anchors this: CA$190k vs CA$200k floor should land at 60 (visible, marginal), not 0 (hidden).

**Deal-breakers are YAML-listed and cap at 30.** Default list: `.NET`, `C#`, `Ruby on Rails`. Matched against the extracted `tech_stack` field. A job with strong signals on every other dimension still caps at 30 if a deal-breaker is present.

**Token-frugality preservation (planning concern, not contract).** The hard filter today skips the LLM call on filter failure. After this change, jobs that hit the 60 cap or the 30 cap can still skip enrichment (their final score is already known). The exact gating is implementation detail for `ce-plan`; the contract is only that the new behavior should not 5x LLM token spend.

### `fit_reason` becomes machine-generated

The current `fit_reason` is LLM-authored prose. After this ships, it is generated by the scorer from the dimension breakdown, e.g.:

```
+5 salary (CA$235k vs CA$200k floor, +17%), +4 tech overlap (Java, Postgres, K8s of 8 preferred),
+5 startup (seed, 11-50), +5 AI core, +3 equity. Cap: none. Total: 82.
```

Far more informative than today's LLM prose, fully reproducible, and explains both the score and how to change it (edit weights or preferred_tech).

### Settings shape

The user's `JOB_PREFERENCE.md` gains one new front-matter key, and `settings.yaml` gains a `scoring.weights` + `scoring.deal_breakers` block. Plan will specify exact keys; brainstorm locks the intent: weights and the deal-breaker list are user-tunable without code changes.

### Dependencies / assumptions

- **Assumption (verified).** Enrichment schema already includes `company_stage`, `company_size_band`, `tech_stack`, `seniority`, `work_remote_allowed` — confirmed in `internal/models/job.go:33-43`.
- **Assumption.** The enrichment LLM call can reliably extract three new booleans (bonus / equity / retirement match) and one new enum (AI intensity) from a typical job description. Needs validation in planning; if extraction quality is poor, those dimensions degrade but the architecture holds.
- **Assumption.** The user's `preferred_tech` list, once promoted to YAML front-matter, will be a clean enumerated list rather than the prose it is today in `JOB_PREFERENCE.md`. Plan should specify the migration.
- **Dependency.** Existing `score --all` recompute path works as the recalibration mechanism; no new migration tooling needed.

### Open questions

1. **Cap for true deal-breakers — 30 vs 40?** 30 is harsher (anything with C# is essentially junk); 40 leaves room for "great role, wrong stack." Default to 30 pending user feedback after first ship.
2. **Cap for hard-filter violations — flat 60, or graduated?** Flat 60 is simpler. Graduated (e.g. salary miss by <10% → 60, by >10% → 50) is more honest. Plan should propose one and flag the trade-off.
3. **Should `preferred_tech` in YAML also drive the existing hard filter (auto-reject jobs with zero tech overlap)?** Probably no — the deal-breaker list covers anti-tech; zero overlap is just "low score," not "filter out."
4. **Status field semantics after `status=filtered` goes away.** Does the field gain a new value (`marginal`), or do we just rely on the score to communicate it? Plan should pick.

## Implementation-Ready Additions

### Product Contract preservation

Unchanged. The four open questions above are resolved below; no requirement, band semantic, or scope boundary was rewritten.

### Key Technical Decisions

**KTD1 — The LLM never picks the score. It only extracts facts; Go computes the score.**
Today `llm.Score` returns `fit_score` as one of the JSON fields the LLM emits. After this ship, the LLM enrichment call returns structured facts only (including three new fields: `has_bonus`, `has_equity`, `has_retirement_match`, and `ai_intensity`). A new pure-Go function `score.Compute(job, profile, weights)` derives the score and the dimension breakdown. Rationale: kills the 0/50 collapse at its root (LLMs are bad at absolute calibration, good at extraction), makes scores reproducible, and makes weights tunable without prompt editing or token spend.

**KTD2 — Open question resolutions.**
- **Q1 (deal-breaker cap):** 30. Honors the user's "under 60 = very bad fit" framing; tech-deal-breakers really are junk signals for this user. Exposed via `scoring.deal_breaker_cap` in YAML (default 30) so it's tunable.
- **Q2 (hard-filter cap shape):** Graduated, by dimension — each dimension contributes its own cap reason. Salary miss by <10% under floor → 60; >10% under floor → 50. Non-remote when remote preferred → 55. Location miss → 55. The lowest applicable cap wins. Adds honest differentiation without complexity (it's three if-branches in the existing hard-filter code path).
- **Q3 (`preferred_tech` in hard filter):** No. `preferred_tech` only drives the positive tech-overlap dimension. The deal-breaker list drives the 30-cap. Zero overlap just means a low score, not exclusion.
- **Q4 (status field):** `status` field semantics are unchanged (`new`/`viewed`/`saved`/`applied`/`rejected`/`filtered`). What changes: `status=filtered` is no longer set by the hard filter — instead a new `score_cap_reason` TEXT column records *why* the score was capped (`"salary_under_floor"`, `"salary_under_floor_severe"`, `"non_remote"`, `"location_miss"`, `"deal_breaker_tech"`). Jobs with a cap reason stay visible by default; the cap reason renders in the dimension breakdown. `--include-filtered` becomes vestigial but is preserved for back-compat (a no-op alias that no longer hides anything new).

**KTD3 — New schema columns, added via the existing idempotent migration.**
Three new columns on `jobs`: `has_bonus INTEGER DEFAULT 0`, `has_equity INTEGER DEFAULT 0`, `has_retirement_match INTEGER DEFAULT 0`, `ai_intensity TEXT`, `score_cap_reason TEXT`. The existing `addColumns` slice in `internal/store/store.go:64` already handles this pattern — append the new columns, and pre-existing DBs auto-heal on next open. `tech_stack`, `company_stage`, `company_size_band`, `seniority`, `work_remote_allowed` already exist.

**KTD4 — Enrichment prompt change is additive, not rewrite.**
The enrichment prompt in `internal/llm/scorer.go:19-46` gains four new keys: `has_bonus` (bool), `has_equity` (bool), `has_retirement_match` (bool — covers RRSP / 401k / pension match), and `ai_intensity` (enum: `core` | `mentioned` | `none`). The `fit_score` and `fit_reason` keys are **removed** from the prompt — the LLM no longer emits them. Reduces output tokens; eliminates the calibration failure mode.

**KTD5 — `JOB_PREFERENCE.md` front-matter gains one key: `preferred_tech`.**
Promoted from the free-text body so the matcher has a clean enumerated list. Example: `preferred_tech: [Java, Python, Elixir, Go, FastAPI, Flask, Django, NestJS, React, NextJS, Docker, Kubernetes, Postgres, BigQuery, Snowflake]`. The free-text body remains for the LLM extraction context but is no longer parsed for matching.

**KTD6 — `settings.yaml` gains a `scoring` block with weights + deal-breakers.**
Extends the existing `scoring` section (currently only `reason_threshold`). All weights are tunable; defaults are opinionated starting points.

```yaml
scoring:
  reason_threshold: 70           # fit_reason emitted at/above this (existing)
  baseline: 60                   # starting score after hard filter passes
  deal_breaker_cap: 30           # hard floor when a deal-breaker matches
  deal_breakers: [".NET", "C#", "Ruby on Rails"]
  weights:
    salary: 6
    tech_overlap: 7
    startup: 5
    ai_intensity: 5
    compensation_extras: 4       # bonus + equity + retirement, summed per-item
    remote_tiebreak: 3
```

### Implementation Units

**IU1 — Extend `models.JobPosting` and DB schema with new enrichment fields.**
Files: `internal/models/job.go`, `internal/store/store.go`, `internal/store/schema_test.go` (or `store_test.go`).
- Add struct fields: `HasBonus bool`, `HasEquity bool`, `HasRetirementMatch bool`, `AIIntensity string`, `ScoreCapReason string`.
- Append to `addColumns` slice: `{"has_bonus", "INTEGER DEFAULT 0"}`, `{"has_equity", "INTEGER DEFAULT 0"}`, `{"has_retirement_match", "INTEGER DEFAULT 0"}`, `{"ai_intensity", "TEXT"}`, `{"score_cap_reason", "TEXT"}`.
- Update the `CREATE TABLE` schema string in lockstep.
- Update `SetEnrichmentAndScore` to write the three booleans + `ai_intensity`.
- Update `Upsert` row scan + insert to round-trip the new columns.
- Test scenarios:
  - Fresh DB: new columns present, schema matches.
  - Legacy DB (simulated by `store_test.go`'s legacy-`CREATE TABLE` pattern): migration adds all five columns; legacy row remains readable; re-open is a no-op.
  - Round-trip: write a job with all new fields populated, read back, assert equality.

**IU2 — Update the enrichment LLM prompt and response parser.**
Files: `internal/llm/scorer.go`, `internal/llm/scorer_test.go`.
- Remove `fit_score` and `fit_reason` from `enrichPromptTmpl` and `enrichJSON`.
- Add four new keys to the prompt template (with anchors, like the existing seniority/employment_type enums): `has_bonus` / `has_equity` / `has_retirement_match` (booleans), `ai_intensity` (one of `core` | `mentioned` | `none`).
- Extend `enrichJSON` struct + `toEnrichment` to parse them. Use the existing `normalizeEnum` helper for `ai_intensity` against an `aiIntensityVals = []string{"core", "mentioned", "none"}` slice.
- Extend `models.Enrichment` with the corresponding fields (the existing struct in `internal/models/`).
- The `Score` function's contract changes: it now returns only the enrichment data — no `FitScore` / `FitReason`. Rename to `Enrich` for clarity? **Decision: keep name `Score` for now to minimize call-site churn; the rename can come later.** Document the rename as a follow-up in the plan.
- Test scenarios:
  - Existing `TestScore_*` tests need updates: anything asserting `FitScore` removal. Most should switch to asserting on `HasBonus` / `AIIntensity` etc.
  - New tests: `TestEnrich_ParsesCompensationExtras` (true for each), `TestEnrich_ParsesAIIntensity` (each enum value + invalid → empty), `TestEnrich_NoFitScoreInOutput` (regression guard).
  - `parseDelimiter` fallback: extend for the new keys.

**IU3 — Implement `score.Compute` (the deterministic scorer).**
Files: new `internal/score/score.go`, new `internal/score/score_test.go`.
- Pure function: `func Compute(job *models.JobPosting, profile *models.Profile, weights Weights) Result` where `Result { Score int; CapReason string; Dimensions []Dimension }` and `Dimension { Name string; Points int; Reason string }`.
- Algorithm (in order):
  1. **Deal-breaker check.** If `job.TechStack` contains any entry from `weights.DealBreakers` (case-insensitive substring), return `{Score: weights.DealBreakerCap, CapReason: "deal_breaker_tech"}`.
  2. **Hard-filter caps (existing filter logic, repurposed).** Reuse the checks in `internal/filter/filter.go` but instead of returning bool, return a cap reason and a cap value:
     - Salary: if `pref_min_salary > 0` and job has salary and converted max < floor → cap. If miss ≤10% → cap 60 (`"salary_under_floor"`); if miss >10% → cap 50 (`"salary_under_floor_severe"`).
     - Work arrangement: if `pref_work_arrangement == "remote"` and no remote signal → cap 55 (`"non_remote"`).
     - Location: if `pref_locations` set and job location known and no token match → cap 55 (`"location_miss"`).
     - Lowest applicable cap wins; if any fired, the final score is the cap (positive dimensions are not added on top — this is what "cap" means).
  3. **Baseline + dimension points (only if no cap fired).** Start at `weights.Baseline` (60). Add per-dimension points (max ~28 across all six):
     - **Salary (max 6):** at floor = 2; ≥10% above = 4; ≥30% above = 6. Below floor is already cap-handled above.
     - **Tech overlap (max 7):** count of `preferred_tech` items found in `job.TechStack` (case-insensitive whole-word match). 0 = 0, 1-2 = 2, 3-4 = 4, ≥5 = 7.
     - **Startup (max 5):** `company_stage` in `{seed, early}` → 5; `growth` → 3; else 0. Plus: `company_size_band` in `{1-10, 11-50}` adds +1 (capped at 5 total).
     - **AI intensity (max 5):** `ai_intensity == "core"` → 5; `"mentioned"` → 2; else 0.
     - **Compensation extras (max 4):** 1 point per true among `has_bonus`, `has_equity`, `has_retirement_match`, plus +1 if all three present (capped at 4).
     - **Remote tiebreak (max 3):** `work_remote_allowed == true` or `remote_type` indicates fully remote → 3; hybrid → 1; else 0. Only nonzero when the hard filter didn't already fire on remote (which it would have).
  4. Clamp to 100. Return the result with per-dimension breakdown.
- Test scenarios (one test per dimension, plus the cap paths):
  - `TestCompute_DealBreakerCapsAt30` — tech stack contains "C#" → score 30, cap reason set, no dimension breakdown added.
  - `TestCompute_SalarySmallMissCapsAt60` — max 180k vs floor 200k (10% miss) → score 60, cap `"salary_under_floor"`.
  - `TestCompute_SalarySevereMissCapsAt50` — max 100k vs floor 200k → score 50, cap `"salary_under_floor_severe"`.
  - `TestCompute_NonRemoteCapsAt55` — remote preferred, no remote signal → score 55.
  - `TestCompute_LocationMissCapsAt55` — Toronto/Remote preferred, job in NYC → score 55.
  - `TestCompute_LowestCapWins` — multiple caps fire; lowest value wins.
  - `TestCompute_BaselineNoSignals` — passes filter, no positive signals → exactly 60.
  - `TestCompute_AllSignalsMax` — every dimension maxed → clamped at 100.
  - `TestCompute_SalaryTiers` — at/above floor tiers.
  - `TestCompute_TechOverlapCounts` — 0, 1, 3, 5+ matches.
  - `TestCompute_StartupStageAndSize` — seed vs growth vs mature, with size interaction.
  - `TestCompute_AIIntensityEnum` — core vs mentioned vs none.
  - `TestCompute_CompensationExtrasSums` — 0, 1, 2, 3, all-three.
  - `TestCompute_WeightsFromYAML` — override defaults via the struct; assert user-supplied weights change outputs.
  - `TestCompute_NilProfile` — profile nil → no caps, no dimension points, score = baseline.

**IU4 — Wire `score.Compute` into the pipeline; remove LLM-as-scorer.**
Files: `cmd/pipeline.go`, `cmd/enrich.go`, `cmd/score.go`.
- After `enrichAndScoreJob` calls the (renamed-contract) LLM enrich, immediately call `score.Compute(job, profileData, weightsFromSettings)`. Persist via a new `st.SetScore(id, scoreResult)` that writes `fit_score`, `fit_reason` (machine-generated from the breakdown), `score_cap_reason`, and the existing enrichment fields.
- `fit_reason` format: a compact single-line summary derived from `result.Dimensions`, e.g. `"+4 salary, +4 tech (Java, Postgres of 8 preferred), +5 startup, +5 AI core, +3 remote | cap: none"`. Cap reason prepended if set, e.g. `"cap: salary_under_floor_severe (50)"`.
- The `enrichAndScoreJob` name becomes misleading (it no longer scores via LLM); keep the name but document that scoring now happens downstream in Go. Alternative: split into `enrichJob` + `scoreJob`; **decision: split — cleaner, and matches the new architecture.** Pipeline calls them in sequence.
- Test scenarios:
  - Pipeline integration test: feed a job through `enrichJob` + `scoreJob`, assert `fit_score` is the expected computed value, `fit_reason` matches the breakdown format, `score_cap_reason` set when expected.
  - Existing `cmd/enrich_test.go` updates: any test asserting on LLM-provided `fit_score` switches to asserting on computed score.

**IU5 — Hard filter semantics change: cap, don't hide.**
Files: `internal/filter/filter.go`, `internal/filter/filter_test.go`, `cmd/pipeline.go`.
- `PassesHardFilter` is **kept** as a token-frugality gate (jobs that fail it skip the LLM enrich call — they only need the cap, not extraction). Its return value is reinterpreted: failing it now means "set cap; skip enrich" rather than "hide."
- In `cmd/pipeline.go`'s ingest loop: replace `st.SetFiltered(j.ID)` + `status=filtered` with computing the cap reason and storing the cap score. Job stays visible.
- `list`'s default exclusion of `status=filtered` becomes a no-op for new jobs (they never get `status=filtered` from this path). Existing DB rows with `status=filtered` remain visible-by-default to avoid surprising post-migration hiding; the `--include-filtered` flag is preserved as a deprecated no-op alias.
- Test scenarios:
  - `TestPassesHardFilter` existing tests stay green (function signature unchanged).
  - New `TestIngest_HardFilterViolation_CapsNotHides` — feed a job that fails the salary floor; assert `fit_score = 60` (or 50), `score_cap_reason` set, `status = "new"` (not `"filtered"`).
  - `TestIngest_DealBreaker_CapsAt30` — feed a job whose extracted `tech_stack` contains a deal-breaker; assert cap.

**IU6 — Extend `settings.yaml` schema and load path.**
Files: `internal/config/settings.go`, `internal/config/settings_test.go` (new or existing).
- Extend `ScoringSettings` struct: `Baseline int`, `DealBreakerCap int`, `DealBreakers []string`, `Weights map[string]int` (or a dedicated `WeightsStruct`).
- Defaults populated by `DefaultSettings()` to the KTD6 values.
- Validation: weights non-negative; `baseline` in [0, 100]; `deal_breaker_cap` in [0, 100].
- Test scenarios:
  - `TestLoadSettings_ScoringWeightsDefaults` — empty YAML → defaults applied.
  - `TestLoadSettings_ScoringWeightsOverride` — explicit weights in YAML → applied.
  - `TestLoadSettings_InvalidWeightsFallBack` — negative weight → fall back to default for that key.

**IU7 — Extend `JOB_PREFERENCE.md` parsing for `preferred_tech`.**
Files: `internal/profile/profile.go`, `internal/profile/profile_test.go`.
- Extend `prefsFrontmatter` struct with `PreferredTech []string yaml:"preferred_tech"`.
- Carry through to `models.Profile.PreferredTech`.
- Test scenarios:
  - `TestLoad_PrefsPreferredTech` — YAML list parses; empty list when absent; round-trips through save/load.

**IU8 — Render the new cap reason and dimension breakdown.**
Files: `internal/render/render.go`.
- `render.Table`: no change (still shows `fit_score` column).
- `render.Detail`: show `score_cap_reason` prominently when set ("Capped: salary under floor (severe)"). `fit_reason` already renders and now carries the dimension breakdown — no change needed.
- `serve` UI: add a small "capped" badge next to the score when `score_cap_reason` is non-empty. Existing CSS for `.status--filtered` can be repurposed or kept as a deprecated class.
- Test scenarios: visual check; existing serve_test.go may need a fixture update if it asserts on absent `data-status="filtered"`.

### Test Strategy

- **Unit tests** for `score.Compute` (IU3) are the highest-value coverage — the algorithm's correctness is fully defined by the test scenarios listed above. Implement TDD-style: write the test table first, then implement against it.
- **Integration tests** for the pipeline (IU4 + IU5) using a fake LLM (the existing `fakeCompletions` test helper in `internal/llm/scorer_test.go`) verify end-to-end scoring without network calls.
- **Migration test** for IU1 follows the existing `store_test.go` pattern: simulate a legacy schema, open the store, assert migration adds the new columns.
- **No live LLM calls in tests.** All LLM-touching tests use the existing httptest fake.
- **Manual smoke test after implementation:**
  1. Run `linkedin-jobs recommended --top 5 --force-overwrite` — verify scores land in expected bands, `fit_reason` shows dimension breakdown, previously-0 jobs now land at 60.
  2. Run `linkedin-jobs list --sort-score` — verify ranking is meaningful and capped jobs appear with visible cap reason.
  3. Edit `settings.yaml` weights, run `linkedin-jobs score --all` — verify recalibration without code changes.

### Migration / Backward Compatibility

- **DB migration:** automatic via the existing `addColumns` mechanism. No user action.
- **Existing `fit_score` values become stale** the first time the new code runs — `score --all` recomputes them with the new algorithm. No forced migration; the user can run it when ready.
- **Existing `status=filtered` rows:** remain visible by default after this ship (no migration hides them). They will progressively clear as the user re-fetches or as `score --all` runs.
- **`--include-filtered` flag:** preserved as a no-op alias; removing it would break muscle memory and scripts. Documentation update notes it's vestigial.
- **`JOB_PREFERENCE.md` migration:** the user's existing file has `preferred_tech` as free-text bullets in the body, not YAML. The matcher will not pick these up until the user moves them to front-matter. The plan ships with a clear one-time edit instruction; no automatic migration.

### Risks

- **Risk: enrichment quality for the 4 new fields.** If the LLM mis-extracts `has_equity` or `ai_intensity`, those dimensions become noisy. Mitigation: the dimensions are weighted low (4 and 5 points max) and the rest of the score still works; can disable a noisy dimension by setting its weight to 0 in YAML.
- **Risk: weights need real calibration.** Defaults are opinionated; the user may need to tune after ~20 jobs. Mitigation: YAML-tunable, instant recalibration via `score --all`, no code changes required.
- **Risk: removing the LLM `fit_score` breaks user expectation of "intelligent" scoring.** The dimension breakdown is actually more informative but looks more mechanical. Mitigation: the `fit_reason` format is designed to read naturally; the breakdown is a feature, not a regression.

### Out of scope (recap from Product Contract)

- Learned-weight / Bayesian scoring from outcome data.
- Multi-profile support.
- Real-time re-scoring on profile edit.
- Removing `--no-filter` flag naming.
- Migration of any existing DB rows beyond the automatic column-add.

### Sequencing

IU1 → IU2 → IU3 (parallel with IU6, IU7) → IU4 → IU5 → IU8. IU3 is the algorithmic heart and can be developed test-first in parallel with the schema/enrichment work. IU4+IU5 wire it in. IU8 is cosmetic and last.
