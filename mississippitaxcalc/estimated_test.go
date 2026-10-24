// Mississippi's estimated tax rules for 2026, checked against the Department
// of Revenue's own documents and the statute.
//
// Mississippi publishes one oracle: the worked example on Form 80-320 (Rev.
// 09/25), with its inputs and its $30.30 of interest.
// TestEstimatedInterestMatchesTheFormExample reproduces it with the 2026
// dates, which give the same months. Every threshold, rate, date and
// percentage is compared against its extract in testdata/. The other cases
// are hand-written.
package mississippitaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/mississippitaxcalc"
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
	return must(mississippitaxcalc.EstimatedTax(2026))
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

type household struct {
	tax         money.USCents
	noTax       bool
	withheld    money.USCents
	withheldOn  estimated.Date
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *mississippitaxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	elections   *estimated.Elections
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts("MS", 2026).FilingStatus(filing.Single).ThisYear(figures).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		on := h.withheldOn
		if on.IsZero() {
			on = day(2026, 4, 1)
		}
		b.Withheld(must(estimated.NewWithheld().On(on).Amount(h.withheld).Build()))
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
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

func prior(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "form-80-320-2025.txt",
		"Form 80-320-25-8-1-000 (Rev. 09/25)",
		"Multiply the amount on line 1 by 80% and enter the result",
		"Enter the lesser of line 2 or line 3 (see instructions)",
		"Required installments Enter 1/4th (.25) of line 4",
		"Income tax withheld (column A only) and estimated tax paid",
		"Underestimate subject to interest (line 5 minus line 6 plus line 7)",
		"compute interest at 1/2% per month from payment due date until paid or next payment due date, whichever is earlier",
		"If your 2025 Mississippi Income Tax Liability is $200 or less, do not complete the remainder of this form; no interest is due on underestimate of tax.",
		"If line 3 is zero and your 2025 Mississippi income tax liability (line 2) exceeds $200 and no estimate payments for the 2025 tax year were made, enter the amount from line 2.",
		"For column (A) only, enter the total overpayment from prior year, estimated tax paid and/or withholding as of payment due date.",
		"Filing requirements met after payment due date - compute interest in applicable columns",
		"Gross income from farming or fishing is at least two-thirds of total gross income",
	)
	mustContain(t, "form-80-106-2025.txt",
		"Every individual taxpayer who does not have at least eighty percent (80%) of his/her annual tax liability prepaid through withholding must make estimated tax payments if his/her annual tax liability exceeds two hundred dollars ($200).")
	mustContain(t, "form-80-100-2025-estimated.txt",
		"First Quarter Second Quarter Third Quarter Fourth Quarter April 15th June 15th September 15th January 15th",
		"will result in an assessment of interest at the rate of 1/2% per month on underpayment of tax from the date payment is due until paid",
		"Filing requirements met after the payment due date. Compute the interest in the applicable columns",
	)
	mustContain(t, "rule-35-III-11-21-100.txt",
		"the estimated tax may be paid in four equal installments",
		"The 15th day of the 4th month of the income year, b. The 15th day of the 6th month of the income year, c. The 15th day of the 9th month of the income year, d. The 15th day of the 1st month after the close of the income year.",
		"provided a return was filed and the return covered a period of twelve (12) months.",
		"the tax liability computed on prior year's income based on Mississippi current year's rates and exemption.",
		"shall be liable for interest of one percent (1%) per month",
	)
	mustContain(t, "hb799-2014-27-7-327.txt",
		"shall estimate an amount not less than eighty percent (80%) of the tax actually due in the case of an individual",
		"One-half of one percent (1/2 of 1%) per month for such taxes assessed on or after January 1, 2019.",
	)
}

