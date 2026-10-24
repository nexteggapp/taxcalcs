// Maryland's estimated tax rules for 2026, checked against the
// Comptroller's own documents and the Tax-General Article.
//
// Maryland publishes no exhaustive oracle and no worked example of the
// interest. The only worked figures are Form 502UP's line 14 example, $500 a
// quarter entered as $500, $1,000, $1,500 and $2,000, which
// TestEstimatedCreditsAreCumulativeAsLine14Says reproduces. Every threshold,
// percentage, date and rate is compared against its extract in testdata/.
// The interest is checked by hand-worked cases at the published 2026 and 2027
// rates. The 2026 Form 502UP, whose printed factors would be the strongest
// check, is not published.
package marylandtaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/marylandtaxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

var day = estimated.MustDate

const md = estimated.Jurisdiction("MD")

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
	return must(marylandtaxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind, phrase string) bool {
	for _, r := range rs {
		if r.Position() == k && strings.Contains(r.Question()+" "+r.Effect(), phrase) {
			return true
		}
	}
	return false
}

func applied(rs []working.Rule, phrase string) bool {
	for _, r := range rs {
		if strings.Contains(r.Applied(), phrase) {
			return true
		}
	}
	return false
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

func withheld(on estimated.Date, a money.USCents) estimated.Withheld {
	return must(estimated.NewWithheld().On(on).Amount(a).Build())
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
	tax         money.USCents // Form 502UP line 7
	local       money.USCents
	noTax       bool
	prior       *estimated.PriorReturn
	withholding []estimated.Withheld
	payments    []estimated.Payment
	extras      *marylandtaxcalc.EstimatedFacts
	returnFiled estimated.Date
	periods     []money.USCents // income to each period end, Form 502UP line 11
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	fb := estimated.NewFigures()
	if !h.noTax {
		fb.Tax(h.tax)
	}
	if h.local > 0 {
		fb.LocalTax(h.local)
	}
	b := estimated.NewFacts(md, 2026).FilingStatus(filing.Single).ThisYear(must(fb.Build())).
		Withheld(h.withholding...).Paid(h.payments...).ReturnFiled(h.returnFiled)
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(marylandtaxcalc.NewEstimatedFacts().TaxableByAnotherState(false).Build()))
	}
	if h.periods != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, inc := range h.periods {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).Income(inc).Build()))
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

