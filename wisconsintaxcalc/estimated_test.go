// Wisconsin's estimated tax rules for 2026, checked against the Department
// of Revenue's own documents and Chapter 71 of the statutes.
//
// Wisconsin publishes no exhaustive oracle for estimated tax. The strongest
// checks are the Schedule U instructions' three worked examples of carrying
// an overpayment forward and back, whose published answers fix the order
// payments are applied in. Every threshold, percentage, date and the rate is
// compared against its extract in testdata/. The interest arithmetic, the
// exceptions and the boundaries are hand-written cases.
package wisconsintaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/wisconsintaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

const wi = estimated.Jurisdiction("WI")

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
	return must(wisconsintaxcalc.EstimatedTax(2026))
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
		if strings.Contains(r.Applied(), s) || strings.Contains(r.Cite(), s) {
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
	tax         money.USCents // Schedule U line 3: net tax less refundable credits
	noTax       bool
	withheld    []estimated.Withheld
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *wisconsintaxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	annualised  []money.USCents // Schedule U line 41 for each period
	asWithheld  bool
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions // replaces waiver when set
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	fig := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		fig = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts(wi, 2026).FilingStatus(filing.Single).ThisYear(fig).
		Withheld(h.withheld...).Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled)
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(wisconsintaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).
			FederalDisasterExtension(false).ActiveDutyOutsideUS(false).Build()))
	}
	el := estimated.NewElections()
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		el.Annualised()
	}
	if h.asWithheld {
		el.WithholdingAsWithheld()
	}
	b.Elections(must(el.Build()))
	if h.waiver != nil {
		b.Positions(must(estimated.NewPositions().Waiver(*h.waiver).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

func withheld(on estimated.Date, a money.USCents) estimated.Withheld {
	return must(estimated.NewWithheld().On(on).Amount(a).Build())
}

// last is a filed 12-month 2025 return as a full-year resident.
func last(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).
		Figures(must(estimated.NewFigures().Tax(tax).Build())).FullYearResident(true).Build())
	return &p
}

// quarters is a household whose required annual payment is $4,000, last
// year's tax, so $1,000 is due by each date: 90% of this year's $10,000 is
// $9,000, more than $4,000.
func quarters(payments ...estimated.Payment) household {
	return household{tax: 10_000 * money.Dollar, prior: last(4_000 * money.Dollar), payments: payments}
}

func penaltyOf(t *testing.T, h household, asOf estimated.Date) working.Result[estimated.Penalty] {
	t.Helper()
	return must(rules(t).Penalty(h.facts(t), asOf))
}

func lineFor(p estimated.Penalty, installment int) (estimated.PenaltyLine, bool) {
	for _, l := range p.Lines() {
		if l.Installment() == installment {
			return l, true
		}
	}
	return estimated.PenaltyLine{}, false
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "form-1-es-inst-2026.txt",
		"if you expect to owe, after subtracting your withholding and credits, at least $500 in tax for 2026",
		"(1) 90% of the tax shown on your 2026 income tax return, (2) 100% of the tax shown on your 2025 income tax return, assuming the return covered 12 months*",
		"computed by annualizing your taxable income",
		"Full-year residents, part-year residents, nonresidents, estates, and trusts are subject to the estimated tax requirement.",
		"You do not have to pay estimated tax if you were a full-year resident of Wisconsin for 2025 and you had no tax liability for that 12-month period.",
		"in four equal installments on or before April 15, 2026, June 15, 2026, September 15, 2026, and January 15, 2027.",
		"If you file your 2026 income tax return by January 31, 2027, pay the entire balance due, and did not owe estimated tax for the September 2026 or prior installment date, the payment with your return will be treated as timely payment of the estimated tax due on January 15, 2027.",
		"If at least two-thirds of your gross income (joint gross income, if applicable) for 2025 or 2026 is from farming or fishing",
		"Note: If any due date falls on a Saturday, Sunday, or legal holiday, use the next business day.",
		"(Note: If line 6 less line 8 is less than $500, you are not required to make estimated tax payments.)",
		"(apply first to April and carry remainder to June, etc.)",
		"D-101A (R. 1-26)",
	)
	mustContain(t, "form-1-es-inst-2026-dates.txt",
		"March 31 and before June 1 June 15, 2026 1/2",
		"May 31 and before September 1 September 15, 2026 3/4",
		"August 31 January 15, 2027 all",
	)
	mustContain(t, "schedule-u-2025.txt",
		"3 Subtract line 2 from line 1.",
		"4 Multiply line 3 by 90% (.90)",
		"6 Subtract line 5 from line 3. If less than $500, do not complete or file this form",
		"8 Required annual payment. Fill in the smaller of line 4 or line 7",
		"16 Divide line 8 by four (4) and see instructions.",
		"April 15, 2025 June 16, 2025 Sept. 15, 2025 Jan. 15, 2026",
		"to the date balance due on tax return was paid or April 15, 2026, whichever is earlier",
		"28 Interest: Days on line 26 x .12 x Amount on 365 line 24",
		"3/31/25 5/31/25 8/31/25 12/31/25",
		"4 2.4 1.5 1",
		"42 22.5% 45% 67.5% 90%",
		"45 Subtract line 44 from line 43. If zero or less, fill in -0-",
		"49 Subtract line 45 from line 48. If zero or less, fill in -0-",
		"50 Fill in the smaller of line 45 or line 48",
		"13 Multiply line 12 by .0798",
		"D-104 (R. 05-25)",
	)
	mustContain(t, "schedule-u-inst-2025.txt",
		"You will not owe underpayment interest if the total tax shown on your 2025 return minus the amount of tax you paid through withholding is less than $500.",
		"You were a Wisconsin resident for the entire 2024 year, your tax liability for 2024 was zero, and your 2024 tax return was (or would have been had you been required to file) for a full 12 months.",
		"because of a federally declared disaster or terroristic or military action. Enter exception code “10”",
		"Fill in on line 2 the total of the refundable credits from your 2025 return",
		"Your 2024 tax return was for less than 12 months",
		"If the amount on line 5 is equal to or more than the amount on line 8, do not complete the rest of Schedule U.",
		"Divide the amount on line 8 by four (4) and fill in the result in each column.",
		"You are considered to have paid one-fourth of any withheld Wisconsin income tax on each payment due date unless you can show otherwise.",
		"If your tax was not withheld evenly throughout the year, you may fill in the amounts actually withheld during each period. Also, enter exception code “08”",
		"If you file your 2025 income tax return and pay the balance due by February 2, 2026, that payment will be accepted as your January 15 installment payment. However, this exception only applies if you were not required to make any estimated payment for the September 15 or earlier due dates.",
		"Any payment made after January 15, 2026, and before April 15, 2026, should be entered on line 22 as a late payment.",
		"Any overpayment on line 21, column (a), should be carried forward to the next installment period with an underpayment",
		"Any overpayment on line 21, column (b), should be carried back to an underpayment in column (a)",
		"Any overpayment on line 21, column (c), should be carried back to underpayment(s) in column (b), then column (a)",
		"Any overpayment on line 21, column (d), should be carried back to underpayment(s) in column (c), then column (b), then column (a)",
		"If you made an estimated tax payment after January 15, 2026, but before April 15, 2026, fill in the amount of that payment on line 22 in column (d). However, do not fill in more than the amount on line 20. Any excess may be carried back to the next prior period, etc.",
		"your underpayment was due to reasonable cause and not willful neglect (use exception code “06”)",
		"stationed outside the continental United States (use exception code “07”)",
		"D-114 (R. 11-25)",
	)
	mustContain(t, "wis-stat-71-09.txt",
		"whose gross income from farming or fishing for the preceding taxable year was at least two-thirds",
		"This section does not apply to any person on active duty with the U.S. armed forces while stationed outside the continental United States.",
		"(a) The tax shown on the return or, if no return is filed, the tax, minus amounts withheld under subch. X, is less than $500.",
		"(b) The preceding taxable year was 12 months, the taxpayer had no liability under s. 71.02 for that year and the taxpayer was a resident of this state for all of that year.",
		"(d) The secretary of revenue determines that the taxpayer retired during the taxable year or during the preceding taxable year after having attained age 62 or becoming disabled",
		"the taxpayer qualifies for a federal extension of time to file under 26",
		"Taxpayers shall make estimated payments in 4 installments, on or before the 15th day of each of the following months: (a) The 4th month of the taxable year. (b) The 6th month of the taxable year. (c) The 9th month of the taxable year. (d) The first month of the next taxable year.",
		"is 25 percent of the lower of the following amounts: 1. Ninety percent of the tax shown on the return",
		"2. The tax shown on the return for the preceding year.",
		"(b) Paragraph (a) 2. does not apply if the preceding taxable year was less than 12 months or if the taxpayer did not file a return for the preceding taxable year.",
		"(d) If 22.5 percent for the first installment, 45 percent for the 2nd installment, 67.5 percent for the 3rd installment and 90 percent for the 4th installment",
		"shall increase the next installment computed under par. (a)",
		"on or before January 31 of the succeeding calendar year",
		"if estimated taxes are not required to be paid on or before the 15th day of the 9th month of the taxable year",
		"updated through 2025 Wis. Act 247",
	)
	mustContain(t, "wis-stat-71-84.txt",
		"interest at the rate of 12 percent per year on the amount of the underpayment for the period of the underpayment.",
		"until either the 15th day of the 4th month beginning after the end of the taxable year or the date of payment, whichever is earlier.",
		"History: 1987 a. 312, 411; 1989 a. 31; 1991 a. 39; 2017 a. 2.",
	)
	mustContain(t, "wis-stat-990-001-4.txt",
		"falls on a Sunday or legal holiday the act may be done or the proceeding had or taken on the next secular day.",
		"falls on a Saturday and the duly established official office hours",
		"may be done on the next succeeding day that is not a Sunday or a legal holiday.",
		"“Legal holiday” as used in this section means any statewide legal holiday provided in s. 995.20.",
	)
	mustContain(t, "wis-stat-995-20.txt",
		"January 1, the 3rd Monday in January",
		"the 3rd Monday in February",
		"the last Monday in May",
		"June 19, which shall be the day of observation for Juneteenth Day, July 4, the 1st Monday in September which shall be known as Labor day, the 2nd Monday in October, November 11, the 4th Thursday in November",
		"December 25, the day of holding the partisan primary election, and the day of holding the general election in November are legal holidays.",
		"Whenever any legal holiday falls on Sunday, the succeeding Monday shall be the legal holiday.",
	)
	mustContain(t, "wis-stat-5-02.txt",
		"on the Tuesday after the first Monday in November",
		"the primary held the 2nd Tuesday in August",
	)
}

