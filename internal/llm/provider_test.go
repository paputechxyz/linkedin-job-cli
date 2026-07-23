package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"linkedin-jobs/internal/config"
)

func clearProviderEnv(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("LJ_LLM_API_KEY", "")
	// Default to disabling the claude-cli backend so opencode/error tests are
	// isolated from a real `claude` login on the dev/CI machine. Tests that
	// exercise the claude-cli branch re-enable it explicitly.
	t.Setenv(claudeCLIDisableEnv, "1")
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", home)
	} else {
		t.Setenv("USERPROFILE", home)
	}
	return home
}

func writeJSON(t *testing.T, path string, v interface{}) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_FromOpencode(t *testing.T) {
	home := clearProviderEnv(t)
	writeJSON(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		map[string]map[string]string{"zai-coding-plan": {"type": "api", "key": "zai-secret"}})
	writeJSON(t, filepath.Join(home, ".config", "opencode", "opencode.json"),
		map[string]string{"model": "zai-coding-plan/glm-5.2"})
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Source != "opencode" {
		t.Errorf("Source=%q want opencode", p.Source)
	}
	if p.APIKey != "zai-secret" {
		t.Errorf("APIKey=%q", p.APIKey)
	}
	if p.Model != "glm-5.2" {
		t.Errorf("Model=%q want glm-5.2", p.Model)
	}
	// Coding Plan keys must hit the /coding/ endpoint — a wrong URL here is the
	// bug that produces ZAI's "Insufficient balance" (code 1113).
	if p.BaseURL != "https://api.z.ai/api/coding/paas/v4" {
		t.Errorf("Coding Plan base URL=%q, want https://api.z.ai/api/coding/paas/v4", p.BaseURL)
	}
}

// TestResolve_LLMEnvBeatsOpencode confirms explicit LJ_LLM_* env vars override
// opencode discovery (otherwise users cannot override the discovered provider).
func TestResolve_LLMEnvBeatsOpencode(t *testing.T) {
	home := clearProviderEnv(t)
	writeJSON(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		map[string]map[string]string{"zai-coding-plan": {"type": "api", "key": "opencode-secret"}})
	p, err := Resolve(config.Config{
		LLMBaseURL: "https://api.example.com/v1",
		LLMAPIKey:  "env-key",
		LLMModel:   "env-model",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Source != "env" || p.APIKey != "env-key" || p.Model != "env-model" {
		t.Errorf("env should win over opencode: %+v", p)
	}
}

// TestResolve_AnthropicEnvBeatsOpencode confirms ANTHROPIC_API_KEY also wins
// over opencode discovery.
func TestResolve_AnthropicEnvBeatsOpencode(t *testing.T) {
	home := clearProviderEnv(t)
	writeJSON(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		map[string]map[string]string{"zai-coding-plan": {"type": "api", "key": "opencode-secret"}})
	t.Setenv("ANTHROPIC_API_KEY", "ant-key")
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Source != "anthropic-env" || p.APIKey != "ant-key" {
		t.Errorf("ANTHROPIC env should win over opencode: %+v", p)
	}
}

func TestResolve_FromAnthropicEnv(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "ant-key")
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Source != "anthropic-env" {
		t.Errorf("Source=%q want anthropic-env", p.Source)
	}
	if p.Headers["x-api-key"] != "ant-key" {
		t.Errorf("x-api-key header not set: %+v", p.Headers)
	}
	if p.Headers["anthropic-version"] == "" {
		t.Errorf("anthropic-version header missing")
	}
}

// TestResolve_AnthropicEnvRedirect confirms that when an opencode/Hermes
// session injects ANTHROPIC_BASE_URL pointing at a redirected OpenAI-compatible
// endpoint, the CLI honors that endpoint and derives the model from opencode
// config instead of forcing the hardcoded Anthropic preset at api.anthropic.com.
func TestResolve_AnthropicEnvRedirect(t *testing.T) {
	home := clearProviderEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "redirected-key")
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.z.ai/api/coding/paas/v4")
	writeJSON(t, filepath.Join(home, ".config", "opencode", "opencode.json"),
		map[string]string{"model": "zai-coding-plan/glm-5.2"})
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Source != "anthropic-env" {
		t.Errorf("Source=%q want anthropic-env", p.Source)
	}
	if p.BaseURL != "https://api.z.ai/api/coding/paas/v4" {
		t.Errorf("BaseURL=%q, want redirected endpoint", p.BaseURL)
	}
	if p.APIKey != "redirected-key" {
		t.Errorf("APIKey=%q want redirected-key", p.APIKey)
	}
	if p.Model != "glm-5.2" {
		t.Errorf("Model=%q want glm-5.2 from opencode config", p.Model)
	}
	if _, ok := p.Headers["x-api-key"]; ok {
		t.Errorf("redirected OpenAI-compatible endpoint must not carry the Anthropic x-api-key header")
	}
}

