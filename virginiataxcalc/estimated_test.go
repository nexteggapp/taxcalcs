// Virginia's estimated tax rules for 2026, checked against the Department of
// Taxation's documents, the Code of Virginia and the Virginia Administrative
// Code.
//
// Virginia publishes no exhaustive oracle for estimated tax. The strongest
// checks are the worked examples in 23VAC10-112-42 and -43 and in the Form
// 760C instructions: the underpayment on each installment, the order a
// payment is applied in, and the amounts Exceptions 3 and 4 need. Their
// inputs and answers both come from the Commonwealth. Every threshold, share,
// date and rate is compared against its extract in testdata/. The
// boundaries, the exceptions, the late start and the rate change are
// hand-worked cases.
package virginiataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/virginiataxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

const va = estimated.Jurisdiction("VA")

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
	return must(virginiataxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

func because(rs []working.Required, k working.PositionKind) string {
	var out []string
	for _, r := range rs {
		if r.Position() == k {
			out = append(out, strings.TrimSpace(r.Question()+" "+r.Effect()))
		}
	}
	return strings.Join(out, " | ")
}

func applied(rs []working.Rule) string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Applied())
	}
	return strings.Join(out, " | ")
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

var dues = []estimated.Date{day(2026, 5, 1), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

// quarterly is one payment of a on each due date.
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
	status      filing.Status
	tax         money.USCents // after the Spouse Tax Adjustment and credits, Form 760C line 1
	noTax       bool
	agi         *money.USCents
	withheld    money.USCents
	withheldOn  estimated.Date
	asWithheld  bool
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *virginiataxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	annualised  []money.USCents // Form 760C line 18f for each period
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions // replaces waiver when set
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	status := h.status
	if status == filing.Unspecified {
		status = filing.Single
	}
	fb := estimated.NewFigures()
	if !h.noTax {
		fb.Tax(h.tax)
	}
	if h.agi != nil {
		fb.StateAGI(*h.agi)
	}
	b := estimated.NewFacts(va, 2026).FilingStatus(status).ThisYear(must(fb.Build())).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		on := h.withheldOn
		if on.IsZero() {
			on = day(2026, 12, 15)
		}
		b.Withheld(must(estimated.NewWithheld().On(on).Amount(h.withheld).Build()))
	}
	if h.asWithheld {
		b.Elections(must(estimated.NewElections().WithholdingAsWithheld().Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(virginiataxcalc.NewEstimatedFacts().MerchantSeamanShare(0).Build()))
	}
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 4, 30), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		b.Elections(must(estimated.NewElections().Annualised().Build()))
	}
	if h.waiver != nil {
		b.Positions(must(estimated.NewPositions().Waiver(*h.waiver).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

// prior is a filed twelve-month return showing tax.
func prior(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).Build())
	return &p
}

func extras(b *virginiataxcalc.EstimatedFactsBuilder) *virginiataxcalc.EstimatedFacts {
	e := must(b.MerchantSeamanShare(0).Build())
	return &e
}

func usd(a money.USCents) *money.USCents { return &a }

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "760es-2026.txt",
		"For taxable years beginning on and after January 1, 2026, if your Virginia income tax liability, after subtracting income tax withheld and any allowable credits, is expected to be more than $1,000, then you must make estimated tax payments",
		"(a) you are single and your expected Virginia adjusted gross income (Line 1, Estimated Income Tax Worksheet on Page 3) is less than $11,950;",
		"combined expected Virginia adjusted gross income (Line 1, Estimated Income Tax Worksheet on Page 3) is less than $23,900;",
		"(c) you are married, filing separately, and your separate expected Virginia adjusted gross income (Line 1, Estimated Income Tax Worksheet on Page 3) is less than $11,950; or",
		"(d) your expected estimated tax liability exceeds your withholding and tax credits by $1,000 or less.",
		"in equal installments on or before May 1, 2026, June 15, 2026, September 15, 2026, and January 15, 2027.",
		"falls on a Saturday, Sunday or legal holiday, you may file and make payment without penalty or interest on the next succeeding business day.",
		"If you file your 2026 income tax return and pay the balance of tax due in full by March 1, 2027, you are not required to make the estimated tax payment that would normally be due on January 15, 2027.",
		"(e) the sum of all installment underpayments for the taxable year is $1,000 or less.",
		"your underpayment computation will be based on 90% of the current year’s income tax liability or 100% of your liability for the preceding year, whichever is less.",
		"Farmers, fishermen and merchant seamen with 2/3 of their estimated Virginia gross income from self-employed farming, fishing or being a merchant seaman have special filing requirements",
		"Va. Dept of Taxation, 2601037, Rev. 09/25",
	)
	mustContain(t, "760es-2026-schedule.txt",
		"unless the requirements to file are not met until after April 15.",
		"on or before April 15th 4 May 1, 2026 1 25% 25% 25% 25%",
		"after April 15th and before June 2nd 3 June 15, 2026 2 .......... 331/3% 331/3% 331/3%",
		"after June 1st and before Sept. 2nd 2 Sept. 15, 2026 3 .......... .......... 50% 50%",
		"after Sept. 1st and before Dec. 31st 1 Jan. 15, 2027 4 .......... .......... .......... 100%",
	)
	mustContain(t, "760c-2025.txt",
		"4. Enter the Amount From Line 2 or Line 3, Whichever Is Less",
		"(Divide the amount on Line 4 by the number of installments reported on Line 5 and enter the result in the appropriate columns.)",
		"(If the sum of all underpayments (do not include any OVERPAYMENTS) reported is $150 or less, stop here; you are",
		"16. Exception 1: Prior Year's Tax 100% of 2024 Tax 25% 50% 75% 100%",
		"17. Exception 2: Tax on Prior Year's Income Using the 100% of Tax 25% 50% 75% 100%",
		"April 30 column: Multiply the actual amount for the period ended April 30, 2025, by 3.",
		"g. Multiply Line 18f by the Percentage Shown for Each Period 22.5% 45% 67.5%",
		"From January 1 to: April 30 May 31 August 31",
		"g. Multiply Line 19f by 90% (.90) for Each Period",
		"(if May 1, 2026, is earlier, enter 365, 320, 228, and 106, respectively)",
		"23. Multiply the Number of Days in Each Column on Line 22 by the Daily Rate .00025 (9% Per Annum)",
		"Rev. 04/26",
	)
	mustContain(t, "760c-2025-instructions.txt",
		"After April 15, but before June 2 3 payments were required",
		"After June 1, but before September 2 2 payments were required",
		"After September 1 1 payment was required",
		"you may divide the total amount withheld by four, or enter the exact amount withheld for each installment and enclose a schedule showing your computation.",
		"An overpayment as of any installment date must be applied to any underpayment in previous periods before being carried over to subsequent periods.",
		"the overpayment is considered a payment made on the installment date for which the overpayment is reported.",
		"Therefore, on Line 12 the taxpayer applied $500 in Column A and $500 in Column B.",
		"Your 2024 Virginia income tax return must have covered a full (twelve month) taxable year, and must have reflected an income tax liability.",
		"You do not need to have filed a Virginia return or incurred a Virginia tax liability for the preceding taxable year to qualify for Exception 2.",
		"Since Exception 3 cannot be applied to the final installment period",
		"Since Exception 4 cannot be applied to the final installment period",
		"An exception can only void the addition to tax; it cannot reduce or eliminate an underpayment.",
		"Otherwise, use the actual date the tax was paid in full or May 1, 2026, whichever is earlier.",
	)
	mustContain(t, "760f-2025.txt",
		"Test 1 – Your gross income from self-employment as a farmer, fisherman, and/or being a merchant seaman is at least two-thirds of your annual gross income for the taxable year",
		"11. Multiply Line 10 by the Daily Interest Rate of .00025 (9% per annum)",
	)
	mustContain(t, "code-58.1-492.txt",
		"ninety percent (sixty-six and two-thirds percent in the case of an individual referred to in § 58.1-490 F, relating to income from farming) of the tax shown on the return for the taxable year",
		"or 100 percent of the tax shown on the return of the taxpayer for the preceding taxable year, whichever is less",
		"1. May 1, if a calendar year,",
		"a payment of estimated tax on any installment date shall be considered a payment of any previous underpayment only to the extent such payment exceeds the amount of the installment determined under subdivision A 1 for such installment date.",
		"if a return showing a liability for tax was filed by the individual, trust or estate for the preceding taxable year and such preceding year was a taxable year of twelve months;",
		"an equal part of such amount shall be deemed paid on each installment date (determined under § 58.1-491) for such taxable year, unless the taxpayer establishes the dates on which all amounts were actually withheld",
		"There shall be no addition to tax imposed for underpayment of estimated tax of $1,000 or less for the taxable year.",
		"2025, c. 725; 2026, c. 7.",
	)
	mustContain(t, "acts-2026-c7.txt",
		"CHAPTER 7 [H 29] Approved February 20, 2026",
		"5. That § 58.1-492 of the Code of Virginia is amended and reenacted as follows:",
		"There shall be no addition to tax imposed for underpayment of estimated tax of $150$1,000 or less for the taxable year.",
		"6. That the provisions of the fifth enactment of this act shall apply to taxable years beginning on and after January 1, 2026.",
	)
	mustContain(t, "code-58.1-491.txt",
		"If the declaration is filed on or before May 1 of the taxable year, the estimated tax shall be paid in four equal installments.",
		"the second, third and fourth installments shall be paid on the following June 15, September 15, and January 15, respectively.",
		"the estimated tax shall be paid in three equal installments.",
	)
	mustContain(t, "code-58.1-490.txt",
		"1. The declaration shall be filed on or before June 15; or 2. After June 1 and before September 2 of the taxable year, the declaration shall be filed on or before September 15; or 3. After September 1 of the taxable year, the declaration shall be filed on or before January 15 of the succeeding year.",
		"I. If on or before March 1 of the succeeding taxable year an individual files his return",
		"which is at least two-thirds of his total estimated gross income for the taxable year",
	)
	mustContain(t, "code-58.1-321.txt",
		"is less than $11,950 for taxable years beginning on and after January 1, 2012.",
		"less than $23,900 for taxable years beginning on and after January 1, 2012 (or one-half of such amount in the case of a married individual filing a separate return).",
	)
	mustContain(t, "code-58.1-15.txt",
		"the \"Underpayment Rate\" established pursuant to § 6621(a)(2) of the Internal Revenue Code plus two percent.",
		"Separate computations shall be made by multiplying the deficiency or overpayment for each period by the rate of interest applicable to that period.",
		"shall also be applicable through May 1.",
	)
	mustContain(t, "23vac10-112-20.txt",
		"if his estimated tax, as defined in 23VAC10-112-10, is greater than $150.",
		"He is single and his expected Virginia adjusted gross income is less than $5,000;",
	)
	mustContain(t, "tb-26-2.txt",
		"Accordingly, the Virginia rates for the second quarter of 2026 will be 8% for tax underpayments (assessments)")
	mustContain(t, "tb-26-3.txt",
		"Accordingly, the Virginia rates for the third quarter of 2026 will be 9% for tax underpayments (assessments)")
	mustContain(t, "tb-26-4.txt",
		"Accordingly, the Virginia rates for the fourth quarter of 2026 will be 9% for tax underpayments (assessments)",
		"1-Apr-26 | 30-Jun-26 | 8% | 8% | 3% |",
		"1-July-26 | 31-Dec-26 | 9% | 9% | 4% |",
	)
	mustContain(t, "tb-25-7.txt",
		"the first quarter 9% underpayment rate will apply through the due date of the return, April 15, 2026 (corporations) and May 1, 2026 (individuals and fiduciaries).")
	mustContain(t, "code-2.2-3300.txt",
		"The last Monday in May — Memorial Day",
		"The first Monday in September — Labor Day",
		"The Tuesday following the first Monday in November — Election Day",
		"Whenever any of such days falls on Saturday, the Friday next preceding such day, or whenever any of such days falls on Sunday, the Monday next following such day",
	)
}

