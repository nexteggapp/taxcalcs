// Pennsylvania's estimated tax rules for 2026, checked against the
// Department of Revenue's own documents.
//
// Pennsylvania publishes no exhaustive oracle for estimated tax. The strongest
// checks are REV-1630's two worked examples: when the penalty applies, and
// the interest on four $474 underpayments. Every threshold, rate, date and
// factor is compared against its extract in testdata/. The exceptions, the
// late start and the boundaries are hand-written cases.
package pennsylvaniataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/pennsylvaniataxcalc"
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
	return must(pennsylvaniataxcalc.EstimatedTax(2026))
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
	tax         money.USCents // after credits, PA-40 line 12 less lines 21 to 23
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *pennsylvaniataxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	periods     []money.USCents // income to each period end, for Exception 2
	noTax       bool
	positions   *estimated.Positions // a waiver asked for, with its cause and dates
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	b := estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).
		ThisYear(h.figures()).
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
		b.State(must(pennsylvaniataxcalc.NewEstimatedFacts().IncomeWithoutWithholding(100_000 * money.Dollar).
			Credits(0).PriorYearForgiveness(0).PriorYearFullForgiveness(false).Build()))
	}
	if h.periods != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, inc := range h.periods {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).Income(inc).Build()))
		}
		b.Elections(must(estimated.NewElections().Annualised().Build()))
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

// prior is a filed full-year resident return with its taxable income.
func prior(taxable, tax, withheld money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).TaxableIncome(taxable).Withheld(withheld).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(true).Build())
	return &p
}

func partYear(taxable, tax, withheld money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).TaxableIncome(taxable).Withheld(withheld).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(false).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "rev-413i-2026.txt",
		"The income threshold for when estimated payments are required to be made is $14,000 for the 2026 tax year.",
		"can reasonably expect to owe at least $430 ($14,000 of income not subject to employer withholding) in tax after subtracting withholding and credits",
		"Individuals that have $14,000 or more of income not subject to employer withholding for a tax year in which estimated tax payments are due may be subject to the estimated underpayment penalty.",
		"Multiply Line 1 by 3.07 percent (0.0307) and enter the result.",
		"Multiply Line 8 by 90 percent (0.90) and enter the result.",
		"If the amount is $430 or greater, this is the amount required to be paid in estimated tax for 2026. If the amount is less than $430, estimated tax payments are not required to be made.",
		"If a return was not filed for 2025 or a part-year resident return was filed, the safe harbor method is not permitted to be used.",
		"(NOTE: This exception does not apply if no return was filed in the prior year or the taxpayer was not a full-year resident of Pennsylvania.)",
		"For each installment period, the timely estimated tax payments and credits are at least 90 percent of the actual tax due on the income earned or received for each installment period.",
		"considered to be applied to the first estimated tax payment amount and period regardless of when the previous tax year’s return is filed",
		"Underpayment penalties will accrue for those periods until such estimated tax payments are received.",
		"or the next business day if the due date falls on a Saturday, Sunday, or holiday",
	)
	mustContain(t, "rev-413i-2026-due-dates.txt",
		"is: Apr. 15, 2026 Jun. 15, 2026 Sep. 15, 2026 Jan. 15, 2027",
		"Before April 1, 2026 4 25% 25% 25% 25%",
		"After March 31, 2026, and before June 1, 2026 3 50% 25% 25%",
		"After May 31, 2026, and before Sept. 1, 2026 2 75% 25%",
		"After Aug. 31, 2026 1 100%",
	)
	mustContain(t, "rev-1630-2025.txt",
		"Did you qualify for 100 percent tax forgiveness in 2024? If yes, stop here. You do not owe an estimated underpayment penalty",
		"Multiply the amount on Line 1a by 0.90.",
		"Line 13 (Total PA Tax Withheld), Line 17 (Nonresident Tax Withheld), Line 21 (Tax Forgiveness Credit), Line 22 (Resident Credit), and Line 23 (Total Other Credits).",
		"If Line 2 is more than Line 1b, you do not owe a penalty",
		"Divide Line 4 by 4 and enter the amount in each of the four Columns a through d.",
		"To calculate, multiply Pennsylvania taxable income from your 2024 return by 3.07 percent (0.0307). From this amount, subtract the amount of the Tax Forgiveness Credit on Line 21 of your 2024 return and enter the amount in Column d. Enter 25 percent of the Column d amount in Column a, 50 percent of the Column d amount in Column b, and 75 percent of the Column d amount in Column c.",
		"01/01/25 - 03/31/25 01/01/25 - 05/31/25 01/01/25 - 08/31/25 01/01/25 - 12/31/25",
		"Multiply Line A by 3.07 percent (0.0307). This is the tax due.",
		"Enter 90 percent of Exception 2 Line B.",
		"If you filed a final return and paid the total tax due by Jan. 31, 2026, enter the amount paid with your return as the fourth installment.",
		"that partial payment is considered a reduction, on the payment date, to the oldest payment period’s underpayment.",
		"Number of days on Line 14a times 0.000192 times underpayment on Line 9.",
		"or April 15, 2026, whichever is earlier",
		"USE WHOLE DOLLARS ONLY.",
		"Enter the amount of your 2024 PA Tax Liability (Line 12 from your 2024 PA-40 tax return), less the amounts from Lines 13, 17, 22, and 23 from your 2024 PA-40 tax return.",
		"If the amount for Line A is $338 or greater, or if you answer “No” to Line B, you do not qualify for the special exception.",
		"makes the full estimated tax payment due in the first period in which it is determined the estimated payment requirements were met",
		"the special exception does not apply for any payment periods",
		"at least 2/3 of your gross income from all sources",
	)
	mustContain(t, "rev-1611.txt", "2025 - 2026 7% 0.000192 1/1/25 - 12/31/26")
}

