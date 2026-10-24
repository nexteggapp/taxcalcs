// Ohio's estimated tax rules for 2026, checked against the Department of
// Taxation's own documents and the Revised Code.
//
// Ohio publishes no worked example of the interest penalty. The strongest
// check is the 2025 IT/SD 2210's eight printed line 15 factors: the formula
// the 2026 rules use reproduces every one, including the third column's,
// which spans the change from 8% in 2025 to 7% in 2026. Every threshold,
// percentage, date and rate is compared against its extract in testdata/.
// The penalty cases are worked by hand from the form's line arithmetic.
package ohiotaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/ohiotaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

const ohio = estimated.Jurisdiction("OH")

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
	return must(ohiotaxcalc.EstimatedTax(2026))
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

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

func timely(on bool) *ohiotaxcalc.EstimatedFacts {
	e := must(ohiotaxcalc.NewEstimatedFacts().PriorYearReturnTimely(on).Build())
	return &e
}

type household struct {
	tax         money.USCents // after credits, IT 1040 line 10 less line 16
	noTax       bool
	withheld    money.USCents // withheld on December 15
	prior       *estimated.PriorReturn
	extras      *ohiotaxcalc.EstimatedFacts // nil is a timely prior return
	noExtras    bool
	payments    []estimated.Payment
	returnFiled estimated.Date
	elections   *estimated.Elections
	waiver      *estimated.WaiverKind
	firstMet    estimated.Date
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	fig := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		fig = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts(ohio, 2026).FilingStatus(filing.Single).ThisYear(fig).
		Paid(h.payments...).ReturnFiled(h.returnFiled).RequirementFirstMet(h.firstMet)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	switch {
	case h.noExtras:
	case h.extras != nil:
		b.State(*h.extras)
	default:
		b.State(*timely(true))
	}
	if h.elections != nil {
		b.Elections(*h.elections)
	}
	if h.waiver != nil {
		b.Positions(must(estimated.NewPositions().Waiver(*h.waiver).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

// prior is a filed twelve-month return with its tax after credits.
func prior(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).Build())
	return &p
}

// fortyThousand is a household whose target is $40,000: 90% of $44,444.44,
// and no prior return on record. Columns need $10,000, $20,000, $30,000 and
// $40,000.
func fortyThousand() household { return household{tax: 4_444_444} }

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "ites-2026.txt",
		"You should make estimated payments for tax year 2026 if your estimated Ohio tax liability (total tax minus total credits) less Ohio withholding is more than $500.",
		"Married taxpayers unsure of their filing status for 2026 should make separate estimated payments based on their individual estimated Ohio tax liabilities.",
		"special rules regarding estimated payments apply to certain taxpayers with farming and/or fishing income. See Adm. Code 5703-7-04.",
	)
	mustContain(t, "ites-2026-layout.txt",
		"1st quarter - April 15, 2026",
		"2nd quarter - June 15, 2026",
		"3rd quarter - Sept. 15, 2026",
		"4th quarter - Jan. 15, 2027",
		"10. Multiply line 8 by 90% (.90)",
		"11. Enter the lesser of line 9 or line 10 (If you did not file a 2025 IT 1040, enter the amount from line 10)",
		"12. 2025 credit carryforward and expected 2026 Ohio income tax withholding",
		"14. Your quarterly estimated payment. Multiply line 13 by 25% (.25)",
		"$ 342.00 plus 2.750% of the amount in excess of $ 26,050",
		"$ 2,394.32 plus 3.125% of the amount in excess of $100,000",
	)
	mustContain(t, "itsd2210-2025.txt",
		"1. 2025 Ohio income taxes paid (timely paid 2025 estimated payments plus withholding plus 2024 credit carryforward)",
		"4. Multiply line 2 by 90% (.90)",
		"5. Is line 1 greater than or equal to line 4? If yes, STOP, you have no interest penalty.",
		"6. Did you timely file a 2024 Ohio income tax return? If yes, continue to line 7. If no, skip to line 8",
		"7. Is line 1 greater than or equal to line 3? If yes, STOP, you have no interest penalty.",
		"8. Is line 2 less any withholding $500 or less? If yes, STOP, you have no interest penalty.",
		"9. If you answered “Yes” on line 6, enter the lesser of line 3 or line 4. If you answered “No”, enter the amount from line 4.",
		"A 4/15/25 – 25% B 6/16/25 – 50% C 9/15/25 – 75% D 1/15/26 – 100% 0.013580 0.019932 0.026311 0.017248",
		"10. Multiply the amount on line 9 by the percentage indicated at the top of each column",
		"11. Multiply the total tax withheld from compensation by the percentage indicated at the top of each column",
		"12. Total cumulative estimated tax paid (including any credit carryforwards) by the dates shown at the top of each column",
		"14. Underpayment subject to interest penalty (line 10 minus line 13; if negative, enter zero)",
		"16. Multiply line 14 by line 15 for each column",
		"A 4/15/25 – 25% B 7/15/25 – 50% C 10/15/25 – 75% D 1/15/26 – 100% 0.019932 0.020151 0.019740 0.017248",
		"based on the statutory interest rate (8% for 2025 and 7% for 2026) and the number of days the estimated payment was late.",
		"Rate = interest rate X number of days the payment is late ÷ 365.25",
		"If a due date falls on a weekend or holiday, the payment is due the next business day.",
		"● The 15th day of the 4th month of the tax year; ● The 15th day of the 6th month of the tax year; ● The 15th day of the 9th month of the tax year; ● The 15th day of the 1st month following the end of the tax year.",
		"complete page 2 using your combined liability for individual and school district income tax",
		"you receive at least 2/3 of your gross income from farming or fishing activities",
	)
	mustContain(t, "it1040-booklet-2025-pages.txt",
		"Round all figures to the nearest dollar.",
		"The interest rate for calendar year 2026 is 7%.",
		"These servicemembers do not owe interest, penalties, or the interest penalty in connection with this extension period.",
	)
	mustContain(t, "orc-5747-09.txt",
		"Effective: September 30, 2025 Latest Legislation: House Bill 96 - 136th General Assembly",
		"(4) \"Required installment\" means a payment equal to twenty-five per cent of the lesser of the following: (a) Ninety per cent of the tax liability for the taxable year; (b) One hundred per cent of the tax liability shown on the return of a taxpayer for the preceding taxable year.",
		"applies only if the taxpayer filed a return under section 5747.08 of the Revised Code for the preceding taxable year and if the preceding taxable year was a twelve-month taxable year.",
		"less the amount to be withheld from the taxpayer's compensation, is more than five hundred dollars.",
		"taxes withheld from compensation shall be considered as paid in equal amounts on each payment date unless the taxpayer establishes the dates on which all amounts were actually withheld",
		"(1) On or before the fifteenth day of the fourth month after the beginning of the taxable year, twenty-two and one-half per cent",
		"(4) On or before the fifteenth day of the first month of the following taxable year, ninety per cent",
		"On or before the fifteenth day of the fourth month of the year following that for which the declaration or amended declaration was filed, an annual return shall be filed",
		"an interest penalty may be added to the taxes for the tax year at the rate per annum prescribed by section 5703.47 of the Revised Code",
		"the required installment less the amount of taxes paid by the date prescribed for that payment;",
		"The period of the underpayment shall run from the day the estimated payment was required to be made to the date on which the payment is made.",
		"shall be considered a payment of any previous underpayment only to the extent the payment of estimated taxes exceeds the amount of the payment presently required to be paid to avoid any penalty.",
		"The tax commissioner may abate, in whole or in part, the interest penalty imposed under division (D) of this section.",
		"determined by annualizing the income received during the year up to the end of the month immediately preceding the month in which the payment is due;",
		"(2) The amount of tax that was paid equals at least one hundred per cent of the tax liability shown on the return of the taxpayer for the preceding taxable year",
	)
	mustContain(t, "orc-5747-09-2018.txt", "Effective: September 28, 2018", "an interest penalty shall be added to the taxes for the tax year")
	mustContain(t, "hb96-section-801-40.txt",
		"(136th General Assembly)",
		"801.40. The amendment by this act of section 5747.09 and division (C) of section 5747.43 of the Revised Code applies to taxable years beginning on or after January 1, 2025.")
	mustContain(t, "orc-5703-47.txt",
		"On the fifteenth day of October of each year, the tax commissioner shall determine the federal short-term rate.",
		"rounded to the nearest whole number per cent, plus three per cent, shall be the interest rate per annum used in making the computation for interest that accrues during the following calendar year.")
	mustContain(t, "orc-5747-02.txt",
		"Effective: September 30, 2025 Latest Legislation: House Bill 96 - 136th General Assembly",
		"(c) For taxable years beginning in 2026 and thereafter, $332.00 plus 2.75% of the amount in excess of $26,050.")
	mustContain(t, "oac-5703-7-04.txt",
		"Effective: November 28, 2024",
		"(1) At least two-thirds of the individual's estimated gross income is estimated gross income from farming or estimated gross income from fishing;")
	mustContain(t, "taxalert-itsd2210-2017.txt",
		"The taxpayer made estimated payments for the current tax year totaling at least 100% of the previous year’s tax liability; or",
		"The taxpayer made estimated payments for the current tax year totaling at least 90% of the current year’s tax liability.",
		"The long-form used for the annualized calculation of the interest penalty is no longer available.")
	mustContain(t, "research-oh-quotes.txt",
		"[AJE] the interest rate prescribed by R.C. 5703.47 for calendar year 2026 is seven percent (7%).",
		"[FAQ] until the later of when the installment is paid or the return is due",
		"[FAQ] Ohio law does not contain a specific safe harbor for first time filers.",
		"[NEWS] is required by law and cannot be removed.")

	// The rate series is 7% for calendar year 2026 and nothing else.
	ps := ohiotaxcalc.RatesFor(2026).Periods()
	if len(ps) != 1 || ps[0].From() != day(2026, 1, 1) || ps[0].To() != day(2026, 12, 31) || ps[0].Rate() != money.Percent(7) {
		t.Errorf("rates %+v, want 7%% for 2026 alone", ps)
	}
}

