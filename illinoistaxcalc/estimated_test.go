// Illinois's estimated tax rules for 2026, checked against the Department of
// Revenue's own documents.
//
// Illinois publishes no exhaustive oracle for estimated tax. The strongest
// checks are two worked examples from the department: the IL-2210
// instructions' Penalty Peterson, whose payments and penalty of $121.20 are
// reproduced line by line, and the annualised installments of 86 Ill. Adm.
// Code 100.8010(d)(2) Examples 1 to 3. Every threshold, percentage, date and
// rate is compared against its extract in testdata/. The exceptions, the
// boundaries and the withholding rules are hand-written cases.
package illinoistaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/illinoistaxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const illinois = estimated.Jurisdiction("IL")

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
	return must(illinoistaxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
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

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

func withheldOn(on estimated.Date, a money.USCents) estimated.Withheld {
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

// answered is every Illinois fact answered: no nursing home, no pass-through
// withholding, no interest.
func answered() *illinoistaxcalc.EstimatedFactsBuilder {
	return illinoistaxcalc.NewEstimatedFacts().NursingHome(false).NoPassThroughWithholding().InterestOnLateInstallments(false)
}

type household struct {
	tax         money.USCents // after credits, IL-1040 lines 14 and 22 less 15 to 17 and 28 to 30
	noTax       bool
	withheld    money.USCents // one entry on December 15
	withholding []estimated.Withheld
	asWithheld  bool
	prior       *estimated.PriorReturn
	noPrior     bool
	payments    []estimated.Payment
	extras      *illinoistaxcalc.EstimatedFacts
	noExtras    bool
	returnFiled estimated.Date
	periods     []money.USCents // annualised tax after credits at each period end
	farm, fish  money.Rate
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	b := estimated.NewFacts(illinois, 2026).FilingStatus(filing.Single).
		ThisYear(h.figures()).Paid(h.payments...).ReturnFiled(h.returnFiled).
		FarmShare(h.farm).FishShare(h.fish)
	if h.withheld > 0 {
		b.Withheld(withheldOn(day(2026, 12, 15), h.withheld))
	}
	b.Withheld(h.withholding...)
	if h.asWithheld {
		b.Elections(must(estimated.NewElections().WithholdingAsWithheld().Build()))
	}
	switch {
	case h.prior != nil:
		b.PriorYear(*h.prior)
	case !h.noPrior:
		b.PriorYear(*prior(1_000_000*money.Dollar, 0)) // large enough never to be the target
	}
	switch {
	case h.extras != nil:
		b.State(*h.extras)
	case !h.noExtras:
		b.State(must(answered().Build()))
	}
	if h.periods != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.periods {
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

func (h household) figures() estimated.Figures {
	if h.noTax {
		return must(estimated.NewFigures().Build())
	}
	return must(estimated.NewFigures().Tax(h.tax).Build())
}

// prior is a filed 12-month return with its tax after credits and its
// withholding.
func prior(tax, withheld money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Withheld(withheld).Build())).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "il-1040-es-2026.txt",
		"IL-1040-ES Estimated Income Tax Payments for Individuals 2026",
		"You must make estimated income tax payments if you reasonably expect your 2026 tax liability to exceed $1,000 after subtracting your Illinois withholding, pass-through withholding, and tax credits",
		"pay your estimated tax in four equal installments that are due on April 15, 2026; June 15, 2026; September 15, 2026; and January 15, 2027.",
		"65 years or older and permanently living in a nursing home",
		"a farmer. We consider you a farmer if at least two-thirds of your total federal gross income is from farming.",
		"We will apply each payment to the earliest due date until that liability is paid",
		"if you pay at least 90 percent of this year’s tax or at least 100 percent of last year’s tax in four equal timely installments",
		"If $1,000 or less, stop.",
		"If the election was made after the extended due date of your prior year return, the credit is considered paid on the date you made the election.",
		"IL-1040-ES (R-09/26)",
	)
	mustContain(t, "il-2210-2025.txt",
		"2025 IL-2210 Computation of Penalties for Individuals",
		"Multiply Column A, Line 3, by 90% (.9).",
		"If Line 5 is $1,000 or less or if you are not required to make estimated tax payments, enter “0,” and go to Step 3. Otherwise, enter the lesser of Column A, Line 6, or Column B, Line 3.",
		"Divide the amount entered on Line 7 by four. This is the amount of each required installment.",
		"1 - 30............................ .02",
		"31 or more ...................... .10",
		"First 3 months First 5 months First 8 months All 12 months",
		"22.5% (.225) 45% (.450) 67.5% (.675) 90% (.900)",
		"Subtract Line 12 from Line 11. If the amount is negative, use brackets.",
		"IL-2210 (R-12/25)",
	)
	mustContain(t, "il-2210-2025-instructions.txt",
		"is assessed at either 2 percent or 10 percent of the unpaid liability based on the number of days the payment is late.",
		"The penalty rates used on this form are for returns due on or after January 1, 2005.",
		"you were not required to file Form IL-1040 for last year",
		"last year’s Form IL-1040 had a zero tax liability (Form IL-1040, Line 14 minus Lines 15, 16, 17, 25, 27, 28, 29, and 30); or",
		"this year’s tax liability (Form IL-1040, Line 14 minus Lines 15, 16, 17, 25, 27, 28, 29, and 30) is $1,000 or less.",
		"We will waive the late-payment penalty for underpayment of estimated tax if you timely paid the lesser of 100 percent of the prior year’s tax liability or 90 percent of the current year’s tax liability.",
		"If this date falls on a weekend or holiday, use the next business day.",
		"If you choose to annualize your income in Step 6, you must use this method for all four installments.",
		"Enter one-fourth of the total tax withheld (or the actual tax withheld from your wages for each quarter).",
		"We consider you to have paid Illinois tax withheld evenly over the entire year unless you send us copies of pay stubs or a letter from your employer",
		"Enter the entire amount in the quarter in which the pass-through entity’s tax year ended.",
		"on or before the extended due date of that prior year return, your credit is considered to be paid on April 15, 2025.",
		"after the extended due date of that prior year return, your credit is considered to be paid on the date you filed the return on which you made the election.",
		"Apply the payment with the earliest date from Line 20 to the first unpaid quarter.",
		"Apply any negative (overpayment) amount in Column E to the next underpaid quarter.",
		"If Column D is “0” and you did not enter a date in Column F, enter 10 percent (.10) here.",
		"the first three months, the first five months, and the first eight months of the tax year",
	)
	mustContain(t, "il-1040-2025-payments-and-penalty.txt",
		"25 Illinois Income Tax withheld.",
		"27 Pass-through withholding.",
		"a Check if at least two-thirds of your federal gross income is from farming.",
		"b Check if you or your spouse are 65 or older and permanently living in a nursing home.",
		"d Check if you were not required to file an Illinois Individual Income Tax return in the previous tax year.",
	)
	mustContain(t, "pub-103-2025.txt",
		"If the payment is 1 – 30 days late, the late-payment penalty rate is two percent (2%).",
		"If the payment is 31+ days late, the late-payment penalty rate is ten percent (10%).",
		"That amount is then divided by 365 (or 366 in leap years) to determine the total interest due.",
		"Interest begins to accrue the day after the date the payment is due through the date you pay the tax.",
	)
	mustContain(t, "idor-interest-rates.txt",
		"Interest is simple interest figured using a daily rate.",
		"01/01/2025 - 12/31/2026 | | 7% |",
	)
	mustContain(t, "adm-100-5000.txt",
		"when the last day for filing a return falls on Saturday, Sunday or a legal holiday, the return is considered timely if it is filed on the next succeeding day that is not a Saturday, Sunday or legal holiday.",
	)
	mustContain(t, "adm-100-8000.txt",
		"$500 for taxable years ending on or after December 31, 2001",
		"shall apply to a couple filing a joint return only if ⅔ of the total gross income of the couple is from farming.",
	)
	mustContain(t, "adm-100-8010.txt",
		"the amount of any required installment shall be 25% of the required annual payment",
		"each payment received by the Department will be applied first to any unpaid balance of the 1st estimated tax installment due",
		"Amounts withheld by a partnership, subchapter S corporation or trust on behalf of the taxpayer under IITA Section 709.5 are treated as payments received by the Department on the last day of the taxable year",
		"In the latter case, all amounts withheld shall be considered as payments of estimated tax on the dates those amounts were actually withheld.",
		"No penalty shall be imposed under IITA Section 804 with respect to any underpayment of estimated tax arising subsequent to the death of the taxpayer.",
		"1st | 22.5% | 2nd | 45% | 3rd | 67.5% | 4th | 90% |",
	)
	mustContain(t, "research-il-statute-quotes.txt",
		"The threshold is \"$1,000 for taxable years ending on or after December 31, 2019\".",
		"§ 804(c)(1)(B)(i): \"90% of the tax shown on the return for the taxable year\".",
		"\"100% of the tax shown on the return of the taxpayer for the preceding taxable year if a return showing a liability for tax was filed by the taxpayer for the preceding taxable year and such preceding year was a taxable year of 12 months\"",
		"\"There shall be paid 4 equal installments of estimated tax\".",
		"§ 3-3(b-25)(1): \"2% of any amount that is paid no later than 30 days after the due date and 10% of any amount that is paid later than 30 days after the due date.\"",
		"§ 804(g)(3): \"all other amounts pursuant to Article 7 shall be deemed a payment of estimated tax on the date the payment is made\".",
		"§ 804(d): \"if the taxpayer had no tax liability for the preceding taxable year and such year was a taxable year of 12 months.\"",
		"P.A. 101-355 raised the statute to $1,000",
	)
}

// The 2026 dates are the ones IL-1040-ES prints, and none moves: they are a
// Wednesday, Monday, Tuesday and Friday. No 2026 Illinois due date falls on
// a weekend. The 2025 IL-2210 example shows the form's rule for one that
// does: June 15, 2025 was a Sunday, and the example's second due date is
// June 16, 2025.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	mustContain(t, "il-2210-2025-example.txt", "06 16 2025 405 645 [240]")
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

// The IL-2210 instructions' example, moved a year: $1,800 of tax after
// credits, $2,350 last year, no withholding, and payments of $100, $200,
// $750, $250 and $400 with the return. The form charges $121.20. In 2026 the
// second due date is June 15 rather than June 16, so the 84 days become 85,
// which is still 10%; every other count is the same.
func TestEstimatedPenaltyMatchesIL2210Example(t *testing.T) {
	mustContain(t, "il-2210-2025-example.txt",
		"is $1,875 as shown on his 2025 Form IL-1040, Lines 14 and 22. His",
		"tax due for 2024 was $2,600.",
		"$100 on April 7, 2025 $750 on September 8, 2025",
		"$200 on June 9, 2025 $250 on January 19, 2026",
		"Mr. Peterson filed his return May 10, 2026, and paid $400.",
		"04 15 2025 405 100 305 0 4 0 7 2 0 2 5 0 0 0.00",
		"305 200 105 0 6 0 9 2 0 2 5 55 .10 20.00 200 x .10 = 20.00",
		"105 750 [645] 0 9 0 8 2 0 2 5 146 .10 10.50 105 x .10 = 10.50",
		"06 16 2025 405 645 [240] 0 9 0 8 2 0 2 5 84 .10 40.50 405 x .10 = 40.50",
		"165 250 [85] 0 1 1 9 2 0 2 6 126 .10 16.50 165 x .10 = 16.50",
		"01 15 2026 405 85 320 0 1 1 9 2 0 2 6 4 .02 1.70 85 x .02 = 1.70",
		"320 400 [80] 0 5 1 0 2 0 2 6 115 .10 32.00 320 x .10 = 32.00",
		"121.20",
	)
	h := household{
		tax:   1_800 * money.Dollar,
		prior: prior(2_350*money.Dollar, 0),
		payments: []estimated.Payment{
			pay(day(2026, 4, 7), 100*money.Dollar), pay(day(2026, 6, 9), 200*money.Dollar),
			pay(day(2026, 9, 8), 750*money.Dollar), pay(day(2027, 1, 19), 250*money.Dollar),
			pay(day(2027, 5, 10), 400*money.Dollar),
		},
	}
	req := must(rules(t).Requirement(h.facts(t))).Value()
	if req.Target() != 1_620*money.Dollar {
		t.Errorf("target %v, want 90%% of $1,800, $1,620", req.Target())
	}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 6, 30)))
	type part struct {
		principal money.USCents
		days      int
		rate      money.Rate
		charge    money.USCents
	}
	want := [][]part{
		{{200 * money.Dollar, 55, 1000, 20 * money.Dollar}, {105 * money.Dollar, 146, 1000, 1_050}},
		{{405 * money.Dollar, 85, 1000, 4_050}},
		{{165 * money.Dollar, 126, 1000, 1_650}},
		{{85 * money.Dollar, 4, 200, 170}, {320 * money.Dollar, 115, 1000, 32 * money.Dollar}},
	}
	wantShort := []money.USCents{305 * money.Dollar, 405 * money.Dollar, 165 * money.Dollar, 405 * money.Dollar}
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	for i, l := range lines {
		if l.Shortfall() != wantShort[i] {
			t.Errorf("quarter %d is %v short, want %v", i+1, l.Shortfall(), wantShort[i])
		}
		parts := l.Parts()
		if len(parts) != len(want[i]) {
			t.Errorf("quarter %d has %d parts, want %d", i+1, len(parts), len(want[i]))
			continue
		}
		for j, p := range parts {
			w := want[i][j]
			if p.Kind() != estimated.PartFlat || p.Principal() != w.principal || p.Days() != w.days || p.Rate() != w.rate || p.Charge() != w.charge {
				t.Errorf("quarter %d part %d: %v %v on %v for %d days is %v; want %v on %v for %d days, %v",
					i+1, j+1, p.Kind(), p.Rate(), p.Principal(), p.Days(), p.Charge(), w.rate, w.principal, w.days, w.charge)
			}
		}
	}
	if res.Value().Total() != 12_120 {
		t.Errorf("penalty %v, want $121.20", res.Value().Total())
	}
	if res.Value().UpperBound() {
		t.Error("an exact penalty is marked as an upper bound")
	}
}

