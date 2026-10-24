// Iowa's estimated tax rules for 2026, checked against the Department of
// Revenue's documents and the Iowa Code.
//
// Iowa publishes no exhaustive oracle for estimated tax, and the IA 2210
// carries no worked example with a penalty figure. The strongest check is the
// worked example in the 1040 expanded instructions for line 35, which states
// how each payment is applied and how many days each piece is charged: the
// test reproduces its application and its day counts. Every threshold,
// percentage, date and rate is compared against its extract in testdata/.
// The boundaries, the exceptions and the late start are hand-written cases
// following the IA 2210's line arithmetic.
package iowataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/iowataxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

var day = estimated.MustDate

const iowa = estimated.Jurisdiction("IA")

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
	return must(iowataxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

func applied(rs []working.Rule, s string) bool {
	for _, r := range rs {
		if strings.Contains(r.Applied(), s) {
			return true
		}
	}
	return false
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

// dues are the 2026 dates after the weekend rule.
var dues = []estimated.Date{day(2026, 4, 30), day(2026, 6, 30), day(2026, 9, 30), day(2027, 2, 1)}

type household struct {
	status      filing.Status
	tax         money.USCents // IA 2210 line 3
	noTax       bool
	withheld    money.USCents
	asWithheld  bool
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *iowataxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	status := h.status
	if status == filing.Unspecified {
		status = filing.Single
	}
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts(iowa, 2026).FilingStatus(status).ThisYear(figures).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
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
		b.State(must(iowataxcalc.NewEstimatedFacts().PriorYearModifications(0).Build()))
	}
	if h.waiver != nil {
		b.Positions(must(estimated.NewPositions().Waiver(*h.waiver).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

// prior is a filed twelve-month 2025 return with its tax and federal AGI.
func prior(tax, agi money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).
		Figures(must(estimated.NewFigures().Tax(tax).FederalAGI(agi).Build())).Build())
	return &p
}

func mods(a money.USCents) *iowataxcalc.EstimatedFacts {
	e := must(iowataxcalc.NewEstimatedFacts().PriorYearModifications(a).Build())
	return &e
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "ia-1040es-2026.txt",
		"married couple filing a joint Iowa return that expects to have a tax liability of $1,000 or more from income not subject to withholding.",
		"If a due date ends on a weekend or holiday as defined in Iowa Code section 421.9A, substitute the next regular business day for the due date.",
		"Pay 110% of the prior year’s Iowa tax liability from IA 1040, line 18, less applicable credits",
		"Estimated tax payments are not required if line 15 is less than $1,000. If line 15 equals or exceeds $1,000, continue to line 16",
		"Multiply line 16 by 25% (.25). This is your quarterly estimated payment amount.",
		"Installment 1 04/30/2026 Installment 2 06/30/2026 Installment 3 09/30/2026 Installment 4 01/31/2027",
		"If two-thirds or more of estimated gross income is from farming or commercial fishing",
		"45-009a (08/29/2025)",
	)
	mustContain(t, "ia-2210-2025.txt",
		"Subtract line 2 from line 1. If less than $200, stop.",
		"Multiply line 3 by 90% (.90).",
		"Subtract line 5 from line 3. If less than $200, stop. You do not owe a penalty.",
		"If 2024 federal AGI plus Iowa net modifications reported on IA 1040, line 3 exceeds $150,000 ($75,000 if married filing separate), enter 110% (1.10) of your 2024 Iowa tax.",
		"Enter the smaller amount of line 4 or line 7.",
		"If less than or equal to line 5, stop. You do not owe IA 2210 penalty.",
		"45-007a (10/22/2025)",
		"Your 2024 return must have covered (or would have, had you been required to file) a period of 12 months.",
		"Between January 1 and April 1 Between April 2 and June 1 Between June 2 and September 1 After September 1 Due Date of Installments Installment Required April 30 4 June 30 3 September 30 2 January 31 1",
		"Divide the amount on line 8 by the number of installments required for the years.",
		"You may consider an equal part of the Iowa income tax withheld during the year as paid on each required installment date, unless you can establish the dates on which the withholding occurred and consider the tax paid on those dates.",
		"Your estimated payments are applied to the earliest unpaid installment.",
		"Number of days from 01/01/2026 or the due date of installment, whichever is later, through and including the date of payment or 04/30/2026, whichever is earlier.",
		"0.027397% (.00027397) per day on the amount shown on line 21",
		"Line 24b: 10% per annum = 0.027397% per day (01/01/26 - 12/31/26)",
		"may be waived if the underpayment was due to casualty, disaster, or other unusual circumstances.",
		"retired at age 62 or later, or became disabled in the tax year for which the estimated payments were required",
		"gross income from farming or fishing is at least two-thirds of your annual gross income",
	)
	mustContain(t, "iowa-code-2026-422.16-12.txt",
		"if their Iowa income tax liability can reasonably be expected to amount to one thousand dollars or more for the year.",
		"The estimated tax shall be paid in quarterly installments. The first installment shall",
		"be paid on or before the last day of the fourth month of the taxpayer’s tax year",
		"the last day of the ninth month of the tax year, and the last day of the first month after the tax year.",
		"is due and payable on or before the end of the fourth month following the close of the tax year.",
		"the amount to be added to the tax for underpayment of estimated tax is an amount determined at the rate in effect under section 421.7.",
		"2025 amendment to subsection 12, paragraph a, subparagraph (1) applies to tax years beginning on or after January 1, 2026; 2025 Acts, ch 148, §60",
		"2025 amendment to subsection 12, paragraph a, subparagraph (1) effective January 1, 2026; 2025 Acts, ch 148, §59",
	)
	mustContain(t, "iowa-code-2026-421.9A.txt",
		"falls on a Saturday, Sunday, or any holiday, the act is considered to be performed timely if the act is performed on or before the first business day following the Saturday, Sunday, or holiday.")
	mustContain(t, "iowa-code-2026-421.7.txt",
		"two percentage points greater than the numerical average, rounded to the nearest one percent")
	mustContain(t, "iac-701-308.6.txt",
		"the requirement that the return for the prior tax year must have had a tax liability is eliminated although the return must have covered a 12-month period.",
		"or who became disabled in the tax year for which the estimated payment was due or in the preceding tax year")
	mustContain(t, "idr-2026-interest-rate.txt",
		"Starting January 1, 2026, the interest rate for taxpayers with overdue payments will be: 10.0% annually 0.8% monthly 0.027397% daily",
		"The daily rate for 2026 is the annual rate divided by 365.")
	mustContain(t, "idr-estimated-payments.txt",
		"If a taxpayer has a tax liability of $200 ($1,000 for tax years beginning on or after January 1, 2026) or more from income that is not subject to withholding, a penalty for underpayment of estimated tax may apply.",
		"or who became disabled in the tax year of which the estimated tax was due or in the preceding tax year")
	mustContain(t, "idr-1040-line-35.txt",
		"Between April 2 and June 1 | June 30 | 3 |",
		"Between June 2 and September 1 | September 30 | 2 |",
		"After September 1 | January 31 | 1 |",
		"The IA 2210 penalty is calculated as a per day interest charge",
		"Iowa tax withholding is credited equally in each installment period.")
}

// The first three dates are the ones the 2026 IA 1040ES prints, and fall on
// weekdays. It prints the fourth as 01/31/2027, a Sunday; its own weekend
// rule and Iowa Code § 421.9A move it to Monday, February 1, 2027.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	if day(2027, 1, 31).Weekday() != time.Sunday {
		t.Fatal("January 31, 2027 is not a Sunday")
	}
	res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
	}
	if !applied(res.Applied(), "January 31, 2027 falls on a weekend or holiday, so it is due February 1, 2027") {
		t.Errorf("the move is not explained: %v", res.Applied())
	}
}