// filed is a filed prior return with its tax, Form 502UP line 9a.
func filed(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "pvw-2026.txt",
		"develop a tax of more than $500 in excess of your Maryland withholding.",
		"You must pay at least onefourth of the total estimated tax on Line 15 of this form on or before April 15, 2026. The remaining quarterly payments are due June 15, 2026, September 15, 2026 and January 15, 2027.",
		"you may file and pay the balance of tax due on or before January 31, 2027.",
		"you must pay at least 110% of your prior year tax to avoid interest for underpayment of estimated tax.",
		"If any due date falls on a Saturday, Sunday or legal holiday, the return and payment must be filed by the next business day.",
		"Your 2026 declaration and full payment of the estimated tax are due on or before January 15, 2027.",
		"The worksheet is designed to develop an estimate of your 2026 Maryland and local income tax.",
	)
	mustContain(t, "502up-2025.txt",
		"A. The tax liability on gross income after deducting Maryland withholding is $500 or less",
		"(1) 110% of last year’s taxes, or (2) 90% of the current year’s taxes.",
		"Multiply Line 7 by 90% (.90).",
		"b. Multiply Line 9a by 110% (1.10).",
		"If first-time filer, enter Line 8",
		"enter one-fourth of Line 1 in column 1, one-half of Line 1 in column 2, three-fourths of Line 1 in column 3 and an amount equal to Line 1 in column 4.",
		"You may elect to report your income based on the actual dates of receipt.",
		"Enter in each column the sum of any estimated payments you have made and any Maryland tax withheld from the beginning of the year to the due date at the top of that column.",
		"each successive entry will include the amount from the prior columns. For example, if you paid $500 in estimated payments each quarter, the first column will contain $500, the second $1,000, the third $1,500, and the fourth $2,000.",
		"count the number of months from the due date of April 15, 2025 that the payment was late. Treat a partial month as one month. Divide that number by twelve and multiply it by the 11.4825% annual rate.",
		"For payments made on or later than December 15, 2025, apply the interest factor of 0.0381",
		"but paid earlier than April 15, 2026,",
		"Interest is computed at a rate of 0.9011% per month after",
		"If at least 90% of your taxable income is taxable by another state, and the underpayment is not greater than the local tax, no interest is due",
		"If your gross income from farming or fishing is at least two-thirds of your total estimated gross income for the year",
		"Line 34 of 2024 Form 502 (reduced by any credits on Line 9 of Part CC of 2024 Form 502CR)",
	)
	mustContain(t, "502up-2025-columns.txt",
		"DUE DATES OF INSTALLMENTS April 15, 2025 June 15, 2025 Sept 15, 2025 Jan 15, 2026",
		"INSTALLMENT PERIODS Jan 1 to Mar 31 Jan 1 to May 31 Jan 1 to Aug 31 Jan 1 to Dec 31",
		"16. 0.0192 0.0289 0.0381 0.0266",
	)
	mustContain(t, "booklet-2025.txt",
		"Interest is due at the rate of 10.8133% annually or 0.9011% per month for any month or part of a month that a tax is paid after the original due date of the 2025 return but before January 1, 2027.",
		"State calculations are rounded to the nearest cent.",
		"a. you owe less than $500 tax on income that is not subject to Maryland withholding;",
		"for calendar year 2025 if the underpayment is the result of the liability created by the tax rate changes.",
	)
	mustContain(t, "md-register-53-18.txt",
		"Issue Date: September 4, 2026",
		"For the 2027 calendar year, the annual interest rate on refunds and moneys owed to the State will be 9.995%.",
	)
	mustContain(t, "tax-general.txt",
		"falls on a Saturday, Sunday, or legal holiday, performance of the act is considered timely if the act is performed on the next succeeding day that is not a Saturday, Sunday, or legal holiday.",
		"to exceed one-half the amount specified in § 6654(e)(1) of the Internal Revenue Code shall file a declaration of estimated income tax.",
		"an initial declaration on or before April 15 of that year; and (ii) a quarterly estimated income tax return on or before June 15, September 15, and January 15",
		"If an individual elects to pay the income tax in full on or before January 31, instead of filing the quarterly estimated income tax return required on January 15",
		"on or before April 15th of the next taxable year",
		"that is at least 2/3 of the total estimated income for a taxable year",
		"at least 25% of the estimated income tax shown on the declaration",
		"assess interest on unpaid tax from the due date to the date on which the tax is paid",
		"less than 90% of the tax required to be shown on the return for the current taxable year; and (ii) less than 110% of the tax paid for the prior taxable year, reduced by the credit allowed under § 10–703 of this article.",
		"if the amount of unpaid income tax is less than one-half the amount specified in § 6654(e)(1)",
		"The rate of interest for each month or fraction of a month is the percent equal to one–twelfth of the annual interest rate that the Comptroller sets for the calendar year",
		"For reasonable cause, a tax collector may waive interest on unpaid tax.",
		"a tax collector shall assess a penalty not exceeding 25% of the amount underestimated",
	)
}

// The 2026 dates are the ones the worksheet prints, and none moves. The
// January 31 file-and-pay date is a Sunday in 2027.
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
		t.Fatalf("January 31, 2027 is a %v", wd)
	}
}

