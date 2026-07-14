---
title: "Preference-Aware Work Arrangement Scoring - Plan"
type: fix
date: 2026-07-14
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-plan-bootstrap
execution: code
---

# Preference-Aware Work Arrangement Scoring - Plan

## Goal Capsule

- **Objective:** Make work arrangement scoring preference-aware — empty or all-three selections mean "no preference" (neutral to score and filter); any proper subset means each matching arrangement adds its full weight.
- **Authority hierarchy:** settings.yaml profile knobs and existing rubric conventions are the source of truth. User's verbal description of desired behavior overrides current code behavior where they conflict.
- **Stop conditions:** All units implemented, all existing tests updated and passing, new test scenarios green, `go build ./...` clean.
- **Execution profile:** Standard — three tightly-scoped units in a Go CLI, each independently committable.
- **Tail ownership:** The plan ends at written code + passing tests. No rollout, migration, or operational steps.

---

## Product Contract

### Summary

Generalize the work arrangement dimension from remote-only to any preferred arrangement. When the user selects no arrangements or all three, the scorer and hard filter treat it as "no preference" and work arrangement has zero effect on the score. When the user selects a proper subset, each preferred arrangement that matches the job's detected arrangement contributes the full weight.

### Problem Frame

The current `remoteTiebreakDimension` in `internal/score/score.go` only rewards remote jobs, and only when the profile contains "remote" in its work arrangement list. The user's settings select all three arrangements (remote, hybrid, onsite), which today still activates the remote bonus — meaning remote jobs get the full weight while hybrid jobs get only a third of it (weight/3) and onsite jobs get nothing, despite the user expressing equal openness to all three. The user wants: all three (or none) = neutral; any subset = each match adds equally.

The hard filter (`internal/filter/filter.go` and `internal/score/score.go:hardFilterCap`) has a parallel issue: with all three selected, jobs lacking any arrangement signal still get capped/filtered because the current substring match finds no token. Under the new semantics, all-three = no preference should mean no filtering penalty either.

### Requirements

**Preference semantics**

- R1. An empty work arrangement list means "no preference" — the dimension contributes zero points and the hard filter does not cap or reject on work arrangement.
- R2. A list containing all three arrangements (remote, hybrid, onsite) also means "no preference" with the same neutral effect.
- R3. Any proper subset of {remote, hybrid, onsite} means "has preference" — the dimension and hard filter are active.

**Scoring dimension**

- R4. When the user has a preference and the job's detected arrangement matches a preferred arrangement, the dimension contributes the full weight.
- R5. When the user has a preference and the job's detected arrangement does not match any preferred arrangement, the dimension contributes zero (and the hard filter cap fires separately).
- R6. Each matching arrangement contributes equally — no tiering. The current hybrid-as-partial-remote concept (hybrid = weight/3) is removed.

**Hard filter**

- R7. Both the hard-filter cap in the scorer (`hardFilterCap`) and the LLM-gating filter (`PassesHardFilter`) respect the no-preference rule: when no preference is expressed, work arrangement never triggers a cap or rejection.

**Consistency**

- R8. The scorer and filter use the same arrangement detection logic so a job that passes the filter also receives the correct dimension score.

### Scope Boundaries

#### Deferred to Follow-Up Work

- Renaming the YAML weight key from `remote_tiebreak` to `work_arrangement` — kept as-is for backward compatibility with existing settings.yaml files; can be revisited if per-arrangement weights are added later.
- Renaming the `CapNonRemote` cap reason constant and its persisted `"non_remote"` value — under the new logic this cap fires for any non-matching arrangement (e.g., onsite-only pref + remote job), not just non-remote jobs. Deferred because the value is stored in `jobs.score_cap_reason` and renaming it requires a data migration for existing stored jobs.
- Aligning the SQL-level work-arrangement filter in `internal/store/store.go` (CLI flag-driven `LIKE` substring matching) with `DetectArrangement` semantics — that filter serves a different entry point (search/recommended CLI flags) and is intentionally left on its current substring logic.
- Per-arrangement weights (separate YAML keys for remote, hybrid, onsite scoring) — the current single-weight model is sufficient for the user's described behavior.
- Rescoring existing stored jobs after the logic change — the user can run `linkedin-jobs rescore-all` manually.

---

## Planning Contract

### Key Technical Decisions

**KTD1. "No preference" detection lives on `models.Profile`.**