// The 2026 dates are the ones IT 1040ES prints. Each is a weekday and none
// is an ORC 1.14 holiday, so none moves.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(fortyThousand().facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
		if want := money.USCents(i+1) * 10_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
}

// The calendar is ORC 1.14's holidays, worked out here from the section's
// own rules, and it moves a weekend or holiday to the next business day.
func TestEstimatedCalendarMovesWeekendsAndHolidays(t *testing.T) {
	mustContain(t, "orc-1-14.txt",
		"(B) The third Monday in January, known as Martin Luther King day;",
		"(C) The third Monday in February, known as Washington-Lincoln day;",
		"(E) The nineteenth day of June, known as Juneteenth day;",
		"(G) The first Monday in September, known as Labor day;",
		"(H) The second Monday in October, known as Columbus day;",
		"(J) The fourth Thursday in November, known as Thanksgiving day;",
		"If any day designated in this section as a legal holiday falls on Sunday, the next succeeding day is a legal holiday.")
	nth := func(year int, m time.Month, wd time.Weekday, n int) estimated.Date {
		d := day(year, m, 1)
		for d.Weekday() != wd {
			d = d.AddDays(1)
		}
		return d.AddDays(7 * (n - 1))
	}
	lastMonday := func(year int, m time.Month) estimated.Date {
		d := day(year, m+1, 1).AddDays(-1)
		for d.Weekday() != time.Monday {
			d = d.AddDays(-1)
		}
		return d
	}
	var want []estimated.Date
	for _, y := range []int{2026, 2027} {
		want = append(want, day(y, 1, 1), nth(y, 1, time.Monday, 3), nth(y, 2, time.Monday, 3))
		if y == 2026 {
			want = append(want, lastMonday(y, 5), day(y, 6, 19), day(y, 7, 4), nth(y, 9, time.Monday, 1),
				nth(y, 10, time.Monday, 2), day(y, 11, 11), nth(y, 11, time.Thursday, 4), day(y, 12, 25))
		}
	}
	got := ohiotaxcalc.CalendarFor(2026).Holidays()
	if len(got) != len(want) {
		t.Fatalf("%d holidays, want %d", len(got), len(want))
	}
	for i, h := range got {
		if h.On() != want[i] {
			t.Errorf("holiday %d (%s) is %v, want %v", i+1, h.Name(), h.On(), want[i])
		}
		if h.On().Weekday() == time.Sunday {
			t.Errorf("%s falls on a Sunday and needs the Monday after it", h.Name())
		}
	}

	c := ohiotaxcalc.CalendarFor(2026)
	for _, tc := range []struct{ from, to estimated.Date }{
		{day(2026, 6, 19), day(2026, 6, 22)}, // Juneteenth, a Friday
		{day(2027, 1, 16), day(2027, 1, 19)}, // a Saturday, then Martin Luther King day
		{day(2025, 6, 15), day(2025, 6, 16)}, // the Sunday the 2025 form moves to 6/16/25
		{day(2026, 6, 15), day(2026, 6, 15)},
	} {
		if got := c.NextBusinessDay(tc.from); got != tc.to {
			t.Errorf("%v moves to %v, want %v", tc.from, got, tc.to)
		}
	}
}