// IL-2210 line 7: no installment when this year's tax less credits and all
// withholding is $1,000 or less. Pass-through withholding counts.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	pt := must(answered().PassThroughWithholding(day(2026, 12, 31), 500*money.Dollar).Build())
	for _, tc := range []struct {
		name     string
		tax      money.USCents
		withheld money.USCents
		extras   *illinoistaxcalc.EstimatedFacts
		exempt   bool
	}{
		{"$1,000.00 left", 400_000, 300_000, nil, true},
		{"$1,000.01 left", 400_001, 300_000, nil, false},
		{"$1,000.00 after pass-through", 400_000, 250_000, &pt, true},
		{"$1,000.01 after pass-through", 400_001, 250_000, &pt, false},
	} {
		res := must(rules(t).Requirement(household{tax: tc.tax, withheld: tc.withheld, extras: tc.extras}.facts(t)))
		if got := res.Value().Exempt(); got != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", tc.name, got, tc.exempt, res.Value().Why())
		}
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding of $1,000 or less") {
			t.Errorf("%s: chose %v %q at %v, want the exception", tc.name, c.Basis(), c.Label(), c.Amount())
		}
		// $1,000.01 rounds to $1,000, so the reason does not show it as the
		// threshold.
		if !tc.exempt && !strings.Contains(res.Value().Why(), "just over $1,000. That is more than $1,000") {
			t.Errorf("%s: why is %q", tc.name, res.Value().Why())
		}
	}
}