// § 422.16(12)(a)(1): $1,000 or more after withholding requires estimates,
// and under it nothing is required and no penalty arises.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{400_001, true},  // $999.99 left
		{400_000, false}, // $1,000.00 left, short of 90% of $5,000
	} {
		h := household{tax: 5_000 * money.Dollar, withheld: tc.withheld}
		r := must(rules(t).Requirement(h.facts(t))).Value()
		if r.Exempt() != tc.exempt || r.Required() == tc.exempt {
			t.Errorf("withheld %v: exempt %v, required %v; want exempt %v (%s)", tc.withheld, r.Exempt(), r.Required(), tc.exempt, r.Why())
		}
		if c, ok := r.Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding under $1,000") {
			t.Errorf("withheld %v: chose %v %q at %v, want the exception", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30))).Value()
		if (p.Total() == 0) != tc.exempt {
			t.Errorf("withheld %v: penalty %v", tc.withheld, p.Total())
		}
	}
}

// IA 2210 line 7: 110% of last year's tax when last year's federal AGI plus
// Iowa net modifications exceeds $150,000, or $75,000 married filing
// separately; 100% at exactly the step.
func TestEstimatedPriorYearStepBoundaries(t *testing.T) {
	for _, tc := range []struct {
		status filing.Status
		agi    money.USCents
		mods   money.USCents
		want   money.USCents
	}{
		{filing.Single, 140_000 * money.Dollar, 10_000 * money.Dollar, 10_000 * money.Dollar},
		{filing.Single, 140_000 * money.Dollar, 1_000_001, 11_000 * money.Dollar},
		{filing.Single, 160_000 * money.Dollar, -10_000 * money.Dollar, 10_000 * money.Dollar},
		{filing.MarriedFilingJointly, 150_000 * money.Dollar, 0, 10_000 * money.Dollar},
		{filing.MarriedFilingSeparately, 75_000 * money.Dollar, 0, 10_000 * money.Dollar},
		{filing.MarriedFilingSeparately, 7_500_001, 0, 11_000 * money.Dollar},
	} {
		h := household{status: tc.status, tax: 20_000 * money.Dollar, prior: prior(10_000*money.Dollar, tc.agi), extras: mods(tc.mods)}
		r := must(rules(t).Requirement(h.facts(t))).Value()
		if r.Target() != tc.want {
			t.Errorf("%v, AGI %v, modifications %v: target %v, want %v", tc.status, tc.agi, tc.mods, r.Target(), tc.want)
		}
	}
}

