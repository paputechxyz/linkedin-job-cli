---
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-plan-bootstrap
title: LLM-Required Scoring - Plan
type: refactor
date: 2026-07-15
topic: llm-required-scoring
execution: code
---

# LLM-Required Scoring - Plan

## Goal Capsule

**Objective.** Make a resolved LLM provider a hard requirement for every fetch+score command (`recommended`/`url`/`search`/`watch`): remove the `--no-score` flag and the silent "Scoring skipped" fallback, so a missing provider fails fast with a setup prompt and performs no fetch. Salary and work_arrangement stay deterministic system rubrics.

**Product authority.** Owner: patrickpu. Source: in-session request 2026-07-15 ("LLM is a requirement with dynamically generated rubrics — get rid of the legacy non-LLM path"; "remove --no-score flag, everything must pass through the LLM"; "no LLM do nothing, do not fetch, just prompt the user to set it up").

**Open blockers.** None.

---

## Product Contract

### Summary

The LLM becomes mandatory on the default fetch+score path. `--no-score` is removed entirely; when no provider resolves, the command prints a setup prompt and exits nonzero without fetching or persisting. Every survivor of the user gates is then enriched+scored — there is no fetch-only mode. The deterministic system rubrics (salary, work_arrangement) are unchanged.

### Problem Frame

The dynamic-rubric scoring system already routes every job through the LLM for enrichment + per-rubric ratings, but two legacy non-LLM paths remain. First, `ingest`'s graceful degradation: when `llm.Resolve` fails it prints "Scoring skipped", leaves the provider nil, and silently persists jobs unscored. Second, the `--no-score` flag on `recommended`/`url`/`search` opts out of the LLM entirely for a fetch-only run. Both contradict the product stance that LLM scoring is a requirement, not an optional layer — they let jobs land in the DB with no score and no signal that the user's rubrics were ever applied.

### Requirements

- R1. A fetch+score command with no resolved LLM provider exits nonzero with a setup prompt and performs no LinkedIn fetch and no persistence.
- R2. The `--no-score` flag is removed from every command; there is no fetch-only path.
- R3. Every job that survives the user gates and the dedup check is enriched+scored (the provider-nil skip branch is gone).
- R4. Salary and work_arrangement remain deterministic system rubrics; the `internal/score` raters and the `Kind: "system"` routing are unchanged.
- R5. Living docs and setup messaging no longer describe scoring as optional and no longer document `--no-score`.

### Acceptance Examples

- AE1. **Covers R1.** Given no LLM provider env vars, when the user runs `recommended`, then it prints a setup prompt and exits nonzero without calling LinkedIn.
- AE2. **Covers R2.** Given any configuration, when the user runs `search "Staff Engineer" Toronto --no-score`, then `--no-score` is rejected as an unknown flag.
- AE3. **Covers R3.** Given a resolved provider, when `ingest` runs, then every non-duplicate survivor is enriched+scored (no job is silently persisted unscored).

### Scope Boundaries

**Outside this change:**

- Converting salary/work_arrangement to dynamic LLM rubrics — the owner explicitly wants them deterministic.
- Removing the `--min-salary` / `--remote` / `--hybrid` / `--onsite` user gates — those are opt-in pre-filters that run before scoring, not part of the scoring path.
- Rewriting historical plan docs under `docs/plans/` that mention `--no-score` — they are point-in-time records.

**Deferred to follow-up:**

- The stale "3 system rubrics" comment in `cmd/doctor.go:171` (leftover from the location-rubric refactor) is unrelated doc drift and is fixed separately.

---

## Planning Contract

### Key Technical Decisions

**KTD1 — Resolve in the caller, before the listing fetch.** Each command resolves the provider at the top of its `RunE` and `die`s on failure *before* calling LinkedIn at all (`c.Recommended` / `c.Search` / `c.SearchURL` all run in the caller, ahead of `ingest`). Resolving inside `ingest` would gate only the detail fetch, not the listing fetch, and would leave R1/AE1 and the owner's "do not fetch" directive unmet. With no `--no-score` escape, a missing key makes the command's whole purpose unfulfillable, and failing first avoids burning slow, rate-limited fetches and avoids half-saved runs. Callers then pass the non-nil provider into `ingest`, making the mandatory-provider precondition explicit in the signature rather than a runtime hope.

