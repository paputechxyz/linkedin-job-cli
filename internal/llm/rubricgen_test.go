package llm

import (
	"testing"
)

func TestParseAmendResult(t *testing.T) {
	salary := 200000.0
	tests := []struct {
		name    string
		input   string
		want    AmendResult
		wantErr bool
	}{
		{
			name:  "bare array with multiple rubric changes",
			input: `[{"id":"preferred_tech","weight":3},{"id":"min_salary","weight":5}]`,
			want: AmendResult{Rubrics: []amendChange{
				{ID: "preferred_tech", Weight: 3},
				{ID: "min_salary", Weight: 5},
			}},
		},
		{
			name:  "single-element array",
			input: `[{"id":"min_salary","weight":160000}]`,
			want:  AmendResult{Rubrics: []amendChange{{ID: "min_salary", Weight: 160000}}},
		},
		{
			name:  "wrapper object with rubrics key only",
			input: `{"rubrics":[{"id":"preferred_tech","description":"likes python","items":["python"]}]}`,
			want: AmendResult{Rubrics: []amendChange{
				{ID: "preferred_tech", Description: "likes python", Items: []string{"python"}},
			}},
		},
		{
			name:  "wrapper object with rubrics plus structured profile fields",
			input: `{"rubrics":[{"id":"role_level","description":"senior or staff"}],"location":"Seattle, WA, USA","min_salary":200000,"min_salary_currency":"USD","work_arrangement":["onsite"]}`,
			want: AmendResult{
				Rubrics:         []amendChange{{ID: "role_level", Description: "senior or staff"}},
				Location:        "Seattle, WA, USA",
				MinSalary:       &salary,
				MinSalaryCurrency: "USD",
				WorkArrangement: []string{"onsite"},
			},
		},
		{
			name:  "structured profile fields without rubrics",
			input: `{"location":"Seattle, WA, USA","min_salary":200000,"min_salary_currency":"USD"}`,
			want: AmendResult{
				Location:        "Seattle, WA, USA",
				MinSalary:       &salary,
				MinSalaryCurrency: "USD",
			},
		},
		{
			name:  "single bare object (regression: weight-only edit)",
			input: `{"id":"min_salary","weight":160000}`,
			want:  AmendResult{Rubrics: []amendChange{{ID: "min_salary", Weight: 160000}}},
		},
		{
			name:  "single bare object with items",
			input: `{"id":"avoided_tech","items":["c#"]}`,
			want:  AmendResult{Rubrics: []amendChange{{ID: "avoided_tech", Items: []string{"c#"}}}},
		},
		{
			name:    "empty string",
			input:   "   ",
			wantErr: true,
		},
		{
			// Regression: extractJSON used to strip array brackets off a
			// multi-object LLM response, leaving comma-separated objects
			// behind. parseAmendResult must recover by re-wrapping them.
			name:  "comma-separated objects without array brackets",
			input: `{"id":"salary","description":"salary floor"},   {"id":"work_arrangement","description":"onsite"}`,
			want: AmendResult{Rubrics: []amendChange{
				{ID: "salary", Description: "salary floor"},
				{ID: "work_arrangement", Description: "onsite"},
			}},
		},
		{
			name:    "object without id and without rubrics key",
			input:   `{"foo":"bar"}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAmendResult(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAmendResult() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !sliceEq(got.WorkArrangement, tt.want.WorkArrangement) {
				t.Errorf("WorkArrangement = %v, want %v", got.WorkArrangement, tt.want.WorkArrangement)
			}
			if !sliceEq(got.PreferredTech, tt.want.PreferredTech) {
				t.Errorf("PreferredTech = %v, want %v", got.PreferredTech, tt.want.PreferredTech)
			}
			if !sliceEq(got.AvoidedTech, tt.want.AvoidedTech) {
				t.Errorf("AvoidedTech = %v, want %v", got.AvoidedTech, tt.want.AvoidedTech)
			}
			if got.Location != tt.want.Location {
				t.Errorf("Location = %q, want %q", got.Location, tt.want.Location)
			}
			if got.MinSalaryCurrency != tt.want.MinSalaryCurrency {
				t.Errorf("MinSalaryCurrency = %q, want %q", got.MinSalaryCurrency, tt.want.MinSalaryCurrency)
			}
			if (got.MinSalary == nil) != (tt.want.MinSalary == nil) {
				t.Errorf("MinSalary = %v, want %v", got.MinSalary, tt.want.MinSalary)
			} else if got.MinSalary != nil && *got.MinSalary != *tt.want.MinSalary {
				t.Errorf("MinSalary = %v, want %v", *got.MinSalary, *tt.want.MinSalary)
			}
			if len(got.Rubrics) != len(tt.want.Rubrics) {
				t.Fatalf("Rubrics got %d, want %d (%+v)", len(got.Rubrics), len(tt.want.Rubrics), got.Rubrics)
			}
			for i := range got.Rubrics {
				if got.Rubrics[i].ID != tt.want.Rubrics[i].ID {
					t.Errorf("rubrics[%d].ID = %q, want %q", i, got.Rubrics[i].ID, tt.want.Rubrics[i].ID)
				}
				if got.Rubrics[i].Weight != tt.want.Rubrics[i].Weight {
					t.Errorf("rubrics[%d].Weight = %d, want %d", i, got.Rubrics[i].Weight, tt.want.Rubrics[i].Weight)
				}
				if got.Rubrics[i].Description != tt.want.Rubrics[i].Description {
					t.Errorf("rubrics[%d].Description = %q, want %q", i, got.Rubrics[i].Description, tt.want.Rubrics[i].Description)
				}
				if !sliceEq(got.Rubrics[i].Items, tt.want.Rubrics[i].Items) {
					t.Errorf("rubrics[%d].Items = %v, want %v", i, got.Rubrics[i].Items, tt.want.Rubrics[i].Items)
				}
			}
		})
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
