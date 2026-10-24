// Kansas's estimated tax rules for 2026, checked against the Department of
// Revenue's forms and the Kansas statutes.
//
// Kansas publishes no exhaustive oracle and no worked example with amounts.
// The strongest checks are Schedule K-210's own: the days it precomputes for
// each column (61, 92, 107 and 15) and its example of a late payment (13
// days). Every threshold, share, date and rate is compared against its
// extract in testdata/. The exceptions, the boundaries and the penalty
// amounts are hand-written cases.
package kansastaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/kansastaxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const ks = estimated.Jurisdiction("KS")

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
	return must(kansastaxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
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

type household struct {
	tax         money.USCents // Form K-40 line 18
	noTax       bool
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *kansastaxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	annualised  []money.USCents // the annualised tax for each period
	asWithheld  bool
	waiver      *estimated.WaiverKind
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts(ks, 2026).FilingStatus(filing.Single).ThisYear(figures).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	}
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
	return must(b.Build())
}

// prior is a filed return covering months months with its tax.
func prior(tax money.USCents, months int) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Months(months).
		Figures(must(estimated.NewFigures().Tax(tax).Build())).Build())
	return &p
}

func notFiled() *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(false).Build())
	return &p
}

func notRequired() *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	return &p
}

func extrasOf(b *kansastaxcalc.EstimatedFactsBuilder) *kansastaxcalc.EstimatedFacts {
	e := must(b.Build())
	return &e
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "k-40es-2026.txt",
		"You must make estimated tax payments if your estimated Kansas income tax after withholding and credits is $500 or more and your withholding and credits may be less than the smaller of: a) 90% of the tax on your tax return for tax year 2026; or b) 100% of the tax shown on your 2025 tax return.",
		"Nonresidents should only consider income from Kansas sources for meeting these conditions.",
		"(April 15, 2026, June 15, 2026, September 15, 2026 and January 15, 2027 unless your return for the 2026 tax year is filed and the tax fully paid by January 31, 2027.",
		"Farmers and Fishers – Payment is due on or before January 15, 2027 unless your return for the 2026 tax year is filed and tax fully paid on or before March 1, 2027.",
		"When the due date falls on a Saturday, Sunday, or legal holiday, substitute the next regular workday.",
		"are at least 90% (66 2/3% for farmers and fishers) of the tax shown on your return for tax year 2026, OR",
		"equal or exceed 100% of the tax shown on your 2025 return, (the return must cover a 12-month period with a tax liability), OR",
		"equal or exceed 90% of the tax shown on your annualized income for the periods January 1, 2026 to March 31, May 31, August 31 and December 31, 2026.",
	)
	mustContain(t, "k-40es-2026-worksheet.txt",
		"If the amount is less than $500, estimated",
		"9. Amount of each quarterly payment (enter 1/4 of line 8 here and on Voucher 1)",
		"If you are beginning estimated payments after April 15, but on or before: June 15 enter 1/3 of line 8; September 15 enter 1/2 of line 8; January 15 enter the total amount on line 8.",
	)
	mustContain(t, "k-210-2025-form.txt",
		"1. Amount from line 18, 2025 Form K-40",
		"2. Multiply line 1 by 90% (farmers and fishers multiply by 66 2/3%)",
		"3. Prior year’s tax liability (from line 19, 2024 Form K-40)",
		"25% of line 4 50% of line 4 75% of line 4 100% of line 4 5. Cumulative total of your 2025 withholding",
		"7. Cumulative total of the credit for taxes paid on the K-120S 25% of tax 50% of tax 75% of tax 100% of tax (line 25 of the K-40)",
		"and credit for taxes paid on K-120S (add lines 5, 6, and 7)",
		"25% of line 2 or 3 50% of line 2 or 3 75% of line 2 or 3 100% of line 2 or 3 whichever is less",
		"22.5% of tax 45% of tax 67.5% of tax 90% of tax",
		"Enter the sum of line 9 less line 8, line 10a less line 8, or, line 10b less line 8",
		"12 4/15/25 6/15/25 9/15/25 1/15/26 12. Due date of each installment",
		"the 61 92 107 13 due date of the next installment or 12/31/25, whichever is earlier.",
		"14. Number of days from 1/15/26 to date paid or 4/15/26, whichever 15 is earlier.",
		"15. Line 13 X 9% X amount on line 11",
		"X 8% X amount on line 11",
	)
	mustContain(t, "k-210-2025-instructions.txt",
		"If your 2025 tax due (line 18 of Form K-40, less withholding and tax credits (excluding estimated tax payments made) is $500 or more, you may be subject to an underpayment of estimated tax penalty",
		"Taxpayers (other than farmers or fishers) are not required to make a payment for the January 15th quarter if a Form K-40 was filed and the tax was paid in full on or before January 31, 2026.",
		"If at least two-thirds of your annual gross income is from farming or fishing",
		"If you did not file an income tax return for the prior tax year, or if you did file a return but your income tax balance (line 19, Form K-40) was zero, then enter zero on line 3 of this schedule.",
		"LINE 5: Multiply the amount on line 4 by the percentage shown in each column of line 5.",
		"LINE 7: Multiply the amount from line 25 of the K-40 by the percentage shown in each column of line 7.",
		"January 1 – March 31 January 1 – May 31 January 1 – August 31 January 1 – December 31",
		"Multiply income by 4 Multiply income by 2.4 Multiply income by 1.5 Multiply income by 1",
		"Enclose a schedule showing your computation of annualized income and tax amounts.",
		"LINE 11: Enter on line 11 the amount of underpayment of tax, which is the lesser of one of the following computations:",
		"EXAMPLE: If you paid the 6/15/25 installment on 6/28/25 the number of days to enter on line 13, column 2 will be computed from 6/15/25 to 6/28/25, which equals 13 days.",
		"Penalty is computed to 12/31/25 at 9% and from 1/1/26 to the date the tax was paid or 4/15/26, whichever is earlier, at 8%.",
	)
	mustContain(t, "k-40-2025-lines-18-to-31.txt",
		"LINE 18 (Total tax balance): Subtract line 17 from line 16",
		"LINE 25 (Credit for taxes paid on the K-120S)",
		"compute interest at .6667% per month or 8% per annum (or portion thereof)",
		"An estimated tax penalty may be due if the total of your withholding (line 19 and tax credits lines 22 and 23) subtracted from line 18 is $500 or more.",
	)
	mustContain(t, "ksa-79-32-101.txt",
		"tax of at least $500 and who expects their withholding and credits to be less than (A) 90% of the tax shown on the return for the current year or (B) 100% of the tax shown for the preceding tax year",
		"(c) If on or before January 31",
		"(d) An individual or corporation with a taxable year of less than 12 months shall make a declaration for less than 12 months as prescribed by rules and regulations of the secretary of revenue.",
	)
	mustContain(t, "ksa-79-32-102.txt",
		"(1) after April 1 and before June 2 of the taxable year, the declaration shall be filed on or before June 15 of the taxable year, or (2) after June 1 and before September 2 of the taxable year, the declaration shall be filed on or before September 15, or (3) after September 1 of the taxable year the declaration shall be filed on or before January 15 of the subsequent taxable year",
	)
	mustContain(t, "ksa-79-32-103.txt",
		"the estimated tax shall be paid in four equal installments",
		"the estimated tax shall be paid in three equal installments",
		"the estimated tax shall be paid in two equal installments",
		"the estimated tax shall be paid in full at the time of filing the declaration",
	)
	mustContain(t, "ksa-79-32-107.txt",
		"until the 15th day of the fourth month following the close of the taxable year for which such estimated tax is a credit, whichever date is earlier, but such penalty shall not be added if the total amount thereof does not exceed $1.",
		"(1) The tax shown on the return of the individual for the preceding taxable year, if a return showing a liability for tax was filed by the individual for the preceding taxable year;",
		"(2) zero if no return was required to be filed or if the tax liability on the individual's return was less than $200 for the preceding taxable year;",
		"may waive or reduce any of such penalties",
	)
	mustContain(t, "ksa-79-2968.txt",
		"which rate is in effect thereunder on July 1 of the year immediately preceding the calendar year for which the rate is being annually fixed hereunder, plus one percentage point",
	)
}

