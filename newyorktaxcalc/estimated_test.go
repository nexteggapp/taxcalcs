// New York's estimated tax rules for 2026, checked against the Department of
// Taxation and Finance's own documents and the Consolidated Laws.
//
// New York publishes no worked penalty example for Part 3 of Form IT-2105.9,
// the regular method; the short method's one example uses the 2025 rate. The
// tests follow the instructions' arithmetic: each part of each shortfall, by
// the day ÷ 365, at each period's rate, a period ending at the payment, the
// end of the rate quarter or the next due date. Every threshold, rate, date
// and percentage is compared against its extract in testdata/. The cases are
// hand-written.
package newyorktaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/newyorktaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

var day = estimated.MustDate

func extract(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return strings.Join(strings.Fields(string(raw)), " ")
}

func mustContain(t *testing.T, name string, phrases ...string) {
	t.Helper()
	text := extract(t, name)
	for _, p := range phrases {
		if !strings.Contains(text, p) {
			t.Errorf("%q is not in %s", p, name)
		}
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func rules(t *testing.T) estimated.EstimatedTax {
	t.Helper()
	return must(newyorktaxcalc.EstimatedTax(2026))
}

func because(rs []working.Required, k working.PositionKind, phrase string) bool {
	for _, r := range rs {
		if r.Position() == k && r.Asked() && (strings.Contains(r.Question(), phrase) || strings.Contains(r.Effect(), phrase)) {
			return true
		}
	}
	return false
}

func has(rs []working.Required, k working.PositionKind) bool { return because(rs, k, "") }

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

type household struct {
	tax         money.USCents
	local       money.USCents
	noTax       bool
	status      filing.Status
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *newyorktaxcalc.EstimatedFacts
	returnFiled estimated.Date
	elections   *estimated.Elections
	annualised  []money.USCents
	farmShare   money.Rate
	fishShare   money.Rate
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	fb := estimated.NewFigures().Tax(h.tax).LocalTax(h.local)
	if h.noTax {
		fb = estimated.NewFigures()
	}
	status := h.status
	if status == filing.Unspecified {
		status = filing.Single
	}
	b := estimated.NewFacts("NY", 2026).FilingStatus(status).ThisYear(must(fb.Build())).
		Paid(h.payments...).ReturnFiled(h.returnFiled).FarmShare(h.farmShare).FishShare(h.fishShare)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(newyorktaxcalc.NewEstimatedFacts().SubjectTo(false, false, false).Build()))
	}
	if h.elections != nil {
		b.Elections(*h.elections)
	}
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		b.Elections(must(estimated.NewElections().Annualised().Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

func prior(tax, agi money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).StateAGI(agi).Build())).FullYearResident(true).Build())
	return &p
}

func penalty(t *testing.T, h household) working.Result[estimated.Penalty] {
	t.Helper()
	return must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
}