**KTD2 — A pure `scoringProvider` helper owns the no-provider error; `mustResolveProvider` wraps it for callers.** `scoringProvider(p *llm.Provider, err error) (*llm.Provider, error)` returns a setup-guidance error when the provider is nil or resolution errored, else returns the provider unchanged. `mustResolveProvider() *llm.Provider` (in `cmd/pipeline.go`) wraps `scoringProvider(llm.Resolve(loadCfg()))` and calls `die` on its error; each caller invokes it once at the top of its `RunE`. Splitting the policy from the exit makes the "missing provider → actionable error, never a silent nil" rule unit-testable without depending on host opencode credential state (which `llm.Resolve` also reads via `~/.local/share/opencode/auth.json` and which would make a Resolve-forced-failure test flaky across machines). The message points at the same knobs `doctor`/`setup` advertise.

**KTD3 — U1 and U2 are compile-coupled and land in one build-green commit.** U1 changes `ingest`'s signature (takes a `*llm.Provider` param, drops the `noScore` field); all four callers in U2 fail to compile until they both pass the provider and drop their `noScore: …` line. Like the location-rubric plan's field-removal work, the units describe separate concerns but cannot be split into independently-green commits.

### Implementation notes

- `ingest`'s signature gains a `provider *llm.Provider` parameter and drops its internal `llm.Resolve`/silent-skip block; the scoring loop's `if provider != nil` guard (`cmd/pipeline.go:109`) is removed since the provider is now guaranteed non-nil by the caller. `paceLLM`/`scoreDelay` and `profileStatus` remain — they pace and narrate LLM calls that always happen now.
- The listing fetch runs in each caller before `ingest` is reached (`c.Recommended` at `recommended.go:45`, `c.Search` at `search.go:57`/`watch.go:48`, `c.SearchURL` at `url.go:66`), so the resolve+die must also sit in the caller to gate it — this is the substance of KTD1.
- `watch` already omits `--no-score`; it gains the `mustResolveProvider()` call (no flag change).
- `die` (`cmd/root.go:80`) calls `os.Exit(1)`; per project convention (enrich/rescore-all die untested), the die path is verified by smoke test, while the pure `scoringProvider` policy is unit-tested.

---

## Implementation Units

### U1. Shared provider helpers + `ingest` takes a provider

- **Goal.** Make the mandatory-provider policy a shared, testable helper and turn `ingest` into a pure consumer of a non-nil provider.
- **Requirements.** R1, R3.
- **Dependencies.** None.
- **Files.** `cmd/pipeline.go`, `cmd/pipeline_test.go`.
- **Approach.** Add `scoringProvider(p *llm.Provider, err error) (*llm.Provider, error)` (pure policy — setup-guidance error on nil/error) and `mustResolveProvider() *llm.Provider` (wraps `scoringProvider(llm.Resolve(loadCfg()))` and `die`s on error). Change `ingest`'s signature to `ingest(jobs []*models.JobPosting, provider *llm.Provider, opts ingestOptions)`; remove its internal `llm.Resolve` + "Scoring skipped" block, remove `noScore bool` from `ingestOptions`, and drop the `if provider != nil` guard so every non-duplicate survivor runs `enrichAndScoreJob`. Print `profileStatus` after the persist step (the provider is already resolved by the caller).
- **Patterns to follow.** The resolve-and-die shape already in `cmd/enrich.go:25` / `cmd/rescore_all.go:42`; the existing `ErrNoProvider` text (`internal/llm/provider.go:26`) and `doctor`/`setup` messaging for the prompt wording.
- **Test scenarios.**
  - `scoringProvider(nil, ErrNoProvider)` returns an error whose message names the setup knobs (covers R1 at the policy level).
  - `scoringProvider(&llm.Provider{}, nil)` returns the provider with no error.
  - Existing gate tests (`TestGateDropReason_*`, `TestApplyGates_*`) stay green — they test `gateDropReason`/`applyGates` directly and do not call `ingest`, so the signature change does not touch them.
  - The caller-side die/exits-before-fetch behavior is verified by smoke test (see Verification Contract), not a unit test, per the project's die-path convention.
