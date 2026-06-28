package linkedin

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"linkedin-jobs/internal/auth"
	"linkedin-jobs/internal/config"
)

// Client is the LinkedIn HTTP client. Anonymous calls (search/detail) need no
// session; authenticated calls (recommended) require one.
type Client struct {
	cfg     config.Config
	http    *http.Client
	session *auth.Session
}

// New constructs a Client with the given config. WithSession should be called
// separately if authenticated calls are needed.
func New(cfg config.Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: time.Duration(cfg.RequestTimeoutSeconds) * time.Second},
	}
}

// WithSession attaches a resolved LinkedIn session for authenticated calls.
func (c *Client) WithSession(s *auth.Session) *Client {
	c.session = s
	return c
}

// HasSession reports whether an authenticated session is available.
func (c *Client) HasSession() bool { return c.session != nil && c.session.CookieHeader != "" }

// ErrAuthRequired is returned when an authenticated call is made without a session.
var ErrAuthRequired = errors.New("authenticated call requires a LinkedIn session: run `linkedin-jobs auth login` or set LJ_COOKIES_FILE / LJ_COOKIE")

// get fetches a URL with browser-like headers, optionally authenticated.
func (c *Client) get(url string, authenticated bool, extra http.Header) (string, http.Header, int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", nil, 0, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,application/json,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if extra != nil {
		for k, vs := range extra {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}
	}
	if authenticated {
		if !c.HasSession() {
			return "", nil, 0, ErrAuthRequired
		}
		req.Header.Set("Cookie", c.session.CookieHeader)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, resp.StatusCode, err
	}
	return string(body), resp.Header, resp.StatusCode, nil
}

// getJSON is like get but sets an application/json accept header.
func (c *Client) getJSON(url string, authenticated bool, extra http.Header) (string, int, error) {
	if extra == nil {
		extra = http.Header{}
	}
	extra.Set("Accept", "application/json")
	body, _, status, err := c.get(url, authenticated, extra)
	return body, status, err
}

// cleanURL strips tracking query params from a LinkedIn job URL.
func cleanURL(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}
