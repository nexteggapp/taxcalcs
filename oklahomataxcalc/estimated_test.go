// Oklahoma's estimated tax rules for 2026, checked against the Tax
// Commission's forms and the statutes as the Legislature publishes them.
//
// Oklahoma publishes no exhaustive oracle and no worked example for
// estimated tax: OW-8-ES, OW-8-P, OW-8-ES-SUP, OW-8-P-SUP-I and the Form 511
// instructions carry none with figures. Every threshold, share, date and rate
// is compared against its extract in testdata/, and the arithmetic is tested
// with hand-worked cases, each worked in its comment as OW-8-P computes it.
package oklahomataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/oklahomataxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

var day = estimated.MustDate

const ok = estimated.Jurisdiction("OK")

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
	return must(oklahomataxcalc.EstimatedTax(2026))
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

type household struct {
	tax         money.USCents // after credits, OW-8-P line 3
	noTax       bool
	status      filing.Status
	withheld    money.USCents // all withheld on December 15
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *oklahomataxcalc.EstimatedFacts
	farmShare   money.Rate
	returnFiled estimated.Date
	annualised  []money.USCents // OW-8-P-SUP-I line 15, by column
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
	b := estimated.NewFacts(ok, 2026).FilingStatus(status).ThisYear(figures).
		Paid(h.payments...).ReturnFiled(h.returnFiled).FarmShare(h.farmShare)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(oklahomataxcalc.NewEstimatedFacts().PriorYearFarmShare(0).Build()))
	}
	e := estimated.NewElections()
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, a := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(a).Build()))
		}
		e.Annualised()
	}
	if h.asWithheld {
		e.WithholdingAsWithheld()
	}
	b.Elections(must(e.Build()))
	if h.waiver != nil {
		b.Positions(must(estimated.NewPositions().Waiver(*h.waiver).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

// filed is a filed 12-month return showing tax.
func filed(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).FullYearResident(true).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "ow-8-es-2026.txt",
		"Tax Year 2026 Worksheet for Individuals Revised 6-2025",
		"A. Multiply line 8 by 70%",
		"B. Enter the tax liability shown on your previous year’s tax return",
		"12 Amount to be paid with each coupon (if paid quarterly, 1/4 of line 11)",
		"or if line 8 minus line 10 is less than $500, stop here. You are not required to make estimated tax payments.",
		"you can reasonably expect your tax liability to exceed your withholding by $500 or more, and you expect your withholding to be less than the smaller of: 1. 70% of your current year’s tax liability, or 2. The tax liability shown on your return for the preceding taxable year of 12 months.",
		"Estates and farmers are not required to make estimated tax payments. A farmer is an individual who derives at least twothirds of his/her gross income for the current year or the previous year from farming activities.",
		"computed at a rate of 20% per annum for the period of underpayment.",
		"No underpayment of estimated tax interest shall be imposed if the tax shown on the return is less than $1,000.",
		"• 1st Quarter - April 15 • 2nd Quarter - June 15 • 3rd Quarter - September 15 • 4th Quarter - January 15",
		"If the due date falls on a weekend or legal holiday when the Oklahoma Tax Commission offices are closed, your payment is due the next business day.",
	)
	mustContain(t, "ow-8-p-2025.txt",
		"2025 Form OW-8-P",
		"3. Oklahoma tax liability. Subtract line 2 from line 1",
		"If less than $1,000, stop here; you do not owe the interest.",
		"4. Multiply line 3 by 70% (0.70)",
		"If less than $500, stop here; you do not owe the interest.",
		"Previous year’s return must be for 12 months. If you were not required to file an income tax return for the previous tax year, stop here; you do not owe the interest.",
		"8. Required annual payment. Enter the smaller of line 4 or line 7",
		"Note: If line 5 is equal to or more than line 8, stop here; you do not owe the interest.",
		"Enter 1/4 of line 8 in each column unless you have checked the box in Section 1.",
		"You are considered to have paid one-fourth of your withholding on each payment due date unless you can show otherwise.",
		"Include in the first quarter any overpayment of tax from your previous tax year’s return that you elected to apply to this year’s estimated tax.",
		"If the preprinted date falls on a weekend or holiday, adjust the date to the next business day.",
		"should be listed using the date you filed your return or the original due date, whichever is earlier.",
		"the earliest date of payment being applied first.",
		"Withholding payments are deemed paid on the quarterly due date(s).",
		"Compute the number of days past due from the date of this payment until the date of the next payment or the original due date if no payment is made.",
		"Column E- Column C x Number of days in Column D x 20% divided by 365",
	)
	for _, name := range []string{"ow-8-es-sup-2026.txt", "ow-8-p-sup-i-2025.txt"} {
		mustContain(t, name,
			"Column A Column B Column C Column D",
			"1/1 - 3/31 1/1 - 5/31 1/1 - 8/31 1/1 - 12/31",
			"2 4 2.4 1.5 1",
			"16 17.5% 35% 52.5% 70%",
			"Subtract line 18 from line 17. If zero or less, enter “0”",
			"Subtract line 19 from line 22. If zero or less, enter “0”",
		)
	}
	mustContain(t, "ow-8-es-sup-2026.txt", "2026 Form OW-8-ES-SUP")
	mustContain(t, "ow-8-p-sup-i-2025.txt",
		"2025 Form OW-8-P-SUP-I",
		"20. Enter 1/4 of line 8 from Section Two, Part 1 of Form OW-8-P in each column",
		"If you use this form for any payment due date, you must use it for all payment due dates.")
	mustContain(t, "511-pkt-2025-estimated.txt",
		"If at least 66.67% (or two-thirds) of your gross income for this year or last year is from farming, estimated payments are not required.",
		"If claiming this exception, you must mark the box on this line and provide a complete copy of your federal return.",
		"The income tax liability is the Oklahoma income tax due less all credits except amounts paid on withholding, estimated tax, and extension payments.",
		"No Underpayment of Estimated Tax Interest shall be imposed if the income tax liability shown on the return is less than $1,000.",
		"If you file your return electronically (through a preparer or the internet), your due date is extended to April 20th.",
		"penalty and interest will accrue from the original due date.",
	)
	mustContain(t, "os68-2385.txt",
		"In the case of a single individual taxpayer, the tax liability of the taxpayer can reasonably be expected to be Five Hundred Dollars ($500.00) or more in excess of taxes to be withheld from wages;",
		"In the case of married individuals, the combined tax liability of the married individuals can reasonably be expected to be Five Hundred Dollars ($500.00) or more in excess of taxes to be withheld from wages;",
		"1. Estates; and",
		"is at least sixty-six and two-thirds percent (66 2/3%) of the total estimated gross income from all sources for the taxable year.",
		"In no event shall the qualification for the previous taxable year be carried forward for more than one (1) year.",
		"Laws 2000, c. 314, § 21, eff. July 1, 2000.",
		"The required annual payment of estimated tax shall be paid in four equal installments as follows:",
		"the first installment shall be paid on April 15 of the taxable year, the second and third on June 15 and September 15, respectively, of the",
		"taxable year and the fourth on January 15 of the succeeding taxable year. However, if taxpayer files return and pays tax due on or before January 31, the payment of the installment due January 15 is waived;",
		"1. Seventy percent (70%) of the tax shown on the return for the taxable year; or 2. One hundred percent (100%) of the tax shown on the return for the preceding taxable year of twelve (12) months.",
		"shall only be permitted for a taxable year of twelve (12) months.",
		"Laws 2000, c. 314, § 22, eff. July 1, 2000.",
		"there shall be added to the amount of the underpayment interest thereon at an annual rate of twenty percent (20%) for the period of the underpayment.",
		"the amount of the underpayment shall be the excess of the required installment over the amount paid on or before the due date of the installment.",
		"to the earlier of the fifteenth day of the fourth month, or for corporations, thirty (30) days after the due date for returns established under the Internal Revenue Code, following the close of the taxable year or the date on which the required installment is paid.",
		"if the tax shown on the return for the taxable year is less than One Thousand Dollars ($1,000.00) or if the taxpayer was an Oklahoma resident throughout the preceding taxable year of twelve (12) months and did not have any liability for tax for the preceding taxable year.",
		"Laws 2021, c. 436, § 3, eff. Nov. 1, 2021.",
	)
	mustContain(t, "os25-82-1.txt",
		"The designation and dates of holidays in Oklahoma shall be as follows: Each Saturday, Sunday,",
		"Any act authorized, required, or permitted to be performed on a holiday as designated in subsection A of this section may be performed on the next succeeding business day",
	)
	// HB 2730 would replace the 20% from November 1, 2026. It passed the
	// House and was last referred in the Senate on April 1, 2026.
	mustContain(t, "hb2730-engr.txt",
		"Wall Street Prime, as of January 1 of that year, plus three",
		"This act shall become effective November 1, 2026.",
		"Passed the House of Representatives the 24th day of February, 2026.",
		"Passed the Senate the ___ day of __________, 2026.")
	mustContain(t, "hb2730-status.txt",
		"Third Reading, Measure passed: Ayes: 87 Nays: 11 | 403 | 02/24/2026 | H",
		"Second Reading referred to Revenue and Taxation Committee then to Appropriations Committee | 763 | 04/01/2026 | S")
	if text := extract(t, "hb2730-status.txt"); !strings.HasSuffix(strings.TrimSuffix(text, " |"), "04/01/2026 | S") {
		t.Errorf("HB 2730 has an action after April 1, 2026: %s", text[len(text)-200:])
	}
}

// nth is the nth weekday of a month, or the last for n of -1.
func nth(year int, month time.Month, wd time.Weekday, n int) estimated.Date {
	if n < 0 {
		d := day(year, month+1, 1).AddDays(-1)
		for d.Weekday() != wd {
			d = d.AddDays(-1)
		}
		return d
	}
	d := day(year, month, 1)
	for d.Weekday() != wd {
		d = d.AddDays(1)
	}
	return d.AddDays(7 * (n - 1))
}

// observed moves a fixed-date holiday other than Christmas off a weekend,
// as 25 O.S. § 82.1(A) does.
func observed(d estimated.Date) estimated.Date {
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDays(-1)
	case time.Sunday:
		return d.AddDays(1)
	}
	return d
}

