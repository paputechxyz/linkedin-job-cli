package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchJSESSIONIDFromURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "li_at=mytoken" {
			t.Errorf("request Cookie = %q, want li_at=mytoken", r.Header.Get("Cookie"))
		}
		w.Header().Set("Set-Cookie", `JSESSIONID="ajax:abc123"; Path=/; HttpOnly; Secure`)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got, err := fetchJSESSIONIDFromURL("mytoken", srv.URL)
	if err != nil {
		t.Fatalf("fetchJSESSIONIDFromURL: %v", err)
	}
	if got != "ajax:abc123" {
		t.Errorf("JSESSIONID = %q, want ajax:abc123", got)
	}
}

func TestFetchJSESSIONIDAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "other=val; Path=/")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got, err := fetchJSESSIONIDFromURL("mytoken", srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("JSESSIONID = %q, want empty (not in response)", got)
	}
}