// The target is the lesser of 90% of this year's tax and 100% of last
// year's, with no 110% tier at any income.
func TestEstimatedTargetOptions(t *testing.T) {
	// 90% of $10,000 is $9,000, less than last year's $9,500.
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(9_500*money.Dollar, 0)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 9_000*money.Dollar {
		t.Errorf("chose %v at %v, want this year's $9,000", c.Basis(), res.Value().Target())
	}
	// Last year's $8,999.99 is less, and stays 100% at $1,000,000 of AGI.
	f := must(estimated.NewFacts(illinois, 2026).FilingStatus(filing.MarriedFilingJointly).
		ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).FederalAGI(1_000_000 * money.Dollar).StateAGI(1_000_000 * money.Dollar).Build())).
		PriorYear(*prior(899_999, 0)).State(must(answered().Build())).Build())
	res = must(rules(t).Requirement(f))
	c, _ := res.Value().Chosen()
	if c.Basis() != estimated.BasisPriorYearTax || c.Percent() != money.Percent(100) || res.Value().Target() != 899_999 {
		t.Errorf("chose %v at %v of %v, want 100%% of last year's $8,999.99", c.Basis(), c.Percent(), res.Value().Target())
	}

	short := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(100).Withheld(0).Build())).Build())
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	for name, tc := range map[string]struct {
		h   household
		ask bool
	}{
		"no return on record": {household{tax: 10_000 * money.Dollar, noPrior: true}, true},
		"not filed":           {household{tax: 10_000 * money.Dollar, prior: &notFiled}, false},
		"an 11-month year":    {household{tax: 10_000 * money.Dollar, prior: &short}, false},
	} {
		res := must(rules(t).Requirement(tc.h.facts(t)))
		o := res.Value().Options()[1]
		if o.Available() || o.WhyNot() == "" {
			t.Errorf("%s: last year's tax is offered", name)
		}
		if res.Value().Target() != 9_000*money.Dollar || res.Value().Exempt() {
			t.Errorf("%s: target %v, exempt %v; want 90%% of this year's", name, res.Value().Target(), res.Value().Exempt())
		}
		if got := has(res.Required(), working.PositionPriorYearReturn); got != tc.ask {
			t.Errorf("%s: asked for last year's return %v, want %v", name, got, tc.ask)
		}
	}
}

