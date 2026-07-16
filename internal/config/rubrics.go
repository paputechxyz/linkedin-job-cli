package config

// MergeRubrics applies a set of changes onto an existing rubric list, upserting
// by ID. Rubrics in the existing set that are not named in changes are preserved
// untouched — this is the surgical-amend contract (R7). A change whose ID
// matches an existing rubric updates only the fields it supplies; a change with
// a new ID is appended (as a dynamic rubric at weight 5 unless it sets one).
func MergeRubrics(existing []Rubric, changes []Rubric) []Rubric {
	byID := make(map[string]int, len(existing))
	out := make([]Rubric, len(existing))
	copy(out, existing)
	for i, r := range out {
		byID[r.ID] = i
	}
	for _, c := range changes {
		if c.ID == "" {
			continue
		}
		if idx, ok := byID[c.ID]; ok {
			cur := out[idx]
			if c.Kind != "" {
				cur.Kind = c.Kind
			}
			if c.Weight != 0 {
				cur.Weight = c.Weight
			}
			if c.Description != "" {
				cur.Description = c.Description
			}
			if c.Items != nil {
				cur.Items = c.Items
			}
			if c.AppliesTo != nil {
				// A non-nil change replaces: non-empty sets/replaces the list,
				// an empty slice clears it (makes the rubric unconditional).
				cur.AppliesTo = c.AppliesTo
			}
			out[idx] = cur
			continue
		}
		// New rubric.
		if c.Kind == "" {
			c.Kind = "dynamic"
		}
		if c.Weight == 0 {
			c.Weight = 5
		}
		out = append(out, c)
		byID[c.ID] = len(out) - 1
	}
	return out
}