// Form 80-320's example: $6,520 of liability and $4,510 last year, so four
// installments of $1,127.50; $1,500, $1,000, $500 and nothing credited by the
// dates. Column C's running underestimate of $382.50 at 2% is $7.65, column
// D's $1,510.00 at 1.5% is $22.65, $30.30 in all. The 2026 dates give the
// same 2, 3, 4 and 3 months as the example's 2025 dates.
func TestEstimatedInterestMatchesTheFormExample(t *testing.T) {
	mustContain(t, "form-80-320-2025.txt",
		"1 2025 Mississippi income tax liability (see instructions) 1 6520.00",
		"4 Enter the lesser of line 2 or line 3 (see instructions) 4 4510.00",
		"through D) 1127.50 1127.50 1127.50 1127.50",
		"A through D). 1500.00 1000.00 500.00 0.00",
		"enter result here and on line 7, columns B through D. (372.50) (245.00) 382.50 1510.00",
		"date, whichever is earlier) 0.010 0.015 0.020 0.015",
		"zero) 0.00 0.00 7.65 22.65",
		"(enter the total of line 10, columns A through D) 11 30.30",
	)
	h := household{tax: 6_520 * money.Dollar, withheld: 1_500 * money.Dollar, prior: prior(4_510 * money.Dollar),
		payments: []estimated.Payment{pay(dues[1], 1_000*money.Dollar), pay(dues[2], 500*money.Dollar)}}
	req := must(rules(t).Requirement(h.facts(t))).Value()
	if req.Target() != 4_510*money.Dollar || req.Options()[0].Amount() != 5_216*money.Dollar {
		t.Errorf("target %v, 80%% option %v; want $4,510 and $5,216", req.Target(), req.Options()[0].Amount())
	}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	lines := res.Value().Lines()
	if len(lines) != 2 {
		t.Fatalf("%d lines, want columns C and D", len(lines))
	}
	for i, want := range []struct {
		col    int
		under  money.USCents
		months int
		charge money.USCents
	}{{3, 38_250, 4, 765}, {4, 151_000, 3, 2_265}} {
		l := lines[i]
		parts := l.Parts()
		if l.Installment() != want.col || l.Shortfall() != want.under || len(parts) != 1 || parts[0].Months() != want.months || parts[0].Charge() != want.charge {
			t.Errorf("line %d: column %d on %v, %d parts; want column %d on %v, %d months, %v", i, l.Installment(), l.Shortfall(), len(parts), want.col, want.under, want.months, want.charge)
		}
	}
	if res.Value().Total() != 3_030 {
		t.Errorf("interest %v, want $30.30", res.Value().Total())
	}
}

// The dates are the ones Rule 100(2) and the booklet print, a quarter each,
// and none moves: no source gives a weekend rule, and none of them, nor
// April 15, 2027, falls on a weekend.
func TestEstimatedScheduleForTwentyTwentySix(t *testing.T) {
	s := must(rules(t).Schedule(household{tax: 5_000 * money.Dollar}.facts(t))).Value()
	for i, in := range s {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
		if want := money.USCents(i+1) * 1_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	if wd := day(2027, 4, 15).Weekday(); wd == time.Saturday || wd == time.Sunday {
		t.Errorf("April 15, 2027 is a %v", wd)
	}
}

// Form 80-320 line 1: a liability of $200 or less owes nothing.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		tax    money.USCents
		exempt bool
	}{{20_000, true}, {20_001, false}} {
		res := must(rules(t).Requirement(household{tax: tc.tax}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("liability %v: exempt %v, want %v", tc.tax, res.Value().Exempt(), tc.exempt)
		}
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Mississippi liability of $200 or less") {
			t.Errorf("liability %v: chosen %v %q at %v", tc.tax, c.Basis(), c.Label(), c.Amount())
		}
		if p := must(rules(t).Penalty(household{tax: tc.tax}.facts(t), day(2027, 4, 15))); tc.exempt && p.Value().Reason() != "Mississippi liability of $200 or less." {
			t.Errorf("liability %v: penalty reason %q", tc.tax, p.Value().Reason())
		}
	}
}

// The target is the lesser of 80% of this year's liability and 100% of last
// year's, which needs a filed twelve-month return.
func TestEstimatedTargetOptions(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(9_000 * money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 8_000*money.Dollar {
		t.Errorf("chose %v for %v, want 80%%, $8,000", c.Basis(), res.Value().Target())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(7_999 * money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || c.Percent() != money.Percent(100) || res.Value().Target() != 7_999*money.Dollar {
		t.Errorf("chose %v at %v for %v, want 100%% of last year's $7,999", c.Basis(), c.Percent(), res.Value().Target())
	}
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(must(estimated.NewFigures().Tax(100).Build())).Build())
	for name, p := range map[string]*estimated.PriorReturn{"none on record": nil, "not filed": &notFiled, "six months": &short} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p}.facts(t)))
		if res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
	}
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t))); !has(res.Required(), working.PositionPriorYearReturn) {
		t.Error("no prior return: not asked")
	}
}

