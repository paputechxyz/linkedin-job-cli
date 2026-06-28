package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"linkedin-jobs/internal/config"
	"linkedin-jobs/internal/models"
)

const systemPrompt = "You are an expert technical recruiter assistant. Summarize job postings concisely for a senior engineer evaluating opportunities."

const summaryPrompt = `Summarize this job posting in 4-6 bullet points. Focus on:
- Role & seniority level
- Key technical requirements / stack
- Team & scope (IC, leadership, product area)
- Notable perks, compensation details, or highlights
- Remote/hybrid/onsite and location

Keep each bullet under 15 words. Be specific — no generic filler.

Job Title: %s
Company: %s
Location: %s
Salary: %s
Description:
%s`

// Summarize summarizes a job posting using an LLM, or falls back to an
// extractive summary when no API key is configured or the call fails.
func Summarize(j *models.JobPosting, cfg config.Config) string {
	if strings.TrimSpace(j.Description) == "" {
		return "No description available."
	}
	if cfg.LLMAPIKey != "" {
		if s, err := llmSummarize(j, cfg); err == nil {
			return s
		}
	}
	return Extractive(j.Description)
}

func llmSummarize(j *models.JobPosting, cfg config.Config) (string, error) {
	desc := j.Description
	if len(desc) > 4000 {
		desc = desc[:4000]
	}
	prompt := fmt.Sprintf(summaryPrompt, j.Title, orNA(j.Company), orNA(j.Location), j.SalaryDisplay(), desc)

	reqBody := map[string]interface{}{
		"model":       cfg.LLMModel,
		"max_tokens":  400,
		"temperature": 0.3,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": prompt},
		},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(cfg.LLMBaseURL, "/") + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.LLMAPIKey)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("LLM API status %d: %s", resp.StatusCode, string(body))
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

var (
	extractKW = regexp.MustCompile(`(?i)(responsibilit|require|qualif|experience|skill|technolog|stack|platform|year.*experience|degree|remote|hybrid|team|product|design|build|architect|salary|compensation|benefit|equity|bonus)`)
	stripLead = regexp.MustCompile(`(?i)^(What you'|What You'|Your |The |As a )`)
)

// Extractive builds a rule-based summary from the description (no LLM key).
func Extractive(description string) string {
	lines := strings.Split(description, "\n")
	var bullets []string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "" || len(t) >= 300 {
			continue
		}
		if extractKW.MatchString(t) && len(bullets) < 5 {
			bullets = append(bullets, "  • "+stripLead.ReplaceAllString(t, ""))
		}
	}
	if len(bullets) == 0 {
		text := strings.Join(firstN(lines, 5), " ")
		sentences := splitSentences(text)
		for _, s := range sentences {
			if len(s) > 20 && len(bullets) < 5 {
				bullets = append(bullets, "  • "+s)
			}
		}
	}
	header := "Summary (extractive — no LLM key configured):\n"
	return header + strings.Join(bullets, "\n")
}

func firstN(lines []string, n int) []string {
	if len(lines) > n {
		return lines[:n]
	}
	return lines
}

func splitSentences(s string) []string {
	var out []string
	cur := strings.Builder{}
	for _, r := range s {
		cur.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			if t := strings.TrimSpace(cur.String()); t != "" {
				out = append(out, t)
			}
			cur.Reset()
		}
	}
	if t := strings.TrimSpace(cur.String()); t != "" {
		out = append(out, t)
	}
	return out
}

func orNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}
