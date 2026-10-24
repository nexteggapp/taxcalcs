// Arizona's estimated tax rules for 2026, checked against the Department of
// Revenue's own documents and the Arizona Revised Statutes.
//
// Arizona publishes no worked example for estimated tax and no exhaustive
// table. The strongest check is Form 221's Chart of Total Days per Period,
// which prints the days each installment spends in each rate period when it
// is never paid; its figures fit the 2026 due dates. Every threshold,
// percentage, date and rate is compared against its extract in testdata/.
// The penalty arithmetic, the exceptions and the boundaries are hand-written
// cases, worked in the comments.
package arizonataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/arizonataxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const az = estimated.Jurisdiction("AZ")

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
	return must(arizonataxcalc.EstimatedTax(2026))
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
			out = append(out, r.Question()+" "+r.Effect())
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

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

// answered are extras with every Arizona fact answered so that no exception
// applies.
func answered() *arizonataxcalc.EstimatedFacts {
	e := must(arizonataxcalc.NewEstimatedFacts().NoFederalLiabilityLastYear(false).
		FederalTaxLessWithholding(50_000 * money.Dollar).PriorYearReturnRequired(true).Build())
	return &e
}

type household struct {
	status      filing.Status
	tax         money.USCents // after credits, Form 221 line 4
	noTax       bool
	gross       money.USCents // this year's Arizona gross income; $200,000 if zero
	noGross     bool
	agi         money.USCents // this year's federal AGI, given only when set
	withheld    money.USCents
	asWithheld  bool
	prior       *estimated.PriorReturn // a filed return with $1,000,000 of tax if nil
	noPrior     bool
	payments    []estimated.Payment
	extras      *arizonataxcalc.EstimatedFacts
	returnFiled estimated.Date
	annualised  []money.USCents // each period's annualised tax, Form 221 worksheet line 14
	waiver      bool
	positions   *estimated.Positions // a waiver asked for, with its cause and dates
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
	if !h.noGross {
		g := h.gross
		if g == 0 {
			g = 200_000 * money.Dollar
		}
		fb.GrossIncome(g)
	}
	if h.agi != 0 {
		fb.FederalAGI(h.agi)
	}
	b := estimated.NewFacts(az, 2026).FilingStatus(status).ThisYear(must(fb.Build())).
		Paid(h.payments...).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	switch {
	case h.prior != nil:
		b.PriorYear(*h.prior)
	case !h.noPrior:
		b.PriorYear(*prior(1_000_000*money.Dollar, 200_000*money.Dollar))
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(*answered())
	}
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
	}
	eb := estimated.NewElections()
	if h.annualised != nil {
		eb.Annualised()
	}
	if h.asWithheld {
		eb.WithholdingAsWithheld()
	}
	b.Elections(must(eb.Build()))
	if h.waiver {
		b.Positions(must(estimated.NewPositions().Waiver(estimated.WaiverNone).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

// prior is a filed 2025 return with its tax after credits and its Arizona
// gross income.
func prior(tax, gross money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).GrossIncome(gross).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(true).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "az-140es-2026-instructions.txt",
		"Your Arizona gross income for 2025 was greater than: $75,000 AND Your Arizona gross income for 2026 exceeds: $75,000 Single Married filing $150,000 $150,000 Joint Married filing $75,000 $75,000 separate Head of $75,000 $75,000 Household",
		"your Arizona gross income is that part of your federal adjusted gross income that you must report to Arizona.",
		"must total either 90% of the tax due for 2026, or 100% of the tax due for 2025.",
		"only if you were required to file and did file a 2025 Arizona income tax return.",
		"For the most part, you must make your payments in four equal installments.",
		"April 15, 2026. June 15, 2026 September 15, 2026 January 15,2027.",
		"If the due date falls on a Saturday, Sunday, or legal holiday, you may pay by midnight on the next business day following that day.",
		"If you file your 2026 Arizona return by February 1, 2027and pay in full the amount stated on the return as payable, you do not have to make the fourth estimated tax payment.",
		"If you report as a farmer or fisherman for federal purposes, you only have to make one installment for a taxable year.",
		"The first installment must equal 50% of your total required payments.",
		"The penalty cannot be more than 10% of the underpayment.",
	)
	mustContain(t, "az-140es-2026.txt",
		"1st Quarter – January to March | Due date is April 15, 2026.",
		"2nd Quarter – April to June | Due date is June 15, 2026.",
		"3rd Quarter – July to September | Due date is September 15, 2026.",
		"4th Quarter – October to December | Due date is January 15, 2027.",
		"22 Enter 90% (.90) of line 21",
		"23 Enter the preceding year’s Arizona tax liability if you were required to file and did file a return for the preceding year",
		"enter 1/4 of line 26",
	)
	mustContain(t, "ador-individual-estimated-payments.txt",
		"Single | $75,000 | $75,000",
		"Married Filing Joint | $150,000 | $150,000",
		"Head of Household | $75,000 | $75,000",
		"Estimated tax payments are due April 15, June 15, September 15, 2026, and January 15, 2027.",
	)
	mustContain(t, "ars-43-581.txt",
		"for the taxable year exceeds $75,000 or $150,000 if a joint return is filed and whose Arizona gross income was greater than $75,000 in the preceding taxable year or $150,000 in the preceding taxable year if a joint return is filed shall make payments of estimated tax",
		"paid in four installments on or before the due dates established by the internal revenue code",
		"at least ninety percent of the tax due for the current taxable year or one hundred percent of the tax due for the preceding taxable year.",
		"The estimated tax payments made pursuant to this section are allowable exceptions under section 6654 of the internal revenue code.",
		"The taxpayer's Arizona income tax liability due on the taxpayer's return is less than $1,000.",
		"the amount of tax due on the return minus the amount of Arizona income tax withheld and tax credits claimed by the taxpayer.",
	)
	mustContain(t, "ars-42-1125-q.txt",
		"a penalty is assessed equal to the amount of interest that would otherwise accrue under section 42-1123 on the amount not paid for the period of nonpayment, not exceeding ten percent of the amount not paid.",
	)
	mustContain(t, "ars-42-1123-ab.txt",
		"interest, compounded annually, in the same manner and at the same times as prescribed by section 6621",
		"the federal short-term rate, determined pursuant to section 6621(b) of the internal revenue code, plus three percentage points.",
		"On January 1 of each year the department shall add any interest outstanding as of that date to the principal amount of the tax.",
	)
	mustContain(t, "ador-interest-rates.txt",
		"7/1/2026 | 12/31/2026 | | 365 | 7% |",
		"4/1/2026 | 6/30/2026 | | 365 | 6% |",
	)
	mustContain(t, "az-221-2025.txt",
		"I am claiming an exception from the imposition of the estimated payment penalty because I qualified for federal relief under IRC § 6654.",
		"Subtract line 5 from line 4. If less than $1,000, stop here. You do not owe the penalty.",
		"7 Multiply line 4 by 90% (.90)",
		"Enter the immediately preceding year’s tax liability after tax credits.",
		"Rate Period One: 7% (Apr-15-25 through Jun-30-25)",
		"Rate Period Two: 7% (Jul-1-25 through Sep-30-25)",
		"Rate Period Three: 7% (Oct-1-25 through Dec-31-25)",
		"Rate Period Four: *% (Jan-1-26 through Apr-15-26)",
		"or April 15, 2026 whichever is earlier",
		"penalty from Rate Period 1, 2, and 3 if the",
		"underpayment is unpaid as of Jan-1-26",
		"list the smaller of line 15 x 10% or the amount from line 28",
		"Jan-1-25 Jan-1-25 Jan-1-25 Jan-1-25 to to to to Mar-31-25 May-31-25 Aug-31-25 Dec-31-25",
		"15 Applicable percentages",
		"22.5% 45% 67.5% 90%",
	)
	mustContain(t, "az-221-2025-instructions.txt",
		"If your Arizona gross income was less than the prescribed thresholds for either 2024 or 2025, you are not subject to the penalty.",
		"You had no federal tax liability for 2024. You were also a U.S. citizen or resident of the U.S. for the entire year and your 2024 tax return was (or would have been had you been required to file) for a full 12 months.",
		"The total tax shown on your 2025 federal income tax return minus your federal withholding is less than $1,000.",
		"Only enter an amount on line 8 if you filed an Arizona personal income tax return for 2024.",
		"Divide the amount on line 9 by four (4).",
		"The department considers you to have paid ¼ (25%) of Arizona income tax withheld on each payment due date unless you can show otherwise.",
		"In that case, use April 15, 2026",
		"Penalty compounds annually on January 1 of each year.",
		"The penalty cannot be more than 10% of the underpayment.",
		"Do not round penalty amounts calculated in Part B, lines 18, 21, 24, and 27 to the nearest whole dollar.",
		"You must round the total penalty amount (box 30c) to the nearest whole dollar.",
		"If you use the Annualized Income Installment Worksheet for any payment due date, you must use it for all payment due dates.",
		"The underpayment was due to a casualty, disaster, or other unusual circumstances, and it would be inequitable to impose the penalty.",
		"you retired after age 62 or became disabled, and your underpayment was due to reasonable cause.",
	)
	mustContain(t, "az-221-2025-days-chart.txt",
		"Rate Period (a) (b) (c) (d)",
		"1 (line 17) 76 15",
		"2 (line 20) 92 92 15",
		"3 (line 23) 92 92 92",
		"4 (line 26) 105 105 105 90",
	)
}

// The computed dates are the ones the 2026 Form 140ES prints. None of them
// falls on a weekend or a holiday, so none moves. The file-and-pay date does:
// January 31, 2027 is a Sunday, and the 140ES prints Monday, February 1.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
	}
	if s := applied(res.Applied()); strings.Contains(s, "falls on a weekend") {
		t.Errorf("a 2026 date moved: %s", s)
	}
	if wd := day(2027, 1, 31).Weekday(); wd != time.Sunday {
		t.Fatalf("January 31, 2027 is a %v", wd)
	}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 2, 1), true}, {day(2027, 2, 2), false}} {
		res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: tc.filed}.facts(t)))
		if got := res.Value()[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
	}
}

