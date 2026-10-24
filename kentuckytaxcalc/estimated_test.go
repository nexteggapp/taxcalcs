// Kentucky's estimated tax rules for 2026.
//
// Kentucky publishes no exhaustive oracle and, as far as the research found,
// no worked example of the penalty with a stated answer. The checks here are
// hand-written cases built on Form 2210-K's line arithmetic, which is the
// federal arithmetic at Kentucky's rate. Every threshold, percentage, date and
// rate is compared against its source: KRS 141.305, KRS 131.183 and 103 KAR
// 1:040 from the Legislative Research Commission's own text, and the
// department's documents through the verified quotes in
// research/estimated-payments/ky.md, because revenue.ky.gov refused every
// download (testdata/SOURCES.md).
package kentuckytaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/kentuckytaxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const ky = estimated.Jurisdiction("KY")

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
	return must(kentuckytaxcalc.EstimatedTax(2026))
}

func find(rs []working.Required, k working.PositionKind) (working.Required, bool) {
	for _, r := range rs {
		if r.Position() == k {
			return r, true
		}
	}
	return working.Required{}, false
}

func has(rs []working.Required, k working.PositionKind) bool {
	_, ok := find(rs, k)
	return ok
}

func count(rs []working.Required, k working.PositionKind) int {
	n := 0
	for _, r := range rs {
		if r.Position() == k {
			n++
		}
	}
	return n
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

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

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

// answered are extras that settle every Kentucky question, so that a test
// sees only the Required entries it is about.
func answered() *kentuckytaxcalc.EstimatedFacts {
	e := must(kentuckytaxcalc.NewEstimatedFacts().NonWageIncome(100_000 * money.Dollar).FilingThresholdMet(true).
		PriorYearUSResident(true).CombinedReturn(false).Build())
	return &e
}

type household struct {
	status      filing.Status
	tax         money.USCents // after credits, refundable ones included
	noTax       bool
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *kentuckytaxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	annualised  []money.USCents // the tax on each period's annualised income
	asWithheld  bool
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
	b := estimated.NewFacts(ky, 2026).FilingStatus(status).ThisYear(figures).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	extras := h.extras
	if extras == nil {
		extras = answered()
	}
	b.State(*extras)
	eb := estimated.NewElections()
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		eb.Annualised()
	}
	if h.asWithheld {
		eb.WithholdingAsWithheld()
	}
	b.Elections(must(eb.Build()))
	if h.waiver != nil {
		b.Positions(must(estimated.NewPositions().Waiver(*h.waiver).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

// prior is a filed full-year Kentucky return with its tax and Kentucky AGI.
func prior(tax, agi money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).
		Figures(must(estimated.NewFigures().Tax(tax).StateAGI(agi).Build())).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "krs-141.305.txt",
		"Gross income from sources other than wages upon which Kentucky income tax will be withheld can reasonably be expected to exceed five thousand dollars ($5,000) for the taxable year",
		"Adjusted gross income can reasonably be expected to be an amount not less than the amount for which a return is required under KRS 141.180.",
		"No estimated tax shall be required if the estimated tax liability can reasonably be expected to be five hundred dollars ($500) or less.",
		"shall be made at the same time and calculated in the same manner as an estimated tax payment for federal income tax purposes under 26 U.S.C. sec. 6654",
		"The tax interest rate identified under KRS 131.183 shall be used to determine the underpayment rate instead of the rate under 26 U.S.C. sec. 6621",
		"Any waiver of penalties shall be performed as provided in KRS 131.175.",
		"Effective: June 27, 2019",
	)
	mustContain(t, "krs-131.183.txt",
		"Except for the addition to tax required when an underpayment of estimated tax occurs under KRS 141.044 and 141.305",
		"the tax interest rate shall be equal to the adjusted prime rate charged by banks rounded to the nearest full percent as adjusted by subsection (2) of this section",
		"The adjusted tax interest rate shall become effective on January 1 of the immediately succeeding year.",
		"shall accrue interest at the tax interest rate as determined in accordance with subsection (1) of this section plus two percent (2%).",
		"any addition to tax provided in KRS 141.044 and 141.305 shall be considered a penalty",
		"Amended 2026 Ky. Acts ch. 161, sec. 2, effective July 15, 2026.",
	)
	mustContain(t, "103-kar-1-040-section-1.txt",
		"Death or serious illness of taxpayer or immediate family.",
		"Destruction or unavailability of taxpayer records by a catastrophic event.",
	)
	// The department's documents, through the research file's verified
	// quotes; revenue.ky.gov refused the downloads.
	mustContain(t, "research-ky.md",
		"[ESI-26] \"Instructions for Filing Estimated Tax Vouchers 2026\", 42A740-S4 (9-25).",
		"[2210K-25] Form 2210-K \"Underpayment of Estimated Tax by Individuals\" 2025, 42A740-S1 (10-25)",
		"[ESI-26]: \"90% of the tax to be shown on your 2026 tax return; or (b) 100% of the tax shown on your 2025 tax return.\"",
		"[ESI-26]: \"Your 2025 tax return must cover all 12 months.\"",
		"[ESI-26]: \"If your Kentucky adjusted gross income for 2025 was more than $150,000 ($75,000 if your filing status for 2026 is married filing separately)\".",
		"[TAM]: \"Married couples in Kentucky that file separately on a combined form should add their incomes together to determine if they meet the 110% threshold.\"",
		"[ESI-26]: \"Installments for calendar year 2026 are due April 15, June 15, September 15, 2026, and January 15, 2027.\"",
		"It prints April 15, June 15 and September 15, 2026 and January 15, 2027.",
		"[TAM]: \"If a payment date falls on a holiday or weekend, the applicable payment date is the next working day.\"",
		"[2210K-25] line 9: \"Required Installments. Enter 25% (.25) of Part II, line 8 in each column\".",
		"[2210K-25] line 10: \"If you have Kentucky income tax withheld, multiply the total by 25 percent (.25) and enter in columns A through D.\"",
		"[ESI-26]: \"If the change occurs after September 1, 2026\".",
		"the voucher is not required if the 2026 Kentucky income tax return is filed and ... paid on or before February 1, 2027.\"",
		"February 1, 2027 is January 31, 2027 (a Sunday) moved to the next working day.",
		"[2210K-25] line 5: \"If the result is $500 or less, stop here; you do not owe a penalty.\"",
		"[2210K-25]: \"You had no liability for 2024, you were a U.S. Citizen or resident alien for the entire year\".",
		"[2210K-25], same item: \"your 2024 tax return ... was for a full 12 months.\"",
		"Farmers and fishermen: this applies if at least two-thirds of gross income in the current or prior year came from farming or fishing.",
		"Periods are the federal ones: 1/1 to 3/31, 5/31, 8/31 and 12/31.",
		"Factors are 4, 2.4, 1.5 and 1, and percentages are 22.5%, 45%, 67.5% and 90%.",
		"[2210K-25]: \"If you use Part III for any payment due dates, you must use it for all payment due dates.\"",
		"The last measurement date is April 15 of the following year.",
		"[2210K-25] line 21: \"The percentage rate for calendar years 2025 is 10 percent and 2026 is 9 percent.\"",
		"[RATE-26]: \"established the Tax Interest Rate effective January 1, 2026, to be 7 %\".",
		"Penalty rate for 2026: 7% or 9%. NOT resolved; it changes the number.",
		"The amending act was not read, so exactly what changed is unverified.",
	)
}

// The 2026 dates are the ones the voucher instructions print, all weekdays,
// so none moves. January 31, 2027 is a Sunday, and the file-and-pay date the
// instructions print, February 1, 2027, is it moved to the Monday.
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
	if day(2027, 1, 31).Weekday() != time.Sunday {
		t.Fatal("January 31, 2027 is not a Sunday")
	}
	late := household{tax: 10_000 * money.Dollar, firstMet: day(2026, 10, 1)}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 1, 31), true}, {day(2027, 2, 1), true}, {day(2027, 2, 2), false}} {
		h := late
		h.returnFiled = tc.filed
		got := must(rules(t).Schedule(h.facts(t))).Value()[3].Status() == estimated.StatusExcused
		if got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
	}
}