// § 1-201(b) moves January 31, 2027 to Monday, February 1: a return filed
// and paid that day takes the place of the January installment, which then
// needs nothing beyond September's and is met, and one filed the next day
// does not.
func TestEstimatedJanuaryThirtyFirstMovesToFebruaryFirst(t *testing.T) {
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 1, 29), true}, {day(2027, 2, 1), true}, {day(2027, 2, 2), false}} {
		h := household{tax: 10_000 * money.Dollar, returnFiled: tc.filed, payments: quarterly(2_250*money.Dollar, 2_250*money.Dollar, 2_250*money.Dollar)}
		s := must(rules(t).Schedule(h.facts(t))).Value()
		if got := s[3].Status() == estimated.StatusMet && s[3].NeededBy() == s[2].NeededBy(); got != tc.excused {
			t.Errorf("filed %v: January %v needing %v; replaced %v, want %v", tc.filed, s[3].Status(), s[3].NeededBy(), got, tc.excused)
		}
		if !tc.excused && s[3].Status() != estimated.StatusShort {
			t.Errorf("filed %v: January is %v, want short", tc.filed, s[3].Status())
		}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
		if res := must(rules(t).Requirement(h.facts(t))); tc.excused && !applied(res.Applied(), "by January 31, 2027 (February 1, 2027,") {
			t.Errorf("filed %v: the rule does not name January 31 moved to February 1: %v", tc.filed, res.Applied())
		}
		if tc.excused && p.Total() != 0 {
			t.Errorf("filed %v: interest %v, want none", tc.filed, p.Total())
		}
		if !tc.excused && p.Total() == 0 {
			t.Errorf("filed %v: no interest on the January installment", tc.filed)
		}
	}
}

// Form 502UP exception A: no interest at $500 or less of tax after
// withholding. One cent more and the rules apply.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{950_000, true},  // $500.00 after withholding
		{949_999, false}, // $500.01
	} {
		h := household{tax: 10_000 * money.Dollar, withholding: []estimated.Withheld{withheld(day(2026, 3, 1), tc.withheld)}}
		res := must(rules(t).Requirement(h.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		// At $500 the exception is the chosen option, at $0.
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding of $500 or less") {
			t.Errorf("withheld %v: chosen %v %q at %v", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
	}
	// Withheld only on December 31, $500.01 short owes interest on the early
	// columns, and $500.00 owes none.
	for _, tc := range []struct {
		withheld money.USCents
		none     bool
	}{{950_000, true}, {949_999, false}} {
		h := household{tax: 10_000 * money.Dollar, withholding: []estimated.Withheld{withheld(day(2026, 12, 31), tc.withheld)}}
		if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); (p.Value().Total() == 0) != tc.none {
			t.Errorf("withheld %v on December 31: interest %v", tc.withheld, p.Value().Total())
		}
	}
}

// The target is the lesser of 90% of this year's tax and 110% of last
// year's, at every income.
func TestEstimatedTargetOptions(t *testing.T) {
	// 90% of $11,000 is $9,900; 110% of $9,000 is $9,900.
	for _, tc := range []struct {
		prior  money.USCents
		chosen estimated.Basis
		target money.USCents
	}{
		{900_001, estimated.BasisThisYearTax, 990_000},  // 110% is $9,900.01
		{900_000, estimated.BasisThisYearTax, 990_000},  // equal
		{899_999, estimated.BasisPriorYearTax, 989_999}, // 110% is $9,899.99
	} {
		res := must(rules(t).Requirement(household{tax: 11_000 * money.Dollar, prior: filed(tc.prior)}.facts(t)))
		r := res.Value()
		c, _ := r.Chosen()
		if c.Basis() != tc.chosen || r.Target() != tc.target {
			t.Errorf("prior %v: chose %v for %v, want %v for %v", tc.prior, c.Basis(), r.Target(), tc.chosen, tc.target)
		}
		if o := r.Options()[1]; o.Percent() != money.Percent(110) {
			t.Errorf("the prior-year option is %v", o.Percent())
		}
	}

	// 110% at a high income too: no step to a different percentage.
	f := household{tax: 1_000_000 * money.Dollar, prior: filed(500_000 * money.Dollar)}.facts(t)
	if r := must(rules(t).Requirement(f)).Value(); r.Target() != 550_000*money.Dollar {
		t.Errorf("target %v at high income, want 110%% of $500,000", r.Target())
	}

	// A first-time filer uses 90% of this year's tax.
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &notFiled}.facts(t))).Value()
	if r.Options()[1].Available() || r.Target() != 9_000*money.Dollar {
		t.Errorf("first-time filer: prior option available %v, target %v", r.Options()[1].Available(), r.Target())
	}

	// No return on record, or one not required: asked, and not offered.
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	for name, p := range map[string]*estimated.PriorReturn{"none on record": nil, "not required": &notRequired} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p}.facts(t)))
		kind := working.PositionPriorYearReturn
		if p != nil {
			kind = working.PositionUnsettledRule
		}
		if res.Value().Options()[1].Available() || !has(res.Required(), kind, "") {
			t.Errorf("%s: prior option available %v, asked %v", name, res.Value().Options()[1].Available(), res.Required())
		}
	}

	// A short prior year is used: nothing in Maryland requires twelve months.
	short := must(estimated.NewPriorReturn().Filed(true).Months(9).Figures(must(estimated.NewFigures().Tax(5_000 * money.Dollar).Build())).Build())
	if r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &short}.facts(t))).Value(); r.Target() != 5_500*money.Dollar {
		t.Errorf("after a 9-month return the target is %v, want $5,500", r.Target())
	}
}