// § 43-581(A): Arizona gross income must exceed $75,000 ($150,000 joint)
// this year and have been greater than it last year. A cent either side of
// each, for each filing status.
func TestEstimatedIncomeThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		status       filing.Status
		this, before money.USCents
		exempt       bool
	}{
		{filing.Single, 7_500_000, 20_000_000, true},
		{filing.Single, 7_500_001, 20_000_000, false},
		{filing.Single, 20_000_000, 7_500_000, true},
		{filing.Single, 20_000_000, 7_500_001, false},
		{filing.MarriedFilingJointly, 15_000_000, 20_000_000, true},
		{filing.MarriedFilingJointly, 15_000_001, 20_000_000, false},
		{filing.MarriedFilingJointly, 20_000_000, 15_000_000, true},
		{filing.MarriedFilingJointly, 20_000_000, 15_000_001, false},
		{filing.MarriedFilingJointly, 10_000_000, 10_000_000, true},
		{filing.MarriedFilingSeparately, 7_500_000, 20_000_000, true},
		{filing.MarriedFilingSeparately, 7_500_001, 7_500_001, false},
		{filing.HeadOfHousehold, 7_500_000, 20_000_000, true},
		{filing.HeadOfHousehold, 7_500_001, 7_500_001, false},
	} {
		h := household{status: tc.status, tax: 10_000 * money.Dollar, gross: tc.this, prior: prior(9_000*money.Dollar, tc.before)}
		r := must(rules(t).Requirement(h.facts(t))).Value()
		if r.Exempt() != tc.exempt || r.Required() == tc.exempt {
			t.Errorf("%v with %v this year and %v last: exempt %v, required %v; want exempt %v (%s)", tc.status, tc.this, tc.before, r.Exempt(), r.Required(), tc.exempt, r.Why())
		}
		if tc.exempt {
			if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
				t.Errorf("%v: a penalty of %v below the income threshold", tc.status, p.Value().Total())
			}
		}
	}
}