// IL-2210's penalty rates: 2% for 1 to 30 days late, 10% for 31 or more, or
// never paid. An amount still unpaid is charged as of the date figured to.
func TestEstimatedPenaltyRateBoundary(t *testing.T) {
	// 90% of $4,444.44 is $4,000, so $1,000 a quarter.
	base := household{tax: 444_444}
	for _, tc := range []struct {
		paid   estimated.Date
		asOf   estimated.Date
		charge money.USCents
	}{
		{day(2026, 4, 15), day(2026, 6, 1), 0},                   // on time
		{day(2026, 4, 16), day(2026, 6, 1), 20 * money.Dollar},   // 1 day
		{day(2026, 5, 15), day(2026, 6, 1), 20 * money.Dollar},   // 30 days
		{day(2026, 5, 16), day(2026, 6, 1), 100 * money.Dollar},  // 31 days
		{estimated.Date{}, day(2026, 5, 15), 20 * money.Dollar},  // unpaid, 30 days so far
		{estimated.Date{}, day(2026, 5, 16), 100 * money.Dollar}, // unpaid, 31 days so far
	} {
		h := base
		if !tc.paid.IsZero() {
			h.payments = []estimated.Payment{pay(tc.paid, 1_000*money.Dollar)}
		}
		res := must(rules(t).Penalty(h.facts(t), tc.asOf))
		if res.Value().Total() != tc.charge {
			t.Errorf("paid %v, figured to %v: penalty %v, want %v", tc.paid, tc.asOf, res.Value().Total(), tc.charge)
		}
	}
}

