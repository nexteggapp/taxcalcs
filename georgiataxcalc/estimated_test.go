// Georgia's estimated tax rules for 2026, checked against the Department of
// Revenue's forms and HB 463.
//
// Georgia publishes no exhaustive oracle for estimated tax. The strongest
// check is Form 500 UET's worked example of Exception 3, in which the fourth
// installment is met by its own period's payment although the year as a
// whole is short: inputs and conclusion both from the Department. Every
// threshold, percentage, date and rate is compared against its extract in
// testdata/. The boundaries, the other exceptions, the January 31 rule and
// the penalty arithmetic are hand-written cases.
package georgiataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/georgiataxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

var day = estimated.MustDate

const georgia = estimated.Jurisdiction("GA")

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
	return must(georgiataxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind, text string) bool {
	for _, r := range rs {
		if r.Position() == k && r.Asked() && (strings.Contains(r.Question(), text) || strings.Contains(r.Effect(), text)) {
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
		out = append(out, pay(dues[i], x))
	}
	return out
}

// period is one Form 500 UET computation period: the taxable income to its
// end, for Exception 3, and the tax on that income annualised, for
// Exception 2.
type period struct{ income, annualisedTax money.USCents }

type household struct {
	status      filing.Status
	tax         money.USCents // Form 500 UET line 3: tax after credits
	noTax       bool
	gross       money.USCents // zero means $100,000
	noGross     bool
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *georgiataxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	periods     []period
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
		gross := h.gross
		if gross == 0 {
			gross = 100_000 * money.Dollar
		}
		fb.GrossIncome(gross)
	}
	b := estimated.NewFacts(georgia, 2026).FilingStatus(status).ThisYear(must(fb.Build())).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(georgiataxcalc.NewEstimatedFacts().Dependents(0).Deductions(15_000 * money.Dollar).Build()))
	}
	if h.periods != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, p := range h.periods {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).Income(p.income).AnnualisedTax(p.annualisedTax).Build()))
		}
		b.Elections(must(estimated.NewElections().Annualised().Build()))
	}
	return must(b.Build())
}

// prior is a filed 12-month return with its tax and taxable income.
func prior(tax, taxable money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).TaxableIncome(taxable).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "form-500-uet.txt",
		"500 UET (Rev. 07/15/25)",
		"For tax years 2025 and later",
		"Enter 100% of the Immediately Preceding Year’s Tax (return must be for a 12-month period)",
		"Enter 70% of the Amount Shown on Line 3",
		"Divide amount on Line 4 by the number of installments required for the year",
		"Enter the lesser of Line 6 or Line 7 for each period",
		"Amounts paid on estimate for each period and tax withheld (withheld treated equally paid for each quarter)",
		"the excess of the total of the amounts on Line 9 of the previous columns over the total of the amounts on Line 8 of the previous columns. If no excess, enter zero.",
		"If any date shown falls on a Saturday, Sunday, or legal holiday, substitute the next regular workday.",
		"Exception 1. —Tax on prior years income using current year rates and exemptions",
		"estimated payments equal or exceed what would have been due on your prior",
		"year’s income if you had computed it at the current year’s rates.",
		"use the tax rate allowed for the current year but use",
		"the other facts and the law applicable to your prior year’s return.",
		"equal or exceed 70 percent of the tax on your annualized taxable income for periods from the first of the year to the end of the month preceding that in which an installment is due.",
		"282.00 (70% x 25% x $1,609.00)",
		"exceeds 90 percent of the tax on your taxable income for periods starting from the first of the year to the end of the month preceding that in which an installment is due.",
		"the law does not permit the use of exception 3 for the fourth installment period",
		"Date of payment or April 15, 20_____whichever is earlier",
		"Penalty (9 percent a year on amount shown on Line 17 for the number of days shown on Line 19)",
		"If you filed your return and paid the balance of tax by January 31st of the following year, consider the balance paid as of January 15th.",
		"your gross income from farming or fishing is at least two-thirds of your annual gross income",
	)
	mustContain(t, "form-500-uet-tables.txt",
		"Between Jan. 1 and March 31 April 15 4",
		"Between Apr. 1 and May 31 June 15 3",
		"Between June 1 and August 31 Sept. 15 2",
		"On or after September 1 Jan. 15 1",
	)
	mustContain(t, "form-500-es.txt",
		"reasonably expects to have gross income during the year which exceeds (1) applicable exemptions, plus (2) estimated deductions, plus (3) $1,000 of income not subject to withholding.",
		"in equal installments during this year on or before April 15, June 15, September 15, and the following January 15.",
		"If the due date falls on a weekend or holiday, the tax shall be due on the next day that is not a weekend or holiday.",
		"Single, ead of household, or Married filing separately................... $12,000",
		"Married filing jointly............................................................................$24,000",
		"EXEMPTION AMOUNT FOR TAX YEAR 2025 Dependent Exemption..................................................................$4,000",
		"500 ES (Rev. 05/12/25)",
	)
	mustContain(t, "hb-463-2026.txt",
		"of this Code section shall be 5.19 percent 4.99 percent for taxable years beginning on or",
		"after January 1, 2025 2026;",
		"a personal exemption in the amount of $4,000.00 $5,000.00 for each dependent",
		"In the case of a married couple filing a joint return, $24,000.00 $30,000.00;",
		"(ii) In the case of a single taxpayer, head of household, or married taxpayer filing a",
		"separate return, $12,000.00 $15,000.00;",
		"without such approval and shall be applicable to all taxable years beginning on or after",
		"January 1, 2026. H. B. 463",
	)
	mustContain(t, "governor-2026-05-11.txt",
		"May 11, 2026",
		"lowers Georgia's state income tax rate from 5.19% to 4.99%, beginning January 1, 2026.",
	)
}

