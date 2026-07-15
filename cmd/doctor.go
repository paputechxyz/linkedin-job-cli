package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/llm"
)

var doctorPing bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose config: LLM provider, settings.yaml completeness, env vars",
	Long: `Verify everything the CLI needs is in place.

Checks (in order):
  1. LLM provider resolves (base URL + model). With --ping, also sends a tiny
     "hello" call to confirm the API key works end-to-end.
  2. settings.yaml present and complete (every documented key set)
  3. All known env vars reported (keys printed, secret values redacted)

Exits 1 if any check fails, 0 if all pass.`,
	Run: func(cmd *cobra.Command, args []string) {
		ok := true
		fmt.Println("linkedin-jobs doctor")
		fmt.Println()

		// 1. LLM provider (+ optional live ping).
		fmt.Println("== LLM ==")
		cfg := loadCfg()
		p, err := llm.Resolve(cfg)
		if err != nil {
			ok = false
			fmt.Printf("  [✗] no provider resolved: %v\n", err)
			fmt.Println("      fix: set OPENAI_API_KEY / LJ_LLM_* / ANTHROPIC_API_KEY (or rely on opencode discovery)")
		} else {
			fmt.Printf("  [✓] provider resolved: source=%s model=%s base=%s key=%s\n",
				p.Source, p.Model, p.BaseURL, p.Redacted())
			if doctorPing {
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
			} else {
				// --ping not set: stay quiet rather than advertising the flag on every run.
			}
		}
		fmt.Println()

		// 2. Settings completeness.
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

		// 3. Env vars (keys printed, secret values redacted).
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
	"LJ_COOKIES_FILE",
	"LJ_COOKIE",
	"OPENAI_API_KEY",
	"OPENAI_BASE_URL",
	"LJ_LLM_API_KEY",
	"LJ_LLM_BASE_URL",
	"LJ_LLM_MODEL",
	"LJ_LLM_DELAY_SECONDS",
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_BASE_URL",
}

var settingsTopSchema = map[string][]string{
	"scoring": {"rubrics"},
	"profile": {"work_arrangement", "min_salary", "min_salary_currency", "locations", "preferred_tech", "avoided_tech"},
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
			// rubrics is the load-bearing key now; flag an empty list too,
			// since defaults only inject the 3 system rubrics on load.
			if r, ok := sub["rubrics"].([]interface{}); ok && len(r) == 0 {
				missing = append(missing, "scoring.rubrics (empty — run 'setup')")
			}
		}
	}
	return missing, nil
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorPing, "ping", false, "send a live \"hello\" call to the resolved LLM (default: only check base URL + model)")
	rootCmd.AddCommand(doctorCmd)
}
