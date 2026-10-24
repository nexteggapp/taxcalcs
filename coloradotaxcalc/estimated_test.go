// Colorado's estimated tax rules for 2026, checked against the Department of
// Revenue's own documents and the Colorado Revised Statutes.
//
// Colorado publishes no exhaustive oracle and no worked example of the
// estimated tax penalty. The one published worked figure is the interest
// example on CDOR's penalties and interest page, which splits a shortfall at
// each calendar year and divides by 365 or 366, as DR 0204 lines 15 and 18 do;
// TestInterestMatchesTheDepartmentsExample reproduces it. Every threshold,
// percentage, date and rate is compared against its extract in testdata/. The
// penalty arithmetic, the boundaries and the exceptions are hand-written
// cases, worked by hand in their comments.
package coloradotaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/coloradotaxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const colorado = estimated.Jurisdiction("CO")

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
	return must(coloradotaxcalc.EstimatedTax(2026))
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
		out = append(out, r.Cite()+": "+r.Applied())
	}
	return strings.Join(out, " | ")
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

func quarterly(a ...money.USCents) []estimated.Payment {
	var out []estimated.Payment
	for i, x := range a {
		out = append(out, pay(dues[i], x))
	}
	return out
}

type household struct {
	status      filing.Status
	tax         money.USCents // DR 0204 line 1
	noTax       bool
	withheld    money.USCents
	withheldOn  estimated.Date
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *coloradotaxcalc.EstimatedFacts
	noExtras    bool
	returnFiled estimated.Date
	periods     []money.USCents // Colorado taxable income to each period end
	asWithheld  bool
	waiver      *estimated.WaiverKind
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
	b := estimated.NewFacts(colorado, 2026).FilingStatus(status).ThisYear(must(fb.Build())).
		Paid(h.payments...).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		on := h.withheldOn
		if on.IsZero() {
			on = day(2026, 12, 15)
		}
		b.Withheld(must(estimated.NewWithheld().On(on).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	switch {
	case h.extras != nil:
		b.State(*h.extras)
	case !h.noExtras:
		b.State(must(coloradotaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).FederalAnnualised(true).Build()))
	}
	eb := estimated.NewElections()
	if h.periods != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, inc := range h.periods {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).Income(inc).Build()))
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
	return must(b.Build())
}

