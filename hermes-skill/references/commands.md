# Command Reference

Full reference for every `linkedin-jobs` CLI command. Global flags apply to all commands.

## Global Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--db` | string | `~/.linkedin-jobs/linkedin_jobs.db` or `$LJ_DB_PATH` | Override SQLite DB path |
| `--json` | bool | false | Emit machine-readable JSON (per-command; see --json column in SKILL.md Command Map) |

## Fetch Commands

### recommended

Pull your personalized LinkedIn "Recommended for you" job feed. **Requires auth session.**

```bash
linkedin-jobs recommended [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--top` | int | 50 | Max number of recommended jobs to fetch |
| `--min-salary` | string | "" | Only keep jobs paying at or above this (e.g. `200k`) |
| `--salary-currency` | string | "" | Currency for `--min-salary` (ISO 4217, e.g. `CAD`); enables FX-aware filtering. Requires `--min-salary`. |
| `--remote` | bool | false | Only keep remote-friendly jobs |
| `--hybrid` | bool | false | Only keep hybrid-friendly jobs (OR with `--remote`/`--onsite`) |
| `--onsite` | bool | false | Only keep on-site jobs (OR with `--remote`/`--hybrid`) |
| `--no-score` | bool | false | Skip LLM enrichment + fit-scoring |
| `--force-overwrite` | bool | false | Re-parse and re-score jobs already in the DB (bypass dedup) |

`--json`: yes. Progress to stderr: fetch count, detail fetch `N/total`, gate pass/drop, scoring summary.

### search

Search LinkedIn's public job board anonymously. **No session required.**

```bash
linkedin-jobs search <keywords> [location] [flags]
```

Args: `keywords` (required), `location` (optional, e.g. `"Toronto"` or `"Remote, US"`).

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--top` | int | 25 | Cap on number of jobs to fetch + process end-to-end |
| `--min-salary` | string | "" | Only keep jobs paying at or above this |
| `--salary-currency` | string | "" | Currency for `--min-salary` (ISO 4217); requires `--min-salary` |
| `--remote` | bool | false | Only remote-friendly jobs |
| `--hybrid` | bool | false | Only hybrid-friendly jobs |
| `--onsite` | bool | false | Only on-site jobs |
| `--no-score` | bool | false | Skip LLM enrichment + fit-scoring |
| `--force-overwrite` | bool | false | Re-parse and re-score jobs already in the DB |

`--json`: yes.

### url

Score every job on a LinkedIn search/collection URL. **Requires auth session** (falls back to limited anonymous endpoint without one).

```bash
linkedin-jobs url <linkedin-search-url> [flags]
```

For URLs with `keywords=`, replays the URL's filters against the authenticated Voyager `jobCards` API so `--top` pulls every page. For URLs with only job IDs (`originToLandingJobPostings`, `currentJobId`), uses those IDs directly.

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--top` | int | 0 | Cap on jobs to process (0 = all jobs from the URL) |
| `--min-salary` | string | "" | Only keep jobs paying at or above this |
| `--salary-currency` | string | "" | Currency for `--min-salary`; requires `--min-salary` |
| `--remote` | bool | false | Only remote-friendly jobs |
| `--hybrid` | bool | false | Only hybrid-friendly jobs |
| `--onsite` | bool | false | Only on-site jobs |
| `--no-score` | bool | false | Skip LLM enrichment + fit-scoring |
| `--force-overwrite` | bool | false | Re-parse and re-score jobs already in the DB |

`--json`: yes.

### watch

Run a search and show only NEW jobs not seen before. **Anonymous.** Compares job IDs against the SQLite store — "new" means IDs not already in the DB.

```bash
linkedin-jobs watch <keywords> <location> [flags]
```

