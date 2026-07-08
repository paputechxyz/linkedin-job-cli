# linkedin-jobs

Pull your personalized **LinkedIn "Recommended for you"** feed from your own
browser session, search the public job board, filter by salary, enrich and
fit-score with an LLM, and persist everything to a local SQLite store with
offline full-text search.

## Highlights

- **`recommended`** — your personalized job feed, authenticated via your own
  LinkedIn browser session (your cookie; no password stored).
- **`search`** — anonymous public job-board search (no login required).
- **`url`** — paste any LinkedIn search/collection URL (job-alert email link,
  saved search, browser URL) and score every job on that page; authenticated via
  your session (like `recommended`) so `--top` pulls the full result set.
- **`hr`** — paste a job URL and get the best person to reach out to (ranked
  contacts + reasoning + a tailored hook + company-scoped LinkedIn search links).
- **Salary parsing** — handles `CA$173,000.00 - CA$220,000.00`, `$212,500/yr`,
  `$120k`, USD/CAD.
- **LLM enrichment + fit scoring** — OpenAI-compatible API extracts structured
  facts and scores each job 0-100 against your resume + preferences.
- **SQLite + FTS5** — every fetched job is stored and instantly searchable
  offline across title, company, and description.
- **Agent-native** — every read command supports `--json`.
- **Pipeline tracking** — tag jobs `saved` / `applied` / `rejected` with notes.
- **Fit scoring** — each job is enriched and scored 0-100 against your pasted
  resume + preferences in a single LLM call, with a fit reason for strong matches.
- **Token-frugal** — duplicates (content-hash) and clear preference mismatches
  are detected with zero LLM calls; only genuine new candidates are scored.
- **Profile** — your resume is an editable markdown file (`RESUME.md`) and
  preference knobs live under the `profile:` section of `settings.yaml`; those
  knobs also drive a deterministic hard filter that auto-tags non-matches.
- **Export** — JSON / CSV / Markdown.

## Install

Requires Go 1.26+. Build the binary locally so you control which version is on
your `PATH` (and can keep multiple builds around):

```bash
go build -o linkedin-jobs .
```

## Auth (for `recommended` and `url`)

`recommended` and `url` use your LinkedIn session. `search` works without it.

Export your LinkedIn cookies (e.g. a browser cookie-exporter extension, or
DevTools → Network → the request `Cookie` header) and point the CLI at them:

```bash
export LJ_COOKIES_FILE=/path/to/cookies.txt   # raw "name=val; name=val" header
# or:  export LJ_COOKIE="li_at=...; JSESSIONID=ajax:..."
```

The `csrf-token` is derived automatically from your `JSESSIONID` cookie.

Verify your session is usable (checks `li_at` + `JSESSIONID` are present):

```bash
linkedin-jobs auth status
```

## Usage

### Recommended (your personalized feed — primary command)

```bash
linkedin-jobs recommended                       # pull your feed
linkedin-jobs recommended --min-salary 200k     # only ≥ $200k
linkedin-jobs recommended --remote              # only remote-friendly
linkedin-jobs recommended --json                # machine-readable output
```

### Search (anonymous)

```bash
linkedin-jobs search "Staff Engineer" Toronto --min-salary 200k
linkedin-jobs search "Senior Developer" "Remote, US" --top 3   # cap at 3 jobs
```

### URL (authenticated)

Paste a LinkedIn search/collection URL — typically a job-alert email link or a
URL copied from the browser. For URLs with a `keywords=` param, the URL's
filters are replayed against the authenticated Voyager `jobCards` API — the
same XHR the browser fires when you scroll `/jobs/search/` — so `--top` pulls
every page (the anonymous `seeMoreJobPostings` endpoint caps early, e.g. 10 of
32). For URLs that only carry explicit job IDs
(`originToLandingJobPostings` from a job-alert email with no keywords, or
`currentJobId`), those IDs are used directly. All the usual gates and scoring
flags apply.

```bash
linkedin-jobs url "https://www.linkedin.com/jobs/search/?currentJobId=4415889466&originToLandingJobPostings=4415889466%2C4434154740&keywords=Staff%20Engineer"
linkedin-jobs url "https://www.linkedin.com/jobs/search/?keywords=Staff%20Engineer&geoId=101788145" --top 50 --min-salary 200k
linkedin-jobs url "https://www.linkedin.com/jobs/collections/recommended/?start=0" --remote
```

Authenticated via your captured browser session (see `auth status`); without a
session it falls back to the limited anonymous endpoint. Salary and full
description are fetched per-job from the public detail page.