// The 2026 dates are the ones REV-413 (I) prints, and none moves.
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

// REV-1630's example of when the penalty applies: $3,599 of tax, $1,344 of
// withholding and credits, 90% of the tax is $3,239 and last year's income at
// this year's rate is $3,502. The form rounds to dollars; this does not.
func TestEstimatedRequirementMatchesREV1630Example(t *testing.T) {
	mustContain(t, "rev-1630-2025.txt",
		"Tax liability for current tax year Less current tax year’s withholding and credits $3,599 – 1,344 $2,255",
		"$3,239 (90 percent of the tax liability on the current return)",
		"$3,502 (PA taxable income on prior year’s return multiplied by the current year’s tax rate).",
		"In this case, $1,344 is less than both safe harbors.")
	// $114,071.66 at 3.07% is $3,502.00.
	f := household{tax: 3_599 * money.Dollar, withheld: 1_344 * money.Dollar, prior: prior(11_407_166, 3_000*money.Dollar, 0)}.facts(t)
	res := must(rules(t).Requirement(f))
	r := res.Value()
	if !r.Required() {
		t.Errorf("not required: %s", r.Why())
	}
	if r.Target() != 323_910 {
		t.Errorf("target %v, want 90%% of $3,599, $3,239.10", r.Target())
	}
	opts := r.Options()
	if opts[1].Basis() != estimated.BasisPriorYearIncome || opts[1].Amount() != 350_200 || !opts[1].Available() {
		t.Errorf("the prior-year option is %v %v (available %v), want last year's income at this year's rate, $3,502", opts[1].Basis(), opts[1].Amount(), opts[1].Available())
	}
	if c, _ := r.Chosen(); c.Basis() != estimated.BasisThisYearTax {
		t.Errorf("chose %v, want this year's tax", c.Basis())
	}
}

// REV-1630's example of the interest: $474 short on each date, unpaid. Its
// line 15a is $24, $18 and $10 for 260, 198 and 107 days of 2025. In 2026 the
// second date is June 15 rather than June 16, so 199 days, which still gives
// $18. The 2027 days have no published rate.
func TestEstimatedInterestMatchesREV1630Example(t *testing.T) {
	mustContain(t, "rev-1630-2025-example.txt",
		"9. Underpayment . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . . $474 $474 $474 $474",
		"payment due dates) . . . . . . . . . . . . . . . . . . . . . . . . . . . . . 14a. 260 198 107",
		"times underpayment of $474 . . . . . . . . . . . . . . . . . . . . . . 15a. $24 $18 $10")
	// 90% of $2,106.67 is $1,896.00, a quarter of which is $474.
	f := household{tax: 210_667}.facts(t)
	for _, asOf := range []estimated.Date{day(2026, 12, 31), day(2027, 4, 15)} {
		res := must(rules(t).Penalty(f, asOf))
		lines := res.Value().Lines()
		if len(lines) != 4 {
			t.Fatalf("%d lines, want four", len(lines))
		}
		wantDays := []int{260, 199, 107}
		wantCharge := []money.USCents{24, 18, 10}
		for i := range wantDays {
			if lines[i].Shortfall() != 474*money.Dollar {
				t.Errorf("line %d is on %v, want $474", i+1, lines[i].Shortfall())
			}
			parts := lines[i].Parts()
			if len(parts) != 1 || parts[0].Days() != wantDays[i] || parts[0].Charge() != wantCharge[i]*money.Dollar {
				t.Errorf("line %d: %d parts; want one of %d days and $%d", i+1, len(parts), wantDays[i], wantCharge[i])
				continue
			}
		}
		if res.Value().Total() != 52*money.Dollar {
			t.Errorf("to %v the penalty is %v, want $52", asOf, res.Value().Total())
		}
		if got := has(res.Required(), working.PositionUnpublishedRate); got != (asOf.Year() == 2027) {
			t.Errorf("to %v, unpublished rate reported %v", asOf, got)
		}
	}
}