// The rates are the bulletins' quarters: 8% from April 1 to June 30, 2026 and
// 9% from July 1 to December 31, 2026, with 9% at the daily .00025 Form 760C
// prints and 8% at no printed factor.
func TestEstimatedRatesAreTheBulletins(t *testing.T) {
	want := []virginiataxcalc.RatePeriod{
		{From: day(2026, 4, 1), To: day(2026, 6, 30), Rate: money.Percent(8)},
		{From: day(2026, 7, 1), To: day(2026, 12, 31), Rate: money.Percent(9), Factor: "0.00025"},
	}
	got := virginiataxcalc.Rates(2026)
	if len(got) != len(want) {
		t.Fatalf("%d rate periods, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rate period %d is %+v, want %+v", i+1, got[i], want[i])
		}
	}
}

// The 2026 dates are the ones Form 760ES prints, and none moves.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	cal := virginiataxcalc.Calendar(2026)
	res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if !cal.IsBusinessDay(in.Due()) {
			t.Errorf("installment %d falls on a weekend or holiday", i+1)
		}
	}
}

// nthWeekday is the nth wd of a month, or the last for n = -1.
func nthWeekday(year int, m time.Month, wd time.Weekday, n int) estimated.Date {
	if n < 0 {
		d := day(year, m+1, 1).AddDays(-1)
		for d.Weekday() != wd {
			d = d.AddDays(-1)
		}
		return d
	}
	d := day(year, m, 1)
	for d.Weekday() != wd {
		d = d.AddDays(1)
	}
	return d.AddDays(7 * (n - 1))
}

