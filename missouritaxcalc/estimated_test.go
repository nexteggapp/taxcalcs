// Missouri's estimated tax rules for 2026, checked against the Department of
// Revenue's own documents and the Revised Statutes of Missouri.
//
// Missouri publishes no worked penalty example. Form MO-2210 defines the line
// arithmetic, and these tests follow it: each column's total underpayment,
// by the day ÷ 365 at each calendar year's rate, from its due date until paid
// or the next due date. Its two exception examples have no penalty to check.
// Every threshold, rate, date and percentage is compared against its extract
// in testdata/. The cases themselves are hand-written.
package missouritaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/missouritaxcalc"
	"github.com/nexteggapp/taxcalcs/money"
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
	return must(missouritaxcalc.EstimatedTax(2026))
}

func because(rs []working.Required, k working.PositionKind, phrase string) bool {
	for _, r := range rs {
		if r.Position() == k && (strings.Contains(r.Question(), phrase) || strings.Contains(r.Effect(), phrase)) {
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

func quarterly(a ...money.USCents) []estimated.Payment {
	var out []estimated.Payment
	for i, x := range a {
		if x > 0 {
			out = append(out, pay(dues[i], x))
		}
	}
	return out
}

type household struct {
	tax         money.USCents
	noTax       bool
	withheld    money.USCents // withheld on December 15
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *missouritaxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	elections   *estimated.Elections
	annualised  []money.USCents
	fishShare   money.Rate
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts("MO", 2026).FilingStatus(filing.MarriedFilingJointly).ThisYear(figures).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled).FishShare(h.fishShare)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(missouritaxcalc.NewEstimatedFacts().NoCombatZone().Build()))
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
	mustContain(t, "mo-1040es-2026.txt",
		"You are required to file a declaration of estimated tax if your Missouri estimated tax is expected to be $100.00 or more (Section 143.521.1, RSMo).",
		"or in equal installments on or before April 15, June 15, Sep­tem­ber 15, and January 15.",
		"If the due date falls on a Saturday, Sunday, or legal holiday, the voucher will be considered timely if filed on the next business day.",
		"a) the tax shown on the preceding year’s return if that return was for a 12 month period and showed a tax liability; or b) 90% (66 2/3% in the case of a farmer) of the total amount due for the current year.",
	)
	mustContain(t, "mo-2210-2025.txt",
		"2. Multiply Line 1 by 90% (66 2/3% for qualified farmers)",
		"4. Subtract Line 3 from Line 1. If less than $500, stop here; do not complete or file this form.",
		"5. Enter the tax shown on your 2024 tax return.",
		"6. Required Annual Payment - Enter the smaller of Line 2 or Line 5",
		"April 15, 2025 June 15, 2025 Sept. 15, 2025 Jan. 15, 2026",
		"25% of 2024 Tax 50% of 2024 Tax 75% of 2024 Tax 100% of 2024 Tax",
		"22.5% of Tax 45% of Tax 67.5% of Tax",
		"90% of Tax 90% of Tax 90% of Tax",
		"Between Jan. 1 and Apr. 1 April 15 4 Between Apr. 2 and Jun. 1 June 15 3 Between Jun. 2 and Sept.1 Sept. 15 2 After Sept. 1 Jan. 15 1",
		"When the due date falls on a Saturday, Sunday, or legal holiday, the estimated tax payment will be considered timely if filed on the next business day.",
		"You may apply a different exception to each underpayment.",
		"The 2024 return must cover a period of 12 months and show a tax liability.",
		"Exception 3 may not be used for the fourth installment period.",
		"This exception does not apply to the fourth quarter.",
		"You may consider an equal part of the income tax withheld during the year as paid on each required installment date",
		"If you filed your return and paid the balance of tax due on or before January 31, 2026, consider the balance paid as of January 15, 2026.",
		"You may be exempt from a penalty for underpayment of estimated tax if you served in the U.S. Armed Forces in an area designated by the President as a combat zone",
		"If (1) your Missouri gross income from farming is at least two‑thirds of your total Missouri gross income",
		"Date of payment, due date of installment, or April 15, 2026, whichever is earlier",
		"Enter the date a payment was made on the installment, the due date of the following installment, or April 15, 2026, whichever is earlier.",
		"Divide this amount by 365 days and multiply the product by seven percent.",
	)
	mustContain(t, "rsmo-143.521.txt",
		"if the Missouri estimated tax can reasonably be expected to be at least one hundred dollars",
		"If on or before January thirty-first")
	mustContain(t, "rsmo-143.531.txt",
		"(1) After April first and before June second of the taxable year, the declaration shall be filed on or before June fifteenth",
		"(2) After June first and before September second of the taxable year, the declaration shall be filed on or before September fifteenth")
	mustContain(t, "rsmo-143.761.txt",
		"The amount of the installment which would be required to be paid if the estimated tax were equal to ninety percent",
		"The fifteenth day of the fourth month following the close of the taxable year",
		"if a return showing a liability for tax was filed for the preceding year and such preceding year was a taxable year of twelve months",
		"on the basis of the actual taxable income for the months in the taxable year ending before the month in which the installment is required to be paid as if such months constituted the taxable year",
		"an equal part of such amount shall be deemed paid on each installment date")
	mustContain(t, "rsmo-32.065.txt", "shall become effective on January first of the immediately succeeding year")
	mustContain(t, "12-csr-10-41.010-emergency-2026.txt", "2025 8% 2026 7%", "effective January 1, 2026")
	mustContain(t, "dor-statutory-interest-rates.txt", "2026 | 7% | 7% |", "2027 | 7% | 7% |")
}

// The 2026 dates are the printed ones and none moves. January 31, 2027 is a
// Sunday; the 2025 form printed January 31, 2026, a Saturday, without moving
// it, so a return filed on Monday, February 1 is not counted as paid on
// January 15.
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

// MO-2210 line 4 stops under $500; the statute's declaration threshold is
// $100. The form is followed, the gap is asked about, and the answer applies
// $100.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	statute := must(missouritaxcalc.NewEstimatedFacts().NoCombatZone().StatutoryThreshold(true).Build())
	for _, tc := range []struct {
		withheld money.USCents
		extras   *missouritaxcalc.EstimatedFacts
		exempt   bool
		asked    bool
	}{
		{950_001, nil, true, true}, // $499.99
		{950_000, nil, false, false},
		{990_001, nil, true, false}, // $99.99
		{990_001, &statute, true, false},
		{990_000, &statute, false, false}, // $100.00
		{960_000, &statute, false, false},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld, extras: tc.extras}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v (statute %v): exempt %v, want %v", tc.withheld, tc.extras != nil, res.Value().Exempt(), tc.exempt)
		}
		limit := "$500"
		if tc.extras != nil {
			limit = "$100"
		}
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding under "+limit) {
			t.Errorf("withheld %v: chosen %v %q at %v", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
		// Only tax less withholding from $100 to under $500 reads differently
		// under the statute.
		for _, d := range res.Divergences() {
			if want := tc.withheld == 950_001 || tc.withheld == 960_000 || tc.withheld == 990_000; d.Rule() == "The $500 stop" && d.AffectsHousehold() != want {
				t.Errorf("withheld %v: the $500 stop affects the household %v, want %v", tc.withheld, d.AffectsHousehold(), want)
			}
		}
		if got := because(res.Required(), working.PositionJurisdictionFact, "start at $100"); got != tc.asked {
			t.Errorf("withheld %v: asked %v, want %v", tc.withheld, got, tc.asked)
		}
	}
}

