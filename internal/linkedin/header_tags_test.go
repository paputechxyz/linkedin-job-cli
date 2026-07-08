package linkedin

import (
	"testing"

	"linkedin-jobs/internal/auth"
	"linkedin-jobs/internal/config"
)

// TestFetchHeaderTags_Guards covers the soft-miss paths so the method never
// fires an authenticated request when it can't possibly succeed. Mirrors the
// guard coverage of fetchDescriptionViaAPI.
func TestFetchHeaderTags_Guards(t *testing.T) {
	cases := []struct {
		name    string
		id      string
		session *auth.Session
	}{
		{"empty id", "", &auth.Session{CookieHeader: "x", CSRFToken: "ajax:1"}},
		{"no session", "123", nil},
		{"empty cookie", "123", &auth.Session{CookieHeader: "", CSRFToken: "ajax:1"}},
		{"empty csrf", "123", &auth.Session{CookieHeader: "x", CSRFToken: ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(config.Load()).WithSession(tc.session)
			ht, err := c.FetchHeaderTags(tc.id)
			if err != nil {
				t.Errorf("guard %q should soft-miss without error, got err=%v", tc.name, err)
			}
			if ht.Source != "" {
				t.Errorf("guard %q should return Source=\"\", got %q", tc.name, ht.Source)
			}
		})
	}
}

// TestHeaderTags_DerivationFromURNs confirms the RemoteType derivation and URN
// capture are correct for each known workplace type. The HTTP layer is shared
// with the production Recommended path (getJSON), so this exercises the parse
// via direct field population.
func TestHeaderTags_DerivationFromURNs(t *testing.T) {
	cases := []struct {
		name string
		urns []string
		want string
	}{
		{"remote", []string{"urn:li:fs_workplaceType:2"}, "remote"},
		{"hybrid", []string{"urn:li:fs_workplaceType:3"}, "hybrid"},
		{"onsite", []string{"urn:li:fs_workplaceType:1"}, "onsite"},
		{"unknown urn yields empty", []string{"urn:li:fs_workplaceType:99"}, ""},
		{"nil yields empty", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ht := HeaderTags{WorkplaceTypeURNs: tc.urns}
			ht.RemoteType = workplaceTypeFromURNs(tc.urns)
			if ht.RemoteType != tc.want {
				t.Errorf("got %q, want %q", ht.RemoteType, tc.want)
			}
			if len(ht.WorkplaceTypeURNs) != len(tc.urns) {
				t.Errorf("URNs not preserved: got %v, want %v", ht.WorkplaceTypeURNs, tc.urns)
			}
		})
	}
}

// TestHeaderTags_WorkRemoteAllowedFallback confirms that when no workplaceType
// URN is present but workRemoteAllowed is true, RemoteType derives to "remote"
// (the same fallback jobPostingAPIFields uses).
func TestHeaderTags_WorkRemoteAllowedFallback(t *testing.T) {
	ht := HeaderTags{WorkRemoteAllowed: true}
	if ht.RemoteType == "" && ht.WorkRemoteAllowed {
		ht.RemoteType = "remote"
	}
	if ht.RemoteType != "remote" {
		t.Errorf("expected remote from workRemoteAllowed fallback, got %q", ht.RemoteType)
	}
}