// § 43-581(E)(2): no penalty when tax less withholding and credits is under
// $1,000. Form 221 line 1: none either when federal tax less federal
// withholding is under $1,000.
func TestEstimatedTaxThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{{900_001, true}, {900_000, false}} { // $999.99 and $1,000.00 left of $10,000
		r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld}.facts(t))).Value()
		if r.Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, r.Exempt(), tc.exempt, r.Why())
		}
		if c, _ := r.Chosen(); tc.exempt && (c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax after credits less withholding under $1,000") {
			t.Errorf("withheld %v: chosen %v %q at %v", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
	}
	for _, tc := range []struct {
		federal money.USCents
		exempt  bool
	}{{99_999, true}, {100_000, false}, {-5_000 * money.Dollar, true}} {
		e := must(arizonataxcalc.NewEstimatedFacts().NoFederalLiabilityLastYear(false).FederalTaxLessWithholding(tc.federal).
			PriorYearReturnRequired(true).Build())
		h := household{tax: 10_000 * money.Dollar, extras: &e}
		r := must(rules(t).Requirement(h.facts(t))).Value()
		if r.Exempt() != tc.exempt {
			t.Errorf("federal tax less withholding %v: exempt %v, want %v (%s)", tc.federal, r.Exempt(), tc.exempt, r.Why())
		}
		if tc.exempt {
			if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
				t.Errorf("federal tax less withholding %v: a penalty of %v", tc.federal, p.Value().Total())
			}
		}
	}
}

// The target is the smaller of 90% of this year's tax and 100% of last
// year's, with no 110% tier at any income.
func TestEstimatedTargetOptions(t *testing.T) {
	// 90% of $10,000 is $9,000, under last year's $9,500.
	r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(9_500*money.Dollar, 200_000*money.Dollar)}.facts(t))).Value()
	if c, _ := r.Chosen(); c.Basis() != estimated.BasisThisYearTax || r.Target() != 9_000*money.Dollar {
		t.Errorf("chose %v at %v, want this year's tax at $9,000", c.Basis(), r.Target())
	}
	// Last year's $8,000 at 100%, even after $5,000,000 of income.
	rich := prior(8_000*money.Dollar, 5_000_000*money.Dollar)
	r = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, gross: 5_000_000 * money.Dollar, prior: rich}.facts(t))).Value()
	c, _ := r.Chosen()
	if c.Basis() != estimated.BasisPriorYearTax || c.Percent() != money.Percent(100) || r.Target() != 8_000*money.Dollar {
		t.Errorf("chose %v at %v of %v, want last year's tax at 100%%, $8,000", c.Basis(), c.Percent(), r.Target())
	}
	if !r.Required() || !strings.Contains(r.Why(), "$8,000 of estimated payments") {
		t.Errorf("required %v: %s", r.Required(), r.Why())
	}
}

