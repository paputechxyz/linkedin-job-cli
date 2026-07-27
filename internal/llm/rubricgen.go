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
  - "applies_to": an OPTIONAL list of work arrangements this rubric should be scored against, drawn from "remote", "hybrid", "onsite". Emit it ONLY when the user's constraint is conditional on arrangement. The canonical case is a location-constraint rubric that applies to hybrid/onsite but not remote (e.g. "hybrid must be in Toronto" → applies_to: ["hybrid", "onsite"]; a remote job is location-agnostic). If the constraint applies to every arrangement, omit "applies_to".
  Do NOT generate rubrics for salary or work arrangement — those are system rubrics scored automatically. Extract them as the structured fields below instead. Group list-type criteria into ONE rubric with all items (e.g. one "preferred_tech" rubric, NOT one rubric per technology).

"work_arrangement": list of preferred arrangements among remote/hybrid/onsite (only those the paragraph mentions),
"min_salary": a number for the salary floor, or null if none stated,
"min_salary_currency": one of USD/CAD/EUR/GBP/AUD/INR/JPY if the paragraph explicitly states it, else null (the tool will infer from location),
"location": a single string naming the user's preferred city AND/OR country (e.g. "Toronto, ON, Canada", "San Francisco, CA, USA", "London, UK", "Berlin, Germany"). Empty string "" when the paragraph does not mention a location. This drives salary-band selection and currency inference, so prefer the most specific city + country the paragraph states.
"preferred_tech": list of preferred technologies (also emitted as a rubric),
"avoided_tech": list of technologies to penalize (also emitted as a rubric).

Only include keys the paragraph actually mentions; omit a key rather than guessing. A vague phrase like "high salary" with no number must produce null for min_salary (the tool will ask the user). Location must always be a string (use "" rather than null when not stated) so it round-trips cleanly through YAML.

Paragraph:
%s`

// GenResult is the LLM's extraction from a preferences paragraph.
type GenResult struct {
	Rubrics           []GenRubric `json:"rubrics"`
	WorkArrangement   []string    `json:"work_arrangement"`
	MinSalary         *float64    `json:"min_salary"`
	MinSalaryCurrency string      `json:"min_salary_currency"`
	Location          string      `json:"location"`
	PreferredTech     []string    `json:"preferred_tech"`
	AvoidedTech       []string    `json:"avoided_tech"`
}

// GenRubric is one LLM-proposed rubric (always dynamic; the tool assigns weights).
type GenRubric struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Items       []string `json:"items"`
	AppliesTo   []string `json:"applies_to"`
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
	AppliesTo   []string `json:"applies_to,omitempty"`
	Weight      int      `json:"weight,omitempty"`
}

const amendPrompt = `Here is the user's current set of scoring rubrics (JSON):
%s

Here is the user's current structured profile (JSON):
%s

The user wants to amend them with this follow-up paragraph:
%s

Return ONLY a JSON object (no prose, no code fences) describing the rubrics and
structured profile fields that should CHANGE. OMIT any key the paragraph does
not mention — unmentioned keys are preserved untouched by the caller. The
object uses EXACTLY these keys when present:

"rubrics": array of rubrics to ADD or CHANGE. Each element has:
  - "id": a short snake_case identifier (existing id = change; new id = add),
  - "description": one phrase on what to look for,
  - "items": list of strings, ONLY for list-type criteria (omit otherwise),
  - "applies_to": OPTIONAL list of arrangements from remote/hybrid/onsite, only
    when the rubric should be scored conditionally (e.g. a hybrid-only location
    constraint has applies_to: ["hybrid","onsite"]; an empty array [] clears it),
  - "weight": OPTIONAL new weight (1-10).

"work_arrangement": new full list of preferred arrangements among remote/hybrid/onsite (replaces the existing list),
"min_salary": new salary floor as a number,
"min_salary_currency": one of USD/CAD/EUR/GBP/AUD/INR/JPY,
"location": new preferred location as a single string (e.g. "Seattle, WA, USA"),
"preferred_tech": new full list of preferred technologies (replaces the existing list),
"avoided_tech": new full list of technologies to penalize (replaces the existing list).