// The required annual payment is the smaller of 90% of this year's tax and
// 100% of last year's, which needs a twelve-month return showing a liability.
func TestEstimatedTargetOptions(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(8_000*money.Dollar, 12)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || res.Value().Target() != 8_000*money.Dollar {
		t.Errorf("chose %v for %v; want last year's $8,000", c.Basis(), res.Value().Target())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(9_500*money.Dollar, 12)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 9_000*money.Dollar {
		t.Errorf("chose %v for %v; want 90%%, $9,000", c.Basis(), res.Value().Target())
	}
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	for name, p := range map[string]*estimated.PriorReturn{"none on record": nil, "not filed": &notFiled} {
		if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p}.facts(t))); res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
	}
}

// Part I line 5 takes last year's tax with no condition; Exception 1 and the
// statute need twelve months and a liability. The latter is used and asked
// about; the caller can take line 5 as printed.
func TestEstimatedZeroOrShortPriorYearIsAnOpenQuestion(t *testing.T) {
	asPrinted := must(missouritaxcalc.NewEstimatedFacts().NoCombatZone().LineFiveAsPrinted(true).Build())
	for name, p := range map[string]*estimated.PriorReturn{"no liability": prior(0, 12), "six months": prior(5_000*money.Dollar, 6)} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p}.facts(t)))
		if res.Value().Options()[1].Available() || !because(res.Required(), working.PositionJurisdictionFact, "MO-2210 line 5 count") {
			t.Errorf("%s: the prior-year test available %v, asked %v; want not offered, and asked", name, res.Value().Options()[1].Available(),
				because(res.Required(), working.PositionJurisdictionFact, "MO-2210 line 5 count"))
		}
		f, _ := p.Figures()
		want, _ := f.Tax()
		res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p, extras: &asPrinted}.facts(t)))
		if res.Value().Target() != want || because(res.Required(), working.PositionJurisdictionFact, "MO-2210 line 5 count") {
			t.Errorf("%s, line 5 as printed: target %v; want %v, not asked", name, res.Value().Target(), want)
		}
	}
}