func facts(subjectTo ...bool) *newyorktaxcalc.EstimatedFacts {
	e := must(newyorktaxcalc.NewEstimatedFacts().SubjectTo(subjectTo[0], subjectTo[1], subjectTo[2]).Build())
	return &e
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "tax-law-685-c-d.txt",
		"due on April fifteenth, June fifteenth and September fifteenth of such taxable year and on January fifteenth of the following taxable year.",
		"the fifteenth day of the fourth month following the close of the taxable year",
		"a payment of estimated tax shall be credited against unpaid required installments in the order in which such installments are required to be paid.",
		"the amount of any required installment shall be twenty-five percent of the required annual payment.",
		"(i) ninety percent of the tax shown on the return for the taxable year",
		"(ii) one hundred percent of the tax shown on the return of the individual for the preceding taxable year.",
		"Clause (ii) of this subparagraph shall not apply if the preceding taxable year was not a taxable year of twelve months or if the individual did not file a return for such preceding taxable year.",
		`exceeds one hundred fifty thousand dollars, clause (ii) of subparagraph (B) of this paragraph shall be applied by substituting "one hundred ten percent" for "one hundred percent".`,
		`substituting "seventy-five thousand dollars" for "one hundred fifty thousand dollars".`,
		"twenty-two and one-half percent in the case of the first installment, forty-five percent in the case of the second installment, sixty-seven and one-half percent in the case of the third installment and ninety percent in the case of the fourth installment",
		"an equal part of such amount shall be deemed paid on each installment due date",
		"If, on or before January thirty-first of the following taxable year, the taxpayer files a return",
		"is less than three hundred dollars.",
		"throughout the preceding taxable year the individual was a resident of this state or a nonresident or part-year resident who had New York source income.",
		"by reason of casualty, disaster or other unusual circumstances",
		"the taxpayer retired after having attained age sixty-two or became disabled",
	)
	mustContain(t, "tax-law-697-j.txt",
		"plus (ii) five and one-half percentage points.",
		"the underpayment rate shall not be less than seven and one-half percent per annum.",
		"the federal short-term rate which applies during the third month following the taxable year shall also apply during the first fifteen days of the",
		"The preceding sentence shall not apply for purposes of computing the amount of any addition to tax for failure to pay estimated tax under subsection (c) of section six hundred eighty-five.",
	)
	mustContain(t, "dtf-interest-rates-2026.txt",
		"Interest rates: 4/1/2026–6/30/2026", "For purposes of Tax Law section 697(j), the underpayment rate for this period is 8.5%.",
		"Interest rates: 7/1/2026–9/30/2026", "For purposes of Tax Law section 697(j), the underpayment rate for this period is 9.5%.",
		"Interest rates: 10/1/2026–12/31/2026", "For purposes of section 697(j) of the Tax Law, the underpayment rate for this period is 9.5%.",
	)
	mustContain(t, "it-2105-i-2026.txt",
		"pay it in four equal installments on April 15, June 15, September 15, 2026, and January 15, 2027.",
		"If you file your 2026 New York State income tax return on or before January 31, 2027, and pay the full balance of tax due, you do not have to make any payment which would otherwise be due on January 15, 2027.",
		"[NYAGI] [or net earnings from self-employment allocated to the MCTD] shown on that return is more than $150,000 [$75,000 if",
		"50%(.50) of line 28",
		"The current rate for Yonkers is 16.75% (.1675).",
	)
	mustContain(t, "it-2105.9-i-2025.txt",
		"the tax due (after deducting tax withheld) is less than $300.",
		"a) New York State tax (enter 1)",
		"b) New York City tax (enter 1)",
		"c) Yonkers tax (enter 1)",
		"d) MCTMT (enter 0)",
		"f) Multiply line e by $300",
		"Income taxes withheld from your salary, pension, etc., are considered payments of estimated tax in equal installments on the four installment dates, unless you prove otherwise.",
		"to and including the date of payment, the last date in the penalty rate period or the next installment due date, whichever is earlier.",
		"The penalty rate will be the federal short-term rate plus five and one half percentage points, but not less than 7.5%.",
		"If you use Worksheet 1 (and Worksheets 2, 3, 4a, 4b, and 4c if applicable) for any payment due date, you must use it for all payment due dates.",
		"If your federal gross income from farming or fishing is at least two-thirds of your federal gross income for 2024 or 2025, you cannot use this form.",
	)
	mustContain(t, "general-construction-law-25-a.txt", "ends on a Saturday, Sunday or a public holiday, such act may be done on the next succeeding business day")
}

// Nothing paid on $9,000 required. April's $2,250 is charged in five
// periods: 61 and 15 days at 8.5%, then from July 1 at 9.5% for 77, 15 and
// 92 days; the rate change splits it. The days from January 1, 2027 have no
// published rate.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	res := penalty(t, household{tax: 10_000 * money.Dollar})
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	april := lines[0].Parts()
	wantDays := []int{61, 15, 77, 15, 92}
	wantRates := []money.Rate{850, 850, 950, 950, 950}
	wantCharges := []money.USCents{3_196, 786, 4_509, 878, 5_388}
	if len(april) != len(wantDays) {
		t.Fatalf("April: %d parts, want %d", len(april), len(wantDays))
	}
	for i, p := range april {
		if p.Days() != wantDays[i] || p.Rate() != wantRates[i] || p.Charge() != wantCharges[i] {
			t.Errorf("April part %d: %d days at %v, %v; want %d at %v, %v", i, p.Days(), p.Rate(), p.Charge(), wantDays[i], wantRates[i], wantCharges[i])
		}
	}
	for i, want := range []money.USCents{14_757, 11_561, 6_266, 0} {
		if lines[i].Total() != want {
			t.Errorf("installment %d: %v, want %v", i+1, lines[i].Total(), want)
		}
	}
	if res.Value().Total() != 32_584 || !because(res.Required(), working.PositionUnpublishedRate, "December 31, 2026") {
		t.Errorf("penalty %v; want $325.84 and the 2027 rate reported", res.Value().Total())
	}
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked")
	}
}