// The $500 test, one cent either side: tax after credits less withholding of
// $500 or less needs no estimates and owes no penalty.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{500 * money.Dollar, true}, // $500.00 left
		{49_999, false},            // $500.01 left
		{50_001, true},             // $499.99 left
	} {
		f := household{tax: 1_000 * money.Dollar, withheld: tc.withheld}.facts(t)
		req := must(rules(t).Requirement(f)).Value()
		if req.Exempt() != tc.exempt || req.Required() == tc.exempt {
			t.Errorf("withheld %v: exempt %v, required %v; want exempt %v (%s)", tc.withheld, req.Exempt(), req.Required(), tc.exempt, req.Why())
		}
		p := must(rules(t).Penalty(f, day(2027, 4, 15))).Value()
		if (p.Total() == 0) != tc.exempt {
			t.Errorf("withheld %v: penalty %v", tc.withheld, p.Total())
		}
		// The exception is the chosen option, at $0, and the penalty's reason.
		if c, ok := req.Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding of $500 or less" || p.Reason() != "Tax less withholding of $500 or less.") {
			t.Errorf("withheld %v: chose %v %q at %v, reason %q", tc.withheld, c.Basis(), c.Label(), c.Amount(), p.Reason())
		}
	}
}

// The target is the lesser of 90% of this year's tax and 100% of last
// year's, and last year's is open only after a timely filed return.
func TestEstimatedTargetOptions(t *testing.T) {
	for name, tc := range map[string]struct {
		prior  *estimated.PriorReturn
		extras *ohiotaxcalc.EstimatedFacts
		chosen estimated.Basis
		target money.USCents
		ask    working.PositionKind
	}{
		"last year lower":        {prior(20_000 * money.Dollar), nil, estimated.BasisPriorYearTax, 20_000 * money.Dollar, working.PositionUnspecified},
		"this year lower":        {prior(50_000 * money.Dollar), nil, estimated.BasisThisYearTax, 40_000 * money.Dollar, working.PositionUnspecified},
		"a cent apart":           {prior(3_999_999), nil, estimated.BasisPriorYearTax, 3_999_999, working.PositionUnspecified},
		"filed late":             {prior(20_000 * money.Dollar), timely(false), estimated.BasisThisYearTax, 40_000 * money.Dollar, working.PositionUnspecified},
		"timeliness not given":   {prior(20_000 * money.Dollar), &ohiotaxcalc.EstimatedFacts{}, estimated.BasisThisYearTax, 40_000 * money.Dollar, working.PositionJurisdictionFact},
		"no return on record":    {nil, nil, estimated.BasisThisYearTax, 40_000 * money.Dollar, working.PositionPriorYearReturn},
		"no return filed":        {func() *estimated.PriorReturn { p := must(estimated.NewPriorReturn().Build()); return &p }(), nil, estimated.BasisThisYearTax, 40_000 * money.Dollar, working.PositionUnspecified},
		"nothing owed and filed": {prior(0), nil, estimated.BasisPriorYearTax, 0, working.PositionUnspecified},
	} {
		h := fortyThousand()
		h.prior, h.extras = tc.prior, tc.extras
		res := must(rules(t).Requirement(h.facts(t)))
		r := res.Value()
		c, _ := r.Chosen()
		if c.Basis() != tc.chosen || r.Target() != tc.target {
			t.Errorf("%s: chose %v at %v, want %v at %v", name, c.Basis(), r.Target(), tc.chosen, tc.target)
		}
		if opt := r.Options()[1]; opt.Available() != (tc.chosen == estimated.BasisPriorYearTax || name == "this year lower") {
			t.Errorf("%s: last year's option available %v (%s)", name, opt.Available(), opt.WhyNot())
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
	}

	// The form asks nothing about a short year, so a six-month return counts.
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(must(estimated.NewFigures().Tax(20_000 * money.Dollar).Build())).Build())
	h := fortyThousand()
	h.prior = &short
	if r := must(rules(t).Requirement(h.facts(t))).Value(); r.Target() != 20_000*money.Dollar {
		t.Errorf("after a six-month return the target is %v, want last year's $20,000", r.Target())
	}
}

