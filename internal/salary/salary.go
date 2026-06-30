package salary

import (
	"regexp"
	"strconv"
	"strings"
)

// Salary is a parsed salary range. Amounts are raw numbers; no conversion.
type Salary struct {
	Low      *float64
	High     *float64
	Currency string
	Raw      string
}

// Max returns the highest salary figure mentioned, used for filtering.
func (s Salary) Max() float64 {
	vals := []float64{}
	if s.Low != nil {
		vals = append(vals, *s.Low)
	}
	if s.High != nil {
		vals = append(vals, *s.High)
	}
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

var (
	// amountRE matches a single money figure with an optional currency. The
	// prefix group (cur) covers symbols/codes BEFORE the amount; the suffix
	// group (cur2) covers an ISO code AFTER the amount (e.g. "$257,000 CAD"),
	// which is how many job posts state the currency for a range. Note: no
	// whitespace is allowed between amount and the k/M unit, so that the \s+
	// before cur2 isn't stolen by a greedy quantifier.
	amountRE = regexp.MustCompile(`(?i)(?P<cur>CA\$|C\$|CAD|US\$|USD|EUR|GBP|AUD|INR|JPY|€|£|¥|₹|\$)?\s*(?P<amt>[\d,]+(?:\.\d+)?)(?P<unit>[kKmM])?(?:\s+(?P<cur2>CAD|USD|EUR|GBP|AUD|INR|JPY))?`)

	currencyMap = map[string]string{
		"ca$": "CAD", "c$": "CAD", "cad": "CAD",
		"us$": "USD", "usd": "USD", "$": "USD",
		"eur": "EUR", "€": "EUR",
		"gbp": "GBP", "£": "GBP",
		"aud": "AUD", "a$": "AUD",
		"inr": "INR", "₹": "INR",
		"jpy": "JPY", "¥": "JPY",
	}

	rangeSplitRE = regexp.MustCompile(`\s[-–—]\s|\s[to]\s`)
)

// Parse parses a salary string from a LinkedIn job page. Returns nil if nothing
// could be parsed.
func Parse(text string) *Salary {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	parts := rangeSplitRE.Split(text, -1)
	var amounts []float64
	currency := "USD"
	for _, part := range parts {
		amt, cur := parseAmount(part, currency)
		if amt != nil {
			amounts = append(amounts, *amt)
			currency = cur
		}
	}
	if len(amounts) == 0 {
		return nil
	}
	s := &Salary{Currency: currency, Raw: text}
	if len(amounts) >= 1 {
		l := amounts[0]
		s.Low = &l
	}
	if len(amounts) >= 2 {
		h := amounts[1]
		s.High = &h
	}
	return s
}

func parseAmount(raw, defaultCurrency string) (*float64, string) {
	m := amountRE.FindStringSubmatch(raw)
	if m == nil {
		return nil, defaultCurrency
	}
	cur := resolveCurrency(m[1], m[4], defaultCurrency)
	amtStr := strings.ReplaceAll(m[2], ",", "")
	f, err := strconv.ParseFloat(amtStr, 64)
	if err != nil {
		return nil, defaultCurrency
	}
	switch strings.ToLower(m[3]) {
	case "k":
		f *= 1_000
	case "m":
		f *= 1_000_000
	}
	return &f, cur
}

// resolveCurrency picks the most specific currency signal: a trailing ISO code
// (cur2) wins, then an explicit (non-ambiguous) prefix, then the carried
// default. A bare "$" prefix alone never overrides the default because it is
// ambiguous between several dollar currencies.
func resolveCurrency(prefix, suffix, defaultCurrency string) string {
	if c := currencyMap[strings.ToLower(suffix)]; c != "" {
		return c
	}
	if p := strings.TrimSpace(prefix); p != "" && p != "$" {
		if c := currencyMap[strings.ToLower(p)]; c != "" {
			return c
		}
	}
	return defaultCurrency
}

// PassesFilter reports whether a salary meets the minimum threshold, using the
// MAX of its range (inclusive: "could this job pay >= min?").
func PassesFilter(s *Salary, min float64) bool {
	if s == nil || s.Max() == 0 {
		return false
	}
	return s.Max() >= min
}

// ParseShorthand parses user-friendly salary shorthand: "200k", "200000",
// "$200k". Used for the --min-salary flag.
func ParseShorthand(text string) (float64, error) {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "$", "")
	text = strings.ReplaceAll(text, ",", "")
	text = strings.TrimSpace(text)
	mult := 1.0
	switch {
	case strings.HasSuffix(strings.ToLower(text), "k"):
		text = text[:len(text)-1]
		mult = 1_000
	case strings.HasSuffix(strings.ToLower(text), "m"):
		text = text[:len(text)-1]
		mult = 1_000_000
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, err
	}
	return f * mult, nil
}