// The target is the smaller of the two tests. The prior-year test needs a
// filed twelve-month return; an unknown AGI or modification assumes 110%.
func TestEstimatedTargetOptions(t *testing.T) {
	r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(12_000*money.Dollar, 100_000*money.Dollar)}.facts(t))).Value()
	if c, _ := r.Chosen(); c.Basis() != estimated.BasisThisYearTax || r.Target() != 9_000*money.Dollar {
		t.Errorf("chose %v at %v, want 90%% of this year's tax, $9,000", c.Basis(), r.Target())
	}
	r = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(8_000*money.Dollar, 100_000*money.Dollar)}.facts(t))).Value()
	if c, _ := r.Chosen(); c.Basis() != estimated.BasisPriorYearTax || r.Target() != 8_000*money.Dollar {
		t.Errorf("chose %v at %v, want 100%% of last year's tax, $8,000", c.Basis(), r.Target())
	}

	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).
		Figures(must(estimated.NewFigures().Tax(5_000 * money.Dollar).FederalAGI(50_000 * money.Dollar).Build())).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   working.PositionKind
	}{
		"no return on record": {nil, working.PositionPriorYearReturn},
		"not filed":           {&notFiled, working.PositionUnspecified},
		"eleven months":       {&short, working.PositionUnspecified},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		if o := res.Value().Options()[1]; o.Available() || o.WhyNot() == "" {
			t.Errorf("%s: the prior-year test is offered", name)
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
	}

	noAGI := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(5_000 * money.Dollar).Build())).Build())
	unanswered := must(iowataxcalc.NewEstimatedFacts().Build())
	for name, tc := range map[string]struct {
		h   household
		ask working.PositionKind
	}{
		"AGI unknown":           {household{tax: 20_000 * money.Dollar, prior: &noAGI}, working.PositionAdjustedGrossIncome},
		"modifications unknown": {household{tax: 20_000 * money.Dollar, prior: prior(5_000*money.Dollar, 10_000*money.Dollar), extras: &unanswered}, working.PositionJurisdictionFact},
	} {
		res := must(rules(t).Requirement(tc.h.facts(t)))
		o := res.Value().Options()[1]
		if o.Percent() != money.Percent(110) || o.Amount() != 5_500*money.Dollar || !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v of last year, %v, asked %v; want 110%%, $5,500, and %v asked", name, o.Percent(), o.Amount(), has(res.Required(), tc.ask), tc.ask)
		}
	}
}