// The 2026 dates are the ones the 1-ES prints. Each is a weekday and none is
// a Wisconsin legal holiday, so none moves.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(quarters().facts(t)))
	cal := wisconsintaxcalc.Calendar(2026)
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if !cal.IsBusinessDay(in.Due()) {
			t.Errorf("installment %d falls on %v, which is not a business day", i+1, in.Due())
		}
		if want := money.USCents(i+1) * 1_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
}

// nth is the nth weekday of a month, or the last for n = -1.
func nth(year int, m time.Month, wd time.Weekday, n int) estimated.Date {
	if n < 0 {
		d := day(year, m+1, 1).AddDays(-1)
		for d.Weekday() != wd {
			d = d.AddDays(-1)
		}
		return d
	}
	d := day(year, m, 1)
	for d.Weekday() != wd {
		d = d.AddDays(1)
	}
	return d.AddDays(7 * (n - 1))
}

// The calendar is § 995.20's list from January 1, 2026 to the April 15,
// 2027 stop, each day worked out here from the statute's own words, and it
// moves a date on a weekend or one of those holidays to the next business
// day.
func TestEstimatedCalendarIsTheStatutesHolidays(t *testing.T) {
	var want []estimated.Date
	for _, y := range []int{2026, 2027} {
		want = append(want,
			day(y, 1, 1),
			nth(y, time.January, time.Monday, 3),
			nth(y, time.February, time.Monday, 3),
			nth(y, time.May, time.Monday, -1),
			day(y, 6, 19),
			day(y, 7, 4),
			nth(y, time.August, time.Tuesday, 2), // § 5.02(12s)
			nth(y, time.September, time.Monday, 1),
			nth(y, time.October, time.Monday, 2),
			nth(y, time.November, time.Monday, 1).AddDays(1), // § 5.02(5)
			day(y, 11, 11),
			nth(y, time.November, time.Thursday, 4),
			day(y, 12, 25),
		)
	}
	var inSpan []estimated.Date
	for _, d := range want {
		if !d.After(day(2027, 4, 15)) {
			if d.Weekday() == time.Sunday {
				t.Fatalf("%v is a Sunday; § 995.20 moves it to the Monday", d)
			}
			inSpan = append(inSpan, d)
		}
	}
	got := wisconsintaxcalc.Calendar(2026).Holidays()
	if len(got) != len(inSpan) {
		t.Fatalf("%d holidays, want %d", len(got), len(inSpan))
	}
	for i, h := range got {
		if h.On() != inSpan[i] {
			t.Errorf("holiday %d is %v (%s), want %v", i+1, h.On(), h.Name(), inSpan[i])
		}
	}

	cal := wisconsintaxcalc.Calendar(2026)
	for _, tc := range []struct{ in, want estimated.Date }{
		{day(2027, 1, 31), day(2027, 2, 1)},    // Sunday
		{day(2026, 7, 4), day(2026, 7, 6)},     // Saturday and a holiday
		{day(2026, 9, 7), day(2026, 9, 8)},     // Labor day
		{day(2026, 11, 3), day(2026, 11, 4)},   // the general election
		{day(2026, 11, 26), day(2026, 11, 27)}, // Thanksgiving
		{day(2026, 12, 25), day(2026, 12, 28)}, // Friday holiday, then the weekend
		{day(2027, 1, 15), day(2027, 1, 15)},   // a business day stays
	} {
		if got := cal.NextBusinessDay(tc.in); got != tc.want {
			t.Errorf("%v moves to %v, want %v", tc.in, got, tc.want)
		}
	}
}

