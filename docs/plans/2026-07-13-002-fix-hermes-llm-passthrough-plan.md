---
title: Hermes/Opencode Session LLM Passthrough - Plan
type: fix
date: 2026-07-13
topic: hermes-llm-passthrough
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-brainstorm
execution: code
product_contract_preservation: Product Contract unchanged — Key Decisions refined into KTDs, implementation units added.
---

# Hermes/Opencode Session LLM Passthrough

## Goal Capsule

- **Objective:** Make the CLI reuse the opencode/Hermes session's LLM when invoked from inside it, by fixing the provider-resolution path that today shadows the session's provider with a hardcoded Anthropic preset.
- **Authority:** User-confirmed scope — same runtime (Hermes == opencode), minimal "just make it work," and `LJ_LLM_*` / `OPENAI_*` must keep precedence when set.
- **Execution profile:** Code — small change in LLM provider resolution plus a skill-doc update.
- **Open blockers:** None.

---

## Product Contract

### Summary

When run inside an opencode/Hermes session, the CLI resolves the session's actual LLM provider (redirected endpoint + session model) instead of a hardcoded Anthropic preset that ignores the session's redirected endpoint. The fix lives in the existing provider-resolution path; standalone usage and explicit env overrides are unchanged.

### Problem Frame

The CLI's `ANTHROPIC_API_KEY` resolution hardcodes Anthropic's endpoint and ignores `ANTHROPIC_BASE_URL`. opencode/Hermes exposes its active LLM to Bash subprocesses two ways: it injects `ANTHROPIC_API_KEY` + `ANTHROPIC_BASE_URL` (redirected at the configured provider — here z.ai coding plan), and it writes opencode's credential store. The CLI already reads that store via an opencode-discovery path that would resolve correctly to the z.ai endpoint and model.

The defect is that the injected env var outranks discovery and produces a provider pointing at `api.anthropic.com` with the z.ai key and a Claude model, so scoring 401s. Live evidence in this session: `config show` reports `anthropic-env` / `api.anthropic.com` / `claude-3-5-sonnet-latest`; the same command with `ANTHROPIC_API_KEY` unset reports `opencode` / `api.z.ai/api/coding/paas/v4` / `glm-5.2`. Standalone users who never enter a session never hit this — it only bites when the CLI is invoked from inside the agent, which is exactly the hermes-skill use case.

### Key Decisions

- **Honor the redirected endpoint; do not reorder discovery.** When `ANTHROPIC_BASE_URL` is present, build the provider from it plus the env key plus the session model, rather than reordering resolution to prefer opencode discovery. Honoring the redirected BASE_URL is robust to any opencode provider (Ollama, vLLM, future providers) without depending on the preset registry, whereas discovery returns false for provider ids the registry does not know.
- **Model is sourced from the session's opencode config.** The `ANTHROPIC_*` env pair carries no model name, so the session's configured model (already readable by the CLI from opencode config, e.g. `glm-5.2`) supplies it, with a default fallback when absent.
- **Detection signal is `ANTHROPIC_BASE_URL`.** A redirected endpoint is what distinguishes "session LLM, not real Anthropic" from a genuine Anthropic key. Absence of the var preserves the existing real-Anthropic path.

### Requirements

**Resolution**

- R1. Inside an opencode/Hermes session with no explicit override, the CLI resolves the session's actual LLM — the endpoint from `ANTHROPIC_BASE_URL` and the model from the session's opencode config — instead of a hardcoded Anthropic preset.
- R2. Explicit `LJ_LLM_*` / `OPENAI_*` env vars take precedence over the session provider when set.

**Non-regression**

- R3. A genuine `ANTHROPIC_API_KEY` with no `ANTHROPIC_BASE_URL` still resolves to Anthropic's endpoint with an Anthropic model.
- R4. The opencode stored-credentials discovery path is unchanged.

**Discoverability**

- R5. `config show` and `doctor` report the resolved session provider's real endpoint and model, and surface `ANTHROPIC_BASE_URL` among the checked env vars.
- R6. `hermes-skill/references/auth-config.md` documents that the CLI reuses the opencode/Hermes session LLM when invoked from inside it, and notes the precedence (`LJ_LLM_*` > session > discovery).

### Acceptance Examples

