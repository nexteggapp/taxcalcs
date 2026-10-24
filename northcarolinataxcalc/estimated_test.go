// North Carolina's estimated tax rules for 2026, checked against the
// Department of Revenue's own documents and the General Statutes.
//
// North Carolina publishes no worked interest example. Form D-422 defines the
// line arithmetic, and these tests follow it: each part of each shortfall,
// by the day ÷ 365, at the rate, from its due date until paid or April 15.
// Every threshold, rate, date and percentage is compared against its extract
// in testdata/. The cases are hand-written.
package northcarolinataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/northcarolinataxcalc"
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
	return must(northcarolinataxcalc.EstimatedTax(2026))
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
	noTax       bool
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *northcarolinataxcalc.EstimatedFacts
	returnFiled estimated.Date
	elections   *estimated.Elections
	annualised  []money.USCents
	farmShare   money.Rate
	fishShare   money.Rate
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts("NC", 2026).FilingStatus(filing.Single).ThisYear(figures).
		Paid(h.payments...).ReturnFiled(h.returnFiled).FarmShare(h.farmShare).FishShare(h.fishShare)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
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

func prior(tax money.USCents, months int) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Months(months).Figures(must(estimated.NewFigures().Tax(tax).Build())).Build())
	return &p
}

func penalty(t *testing.T, h household) working.Result[estimated.Penalty] {
	t.Helper()
	return must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "gs-105-163.15.txt",
		"(i) the fifteenth day of the fourth month following the close of the taxable year",
		"A payment of estimated tax shall be credited against unpaid required installments in the order in which such installments are required to be paid.",
		"(1) First installment – April 15 of taxable year; (2) Second installment – June 15 of taxable year; (3) Third installment – September 15 of taxable year; and (4) Fourth installment – January 15 of following taxable year.",
		"the amount of any required installment shall be twenty‑five percent (25%) of the required annual payment.",
		"(1) Ninety percent (90%) of the tax shown on the return for the taxable year",
		"(2) One hundred percent (100%) of the tax shown on the return of the individual for the preceding taxable year, if the preceding taxable year was a taxable year of 12 months and the individual filed a return for that year.",
		"(1) First installment – twenty‑two and one‑half percent (22.5%); (2) Second installment – forty‑five percent (45%); (3) Third installment – sixty‑seven and one‑half percent (67.5%); and (4) Fourth installment – ninety percent (90%).",
		"is less than the amount set in section 6654(e) of the Code or if the individual did not have any liability for tax under Part 2 of Article 4 for the preceding taxable year.",
		"an equal part of that amount is considered to have been paid on each due date of the taxable year",
		"If, on or before January 31 of the following taxable year, the taxpayer files a return",
		"shown on the return of the individual for the preceding taxable year is at least sixty‑six and two‑thirds percent",
	)
	mustContain(t, "d-422-2025.txt",
		"6. Line 3 minus Line 5. If less than $1,000, stop; do not complete this form. You do not owe interest.",
		"8. Required annual payment. Enter the smaller of Line 4 or Line 7.",
		"16. Divide Line 8 by 4 and enter the result in each column.",
		"(If the payment due date falls on a Saturday, Sunday or holiday, the",
		"payment is due on or before the next business day.)",
		"on Line 23 X on Line 25 X .07",
	)
	mustContain(t, "d-422-2025-instructions.txt",
		"If you file your 2025 return and pay the tax due by January 31, 2026, include on Column (d) the amount of tax you paid with your return.",
		"enter 365, 304, 212, and 90 respectively in columns (a), (b), (c), and (d).",
		"gross income from all sources for 2024 or 2025",
		"You did not have any individual income tax liability for 2024.",
	)
	mustContain(t, "nc-40-2026.txt", "For Tax Years Beginning on or after January 1, 2026",
		"estimated income tax at that time, 1/4 of the balance on September 15", "If at least two-thirds of your estimated gross income is")
	mustContain(t, "ncdor-interest-rate.txt", "7% Interest Rate for July 1, 2026 through December 31, 2026", "7% | January 1, 2025 through December 31, 2026 |")
	mustContain(t, "ncdor-estimated-income-tax.txt", "is $1,000 or more regardless of the amount of income you have that is not subject to withholding.")
	mustContain(t, "gs-105-241.21.txt", "less than five percent (5%) per year and may not exceed sixteen percent (16%) per year.")
	mustContain(t, "gs-105-237.txt", "on taxes imposed prior to or during a period for which a taxpayer has declared bankruptcy")
}

