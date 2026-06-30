package linkedin

import "testing"

func TestDescriptionSalary_CartaCADRange(t *testing.T) {
	// Verbatim slice of job 4380826388's description body. The salary badge
	// showed a different band (212,500-287,500 USD); the description carries
	// the authoritative localized range with a trailing CAD code.
	desc := `Experience: We recommend 10+ years of professional software engineering experience with a track record of high-level technical leadership.
Salary
Benefits
Carta’s compensation package includes a market competitive salary, equity for all full time roles, exceptional benefits, and, for applicable roles, commissions plans. Our expected cash compensation (salary + commission if applicable) range for this role is:
$205,600 - $257,000 CAD in Toronto, Ontario, Canada
We are hiring for multiple levels and locations, so final offers may vary from the amounts listed based on geography, experience and expertise, and other factors.`

	s := descriptionSalary(desc)
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

func TestDescriptionSalary_NoCurrencyReturnsNil(t *testing.T) {
	// Bare "$200k - $250k" with no currency signal must NOT override the badge.
	if s := descriptionSalary("We pay $200k - $250k depending on experience."); s != nil {
		t.Errorf("expected nil for bare-$ range, got %+v", s)
	}
}

func TestDescriptionSalary_RejectsSmallFigures(t *testing.T) {
	// Non-salary ranges (e.g. a 3-5 year experience band) must be rejected.
	if s := descriptionSalary("Requires CAD 3 - 5 years of experience."); s != nil {
		t.Errorf("expected nil for small range, got %+v", s)
	}
}
