# Plan: Merge JOB_PREFERENCE.md into settings.yaml

**Date:** 2026-06-30
**Status:** Approved (build mode)
**Scope:** Consolidate the structured preference knobs currently in `JOB_PREFERENCE.md` front-matter into a new `profile:` section of `settings.yaml`, then delete `JOB_PREFERENCE.md`.

## Context

Two separate config systems feed `models.Profile` today:
- `settings.yaml` → `config.Settings` (stats, filter, scoring weights, enrich). Holds `deal_breakers` but **no** preference knobs.
- `JOB_PREFERENCE.md` front-matter → `Profile.Pref*` structured knobs (`work_arrangement`, `min_salary`, `min_salary_currency`, `locations`, `preferred_tech`) — read **directly** by the deterministic scorer + hard filter.
- `JOB_PREFERENCE.md` body + `RESUME.md` → `Profile.PreferencesText` / `ResumeText` → LLM enrich prompt context only.

The score *number* is purely deterministic; the LLM only extracts facts. The prose preference body's scoring impact is marginal (it's context the LLM isn't asked to act on; the structured knobs carry the real signal).

## Decisions (confirmed)
- **Prose body:** drop it. Only structured knobs move to YAML. No `preferences_text` field.
- **RESUME.md:** stays a separate file (large; has a stdin-paste workflow; conceptually candidate background, not preferences).
- **Migration:** manual edit of `settings.yaml` + delete `JOB_PREFERENCE.md`.
- **`profile prefs` subcommand:** delete it. `settings.yaml` is the hand-edited source of truth.

## Changes

### 1. Config schema — `internal/config/settings.go`
Add `ProfileSettings` (`WorkArrangement`, `MinSalary *float64`, `MinSalaryCurrency`, `Locations`, `PreferredTech []string`) and a `Profile ProfileSettings` field on `Settings`. No defaults (zero = unset, mirroring today's loader).

### 2. Profile builder — `internal/profile/profile.go`
Trim to resume-only. New `Load(prefs config.ProfileSettings) (*models.Profile, error)` reads `RESUME.md` for `ResumeText` and maps `prefs` → `Pref*`. Keep `ResumePath`/`SaveResume`/`nowISO`. Remove `PrefsFile`/`PrefsPath`/`prefsFrontmatter`/`SavePrefs`/`splitFrontmatter`/old `Load` body.

### 3. Thread `settings.Profile` through callers
- `cmd/pipeline.go:78`, `cmd/score.go:39`, `cmd/enrich.go:35` → `profile.Load(settings.Profile)`.
- `enrichAndScoreJob` unchanged (receives `*models.Profile`).

### 4. `cmd/profile.go`
- Delete `profilePrefsCmd` + its 4 flags + `prefWork/...` vars + `normalizeArrangement`.
- `profile show` → retarget at `profile.Load(settings.Profile)`; print knobs + resume; source label = `settings.yaml (profile:)`.
- `profile clear` → removes `RESUME.md` only; notes knobs live in `settings.yaml` (no comment-destroying YAML rewrite).
- `profile resume` unchanged. Rewrite parent help text.

### 5. Loose ends
- `cmd/config.go:115` — `preferences:` points at `settings.yaml` `profile:` section.
- `internal/llm/scorer.go:17` — trim stale *"plus a fit score against the candidate's resume and preferences"* from system prompt.
- `models.Profile.PreferencesText` stays (always empty; keeps `llm/scorer.go:88` + JSON working).

### 6. Data migration (manual)
Add to `settings.yaml`:
```yaml
profile:
  work_arrangement: remote
  min_salary: 200000
  min_salary_currency: CAD
  locations: Remote,Toronto
  preferred_tech: [Java, Python, Elixir, Go, FastAPI, Flask, Django, NestJS, ReactJS, NextJS, Docker, Kubernetes, Postgres, BigQuery, Snowflake, AWS, GCP]
```
Delete `JOB_PREFERENCE.md`.

### 7. Tests & docs
- `internal/profile/profile_test.go` — drop `SavePrefs`/front-matter tests; add `TestLoad_FromSettings`.
- `cmd/profile_test.go` — update for deleted `prefs` cmd + retargeted `show`/`clear`.
- `README.md` + `internal/profile` package doc — replace `JOB_PREFERENCE.md` refs with `settings.yaml` `profile:` section.

## Verify
`go test ./...`, `go vet ./...`, `just build`. `profile show` reads from YAML; `serve`/`score --all` confirm caps fire identically (knob values unchanged → scores unchanged).

## Behavior change
`profile clear` no longer wipes preference knobs (they're hand-edited in YAML now) — only removes `RESUME.md`.