// prior is a filed twelve-month Colorado return with its tax and last year's
// federal AGI.
func prior(tax, federalAGI money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).FederalAGI(federalAGI).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(true).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "dr0104ep-2026.txt",
		"DR 0104EP (06/10/25)",
		"you must pay estimated tax if you expect to owe more than $1,000 in net tax for 2026, after subtracting any withholding or credits you might have.",
		"• 70% of the actual net Colorado tax liability, or",
		"• 100% of the preceding year’s net Colorado tax liability. This rule only applies if the preceding year was a 12-month tax year, the individual filed a Colorado return, and the federal adjusted gross income on that return was $150,000 or less (or, if married filing separately, $75,000 or less), or",
		"• 110% of the preceding year’s net Colorado tax liability. This amount applies only if the preceding year was a 12-month year, and the individual filed a Colorado return",
		"If at least 2/3 of your income is from farming or fishing, 50% of the actual net Colorado tax liability.",
		"If you use an annualized method to file estimated payments for your federal income tax, you may file using this method for Colorado estimated payments.",
		"Any overpayment designated for carryforward on your Colorado income tax return from the previous year will be applied to the first estimated tax payment due for this tax period.",
		"2. Estimated 2026 Colorado income tax — 4.4% of line 1",
		"00 April 15 00 June 15 00 September 15 00 January 15, 2027",
		"Due Dates: If the due date falls on a weekend or legal holiday, payment will be due the next business day.",
	)
	mustContain(t, "dr0204-2025.txt",
		"DR 0204 (09/25/25)",
		"2. (a) Statutory exemption $ 1,000.00",
		"3. Subtract line 2(c) from line 1. If 2(c) is larger, enter zero and no penalty is due.",
		"(b) Enter 70% of the amount on line 4(a).",
		"(b) If your previous year federal adjusted gross income is greater than $150,000 (greater than $75,000 if married filing separate), enter 10% of line 5(a).",
		"6. Required payment. Enter the smaller of lines 4(b) or 5(c)",
		"7. Divide the amount on line 6",
		"by four.",
		"If the tax return is filed and any tax due is paid by January 31 no penalty will be computed in column four.",
		"13. Date of payment or December 31, whichever is earlier",
		"multiplied by 12% multiplied by number of days on line 14 divided by 365 (366 for leap year)",
		"16. Date of payment or April 15, whichever is earlier",
		"17. Number of days from December 31, or due date of payment, whichever is later, to date on line 16",
		"multiplied by 11% multiplied by number of days on line 17 divided by 365 (366 for leap year)",
		"may elect to use the annualized income installment method to compute their estimated tax payments if they elect annualized installments for the payment of their federal income tax.",
	)
	mustContain(t, "dr0204-2025-annualized.txt",
		"March 31 May 31 August 31 December 31",
		"22. Annualization factor 4 2.4 1.5 1",
		"line 23 multiplied by 4.4%",
		"25. Applicable percentage 17.5% 35% 52.5% 70%",
		"Line 24 multiplied by line 25, subtract amounts entered on line 26 in earlier quarters. Enter here and on line 7.",
	)
	mustContain(t, "cdor-penalties-and-interest.txt",
		"2025 | 9% | 12% |",
		"2026 | 8% | 11% |",
		"2027 | 7% | 10% |",
		"dividing the annual rate by 365 days (or 366 days for leap years)",
	)
	mustContain(t, "cdor-estimated-payments.txt",
		"Estimated tax payments are due in four equal installments on the following dates.",
		"If the due date falls on a Saturday, Sunday, or legal holiday, payment is due on the next business day.",
	)
	mustContain(t, "crs-39-22-605.txt",
		"(I) Seventy percent of the taxpayer's actual Colorado tax liability shown on the return for the taxable year",
		"(II) (A) One hundred percent of the taxpayer's actual Colorado tax liability shown on the return of the individual for the preceding taxable year.",
		"shall not apply if the preceding taxable year was not a taxable year of twelve months or if the individual did not file a Colorado return",
		"for the preceding taxable year beginning in any calendar year exceeds one hundred fifty thousand dollars",
		"shall be applied by substituting one hundred ten percent for one hundred percent.",
		"shall be applied by substituting seventy-five thousand dollars for one hundred fifty thousand dollars.",
		"(a) The amount of any required installment shall be twenty-five percent of the required annual payment.",
		"(I) The fifteenth day of the fourth month following the close of the taxable year",
		"credited against unpaid required installments in the order in which such installments are required to be paid.",
		"falls on a legal federal holiday, then the due date shall be delayed and due on the adjusted federal due date.",
		"is less than one thousand dollars.",
		"(III) The individual was a resident of Colorado throughout the preceding taxable year.",
		"an equal part of such amount shall be deemed paid on each due date for such taxable year, unless the taxpayer establishes the dates",
		"(9) If, on or before January 31 of the following taxable year, the taxpayer files a return",
		"When the taxpayer has elected annualized installments for the payment of federal income tax",
		"L. 2022: (2)(c) amended, (SB 22-233)",
	)
	mustContain(t, "crs-39-21-110.5.txt",
		`the annual rate of interest shall be the prime rate, as reported by the "Wall Street Journal", plus three points, rounded to the nearest full percent.`,
		"on July 1, or the next succeeding business day, of each year, the adjusted annual rate of interest shall be established in the same manner, to become effective on January 1 of the next succeeding year.",
	)
	mustContain(t, "crs-39-21-109.txt", "(5) This section shall not apply to any failure to pay estimated Colorado income tax.")
}

// Neither the 2025 nor the 2026 session changed § 39-22-605 or
// § 39-21-110.5, so the 2024 statute text is current and no form here
// predates a change in the law.
func TestEstimatedStatuteUnchangedSince2024(t *testing.T) {
	mustContain(t, "redbook-2025-articles-21-22.txt", "39-21-102 (9)", "39-22-604 (3)(a)")
	mustContain(t, "redbook-2026-articles-21-22.txt", "39-21-102 (9)", "39-22-629 (1)(a)")
	for _, name := range []string{"redbook-2025-articles-21-22.txt", "redbook-2026-articles-21-22.txt"} {
		text := extract(t, name)
		for _, s := range []string{"39-22-605 ", "39-21-110.5 ", "39-21-109 "} {
			if strings.Contains(text, s) {
				t.Errorf("%s lists %q as changed", name, s)
			}
		}
	}
}

