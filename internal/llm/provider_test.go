package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"linkedin-jobs/internal/config"
)

func clearProviderEnv(t *testing.T) (string, string) {
	t.Helper()
	cfgDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("LJ_CONFIG_DIR", cfgDir)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("LJ_LLM_API_KEY", "")
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", home)
	} else {
		t.Setenv("USERPROFILE", home)
	}
	return cfgDir, home
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

func TestResolve_FromConfigFile(t *testing.T) {
	clearProviderEnv(t)
	writeJSON(t, filepath.Join(config.ConfigDir(), "config.json"),
		configFile{BaseURL: "https://example.com/v1", APIKey: "sk-config", Model: "m"})
	p, err := Resolve(config.Config{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Source != "config" || p.APIKey != "sk-config" || p.BaseURL != "https://example.com/v1" {
		t.Errorf("unexpected provider: %+v", p)
	}
}

func TestResolve_FromOpencode(t *testing.T) {
	_, home := clearProviderEnv(t)
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
// opencode discovery (otherwise users cannot override the discovered provider
// without running the wizard).
func TestResolve_LLMEnvBeatsOpencode(t *testing.T) {
	_, home := clearProviderEnv(t)
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
	_, home := clearProviderEnv(t)
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

func TestSave_RoundTripAndMode(t *testing.T) {
	cfgDir, _ := clearProviderEnv(t)
	in := &Provider{BaseURL: "https://example.com/v1", APIKey: "sk-roundtrip", Model: "m"}
	if err := Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(filepath.Join(cfgDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("config file mode = %o, want 0600", mode)
	}
	p, ok := fromConfigFile()
	if !ok || p.APIKey != "sk-roundtrip" || p.BaseURL != "https://example.com/v1" {
		t.Errorf("round-trip failed: %+v ok=%v", p, ok)
	}
}

func TestNewAnthropicProvider(t *testing.T) {
	p := NewAnthropicProvider("claude-key")
	if p.Headers["x-api-key"] != "claude-key" {
		t.Errorf("x-api-key not injected: %+v", p.Headers)
	}
	if p.Headers["anthropic-version"] == "" {
		t.Errorf("anthropic-version missing")
	}
	if !strings.HasPrefix(p.BaseURL, "https://api.anthropic.com") {
		t.Errorf("base URL=%q", p.BaseURL)
	}
}

func TestRedacted(t *testing.T) {
	p := &Provider{APIKey: "sk-abcdefgh"} // 11 chars -> 7 stars + last 4
	if r := p.Redacted(); r != "*******efgh" {
		t.Errorf("Redacted=%q want *******efgh", r)
	}
}
