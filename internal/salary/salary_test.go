package salary

import "testing"

func TestParse_TrailingCurrencyCAD(t *testing.T) {
	// "$205,600 - $257,000 CAD" — currency stated once at the END of the range.
	s := Parse("$205,600 - $257,000 CAD")
	if s == nil {
		t.Fatal("expected a salary")
	}
	if s.Low == nil || *s.Low != 205600 {
		t.Errorf("low = %v, want 205600", s.Low)
	}
	if s.High == nil || *s.High != 257000 {
		t.Errorf("high = %v, want 257000", s.High)
	}
	if s.Currency != "CAD" {
		t.Errorf("currency = %q, want CAD", s.Currency)
	}
}

func TestParse_ExplicitCADPrefix(t *testing.T) {
	s := Parse("CA$200k - CA$250k")
	if s == nil || s.Currency != "CAD" {
		t.Errorf("currency = %q, want CAD", currencyOf(s))
	}
	if s.High == nil || *s.High != 250000 {
		t.Errorf("high = %v, want 250000", s.High)
	}
}

func TestParse_BareDollarDefaultsUSD(t *testing.T) {
	// No currency signal anywhere -> defaults to USD (unchanged legacy behavior).
	s := Parse("$200k - $250k")
	if s == nil || s.Currency != "USD" {
		t.Errorf("currency = %q, want USD", currencyOf(s))
	}
}

func TestParse_TrailingUSD(t *testing.T) {
	s := Parse("$120,000 - $140,000 USD")
	if s == nil || s.Currency != "USD" {
		t.Errorf("currency = %q, want USD", currencyOf(s))
	}
}

func TestParseShorthand(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"200k", 200000},
		{"200000", 200000},
		{"$200k", 200000},
		{"1.5m", 1500000},
	}
	for _, c := range cases {
		got, err := ParseShorthand(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseShorthand(%q) = %v err=%v, want %v", c.in, got, err, c.want)
		}
	}
}

func currencyOf(s *Salary) string {
	if s == nil {
		return "<nil>"
	}
	return s.Currency
}