// The forms are followed because neither predates the law: K.S.A.
// 79-32,107 was last amended effective July 1, 2024, the forms were revised
// in August and September 2025, and the 2026 composite listing shows no
// change to any section these rules use.
func TestEstimatedFormsPostdateTheLaw(t *testing.T) {
	mustContain(t, "ksa-79-32-107.txt", "L. 2024, ch. 81, § 17; July 1.")
	mustContain(t, "form-revisions.txt",
		"Kansas 2026 Individual Estimated Tax - K-40ES - Revised 8-26-25",
		"K-210 Underpayment of Individual Estimated Tax Rev. 9-15-25")
	mustContain(t, "composite-2026-chapter-79.txt", "79-32,110 (Supp.) Am 2029 23 155 7/1/2026")
	listing := extract(t, "composite-2026-chapter-79.txt")
	for _, s := range []string{"79-32,101", "79-32,102", "79-32,103", "79-32,107", "79-2968"} {
		if strings.Contains(listing, s) {
			t.Errorf("the 2026 composite listing changes %s", s)
		}
	}
}

// The 2026 dates are the ones the K-40ES prints, and none moves.
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
}

// $500 or more of tax after withholding and credits brings the rules in;
// $499.99 does not. Refundable credits count against it, and so does the
// credit for tax paid on the K-120S.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		withheld money.USCents
		extras   *kansastaxcalc.EstimatedFacts
		exempt   bool
	}{
		{"$499.99 after withholding", 950_001, nil, true},
		{"$500.00 after withholding", 950_000, nil, false},
		{"$499.99 after refundable credits", 900_000, extrasOf(kansastaxcalc.NewEstimatedFacts().RefundableCredits(50_001)), true},
		{"$500.00 after refundable credits", 900_000, extrasOf(kansastaxcalc.NewEstimatedFacts().RefundableCredits(50_000)), false},
		{"$499.99 after the K-120S credit", 900_000, extrasOf(kansastaxcalc.NewEstimatedFacts().PassThroughCredit(50_001)), true},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld, extras: tc.extras}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", tc.name, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		// Under the threshold the exception is the chosen option, at $0.
		c, ok := res.Value().Chosen()
		if tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding and credits under $500") {
			t.Errorf("%s: chosen %v %v %q", tc.name, c.Basis(), c.Amount(), c.Label())
		}
	}
}