// observed moves a fixed holiday on a Saturday to the Friday and on a Sunday
// to the Monday, as § 2.2-3300 does.
func observed(d estimated.Date) estimated.Date {
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDays(-1)
	case time.Sunday:
		return d.AddDays(1)
	}
	return d
}

// The calendar is § 2.2-3300's holidays from January 2026 to February 2027,
// worked out from the statute's rules, and it moves a date that falls on one
// or on a weekend to the next business day.
func TestEstimatedCalendarMovesWeekendsAndHolidays(t *testing.T) {
	want := []estimated.Date{
		observed(day(2026, 1, 1)),
		nthWeekday(2026, time.January, time.Monday, 3),
		nthWeekday(2026, time.February, time.Monday, 3),
		nthWeekday(2026, time.May, time.Monday, -1),
		observed(day(2026, 6, 19)),
		observed(day(2026, 7, 4)),
		nthWeekday(2026, time.September, time.Monday, 1),
		nthWeekday(2026, time.October, time.Monday, 2),
		nthWeekday(2026, time.November, time.Monday, 1).AddDays(1),
		observed(day(2026, 11, 11)),
		nthWeekday(2026, time.November, time.Thursday, 4),
		nthWeekday(2026, time.November, time.Thursday, 4).AddDays(1),
		observed(day(2026, 12, 25)),
		observed(day(2027, 1, 1)),
		nthWeekday(2027, time.January, time.Monday, 3),
		nthWeekday(2027, time.February, time.Monday, 3),
	}
	cal := virginiataxcalc.Calendar(2026)
	hs := cal.Holidays()
	if len(hs) != len(want) {
		t.Fatalf("%d holidays, want %d", len(hs), len(want))
	}
	for i, h := range hs {
		if h.On() != want[i] {
			t.Errorf("holiday %d (%s) is %v, want %v", i+1, h.Name(), h.On(), want[i])
		}
	}
	for _, tc := range []struct{ from, to estimated.Date }{
		{day(2026, 7, 3), day(2026, 7, 6)},     // Independence Day observed, then the weekend
		{day(2026, 9, 7), day(2026, 9, 8)},     // Labor Day
		{day(2026, 11, 26), day(2026, 11, 30)}, // Thanksgiving, the Friday after, the weekend
		{day(2027, 5, 1), day(2027, 5, 3)},     // a Saturday
		{day(2027, 3, 1), day(2027, 3, 1)},     // the March 1 rule's last day, a Monday
	} {
		if got := cal.NextBusinessDay(tc.from); got != tc.to {
			t.Errorf("%v moves to %v, want %v", tc.from, got, tc.to)
		}
	}
}

// Form 760ES Section I: estimated payments are required when tax after
// withholding is more than $1,000. $1,500 of tax with $500 withheld leaves
// exactly $1,000, which does not require them; a cent less withheld does.
func TestEstimatedFilingThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		required bool
	}{{50_000, false}, {49_999, true}} {
		res := must(rules(t).Requirement(household{tax: 1_500 * money.Dollar, withheld: tc.withheld}.facts(t)))
		if got := res.Value().Required(); got != tc.required {
			t.Errorf("withheld %v: required %v, want %v (%s)", tc.withheld, got, tc.required, res.Value().Why())
		}
	}
}

// Form 760ES Section I and § 58.1-321: below $11,950 of VAGI single or
// married filing separately, or $23,900 jointly, no return and no payments
// are required. No floor is given for other statuses.
func TestEstimatedIncomeFloorBoundaries(t *testing.T) {
	for _, tc := range []struct {
		status filing.Status
		agi    money.USCents
		exempt bool
	}{
		{filing.Single, 1_194_999, true},
		{filing.Single, 1_195_000, false},
		{filing.MarriedFilingJointly, 2_389_999, true},
		{filing.MarriedFilingJointly, 2_390_000, false},
		{filing.MarriedFilingSeparately, 1_194_999, true},
		{filing.MarriedFilingSeparately, 1_195_000, false},
		{filing.HeadOfHousehold, 500_000, false},
	} {
		h := household{status: tc.status, tax: 10_000 * money.Dollar, agi: usd(tc.agi)}
		res := must(rules(t).Requirement(h.facts(t)))
		if got := res.Value().Exempt(); got != tc.exempt {
			t.Errorf("%v with VAGI %v: exempt %v, want %v", tc.status, tc.agi, got, tc.exempt)
		}
		if c, _ := res.Value().Chosen(); tc.exempt && (c.Basis() != estimated.BasisException || c.Amount() != 0 || !strings.HasPrefix(c.Label(), "Virginia adjusted gross income under $")) {
			t.Errorf("%v with VAGI %v: chosen %v %q at %v, want the income floor", tc.status, tc.agi, c.Basis(), c.Label(), c.Amount())
		}
		if tc.exempt {
			if p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 1))); p.Value().Total() != 0 {
				t.Errorf("%v with VAGI %v: penalty %v", tc.status, tc.agi, p.Value().Total())
			}
		}
	}
}

// § 58.1-492(D)(3) and Form 760ES Section VIII(e): no addition when the
// year's underpayments total $1,000 or less. 90% of $1,111.11 is $1,000.00,
// $250 a quarter, all unpaid; 90% of $1,111.12 is $1,000.01.
func TestEstimatedSmallUnderpaymentBoundary(t *testing.T) {
	for _, tc := range []struct {
		tax    money.USCents
		excuse bool
	}{{111_111, true}, {111_112, false}} {
		f := household{tax: tc.tax}.facts(t)
		sched := must(rules(t).Schedule(f)).Value()
		var total money.USCents
		for _, in := range sched {
			total += in.Shortfall()
			if got := in.Status() == estimated.StatusExcused; got != tc.excuse {
				t.Errorf("tax %v: installment %d is %v", tc.tax, in.Number(), in.Status())
			}
		}
		p := must(rules(t).Penalty(f, day(2026, 12, 31))).Value()
		if got := p.Total() == 0; got != tc.excuse {
			t.Errorf("tax %v, underpayments %v: penalty %v", tc.tax, total, p.Total())
		}
	}
}