// The 2026 dates are the 500-ES's April 15, June 15, September 15 and
// January 15. None falls on a weekend, so none moves. January 31, 2027, the
// Instruction F date, is a Sunday; the form does not move it.
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
	if wd := day(2027, 1, 31).Weekday(); wd != time.Sunday {
		t.Errorf("January 31, 2027 is a %v", wd)
	}
	// Filed on the Sunday itself, the rule applies; filed on Monday, February
	// 1, it does not, and the question is asked.
	paid := quarterly(1_750*money.Dollar, 1_750*money.Dollar, 1_750*money.Dollar)
	sunday := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, payments: paid, returnFiled: day(2027, 1, 31)}.facts(t)))
	if s := sunday.Value()[3]; s.Status() != estimated.StatusMet {
		t.Errorf("filed January 31: January is %v", s.Status())
	}
	monday := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, payments: paid, returnFiled: day(2027, 2, 1)}.facts(t), day(2027, 4, 15)))
	if !has(monday.Required(), working.PositionUnsettledRule, "Jan 31, 2027, a Sunday") {
		t.Errorf("filed February 1: the moved date is not asked about: %v", monday.Required())
	}
}

// Estimated tax is required when gross income exceeds the deductions, the
// dependent exemptions and $1,000. Equal to the sum is not over it.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	two := must(georgiataxcalc.NewEstimatedFacts().Dependents(2).Build())
	for _, tc := range []struct {
		name   string
		status filing.Status
		extras *georgiataxcalc.EstimatedFacts
		at     money.USCents // gross income equal to the threshold
	}{
		// $15,000 standard deduction, two dependents at $5,000, and $1,000.
		{"single, two dependents", filing.Single, &two, 26_000 * money.Dollar},
		{"head of household, two dependents", filing.HeadOfHousehold, &two, 26_000 * money.Dollar},
		{"separate, two dependents", filing.MarriedFilingSeparately, &two, 26_000 * money.Dollar},
		// $30,000 for a joint return.
		{"joint, two dependents", filing.MarriedFilingJointly, &two, 41_000 * money.Dollar},
	} {
		for _, gross := range []money.USCents{tc.at, tc.at + 1} {
			res := must(rules(t).Requirement(household{status: tc.status, tax: 2_000 * money.Dollar, gross: gross, extras: tc.extras}.facts(t)))
			if exempt := res.Value().Exempt(); exempt != (gross == tc.at) {
				t.Errorf("%s, gross %v: exempt %v (%s)", tc.name, gross, exempt, res.Value().Why())
			}
			if gross == tc.at {
				// The exception is chosen at $0, and the form's 2025 figures,
				// $5,000 lower or more, would have required payments.
				if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Gross income of "+tc.at.Whole()+" or less" {
					t.Errorf("%s: chosen %v %v %q", tc.name, c.Basis(), c.Amount(), c.Label())
				}
				if ds := res.Divergences(); len(ds) != 1 || !ds[0].AffectsHousehold() {
					t.Errorf("%s: the deduction divergence does not affect a household it decides", tc.name)
				}
			}
			// The deductions were not given, so the standard deduction is a
			// stated default.
			if !has(res.Required(), working.PositionJurisdictionFact, "standard deduction") {
				t.Errorf("%s: the deduction default is not reported", tc.name)
			}
		}
	}

	// A surviving spouse has no standard deduction of its own in the act.
	res := must(rules(t).Requirement(household{status: filing.SurvivingSpouse, tax: 2_000 * money.Dollar, gross: 16_000 * money.Dollar,
		extras: &georgiataxcalc.EstimatedFacts{}}.facts(t)))
	if !res.Value().Exempt() || !has(res.Required(), working.PositionJurisdictionFact, "uses the $15,000 single amount") {
		t.Errorf("surviving spouse at $16,000: exempt %v, asked %v", res.Value().Exempt(), res.Required())
	}

	// Without gross income the requirement is taken as met, and asked.
	res = must(rules(t).Requirement(household{tax: 2_000 * money.Dollar, noGross: true}.facts(t)))
	if res.Value().Exempt() || !has(res.Required(), working.PositionJurisdictionFact, "gross income") {
		t.Errorf("no gross income: exempt %v, asked %v", res.Value().Exempt(), res.Required())
	}
}