// A payment stops the charge on what it pays: April's $2,250 paid May 1 runs
// 16 days at 8.5%, $8.38.
func TestEstimatedPaymentStopsTheCharge(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 5, 1), 2_250*money.Dollar), pay(dues[1], 2_250*money.Dollar), pay(dues[2], 2_250*money.Dollar), pay(dues[3], 2_250*money.Dollar)}}
	res := penalty(t, h)
	if l := res.Value().Lines(); len(l) != 1 || l[0].Days() != 16 || res.Value().Total() != 838 {
		t.Errorf("penalty %v over %v; want $8.38 for 16 days", res.Value().Total(), l)
	}
}

// The dates § 685(c)(1) gives, a quarter each, and none moves. January 31,
// 2027 is a Sunday: IT-2105-I prints the date and does not move it, so a
// return filed on February 1 does not take the penalty off January.
func TestEstimatedScheduleForTwentyTwentySix(t *testing.T) {
	s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t))).Value()
	for i, in := range s {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
		if want := money.USCents(i+1) * 2_250 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 1, 31), true}, {day(2027, 2, 1), false}} {
		s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: tc.filed}.facts(t))).Value()
		if got := s[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
	}
}

// $300 for each of the state, City and Yonkers taxes; the MCTMT adds none.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		extras   *newyorktaxcalc.EstimatedFacts
		withheld money.USCents
		exempt   bool
	}{
		{facts(false, false, false), 970_001, true},
		{facts(false, false, false), 970_000, false},
		{facts(true, false, false), 940_001, true},
		{facts(true, false, false), 940_000, false},
		{facts(true, true, false), 910_001, true},
		{facts(false, false, true), 970_000, false},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld, extras: tc.extras}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
	}
	// Under the threshold the exception is the chosen option, at $0.
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: 945_000, extras: facts(true, false, false)}.facts(t)))
	if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding under $600" {
		t.Errorf("chosen %v: %v at %v labelled %q", ok, c.Basis(), c.Amount(), c.Label())
	}
	if !strings.Contains(res.Value().Why(), "leaves $550. That is under $600. The threshold is $300 for each") {
		t.Errorf("why %q", res.Value().Why())
	}
	none := must(newyorktaxcalc.NewEstimatedFacts().Build())
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, local: 1_000 * money.Dollar, extras: &none}.facts(t))); !because(res.Required(), working.PositionJurisdictionFact, "New York City and Yonkers") {
		t.Error("which taxes apply is not asked")
	}
}

// 100% of last year's tax, or 110% above $150,000 of last year's New York
// AGI ($75,000 separately) or of MCTD self-employment earnings.
func TestEstimatedPriorYearStep(t *testing.T) {
	for _, tc := range []struct {
		status filing.Status
		agi    money.USCents
		want   money.USCents
	}{
		{filing.Single, 15_000_000, 8_000 * money.Dollar},
		{filing.Single, 15_000_001, 8_800 * money.Dollar},
		{filing.MarriedFilingSeparately, 7_500_000, 8_000 * money.Dollar},
		{filing.MarriedFilingSeparately, 7_500_001, 8_800 * money.Dollar},
	} {
		res := must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, status: tc.status, prior: prior(8_000*money.Dollar, tc.agi)}.facts(t)))
		if res.Value().Target() != tc.want {
			t.Errorf("%v, AGI %v: target %v, want %v", tc.status, tc.agi, res.Value().Target(), tc.want)
		}
	}
	mctd := must(newyorktaxcalc.NewEstimatedFacts().SubjectTo(false, false, true).PriorYearMCTDEarnings(15_000_001).Build())
	if res := must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: prior(8_000*money.Dollar, 100_000*money.Dollar), extras: &mctd}.facts(t))); res.Value().Target() != 8_800*money.Dollar {
		t.Errorf("MCTD earnings over $150,000: target %v, want $8,800", res.Value().Target())
	}
	unknown := must(newyorktaxcalc.NewEstimatedFacts().SubjectTo(false, false, true).Build())
	res := must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: prior(8_000*money.Dollar, 100_000*money.Dollar), extras: &unknown}.facts(t)))
	if res.Value().Target() != 8_800*money.Dollar || !because(res.Required(), working.PositionJurisdictionFact, "MCTD") {
		t.Errorf("MCTD earnings not given: target %v", res.Value().Target())
	}
	noAGI := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(8_000 * money.Dollar).Build())).Build())
	if res := must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: &noAGI}.facts(t))); res.Value().Target() != 8_800*money.Dollar || !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Errorf("no AGI: target %v", res.Value().Target())
	}
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(100).StateAGI(1).Build())).Build())
	for name, p := range map[string]*estimated.PriorReturn{"none on record": nil, "not filed": &notFiled, "eleven months": &short} {
		if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p}.facts(t))); res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
	}
}