// The 2025 IT/SD 2210 prints eight line 15 factors. The formula the 2026
// rules use, 8% for 2025 and 7% for 2026 times the days late ÷ 365.25 to six
// places, gives every one. The third columns span the change of rate on
// January 1, 2026, the nearest published change: 107 or 77 days at 8% and 15
// at 7%.
func TestEstimatedFactorsReproduceTheTwentyTwentyFiveForm(t *testing.T) {
	rates := must(estimated.NewRateSeries().
		Period(day(2025, 1, 1), day(2025, 12, 31), money.Percent(8), "IT/SD 2210 (2025): 8% for 2025").
		Period(day(2026, 1, 1), day(2026, 12, 31), money.Percent(7), "IT/SD 2210 (2025): 7% for 2026").
		Build())
	for _, tc := range []struct {
		from, to estimated.Date
		want     string
	}{
		// Page 2, individuals.
		{day(2025, 4, 15), day(2025, 6, 16), "0.013580"},
		{day(2025, 6, 16), day(2025, 9, 15), "0.019932"},
		{day(2025, 9, 15), day(2026, 1, 15), "0.026311"},
		{day(2026, 1, 15), day(2026, 4, 15), "0.017248"},
		// Page 3, pass-through entities, due in the 4th, 7th and 10th months.
		{day(2025, 4, 15), day(2025, 7, 15), "0.019932"},
		{day(2025, 7, 15), day(2025, 10, 15), "0.020151"},
		{day(2025, 10, 15), day(2026, 1, 15), "0.019740"},
	} {
		if got := must(ohiotaxcalc.FactorFor(2026, tc.from, tc.to, rates)); got != tc.want {
			t.Errorf("%v to %v: %s, want %s as printed", tc.from, tc.to, got, tc.want)
		}
	}
	// At 8% alone the third column would be 0.026721, not the printed one.
	eight := must(estimated.NewRateSeries().Period(day(2025, 1, 1), day(2026, 12, 31), money.Percent(8), "8% throughout").Build())
	if got := must(ohiotaxcalc.FactorFor(2026, day(2025, 9, 15), day(2026, 1, 15), eight)); got != "0.026721" {
		t.Errorf("at 8%% throughout: %s, want 0.026721", got)
	}
}