// Nothing paid on $9,000 required: each column's total underpayment to the
// next due date. April's $2,250 for 61 days, June's $4,500 for 92,
// September's $6,750 for 107 days of 2026 and 15 of 2027, January's $9,000
// for 90. The September column spans the 2026 and 2027 rate periods; both
// are 7%, so the boundary splits the charge without changing the rate.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	res := penalty(t, household{tax: 10_000 * money.Dollar})
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	want := []struct {
		under   money.USCents
		days    []int
		charges []money.USCents
	}{
		{225_000, []int{61}, []money.USCents{2_632}},
		{450_000, []int{92}, []money.USCents{7_940}},
		{675_000, []int{107, 15}, []money.USCents{13_851, 1_942}},
		{900_000, []int{90}, []money.USCents{15_534}},
	}
	for i, l := range lines {
		parts := l.Parts()
		if l.Shortfall() != want[i].under || len(parts) != len(want[i].days) {
			t.Errorf("column %d: %v in %d parts; want %v in %d", i+1, l.Shortfall(), len(parts), want[i].under, len(want[i].days))
			continue
		}
		for j, p := range parts {
			if p.Days() != want[i].days[j] || p.Charge() != want[i].charges[j] || p.Rate() != money.Percent(7) {
				t.Errorf("column %d part %d: %d days %v at %v; want %d days %v at 7%%", i+1, j, p.Days(), p.Charge(), p.Rate(), want[i].days[j], want[i].charges[j])
			}
		}
	}
	if parts := lines[2].Parts(); !strings.Contains(parts[0].Source(), "2026") || !strings.Contains(parts[1].Source(), "2027") {
		t.Errorf("September's parts cite %q and %q", parts[0].Source(), parts[1].Source())
	}
	if res.Value().Total() != 41_899 {
		t.Errorf("penalty %v, want $418.99", res.Value().Total())
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("an unpublished rate is reported, but 2027's is posted")
	}
	// Never past April 15, 2027.
	if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 7, 1))); p.Value().AsOf() != day(2027, 4, 15) || p.Value().Total() != 41_899 {
		t.Errorf("to July 1: figured to %v, %v", p.Value().AsOf(), p.Value().Total())
	}
}

// A payment between due dates stops the charge on what it pays: April's
// $2,250 paid May 15 runs 30 days, $12.95.
func TestEstimatedPaymentBetweenDueDates(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 5, 15), 2_250*money.Dollar), pay(dues[1], 2_250*money.Dollar), pay(dues[2], 2_250*money.Dollar), pay(dues[3], 2_250*money.Dollar)}}
	res := penalty(t, h)
	if l := res.Value().Lines(); len(l) != 1 || l[0].Days() != 30 || res.Value().Total() != 1_295 {
		t.Errorf("penalty %v over %v; want $12.95 on April for 30 days", res.Value().Total(), l)
	}
}

