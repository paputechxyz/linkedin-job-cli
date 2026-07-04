package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
	"linkedin-jobs/internal/profile"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose config: LLM ping, resume, settings.yaml completeness, env vars",
	Long: `Verify everything the CLI needs is in place.

Checks (in order):
  1. LLM provider resolves and answers a tiny "hello" call
  2. Resume loaded from RESUME.md
  3. settings.yaml present and complete (every documented key set)
  4. All known env vars reported (keys printed, secret values redacted)

Exits 1 if any check fails, 0 if all pass.`,
	Run: func(cmd *cobra.Command, args []string) {
		ok := true
		fmt.Println("linkedin-jobs doctor")
		fmt.Println()

		// 1. LLM provider + live ping.
		fmt.Println("== LLM ==")
		cfg := loadCfg()
		p, err := llm.Resolve(cfg)
		if err != nil {
			ok = false
			fmt.Printf("  [✗] no provider resolved: %v\n", err)
			fmt.Println("      fix: linkedin-jobs config llm  (or set OPENAI_API_KEY / LJ_LLM_* / ANTHROPIC_API_KEY)")
		} else {
			fmt.Printf("  [✓] provider resolved: source=%s model=%s base=%s key=%s\n",
				p.Source, p.Model, p.BaseURL, p.Redacted())
			// Reasoning models (e.g. GLM-5.2) spend tokens on internal
			// thinking before emitting visible content, so give the call
			// enough room to actually produce an answer.
			reply, err := llm.Chat(p,
				"You are a connectivity health check.",
				"Reply with exactly one word: hello",
				128, 0)
			if err != nil {
				ok = false
				fmt.Printf("  [✗] LLM call failed: %v\n", err)
			} else if reply = strings.TrimSpace(reply); reply == "" {
				fmt.Printf("  [~] LLM responded with empty content (API reachable, but model returned no text)\n")
			} else {
				fmt.Printf("  [✓] LLM responded: %q\n", reply)
			}
		}
		fmt.Println()

		// 2. Resume.
		fmt.Println("== Resume ==")
		resumePath := profile.ResumePath()
		if info, err := os.Stat(resumePath); err != nil || info.Size() == 0 {
			ok = false
			fmt.Printf("  [✗] no resume at %s\n", resumePath)
			fmt.Println("      fix: linkedin-jobs profile resume  (paste text, end with Ctrl-D)")
		} else {
			fmt.Printf("  [✓] %s (%s)\n", resumePath, humanBytes(info.Size()))
		}
		fmt.Println()

		// 3. Settings completeness.
		fmt.Println("== Settings ==")
		sp := config.SettingsPath()
		missing, err := checkSettings(sp)
		if err != nil {
			ok = false
			fmt.Printf("  [✗] %v\n", err)
			fmt.Println("      using built-in defaults; create the file to make this check pass")
		} else if len(missing) == 0 {
			fmt.Printf("  [✓] %s — all expected keys present\n", sp)
		} else {
			ok = false
			fmt.Printf("  [~] %s — missing keys:\n", sp)
			for _, m := range missing {
				fmt.Printf("        - %s\n", m)
			}
		}
		fmt.Println()

		// 4. Env vars (keys printed, secret values redacted).
		fmt.Println("== Environment ==")
		for _, k := range doctorEnvKeys {
			v, set := os.LookupEnv(k)
			if !set {
				fmt.Printf("  %-22s = (unset)\n", k)
				continue
			}
			fmt.Printf("  %-22s = %s\n", k, redactEnv(k, v))
		}

		fmt.Println()
		if !ok {
			os.Exit(1)
		}
	},
}

var doctorEnvKeys = []string{
	"LJ_DB_PATH",
	"LJ_CONFIG_DIR",
	"LJ_COOKIES_FILE",
	"LJ_COOKIE",
	"OPENAI_API_KEY",
	"OPENAI_BASE_URL",
	"LJ_LLM_API_KEY",
	"LJ_LLM_BASE_URL",
	"LJ_LLM_MODEL",
	"LJ_LLM_DELAY_SECONDS",
	"ANTHROPIC_API_KEY",
}

var settingsTopSchema = map[string][]string{
	"stats":   {"top_companies_limit"},
	"filter":  {"auto_filter"},
	"scoring": {"reason_threshold", "baseline", "deal_breaker_cap", "deal_breakers", "weights"},
	"enrich":  {"auto_enrich_on_save"},
	"profile": {"work_arrangement", "min_salary", "min_salary_currency", "locations", "preferred_tech", "avoided_tech"},
}

var weightsKeys = []string{
	"salary", "tech_overlap", "startup", "ai_intensity", "compensation_extras", "remote_tiebreak",
}

// redactEnv prints the value verbatim for clearly non-secret keys (paths, URLs,
// model names, numeric delays) and redacts to last-4-visible for anything that
// looks like a secret (KEY/SECRET/TOKEN/COOKIE/PASSWORD).
func redactEnv(k, v string) string {
	upper := strings.ToUpper(k)
	secret := strings.Contains(upper, "KEY") ||
		strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "TOKEN") ||
		strings.Contains(upper, "COOKIE") ||
		strings.Contains(upper, "PASSWORD")
	if !secret {
		return v
	}
	if len(v) <= 4 {
		return strings.Repeat("*", len(v))
	}
	return strings.Repeat("*", len(v)-4) + v[len(v)-4:]
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// checkSettings reads settings.yaml and returns the list of expected keys that
// are absent. An error is returned only if the file is missing or unreadable;
// missing keys are advisory (defaults fill them in at runtime).
func checkSettings(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no settings file at %s", path)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	var missing []string
	for section, keys := range settingsTopSchema {
		sub, ok := root[section].(map[string]interface{})
		if !ok {
			missing = append(missing, section)
			continue
		}
		for _, k := range keys {
			if _, present := sub[k]; !present {
				missing = append(missing, section+"."+k)
			}
		}
		if section == "scoring" {
			if w, ok := sub["weights"].(map[string]interface{}); ok {
				for _, wk := range weightsKeys {
					if _, present := w[wk]; !present {
						missing = append(missing, "scoring.weights."+wk)
					}
				}
			}
		}
	}
	return missing, nil
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