// The interest follows REV-1611's printed 0.000192 a day, not 7% ÷ 365. On
// a $100,000 shortfall for 260 days the two differ by $6: $4,992 printed,
// $4,986 from the rate. REV-1630 rounds each year's charge to whole dollars.
func TestEstimatedInterestUsesThePrintedDailyFactor(t *testing.T) {
	// 90% of $444,444.44 is $400,000.00, a quarter of which is $100,000.
	f := household{tax: 44_444_444}.facts(t)
	res := must(rules(t).Penalty(f, day(2026, 12, 31)))
	parts := res.Value().Lines()[0].Parts()
	if len(parts) != 1 || parts[0].Principal() != 100_000*money.Dollar || parts[0].Days() != 260 {
		t.Fatalf("line 1 parts %+v, want one on $100,000 for 260 days", parts)
	}
	if parts[0].Charge() != 4_992*money.Dollar {
		t.Errorf("line 1 charges %v, want $4,992 at the printed factor", parts[0].Charge())
	}
}

// Exception 1 excuses an installment; it does not lower one. Paying a little
// over the prior-year figure each quarter owes nothing, but paying a little
// under it in April is charged on April's shortfall against 90% of this
// year's tax.
func TestEstimatedExceptionOneExcusesButDoesNotLower(t *testing.T) {
	// 90% of $17,777.78 is $16,000, so $4,000 a quarter. Last year's
	// $416,938.11 at 3.07% is $12,800, so $3,200 a quarter.
	last := prior(41_693_811, 12_000*money.Dollar, 0)
	f := household{tax: 1_777_778, prior: last, payments: quarterly(3_300*money.Dollar, 3_300*money.Dollar, 3_300*money.Dollar, 3_300*money.Dollar)}.facts(t)
	req := must(rules(t).Requirement(f))
	if req.Value().Target() != 12_800*money.Dollar {
		t.Errorf("target %v, want the prior-year $12,800", req.Value().Target())
	}
	if got := req.Value().Options()[1].Amount() / 4; got != 3_200*money.Dollar {
		t.Errorf("the prior-year test is %v a quarter, want $3,200", got)
	}
	// Each installment reaches its $3,200 a quarter, so it is met and shows
	// no shortfall. REV-1630's shortfall against 90% is excused by Exception
	// 1, so the penalty below is unchanged at $0.
	sched := must(rules(t).Schedule(f))
	for i, in := range sched.Value() {
		if in.Status() != estimated.StatusMet || in.Shortfall() != 0 {
			t.Errorf("installment %d is %v with %v short; want met with none", i+1, in.Status(), in.Shortfall())
		}
		if want := money.USCents(i+1) * 3_200 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	if p := must(rules(t).Penalty(f, day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v when every quarter met Exception 1", p.Value().Total())
	}

	f = household{tax: 1_777_778, prior: last, payments: quarterly(3_100*money.Dollar, 3_300*money.Dollar, 3_300*money.Dollar, 3_300*money.Dollar)}.facts(t)
	p := must(rules(t).Penalty(f, day(2027, 4, 15)))
	lines := p.Value().Lines()
	// April: $4,000 less $3,100 is $900, paid June 15: 61 days × 0.000192 ×
	// $900 = $10.54, so $11. Not $100 against the $3,200 test.
	if len(lines) != 1 || lines[0].Installment() != 1 || lines[0].Shortfall() != 900*money.Dollar || p.Value().Total() != 11*money.Dollar {
		t.Errorf("penalty %v over %d lines; want $11 on April's $900", p.Value().Total(), len(lines))
	}
}

// Exception 2 is 90% of the tax on the income actually received to each
// period end, not annualised.
func TestEstimatedExceptionTwoUsesIncomeToDate(t *testing.T) {
	h := household{
		tax:      1_777_778,
		periods:  []money.USCents{5_000 * money.Dollar, 20_000 * money.Dollar, 200_000 * money.Dollar, 600_000 * money.Dollar},
		payments: quarterly(200*money.Dollar, 400*money.Dollar, 5_000*money.Dollar, 10_400*money.Dollar),
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	// 90% of 3.07% of $5,000 is $138.15; of $20,000, $552.60; of $200,000,
	// $5,526. The last is capped by the regular $16,000. Each is reached, so
	// each is met with no shortfall; the first three were excused shortfalls
	// against 90%, and the penalty below is unchanged at $0.
	wantNeeded := []money.USCents{13_815, 55_260, 552_600, 1_600_000}
	for i, in := range sched.Value() {
		if in.NeededBy() != wantNeeded[i] || in.Status() != estimated.StatusMet || in.Shortfall() != 0 {
			t.Errorf("installment %d needs %v and is %v with %v short; want %v, met, none", i+1, in.NeededBy(), in.Status(), in.Shortfall(), wantNeeded[i])
		}
	}
	found := false
	for _, a := range sched.Applied() {
		found = found || strings.Contains(a.Applied(), "$138.15, $552.60, $5526.00")
	}
	if !found {
		t.Errorf("Exception 2's running totals are not $138.15, $552.60 and $5,526: %v", sched.Applied())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v when Exception 2 covers every short quarter", p.Value().Total())
	}
	h.periods = nil
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() == 0 {
		t.Error("without Exception 2 the same payments owe nothing")
	}
}

// A filer who first meets the requirement in July and owed no estimates last
// year has April and June excused by paying 75% by September 15. One who
// owed estimates last year is charged on April and June until the catch-up.
func TestEstimatedLateStarterSpecialException(t *testing.T) {
	payments := []estimated.Payment{pay(dues[2], 12_000*money.Dollar), pay(dues[3], 4_000*money.Dollar)}
	small := partYear(10_000*money.Dollar, 300*money.Dollar, 0) // line A $300, under $430
	h := household{tax: 1_777_778, prior: small, payments: payments, firstMet: day(2026, 7, 1)}
	sched := must(rules(t).Schedule(h.facts(t)))
	wantNeeded := []money.USCents{0, 0, 12_000 * money.Dollar, 16_000 * money.Dollar}
	for i, in := range sched.Value() {
		if in.NeededBy() != wantNeeded[i] {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), wantNeeded[i])
		}
		// The special exception leaves nothing needed by April and June, so
		// they show nothing due and no shortfall; the penalty below is
		// unchanged at $0.
		if i < 2 && (in.Status() != estimated.StatusNothingDue || in.Shortfall() != 0) {
			t.Errorf("installment %d is %v with %v short, want nothing due", i+1, in.Status(), in.Shortfall())
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v under the special exception", p.Value().Total())
	}

	// Short of the whole catch-up by September 15, the exception applies to no
	// installment.
	short := household{tax: 1_777_778, prior: small, firstMet: day(2026, 7, 1),
		payments: []estimated.Payment{pay(dues[2], 11_999*money.Dollar), pay(dues[3], 4_001*money.Dollar)}}
	if s := must(rules(t).Schedule(short.facts(t))).Value(); s[0].Status() != estimated.StatusShort || s[1].Status() != estimated.StatusShort {
		t.Errorf("April and June are %v and %v with the catch-up $1 short; want both short", s[0].Status(), s[1].Status())
	}

	// The same filer, not told when the requirement arose, is asked.
	h.firstMet = estimated.Date{}
	if res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); !has(res.Required(), working.PositionRequirementFirstMet) || res.Value().Total() == 0 {
		t.Errorf("without the date: penalty %v, asked %v", res.Value().Total(), has(res.Required(), working.PositionRequirementFirstMet))
	}

	// Line A of $5,000: no special exception. April's $4,000 runs 153 days to
	// September 15, $117.50, so $118; June's 92 days, $70.66, so $71.
	h = household{tax: 1_777_778, prior: partYear(10_000*money.Dollar, 5_000*money.Dollar, 0), payments: payments, firstMet: day(2026, 7, 1)}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 189*money.Dollar {
		t.Errorf("penalty %v, want $189", p.Value().Total())
	}

	// The form's groups: March 31 is in the first, April 1 in the second.
	for _, tc := range []struct {
		met     estimated.Date
		excused bool
	}{{day(2026, 3, 31), false}, {day(2026, 4, 1), true}} {
		h := household{tax: 1_777_778, prior: small, firstMet: tc.met,
			payments: []estimated.Payment{pay(dues[1], 8_000*money.Dollar), pay(dues[2], 4_000*money.Dollar), pay(dues[3], 4_000*money.Dollar)}}
		// Under the special exception April needs nothing; outside it, April is
		// short.
		got := must(rules(t).Schedule(h.facts(t))).Value()[0].Status() == estimated.StatusNothingDue
		if got != tc.excused {
			t.Errorf("first met %v: April excused %v, want %v", tc.met, got, tc.excused)
		}
	}
}

// The thresholds, as the form words them: $14,000 or more of income without
// withholding starts the rules, and $430 or more of tax after withholding.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		income   money.USCents
		withheld money.USCents
		exempt   bool
	}{
		{1_399_999, 0, true},
		{1_400_000, 0, false},
		{1_400_000, 957_001, true},  // $429.99 after withholding
		{1_400_000, 957_000, false}, // $430.00
	} {
		e := must(pennsylvaniataxcalc.NewEstimatedFacts().IncomeWithoutWithholding(tc.income).Credits(0).
			PriorYearForgiveness(0).PriorYearFullForgiveness(false).Build())
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld, extras: &e}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("income %v, withheld %v: exempt %v, want %v (%s)", tc.income, tc.withheld, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		// The exception that applies is the chosen option, at $0.
		want := "Income without withholding under $14,000"
		if tc.income >= 1_400_000 {
			want = "Tax less withholding and credits under $430"
		}
		if c, chosen := res.Value().Chosen(); tc.exempt && (!chosen || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != want) {
			t.Errorf("income %v, withheld %v: chose %v %q at %v, want %q", tc.income, tc.withheld, c.Basis(), c.Label(), c.Amount(), want)
		}
	}
}

