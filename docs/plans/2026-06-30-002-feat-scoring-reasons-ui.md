# Plan: Dominant, human-readable scoring reasons

**Date:** 2026-06-30
**Status:** Approved (build mode)
**Scope:** Surface *why* a job scored the way it did — specifically and human-readily — and make that reason dominant in the web UI next to the score.

## Context (what already exists)

A reason is already computed and stored on every scored job:
- `fit_reason` column — rendered string from `score.FitReason()` (`internal/score/score.go:156`).
- `score_cap_reason` column — stable machine code (`deal_breaker_tech`, `salary_under_floor`, …).

Wired end-to-end: persisted by `store.SetScore`, surfaced to the view via `jobView.FitReason` (`cmd/serve.go:289`, mapped at `:313`), rendered collapsed at `cmd/serve.go:1148`.

**Two real gaps:**
1. **Content:** cap reasons are terse machine codes that don't name the offender — `cap: deal_breaker_tech (30)` doesn't say *which* tech; `non_remote` doesn't say what you asked for. The specifics (matched token, miss %, preferred locations) are computed inside `Compute`/`hardFilterCap` but **discarded** before rendering.
2. **UI:** the reason is buried in a collapsed `<details>` (`cmd/serve.go:1148`), never next to the score badge (`.job-head`, `:1090-1095`).

**Decisions (confirmed with user):**
- Reason source: **deterministic only** — improve `score.FitReason` purely from the computed result. No LLM, no token cost, fully reproducible.
- UI prominence: **caption under the badge** in a right-side `.job-head-aside`; full breakdown stays in the expandable `<details>`.
- Storage depth: **just improve the reason string** in place. No new column, no structured JSON. (`fit_reason` already exists — no migration.)

No schema migration is needed.

## Step 1 — Carry cap specifics through `Result` (`internal/score/score.go`)

Add a human-detail field to `Result` (`:32`) without touching the stable `CapReason` code:

```go
type Result struct {
    Score      int
    CapReason  string   // unchanged: stable machine code, persisted to score_cap_reason
    CapDetail  string   // NEW: human sentence naming the offender
    Dimensions []Dimension
}
```

Populate `CapDetail` where the specifics are known:
- Deal-breaker branch (`Compute`, `:109`): `dealBreakerMatch` already returns the matched token → `CapDetail = fmt.Sprintf("Deal-breaker tech %q in stack", token)`.
- `hardFilterCap` (`:214`): extend `capResult` (`:205`) with a `detail string`, built at each firing site:
  - Salary (`:221`): `fmt.Sprintf("Salary %s is %.0f%% under your %s floor", money(converted,…), missPct*100, money(floor,…))`.
  - Non-remote (`:234`): `"Role has no remote signal; you want fully remote"`.
  - Location (`:240`): `fmt.Sprintf("Location %q not in your preferred (%s)", job.Location, profile.PrefLocations)`.
  - The lowest-cap selection (`:251`) carries the matching `detail`; `Compute` threads `cap.detail` into `Result.CapDetail`.

## Step 2 — Rewrite `FitReason` to render sentences (`internal/score/score.go:156`)

- Capped → `fmt.Sprintf("%s → capped at %d", r.CapDetail, r.Score)` (falls back to the machine code if `CapDetail` is empty, for defensiveness).
- Baseline-only → `"no positive signals matched your profile → %d"`.
- Dimensions → unchanged format `+N dim (reason), … | total N` (already human-readable; truncates well for the caption since salary/tech lead). `score_cap_reason` column keeps the stable machine code — only the human `fit_reason` string changes.

## Step 3 — Backfill existing rows

Rebuild, then run **`linkedin-jobs score --all`** (`cmd/score.go:19`) to regenerate every row's `fit_reason` with the new sentences. Deterministic → identical on re-runs.

## Step 4 — Web UI: caption under the badge (`cmd/serve.go`)

- **View:** add `ScoreBlurb string` and `ScoreCapped bool` to `jobView` (`:277`). In `toJobView` (`:292`): `v.ScoreBlurb = preview(j.FitReason, 110)` (reuse `preview()` at `:361`); `v.ScoreCapped = j.ScoreCapReason != ""`. Full `FitReason` stays for the details block.
- **Template (`:1090-1095`):** wrap the badge + blurb in a right-side aside:

```html
<div class="job-head-aside">
  {{score badge, unchanged}}
  {{if .ScoreBlurb}}<div class="score-blurb score-blurb--{{if .ScoreCapped}}capped{{else}}{{.ScoreClass}}{{end}}">{{.ScoreBlurb}}</div>{{end}}
</div>
```

Keep the existing collapsed `<details>Fit reason</details>` (`:1148`) as the full breakdown.

- **CSS (after `:793`):**

```css
.job-head-aside { display:flex; flex-direction:column; align-items:flex-end; gap:6px; flex:0 0 auto; }
.score-blurb {
  font-size:.78rem; line-height:1.25; max-width:240px; text-align:right;
  overflow:hidden; text-overflow:ellipsis; white-space:nowrap;
  color:var(--ink-2); padding:2px 8px; border-radius:6px;
}
.score-blurb--high { color:var(--score-high-on); background:var(--score-high-soft); }
.score-blurb--mid  { color:var(--score-mid-on);  background:var(--score-mid-soft); }
.score-blurb--low  { color:var(--score-low-on);  background:var(--score-low-soft); }
.score-blurb--capped { color:var(--score-low-on); background:var(--score-low-soft); font-weight:600; }
```

Responsive: adjust `max-width` in the existing `@media` blocks (`:952-965`).

## Step 5 — Verify

- `go test ./internal/score/...` — `score_test.go` `TestFitReason_CappedJob` (`:387`) asserts the old `cap: deal_breaker_tech (30)` string and must be updated to the new sentence format. Add `CapDetail` assertions to the cap tests (`TestCompute_DealBreaker*`, `TestCompute_Salary*`, `TestCompute_NonRemote*`) to lock the specifics.
- `go vet ./... && just build`.
- `linkedin-jobs score --all`, then `linkedin-jobs serve` — eyeball: high-fit cards show a green caption, capped cards show a red caption naming the violated constraint, unscored cards show just `—`.

## Out of scope

- **Dedup reason** — currently silent (`pipeline.go:94` just increments a counter). "Why it's not scored" is mostly the capped case (covered); deduped jobs have no per-job reason and need a new status/note path. Deferred to a later pass.
- LLM-polished reasons (declined — tokens + non-determinism).
- Structured per-dimension JSON storage (declined — improve the string only).