// The target is the least of 90% of this year's tax, 100% of last year's and
// the tax on last year's income at this year's rates. One cent decides
// between them, and a tie goes to this year's tax.
func TestEstimatedTargetOptions(t *testing.T) {
	for _, tc := range []struct {
		prior  money.USCents
		basis  estimated.Basis
		target money.USCents
	}{
		{899_999, estimated.BasisPriorYearTax, 899_999},
		{900_000, estimated.BasisThisYearTax, 900_000},
		{900_001, estimated.BasisThisYearTax, 900_000},
	} {
		r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(tc.prior)}.facts(t))).Value()
		if c, _ := r.Chosen(); c.Basis() != tc.basis || r.Target() != tc.target {
			t.Errorf("last year %v: chose %v at %v, want %v at %v", tc.prior, c.Basis(), r.Target(), tc.basis, tc.target)
		}
	}

	e := extras(virginiataxcalc.NewEstimatedFacts().PriorYearIncomeTax(7_999_99))
	r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(8_000 * money.Dollar), extras: e}.facts(t))).Value()
	if c, _ := r.Chosen(); c.Basis() != estimated.BasisPriorYearIncome || r.Target() != 799_999 {
		t.Errorf("with Exception 2 at $7,999.99: chose %v at %v", c.Basis(), r.Target())
	}

	// A return last year that showed no tax sets the target at zero: Form 760C
	// line 4 takes the lesser of 90% of this year's tax and the $0 on line 3.
	f := household{tax: 10_000 * money.Dollar, prior: prior(0)}.facts(t)
	if r := must(rules(t).Requirement(f)).Value(); r.Target() != 0 || r.Required() {
		t.Errorf("after a $0 return: target %v, required %v", r.Target(), r.Required())
	}
	if p := must(rules(t).Penalty(f, day(2027, 5, 1))).Value(); p.Total() != 0 {
		t.Errorf("after a $0 return: penalty %v", p.Total())
	}

	// Unavailable options say why, and the questions are asked.
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   string
	}{
		"no return on record": {nil, "Did you file a 2025 Virginia return"},
		"no return filed":     {&notFiled, "Form 760C says whether last year then counts as zero"},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		o := res.Value().Options()
		if o[1].Available() || o[1].WhyNot() == "" {
			t.Errorf("%s: last year's tax is offered", name)
		}
		if !strings.Contains(because(res.Required(), working.PositionPriorYearReturn), tc.ask) {
			t.Errorf("%s: asked %q", name, because(res.Required(), working.PositionPriorYearReturn))
		}
	}
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	if o := res.Value().Options()[2]; o.Available() || !strings.Contains(because(res.Required(), working.PositionJurisdictionFact), "cancel the charge under Exception 2") {
		t.Errorf("without the Exception 2 figure: offered %v, asked %q", o.Available(), because(res.Required(), working.PositionJurisdictionFact))
	}
}

// Without this year's tax the installments are measured against last year's
// tax, which can only be too high, so the penalty is an upper bound.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 8_000*money.Dollar {
		t.Errorf("required %v, target %v: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option: available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Error("this year's tax is not asked for")
	}
	for i, in := range must(rules(t).Schedule(h.facts(t))).Value() {
		if want := money.USCents(i+1) * 2_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31))).Value()
	if p.Total() == 0 || !p.UpperBound() {
		t.Errorf("unpaid: penalty %v, upper bound %v", p.Total(), p.UpperBound())
	}
	h.payments = quarterly(2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar)
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 1))).Value(); p.Total() != 0 || p.UpperBound() {
		t.Errorf("paid to last year's tax: penalty %v, upper bound %v; want an exact zero", p.Total(), p.UpperBound())
	}

	// With no return last year and only Exception 2, a missed installment is
	// measured against 90% of this year's tax, which is not known.
	ex2 := household{noTax: true, extras: extras(virginiataxcalc.NewEstimatedFacts().PriorYearIncomeTax(8_000 * money.Dollar))}
	if c, _ := must(rules(t).Requirement(ex2.facts(t))).Value().Chosen(); c.Basis() != estimated.BasisPriorYearIncome {
		t.Errorf("chose %v, want last year's income", c.Basis())
	}
	if _, err := rules(t).Penalty(ex2.facts(t), day(2027, 5, 1)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("unpaid under Exception 2 alone: %v, want ErrNeedsThisYearTax", err)
	}
	ex2.payments = h.payments
	if p := must(rules(t).Penalty(ex2.facts(t), day(2027, 5, 1))).Value(); p.Total() != 0 || p.UpperBound() {
		t.Errorf("paid to Exception 2: penalty %v, upper bound %v; want an exact zero", p.Total(), p.UpperBound())
	}

	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() {
		t.Error("a target with nothing to measure it by")
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 5, 1)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v", err)
	}
}

// 23VAC10-112-42, Example 2: $5,000 of tax, $1,125 a quarter, paid $200,
// $800, $3,000 and $0. The September overpayment of $1,875 goes $925 to May
// 1, $325 to June 15 and $625 to January 15. The June payment pays June
// before May, which is not the federal order.
func TestEstimatedPaymentOrderMatchesRegulationExample(t *testing.T) {
	mustContain(t, "23vac10-112-42.txt",
		"He made payments as follows: $200 on May 1, $800 on June 15, $3,000 on September 15 and $0 on January 15.",
		"The $1,875 overpayment from September 15 would be applied to the other quarterly underpayments in the following order and amounts: $925 to May 1, $325 to June 15, and $625 to January 15.")
	f := household{tax: 5_000 * money.Dollar, payments: quarterly(200*money.Dollar, 800*money.Dollar, 3_000*money.Dollar, 0)}.facts(t)
	sched := must(rules(t).Schedule(f)).Value()
	for i, want := range []money.USCents{925 * money.Dollar, 325 * money.Dollar, 0, 500 * money.Dollar} {
		if got := sched[i].Shortfall(); got != want {
			t.Errorf("installment %d is %v short, want %v", i+1, got, want)
		}
	}
	lines := must(rules(t).Penalty(f, day(2027, 5, 1))).Value().Lines()
	if len(lines) != 3 {
		t.Fatalf("%d penalty lines, want three", len(lines))
	}
	// May's $925 and June's $325 are paid on September 15; January's $500 is
	// never paid and runs to May 1, 2027.
	for i, want := range []int{137, 92, 106} {
		if lines[i].Days() != want {
			t.Errorf("line %d runs %d days, want %d", lines[i].Installment(), lines[i].Days(), want)
		}
	}
}