Args: `keywords` and `location` (both required).

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--top` | int | 25 | Cap on jobs to pull from LinkedIn each run |
| `--min-salary` | string | "" | Only keep jobs paying at or above this |
| `--salary-currency` | string | "" | Currency for `--min-salary`; requires `--min-salary` |
| `--remote` | bool | false | Only remote-friendly jobs |
| `--hybrid` | bool | false | Only hybrid-friendly jobs |
| `--onsite` | bool | false | Only on-site jobs |
| `--force-overwrite` | bool | false | Re-process existing jobs (bypass new-only pre-filter and dedup) |

`--json`: yes. Note: `watch` has no `--no-score` flag — scoring runs if an LLM provider is configured.

### job

Fetch + fit-score a single LinkedIn job by its numeric ID. **Works with or without auth.**

```bash
linkedin-jobs job <job-id>
```

No command-specific flags. `--json`: yes.

## Store/Query Commands

### list

List saved jobs from the local database with optional filters.

```bash
linkedin-jobs list [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--min-salary` | string | "" | Filter by minimum salary |
| `--salary-currency` | string | "" | Currency for `--min-salary`; requires `--min-salary` |
| `--company` | string | "" | Filter by company name (substring) |
| `--title` | string | "" | Filter by title (substring) |
| `--location` | string | "" | Filter by location (substring) |
| `--remote` | bool | false | Only remote-friendly jobs |
| `--hybrid` | bool | false | Only hybrid-friendly jobs |
| `--onsite` | bool | false | Only on-site jobs |
| `--status` | string | "" | Filter by status (new/viewed/saved/applied/rejected/filtered) |
| `--source` | string | "" | Filter by source (recommended/search) |
| `--limit` | int | 50 | Max results |
| `--min-score` | int | 0 | Only jobs with fit_score >= N |
| `--sort-score` | bool | false | Sort by fit_score descending (default: sort by salary) |

`--json`: yes.

### show

Show full details for a single saved job.

```bash
linkedin-jobs show <job-id>
```

No command-specific flags. `--json`: yes.

### query

Offline full-text search over stored jobs (FTS5). Searches across title, company, and description.

```bash
linkedin-jobs query <text> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--limit` | int | 50 | Max results |
| `--exclude` | stringSlice | nil | Exclude terms (repeatable: `--exclude amazon --exclude google`) |

`--json`: yes.

### stats

Aggregate stats over the local job database.

```bash
linkedin-jobs stats [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--top` | int | 50 | Number of top companies to show |

`--json`: yes.

### count

Print the number of jobs saved in the local database.

```bash
linkedin-jobs count [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--filtered` | bool | false | Count only jobs tagged status=filtered |

`--json`: yes (emits `{"count": N}`).

### export

Export saved jobs to JSON, CSV, or Markdown. Uses `--format` (not `--json`).

```bash
linkedin-jobs export [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--format` / `-f` | string | "json" | Output format: `json`, `csv`, or `markdown` |
| `--out` / `-o` | string | "" | Output file (default: stdout) |

`--json`: N/A — use `--format json` instead.

### tag

Set a job's pipeline status. Valid statuses: `new`, `viewed`, `saved`, `applied`, `rejected`, `filtered`.

```bash
linkedin-jobs tag <job-id> <status> [--note "text"]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--note` | string | "" | Attach a note to the job |

`--json`: yes (emits the updated job). **Approval gate:** confirm before `tag <id> applied` (real-world commitment).

### purge

Delete jobs from the local database. **Approval gate: confirm + offer `export`/`count` first.**

```bash
linkedin-jobs purge [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--yes` | bool | false | Skip the interactive confirmation prompt (required for non-interactive/agent use) |
| `--filtered` | bool | false | Delete only jobs tagged status=filtered (less destructive) |

`--json`: no (always text). Without `--yes`, the command prompts on stdin and blocks in non-interactive contexts.

## Enrich/Score Commands

### enrich

Enrich + fit-score one job, or all unenriched jobs. Requires an LLM provider.

```bash
linkedin-jobs enrich [<job-id>] [--all]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--all` | bool | false | Enrich all jobs that lack enrichment |

`--json`: yes in single-job mode only (`enrich <job-id> --json`). `enrich --all` produces stderr progress only (no stdout) — follow up with `list` or `show` to present results. **Approval gate:** confirm before `--all` (unbounded LLM cost).

### rescore-all

Re-enrich + re-score every stored job via the LLM, and re-judge filter status. Always calls LLM (one call per job), ignores dedup. Use after editing preferences/weights.

```bash
linkedin-jobs rescore-all
```

No flags. `--json`: no (stderr progress only). **Approval gate:** confirm before running (costs tokens proportional to DB size). Explicit triage statuses (saved/applied/rejected) are preserved — only the `filtered` tag is re-judged.

## HR Outreach

### hr

Research who to reach out to about a job. Returns the best person to contact, a ranked shortlist, reasoning, a tailored outreach hook, and company-scoped LinkedIn people-search URLs. **Anonymous — no session required.**

```bash
linkedin-jobs hr <linkedin-job-url> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--no-llm` | bool | false | Skip the LLM; use the deterministic heuristic only (founding roles → founders/CTO; manager roles → VP/Director; otherwise recruiter first) |

`--json`: yes.

## Profile Commands

### profile show

Show the active preference knobs.

```bash
linkedin-jobs profile show
```

`--json`: yes.

## Config Commands

### config show

Show the resolved LLM provider (key redacted) and settings.

```bash
linkedin-jobs config show
```

`--json`: no (always text).

### config path

Print the settings/db file locations.

```bash
linkedin-jobs config path
```

`--json`: no (always text).

### doctor

Diagnose config: LLM provider, settings.yaml completeness, env vars.

```bash
linkedin-jobs doctor
```

`--json`: no (always text).

### auth status

Show whether a usable LinkedIn session is available (checks `li_at` + `JSESSIONID` cookies).

```bash
linkedin-jobs auth status
```

`--json`: no (always text).

### version

Print the linkedin-jobs version.

```bash
linkedin-jobs version
```

`--json`: no (always text).

## Web UI

### serve

Serve a read-only web UI to browse all stored jobs on localhost.

```bash
linkedin-jobs serve [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--addr` | string | "127.0.0.1" | Bind address (**never override to 0.0.0.0** — no auth) |
| `--port` | int | 8080 | Port to serve on |

`--json`: no. Human-facing — the agent should not scrape the HTML. The web UI includes full-text search, filters, and editable job status + delete (POST endpoints with CSRF protection).

## Internal

### header-tags

Fetch the workplace-type header tag from LinkedIn's Voyager API.

```bash
linkedin-jobs header-tags <job-id>
```

`--json`: yes. Primarily a diagnostic/debugging command.