// § 71.84(1)'s 12% runs without a break from the first day interest can
// accrue to the stop, so no day is unrated.
func TestEstimatedRateIsStatutoryToTheStop(t *testing.T) {
	ps := wisconsintaxcalc.Rates(2026).Periods()
	if len(ps) != 1 || ps[0].From() != dues[0].AddDays(1) || ps[0].To() != day(2027, 4, 15) || ps[0].Rate() != money.Percent(12) {
		t.Fatalf("rate periods %+v, want 12%% from April 16, 2026 to April 15, 2027", ps)
	}
}

// The $500 threshold, one cent either side. 90% of $600 is $540, more than
// the withholding, so only the threshold decides.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{10_001, true},  // $499.99
		{10_000, false}, // $500.00
	} {
		h := household{tax: 600 * money.Dollar, withheld: []estimated.Withheld{withheld(day(2026, 6, 1), tc.withheld)}}
		res := must(rules(t).Requirement(h.facts(t)))
		if res.Value().Exempt() != tc.exempt || res.Value().Required() == tc.exempt {
			t.Errorf("withheld %v: exempt %v, required %v; want exempt %v (%s)", tc.withheld, res.Value().Exempt(), res.Value().Required(), tc.exempt, res.Value().Why())
		}
		c, _ := res.Value().Chosen()
		if isException := c.Basis() == estimated.BasisException && c.Amount() == 0 && c.Label() == "Tax less withholding under $500"; isException != tc.exempt {
			t.Errorf("withheld %v: chosen %v at %v, %q", tc.withheld, c.Basis(), c.Amount(), c.Label())
		}
		if strings.Contains(res.Value().Why(), "$500, which is under $500") {
			t.Errorf("withheld %v: the reason rounds to a figure that is not under $500: %s", tc.withheld, res.Value().Why())
		}
		p := penaltyOf(t, h, day(2027, 4, 15))
		if (p.Value().Total() == 0) != tc.exempt {
			t.Errorf("withheld %v: interest %v", tc.withheld, p.Value().Total())
		}
	}
}

// The target is the smaller of 90% of this year's tax and 100% of last
// year's. Last year's needs a filed 12-month return, and there is no 110%.
func TestEstimatedTargetOptions(t *testing.T) {
	res := must(rules(t).Requirement(quarters().facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || c.Percent() != money.Percent(100) || res.Value().Target() != 4_000*money.Dollar {
		t.Errorf("chose %v %v at %v, want 100%% of last year's, $4,000", c.Basis(), c.Percent(), res.Value().Target())
	}
	if o := res.Value().Options()[0]; o.Amount() != 9_000*money.Dollar || o.Percent() != money.Percent(90) {
		t.Errorf("this year's option is %v of %v, want 90%%, $9,000", o.Percent(), o.Amount())
	}

	h := quarters()
	h.tax = 4_000 * money.Dollar
	if res := must(rules(t).Requirement(h.facts(t))); res.Value().Target() != 3_600*money.Dollar {
		t.Errorf("target %v, want 90%% of $4,000, $3,600", res.Value().Target())
	}

	// High income last year: still 100%.
	rich := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(4_000 * money.Dollar).
		FederalAGI(5_000_000 * money.Dollar).StateAGI(5_000_000 * money.Dollar).Build())).FullYearResident(true).Build())
	h = quarters()
	h.prior = &rich
	if res := must(rules(t).Requirement(h.facts(t))); res.Value().Target() != 4_000*money.Dollar {
		t.Errorf("at $5,000,000 of AGI the target is %v, want 100%% of last year's $4,000", res.Value().Target())
	}

	notFiled := must(estimated.NewPriorReturn().Filed(false).FullYearResident(true).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(must(estimated.NewFigures().Tax(100).Build())).FullYearResident(true).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   bool
	}{
		"not filed":  {&notFiled, false},
		"short year": {&short, false},
		"none given": {nil, true},
	} {
		h := household{tax: 10_000 * money.Dollar, prior: tc.prior}
		res := must(rules(t).Requirement(h.facts(t)))
		if res.Value().Options()[1].Available() || res.Value().Target() != 9_000*money.Dollar {
			t.Errorf("%s: last year's option offered, target %v", name, res.Value().Target())
		}
		if has(res.Required(), working.PositionPriorYearReturn) != tc.ask {
			t.Errorf("%s: asked for last year's return %v, want %v", name, !tc.ask, tc.ask)
		}
	}
}