// No payments against a $40,000 target. Each column's line 14 is the
// cumulative shortfall, charged at the span's factor and rounded to the
// dollar: $10,000 × 0.011691 is $116.91, so $117; $20,000 × 0.017632 is
// $352.64, so $353; $30,000 × 0.020507 for September 15 to December 31 is
// $615.21, so $615. The 2027 rate is not published, so the third column's
// January days and the whole fourth column are not charged.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	res := must(rules(t).Penalty(fortyThousand().facts(t), day(2027, 4, 15)))
	p := res.Value()
	lines := p.Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	want := []struct {
		short  money.USCents
		days   int
		factor string
		charge money.USCents
	}{
		{10_000 * money.Dollar, 61, "0.011691", 117 * money.Dollar},
		{20_000 * money.Dollar, 92, "0.017632", 353 * money.Dollar},
		{30_000 * money.Dollar, 107, "0.020507", 615 * money.Dollar},
		{40_000 * money.Dollar, 0, "", 0},
	}
	for i, w := range want {
		l := lines[i]
		if l.Shortfall() != w.short || l.Days() != w.days || l.Total() != w.charge {
			t.Errorf("line %d: %v short, %d days, %v; want %v, %d, %v", i+1, l.Shortfall(), l.Days(), l.Total(), w.short, w.days, w.charge)
		}
		parts := l.Parts()
		if w.factor == "" {
			if len(parts) != 0 {
				t.Errorf("line %d has %d parts with no published rate", i+1, len(parts))
			}
			continue
		}
		if len(parts) != 1 || parts[0].Rate() != money.Percent(7) || !strings.Contains(parts[0].Source(), w.factor) {
			t.Errorf("line %d parts %+v, want one at 7%% and %s", i+1, parts, w.factor)
		}
	}
	if p.Total() != 1_085*money.Dollar || p.UpperBound() {
		t.Errorf("penalty %v (upper bound %v), want exactly $1,085", p.Total(), p.UpperBound())
	}
	if lines[2].Parts()[0].To() != day(2026, 12, 31) {
		t.Errorf("the third column is charged to %v, want December 31, 2026", lines[2].Parts()[0].To())
	}
	n := 0
	for _, r := range res.Required() {
		if r.Position() == working.PositionUnpublishedRate {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d unpublished-rate questions, want the 2026 form's factors and the 2027 rate", n)
	}

	// Figured to May 15, 2026, only 30 days of the first column: 7% × 30 ÷
	// 365.25 is 0.005749, and $57.49 rounds to $57. No 2027 day is reached.
	res = must(rules(t).Penalty(fortyThousand().facts(t), day(2026, 5, 15)))
	if res.Value().Total() != 57*money.Dollar || len(res.Value().Lines()) != 1 || res.Value().AsOf() != day(2026, 5, 15) {
		t.Errorf("to May 15: %v over %d lines, want $57 on one", res.Value().Total(), len(res.Value().Lines()))
	}
	n = 0
	for _, r := range res.Required() {
		if r.Position() == working.PositionUnpublishedRate {
			n++
		}
	}
	if n != 1 {
		t.Errorf("to May 15: %d unpublished-rate questions, want only the 2026 form's factors", n)
	}
}