// Form 502UP line 9b of zero makes line 10 zero, so no interest arises.
func TestEstimatedNoTaxLastYearMeansNoInterest(t *testing.T) {
	f := household{tax: 10_000 * money.Dollar, prior: filed(0)}.facts(t)
	r := must(rules(t).Requirement(f)).Value()
	if r.Required() || r.Target() != 0 {
		t.Errorf("required %v, target %v", r.Required(), r.Target())
	}
	for i, in := range must(rules(t).Schedule(f)).Value() {
		if in.Status() != estimated.StatusNothingDue {
			t.Errorf("installment %d is %v", i+1, in.Status())
		}
	}
	if p := must(rules(t).Penalty(f, day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("interest %v", p.Value().Total())
	}
}

// 502UP's only worked figures: $500 paid each quarter is $500, $1,000,
// $1,500 and $2,000 on line 14.
func TestEstimatedCreditsAreCumulativeAsLine14Says(t *testing.T) {
	f := household{tax: 10_000 * money.Dollar, payments: quarterly(500*money.Dollar, 500*money.Dollar, 500*money.Dollar, 500*money.Dollar)}.facts(t)
	for i, in := range must(rules(t).Schedule(f)).Value() {
		if want := money.USCents(i+1) * 500 * money.Dollar; in.CreditedBy() != want {
			t.Errorf("line 14 column %d is %v, want %v", i+1, in.CreditedBy(), want)
		}
	}
}

// Nothing paid on a $36,000 target: line 15 is $9,000, $18,000, $27,000
// and $36,000, because each column includes the earlier underpayments. Each
// is charged to the next due date at one-twelfth of the year's rate a
// month. The third column spans the change from 10.8133% for 2026 to 9.995%
// for 2027: three months at the first and the month ending January 15 at the
// second.
func TestEstimatedInterestByTheMonthAcrossTheRateChange(t *testing.T) {
	f := household{tax: 40_000 * money.Dollar, prior: filed(40_000 * money.Dollar)}.facts(t)
	res := must(rules(t).Penalty(f, day(2027, 4, 15)))
	lines := res.Value().Lines()
	want := []struct {
		short  money.USCents
		months []int
		charge []money.USCents
	}{
		{900_000, []int{2}, []money.USCents{16_220}},              // 9,000 × 2 × 10.8133% ÷ 12 = 162.1995
		{1_800_000, []int{3}, []money.USCents{48_660}},            // 18,000 × 3 × 10.8133% ÷ 12 = 486.5985
		{2_700_000, []int{3, 1}, []money.USCents{72_990, 22_489}}, // 729.8978 and 27,000 × 9.995% ÷ 12 = 224.8875
		{3_600_000, []int{3}, []money.USCents{89_955}},            // 36,000 × 3 × 9.995% ÷ 12 = 899.55
	}
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	for i, w := range want {
		l := lines[i]
		if l.Shortfall() != w.short {
			t.Errorf("column %d underpayment %v, want %v", i+1, l.Shortfall(), w.short)
		}
		parts := l.Parts()
		if len(parts) != len(w.months) {
			t.Errorf("column %d has %d parts, want %d", i+1, len(parts), len(w.months))
			continue
		}
		for j, p := range parts {
			if p.Months() != w.months[j] || p.Charge() != w.charge[j] {
				t.Errorf("column %d part %d: %d months, %v; want %d months, %v", i+1, j+1, p.Months(), p.Charge(), w.months[j], w.charge[j])
			}
		}
	}
	if parts := lines[2].Parts(); len(parts) == 2 && (parts[0].Rate() == parts[1].Rate() || parts[1].To() != day(2027, 1, 15)) {
		t.Errorf("the third column does not change rate on the month ending January 15: %v, %v to %v", parts[0].Rate(), parts[1].Rate(), parts[1].To())
	}
	if res.Value().Total() != 250_314 {
		t.Errorf("interest %v, want $2,503.14", res.Value().Total())
	}
	// The working carries each rate as published, not cut to basis points.
	if parts := lines[2].Parts(); len(parts) == 2 && (parts[0].FineRate() != 108_133 || parts[1].FineRate() != 99_950) {
		t.Errorf("the third column's rates are %v and %v, want 10.8133%% and 9.995%%", parts[0].FineRate(), parts[1].FineRate())
	}

	// At a hundred times the size the rates show to their last digit:
	// $900,000 × 2 × 10.8133% ÷ 12 is $16,219.95, and the third column's
	// month at 9.995% on $2,700,000 is $22,488.75.
	big := household{tax: 4_000_000 * money.Dollar, prior: filed(4_000_000 * money.Dollar)}.facts(t)
	lines = must(rules(t).Penalty(big, day(2027, 4, 15))).Value().Lines()
	if got := lines[0].Parts()[0].Charge(); got != 1_621_995 {
		t.Errorf("April's column at 10.8133%%: %v, want $16,219.95", got)
	}
	if got := lines[2].Parts()[1].Charge(); got != 2_248_875 {
		t.Errorf("the month ending January 15 at 9.995%%: %v, want $22,488.75", got)
	}
}

// A payment after its due date but before the next is charged the months
// it was late, a part of a month counting as one: paid May 15 is one month,
// May 16 two.
func TestEstimatedLatePaymentCountsWholeMonths(t *testing.T) {
	for _, tc := range []struct {
		paid   estimated.Date
		charge money.USCents
	}{{day(2026, 5, 15), 8_110}, {day(2026, 5, 16), 16_220}} {
		payments := append([]estimated.Payment{pay(tc.paid, 9_000*money.Dollar)}, quarterly(0, 9_000*money.Dollar, 9_000*money.Dollar, 9_000*money.Dollar)[1:]...)
		f := household{tax: 40_000 * money.Dollar, prior: filed(40_000 * money.Dollar), payments: payments}.facts(t)
		p := must(rules(t).Penalty(f, day(2027, 4, 15))).Value()
		if len(p.Lines()) != 1 || p.Total() != tc.charge {
			t.Errorf("paid %v: %d lines, interest %v; want one line of %v", tc.paid, len(p.Lines()), p.Total(), tc.charge)
		}
	}
}

// Withholding counts on the dates withheld. $36,000 withheld all on
// December 15 reaches the target, yet leaves the first three columns short
// until then.
func TestEstimatedWithholdingCountsOnItsDates(t *testing.T) {
	h := household{tax: 40_000 * money.Dollar, prior: filed(40_000 * money.Dollar), withholding: []estimated.Withheld{withheld(day(2026, 12, 15), 36_000*money.Dollar)}}
	r := must(rules(t).Requirement(h.facts(t))).Value()
	if r.Required() {
		t.Errorf("required with the target withheld: %s", r.Why())
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	// 16,220 + 48,660 + 27,000 for Sep 15 to Dec 15, three months at 10.8133%.
	if len(p.Lines()) != 3 || p.Total() != 16_220+48_660+72_990 {
		t.Errorf("%d lines, interest %v; want three, $1,378.70", len(p.Lines()), p.Total())
	}
	h.withholding = []estimated.Withheld{withheld(day(2026, 4, 15), 9_000*money.Dollar), withheld(day(2026, 6, 15), 9_000*money.Dollar),
		withheld(day(2026, 9, 15), 9_000*money.Dollar), withheld(day(2026, 12, 15), 9_000*money.Dollar)}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("withheld a quarter by each date: interest %v", p.Value().Total())
	}
}

// Form 502UP lines 11 to 13 by election: the target in the shares of the
// year's income received by each period's end. Income arriving late owes
// nothing on the early dates (code 301).
func TestEstimatedIncomeShareMethod(t *testing.T) {
	h := household{tax: 40_000 * money.Dollar, prior: filed(40_000 * money.Dollar),
		periods:  []money.USCents{0, 10_000 * money.Dollar, 20_000 * money.Dollar, 100_000 * money.Dollar},
		payments: quarterly(0, 3_600*money.Dollar, 3_600*money.Dollar, 28_800*money.Dollar)}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{0, 360_000, 720_000, 3_600_000} {
		if s[i].NeededBy() != want {
			t.Errorf("line 13 column %d is %v, want %v", i+1, s[i].NeededBy(), want)
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("interest %v under the income-share method", p.Value().Total())
	}
	h.periods = nil
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() == 0 {
		t.Error("the same payments owe nothing by even quarters")
	}

	bad := household{tax: 40_000 * money.Dollar, periods: []money.USCents{5_000, 4_000, 6_000, 10_000}}
	if _, err := rules(t).Requirement(bad.facts(t)); err == nil {
		t.Error("income received to May 31 below that to March 31 is accepted")
	}
}

// At least 90% of income taxable by another state and an underpayment no
// greater than the local tax: no interest (code 302).
func TestEstimatedIncomeTaxableByAnotherState(t *testing.T) {
	yes := must(marylandtaxcalc.NewEstimatedFacts().TaxableByAnotherState(true).Build())
	// $1,500 a quarter against $2,250: line 15 is $750, $1,500, $2,250 and
	// $3,000.
	payments := quarterly(1_500*money.Dollar, 1_500*money.Dollar, 1_500*money.Dollar, 1_500*money.Dollar)
	for _, tc := range []struct {
		local money.USCents
		none  bool
	}{{3_000 * money.Dollar, true}, {299_999, false}} {
		h := household{tax: 10_000 * money.Dollar, local: tc.local, payments: payments, extras: &yes}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
		if (p.Total() == 0) != tc.none {
			t.Errorf("local tax %v: interest %v", tc.local, p.Total())
		}
		s := must(rules(t).Schedule(h.facts(t))).Value()
		if (s[0].Status() == estimated.StatusExcused) != tc.none {
			t.Errorf("local tax %v: April is %v", tc.local, s[0].Status())
		}
	}
	unsaid := must(marylandtaxcalc.NewEstimatedFacts().Build())
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, local: 3_000 * money.Dollar, payments: payments, extras: &unsaid}.facts(t), day(2027, 4, 15)))
	if !has(res.Required(), working.PositionJurisdictionFact, "taxable by another state") || res.Value().Total() == 0 {
		t.Errorf("not answered: asked %v, interest %v", res.Required(), res.Value().Total())
	}
	res = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, payments: payments, extras: &yes}.facts(t), day(2027, 4, 15)))
	if !has(res.Required(), working.PositionJurisdictionFact, "county income tax") || res.Value().Total() == 0 {
		t.Errorf("no local tax given: asked %v, interest %v", res.Required(), res.Value().Total())
	}
}