// A twelve-month 2025 with no Iowa tax, or with no return because none was
// required, makes the required payment nothing (IA 2210 lines 7 and 8;
// 701—308.6(1)(a)). That needs nothing about this year, so it holds without
// this year's tax and its zero is exact.
func TestEstimatedNoLiabilityLastYear(t *testing.T) {
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	for name, p := range map[string]*estimated.PriorReturn{
		"no tax":       prior(0, 20_000*money.Dollar),
		"not required": &notRequired,
	} {
		for _, noTax := range []bool{false, true} {
			h := household{tax: 50_000 * money.Dollar, noTax: noTax, prior: p}
			res := must(rules(t).Requirement(h.facts(t)))
			if !res.Value().Exempt() || res.Value().Required() {
				t.Errorf("%s (this year's tax known %v): %s", name, !noTax, res.Value().Why())
			}
			if has(res.Required(), working.PositionThisYearTax) {
				t.Errorf("%s: this year's tax is asked for though it cannot matter", name)
			}
			pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30))).Value()
			if pen.Total() != 0 || pen.UpperBound() {
				t.Errorf("%s: penalty %v, upper bound %v; want an exact zero", name, pen.Total(), pen.UpperBound())
			}
		}
	}
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).
		Figures(must(estimated.NewFigures().Tax(0).FederalAGI(0).Build())).Build())
	if r := must(rules(t).Requirement(household{tax: 50_000 * money.Dollar, prior: &short}.facts(t))).Value(); r.Exempt() {
		t.Error("an eleven-month year with no tax excuses the penalty")
	}
}

// The IA 2210 table: a requirement first met after April 1 starts at the
// next row, and IA 2210 line 17 divides the year's amount among the
// installments that remain.
func TestEstimatedLateStart(t *testing.T) {
	// 90% of $11,111.11 is $10,000.00.
	tax := money.USCents(1_111_111)
	four := []money.USCents{250_000, 500_000, 750_000, 1_000_000}
	three := []money.USCents{0, 333_333, 666_667, 1_000_000}
	two := []money.USCents{0, 0, 500_000, 1_000_000}
	one := []money.USCents{0, 0, 0, 1_000_000}
	for _, tc := range []struct {
		met  estimated.Date
		want []money.USCents
	}{
		{day(2026, 4, 1), four},
		{day(2026, 4, 2), three},
		{day(2026, 6, 1), three},
		{day(2026, 6, 2), two},
		{day(2026, 9, 1), two},
		{day(2026, 9, 2), one},
	} {
		sched := must(rules(t).Schedule(household{tax: tax, firstMet: tc.met}.facts(t))).Value()
		for i, in := range sched {
			if in.NeededBy() != tc.want[i] {
				t.Errorf("first met %v: installment %d needs %v, want %v", tc.met, i+1, in.NeededBy(), tc.want[i])
			}
			if tc.want[i] == 0 && in.Status() != estimated.StatusNothingDue {
				t.Errorf("first met %v: installment %d is %v, want nothing due", tc.met, i+1, in.Status())
			}
		}
	}
	res := must(rules(t).Requirement(household{tax: tax}.facts(t)))
	if !has(res.Required(), working.PositionRequirementFirstMet) {
		t.Error("when the requirement was first met is not asked")
	}
	res = must(rules(t).Requirement(household{tax: tax, firstMet: day(2026, 1, 1)}.facts(t)))
	if has(res.Required(), working.PositionRequirementFirstMet) {
		t.Error("when the requirement was first met is asked though it was given")
	}
}