// $500 or less of tax after withholding owes nothing; $500.01 does. The
// statute and Form 2210-K line 5 agree on this, against the voucher
// instructions' "at least $500".
func TestEstimatedFiveHundredDollarBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{950_000, true},  // $500.00 left
		{949_999, false}, // $500.01 left
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		// At $500 the exception is the chosen option, at $0.
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding of $500 or less") {
			t.Errorf("withheld %v: chosen %v %q at %v", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
	}
}

// KRS 141.305(1): more than $5,000 of income without Kentucky withholding, or
// AGI at the filing threshold, brings in the rules. Neither, both answered,
// means no payments are required.
func TestEstimatedIncomeTests(t *testing.T) {
	for _, tc := range []struct {
		nonWage money.USCents
		exempt  bool
	}{
		{500_000, true},  // $5,000.00 is not more than $5,000
		{500_001, false}, // $5,000.01 is
	} {
		e := must(kentuckytaxcalc.NewEstimatedFacts().NonWageIncome(tc.nonWage).FilingThresholdMet(false).
			PriorYearUSResident(true).Build())
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &e}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("non-wage income %v: exempt %v, want %v", tc.nonWage, res.Value().Exempt(), tc.exempt)
		}
	}
	// Exempt by the income tests, neither this year's tax nor last year's
	// return can change the target, so neither is asked for.
	low := must(kentuckytaxcalc.NewEstimatedFacts().NonWageIncome(0).FilingThresholdMet(false).Build())
	res := must(rules(t).Requirement(household{noTax: true, extras: &low}.facts(t)))
	if !res.Value().Exempt() || has(res.Required(), working.PositionThisYearTax) || has(res.Required(), working.PositionPriorYearReturn) {
		t.Errorf("exempt by income: exempt %v, asked %v", res.Value().Exempt(), res.Required())
	}
	// The filing threshold alone brings the rules in.
	e := must(kentuckytaxcalc.NewEstimatedFacts().NonWageIncome(0).FilingThresholdMet(true).Build())
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &e}.facts(t))); !res.Value().Required() {
		t.Errorf("AGI at the filing threshold: %s", res.Value().Why())
	}
	// Neither answered: the rules are applied, and the question asked.
	e = must(kentuckytaxcalc.NewEstimatedFacts().PriorYearUSResident(true).Build())
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &e}.facts(t)))
	if !res.Value().Required() || !has(res.Required(), working.PositionJurisdictionFact) {
		t.Errorf("income tests unanswered: required %v, asked %v", res.Value().Required(), has(res.Required(), working.PositionJurisdictionFact))
	}
}