// The target is the smaller of 90% of this year's tax and 100% of last
// year's, with no 110% tier, and each is unavailable for a stated reason.
func TestEstimatedTargetOptions(t *testing.T) {
	ten := 10_000 * money.Dollar
	for _, tc := range []struct {
		name   string
		prior  *estimated.PriorReturn
		extras *kansastaxcalc.EstimatedFacts
		target money.USCents
		chosen estimated.Basis
		ask    working.PositionKind
	}{
		{"last year is less", prior(8_000*money.Dollar, 12), nil, 8_000 * money.Dollar, estimated.BasisPriorYearTax, working.PositionUnspecified},
		{"this year is less", prior(9_500*money.Dollar, 12), nil, 9_000 * money.Dollar, estimated.BasisThisYearTax, working.PositionUnspecified},
		{"a cent apart", prior(899_999, 12), nil, 899_999, estimated.BasisPriorYearTax, working.PositionUnspecified},
		{"no return on record", nil, nil, 9_000 * money.Dollar, estimated.BasisThisYearTax, working.PositionPriorYearReturn},
		{"a short year, not answered", prior(5_000*money.Dollar, 6), nil, 9_000 * money.Dollar, estimated.BasisThisYearTax, working.PositionUnsettledRule},
		{"a short year, not used", prior(5_000*money.Dollar, 6), extrasOf(kansastaxcalc.NewEstimatedFacts().ShortPriorYear(false)), 9_000 * money.Dollar, estimated.BasisThisYearTax, working.PositionUnspecified},
		{"a short year, used", prior(5_000*money.Dollar, 6), extrasOf(kansastaxcalc.NewEstimatedFacts().ShortPriorYear(true)), 5_000 * money.Dollar, estimated.BasisPriorYearTax, working.PositionUnspecified},
	} {
		res := must(rules(t).Requirement(household{tax: ten, prior: tc.prior, extras: tc.extras}.facts(t)))
		r := res.Value()
		c, _ := r.Chosen()
		if r.Target() != tc.target || c.Basis() != tc.chosen {
			t.Errorf("%s: target %v on %v, want %v on %v", tc.name, r.Target(), c.Basis(), tc.target, tc.chosen)
		}
		if got := has(res.Required(), working.PositionPriorYearReturn); got != (tc.ask == working.PositionPriorYearReturn) {
			t.Errorf("%s: last year's return asked %v", tc.name, got)
		}
		if got := has(res.Required(), working.PositionUnsettledRule); got != (tc.ask == working.PositionUnsettledRule) {
			t.Errorf("%s: the 12-month reading reported %v", tc.name, got)
		}
		if p := r.Options()[1]; p.Percent() != money.Percent(100) {
			t.Errorf("%s: the prior-year test is %v", tc.name, p.Percent())
		}
	}
	short := must(rules(t).Requirement(household{tax: ten, prior: prior(5_000*money.Dollar, 6)}.facts(t)))
	if o := short.Value().Options()[1]; o.Available() || !strings.Contains(o.WhyNot(), "12-month") {
		t.Errorf("a 6-month year: available %v, %q", o.Available(), o.WhyNot())
	}
	if !short.Value().Required() || !strings.Contains(short.Value().Why(), "$9,000") {
		t.Errorf("$10,000 of tax and no withholding: %s", short.Value().Why())
	}
}