// Filed and paid by January 31, 2027, the balance counts as paid on January
// 15: the first three columns stop then and January's bears nothing.
func TestEstimatedReturnFiledByJanuaryThirtyFirst(t *testing.T) {
	res := penalty(t, household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 1, 29)})
	if res.Value().AsOf() != day(2027, 1, 15) || res.Value().Total() != 26_365 {
		t.Errorf("filed January 29: to %v, %v; want January 15 and $263.65", res.Value().AsOf(), res.Value().Total())
	}
	res = penalty(t, household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 3, 1)})
	if res.Value().AsOf() != day(2027, 3, 1) {
		t.Errorf("filed March 1: to %v", res.Value().AsOf())
	}
}

// Withholding counts in equal parts on the four dates unless the filer shows
// the dates.
func TestEstimatedWithholding(t *testing.T) {
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar}); p.Value().Total() != 0 {
		t.Errorf("spread evenly: penalty %v", p.Value().Total())
	}
	asWithheld := must(estimated.NewElections().WithholdingAsWithheld().Build())
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar, elections: &asWithheld}); p.Value().Total() == 0 {
		t.Error("on its December date: no penalty")
	}
}

// A requirement first met on July 1 starts at September 15 with two
// installments of $4,500: $92.34 and $12.95 for September, $155.34 for
// January. The table's boundaries: April 1 is in the first row, April 2 in
// the second.
func TestEstimatedLateStarter(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, firstMet: day(2026, 7, 1)}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{0, 0, 4_500 * money.Dollar, 9_000 * money.Dollar} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, s[i].NeededBy(), want)
		}
	}
	if s[0].Status() != estimated.StatusNothingDue || s[1].Status() != estimated.StatusNothingDue {
		t.Errorf("April and June are %v and %v; want nothing due", s[0].Status(), s[1].Status())
	}
	if p := penalty(t, h); p.Value().Total() != 26_063 || has(p.Required(), working.PositionRequirementFirstMet) {
		t.Errorf("penalty %v, want $260.63", p.Value().Total())
	}
	for _, tc := range []struct {
		met  estimated.Date
		need money.USCents
	}{{day(2026, 4, 1), 2_250 * money.Dollar}, {day(2026, 4, 2), 0}} {
		s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, firstMet: tc.met}.facts(t))).Value()
		if s[0].NeededBy() != tc.need {
			t.Errorf("first met %v: April needs %v, want %v", tc.met, s[0].NeededBy(), tc.need)
		}
	}
	if s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, firstMet: day(2026, 4, 2)}.facts(t))).Value(); s[1].NeededBy() != 3_000*money.Dollar {
		t.Errorf("three installments: June needs %v, want $3,000", s[1].NeededBy())
	}
	if p := penalty(t, household{tax: 10_000 * money.Dollar}); !has(p.Required(), working.PositionRequirementFirstMet) {
		t.Error("when the requirement was met is not asked")
	}
}

// Exception 2: payments reaching each date's share of the tax on last year's
// income at this year's rates excuse the installment.
func TestEstimatedExceptionTwo(t *testing.T) {
	e := must(missouritaxcalc.NewEstimatedFacts().NoCombatZone().PriorYearIncomeAtThisYearsRates(4_000 * money.Dollar).Build())
	h := household{tax: 10_000 * money.Dollar, extras: &e, payments: quarterly(1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar)}
	if p := penalty(t, h); p.Value().Total() != 0 {
		t.Errorf("Exception 2 met each quarter: penalty %v", p.Value().Total())
	}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, in := range s {
		if in.Status() != estimated.StatusExcused || in.Shortfall() == 0 {
			t.Errorf("installment %d is %v with %v short; want excused with a shortfall", i+1, in.Status(), in.Shortfall())
		}
	}
	h.payments = quarterly(999*money.Dollar, 1_001*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar)
	if p := penalty(t, h); p.Value().Total() == 0 {
		t.Error("April $1 short of Exception 2: no penalty")
	}
	nr := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	h = household{tax: 10_000 * money.Dollar, extras: &e, prior: &nr, payments: quarterly(1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar)}
	if p := penalty(t, h); p.Value().Total() == 0 {
		t.Error("Exception 2 is open to somebody not required to file last year")
	}
}