// The two options and the 110% step, one cent either side of $150,000 and,
// married filing separately, of $75,000.
func TestEstimatedTargetOptions(t *testing.T) {
	// This year's 90% is the smaller.
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(12_000*money.Dollar, 100_000*money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 9_000*money.Dollar {
		t.Errorf("chose %v at %v, want 90%% of this year's, $9,000", c.Basis(), res.Value().Target())
	}

	for _, tc := range []struct {
		status filing.Status
		agi    money.USCents
		want   money.USCents
	}{
		{filing.Single, 15_000_000, 8_000 * money.Dollar},
		{filing.Single, 15_000_001, 8_800 * money.Dollar},
		{filing.MarriedFilingJointly, 15_000_001, 8_800 * money.Dollar},
		{filing.MarriedFilingSeparately, 7_500_000, 8_000 * money.Dollar},
		{filing.MarriedFilingSeparately, 7_500_001, 8_800 * money.Dollar},
	} {
		res := must(rules(t).Requirement(household{status: tc.status, tax: 20_000 * money.Dollar, prior: prior(8_000*money.Dollar, tc.agi)}.facts(t)))
		c, _ := res.Value().Chosen()
		if c.Basis() != estimated.BasisPriorYearTax || res.Value().Target() != tc.want {
			t.Errorf("%v with %v of 2025 AGI: chose %v at %v, want last year's at %v", tc.status, tc.agi, c.Basis(), res.Value().Target(), tc.want)
		}
	}

	// Without last year's AGI, 110% is assumed and the AGI asked for.
	noAGI := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(8_000 * money.Dollar).Build())).Build())
	res = must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: &noAGI}.facts(t)))
	if res.Value().Target() != 8_800*money.Dollar || !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Errorf("no 2025 AGI: target %v, asked %v; want $8,800 and asked", res.Value().Target(), has(res.Required(), working.PositionAdjustedGrossIncome))
	}

	// When the prior-year option is not open, and why.
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(must(estimated.NewFigures().Tax(100).StateAGI(1_000).Build())).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		why   string
		ask   bool
	}{
		"no return on record": {nil, "Needs your 2025 Kentucky return", true},
		"not filed":           {&notFiled, "didn't file a 2025 Kentucky return", false},
		"a short year":        {&short, "covered 6 months", false},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		o := res.Value().Options()[1]
		if o.Available() || !strings.Contains(o.WhyNot(), tc.why) {
			t.Errorf("%s: available %v, %q", name, o.Available(), o.WhyNot())
		}
		if has(res.Required(), working.PositionPriorYearReturn) != tc.ask {
			t.Errorf("%s: asked for the prior return %v, want %v", name, !tc.ask, tc.ask)
		}
	}
}