// The prior-year option needs a filed 2025 return that, under the 2026 Form
// 140ES, the filer was required to file. A short year does not take it away,
// but is asked about.
func TestEstimatedPriorYearOptionAvailability(t *testing.T) {
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	voluntary := must(arizonataxcalc.NewEstimatedFacts().NoFederalLiabilityLastYear(false).
		FederalTaxLessWithholding(50_000 * money.Dollar).PriorYearReturnRequired(false).Build())
	for name, tc := range map[string]struct {
		h   household
		ask working.PositionKind
	}{
		"no return on record": {household{noPrior: true}, working.PositionPriorYearReturn},
		"not required":        {household{prior: &notRequired}, working.PositionUnspecified},
		"not filed":           {household{prior: &notFiled}, working.PositionUnspecified},
		"filed voluntarily":   {household{extras: &voluntary}, working.PositionUnspecified},
	} {
		tc.h.tax = 10_000 * money.Dollar
		res := must(rules(t).Requirement(tc.h.facts(t)))
		if o := res.Value().Options()[1]; o.Available() || o.WhyNot() == "" {
			t.Errorf("%s: the prior-year option is available %v (%q)", name, o.Available(), o.WhyNot())
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
	}

	short := must(estimated.NewPriorReturn().Filed(true).Months(6).
		Figures(must(estimated.NewFigures().Tax(4_000 * money.Dollar).GrossIncome(100_000 * money.Dollar).Build())).Build())
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &short}.facts(t)))
	if o := res.Value().Options()[1]; !o.Available() || res.Value().Target() != 4_000*money.Dollar {
		t.Errorf("a 6-month 2025 return: available %v, target %v; want offered at $4,000", o.Available(), res.Value().Target())
	}
	if !strings.Contains(because(res.Required(), working.PositionUnsettledRule), "6 months") {
		t.Errorf("the short year is not asked about: %v", res.Required())
	}

	// Whether the return was required is asked only when it decides the
	// target.
	unanswered := must(arizonataxcalc.NewEstimatedFacts().NoFederalLiabilityLastYear(false).FederalTaxLessWithholding(50_000 * money.Dollar).Build())
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(4_000*money.Dollar, 100_000*money.Dollar), extras: &unanswered}.facts(t)))
	if !strings.Contains(because(res.Required(), working.PositionJurisdictionFact), "Were you required to file your 2025 Arizona return?") {
		t.Errorf("whether the 2025 return was required is not asked: %v", res.Required())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &unanswered}.facts(t)))
	if strings.Contains(because(res.Required(), working.PositionJurisdictionFact), "Were you required to file your 2025 Arizona return?") {
		t.Error("asked whether the 2025 return was required when this year's tax set the target")
	}
}

// April's $10,000 shortfall spans the rate change on July 1. 90% of
// $44,444.44 is $40,000, a quarter of which is $10,000. Nothing is paid until
// $10,000 on July 31, which pays April, and $30,000 on September 15, which
// pays June and covers September and January.
//
//	April, Apr 15 to Jun 30, 76 days at 6%: 10,000 × 76 / 365 × 6% = $124.93
//	April, Jun 30 to Jul 31, 31 days at 7%: 10,000 × 31 / 365 × 7% =  $59.45
//	June,  Jun 15 to Jun 30, 15 days at 6%: 10,000 × 15 / 365 × 6% =  $24.66
//	June,  Jun 30 to Sep 15, 77 days at 7%: 10,000 × 77 / 365 × 7% = $147.67
//
// $356.71 in all, which box 30c rounds to $357. Each line is under its cap of
// 10% of $10,000, $1,000.
func TestEstimatedPenaltyAcrossTheRateChange(t *testing.T) {
	h := household{tax: 4_444_444, payments: []estimated.Payment{pay(day(2026, 7, 31), 10_000*money.Dollar), pay(dues[2], 30_000*money.Dollar)}}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	lines := res.Value().Lines()
	if len(lines) != 2 {
		t.Fatalf("%d lines, want April and June", len(lines))
	}
	want := [][]struct {
		days   int
		rate   money.Rate
		charge money.USCents
	}{
		{{76, money.Percent(6), 12_493}, {31, money.Percent(7), 5_945}},
		{{15, money.Percent(6), 2_466}, {77, money.Percent(7), 14_767}},
	}
	for i, w := range want {
		parts := lines[i].Parts()
		if len(parts) != len(w) {
			t.Errorf("line %d has %d parts, want %d", i+1, len(parts), len(w))
			continue
		}
		for j, p := range parts {
			if p.Days() != w[j].days || p.Rate() != w[j].rate || p.Charge() != w[j].charge || p.Principal() != 10_000*money.Dollar {
				t.Errorf("line %d part %d: %d days at %v on %v is %v; want %d days at %v, %v", i+1, j+1, p.Days(), p.Rate(), p.Principal(), p.Charge(), w[j].days, w[j].rate, w[j].charge)
			}
		}
	}
	if res.Value().Total() != 35_671 {
		t.Errorf("penalty %v, want $356.71", res.Value().Total())
	}
	s := applied(res.Applied())
	for _, phrase := range []string{"to the nearest whole dollar, $357.00", "under the cap of 10% of it, $1000.00"} {
		if !strings.Contains(s, phrase) {
			t.Errorf("the working does not say %q: %s", phrase, s)
		}
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("an unpublished rate reported when every shortfall was paid in 2026")
	}
}