// Form 80-320 line 4 with a zero line 3: 80% of this year's when no estimated
// payments were made, and the lesser, zero, when some were.
func TestEstimatedZeroPriorYearFollowsLineFour(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0)}.facts(t)))
	if res.Value().Target() != 8_000*money.Dollar || res.Value().Options()[1].Available() {
		t.Errorf("no payments: target %v; want 80%%, $8,000", res.Value().Target())
	}
	h := household{tax: 10_000 * money.Dollar, prior: prior(0), payments: []estimated.Payment{pay(dues[0], 100*money.Dollar)}}
	res = must(rules(t).Requirement(h.facts(t)))
	if res.Value().Target() != 0 || res.Value().Required() {
		t.Errorf("with a payment: target %v; want zero", res.Value().Target())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("with a payment: interest %v", p.Value().Total())
	}
}

// Rule 100(3) measures a filer not required to file last year against the
// tax on last year's income at this year's rates. Not given, the form's rule
// is used and the question is asked; given, it is the prior-year test.
func TestEstimatedNotRequiredToFileIsAnOpenQuestion(t *testing.T) {
	nr := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &nr}.facts(t)))
	if res.Value().Target() != 8_000*money.Dollar || !because(res.Required(), working.PositionJurisdictionFact, "at 2026 Mississippi rates") {
		t.Errorf("not given: target %v, asked %v; want $8,000 and asked", res.Value().Target(), because(res.Required(), working.PositionJurisdictionFact, "at 2026 Mississippi rates"))
	}
	e := must(mississippitaxcalc.NewEstimatedFacts().PriorYearIncomeAtThisYearsRates(3_000 * money.Dollar).Build())
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &nr, extras: &e}.facts(t)))
	c, _ := res.Value().Chosen()
	if c.Basis() != estimated.BasisPriorYearIncome || res.Value().Target() != 3_000*money.Dollar || because(res.Required(), working.PositionJurisdictionFact, "at 2026 Mississippi rates") {
		t.Errorf("given: chose %v for %v; want last year's income at this year's rates, $3,000, not asked", c.Basis(), res.Value().Target())
	}
}

// All withholding counts on April 15, whenever it was withheld. Withholding
// after April 15 is an open question the caller answers with the election.
func TestEstimatedWithholdingCountsInColumnA(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, withheld: 8_000 * money.Dollar, withheldOn: day(2026, 12, 15)}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 0 || !because(res.Required(), working.PositionJurisdictionFact, "all withholding toward Apr 15") {
		t.Errorf("December withholding: interest %v, asked %v; want none, and asked", res.Value().Total(), because(res.Required(), working.PositionJurisdictionFact, "all withholding toward Apr 15"))
	}
	if r := must(rules(t).Requirement(h.facts(t))).Value(); r.Required() {
		t.Errorf("withholding reaches the target: %s", r.Why())
	}
	asWithheld := must(estimated.NewElections().WithholdingAsWithheld().Build())
	h.elections = &asWithheld
	res = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	// $2,000 short on April 15 for 2 months, $4,000 on June 15 for 3, $6,000
	// on September 15 to December 15: 3 months. $20 + $60 + $90.
	if res.Value().Total() != 170*money.Dollar || because(res.Required(), working.PositionJurisdictionFact, "all withholding toward Apr 15") {
		t.Errorf("on its date: interest %v; want $170, not asked", res.Value().Total())
	}
	// Withholding on or before April 15 asks nothing.
	early := household{tax: 10_000 * money.Dollar, withheld: 8_000 * money.Dollar, withheldOn: day(2026, 4, 15)}
	if res := must(rules(t).Penalty(early.facts(t), day(2027, 4, 15))); because(res.Required(), working.PositionJurisdictionFact, "all withholding toward Apr 15") {
		t.Error("withholding by April 15 is asked about")
	}
}