// 100% Tax Forgiveness last year takes this year out of the penalty, and the
// credits are added back before the 90% is taken.
func TestEstimatedForgivenessAndCredits(t *testing.T) {
	e := must(pennsylvaniataxcalc.NewEstimatedFacts().PriorYearFullForgiveness(true).Build())
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &e}.facts(t)))
	if c, _ := res.Value().Chosen(); !res.Value().Exempt() || res.Value().Required() || c.Label() != "Full Tax Forgiveness last year" {
		t.Errorf("after 100%% forgiveness: chose %q: %s", c.Label(), res.Value().Why())
	}

	// $9,000 after $1,000 of credits: 90% of $10,000 is $9,000, less $1,000
	// is $8,000, where 90% of the $9,000 would be $8,100.
	e = must(pennsylvaniataxcalc.NewEstimatedFacts().IncomeWithoutWithholding(300_000 * money.Dollar).Credits(1_000 * money.Dollar).
		PriorYearForgiveness(0).PriorYearFullForgiveness(false).Build())
	res = must(rules(t).Requirement(household{tax: 9_000 * money.Dollar, extras: &e}.facts(t)))
	if res.Value().Target() != 8_000*money.Dollar {
		t.Errorf("target %v, want $8,000", res.Value().Target())
	}

	// Nothing said: every Pennsylvania fact is asked for.
	f := must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(9_000 * money.Dollar).Build())).Build())
	res = must(rules(t).Requirement(f))
	n := 0
	for _, r := range res.Required() {
		if r.Position() == working.PositionJurisdictionFact {
			n++
		}
	}
	if n != 3 {
		t.Errorf("%d Pennsylvania facts asked for, want income, credits and last year's forgiveness", n)
	}
}

