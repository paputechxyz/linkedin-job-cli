package cmd

import (
	"strings"
	"testing"
)

func TestParseChecksums(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]string
	}{
		{
			"standard sha256sum format",
			strings.Join([]string{
				"3e19f2f020a592f556ac804def605cdb43b1ed1f2c5c00f1f361e1ae569e7f2d  linkedin-jobs_darwin_amd64",
				"3fbbcf1d8a3e06cc1dd167f8a2fbac9ae1962a9d1398f4c0a6a7e7c45b95806f  linkedin-jobs_darwin_arm64",
				"3ed13d3dc3ba203e87319791ff7476eeb506477629b44da37a57158bf73aa28e  linkedin-jobs_windows_amd64.exe",
			}, "\n"),
			map[string]string{
				"linkedin-jobs_darwin_amd64":      "3e19f2f020a592f556ac804def605cdb43b1ed1f2c5c00f1f361e1ae569e7f2d",
				"linkedin-jobs_darwin_arm64":      "3fbbcf1d8a3e06cc1dd167f8a2fbac9ae1962a9d1398f4c0a6a7e7c45b95806f",
				"linkedin-jobs_windows_amd64.exe": "3ed13d3dc3ba203e87319791ff7476eeb506477629b44da37a57158bf73aa28e",
			},
		},
		{
			"blank lines and whitespace ignored",
			"\n  \n52c8ce26baace796c45a9fff6b24be1e67ed1f37971b03814219fad21d29ca5c  linkedin-jobs_linux_amd64\n",
			map[string]string{"linkedin-jobs_linux_amd64": "52c8ce26baace796c45a9fff6b24be1e67ed1f37971b03814219fad21d29ca5c"},
		},
		{
			"binary-mode star prefix on filename",
			"abc123  *linkedin-jobs_linux_arm64",
			map[string]string{"linkedin-jobs_linux_arm64": "abc123"},
		},
		{"empty file", "", map[string]string{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseChecksums(strings.NewReader(c.in))
			if err != nil {
				t.Fatalf("parseChecksums error: %v", err)
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %d entries, want %d (%v)", len(got), len(c.want), got)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Errorf("sums[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestVerifyChecksum(t *testing.T) {
	// sha256("hello world") = b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9
	const digest = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	sums := map[string]string{
		"linkedin-jobs_darwin_arm64": digest,
	}

	cases := []struct {
		name    string
		asset   string
		data    []byte
		wantErr bool
	}{
		{"matching digest", "linkedin-jobs_darwin_arm64", []byte("hello world"), false},
		{"mismatched digest", "linkedin-jobs_darwin_arm64", []byte("tampered"), true},
		{"missing entry", "linkedin-jobs_linux_amd64", []byte("hello world"), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := verifyChecksum(c.asset, c.data, sums)
			if c.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}
