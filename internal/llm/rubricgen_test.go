package llm

import (
	"testing"
)

func TestParseAmendChanges(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []amendChange
		wantErr bool
	}{
		{
			name:  "bare array with multiple changes",
			input: `[{"id":"preferred_tech","weight":3},{"id":"min_salary","weight":5}]`,
			want: []amendChange{
				{ID: "preferred_tech", Weight: 3},
				{ID: "min_salary", Weight: 5},
			},
		},
		{
			name:  "single-element array",
			input: `[{"id":"min_salary","weight":160000}]`,
			want:  []amendChange{{ID: "min_salary", Weight: 160000}},
		},
		{
			name:  "wrapper object with rubrics key",
			input: `{"rubrics":[{"id":"preferred_tech","description":"likes python","items":["python"]}]}`,
			want: []amendChange{
				{ID: "preferred_tech", Description: "likes python", Items: []string{"python"}},
			},
		},
		{
			name:  "single bare object (regression: weight-only edit)",
			input: `{"id":"min_salary","weight":160000}`,
			want:  []amendChange{{ID: "min_salary", Weight: 160000}},
		},
		{
			name:  "single bare object with items",
			input: `{"id":"avoided_tech","items":["c#"]}`,
			want:  []amendChange{{ID: "avoided_tech", Items: []string{"c#"}}},
		},
		{
			name:    "empty string",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "object without id and without rubrics key",
			input:   `{"foo":"bar"}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAmendChanges(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAmendChanges() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseAmendChanges() got %d changes, want %d (%+v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i].ID != tt.want[i].ID {
					t.Errorf("change[%d].ID = %q, want %q", i, got[i].ID, tt.want[i].ID)
				}
				if got[i].Weight != tt.want[i].Weight {
					t.Errorf("change[%d].Weight = %d, want %d", i, got[i].Weight, tt.want[i].Weight)
				}
				if got[i].Description != tt.want[i].Description {
					t.Errorf("change[%d].Description = %q, want %q", i, got[i].Description, tt.want[i].Description)
				}
				if !sliceEq(got[i].Items, tt.want[i].Items) {
					t.Errorf("change[%d].Items = %v, want %v", i, got[i].Items, tt.want[i].Items)
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
