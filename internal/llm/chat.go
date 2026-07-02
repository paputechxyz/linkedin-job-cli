package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Chat sends a single-turn chat completion (one system + one user message) to
// the provider's OpenAI-compatible /chat/completions endpoint and returns the
// assistant message content. Used by enrichment and the `hr` outreach research.
// An HTTP failure returns an error (body truncated + scrubbed); an empty
// choices array returns a descriptive error.
func Chat(p *Provider, system, user string, maxTokens int, temperature float64) (string, error) {
	if maxTokens <= 0 {
		maxTokens = 2048
	}
	reqBody := map[string]interface{}{
		"model":       p.Model,
		"max_tokens":  maxTokens,
		"temperature": temperature,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	p.Apply(req)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("LLM API status %d: %s", resp.StatusCode, truncateForError(string(body)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("LLM returned no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