// Without this year's tax the target is last year's tax alone, the
// installments are measured against it, and the interest is an upper bound.
// With no prior year either there is no target. An exception that needs
// nothing about this year is still exact.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := quarters()
	h.noTax = true
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 4_000*money.Dollar || !r.TargetKnown() {
		t.Errorf("required %v, target %v: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, in := range sched.Value() {
		if want := money.USCents(i+1) * 1_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	p := penaltyOf(t, h, day(2027, 4, 15))
	if !p.Value().UpperBound() || p.Value().Total() != 31_924 {
		t.Errorf("interest %v, upper bound %v; want $319.24 as an upper bound", p.Value().Total(), p.Value().UpperBound())
	}

	none := household{noTax: true}
	res = must(rules(t).Requirement(none.facts(t)))
	if res.Value().TargetKnown() || res.Value().Required() {
		t.Errorf("with nothing to measure: target known %v, required %v", res.Value().TargetKnown(), res.Value().Required())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}

	zero := household{noTax: true, prior: last(0)}
	if p := penaltyOf(t, zero, day(2027, 4, 15)); p.Value().Total() != 0 || p.Value().UpperBound() {
		t.Errorf("after a zero year: interest %v, upper bound %v; want an exact zero", p.Value().Total(), p.Value().UpperBound())
	}
}

// Schedule U lines 26 to 28: each shortfall × days ÷ 365 × .12, to the cent.
// $1,000 short on each date and never paid runs 365, 304, 212 and 90 days to
// April 15, 2027: $120.00, $99.95, $69.70 and $29.59. The 2025 short method
// prints .0798 for the same thing in one step, which on $4,000 gives $319.20.
func TestEstimatedInterestArithmetic(t *testing.T) {
	mustContain(t, "schedule-u-2025.txt", "13 Multiply line 12 by .0798")
	p := penaltyOf(t, quarters(), day(2027, 4, 15)).Value()
	wantDays := []int{365, 304, 212, 90}
	wantCharge := []money.USCents{12_000, 9_995, 6_970, 2_959}
	for i := range wantDays {
		l, ok := lineFor(p, i+1)
		if !ok {
			t.Fatalf("no line for installment %d", i+1)
		}
		parts := l.Parts()
		if len(parts) != 1 || parts[0].Days() != wantDays[i] || parts[0].Charge() != wantCharge[i] || parts[0].Rate() != money.Percent(12) {
			t.Errorf("line %d: %d parts, want one of %d days and %v", i+1, len(parts), wantDays[i], wantCharge[i])
		}
	}
	if p.Total() != 31_924 {
		t.Errorf("interest %v, want $319.24", p.Total())
	}
	short := money.USCents(4_000 * 798 / 100) // 4,000 × .0798 in cents
	if d := p.Total() - short; d < 0 || d > 10 {
		t.Errorf("the regular method's %v is not within 10 cents of the short method's %v", p.Total(), short)
	}
}

// Wisconsin's rate does not change: § 71.84(1) sets 12%, its history ends at
// 2017 Wis. Act 2, and no rate change falls in the 2026 window. A shortfall
// that runs from September 2026 into 2027 is one span at 12%, as Schedule U
// line 28 computes it, and nothing is unrated however late asOf is.
func TestEstimatedInterestAcrossTheYearEndIsOneSpan(t *testing.T) {
	h := quarters(pay(dues[0], 1_000*money.Dollar), pay(dues[1], 1_000*money.Dollar), pay(dues[3], 2_000*money.Dollar))
	res := penaltyOf(t, h, day(2027, 12, 31))
	p := res.Value()
	if p.AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want the April 15, 2027 stop", p.AsOf())
	}
	l, ok := lineFor(p, 3)
	if !ok || len(p.Lines()) != 1 {
		t.Fatalf("%d lines, want September's alone", len(p.Lines()))
	}
	// January's $2,000 covers January first; the $1,000 over goes back to
	// September on January 15: 122 days, $40.11.
	parts := l.Parts()
	if len(parts) != 1 || parts[0].From() != dues[2] || parts[0].To() != dues[3] || parts[0].Days() != 122 || parts[0].Charge() != 4_011 {
		t.Errorf("September's parts %+v, want one of 122 days, $40.11", parts)
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("an unpublished rate is reported for a statutory rate")
	}
}