// Shortfalls never paid run through every rate period, as Form 221's Chart of
// Total Days per Period counts them: column (a) 76, 92, 92 and 105; (b) 15,
// 92, 92 and 105; (c) 15, 92 and 105; (d) 90. The chart's 15 days in column
// (b) are June 15 to June 30, which fits 2026's dates; the 2025 form's own
// June 16 would give 14. The 2027 rate is not published, so the fourth rate
// period's 105 and 90 days are counted to the April 15 stop but not charged.
//
// On January 1, 2027 April's penalty so far is added to its principal: 10,000
// × 76 / 365 × 6% = $124.93, and 10,000 × 92 / 365 × 7% = $176.44 twice, so
// $477.81.
func TestEstimatedDaysMatchTheForm221Chart(t *testing.T) {
	h := household{tax: 4_444_444}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 6, 30)))
	if res.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want the April 15, 2027 stop", res.Value().AsOf())
	}
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	chart := [][]int{{76, 92, 92}, {15, 92, 92}, {15, 92}, nil}
	fourth := []int{105, 105, 105, 90}
	for i, want := range chart {
		parts := lines[i].Parts()
		if len(parts) != len(want) {
			t.Errorf("line %d has %d parts, want %d", i+1, len(parts), len(want))
			continue
		}
		for j, p := range parts {
			if p.Days() != want[j] {
				t.Errorf("line %d rate period %d: %d days, want %d", i+1, j+1, p.Days(), want[j])
			}
		}
		from := day(2026, 12, 31)
		if i == 3 {
			from = dues[3]
		}
		if got := res.Value().AsOf().DaysSince(from); got != fourth[i] {
			t.Errorf("line %d: %d days in the fourth rate period, the chart prints %d", i+1, got, fourth[i])
		}
	}
	if lines[3].Days() != 90 || lines[0].Days() != 365 {
		t.Errorf("lines run %d and %d days, want 365 and 90", lines[0].Days(), lines[3].Days())
	}
	if !strings.Contains(because(res.Required(), working.PositionUnpublishedRate), "Dec 31, 2026") {
		t.Errorf("the unpublished 2027 rate is not reported: %v", res.Required())
	}
	if s := applied(res.Applied()); !strings.Contains(s, "the $477.81 of penalty on the April 15, 2026 installment outstanding on January 1, 2027 is added to its principal") {
		t.Errorf("April's penalty is not compounded on January 1: %s", s)
	}
}

// The exceptions that leave nothing to pay: a federal exception, withholding
// that reaches the target, and filing and paying by February 1.
func TestEstimatedExceptions(t *testing.T) {
	none := must(arizonataxcalc.NewEstimatedFacts().NoFederalLiabilityLastYear(true).Build())
	h := household{tax: 10_000 * money.Dollar, extras: &none}
	r := must(rules(t).Requirement(h.facts(t)))
	if !r.Value().Exempt() || !strings.Contains(applied(r.Applied()), "federal exception") {
		t.Errorf("no federal liability last year: exempt %v (%s)", r.Value().Exempt(), r.Value().Why())
	}
	if c, _ := r.Value().Chosen(); c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "No federal tax liability last year" {
		t.Errorf("no federal liability last year: chosen %v %q at %v", c.Basis(), c.Label(), c.Amount())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Reason() != "No federal tax liability last year." {
		t.Errorf("no federal liability last year: the penalty's reason is %q", p.Value().Reason())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v after no federal liability last year", p.Value().Total())
	}

	// 90% of $10,000 is $9,000, which $9,000 of withholding reaches.
	h = household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar}
	r = must(rules(t).Requirement(h.facts(t)))
	if r.Value().Required() || r.Value().Exempt() {
		t.Errorf("withholding at the target: required %v, exempt %v", r.Value().Required(), r.Value().Exempt())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v with withholding at the target", p.Value().Total())
	}

	// Filed and paid on February 1: nothing on January, and the penalty on
	// the rest stops that day. $2,250 a quarter on 90% of $10,000.
	h = household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 1)}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if p.Value().AsOf() != day(2027, 2, 1) || len(p.Value().Lines()) != 3 {
		t.Errorf("figured to %v over %d lines; want February 1 and three lines", p.Value().AsOf(), len(p.Value().Lines()))
	}
	if !strings.Contains(applied(p.Applied()), "fourth installment is excused") {
		t.Errorf("the January installment is not excused: %s", applied(p.Applied()))
	}
}