// An unpaid first installment carries through every column: $1,000 at ½% for
// 2, 3, 4 and 3 months, $60. § 27-7-327(e) has set ½% since 2019 and the rate
// does not change within the year, so the boundary tested is the column's:
// the carried shortfall is charged again in each later period.
func TestEstimatedShortfallCarriesThroughEveryColumn(t *testing.T) {
	h := household{tax: 5_000 * money.Dollar, payments: []estimated.Payment{pay(dues[1], 1_000*money.Dollar), pay(dues[2], 1_000*money.Dollar), pay(dues[3], 1_000*money.Dollar)}}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	want := []money.USCents{1_000, 1_500, 2_000, 1_500}
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	for i, l := range lines {
		if l.Shortfall() != 1_000*money.Dollar || l.Total() != want[i] {
			t.Errorf("column %d: %v on %v; want %v on $1,000", i+1, l.Total(), l.Shortfall(), want[i])
		}
	}
	if res.Value().Total() != 60*money.Dollar {
		t.Errorf("interest %v, want $60", res.Value().Total())
	}
	if !has(res.Required(), working.PositionRequirementFirstMet) {
		t.Error("when the requirement was met is not asked")
	}
}

// A payment between due dates stops the charge on what it pays. Paid May 1,
// April's $1,000 runs half a month: counted as a whole month by default,
// $5.00, and asked; answered with completed months only, nothing.
func TestEstimatedPartMonthIsAnOpenQuestion(t *testing.T) {
	payments := []estimated.Payment{pay(day(2026, 5, 1), 1_000*money.Dollar), pay(dues[1], 1_000*money.Dollar), pay(dues[2], 1_000*money.Dollar), pay(dues[3], 1_000*money.Dollar)}
	res := must(rules(t).Penalty(household{tax: 5_000 * money.Dollar, payments: payments}.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 500 || !because(res.Required(), working.PositionJurisdictionFact, "part of a month") {
		t.Errorf("part month by default: %v, asked %v; want $5.00 and asked", res.Value().Total(), because(res.Required(), working.PositionJurisdictionFact, "part of a month"))
	}
	if parts := res.Value().Lines()[0].Parts(); len(parts) != 1 || parts[0].To() != day(2026, 5, 1) || parts[0].Months() != 1 {
		t.Errorf("parts %v; want one to May 1 for one month", parts)
	}
	e := must(mississippitaxcalc.NewEstimatedFacts().PartMonthCountsWhole(false).Build())
	res = must(rules(t).Penalty(household{tax: 5_000 * money.Dollar, payments: payments, extras: &e}.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 0 || because(res.Required(), working.PositionJurisdictionFact, "part of a month") {
		t.Errorf("completed months only: %v; want nothing, not asked", res.Value().Total())
	}
}

// A requirement first met on July 1 computes interest only from September:
// $3,000 for 4 months and $4,000 for 3, $120.
func TestEstimatedRequirementMetLate(t *testing.T) {
	h := household{tax: 5_000 * money.Dollar, firstMet: day(2026, 7, 1)}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 120*money.Dollar || has(res.Required(), working.PositionRequirementFirstMet) {
		t.Errorf("met July 1: interest %v; want $120", res.Value().Total())
	}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	if s[0].Status() != estimated.StatusExcused || s[1].Status() != estimated.StatusExcused || s[2].Status() != estimated.StatusShort {
		t.Errorf("statuses %v %v %v; want excused, excused, short", s[0].Status(), s[1].Status(), s[2].Status())
	}
}

// The interest stops on the day the return is filed and paid, and never runs
// past April 15, 2027.
func TestEstimatedInterestStops(t *testing.T) {
	h := household{tax: 5_000 * money.Dollar, payments: []estimated.Payment{pay(dues[0], 1_000*money.Dollar), pay(dues[1], 1_000*money.Dollar), pay(dues[2], 1_000*money.Dollar)}}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 6, 30)))
	if res.Value().AsOf() != day(2027, 4, 15) || res.Value().Total() != 15*money.Dollar {
		t.Errorf("to June 30: figured to %v, %v; want April 15 and $15", res.Value().AsOf(), res.Value().Total())
	}
	// Filed and paid February 15: January's $1,000 for one month.
	h.returnFiled = day(2027, 2, 15)
	res = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().AsOf() != day(2027, 2, 15) || res.Value().Total() != 5*money.Dollar {
		t.Errorf("filed February 15: figured to %v, %v; want $5", res.Value().AsOf(), res.Value().Total())
	}
}

// Without this year's liability, the target is last year's alone and the
// interest is the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(4_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	if r := res.Value(); !r.Required() || r.Target() != 4_000*money.Dollar || r.Options()[0].WhyNot() != estimated.NeedsThisYearTax || !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("required %v, target %v", r.Required(), r.Target())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); !p.Value().UpperBound() || p.Value().Total() == 0 {
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
	want := map[string][2]string{
		"The interest rate":              {"½% a month", "R. 100(1)"},
		"The prior-year test":            {"lesser of 80%", "§ 27-7-327"},
		"Last year's liability was zero": {"80% of this year's", "R. 100(3)"},
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
		// 80% of this year's is chosen, nothing is paid, and last year is not
		// zero: only the interest rate touches these facts.
		if want := d.Rule() == "The interest rate"; d.AffectsHousehold() != want || d.Uses() != working.ReadingForm {
			t.Errorf("%q: affects %v, uses %v; want %v and the form", d.Rule(), d.AffectsHousehold(), d.Uses(), want)
		}
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.StatuteSource(), w[1]) || d.FormSource() == "" || d.Statute() == "" {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s)", d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := mississippitaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100_000).Build())
	for _, f := range []estimated.Facts{
		must(estimated.NewFacts("MS", 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(7000).Build()),
		must(estimated.NewFacts("MS", 2026).FilingStatus(filing.Single).ThisYear(this).FishShare(7000).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("a farmer or fisher: %v, want ErrUnsupported", err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts("MS", 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts("MS", 2026).FilingStatus(filing.Single).ThisYear(this).State(1).Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}
	// The annualized method and waivers do not exist; the regular answer is
	// given and says so.
	ann := must(estimated.NewFacts("MS", 2026).FilingStatus(filing.Single).ThisYear(this).
		Elections(must(estimated.NewElections().Annualised().Build())).
		Periods(must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 5, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).
		Positions(must(estimated.NewPositions().Waiver(estimated.WaiverCasualty).Build())).Build())
	res := must(rules(t).Penalty(ann, day(2027, 4, 15)))
	text := ""
	for _, r := range res.Applied() {
		text += r.Applied() + " "
	}
	if !strings.Contains(text, "no annualized income method") || !strings.Contains(text, "no waiver") || res.Value().Total() == 0 {
		t.Errorf("annualized and waiver: total %v, applied %q", res.Value().Total(), text)
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$200", "80%", "100%", "½% a month", "April payment", "January 15 to April 15", "no annualized method"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Withholding over the tax never reads as a negative balance. Mississippi's
// sentences state the liability and the withholding, not their difference.
func TestWithholdingOverTheTaxHasNoNegativeBalance(t *testing.T) {
	var withheld []estimated.Withheld
	for m := time.January; m <= time.December; m++ {
		withheld = append(withheld, must(estimated.NewWithheld().On(day(2026, m, 28)).Amount(money.Dollars(300)).Build()))
	}
	figures := func(tax, agi int64) estimated.Figures {
		return must(estimated.NewFigures().Tax(money.Dollars(tax)).FederalAGI(money.Dollars(agi)).StateAGI(money.Dollars(agi)).Build())
	}
	prior := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800, 55_000)).Build())
	f := must(estimated.NewFacts(estimated.Jurisdiction("MS"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want no minus sign", got)
	}
}

func retiredOn(on, born estimated.Date) estimated.Positions {
	return must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, on).Born(born).Build())
}

// Mississippi publishes no waiver and no rule refusing one, so the outcome
// is left to the department and reported as unsettled.
func TestEstimatedWaiverIsUndecided(t *testing.T) {
	for name, pos := range map[string]estimated.Positions{
		"retired in 2026 at 64": retiredOn(day(2026, 6, 1), day(1962, 3, 1)),
		"a disaster":            must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()),
	} {
		res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, positions: &pos}.facts(t), day(2027, 4, 15)))
		p := res.Value()
		if o, ok := p.WaiverOutcome(); !ok || !o.Undecided() || p.Total() == 0 || p.IfWaived() != p.Total() {
			t.Errorf("%s: outcome %v %+v, total %v, if waived %v", name, ok, o, p.Total(), p.IfWaived())
		}
		if !because(res.Required(), working.PositionUnsettledRule, "waived") {
			t.Errorf("%s: the open waiver rule is not reported", name)
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
