package auth

import "testing"

func TestCSRFFromCookieHeader(t *testing.T) {
	cases := map[string]string{
		"JSESSIONID=ajax:1234567890123456":          "ajax:1234567890123456",
		"JSESSIONID=\"ajax:1234567890123456\"; b=2": "ajax:1234567890123456",
		"a=1; jsessionid=ajax:99; c=3":              "ajax:99",
		"li_at=foo; JSESSIONID=\"ajax:7\"; bsync=1": "ajax:7",
		"no_session_here=1":                         "",
	}
	for in, want := range cases {
		got := csrfFromCookieHeader(in)
		if got != want {
			t.Errorf("csrfFromCookieHeader(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHasCookie(t *testing.T) {
	cases := []struct {
		header string
		name   string
		want   bool
	}{
		{"li_at=abc; JSESSIONID=ajax:1", "li_at", true},
		{"li_at=abc; JSESSIONID=ajax:1", "LI_AT", true}, // case-insensitive
		{"bcookie=1; lidc=2", "li_at", false},
		{"", "li_at", false},
		{"x=1; jsessionid=ajax:1", "jsessionid", true},
		{"x=1", "jsessionid", false},
		// values containing '=' must not break parsing
		{"li_at=a=b; JSESSIONID=ajax:1", "li_at", true},
	}
	for _, tc := range cases {
		got := hasCookie(tc.header, tc.name)
		if got != tc.want {
			t.Errorf("hasCookie(%q, %q) = %v, want %v", tc.header, tc.name, got, tc.want)
		}
	}
}

func TestSessionValid(t *testing.T) {
	cases := []struct {
		name string
		s    *Session
		want bool
	}{
		{"complete session", &Session{
			CookieHeader: "li_at=abc; JSESSIONID=\"ajax:1234\"",
			CSRFToken:    "ajax:1234",
			Source:       "press-auth",
		}, true},
		{"missing li_at (pre-login capture)", &Session{
			CookieHeader: "__cf_bm=x; bcookie=y; lidc=z",
			CSRFToken:    "ajax:1234",
		}, false},
		{"empty csrf (no JSESSIONID)", &Session{
			CookieHeader: "li_at=abc",
			CSRFToken:    "",
		}, false},
		{"nil session", nil, false},
		{"empty cookie header", &Session{CSRFToken: "ajax:1"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Valid(); got != tc.want {
				t.Errorf("Valid() = %v, want %v", got, tc.want)
			}
		})
	}
}
