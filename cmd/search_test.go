package cmd

import "testing"

func TestResolveWorkType(t *testing.T) {
	cases := []struct {
		name                                          string
		remote, hybrid, onsite                        bool
		want                                          string
	}{
		{"none", false, false, false, ""},
		{"remote_only", true, false, false, "2"},
		{"hybrid_only", false, true, false, "3"},
		{"onsite_only", false, false, true, "1"},
		{"remote_hybrid", true, true, false, "2,3"},
		{"remote_onsite", true, false, true, "1,2"},
		{"hybrid_onsite", false, true, true, "1,3"},
		{"all_three", true, true, true, "1,2,3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveWorkType(c.remote, c.hybrid, c.onsite)
			if got != c.want {
				t.Errorf("resolveWorkType(remote=%v, hybrid=%v, onsite=%v) = %q, want %q",
					c.remote, c.hybrid, c.onsite, got, c.want)
			}
		})
	}
}

func TestWorkTypeLabel(t *testing.T) {
	cases := []struct {
		name                                   string
		remote, hybrid, onsite                 bool
		want                                   string
	}{
		{"none", false, false, false, ""},
		{"remote_only", true, false, false, "remote"},
		{"hybrid_only", false, true, false, "hybrid"},
		{"onsite_only", false, false, true, "onsite"},
		{"remote_hybrid", true, true, false, "remote/hybrid"},
		{"all_three", true, true, true, "onsite/remote/hybrid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := workTypeLabel(c.remote, c.hybrid, c.onsite)
			if got != c.want {
				t.Errorf("workTypeLabel(remote=%v, hybrid=%v, onsite=%v) = %q, want %q",
					c.remote, c.hybrid, c.onsite, got, c.want)
			}
		})
	}
}