// The target is the smaller of 70% of this year's tax and 100% of last
// year's, whatever last year's income.
func TestEstimatedTargetOptions(t *testing.T) {
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(must(estimated.NewFigures().Tax(1).Build())).Build())
	notFiled := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	rich := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(6_000 * money.Dollar).FederalAGI(5_000_000 * money.Dollar).Build())).Build())
	for _, tc := range []struct {
		name   string
		prior  *estimated.PriorReturn
		target money.USCents
		basis  estimated.Basis
		open   bool
		kind   working.PositionKind
		asked  string
	}{
		{"70% is smaller", prior(8_000*money.Dollar, 0), 7_000 * money.Dollar, estimated.BasisThisYearTax, true, working.PositionUnsettledRule, "before or after credits"},
		{"100% is smaller", prior(6_000*money.Dollar, 0), 6_000 * money.Dollar, estimated.BasisPriorYearTax, true, working.PositionUnspecified, ""},
		{"no 110% at high income", &rich, 6_000 * money.Dollar, estimated.BasisPriorYearTax, true, working.PositionUnspecified, ""},
		{"no tax last year", prior(0, 0), 0, estimated.BasisPriorYearTax, true, working.PositionUnspecified, ""},
		{"no prior return", nil, 7_000 * money.Dollar, estimated.BasisThisYearTax, false, working.PositionPriorYearReturn, "Did you file a 2025 Georgia return"},
		{"none filed", &notFiled, 7_000 * money.Dollar, estimated.BasisThisYearTax, false, working.PositionUnsettledRule, "open with no 2025 Georgia return"},
		{"a short year", &short, 7_000 * money.Dollar, estimated.BasisThisYearTax, false, working.PositionUnspecified, ""},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		r := res.Value()
		c, _ := r.Chosen()
		if r.Target() != tc.target || c.Basis() != tc.basis {
			t.Errorf("%s: target %v on %v, want %v on %v", tc.name, r.Target(), c.Basis(), tc.target, tc.basis)
		}
		o := r.Options()[1]
		if o.Available() != tc.open || o.Percent() != money.Percent(100) {
			t.Errorf("%s: last year's option available %v at %v (%s)", tc.name, o.Available(), o.Percent(), o.WhyNot())
		}
		if r.Options()[0].Percent() != money.Percent(70) {
			t.Errorf("%s: this year's option is %v", tc.name, r.Options()[0].Percent())
		}
		if tc.asked != "" && !has(res.Required(), tc.kind, tc.asked) {
			t.Errorf("%s: %q is not asked: %v", tc.name, tc.asked, res.Required())
		}
	}
	// No tax last year: nothing is required, by the form's arithmetic.
	if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, prior: prior(0, 0)}.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("no tax last year: a penalty of %v", p.Value().Total())
	}
}