Salary and work_arrangement are SYSTEM rubrics scored from the structured
fields — do NOT add them as rubrics; emit them as min_salary / work_arrangement
/ min_salary_currency / location instead. The structured fields are REPLACE
semantics: if the paragraph names a new city or salary, the new value replaces
the old one (not appended).`

// AmendResult captures both rubric changes and structured profile field changes
// extracted from a follow-up paragraph. Only fields the paragraph mentioned are
// populated; the caller preserves everything else. Pointer / slice fields use
// nil/empty as "paragraph did not mention".
type AmendResult struct {
	Rubrics           []amendChange `json:"rubrics"`
	WorkArrangement   []string      `json:"work_arrangement"`
	MinSalary         *float64      `json:"min_salary"`
	MinSalaryCurrency string        `json:"min_salary_currency"`
	Location          string        `json:"location"`
	PreferredTech     []string      `json:"preferred_tech"`
	AvoidedTech       []string      `json:"avoided_tech"`
}

// GenerateAmend returns the rubric + structured-profile changes implied by a
// follow-up paragraph against the existing settings. The caller merges each
// portion (MergeRubrics for rubrics, field-by-field for profile) so untouched
// rubrics and profile fields survive.
func GenerateAmend(existing []config.Rubric, profile config.ProfileSettings, paragraph string, provider *Provider) (AmendResult, error) {
	existingJSON, _ := json.Marshal(existing)
	profileJSON, _ := json.Marshal(profile)
	content, err := Chat(provider, rubricGenSystem,
		fmt.Sprintf(amendPrompt, string(existingJSON), string(profileJSON), paragraph), 2048, 0.2)
	if err != nil {
		return AmendResult{}, err
	}
	jstr := extractJSON(content)
	if jstr == "" {
		return AmendResult{}, fmt.Errorf("could not parse amend response: %s", truncateForError(content))
	}
	res, err := parseAmendResult(jstr)
	if err != nil {
		return AmendResult{}, fmt.Errorf("invalid amend JSON: %w", err)
	}
	return res, nil
}

// parseAmendResult accepts the shapes an LLM may emit for an amend response:
// the canonical wrapper object {"rubrics":[...], "location":"...", ...}, a bare
// JSON array [...], a wrapper object with only {"rubrics":[...]}, or
// (defensively) a single bare object describing one rubric change. All are
// normalized into an AmendResult.
func parseAmendResult(jstr string) (AmendResult, error) {
	trimmed := strings.TrimSpace(jstr)
	if trimmed == "" {
		return AmendResult{}, fmt.Errorf("empty response")
	}
	// Bare array form: treat as rubric-only changes (back-compat for older
	// prompts and LLMs that return only rubric edits).
	if trimmed[0] == '[' {
		var changes []amendChange
		if err := json.Unmarshal([]byte(jstr), &changes); err != nil {
			return AmendResult{}, err
		}
		return AmendResult{Rubrics: changes}, nil
	}
	// Object form. Unmarshal into the full AmendResult; if nothing populated,
	// fall through to single-bare-object, then comma-separated recovery before
	// finally rejecting. (A single bare rubric object like {"id":"...","weight":N}
	// unmarshals to an empty AmendResult because Go's decoder ignores unknown
	// fields — so we cannot reject the empty case outright.)
	var res AmendResult
	if err := json.Unmarshal([]byte(jstr), &res); err == nil && hasAmendField(res) {
		return res, nil
	}
	// Single bare object {"id":"...","weight":N} without a "rubrics" wrapper.
	var single amendChange
	if err := json.Unmarshal([]byte(jstr), &single); err == nil && single.ID != "" {
		return AmendResult{Rubrics: []amendChange{single}}, nil
	}
	// Last resort: the extractor stripped array brackets from a multi-object
	// response. Re-wrap "{...}, {...}" into "[...]" and retry once. We only
	// do this when there is clearly more than one object (a `},{` boundary),
	// otherwise we'd mask legitimate single-object parse failures.
	if trimmed[0] == '{' && strings.Contains(trimmed, "},") && strings.HasSuffix(trimmed, "}") {
		wrapped := "[" + trimmed + "]"
		var changes []amendChange
		if err := json.Unmarshal([]byte(wrapped), &changes); err == nil && len(changes) > 1 {
			return AmendResult{Rubrics: changes}, nil
		}
	}
	return AmendResult{}, fmt.Errorf("could not parse amend response: %s", truncateForError(jstr))
}

// hasAmendField reports whether the AmendResult has at least one populated
// field — used to distinguish a real (possibly sparse) amend response from a
// bare object that the JSON decoder silently accepted.
func hasAmendField(r AmendResult) bool {
	return len(r.Rubrics) > 0 || r.Location != "" || r.MinSalary != nil ||
		r.MinSalaryCurrency != "" || len(r.WorkArrangement) > 0 ||
		len(r.PreferredTech) > 0 || len(r.AvoidedTech) > 0
}
