# linkedin-jobs

Pull your personalized **LinkedIn "Recommended for you"** feed from your own
browser session, search the public job board, filter by salary, summarize with
an LLM, and persist everything to a local SQLite store with offline full-text
search.

This is a Go rewrite + extension of an earlier Python tool (`linkedin-job-cli`).

## Highlights

- **`recommended`** — your personalized job feed, authenticated via your own
  LinkedIn browser session (piggybacks on your login; no password stored).
- **`search`** — anonymous public job-board search (no login required).
- **Salary parsing** — handles `CA$173,000.00 - CA$220,000.00`, `$212,500/yr`,
  `$120k`, USD/CAD.
- **LLM summaries** — OpenAI-compatible API, with a rule-based extractive
  fallback when no key is set.
- **SQLite + FTS5** — every fetched job is stored and instantly searchable
  offline across title, company, and description.
- **Agent-native** — every read command supports `--json`.
- **Pipeline tracking** — tag jobs `saved` / `applied` / `rejected` with notes.
- **Export** — JSON / CSV / Markdown.

## Install

Requires Go 1.26+.

```bash
go build -o linkedin-jobs .
# or, once published:
go install .
```

## Auth (for `recommended` only)

`recommended` needs your LinkedIn session. `search` works without it.

**Option A — press-auth (recommended).** Capture your session once via a
controlled Chrome window (not your daily profile). The session is stored
encrypted in the macOS Keychain.

```bash
go install github.com/mvanhorn/cli-printing-press/v4/cmd/press-auth@latest
linkedin-jobs auth login     # sign in once in the window that opens
linkedin-jobs auth status    # verify
```

**Option B — manual cookie header.** If you can't use press-auth, export your
LinkedIn cookies (e.g. a browser cookie-exporter extension, or DevTools →
Network → the request `Cookie` header) and point the CLI at them:

```bash
export LJ_COOKIES_FILE=/path/to/cookies.txt   # raw "name=val; name=val" header
# or:  export LJ_COOKIE="li_at=...; JSESSIONID=ajax:..."
```

The `csrf-token` is derived automatically from your `JSESSIONID` cookie.

## Usage

### Recommended (your personalized feed — primary command)

```bash
linkedin-jobs recommended                       # pull your feed
linkedin-jobs recommended --min-salary 200k     # only ≥ $200k
linkedin-jobs recommended --remote              # only remote-friendly
linkedin-jobs recommended --exclude-company Tata --exclude-company Wipro
linkedin-jobs recommended --json                # machine-readable output
```

### Search (anonymous)

```bash
linkedin-jobs search "Staff Engineer" Toronto --min-salary 200k
linkedin-jobs search "Senior Developer" "Remote, US" --pages 2
```

### Work with stored jobs

```bash
linkedin-jobs list --company Google --min-salary 150k
linkedin-jobs show 4430749190
linkedin-jobs query "staff backend"            # offline FTS5 search
linkedin-jobs query "engineer" --exclude amazon
linkedin-jobs summarize                         # backfill LLM summaries
linkedin-jobs stats
linkedin-jobs tag 4430749190 applied --note "referred by Sam"
linkedin-jobs export --format csv -o jobs.csv
linkedin-jobs watch "Staff Engineer" Toronto   # show only jobs new since last run
linkedin-jobs clear
```

## LLM configuration

The summarizer uses any OpenAI-compatible API:

```bash
export OPENAI_API_KEY="sk-..."                 # or LJ_LLM_API_KEY
export LJ_LLM_MODEL="gpt-4o-mini"              # optional, default gpt-4o-mini
# For Ollama / vLLM / Azure:
export LJ_LLM_BASE_URL="http://localhost:11434/v1"
```

No key? Summaries fall back to a rule-based extractive summary.

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
| `LJ_DB_PATH`      | SQLite database path                               | `./linkedin_jobs.db`             |
| `LJ_COOKIES_FILE` | path to a file with a raw `Cookie:` header          | —                                |
| `LJ_COOKIE`       | raw cookie header string                            | —                                |
| `OPENAI_API_KEY`  | LLM key (or `LJ_LLM_API_KEY`)                      | —                                |
| `LJ_LLM_BASE_URL` | OpenAI-compatible base URL (or `OPENAI_BASE_URL`)  | `https://api.openai.com/v1`      |
| `LJ_LLM_MODEL`    | model name                                          | `gpt-4o-mini`                    |

## Project structure

```
main.go
cmd/                       cobra commands (recommended, search, list, show, query, …)
internal/
  auth/                    session resolution (press-auth → env → file) + csrf
  config/                  env-based configuration
  linkedin/                HTTP client, anonymous scraper, recommended graphql
  llm/                     OpenAI-compatible summarizer + extractive fallback
  models/                  JobPosting
  render/                  table / detail / JSON / stats output
  salary/                  salary parsing + filtering
  store/                   SQLite + FTS5 persistence
```

## Notes

- LinkedIn may rate-limit aggressive scraping. Detail fetches use a polite
  delay (default 0.8s, configurable).
- Salary data is only present on jobs where the employer provided it.
- This tool is for personal job-search use. Respect LinkedIn's Terms of Service.
