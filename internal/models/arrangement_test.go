package models

import "testing"

func TestHasWorkArrangementPreference(t *testing.T) {
	cases := []struct {
		name  string
		prefs []string
		want  bool
	}{
		{"empty", nil, false},
		{"all_three", []string{"remote", "hybrid", "onsite"}, false},
		{"all_three_any_order", []string{"onsite", "remote", "hybrid"}, false},
		{"all_three_case_insensitive", []string{"Remote", "HYBRID", "On-Site"}, false},
		{"remote_only", []string{"remote"}, true},
		{"hybrid_only", []string{"hybrid"}, true},
		{"onsite_only", []string{"onsite"}, true},
		{"hybrid_onsite", []string{"hybrid", "onsite"}, true},
		{"remote_hybrid", []string{"remote", "hybrid"}, true},
		{"remote_onsite", []string{"remote", "onsite"}, true},
		{"hyphenated_on_site", []string{"on-site"}, true},
		{"spaced_on_site", []string{"on site"}, true},
		{"with_whitespace", []string{"  Remote  ", " Hybrid"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &Profile{PrefWorkArrangement: tc.prefs}
			if got := p.HasWorkArrangementPreference(); got != tc.want {
				t.Errorf("prefs=%v got %v want %v", tc.prefs, got, tc.want)
			}
		})
	}
}

func TestPrefersArrangement(t *testing.T) {
	p := &Profile{PrefWorkArrangement: []string{"remote", "on-site"}}
	cases := []struct {
		arrangement string
		want        bool
	}{
		{"remote", true},
		{"onsite", true},
		{"hybrid", false},
		{"", false},
		{"Remote", true},
		{"On-site", true},
	}
	for _, tc := range cases {
		t.Run(tc.arrangement, func(t *testing.T) {
			if got := p.PrefersArrangement(tc.arrangement); got != tc.want {
				t.Errorf("arrangement=%q got %v want %v", tc.arrangement, got, tc.want)
			}
		})
	}
}

func TestDetectArrangement(t *testing.T) {
	cases := []struct {
		name     string
		location string
		remote   string
		want     string
	}{
		{"remote_in_location", "Remote, Canada", "", "remote"},
		{"remote_in_type", "Toronto", "Remote", "remote"},
		{"hybrid_in_location", "Hybrid - NYC", "", "hybrid"},
		{"hybrid_in_type", "NYC", "Hybrid", "hybrid"},
		{"onsite_in_location", "On-site, SF", "", "onsite"},
		{"onsite_in_type", "SF", "onsite", "onsite"},
		{"onsite_no_hyphen", "SF onsite", "", "onsite"},
		{"onsite_spaced", "SF on site", "", "onsite"},
		{"hybrid_priority_over_remote", "Remote (Hybrid)", "Remote", "hybrid"},
		{"no_signal", "San Francisco, CA", "", ""},
		{"empty", "", "", ""},
		{"location_only_no_arrangement", "New York, NY", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := &JobPosting{Location: tc.location, RemoteType: tc.remote}
			if got := j.DetectArrangement(); got != tc.want {
				t.Errorf("location=%q remote=%q got %q want %q", tc.location, tc.remote, got, tc.want)
			}
		})
	}
}
