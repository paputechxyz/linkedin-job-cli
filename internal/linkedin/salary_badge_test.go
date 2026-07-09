package linkedin

import (
	"testing"
)

// TestExtractSalaryBadge_SkipsSimilarJobs is the regression test for the
// misattribution bug: a detail page's "Similar jobs" rail reuses the same
// `.main-job-card__salary-info` class for *other* postings. The badge must not
// be lifted from that sidebar and attached to the job being viewed. The fixture
// mirrors the real markup observed on /jobs/view/<id>/ pages (topcard holds no
// salary; the only salary spans live inside <section class="...similar-jobs">).
func TestExtractSalaryBadge_SkipsSimilarJobs(t *testing.T) {
	html := `<html><body>
		<section class="top-card-layout container-lined overflow-hidden">
			<h1 class="topcard__title">Senior Full Stack Engineer</h1>
			<div class="topcard__flavor-row">
				<span class="topcard__flavor">Musashi AI North America</span>
				<span class="topcard__flavor topcard__flavor--bullet">Waterloo, Ontario, Canada</span>
			</div>
		</section>
		<section class="core-section-container my-3 similar-jobs">
			<h2 class="core-section-container__title">People also viewed</h2>
			<div>
				<h3 class="base-main-card__title">Senior Software Developer</h3>
				<span class="main-job-card__location">Kitchener, Ontario, Canada</span>
				<span class="main-job-card__salary-info">$100,000.00 - $140,000.00</span>
			</div>
		</section>
	</body></html>`

	cur, sal := extractSalaryBadge(docFrom(t, html))
	if sal != nil {
		t.Errorf("expected no badge (main job has none); got currency=%q low=%v high=%v",
			cur, ptrVal(sal.Low), ptrVal(sal.High))
	}
}

// TestExtractSalaryBadge_SkipsAsideSidebar covers the right-rail
// "People also viewed" / "Similar searches" sidebars, which use
// `.aside-section-container` and can also carry other postings' salary badges.
func TestExtractSalaryBadge_SkipsAsideSidebar(t *testing.T) {
	html := `<html><body>
		<section class="top-card-layout"><h1 class="topcard__title">Staff Engineer</h1></section>
		<section class="right-rail">
			<aside class="aside-section-container mb-4 people-also-viewed">
				<span class="main-job-card__salary-info">CA$151,000.00 - CA$200,000.00</span>
			</aside>
		</section>
	</body></html>`

	if _, sal := extractSalaryBadge(docFrom(t, html)); sal != nil {
		t.Errorf("expected no badge from aside sidebar; got %+v", sal)
	}
}

// TestExtractSalaryBadge_ReadsMainJobBadge confirms a badge that lives in the
// main job's own region (not inside a similar-jobs/aside container) is still
// captured with its currency.
func TestExtractSalaryBadge_ReadsMainJobBadge(t *testing.T) {
	html := `<html><body>
		<section class="top-card-layout container-lined overflow-hidden">
			<h1 class="topcard__title">Backend Engineer</h1>
			<div class="topcard__flavor-row">
				<span class="main-job-card__salary-info">$130,000.00 - $170,000.00</span>
			</div>
		</section>
		<section class="core-section-container my-3 similar-jbs">
			<span class="main-job-card__salary-info">$90,000.00 - $110,000.00</span>
		</section>
	</body></html>`

	cur, sal := extractSalaryBadge(docFrom(t, html))
	if sal == nil || sal.Low == nil || sal.High == nil {
		t.Fatalf("expected main-job badge parsed; got sal=%+v cur=%q", sal, cur)
	}
	if *sal.Low != 130000 || *sal.High != 170000 {
		t.Errorf("expected 130000-170000; got %v-%v", *sal.Low, *sal.High)
	}
	if cur != "USD" {
		t.Errorf("expected USD currency; got %q", cur)
	}
}

// TestExtractSalaryBadge_PrefersFirstCurrencyMatch mirrors the original
// preference logic: among main-region badges, a span carrying a currency signal
// ($/CAD/USD) wins over a bare-amount span.
func TestExtractSalaryBadge_PrefersFirstCurrencyMatch(t *testing.T) {
	html := `<html><body>
		<section class="top-card-layout">
			<span class="main-job-card__salary-info">Competitive</span>
			<span class="main-job-card__salary-info">$120,000.00 - $160,000.00</span>
		</section>
	</body></html>`

	_, sal := extractSalaryBadge(docFrom(t, html))
	if sal == nil || sal.High == nil || *sal.High != 160000 {
		t.Errorf("expected currency-bearing badge to win; got %+v", sal)
	}
}

func ptrVal(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
