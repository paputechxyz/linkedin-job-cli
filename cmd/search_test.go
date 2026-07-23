package cmd

import "testing"

func TestSplitSearchQuery(t *testing.T) {
	cases := []struct {
		in              string
		wantKeywords    string
		wantLocation    string
	}{
		{"Staff Engineer, Toronto", "Staff Engineer", "Toronto"},
		// Multi-comma location stays intact (first comma is the splitter).
		{"Senior Developer, Remote, US", "Senior Developer", "Remote, US"},
		{"Senior Developer, Toronto, Ontario, Canada", "Senior Developer", "Toronto, Ontario, Canada"},
		// No comma → keywords-only.
		{"Staff Engineer", "Staff Engineer", ""},
		// Whitespace around the comma is trimmed.
		{"  Staff Engineer ,  Toronto  ", "Staff Engineer", "Toronto"},
		// Leading comma → empty keywords, location is the rest.
		{", Toronto", "", "Toronto"},
		// Empty input.
		{"", "", ""},
	}
	for _, c := range cases {
		kw, loc := splitSearchQuery(c.in)
		if kw != c.wantKeywords || loc != c.wantLocation {
			t.Errorf("splitSearchQuery(%q) = (%q, %q), want (%q, %q)",
				c.in, kw, loc, c.wantKeywords, c.wantLocation)
		}
	}
}