// No liability last year excuses the penalty for a resident, or a
// nonresident with New York source income.
func TestEstimatedNoLiabilityException(t *testing.T) {
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0, 10_000*money.Dollar)}.facts(t))); !res.Value().Exempt() {
		t.Errorf("resident: %s", res.Value().Why())
	}
	nonresident := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(0).Build())).FullYearResident(false).Build())
	source := must(newyorktaxcalc.NewEstimatedFacts().SubjectTo(false, false, false).PriorYearNewYorkSourceIncome(true).Build())
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &nonresident, extras: &source}.facts(t))); !res.Value().Exempt() {
		t.Errorf("nonresident with New York source income: %s", res.Value().Why())
	}
	noSource := must(newyorktaxcalc.NewEstimatedFacts().SubjectTo(false, false, false).PriorYearNewYorkSourceIncome(false).Build())
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &nonresident, extras: &noSource}.facts(t))); res.Value().Exempt() {
		t.Error("nonresident without New York source income is exempt")
	}
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &nonresident}.facts(t)))
	if res.Value().Exempt() || !has(res.Required(), working.PositionResidency) {
		t.Errorf("source income not given: exempt %v", res.Value().Exempt())
	}
}

// Withholding counts in equal parts unless the filer proves the dates.
func TestEstimatedWithholding(t *testing.T) {
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar}); p.Value().Total() != 0 {
		t.Errorf("spread evenly: penalty %v", p.Value().Total())
	}
	asWithheld := must(estimated.NewElections().WithholdingAsWithheld().Build())
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar, elections: &asWithheld}); p.Value().Total() == 0 {
		t.Error("on its December date: no penalty")
	}
}

// The annualized method: 22.5%, 45%, 67.5% and 90%, with recapture.
func TestEstimatedAnnualizedMethod(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, annualised: []money.USCents{2_000 * money.Dollar, 4_000 * money.Dollar, 8_000 * money.Dollar, 10_000 * money.Dollar}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{450 * money.Dollar, 1_800 * money.Dollar, 5_400 * money.Dollar, 9_000 * money.Dollar} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, s[i].NeededBy(), want)
		}
	}
}