// Interest is charged only when the caller says it runs, at the uniform
// rate. The department's page prints one 7% rate for 01/01/2025 to
// 12/31/2026, so a shortfall crossing July 1, 2026, a reset date, is charged
// at the same rate on both sides; one that runs into 2027 stops at the last
// published day and reports the gap.
func TestEstimatedInterestOnlyWhenAsked(t *testing.T) {
	h := household{tax: 444_444} // $1,000 a quarter, never paid
	res := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if res.Value().Total() != 300*money.Dollar || has(res.Required(), working.PositionJurisdictionFact) {
		t.Errorf("answered no: penalty %v, asked %v; want $300 and no Illinois fact asked", res.Value().Total(), res.Required())
	}
	for _, l := range res.Value().Lines() {
		for _, p := range l.Parts() {
			if p.Kind() == estimated.PartInterest {
				t.Errorf("interest charged though not asked for")
			}
		}
	}

	unanswered := must(illinoistaxcalc.NewEstimatedFacts().NursingHome(false).NoPassThroughWithholding().Build())
	h.extras = &unanswered
	res = must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if !has(res.Required(), working.PositionJurisdictionFact) || res.Value().Total() != 300*money.Dollar {
		t.Errorf("unanswered: penalty %v, asked %v; want $300 and the interest question", res.Value().Total(), res.Required())
	}

	yes := must(illinoistaxcalc.NewEstimatedFacts().NursingHome(false).NoPassThroughWithholding().InterestOnLateInstallments(true).Build())
	h.extras = &yes
	// April's $1,000 for 260 days at 7% ÷ 365 is $49.86, June's for 199 days
	// $38.16, September's for 107 days $20.52; January's is not due yet.
	for _, asOf := range []estimated.Date{day(2026, 12, 31), day(2027, 1, 14)} {
		res = must(rules(t).Penalty(h.facts(t), asOf))
		var interest money.USCents
		for _, l := range res.Value().Lines() {
			for _, p := range l.Parts() {
				if p.Kind() == estimated.PartInterest {
					interest += p.Charge()
					if p.Rate() != money.Percent(7) {
						t.Errorf("interest at %v, want 7%%", p.Rate())
					}
				}
			}
		}
		if interest != 4_986+3_816+2_052 || res.Value().Total() != 300*money.Dollar+interest {
			t.Errorf("to %v: interest %v, total %v; want $108.54 and $408.54", asOf, interest, res.Value().Total())
		}
		if got := has(res.Required(), working.PositionUnpublishedRate); got != (asOf.Year() == 2027) {
			t.Errorf("to %v: unpublished rate reported %v", asOf, got)
		}
	}
}

// Farmers and filers 65 or older in a nursing home owe no estimates; nor
// does anyone who had no return to file, or no tax due after withholding,
// last year. Fishermen have no rule of their own.
func TestEstimatedExceptions(t *testing.T) {
	nursing := must(answered().NursingHome(true).Build())
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	shortZero := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(must(estimated.NewFigures().Tax(300_000).Withheld(300_000).Build())).Build())
	for name, tc := range map[string]struct {
		h      household
		exempt bool
		why    string
	}{
		"two-thirds from farming":           {household{tax: 10_000 * money.Dollar, farm: 6_667}, true, "two-thirds of your gross income is from farming"},
		"just under two-thirds":             {household{tax: 10_000 * money.Dollar, farm: 6_666}, false, ""},
		"a fisherman":                       {household{tax: 10_000 * money.Dollar, fish: 10_000}, false, ""},
		"nursing home":                      {household{tax: 10_000 * money.Dollar, extras: &nursing}, true, "live permanently in a nursing home"},
		"no return required last year":      {household{tax: 10_000 * money.Dollar, prior: &notRequired}, true, ""},
		"last year covered by withholding":  {household{tax: 10_000 * money.Dollar, prior: prior(300_000, 300_000)}, true, ""},
		"last year one cent short":          {household{tax: 10_000 * money.Dollar, prior: prior(300_000, 299_999)}, false, ""},
		"no tax last year":                  {household{tax: 10_000 * money.Dollar, prior: prior(0, 0)}, true, ""},
		"covered last year, but six months": {household{tax: 10_000 * money.Dollar, prior: &shortZero}, false, ""},
	} {
		res := must(rules(t).Requirement(tc.h.facts(t)))
		if got := res.Value().Exempt(); got != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", name, got, tc.exempt, res.Value().Why())
		}
		if !strings.Contains(res.Value().Why(), tc.why) {
			t.Errorf("%s: the reason %q does not say %q", name, res.Value().Why(), tc.why)
		}
		if tc.exempt {
			if p := must(rules(t).Penalty(tc.h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 || len(p.Value().Lines()) != 0 {
				t.Errorf("%s: a penalty of %v", name, p.Value().Total())
			}
		}
	}

	// Last year's withholding not given: the test is taken on the tax alone,
	// and the withholding is asked for.
	noWithholding := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(300_000).Build())).Build())
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &noWithholding}.facts(t)))
	if res.Value().Exempt() || !has(res.Required(), working.PositionPriorYearReturn) {
		t.Errorf("last year's withholding not given: exempt %v, asked %v", res.Value().Exempt(), res.Required())
	}
}