// Schedule K-210 takes last year's figure as zero when no return was filed
// or it showed no tax, whatever the months, so no penalty arises. A small
// liability is used as it is.
func TestEstimatedNoPriorYearLiability(t *testing.T) {
	for name, p := range map[string]*estimated.PriorReturn{
		"not filed":         notFiled(),
		"not required":      notRequired(),
		"no tax":            prior(0, 12),
		"no tax, 6 months":  prior(0, 6),
		"not filed, unpaid": notFiled(),
	} {
		f := household{tax: 10_000 * money.Dollar, prior: p}.facts(t)
		res := must(rules(t).Requirement(f))
		if !res.Value().Exempt() || res.Value().Required() {
			t.Errorf("%s: exempt %v, required %v", name, res.Value().Exempt(), res.Value().Required())
		}
		label := "No Kansas return filed last year"
		if p.Filed() {
			label = "No Kansas tax last year"
		}
		if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisException || c.Label() != label || c.Amount() != 0 {
			t.Errorf("%s: chosen %v %q at %v", name, c.Basis(), c.Label(), c.Amount())
		}
		pen := must(rules(t).Penalty(f, day(2027, 4, 15)))
		if pen.Value().Total() != 0 || len(pen.Value().Lines()) != 0 || pen.Value().Reason() != label+"." {
			t.Errorf("%s: a penalty of %v because %q", name, pen.Value().Total(), pen.Value().Reason())
		}
	}
	// $150 of tax last year: the form uses it; the statute would use zero.
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(150*money.Dollar, 12)}.facts(t)))
	if res.Value().Exempt() || res.Value().Target() != 150*money.Dollar {
		t.Errorf("$150 last year: exempt %v, target %v; want $150", res.Value().Exempt(), res.Value().Target())
	}
}

// Schedule K-210 line 13 precomputes 61, 92 and 107 days, and line 14 the 15
// days in January; 2026's dates fall on the same days of the year, so the
// columns run the same spans. Nothing paid on a $4,000 target: $1,000 short
// for 61 days at 8% is $13.37, $2,000 for 92 days $40.33, and $3,000 for 107
// days $70.36.
func TestEstimatedPenaltyColumnsMatchK210Days(t *testing.T) {
	f := household{tax: 10_000 * money.Dollar, prior: prior(4_000*money.Dollar, 12)}.facts(t)
	res := must(rules(t).Penalty(f, day(2027, 4, 15)))
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	wantShort := []money.USCents{1_000 * money.Dollar, 2_000 * money.Dollar, 3_000 * money.Dollar, 4_000 * money.Dollar}
	wantDays := []int{61, 92, 107 + 15, 90}
	for i, l := range lines {
		if l.Shortfall() != wantShort[i] || l.Days() != wantDays[i] {
			t.Errorf("column %d: %v short for %d days, want %v for %d", i+1, l.Shortfall(), l.Days(), wantShort[i], wantDays[i])
		}
	}
	wantCharge := []money.USCents{1_337, 4_033, 7_036}
	for i, want := range wantCharge {
		parts := lines[i].Parts()
		if len(parts) != 1 || parts[0].Charge() != want || parts[0].Rate() != money.Percent(8) {
			t.Errorf("column %d: parts %d, want one charging %v at 8%%", i+1, len(parts), want)
		}
	}
	if res.Value().Total() != 12_406 {
		t.Errorf("total %v, want $124.06", res.Value().Total())
	}
	if !has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("the 2027 days are not reported as unrated")
	}
}

// The published 2026 rate is one 8% for the whole calendar year, so there is
// no change of rate inside 2026 to test. The third column runs across the
// period boundary at December 31: its 107 days of 2026 are charged at 8%,
// and its 15 days of 2027 are reported as having no published rate.
func TestEstimatedPenaltyAcrossTheRatePeriodBoundary(t *testing.T) {
	f := household{tax: 10_000 * money.Dollar, prior: prior(4_000*money.Dollar, 12),
		payments: quarterly(1_000*money.Dollar, 1_000*money.Dollar)}.facts(t)
	for _, tc := range []struct {
		asOf    estimated.Date
		days    int
		unrated bool
	}{{day(2026, 12, 31), 107, false}, {day(2027, 1, 15), 122, true}} {
		res := must(rules(t).Penalty(f, tc.asOf))
		lines := res.Value().Lines()
		if len(lines) != 1 || lines[0].Installment() != 3 || lines[0].Days() != tc.days {
			t.Fatalf("to %v: %d lines; want the third column for %d days", tc.asOf, len(lines), tc.days)
		}
		parts := lines[0].Parts()
		if len(parts) != 1 || parts[0].Days() != 107 || parts[0].To() != day(2026, 12, 31) || parts[0].Charge() != 2_345 {
			t.Errorf("to %v: parts %d; want one of 107 days to December 31, $1,000 at 8%%, $23.45", tc.asOf, len(parts))
		}
		if got := has(res.Required(), working.PositionUnpublishedRate); got != tc.unrated {
			t.Errorf("to %v: unpublished rate reported %v", tc.asOf, got)
		}
	}
}

