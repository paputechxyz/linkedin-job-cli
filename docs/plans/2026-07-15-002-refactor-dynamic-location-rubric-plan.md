---
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-brainstorm
title: Dynamic Location Rubric - Plan
type: refactor
date: 2026-07-15
topic: dynamic-location-rubric
execution: code
---

# Dynamic Location Rubric - Plan

## Goal Capsule

**Objective.** Move the `location` rubric from a deterministic system rubric (binary city-token match) to an LLM-rated dynamic rubric, so a user's jurisdiction and proximity nuance — Canada-easy, US-via-C2C, remote-is-flexible, "within 30km of Mississauga" — is captured in a plain-English description the model rates per job. The structured `profile.locations` field is dropped entirely.

**Product authority.** Owner: patrickpu. Source: in-session brainstorm 2026-07-15.

**Open blockers.** None blocking start. LLM proximity judgment (e.g., recognizing Brampton/Oakville sit within ~30km of Mississauga) is an accepted assumption validated during planning — see KTD1 and the score test's mock-rating approach.

## Product Contract

### Summary

Location becomes a dynamic rubric the LLM generates from the preferences paragraph and rates 1–5 per job at enrichment, replacing the deterministic substring matcher. System rubrics shrink to `salary` and `work_arrangement`; the orphaned `profile.locations` token list is removed in favor of the rubric's natural-language description.

### Problem Frame

The deterministic `locationRating` scores a job 5 if `job.Location + job.RemoteType` contains a preferred token, else 1. That model collapses for a remote Canadian contractor: "Remote · United States" misses the `Seattle` token (scores 1) even though it is a strong fit via corp-to-corp, and a hybrid role "within 30km of Mississauga" cannot be matched at all without geocoding. The token list has no concept of country, employment jurisdiction, or radius. Because the rest of the scoring system already routes free-text nuance through LLM-rated dynamic rubrics, location is the mismatch — the one system rubric whose assumption (clean geographic token match) the user's real preferences violate.

### Key Decisions

**Dynamic, not deterministic, for location.** The user's fit criteria are contextual (remote reduces importance; hybrid pins a city; US implies contracting) and geographic (proximity radius) — both are judgment calls a deterministic matcher cannot express. The trade is reproducibility: location scores become LLM-judged rather than exact, accepted because the alternative cannot represent the criteria at all.

**System rubrics are salary and work_arrangement only.** These two keep deterministic logic because they rest on clean numeric (salary floor + FX conversion) and set-membership (remote/hybrid/onsite) comparisons that do not benefit from LLM judgment. Location leaves this set.

**Drop `profile.locations` entirely.** Once location is LLM-judged, the structured token list has no consumer. The rubric's `description` and `items` carry the geographic intent instead. No structured location field is retained as a prompt seed — confirmed by the owner.

### Requirements

**Location rubric conversion**

R1. The `location` rubric is generated as a dynamic rubric from the preferences paragraph by `setup`, and amended by `amend`, like any other dynamic rubric — not hardcoded as a system rubric.

R2. The rubric-generation LLM prompt no longer excludes location from the set of rubrics it may produce. Salary and work_arrangement remain the only exclusions (extracted as structured profile params instead).

R3. At enrichment, the `location` rubric is rated 1–5 by the LLM alongside other dynamic rubrics, from the job's location and remote-type fields. A remote job's location is judged on jurisdiction/eligibility fit, not city proximity, when the rubric description so directs.

R4. The deterministic `locationRating` function, the `RubricLocation` constant, and the `locationMatches` helper are removed from `internal/score`.

**Structured field removal**

R5. The `profile.locations` / `PrefLocations` field is removed from `models.Profile`, the config `ProfileSettings` struct, and the settings template. No consumer may remain.

R6. The `Locations` structured-param field is removed from `llm.GenResult`; the generation prompt no longer asks the LLM to extract a locations token list.

R7. Existing `settings.yaml` files carrying `profile.locations` load without error (unknown YAML keys are ignored), but `reset` / `setup` regenerate the file without the field.

**Consistency ripple**

R8. The "system rubric" count is two (`salary`, `work_arrangement`) everywhere it appears: default settings, `config show` output, `doctor` schema validation, and documentation.

### Acceptance Examples

**AE1. — Hybrid proximity (covers R3).** A hybrid role located in Brampton, ON, against a location rubric described as "hybrid roles must be in Toronto, Mississauga, or within 30km of Mississauga; remote is flexible anywhere," rates 4–5. The deterministic matcher would have scored it 1 (no token match).

**AE2. — Remote jurisdiction (covers R3).** A fully-remote US-based role, against the same rubric, rates 4–5 because remote location is flexible. Under the old matcher it scored 1.

