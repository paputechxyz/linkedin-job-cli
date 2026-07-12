# Auth, Config, and Environment

## Auth Model

The CLI has two auth modes:

### Authenticated (LinkedIn session)

Required for `recommended` and `url`. Uses your captured browser session cookies.

Set cookies via one of:
- `LJ_COOKIES_FILE=/path/to/cookies.txt` — file containing a raw `Cookie:` header (`name=val; name=val`)
- `LJ_COOKIE="li_at=...; JSESSIONID=ajax:..."` — raw cookie header string

The CSRF token is derived automatically from the `JSESSIONID` cookie. The CLI checks for `li_at` + `JSESSIONID` cookies.

Check session state:
```bash
linkedin-jobs auth status    # reports whether a usable session is available
```

**Security:** LinkedIn session cookies enable full account takeover. Store `LJ_COOKIES_FILE` with `0600` permissions in a user-only directory. Treat it with the same care as SSH private keys. The agent must never `cat`, `echo`, or transmit the file contents — use `auth status` for session checks only.

### Anonymous (no session)

`search`, `hr`, `watch`, and `job` work without a session. No login needed.

## LLM Configuration

Scoring is optional — all read commands work without an LLM key. When scoring runs and no provider is configured, it is skipped with a clear message.

### Provider Resolution (first match wins)

1. `LJ_LLM_API_KEY` or `OPENAI_API_KEY` env var
2. `ANTHROPIC_API_KEY` env var (targets Anthropic's OpenAI-compatible endpoint)
3. opencode's stored credentials (reuses the provider configured in opencode, e.g. GLM Coding Plan key → `glm-5.2`)

Explicit env vars win over opencode discovery, so you can override the discovered provider.

### LLM Settings

| Variable | Purpose | Default |
|----------|---------|---------|
| `LJ_LLM_API_KEY` / `OPENAI_API_KEY` | LLM API key | — |
| `LJ_LLM_MODEL` | Model name | `gpt-4o-mini` |
| `LJ_LLM_BASE_URL` / `OPENAI_BASE_URL` | OpenAI-compatible base URL (Ollama, vLLM, Azure) | `https://api.openai.com/v1` |
| `ANTHROPIC_API_KEY` | Claude provider (auto-detected) | — |
| `LJ_LLM_DELAY_SECONDS` | Pause between successive LLM calls (avoids 429s) | `2.0` |

**Data residency:** `LJ_LLM_BASE_URL` can point to a self-hosted endpoint (Ollama at `http://localhost:11434/v1`, vLLM, etc.) for users who need data residency control. The scoring pipeline transmits job descriptions, the user's resume (truncated), and preference knobs to the configured provider.

### Config Commands

```bash
linkedin-jobs config show     # resolved provider (key redacted) + settings
linkedin-jobs config path     # settings/resume/db file locations
linkedin-jobs doctor          # diagnose provider + settings completeness
```

## Settings (settings.yaml)

Optional `settings.yaml` in your **project root** when one is already present there, otherwise in `~/.linkedin-jobs/`. Same location as `RESUME.md`.

```yaml
stats:
  top_companies_limit: 50        # default 50; also `stats --top N`

filter:
  auto_filter: true              # false = always call LLM (no deterministic cap)

scoring:
  reason_threshold: 70           # fit_reason emitted at/above this score
  baseline: 60                   # starting score after passing hard filter
  deal_breaker_cap: 30           # hard floor when deal-breaker tech matched
  deal_breakers: [".NET", "C#", "Ruby"]
  weights:
    salary: 6
    tech_overlap: 4
    startup: 5
    ai_intensity: 3
    compensation_extras: 3
    remote_tiebreak: 6

enrich:
  auto_enrich_on_save: false     # true = auto-score jobs when tagged `saved`

profile:
  work_arrangement: [remote, hybrid]
  min_salary: 200000
  min_salary_currency: CAD
  locations: [Remote, Toronto]
  preferred_tech: [Java, Python, Go, Postgres, AWS]
  avoided_tech: [C#, .NET, Ruby]   # caps score at deal_breaker_cap
```

## Resume (RESUME.md)

Free-text markdown file. Sent to the LLM as candidate context during scoring. Lives in the same directory as `settings.yaml`.

```bash
linkedin-jobs profile resume    # paste resume text, end with Ctrl-D
linkedin-jobs profile show      # show resume + active knobs
linkedin-jobs profile clear     # delete the resume file
```

## Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `LJ_DB_PATH` | SQLite database path | `~/.linkedin-jobs/linkedin_jobs.db` |
| `LJ_COOKIES_FILE` | Path to a file with a raw `Cookie:` header | — |
| `LJ_COOKIE` | Raw cookie header string | — |
| `LJ_LLM_API_KEY` / `OPENAI_API_KEY` | LLM API key | — |
| `LJ_LLM_BASE_URL` / `OPENAI_BASE_URL` | OpenAI-compatible base URL | `https://api.openai.com/v1` |
| `LJ_LLM_MODEL` | Model name | `gpt-4o-mini` |
| `LJ_LLM_DELAY_SECONDS` | Seconds to pause between LLM calls | `2.0` |
| `ANTHROPIC_API_KEY` | Claude provider (auto-detected) | — |

## File Locations

When a `settings.yaml` or `RESUME.md` already exists in the project root (CWD), the CLI uses the project root. Otherwise, everything lives under `~/.linkedin-jobs/`:

- `~/.linkedin-jobs/linkedin_jobs.db` — SQLite database
- `~/.linkedin-jobs/settings.yaml` — settings
- `~/.linkedin-jobs/RESUME.md` — resume
- `~/.linkedin-jobs/fx_cache.json` — FX rate cache (daily)

Use `linkedin-jobs config path` to see the resolved locations.
