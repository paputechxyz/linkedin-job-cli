package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"linkedin-jobs/internal/config"
)

// LoginViaBrowser launches a headed Chrome with a managed persistent profile,
// navigates to LinkedIn, and waits for the user to log in. Once the li_at
// cookie appears, it captures all linkedin.com cookies and returns them.
// The browser is closed when the function returns.
func LoginViaBrowser(profileDir string, timeout time.Duration) (map[string]string, error) {
	if runtime.GOOS != "darwin" {
		return nil, ErrUnsupportedPlatform
	}

	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, fmt.Errorf("create profile dir: %w", err)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.UserDataDir(profileDir),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var linkedinCookies []*network.Cookie
	err := chromedp.Run(timeoutCtx,
		chromedp.Navigate("https://www.linkedin.com/login"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			for {
				all, err := network.GetCookies().Do(ctx)
				if err != nil {
					return err
				}
				for _, c := range all {
					if c.Name == "li_at" && c.Value != "" {
						linkedinCookies = filterLinkedInCookies(all)
						return nil
					}
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
				}
			}
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("guided login: %w", err)
	}

	cookies := make(map[string]string)
	for _, c := range linkedinCookies {
		cookies[c.Name] = c.Value
	}
	return cookies, nil
}

// ChromeProfileDir returns the default managed profile directory for the
// guided browser login.
func ChromeProfileDir() string {
	return filepath.Join(config.HomeDir(), "chrome-profile")
}

// filterLinkedInCookies returns only cookies whose domain contains
// linkedin.com.
func filterLinkedInCookies(all []*network.Cookie) []*network.Cookie {
	var out []*network.Cookie
	for _, c := range all {
		if strings.Contains(c.Domain, "linkedin.com") {
			out = append(out, c)
		}
	}
	return out
}
