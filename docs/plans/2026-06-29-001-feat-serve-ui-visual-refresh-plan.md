---
title: Serve UI Visual Refresh - Plan
type: feat
date: 2026-06-29
topic: serve-ui-visual-refresh
artifact_contract: ce-unified-plan/v1
artifact_readiness: implementation-ready
product_contract_source: ce-brainstorm
execution: code
---

# Serve UI Visual Refresh - Plan

*Product Contract preservation: Product Contract unchanged from the requirements-only brainstorm. This enrichment adds Planning Contract, Implementation Units, Verification Contract, and Definition of Done only.*

## Goal Capsule

- **Objective:** Visually refresh the existing Go-served `serve` UI so high-fit jobs and job status read at a glance, and give it a stronger color identity with dark mode.
- **Product authority:** Project owner (personal, single-user tool).
- **Open blockers:** None. Two items deferred to implementation (see Outstanding Questions) — both safely defaultable.
- **Execution:** Code — a reskin of the `serve` UI inside `cmd/serve.go`, with the visual design generated via Open Design.

## Product Contract

### Summary

A visual refresh of the existing Go-served `serve` UI: high-fit jobs and job status become scannable at a glance, and the UI gains a distinct color identity with dark mode. The design is produced as an HTML/CSS mockup and ported back into the Go template. It is a reskin, not new features or a re-platform.

### Problem Frame

The `serve` UI is functional but reads like a debug table. All job cards look alike, so the strongest matches don't stand out from the noise, and the user has to scan score chips and status text to find what matters. The GitHub-neutral palette is competent but bland and has no identity. There is no dark mode. The underlying tool is a single-user, localhost, read-mostly browser over a local SQLite store — it already works; only the presentation is holding it back.

### Key Decisions

- **Stay on Go-served HTML, not React.** The personal-use goal does not earn React's carrying cost (a build pipeline, a JSON API layer the store doesn't currently need, and a second runtime to maintain). Open Design outputs HTML/CSS natively, so the existing Go template can be polished directly with no framework.
- **Reskin, not restructure.** The work changes presentation only — CSS, color tokens, card visual hierarchy, and theme. Routes, handlers, the store layer, and the data model are untouched. All current behavior stays identical.
- **Dark mode follows OS preference.** Use `prefers-color-scheme` so dark mode is automatic, with no JavaScript and no persisted state. This matches the no-new-architecture, personal-use framing.
- **Design is produced in Open Design, then ported.** Open Design generates the polished HTML/CSS mockup; the resulting tokens and markup are translated back into the Go template's embedded `pageHTML` and `<style>` block. Open Design is the design vehicle because the owner asked for it and it targets HTML directly.

### Requirements

**Visual hierarchy**

- R1. High-fit-score jobs are visually prioritized so they are identifiable at a glance within a list of results.
- R2. Job status — especially `new` and `saved` — is distinguishable at a glance without expanding a card.

**Color identity and theme**

- R3. The UI presents a distinct color identity rather than the current generic neutral palette.
- R4. The UI renders in dark mode.

**Preserved behavior**

- R5. All existing functionality continues to work unchanged: full-text search, column filters, sort by fit score or salary, status change, hard delete, expand/collapse of long-text fields, CSRF-guarded writes, localhost-only binding, and single-binary distribution.

### Scope Boundaries

- No JavaScript framework migration (React/Vue/Svelte), and no client framework added — not even a sprinkle lib — unless an existing, preserved interaction demands it.
- No JSON API layer; the store continues to be used in-process by the handlers.
- No new interactive features — inline note editing, kanban board, saved views, and live faceted filters are out.
- No multi-user, authentication, or deployment work; the tool stays single-user and localhost-only.
- No changes to the CLI commands or to the data/store model.
- The CLI surface is unchanged; only the `serve` UI is in scope.

### Outstanding Questions

- **Deferred to Implementation — "pop" treatment.** The specific visual device for making top matches stand out (left-border accent, tinted card background, larger score badge, or a combination) is to be resolved from the Open Design mockup and a review pass against real data.
- **Deferred to Implementation — dark-mode toggle.** Dark mode is defaulted to OS preference; revisit if a persisted manual light/dark switch is wanted (it adds a small amount of JavaScript and localStorage state).

### Sources / Research