// Without this year's tax, the target is last year's alone and the penalty is
// the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000*money.Dollar, 100_000*money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	if r := res.Value(); !r.Required() || r.Target() != 8_000*money.Dollar || r.Options()[0].WhyNot() != estimated.NeedsThisYearTax || !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("required %v, target %v", r.Required(), r.Target())
	}
	if p := penalty(t, h); !(p.Value().UpperBound() || !p.Value().RatedThrough().IsZero()) || p.Value().Total() == 0 {
		t.Errorf("penalty %v, upper bound %v", p.Value().Total(), p.Value().UpperBound())
	}
	none := household{noTax: true}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v", err)
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"January 31, 2027 is a Sunday":   {"not moved", "§ 25-a"},
		"Casualty or disaster":           {"no waiver for a casualty", "§ 685(d)(4)(A)"},
		"A requirement that arises late": {"25% in every column", "§ 685(c)(3)(A)"},
	}
	if len(res.Divergences()) != len(want) {
		t.Errorf("%d divergences, want %d", len(res.Divergences()), len(want))
	}
	for _, d := range res.Divergences() {
		w, ok := want[d.Rule()]
		if !ok {
			t.Errorf("unexpected divergence %q", d.Rule())
			continue
		}
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.StatuteSource(), w[1]) || d.FormSource() == "" || d.Statute() == "" {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s)", d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
		}
		// No return filed, no waiver asked and an on-time start: neither
		// reading changes the figures. The casualty waiver follows the
		// statute, which grants it.
		want := working.ReadingForm
		if d.Rule() == "Casualty or disaster" {
			want = working.ReadingStatute
		}
		if d.Uses() != want || d.AffectsHousehold() {
			t.Errorf("%q uses %v, affects %v; want %v, not affecting", d.Rule(), d.Uses(), d.AffectsHousehold(), want)
		}
	}
	// Filed and paid on Monday, February 1, 2027: only the Sunday rule
	// changes the figures.
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 1)}.facts(t)))
	for _, d := range res.Divergences() {
		if d.AffectsHousehold() != (d.Rule() == "January 31, 2027 is a Sunday") {
			t.Errorf("filed February 1: %q affects %v", d.Rule(), d.AffectsHousehold())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := newyorktaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	for name, h := range map[string]household{
		"farmer": {tax: 10_000 * money.Dollar, farmShare: 7000},
		"fisher": {tax: 10_000 * money.Dollar, fishShare: 7000},
	} {
		if _, err := rules(t).Requirement(h.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	last := must(newyorktaxcalc.NewEstimatedFacts().FarmerLastYear(true).Build())
	if _, err := rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &last}.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer last year: %v", err)
	}
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, farmShare: 1000}.facts(t))); !because(res.Required(), working.PositionJurisdictionFact, "farmers' rules") {
		t.Error("some farm income and last year not given: not asked")
	}
	this := must(estimated.NewFigures().Tax(100_000).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts("NY", 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts("NY", 2026).FilingStatus(filing.Single).ThisYear(this).State(struct{}{}).Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}
	if _, err := newyorktaxcalc.NewEstimatedFacts().PriorYearMCTDEarnings(-1).Build(); err == nil {
		t.Error("negative MCTD earnings are accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$300", "New York City", "Yonkers", "MCTMT", "110%", "5½ points", "8.5%", "9.5%"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Withholding over the tax leaves a refund, never a negative balance.
func TestWithholdingOverTheTaxReadsAsARefund(t *testing.T) {
	var withheld []estimated.Withheld
	for m := time.January; m <= time.December; m++ {
		withheld = append(withheld, must(estimated.NewWithheld().On(day(2026, m, 28)).Amount(money.Dollars(300)).Build()))
	}
	figures := func(tax, agi int64) estimated.Figures {
		return must(estimated.NewFigures().Tax(money.Dollars(tax)).FederalAGI(money.Dollars(agi)).StateAGI(money.Dollars(agi)).Build())
	}
	prior := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800, 55_000)).Build())
	f := must(estimated.NewFacts(estimated.Jurisdiction("NY"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}

// Tax Law § 685(d)(4) grants the federal pair: a casualty or disaster, and
// retirement after 62 or disability in the tax year or the one before.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "it-2105.9-i-2025.txt", "you retired in 2024 or 2025 after reaching the age of 62, or you")
	mustContain(t, "tax-law-685-c-d.txt", "taxable year the taxpayer retired after having attained age sixty-two or")
	h := household{tax: 10_000 * money.Dollar}
	before := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value().Total()
	if before == 0 {
		t.Fatal("no penalty to waive")
	}
	born := day(1962, 3, 1)
	for _, tc := range []struct {
		name, want, phrase string
		pos                *estimated.PositionsBuilder
	}{
		{"retired in 2026 at 64", "eligible", "retired in 2026 after reaching 62", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(born)},
		{"retired in 2025 at 63", "eligible", "retired in 2025", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2025, 6, 1)).Born(born)},
		{"retired in 2020", "refused", "covers retiring in 2025 or 2026", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(born)},
		{"retired at 61", "refused", "reached 62", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1965, 1, 1))},
		{"disaster", "eligible", "The New York Tax Department can waive", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1))},
		{"disabled in 2023", "refused", "became disabled in 2023", estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2023, 11, 1))},
	} {
		pos := must(tc.pos.Build())
		h.positions = &pos
		res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		p := res.Value()
		o, ok := p.WaiverOutcome()
		got := map[bool]string{true: "eligible"}[o.Eligible()]
		if o.Refused() {
			got = "refused"
		}
		if o.Undecided() {
			got = "undecided"
		}
		waived := before
		if got == "eligible" {
			waived = 0
		}
		if !ok || got != tc.want || !strings.Contains(o.Reason(), tc.phrase) || p.Total() != before || p.IfWaived() != waived {
			t.Errorf("%s: %s, %q, total %v, if waived %v; want %s with %q, total %v", tc.name, got, o.Reason(), p.Total(), p.IfWaived(), tc.want, tc.phrase, before)
		}
	}
}