// The Schedule U instructions' three worked examples, with $1,000 due each
// date. Their published answers are what lines 22 and 23 carry, which is
// what decides each installment's shortfall and the days it is charged.
func TestEstimatedScheduleUExamples(t *testing.T) {
	mustContain(t, "schedule-u-inst-2025.txt",
		"Column (a), line 21, has an overpayment of $1,000. Columns (b), (c), and (d), line 20, each have underpayments of $400.",
		"Lastly, carry forward the remaining overpayment of $200 from column (a) to column (d) by entering $200 on line 23.",
		"Columns (a), (b), and (c), line 20, each have underpayments of $500. Column (d), line 21, has an overpayment of $1,200.",
		"Lastly, carry back the remaining overpayment of $200 from column (d) to column (a) by entering $200 on line 22.",
		"Both columns (a) and (c), line 21, have an overpayment of $500. Both columns (b) and (d), line 20, have an underpayment of $600.",
		"Second, carry back $100 from column (c) to column (b) by entering $100 on line 22.",
		"Lastly, carry forward the remaining overpayment of $400 from column (c) to column (d) by entering $400 on line 23.",
	)
	type part struct {
		principal money.USCents
		to        estimated.Date
		days      int
		charge    money.USCents
	}
	stop := day(2027, 4, 15)
	for _, tc := range []struct {
		name      string
		payments  []estimated.Payment
		shortfall []money.USCents
		parts     map[int][]part
		total     money.USCents
	}{
		{
			// Line 23 carries $400, $400 and $200 forward from (a), leaving
			// $200 of (d) for 90 days.
			name:      "carryforward",
			payments:  quarterly(2_000*money.Dollar, 600*money.Dollar, 600*money.Dollar, 600*money.Dollar),
			shortfall: []money.USCents{0, 0, 0, 200 * money.Dollar},
			parts:     map[int][]part{4: {{200 * money.Dollar, stop, 90, 592}}},
			total:     592,
		},
		{
			// Line 22 carries $500, $500 and $200 back from (d) on January
			// 15, leaving $300 of (a) to April 15, 2027.
			name:      "carryback",
			payments:  quarterly(500*money.Dollar, 500*money.Dollar, 500*money.Dollar, 2_200*money.Dollar),
			shortfall: []money.USCents{500 * money.Dollar, 500 * money.Dollar, 500 * money.Dollar, 0},
			parts: map[int][]part{
				1: {{200 * money.Dollar, dues[3], 275, 1_808}, {300 * money.Dollar, stop, 365, 3_600}},
				2: {{500 * money.Dollar, dues[3], 214, 3_518}},
				3: {{500 * money.Dollar, dues[3], 122, 2_005}},
			},
			total: 10_931,
		},
		{
			// (a) carries $500 forward to (b); (c) carries $100 back to (b)
			// on September 15 and $400 forward to (d).
			name:      "carryforward and carryback",
			payments:  quarterly(1_500*money.Dollar, 400*money.Dollar, 1_500*money.Dollar, 400*money.Dollar),
			shortfall: []money.USCents{0, 100 * money.Dollar, 0, 200 * money.Dollar},
			parts: map[int][]part{
				2: {{100 * money.Dollar, dues[2], 92, 302}},
				4: {{200 * money.Dollar, stop, 90, 592}},
			},
			total: 894,
		},
	} {
		h := quarters(tc.payments...)
		sched := must(rules(t).Schedule(h.facts(t))).Value()
		for i, in := range sched {
			if in.Shortfall() != tc.shortfall[i] {
				t.Errorf("%s: installment %d is %v short, want %v", tc.name, i+1, in.Shortfall(), tc.shortfall[i])
			}
		}
		p := penaltyOf(t, h, stop).Value()
		if len(p.Lines()) != len(tc.parts) {
			t.Errorf("%s: %d lines, want %d", tc.name, len(p.Lines()), len(tc.parts))
		}
		for n, want := range tc.parts {
			l, ok := lineFor(p, n)
			if !ok {
				t.Errorf("%s: no line for installment %d", tc.name, n)
				continue
			}
			got := l.Parts()
			if len(got) != len(want) {
				t.Errorf("%s: installment %d has %d parts, want %d", tc.name, n, len(got), len(want))
				continue
			}
			for i, w := range want {
				g := got[i]
				if g.Principal() != w.principal || g.To() != w.to || g.Days() != w.days || g.Charge() != w.charge {
					t.Errorf("%s: installment %d part %d is %v to %v, %d days, %v; want %v to %v, %d days, %v",
						tc.name, n, i+1, g.Principal(), g.To(), g.Days(), g.Charge(), w.principal, w.to, w.days, w.charge)
				}
			}
		}
		if p.Total() != tc.total {
			t.Errorf("%s: interest %v, want %v", tc.name, p.Total(), tc.total)
		}
	}
}

// A payment after January 15 goes to the January shortfall first, then back
// to September, never to April first.
func TestEstimatedLatePaymentGoesToJanuaryFirst(t *testing.T) {
	h := quarters(pay(day(2027, 2, 10), 1_500*money.Dollar))
	p := penaltyOf(t, h, day(2027, 4, 15)).Value()
	jan, _ := lineFor(p, 4)
	if parts := jan.Parts(); len(parts) != 1 || parts[0].Principal() != 1_000*money.Dollar || parts[0].To() != day(2027, 2, 10) || parts[0].Days() != 26 || parts[0].Charge() != 855 {
		t.Errorf("January's parts %+v, want $1,000 for 26 days, $8.55", parts)
	}
	sep, _ := lineFor(p, 3)
	if parts := sep.Parts(); len(parts) != 2 || parts[0].Principal() != 500*money.Dollar || parts[0].To() != day(2027, 2, 10) {
		t.Errorf("September's parts %+v, want $500 paid February 10 and $500 unpaid", parts)
	}
	apr, _ := lineFor(p, 1)
	if parts := apr.Parts(); len(parts) != 1 || parts[0].To() != day(2027, 4, 15) {
		t.Errorf("April's parts %+v, want all of it unpaid to April 15, 2027", parts)
	}
}

// A payment counts first toward its own period. A June 1 payment does not
// pay April's shortfall until June's own installment is covered.
func TestEstimatedPaymentCoversItsOwnPeriodFirst(t *testing.T) {
	h := quarters(pay(day(2026, 6, 1), 1_500*money.Dollar), pay(dues[2], 1_000*money.Dollar), pay(dues[3], 1_000*money.Dollar))
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	if sched[0].Shortfall() != 1_000*money.Dollar || sched[1].Shortfall() != 0 {
		t.Errorf("April %v and June %v short, want $1,000 and nothing", sched[0].Shortfall(), sched[1].Shortfall())
	}
	p := penaltyOf(t, h, day(2027, 4, 15)).Value()
	// $500 of April paid June 1, 47 days, $7.73; $500 never paid, $60.00.
	if p.Total() != 773+6_000 {
		t.Errorf("interest %v, want $67.73", p.Total())
	}
}

// Last year's overpayment applied forward counts on April 15.
func TestEstimatedOverpaymentAppliedForward(t *testing.T) {
	forward := must(estimated.NewPayment().Amount(4_000 * money.Dollar).FromPriorYear().Build())
	if p := penaltyOf(t, quarters(forward), day(2027, 4, 15)); p.Value().Total() != 0 {
		t.Errorf("interest %v with the whole year paid by last year's overpayment", p.Value().Total())
	}
}

