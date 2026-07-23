package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// claudeCLIDisableEnv lets users (and tests) force the HTTP path even when a
// logged-in `claude` CLI is present. Set to any non-empty value to disable.
const claudeCLIDisableEnv = "LJ_LLM_DISABLE_CLAUDE_CLI"

// Synthetic Provider fields used for display when the LLM is served by the
// `claude` CLI rather than an HTTP endpoint. They are labels, not secrets, so
// Redacted returns the key verbatim.
const (
	claudeCLIDisplayKey = "(subscription)"
	claudeCLIBaseLabel  = "(claude CLI)"
)

// claudeLookPath resolves the `claude` executable. Swappable for tests.
var claudeLookPath = exec.LookPath

// claudeAuthStatus reports whether the `claude` CLI at execPath has a valid
// login. Swappable for tests.
var claudeAuthStatus = defaultClaudeAuthStatus

// defaultClaudeAuthStatus runs `claude auth status --json` and reports the
// loggedIn field. Any error (non-zero exit, non-JSON output) is treated as
// "not authenticated" so resolution falls through to the next provider rather
// than hard-failing.
func defaultClaudeAuthStatus(execPath string) bool {
	out, err := exec.Command(execPath, "auth", "status", "--json").Output()
	if err != nil {
		return false
	}
	var s struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if json.Unmarshal(out, &s) != nil {
		return false
	}
	return s.LoggedIn
}

// FromClaudeCLI resolves a provider backed by the locally installed `claude`
// CLI (Claude Code). It lets the CLI reuse the user's Claude Pro/Max
// subscription instead of requiring a separate pay-as-you-go API key — useful
// when running under a Claude Code OAuth session, which does not inject a
// portable ANTHROPIC_API_KEY into subprocess env.
//
// Returns ok=false (so resolution falls through) when:
//   - LJ_LLM_DISABLE_CLAUDE_CLI is set,
//   - `claude` is not on PATH, or
//   - `claude auth status` reports not logged in.
func FromClaudeCLI() (*Provider, bool) {
	if os.Getenv(claudeCLIDisableEnv) != "" {
		return nil, false
	}
	execPath, err := claudeLookPath("claude")
	if err != nil {
		return nil, false
	}
	if !claudeAuthStatus(execPath) {
		return nil, false
	}
	// Empty model => let `claude -p` pick its default (the signed-in account's
	// model). LJ_LLM_MODEL overrides it if the user wants a specific one.
	model := strings.TrimSpace(os.Getenv("LJ_LLM_MODEL"))
	return &Provider{
		Kind:    backendClaudeCLI,
		Source:  "claude-cli",
		BaseURL: claudeCLIBaseLabel,
		APIKey:  claudeCLIDisplayKey,
		Model:   model,
		cliPath: execPath,
	}, true
}

// claudeRun executes a single `claude -p` completion. Swappable for tests.
// maxTokens and temperature are accepted for interface parity with the HTTP
// backend but are not honored (the `claude` CLI exposes no such flags); output
// sizing is driven by the prompt's own instructions.
var claudeRun = defaultClaudeRun

// defaultClaudeRun invokes `claude -p --output-format json` with a custom
// system prompt (replacing Claude Code's agentic default) and the user prompt
// as the positional argument. The working directory is forced to the OS temp
// dir so the subprocess does not auto-discover a CLAUDE.md from the caller's
// CWD and contaminate the extraction.
func defaultClaudeRun(execPath, model, system, user string, maxTokens int, temperature float64) (string, error) {
	args := []string{
		"-p",
		"--output-format", "json",
		"--system-prompt", system,
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, user)

	cmd := exec.Command(execPath, args...)
	cmd.Dir = os.TempDir()
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude CLI failed: %w: %s", err, truncateForError(stderr.String()))
	}

	var res struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		Result  string `json:"result"`
		IsError bool   `json:"is_error"`
	}
	if jerr := json.Unmarshal(out, &res); jerr != nil {
		// Tolerate non-JSON (e.g. text-format fallback) by returning trimmed raw.
		if s := strings.TrimSpace(string(out)); s != "" {
			return s, nil
		}
		return "", fmt.Errorf("claude CLI returned unparseable output: %s", truncateForError(string(out)))
	}
	return parseClaudeResult(res.Type, res.Subtype, res.Result, res.IsError)
}

// parseClaudeResult interprets the parsed fields of `claude -p --output-format
// json` output and returns the assistant text or a descriptive error.
func parseClaudeResult(typ, subtype, result string, isError bool) (string, error) {
	if isError || (subtype != "" && subtype != "success") {
		return "", fmt.Errorf("claude CLI error (%s): %s", subtype, truncateForError(result))
	}
	return strings.TrimSpace(result), nil
}