// 23VAC10-112-42, Example 1: $10,000 of tax and $1,500 paid on each date is
// $750 short on each, for 365, 320, 228 and 106 days to May 1, the day counts
// Form 760C line 22 prints. The example's charges at 12% ($90, $78.75, $56.25
// and $26.25) follow 12, 10.5, 7.5 and 3.5 months rather than the days it
// writes, and 2026 has its own rates, so the charges here are worked from the
// 2026 rates: May's $750 is 60 days at 8%, $9.86, and 184 days at .00025,
// $34.50, with the 2027 days unrated.
func TestEstimatedUnderpaymentMatchesRegulationExample(t *testing.T) {
	mustContain(t, "23vac10-112-42.txt",
		"90% of $2,500 = | $2,250 |",
		"less installment paid | $1,500 |",
		"| $750 |",
		"2nd installment-period: 6/15/88 to 5/1/89 (12% x 320/365 x $750) | $78.75 |",
		"Total addition to tax | $251.25 |")
	f := household{tax: 10_000 * money.Dollar, payments: quarterly(1_500*money.Dollar, 1_500*money.Dollar, 1_500*money.Dollar, 1_500*money.Dollar)}.facts(t)
	res := must(rules(t).Penalty(f, day(2027, 5, 1)))
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	for i, want := range []int{365, 320, 228, 106} {
		if lines[i].Shortfall() != 750*money.Dollar || lines[i].Days() != want {
			t.Errorf("line %d: %v short for %d days, want $750 for %d", i+1, lines[i].Shortfall(), lines[i].Days(), want)
		}
	}
	if got := lines[0].Total(); got != 4_436 {
		t.Errorf("May's line is %v, want $44.36", got)
	}
	if !strings.Contains(because(res.Required(), working.PositionUnpublishedRate), "rate for the days after Dec 31, 2026 isn't published yet") {
		t.Errorf("the 2027 gap is not reported: %q", because(res.Required(), working.PositionUnpublishedRate))
	}
}

// The Form 760C instructions for line 13: a $1,000 payment on May 15 against
// a $500 underpayment in column A pays $500 late in A and $500 on time in B.
// Here May 1 is $500 short of $2,250, and January is left unpaid so that the
// $1,000 exception does not hide the charge: $500 for 14 days at 8%, $1.53.
func TestEstimatedLatePaymentMatchesInstructionsExample(t *testing.T) {
	mustContain(t, "760c-2025-instructions.txt",
		"a taxpayer made a late payment of $1,000 for the first installment on May 15, 2025 and had an underpayment on Line 11, Column A, of only $500.")
	f := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{
		pay(dues[0], 1_750*money.Dollar), pay(day(2026, 5, 15), 1_000*money.Dollar), pay(dues[1], 1_750*money.Dollar), pay(dues[2], 2_250*money.Dollar),
	}}.facts(t)
	sched := must(rules(t).Schedule(f)).Value()
	if sched[0].Shortfall() != 500*money.Dollar || sched[1].Shortfall() != 0 {
		t.Errorf("A is %v short and B %v; want $500 and nothing", sched[0].Shortfall(), sched[1].Shortfall())
	}
	lines := must(rules(t).Penalty(f, day(2026, 12, 31))).Value().Lines()
	if len(lines) != 2 || lines[0].Days() != 14 || lines[0].Total() != 153 {
		t.Errorf("lines %d; May's runs %d days for %v, want 14 days and $1.53", len(lines), lines[0].Days(), lines[0].Total())
	}
}

// The Form 760C instructions for line 21: $1,000 short on May 1, paid $500
// on May 15 and $500 on June 30, is two computations. At 8% they are $1.53
// for 14 days and $6.58 for 60.
func TestEstimatedTwoPaymentsMatchInstructionsExample(t *testing.T) {
	mustContain(t, "760c-2025-instructions.txt",
		"if you report an underpayment of $1,000 for the first installment period and pay $500 on May 15, 2025 AND $500 on June 30, 2025, you will need to make two separate computations:")
	f := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{
		pay(dues[0], 1_250*money.Dollar), pay(day(2026, 5, 15), 500*money.Dollar), pay(dues[1], 2_250*money.Dollar),
		pay(day(2026, 6, 30), 500*money.Dollar), pay(dues[2], 2_250*money.Dollar),
	}}.facts(t)
	line := must(rules(t).Penalty(f, day(2026, 12, 31))).Value().Lines()[0]
	parts := line.Parts()
	if len(parts) != 2 || parts[0].Days() != 14 || parts[0].Charge() != 153 || parts[1].Days() != 60 || parts[1].Charge() != 658 {
		t.Fatalf("May's parts %+v; want 14 days for $1.53 and 60 days for $6.58", parts)
	}
	if line.Days() != 60 || line.Total() != 811 {
		t.Errorf("May's line runs %d days for %v, want 60 days and $8.11", line.Days(), line.Total())
	}
}

// A shortfall from May 1 paid on September 15 spans the change from 8% to 9%
// on July 1. § 58.1-15(A) charges each period at its own rate: $10,000 for
// 60 days at 8% over 365 is $131.51, and for 77 days at .00025 is $192.50.
func TestEstimatedPenaltySpansTheRateChange(t *testing.T) {
	// 90% of $44,444.44 is $40,000, $10,000 a quarter.
	f := household{tax: 4_444_444, payments: quarterly(0, 10_000*money.Dollar, 20_000*money.Dollar, 10_000*money.Dollar)}.facts(t)
	res := must(rules(t).Penalty(f, day(2027, 5, 1)))
	lines := res.Value().Lines()
	if len(lines) != 1 {
		t.Fatalf("%d lines, want May's alone", len(lines))
	}
	parts := lines[0].Parts()
	if len(parts) != 2 {
		t.Fatalf("%d parts, want one for each rate", len(parts))
	}
	for i, want := range []struct {
		from, to estimated.Date
		days     int
		rate     money.Rate
		charge   money.USCents
	}{
		{day(2026, 5, 1), day(2026, 6, 30), 60, money.Percent(8), 13_151},
		{day(2026, 6, 30), day(2026, 9, 15), 77, money.Percent(9), 19_250},
	} {
		p := parts[i]
		if p.From() != want.from || p.To() != want.to || p.Days() != want.days || p.Rate() != want.rate || p.Charge() != want.charge {
			t.Errorf("part %d: %v to %v, %d days at %v, %v; want %v to %v, %d days at %v, %v",
				i+1, p.From(), p.To(), p.Days(), p.Rate(), p.Charge(), want.from, want.to, want.days, want.rate, want.charge)
		}
	}
	if res.Value().Total() != 32_401 {
		t.Errorf("penalty %v, want $324.01", res.Value().Total())
	}
	if !strings.Contains(because(res.Required(), working.PositionUnpublishedRate), "8% times the days over 365") {
		t.Errorf("the 8%% default is not reported: %q", because(res.Required(), working.PositionUnpublishedRate))
	}
}

