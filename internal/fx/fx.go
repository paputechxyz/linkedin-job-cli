// Package fx converts amounts between currencies using live exchange rates
// (ECB reference rates via the free Frankfurter API), cached on disk for the
// calendar day so repeated CLI invocations don't hit the network. When offline
// it falls back to a small hardcoded rate table so salary filtering keeps
// working without blocking the user.
package fx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"linkedin-jobs/internal/config"
)

// CacheFile holds the on-disk rate cache, defaults to ~/.linkedin-jobs/fx_cache.json
// (next to the global DB). Tests may override it.
var CacheFile = defaultCacheFile()

// rateBase is the currency rates are expressed against (1 rateBase = rate * code).
const rateBase = "USD"

// fallbackRates are used only when no live rate and no usable cache exist.
// Approximate values — kept conservative so an offline CAD floor doesn't slip.
var fallbackRates = map[string]float64{
	"USD": 1.0,
	"CAD": 1.36,
	"EUR": 0.92,
	"GBP": 0.79,
	"AUD": 1.52,
	"INR": 83.0,
	"JPY": 150.0,
}

type rateCache struct {
	Date      string             `json:"date"`
	Base      string             `json:"base"`
	Rates     map[string]float64 `json:"rates"`
	FetchedAt string             `json:"fetched_at"`
}

var (
	mu     sync.Mutex
	cached rateCache
	loaded bool
)

func defaultCacheFile() string {
	return filepath.Join(config.HomeDir(), "fx_cache.json")
}

// Normalize upper-cases and trims a currency code (e.g. "cad" -> "CAD").
func Normalize(code string) string { return strings.ToUpper(strings.TrimSpace(code)) }

// Supported reports whether code is a known convertible currency given the
// current rate table. Triggers a rate load (network/cache) on first use.
func Supported(code string) bool {
	code = Normalize(code)
	if code == "" {
		return false
	}
	_, ok := currentRates().Rates[code]
	return ok
}

// Convert converts amount from -> to using live (cached) rates. from==to is a
// no-op and needs no rates. Returns an error if either currency is unknown.
func Convert(amount float64, from, to string) (float64, error) {
	from = Normalize(from)
	to = Normalize(to)
	if from == "" || to == "" {
		return 0, fmt.Errorf("fx: empty currency code")
	}
	if from == to {
		return amount, nil
	}
	r := currentRates()
	rf, ok := r.Rates[from]
	if !ok {
		return 0, fmt.Errorf("fx: unsupported source currency %q", from)
	}
	rt, ok := r.Rates[to]
	if !ok {
		return 0, fmt.Errorf("fx: unsupported target currency %q", to)
	}
	if rf == 0 {
		return 0, fmt.Errorf("fx: zero rate for %q", from)
	}
	return amount * (rt / rf), nil
}

// currentRates returns the active rate table, loading from cache or fetching as
// needed. It never returns a nil Rates map: on total failure it falls back to
// the hardcoded table so callers always get a usable conversion for majors.
func currentRates() rateCache {
	mu.Lock()
	defer mu.Unlock()
	if loaded && fresh(cached) {
		return cached
	}
	if rc, err := loadFromCache(); err == nil && rc.Rates != nil {
		cached, loaded = rc, true
		return cached // today's cache wins even over a fresh fetch
	}
	if rc, err := fetch(); err == nil && rc.Rates != nil {
		cached, loaded = rc, true
		_ = saveCache(rc)
		return cached
	}
	// No network and no cache: serve the fallback so the tool keeps working.
	return rateCache{Date: today(), Base: rateBase, Rates: fallbackRates}
}

func fresh(rc rateCache) bool { return rc.Date == today() && rc.Rates != nil }

func loadFromCache() (rateCache, error) {
	var rc rateCache
	data, err := os.ReadFile(CacheFile)
	if err != nil {
		return rc, err
	}
	if err := json.Unmarshal(data, &rc); err != nil {
		return rc, err
	}
	if rc.Base == "" {
		rc.Base = rateBase
	}
	if rc.Rates[rateBase] == 0 {
		rc.Rates[rateBase] = 1.0
	}
	return rc, nil
}

func fetch() (rateCache, error) {
	const url = "https://api.frankfurter.app/latest?from=" + rateBase
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return rateCache{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return rateCache{}, fmt.Errorf("fx: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return rateCache{}, err
	}
	var fr struct {
		Date  string             `json:"date"`
		Base  string             `json:"base"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(body, &fr); err != nil {
		return rateCache{}, err
	}
	if fr.Date == "" {
		fr.Date = today()
	}
	if fr.Base == "" {
		fr.Base = rateBase
	}
	if fr.Rates[rateBase] == 0 {
		fr.Rates[rateBase] = 1.0
	}
	return rateCache{Date: fr.Date, Base: fr.Base, Rates: fr.Rates, FetchedAt: time.Now().UTC().Format(time.RFC3339)}, nil
}

func saveCache(rc rateCache) error {
	if dir := filepath.Dir(CacheFile); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(rc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(CacheFile, data, 0o644)
}

func today() string { return time.Now().Format("2006-01-02") }