// The calendar is 25 O.S. § 82.1(A), worked out here from its rules: fixed
// dates moved off a weekend, and the nth Mondays and Thursdays. The extra
// Christmas day the Governor's order picks is not in it.
func TestEstimatedCalendarIsSection82Point1(t *testing.T) {
	mustContain(t, "os25-82-1.txt",
		"New Year's Day on the 1st day of January, Martin Luther King, Jr.'s Birthday on the third Monday in January, Presidents' Day on the third Monday in February, Memorial Day on the last Monday in May, Independence Day on the 4th day of July, Labor Day on the first Monday in September, Veterans' Day on the 11th day of November, Thanksgiving Day on the fourth Thursday in November, the day after Thanksgiving Day, Christmas on the 25th day of December,",
		"if any of such holidays other than Christmas fall on Saturday, the preceding Friday shall be a holiday in that year and if any of such holidays other than Christmas fall on Sunday, the succeeding Monday shall be a holiday in that year.",
		"The Governor shall issue an Executive Order each year specifying the dates")
	want := []estimated.Date{
		observed(day(2026, 1, 1)),
		nth(2026, time.January, time.Monday, 3),
		nth(2026, time.February, time.Monday, 3),
		nth(2026, time.May, time.Monday, -1),
		observed(day(2026, 7, 4)),
		nth(2026, time.September, time.Monday, 1),
		observed(day(2026, 11, 11)),
		nth(2026, time.November, time.Thursday, 4),
		nth(2026, time.November, time.Thursday, 4).AddDays(1),
		day(2026, 12, 25),
		observed(day(2027, 1, 1)),
		nth(2027, time.January, time.Monday, 3),
		nth(2027, time.February, time.Monday, 3),
	}
	if want[4] != day(2026, 7, 3) || want[3] != day(2026, 5, 25) || want[11] != day(2027, 1, 18) {
		t.Fatalf("worked the holidays out wrongly: %v", want)
	}
	cal := must(oklahomataxcalc.EstimatedCalendar(2026))
	got := cal.Holidays()
	if len(got) != len(want) {
		t.Fatalf("%d holidays, want %d", len(got), len(want))
	}
	for i, h := range got {
		if h.On() != want[i] {
			t.Errorf("holiday %d (%s) is %v, want %v", i+1, h.Name(), h.On(), want[i])
		}
	}
	// The calendar moves a date that falls on a weekend or holiday.
	for from, to := range map[estimated.Date]estimated.Date{
		day(2026, 7, 3):   day(2026, 7, 6),   // a holiday, then a weekend
		day(2026, 11, 26): day(2026, 11, 30), // Thanksgiving and the day after
		day(2027, 1, 31):  day(2027, 2, 1),   // a Sunday
		day(2027, 1, 16):  day(2027, 1, 19),  // a weekend, then MLK Day
		day(2026, 4, 15):  day(2026, 4, 15),
	} {
		if got := cal.NextBusinessDay(from); got != to {
			t.Errorf("%v moves to %v, want %v", from, got, to)
		}
	}
	if _, err := oklahomataxcalc.EstimatedCalendar(2027); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2027 calendar: %v, want ErrNoRules", err)
	}
}