// IL-2210 takes each quarter's withholding off that quarter's installment and
// carries an excess forward, never back. Withholding in June does not pay
// April's shortfall; it covers June and September. The status reads the
// running totals, so September is short of the $3,000 needed by then though
// its own installment is paid.
func TestEstimatedWithholdingMeetsItsOwnQuarter(t *testing.T) {
	h := household{tax: 444_444, withholding: []estimated.Withheld{withheldOn(day(2026, 6, 1), 2_000*money.Dollar)}, asWithheld: true}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	wantStatus := []estimated.Status{estimated.StatusShort, estimated.StatusMet, estimated.StatusShort, estimated.StatusShort}
	wantShort := []money.USCents{1_000 * money.Dollar, 0, 0, 1_000 * money.Dollar}
	for i, in := range sched {
		if in.Status() != wantStatus[i] || in.Shortfall() != wantShort[i] {
			t.Errorf("installment %d is %v, %v short; want %v, %v short", i+1, in.Status(), in.Shortfall(), wantStatus[i], wantShort[i])
		}
	}
	// April's $1,000 unpaid, 10%, and January's $1,000, 44 days late by
	// February 28, 10%.
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 2, 28))); p.Value().Total() != 200*money.Dollar {
		t.Errorf("penalty %v, want $200", p.Value().Total())
	}

	// Without the election the same $2,000 is $500 a quarter: each quarter
	// $500 short, all unpaid.
	h.asWithheld = false
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 2, 28))); p.Value().Total() != 200*money.Dollar || len(p.Value().Lines()) != 4 {
		t.Errorf("spread evenly: penalty %v over %d lines, want $200 over four", p.Value().Total(), len(p.Value().Lines()))
	}
}

// Pass-through withholding counts in full in the quarter whose due date is
// the first on or after the entity's year end: June 30 is the September
// quarter.
func TestEstimatedPassThroughWithholdingCountsAtTheEntitysYearEnd(t *testing.T) {
	e := must(answered().PassThroughWithholding(day(2026, 6, 30), 1_000*money.Dollar).Build())
	h := household{tax: 444_444, extras: &e, payments: quarterly(1_000*money.Dollar, 1_000*money.Dollar, 0, 1_000*money.Dollar)}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, in := range sched {
		if in.Status() != estimated.StatusMet {
			t.Errorf("installment %d is %v, want met", i+1, in.Status())
		}
	}
	// A December 31 year end counts toward January only. September's unpaid
	// $1,000 is not paid by it, as it would be if it were a payment applied to
	// the oldest quarter.
	e = must(answered().PassThroughWithholding(day(2026, 12, 31), 1_000*money.Dollar).Build())
	h = household{tax: 444_444, extras: &e, payments: quarterly(1_000*money.Dollar, 1_000*money.Dollar)}
	sched = must(rules(t).Schedule(h.facts(t))).Value()
	if sched[2].Shortfall() != 1_000*money.Dollar || sched[3].Shortfall() != 0 {
		t.Errorf("a December 31 year end: September %v short and January %v; want September $1,000 short and January paid", sched[2].Shortfall(), sched[3].Shortfall())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 100*money.Dollar {
		t.Errorf("penalty %v, want 10%% of September's $1,000", p.Value().Total())
	}
}

// Last year's overpayment counts on April 15, or on the day it was elected
// if that came after the extended due date of last year's return.
func TestEstimatedOverpaymentFromLastYear(t *testing.T) {
	credit := must(estimated.NewPayment().Amount(1_000 * money.Dollar).FromPriorYear().Build())
	h := household{tax: 444_444, payments: append([]estimated.Payment{credit}, pay(dues[1], 1_000*money.Dollar), pay(dues[2], 1_000*money.Dollar), pay(dues[3], 1_000*money.Dollar))}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 0 || !has(res.Required(), working.PositionJurisdictionFact) {
		t.Errorf("unanswered: penalty %v, asked %v; want none, and the election date asked for", res.Value().Total(), res.Required())
	}
	late := must(answered().OverpaymentElectedLate(day(2026, 6, 1)).Build())
	h.extras = &late
	// April's $1,000 is paid on June 1, 47 days late: 10%.
	if res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); res.Value().Total() != 100*money.Dollar {
		t.Errorf("elected June 1: penalty %v, want $100", res.Value().Total())
	}
}