// A penalty is shown before any waiver, and the waiver is asked about.
func TestEstimatedWaiverIsAsked(t *testing.T) {
	p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 4, 15)))
	if p.Value().Total() == 0 || !has(p.Required(), working.PositionPenaltyWaiver) {
		t.Errorf("penalty %v, waiver asked %v", p.Value().Total(), has(p.Required(), working.PositionPenaltyWaiver))
	}
	p = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, waiver: true}.facts(t), day(2027, 4, 15)))
	if has(p.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is asked after it was answered")
	}
}

// Form 221's annualised worksheet, lines 15 to 23, on a target of $40,000
// ($10,000 a quarter) and annualised tax of $8,000, $20,000, $30,000 and
// $40,000:
//
//	(a) 22.5% of $8,000 is $1,800; regular $10,000; $1,800, $8,200 carried
//	(b) 45% of $20,000 less $1,800 is $7,200; regular $18,200; $7,200
//	(c) 67.5% of $30,000 less $9,000 is $11,250; regular $21,000; $11,250
//	(d) 90% of $40,000 less $20,250 is $15,750; regular $19,750; $15,750
func TestEstimatedAnnualisedWorksheet(t *testing.T) {
	h := household{tax: 4_444_444, annualised: []money.USCents{8_000 * money.Dollar, 20_000 * money.Dollar, 30_000 * money.Dollar, 40_000 * money.Dollar}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{1_800 * money.Dollar, 9_000 * money.Dollar, 20_250 * money.Dollar, 36_000 * money.Dollar} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v by its date, want %v", i+1, s[i].NeededBy(), want)
		}
	}
	bad := h.facts(t)
	wrong := must(estimated.NewFacts(az, 2026).FilingStatus(filing.Single).ThisYear(bad.ThisYear()).
		Elections(must(estimated.NewElections().Annualised().Build())).
		Periods(must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 6, 30)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).Build())
	if _, err := rules(t).Schedule(wrong); err == nil {
		t.Error("a period ending June 30 was accepted")
	}
}

// Withholding counts a quarter on each date, unless the filer shows when it
// was withheld. Last year's $4,000 is the target, $1,000 a quarter. $4,000
// withheld on December 15 meets every quarter spread evenly; counted on its
// date, it meets only January's.
func TestEstimatedWithholdingCredit(t *testing.T) {
	h := household{tax: 20_000 * money.Dollar, withheld: 4_000 * money.Dollar, prior: prior(4_000*money.Dollar, 200_000*money.Dollar)}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, in := range s {
		if in.Status() != estimated.StatusMet {
			t.Errorf("spread: installment %d is %v", i+1, in.Status())
		}
	}
	h.asWithheld = true
	s = must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []estimated.Status{estimated.StatusShort, estimated.StatusShort, estimated.StatusShort, estimated.StatusMet} {
		if s[i].Status() != want {
			t.Errorf("as withheld: installment %d is %v, want %v", i+1, s[i].Status(), want)
		}
	}
}

// Arizona gross income comes from GrossIncome; with only federal AGI given it
// is taken from that and asked about, and with neither it is taken as over
// the threshold and asked about.
func TestEstimatedGrossIncomeWhenNotGiven(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, noGross: true, agi: 70_000 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() || !strings.Contains(applied(res.Applied()), "federal AGI, taken as Arizona gross income") {
		t.Errorf("$70,000 of federal AGI: exempt %v (%s)", res.Value().Exempt(), applied(res.Applied()))
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, noGross: true, agi: 90_000 * money.Dollar}.facts(t)))
	if res.Value().Exempt() || !strings.Contains(because(res.Required(), working.PositionAdjustedGrossIncome), "your Arizona gross income if you lived in Arizona all year") {
		t.Errorf("$90,000 of federal AGI: exempt %v, asked %v", res.Value().Exempt(), res.Required())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, noGross: true}.facts(t)))
	if !res.Value().Required() || !strings.Contains(because(res.Required(), working.PositionAdjustedGrossIncome), "over $75,000 in both 2025 and 2026") {
		t.Errorf("no income given: required %v, asked %v", res.Value().Required(), res.Required())
	}
	// Nothing Arizona-specific said: both federal exceptions are asked about.
	e := must(arizonataxcalc.NewEstimatedFacts().Build())
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &e}.facts(t)))
	n := 0
	for _, r := range res.Required() {
		if r.Position() == working.PositionJurisdictionFact {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d Arizona facts asked for, want the two federal exceptions: %v", n, res.Required())
	}
}