// Days at 9% are charged at the .00025 Form 760C prints, not 9% over 365: on
// $100,000 for the 107 days from September 15 to December 31 that is
// $2,675.00, where 9% over 365 would be $2,638.36.
func TestEstimatedInterestUsesThePrintedDailyRate(t *testing.T) {
	// 90% of $444,444.44 is $400,000, $100,000 a quarter.
	f := household{tax: 44_444_444, payments: quarterly(100_000*money.Dollar, 100_000*money.Dollar, 0, 100_000*money.Dollar)}.facts(t)
	res := must(rules(t).Penalty(f, day(2026, 12, 31)))
	lines := res.Value().Lines()
	if len(lines) != 1 || lines[0].Installment() != 3 || lines[0].Total() != 2_675*money.Dollar {
		t.Errorf("lines %d, total %v; want September's alone at $2,675", len(lines), res.Value().Total())
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Errorf("an unpublished rate is reported for days at 9%%: %q", because(res.Required(), working.PositionUnpublishedRate))
	}
}

// The addition runs to May 1, 2027 at the latest, and the rate from January
// 1, 2027 is not published, so those days are reported rather than charged.
func TestEstimatedPenaltyStopsOnMayFirst(t *testing.T) {
	f := household{tax: 44_444_444, payments: quarterly(100_000*money.Dollar, 100_000*money.Dollar, 0, 100_000*money.Dollar)}.facts(t)
	res := must(rules(t).Penalty(f, day(2027, 6, 30)))
	p := res.Value()
	if p.AsOf() != day(2027, 5, 1) {
		t.Errorf("figured to %v, want May 1, 2027", p.AsOf())
	}
	if l := p.Lines()[0]; l.Days() != 228 || l.Total() != 2_675*money.Dollar {
		t.Errorf("September's line runs %d days for %v; want 228 days, charged only to December 31", l.Days(), l.Total())
	}
	if !strings.Contains(because(res.Required(), working.PositionUnpublishedRate), "rate for the days after Dec 31, 2026 isn't published yet") {
		t.Errorf("the gap is not reported: %q", because(res.Required(), working.PositionUnpublishedRate))
	}
}

// Exception 1 needs a twelve-month return last year that showed tax. It asks
// for the same amounts as the installments measured on last year's tax.
func TestEstimatedExceptionOneConditions(t *testing.T) {
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(must(estimated.NewFigures().Tax(4_000 * money.Dollar).Build())).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		want  string
	}{
		"a full year with tax": {prior(4_000 * money.Dollar), "Exception 1 is open"},
		"a full year, no tax":  {prior(0), "Exception 1 is not open"},
		"six months":           {&short, "Exception 1 is not open"},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		if !strings.Contains(applied(res.Applied()), tc.want) {
			t.Errorf("%s: %q is not applied", name, tc.want)
		}
	}
}

// Exception 2 cancels the addition for each installment whose payments by
// its date reach 25%, 50%, 75% and 100% of the tax on last year's income at
// this year's rates. A cent short in May leaves May charged; June is
// excused because the cent is made up by June 15.
func TestEstimatedExceptionTwo(t *testing.T) {
	e := extras(virginiataxcalc.NewEstimatedFacts().PriorYearIncomeTax(8_000 * money.Dollar))
	h := household{tax: 20_000 * money.Dollar, extras: e, payments: quarterly(199_999, 200_001, 200_000, 200_000)}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	want := []estimated.Status{estimated.StatusShort, estimated.StatusExcused, estimated.StatusExcused, estimated.StatusExcused}
	for i, in := range sched {
		if in.Status() != want[i] || in.Shortfall() == 0 {
			t.Errorf("installment %d is %v with %v short; want %v with a shortfall", i+1, in.Status(), in.Shortfall(), want[i])
		}
	}
	lines := must(rules(t).Penalty(h.facts(t), day(2027, 5, 1))).Value().Lines()
	if len(lines) != 1 || lines[0].Installment() != 1 {
		t.Errorf("%d lines; want May's alone", len(lines))
	}
	h.payments = quarterly(2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar)
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 1))).Value(); p.Total() != 0 {
		t.Errorf("paid to Exception 2: penalty %v", p.Total())
	}
}

// 23VAC10-112-43, Example 4: Virginia tax of $2,048, $2,755 and $4,722 on
// annualized income needs $460.80, $1,239.75 and $3,187.35, and $1,100 a
// quarter meets it. In 2026 the $25 shortfalls also total under $1,000.
func TestEstimatedExceptionThreeMatchesRegulationExample(t *testing.T) {
	mustContain(t, "23vac10-112-43.txt",
		"5. Virginia tax on the amount shown on line 4 | 2,048 | 2,755 | 4,722 |",
		"Installments due through the applicable period | $460.80 | $1,239.75 | $3,187.35 |",
		"Installments paid through the applicable period | $1,100 | $2,200 | $3,300 |")
	h := household{tax: 5_000 * money.Dollar, annualised: []money.USCents{2_048 * money.Dollar, 2_755 * money.Dollar, 4_722 * money.Dollar, 5_000 * money.Dollar},
		payments: quarterly(1_100*money.Dollar, 1_100*money.Dollar, 1_100*money.Dollar, 1_100*money.Dollar)}
	res := must(rules(t).Schedule(h.facts(t)))
	sched := res.Value()
	// Each date needs a quarter of 90% of $5,000 by the running total; the
	// exception's lower amounts are in the working, and every date is excused.
	for i, want := range []money.USCents{112_500, 225_000, 337_500, 450_000} {
		if sched[i].NeededBy() != want || sched[i].Status() != estimated.StatusExcused {
			t.Errorf("installment %d needs %v and is %v; want %v and excused", i+1, sched[i].NeededBy(), sched[i].Status(), want)
		}
	}
	if !strings.Contains(applied(res.Applied()), "$460.80, $1239.75 and $3187.35") {
		t.Errorf("Exception 3 does not need $460.80, $1,239.75 and $3,187.35: %s", applied(res.Applied()))
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 1))).Value(); p.Total() != 0 {
		t.Errorf("penalty %v, want none", p.Total())
	}
}

// Exception 3 at ten times the example, where the $1,000 exception does not
// apply: $4,608 by May 1 meets it and a cent less does not; September misses
// it, and it never applies to January.
func TestEstimatedExceptionThree(t *testing.T) {
	for _, tc := range []struct {
		may     money.USCents
		mayCase estimated.Status
	}{{460_800, estimated.StatusExcused}, {460_799, estimated.StatusShort}} {
		h := household{tax: 50_000 * money.Dollar, annualised: []money.USCents{20_480 * money.Dollar, 27_550 * money.Dollar, 47_220 * money.Dollar, 50_000 * money.Dollar},
			payments: quarterly(tc.may, 10_000*money.Dollar, 10_000*money.Dollar, 10_000*money.Dollar)}
		sched := must(rules(t).Schedule(h.facts(t))).Value()
		want := []estimated.Status{tc.mayCase, estimated.StatusExcused, estimated.StatusShort, estimated.StatusShort}
		for i, in := range sched {
			if in.Status() != want[i] {
				t.Errorf("May %v: installment %d is %v, want %v", tc.may, i+1, in.Status(), want[i])
			}
		}
	}
}

