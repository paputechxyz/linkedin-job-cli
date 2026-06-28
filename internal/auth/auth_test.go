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