// The 2026 dates are the ones OW-8-ES prints. Each is a business day, so
// none moves.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t)))
	cal := must(oklahomataxcalc.EstimatedCalendar(2026))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if !cal.IsBusinessDay(in.Due()) {
			t.Errorf("installment %d falls on a %v or holiday", i+1, in.Due().Weekday())
		}
	}
	if applied(res.Applied(), "falls on a weekend or legal holiday") {
		t.Error("a 2026 due date was reported as moved")
	}
}

// § 2385.9(A)(1)'s January 31, 2027 is a Sunday, and § 82.1(C) moves it to
// Monday, February 1. The waiver is reported there and not applied, because
// the forms have no step for it.
func TestEstimatedJanuaryThirtyFirstMovesAndIsNotApplied(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 1)}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !applied(res.Applied(), "January 31, 2027 is a weekend or legal holiday, so the date moves to February 1, 2027") {
		t.Errorf("the move to February 1 is not reported: %v", res.Applied())
	}
	lines := res.Value().Lines()
	if len(lines) != 4 || lines[3].Total() == 0 {
		t.Fatalf("the January installment bears no interest; %d lines", len(lines))
	}
	// $1,750 for the 17 days from January 15 to February 1: $16.30.
	if lines[3].Total() != 1_630 {
		t.Errorf("January interest %v, want $16.30", lines[3].Total())
	}
	if !applied(res.Applied(), "the January 15 installment carries $16.30 of interest, which the statute's waiver would remove") {
		t.Errorf("the waiver's effect is not reported")
	}
	h.returnFiled = day(2027, 2, 2)
	if applied(must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Applied(), "statute waives") {
		t.Error("the waiver is reported for a return filed February 2")
	}
}