// Each divergence is reported with both readings, both sources, the reading
// the figures follow, and whether it changes these facts.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string]struct {
		form, statuteSource, statute string
		uses                         working.Reading
		affects                      bool
	}{
		// Nothing paid: every shortfall accrues in 2026 and runs past April 15, 2027.
		"Arizona gross income of exactly the threshold": {"only below the threshold", "§ 43-581(A)", "exactly $75,000 isn't subject", working.ReadingStatute, false},
		"The prior-year option needs a filed return":    {"required to file a 2025 Arizona return, and filed it", "§ 43-581(A), (D)", "no condition that a return was required, filed or covered 12 months", working.ReadingForm, false},
		"When the penalty stops":                        {"April 15 of the next year", "§ 42-1125(Q)", "can't exceed 10% of that amount", working.ReadingForm, true},
		"The 2026 rates":                                {"prints 7%", "§ 42-1123(A)", "federal short-term rate", working.ReadingStatute, true},
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
		if !strings.Contains(d.Form(), w.form) || !strings.Contains(d.StatuteSource(), w.statuteSource) || d.FormSource() == "" || !strings.Contains(d.Statute(), w.statute) {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s)", d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
		}
		if d.Uses() != w.uses || d.AffectsHousehold() != w.affects {
			t.Errorf("%q uses %v and affects these facts %v; want %v and %v", d.Rule(), d.Uses(), d.AffectsHousehold(), w.uses, w.affects)
		}
	}
	affects := func(h household, rule string) bool {
		t.Helper()
		for _, d := range must(rules(t).Requirement(h.facts(t))).Divergences() {
			if d.Rule() == rule {
				return d.AffectsHousehold()
			}
		}
		t.Fatalf("no divergence %q", rule)
		return false
	}
	if !affects(household{tax: 10_000 * money.Dollar, gross: 75_000 * money.Dollar}, "Arizona gross income of exactly the threshold") {
		t.Error("the boundary does not affect exactly $75,000")
	}
	if affects(household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar}, "When the penalty stops") {
		t.Error("the April 15 stop affects a household with no shortfall")
	}
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	if !affects(household{tax: 10_000 * money.Dollar, prior: &notFiled}, "The prior-year option needs a filed return") {
		t.Error("the filed-return condition does not affect a household that did not file")
	}
	// The figure follows the 140ES and the statute at the boundary, not Form
	// 221's "less than".
	r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, gross: 75_000 * money.Dollar}.facts(t))).Value()
	if !r.Exempt() {
		t.Error("exactly $75,000 is subject, as Form 221's instructions read")
	}
	// The penalty follows the form's April 15 stop, not the period of
	// nonpayment.
	p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 12, 31)))
	if p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, year := range []int{2025, 2027} {
		if _, err := arizonataxcalc.EstimatedTax(year); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", year, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"a farmer":    must(estimated.NewFacts(az, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(7000).Build()),
		"a fisherman": must(estimated.NewFacts(az, 2026).FilingStatus(filing.Single).ThisYear(this).FishShare(7000).Build()),
		"a nonresident alien": must(estimated.NewFacts(az, 2026).FilingStatus(filing.Single).ThisYear(this).
			State(must(arizonataxcalc.NewEstimatedFacts().NonresidentAlien().Build())).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(az, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(az, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Arizona's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := rules(t).Penalty(f, day(2027, 4, 15)); err == nil {
			t.Errorf("%s: penalty accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty figured to no date")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$75,000", "$150,000", "Arizona gross income", "$1,000", "no 110%", "10% of the shortfall", "January 1", "federal exception", "required to file", "February 1, 2027", "6%"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if e.VersusFederal() == "" || len(e.Sources()) == 0 {
		t.Error("the explanation has no comparison with federal or no sources")
	}
}

// Without this year's tax, the target is 100% of last year's, the penalty
// against it is an upper bound, and with no 2025 return there is no target.
// An exception that needs nothing about this year's tax still gives an exact
// zero.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000*money.Dollar, 200_000*money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 8_000*money.Dollar || !r.TargetKnown() {
		t.Errorf("required %v, target %v; want $8,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if c, ok := r.Chosen(); !ok || c.Basis() != estimated.BasisPriorYearTax {
		t.Errorf("chose %v", c.Basis())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{2_000 * money.Dollar, 4_000 * money.Dollar, 6_000 * money.Dollar, 8_000 * money.Dollar} {
		if got := sched.Value()[i].NeededBy(); got != want {
			t.Errorf("installment %d needs %v, want %v", i+1, got, want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if pen.Value().Total() == 0 || !(pen.Value().UpperBound() || !pen.Value().RatedThrough().IsZero()) {
		t.Errorf("unpaid: penalty %v, upper bound %v", pen.Value().Total(), pen.Value().UpperBound())
	}

	none := household{noTax: true, noPrior: true}
	res = must(rules(t).Requirement(none.facts(t)))
	if res.Value().TargetKnown() || res.Value().Required() {
		t.Errorf("no 2025 return: target known %v, required %v", res.Value().TargetKnown(), res.Value().Required())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}

	exact := []household{
		{noTax: true, prior: prior(8_000*money.Dollar, 60_000*money.Dollar)},
		{noTax: true, extras: func() *arizonataxcalc.EstimatedFacts {
			e := must(arizonataxcalc.NewEstimatedFacts().NoFederalLiabilityLastYear(true).Build())
			return &e
		}()},
	}
	for i, h := range exact {
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if p.Value().Total() != 0 || p.Value().UpperBound() {
			t.Errorf("case %d: penalty %v, upper bound %v; want an exact zero", i+1, p.Value().Total(), p.Value().UpperBound())
		}
	}
}

// The waivers are decided from the conditions the Form 221 instructions states: a
// casualty or disaster, or retiring after 62 or becoming disabled in the tax
// year or the year before. The penalty is still figured in full.
func TestEstimatedWaiverIsDecidedFromItsConditions(t *testing.T) {
	mustContain(t, "az-221-2025-instructions.txt", "In 2024 or 2025, you retired after age 62 or became disabled")
	base := household{tax: 10_000 * money.Dollar}
	before := must(rules(t).Penalty(base.facts(t), day(2027, 4, 15)))
	if before.Value().Total() == 0 {
		t.Fatal("the household has no penalty to waive")
	}
	for _, c := range []struct {
		name      string
		positions estimated.Positions
		eligible  bool
		refused   bool
		reason    string
	}{
		{"retired in 2026 at 64", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build()), true, false, "after reaching 62"},
		{"retired in 2025, the year before", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2025, 6, 1)).Born(day(1960, 3, 1)).Build()), true, false, "You retired in 2025"},
		{"retired in 2024", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2024, 6, 1)).Born(day(1955, 3, 1)).Build()), false, true, "2025 or 2026"},
		{"retired in 2020", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, 3, 1)).Build()), false, true, "You retired in 2020"},
		{"retired at 61", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1965, 3, 1)).Build()), false, true, "You were 61"},
		{"disabled in 2026", must(estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2026, 2, 1)).Build()), true, false, "became disabled in 2026"},
		{"a disaster", must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()), true, false, "a disaster"},
	} {
		h := base
		h.positions = &c.positions
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		o, ok := p.Value().WaiverOutcome()
		switch {
		case !ok:
			t.Errorf("%s: the penalty has no waiver outcome", c.name)
		case o.Eligible() != c.eligible || o.Refused() != c.refused:
			t.Errorf("%s: eligible %v, refused %v: %q", c.name, o.Eligible(), o.Refused(), o.Reason())
		case !strings.Contains(o.Reason(), c.reason):
			t.Errorf("%s: the reason %q does not say %q", c.name, o.Reason(), c.reason)
		case c.eligible && !strings.Contains(strings.ToLower(o.Reason()), "the arizona department of revenue"):
			t.Errorf("%s: the reason %q does not name %s", c.name, o.Reason(), "the arizona department of revenue")
		}
		if p.Value().Total() != before.Value().Total() {
			t.Errorf("%s: the penalty is %v, and %v before any waiver", c.name, p.Value().Total(), before.Value().Total())
		}
		want := p.Value().Total()
		if c.eligible {
			want = 0
		}
		if p.Value().IfWaived() != want {
			t.Errorf("%s: if waived the penalty is %v, want %v", c.name, p.Value().IfWaived(), want)
		}
		if has(p.Required(), working.PositionPenaltyWaiver) {
			t.Errorf("%s: the waiver is still asked after it was answered", c.name)
		}
	}
}

// An exception sets a $0 target, so a question whose answer would only
// change the target is not asked under one.
func TestEstimatedExceptionAsksNothingAboutTheTarget(t *testing.T) {
	for _, c := range []struct {
		name   string
		h      household
		exempt bool
	}{
		{"tax under the threshold, no 2025 return", household{tax: 400 * money.Dollar, noPrior: true}, true},
		{"tax over the threshold, no 2025 return", household{tax: 10_000 * money.Dollar, noPrior: true}, false},
	} {
		res := must(rules(t).Requirement(c.h.facts(t)))
		if res.Value().Exempt() != c.exempt {
			t.Fatalf("%s: exempt is %v, want %v", c.name, res.Value().Exempt(), c.exempt)
		}
		if has(res.Required(), working.PositionPriorYearReturn) == c.exempt {
			t.Errorf("%s: asked about %v is %v", c.name, working.PositionPriorYearReturn, !c.exempt)
		}
		for _, q := range res.Required() {
			if c.exempt && strings.Contains(strings.ToLower(q.Effect()), "target") {
				t.Errorf("%s: an exception sets the target, yet a question says %q", c.name, q.Effect())
			}
		}
	}
}
