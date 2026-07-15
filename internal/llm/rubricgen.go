package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"linkedin-jobs/internal/config"
)

const rubricGenSystem = "You are helping configure a job-fit scoring tool. You extract structured scoring rubrics from a user's free-text preferences paragraph."

const rubricGenPrompt = `From the preferences paragraph below, extract the user's job criteria and return ONLY a JSON object (no prose, no code fences) with EXACTLY these keys:

"rubrics": an array of scoring criteria the user cares about. Each element has:
  - "id": a short snake_case identifier (e.g. "preferred_tech", "free_snacks", "avoided_tech", "ai_intensity"),
  - "description": one phrase on what to look for in a job posting,
  - "items": a list of strings, ONLY when the criterion is a list of things (e.g. preferred tech, avoided tech). Omit "items" for single criteria like "free snacks" or "startup stage".
  Do NOT generate rubrics for salary or work arrangement — those are system rubrics scored automatically. Extract them as the structured fields below instead. Group list-type criteria into ONE rubric with all items (e.g. one "preferred_tech" rubric, NOT one rubric per technology).

"work_arrangement": list of preferred arrangements among remote/hybrid/onsite (only those the paragraph mentions),
"min_salary": a number for the salary floor, or null if none stated,
"min_salary_currency": one of USD/CAD/EUR/GBP if inferable, else null,
"preferred_tech": list of preferred technologies (also emitted as a rubric),
"avoided_tech": list of technologies to penalize (also emitted as a rubric).

Only include keys the paragraph actually mentions; omit a key rather than guessing. A vague phrase like "high salary" with no number must produce null for min_salary (the tool will ask the user).

Paragraph:
%s`

// GenResult is the LLM's extraction from a preferences paragraph.
type GenResult struct {
	Rubrics           []GenRubric `json:"rubrics"`
	WorkArrangement   []string    `json:"work_arrangement"`
	MinSalary         *float64    `json:"min_salary"`
	MinSalaryCurrency string      `json:"min_salary_currency"`
	PreferredTech     []string    `json:"preferred_tech"`
	AvoidedTech       []string    `json:"avoided_tech"`
}

// GenRubric is one LLM-proposed rubric (always dynamic; the tool assigns weights).
type GenRubric struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Items       []string `json:"items"`
}

// GenerateRubrics extracts rubrics + structured profile params from a paragraph.
// Used by setup/reset. The returned rubrics are dynamic (weight is assigned by
// the caller). A transport failure returns an error; an unparseable response
// returns a zero result with the raw text in the error.
func GenerateRubrics(paragraph string, provider *Provider) (GenResult, error) {
	content, err := Chat(provider, rubricGenSystem, fmt.Sprintf(rubricGenPrompt, paragraph), 2048, 0.2)
	if err != nil {
		return GenResult{}, err
	}
	jstr := extractJSON(content)
	if jstr == "" {
		return GenResult{}, fmt.Errorf("could not parse rubrics from LLM response: %s", truncateForError(content))
	}
	var res GenResult
	if err := json.Unmarshal([]byte(jstr), &res); err != nil {
		return GenResult{}, fmt.Errorf("invalid rubrics JSON: %w", err)
	}
	return res, nil
}

// AmendRubrics takes the existing rubric set and a follow-up paragraph, and
// returns ONLY the rubrics to add or change (keyed by id). The caller merges
// them onto the existing set so unmentioned rubrics are preserved untouched.
// Weight-only edits return the rubric id with the new weight.
type amendChange struct {
	ID          string   `json:"id"`
	Description string   `json:"description,omitempty"`
	Items       []string `json:"items,omitempty"`
	Weight      int      `json:"weight,omitempty"`
}

const amendPrompt = `Here is the user's current set of scoring rubrics (JSON):
%s

The user wants to amend them with this follow-up paragraph:
%s

Return ONLY a JSON array (no prose, no code fences) of the rubrics to ADD or CHANGE. For each, include "id", and whichever of "description", "items", and "weight" apply. Do NOT include rubrics the paragraph does not mention — they must be preserved unchanged. A weight edit returns just {"id": "...", "weight": N}. A new rubric returns id, description, and items if it is a list.`

// GenerateAmend returns the rubric changes implied by a follow-up paragraph
// against the existing set. The caller merges them (MergeRubrics) so untouched
// rubrics survive.
func GenerateAmend(existing []config.Rubric, paragraph string, provider *Provider) ([]amendChange, error) {
	existingJSON, _ := json.Marshal(existing)
	content, err := Chat(provider, rubricGenSystem,
		fmt.Sprintf(amendPrompt, string(existingJSON), paragraph), 2048, 0.2)
	if err != nil {
		return nil, err
	}
	jstr := extractJSON(content)
	if jstr == "" {
		return nil, fmt.Errorf("could not parse amend response: %s", truncateForError(content))
	}
	// The response may be a bare array or wrapped in an object; unwrap "rubrics".
	if strings.TrimSpace(jstr) != "" && jstr[0] == '{' {
		var wrap struct {
			Rubrics []amendChange `json:"rubrics"`
		}
		if err := json.Unmarshal([]byte(jstr), &wrap); err == nil && wrap.Rubrics != nil {
			return wrap.Rubrics, nil
		}
	}
	var changes []amendChange
	if err := json.Unmarshal([]byte(jstr), &changes); err != nil {
		return nil, fmt.Errorf("invalid amend JSON: %w", err)
	}
	return changes, nil
}