// The interest stops on April 15, 2027, or on the day the return was filed
// and paid.
func TestEstimatedInterestStops(t *testing.T) {
	f := household{tax: 40_000 * money.Dollar, prior: filed(40_000 * money.Dollar)}.facts(t)
	if p := must(rules(t).Penalty(f, day(2027, 6, 30))).Value(); p.AsOf() != day(2027, 4, 15) || p.Total() != 250_314 {
		t.Errorf("to June 30: figured to %v, %v; want April 15, $2,503.14", p.AsOf(), p.Total())
	}
	// Filed February 20: the last column runs two months, January 15 to
	// February 15 and the part to February 20, at 9.995%.
	f = household{tax: 40_000 * money.Dollar, prior: filed(40_000 * money.Dollar), returnFiled: day(2027, 2, 20)}.facts(t)
	p := must(rules(t).Penalty(f, day(2027, 4, 15))).Value()
	if p.AsOf() != day(2027, 2, 20) {
		t.Errorf("figured to %v, want February 20", p.AsOf())
	}
	last := p.Lines()[3].Parts()
	if len(last) != 1 || last[0].Months() != 2 || last[0].Charge() != 59_970 {
		t.Errorf("last column %+v; want two months, $599.70", last)
	}
	// Before a due date has passed, nothing has accrued on it.
	if p := must(rules(t).Penalty(f, day(2026, 6, 1))).Value(); len(p.Lines()) != 1 || p.Total() != 16_220 {
		t.Errorf("to June 1: %d lines, %v; want April's two months, $162.20", len(p.Lines()), p.Total())
	}
}