### HR (who to reach out to about a job)

Paste any LinkedIn job URL and get back the single best person to contact to get
your application noticed, a ranked shortlist, the reasoning, a tailored outreach
hook, and a ready-to-click LinkedIn people-search URL for each contact scoped to
that company. It fetches the public job page (extracting the company, its
LinkedIn slug, and its numeric company id from the page's "See who you know"
links), pulls the company's public profile, then asks the LLM to pick the
highest-leverage contact — reading the job description for signals like "work
directly with our CTO". With no LLM configured it falls back to a deterministic
heuristic (founding roles → founders/CTO; manager roles → VP/Director;
otherwise recruiter first) and still emits usable search links. Works
anonymously; no session required.

```bash
linkedin-jobs hr "https://www.linkedin.com/jobs/view/4435820129/"
linkedin-jobs hr "https://www.linkedin.com/jobs/search/?currentJobId=4435820129&f_C=105863333"
linkedin-jobs hr "<url>" --json      # machine-readable report
linkedin-jobs hr "<url>" --no-llm    # heuristic only (no LLM key needed)
```

### Work with stored jobs

```bash
linkedin-jobs list --company Google --min-salary 150k
linkedin-jobs list --sort-score --min-score 70      # your best-fit shortlist
linkedin-jobs show 4430749190
linkedin-jobs query "staff backend"            # offline FTS5 search
linkedin-jobs query "engineer" --exclude amazon
linkedin-jobs stats --top 25
linkedin-jobs tag 4430749190 applied --note "referred by Sam"
linkedin-jobs export --format csv -o jobs.csv
linkedin-jobs watch "Staff Engineer" Toronto --top 10  # only jobs new since last run
linkedin-jobs clear
```

### Web UI (local browser)

```bash
linkedin-jobs serve                      # read-only browser on http://127.0.0.1:8080
linkedin-jobs serve --port 9000          # custom port
```

Serves a local page listing every stored job with all fields visible.
Long-text fields (description, summaries, company overview, fit reason, notes)
are collapsed by default and expand on click; the job title links out to its
LinkedIn posting (and marks the job `new → viewed` automatically). Includes
full-text search (FTS5), filters (company, location, salary, score, status,
source, remote), and sort by fit score or salary — all reusing the same store
layer as the CLI. Binds to localhost only.

Editable from the browser: job **status** (`new`/`viewed`/`saved`/`applied`/
`rejected`) and **hard delete**. Every other field stays read-only. Writes are
POST endpoints guarded by a per-session CSRF token.

### Profile + fit scoring

Paste your resume and set preference knobs once; they drive both scoring and
filtering. Your resume is plain markdown **in your project root** (the directory
you run the CLI from; override with `LJ_CONFIG_DIR`) so it travels with this
job-search folder and you can edit it by hand at any time. Preference knobs live
under the `profile:` section of `settings.yaml`:

- `RESUME.md` — your resume (free text); sent to your LLM as candidate context
- `settings.yaml` → `profile:` — structured knobs for the hard filter + rubric:

  ```yaml
  profile:
    work_arrangement: [remote, hybrid]
    min_salary: 200000
    min_salary_currency: CAD
    locations: [Remote, Toronto]
    preferred_tech: [Java, Python, Go, Postgres, AWS]
    avoided_tech: [C#, .NET, Ruby]   # deal-breakers: caps score at scoring.deal_breaker_cap
  ```

```bash
linkedin-jobs profile resume          # paste resume text, end with Ctrl-D
linkedin-jobs profile show            # show resume + active knobs
# edit preference knobs by hand in settings.yaml (profile: section)
```

