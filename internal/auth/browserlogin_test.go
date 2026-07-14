package auth

import (
	"path/filepath"
	"testing"

	"github.com/chromedp/cdproto/network"
)

func TestFilterLinkedInCookies(t *testing.T) {
	all := []*network.Cookie{
		{Name: "li_at", Value: "abc", Domain: ".linkedin.com"},
		{Name: "JSESSIONID", Value: "ajax:1", Domain: ".www.linkedin.com"},
		{Name: "tracker", Value: "xyz", Domain: ".example.com"},
		{Name: "lidc", Value: "def", Domain: ".linkedin.com"},
	}
	got := filterLinkedInCookies(all)
	if len(got) != 3 {
		t.Fatalf("filterLinkedInCookies returned %d cookies, want 3", len(got))
	}
	for _, c := range got {
		if c.Name == "tracker" {
			t.Error("example.com cookie should be filtered out")
		}
	}
}

func TestFilterLinkedInCookiesEmpty(t *testing.T) {
	got := filterLinkedInCookies(nil)
	if len(got) != 0 {
		t.Errorf("expected 0 cookies, got %d", len(got))
	}
}

func TestChromeProfileDir(t *testing.T) {
	p := ChromeProfileDir()
	if p == "" {
		t.Error("ChromeProfileDir() is empty")
	}
	if filepath.Base(p) != "chrome-profile" {
		t.Errorf("ChromeProfileDir() base = %q, want chrome-profile", filepath.Base(p))
	}
}