// KY-TAM-19-02 adds the incomes of a couple filing separately on a combined
// return. Which step the sum is tested against is not settled, so between
// $75,000 and $150,000 the $75,000 step is used and the question reported.
func TestEstimatedCombinedReturn(t *testing.T) {
	combined := func(spouse *money.USCents) *kentuckytaxcalc.EstimatedFacts {
		b := kentuckytaxcalc.NewEstimatedFacts().NonWageIncome(100_000 * money.Dollar).FilingThresholdMet(true).PriorYearUSResident(true).CombinedReturn(true)
		if spouse != nil {
			b.SpousePriorYearAGI(*spouse)
		}
		e := must(b.Build())
		return &e
	}
	spouse := 50_000 * money.Dollar
	h := household{status: filing.MarriedFilingSeparately, tax: 20_000 * money.Dollar, prior: prior(8_000*money.Dollar, 50_000*money.Dollar), extras: combined(&spouse)}
	res := must(rules(t).Requirement(h.facts(t)))
	if res.Value().Target() != 8_800*money.Dollar {
		t.Errorf("combined $100,000: target %v, want 110%%, $8,800", res.Value().Target())
	}
	if r, ok := find(res.Required(), working.PositionUnsettledRule); !ok || r.Question() != "" || !strings.Contains(r.Effect(), "$75,000 for married filing separately") {
		t.Errorf("the combined-return step is not reported: %v", res.Required())
	}

	// Under $75,000 together, 100% and no question.
	spouse = 20_000 * money.Dollar
	h.extras = combined(&spouse)
	res = must(rules(t).Requirement(h.facts(t)))
	if res.Value().Target() != 8_000*money.Dollar || has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Errorf("combined $70,000: target %v, asked %v", res.Value().Target(), has(res.Required(), working.PositionAdjustedGrossIncome))
	}

	// The spouse's AGI not given: 110% assumed and asked.
	h.extras = combined(nil)
	res = must(rules(t).Requirement(h.facts(t)))
	if res.Value().Target() != 8_800*money.Dollar || !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Errorf("no spouse AGI: target %v", res.Value().Target())
	}

	// Married filing separately, combined or not unsaid: asked, own AGI used.
	e := must(kentuckytaxcalc.NewEstimatedFacts().NonWageIncome(100_000 * money.Dollar).FilingThresholdMet(true).PriorYearUSResident(true).Build())
	h.extras = &e
	res = must(rules(t).Requirement(h.facts(t)))
	if res.Value().Target() != 8_000*money.Dollar || !has(res.Required(), working.PositionJurisdictionFact) {
		t.Errorf("combined unsaid: target %v, asked %v", res.Value().Target(), has(res.Required(), working.PositionJurisdictionFact))
	}
}