- Current UI implementation: `cmd/serve.go` — a single file containing the handlers and the embedded `pageHTML` template with its inline `<style>` and a small vanilla-JS script (status change, delete, mark-viewed via `fetch`).
- Behavior reference: `README.md`, "Web UI (local browser)" section — documents the read-mostly contract, editable status/delete, and localhost binding.
- No frontend tooling exists today: the project is pure Go (no `package.json`, no `node_modules`), which is why the no-framework decision carries no migration cost.
- No CI workflows exist (`.github/workflows` absent), so automated verification is local Go tooling only.

## Planning Contract

### Key Technical Decisions

- **KTD1. Dark mode via `prefers-color-scheme`, no toggle.** A single `@media (prefers-color-scheme: dark)` block remaps the color tokens. Zero JavaScript, zero persisted state, follows the OS. A manual toggle is explicitly out of scope (see Outstanding Questions) to honor the no-new-architecture decision.
- **KTD2. Single-file reskin inside `cmd/serve.go`.** The new CSS and markup stay in the embedded `pageHTML` constant — no extracted `.css`/`.js` files, no `embed.FS`, no new package. This preserves the single-binary distribution and the existing one-file architecture, minimizing carrying cost.
- **KTD3. Open Design generates the design; the port adapts, not copy-pastes.** Open Design output is plain HTML/CSS and cannot know about Go `html/template` directives (`{{range}}`, `{{if}}`, `{{.Field}}`) or the existing vanilla-JS contract. Implementation translates the mockup's *token system and visual treatment* into the template by hand, reconciling every `{{...}}` directive and JS hook. Treat the mockup as a high-fidelity design source, not drop-in code.
- **KTD4. Preserve the JS contract exactly.** The existing `<script>` depends on a fixed set of selectors and attributes: `article.job`, `data-id`, `data-status`, `select.js-status-select`, `button.js-delete`, `.js-delete-form`, `.job-head a`, and `meta[name=csrf-token]`. The reskin must keep every one of these intact (classes can be *added*, never renamed or removed), or status change / delete / mark-viewed silently break with no handler change to catch them. U3's render test guards this.
- **KTD5. Color token system as the spine of the reskin.** Replace the flat `:root` variable block with a layered token set (surface / ink / muted / line / accent / score-high / score-mid / score-low / status-new / status-saved) defined once for light and remapped under `prefers-color-scheme: dark`. All card chrome and chips reference tokens, so dark mode and the new identity come for free once the tokens are right.
- **KTD6. Visual hierarchy via two reinforcing devices.** Top-match prominence (R1) comes from (a) a larger, more saturated fit-score badge and (b) a reserved accent treatment for high-score cards (e.g. a colored left border or subtle surface tint) that the mid/low tiers do not get. Status pop (R2) comes from a status pill/indicator on the card, distinct from the inline muted text today. Exact form is finalized from the Open Design mockup (see Outstanding Questions).

### Assumptions

- Open Design is operational on this machine and a design-generation run can complete against a concrete brief. If the run fails or is unavailable, fall back to implementing the token system and hierarchy devices directly from KTD5/KTD6 and note the fallback in the commit message.
- The repo has no hidden CSS/style conventions beyond `cmd/serve.go` (confirmed: no other HTML/CSS files, no `embed` of assets).
- Verification is local Go tooling plus a render-safety test; no automated visual/browser regression suite exists or is being added.

## Implementation Units

### U1. Generate the reskin design via Open Design

- **Goal:** Produce a polished HTML/CSS mockup that demonstrates R1–R4 against representative job data, to drive the port in U2.
- **Files:** No repo files. Creates a new Open Design project and artifact (out of tree).
- **Patterns / approach:**
  - Create a dedicated Open Design project (do not reuse the unrelated existing project).
  - Run `start_run` with a concrete brief grounded in the current `pageHTML` structure: a job-card list with the existing fields (title, company, location, salary, status, source, fit score, enrichment chips, expandable summary/description/overview/fit-reason/notes, status `<select>`, delete), the filter form, and the count/empty/footer states.
  - The brief must specify the three load-bearing outcomes: (1) top matches visually dominate a list of mixed-score cards, (2) `new`/`saved` status is recognizable without expanding, (3) a distinct color identity with a working dark-mode rendering.
  - Poll `get_run` to completion, then pull the artifact with `get_artifact`. Do not substitute a hand-written mockup as a "faster" path — the Open Design run is the design source of record per KTD3.