// The 2026 dates are the ones DR 0104EP prints. None falls on a weekend, so
// none moves. The file-and-pay date, January 31, 2027, is a Sunday and is
// not moved: neither the statute nor DR 0204 says it moves.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar}.facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
	}
	if wd := day(2027, 1, 31).Weekday(); wd != time.Sunday {
		t.Fatalf("January 31, 2027 is a %v", wd)
	}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
		asked   bool
	}{
		{day(2027, 1, 29), true, false},
		{day(2027, 1, 31), true, false},
		{day(2027, 2, 1), false, true}, // the next business day: an open question
		{day(2027, 2, 2), false, false},
	} {
		res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar, returnFiled: tc.filed}.facts(t)))
		if got := res.Value()[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
		if got := strings.Contains(because(res.Required(), working.PositionUnsettledRule), "Sunday"); got != tc.asked {
			t.Errorf("filed %v: the moving January 31 asked %v, want %v", tc.filed, got, tc.asked)
		}
	}
}

// DR 0204 exempts tax less withholding of $1,000 or less. $1,000.00 is
// exempt; $1,000.01 is not. $3,000 of tax keeps the withholding under the 70%
// target, $2,100, so the second case is short and owes a penalty.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{200_000, true},  // $3,000 less $2,000: $1,000.00
		{199_999, false}, // $1,000.01
	} {
		h := household{tax: 3_000 * money.Dollar, withheld: tc.withheld}
		res := must(rules(t).Requirement(h.facts(t)))
		if got := res.Value().Exempt(); got != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, got, tc.exempt, res.Value().Why())
		}
		if c, _ := res.Value().Chosen(); tc.exempt && (c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding of $1,000 or less") {
			t.Errorf("withheld %v: chosen %v %q at %v", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
		if got := res.Value().Required(); got == tc.exempt {
			t.Errorf("withheld %v: required %v", tc.withheld, got)
		}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if got := p.Value().Total() == 0; got != tc.exempt {
			t.Errorf("withheld %v: penalty %v", tc.withheld, p.Value().Total())
		}
	}
}

// § 39-22-605(6)(c): 110% when last year's federal AGI exceeds $150,000, or
// $75,000 on a separate return this year.
func TestEstimatedPriorYearStepBoundary(t *testing.T) {
	for _, tc := range []struct {
		status filing.Status
		agi    money.USCents
		want   money.Rate
	}{
		{filing.Single, 15_000_000, money.Percent(100)},
		{filing.Single, 15_000_001, money.Percent(110)},
		{filing.MarriedFilingJointly, 15_000_000, money.Percent(100)},
		{filing.MarriedFilingJointly, 15_000_001, money.Percent(110)},
		{filing.MarriedFilingSeparately, 7_500_000, money.Percent(100)},
		{filing.MarriedFilingSeparately, 7_500_001, money.Percent(110)},
		{filing.MarriedFilingJointly, 7_500_001, money.Percent(100)},
	} {
		res := must(rules(t).Requirement(household{status: tc.status, tax: 100_000 * money.Dollar, prior: prior(10_000*money.Dollar, tc.agi)}.facts(t)))
		o := res.Value().Options()[1]
		if o.Percent() != tc.want || o.Amount() != tc.want.Of(10_000*money.Dollar) {
			t.Errorf("%v, AGI %v: %v of last year's tax, %v; want %v", tc.status, tc.agi, o.Percent(), o.Amount(), tc.want)
		}
	}
	// Without last year's AGI, 110% is assumed and asked about.
	f := must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).Build())
	res := must(rules(t).Requirement(household{tax: 100_000 * money.Dollar, prior: &p}.facts(t)))
	if o := res.Value().Options()[1]; o.Percent() != money.Percent(110) || !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Errorf("AGI not given: %v, asked %v", o.Percent(), has(res.Required(), working.PositionAdjustedGrossIncome))
	}
}