// Nothing paid on $9,000 required: each quarter's $2,250 at 7% by the day
// to December 31, 260, 199 and 107 days. North Carolina's 7% covers all of
// 2026; the boundary is January 1, 2027, whose rate is not published, and
// the days after it are not charged. D-422 counts 365, 304, 212 and 90 days
// to April 15 once that rate exists.
func TestEstimatedInterestArithmetic(t *testing.T) {
	res := penalty(t, household{tax: 10_000 * money.Dollar})
	wantDays := []int{260, 199, 107}
	wantCharge := []money.USCents{11_219, 8_587, 4_617}
	lines := res.Value().Lines()
	for i, w := range wantDays {
		parts := lines[i].Parts()
		if len(parts) != 1 || parts[0].Days() != w || parts[0].Charge() != wantCharge[i] || parts[0].Rate() != money.Percent(7) {
			t.Errorf("installment %d: %d parts; want one of %d days, %v at 7%%", i+1, len(parts), w, wantCharge[i])
		}
	}
	if res.Value().Total() != 24_423 || !because(res.Required(), working.PositionUnpublishedRate, "Dec 31, 2026") {
		t.Errorf("interest %v; want $244.23 and the 2027 rate reported", res.Value().Total())
	}
}

// A payment stops the charge on what it pays: April's $2,250 paid May 15
// runs 30 days, $12.95.
func TestEstimatedPaymentStopsTheCharge(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 5, 15), 2_250*money.Dollar), pay(dues[1], 2_250*money.Dollar), pay(dues[2], 2_250*money.Dollar), pay(dues[3], 2_250*money.Dollar)}}
	res := penalty(t, h)
	if l := res.Value().Lines(); len(l) != 1 || l[0].Days() != 30 || res.Value().Total() != 1_295 {
		t.Errorf("interest %v over %v; want $12.95 for 30 days", res.Value().Total(), l)
	}
}

// The statute's dates, a quarter each, and none moves.
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
}

// Filed and paid by January 31, 2027, the tax counts in the January column:
// the earlier shortfalls stop on January 15 and January's bears nothing.
// January 31 is a Sunday and no source says whether it moves.
func TestEstimatedJanuaryThirtyFirst(t *testing.T) {
	res := penalty(t, household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 1, 29)})
	if res.Value().AsOf() != day(2027, 1, 15) || res.Value().Total() != 24_423 {
		t.Errorf("filed January 29: to %v, %v; want January 15 and $244.23", res.Value().AsOf(), res.Value().Total())
	}
	for _, tc := range []struct {
		filed   estimated.Date
		moves   bool
		excused bool
		asked   bool
	}{
		{day(2027, 1, 31), false, true, false},
		{day(2027, 2, 1), false, false, true},
		{day(2027, 2, 1), true, true, false},
	} {
		h := household{tax: 10_000 * money.Dollar, returnFiled: tc.filed}
		if tc.moves {
			e := must(northcarolinataxcalc.NewEstimatedFacts().JanuaryThirtyFirstMoves(true).Build())
			h.extras = &e
		}
		res := must(rules(t).Schedule(h.facts(t)))
		if got := res.Value()[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v, moves %v: excused %v, want %v", tc.filed, tc.moves, got, tc.excused)
		}
		if got := because(res.Required(), working.PositionJurisdictionFact, "a Sunday"); got != tc.asked {
			t.Errorf("filed %v, moves %v: asked %v, want %v", tc.filed, tc.moves, got, tc.asked)
		}
	}
}