// 86 Ill. Adm. Code 100.8010(d)(2) Examples 1 to 3: a required annual payment
// of $13,648, so $3,412 a quarter, and annualised installments of $1,278,
// $1,660 and $3,414, which leave $7,296 for January.
func TestEstimatedAnnualisedMatchesTheRulesExamples(t *testing.T) {
	mustContain(t, "adm-100-8010.txt",
		"determines his or her required annual payment under subsection (d)(1) to be $13,648.",
		"Taxpayer pays $1,278 as the required installment on April 15.",
		"Taxpayer pays $1,660 as the required installment on June 15.",
		"Taxpayer pays $3,414 as the required installment on September 15.",
		"the required installment under subsection (d)(1) for the installment due on January 15 is $7,296",
	)
	// 90% of $15,164.44 is $13,648. The annualised taxes give 22.5% of
	// $5,680 = $1,278; 45% of $6,528.89 = $2,938 less $1,278; and 67.5% of
	// $9,410.37 = $6,352 less $2,938.
	h := household{tax: 1_516_444, periods: []money.USCents{568_000, 652_889, 941_037, 1_516_444}}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	want := []money.USCents{127_800, 293_800, 635_200, 1_364_800}
	for i, in := range sched {
		if in.NeededBy() != want[i] {
			t.Errorf("installment %d needs %v by its date, want %v", i+1, in.NeededBy(), want[i])
		}
	}
}

