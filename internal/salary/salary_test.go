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

func TestInDescription_CartaCADRange(t *testing.T) {
	desc := `Experience: We recommend 10+ years of professional software engineering experience.
Salary
Benefits
Carta’s compensation package includes a market competitive salary. Our expected cash compensation range for this role is:
$205,600 - $257,000 CAD in Toronto, Ontario, Canada
We are hiring for multiple levels and locations.`

	s := InDescription(desc)
	if s == nil {
		t.Fatal("expected a salary from the description body")
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

func TestInDescription_NoCurrencyReturnsNil(t *testing.T) {
	if s := InDescription("We pay $200k - $250k depending on experience."); s != nil {
		t.Errorf("expected nil for bare-$ range, got %+v", s)
	}
}

func TestInDescription_RejectsSmallFigures(t *testing.T) {
	if s := InDescription("Requires CAD 3 - 5 years of experience."); s != nil {
		t.Errorf("expected nil for small range, got %+v", s)
	}
}

func TestInDescriptionWithDefault_LabeledBareRangeInheritsBadgeCurrency(t *testing.T) {
	// "Salary: $190K/yr - $300K/yr" — bare "$" with a Salary label. The badge
	// already supplied CAD, so the description range should override the badge
	// and inherit CAD rather than defaulting to USD.
	desc := "About the role.\nSalary: $190K/yr - $300K/yr\nWe offer equity."

	s := InDescriptionWithDefault(desc, "CAD")
	if s == nil {
		t.Fatal("expected a salary from the labeled bare-$ range")
	}
	if s.Low == nil || *s.Low != 190000 {
		t.Errorf("low = %v, want 190000", s.Low)
	}
	if s.High == nil || *s.High != 300000 {
		t.Errorf("high = %v, want 300000", s.High)
	}
	if s.Currency != "CAD" {
		t.Errorf("currency = %q, want CAD (inherited from badge)", s.Currency)
	}
}

func TestInDescriptionWithDefault_NoLabelStillStrict(t *testing.T) {
	// A bare "$" range with NO Salary label and NO currency signal must not
	// match even when a default currency is supplied — avoids grabbing
	// incidental figures. Only the strict currency-stated path or a labeled
	// range can override.
	if s := InDescriptionWithDefault("We pay $200k - $250k depending on experience.", "CAD"); s != nil {
		t.Errorf("expected nil for unlabeled bare-$ range, got %+v", s)
	}
}

func TestInDescriptionWithDefault_StrictRangeWinsOverLabeledBare(t *testing.T) {
	// When both a currency-stated range and a labeled bare-$ range are present,
	// the explicit-currency one is authoritative and is returned first.
	desc := "Pay band: $205,600 - $257,000 CAD\nAlso: Salary: $190k - $300k"

	s := InDescriptionWithDefault(desc, "USD")
	if s == nil {
		t.Fatal("expected the currency-stated range to win")
	}
	if s.Currency != "CAD" {
		t.Errorf("currency = %q, want CAD (from explicit range)", s.Currency)
	}
	if s.High == nil || *s.High != 257000 {
		t.Errorf("high = %v, want 257000", s.High)
	}
}

func TestInDescriptionWithDefault_NoDefaultSkipsLabeledBare(t *testing.T) {
	// Without a default currency (no badge to inherit), a labeled bare-$ range
	// is not matched — we can't know the currency, so we don't guess.
	if s := InDescriptionWithDefault("Salary: $190k - $300k", ""); s != nil {
		t.Errorf("expected nil when no default currency, got %+v", s)
	}
}

func TestInDescription_TrailingISOOnEachAmount(t *testing.T) {
	// "$125,000 USD - $165,000 USD" — each amount carries its own trailing ISO
	// code, with the dash between amount-1's "USD" and amount-2's "$". This is
	// the authoritative employer-stated range and must override the LinkedIn
	// salary badge (which frequently shows a different, generic band).
	desc := "About the role.\nSalary Range US: $125,000 USD - $165,000 USD\nWe offer equity."

	s := InDescription(desc)
	if s == nil {
		t.Fatal("expected a salary from the per-amount trailing-ISO range")
	}
	if s.Low == nil || *s.Low != 125000 {
		t.Errorf("low = %v, want 125000", s.Low)
	}
	if s.High == nil || *s.High != 165000 {
		t.Errorf("high = %v, want 165000", s.High)
	}
	if s.Currency != "USD" {
		t.Errorf("currency = %q, want USD", s.Currency)
	}
}