// Tax less withholding under $1,000 owes nothing.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{{900_001, true}, {900_000, false}} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v", tc.withheld, res.Value().Exempt(), tc.exempt)
		}
		// The exception is the chosen option, at $0.
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding under $1,000") {
			t.Errorf("withheld %v: chose %v %q at %v", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
	}
}

// The lesser of 90% of this year's tax and 100% of last year's, from a filed
// twelve-month return.
func TestEstimatedTargetOptions(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(8_999*money.Dollar, 12)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || c.Percent() != money.Percent(100) || res.Value().Target() != 8_999*money.Dollar {
		t.Errorf("chose %v for %v; want 100%% of last year's $8,999", c.Basis(), res.Value().Target())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(9_001*money.Dollar, 12)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 9_000*money.Dollar {
		t.Errorf("chose %v for %v; want 90%%, $9,000", c.Basis(), res.Value().Target())
	}
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	for name, p := range map[string]*estimated.PriorReturn{"none on record": nil, "not filed": &notFiled, "eleven months": prior(100, 11)} {
		if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p}.facts(t))); res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
	}
}

// No liability last year means no interest, with no twelve-month or
// residence condition.
func TestEstimatedNoLiabilityException(t *testing.T) {
	for name, p := range map[string]*estimated.PriorReturn{"twelve months": prior(0, 12), "six months": prior(0, 6)} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p}.facts(t)))
		if !res.Value().Exempt() {
			t.Errorf("%s: %s", name, res.Value().Why())
		}
		if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisException || c.Label() != "No North Carolina tax liability last year" {
			t.Errorf("%s: chose %v %q", name, c.Basis(), c.Label())
		}
		if p := penalty(t, household{tax: 10_000 * money.Dollar, prior: p}).Value(); p.Total() != 0 || p.Reason() != "No North Carolina tax liability last year." {
			t.Errorf("%s: penalty %v because %q", name, p.Total(), p.Reason())
		}
	}
	nr := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &nr}.facts(t))); !res.Value().Exempt() {
		t.Errorf("not required to file: %s", res.Value().Why())
	}
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(1, 12)}.facts(t))); res.Value().Exempt() {
		t.Error("one cent of liability last year is exempt")
	}
}