// No Kentucky liability last year, on a full-year return, as a U.S. citizen
// or resident alien, means no penalty.
func TestEstimatedNoLiabilityLastYear(t *testing.T) {
	zero := prior(0, 30_000*money.Dollar)
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	shortZero := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(0).Build())).Build())
	for name, tc := range map[string]struct {
		prior  *estimated.PriorReturn
		exempt bool
	}{
		"no tax shown":          {zero, true},
		"no return required":    {&notRequired, true},
		"no tax, eleven months": {&shortZero, false},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("%s: exempt %v, want %v", name, res.Value().Exempt(), tc.exempt)
		}
		if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t), day(2027, 4, 15))); tc.exempt && p.Value().Total() != 0 {
			t.Errorf("%s: a penalty of %v", name, p.Value().Total())
		}
	}
	// Citizenship or residence not said: asked, and not applied.
	e := must(kentuckytaxcalc.NewEstimatedFacts().NonWageIncome(100_000 * money.Dollar).FilingThresholdMet(true).Build())
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: zero, extras: &e}.facts(t)))
	if res.Value().Exempt() || !has(res.Required(), working.PositionJurisdictionFact) {
		t.Errorf("residence unsaid: exempt %v, asked %v", res.Value().Exempt(), has(res.Required(), working.PositionJurisdictionFact))
	}
}

// Four quarters of 25%, with a payment applied to the oldest shortfall
// first, charged 9% a year by the day. April's $1,000 shortfall is paid by
// the June payment: 61 days × 9% ÷ 365 × $1,000 is $15.04.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, payments: quarterly(1_250*money.Dollar, 3_250*money.Dollar, 2_250*money.Dollar, 2_250*money.Dollar)}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{2_250, 4_500, 6_750, 9_000} {
		if sched[i].NeededBy() != want*money.Dollar {
			t.Errorf("installment %d needs %v, want $%d", i+1, sched[i].NeededBy(), want)
		}
	}
	if sched[0].Status() != estimated.StatusShort || sched[0].Shortfall() != 1_000*money.Dollar || sched[1].Status() != estimated.StatusMet {
		t.Errorf("April is %v %v short, June %v", sched[0].Status(), sched[0].Shortfall(), sched[1].Status())
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	lines := p.Value().Lines()
	if len(lines) != 1 || lines[0].Days() != 61 || p.Value().Total() != 1_504 {
		t.Fatalf("penalty %v over %d lines; want $15.04 on April for 61 days", p.Value().Total(), len(lines))
	}
	if parts := lines[0].Parts(); len(parts) != 1 || parts[0].Rate() != money.Percent(9) {
		t.Errorf("parts %+v, want one at 9%%", parts)
	}

	// The penalty never runs past April 15, 2027, and stops on the day a
	// return is filed and paid.
	if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 6, 30))); p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
	if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 3, 1)}.facts(t), day(2027, 4, 15))); p.Value().AsOf() != day(2027, 3, 1) {
		t.Errorf("figured to %v, want the day the return was paid", p.Value().AsOf())
	}
}