- **Verification.** `go build ./... && go vet ./... && go test ./cmd/...` green; smoke: `recommended` with no key exits nonzero with the setup prompt and performs no fetch (verified end-to-end via U2's caller wiring).

### U2. Wire callers: resolve-and-die up front, drop `--no-score`

- **Goal.** Each fetch+score command resolves the provider before any LinkedIn call and passes it into `ingest`; the `--no-score` flag and its plumbing are removed from the three commands that exposed it.
- **Requirements.** R1, R2, R3.
- **Dependencies.** U1 (compile-coupled — see KTD3; lands in one commit).
- **Files.** `cmd/recommended.go`, `cmd/url.go`, `cmd/search.go`, `cmd/watch.go`.
- **Approach.** In each command's `RunE`, call `provider := mustResolveProvider()` at the very top (before `newClient`/`c.Recommended`/`c.Search`/`c.SearchURL`) and pass `provider` into the `ingest(...)` call. In `recommended`/`url`/`search` also remove the `*NoScore` package var (`recNoScore`/`urlNoScore`/`searchNoScore`), the `noScore: <var>` line from the `ingestOptions{}` literal, and the `Flags().BoolVar(&<var>, "no-score", …)` registration. `watch` gets the `mustResolveProvider()` call and the provider arg only (it never had the flag).
- **Test scenarios.** `Test expectation: none -- pure wiring + flag/var removal; the build plus the smoke tests cover it.`
- **Verification.** `recommended --no-score`, `url … --no-score`, and `search … --no-score` are each rejected as an unknown flag; with no key, `recommended`/`search`/`url`/`watch` each die with the setup prompt before fetching.

### U3. Update messaging and docs to reflect LLM-required

- **Goal.** Remove every "scoring is optional / skipped" claim and every `--no-score` reference from living docs and setup output.
- **Requirements.** R5.
- **Dependencies.** U2 (docs should not advertise a flag that no longer exists).
- **Files.** `cmd/setup.go`, `README.md`, `hermes-skill/SKILL.md`, `hermes-skill/references/auth-config.md`, `hermes-skill/references/pipeline.md`, `hermes-skill/references/commands.md`.
- **Approach.** `setup.go` LLM-provider step (`setup.go:127`): reframe "Scoring is optional — every read command works without an LLM" to state a provider is required for scoring and that without one the fetch commands exit and ask the user to configure one. `README.md:442`: replace "No key? Scoring is skipped with a clear message…" with the required-provider behavior. `SKILL.md:62` and `auth-config.md:86`: drop the "Scoring is optional" sentence. `pipeline.md:75`: delete the `--no-score` line. `commands.md`: delete the three `--no-score` table rows (lines 30, 53, 76) and update the watch note (line 101) to drop its `--no-score` reference. Do not touch `docs/plans/`.
- **Test scenarios.** `Test expectation: none -- documentation.`
- **Verification.** `rg -n "no-score|Scoring is optional|scoring is skipped"` over `README.md`, `hermes-skill/`, and `cmd/setup.go` returns no hits (`docs/plans/` is intentionally out of scope per Scope Boundaries).

---

## Verification Contract

| Gate | Command / check | Proves |
|---|---|---|
| Build + vet + tests | `go build ./... && go vet ./... && go test ./...` | Green across all packages; `scoringProvider` policy tests pass; gate tests unchanged. |
| Flag removal (smoke) | `linkedin-jobs search "x" Toronto --no-score` | Rejected as unknown flag (R2). |
| Fail-fast (smoke) | `linkedin-jobs recommended` with no provider env | Prints setup prompt, exits nonzero, performs no LinkedIn fetch (neither listing nor detail) (R1, AE1). |
| End-to-end (smoke) | `linkedin-jobs search "Staff Engineer" Toronto --top 2` with a key | Fetches, scores every survivor, persists scores (R3). |
| Docs clean | `rg -n "no-score\|Scoring is optional\|scoring is skipped" README.md hermes-skill cmd/setup.go` | No hits in living docs or setup output (R5). |

## Definition of Done

- `go build ./... && go vet ./... && go test ./...` green.
- `--no-score` is rejected by `recommended`, `url`, and `search`; all four fetch+score commands resolve the provider before any LinkedIn call.
- No provider → setup prompt + nonzero exit, no fetch, no persist (fail-fast).
- No `noScore` / `*NoScore` dead code remains; the `provider != nil` guard is gone.
- No living doc or setup message calls scoring optional or mentions `--no-score`.
- Salary/work_arrangement deterministic scoring is untouched.