// What the research could not settle is asked, with the default named.
func TestEstimatedOpenQuestionsAreReported(t *testing.T) {
	f := household{tax: 40_000 * money.Dollar, prior: filed(40_000 * money.Dollar)}.facts(t)
	res := must(rules(t).Penalty(f, day(2027, 4, 15)))
	for _, q := range []struct {
		kind   working.PositionKind
		phrase string
	}{
		{working.PositionUnpublishedRate, "2026 interest factors"},
		{working.PositionUnpublishedRate, "up to 25%"},
		{working.PositionPenaltyWaiver, "reasonable cause"},
	} {
		if !has(res.Required(), q.kind, q.phrase) {
			t.Errorf("%v %q is not asked: %v", q.kind, q.phrase, res.Required())
		}
	}
	k := estimated.WaiverCasualty
	res = must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, prior: filed(40_000 * money.Dollar), waiver: &k}.facts(t), day(2027, 4, 15)))
	if has(res.Required(), working.PositionPenaltyWaiver, "") || res.Value().Waiver() != estimated.WaiverCasualty {
		t.Errorf("a waiver asked for is still asked about, or not carried: %v", res.Value().Waiver())
	}
	// Nothing short: no question about the factors or the 25% penalty.
	paid := household{tax: 40_000 * money.Dollar, prior: filed(40_000 * money.Dollar), payments: quarterly(9_000*money.Dollar, 9_000*money.Dollar, 9_000*money.Dollar, 9_000*money.Dollar)}
	if res := must(rules(t).Penalty(paid.facts(t), day(2027, 4, 15))); has(res.Required(), working.PositionUnpublishedRate, "") {
		t.Errorf("asked about rates with nothing short: %v", res.Required())
	}
}