// Kentucky publishes one rate for each calendar year, and the 2026 rate is
// the only one a 2026 shortfall can run under before 2027. So the boundaries
// tested are July 15, 2026, when the amended KRS 131.183 took effect and the
// form's rate is the same on both sides, and December 31, 2026, after which
// no rate is published.
func TestEstimatedPenaltyAcrossRatePeriods(t *testing.T) {
	// April's $1,000, paid on September 15 with nothing in between: 153 days
	// in one part at 9%, $37.73.
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(dues[0], 1_250*money.Dollar), pay(dues[2], 1_000*money.Dollar)}}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	parts := p.Value().Lines()[0].Parts()
	if len(parts) != 1 || parts[0].Days() != 153 || parts[0].Charge() != 3_773 {
		t.Errorf("across July 15: %+v, want one part of 153 days and $37.73", parts)
	}

	// September's $2,250, never paid: 107 days of 2026 at 9%, $59.36, and the
	// 2027 days are reported rather than charged.
	h = household{tax: 10_000 * money.Dollar, payments: quarterly(2_250*money.Dollar, 2_250*money.Dollar)}
	p = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	lines := p.Value().Lines()
	if len(lines) != 2 || lines[0].Installment() != 3 {
		t.Fatalf("%d lines, want September and January", len(lines))
	}
	if parts := lines[0].Parts(); len(parts) != 1 || parts[0].Days() != 107 || parts[0].Charge() != 5_936 || parts[0].To() != day(2026, 12, 31) {
		t.Errorf("September: %+v, want 107 days to December 31 and $59.36", parts)
	}
	if len(lines[1].Parts()) != 0 {
		t.Errorf("January is charged %v with no 2027 rate", lines[1].Total())
	}
	gap := false
	for _, r := range p.Required() {
		if r.Position() == working.PositionUnpublishedRate && strings.Contains(r.Effect(), "Kentucky rate for the days after December 31, 2026 isn't published yet") {
			gap = true
		}
	}
	if !gap || count(p.Required(), working.PositionUnpublishedRate) != 2 {
		t.Errorf("the 2027 gap is not reported beside the 2026 rate question: %v", p.Required())
	}
	if p.Value().Total() != 5_936 {
		t.Errorf("total %v, want $59.36", p.Value().Total())
	}
}

// The rate is the form's 9%, and the 7% reading is an open question.
func TestEstimatedRateIsAnOpenQuestion(t *testing.T) {
	p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2026, 12, 31)))
	var found bool
	for _, r := range p.Required() {
		if r.Position() == working.PositionUnpublishedRate && strings.Contains(r.Effect(), "9%") && strings.Contains(r.Effect(), "7%") {
			found = true
		}
	}
	if !found {
		t.Errorf("the 7%% or 9%% question is not reported: %v", p.Required())
	}
	// The figure uses the form's 9%, and the reading affects this penalty.
	for _, d := range p.Divergences() {
		if d.Rule() == "The penalty rate for 2026" && (d.Uses() != working.ReadingForm || !d.AffectsHousehold() || !strings.Contains(d.Form(), "July 15, 2026")) {
			t.Errorf("the rate divergence uses %v, affects %v: %q", d.Uses(), d.AffectsHousehold(), d.Form())
		}
	}
	// No penalty, no question.
	p = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, payments: quarterly(2_250*money.Dollar, 2_250*money.Dollar, 2_250*money.Dollar, 2_250*money.Dollar)}.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 0 || has(p.Required(), working.PositionUnpublishedRate) {
		t.Errorf("paid in full: penalty %v, rate asked %v", p.Value().Total(), has(p.Required(), working.PositionUnpublishedRate))
	}
}

// The January voucher is excused for a return filed and paid by February 1,
// 2027 only when the requirement first arose after September 1, 2026.
func TestEstimatedJanuaryInstallment(t *testing.T) {
	for _, tc := range []struct {
		firstMet estimated.Date
		filed    estimated.Date
		excused  bool
	}{
		{day(2026, 9, 2), day(2027, 2, 1), true},
		{day(2026, 9, 1), day(2027, 2, 1), false},
		{day(2026, 9, 2), day(2027, 2, 2), false},
		{day(2026, 1, 1), day(2027, 1, 20), false},
	} {
		h := household{tax: 10_000 * money.Dollar, firstMet: tc.firstMet, returnFiled: tc.filed}
		got := must(rules(t).Schedule(h.facts(t))).Value()[3].Status() == estimated.StatusExcused
		if got != tc.excused {
			t.Errorf("first met %v, filed %v: excused %v, want %v", tc.firstMet, tc.filed, got, tc.excused)
		}
	}
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 1, 20)}.facts(t)))
	if !has(res.Required(), working.PositionRequirementFirstMet) {
		t.Errorf("filed early with no date for the requirement: not asked")
	}
}