// Schedule U Part IV, with its recapture: each installment is the smaller of
// the annualised amount and the regular quarter plus what was lowered
// before.
func TestEstimatedAnnualisedMethod(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, annualised: []money.USCents{1_000 * money.Dollar, 2_000 * money.Dollar, 4_000 * money.Dollar, 10_000 * money.Dollar}}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	// 22.5% of $1,000, 45% of $2,000, 67.5% of $4,000 and 90% of $10,000,
	// each less the installments before.
	for i, want := range []money.USCents{225 * money.Dollar, 900 * money.Dollar, 2_700 * money.Dollar, 9_000 * money.Dollar} {
		if sched[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, sched[i].NeededBy(), want)
		}
	}
	// A smaller tax for the whole year lowers the last installment to 90% of
	// it, less the $2,700 before.
	h.annualised[3] = 8_000 * money.Dollar
	if got := must(rules(t).Schedule(h.facts(t))).Value()[3].NeededBy(); got != 7_200*money.Dollar {
		t.Errorf("installment 4 needs %v, want $7,200", got)
	}
	// A large annualised tax early cannot raise an installment over the
	// regular quarter.
	h.annualised = []money.USCents{20_000 * money.Dollar, 20_000 * money.Dollar, 20_000 * money.Dollar, 10_000 * money.Dollar}
	sched = must(rules(t).Schedule(h.facts(t))).Value()
	for i, in := range sched {
		if want := money.USCents(i+1) * 2_250 * money.Dollar; in.NeededBy() != want {
			t.Errorf("capped: installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
}

// § 71.09(14): a return filed and paid by January 31, moved to Monday
// February 1, 2027, replaces the January payment only when nothing was
// required by September 15.
func TestEstimatedReturnFiledByJanuary31(t *testing.T) {
	late := household{tax: 10_000 * money.Dollar, annualised: []money.USCents{0, 0, 0, 10_000 * money.Dollar}}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
		total   money.USCents
	}{
		{day(2027, 2, 1), true, 0},
		// $9,000 for 18 days, January 15 to February 2: $53.26.
		{day(2027, 2, 2), false, 5_326},
	} {
		late.returnFiled = tc.filed
		sched := must(rules(t).Schedule(late.facts(t))).Value()
		if got := sched[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
		for i := 0; i < 3; i++ {
			if sched[i].Status() != estimated.StatusNothingDue {
				t.Errorf("filed %v: installment %d is %v, want nothing due", tc.filed, i+1, sched[i].Status())
			}
		}
		if p := penaltyOf(t, late, day(2027, 4, 15)); p.Value().Total() != tc.total {
			t.Errorf("filed %v: interest %v, want %v", tc.filed, p.Value().Total(), tc.total)
		}
	}

	// Equal quarters: something was due earlier, so filing early does not
	// replace January.
	h := quarters()
	h.returnFiled = day(2027, 1, 20)
	res := must(rules(t).Schedule(h.facts(t)))
	if res.Value()[3].Status() == estimated.StatusExcused || !applied(res.Applied(), "does not replace the January installment") {
		t.Errorf("January is %v with quarters due earlier", res.Value()[3].Status())
	}
}

// The interest stops on the day the return's balance was paid, and never
// runs past April 15, 2027.
func TestEstimatedInterestStops(t *testing.T) {
	h := quarters()
	h.returnFiled = day(2027, 3, 1)
	res := penaltyOf(t, h, day(2027, 4, 15))
	if res.Value().AsOf() != day(2027, 3, 1) || !applied(res.Applied(), "balance due on the return was paid") {
		t.Errorf("figured to %v, want the day the return was paid", res.Value().AsOf())
	}
	if p := penaltyOf(t, quarters(), day(2027, 6, 30)); p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
}

// Code 08: withholding counted in the period it was withheld. $2,500 in March
// covers April and carries $1,000 to June and $500 to September; the $500
// in December falls in January's period.
func TestEstimatedWithholdingAsWithheld(t *testing.T) {
	h := quarters()
	h.withheld = []estimated.Withheld{withheld(day(2026, 3, 1), 2_500*money.Dollar), withheld(day(2026, 12, 15), 500*money.Dollar)}
	even := must(rules(t).Schedule(h.facts(t))).Value()
	for i, in := range even {
		if in.Shortfall() != 250*money.Dollar {
			t.Errorf("spread evenly: installment %d is %v short, want $250", i+1, in.Shortfall())
		}
	}
	h.asWithheld = true
	res := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{0, 0, 500 * money.Dollar, 500 * money.Dollar} {
		if got := res.Value()[i].Shortfall(); got != want {
			t.Errorf("as withheld: installment %d is %v short, want %v", i+1, got, want)
		}
	}
	if !applied(res.Applied(), "code 08") {
		t.Error("the code 08 election is not reported")
	}
}

// A late starter still owes a quarter from April 15. Here the requirement
// arose in July, and September's $3,000 pays June's $1,000 and then April's
// $1,000 on September 15.
func TestEstimatedLateStarterIsChargedFromApril(t *testing.T) {
	h := quarters(pay(dues[2], 3_000*money.Dollar), pay(dues[3], 1_000*money.Dollar))
	h.firstMet = day(2026, 7, 1)
	res := penaltyOf(t, h, day(2027, 4, 15))
	// April: 153 days, $50.30. June: 92 days, $30.25.
	if res.Value().Total() != 5_030+3_025 {
		t.Errorf("interest %v, want $80.55", res.Value().Total())
	}
	if !applied(res.Applied(), "first met on July 1, 2026") {
		t.Error("the late start is not explained")
	}
	if has(res.Required(), working.PositionRequirementFirstMet) {
		t.Error("asked when the requirement arose, which changes nothing in Wisconsin")
	}
}

func TestEstimatedExceptions(t *testing.T) {
	zero := func(build func(*estimated.PriorReturnBuilder)) *estimated.PriorReturn {
		b := estimated.NewPriorReturn()
		build(b)
		p := must(b.Build())
		return &p
	}
	zeroFigures := must(estimated.NewFigures().Tax(0).Build())
	// zero is whether the interest is nil anyway: a filed 12-month return
	// with no tax makes the prior-year target $0, resident or not.
	for name, tc := range map[string]struct {
		prior        *estimated.PriorReturn
		exempt, zero bool
		ask          working.PositionKind
	}{
		"code 02":                         {zero(func(b *estimated.PriorReturnBuilder) { b.Filed(true).Figures(zeroFigures).FullYearResident(true) }), true, true, 0},
		"code 02, not required to file":   {zero(func(b *estimated.PriorReturnBuilder) { b.NotRequiredToFile().FullYearResident(true) }), true, true, 0},
		"not required to file, part-year": {zero(func(b *estimated.PriorReturnBuilder) { b.NotRequiredToFile().FullYearResident(false) }), false, false, 0},
		"not required to file, not said":  {zero(func(b *estimated.PriorReturnBuilder) { b.NotRequiredToFile() }), false, false, working.PositionResidency},
		"filed, part-year resident":       {zero(func(b *estimated.PriorReturnBuilder) { b.Filed(true).Figures(zeroFigures).FullYearResident(false) }), false, true, 0},
		"short year": {zero(func(b *estimated.PriorReturnBuilder) {
			b.Filed(true).Months(11).Figures(zeroFigures).FullYearResident(true)
		}), false, false, 0},
		"tax last year": {last(1_000 * money.Dollar), false, false, 0},
	} {
		h := household{tax: 10_000 * money.Dollar, prior: tc.prior}
		res := must(rules(t).Requirement(h.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", name, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		if tc.ask != 0 && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
		if p := penaltyOf(t, h, day(2027, 4, 15)); (p.Value().Total() == 0) != tc.zero {
			t.Errorf("%s: interest %v", name, p.Value().Total())
		}
	}

	// Code 10: a federal disaster extension means no interest.
	disaster := must(wisconsintaxcalc.NewEstimatedFacts().FederalDisasterExtension(true).Build())
	h := quarters()
	h.extras = &disaster
	if res := must(rules(t).Requirement(h.facts(t))); !res.Value().Exempt() {
		t.Errorf("code 10: not exempt: %s", res.Value().Why())
	}
	if p := penaltyOf(t, h, day(2027, 4, 15)); p.Value().Total() != 0 {
		t.Errorf("code 10: interest %v", p.Value().Total())
	}

	// Not answered: asked, and assumed not.
	h = quarters()
	h.extras = &wisconsintaxcalc.EstimatedFacts{}
	res := must(rules(t).Requirement(h.facts(t)))
	n := 0
	for _, r := range res.Required() {
		if r.Position() == working.PositionJurisdictionFact {
			n++
		}
	}
	if n != 2 || res.Value().Exempt() {
		t.Errorf("%d Wisconsin facts asked, exempt %v; want last year's farming and the disaster extension, not exempt", n, res.Value().Exempt())
	}

	// Codes 05 and 06 are waivers the department decides: the interest is
	// shown before them.
	full := penaltyOf(t, quarters(), day(2027, 4, 15)).Value().Total()
	for _, tc := range []struct {
		k    estimated.WaiverKind
		code string
	}{{estimated.WaiverCasualty, "code 05"}, {estimated.WaiverRetiredOrDisabled, "code 06"}} {
		h := quarters()
		h.waiver = &tc.k
		res := penaltyOf(t, h, day(2027, 4, 15))
		if res.Value().Total() != full || res.Value().Waiver() != tc.k || !applied(res.Applied(), tc.code) {
			t.Errorf("%s: interest %v, waiver %v", tc.code, res.Value().Total(), res.Value().Waiver())
		}
		if has(res.Required(), working.PositionPenaltyWaiver) {
			t.Errorf("%s: the waiver is asked for though answered", tc.code)
		}
	}
	if res := penaltyOf(t, quarters(), day(2027, 4, 15)); !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver question is not asked")
	}

	// Code 07 follows the form: a waiver to apply for, the interest shown
	// before it.
	abroad := must(wisconsintaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).FederalDisasterExtension(false).ActiveDutyOutsideUS(true).Build())
	h = quarters()
	h.extras = &abroad
	if res := penaltyOf(t, h, day(2027, 4, 15)); res.Value().Total() != full || !applied(res.Applied(), "code 07") {
		t.Errorf("code 07: interest %v", res.Value().Total())
	}

	// Withholding that reaches the required annual payment ends Schedule U at
	// line 8, whatever the election.
	h = quarters()
	h.withheld = []estimated.Withheld{withheld(day(2026, 12, 15), 4_000*money.Dollar)}
	h.asWithheld = true
	res2 := must(rules(t).Requirement(h.facts(t)))
	if res2.Value().Required() {
		t.Errorf("withholding of the whole target: %s", res2.Value().Why())
	}
	if p := penaltyOf(t, h, day(2027, 4, 15)); p.Value().Total() != 0 {
		t.Errorf("withholding of the whole target: interest %v", p.Value().Total())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(quarters().facts(t)))
	want := map[string][4]string{
		"Retirement or disability":                          {"reasonable cause", "Schedule U Instructions (2025)", "no reasonable-cause condition", "§ 71.09(11)(d)"},
		"Active duty outside the continental United States": {"apply for a waiver", "Schedule U Instructions (2025)", "does not apply to such a person", "§ 71.09(2)"},
		"A requirement first met after March 31":            {"1/2, 3/4 or all", "Form 1-ES", "25%", "§ 71.09(12), (13)(a)"},
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
	}
	// Which reading each follows. Equal quarters with no waiver asked, no
	// active duty and no late start: none of the three reaches these facts.
	// An active-duty filer is reached by the second.
	uses := map[string]working.Reading{
		"Retirement or disability":                          working.ReadingForm,
		"Active duty outside the continental United States": working.ReadingForm,
		"A requirement first met after March 31":            working.ReadingStatute,
	}
	for _, d := range res.Divergences() {
		if d.Uses() != uses[d.Rule()] || d.AffectsHousehold() {
			t.Errorf("%q uses %v and affects %v; want %v and false", d.Rule(), d.Uses(), d.AffectsHousehold(), uses[d.Rule()])
		}
	}
	abroad := must(wisconsintaxcalc.NewEstimatedFacts().ActiveDutyOutsideUS(true).Build())
	h := quarters()
	h.extras = &abroad
	for _, d := range must(rules(t).Requirement(h.facts(t))).Divergences() {
		if want := d.Rule() == "Active duty outside the continental United States"; d.AffectsHousehold() != want {
			t.Errorf("active duty: %q affects %v, want %v", d.Rule(), d.AffectsHousehold(), want)
		}
	}
	// The second reading is not what the figure follows: an active-duty
	// filer is still charged, and the retirement waiver still waits on the
	// department.
	mustContain(t, "wis-stat-71-09.txt", "This section does not apply to any person on active duty")
	mustContain(t, "schedule-u-inst-2025.txt", "You were on active duty with the United States Armed Forces and were stationed outside the continental United States (use exception code “07”).")
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, year := range []int{2025, 2027} {
		if _, err := wisconsintaxcalc.EstimatedTax(year); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", year, err)
		}
	}
	this := must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())
	base := func(j estimated.Jurisdiction, year int) *estimated.FactsBuilder {
		return estimated.NewFacts(j, year).FilingStatus(filing.Single).ThisYear(this)
	}
	priorFarmer := must(wisconsintaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(true).Build())
	for name, f := range map[string]estimated.Facts{
		"a farmer":           must(base(wi, 2026).FarmShare(6_700).Build()),
		"a fisher":           must(base(wi, 2026).FishShare(6_700).Build()),
		"a farmer last year": must(base(wi, 2026).State(priorFarmer).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	if _, err := rules(t).Requirement(must(base(wi, 2026).FarmShare(6_600).Build())); err != nil {
		t.Errorf("66%% from farming: %v", err)
	}
	wrongEnd := must(estimated.NewPeriod().End(day(2026, 4, 30)).AnnualisedTax(100).Build())
	ends := []estimated.Date{day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
	b := base(wi, 2026).Periods(wrongEnd).Elections(must(estimated.NewElections().Annualised().Build()))
	for _, e := range ends {
		b.Periods(must(estimated.NewPeriod().End(e).AnnualisedTax(100).Build()))
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(base(estimated.California, 2026).Build()),
		"2027":               must(base(wi, 2027).Build()),
		"other extras":       must(base(wi, 2026).State("not Wisconsin's").Build()),
		"a period end":       must(b.Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(quarters().facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date: accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"$500", "12%", "90%", "100%", "no 110%", "most recent one first", "full-year Wisconsin resident", "February 1, 2027", "annualized"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if e.VersusFederal() == "" || len(e.Sources()) == 0 {
		t.Error("the explanation has no comparison with federal or no sources")
	}
}

// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
// never as a negative amount.
func TestEstimatedWithholdingOverTheTaxReadsAsARefund(t *testing.T) {
	figures := func(tax money.USCents) estimated.Figures {
		return must(estimated.NewFigures().Tax(tax).FederalAGI(60_000 * money.Dollar).StateAGI(60_000 * money.Dollar).Build())
	}
	last := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800 * money.Dollar)).Build())
	b := estimated.NewFacts("WI", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
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

// § 71.09(11)(c) and (d) and the Schedule U instructions grant the federal
// pair: a casualty or disaster, and retiring after 62 or becoming disabled in
// the year or the year before.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "schedule-u-inst-2025.txt", "In 2024 or 2025 you retired after age 62 or became disabled")
	full := penaltyOf(t, quarters(), day(2027, 4, 15)).Value().Total()
	for _, tc := range []struct {
		name string
		pos  estimated.Positions
		want func(estimated.WaiverOutcome) bool
		in   string
	}{
		{"retired in 2026 at 64", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build()),
			estimated.WaiverOutcome.Eligible, "after reaching 62"},
		{"retired in 2020", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, 3, 1)).Build()),
			estimated.WaiverOutcome.Refused, "2025 or 2026"},
		{"retired in 2026 at 60", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1966, 3, 1)).Build()),
			estimated.WaiverOutcome.Refused, "reached 62"},
		{"a disaster", must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()),
			estimated.WaiverOutcome.Eligible, "Department of Revenue"},
	} {
		h := quarters()
		h.positions = &tc.pos
		res := penaltyOf(t, h, day(2027, 4, 15))
		o, ok := res.Value().WaiverOutcome()
		if !ok || !tc.want(o) || !strings.Contains(o.Reason(), tc.in) {
			t.Errorf("%s: outcome %q, %v", tc.name, o.Reason(), ok)
		}
		if res.Value().Total() != full || full == 0 {
			t.Errorf("%s: interest %v, without a waiver %v", tc.name, res.Value().Total(), full)
		}
		if wantIf := map[bool]money.USCents{true: 0, false: full}[o.Eligible()]; res.Value().IfWaived() != wantIf {
			t.Errorf("%s: if waived %v, want %v", tc.name, res.Value().IfWaived(), wantIf)
		}
	}
}

// Under an exception, no question says its answer changes the target.
func TestEstimatedExemptAsksNothingAboutTheTarget(t *testing.T) {
	disaster := must(wisconsintaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).
		FederalDisasterExtension(true).ActiveDutyOutsideUS(false).Build())
	res := must(rules(t).Requirement(household{noTax: true, extras: &disaster}.facts(t)))
	if !res.Value().Exempt() {
		t.Fatal("not exempt under a federal disaster extension")
	}
	for _, q := range res.Required() {
		if strings.Contains(strings.ToLower(q.Effect()), "target") {
			t.Errorf("an exempt requirement asks %q", q.Effect())
		}
	}
	if has(res.Required(), working.PositionThisYearTax) {
		t.Error("an exempt requirement asks for this year's tax")
	}
}