// 23VAC10-112-43, Example 5: tax of $505, $969 and $3,154 on the income
// received to April 30, May 31 and August 31 needs 90% of it, which the
// example rounds to $455, $872 and $2,839, and $975 a quarter meets it.
func TestEstimatedExceptionFourMatchesRegulationExample(t *testing.T) {
	mustContain(t, "23vac10-112-43.txt",
		"7. Virginia tax on amounts shown on line 6 | 505 | 969 | 3,154 |",
		"8. 90% of line 7 | 455 | 872 | 2,839 |",
		"9. Installments paid through the applicable period | 975 | 1,950 | 2,925 |")
	e := extras(virginiataxcalc.NewEstimatedFacts().IncomeToDateTax(505*money.Dollar, 969*money.Dollar, 3_154*money.Dollar))
	h := household{tax: 5_000 * money.Dollar, prior: prior(4_000 * money.Dollar), extras: e,
		payments: quarterly(975*money.Dollar, 975*money.Dollar, 975*money.Dollar, 975*money.Dollar)}
	res := must(rules(t).Schedule(h.facts(t)))
	sched := res.Value()
	// 90% of $505, $969 and $3,154 is $454.50, $872.10 and $2,838.60, which
	// the example rounds to $455, $872 and $2,839. The running totals are a
	// quarter of last year's $4,000 each, and every date is excused.
	if !strings.Contains(applied(res.Applied()), "$454.50, $872.10 and $2838.60") {
		t.Errorf("Exception 4 does not need $454.50, $872.10 and $2,838.60: %s", applied(res.Applied()))
	}
	for i, want := range []money.USCents{100_000, 200_000, 300_000} {
		if sched[i].NeededBy() != want || sched[i].Status() != estimated.StatusExcused {
			t.Errorf("installment %d needs %v and is %v; want %v and excused", i+1, sched[i].NeededBy(), sched[i].Status(), want)
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 1))).Value(); p.Total() != 0 {
		t.Errorf("penalty %v, want none", p.Total())
	}
}

// Exception 4 at ten times the example: $4,545 by May 1 meets it and a cent
// less does not. June is excused either way. The year is paid in full by
// January 15.
func TestEstimatedExceptionFour(t *testing.T) {
	e := extras(virginiataxcalc.NewEstimatedFacts().IncomeToDateTax(5_050*money.Dollar, 9_690*money.Dollar, 31_540*money.Dollar))
	for _, tc := range []struct {
		may   money.USCents
		lines int
	}{{454_500, 0}, {454_499, 1}} {
		h := household{tax: 50_000 * money.Dollar, extras: e, payments: quarterly(tc.may, 5_000*money.Dollar, 20_000*money.Dollar, 2_000_000-tc.may)}
		sched := must(rules(t).Schedule(h.facts(t))).Value()
		if sched[1].Status() != estimated.StatusExcused {
			t.Errorf("May %v: June is %v, want excused", tc.may, sched[1].Status())
		}
		if lines := must(rules(t).Penalty(h.facts(t), day(2027, 5, 1))).Value().Lines(); len(lines) != tc.lines {
			t.Errorf("May %v: %d lines, want %d", tc.may, len(lines), tc.lines)
		}
	}
}

// Form 760ES Section IV: filing and paying in full by March 1, 2027 means
// the January payment is not required. March 2 is too late, and the addition
// then stops on the day the return is paid.
func TestEstimatedReturnFiledByMarchFirst(t *testing.T) {
	three := quarterly(2_250*money.Dollar, 2_250*money.Dollar, 2_250*money.Dollar, 0)
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 3, 1), true}, {day(2027, 3, 2), false}} {
		f := household{tax: 10_000 * money.Dollar, payments: three, returnFiled: tc.filed}.facts(t)
		jan := must(rules(t).Schedule(f)).Value()[3]
		if got := jan.Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January is %v", tc.filed, jan.Status())
		}
		p := must(rules(t).Penalty(f, day(2027, 5, 1))).Value()
		if p.AsOf() != tc.filed {
			t.Errorf("filed %v: figured to %v", tc.filed, p.AsOf())
		}
		if got := len(p.Lines()) == 0; got != tc.excused {
			t.Errorf("filed %v: %d lines", tc.filed, len(p.Lines()))
		}
	}
}

// Form 760ES's payment schedule: a requirement first met after April 15 is
// paid in thirds from June 15, after June 1 in halves from September 15, and
// after September 1 in full on January 15.
func TestEstimatedLateStart(t *testing.T) {
	for _, tc := range []struct {
		met  estimated.Date
		need []money.USCents
	}{
		{day(2026, 4, 15), []money.USCents{225_000, 450_000, 675_000, 900_000}},
		{day(2026, 4, 16), []money.USCents{0, 300_000, 600_000, 900_000}},
		{day(2026, 6, 1), []money.USCents{0, 300_000, 600_000, 900_000}},
		{day(2026, 6, 2), []money.USCents{0, 0, 450_000, 900_000}},
		{day(2026, 9, 1), []money.USCents{0, 0, 450_000, 900_000}},
		{day(2026, 9, 2), []money.USCents{0, 0, 0, 900_000}},
	} {
		sched := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, firstMet: tc.met}.facts(t))).Value()
		for i, in := range sched {
			if in.NeededBy() != tc.need[i] {
				t.Errorf("first met %v: installment %d needs %v, want %v", tc.met, i+1, in.NeededBy(), tc.need[i])
			}
			if tc.need[i] == 0 && in.Status() != estimated.StatusNothingDue {
				t.Errorf("first met %v: installment %d is %v, want nothing due", tc.met, i+1, in.Status())
			}
		}
	}

	// Not told when the requirement arose, the rules ask, and take it as by
	// April 15.
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 5, 1)))
	if !has(res.Required(), working.PositionRequirementFirstMet) {
		t.Error("the late-start question is not asked")
	}
	res = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, firstMet: day(2026, 1, 1)}.facts(t), day(2027, 5, 1)))
	if has(res.Required(), working.PositionRequirementFirstMet) {
		t.Error("the late-start question is asked though it was answered")
	}
}

// Withholding is divided by four unless the filer elects to show when it was
// withheld. $36,000 withheld on December 15 covers every quarter when divided,
// and only January when entered in the period it was withheld in.
func TestEstimatedWithholdingAsWithheld(t *testing.T) {
	h := household{tax: 40_000 * money.Dollar, withheld: 36_000 * money.Dollar}
	for _, in := range must(rules(t).Schedule(h.facts(t))).Value() {
		if in.Shortfall() != 0 {
			t.Errorf("divided by four: installment %d is %v short", in.Number(), in.Shortfall())
		}
	}
	h.asWithheld = true
	for i, in := range must(rules(t).Schedule(h.facts(t))).Value() {
		want := 9_000 * money.Dollar
		if i == 3 {
			want = 0
		}
		if in.Shortfall() != want {
			t.Errorf("as withheld: installment %d is %v short, want %v", i+1, in.Shortfall(), want)
		}
	}
}