// The thresholds, one cent either side: $500 of tax over withholding, tax
// of $1,000, and a farm share of two-thirds this year or last.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tax      money.USCents
		withheld money.USCents
		exempt   bool
	}{
		{"$499.99 over withholding", 200_000, 150_001, true},
		{"$500.00 over withholding", 200_000, 150_000, false},
		{"tax of $999.99", 99_999, 0, true},
		{"tax of $1,000.00", 100_000, 0, false},
	} {
		res := must(rules(t).Requirement(household{tax: tc.tax, withheld: tc.withheld}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", tc.name, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		// The exception is the chosen option, at $0.
		want := map[string]string{"$499.99 over withholding": "Tax less withholding under $500", "tax of $999.99": "Tax after credits under $1,000"}[tc.name]
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != want) {
			t.Errorf("%s: chose %v %q at %v, want %q", tc.name, c.Basis(), c.Label(), c.Amount(), want)
		}
		// $1,500 withheld already reaches 70% of $2,000; nothing withheld
		// reaches nothing.
		if !tc.exempt && res.Value().Required() != (tc.withheld == 0) {
			t.Errorf("%s: required %v: %s", tc.name, res.Value().Required(), res.Value().Why())
		}
	}
	for _, tc := range []struct {
		this, last money.Rate
		exempt     bool
	}{
		{6_666, 0, false},
		{6_667, 0, true},
		{0, 6_666, false},
		{0, 6_667, true},
	} {
		e := must(oklahomataxcalc.NewEstimatedFacts().PriorYearFarmShare(tc.last).Build())
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, farmShare: tc.this, extras: &e}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("farm share %v this year, %v last: exempt %v, want %v", tc.this, tc.last, res.Value().Exempt(), tc.exempt)
		}
	}
}

// The target is the lesser of 70% of this year's tax and 100% of last
// year's, and last year's needs a filed 12-month return.
func TestEstimatedTargetOptions(t *testing.T) {
	// 70% of $10,000 is $7,000; last year's $8,000 is more.
	r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: filed(8_000 * money.Dollar)}.facts(t))).Value()
	if c, _ := r.Chosen(); c.Basis() != estimated.BasisThisYearTax || r.Target() != 7_000*money.Dollar {
		t.Errorf("chose %v at %v, want this year's tax at $7,000", c.Basis(), r.Target())
	}
	// Last year's $6,000 is less, and 100% at any income.
	r = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: filed(6_000 * money.Dollar)}.facts(t))).Value()
	if c, _ := r.Chosen(); c.Basis() != estimated.BasisPriorYearTax || c.Percent() != money.Percent(100) || r.Target() != 6_000*money.Dollar {
		t.Errorf("chose %v at %v of %v, want last year's tax at 100%%, $6,000", c.Basis(), c.Percent(), r.Target())
	}

	short := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(100).Build())).Build())
	unfiled := must(estimated.NewPriorReturn().Filed(false).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   bool
	}{
		"no return on record": {nil, true},
		"not filed":           {&unfiled, false},
		"an 11-month return":  {&short, false},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		o := res.Value().Options()[1]
		if o.Available() || o.WhyNot() == "" {
			t.Errorf("%s: last year's option is available", name)
		}
		if res.Value().Target() != 7_000*money.Dollar {
			t.Errorf("%s: target %v, want $7,000", name, res.Value().Target())
		}
		if has(res.Required(), working.PositionPriorYearReturn) != tc.ask {
			t.Errorf("%s: asked for last year's return %v, want %v", name, !tc.ask, tc.ask)
		}
	}
}