// The 1040 expanded instructions for line 35, IA 2210 Example 2: $1,000 due
// each quarter, paid $500 on June 15, $2,000 on June 30, $500 on January 31
// and $1,000 on February 25. Its April pieces are charged 46 and 61 days, its
// September $500 92 days in the first year and 31 in the next, and its
// fourth $1,000 25 days from January 31. In 2026 the fourth installment is
// due February 1, 2027, so that piece is 24 days, and the 2027 days have no
// published rate.
func TestEstimatedPenaltyMatchesLineThirtyFiveExample(t *testing.T) {
	mustContain(t, "idr-1040-line-35.txt",
		"The taxpayer should have paid $1,000 each quarter, for a total of $4,000 for the entire year.",
		"The first payment of the year ($500) was made June 15. The second ($2,000) was two weeks later on June 30.",
		"The $500 paid June 15 has a penalty for 46 days (May 1 - June 15). The $500 paid June 30 has a penalty for 61 days (May 1 - June 30).",
		"Second installment period (due June 30): No penalty is charged for this period.",
		"Penalty on this $500 is for 92 days for the October 1 - December 31 quarter AND 31 days for January 1 - 31.",
		"The $1,000 payment made February 25, is applied to this period's underpayment.")
	h := household{tax: 10_000 * money.Dollar, prior: prior(4_000*money.Dollar, 100_000*money.Dollar), payments: []estimated.Payment{
		pay(day(2026, 6, 15), 500*money.Dollar), pay(day(2026, 6, 30), 2_000*money.Dollar),
		pay(day(2027, 1, 31), 500*money.Dollar), pay(day(2027, 2, 25), 1_000*money.Dollar),
	}}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30)))
	lines := res.Value().Lines()
	if len(lines) != 3 {
		t.Fatalf("%d lines, want April, September and February", len(lines))
	}
	type part struct {
		on     money.USCents
		days   int
		charge money.USCents
	}
	for i, want := range []struct {
		installment int
		shortfall   money.USCents
		days        int
		parts       []part
	}{
		// $500 × 46 × .00027397 = $6.30; × 61 = $8.36.
		{1, 1_000 * money.Dollar, 61, []part{{500 * money.Dollar, 46, 630}, {500 * money.Dollar, 61, 836}}},
		// $500 × 92 × .00027397 = $12.60; the 31 days of 2027 are unrated.
		{3, 500 * money.Dollar, 123, []part{{500 * money.Dollar, 92, 1_260}}},
		{4, 1_000 * money.Dollar, 24, nil},
	} {
		l := lines[i]
		if l.Installment() != want.installment || l.Shortfall() != want.shortfall || l.Days() != want.days {
			t.Errorf("line %d: installment %d, %v short, %d days; want %d, %v, %d", i+1, l.Installment(), l.Shortfall(), l.Days(), want.installment, want.shortfall, want.days)
		}
		parts := l.Parts()
		if len(parts) != len(want.parts) {
			t.Errorf("line %d: %d parts, want %d", i+1, len(parts), len(want.parts))
			continue
		}
		for j, p := range parts {
			if p.Principal() != want.parts[j].on || p.Days() != want.parts[j].days || p.Charge() != want.parts[j].charge {
				t.Errorf("line %d part %d: %v for %d days is %v; want %v for %d days, %v", i+1, j+1,
					p.Principal(), p.Days(), p.Charge(), want.parts[j].on, want.parts[j].days, want.parts[j].charge)
			}
		}
	}
	if res.Value().Total() != 2_726 {
		t.Errorf("penalty %v, want $27.26", res.Value().Total())
	}
	if !has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("the 2027 days are not reported as unrated")
	}
}