**AE3. — Out-of-radius onsite (covers R3).** An onsite role in Ottawa against the same rubric rates 1–2, reflecting the genuine mismatch a radius constraint encodes.

### Scope Boundaries

**Outside this change:**

- A country/jurisdiction taxonomy or geocoding — proximity and jurisdiction are LLM-judged from natural language, not computed from coordinates.
- A separate contractor-vs-employee scoring dimension — the C2C/employment distinction lives inside the location rubric's description, not as its own rubric.
- Deterministic remote-neutralization logic (the rejected alternative) — remote handling is the LLM's judgment, directed by the rubric text.

### Sources / Research

- `internal/score/score.go:176` — current `locationRating` (the binary matcher being replaced).
- `internal/llm/rubricgen.go:19` — generation prompt line excluding location ("Do NOT generate rubrics for salary, work arrangement, or location").
- `internal/llm/rubricgen.go:34` — `GenResult` struct with the `Locations` field to remove.
- `internal/models/profile.go:14` — `PrefLocations` field to remove.
- Real setup paragraph validated during brainstorm: "remote or hybrid backend role, $200k+ CAD in Toronto, Python Typescript preferred, no C#, for hybrid role location must be in Toronto, Mississauga or anywhere less than 30km from Mississauga."

*Product Contract unchanged. Planning adds the HOW below.*

## Planning Contract

### Known Technical Decisions

**KTD1 — Location-as-dynamic needs no scorer change.** `rateRubric` (`internal/score/score.go:90`) routes any rubric with `kind != "system"` to the `dynamicRatings` map; `dynamicRubricBlock` (`internal/llm/scorer.go:80`) includes all non-system rubrics in the enrichment prompt automatically. Removing `location` from the system-rubric defaults and from the generation exclusion list is sufficient — location then flows through the existing dynamic path with zero structural change to the scorer or the score loop.

**KTD2 — Enrich prompt already carries the job location.** The prompt template passes `j.Location` (`internal/llm/scorer.go:38`), so the LLM sees the raw location string. `j.RemoteType` is not interpolated separately, but the Location string on LinkedIn postings typically encodes arrangement ("Remote · Canada", "Hybrid - Toronto, ON") and the LLM extracts `work_arrangement` as a structured fact in the same call. This is sufficient for the location rating. A robustness option (surfacing `j.RemoteType` explicitly in the job-facts block) is deferred unless enrichment testing reveals misjudged arrangement.

**KTD3 — Migration is non-breaking.** `yaml.Unmarshal` ignores unknown keys, so an existing `settings.yaml` carrying `profile.locations` loads without error. The field is simply unread. `reset` / `setup` regenerate the file without the field. The location rubric is generated fresh as `kind: dynamic` from the paragraph.

**KTD4 — One atomic change, not incremental commits.** Removing a struct field (`PrefLocations` / `Locations` / `GenResult.Locations`) breaks compilation in every package that references it until all references are gone. The implementation units below describe the work by concern; they land together in a single build-and-test-green commit.

### Approach

Bottom-up removal: data model first (config, models, profile), then the deterministic scorer, then LLM generation, then CLI surfaces, then tests, then docs. Because the change is field-removal-driven and cross-layer, verify with `go build ./...` only after all Go units are complete — intermediate compilation is expected to fail. Test with `go test ./...` and smoke-test `config show` + `doctor`.

## Implementation Units

### IU1 — Remove location from the data model

Remove the structured location field and the system-rubric definition so the system no longer knows location as a deterministic concept.

**Files:**
- `internal/config/settings.go` — remove the `RubricLocation = "location"` constant; remove the `{ID: RubricLocation, ...}` entry from `DefaultScoringSettings`; remove `Locations []string` from `ProfileSettings`; remove the `locations: []` line from `defaultSettingsTemplate`; update package and struct comments that name "location" as a system rubric (lines 19, 36, 64, 71, 95, 155).
- `internal/models/profile.go` — remove `PrefLocations []string` (line 14); update the comment at line 8 that references locations.
- `internal/profile/profile.go` — remove `PrefLocations: prefs.Locations` (line 25); remove `len(p.PrefLocations) == 0` from the empty-profile guard (line 40); update the package doc comment (lines 1–8).

**Covers:** R5, R8 (defaults).

### IU2 — Remove deterministic location scoring

Delete the location branch from the scorer. After this, `salary` and `work_arrangement` are the only system-rubric cases; location flows through `dynamicRatings` per KTD1.

**Files:**
- `internal/score/score.go` — remove `case config.RubricLocation: return locationRating(...)` (lines 97–98); delete the `locationRating` function (lines 176–190) and the `locationMatches` helper (lines 212–223); update the package doc comment (lines 3–5) to name only salary and work arrangement.