// Without this year's tax the target is last year's tax alone, the
// interest is an upper bound, and with no last year there is no target.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: filed(8_000 * money.Dollar)}
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
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{200_000, 400_000, 600_000, 800_000} {
		if sched[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, sched[i].NeededBy(), want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if !pen.UpperBound() || pen.Total() == 0 {
		t.Errorf("unpaid: interest %v, upper bound %v", pen.Total(), pen.UpperBound())
	}

	// Withholding that reaches last year's tax needs nothing more.
	h.withheld = 8_000 * money.Dollar
	if r := must(rules(t).Requirement(h.facts(t))).Value(); r.Required() {
		t.Errorf("withholding reaches the target and payments are required: %s", r.Why())
	}

	none := household{noTax: true}
	r = must(rules(t).Requirement(none.facts(t))).Value()
	if r.TargetKnown() || r.Required() {
		t.Errorf("no tax and no last year: target known %v, required %v", r.TargetKnown(), r.Required())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}

	// An exception that needs nothing about this year still applies, and its
	// zero is exact.
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	exact := must(rules(t).Penalty(household{noTax: true, prior: &notRequired}.facts(t), day(2027, 4, 15))).Value()
	if exact.Total() != 0 || exact.UpperBound() {
		t.Errorf("not required to file: interest %v, upper bound %v; want an exact zero", exact.Total(), exact.UpperBound())
	}
}

// OW-8-P Part 3 on $10,000 of tax and nothing paid: $1,750 a quarter, each
// charged 20% ÷ 365 a day to April 15, 2027.
//
//	April 15:     $1,750 × 365 days × 20% ÷ 365 = $350.00
//	June 15:      $1,750 × 304 days             = $291.51
//	September 15: $1,750 × 212 days             = $203.29
//	January 15:   $1,750 × 90 days              =  $86.30
//
// Oklahoma's rate is fixed by statute and no published change falls in the
// window, so the April shortfall runs past November 1, 2026, HB 2730's
// effective date, and past the year end, as one span at 20%.
func TestEstimatedInterestArithmetic(t *testing.T) {
	f := household{tax: 10_000 * money.Dollar}.facts(t)
	for _, asOf := range []estimated.Date{day(2027, 4, 15), day(2027, 12, 31)} {
		res := must(rules(t).Penalty(f, asOf))
		p := res.Value()
		if p.AsOf() != day(2027, 4, 15) {
			t.Errorf("figured to %v, want April 15, 2027", p.AsOf())
		}
		wantDays := []int{365, 304, 212, 90}
		wantCharge := []money.USCents{35_000, 29_151, 20_329, 8_630}
		lines := p.Lines()
		if len(lines) != 4 {
			t.Fatalf("%d lines, want four", len(lines))
		}
		for i, l := range lines {
			parts := l.Parts()
			if l.Shortfall() != 1_750*money.Dollar || len(parts) != 1 || parts[0].Days() != wantDays[i] ||
				parts[0].Rate() != money.Percent(20) || l.Total() != wantCharge[i] {
				t.Errorf("line %d: %v short, %d parts, total %v; want $1,750, one part of %d days at 20%%, %v",
					i+1, l.Shortfall(), len(parts), l.Total(), wantDays[i], wantCharge[i])
			}
		}
		if p.Total() != 93_110 {
			t.Errorf("total %v, want $931.10", p.Total())
		}
		if has(res.Required(), working.PositionUnpublishedRate) {
			t.Error("an unpublished rate is reported for a rate fixed by statute")
		}
	}
}

// A payment goes to the earliest balance first. $1,000 on April 15 leaves
// $750 of April's $1,750 short; $2,000 on May 15 pays it after 30 days and
// leaves $1,250 toward June, which is then $500 short.
//
//	April:     $750 × 30 days × 20% ÷ 365 = $12.33
//	June:      $500 × 304 days            = $83.29
//	September: $1,750 × 212 days          = $203.29
//	January:   $1,750 × 90 days           = $86.30
func TestEstimatedPaymentsApplyEarliestFirst(t *testing.T) {
	f := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{
		pay(day(2026, 4, 15), 1_000*money.Dollar), pay(day(2026, 5, 15), 2_000*money.Dollar)}}.facts(t)
	p := must(rules(t).Penalty(f, day(2027, 4, 15))).Value()
	want := []money.USCents{1_233, 8_329, 20_329, 8_630}
	for i, l := range p.Lines() {
		if l.Total() != want[i] {
			t.Errorf("line %d: %v, want %v", i+1, l.Total(), want[i])
		}
	}
	if p.Total() != 38_521 {
		t.Errorf("total %v, want $385.21", p.Total())
	}
	sched := must(rules(t).Schedule(f)).Value()
	if sched[0].Shortfall() != 750*money.Dollar || sched[1].Shortfall() != 500*money.Dollar {
		t.Errorf("shortfalls %v and %v, want $750 and $500", sched[0].Shortfall(), sched[1].Shortfall())
	}

	// Last year's overpayment applied forward counts on April 15.
	over := must(estimated.NewPayment().Amount(1_750 * money.Dollar).FromPriorYear().Build())
	s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{over}}.facts(t))).Value()
	if s[0].Status() != estimated.StatusMet || s[0].CreditedBy() != 1_750*money.Dollar {
		t.Errorf("an overpayment applied forward: April is %v with %v credited", s[0].Status(), s[0].CreditedBy())
	}
}