// The penalty over the year end. The published rates for 2025 and 2026 are
// both 10%, and no 2026 shortfall can reach back into 2025, so the only
// boundary inside a 2026 penalty is December 31, 2026, after which the rate is
// not published. A $1,000 April shortfall never paid runs 245 days in 2026:
// $1,000 × 245 × .00027397 = $67.12. Past the year end the charge is the
// same and the gap is reported, and the penalty stops at April 30, 2027.
func TestEstimatedPenaltyAcrossTheYearEnd(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, prior: prior(4_000*money.Dollar, 100_000*money.Dollar)}
	for _, tc := range []struct {
		asOf, stop estimated.Date
		unrated    bool
	}{
		{day(2026, 12, 31), day(2026, 12, 31), false},
		{day(2027, 4, 30), day(2027, 4, 30), true},
		{day(2027, 6, 30), day(2027, 4, 30), true},
	} {
		res := must(rules(t).Penalty(h.facts(t), tc.asOf))
		l := res.Value().Lines()[0]
		parts := l.Parts()
		if len(parts) != 1 || parts[0].Days() != 245 || parts[0].Charge() != 6_712 || parts[0].To() != day(2026, 12, 31) {
			t.Errorf("to %v: line 1 parts %v, want one of 245 days and $67.12 ending December 31, 2026", tc.asOf, parts)
		}
		if res.Value().AsOf() != tc.stop {
			t.Errorf("to %v: figured to %v, want %v", tc.asOf, res.Value().AsOf(), tc.stop)
		}
		if has(res.Required(), working.PositionUnpublishedRate) != tc.unrated {
			t.Errorf("to %v: unpublished rate reported %v", tc.asOf, !tc.unrated)
		}
	}
}

// The interest follows the printed .00027397 a day, not 10% ÷ 365. On a
// $100,000 shortfall for 245 days the two give $6,712.27 and $6,712.33.
func TestEstimatedInterestUsesThePrintedDailyFactor(t *testing.T) {
	// 90% of $444,444.44 is $400,000.00, a quarter of which is $100,000.
	res := must(rules(t).Penalty(household{tax: 44_444_444}.facts(t), day(2026, 12, 31)))
	parts := res.Value().Lines()[0].Parts()
	if len(parts) != 1 || parts[0].Principal() != 100_000*money.Dollar || parts[0].Days() != 245 || parts[0].Charge() != 671_227 {
		t.Errorf("line 1 parts %v, want one of $6,712.27 on $100,000 for 245 days", parts)
	}
}

// A return filed with the balance paid stops the penalty that day.
func TestEstimatedReturnFiledStopsThePenalty(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 3, 1)}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30)))
	if p.Value().AsOf() != day(2027, 3, 1) || !applied(p.Applied(), "every shortfall still outstanding was paid that day") {
		t.Errorf("figured to %v, want March 1, 2027", p.Value().AsOf())
	}
}

// IA 2210 line 20: withholding is spread in equal parts over the dates
// unless the filer shows when it was withheld.
func TestEstimatedWithholdingCredit(t *testing.T) {
	// 90% of $20,000 is $18,000, all of it withheld on December 15.
	h := household{tax: 20_000 * money.Dollar, withheld: 18_000 * money.Dollar}
	for i, in := range must(rules(t).Schedule(h.facts(t))).Value() {
		if in.Status() != estimated.StatusMet {
			t.Errorf("spread: installment %d is %v", i+1, in.Status())
		}
	}
	h.asWithheld = true
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []estimated.Status{estimated.StatusShort, estimated.StatusShort, estimated.StatusShort, estimated.StatusMet} {
		if sched[i].Status() != want {
			t.Errorf("as withheld: installment %d is %v, want %v", i+1, sched[i].Status(), want)
		}
	}
}