A `HasWorkArrangementPreference()` method on `models.Profile` returns false when the list is empty or contains all three canonical arrangements (case-insensitive). Centralizing this on the model ensures the scorer and filter agree. The canonical set is `{remote, hybrid, onsite}`; "on-site" and "on site" normalize to "onsite" before the check.

**KTD2. Job arrangement detection via `DetectArrangement` method on `JobPosting`.**

A method on `models.JobPosting` builds the lowercased location+remote_type blob internally and returns a single normalized arrangement: `"remote"`, `"hybrid"`, `"onsite"`, or `""` (unknown). Detection priority is hybrid > remote > onsite — a blob containing both "remote" and "hybrid" resolves to "hybrid" because hybrid is the more specific arrangement mode. The function also matches the hyphenated "on-site" and spaced "on site" forms to "onsite", fixing a pre-existing gap where the substring matcher in `arrangementMatches` fails to match "onsite" prefs against "on-site" blobs.

**KTD3. Each matching arrangement contributes the full weight.**

The user's language ("should add to the score") implies equal contribution per match. The current tiering (remote=full, hybrid=weight/3) is removed. A job has exactly one detected arrangement, so at most one match fires — there is no multi-match accumulation within a single job. For the no-preference case (empty or all-three), zero contribution is chosen over equal-reward-all because the user said "it shouldn't affect the score" — zero avoids inflating every job's baseline when the user is fully agnostic.

**KTD4. Hard filter replaces permissive substring matching with detection-based matching.**

The current `arrangementMatches` returns true if ANY preferred token appears as a substring of the blob. The new approach detects the job's single arrangement via `DetectArrangement`, then checks membership in the preferred set. This is stricter and deterministic: a blob like "Remote (Hybrid)" resolves to "hybrid", not permissively matching a "remote" pref. The trade-off is that a remote-only-pref user will have such dual-signal jobs filtered out — the job advertises remote work but is classified as hybrid by the priority rule. When the detected arrangement is `""` (unknown) and the user has a preference, the job does not match — preserving the current conservative behavior for jobs with no arrangement signal.

**KTD5. YAML weight key stays `remote_tiebreak`; dimension display name changes to `work_arrangement`.**

The struct field, YAML key, and default template key remain `remote_tiebreak` for backward compatibility. The dimension `Name` field (rendered in `fit_reason`) changes from `"remote"` to `"work_arrangement"` to accurately describe the generalized behavior.

---

## Implementation Units

### U1. Shared arrangement helpers in models package

**Goal:** Add `HasWorkArrangementPreference` on `Profile` and `DetectArrangement` as a shared helper so the scorer and filter share one source of truth for arrangement logic.

**Requirements:** R1, R2, R3, R8

**Dependencies:** none

**Files:**
- `internal/models/profile.go` — add `HasWorkArrangementPreference() bool` method
- `internal/models/job.go` — add `DetectArrangement() string` method on `JobPosting` (builds blob internally)
- `internal/models/profile_test.go` — test the new method
- `internal/models/job_test.go` — new or existing test file for `DetectArrangement`

**Approach:**

`HasWorkArrangementPreference` normalizes the pref list to lowercase, trims whitespace, and converts "on-site"/"on site" to "onsite". Returns false if the resulting set is empty or equals `{remote, hybrid, onsite}`. Returns true for any proper subset.

`DetectArrangement` builds `blob := strings.ToLower(j.Location + " " + j.RemoteType)` (encapsulating the currently-duplicated blob construction), then checks in priority order: "hybrid" → "remote" → "onsite"/"on-site"/"on site" → "". Returns the canonical lowercase token or empty string.

Both methods eliminate the need for the duplicated `arrangementMatches` helper currently living independently in `internal/score/score.go` and `internal/filter/filter.go`.

**Patterns to follow:** Existing model methods like `HasSalary()`, `SalaryMax()` on `JobPosting` follow the same pattern of deriving a computed value from raw fields.

**Test scenarios:**

*HasWorkArrangementPreference:*
- Empty list → false (Covers R1)
- All three (remote, hybrid, onsite) → false (Covers R2)
- Remote only → true (Covers R3)
- Hybrid + onsite → true
- Remote + hybrid → true
- Case-insensitive: `["Remote", "HYBRID", "On-Site"]` → false (all three)
- Hyphenated form: `["on-site"]` → true (proper subset, normalizes correctly)