// A late payment ends a column's charge on the day it is paid, as K-210's
// example has June's installment paid on June 28 for 13 days. A part payment
// lowers the charge from that day.
func TestEstimatedLatePaymentShortensTheColumn(t *testing.T) {
	last := prior(4_000*money.Dollar, 12)
	thousand := 1_000 * money.Dollar
	late := household{tax: 10_000 * money.Dollar, prior: last, payments: []estimated.Payment{
		pay(dues[0], thousand), pay(day(2026, 6, 28), thousand), pay(dues[2], thousand), pay(dues[3], thousand)}}
	res := must(rules(t).Penalty(late.facts(t), day(2027, 4, 15)))
	lines := res.Value().Lines()
	// $1,000 × 13 × 8% ÷ 365 is $2.85.
	if len(lines) != 1 || lines[0].Installment() != 2 || lines[0].Days() != 13 || res.Value().Total() != 285 {
		t.Errorf("June paid on June 28: %d lines, total %v; want the second column for 13 days, $2.85", len(lines), res.Value().Total())
	}
	sched := must(rules(t).Schedule(late.facts(t))).Value()
	if sched[1].Status() != estimated.StatusShort || sched[1].Shortfall() != thousand || sched[2].Status() != estimated.StatusMet {
		t.Errorf("June %v %v, September %v", sched[1].Status(), sched[1].Shortfall(), sched[2].Status())
	}

	part := late
	part.payments = []estimated.Payment{pay(dues[0], thousand), pay(day(2026, 6, 28), 400*money.Dollar),
		pay(day(2026, 7, 15), 600*money.Dollar), pay(dues[2], thousand), pay(dues[3], thousand)}
	res = must(rules(t).Penalty(part.facts(t), day(2027, 4, 15)))
	parts := res.Value().Lines()[0].Parts()
	// $2.85 for 13 days on $1,000, then $600 × 17 × 8% ÷ 365, $2.24.
	if len(parts) != 2 || parts[1].Principal() != 600*money.Dollar || parts[1].Days() != 17 || res.Value().Total() != 509 {
		t.Errorf("June paid in two parts: %d parts, total %v; want $5.09", len(parts), res.Value().Total())
	}

	// June unpaid until September: the second column runs its whole 92 days,
	// $20.16, and the third column is met.
	catchUp := late
	catchUp.payments = []estimated.Payment{pay(dues[0], thousand), pay(dues[2], 2*thousand), pay(dues[3], thousand)}
	res = must(rules(t).Penalty(catchUp.facts(t), day(2027, 4, 15)))
	if lines := res.Value().Lines(); len(lines) != 1 || lines[0].Days() != 92 || res.Value().Total() != 2_016 {
		t.Errorf("June caught up in September: total %v, want $20.16", res.Value().Total())
	}
}

// Withholding and the K-120S credit count a quarter on each date, however
// late they were paid. Kansas has no election to count withholding when it
// was withheld. Last year's overpayment counts on April 15, which no Kansas
// document states, so it is asked.
func TestEstimatedCreditsAreSpreadEvenly(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, withheld: 4_000 * money.Dollar, asWithheld: true,
		extras:   extrasOf(kansastaxcalc.NewEstimatedFacts().PassThroughCredit(400 * money.Dollar)),
		payments: []estimated.Payment{must(estimated.NewPayment().FromPriorYear().Amount(500 * money.Dollar).Build())}}
	res := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{160_000, 270_000, 380_000, 490_000} {
		if got := res.Value()[i].CreditedBy(); got != want {
			t.Errorf("installment %d credited %v, want %v", i+1, got, want)
		}
	}
	if !has(res.Required(), working.PositionUnsettledRule) {
		t.Error("the date last year's overpayment counts is not reported")
	}
}

// Under the annualised method each column needs the lesser of the regular
// amount and 22.5%, 45%, 67.5% or 90% of the period's annualised tax.
func TestEstimatedAnnualisedTakesTheLesserColumn(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, prior: prior(4_000*money.Dollar, 12),
		annualised: []money.USCents{2_000 * money.Dollar, 3_000 * money.Dollar, 4_000 * money.Dollar, 10_000 * money.Dollar},
		payments:   quarterly(450*money.Dollar, 900*money.Dollar, 1_350*money.Dollar, 1_300*money.Dollar)}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{45_000, 135_000, 270_000, 400_000} {
		in := sched.Value()[i]
		if in.NeededBy() != want || in.Status() != estimated.StatusMet {
			t.Errorf("installment %d needs %v and is %v; want %v and met", i+1, in.NeededBy(), in.Status(), want)
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v when every column met the annualised amount", p.Value().Total())
	}
	h.annualised = nil
	if p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31))); p.Value().Total() == 0 {
		t.Error("without the annualised method the same payments owe nothing")
	}
	bad := must(estimated.NewFacts(ks, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(100).Build())).
		Periods(must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 6, 30)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).Build())
	if _, err := rules(t).Requirement(bad); err == nil {
		t.Error("a period ending June 30 was accepted")
	}
}