// The prior-year test needs a filed full-year resident return with its
// taxable income.
func TestEstimatedPriorYearOptionAvailability(t *testing.T) {
	unanswered := must(estimated.NewPriorReturn().Filed(true).
		Figures(must(estimated.NewFigures().Tax(100).TaxableIncome(10_000).Build())).Build())
	noIncome := must(estimated.NewPriorReturn().Filed(true).
		Figures(must(estimated.NewFigures().Tax(100).Build())).FullYearResident(true).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   working.PositionKind
	}{
		"part-year":            {partYear(100_000*money.Dollar, 3_070*money.Dollar, 0), working.PositionUnspecified},
		"residence not given":  {&unanswered, working.PositionResidency},
		"taxable income unset": {&noIncome, working.PositionPriorYearReturn},
		"no return on record":  {nil, working.PositionPriorYearReturn},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		if res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
	}
}

// A return filed and paid by January 31 is the fourth installment, and the
// penalty stops the day it is paid.
func TestEstimatedReturnFiledAndPaid(t *testing.T) {
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 1, 29), true}, {day(2027, 2, 1), false}} {
		res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: tc.filed}.facts(t)))
		if got := res.Value()[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
	}
	p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 3, 1)}.facts(t), day(2027, 4, 15)))
	if p.Value().AsOf() != day(2027, 3, 1) {
		t.Errorf("figured to %v, want the day the return was paid", p.Value().AsOf())
	}
	// § 7352(d)(1): never beyond the fifteenth day of the fourth month.
	p = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 6, 30)))
	if p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"When the rules start":                         {"$14,000 or more", "§ 7325(a)(1)"},
		"A requirement first met after April 1":        {"special exception", "§§ 7325(d), 7326(a)"},
		"A return filed and paid by January 31":        {"Any final return", "§ 7325(h)"},
		"The prior-year test after a part-year return": {"only if you were a full-year resident", "§ 7352(d)(2)(A)"},
		"The daily interest factor":                    {"0.000192", "§ 806"},
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
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.StatuteSource(), w[1]) || d.FormSource() == "" || d.Statute() == "" || d.Uses() != working.ReadingForm {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s), uses %v", d.Rule(), d.Form(), d.Statute(), d.StatuteSource(), d.Uses())
		}
		// A full-year filer with $100,000 of income, no late start, no early
		// return and nothing paid is charged interest, and touched by nothing
		// else.
		if got, want := d.AffectsHousehold(), d.Rule() == "The daily interest factor"; got != want {
			t.Errorf("%q affects these facts %v, want %v", d.Rule(), got, want)
		}
	}
	affects := func(h household, rule string) bool {
		for _, d := range must(rules(t).Requirement(h.facts(t))).Divergences() {
			if d.Rule() == rule {
				return d.AffectsHousehold()
			}
		}
		t.Fatalf("no divergence %q", rule)
		return false
	}
	exactly := must(pennsylvaniataxcalc.NewEstimatedFacts().IncomeWithoutWithholding(14_000 * money.Dollar).Credits(0).
		PriorYearForgiveness(0).PriorYearFullForgiveness(false).Build())
	for name, tc := range map[string]struct {
		h    household
		rule string
		want bool
	}{
		"exactly $14,000":     {household{tax: 10_000 * money.Dollar, extras: &exactly}, "When the rules start", true},
		"first met in July":   {household{tax: 10_000 * money.Dollar, firstMet: day(2026, 7, 1)}, "A requirement first met after April 1", true},
		"filed January 29":    {household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 1, 29)}, "A return filed and paid by January 31", true},
		"part-year last year": {household{tax: 10_000 * money.Dollar, prior: partYear(100_000*money.Dollar, 3_070*money.Dollar, 0)}, "The prior-year test after a part-year return", true},
		"paid in full":        {household{tax: 10_000 * money.Dollar, payments: quarterly(2_250*money.Dollar, 2_250*money.Dollar, 2_250*money.Dollar, 2_250*money.Dollar)}, "The daily interest factor", false},
	} {
		if got := affects(tc.h, tc.rule); got != tc.want {
			t.Errorf("%s: %q affects %v, want %v", name, tc.rule, got, tc.want)
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := pennsylvaniataxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	farmer := must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(7000).Build())
	if _, err := rules(t).Requirement(farmer); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer: %v, want ErrUnsupported", err)
	}
	// Pennsylvania's farmer rules are for farming alone.
	fisher := must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).ThisYear(this).FishShare(7000).Build())
	if _, err := rules(t).Requirement(fisher); err != nil {
		t.Errorf("a fisher: %v", err)
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(estimated.Pennsylvania, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Pennsylvania's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	for _, s := range []string{"$14,000", "$430", "3.07%", "last year's income at this year's rate", "excuse a quarter", "without annualising"} {
		if !strings.Contains(e.Summary()+strings.Join(e.Body(), " ")+e.VersusFederal(), s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Without this year's tax, Pennsylvania's target is last year's income at
// this year's rate, and the installments are measured against it. REV-1630
// charges an installment that misses it on its shortfall against 90% of this
// year's tax, so the penalty is figured only when none does.
func TestEstimatedWithoutThisYearsTaxUsesLastYearsIncomeAlone(t *testing.T) {
	// $100,000 at 3.07% is $3,070, a quarter of which is $767.50.
	h := household{noTax: true, prior: prior(100_000*money.Dollar, 3_000*money.Dollar, 0)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 3_070*money.Dollar {
		t.Errorf("required %v, target %v; want $3,070: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != "Needs this year's tax" {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if c, ok := r.Chosen(); !ok || c.Basis() != estimated.BasisPriorYearIncome {
		t.Errorf("chose %v", c.Basis())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{76_750, 153_500, 230_250, 307_000} {
		if got := sched.Value()[i].NeededBy(); got != want {
			t.Errorf("installment %d needs %v, want %v", i+1, got, want)
		}
	}

	if _, err := rules(t).Penalty(h.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("unpaid: %v, want ErrNeedsThisYearTax", err)
	}
	paid := h
	paid.payments = quarterly(76_750, 76_750, 76_750, 76_750)
	pen := must(rules(t).Penalty(paid.facts(t), day(2027, 4, 15)))
	if pen.Value().Total() != 0 || pen.Value().UpperBound() {
		t.Errorf("paid to the prior-year test: penalty %v, upper bound %v; want an exact zero", pen.Value().Total(), pen.Value().UpperBound())
	}
}

// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
// never as a negative amount.
func TestEstimatedWithholdingOverTheTaxReadsAsARefund(t *testing.T) {
	figures := func(tax money.USCents) estimated.Figures {
		return must(estimated.NewFigures().Tax(tax).FederalAGI(60_000 * money.Dollar).StateAGI(60_000 * money.Dollar).Build())
	}
	last := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800 * money.Dollar)).Build())
	b := estimated.NewFacts("PA", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
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

// An exception sets a $0 target, so a question whose answer would only
// change the target is not asked under one.
func TestEstimatedExceptionAsksNothingAboutTheTarget(t *testing.T) {
	for _, c := range []struct {
		name   string
		h      household
		exempt bool
	}{
		{"withholding over the tax, no 2025 return", household{tax: 1_000 * money.Dollar, withheld: 2_000 * money.Dollar}, true},
		{"no withholding, no 2025 return", household{tax: 10_000 * money.Dollar}, false},
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

// Pennsylvania's estimated tax rules have none of the federal waivers, so
// every waiver asked for is refused and the penalty stands. Federal law
// grants a casualty waiver and one for retiring after 62.
func TestEstimatedWaiverIsRefusedUnlikeFederal(t *testing.T) {
	base := household{tax: 10_000 * money.Dollar}
	before := must(rules(t).Penalty(base.facts(t), day(2027, 6, 30)))
	if before.Value().Total() == 0 {
		t.Fatal("the household has no penalty to waive")
	}
	for _, c := range []struct {
		name      string
		positions estimated.Positions
	}{
		{"retired in 2026 at 64", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build())},
		{"a disaster", must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build())},
		{"a casualty", must(estimated.NewPositions().Waiver(estimated.WaiverCasualty).Build())},
	} {
		h := base
		h.positions = &c.positions
		p := must(rules(t).Penalty(h.facts(t), day(2027, 6, 30)))
		o, ok := p.Value().WaiverOutcome()
		switch {
		case !ok:
			t.Errorf("%s: the penalty has no waiver outcome", c.name)
		case !o.Refused():
			t.Errorf("%s: the waiver is not refused: %q", c.name, o.Reason())
		case !strings.Contains(o.Reason(), "Board of Appeals"):
			t.Errorf("%s: the reason %q does not say where an abatement goes", c.name, o.Reason())
		}
		if p.Value().Total() != before.Value().Total() || p.Value().IfWaived() != p.Value().Total() {
			t.Errorf("%s: the penalty is %v and %v if waived, against %v", c.name, p.Value().Total(), p.Value().IfWaived(), before.Value().Total())
		}
	}
}