**Covers:** R4.

### IU3 — Allow location as a dynamic rubric in generation

Stop excluding location from the generation prompt and stop extracting it as a structured param. The LLM now generates a `location` dynamic rubric from the paragraph (with a description like "hybrid must be in Toronto/Mississauga/30km radius; remote is flexible").

**Files:**
- `internal/llm/rubricgen.go` — change the exclusion clause at line 19 from "salary, work arrangement, or location" to "salary or work arrangement"; remove the `"locations": list of preferred location tokens,` param instruction (line 24); remove `Locations []string` from `GenResult` (line 39).
- `cmd/setup.go` — remove the `gen.Locations` → `prof.Locations` assignment (lines 89–90); remove the `locations:` display line (line 109); update the intro text at line 24 ("always apply: salary, work arrangement, location") to drop location, and line 53 similarly.

**Covers:** R1, R2, R6.

### IU4 — CLI surfaces and repo settings

Update the CLI validation and display surfaces that still assume location is structured.

**Files:**
- `cmd/doctor.go` — remove `"locations"` from the `profile` keys in `settingsTopSchema` (line 120).
- `settings.yaml` (repo-root dev config) — remove the `location` system-rubric entry from `scoring.rubrics` and the `locations:` field from `profile`.

**Note:** `cmd/config.go` needs no change — `countSystemRubrics` already counts dynamically by `kind == "system"`, so it reports 2 once location leaves the defaults.

**Covers:** R8, R7 (repo settings).

### IU5 — Tests

Rewrite tests that assert the old location behavior or the old system-rubric count.

**Files:**
- `internal/score/score_test.go` — delete `TestCompute_LocationRating` (lines 164+) and any helper setup referencing `config.RubricLocation` / `PrefLocations`.
- `internal/config/settings_test.go` — change `TestLoadSettings_DefaultWhenAbsent` to assert 2 system rubrics (was 3) and that neither has `id: location`.
- `cmd/config_test.go` (if it asserts "3 system" in the show output) — update to 2.

**Covers:** verification for R1–R8.

### IU6 — Documentation

Remove location-as-system-rubric and `profile.locations` from all user-facing docs.

**Files:**
- `README.md` — update the settings example (drop the `location` system rubric, drop `locations` from profile).
- `hermes-skill/SKILL.md` — update any system-rubric enumeration to salary + work_arrangement.
- `hermes-skill/references/pipeline.md` — update the system-rubric list and any `fit_reason` example that names location as deterministic.
- `hermes-skill/references/auth-config.md` — drop the `location` system rubric and `locations` profile field from the settings example.

**Covers:** R8 (docs).

## Test Strategy

**Unit tests.** `TestCompute_LocationRating` is deleted (the function it tested is gone). `TestLoadSettings_DefaultWhenAbsent` asserts 2 system rubrics. Existing salary and work-arrangement tests are untouched and must still pass.

**Integration signal.** `go build ./... && go vet ./... && go test ./...` green across all 12 packages (internal/filter already removed). The `config show` smoke-test reports "2 system"; `doctor` reports all expected keys present against a settings file without `locations`.

**LLM-rating coverage (assumption per KTD1).** The location rubric's enrichment rating is LLM-judged and therefore not unit-testable with a deterministic oracle. The acceptance examples (AE1–AE3) describe the expected judgment; they are validated by inspecting real enrichment output during a `setup` + `fetch` dogfood cycle, not by an automated assertion. This is an accepted limitation of the dynamic-rubric architecture.

## Migration

Non-breaking (KTD3). Existing `settings.yaml` files with `profile.locations` load without error — `yaml.Unmarshal` ignores unknown keys. Users who want the new location rubric run `setup` (or `amend`) to regenerate rubrics from their paragraph; the LLM produces a `location` dynamic rubric from the location text in the paragraph. Users who do nothing keep their old file, with `locations` silently unread and no `location` rubric present (location simply does not factor into scoring until generated).

## Risks

- **LLM arrangement-blindness (low).** If a posting's Location string does not encode remote/hybrid and the description is vague, the LLM may rate a remote job's location as a miss. Mitigated by KTD2 (arrangement is usually in the string) and the optional robustness enhancement; the impact is one rubric's rating, not a crash.
- **Existing settings.yaml without a location rubric (none).** Users who skip `setup` lose location scoring entirely until they regenerate. This is the intended behavior, not a risk — the old deterministic matcher is gone by design.

## Sequencing

Single commit. IU1 → IU2 → IU3 → IU4 → IU5 (verify: `go build && go vet && go test ./...`) → IU6 (docs). IU1–IU3 are compile-coupled and cannot be split into independently-green commits. Smoke-test `config show` and `doctor` after IU5.
