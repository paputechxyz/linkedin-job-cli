package cmd

import "testing"

func TestParseJobIDArg(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"4434368088", "4434368088"},
		{"1", "1"},
		{"007", "007"},
		// rejected — not a bare integer id
		{"-3", ""},
		{"+5", ""},
		{"0x10", ""},
		{"1.5", ""},
		{"4434368088/", ""},
		{" 4434368088 ", "4434368088"}, // whitespace trimmed, then digits
		{"https://www.linkedin.com/jobs/view/4434368088/", ""},
		{"staff engineer", ""},
		{"abc", ""},
	}
	for _, c := range cases {
		t.Run(fmtQuote(c.in), func(t *testing.T) {
			got := parseJobIDArg(c.in)
			if got != c.want {
				t.Errorf("parseJobIDArg(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestViewJobIDFromURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		// single-job view URLs → id extracted (pure-digit + slug forms)
		{"https://www.linkedin.com/jobs/view/4435820129/", "4435820129"},
		{"https://www.linkedin.com/jobs/view/4435820129", "4435820129"},
		{"https://www.linkedin.com/jobs/view/4435820129/?currentJobId=999", "4435820129"}, // path wins
		{"https://www.linkedin.com/jobs/view/senior-full-stack-software-engineer-at-stan-ai-4431544268/", "4431544268"},
		{"http://www.linkedin.com/jobs/view/foo-bar-1234567/", "1234567"},
		// NOT single-job view URLs → "" (pass through to SearchURL)
		{"https://www.linkedin.com/jobs/search/?keywords=Staff%20Engineer", ""},
		{"https://www.linkedin.com/jobs/search/?currentJobId=4415889466&originToLandingJobPostings=4415889466%2C4434154740", ""},
		{"https://www.linkedin.com/jobs/collections/recommended/?start=0", ""},
		{"https://www.linkedin.com/jobs/view/?currentJobId=4415889466", ""}, // no id in path
		{"not a url", ""},
		{"", ""},
	}
	for _, c := range cases {
		t.Run(fmtQuote(c.in), func(t *testing.T) {
			got := viewJobIDFromURL(c.in)
			if got != c.want {
				t.Errorf("viewJobIDFromURL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// fmtQuote keeps subtest names readable without pulling fmt into the prod path.
func fmtQuote(s string) string {
	if s == "" {
		return "empty"
	}
	return s
}