- AE1. Session LLM used, no override
  - **Given:** opencode session injects `ANTHROPIC_API_KEY` and `ANTHROPIC_BASE_URL=https://api.z.ai/api/coding/paas/v4`; opencode config model is `zai-coding-plan/glm-5.2`; no `LJ_LLM_*` / `OPENAI_*` set.
  - **When:** the CLI runs an LLM-using command (`enrich`, scoring) or `config show`.
  - **Then:** the provider resolves to the z.ai endpoint with model `glm-5.2`; the LLM call succeeds.
  - **Covers:** R1.
- AE2. Explicit override wins
  - **Given:** the session env above, plus `LJ_LLM_API_KEY` / `LJ_LLM_BASE_URL` / `LJ_LLM_MODEL` set to a different provider.
  - **When:** `config show`.
  - **Then:** the provider resolves to the `LJ_LLM_*` provider, not the session one.
  - **Covers:** R2.
- AE3. Real Anthropic key, no redirect
  - **Given:** `ANTHROPIC_API_KEY` set and `ANTHROPIC_BASE_URL` unset.
  - **When:** `config show`.
  - **Then:** the provider resolves to `api.anthropic.com` with an Anthropic model — current behavior preserved.
  - **Covers:** R3.

### Scope Boundaries

**Outside this product's identity:**

- Calling back into the Hermes session via MCP/IPC, or running a localhost LLM proxy — the CLI calls the LLM directly through the session's exposed creds/endpoint.
- Supporting non-OpenAI-compatible providers — the CLI's client speaks `/chat/completions` only.
- A separate/distinct Hermes agent — confirmed same runtime (opencode).

**Deferred for later:**

- Generalizing to other agent runtimes that expose their LLM to subprocesses differently than the `ANTHROPIC_*` env pair.

### Dependencies and Assumptions

- A1. opencode/Hermes injects `ANTHROPIC_API_KEY` + `ANTHROPIC_BASE_URL` into Bash subprocesses for the active provider (verified for this z.ai session; generalized as the assumed contract). If opencode changes how it exposes its LLM, the detection signal moves with it.
- A2. The session's redirected endpoint is OpenAI-compatible, since the CLI's client speaks only `/chat/completions`.
- A3. The session's configured model lives in opencode config in `provider/model` form, which the CLI already parses today.

### Sources

- `internal/llm/provider.go` — the resolution order, the `ANTHROPIC_API_KEY` branch that ignores `ANTHROPIC_BASE_URL`, the opencode-discovery path, and the provider presets.
- `internal/llm/chat.go` — the OpenAI-compatible `/chat/completions` client.
- `internal/config/config.go` — the `LJ_LLM_*` / `OPENAI_*` env layer.
- `hermes-skill/references/auth-config.md` — provider-resolution docs to update.
- Live evidence (this session): `config show` → `anthropic-env` / `api.anthropic.com` / `claude-3-5-sonnet-latest` (broken); `env -u ANTHROPIC_API_KEY ... config show` → `opencode` / `api.z.ai/api/coding/paas/v4` / `glm-5.2` (correct).

---

## Planning Contract

### Key Technical Decisions

- **KTD1. Fix the anthropic-env branch to honor a redirected `ANTHROPIC_BASE_URL`; do not reorder resolution.** When `ANTHROPIC_API_KEY` is set together with `ANTHROPIC_BASE_URL`, build the provider from that base URL + the env key + the session model, rather than reordering to prefer opencode discovery. Honoring the redirected endpoint is robust to any opencode provider (z.ai today, Ollama/vLLM tomorrow) without depending on the preset registry, whereas discovery returns false for provider ids the registry does not know.
- **KTD2. Model is sourced from opencode config when redirected.** The `ANTHROPIC_*` env pair carries no model name, so the model part of opencode's `provider/model` string (already readable via `readOpencodeModel`) supplies it, with a default fallback when opencode config is absent. The existing split logic in `FromOpencode` is the pattern to reuse.
- **KTD3. Detection signal is the presence of `ANTHROPIC_BASE_URL`.** A redirected endpoint distinguishes "session LLM, not real Anthropic" from a genuine Anthropic key. Absence of the var preserves the existing real-Anthropic path (hardcoded `api.anthropic.com` preset, `claude-3-5-sonnet-latest`, `x-api-key` header) — non-regression for real Anthropic users.
- **KTD4. Resolution precedence is unchanged.** `LJ_LLM_*` / `OPENAI_*` (tier 1) still wins; the `ANTHROPIC_API_KEY` path (tier 2, now redirect-aware) still beats opencode discovery (tier 3). Existing tests `TestResolve_LLMEnvBeatsOpencode` and `TestResolve_AnthropicEnvBeatsOpencode` continue to assert this.