// Withholding counts a quarter on each due date, whenever withheld, unless
// the filer shows the dates.
func TestEstimatedWithholding(t *testing.T) {
	// $4,000 withheld in December; 70% of $10,000 is $7,000, $1,750 a
	// quarter. $1,000 counts on each date, so April is $750 short, and each
	// later $1,000 first pays the oldest balance.
	h := household{tax: 10_000 * money.Dollar, withheld: 4_000 * money.Dollar}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	wantShort := []money.USCents{75_000, 150_000, 175_000, 175_000}
	for i, in := range s {
		if in.CreditedBy() != money.USCents(i+1)*1_000*money.Dollar || in.Shortfall() != wantShort[i] {
			t.Errorf("spread: installment %d has %v credited and is %v short, want %v and %v",
				i+1, in.CreditedBy(), in.Shortfall(), money.USCents(i+1)*1_000*money.Dollar, wantShort[i])
		}
	}
	h.asWithheld = true
	res := must(rules(t).Schedule(h.facts(t)))
	if res.Value()[0].Shortfall() != 1_750*money.Dollar {
		t.Errorf("as withheld: April is %v short, want $1,750", res.Value()[0].Shortfall())
	}
	if !has(res.Required(), working.PositionUnsettledRule) || !applied(res.Applied(), "credited on the dates it was withheld") {
		t.Error("crediting withholding on its dates is not reported as the open question it is")
	}
	// Withholding at the target: nothing required, and no interest.
	h = household{tax: 10_000 * money.Dollar, withheld: 7_000 * money.Dollar}
	if r := must(rules(t).Requirement(h.facts(t))).Value(); r.Required() {
		t.Errorf("withholding reaches the target: %s", r.Why())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(); p.Total() != 0 {
		t.Errorf("interest %v with withholding at the target", p.Total())
	}
}

// OW-8-P-SUP-I with the 2026 periods. Tax after credits on the annualised
// income of $2,000, $4,000, $8,000 and $9,000; the regular quarter is $1,750.
//
//	17.5% of $2,000 = $350, under $1,750:          $350
//	35% of $4,000 = $1,400 less $350 = $1,050:     $1,050
//	52.5% of $8,000 = $4,200 less $1,400 = $2,800: $2,800, under $1,750 plus the $2,100 not yet made up
//	70% of $9,000 = $6,300 less $4,200 = $2,100:   $2,100, under $1,750 plus the $1,050 not yet made up
func TestEstimatedAnnualisedMethod(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar,
		annualised: []money.USCents{2_000 * money.Dollar, 4_000 * money.Dollar, 8_000 * money.Dollar, 9_000 * money.Dollar}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{35_000, 140_000, 420_000, 630_000} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v by its date, want %v", i+1, s[i].NeededBy(), want)
		}
	}
	bad := estimated.NewFacts(ok, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).
		Elections(must(estimated.NewElections().Annualised().Build()))
	for _, end := range []estimated.Date{day(2026, 3, 31), day(2026, 6, 30), day(2026, 8, 31), day(2026, 12, 31)} {
		bad.Periods(must(estimated.NewPeriod().End(end).Build()))
	}
	if _, err := rules(t).Schedule(must(bad.Build())); err == nil {
		t.Error("a second period ending June 30 was accepted")
	}
}