*DetectArrangement:*
- Blob with "remote" → "remote"
- Blob with "hybrid" → "hybrid"
- Blob with "onsite" → "onsite"
- Blob with "on-site" (hyphenated) → "onsite"
- Blob with "on site" (spaced) → "onsite"
- Blob with both "remote" and "hybrid" → "hybrid" (priority)
- Empty location + empty remote_type → ""
- Location with no arrangement keyword → ""

**Verification:** `go test ./internal/models/...` passes. `go build ./...` compiles.

---

### U2. Generalize work arrangement scoring + hard-filter cap in score package

**Goal:** Replace `remoteTiebreakDimension` with a preference-aware `workArrangementDimension`, and update `hardFilterCap`'s work arrangement check to skip when no preference.

**Requirements:** R1, R2, R3, R4, R5, R6, R7, R8

**Dependencies:** U1

**Files:**
- `internal/score/score.go` — rewrite `remoteTiebreakDimension` → `workArrangementDimension`; update `hardFilterCap` work arrangement block; remove now-unused `arrangementMatches` and `sliceContains` if no longer needed (check other callers first)
- `internal/score/score_test.go` — update `TestCompute_RemoteTiebreak`, `TestCompute_NonRemoteCapsAt55`, and any other tests encoding old remote-only behavior; add new preference-matrix tests
- `internal/config/settings.go` — update the comment for `remote_tiebreak` in `defaultSettingsTemplate` to reflect generalized behavior

**Approach:**

*Dimension rewrite:* The new `workArrangementDimension` checks `profile.HasWorkArrangementPreference()`. If false, returns zero. If true, calls `job.DetectArrangement()` and checks whether the result is in the normalized pref set (the same normalization `HasWorkArrangementPreference` uses — lowercase, trim, "on-site"/"on site" → "onsite"). On match, returns `Dimension{Name: "work_arrangement", Points: w.RemoteTiebreak, Reason: <arrangement>}`. On no match, returns zero.

*Hard-filter cap rewrite:* The work arrangement block in `hardFilterCap` currently fires when `len(profile.PrefWorkArrangement) > 0 && !arrangementMatches(blob, profile.PrefWorkArrangement)`. Replace with: if `profile.HasWorkArrangementPreference()` and the detected arrangement is either `""` (unknown) or not in the normalized pref set, fire the `CapNonRemote` cap. When no preference, skip entirely.

*Call site:* Update the `Compute` function's dimension call from `remoteTiebreakDimension` to `workArrangementDimension`.

*Settings template:* Update the comment from `# full-remote=full, hybrid=partial` to describe the new generalized behavior (e.g., `# each preferred arrangement match = full weight`).

**Patterns to follow:** The existing dimension functions (`salaryDimension`, `techOverlapDimension`, etc.) follow the same structure: check max weight, check profile fields, compute points, return Dimension.

**Test scenarios:**

*No-preference dimension cases:*
- Empty prefs + remote job → 0 arrangement points, no cap (Covers R1)
- All-three prefs + remote job → 0 arrangement points, no cap (Covers R2)
- All-three prefs + onsite job → 0 arrangement points, no cap
- All-three prefs + job with no arrangement signal → 0 arrangement points, no cap (previously capped at 55)

*Single-arrangement preference cases:*
- Remote-only pref + remote job → full weight (Covers R4)
- Remote-only pref + hybrid job → 0 points; cap fires at CapNonRemote (Covers R5)
- Remote-only pref + onsite job → 0 points; cap fires
- Onsite-only pref + onsite job → full weight
- Onsite-only pref + remote job → 0 points; cap fires
- Hybrid-only pref + hybrid job → full weight
- Hybrid-only pref + remote job → 0 points; cap fires

*Multi-arrangement preference cases:*
- Hybrid+onsite pref + hybrid job → full weight (Covers R4)
- Hybrid+onsite pref + onsite job → full weight
- Hybrid+onsite pref + remote job → 0 points; cap fires
- Remote+hybrid pref + remote job → full weight
- Remote+hybrid pref + hybrid job → full weight
- Remote+onsite pref + remote job → full weight
- Remote+onsite pref + onsite job → full weight

*Hard-filter cap interaction:*
- Remote-only pref + job with no arrangement signal → cap fires at CapNonRemote (unknown arrangement, conservative)
- All-three prefs + job with no arrangement signal → no cap (Covers R7)

*FitReason rendering:*
- Dimension name in output is "work_arrangement" not "remote"

