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

// Provider is a resolved OpenAI-compatible LLM provider. The HTTP client sends
// Authorization: Bearer <APIKey> plus any extra Headers (e.g. Anthropic's
// x-api-key / anthropic-version).
type Provider struct {
	BaseURL string
	APIKey  string
	Model   string
	Headers map[string]string
	Source  string // config | opencode | anthropic-env | env
}

// ErrNoProvider means no provider could be resolved.
var ErrNoProvider = errors.New("no LLM provider configured: run `linkedin-jobs config`, or set OPENAI_API_KEY / LJ_LLM_* / ANTHROPIC_API_KEY")

// configFile is the on-disk shape of ~/.linkedin-jobs/config.json.
type configFile struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

// ConfigPath returns the resolved path to the provider config file.
func ConfigPath() string {
	return filepath.Join(config.ConfigDir(), "config.json")
}

// providerPreset maps a known provider id to its OpenAI-compatible endpoint.
// injectKeyHeader names a header that should carry the API key in addition to
// (or instead of relying on) Bearer auth.
type providerPreset struct {
	baseURL         string
	headers         map[string]string
	injectKeyHeader string
	model           string
}

var presets = map[string]providerPreset{
	"zai":             {"https://api.z.ai/api/paas/v4", nil, "", "glm-4.5"},
	"zai-coding-plan": {"https://api.z.ai/api/paas/v4", nil, "", "glm-4.5"},
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

// Resolve resolves a provider in priority order (KTD6):
//  1. persisted config file
//  2. opencode stored credentials
//  3. ANTHROPIC_API_KEY env (Anthropic OpenAI-compat)
//  4. LJ_LLM_* / OPENAI_* env (existing config)
//  5. ErrNoProvider
//
// Resolution is non-interactive; the interactive wizard lives in the `config`
// command. cfg supplies the env-layer values.
func Resolve(cfg config.Config) (*Provider, error) {
	if p, ok := fromConfigFile(); ok {
		p.Source = "config"
		return p, nil
	}
	if p, ok := FromOpencode(); ok {
		return p, nil
	}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		p := buildProvider(presets["anthropic"], key)
		p.Source = "anthropic-env"
		return p, nil
	}
	if cfg.LLMAPIKey != "" {
		return &Provider{
			BaseURL: cfg.LLMBaseURL,
			APIKey:  cfg.LLMAPIKey,
			Model:   cfg.LLMModel,
			Source:  "env",
		}, nil
	}
	return nil, ErrNoProvider
}

func fromConfigFile() (*Provider, bool) {
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		return nil, false
	}
	var cf configFile
	if err := json.Unmarshal(data, &cf); err != nil || cf.APIKey == "" {
		return nil, false
	}
	return &Provider{BaseURL: cf.BaseURL, APIKey: cf.APIKey, Model: cf.Model}, true
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

// Save persists a provider to config.json with mode 0600 (secret at rest).
func Save(p *Provider) error {
	if err := os.MkdirAll(config.ConfigDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(configFile{BaseURL: p.BaseURL, APIKey: p.APIKey, Model: p.Model}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(), append(data, '\n'), 0o600)
}

// Redacted returns the API key with only the last 4 chars visible, for display.
func (p *Provider) Redacted() string {
	k := p.APIKey
	if len(k) <= 4 {
		return strings.Repeat("*", len(k))
	}
	return strings.Repeat("*", len(k)-4) + k[len(k)-4:]
}

// NewAnthropicProvider builds a Provider for an Anthropic/Claude key using the
// Anthropic OpenAI-compatible endpoint. Used by the connect wizard.
func NewAnthropicProvider(key string) *Provider {
	p := buildProvider(presets["anthropic"], key)
	p.Source = "config"
	return p
}