// Maryland's reasonable-cause waiver lists no causes, so a casualty or
// disaster is left open and the open point asked. Retirement and disability
// have no waiver, even where the federal one applies.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pos     *estimated.PositionsBuilder
		refused bool
		want    string
	}{
		{"disaster", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)), false, "if a casualty or disaster counts"},
		{"retired in 2026 at 64", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)), true, "not retirement or disability"},
		{"retired in 2020", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, 3, 1)), true, "not retirement or disability"},
	} {
		pos := must(tc.pos.Build())
		res := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, prior: filed(40_000 * money.Dollar), positions: &pos}.facts(t), day(2027, 4, 15)))
		p := res.Value()
		o, ok := p.WaiverOutcome()
		if !ok || o.Refused() != tc.refused || o.Undecided() == tc.refused || !strings.Contains(o.Reason(), tc.want) {
			t.Errorf("%s: outcome %v refused %v, %q", tc.name, ok, o.Refused(), o.Reason())
		}
		if p.Total() == 0 || p.IfWaived() != p.Total() {
			t.Errorf("%s: total %v, if waived %v; nothing is eligible", tc.name, p.Total(), p.IfWaived())
		}
		if has(res.Required(), working.PositionUnsettledRule, "casualty or disaster") == tc.refused {
			t.Errorf("%s: the open point is asked %v", tc.name, !tc.refused)
		}
	}
}