- **Test scenarios:** N/A (design artifact). Acceptance is that the retrieved mockup visibly demonstrates R1, R2, and a coherent light+dark token palette.
- **Verification:** `get_artifact` returns HTML/CSS whose palette and card treatment can be mapped onto the Go template constraints in U2.

### U2. Port the design into the Go template in `cmd/serve.go`

- **Goal:** Translate the Open Design mockup into the embedded `pageHTML` template's `<style>` block and card markup, satisfying R1–R5.
- **Files:** `cmd/serve.go` only (the `pageHTML` constant: `<style>` block, the `<article class="job">` card markup, the status/score presentation, and the filter/header/footer chrome). No handler changes, no new files.
- **Patterns / approach:**
  - Replace the `:root` token block with the layered token system from KTD5; add the `@media (prefers-color-scheme: dark)` remap. All existing selectors (`.job`, `.score-high/mid/low`, `.chip`, `.meta`, `details`, etc.) are restyled to reference tokens.
  - Implement the two hierarchy devices from KTD6 on the high-score tier only (driven by the existing `.score-high` / `ScoreClass` field — no template-logic change needed).
  - Add a status indicator (pill/dot) surfaced from the existing `.Status` / `data-status`, without removing the current inline status text the JS updates.
  - **Preserve every template directive and JS hook verbatim** (KTD4): all `{{range .Jobs}}`, `{{if}}`, `{{.Field}}`, `{{$.CSRF}}`, the `data-id`/`data-status` attributes, `.js-status-select`, `.js-delete`, `.js-delete-form`, `.job-head a`, and `meta[name=csrf-token]`. Escape any literal `{` / `}` introduced from the mockup's CSS/JS so `html/template` parsing is not broken.
  - Keep the existing `<script>` block behaviorally unchanged; only update selectors it relies on if (and only if) classes were added — never remove a hook.
- **Test scenarios:** Covered by U3.
- **Verification:** `go build ./...` compiles; template parses and renders under U3; light and dark render via `prefers-color-scheme`.

### U3. Add a render-safety test for the template and JS contract

- **Goal:** Guard against template-parse and JS-hook regressions (KTD4), since status/delete/view have no handler-level safety net.
- **Files:** `cmd/serve_test.go` (new). Follow the style of existing `cmd/enrich_test.go`, `cmd/profile_test.go`.
- **Patterns / approach:** Parse `pageHTML` with `template.New("page").Parse`, execute it against a constructed `pageData` (a couple of `jobView` entries with score/status set, one `filtered`, one with nil score), and assert on the rendered bytes.
- **Test scenarios:**
  - Template parses without error.
  - Renders with a populated job list without error; rendered HTML contains every required hook: `article.job`, `data-id`, `data-status`, `class="js-status-select"` (or the renamed-but-intentional equivalent — assert the actual class used), `js-delete`, and `meta[name=csrf-token]`.
  - Renders with an empty job list (`Jobs: nil`) and an error state without panicking.
  - A high-score job renders with the high-tier class so the hierarchy device is wired (assert the score-tier class is present on the right card).
  - CSRF token round-trips into the rendered `meta` and the delete form's hidden field.
- **Verification:** `go test ./...` passes (existing tests + this new test).

## Verification Contract

| Command / check            | Scope                                   | Applies to | Done signal                       |
|----------------------------|-----------------------------------------|------------|-----------------------------------|
| `go build ./...`           | Whole module compiles                   | U2, U3     | Exit 0, no output                 |
| `go vet ./...`             | Static analysis clean                   | U2, U3     | Exit 0, no findings               |
| `go test ./...`            | Existing tests + new serve render test  | U3         | All packages pass                 |
| JS-contract assertions     | Required hooks present after render     | U3         | New `serve_test.go` assertions green |
| Manual visual (post-merge) | Light + dark, hierarchy, status pop     | U2         | Owner confirms in browser (non-blocking for pipeline) |

## Definition of Done

- U1 produced an Open Design mockup demonstrating the load-bearing outcomes (or a documented fallback per the Assumptions).
- U2 ported the design into `cmd/serve.go` with every template directive and JS hook preserved.
- U3 added `cmd/serve_test.go`; `go build ./...`, `go vet ./...`, and `go test ./...` are all green.
- R1–R5 are satisfied: top matches and status pop, a distinct identity, dark mode via OS preference, and all existing behavior intact.