// The target is the smaller of 70% of this year's tax and the prior-year
// test, and the prior-year test needs a filed twelve-month Colorado return.
func TestEstimatedTargetOptions(t *testing.T) {
	// 70% of $40,000 is $28,000; 100% of $20,000 is $20,000.
	res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: prior(20_000*money.Dollar, 100_000*money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || res.Value().Target() != 20_000*money.Dollar {
		t.Errorf("chose %v at %v, want last year's $20,000", c.Basis(), res.Value().Target())
	}
	// 70% of $40,000 is $28,000; 100% of $30,000 is $30,000.
	res = must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: prior(30_000*money.Dollar, 100_000*money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 28_000*money.Dollar {
		t.Errorf("chose %v at %v, want 70%% of this year's, $28,000", c.Basis(), res.Value().Target())
	}
	if o := res.Value().Options()[0]; o.Percent() != money.Percent(70) {
		t.Errorf("this year's option is %v, want 70%%", o.Percent())
	}

	f := must(estimated.NewFigures().Tax(5_000 * money.Dollar).FederalAGI(50_000 * money.Dollar).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(f).Build())
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   bool
	}{
		"no return on record": {nil, true},
		"not filed":           {&notFiled, false},
		"not required":        {&notRequired, false},
		"a six-month year":    {&short, false},
	} {
		res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: tc.prior}.facts(t)))
		if res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
		if res.Value().Target() != 28_000*money.Dollar {
			t.Errorf("%s: target %v, want $28,000", name, res.Value().Target())
		}
		if has(res.Required(), working.PositionPriorYearReturn) != tc.ask {
			t.Errorf("%s: asked for last year's return %v, want %v", name, !tc.ask, tc.ask)
		}
	}
}

// $40,000 of tax, nothing withheld or paid, no return last year: the target
// is 70%, $28,000, and each installment $7,000. Colorado's rate changes on
// January 1: 11% for 2026, 10% for 2027 (CDOR's table). Figured to April 15,
// 2027, each shortfall before January spans both:
//
//	April 15:     260 days to Dec 31 at 11%: 7,000 × 0.11 × 260 / 365 = 548.493 → $548.49
//	              105 days to Apr 15 at 10%: 7,000 × 0.10 × 105 / 365 = 201.370 → $201.37
//	June 15:      199 days at 11%: 419.808 → $419.81; 105 days at 10%: $201.37
//	September 15: 107 days at 11%: 225.726 → $225.73; 105 days at 10%: $201.37
//	January 15:    90 days at 10%: 7,000 × 0.10 × 90 / 365 = 172.603 → $172.60
//
// The total is $1,970.74.
func TestEstimatedPenaltyAcrossTwoRatePeriods(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), day(2027, 4, 15)))
	p := res.Value()
	want := [][]struct {
		days   int
		rate   money.Rate
		charge money.USCents
	}{
		{{260, money.Percent(11), 54_849}, {105, money.Percent(10), 20_137}},
		{{199, money.Percent(11), 41_981}, {105, money.Percent(10), 20_137}},
		{{107, money.Percent(11), 22_573}, {105, money.Percent(10), 20_137}},
		{{90, money.Percent(10), 17_260}},
	}
	lines := p.Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 4", len(lines))
	}
	for i, l := range lines {
		if l.Shortfall() != 7_000*money.Dollar {
			t.Errorf("line %d is on %v, want $7,000", i+1, l.Shortfall())
		}
		parts := l.Parts()
		if len(parts) != len(want[i]) {
			t.Errorf("line %d has %d parts, want %d", i+1, len(parts), len(want[i]))
			continue
		}
		for j, w := range want[i] {
			if parts[j].Days() != w.days || parts[j].Rate() != w.rate || parts[j].Charge() != w.charge {
				t.Errorf("line %d part %d: %d days at %v is %v; want %d days at %v, %v", i+1, j+1, parts[j].Days(), parts[j].Rate(), parts[j].Charge(), w.days, w.rate, w.charge)
			}
		}
	}
	if p.Total() != 197_074 {
		t.Errorf("penalty %v, want $1,970.74", p.Total())
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("the 2027 rate is published, but a gap is reported")
	}
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waivers are not asked about")
	}

	// A payment pays the oldest shortfall first. $7,000 on May 15 pays April:
	// 30 days at 11%, 7,000 × 0.11 × 30 / 365 = 63.288 → $63.29.
	h := household{tax: 40_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 5, 15), 7_000*money.Dollar)}}
	p = must(rules(t).Penalty(h.facts(t), day(2026, 6, 1))).Value()
	if l := p.Lines(); len(l) == 0 || l[0].Installment() != 1 || l[0].Total() != 6_329 || p.Total() != 6_329 {
		t.Errorf("penalty %v to June 1, want $63.29, all on April's", p.Total())
	}
}