// Form 2210-K Part III: 22.5%, 45%, 67.5% and 90% of the annualised tax,
// less earlier installments, capped by the regular installment plus what was
// carried forward.
func TestEstimatedAnnualised(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, annualised: []money.USCents{2_000 * money.Dollar, 4_000 * money.Dollar, 8_000 * money.Dollar, 10_000 * money.Dollar}}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{450, 1_800, 5_400, 9_000} {
		if sched[i].NeededBy() != want*money.Dollar {
			t.Errorf("installment %d needs %v, want $%d", i+1, sched[i].NeededBy(), want)
		}
	}
	// The periods must be the form's.
	f := estimated.NewFacts(ky, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(100).Build())).
		Elections(must(estimated.NewElections().Annualised().Build()))
	for _, end := range []estimated.Date{day(2026, 3, 31), day(2026, 6, 30), day(2026, 8, 31), day(2026, 12, 31)} {
		f.Periods(must(estimated.NewPeriod().End(end).Build()))
	}
	if _, err := rules(t).Schedule(must(f.Build())); err == nil {
		t.Error("a period ending June 30 was accepted")
	}
}

// Withholding is spread 25% to each date, even when the election is made.
func TestEstimatedWithholdingIsSpreadEvenly(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, withheld: 4_000 * money.Dollar, asWithheld: true}
	res := must(rules(t).Schedule(h.facts(t)))
	if got := res.Value()[0].CreditedBy(); got != 1_000*money.Dollar {
		t.Errorf("credited by April %v, want a quarter of the December withholding, $1,000", got)
	}
	if !applied(res.Applied(), "offers no election") {
		t.Error("the election is not said to be refused")
	}
}

// Kentucky's waivers are for reasonable cause only.
func TestEstimatedWaivers(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); !has(p.Required(), working.PositionPenaltyWaiver) {
		t.Error("waiver not asked")
	}
	k := estimated.WaiverRetiredOrDisabled
	h.waiver = &k
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if has(p.Required(), working.PositionPenaltyWaiver) || !applied(p.Applied(), "no waiver for retirement or disability") {
		t.Errorf("retired: asked %v", has(p.Required(), working.PositionPenaltyWaiver))
	}
	if p.Value().Waiver() != k || p.Value().Total() == 0 {
		t.Errorf("waiver %v, total %v; the penalty is before any waiver", p.Value().Waiver(), p.Value().Total())
	}
}

// A casualty can be waived as reasonable cause; retirement can't, at any age
// or date, unlike the federal waiver.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pos      *estimated.PositionsBuilder
		eligible bool
		want     string
	}{
		{"disaster", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)), true, "destroyed or kept from you"},
		{"retired in 2026 at 64", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)), false, "not retirement or disability alone"},
		{"retired in 2020", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, 3, 1)), false, "not retirement or disability alone"},
		{"disabled in 2026", estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2026, 6, 1)), false, "not retirement or disability alone"},
	} {
		pos := must(tc.pos.Build())
		p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, positions: &pos}.facts(t), day(2027, 4, 15))).Value()
		o, ok := p.WaiverOutcome()
		if !ok || o.Eligible() != tc.eligible || o.Refused() == tc.eligible || !strings.Contains(o.Reason(), tc.want) {
			t.Errorf("%s: outcome %v eligible %v, %q", tc.name, ok, o.Eligible(), o.Reason())
		}
		if p.Total() == 0 {
			t.Errorf("%s: the penalty is before any waiver, got $0", tc.name)
		}
		want := p.Total()
		if tc.eligible {
			want = 0
		}
		if p.IfWaived() != want {
			t.Errorf("%s: if waived %v, want %v", tc.name, p.IfWaived(), want)
		}
	}
}

