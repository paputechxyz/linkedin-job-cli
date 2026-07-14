# Scoring Pipeline

How the linkedin-jobs CLI processes jobs from fetch to display. Understanding this pipeline is essential for interpreting scores, explaining why jobs were dropped or capped, and guiding profile/scoring configuration.

## Pipeline Overview

When you run `recommended`, `search`, `url`, `watch`, or `job`, jobs flow through these stages:

```
Fetch job cards
    ↓
Detail fetch (salary + full description)  ← stderr: "N/total" progress
    ↓
Pre-score gate (--remote/--hybrid/--onsite/--min-salary)  ← drops pre-store, zero LLM tokens
    ↓
Persist all surviving jobs to SQLite  ← dedup memory (content-hash)
    ↓
Dedup check  ← skips scoring for already-enriched jobs (zero tokens)
    ↓
Hard filter (profile knobs from settings.yaml)  ← caps score, no LLM call
    ↓
LLM enrich + score (one call per genuine new candidate)
    ↓
Display (sorted/filtered output)
```

Only the last stage costs LLM tokens. Dedup and the hard filter are deterministic and free.

## Pre-score Gate (CLI Flags)

Triggered by `--remote`, `--hybrid`, `--onsite`, and `--min-salary` CLI flags. Runs **after** the detail fetch but **before** anything is stored or scored.

- **Effect: drops** jobs that fail the gate. Dropped jobs are never stored, never scored, never visible. Each drop is logged to stderr with the reason.
- **`--remote` / `--hybrid` / `--onsite`**: OR together. A job is kept when its location or `remote_type` contains the token. `--onsite` matches both `onsite` and the hyphenated `On-site` form.
- **`--min-salary`**: a floor on the job's **max** salary (inclusive). Shorthand: `200k`, `$200,000`, `1.5m`. Jobs with no salary data are **dropped** when a floor is active.
- **`--salary-currency`**: pairs with `--min-salary` for FX-aware filtering (live ECB reference rates via Frankfurter API, cached per day). Requires `--min-salary`.
- **No LLM** — purely deterministic. Omit all four flags and the gate is a no-op.

## Profile Knobs (settings.yaml)

Persistent preference knobs under the `profile:` section of `settings.yaml`. Applied **every run** (unlike the per-invocation pre-score gate).

- **Effect: caps score** — mismatches are stored and visible but ranked low, with a recorded cap reason. They are NOT dropped.
- **Scope:** work arrangement, min salary, locations, preferred tech, avoided tech.
- A job with no salary **passes** the profile knob filter ("unknown is not a mismatch") — unlike the pre-score gate which drops it.
- Disable: set `filter.auto_filter: false` in `settings.yaml`.

### Pre-score Gate vs Profile Knobs

| Aspect | Pre-score gate (CLI flags) | Profile knobs (settings.yaml) |
|--------|---------------------------|-------------------------------|
| Trigger | Per-invocation flags | Persistent; applied every run |
| Effect on mismatch | **Drops** — never stored, never scored | **Caps score** — stored, visible, ranked low |
| When it runs | Batch-level, before persist | Per-job, after persist; also feeds rubric |
| Job with no salary | Dropped (when floor set) | Passes ("unknown is not a mismatch") |
| Scope | Work arrangement, salary floor | + locations, preferred/avoided tech |
| Disable | Omit the flag | `filter.auto_filter: false` |

## Dedup

A content-hash of company + title + full description + listed-at. If a job with the same hash is already enriched in the DB, scoring is skipped entirely (zero tokens). Use `--force-overwrite` to bypass dedup and re-parse + re-score existing jobs.

## Hard Filter (Deterministic Score Cap)

When `filter.auto_filter: true` (default), jobs that fail the hard filter (using profile knobs) are still stored but their score is capped at `scoring.deal_breaker_cap` (default 30). No LLM call is made for these jobs — the cap is deterministic. The cap reason is recorded and visible.

## LLM Enrichment + Scoring

One LLM call per genuine new candidate (one that passed dedup + hard filter). The LLM:

1. **Extracts structured facts:** company overview, industry, tech stack, seniority, employment type, years of experience, company size/stage, founding role, visa sponsorship, work arrangement, bonus/equity/retirement match, AI intensity.
2. **Does NOT pick a score** — the LLM only extracts facts. The deterministic rubric (`score.Compute`) derives the 0-100 fit score from the enriched facts + profile.

**Data sent to the LLM provider:** job description (full text) and preference knobs. Users should verify their provider's data retention policy. `LJ_LLM_BASE_URL` can point to a self-hosted endpoint (Ollama, vLLM) for data residency control.

### Scoring Rubric

The rubric (`score.Compute`) uses weights from `settings.yaml` under `scoring.weights`:

| Weight | What it scores |
|--------|---------------|
| `salary` (default 6) | Tiered by how far above the floor (0 / at-floor / +10% / +30%) |
| `tech_overlap` (default 4) | Count of `preferred_tech` items found in the enriched `tech_stack` |
| `startup` (default 5) | Company stage seed/early + size 1-50 |
| `ai_intensity` (default 3) | Core=full, mentioned=partial, none=0 |
| `compensation_extras` (default 3) | Bonus + equity + retirement match (1pt each, +1 all three) |
| `remote_tiebreak` (default 6) | Full-remote=full, hybrid=partial |

Starting score after passing the hard filter: `scoring.baseline` (default 60). Deal-breaker tech (from `scoring.deal_breakers` or `profile.avoided_tech`) caps the score at `scoring.deal_breaker_cap` (default 30).

A `fit_reason` is included when the score is at or above `scoring.reason_threshold` (default 70).

## Token-Frugality Features

- **Dedup:** re-fetched or cross-source duplicates skip scoring (zero tokens).
- **Hard filter:** clear preference mismatches are score-capped without an LLM call.
- **`--no-score`:** skip the LLM entirely for a fetch run.
- **Pre-score gate:** drops jobs before they reach the DB or LLM.
- **`LJ_LLM_DELAY_SECONDS`:** pauses between successive LLM calls (default 2.0s) to avoid provider rate limits (HTTP 429).

## Re-scoring

- **`enrich <job-id>`:** enrich + score a single job. Outputs JSON with `--json`.
- **`enrich --all`:** enrich all unenriched jobs. No stdout (stderr progress only). Follow up with `list` or `show`.
- **`rescore-all`:** re-enrich + re-score EVERY stored job (ignores dedup). Always calls LLM. Re-judges the `filtered` tag based on current profile. Preserves explicit triage statuses (saved/applied/rejected).

After editing the `settings.yaml` profile knobs, run `rescore-all` to re-score with updated context.
