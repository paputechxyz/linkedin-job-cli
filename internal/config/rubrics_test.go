package config

import "testing"

func TestMergeRubrics_PreservesUntouchedAndDefaultsNew(t *testing.T) {
	existing := []Rubric{
		{ID: "salary", Kind: "system", Weight: 5},
		{ID: "work_arrangement", Kind: "system", Weight: 5},
		{ID: "preferred_tech", Kind: "dynamic", Weight: 7, Description: "tech I like", Items: []string{"Go"}},
	}
	// Amend: add free_snacks, change salary weight to 8. preferred_tech unmentioned.
	changes := []Rubric{
		{ID: "free_snacks", Description: "free snacks"},
		{ID: "salary", Weight: 8},
	}
	merged := MergeRubrics(existing, changes)

	byID := map[string]Rubric{}
	for _, r := range merged {
		byID[r.ID] = r
	}

	// AE1: salary weight changed to 8; its system kind preserved.
	if got := byID["salary"]; got.Weight != 8 || got.Kind != "system" {
		t.Errorf("salary = %+v, want weight 8 kind system", got)
	}
	// Untouched rubric preserved exactly.
	if got := byID["preferred_tech"]; got.Weight != 7 || got.Description != "tech I like" || len(got.Items) != 1 {
		t.Errorf("preferred_tech not preserved: %+v", got)
	}
	// Untouched system rubric preserved.
	if got := byID["work_arrangement"]; got.Kind != "system" || got.Weight != 5 {
		t.Errorf("work_arrangement not preserved: %+v", got)
	}
	// New rubric defaulted to dynamic, weight 5.
	if got, ok := byID["free_snacks"]; !ok {
		t.Errorf("free_snacks not added")
	} else if got.Kind != "dynamic" || got.Weight != 5 {
		t.Errorf("free_snacks = %+v, want dynamic weight 5", got)
	}
}

func TestMergeRubrics_WeightOnlyEditKeepsSystem(t *testing.T) {
	existing := []Rubric{{ID: "location", Kind: "system", Weight: 5, Description: "loc"}}
	merged := MergeRubrics(existing, []Rubric{{ID: "location", Weight: 10}})
	if len(merged) != 1 {
		t.Fatalf("got %d rubrics, want 1", len(merged))
	}
	if merged[0].Weight != 10 || merged[0].Kind != "system" || merged[0].Description != "loc" {
		t.Errorf("location = %+v, want weight 10 system + preserved description", merged[0])
	}
}