// Each divergence is reported with both readings and both sources, and
// follows the form.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 4, 15)))
	want := map[string][4]string{
		"Exactly $500":                          {"$500 or less", "line 5", "$500 or less", "KRS 141.305(2)"},
		"The penalty rate for 2026":             {"9%", "line 21", "7%", "2026 Ky. Acts ch. 161"},
		"A return filed and paid by February 1": {"after September 1, 2026", "42A740-S4", "files and pays in full by January 31", "§ 6654(h)"},
		"When withholding counts as paid":       {"25% on each due date", "line 10", "actually withheld", "§ 6654(g)(1)"},
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
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.FormSource(), w[1]) ||
			!strings.Contains(d.Statute(), w[2]) || !strings.Contains(d.StatuteSource(), w[3]) {
			t.Errorf("%q does not pin both readings: form %q (%s), statute %q (%s)", d.Rule(), d.Form(), d.FormSource(), d.Statute(), d.StatuteSource())
		}
		if d.Uses() != working.ReadingForm {
			t.Errorf("%q uses %v", d.Rule(), d.Uses())
		}
	}

	// $10,000 of tax, nothing withheld and no return filed: only the rate
	// reaches these facts.
	affects := func(res working.Result[estimated.Requirement]) map[string]bool {
		out := map[string]bool{}
		for _, d := range res.Divergences() {
			out[d.Rule()] = d.AffectsHousehold()
		}
		return out
	}
	for rule, a := range map[string]bool{"Exactly $500": false, "The penalty rate for 2026": true, "A return filed and paid by February 1": false, "When withholding counts as paid": false} {
		for _, d := range res.Divergences() {
			if d.Rule() == rule && d.AffectsHousehold() != a {
				t.Errorf("%q affects %v, want %v", rule, d.AffectsHousehold(), a)
			}
		}
	}
	for _, tc := range []struct {
		rule string
		h    household
	}{
		{"Exactly $500", household{tax: 10_000 * money.Dollar, withheld: 950_000}},
		{"A return filed and paid by February 1", household{tax: 10_000 * money.Dollar, firstMet: day(2026, 9, 1), returnFiled: day(2027, 2, 1)}},
		{"When withholding counts as paid", household{tax: 10_000 * money.Dollar, withheld: 1_000 * money.Dollar}},
	} {
		if !affects(must(rules(t).Requirement(tc.h.facts(t))))[tc.rule] {
			t.Errorf("%q does not affect the facts it turns on", tc.rule)
		}
	}
	if affects(must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, firstMet: day(2026, 9, 2), returnFiled: day(2027, 2, 1)}.facts(t))))["A return filed and paid by February 1"] {
		t.Error("the readings agree for a requirement first met after September 1, yet the divergence affects it")
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, year := range []int{2025, 2027} {
		if _, err := kentuckytaxcalc.EstimatedTax(year); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", year, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"a farmer": must(estimated.NewFacts(ky, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(6_667).Build()),
		"a fisher": must(estimated.NewFacts(ky, 2026).FilingStatus(filing.Single).ThisYear(this).FishShare(6_667).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(ky, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(ky, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Kentucky's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := kentuckytaxcalc.NewEstimatedFacts().NonWageIncome(-1).Build(); err == nil {
		t.Error("negative gross income accepted")
	}
	if _, err := kentuckytaxcalc.NewEstimatedFacts().SpousePriorYearAGI(1).Build(); err == nil {
		t.Error("a spouse's AGI without a combined return accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$500", "$5,000", "110%", "combined return", "9%", "7%", "September 1", "reasonable cause"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Without this year's tax the target is last year's figure alone, and the
// penalty against it is an upper bound.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000*money.Dollar, 100_000*money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 8_000*money.Dollar {
		t.Errorf("required %v, target %v; want $8,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Error("this year's tax is not asked for")
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !(p.Value().UpperBound() || !p.Value().RatedThrough().IsZero()) || p.Value().Total() == 0 {
		t.Errorf("unpaid: penalty %v, upper bound %v", p.Value().Total(), p.Value().UpperBound())
	}

	// No return on record either: no target.
	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() {
		t.Error("a target with neither year's tax")
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}

	// No liability last year needs nothing about this year: an exact zero.
	zero := household{noTax: true, prior: prior(0, 20_000*money.Dollar)}
	p = must(rules(t).Penalty(zero.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 0 || p.Value().UpperBound() {
		t.Errorf("no liability: penalty %v, upper bound %v", p.Value().Total(), p.Value().UpperBound())
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("KY"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}