// Exceptions 3 and 4 excuse the first three installments only. Exception 3
// is 22.5%, 45% and 67.5% of the annualized tax; Exception 4, as the form
// prints it, 90% of the tax on income to date.
func TestEstimatedExceptionsThreeAndFour(t *testing.T) {
	payments := quarterly(900*money.Dollar, 900*money.Dollar, 3_600*money.Dollar, 3_600*money.Dollar)
	h := household{tax: 10_000 * money.Dollar, payments: payments, annualised: []money.USCents{4_000 * money.Dollar, 4_000 * money.Dollar, 8_000 * money.Dollar, 10_000 * money.Dollar}}
	if p := penalty(t, h); p.Value().Total() != 0 {
		t.Errorf("Exception 3: penalty %v", p.Value().Total())
	}
	e := must(missouritaxcalc.NewEstimatedFacts().NoCombatZone().TaxOnIncomeToDate(1_000*money.Dollar, 2_000*money.Dollar, 6_000*money.Dollar).Build())
	if p := penalty(t, household{tax: 10_000 * money.Dollar, payments: payments, extras: &e}); p.Value().Total() != 0 {
		t.Errorf("Exception 4: penalty %v", p.Value().Total())
	}
	if p := penalty(t, household{tax: 10_000 * money.Dollar, payments: payments}); p.Value().Total() == 0 {
		t.Error("without an exception the same payments owe nothing")
	}
	// One cent under each exception's $900 in April is charged.
	cent := quarterly(89_999, 90_001, 3_600*money.Dollar, 3_600*money.Dollar)
	h.payments = cent
	if l := penalty(t, h).Value().Lines(); len(l) != 1 || l[0].Installment() != 1 {
		t.Errorf("Exception 3 a cent short in April: lines %v; want April's", l)
	}
	if l := penalty(t, household{tax: 10_000 * money.Dollar, payments: cent, extras: &e}).Value().Lines(); len(l) != 1 || l[0].Installment() != 1 {
		t.Errorf("Exception 4 a cent short in April: lines %v; want April's", l)
	}
	// January's shortfall is never excused by them.
	short := quarterly(900*money.Dollar, 900*money.Dollar, 3_600*money.Dollar, 3_000*money.Dollar)
	p := penalty(t, household{tax: 10_000 * money.Dollar, payments: short, extras: &e})
	if l := p.Value().Lines(); len(l) != 1 || l[0].Installment() != 4 {
		t.Errorf("January $600 short: lines %v; want only January", l)
	}
}

// An excused April shortfall carries into June's column, where line 25
// charges it: June's $3,600 includes April's $1,350, for 92 days, $63.52.
func TestEstimatedExcusedShortfallCarriesForward(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, annualised: []money.USCents{4_000 * money.Dollar, 20_000 * money.Dollar, 20_000 * money.Dollar, 10_000 * money.Dollar},
		payments: quarterly(900*money.Dollar, 0, 6_750*money.Dollar, 2_250*money.Dollar)}
	p := penalty(t, h)
	l := p.Value().Lines()
	if len(l) != 1 || l[0].Installment() != 2 || l[0].Shortfall() != 3_600*money.Dollar || p.Value().Total() != 6_352 {
		t.Errorf("lines %v, total %v; want June's $3,600 for $63.52", l, p.Value().Total())
	}
}

// Installments due during service in a combat zone are exempt. Not answered,
// the question is asked when there is a penalty.
func TestEstimatedCombatZone(t *testing.T) {
	e := must(missouritaxcalc.NewEstimatedFacts().CombatZone(day(2026, 3, 1), day(2026, 7, 31)).Build())
	p := penalty(t, household{tax: 10_000 * money.Dollar, extras: &e})
	if p.Value().Total() != 13_851+1_942+15_534 || because(p.Required(), working.PositionJurisdictionFact, "combat zone") {
		t.Errorf("service March to July: penalty %v; want September's and January's only", p.Value().Total())
	}
	none := must(missouritaxcalc.NewEstimatedFacts().Build())
	if p := penalty(t, household{tax: 10_000 * money.Dollar, extras: &none}); !because(p.Required(), working.PositionJurisdictionFact, "combat zone") {
		t.Error("combat zone service is not asked about")
	}
	if _, err := missouritaxcalc.NewEstimatedFacts().CombatZone(day(2026, 8, 1), day(2026, 7, 1)).Build(); err == nil {
		t.Error("a combat zone span that runs backward is accepted")
	}
}