// When the $500 exception sets the target, last year's return can't change
// it, so it isn't asked for.
func TestEstimatedExemptAsksNothingOfTheTarget(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 400 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() || has(res.Required(), working.PositionPriorYearReturn, "") {
		t.Errorf("exempt %v, asked %v", res.Value().Exempt(), res.Required())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 4, 15)))
	want := map[string][2]string{
		"No interest at $500":                          {"$500 or less", "§ 13-602(c)"},
		"Who may use 110% of last year's tax":          {"more income than you did in 2025", "§ 13-602(a)(2)"},
		"Last year's tax":                              {"line 34", "§ 13-602(a)(2)(ii)"},
		"The interest factors":                         {"0.0192, 0.0289, 0.0381 and 0.0266", "§ 13-604(a)"},
		"A third-period payment made from December 15": {"on or after December 15", "§§ 10-820(b)(1), 13-602(a), 13-604(a)"},
		"The penalty of up to 25%":                     {"Only interest", "§ 13-702(a)"},
	}
	statute := map[string]string{
		"No interest at $500":                          "less than one-half",
		"Who may use 110% of last year's tax":          "no condition on income",
		"Last year's tax":                              "tax paid",
		"The interest factors":                         "0.0191, 0.0287, 0.0377 and 0.0270",
		"A third-period payment made from December 15": "due January 15",
		"The penalty of up to 25%":                     "up to 25%",
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
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.StatuteSource(), w[1]) || !strings.Contains(d.Statute(), statute[d.Rule()]) || d.FormSource() == "" {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s)", d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
		}
		// $10,000 of tax, nothing paid and no return on record: interest is
		// charged, so the factors and the 25% penalty affect it, and nothing
		// else does.
		wantAffects := d.Rule() == "The interest factors" || d.Rule() == "The penalty of up to 25%"
		if d.AffectsHousehold() != wantAffects {
			t.Errorf("%q affects %v, want %v", d.Rule(), d.AffectsHousehold(), wantAffects)
		}
		wantUses := working.ReadingForm
		if d.Rule() == "The interest factors" || d.Rule() == "A third-period payment made from December 15" {
			wantUses = working.ReadingStatute
		}
		if d.Uses() != wantUses {
			t.Errorf("%q uses %v, want %v", d.Rule(), d.Uses(), wantUses)
		}
	}
	// Exactly $500 after withholding is where the form and the statute part.
	at := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withholding: []estimated.Withheld{withheld(day(2026, 3, 1), 950_000)}}.facts(t)))
	for _, d := range at.Divergences() {
		if d.Rule() == "No interest at $500" && !d.AffectsHousehold() {
			t.Error("exactly $500 is not affected by the $500 divergence")
		}
	}
	// The form's 2025 factors and the statute's monthly figures for the same
	// spans, as the divergence states them.
	mustContain(t, "502up-2025-columns.txt", "0.0192 0.0289 0.0381 0.0266")
	mustContain(t, "502up-2025.txt", "0.9568% per month for due dates prior to January 1, 2026")
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := marylandtaxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"a farmer":    must(estimated.NewFacts(md, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(7000).Build()),
		"a fisherman": must(estimated.NewFacts(md, 2026).FilingStatus(filing.Single).ThisYear(this).FishShare(7000).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"Pennsylvania's facts": must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":                 must(estimated.NewFacts(md, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":         must(estimated.NewFacts(md, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Maryland's").Build()),
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
	for _, s := range []string{"$500", "110%", "county", "on the dates", "10.8133%", "9.995%", "by the month", "January 31", "25%"} {
		if !strings.Contains(e.Summary()+strings.Join(e.Body(), " ")+e.VersusFederal(), s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Without this year's tax the target is 110% of last year's, and the
// interest figured against it is the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: filed(10_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 11_000*money.Dollar {
		t.Errorf("required %v, target %v; want $11,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax, "") {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{275_000, 550_000, 825_000, 1_100_000} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, s[i].NeededBy(), want)
		}
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if !p.UpperBound() || p.Total() == 0 {
		t.Errorf("unpaid: interest %v, upper bound %v", p.Total(), p.UpperBound())
	}

	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() {
		t.Errorf("a target with neither year's tax: %v", r.Target())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v", err)
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("MD"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}