// CDOR's interest example: $200 due April 15, 2022, paid February 22, 2024,
// at 3%, 5% and 8%, is $4.27, $10.00 and $2.32, $16.59 in all. It checks the
// method this package charges: simple interest, each calendar year at its
// own rate, divided by 365 or 366.
func TestInterestMatchesTheDepartmentsExample(t *testing.T) {
	mustContain(t, "cdor-penalties-and-interest.txt",
		"2022 | 3% | 6% |", "2023 | 5% | 8% |", "2024 | 8% | 11% |",
		"A total of $16.59 interest is calculated as follows:",
		"$4.27 of interest is calculated at an annual rate of 3% for the 260 days",
		"$10.00 of interest is calculated at an annual rate of 5% for the full year of 2023.",
		"$2.32 of interest is calculated at an annual rate of 8% for the 53 days",
	)
	page := "CDOR, Tax Topics: Penalties and Interest"
	s := must(estimated.NewRateSeries().
		Period(day(2022, 1, 1), day(2022, 12, 31), money.Percent(3), page).
		Period(day(2023, 1, 1), day(2023, 12, 31), money.Percent(5), page).
		Period(day(2024, 1, 1), day(2024, 12, 31), money.Percent(8), page).
		Build())
	parts, gap, err := estimated.SimpleDaily(200*money.Dollar, day(2022, 4, 15), day(2024, 2, 22), s)
	if err != nil || !gap.IsZero() {
		t.Fatalf("err %v, gap %v", err, gap)
	}
	want := []money.USCents{427, 1_000, 232}
	if len(parts) != len(want) {
		t.Fatalf("%d parts, want %d", len(parts), len(want))
	}
	var total money.USCents
	for i, p := range parts {
		if p.Charge() != want[i] {
			t.Errorf("part %d is %v, want %v", i+1, p.Charge(), want[i])
		}
		total += p.Charge()
	}
	if total != 1_659 {
		t.Errorf("total %v, want $16.59", total)
	}
}

// § 39-22-605(8)(a): withholding is paid in equal parts on the due dates,
// unless the filer establishes when it was actually withheld.
func TestEstimatedWithholding(t *testing.T) {
	// $8,000 withheld in December, against $7,000 a quarter.
	h := household{tax: 40_000 * money.Dollar, withheld: 8_000 * money.Dollar}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, in := range s {
		if want := money.USCents(i+1) * 2_000 * money.Dollar; in.CreditedBy() != want {
			t.Errorf("evenly: installment %d credited %v, want %v", i+1, in.CreditedBy(), want)
		}
	}
	h.asWithheld = true
	s = must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{0, 0, 0, 8_000 * money.Dollar} {
		if s[i].CreditedBy() != want {
			t.Errorf("as withheld: installment %d credited %v, want %v", i+1, s[i].CreditedBy(), want)
		}
	}
	// Last year's overpayment counts toward April.
	h = household{tax: 40_000 * money.Dollar, payments: []estimated.Payment{must(estimated.NewPayment().Amount(7_000 * money.Dollar).FromPriorYear().Build())}}
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].Status() != estimated.StatusMet {
		t.Errorf("April with last year's overpayment is %v", s[0].Status())
	}
}