// The exceptions: tax under $1,000, under $500 over withholding, no return
// required last year, a filed return showing no tax, and farming.
func TestEstimatedExceptions(t *testing.T) {
	// Under $1,000: exempt, with the test's basis asked about.
	res := must(rules(t).Requirement(household{tax: 900 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() || !has(res.Required(), working.PositionUnsettledRule) {
		t.Errorf("$900 of tax: exempt %v, basis asked %v", res.Value().Exempt(), has(res.Required(), working.PositionUnsettledRule))
	}
	if p := must(rules(t).Penalty(household{tax: 900 * money.Dollar}.facts(t), day(2027, 4, 15))).Value(); p.Total() != 0 {
		t.Errorf("$900 of tax owes %v", p.Total())
	}

	// Not required to file last year: the form excuses everyone, and the
	// statute only a full-year resident.
	for name, tc := range map[string]struct {
		build func(*estimated.PriorReturnBuilder) *estimated.PriorReturnBuilder
		ask   working.PositionKind
	}{
		"resident":            {func(b *estimated.PriorReturnBuilder) *estimated.PriorReturnBuilder { return b.FullYearResident(true) }, working.PositionUnspecified},
		"residence not given": {func(b *estimated.PriorReturnBuilder) *estimated.PriorReturnBuilder { return b }, working.PositionResidency},
		"part-year":           {func(b *estimated.PriorReturnBuilder) *estimated.PriorReturnBuilder { return b.FullYearResident(false) }, working.PositionUnsettledRule},
	} {
		p := must(tc.build(estimated.NewPriorReturn().NotRequiredToFile()).Build())
		res := must(rules(t).Penalty(household{tax: 50_000 * money.Dollar, prior: &p}.facts(t), day(2027, 4, 15)))
		if res.Value().Total() != 0 {
			t.Errorf("%s: interest %v when no return was required last year", name, res.Value().Total())
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
		if tc.ask == working.PositionUnspecified && len(res.Required()) != 0 {
			t.Errorf("%s: asked %v", name, res.Required())
		}
	}

	// A filed return showing no tax gives a target of nothing.
	r := must(rules(t).Requirement(household{tax: 50_000 * money.Dollar, prior: filed(0)}.facts(t))).Value()
	if r.Required() || r.Target() != 0 {
		t.Errorf("no tax last year: required %v, target %v", r.Required(), r.Target())
	}

	// A farmer this year is exempt; a fisher has no exception.
	if r := must(rules(t).Requirement(household{tax: 50_000 * money.Dollar, farmShare: 7_000}.facts(t))).Value(); !r.Exempt() || !strings.Contains(r.Why(), "two-thirds") {
		t.Errorf("a farmer: %s", r.Why())
	}
	fisher := must(estimated.NewFacts(ok, 2026).FilingStatus(filing.Single).FishShare(7_000).
		ThisYear(must(estimated.NewFigures().Tax(50_000 * money.Dollar).Build())).Build())
	if r := must(rules(t).Requirement(fisher)).Value(); r.Exempt() || !r.Required() {
		t.Errorf("a fisher: exempt %v: %s", r.Exempt(), r.Why())
	}
	// Last year's farm share not given is asked for.
	if res := must(rules(t).Requirement(fisher)); !has(res.Required(), working.PositionJurisdictionFact) {
		t.Error("last year's farm share is not asked for")
	}

	// A waiver asked for is not given.
	casualty := estimated.WaiverCasualty
	res2 := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, waiver: &casualty}.facts(t), day(2027, 4, 15)))
	if res2.Value().Total() != 93_110 || !applied(res2.Applied(), "have no waiver") {
		t.Errorf("a casualty waiver: interest %v", res2.Value().Total())
	}
}

// A return filed with the balance paid stops the interest that day.
func TestEstimatedReturnFiledStopsInterest(t *testing.T) {
	p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 3, 1)}.facts(t), day(2027, 4, 15)))
	if p.Value().AsOf() != day(2027, 3, 1) {
		t.Errorf("figured to %v, want March 1, 2027", p.Value().AsOf())
	}
	// April's $1,750 for 320 days: $306.85.
	if got := p.Value().Lines()[0].Total(); got != 30_685 {
		t.Errorf("April's line %v, want $306.85", got)
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][4]string{
		"No liability last year":                {"didn't have to file", "OW-8-P (2025), Part 1 line 7", "lived in Oklahoma all of a 12-month prior year", "§ 2385.13(C)"},
		"A return filed and paid by January 31": {"OW-8-P has no step for it", "OW-8-P (2025), Part 3", "waives the January 15 installment", "§ 2385.9(A)(1)"},
		"Who must make estimated payments":      {"70% of this year's tax and last year's tax", "OW-8-ES (revised 6-2025)", "no condition on withholding", "§ 2385.7(A)"},
		"Married individuals":                   {"its own tax and withholding", "OW-8-ES (revised 6-2025)", "combined tax over withholding", "§ 2385.7(A)(2)"},
		"The $1,000 test":                       {"after refundable and nonrefundable credits", "OW-8-P (2025), Part 1 lines 1 to 3", "doesn't say whether", "§ 2385.13(C)"},
		"The farmer share":                      {"66.67% (two-thirds)", "line 22", "66 2/3%", "§ 2385.7(B)(2)"},
	}
	if len(res.Divergences()) != len(want) {
		t.Errorf("%d divergences, want %d", len(res.Divergences()), len(want))
	}
	for _, d := range res.Divergences() {
		w, found := want[d.Rule()]
		if !found {
			t.Errorf("unexpected divergence %q", d.Rule())
			continue
		}
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.FormSource(), w[1]) ||
			!strings.Contains(d.Statute(), w[2]) || !strings.Contains(d.StatuteSource(), w[3]) {
			t.Errorf("%q does not pin both readings: form %q (%s), statute %q (%s)", d.Rule(), d.Form(), d.FormSource(), d.Statute(), d.StatuteSource())
		}
	}
	// The figure follows the form on each: a filer who filed separately is
	// tested on their own return.
	r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, status: filing.MarriedFilingSeparately}.facts(t)))
	if !r.Value().Required() || !applied(r.Applied(), "combined liability") {
		t.Errorf("filing separately: required %v", r.Value().Required())
	}
	// A single filer is untouched by the married rule; the separate filer is.
	for _, tc := range []struct {
		res     working.Result[estimated.Requirement]
		affects bool
	}{{res, false}, {r, true}} {
		for _, d := range tc.res.Divergences() {
			if d.Rule() == "Married individuals" && (d.AffectsHousehold() != tc.affects || d.Uses() != working.ReadingForm) {
				t.Errorf("%q affects %v, uses %v; want affects %v", d.Rule(), d.AffectsHousehold(), d.Uses(), tc.affects)
			}
		}
	}
}

