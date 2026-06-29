package config

import "testing"

func TestLoad_LLMDelayDefault(t *testing.T) {
	t.Setenv("LJ_LLM_DELAY_SECONDS", "")
	c := Load()
	if c.LLMDelaySeconds != 2.0 {
		t.Errorf("default LLMDelaySeconds = %v, want 2.0", c.LLMDelaySeconds)
	}
}

func TestLoad_LLMDelayEnvOverride(t *testing.T) {
	t.Setenv("LJ_LLM_DELAY_SECONDS", "0.5")
	c := Load()
	if c.LLMDelaySeconds != 0.5 {
		t.Errorf("LLMDelaySeconds = %v, want 0.5", c.LLMDelaySeconds)
	}
}

func TestLoad_LLMDelayInvalidFallsBack(t *testing.T) {
	cases := []string{"not-a-number", "-3"}
	for _, v := range cases {
		t.Setenv("LJ_LLM_DELAY_SECONDS", v)
		c := Load()
		if c.LLMDelaySeconds != 2.0 {
			t.Errorf("LJ_LLM_DELAY_SECONDS=%q: got %v, want fallback 2.0", v, c.LLMDelaySeconds)
		}
	}
}

func TestLoad_LLMDelayZeroDisables(t *testing.T) {
	t.Setenv("LJ_LLM_DELAY_SECONDS", "0")
	c := Load()
	if c.LLMDelaySeconds != 0 {
		t.Errorf("explicit 0 should be honored, got %v", c.LLMDelaySeconds)
	}
}