// DR 0204 Part 4: Colorado taxable income to each period end, times 4, 2.4,
// 1.5 and 1, at 4.4%, times 17.5%, 35%, 52.5% and 70%, less earlier
// installments:
//
//	$10,000 × 4   = $40,000; × 4.4% = $1,760; × 17.5% = $308.00
//	$20,000 × 2.4 = $48,000; × 4.4% = $2,112; × 35%   = $739.20,   less $308.00 = $431.20
//	$60,000 × 1.5 = $90,000; × 4.4% = $3,960; × 52.5% = $2,079.00, less $739.20 = $1,339.80
//	$200,000 × 1;  × 4.4% = $8,800; × 70% = $6,160.00, less $2,079.00 = $4,081.00
func TestEstimatedAnnualisedInstallments(t *testing.T) {
	h := household{tax: 8_800 * money.Dollar, periods: []money.USCents{10_000 * money.Dollar, 20_000 * money.Dollar, 60_000 * money.Dollar, 200_000 * money.Dollar}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{30_800, 73_920, 207_900, 616_000} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v by its date, want %v", i+1, s[i].NeededBy(), want)
		}
	}
	// The form enters line 26 on line 7 as it is, without the federal cap at
	// the regular installment: $100,000 by March 31 needs $3,080 in April,
	// more than the regular quarter of $1,540.
	h.periods[0] = 100_000 * money.Dollar
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].NeededBy() != 308_000 {
		t.Errorf("April needs %v, want $3,080", s[0].NeededBy())
	}
	// Not annualising federally: the regular quarters.
	no := must(coloradotaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).FederalAnnualised(false).Build())
	h.extras = &no
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].NeededBy() != 154_000 {
		t.Errorf("without the federal election April needs %v, want $1,540", s[0].NeededBy())
	}
	// Not said: the election is honoured and the question asked.
	h.extras, h.noExtras = nil, true
	res := must(rules(t).Schedule(h.facts(t)))
	if res.Value()[0].NeededBy() != 308_000 || !strings.Contains(because(res.Required(), working.PositionJurisdictionFact), "annualised installments for federal income tax") {
		t.Errorf("unsaid: April needs %v; asked: %s", res.Value()[0].NeededBy(), because(res.Required(), working.PositionJurisdictionFact))
	}
	// The periods must be the form's.
	f := must(estimated.NewFacts(colorado, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(100).Build())).
		Periods(
			must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 6, 30)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).Build())
	if _, err := rules(t).Schedule(f); err == nil {
		t.Error("a period ending June 30 is accepted")
	}
}