// A payment between two due dates counts only from the next one. April's
// $10,000 paid on May 1 is still charged April's whole span, $117, and the
// June and September columns are short by $10,000 and $20,000 less than
// before: $176 and $410. The statute would charge April's 16 days alone.
func TestEstimatedPaymentBetweenDatesCountsAtTheNextDate(t *testing.T) {
	h := fortyThousand()
	h.payments = []estimated.Payment{pay(day(2026, 5, 1), 10_000*money.Dollar)}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	lines := p.Lines()
	if lines[0].Shortfall() != 10_000*money.Dollar || lines[0].Days() != 61 || lines[0].Total() != 117*money.Dollar {
		t.Errorf("April: %v short, %d days, %v; want $10,000 for 61 days, $117", lines[0].Shortfall(), lines[0].Days(), lines[0].Total())
	}
	if lines[1].Shortfall() != 10_000*money.Dollar || lines[2].Shortfall() != 20_000*money.Dollar {
		t.Errorf("June and September short %v and %v, want $10,000 and $20,000", lines[1].Shortfall(), lines[2].Shortfall())
	}
	if p.Total() != 703*money.Dollar {
		t.Errorf("penalty %v, want $117 + $176 + $410 = $703", p.Total())
	}
}

// Lines 1 and 5: withholding and timely payments that reach the target for
// the year owe nothing, whenever in the year they came. A cent short, or the
// same payment after January 15, and every column is charged.
func TestEstimatedWholeYearTest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payment estimated.Payment
		total   money.USCents
	}{
		{"the whole target on January 15", pay(day(2027, 1, 15), 40_000*money.Dollar), 0},
		{"a cent short", pay(day(2027, 1, 15), 3_999_999), 1_085 * money.Dollar},
		{"the whole target on January 19", pay(day(2027, 1, 19), 40_000*money.Dollar), 1_085 * money.Dollar},
	} {
		h := fortyThousand()
		h.payments = []estimated.Payment{tc.payment}
		if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(); p.Total() != tc.total {
			t.Errorf("%s: penalty %v, want %v", tc.name, p.Total(), tc.total)
		}
	}
	h := fortyThousand()
	h.payments = []estimated.Payment{pay(day(2027, 1, 15), 40_000*money.Dollar)}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, in := range sched {
		want := estimated.StatusExcused
		if i == 3 {
			want = estimated.StatusMet
		}
		if in.Status() != want || (i < 3 && in.Shortfall() != 10_000*money.Dollar) {
			t.Errorf("installment %d is %v with %v short, want %v", i+1, in.Status(), in.Shortfall(), want)
		}
	}
}

// Line 7: after a timely return, payments for the year that reach last
// year's tax owe nothing. After a late one the target is 90% of this year's.
func TestEstimatedPriorYearSafeHarbour(t *testing.T) {
	h := fortyThousand()
	h.prior = prior(20_000 * money.Dollar)
	h.payments = []estimated.Payment{pay(day(2027, 1, 15), 20_000*money.Dollar)}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(); p.Total() != 0 {
		t.Errorf("after a timely return: penalty %v, want none", p.Total())
	}
	h.extras = timely(false)
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(); p.Total() != 1_085*money.Dollar {
		t.Errorf("after a late return: penalty %v, want $1,085", p.Total())
	}
}

// Line 11: withholding counts 25% by each date, whenever it was withheld,
// and the election to use the dates withheld has no line on the form.
// $20,000 withheld in December against $40,000: each column is half short.
// $5,000 × 0.011691 is $58.46, so $58; $10,000 × 0.017632 is $176.32, so
// $176; $15,000 × 0.020507 is $307.61, so $308.
func TestEstimatedWithholdingIsSpreadEvenly(t *testing.T) {
	h := fortyThousand()
	h.withheld = 20_000 * money.Dollar
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 542*money.Dollar {
		t.Errorf("penalty %v, want $542", p.Value().Total())
	}
	e := must(estimated.NewElections().WithholdingAsWithheld().Build())
	h.elections = &e
	q := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if q.Value().Total() != p.Value().Total() || !applied(q.Applied(), "no line for crediting withholding on the dates it was withheld") {
		t.Errorf("with the election: penalty %v, and the rules do not say it is not applied", q.Value().Total())
	}
}

// Last year's overpayment applied forward counts on April 15. $10,000 of it
// meets April; June and September are short $10,000 and $20,000.
func TestEstimatedPriorYearOverpayment(t *testing.T) {
	h := fortyThousand()
	h.payments = []estimated.Payment{must(estimated.NewPayment().Amount(10_000 * money.Dollar).FromPriorYear().Build())}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if lines := p.Lines(); len(lines) != 3 || lines[0].Installment() != 2 || p.Total() != 586*money.Dollar {
		t.Errorf("penalty %v over %d lines, want $176 + $410 = $586 from June", p.Total(), len(lines))
	}
}

