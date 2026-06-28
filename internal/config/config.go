package config

import (
	"os"
	"path/filepath"
)

// Config holds runtime configuration resolved from the environment.
type Config struct {
	DBPath string

	// LLM (OpenAI-compatible)
	LLMBaseURL string
	LLMAPIKey  string
	LLMModel   string

	// Scraping
	UserAgent             string
	RequestTimeoutSeconds int
	DetailDelaySeconds    float64

	// LinkedIn session (for recommended). Press-auth is preferred; these are fallbacks.
	CookiesFile  string // path to a file holding a raw "Cookie:" header value or Netscape cookies.txt
	CookieHeader string // raw cookie header override (env LJ_COOKIE)
}

// Load resolves configuration from the environment with sensible defaults.
func Load() Config {
	c := Config{
		DBPath:                envOr("LJ_DB_PATH", filepath.Join(cwd(), "linkedin_jobs.db")),
		LLMBaseURL:            envOr("LJ_LLM_BASE_URL", envOr("OPENAI_BASE_URL", "https://api.openai.com/v1")),
		LLMAPIKey:             envOr("LJ_LLM_API_KEY", envOr("OPENAI_API_KEY", "")),
		LLMModel:              envOr("LJ_LLM_MODEL", "gpt-4o-mini"),
		UserAgent:             defaultUA(),
		RequestTimeoutSeconds: 20,
		DetailDelaySeconds:    0.8,
		CookiesFile:           os.Getenv("LJ_COOKIES_FILE"),
		CookieHeader:          os.Getenv("LJ_COOKIE"),
	}
	return c
}

// WithDBPath returns a copy with the DB path overridden (used by --db flag).
func (c Config) WithDBPath(p string) Config {
	c.DBPath = p
	return c
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func cwd() string {
	d, err := os.Getwd()
	if err != nil {
		return "."
	}
	return d
}

func defaultUA() string {
	return "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
}