// Without this year's tax, the target is last year's alone and the penalty is
// the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000*money.Dollar, 12)}
	res := must(rules(t).Requirement(h.facts(t)))
	if r := res.Value(); !r.Required() || r.Target() != 8_000*money.Dollar || r.Options()[0].WhyNot() != estimated.NeedsThisYearTax || !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("required %v, target %v", r.Required(), r.Target())
	}
	if p := penalty(t, h); !p.Value().UpperBound() || p.Value().Total() == 0 {
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
		"What the penalty is measured against":             {"smaller of two amounts. They are 90%", "143.761.2"},
		"The $500 stop":                                    {"under $500", "143.521.1"},
		"A prior-year return with no tax, or a short year": {"Exception 1 on the same form", "143.761.4(1)"},
		"Exception 4, tax on income to date":               {"90% of the tax on the income", "143.761.4(3)"},
		"An excused installment's shortfall":               {"Line 19d", "143.761.4"},
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
		// 90% of this year's is chosen and nothing is excused, so none of
		// the divergences touches these facts.
		if d.AffectsHousehold() {
			t.Errorf("%q affects a household it does not touch", d.Rule())
		}
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.StatuteSource(), w[1]) || d.FormSource() == "" || d.Statute() == "" {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s)", d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := missouritaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100_000).Build())
	farmer := must(estimated.NewFacts("MO", 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(7000).Build())
	if _, err := rules(t).Requirement(farmer); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer: %v, want ErrUnsupported", err)
	}
	// 12 CSR 10-2.067(2)(B): fishing is not farming.
	if _, err := rules(t).Requirement(household{tax: 10_000 * money.Dollar, fishShare: 7000}.facts(t)); err != nil {
		t.Errorf("a fisher: %v", err)
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts("MO", 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts("MO", 2026).FilingStatus(filing.Single).ThisYear(this).State("x").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$100", "$500", "7%", "100%", "last year's income taxed at this year's rates", "fewer, larger installments", "reasonable cause"} {
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("MO"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}

func waived(t *testing.T, pos estimated.Positions) estimated.Penalty {
	t.Helper()
	return must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, positions: &pos}.facts(t), day(2027, 4, 15))).Value()
}

func retiredOn(on, born estimated.Date) estimated.Positions {
	return must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, on).Born(born).Build())
}

// Missouri has no waiver: the addition is charged even with reasonable
// cause, where federal law waives it for retirement, disability or a
// casualty.
func TestEstimatedWaiverIsRefused(t *testing.T) {
	for name, pos := range map[string]estimated.Positions{
		"retired in 2026 at 64": retiredOn(day(2026, 6, 1), day(1962, 3, 1)),
		"a disaster":            must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()),
	} {
		p := waived(t, pos)
		if o, ok := p.WaiverOutcome(); !ok || !o.Refused() || !strings.Contains(o.Reason(), "reasonable cause") || p.Total() == 0 || p.IfWaived() != p.Total() {
			t.Errorf("%s: outcome %v %+v, total %v, if waived %v", name, ok, o, p.Total(), p.IfWaived())
		}
	}
}

// Under an exception no question may say its answer changes the target, so
// last year's return isn't asked for.
func TestEstimatedExemptAsksNothingOfTheTarget(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 100 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() {
		t.Fatal("$100 of tax is not exempt")
	}
	if has(res.Required(), working.PositionPriorYearReturn) {
		t.Error("last year's return is asked for under an exception")
	}
	for _, q := range res.Required() {
		if strings.Contains(strings.ToLower(q.Effect()), "target") {
			t.Errorf("under an exception a question says %q", q.Effect())
		}
	}
}
