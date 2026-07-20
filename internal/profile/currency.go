package profile

import (
	"strings"
)

// locationCurrencyMap maps a country OR a major tech-hub city to its ISO 4217
// currency code. Used to auto-derive min_salary_currency from the user's
// location preference during setup, and as the fallback when the description
// posts multiple locale-specific salary bands and the user's preferred region
// needs to drive the pick.
//
// Country entries are checked first; city entries cover the common case where
// the user writes a city only ("Toronto", "San Francisco") with no country.
// Keys are lowercased; the matcher is substring-based so "Toronto, ON, Canada"
// hits both the city ("toronto") and the country ("canada").
var locationCurrencyMap = map[string]string{
	// Countries.
	"canada":             "CAD",
	"united states":     "USD",
	"usa":               "USD",
	"us":                "USD",
	"united kingdom":    "GBP",
	"uk":                "GBP",
	"great britain":     "GBP",
	"england":           "GBP",
	"germany":           "EUR",
	"france":            "EUR",
	"netherlands":       "EUR",
	"spain":             "EUR",
	"italy":             "EUR",
	"ireland":           "EUR",
	"poland":            "EUR",
	"sweden":            "EUR",
	"portugal":          "EUR",
	"belgium":           "EUR",
	"austria":           "EUR",
	"finland":           "EUR",
	"greece":            "EUR",
	"europe":            "EUR",
	"india":             "INR",
	"japan":             "JPY",
	"australia":         "AUD",

	// Canada cities.
	"toronto":       "CAD",
	"vancouver":     "CAD",
	"montreal":      "CAD",
	"mississauga":   "CAD",
	"ottawa":        "CAD",
	"calgary":       "CAD",
	"edmonton":      "CAD",
	"waterloo":      "CAD",
	"halifax":       "CAD",
	"victoria":      "CAD",
	"quebec":        "CAD",

	// US cities.
	"san francisco":  "USD",
	"new york":       "USD",
	"seattle":        "USD",
	"austin":         "USD",
	"boston":         "USD",
	"chicago":        "USD",
	"los angeles":    "USD",
	"denver":         "USD",
	"portland":       "USD",
	"san diego":      "USD",
	"atlanta":        "USD",
	"dallas":         "USD",
	"houston":        "USD",
	"miami":          "USD",
	"phoenix":        "USD",
	"washington":     "USD",
	"boulder":        "USD",
	"raleigh":        "USD",

	// UK / Europe cities.
	"london":         "GBP",
	"manchester":     "GBP",
	"dublin":         "EUR",
	"berlin":         "EUR",
	"munich":         "EUR",
	"paris":          "EUR",
	"amsterdam":      "EUR",
	"madrid":         "EUR",
	"barcelona":      "EUR",
	"rome":           "EUR",
	"milan":          "EUR",
	"stockholm":      "EUR",
	"lisbon":         "EUR",
	"vienna":         "EUR",

	// India / Japan / Australia cities.
	"bangalore":      "INR",
	"bengaluru":      "INR",
	"mumbai":         "INR",
	"delhi":          "INR",
	"hyderabad":      "INR",
	"pune":           "INR",
	"chennai":        "INR",
	"tokyo":          "JPY",
	"osaka":          "JPY",
	"sydney":         "AUD",
	"melbourne":      "AUD",
	"brisbane":       "AUD",
}

// InferCurrencyFromLocation maps a free-text location string to its ISO 4217
// currency code using locationCurrencyMap. The match is case-insensitive and
// substring-based: any city or country token in the map that appears in the
// location string wins. Returns "" when no token matches.
//
// Order of precedence: the longest matching key wins, so "Toronto, Canada"
// resolves via "canada" (length 6) over "toronto" (length 7) — wait, "toronto"
// is longer. The rule is: scan every map key, find the ones that appear as
// substrings, and return the currency of the longest. That way a city match
// (more specific) wins over a country match (less specific) when they disagree,
// and "San Francisco" beats "US" when the location string contains both.
func InferCurrencyFromLocation(loc string) string {
	loc = strings.ToLower(strings.TrimSpace(loc))
	if loc == "" {
		return ""
	}
	bestKey := ""
	for key := range locationCurrencyMap {
		if key == "" {
			continue
		}
		if !strings.Contains(loc, key) {
			continue
		}
		// Prefer the longest matching key — a more specific token (city) wins
		// over a less specific one (country) when both appear.
		if len(key) > len(bestKey) {
			bestKey = key
		}
	}
	if bestKey == "" {
		return ""
	}
	return locationCurrencyMap[bestKey]
}