// A requirement first met later in the year is paid in fewer, larger
// installments, by Form 500 UET Instruction B's chart.
func TestEstimatedLateStartChart(t *testing.T) {
	for _, tc := range []struct {
		met    estimated.Date
		needed []money.USCents
	}{
		{day(2026, 3, 31), []money.USCents{175_000, 350_000, 525_000, 700_000}},
		// $7,000 in thirds: $2,333.33, $4,666.67 and $7,000.
		{day(2026, 4, 1), []money.USCents{0, 233_333, 466_667, 700_000}},
		{day(2026, 5, 31), []money.USCents{0, 233_333, 466_667, 700_000}},
		{day(2026, 6, 1), []money.USCents{0, 0, 350_000, 700_000}},
		{day(2026, 8, 31), []money.USCents{0, 0, 350_000, 700_000}},
		{day(2026, 9, 1), []money.USCents{0, 0, 0, 700_000}},
	} {
		res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, firstMet: tc.met}.facts(t)))
		for i, in := range res.Value() {
			if in.NeededBy() != tc.needed[i] {
				t.Errorf("first met %v: installment %d needs %v, want %v", tc.met, i+1, in.NeededBy(), tc.needed[i])
			}
		}
		// Nothing short before the first installment that applies.
		if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, firstMet: tc.met}.facts(t), day(2027, 4, 15))); len(p.Value().Lines()) != 4-countZero(tc.needed) {
			t.Errorf("first met %v: %d penalty lines", tc.met, len(p.Value().Lines()))
		}
	}
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	if !has(res.Required(), working.PositionRequirementFirstMet, "on or after Apr 1") {
		t.Errorf("the start date is not asked for: %v", res.Required())
	}
}

func countZero(xs []money.USCents) int {
	n := 0
	for _, x := range xs {
		if x == 0 {
			n++
		}
	}
	return n
}

// Nothing paid on $1,750 a quarter: 9% a year by the day to April 15, 2027.
// April's runs 365 days, $1,750 × 9% × 365 ÷ 365 = $157.50; June's 304 days,
// $131.18; September's 212, $91.48; January's 90, $38.84. Total $419.00.
//
// Georgia's rate is a fixed 9% with no rate periods, so no shortfall spans a
// change of rate. April's spans the year end into 2027 and is charged as one
// span at 9%, as Form 500 UET line 20 charges it.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 6, 30)))
	p := res.Value()
	if p.AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.AsOf())
	}
	wantDays := []int{365, 304, 212, 90}
	wantCharge := []money.USCents{15_750, 13_118, 9_148, 3_884}
	lines := p.Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	for i, l := range lines {
		parts := l.Parts()
		if l.Shortfall() != 1_750*money.Dollar || len(parts) != 1 || parts[0].Days() != wantDays[i] || parts[0].Charge() != wantCharge[i] || parts[0].Rate() != money.Percent(9) {
			t.Errorf("line %d: shortfall %v, parts %d; want one part of %d days, %v at 9%%", i+1, l.Shortfall(), len(parts), wantDays[i], wantCharge[i])
		}
	}
	if p.Total() != 41_900 {
		t.Errorf("total %v, want $419.00", p.Total())
	}
	if p.UpperBound() || has(res.Required(), working.PositionUnpublishedRate, "") {
		t.Error("a fixed rate leaves nothing unpublished and nothing bounded")
	}
}