// Facts for another jurisdiction, another year or with another state's
// extras are refused, and a year not built is ErrNoRules. Oklahoma has no
// class of filer the facts can describe whose rules are not built: farmers
// are exempt and that is built, and fiscal years and estates cannot be
// described. So nothing here returns ErrUnsupported.
func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := oklahomataxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"Pennsylvania's facts": must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":                 must(estimated.NewFacts(ok, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":         must(estimated.NewFacts(ok, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Oklahoma's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date was figured")
	}
	for _, r := range []money.Rate{-1, 10_001} {
		if _, err := oklahomataxcalc.NewEstimatedFacts().PriorYearFarmShare(r).Build(); err == nil {
			t.Errorf("a farm share of %v was accepted", r)
		}
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$500", "70%", "20% a year", "$1,000", "January 31", "17.5%", "no 110% tier"} {
		if !strings.Contains(all, s) {
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
	b := estimated.NewFacts("OK", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
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

// Oklahoma's estimated tax statutes, 68 O.S. §§ 2385.7 and 2385.13, have no
// casualty, retirement or disability waiver, so even a retirement the
// federal rules would waive is refused.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar}
	before := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value().Total()
	if before == 0 {
		t.Fatal("no penalty to waive")
	}
	born := day(1962, 3, 1)
	for _, tc := range []struct {
		name, want, phrase string
		pos                *estimated.PositionsBuilder
	}{
		{"retired in 2026 at 64", "refused", "no waiver of this interest for retirement or disability", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(born)},
		{"retired in 2020", "refused", "for retirement or disability", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(born)},
		{"disaster", "refused", "postpones the due dates by order", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1))},
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
	}
}