// The form runs the last span to April 15, 2027 whenever the return was
// filed, and never past it.
func TestEstimatedReturnFiledDoesNotShortenTheForm(t *testing.T) {
	h := fortyThousand()
	h.returnFiled = day(2027, 2, 1)
	res := must(rules(t).Penalty(h.facts(t), day(2027, 6, 30)))
	if res.Value().AsOf() != day(2027, 4, 15) || res.Value().Total() != 1_085*money.Dollar {
		t.Errorf("figured to %v at %v, want April 15, 2027 at $1,085", res.Value().AsOf(), res.Value().Total())
	}
	if !applied(res.Applied(), "filing early does not shorten the penalty") {
		t.Error("the rules do not say filing early does not stop the interest")
	}
}

// Ohio has no waiver. From tax year 2025 the commissioner may abate the
// penalty, which is shown before any abatement.
func TestEstimatedAbatementIsTheCommissioners(t *testing.T) {
	h := fortyThousand()
	k := estimated.WaiverCasualty
	h.waiver = &k
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 1_085*money.Dollar || res.Value().Waiver() != estimated.WaiverCasualty || !applied(res.Applied(), "may abate the penalty in whole or in part") {
		t.Errorf("penalty %v with waiver %v; want $1,085 before abatement, and the abatement said", res.Value().Total(), res.Value().Waiver())
	}
}