// A payment counts first toward its own period's installment. $750 paid on
// May 1 to make up April's shortfall goes to June's installment instead, so
// April's $750 is not paid until the June 15 payment exceeds June's own
// $1,750: 61 days, $750 × 9% × 61 ÷ 365 = $11.28. Paying the oldest
// shortfall first, as Form 2210 does, would charge 16 days, $2.96.
func TestEstimatedPaymentsGoToTheirOwnPeriodFirst(t *testing.T) {
	payments := []estimated.Payment{
		pay(dues[0], 1_000*money.Dollar), pay(day(2026, 5, 1), 750*money.Dollar),
		pay(dues[1], 1_750*money.Dollar), pay(dues[2], 1_750*money.Dollar), pay(dues[3], 1_750*money.Dollar),
	}
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, payments: payments}.facts(t), day(2027, 4, 15)))
	lines := res.Value().Lines()
	if len(lines) != 1 || lines[0].Installment() != 1 || lines[0].Shortfall() != 750*money.Dollar {
		t.Fatalf("lines %v, want April's $750 alone", lines)
	}
	parts := lines[0].Parts()
	if len(parts) != 1 || parts[0].To() != dues[1] || parts[0].Charge() != 1_128 || res.Value().Total() != 1_128 {
		t.Errorf("April's part %+v, total %v; want 61 days to June 15, $11.28", parts, res.Value().Total())
	}
	if !has(res.Required(), working.PositionUnsettledRule, "pays off an earlier shortfall") {
		t.Errorf("which payment pays an earlier shortfall is not asked: %v", res.Required())
	}
}

// Form 500 UET's example of Exception 3, on a joint return with $779 of tax:
// $109, $182, $291 and $436 paid by the four dates, against 90% of the tax on
// $0, $1,800 and $5,800 of income to date. Each of the first three
// installments is excused, and the fourth is met by its own $145, though the
// year is $109.30 short of 70% of $779. The form's period taxes are at 2025's
// 5.19%; at 2026's 4.99% the 90% figures are $0, $80.84 and $260.48, still
// under what was paid, so the conclusion is the same.
func TestEstimatedMatchesForm500UETExample(t *testing.T) {
	mustContain(t, "form-500-uet-tables.txt",
		"Jan. 1 to March 31 $0.00 $-0- $-0- $109.00",
		"Jan. 1 to May 31 $1,800.00 $93.00 $84.00 $182.00",
		"Jan. 1 to Aug. 31 $5,800.00 $301.00 $271.00 $291.00",
		"Jan. 1 to Dec. 31 $15,000.00 $779.00 $436.00")
	mustContain(t, "form-500-uet.txt",
		"there is not a penalty for the first three installment periods",
		"no penalty is owed for that period because there is no underpayment. The 145.00 tax withheld for that period (436.00 minus 291.00 shown in column 5 above) is more than 70 percent of the tax that would have been due for the fourth installment")
	// Exception 2 is put out of reach with a large annualised tax.
	h := household{
		status:   filing.MarriedFilingJointly,
		tax:      779 * money.Dollar,
		payments: quarterly(109*money.Dollar, 73*money.Dollar, 109*money.Dollar, 145*money.Dollar),
		periods: []period{{0, 1_000_000 * money.Dollar}, {1_800 * money.Dollar, 1_000_000 * money.Dollar},
			{5_800 * money.Dollar, 1_000_000 * money.Dollar}, {15_000 * money.Dollar, 1_000_000 * money.Dollar}},
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	// 70% of $779 is $545.30, in quarters of $136.33, $136.32, $136.33 and
	// $136.32. January's own $145 covers its own installment, so it has no
	// shortfall, but the $436 paid by then is under the $545.30 needed, so on
	// the running totals it is short; no exception covers January.
	want := []estimated.Status{estimated.StatusExcused, estimated.StatusExcused, estimated.StatusExcused, estimated.StatusShort}
	wantShort := []money.USCents{2_733, 6_332, 2_733, 0}
	for i, in := range sched.Value() {
		if in.Status() != want[i] || in.Shortfall() != wantShort[i] {
			t.Errorf("installment %d is %v, %v short; want %v, %v short", i+1, in.Status(), in.Shortfall(), want[i], wantShort[i])
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v where the example owes none", p.Value().Total())
	}
	// Without Exception 3 the first three are charged, and January is still
	// met by its own payment.
	h.periods = nil
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if n := len(p.Value().Lines()); n != 3 || p.Value().Lines()[2].Installment() != 3 {
		t.Errorf("%d lines without Exception 3, want April, June and September", n)
	}
}

// Exception 1: last year's $120,000 of taxable income at 4.99% is $5,988, a
// quarter of which is $1,497. Paying $1,497 in April excuses April's $253
// shortfall against $1,750. Paying $1,490 does not: April's $260 is charged
// for 365 days, $260 × 9% = $23.40, while June to January, each past $1,497
// a quarter so far, are excused.
func TestEstimatedExceptionOne(t *testing.T) {
	last := prior(8_000*money.Dollar, 120_000*money.Dollar)
	h := household{tax: 10_000 * money.Dollar, prior: last, payments: quarterly(149_700, 1_503*money.Dollar, 1_500*money.Dollar, 1_500*money.Dollar)}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, in := range sched.Value() {
		if in.Status() != estimated.StatusExcused || in.Shortfall() == 0 {
			t.Errorf("installment %d is %v with %v short; want excused with a shortfall", i+1, in.Status(), in.Shortfall())
		}
	}
	if !has(sched.Required(), working.PositionUnsettledRule, "income at current rates and exemptions") {
		t.Errorf("Exception 1's reading is not asked: %v", sched.Required())
	}

	h.payments = quarterly(1_490*money.Dollar, 1_510*money.Dollar, 1_500*money.Dollar, 1_500*money.Dollar)
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if lines := p.Value().Lines(); len(lines) != 1 || lines[0].Installment() != 1 || lines[0].Shortfall() != 260*money.Dollar || p.Value().Total() != 2_340 {
		t.Errorf("penalty %v over %v; want $23.40 on April's $260", p.Value().Total(), lines)
	}

	// Without last year's taxable income the exception is not applied.
	noIncome := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(8_000 * money.Dollar).Build())).Build())
	h.prior = &noIncome
	if res := must(rules(t).Requirement(h.facts(t))); !has(res.Required(), working.PositionPriorYearReturn, "What was your 2025 Georgia taxable income?") {
		t.Errorf("the missing income is not asked: %v", res.Required())
	}
}