// Withholding counts in equal parts unless the filer establishes the dates.
func TestEstimatedWithholding(t *testing.T) {
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar}); p.Value().Total() != 0 {
		t.Errorf("spread evenly: interest %v", p.Value().Total())
	}
	asWithheld := must(estimated.NewElections().WithholdingAsWithheld().Build())
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar, elections: &asWithheld}); p.Value().Total() == 0 {
		t.Error("on its December date: no interest")
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

// Without this year's tax, the target is last year's alone and the interest
// is the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000*money.Dollar, 12)}
	res := must(rules(t).Requirement(h.facts(t)))
	if r := res.Value(); !r.Required() || r.Target() != 8_000*money.Dollar || r.Options()[0].WhyNot() != estimated.NeedsThisYearTax || !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("required %v, target %v", r.Required(), r.Target())
	}
	if p := penalty(t, h); !(p.Value().UpperBound() || !p.Value().RatedThrough().IsZero()) || p.Value().Total() == 0 {
		t.Errorf("interest %v, upper bound %v", p.Value().Total(), p.Value().UpperBound())
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
	// None of them changes anything for a household that filed nothing early,
	// has no farm income and was required from the start.
	for _, d := range res.Divergences() {
		if d.AffectsHousehold() {
			t.Errorf("%q affects a plain household", d.Rule())
		}
	}
	filed := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 1, 29)}.facts(t)))
	for _, d := range filed.Divergences() {
		if d.Rule() == "A return filed and paid by January 31" && (!d.AffectsHousehold() || d.Uses() != working.ReadingForm) {
			t.Errorf("filed January 29: %q affects %v, uses %v", d.Rule(), d.AffectsHousehold(), d.Uses())
		}
	}
	want := map[string][2]string{
		"A return filed and paid by January 31": {"January 15 column", "§ 105-163.15(h)"},
		"Due dates on a weekend or holiday":     {"next business day", "§ 105-163.15(c)"},
		"A requirement that arises late":        {"a quarter in every column", "§ 105-163.15(d)"},
		"Who counts as a farmer":                {"2024 or 2025", "§ 105-163.15(i)(3)"},
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
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := northcarolinataxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
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
	last := must(northcarolinataxcalc.NewEstimatedFacts().FarmerLastYear(true).Build())
	if _, err := rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &last}.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer last year: %v", err)
	}
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, fishShare: 1000}.facts(t))); !because(res.Required(), working.PositionJurisdictionFact, "farmers' rules") {
		t.Error("some fishing income and last year not given: not asked")
	}
	this := must(estimated.NewFigures().Tax(100_000).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts("NC", 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts("NC", 2026).FilingStatus(filing.Single).ThisYear(this).State(7).Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}
	k := estimated.WaiverCasualty
	res := must(rules(t).Penalty(must(estimated.NewFacts("NC", 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(10_000*money.Dollar).Build())).
		Positions(must(estimated.NewPositions().Waiver(k).Build())).Build()), day(2027, 4, 15)))
	applied := ""
	for _, r := range res.Applied() {
		applied += r.Applied()
	}
	if !strings.Contains(applied, "bankruptcy") || res.Value().Total() == 0 {
		t.Error("a casualty waiver request does not say only bankruptcy waives interest")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$1,000", "7%", "110%", "half-year", "January 15", "casualty, retirement or disability"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
// never as a negative amount.
func TestEstimatedWithholdingOverTheTaxReadsAsARefund(t *testing.T) {
	figures := func(tax money.USCents) estimated.Figures {
		return must(estimated.NewFigures().Tax(tax).FederalAGI(60_000 * money.Dollar).StateAGI(60_000 * money.Dollar).Build())
	}
	last := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800 * money.Dollar)).Build())
	b := estimated.NewFacts("NC", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
	for m := time.January; m <= time.December; m++ {
		b.Withheld(must(estimated.NewWithheld().On(estimated.MustDate(2026, m, 28)).Amount(300 * money.Dollar).Build()))
	}
	res := must(rules(t).Requirement(must(b.Build())))
	r := res.Value()
	if !strings.Contains(r.Why(), "a $600 refund") {
		t.Errorf("why = %q, want it to read \"a $600 refund\"", r.Why())
	}
	texts := []string{r.Why()}
	for _, o := range r.Options() {
		texts = append(texts, o.Label(), o.WhyNot())
	}
	for _, q := range res.Required() {
		texts = append(texts, q.Because(), q.Question(), q.Effect())
	}
	for _, d := range res.Divergences() {
		texts = append(texts, d.Rule(), d.Form(), d.Statute())
	}
	for _, s := range texts {
		if strings.Contains(s, "−$") || strings.Contains(s, "-$") {
			t.Errorf("a negative amount where a refund belongs: %q", s)
		}
	}
}

// North Carolina waives this interest only for a period of bankruptcy,
// N.C. Gen. Stat. § 105-237(a)(2), so even a retirement the federal rules
// would waive is refused.
func TestEstimatedWaiverOutcomes(t *testing.T) {
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
		{"retired in 2026 at 64", "refused", "only for a period of bankruptcy", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(born)},
		{"retired in 2020", "refused", "only for a period of bankruptcy", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(born)},
		{"disaster", "refused", "casualty, disaster, retirement or disability doesn't waive", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1))},
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