### High-Level Technical Design

Resolution in `internal/llm/provider.go` `Resolve`, after this change:

| Precedence | Trigger | Provider built |
|---|---|---|
| 1 | `LJ_LLM_*` / `OPENAI_*` key set | `{cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel}` (unchanged) |
| 2a | `ANTHROPIC_API_KEY` + `ANTHROPIC_BASE_URL` set | `{ANTHROPIC_BASE_URL, key, opencode-model-or-default}` (new) |
| 2b | `ANTHROPIC_API_KEY` set, no `ANTHROPIC_BASE_URL` | hardcoded Anthropic preset (unchanged) |
| 3 | opencode stored credentials | opencode discovery (unchanged) |
| — | none | `ErrNoProvider` |

The 2a provider uses Bearer auth (OpenAI-compatible) — it must NOT inject Anthropic's `x-api-key`/`anthropic-version` headers, since the redirected endpoint (z.ai) is OpenAI-compatible, not Anthropic-native. `Source` stays `"anthropic-env"` so `config show`/`doctor` continuity is preserved and existing precedence tests hold.

### Assumptions

- A1. opencode/Hermes injects `ANTHROPIC_API_KEY` + `ANTHROPIC_BASE_URL` into Bash subprocesses for the active provider (verified for this z.ai session; generalized as the assumed contract).
- A2. The session's redirected endpoint is OpenAI-compatible (`/chat/completions`), since the CLI's client speaks only that.
- A3. The session's configured model lives in opencode config in `provider/model` form, which the CLI already parses.

---

## Implementation Units

### U1. Honor redirected `ANTHROPIC_BASE_URL` in provider resolution

- **Goal:** Make the CLI resolve the opencode/Hermes session's real LLM provider when `ANTHROPIC_BASE_URL` redirects away from Anthropic.
- **Requirements:** R1, R2, R3, R4
- **Files:**
  - `internal/llm/provider.go` (modify — the `ANTHROPIC_API_KEY` branch of `Resolve`)
  - `internal/llm/provider_test.go` (modify — extend test coverage)
- **Approach:** In `Resolve`'s `ANTHROPIC_API_KEY` branch, read `ANTHROPIC_BASE_URL`. When set, return a provider with that base URL, the env key, Bearer auth (no Anthropic headers), and the model taken from opencode config (split the model part off `readOpencodeModel()`'s `provider/model` result; default when absent or malformed). When `ANTHROPIC_BASE_URL` is unset, keep the current `buildProvider(presets["anthropic"], key)` path verbatim. Keep `Source = "anthropic-env"` for both arms.
- **Patterns to follow:** The model-split already in `FromOpencode` (`strings.SplitN(m, "/", 2)`); the Bearer-only auth used by the `LJ_LLM_*`/`OPENAI_*` tier.
- **Test scenarios:**
  - **Redirect honored:** `ANTHROPIC_API_KEY` + `ANTHROPIC_BASE_URL` set + opencode config model `zai-coding-plan/glm-5.2` → provider BaseURL = the env value, Model = `glm-5.2`, no `x-api-key` header, Source = `anthropic-env`. (Covers AE1, R1.)
  - **Redirect without opencode model:** `ANTHROPIC_API_KEY` + `ANTHROPIC_BASE_URL` set, opencode config absent → model falls back to a default, BaseURL still = env value. (Edge case.)
  - **Real Anthropic preserved:** `ANTHROPIC_API_KEY` set, `ANTHROPIC_BASE_URL` unset → `api.anthropic.com`, `x-api-key` header set, `claude-3-5-sonnet-latest`. Existing `TestResolve_FromAnthropicEnv` covers this once `clearProviderEnv` also clears `ANTHROPIC_BASE_URL`. (Covers AE3, R3.)
  - **Precedence preserved:** `LJ_LLM_*` still beats the anthropic-env path (`TestResolve_LLMEnvBeatsOpencode` unchanged); `ANTHROPIC_API_KEY` still beats discovery (`TestResolve_AnthropicEnvBeatsOpencode` unchanged). (Covers AE2, R2, R4.)
