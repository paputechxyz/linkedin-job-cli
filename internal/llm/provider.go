package llm

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"linkedin-jobs/internal/config"
)

// backend kinds select how Chat() talks to the model.
const (
	backendHTTP      = ""           // default: OpenAI-compatible HTTP /chat/completions
	backendClaudeCLI = "claude-cli" // shell out to the `claude` CLI (Claude Code)
)

// Provider is a resolved OpenAI-compatible LLM provider. The HTTP client sends
// Authorization: Bearer <APIKey> plus any extra Headers (e.g. Anthropic's
// x-api-key / anthropic-version).
type Provider struct {
	BaseURL string
	APIKey  string
	Model   string
	Headers map[string]string
	Source  string // config | opencode | anthropic-env | env | claude-cli
	Kind    string // backendHTTP (default) | backendClaudeCLI

	// cliPath is the resolved `claude` binary path when Kind == backendClaudeCLI.
	// Empty for HTTP providers.
	cliPath string
}

// ErrNoProvider means no provider could be resolved.
var ErrNoProvider = errors.New("no LLM provider configured: set OPENAI_API_KEY / LJ_LLM_* / ANTHROPIC_API_KEY (or log in with `claude`)")

// providerPreset maps a known provider id to its OpenAI-compatible endpoint.
// injectKeyHeader names a header that should carry the API key in addition to
// (or instead of relying on) Bearer auth.
type providerPreset struct {
	baseURL         string
	headers         map[string]string
	injectKeyHeader string
	model           string
}

// defaultModel is the fallback model when a provider is resolved from a
// redirected ANTHROPIC_BASE_URL and no opencode model is discoverable.
const defaultModel = "gpt-4o-mini"

var presets = map[string]providerPreset{
	"zai":             {"https://api.z.ai/api/paas/v4", nil, "", "glm-4.5"},
	"zai-coding-plan": {"https://api.z.ai/api/coding/paas/v4", nil, "", "glm-5.2"},
	"anthropic":       {"https://api.anthropic.com/v1", map[string]string{"anthropic-version": "2023-06-01"}, "x-api-key", "claude-3-5-sonnet-latest"},
}

// Apply sets auth + extra headers on an HTTP request.
func (p *Provider) Apply(req *http.Request) {
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	for k, v := range p.Headers {
		req.Header.Set(k, v)
	}
}

// Resolve resolves a provider in priority order (most-explicit first):
//  1. LJ_LLM_* / OPENAI_* env (explicit env override)
//  2. ANTHROPIC_API_KEY env (Anthropic preset; honors a redirected
//     ANTHROPIC_BASE_URL, e.g. an opencode/Hermes session pointing it at z.ai)
//  3. claude CLI (reuses a logged-in Claude Code session's subscription)
//  4. opencode stored credentials (implicit discovery)
//  5. ErrNoProvider
//
// Resolution is env-driven only — there is no persisted provider file. cfg
// supplies the env-layer values.
func Resolve(cfg config.Config) (*Provider, error) {
	if cfg.LLMAPIKey != "" {
		return &Provider{
			BaseURL: cfg.LLMBaseURL,
			APIKey:  cfg.LLMAPIKey,
			Model:   cfg.LLMModel,
			Source:  "env",
		}, nil
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		if base := os.Getenv("ANTHROPIC_BASE_URL"); base != "" {
			model := defaultModel
			if m := readOpencodeModel(); m != "" {
				if parts := strings.SplitN(m, "/", 2); len(parts) == 2 && parts[1] != "" {
					model = parts[1]
				} else {
					model = m
				}
			}
			return &Provider{BaseURL: base, APIKey: key, Model: model, Source: "anthropic-env"}, nil
		}
		p := buildProvider(presets["anthropic"], key)
		p.Source = "anthropic-env"
		return p, nil
	}
	if p, ok := FromClaudeCLI(); ok {
		return p, nil
	}
	if p, ok := FromOpencode(); ok {
		return p, nil
	}
	return nil, ErrNoProvider
}

// FromOpencode reads opencode's stored credentials + model config and maps them
// to a Provider via the preset registry. Returns ok=false if opencode is not
// configured or the provider id is unknown to the registry.
func FromOpencode() (*Provider, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, false
	}
	authPath := filepath.Join(home, ".local", "share", "opencode", "auth.json")
	data, err := os.ReadFile(authPath)
	if err != nil {
		return nil, false
	}
	var auth map[string]struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	if json.Unmarshal(data, &auth) != nil || len(auth) == 0 {
		return nil, false
	}
	// Prefer the provider named in opencode.json's "model" ("provider/model").
	provID := ""
	modelName := ""
	if m := readOpencodeModel(); m != "" {
		if parts := strings.SplitN(m, "/", 2); len(parts) == 2 {
			provID, modelName = parts[0], parts[1]
		}
	}
	cred, ok := auth[provID]
	if !ok || cred.Key == "" {
		// fall back to the first stored credential
		provID, cred = "", struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		}{}
		for k, v := range auth {
			provID, cred = k, v
			break
		}
		if cred.Key == "" {
			return nil, false
		}
	}
	preset, ok := presets[provID]
	if !ok {
		// We have a key but no known base URL for this provider id.
		return nil, false
	}
	p := buildProvider(preset, cred.Key)
	if modelName != "" {
		p.Model = modelName
	}
	p.Source = "opencode"
	return p, true
}

func readOpencodeModel() string {
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, ".config", "opencode", "opencode.json"),
		filepath.Join(home, ".opencode", "opencode.json"),
	} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var v struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(data, &v) == nil && v.Model != "" {
			return v.Model
		}
	}
	return ""
}

func buildProvider(p providerPreset, key string) *Provider {
	headers := map[string]string{}
	for k, v := range p.headers {
		headers[k] = v
	}
	if p.injectKeyHeader != "" {
		headers[p.injectKeyHeader] = key
	}
	return &Provider{BaseURL: p.baseURL, APIKey: key, Model: p.model, Headers: headers}
}

// Redacted returns the API key with only the last 4 chars visible, for display.
// The claude-cli backend carries no real secret (its key is a synthetic label),
// so it is returned verbatim.
func (p *Provider) Redacted() string {
	if p.Kind == backendClaudeCLI {
		return p.APIKey
	}
	k := p.APIKey
	if len(k) <= 4 {
		return strings.Repeat("*", len(k))
	}
	return strings.Repeat("*", len(k)-4) + k[len(k)-4:]
}