*Existing test updates:*
- `TestCompute_RemoteTiebreak` → rename and rework: hybrid case with remote-only prefs now caps instead of scoring max/3; use multi-arrangement profiles to test hybrid/onsite dimension contributions
- `TestCompute_NonRemoteCapsAt55` → update if profile setup changes; verify cap still fires for mismatched single-arrangement prefs
- `TestCompute_AllSignalsCombined` → verify score unchanged (fullProfile has remote-only prefs, job is remote → still gets full weight); dimension name assertion updates if any

**Verification:** `go test ./internal/score/...` passes. `go build ./...` compiles. FitReason output shows "work_arrangement" dimension name.

---

### U3. Update hard filter in filter package to respect no-preference

**Goal:** Update `PassesHardFilter` in `internal/filter/filter.go` so work arrangement filtering is skipped when the user has no preference (empty or all three).

**Requirements:** R1, R2, R7, R8

**Dependencies:** U1

**Files:**
- `internal/filter/filter.go` — replace the work arrangement block in `PassesHardFilter` to use `HasWorkArrangementPreference` and `DetectArrangement`; remove the now-unused `arrangementMatches` helper if no other callers remain in this package
- `internal/filter/filter_test.go` — update existing tests and add no-preference cases

**Approach:**

The current work arrangement check in `PassesHardFilter`:
```
if len(p.PrefWorkArrangement) > 0 && !arrangementMatches(blob, p.PrefWorkArrangement) {
    return false
}
```

Replace with:
```
if p.HasWorkArrangementPreference() {
    arrangement := job.DetectArrangement()
    if arrangement == "" || !normalizedPrefSet.Contains(arrangement) {
        return false
    }
}
```

When no preference (`HasWorkArrangementPreference()` returns false), the block is skipped entirely — jobs pass regardless of arrangement signal. This mirrors the hard-filter cap change in U2.

**Patterns to follow:** The existing `PassesHardFilter` already follows the "nil profile passes everything" and "unknown fields are never mismatches" conventions. The change preserves both: nil profile still returns true; the conservative treatment of unknown arrangement (empty detection) as non-matching when the user has a preference is unchanged.

**Test scenarios:**

*No-preference cases:*
- Empty prefs + onsite job → passes (Covers R1)
- Empty prefs + job with no arrangement signal → passes (existing behavior preserved)
- All-three prefs + onsite job → passes (Covers R2)
- All-three prefs + job with no arrangement signal → passes (Covers R7; previously filtered)

*Single-preference cases:*
- Remote-only pref + remote job → passes
- Remote-only pref + hybrid job → filtered (Covers R8; hybrid detected, not in prefs)
- Remote-only pref + onsite job → filtered
- Onsite-only pref + onsite job → passes
- Onsite-only pref + remote job → filtered
- Onsite-only pref + "on-site" hyphenated blob → passes (normalization fix)

*Multi-preference cases:*
- Hybrid+onsite pref + hybrid job → passes
- Hybrid+onsite pref + onsite job → passes
- Hybrid+onsite pref + remote job → filtered
- Remote+hybrid pref + "Remote (Hybrid)" blob → passes (detects hybrid, in prefs)

*Existing test updates:*
- `TestPassesHardFilter_RemoteRequired` → update: "hybrid mentions remote-ish" case now correctly filtered (hybrid detected, not in remote-only prefs); add multi-arrangement pref cases
- `TestPassesHardFilter_Combined` → verify unchanged behavior for single-arrangement pref

**Verification:** `go test ./internal/filter/...` passes. `go build ./...` compiles.

---

## Verification Contract

| Scope | Command | Purpose |
|---|---|---|
| All affected packages | `go test ./internal/models/... ./internal/score/... ./internal/filter/...` | Proves U1–U3 behavioral changes |
| Full test suite | `go test ./...` | No regressions in unrelated packages |
| Build | `go build ./...` | Compiles cleanly |

---

## Definition of Done

- `HasWorkArrangementPreference` on `Profile` and `DetectArrangement` on `JobPosting` exist and are tested (U1).
- The scoring dimension rewards any matching preferred arrangement at full weight; empty/all-three prefs produce zero dimension points (U2).
- The hard-filter cap in the scorer and `PassesHardFilter` in the filter package both skip work arrangement when no preference is expressed (U2, U3).
- All new test scenarios pass; existing tests updated to match new behavior (no test deleted without replacement coverage).
- `go test ./...` green and `go build ./...` clean.
- No abandoned experimental code left in the diff.