- **Verification:** `go test ./internal/llm/...` green; `go vet ./...` clean.

### U2. Surface `ANTHROPIC_BASE_URL` in diagnostics

- **Goal:** Make the new signal visible in `doctor` so users can diagnose session-LLM resolution.
- **Requirements:** R5
- **Files:**
  - `cmd/doctor.go` (modify — add `ANTHROPIC_BASE_URL` to `doctorEnvKeys`)
  - `internal/llm/provider_test.go` (modify — add `ANTHROPIC_BASE_URL` to `clearProviderEnv` so tests are hermetic)
- **Approach:** `config show` already prints `Provider/Base URL/Model` straight from the resolved `Provider`, so it auto-reflects U1 with no code change. Add `ANTHROPIC_BASE_URL` to the `doctorEnvKeys` slice so `doctor` reports it (non-secret URL, printed as-is). Add `t.Setenv("ANTHROPIC_BASE_URL", "")` to `clearProviderEnv` so the existing no-redirect Anthropic tests are not polluted by a leaked host env.
- **Patterns to follow:** Existing `doctorEnvKeys` slice and `redactEnv` (URLs are not redacted — they are not secrets).
- **Test scenarios:**
  - **doctor lists the var:** `doctorEnvKeys` contains `ANTHROPIC_BASE_URL`.
  - **Hermetic tests:** with `clearProviderEnv`, `ANTHROPIC_BASE_URL` is empty regardless of host env.
- **Verification:** `go test ./internal/llm/... ./cmd/...` green; `linkedin-jobs doctor` shows the var.

### U3. Document the session-LLM passthrough in the skill

- **Goal:** Make the behavior discoverable to anyone invoking the CLI through the hermes skill.
- **Requirements:** R6
- **Files:**
  - `hermes-skill/references/auth-config.md` (modify — the "Provider Resolution" subsection)
- **Approach:** Update the "Provider Resolution (first match wins)" list to record the redirect-aware step: when `ANTHROPIC_BASE_URL` redirects the endpoint, the CLI uses the session's provider and model. Add a short note that when invoked inside an opencode/Hermes session the CLI reuses the session's LLM with no `LJ_LLM_*` needed, and restate precedence (`LJ_LLM_*` > session anthropic-env > opencode discovery).
- **Patterns to follow:** The existing provider-resolution prose and env-var table in the same file.
- **Test scenarios:**
  - **Docs match behavior:** the resolution list names the `ANTHROPIC_BASE_URL` redirect arm and the precedence order matches KTD4.
- **Verification:** the auth-config resolution section matches the implemented resolution order.

---

## Verification Contract

| Gate | Command / check | Applies to |
|------|-----------------|------------|
| Unit tests green | `go test ./internal/llm/...` | U1, U2 |
| Cmd tests green | `go test ./cmd/...` | U2 |
| Vet clean | `go vet ./...` | U1, U2 |
| Build clean | `go build ./...` | U1, U2 |
| Live resolution (in session) | `linkedin-jobs config show` reports the z.ai endpoint + `glm-5.2` | U1 |
| Live resolution (override) | `LJ_LLM_API_KEY=x LJ_LLM_BASE_URL=y LJ_LLM_MODEL=z linkedin-jobs config show` reports `y`/`z` | U1 |
| Real-Anthropic preserved | `env -u ANTHROPIC_BASE_URL ANTHROPIC_API_KEY=k linkedin-jobs config show` reports `api.anthropic.com` | U1 |
| Doctor surfaces var | `linkedin-jobs doctor` lists `ANTHROPIC_BASE_URL` | U2 |
| Docs accurate | `auth-config.md` resolution list matches implemented order | U3 |

---

## Definition of Done

- `Resolve` honors `ANTHROPIC_BASE_URL` (U1) with new + existing provider tests green.
- `clearProviderEnv` clears `ANTHROPIC_BASE_URL`; `doctor` lists it (U2).
- `auth-config.md` documents the session-LLM passthrough and precedence (U3).
- In an opencode/Hermes session with no `LJ_LLM_*`, `config show` reports the session's real provider/model and scoring succeeds; explicit `LJ_LLM_*` still wins; real-Anthropic (no BASE_URL) unchanged.
- `go test ./...`, `go vet ./...`, `go build ./...` all clean.