// The annualised test and a late start have no line on the form, so neither
// changes the figure, and the rules say so.
func TestEstimatedStatutoryTestsTheFormOmits(t *testing.T) {
	h := fortyThousand()
	e := must(estimated.NewElections().Annualised().Build())
	h.elections = &e
	f := must(estimated.NewFacts(ohio, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(h.tax).Build())).
		State(*timely(true)).Elections(e).
		Periods(
			must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 5, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).
		Build())
	res := must(rules(t).Penalty(f, day(2027, 4, 15)))
	if res.Value().Total() != 1_085*money.Dollar || !applied(res.Applied(), "the annualised election is not applied") {
		t.Errorf("annualised: penalty %v, and the rules do not say it is not applied", res.Value().Total())
	}

	h = fortyThousand()
	h.firstMet = day(2026, 7, 1)
	res = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 1_085*money.Dollar || !applied(res.Applied(), "Ohio has no exception for a late start") {
		t.Errorf("late start: penalty %v, and the rules do not say so", res.Value().Total())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(fortyThousand().facts(t)))
	want := map[string][2]string{
		"Last year's tax as the target":     {"Ohio return on time", "5747.09(A)(4)(b)"},
		"The whole-year tests":              {"however they were spread", "5747.09(D)(1) to (4) and (E)"},
		"What each column is short":         {"25%, 50%, 75% or 100%", "5747.09(A)(4) and (D)(1) to (4)"},
		"When the interest stops":           {"to the next due date, and the last to April 15", "ORC 5747.09(D)"},
		"Withholding on the dates withheld": {"no line for the dates", "ORC 5747.09(B)"},
		"The annualised income test":        {"withdrawn from tax year 2017", "ORC 5747.09(E)(1)"},
		"The 2026 worksheet's tax brackets": {"3.125% over $100,000", "5747.02(A)(3)(c)"},
	}
	statute := map[string]string{
		"Last year's tax as the target":     "on time or late",
		"The whole-year tests":              "by its own date",
		"What each column is short":         "only after the installment then due is covered",
		"When the interest stops":           "to the date the payment is made",
		"Withholding on the dates withheld": "establish the dates",
		"The annualised income test":        "annualised to the end of the month before",
		"The 2026 worksheet's tax brackets": "$332 plus 2.75%",
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
			t.Errorf("%q does not pin both readings: form %q (%s), statute %q (%s)", d.Rule(), d.Form(), d.FormSource(), d.Statute(), d.StatuteSource())
		}
	}
	// With nothing paid and no elections, the payment-order, withholding,
	// annualising and bracket readings change nothing.
	for _, d := range res.Divergences() {
		switch d.Rule() {
		case "What each column is short", "When the interest stops", "Withholding on the dates withheld", "The annualised income test", "The 2026 worksheet's tax brackets":
			if d.AffectsHousehold() {
				t.Errorf("%q affects a household that paid nothing", d.Rule())
			}
		}
	}
	// The out-of-date worksheet is dated against the act that replaced it.
	for _, d := range res.Divergences() {
		if d.Rule() == "The 2026 worksheet's tax brackets" && (!strings.Contains(d.Form(), "out of date") || !strings.Contains(d.StatuteSource(), "effective September 30, 2025")) {
			t.Errorf("the worksheet divergence does not say it is out of date and when the act took effect: %q, %q", d.Form(), d.StatuteSource())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := ohiotaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, share := range map[string]func(*estimated.FactsBuilder) *estimated.FactsBuilder{
		"a farmer":                  func(b *estimated.FactsBuilder) *estimated.FactsBuilder { return b.FarmShare(6_667) },
		"a fisher":                  func(b *estimated.FactsBuilder) *estimated.FactsBuilder { return b.FishShare(6_667) },
		"both, two-thirds together": func(b *estimated.FactsBuilder) *estimated.FactsBuilder { return b.FarmShare(4_000).FishShare(2_667) },
	} {
		f := must(share(estimated.NewFacts(ohio, 2026).FilingStatus(filing.Single).ThisYear(this)).Build())
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	under := must(estimated.NewFacts(ohio, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(6_666).Build())
	if _, err := rules(t).Requirement(under); err != nil {
		t.Errorf("a farmer just under two-thirds: %v", err)
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(ohio, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(ohio, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Ohio's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(fortyThousand().facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date was figured")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"more than $500", "90%", "100%", "on time", "whole year", "next date", "April 15", "7% for 2026", "365.25", "may reduce or remove it", "school district"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if e.VersusFederal() == "" || len(e.Sources()) == 0 {
		t.Error("the explanation has no comparison with federal, or no sources")
	}
}

// Without this year's tax the target is last year's tax, the most line 9 can
// be, and the penalty against it is an upper bound unless the year's payments
// reach it.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(20_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 20_000*money.Dollar {
		t.Errorf("required %v, target %v; want $20,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{5_000, 10_000, 15_000, 20_000} {
		if got := sched.Value()[i].NeededBy(); got != want*money.Dollar {
			t.Errorf("installment %d needs %v, want $%d", i+1, got, want)
		}
	}
	// $5,000, $10,000 and $15,000 short: $58 + $176 + $308.
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if pen.Total() != 542*money.Dollar || !(pen.UpperBound() || !pen.RatedThrough().IsZero()) {
		t.Errorf("unpaid: penalty %v, upper bound %v; want at most $542", pen.Total(), pen.UpperBound())
	}
	h.payments = []estimated.Payment{pay(day(2027, 1, 15), 20_000*money.Dollar)}
	pen = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if pen.Total() != 0 || pen.UpperBound() {
		t.Errorf("paid to last year's tax: penalty %v, upper bound %v; want an exact zero", pen.Total(), pen.UpperBound())
	}

	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() || r.Required() {
		t.Errorf("with no return on record there is a target: %s", r.Why())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
// never as a negative amount.
func TestEstimatedWithholdingOverTheTaxReadsAsARefund(t *testing.T) {
	figures := func(tax money.USCents) estimated.Figures {
		return must(estimated.NewFigures().Tax(tax).FederalAGI(60_000 * money.Dollar).StateAGI(60_000 * money.Dollar).Build())
	}
	last := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800 * money.Dollar)).Build())
	b := estimated.NewFacts("OH", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
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

// Ohio has no casualty, retirement or disability waiver. From tax year 2025
// the Tax Commissioner may abate the penalty, ORC 5747.09(D), which names no
// grounds, so a retirement the federal rules would waive is undecided.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	h := fortyThousand()
	before := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value().Total()
	if before == 0 {
		t.Fatal("no penalty to waive")
	}
	born := day(1962, 3, 1)
	for _, tc := range []struct {
		name, want, phrase string
		pos                *estimated.PositionsBuilder
	}{
		{"retired in 2026 at 64", "undecided", "no waiver for retirement or disability", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(born)},
		{"retired in 2020", "undecided", "abate all or part", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(born)},
		{"disaster", "undecided", "no waiver for a casualty or disaster as such", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1))},
	} {
		pos := must(tc.pos.Build())
		h.positions = &pos
		res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		p := res.Value()
		o, ok := p.WaiverOutcome()
		got := map[bool]string{true: "eligible"}[o.Eligible()]
		if o.Refused() {
			got = "refused"
		}
		if o.Undecided() {
			got = "undecided"
		}
		waived := before
		if got == "eligible" {
			waived = 0
		}
		if !ok || got != tc.want || !strings.Contains(o.Reason(), tc.phrase) || p.Total() != before || p.IfWaived() != waived {
			t.Errorf("%s: %s, %q, total %v, if waived %v; want %s with %q, total %v", tc.name, got, o.Reason(), p.Total(), p.IfWaived(), tc.want, tc.phrase, before)
		}
		if !has(res.Required(), working.PositionUnsettledRule) {
			t.Errorf("%s: the open point is not reported", tc.name)
		}
	}
}