// TestResolve_AnthropicEnvRedirectNoOpencodeModel confirms the redirect arm
// falls back to a default model when opencode config is absent.
func TestResolve_AnthropicEnvRedirectNoOpencodeModel(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("ANTHROPIC_BASE_URL", "https://example.com/v1")
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.BaseURL != "https://example.com/v1" {
		t.Errorf("BaseURL=%q want https://example.com/v1", p.BaseURL)
	}
	if p.Model != defaultModel {
		t.Errorf("Model=%q want default %q", p.Model, defaultModel)
	}
}

func TestResolve_FromEnv(t *testing.T) {
	clearProviderEnv(t)
	p, err := Resolve(config.Config{
		LLMBaseURL: "https://api.openai.com/v1",
		LLMAPIKey:  "sk-env",
		LLMModel:   "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Source != "env" || p.APIKey != "sk-env" || p.Model != "gpt-4o-mini" {
		t.Errorf("unexpected provider: %+v", p)
	}
}

func TestResolve_Unconfigured(t *testing.T) {
	clearProviderEnv(t)
	_, err := Resolve(config.Config{})
	if err != ErrNoProvider {
		t.Fatalf("want ErrNoProvider, got %v", err)
	}
}

func TestRedacted(t *testing.T) {
	p := &Provider{APIKey: "sk-abcdefgh"} // 11 chars -> 7 stars + last 4
	if r := p.Redacted(); r != "*******efgh" {
		t.Errorf("Redacted=%q want *******efgh", r)
	}
}

// --- claude-cli backend ---

// enableFakeClaudeCLI stubs the PATH lookup + auth check so a logged-in
// `claude` is reported without depending on a real binary. Re-enables the
// backend (clearProviderEnv disables it by default).
func enableFakeClaudeCLI(t *testing.T, authed bool) {
	t.Helper()
	t.Setenv(claudeCLIDisableEnv, "")
	prevLook, prevAuth := claudeLookPath, claudeAuthStatus
	claudeLookPath = func(string) (string, error) { return "/fake/claude", nil }
	claudeAuthStatus = func(string) bool { return authed }
	t.Cleanup(func() {
		claudeLookPath = prevLook
		claudeAuthStatus = prevAuth
	})
}

func TestResolve_FromClaudeCLI(t *testing.T) {
	clearProviderEnv(t)
	enableFakeClaudeCLI(t, true)
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Kind != backendClaudeCLI || p.Source != "claude-cli" {
		t.Errorf("expected claude-cli backend, got %+v", p)
	}
	if p.cliPath != "/fake/claude" {
		t.Errorf("cliPath=%q want /fake/claude", p.cliPath)
	}
	// No explicit model => empty so `claude -p` picks its default.
	if p.Model != "" {
		t.Errorf("Model=%q want empty (claude default)", p.Model)
	}
	// Synthetic key is not a secret; Redacted must surface it verbatim.
	if p.Redacted() != claudeCLIDisplayKey {
		t.Errorf("Redacted=%q want %q", p.Redacted(), claudeCLIDisplayKey)
	}
}

func TestResolve_ClaudeCLIModelOverride(t *testing.T) {
	clearProviderEnv(t)
	enableFakeClaudeCLI(t, true)
	t.Setenv("LJ_LLM_MODEL", "sonnet")
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Model != "sonnet" {
		t.Errorf("Model=%q want sonnet", p.Model)
	}
}

func TestResolve_LLMEnvBeatsClaudeCLI(t *testing.T) {
	clearProviderEnv(t)
	enableFakeClaudeCLI(t, true)
	p, err := Resolve(config.Config{LLMBaseURL: "u", LLMAPIKey: "k", LLMModel: "m"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Source != "env" || p.Kind != backendHTTP {
		t.Errorf("explicit env key must win: %+v", p)
	}
}

func TestResolve_AnthropicEnvBeatsClaudeCLI(t *testing.T) {
	clearProviderEnv(t)
	enableFakeClaudeCLI(t, true)
	t.Setenv("ANTHROPIC_API_KEY", "ant-key")
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Source != "anthropic-env" {
		t.Errorf("ANTHROPIC_API_KEY must win: %+v", p)
	}
}

func TestResolve_ClaudeCLIBeatsOpencode(t *testing.T) {
	home := clearProviderEnv(t)
	enableFakeClaudeCLI(t, true)
	// opencode creds exist too — claude-cli must take priority.
	writeJSON(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		map[string]map[string]string{"zai-coding-plan": {"type": "api", "key": "zai-secret"}})
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Kind != backendClaudeCLI {
		t.Errorf("claude-cli must beat opencode: %+v", p)
	}
}

func TestResolve_ClaudeCLIDisabledFallsToOpencode(t *testing.T) {
	home := clearProviderEnv(t)
	enableFakeClaudeCLI(t, true)
	// Re-disable the backend; opencode creds should then resolve.
	t.Setenv(claudeCLIDisableEnv, "1")
	writeJSON(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		map[string]map[string]string{"zai-coding-plan": {"type": "api", "key": "zai-secret"}})
	writeJSON(t, filepath.Join(home, ".config", "opencode", "opencode.json"),
		map[string]string{"model": "zai-coding-plan/glm-5.2"})
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Source != "opencode" {
		t.Errorf("disabled claude-cli must fall through to opencode: %+v", p)
	}
}

func TestResolve_ClaudeCLINotAuthedFallsToOpencode(t *testing.T) {
	home := clearProviderEnv(t)
	enableFakeClaudeCLI(t, false) // present but not logged in
	writeJSON(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"),
		map[string]map[string]string{"zai-coding-plan": {"type": "api", "key": "zai-secret"}})
	writeJSON(t, filepath.Join(home, ".config", "opencode", "opencode.json"),
		map[string]string{"model": "zai-coding-plan/glm-5.2"})
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Source != "opencode" {
		t.Errorf("unauthenticated claude must fall through to opencode: %+v", p)
	}
}

func TestResolve_ClaudeCLINotOnPath(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv(claudeCLIDisableEnv, "")
	prevLook, prevAuth := claudeLookPath, claudeAuthStatus
	claudeLookPath = func(string) (string, error) { return "", os.ErrNotExist }
	claudeAuthStatus = func(string) bool { return true }
	t.Cleanup(func() { claudeLookPath, claudeAuthStatus = prevLook, prevAuth })
	_, err := Resolve(config.Config{})
	if err != ErrNoProvider {
		t.Fatalf("want ErrNoProvider, got %v", err)
	}
}

func TestResolve_ClaudeCLIFallsToError(t *testing.T) {
	// claude present but not authed, no opencode, no key => ErrNoProvider.
	clearProviderEnv(t)
	enableFakeClaudeCLI(t, false)
	_, err := Resolve(config.Config{})
	if err != ErrNoProvider {
		t.Fatalf("want ErrNoProvider, got %v", err)
	}
}