// Filing and paying by January 31 excuses the January installment. January
// 31, 2027 is a Sunday; no Kansas document says whether it moves, so it is
// not moved and a return filed on Monday, February 1 asks.
func TestEstimatedFileAndPayByJanuary31(t *testing.T) {
	moves := extrasOf(kansastaxcalc.NewEstimatedFacts().JanuaryThirtyFirstMoves(true))
	for _, tc := range []struct {
		filed   estimated.Date
		extras  *kansastaxcalc.EstimatedFacts
		excused bool
		asked   bool
	}{
		{day(2027, 1, 29), nil, true, false},
		{day(2027, 1, 31), nil, true, false},
		{day(2027, 2, 1), nil, false, true},
		{day(2027, 2, 1), moves, true, false},
		{day(2027, 2, 2), moves, false, false},
	} {
		res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: tc.filed, extras: tc.extras}.facts(t)))
		jan := res.Value()[3]
		if got := jan.Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
		if tc.excused && jan.NeededBy() != res.Value()[2].NeededBy() {
			t.Errorf("filed %v: January needs %v, want September's %v", tc.filed, jan.NeededBy(), res.Value()[2].NeededBy())
		}
		if got := has(res.Required(), working.PositionUnsettledRule); got != tc.asked {
			t.Errorf("filed %v: asked %v, want %v", tc.filed, got, tc.asked)
		}
	}
	p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 3, 1)}.facts(t), day(2027, 4, 15)))
	if p.Value().AsOf() != day(2027, 3, 1) {
		t.Errorf("figured to %v, want the day the return was paid", p.Value().AsOf())
	}
	// K.S.A. 79-32,107(a): never beyond the 15th day of the fourth month.
	p = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 6, 30)))
	if p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
}

// A late starter is measured against a quarter by every date, as Schedule
// K-210 prints, and asked. With the statute's installments taken, nothing is
// needed before the group's first date and the rest is in equal parts; the
// groups start April 2, June 2 and September 2.
func TestEstimatedLateStart(t *testing.T) {
	statute := extrasOf(kansastaxcalc.NewEstimatedFacts().LateStartInstallments(true))
	h := household{tax: 10_000 * money.Dollar, firstMet: day(2026, 7, 1),
		payments: quarterly(0, 0, 4_500*money.Dollar, 4_500*money.Dollar)}
	res := must(rules(t).Schedule(h.facts(t)))
	if res.Value()[0].NeededBy() != 2_250*money.Dollar || !has(res.Required(), working.PositionUnsettledRule) {
		t.Errorf("by default April needs %v, asked %v; want $2,250 and asked", res.Value()[0].NeededBy(), has(res.Required(), working.PositionUnsettledRule))
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31))); p.Value().Total() == 0 {
		t.Error("by default the late starter owes nothing for April and June")
	}
	h.extras = statute
	res = must(rules(t).Schedule(h.facts(t)))
	if has(res.Required(), working.PositionUnsettledRule) || has(res.Required(), working.PositionRequirementFirstMet) {
		t.Error("asked after answering")
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v under the statute's installments", p.Value().Total())
	}

	for _, tc := range []struct {
		met  estimated.Date
		want []money.USCents
	}{
		{day(2026, 4, 1), []money.USCents{225_000, 450_000, 675_000, 900_000}},
		{day(2026, 4, 2), []money.USCents{0, 300_000, 600_000, 900_000}},
		{day(2026, 6, 1), []money.USCents{0, 300_000, 600_000, 900_000}},
		{day(2026, 6, 2), []money.USCents{0, 0, 450_000, 900_000}},
		{day(2026, 9, 1), []money.USCents{0, 0, 450_000, 900_000}},
		{day(2026, 9, 2), []money.USCents{0, 0, 0, 900_000}},
	} {
		s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, firstMet: tc.met, extras: statute}.facts(t))).Value()
		for i, want := range tc.want {
			if s[i].NeededBy() != want {
				t.Errorf("first met %v: installment %d needs %v, want %v", tc.met, i+1, s[i].NeededBy(), want)
			}
		}
	}
	// A third of last year's $100.00 rounds to $33.33 and two thirds to
	// $66.67.
	s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, prior: prior(100*money.Dollar, 12), firstMet: day(2026, 5, 1), extras: statute}.facts(t))).Value()
	if s[1].NeededBy() != 3_333 || s[2].NeededBy() != 6_667 || s[3].NeededBy() != 10_000 {
		t.Errorf("thirds of $100.00: %v, %v, %v", s[1].NeededBy(), s[2].NeededBy(), s[3].NeededBy())
	}
}