// A return filed with the balance paid stops the penalty that day. Illinois
// has no January 31 waiver, so January is still charged, at 2% for 14 days.
func TestEstimatedReturnFiledAndPaid(t *testing.T) {
	h := household{tax: 444_444, payments: quarterly(1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar), returnFiled: day(2027, 1, 29)}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	if sched[3].Status() != estimated.StatusShort {
		t.Errorf("January is %v, want short", sched[3].Status())
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if p.Value().AsOf() != day(2027, 1, 29) || p.Value().Total() != 20*money.Dollar {
		t.Errorf("figured to %v at %v; want January 29, 2027 and $20", p.Value().AsOf(), p.Value().Total())
	}
}

// Every Illinois fact a caller can answer is asked for when it is not, and
// the reasonable-cause question when there is a penalty.
func TestEstimatedUnansweredFactsAreAsked(t *testing.T) {
	h := household{tax: 444_444, noExtras: true}
	res := must(rules(t).Requirement(h.facts(t)))
	if n := count(res.Required(), working.PositionJurisdictionFact); n != 2 {
		t.Errorf("%d Illinois facts asked, want the nursing home and pass-through withholding", n)
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if n := count(p.Required(), working.PositionJurisdictionFact); n != 3 {
		t.Errorf("%d Illinois facts asked for the penalty, want those two and interest", n)
	}
	if !has(p.Required(), working.PositionPenaltyWaiver) {
		t.Error("reasonable cause is not asked")
	}
	casualty := estimated.WaiverCasualty
	h.waiver = &casualty
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); has(p.Required(), working.PositionPenaltyWaiver) || p.Value().Waiver() != estimated.WaiverCasualty {
		t.Errorf("a casualty: waiver %v, still asked %v", p.Value().Waiver(), has(p.Required(), working.PositionPenaltyWaiver))
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"No liability last year":                 {"all Illinois withholding from last year's tax", "5/804(d)"},
		"Last year's return as a measure":        {"Column B", "5/804(c)(1)(B)(ii)"},
		"Withholding not from wages":             {"includes 1099 withholding", "(g)(3)"},
		"How withholding meets the installments": {"to later quarters only", "100.8010(e)(1)"},
		"Choosing actual withholding dates":      {"one-fourth", "5/804(g)(1)"},
		"The $1,000 threshold":                   {"$1,000 or less", "100.8000(a)(1)"},
		"Exceptions the form does not list":      {"five cases", "(g)(6)"},
		"The farmer test":                        {"federal gross income", "5/803(e)"},
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
		// Nothing withheld, no farming, no exception: none of the rules
		// changes anything.
		if d.AffectsHousehold() {
			t.Errorf("%q affects a household it does not touch", d.Rule())
		}
		statute := d.Rule() == "Last year's return as a measure" || d.Rule() == "Choosing actual withholding dates"
		if (d.Uses() == working.ReadingStatute) != statute {
			t.Errorf("%q uses %v", d.Rule(), d.Uses())
		}
	}
	// Withholding is what the withholding rules turn on.
	for _, d := range must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: 2_000 * money.Dollar}.facts(t))).Divergences() {
		if d.Rule() == "Withholding not from wages" && !d.AffectsHousehold() {
			t.Errorf("%q does not affect a household with withholding", d.Rule())
		}
	}
	for name, s := range map[string]string{
		"the statute's no-liability test": "had no tax liability",
		"the statute's non-wage rule":     "on the date it is made",
		"the rule's stale threshold":      "still says $500",
		"the rule's death exception":      "death",
	} {
		found := false
		for _, d := range res.Divergences() {
			found = found || strings.Contains(d.Statute(), s)
		}
		if !found {
			t.Errorf("%s (%q) is not in any divergence", name, s)
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := illinoistaxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	fiscal := must(illinoistaxcalc.NewEstimatedFacts().PassThroughWithholding(day(2025, 12, 31), 100).Build())
	for name, f := range map[string]estimated.Facts{
		"Pennsylvania's facts": must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":                 must(estimated.NewFacts(illinois, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":         must(estimated.NewFacts(illinois, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Illinois's").Build()),
		"a 2025 entity year":   must(estimated.NewFacts(illinois, 2026).FilingStatus(filing.Single).ThisYear(this).State(fiscal).Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := illinoistaxcalc.NewEstimatedFacts().PassThroughWithholding(day(2026, 12, 31), -1).Build(); err == nil {
		t.Error("negative pass-through withholding accepted")
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date accepted")
	}
}

// The farming and nursing home exceptions are in the reasons, pinned by
// TestEstimatedExceptions.
func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$1,000", "2%", "10%", "30 days", "90%", "100%", "not interest", "January 31", "22.5%"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if strings.Contains(strings.Join(e.Body(), " "), "§") {
		t.Error("the explanation's body cites sections")
	}
}

// Without this year's tax the target is 100% of last year's, the penalty is
// an upper bound, and with no prior return there is no target. An exception
// that needs nothing about this year still gives an exact zero.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(4_000*money.Dollar, 0)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 4_000*money.Dollar {
		t.Errorf("required %v, target %v; want $4,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{100_000, 200_000, 300_000, 400_000} {
		if sched[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, sched[i].NeededBy(), want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !pen.Value().UpperBound() || pen.Value().Total() != 400*money.Dollar {
		t.Errorf("penalty %v, upper bound %v; want at most $400", pen.Value().Total(), pen.Value().UpperBound())
	}

	none := household{noTax: true, noPrior: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() {
		t.Error("a target with neither year's tax")
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}

	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	exempt := household{noTax: true, prior: &notRequired}
	p := must(rules(t).Penalty(exempt.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 0 || p.Value().UpperBound() {
		t.Errorf("no return required last year: penalty %v, upper bound %v; want an exact zero", p.Value().Total(), p.Value().UpperBound())
	}
}

// Illinois has no waiver for retirement or disability, where federal law has
// one: a retiree the IRS can excuse is refused. A casualty or disaster can be
// reasonable cause under 86 Ill. Adm. Code 700.400. The penalty is the same
// before any waiver in each case.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "adm-700-400.txt",
		"destruction by fire, other casualty or civil disturbance, of the taxpayer residence or place of business records",
		"(for example, because of a natural disaster)")
	base := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 4, 15))).Value().Total()
	if base == 0 {
		t.Fatal("no penalty to waive")
	}
	for _, tc := range []struct {
		name string
		pos  estimated.Positions
		want string
		says string
	}{
		{"a casualty", must(estimated.NewPositions().WaiverFor(estimated.CauseCasualty, day(2026, 8, 1)).Build()), "eligible", "fire or other casualty"},
		{"a disaster", must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()), "eligible", "within an IRS disaster extension"},
		{"retired in 2026 at 64", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build()), "refused", "no waiver for retirement or disability"},
		{"disabled in 2026", must(estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2026, 6, 1)).Build()), "refused", "no waiver for retirement or disability"},
	} {
		pos := tc.pos
		p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, positions: &pos}.facts(t), day(2027, 4, 15))).Value()
		o, ok := p.WaiverOutcome()
		got := map[bool]string{true: "eligible"}[o.Eligible()] + map[bool]string{true: "refused"}[o.Refused()] + map[bool]string{true: "undecided"}[o.Undecided()]
		switch {
		case p.Total() != base:
			t.Errorf("%s: penalty %v, want %v before any waiver", tc.name, p.Total(), base)
		case !ok || got != tc.want || !strings.Contains(o.Reason(), tc.says):
			t.Errorf("%s: %s %q, want %s saying %q", tc.name, got, o.Reason(), tc.want, tc.says)
		case o.Eligible() != (p.IfWaived() == 0):
			t.Errorf("%s: if waived %v", tc.name, p.IfWaived())
		}
	}
}

// Under an exception the target is $0, so last year's return isn't asked
// for as a way to change it. Without one, it is.
func TestEstimatedPriorReturnAskedOnlyWhenItCanSetTheTarget(t *testing.T) {
	under := must(rules(t).Requirement(household{tax: 800 * money.Dollar, noPrior: true}.facts(t)))
	if !under.Value().Exempt() || has(under.Required(), working.PositionPriorYearReturn) {
		t.Errorf("$1,000 or less: exempt %v, asked %v", under.Value().Exempt(), has(under.Required(), working.PositionPriorYearReturn))
	}
	over := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, noPrior: true}.facts(t)))
	if !has(over.Required(), working.PositionPriorYearReturn) {
		t.Error("over $1,000: last year's return is not asked for")
	}
}