// The exceptions: the $1,000 exemption (TestEstimatedThresholdBoundary), the
// fourth installment for a return paid by January 31
// (TestEstimatedDueDatesForTwentyTwentySix), and farmers (refused). The
// statute's no-liability exception and its waivers are not on DR 0204: they
// are reported and not applied.
func TestEstimatedExceptions(t *testing.T) {
	// No liability last year, a resident throughout, and no return because
	// none was required: reported, not applied. (A filed return with no tax
	// gives a prior-year target of nothing, so the exception matters only
	// without one.)
	zero := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	res := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, prior: &zero}.facts(t), day(2027, 4, 15)))
	if res.Value().Total() == 0 {
		t.Error("the no-liability exception was applied, though DR 0204 does not list it")
	}
	if !strings.Contains(applied(res.Applied()), "§ 39-22-605(7)(b): you had no Colorado liability for 2025") {
		t.Errorf("the no-liability exception is not reported: %s", applied(res.Applied()))
	}
	// A waiver asked for is not subtracted; a casualty is only good cause.
	casualty := estimated.WaiverCasualty
	res = must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, waiver: &casualty}.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 197_074 || res.Value().Waiver() != estimated.WaiverCasualty || has(res.Required(), working.PositionPenaltyWaiver) {
		t.Errorf("with a casualty waiver asked: %v, %v", res.Value().Total(), res.Value().Waiver())
	}
	if !strings.Contains(applied(res.Applied()), "no waiver for a casualty or disaster as such") {
		t.Error("the casualty request is not answered")
	}
	// The penalty stops on the day the return is paid, and by April 15.
	p := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, returnFiled: day(2027, 3, 1)}.facts(t), day(2027, 4, 15)))
	if p.Value().AsOf() != day(2027, 3, 1) {
		t.Errorf("figured to %v, want the day the return was paid", p.Value().AsOf())
	}
	p = must(rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), day(2027, 6, 30)))
	if p.Value().AsOf() != day(2027, 4, 15) || p.Value().Total() != 197_074 {
		t.Errorf("figured to %v at %v, want April 15, 2027 and $1,970.74", p.Value().AsOf(), p.Value().Total())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar}.facts(t)))
	want := map[string][4]string{
		"Tax of exactly $1,000 after withholding":          {"$1,000 or less", "Exception 2", "less than $1,000", "§ 39-22-605(7)(a)"},
		"The interest rates DR 0204 prints":                {"12% to December 31 and 11% after", "lines 15 and 18", "11% for 2026 and 10% for 2027", "39-21-110.5"},
		"No liability last year":                           {"isn't one of them", "Part 1", "resident throughout", "§ 39-22-605(7)(b)"},
		"Waivers for good cause, retirement or disability": {"no waiver", "DR 0204", "retired after 62", "§ 39-22-605(7)(c), (d)"},
		"Due dates on a weekend or holiday":                {"weekend or legal holiday", "DR 0104EP", "legal federal holiday", "§ 39-22-605(5)(c)"},
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
		// Nothing paid on $40,000 of tax: only the rate changes these figures.
		uses, affects := working.ReadingForm, false
		if d.Rule() == "The interest rates DR 0204 prints" {
			uses, affects = working.ReadingStatute, true
		}
		if d.Uses() != uses || d.AffectsHousehold() != affects {
			t.Errorf("%q uses %v and affects these facts %v; want %v and %v", d.Rule(), d.Uses(), d.AffectsHousehold(), uses, affects)
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
	if !affects(household{tax: 3_000 * money.Dollar, withheld: 2_000 * money.Dollar}, "Tax of exactly $1,000 after withholding") {
		t.Error("the $1,000 boundary does not affect exactly $1,000")
	}
	if affects(household{tax: 3_000 * money.Dollar, withheld: 2_000 * money.Dollar}, "The interest rates DR 0204 prints") {
		t.Error("the rates affect a household with no penalty")
	}
	zero := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	if !affects(household{tax: 40_000 * money.Dollar, prior: &zero}, "No liability last year") {
		t.Error("the no-liability exception does not affect a resident with no return required last year")
	}
	casualty := estimated.WaiverCasualty
	if !affects(household{tax: 40_000 * money.Dollar, waiver: &casualty}, "Waivers for good cause, retirement or disability") {
		t.Error("the waiver divergence does not affect a household that asks for one")
	}
	// The rates the figure uses are the statute's, not the form's.
	p := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), day(2027, 4, 15)))
	for _, l := range p.Value().Lines() {
		for _, part := range l.Parts() {
			if r := part.Rate(); r != money.Percent(11) && r != money.Percent(10) {
				t.Errorf("a part is charged %v", r)
			}
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := coloradotaxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(40_000 * money.Dollar).Build())
	base := func() *estimated.FactsBuilder {
		return estimated.NewFacts(colorado, 2026).FilingStatus(filing.Single).ThisYear(this)
	}
	priorFarmer := must(coloradotaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(true).Build())
	for name, f := range map[string]estimated.Facts{
		"a farmer":            must(base().FarmShare(7000).Build()),
		"a fisherman":         must(base().FishShare(7000).Build()),
		"farming and fishing": must(base().FarmShare(3400).FishShare(3300).Build()),
		"a farmer last year":  must(base().State(priorFarmer).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	if _, err := rules(t).Requirement(must(base().FarmShare(6666).Build())); err != nil {
		t.Errorf("66.66%% from farming: %v", err)
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(colorado, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(base().State("not Colorado's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// Last year's farming not said: asked for.
	res := must(rules(t).Requirement(must(base().Build())))
	if !strings.Contains(because(res.Required(), working.PositionJurisdictionFact), "farming or fishing") {
		t.Errorf("last year's farming is not asked about: %v", res.Required())
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"70%", "$150,000", "11% for 2026 and 10% for 2027", "once a year", "annualise federally", "17.5%, 35%, 52.5% and 70%", "Colorado residence", "good cause", "$1,000 or less", "January 31"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if e.VersusFederal() == "" || len(e.Sources()) == 0 {
		t.Error("the explanation has no comparison with federal or no sources")
	}
}

// Without this year's tax the target is last year's tax alone, and the
// penalty against it is an upper bound.
func TestEstimatedWithoutThisYearsTaxUsesLastYearsAlone(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000*money.Dollar, 100_000*money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 8_000*money.Dollar || r.Exempt() {
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
	for i, want := range []money.USCents{200_000, 400_000, 600_000, 800_000} {
		if got := sched.Value()[i].NeededBy(); got != want {
			t.Errorf("installment %d needs %v, want %v", i+1, got, want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !pen.Value().UpperBound() || pen.Value().Total() == 0 {
		t.Errorf("unpaid: penalty %v, upper bound %v", pen.Value().Total(), pen.Value().UpperBound())
	}
	paid := h
	paid.payments = quarterly(2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar)
	if pen := must(rules(t).Penalty(paid.facts(t), day(2027, 4, 15))); pen.Value().Total() != 0 {
		t.Errorf("paid to last year's tax: penalty %v", pen.Value().Total())
	}

	// With no return last year either, there is no target.
	none := household{noTax: true}
	res = must(rules(t).Requirement(none.facts(t)))
	if res.Value().TargetKnown() || res.Value().Required() {
		t.Errorf("no target expected: %s", res.Value().Why())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// An exception sets the target, so no question may say its answer changes
// the target. $500 of tax, with last year's return not given.
func TestEstimatedExceptionAsksNothingOfTheTarget(t *testing.T) {
	b := estimated.NewFacts(estimated.Jurisdiction("CO"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(500 * money.Dollar).Build()))
	res := must(must(coloradotaxcalc.EstimatedTax(2026)).Requirement(must(b.Build())))
	if !res.Value().Exempt() {
		t.Fatal("no exception applies")
	}
	for _, r := range res.Required() {
		if strings.Contains(strings.ToLower(r.Effect()), "target") {
			t.Errorf("%v: %q", r.Position(), r.Effect())
		}
	}
}

// The waiver asked for is decided from C.R.S. § 39-22-605(7)(c) and (d). The
// penalty stays the figure before any waiver, and IfWaived shows $0 only
// when the waiver is open to the facts.

// waiverPenalty is the penalty on $10,000 of tax with nothing paid or
// withheld, figured to Apr 15, 2027, with the waiver asked for as pos.
func waiverPenalty(t *testing.T, pos estimated.Positions) working.Result[estimated.Penalty] {
	t.Helper()
	f := must(estimated.NewFacts(estimated.Jurisdiction("CO"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Positions(pos).Build())
	return must(must(coloradotaxcalc.EstimatedTax(2026)).Penalty(f, day(2027, 4, 15)))
}

func waiverFor(c estimated.WaiverCause, on, born estimated.Date) estimated.Positions {
	return must(estimated.NewPositions().WaiverFor(c, on).Born(born).Build())
}

type waiverCase struct {
	name   string
	pos    estimated.Positions
	want   string // eligible, refused or undecided
	reason string // a phrase of the outcome's reason
}

// checkWaivers checks each case's outcome, and that the penalty is the one
// figured with no waiver asked for.
func checkWaivers(t *testing.T, cases []waiverCase) {
	t.Helper()
	before := waiverPenalty(t, must(estimated.NewPositions().Waiver(estimated.WaiverNone).Build())).Value().Total()
	if before == 0 {
		t.Fatal("no penalty arises, so no waiver is decided")
	}
	for _, c := range cases {
		p := waiverPenalty(t, c.pos).Value()
		o, ok := p.WaiverOutcome()
		if !ok {
			t.Errorf("%s: no waiver outcome", c.name)
			continue
		}
		got, ifWaived := "undecided", p.Total()
		switch {
		case o.Eligible():
			got, ifWaived = "eligible", 0
		case o.Refused():
			got = "refused"
		}
		if got != c.want || !strings.Contains(o.Reason(), c.reason) {
			t.Errorf("%s: %s, %q; want %s, mentioning %q", c.name, got, o.Reason(), c.want, c.reason)
		}
		if p.Total() != before || p.IfWaived() != ifWaived {
			t.Errorf("%s: penalty %v, %v if waived; want %v, %v if waived", c.name, p.Total(), p.IfWaived(), before, ifWaived)
		}
	}
}

// § 39-22-605(7)(d) is the federal retirement and disability waiver. There is
// no casualty waiver as such: (7)(c) waives for good cause instead.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "crs-39-22-605.txt",
		"to the extent the executive director determines that the underpayment was due to good cause shown by the taxpayer.",
		"(I) The taxpayer either retired after having attained age sixty-two or became disabled in the taxable year for which estimated payments were required to be made or in the taxable year preceding such taxable year; and (II) Such underpayment was due to reasonable cause and not to willful neglect.")
	checkWaivers(t, []waiverCase{
		{"retired in 2026 at 64", waiverFor(estimated.CauseRetirement, day(2026, 6, 1), day(1962, 3, 1)), "eligible", "The executive director needs"},
		{"retired in 2020", waiverFor(estimated.CauseRetirement, day(2020, 6, 1), day(1955, 3, 1)), "refused", "2025 or 2026"},
		{"disabled in 2025", waiverFor(estimated.CauseDisability, day(2025, 2, 1), estimated.Date{}), "eligible", "disabled in 2025"},
		// The difference from federal: good cause, not a casualty waiver.
		{"a casualty", waiverFor(estimated.CauseCasualty, day(2026, 8, 1), estimated.Date{}), "eligible", "good cause"},
		{"a disaster", waiverFor(estimated.CauseDisaster, day(2026, 8, 1), estimated.Date{}), "eligible", "no waiver for a casualty or disaster as such"},
	})
}
