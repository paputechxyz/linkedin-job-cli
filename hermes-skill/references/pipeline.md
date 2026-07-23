# Scoring Pipeline

How the linkedin-jobs CLI processes jobs from fetch to display. Understanding this pipeline is essential for interpreting scores and guiding rubric/profile configuration.

## Pipeline Overview

When you run `recommended`, `search`, `url`, or `job`, jobs flow through these stages:

```
Fetch job cards
    ↓
Detail fetch (salary + full description)  ← stderr: "N/total" progress
    ↓
Persist all fetched jobs to SQLite  ← dedup memory (content-hash)
    ↓
Dedup check  ← skips scoring for already-enriched jobs (zero tokens)
    ↓
LLM enrich + score (one call per genuine new candidate)
    ↓
Display (full fetched set; ranking via fit_score)
```

Only the enrich stage costs LLM tokens. Dedup is deterministic and free. Every new candidate is enriched and scored — nothing is dropped pre-LLM. Preferences (work arrangement, salary floor) live under `profile:` in `settings.yaml` and feed the soft system rubrics, which lower the score on mismatches rather than dropping jobs. Use `list --remote --min-salary ...` or the `serve` UI filters to exclude at view time.

## Dedup

A content-hash of company + title + full description + listed-at. If a job with the same hash is already enriched in the DB, scoring is skipped entirely (zero tokens). Use `--force-overwrite` to bypass dedup and re-parse + re-score existing jobs.

## LLM Enrichment + Scoring

One LLM call per genuine new candidate (one that passed dedup). The LLM:

1. **Extracts structured facts:** company overview, industry, tech stack, seniority, employment type, years of experience, company size/stage, founding role, work arrangement.
2. **Rates each dynamic rubric 1-5** for the job (e.g. `preferred_tech: 5`, `avoided_tech: 1`, `free_snacks: 3`). System rubrics are NOT rated by the LLM — they are computed deterministically in Go.
3. **Does NOT pick the score** — Go derives the 0-100 fit score from the rubric ratings + weights.

**Data sent to the LLM provider:** job description (full text) and the active rubric set. Users should verify their provider's data retention policy. `LJ_LLM_BASE_URL` can point to a self-hosted endpoint (Ollama, vLLM) for data residency control.

## Rubric Scoring

Scores come from a **rubric set** in `settings.yaml` under `scoring.rubrics`. Generate it with `linkedin-jobs setup` (a preferences paragraph), refine with `amend`, or start over with `reset`.

Each rubric has a **weight** (1-10, default 5) and a **rating** (1-5) per job:

- **System rubrics** (computed in Go): `salary` (vs `profile.min_salary` floor), `work_arrangement` (matches `profile.work_arrangement`).
- **Dynamic rubrics** (rated 1-5 by the LLM): everything else, generated from your paragraph — e.g. `preferred_tech`, `avoided_tech`, `location`, `free_snacks`, `ai_intensity`. List-type criteria carry `items`. Location is dynamic so jurisdiction and proximity nuance (e.g. "remote anywhere", "within 30km of Mississauga") lives in the rubric description and is LLM-judged per job.

The final score is a weight-normalized average mapped to 0-100:

```
score = ( Σ weightᵢ · ratingᵢ / Σ weightᵢ ) / 5 × 100
```

So rating 5 → 100, 4 → 80, 3 → 60, 2 → 40, 1 → 20. A job rated 4/5 across the board scores ~80 whether there are 3 rubrics or 15 — the rubric count does not distort the scale. **There are no hard caps or baselines**; a job matching an avoided tech simply gets a low rating on that rubric.

A `fit_reason` showing the per-rubric breakdown is always stored (e.g. `salary 4/5 (w5), preferred_tech 5/5 (w5), avoided_tech 1/5 (w5) | total 73`).

## Token-Frugality Features

- **Dedup:** re-fetched or cross-source duplicates skip scoring (zero tokens).
- **`LJ_LLM_DELAY_SECONDS`:** pauses between successive LLM calls (default 2.0s) to avoid provider rate limits (HTTP 429).

## Re-scoring

- **`rescore-all`:** re-enrich + re-score EVERY stored job (ignores dedup) against the current rubric set. Always calls LLM. Explicit triage statuses (saved/applied/rejected) are preserved.

After generating/amending rubrics or editing weights in `settings.yaml`, run `rescore-all` to re-score with the updated rubric set.