// The penalty is before any waiver, and the statute's $1 floor is reported
// rather than applied.
func TestEstimatedWaiverAndTheDollarFloor(t *testing.T) {
	last := prior(4_000*money.Dollar, 12)
	// April $40 short for 61 days: $0.53.
	small := household{tax: 10_000 * money.Dollar, prior: last,
		payments: quarterly(960*money.Dollar, 1_040*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar)}
	res := must(rules(t).Penalty(small.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 53 {
		t.Errorf("penalty %v, want $0.53", res.Value().Total())
	}
	floor := false
	for _, r := range res.Applied() {
		if r.Cite() == "K.S.A. 79-32,107(a)" && strings.Contains(r.Applied(), "does not exceed $1.00") {
			floor = true
		}
	}
	if !floor {
		t.Error("the $1 floor is not reported")
	}
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked")
	}
	none := estimated.WaiverNone
	small.waiver = &none
	if res := must(rules(t).Penalty(small.facts(t), day(2027, 4, 15))); has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is asked after it was answered")
	}
	casualty := estimated.WaiverCasualty
	small.waiver = &casualty
	res = must(rules(t).Penalty(small.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 53 || res.Value().Waiver() != estimated.WaiverCasualty {
		t.Errorf("with a casualty waiver asked: %v, %v", res.Value().Total(), res.Value().Waiver())
	}
}

// Kansas publishes no casualty, disaster, retirement or disability waiver,
// where federal law has all four, and § 79-32,107(g) doesn't say whether its
// reasonable-cause waiver reaches this penalty. A waiver asked for is
// undecided, the open point is asked, and the penalty is in full.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "ksa-79-32-107.txt", "failure of the taxpayer to comply with the provisions of subsections (a), (e) or (f) was due to reasonable causes")
	h := household{tax: 10_000 * money.Dollar, prior: prior(4_000*money.Dollar, 12)}
	base := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value().Total()
	if base == 0 {
		t.Fatal("no penalty to waive")
	}
	for name, pos := range map[string]estimated.Positions{
		"retired in 2026 at 64": must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build()),
		"a disaster":            must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()),
	} {
		b := estimated.NewFacts(ks, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(h.tax).Build())).
			PriorYear(*h.prior).Elections(must(estimated.NewElections().Build())).Positions(pos)
		res := must(rules(t).Penalty(must(b.Build()), day(2027, 4, 15)))
		p := res.Value()
		o, ok := p.WaiverOutcome()
		unsettled := false
		for _, q := range res.Required() {
			unsettled = unsettled || (q.Position() == working.PositionUnsettledRule && strings.Contains(q.Effect(), "reasonable cause"))
		}
		if p.Total() != base || !ok || !o.Undecided() || !strings.Contains(o.Reason(), "no casualty, disaster, retirement or disability waiver") || p.IfWaived() != base || !unsettled {
			t.Errorf("%s: penalty %v of %v, outcome %v %q, open point asked %v", name, p.Total(), base, ok, o.Reason(), unsettled)
		}
	}
}

// Under an exception the target is $0, so neither return is asked for as a
// way to change it. Without one, last year's return is.
func TestEstimatedTargetQuestionsOnlyWhenTheTargetIsOpen(t *testing.T) {
	under := must(rules(t).Requirement(household{tax: 400 * money.Dollar}.facts(t)))
	if !under.Value().Exempt() || has(under.Required(), working.PositionPriorYearReturn) {
		t.Errorf("under $500: exempt %v, asked %v", under.Value().Exempt(), has(under.Required(), working.PositionPriorYearReturn))
	}
	noTax := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(0).Build())).Build())
	none := must(rules(t).Requirement(household{noTax: true, prior: &noTax}.facts(t)))
	if !none.Value().Exempt() || has(none.Required(), working.PositionThisYearTax) {
		t.Errorf("no tax last year: exempt %v, this year's tax asked %v", none.Value().Exempt(), has(none.Required(), working.PositionThisYearTax))
	}
	over := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	if !has(over.Required(), working.PositionPriorYearReturn) {
		t.Error("over $500: last year's return is not asked for")
	}
}