// Exception 2 is 70% of the annualised tax, by a quarter for each
// installment so far: on $4,000 a period, $700, $1,400, $2,100 and $2,800.
// Exception 3 is 90% of 4.99% of the income to date: on $20,000, $898.20.
func TestEstimatedExceptionsTwoAndThree(t *testing.T) {
	big := 10_000_000 * money.Dollar
	four := 4_000 * money.Dollar
	h := household{tax: 10_000 * money.Dollar, payments: quarterly(700*money.Dollar, 700*money.Dollar, 700*money.Dollar, 700*money.Dollar),
		periods: []period{{big, four}, {big, four}, {big, four}, {big, four}}}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, in := range sched.Value() {
		if in.Status() != estimated.StatusExcused {
			t.Errorf("Exception 2: installment %d is %v, want excused", i+1, in.Status())
		}
	}
	if !has(sched.Required(), working.PositionUnsettledRule, "25% for the first, then 50%, 75% and 100%") {
		t.Errorf("the later shares of Exception 2 are not asked: %v", sched.Required())
	}
	h.payments = quarterly(69_999, 700*money.Dollar, 700*money.Dollar, 700*money.Dollar)
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].Status() != estimated.StatusShort {
		t.Errorf("a cent under Exception 2, April is %v", s[0].Status())
	}

	for _, tc := range []struct {
		paid    money.USCents
		excused bool
	}{{89_820, true}, {89_819, false}} {
		h := household{tax: 10_000 * money.Dollar, payments: quarterly(tc.paid),
			periods: []period{{20_000 * money.Dollar, big}, {big, big}, {big, big}, {big, big}}}
		if got := must(rules(t).Schedule(h.facts(t))).Value()[0].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("Exception 3 with %v paid: excused %v, want %v", tc.paid, got, tc.excused)
		}
	}
	// Exception 3 never excuses January: income of nothing to date would
	// excuse every earlier installment.
	h = household{tax: 10_000 * money.Dollar, periods: []period{{0, big}, {0, big}, {0, big}, {0, big}}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	if s[2].Status() != estimated.StatusExcused || s[3].Status() != estimated.StatusShort {
		t.Errorf("September is %v and January %v; want excused and short", s[2].Status(), s[3].Status())
	}
	// The periods must be Form 500 UET's.
	bad := must(estimated.NewFacts(georgia, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(1).Build())).
		Periods(must(estimated.NewPeriod().End(day(2026, 3, 30)).Build()), must(estimated.NewPeriod().End(day(2026, 5, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).Build())
	if _, err := rules(t).Requirement(bad); err == nil {
		t.Error("a period ending March 30 was accepted")
	}
}

// Instruction F: the balance paid with a return filed by January 31 counts as
// paid on January 15. With nothing paid at all and the return filed January
// 20, every shortfall stops on January 15: April's $1,750 for 275 days,
// $118.66; June's 214, $92.34; September's 122, $52.64; January's none.
// Filed February 10 instead, January's $1,750 runs 26 days, $11.22.
func TestEstimatedReturnFiledByJanuary31(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 1, 20)}.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 26_364 || len(res.Value().Lines()) != 3 {
		t.Errorf("filed January 20: penalty %v over %d lines, want $263.64 over three", res.Value().Total(), len(res.Value().Lines()))
	}
	if res.Value().AsOf() != day(2027, 1, 20) {
		t.Errorf("figured to %v, want the filing day", res.Value().AsOf())
	}
	paid := quarterly(1_750*money.Dollar, 1_750*money.Dollar, 1_750*money.Dollar)
	late := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, payments: paid, returnFiled: day(2027, 2, 10)}.facts(t), day(2027, 4, 15)))
	if late.Value().Total() != 1_122 {
		t.Errorf("filed February 10: penalty %v, want $11.22", late.Value().Total())
	}
	monday := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, payments: paid, returnFiled: day(2027, 2, 1)}.facts(t), day(2027, 4, 15)))
	// 17 days: $1,750 × 9% × 17 ÷ 365 = $7.34.
	if monday.Value().Total() != 734 {
		t.Errorf("filed February 1: penalty %v, want $7.34", monday.Value().Total())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	ds := res.Divergences()
	if len(ds) != 1 {
		t.Fatalf("%d divergences, want one", len(ds))
	}
	d := ds[0]
	// The figures follow the act. This household's gross income is over the
	// threshold on either set of figures.
	if d.Uses() != working.ReadingStatute || d.AffectsHousehold() {
		t.Errorf("uses %v, affects the household %v; want the statute, and false", d.Uses(), d.AffectsHousehold())
	}
	if d.Rule() != "The deductions in the test of whether estimated tax is required" ||
		!strings.Contains(d.Form(), "$12,000") || !strings.Contains(d.Form(), "$4,000") || !strings.Contains(d.Form(), "out of date") ||
		!strings.Contains(d.FormSource(), "Rev. 05/12/25") ||
		!strings.Contains(d.Statute(), "$15,000") || !strings.Contains(d.Statute(), "$30,000") || !strings.Contains(d.Statute(), "$5,000") ||
		!strings.Contains(d.Statute(), "May 11, 2026") || !strings.Contains(d.StatuteSource(), "HB 463") {
		t.Errorf("the divergence does not pin both readings: %q (%s); %q (%s)", d.Form(), d.FormSource(), d.Statute(), d.StatuteSource())
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := georgiataxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for _, share := range []struct{ farm, fish money.Rate }{{7_000, 0}, {0, 7_000}, {4_000, 3_000}} {
		f := must(estimated.NewFacts(georgia, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(share.farm).FishShare(share.fish).Build())
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("farming %v, fishing %v: %v, want ErrUnsupported", share.farm, share.fish, err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(georgia, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(georgia, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Georgia's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := georgiataxcalc.NewEstimatedFacts().Dependents(-1).Build(); err == nil {
		t.Error("a negative count of dependents was accepted")
	}
	if _, err := rules(t).Penalty(household{tax: 1}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date was figured")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"70% of this year's tax", "100% of last year's", "no 110% tier", "9% a year", "4.99%", "gross income", "$1,000", "$15,000", "$5,000",
		"first toward its own period's installment", "January 31", "no casualty, retirement or disability waiver"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Without this year's tax, the target is 100% of last year's, and the
// penalty against it is an upper bound. With no prior return either, there
// is no target.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	// Last year's $200,000 of taxable income puts Exception 1 out of reach.
	h := household{noTax: true, prior: prior(8_000*money.Dollar, 200_000*money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 8_000*money.Dollar {
		t.Errorf("required %v, target %v; want $8,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option: available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax, "") {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, in := range sched.Value() {
		if want := money.USCents(i+1) * 2_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !p.Value().UpperBound() || p.Value().Total() == 0 {
		t.Errorf("penalty %v, upper bound %v; want a bound above zero", p.Value().Total(), p.Value().UpperBound())
	}

	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() || r.Required() {
		t.Errorf("no prior return: target known %v, required %v", r.TargetKnown(), r.Required())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// An exception sets the target, so no question may say its answer changes
// the target. $1,000 of gross income, with this year's tax and last year's
// return not given.
func TestEstimatedExceptionAsksNothingOfTheTarget(t *testing.T) {
	b := estimated.NewFacts(estimated.Jurisdiction("GA"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().GrossIncome(1_000 * money.Dollar).Build()))
	res := must(must(georgiataxcalc.EstimatedTax(2026)).Requirement(must(b.Build())))
	if !res.Value().Exempt() {
		t.Fatal("no exception applies")
	}
	for _, r := range res.Required() {
		if strings.Contains(strings.ToLower(r.Effect()), "target") {
			t.Errorf("%v: %q", r.Position(), r.Effect())
		}
	}
}

// The waiver asked for is decided from Form 500 UET, which has none, and the
// Department's disaster relief by announcement. The penalty stays the figure
// before any waiver, and IfWaived shows $0 only when the waiver is open to
// the facts.

// waiverPenalty is the penalty on $10,000 of tax with nothing paid or
// withheld, figured to Apr 15, 2027, with the waiver asked for as pos.
func waiverPenalty(t *testing.T, pos estimated.Positions) working.Result[estimated.Penalty] {
	t.Helper()
	f := must(estimated.NewFacts(estimated.Jurisdiction("GA"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Positions(pos).Build())
	return must(must(georgiataxcalc.EstimatedTax(2026)).Penalty(f, day(2027, 4, 15)))
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

// Georgia has no waiver for a casualty, retirement or disability, so those
// are refused, including a retirement the federal waiver would cover. A
// disaster is left open, because the Department postpones due dates for one
// only by announcement.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	checkWaivers(t, []waiverCase{
		{"retired in 2026 at 64", waiverFor(estimated.CauseRetirement, day(2026, 6, 1), day(1962, 3, 1)), "refused", "no waiver of the penalty for retirement or disability"},
		{"retired in 2020", waiverFor(estimated.CauseRetirement, day(2020, 6, 1), day(1955, 3, 1)), "refused", "retirement or disability"},
		{"disabled in 2026", waiverFor(estimated.CauseDisability, day(2026, 2, 1), estimated.Date{}), "refused", "retirement or disability"},
		{"a casualty", waiverFor(estimated.CauseCasualty, day(2026, 8, 1), estimated.Date{}), "refused", "for a casualty"},
		{"a disaster", waiverFor(estimated.CauseDisaster, day(2026, 8, 1), estimated.Date{}), "undecided", "announces relief"},
	})
	res := waiverPenalty(t, waiverFor(estimated.CauseDisaster, day(2026, 8, 1), estimated.Date{}))
	if !has(res.Required(), working.PositionUnsettledRule, "Department announces it") {
		t.Error("a disaster: the open point, the Department's announcement, is not asked about")
	}
	res = waiverPenalty(t, waiverFor(estimated.CauseCasualty, day(2026, 8, 1), estimated.Date{}))
	if has(res.Required(), working.PositionUnsettledRule, "Department announces it") {
		t.Error("a casualty: refused, yet the disaster announcement is asked about")
	}
}
