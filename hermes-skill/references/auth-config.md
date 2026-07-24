# Auth, Config, and Environment

## Auth Model

The CLI has two auth modes:

### Authenticated (LinkedIn session)

Required for `recommended` and `url`. Uses your captured browser session cookies.

**Resolution order** (first match wins):

1. `LJ_COOKIE` env var — raw cookie header string (`name=val; name=val`). Highest priority override.
2. `LJ_COOKIES_FILE` env var — path to a file containing either a raw `Cookie:` header (`name=val; name=val`) or Netscape cookies.txt format.
3. `~/.linkedin-jobs/cookies.txt` — default file, written automatically by `auth login`. This is the most common source for interactive use.

The CSRF token is derived automatically from the `JSESSIONID` cookie. The CLI checks for `li_at` + `JSESSIONID` cookies.

### Browser-based login (`auth login`)

**macOS & Windows + Chrome.** The easiest way to set up a session — no manual cookie export needed.

```bash
linkedin-jobs auth login
```

**Stage 1 — silent read (no browser window opens):**

The CLI reads cookies directly from Chrome's encrypted cookie database on disk:

1. Locates the cookie DB — macOS: `~/Library/Application Support/Google/Chrome/Default/Network/Cookies`; Windows: `%LOCALAPPDATA%\Google\Chrome\User Data\Default\Network\Cookies`.
2. Copies it to a temp dir (Chrome locks the file while running; WAL sidecars included).
3. Retrieves the Chrome cookie key from the OS secret store. **macOS:** `Chrome Safe Storage` passphrase via the Keychain (`security find-generic-password`) — first run triggers a keychain prompt, the user clicks "Always Allow". **Windows:** the AES key in Chrome's `Local State`, base64-decoded, `DPAPI`-prefix-stripped, and unprotected via `CryptUnprotectData` (no prompt).
4. Decrypts each LinkedIn cookie. **macOS:** PBKDF2-HMAC-SHA1 (salt `saltysalt`, 1003 iterations) → AES-128-CBC (IV of 16 spaces) → PKCS7 unpad. **Windows:** AES-256-GCM (`v10` || nonce(12) || ciphertext || tag(16)) with the DPAPI-unprotected key. Chrome 130+ (DB v24) prepends a SHA256 host digest that is stripped automatically on both platforms.
5. `li_at` (auth token) is persisted to disk and usually present. `JSESSIONID` (CSRF source) is session-only and often absent — when missing, the CLI fetches a fresh one via HTTP GET to `https://www.linkedin.com/` with `li_at` and reads it from `Set-Cookie`.
6. If both are present → session assembled and written. **No browser opens.**

**Stage 2 — guided browser login (fallback):**

If the silent read fails (not logged in, Chrome missing, keychain denied, stale cookies), the CLI launches a headed Chrome via chromedp:

1. Uses a **managed profile** at `~/.linkedin-jobs/chrome-profile/` (not the user's real Chrome profile, so no conflict with their running browser). Persists across runs for LinkedIn trust accumulation.
2. Anti-bot hardening: headless disabled, `enable-automation` off, `AutomationControlled` blink feature removed.
3. Navigates to `https://www.linkedin.com/login`. **The user logs in normally** (credentials, 2FA, challenges). The CLI never sees the password.
4. Polls every 2s via CDP `Network.getCookies` for `li_at` (HttpOnly — only readable via DevTools Protocol, not JS). Timeout: 5 minutes.
5. On detection: captures all `linkedin.com` cookies, closes browser, writes session.

LinkedIn may challenge a fresh managed profile on first login (email/SMS verification). The user completes it in the window; capture proceeds automatically.

**Write target:** `LJ_COOKIES_FILE` env path if set, otherwise `~/.linkedin-jobs/cookies.txt` (0600 perms). Written as a raw `Cookie:` header.

**Refreshing:** re-run `auth login` when `auth status` reports an incomplete/stale session.

### Manual cookie setup (headless, non-macOS/Windows, CI)

For environments without a browser, export cookies manually and set an env var:

```bash
export LJ_COOKIES_FILE=/path/to/cookies.txt   # raw "name=val; name=val" header or Netscape cookies.txt
# or:
export LJ_COOKIE="li_at=...; JSESSIONID=ajax:..."
```

**Verify the session with `doctor`, not just `auth status`:**

```bash
linkedin-jobs doctor          # canonical first-run check; prints every LJ_* env var
                              # (set/unset, secrets redacted). Look for the
                              # LJ_COOKIES_FILE line under "== Environment ==".

linkedin-jobs auth status     # fast mid-session boolean check. Only meaningful
                              # AFTER you've confirmed via doctor that
                              # the session is set up.
```

**If `doctor` shows no session:** recommend `auth login` on macOS or Windows. For other platforms or headless, the user must export `LJ_COOKIES_FILE` or `LJ_COOKIE` in the agent's shell. Do **not** silently fall back to anonymous `search`; that yields irrelevant global results. See SKILL.md Pitfall #1.

**Security:** LinkedIn session cookies enable full account takeover. The cookies file has `0600` permissions in a user-only directory. Treat it with the same care as SSH private keys. The agent must never `cat`, `echo`, or transmit the file contents — use `doctor` / `auth status` for session checks only.

### Anonymous (no session)

`search`, `hr`, and `job` work without a session. No login needed.

## LLM Configuration

An LLM provider is required for scoring — fetch+score commands (`recommended`/`url`/`search`/`job`) exit with a setup prompt when none is configured.

### Provider Resolution (first match wins)

1. `LJ_LLM_API_KEY` or `OPENAI_API_KEY` env var
2. `ANTHROPIC_API_KEY` env var — Anthropic's endpoint by default; when `ANTHROPIC_BASE_URL` redirects it (e.g. an opencode/Hermes session pointing it at z.ai), the CLI honors the redirected endpoint and takes the model from opencode config
3. **`claude` CLI (Claude Code session reuse)** — when `claude` is on PATH and `claude auth status` reports a login, the CLI shells out to `claude -p` for each LLM call. This lets it reuse the user's Claude Pro/Max subscription **without a separate API key**, which is exactly what a Claude Code OAuth session needs (its OAuth token is not exposed to subprocesses and cannot be used as a bare API key). Set `LJ_LLM_DISABLE_CLAUDE_CLI=1` to force the HTTP path.
4. opencode's stored credentials (reuses the provider configured in opencode, e.g. GLM Coding Plan key → `glm-5.2`)

Explicit env vars win over both claude-cli and opencode discovery, so you can override either. When you invoke this CLI from inside an opencode/Hermes session, no `LJ_LLM_*` is needed — the session injects `ANTHROPIC_API_KEY` + `ANTHROPIC_BASE_URL`, and the CLI reuses that session LLM. When you invoke it from inside a **Claude Code** session (OAuth login), the `claude` CLI branch handles it automatically — again no `LJ_LLM_*` needed. Set `LJ_LLM_*` to override either.

### LLM Settings

| Variable | Purpose | Default |
|----------|---------|---------|
| `LJ_LLM_API_KEY` / `OPENAI_API_KEY` | LLM API key | — |
| `LJ_LLM_MODEL` | Model name | `gpt-4o-mini` |
| `LJ_LLM_BASE_URL` / `OPENAI_BASE_URL` | OpenAI-compatible base URL (Ollama, vLLM, Azure) | `https://api.openai.com/v1` |
| `ANTHROPIC_API_KEY` | Claude provider (auto-detected) | — |
| `LJ_LLM_DISABLE_CLAUDE_CLI` | Disable the `claude` CLI backend (force HTTP) | — |
| `LJ_LLM_DELAY_SECONDS` | Pause between successive LLM calls (avoids 429s) | `2.0` |

**Data residency:** `LJ_LLM_BASE_URL` can point to a self-hosted endpoint (Ollama at `http://localhost:11434/v1`, vLLM, etc.) for users who need data residency control. The scoring pipeline transmits job descriptions and preference knobs to the configured provider.

### Config Commands

```bash
linkedin-jobs config show     # resolved provider (key redacted) + settings
linkedin-jobs config path     # settings/db file locations
linkedin-jobs doctor          # diagnose provider + settings completeness
```

## Settings (settings.yaml)

Optional `settings.yaml` in `~/.linkedin-jobs/` (override with `$LJ_SETTINGS_FILE`):

```yaml
scoring:
  rubrics:                       # weight 1-10 (default 5); run `setup` to generate
    - id: salary                 # system rubrics are computed in Go
      kind: system
      weight: 5
    - id: work_arrangement
      kind: system
      weight: 5
    - id: location
      kind: system
      weight: 5
    - id: preferred_tech         # dynamic rubrics are rated 1-5 by the LLM
      kind: dynamic
      weight: 5
      items: [Java, Python, Go]
    - id: avoided_tech
      kind: dynamic
      weight: 5
      items: [C#, .NET]
    - id: location
      kind: dynamic
      weight: 5
      description: "Hybrid must be in Toronto/Mississauga; remote is flexible anywhere"
      applies_to: [hybrid, onsite]  # optional: skip this rubric for other arrangements

profile:                         # structured inputs for the system rubrics
  work_arrangement: [remote, hybrid]
  min_salary: 200000
  min_salary_currency: CAD
  preferred_tech: [Java, Python, Go, Postgres, AWS]
  avoided_tech: [C#, .NET, Ruby]
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
| `LJ_LLM_DISABLE_CLAUDE_CLI` | Disable the `claude` CLI backend (force HTTP) | — |

## File Locations

Everything lives under `~/.linkedin-jobs/` (override settings path via `$LJ_SETTINGS_FILE`):

- `~/.linkedin-jobs/linkedin_jobs.db` — SQLite database
- `~/.linkedin-jobs/cookies.txt` — LinkedIn session cookies (written by `auth login`, 0600 perms)
- `~/.linkedin-jobs/chrome-profile/` — managed Chrome profile for guided login (created on first `auth login` fallback)
- `~/.linkedin-jobs/settings.yaml` — settings
- `~/.linkedin-jobs/fx_cache.json` — FX rate cache (daily)

Use `linkedin-jobs config path` to see the resolved locations.