When you fetch jobs (`recommended` / `search` / `url` / `watch`), a batch-level
pre-score gate runs first (see [below](#pre-score-gate)), then each surviving job
flows through five gates — only the last costs an LLM token:

1. **Persist full description** (always saved, for dedup memory).
2. **Dedup** — a content-hash of company + title + full description. Re-fetched
   or cross-source duplicates skip scoring entirely (zero tokens).
3. **Hard filter** — a deterministic check using only pre-LLM fields (work
   arrangement, salary floor, preferred locations). Clear mismatches are
   score-capped (visible, but ranked low) with a recorded cap reason.
4. **Enrich + score** — one OpenAI-compatible call per genuine new candidate
   fills structured fields (company overview, industry, tech stack, seniority,
   employment type, years, company size/stage, founding role, visa, work
   arrangement) and a 0-100 `fit_score`, plus a `fit_reason` when score ≥ 70.
5. **Display** — sorted/filtered output.

```bash
linkedin-jobs enrich 4430749190       # enrich+score one job
linkedin-jobs enrich --all            # backfill all unenriched jobs
linkedin-jobs score --all             # re-score everything after a profile edit
```

Token-frugality flag: `--no-score` (skip the LLM).

### Pre-score gate

A deterministic, **batch-level** filter triggered by the `--remote`, `--hybrid`,
`--onsite`, and `--min-salary` CLI flags. It runs after the detail fetch but
**before** anything is stored or scored, so it costs **zero LLM tokens**: failing
jobs are dropped in-memory and never reach the DB. Each drop is logged to stderr
with the title, company, and a human-readable reason (e.g.
`dropped "Senior Eng" @ Acme: salary $150,000 below CA$200,000 floor`).

- **No LLM** — purely deterministic; runs before the per-job scoring pipeline.
  Omit all four flags and the gate is a no-op.
- **`--remote` / `--hybrid` / `--onsite`** — a job is kept when its location or
  `remote_type` contains the token; the flags **OR** together, so
  `--remote --hybrid` keeps jobs matching either. On-site matches both
  `remote_type=onsite` and the hyphenated `On-site` form common in raw location
  text.
- **`--min-salary`** — a floor on the job's **max** salary (inclusive: "could
  this job pay ≥ min?"). Shorthand parsing accepts `200k`, `$200,000`, `1.5m`.
  Empty or `0` disables it.
  - **Salary source:** parsed from the **description body first** (the
    authoritative, currency-stated range the employer posted), falling back to
    LinkedIn's page-chrome **salary badge** (a low-confidence "est." band). The
    gate is source-agnostic — either source can clear the floor.
  - **Currency-aware:** a job with no currency signal is treated as `USD`. Pair
    the floor with `--salary-currency CAD` to FX-convert the job's max salary
    into the floor's currency before comparing (live ECB reference rates via the
    Frankfurter API, cached per day, with a small offline fallback table). If a
    rate is unavailable it falls back to a raw numeric compare rather than
    dropping. `--salary-currency` requires `--min-salary`.
  - **No salary data → dropped** when a floor is active (a floor implies "only
    show jobs I know pay enough").

```bash
linkedin-jobs recommended --remote --hybrid --min-salary 200k --salary-currency CAD
```

#### Pre-score gate vs. `settings.yaml` profile knobs

Both are deterministic and LLM-free, but they differ in scope, effect, and
persistence. The pre-score gate **drops** jobs; the profile knobs **score-cap**
them (the "hard filter" in step 3 of the pipeline above).

| Aspect              | Pre-score gate (CLI flags)                              | `settings.yaml` `profile:` knobs                          |
|---------------------|---------------------------------------------------------|-----------------------------------------------------------|
| Trigger             | Per-invocation flags                                    | Persistent; applied every run                             |
| Effect on mismatch  | **Drops** — never stored, never scored                  | **Caps score** — stored + visible, ranked low (cap reason) |
| When it runs        | Batch-level, before persist                             | Per-job, after persist; also feeds the rubric scorer      |
| Job with no salary  | Dropped (when a floor is set)                           | Passes ("unknown is not a mismatch")                      |
| Scope               | Work arrangement, salary floor                          | + locations, preferred/avoided tech                       |
| Disable             | Omit the flag                                           | `filter.auto_filter: false`                              |

Reach for the **pre-score gate** for one-off hard cuts ("only remote jobs paying
≥ CA$200k *this run*"). Reach for **profile knobs** for standing preferences you
want applied every run, where mismatches stay visible but sink to the bottom of
your shortlist.

### LLM configuration

Bring your own key — no provider key ships. The fastest setup is the wizard,
which reuses credentials you already have:

```bash
linkedin-jobs config llm              # connect: opencode / Claude / custom
linkedin-jobs config show             # resolved provider (key redacted) + settings
linkedin-jobs config path
```

Resolution order (first match wins): persisted `config.json` → `LJ_LLM_*` /
`OPENAI_API_KEY` env → `ANTHROPIC_API_KEY` env → opencode's stored credentials.
Explicit env vars win over opencode discovery so you can override the discovered
provider without running the wizard. The opencode preset reuses the provider
configured in opencode (e.g. your GLM Coding Plan key → `glm-5.2`); the Claude
preset targets Anthropic's OpenAI-compatible endpoint.

Or set env vars directly:

```bash
export OPENAI_API_KEY="sk-..."                 # or LJ_LLM_API_KEY
export LJ_LLM_MODEL="gpt-4o-mini"              # optional, default gpt-4o-mini
# For Ollama / vLLM / Azure:
export LJ_LLM_BASE_URL="http://localhost:11434/v1"
```

No key? Scoring is skipped with a clear message; all other commands still work.

### Settings

Optional `settings.yaml` **in your project root** (override the dir with
`LJ_CONFIG_DIR`). This is the same project-local location as `RESUME.md`;
secrets (`config.json`) still live globally in `~/.linkedin-jobs/`:

```yaml
stats:
  top_companies_limit: 50        # default 50 (was hardcoded 10); also `stats --top N`
filter:
  auto_filter: true              # set false to disable the hard filter
scoring:
  reason_threshold: 70           # fit_reason included at/above this score
enrich:
  auto_enrich_on_save: false     # tag saved does not auto-score by default
profile:                         # preference knobs for the hard filter + rubric
  work_arrangement: [remote, hybrid]
  min_salary: 200000
  min_salary_currency: CAD
  locations: [Remote, Toronto]
  preferred_tech: [Java, Python, Go, Postgres, AWS]
  avoided_tech: [C#, .NET, Ruby]   # caps score at scoring.deal_breaker_cap
```

When scoring runs, the CLI prints which profile context it loaded (resume from
`RESUME.md`, knobs from `settings.yaml`), so you can tell at a glance whether
scores reflect your actual context or ran context-free.

## How recommended works

LinkedIn serves personalized recommendations through an authenticated
[persisted-query GraphQL](https://www.linkedin.com/voyager/api/graphql) call
(queryId `voyagerJobsDashJobCards.e5b6b761ede078dabe8ad857aa42c220`), paginated
25 at a time. The CLI replays that call using your session cookies + a
`csrf-token` derived from your `JSESSIONID` cookie, then decodes the normalized
entity graph (`included[].JobPostingCard`) into job cards. Salary and full
description are fetched per-job from the public detail page (JSON-LD
`JobPosting`) — the same anonymous path `search` uses.

## Configuration & env

| Variable          | Purpose                                            | Default                          |
|-------------------|----------------------------------------------------|----------------------------------|
| `LJ_DB_PATH`      | SQLite database path                               | `~/linkedin-jobs/linkedin_jobs.db` |
| `LJ_CONFIG_DIR`   | directory for `settings.yaml` (incl. `profile:` knobs) and `RESUME.md` (also `config.json` secrets) | project root (CWD); `~/.linkedin-jobs/` for `config.json` when unset |
| `LJ_LLM_DELAY_SECONDS` | seconds to pause between successive LLM scoring calls (avoids 429s) | `2.0` |
| `ANTHROPIC_API_KEY` | Claude provider (auto-detected by config)        | —                                |
| `LJ_COOKIES_FILE` | path to a file with a raw `Cookie:` header          | —                                |
| `LJ_COOKIE`       | raw cookie header string                            | —                                |
| `OPENAI_API_KEY`  | LLM key (or `LJ_LLM_API_KEY`)                      | —                                |
| `LJ_LLM_BASE_URL` | OpenAI-compatible base URL (or `OPENAI_BASE_URL`)  | `https://api.openai.com/v1`      |
| `LJ_LLM_MODEL`    | model name                                          | `gpt-4o-mini`                    |

## Project structure

```
main.go
cmd/                       cobra commands (recommended, search, list, enrich, score, profile, config, hr, …)
internal/
  auth/                    session resolution (cookie env/file) + csrf
  config/                  env-based config + YAML settings
  filter/                  deterministic hard preference filter
  hr/                      outreach research: best contact + ranked list for a job
  linkedin/                HTTP client, anonymous scraper, recommended graphql
  llm/                     OpenAI-compatible provider resolution + enrich/score
  models/                  JobPosting, Profile, Enrichment
  profile/                resume + preferences as editable markdown files
  render/                  table / detail / JSON / stats output
  salary/                  salary parsing + filtering
  store/                   SQLite + FTS5 persistence + content-hash dedup
```

## Notes

- LinkedIn may rate-limit aggressive scraping. Detail fetches use a polite
  delay (default 0.8s, configurable). LLM scoring calls are paced too
  (`LJ_LLM_DELAY_SECONDS`, default 2.0) to avoid provider rate limits (HTTP 429).
- Salary data is only present on jobs where the employer provided it.
- This tool is for personal job-search use. Respect LinkedIn's Terms of Service.