// Each divergence is reported with both readings and both sources, follows
// the form, and affects none of these facts: a filed 12-month return with
// $4,000 of tax, the requirement met from the start, and a penalty over $1.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, prior: prior(4_000*money.Dollar, 12)}.facts(t), day(2027, 4, 15)))
	want := map[string][4]string{
		"No return filed last year":         {"no Kansas return", "K-210", "only if you weren't required to file", "79-32,107(b)(1) and (b)(2)"},
		"Last year's tax under $200":        {"however small", "line 3", "less than $200", "79-32,107(b)(2)"},
		"A return for less than 12 months":  {"12-month return", "K-40ES", "no 12-month condition", "79-32,101(d)"},
		"Needing to pay only after April 1": {"25%, 50%, 75% and 100%", "worksheet line 9", "three equal payments", "79-32,103(a)(2) to (4)"},
		"A penalty of $1 or less":           {"no minimum", "lines 17 and 18", "does not exceed $1", "79-32,107(a)"},
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
		if d.Uses() != working.ReadingForm || d.AffectsHousehold() {
			t.Errorf("%q uses %v, affects %v; want the form, and no effect", d.Rule(), d.Uses(), d.AffectsHousehold())
		}
	}

	// Each one affects the facts it turns on, and the statute is used when
	// the position taken says so.
	affects := func(res working.Result[estimated.Penalty], rule string) working.Divergence {
		for _, d := range res.Divergences() {
			if d.Rule() == rule {
				return d
			}
		}
		t.Fatalf("no divergence %q", rule)
		return working.Divergence{}
	}
	asOf := day(2027, 4, 15)
	for _, tc := range []struct {
		rule string
		h    household
		uses working.Reading
	}{
		{"No return filed last year", household{tax: 10_000 * money.Dollar, prior: notFiled()}, working.ReadingForm},
		{"Last year's tax under $200", household{tax: 10_000 * money.Dollar, prior: prior(150*money.Dollar, 12)}, working.ReadingForm},
		{"A return for less than 12 months", household{tax: 10_000 * money.Dollar, prior: prior(5_000*money.Dollar, 6),
			extras: extrasOf(kansastaxcalc.NewEstimatedFacts().ShortPriorYear(true))}, working.ReadingStatute},
		{"Needing to pay only after April 1", household{tax: 10_000 * money.Dollar, firstMet: day(2026, 7, 1)}, working.ReadingForm},
		{"A penalty of $1 or less", household{tax: 10_000 * money.Dollar, prior: prior(4_000*money.Dollar, 12),
			payments: quarterly(960*money.Dollar, 1_040*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar)}, working.ReadingForm},
	} {
		d := affects(must(rules(t).Penalty(tc.h.facts(t), asOf)), tc.rule)
		if !d.AffectsHousehold() || d.Uses() != tc.uses {
			t.Errorf("%q: affects %v, uses %v; want it to affect, using %v", tc.rule, d.AffectsHousehold(), d.Uses(), tc.uses)
		}
	}
	if d := affects(must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, prior: notRequired()}.facts(t), asOf)), "No return filed last year"); d.AffectsHousehold() {
		t.Error("no return was required, and the readings agree, yet the divergence affects it")
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := kansastaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"a farmer": must(estimated.NewFacts(ks, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(7000).Build()),
		"a fisher": must(estimated.NewFacts(ks, 2026).FilingStatus(filing.Single).ThisYear(this).FishShare(7000).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(ks, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(ks, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Kansas's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := kansastaxcalc.NewEstimatedFacts().RefundableCredits(-1).Build(); err == nil {
		t.Error("a negative credit was accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	for _, s := range []string{"$500", "90%", "100%", "no 110% tier", "as zero", "8% for 2026", "January 31", "Schedule K-210"} {
		if !strings.Contains(e.Summary()+strings.Join(e.Body(), " ")+e.VersusFederal(), s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Without this year's tax the target is last year's tax alone, the penalty
// is an upper bound, and with no prior-year figure there is no target. No
// liability last year still exempts, exactly.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(4_000*money.Dollar, 12)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 4_000*money.Dollar || !r.TargetKnown() {
		t.Errorf("required %v, target %v: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Error("this year's tax is not asked for")
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{100_000, 200_000, 300_000, 400_000} {
		if got := sched.Value()[i].NeededBy(); got != want {
			t.Errorf("installment %d needs %v, want %v", i+1, got, want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if pen.Value().Total() != 12_406 || !pen.Value().UpperBound() {
		t.Errorf("penalty %v, upper bound %v; want $124.06 at most", pen.Value().Total(), pen.Value().UpperBound())
	}

	none := household{noTax: true}
	if res := must(rules(t).Requirement(none.facts(t))); res.Value().TargetKnown() {
		t.Errorf("with no tax and no prior return: %s", res.Value().Why())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}

	exempt := household{noTax: true, prior: notFiled()}
	p := must(rules(t).Penalty(exempt.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 0 || p.Value().UpperBound() {
		t.Errorf("no return last year: penalty %v, upper bound %v; want an exact zero", p.Value().Total(), p.Value().UpperBound())
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("KS"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}