// The penalty is before any waiver, and the waiver is asked for until
// answered.
func TestEstimatedWaiverIsAsked(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar}
	if res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30))); !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked")
	}
	k := estimated.WaiverRetiredOrDisabled
	h.waiver = &k
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30)))
	if has(res.Required(), working.PositionPenaltyWaiver) || res.Value().Waiver() != k || res.Value().Total() == 0 {
		t.Errorf("answered: asked %v, waiver %v, total %v", has(res.Required(), working.PositionPenaltyWaiver), res.Value().Waiver(), res.Value().Total())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][4]string{
		"The small-balance threshold":                  {"below $200", "10/22/2025", "below $1,000 of Iowa tax after withholding", "2025 Iowa Acts ch. 148, § 59, effective January 1, 2026"},
		"The daily interest factor":                    {".00027397", "lines 24a and 24b", "0.000273973", "§§ 422.16(12)(d)(3), 421.7(2)"},
		"The waiver for disability in the year before": {"disability, in the tax year", "Waiver of Penalty", "disability in the year before", "701—308.6(2)(b)"},
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
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.FormSource(), w[1]) || !strings.Contains(d.Statute(), w[2]) || !strings.Contains(d.StatuteSource(), w[3]) {
			t.Errorf("%q does not pin both readings: form %q (%s), statute %q (%s)", d.Rule(), d.Form(), d.FormSource(), d.Statute(), d.StatuteSource())
		}
		// $10,000 due with nothing withheld is over both thresholds, and no
		// waiver is asked for.
		wantUses := working.ReadingForm
		if d.Rule() == "The small-balance threshold" {
			wantUses = working.ReadingStatute
		}
		if d.Uses() != wantUses {
			t.Errorf("%q uses %v, want %v", d.Rule(), d.Uses(), wantUses)
		}
		if d.Rule() != "The daily interest factor" && d.AffectsHousehold() {
			t.Errorf("%q affects a household it does not touch", d.Rule())
		}
	}

	// $500 left after withholding is under $1,000 but not under $200, so the
	// two thresholds differ.
	for _, d := range must(rules(t).Requirement(household{tax: 5_000 * money.Dollar, withheld: 4_500 * money.Dollar}.facts(t))).Divergences() {
		if d.Rule() == "The small-balance threshold" && !d.AffectsHousehold() {
			t.Errorf("%q does not affect $500 left after withholding", d.Rule())
		}
	}
	// On a $100,000 shortfall for 245 days the printed factor and the rate
	// give different penalties.
	for _, d := range must(rules(t).Requirement(household{tax: 44_444_444}.facts(t))).Divergences() {
		if d.Rule() == "The daily interest factor" && !d.AffectsHousehold() {
			t.Errorf("%q does not affect a $100,000 shortfall", d.Rule())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := iowataxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())
	base := func() *estimated.FactsBuilder {
		return estimated.NewFacts(iowa, 2026).FilingStatus(filing.Single).ThisYear(this)
	}
	ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
	var periods []estimated.Period
	for _, e := range ends {
		periods = append(periods, must(estimated.NewPeriod().End(e).Build()))
	}
	for name, f := range map[string]estimated.Facts{
		"a farmer":   must(base().FarmShare(7000).Build()),
		"a fisher":   must(base().FishShare(7000).Build()),
		"annualised": must(base().Elections(must(estimated.NewElections().Annualised().Build())).Periods(periods...).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(iowa, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(base().State("not Iowa's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(must(base().Build()), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"$1,000", "$200", "90%", "110%", "net modifications", "$150,000", "April 30", "February 1, 2027", "fewer", "10% a year", "IA 2210F"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Without this year's tax the target is last year's test alone, the penalty
// is an upper bound, and with no prior return there is no target.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(4_000*money.Dollar, 100_000*money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 4_000*money.Dollar || !r.TargetKnown() {
		t.Errorf("required %v, target %v; want $4,000: %s", r.Required(), r.Target(), r.Why())
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
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{1_000 * money.Dollar, 2_000 * money.Dollar, 3_000 * money.Dollar, 4_000 * money.Dollar} {
		if sched[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, sched[i].NeededBy(), want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30))).Value()
	if !(pen.UpperBound() || !pen.RatedThrough().IsZero()) || pen.Total() == 0 {
		t.Errorf("unpaid: penalty %v, upper bound %v", pen.Total(), pen.UpperBound())
	}

	none := household{noTax: true}
	r = must(rules(t).Requirement(none.facts(t))).Value()
	if r.TargetKnown() || r.Required() {
		t.Errorf("with no prior return: target known %v, required %v", r.TargetKnown(), r.Required())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 30)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// Iowa's waivers are the federal pair, but the 2025 IA 2210, whose reading is
// followed, covers retiring or becoming disabled in the tax year only, where
// federal law also covers the year before. Disability in 2025, which the
// rule covers, is refused by the form and reported as the divergence.
// Retiring in 2025 is open in both documents. The penalty is the same before
// any waiver in each case.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "ia-2210-2025.txt",
		"may be waived if the underpayment was due to casualty, disaster, or other unusual circumstances.",
		"retired at age 62 or later, or became disabled in the tax year for which the estimated payments were required")
	mustContain(t, "iac-701-308.6.txt",
		"retired after having attained age 62, or who became disabled in the tax year for which the estimated payment was due or in the preceding tax year")
	h := household{tax: 10_000 * money.Dollar}
	base := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30))).Value().Total()
	if base == 0 {
		t.Fatal("no penalty to waive")
	}
	for _, tc := range []struct {
		name      string
		pos       estimated.Positions
		want      string
		says      string
		unsettled bool
		diverges  bool
	}{
		{"retired in 2026 at 64", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build()), "eligible", "at 64", false, false},
		{"retired in 2026 at 60", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1966, 3, 1)).Build()), "refused", "62 or older", false, false},
		{"retired in 2020", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, 3, 1)).Build()), "refused", "covers retiring in 2026", false, false},
		{"retired in 2025", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2025, 6, 1)).Born(day(1960, 3, 1)).Build()), "undecided", "year before", true, false},
		{"disabled in 2026", must(estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2026, 2, 1)).Build()), "eligible", "became disabled in 2026", false, false},
		{"disabled in 2025", must(estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2025, 9, 1)).Build()), "refused", "only for becoming disabled in 2026", false, true},
		{"a disaster", must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()), "eligible", "Iowa Department of Revenue", false, false},
	} {
		h.positions = &tc.pos
		res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30)))
		p := res.Value()
		o, ok := p.WaiverOutcome()
		got := map[bool]string{true: "eligible"}[o.Eligible()] + map[bool]string{true: "refused"}[o.Refused()] + map[bool]string{true: "undecided"}[o.Undecided()]
		switch {
		case p.Total() != base:
			t.Errorf("%s: penalty %v, want %v before any waiver", tc.name, p.Total(), base)
		case !ok || got != tc.want || !strings.Contains(o.Reason(), tc.says):
			t.Errorf("%s: %s %q, want %s saying %q", tc.name, got, o.Reason(), tc.want, tc.says)
		case o.Eligible() != (p.IfWaived() == 0):
			t.Errorf("%s: if waived %v", tc.name, p.IfWaived())
		case has(res.Required(), working.PositionUnsettledRule) != tc.unsettled:
			t.Errorf("%s: open point asked %v", tc.name, has(res.Required(), working.PositionUnsettledRule))
		}
		for _, d := range res.Divergences() {
			if d.Rule() == "The waiver for disability in the year before" && d.AffectsHousehold() != tc.diverges {
				t.Errorf("%s: waiver divergence affects %v", tc.name, d.AffectsHousehold())
			}
		}
	}
}

// Under the $1,000 exception the target is $0, so last year's return isn't
// asked for as a way to change it. Above it, it is.
func TestEstimatedPriorReturnAskedOnlyWhenItCanSetTheTarget(t *testing.T) {
	under := must(rules(t).Requirement(household{tax: 800 * money.Dollar}.facts(t)))
	if !under.Value().Exempt() || has(under.Required(), working.PositionPriorYearReturn) {
		t.Errorf("under $1,000: exempt %v, asked %v", under.Value().Exempt(), has(under.Required(), working.PositionPriorYearReturn))
	}
	over := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	if !has(over.Required(), working.PositionPriorYearReturn) {
		t.Error("over $1,000: last year's return is not asked for")
	}
}