// Neither the Code nor the forms provide a waiver, so asking for one changes
// nothing and says so.
func TestEstimatedWaiverIsNotAvailable(t *testing.T) {
	casualty := estimated.WaiverCasualty
	plain := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2026, 12, 31)))
	asked := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, waiver: &casualty}.facts(t), day(2026, 12, 31)))
	if asked.Value().Total() != plain.Value().Total() || plain.Value().Total() == 0 {
		t.Errorf("with a waiver %v, without %v", asked.Value().Total(), plain.Value().Total())
	}
	if !strings.Contains(applied(asked.Applied()), "provide a waiver") {
		t.Error("the missing waiver is not explained")
	}
	if o, ok := asked.Value().WaiverOutcome(); !ok || !o.Refused() || asked.Value().IfWaived() != asked.Value().Total() {
		t.Errorf("casualty waiver outcome %+v, %v", o, ok)
	}
	// Retiring in 2026 at 64 meets the federal conditions, but Virginia has
	// no such waiver.
	retired := must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build())
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, positions: &retired}.facts(t), day(2026, 12, 31)))
	if o, ok := res.Value().WaiverOutcome(); !ok || !o.Refused() || !strings.Contains(o.Reason(), "retirement") {
		t.Errorf("retirement waiver outcome %q, %v", o.Reason(), ok)
	}
	if res.Value().Total() != plain.Value().Total() {
		t.Errorf("with a retirement waiver %v, without %v", res.Value().Total(), plain.Value().Total())
	}
}

// Under the income floor no target is needed, so no question about last
// year's return or this year's tax is asked.
func TestEstimatedIncomeFloorAsksNothingAboutTheTarget(t *testing.T) {
	res := must(rules(t).Requirement(household{noTax: true, agi: usd(5_000 * money.Dollar)}.facts(t)))
	if !res.Value().Exempt() {
		t.Fatal("not exempt under the income floor")
	}
	for _, q := range res.Required() {
		if strings.Contains(strings.ToLower(q.Effect()), "target") {
			t.Errorf("an exempt requirement asks %q", q.Effect())
		}
	}
	if has(res.Required(), working.PositionThisYearTax) || has(res.Required(), working.PositionPriorYearReturn) {
		t.Error("an exempt requirement asks for this year's tax or last year's return")
	}
}

// Two-thirds of gross income from farming, fishing or work as a merchant
// seaman puts a filer under Form 760F, which is not built.
func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, year := range []int{2025, 2027} {
		if _, err := virginiataxcalc.EstimatedTax(year); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", year, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	base := func() *estimated.FactsBuilder {
		return estimated.NewFacts(va, 2026).FilingStatus(filing.Single).ThisYear(this)
	}
	seaman := must(virginiataxcalc.NewEstimatedFacts().MerchantSeamanShare(6_667).Build())
	mixed := must(virginiataxcalc.NewEstimatedFacts().MerchantSeamanShare(3_000).Build())
	for name, tc := range map[string]struct {
		f           estimated.Facts
		unsupported bool
	}{
		"a farmer":              {must(base().FarmShare(6_667).Build()), true},
		"a fisherman":           {must(base().FishShare(6_667).Build()), true},
		"a merchant seaman":     {must(base().State(seaman).Build()), true},
		"farming and seafaring": {must(base().FarmShare(3_667).State(mixed).Build()), true},
		"just under two-thirds": {must(base().FarmShare(6_666).Build()), false},
	} {
		_, err := rules(t).Requirement(tc.f)
		if got := errors.Is(err, estimated.ErrUnsupported); got != tc.unsupported {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"Pennsylvania's facts": must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":                 must(estimated.NewFacts(va, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":         must(base().State("not Virginia's").Build()),
		"wrong period ends": must(base().Elections(must(estimated.NewElections().Annualised().Build())).Periods(
			must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 5, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := virginiataxcalc.NewEstimatedFacts().MerchantSeamanShare(10_001).Build(); err == nil {
		t.Error("a share over 100% is accepted")
	}

	// Not told about seafaring, the rules ask and assume none.
	res := must(rules(t).Requirement(must(base().Build())))
	if !strings.Contains(because(res.Required(), working.PositionJurisdictionFact), "merchant seaman") {
		t.Error("the merchant seaman share is not asked for")
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"When estimated payments are required":                  {"more than $1,000", "23VAC10-112-20"},
		"The small-underpayment exception":                      {"$150 or less", "2026 Acts c. 7"},
		"The January installment for a return filed by March 1": {"for any filer", "§ 58.1-490(I)"},
		"A requirement first met after April 15":                {"withholding is still divided by four", "58.1-492(C)(1) and (D)(2)"},
		"Exceptions 3 and 4 and the January installment":        {"do not apply to the fourth installment", "(C)(1)(c) and (C)(2)"},
		"Withholding on the dates withheld":                     {"that period's column", "58.1-492(B)(2) and (D)(2)"},
		"The daily rate":                                        {".00025", "23VAC10-112-42"},
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
	// The small-underpayment figure follows the 2026 law; the rest follow the
	// forms. With nothing paid, the September and January shortfalls run at
	// the printed 9% daily rate, and nothing else these facts hold turns on a
	// divergence.
	for _, d := range res.Divergences() {
		uses := working.ReadingForm
		if d.Rule() == "The small-underpayment exception" {
			uses = working.ReadingStatute
		}
		if d.Uses() != uses {
			t.Errorf("%q uses %v, want %v", d.Rule(), d.Uses(), uses)
		}
		if want := d.Rule() == "The daily rate"; d.AffectsHousehold() != want {
			t.Errorf("%q affects these facts %v, want %v", d.Rule(), d.AffectsHousehold(), want)
		}
	}
	// Shortfalls of exactly $1,000 are excused by the law and not by the
	// 2025 form's $150.
	for _, d := range must(rules(t).Requirement(household{tax: 111_111}.facts(t))).Divergences() {
		if d.Rule() == "The small-underpayment exception" && !d.AffectsHousehold() {
			t.Error("$1,000 of shortfalls: the small-underpayment reading does not affect these facts")
		}
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$1,000", "May 1", "90%", "100%", "no 110% tier", "March 1, 2027", "8%", "9%", ".00025", "pays that installment before an earlier one"} {
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
	b := estimated.NewFacts("VA", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
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
