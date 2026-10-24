// West Virginia's estimated tax rules for 2026, checked against the Tax
// Division's documents and the West Virginia Code.
//
// West Virginia publishes no exhaustive oracle and no worked example with
// amounts. The strongest checks are IT-210's own: its chart of the days and
// blended daily rates for each column of the 2025 form, which spans the real
// change from 12.25% to 11.5%, and its example of a June payment that first
// pays April's underpayment. Every threshold, share, date and rate is
// compared against its extract in testdata/. The exceptions, the boundaries
// and the penalty amounts are hand-written cases.
package westvirginiataxcalc_test

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/westvirginiataxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

const wv = estimated.Jurisdiction("WV")

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
	return must(westvirginiataxcalc.EstimatedTax(2026))
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

// quarterly is one payment of a on each due date.
func quarterly(a ...money.USCents) []estimated.Payment {
	var out []estimated.Payment
	for i, x := range a {
		out = append(out, pay(dues[i], x))
	}
	return out
}

// A tax of $4,444.44 makes 90% $4,000.00, so $1,000 a quarter.
const thousandAQuarter = 444_444

type household struct {
	tax         money.USCents // IT-210 Part I line 3
	noTax       bool
	withheld    money.USCents
	withheldOn  estimated.Date
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *westvirginiataxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	annualised  []money.USCents // IT-210 Part II line 10 for each period
	asWithheld  bool
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions // replaces waiver when set
	farmShare   money.Rate
	fishShare   money.Rate
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts(wv, 2026).FilingStatus(filing.Single).ThisYear(figures).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled).
		FarmShare(h.farmShare).FishShare(h.fishShare)
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
	if h.positions != nil {
		b.Positions(*h.positions)
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

func usResident(yes bool) *westvirginiataxcalc.EstimatedFacts {
	e := must(westvirginiataxcalc.NewEstimatedFacts().PriorYearUSCitizenOrResident(yes).Build())
	return &e
}

// big is a prior-year tax large enough that 90% of this year's is the
// target.
var big = prior(1_000_000*money.Dollar, 12)

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "it140esi-2023.txt",
		"is at least $600, unless that liability is less than ten percent of your estimated tax.",
		"Current year quarterly installments are typically due April 15, June 15, and September 15 with the fourth quarter due January 15 of the following year.",
		"If the due date falls on Saturday, Sunday, or a legal holiday, the payment is due the next business day.",
		"If you file your return and pay the balance due by January 31 of the following year, you are NOT required to make the 4th quarter estimated tax installment.",
		"The annual interest rate for tax underpayments is determined by the State Tax Commissioner at six month intervals, but cannot be less than eight percent (8%).",
	)
	mustContain(t, "it140-2025-penalty.txt",
		"You may owe the penalty if you did not pay at least the smaller of 90% of your current year tax liability; or 100% of your prior tax liability (if you filed a prior year return that covered a full 12 months).",
		"You had $0 tax due after credit for the prior year and meet ALL of the following conditions:",
		"you were a citizen or resident of the United States throughout the preceding taxable year;",
		"your tax liability for the current year is less than $5,000.",
		"The total tax shown on your current return minus the tax you paid through West Virginia withholding is less than $600.",
		"If you file your tax return and pay any tax due on or before February 1, no fourth quarter penalty is due.",
		"If at least two-thirds of your gross income for this year was from farming sources, the following special rules apply:",
		"all or part of the penalty may be waived if the West Virginia Tax Division determines that reasonable cause has been shown.",
		"unless that liability is less than ten percent (10%) of your estimated tax.",
	)
	mustContain(t, "it210-2025.txt",
		"If line 5 is less than $600, you are not subject to the penalty and need not file form IT-210.",
		"Multiply line 3 by ninety percent (90%) and enter the result.",
		"Enter your tax after credits from your prior year West Virginia tax return.",
		"If line 7 is zero and line 3 is more than $5,000, enter the amount shown on line 6. Otherwise, enter the smaller of line 6 or line 7.",
		"Enter one-fourth of line 8, Part I, of Form IT-210 in each column.",
		"For most taxpayers, this is the amount shown on line 8 of PART I divided by four.",
		"January 1 March 31", "January 1 May 31", "January 1 August 31", "January 1 December 31",
		"11. Applicable percentage", "22.5%", "45%", "67.5%", "90%",
		"LINE 12 REQUIRED PAYMENTS. Multiply the amount on line 10 by the factor on line 11.",
		"For West Virginia income tax withheld, you are considered to have paid one-fourth of these amounts on each payment due date, unless you check the box on line 11 in Part I and show otherwise.",
		"If you filed your return by the due date (including extensions), treat the overpayment as a payment made on April 15.",
		"If you file your return and pay the tax due on or before February 1, include the tax you pay with your return in column (d) of line 2.",
		"For January 1, 2026 to December 31, 2026 the rate is 11.5%. The resultant daily rate of 0.000315 is applied to all underpayments.",
		"Each payment must be applied to the oldest outstanding underpayment.",
		"TO the date the amount on line 8 was paid, or 4/15, whichever is earlier",
		"The exact closing day of each quarter may shift to accommodate weekends or holidays as permitted by code.",
	)
	mustContain(t, "it140-2025-entries.txt", "Round off amounts to WHOLE DOLLARS – NO CENTS.")
	mustContain(t, "tsd365.txt", "January 1, 2026 - December 31, 2026 11.5%")
	mustContain(t, "wvcode-11-21-55.txt",
		"(1) After April 1 and before June 2 of the taxable year, the declaration shall be filed on or before June 15")
	mustContain(t, "wvcode-11-10-18a.txt",
		"reduced by the credit allowable for withheld tax, is less than $600.",
		"(D) The individual's West Virginia personal income tax liability for the current taxable year is less than $5,000.",
		"1st 22.5 2nd 45 3rd 67.5 4th 90",
		"If on or before the last day of the first month following the end of the taxable year, the taxpayer files his or her annual personal income tax return for that taxable year and pays in full",
		"(1) The due date of the annual return following the close of the taxable year for which the installment was due (determined without regard to any extension of time for filing such annual return)",
		"a payment of estimated tax shall be credited against unpaid required installments in the order in which such installments are required to be paid.",
		"(j) Special rules for farmers.",
	)
	mustContain(t, "wvcode-11-10-17a.txt",
		"the adjusted prime rate charged by banks (rounded to the nearest hundredth of a percent) plus three percentage points per annum as of the first business day of December")
	mustContain(t, "wvcode-2-2-1.txt",
		`(2) The third Monday of January is "Martin Luther King's Birthday";`,
		`(4) The last Monday in May is "Memorial Day";`,
		`(5) June 20 is "West Virginia Day";`,
		`(6) July 4 is "Independence Day";`,
		`(7) The first Monday of September is "Labor Day";`,
		`(8) The second Monday of October is "Columbus Day";`,
		`(9) November 11 is "Veterans' Day";`,
		`(10) The fourth Thursday of November is "Thanksgiving Day";`,
		`(11) The day after Thanksgiving Day is "Lincoln's Day";`,
		`(12) December 25 is "Christmas Day";`,
		`(1) January 1 is "New Year's Day";`,
		"If a holiday otherwise described in subsection (a) of this section falls on a Saturday, then the preceding Friday is the legal holiday",
		"then the date on which the act, event, default or omission is required or allowed to occur is the next day that is not a Saturday, Sunday, legal holiday, or designated day off.",
	)
}

// The forms are followed because neither predates the law: the IT-140ESI is
// dated September 2023 and the IT-210 is for 2025, and each section's bill
// history ends earlier.
func TestEstimatedFormsPostdateTheLaw(t *testing.T) {
	mustContain(t, "it140esi-2023.txt", "Sept 2023 | West Virginia Tax Division")
	mustContain(t, "it140-2025-penalty.txt", "22 — 2025 West Virginia Personal Income Tax Information and Instructions")
	for file, latest := range map[string]string{
		"wvcode-11-10-18a.txt": "Bill History For §11-10-18A 1998 Regular Session HB4303",
		"wvcode-11-21-55.txt":  "Bill History For §11-21-55 1990 Regular Session HB4793",
		"wvcode-11-21-56.txt":  "Bill History For §11-21-56 1961 Regular Session SB106",
		"wvcode-11-10-17a.txt": "Bill History For §11-10-17A 2015 Regular Session HB3006",
	} {
		mustContain(t, file, latest)
	}
}

// wvHolidays are the § 2-2-1(a) holidays for a year, moved under § 2-2-1(b),
// worked out here from the statute's own rules rather than copied from the
// package.
func wvHolidays(year int) []estimated.Date {
	nth := func(m time.Month, wd time.Weekday, n int) estimated.Date {
		d := day(year, m, 1)
		for d.Weekday() != wd {
			d = d.AddDays(1)
		}
		return d.AddDays(7 * (n - 1))
	}
	lastMonday := func(m time.Month) estimated.Date {
		d := day(year, m+1, 1).AddDays(-1)
		for d.Weekday() != time.Monday {
			d = d.AddDays(-1)
		}
		return d
	}
	observed := func(d estimated.Date) estimated.Date {
		switch d.Weekday() {
		case time.Saturday:
			return d.AddDays(-1)
		case time.Sunday:
			return d.AddDays(1)
		}
		return d
	}
	thanksgiving := nth(time.November, time.Thursday, 4)
	return []estimated.Date{
		observed(day(year, 1, 1)), nth(time.January, time.Monday, 3), nth(time.February, time.Monday, 3),
		lastMonday(time.May), observed(day(year, 6, 20)), observed(day(year, 7, 4)), nth(time.September, time.Monday, 1),
		nth(time.October, time.Monday, 2), observed(day(year, 11, 11)), thanksgiving, thanksgiving.AddDays(1),
		observed(day(year, 12, 25)),
	}
}

// The 2026 dates are the ones the IT-140ESI prints. None falls on a weekend
// or a § 2-2-1 holiday, so none moves.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	holidays := append(wvHolidays(2026), wvHolidays(2027)...)
	if holidays[4] != day(2026, 6, 19) || holidays[5] != day(2026, 7, 3) {
		t.Errorf("West Virginia Day and Independence Day 2026 are %v and %v, want the Fridays before", holidays[4], holidays[5])
	}
	res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
		for _, h := range holidays {
			if in.Due() == h {
				t.Errorf("installment %d falls on the holiday %v", i+1, h)
			}
		}
	}
}

// § 11-10-18a(i) excuses the January installment for a return filed and paid
// by the last day of January. January 31, 2027 is a Sunday, and § 2-2-1(e)
// moves it to Monday, February 1, the date the booklet prints. The penalty
// stops on the day the return is paid.
func TestEstimatedReturnFiledByJanuaryThirtyFirstMoves(t *testing.T) {
	if wd := day(2027, 1, 31).Weekday(); wd != time.Sunday {
		t.Fatalf("January 31, 2027 is a %v", wd)
	}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 1, 29), true}, {day(2027, 2, 1), true}, {day(2027, 2, 2), false}} {
		h := household{tax: thousandAQuarter, prior: big, payments: quarterly(1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar), returnFiled: tc.filed}
		s := must(rules(t).Schedule(h.facts(t))).Value()
		// Excused, January needs nothing more than September, so the
		// $3,000 paid meets it; otherwise it is $1,000 short.
		excused := s[3].NeededBy() == s[2].NeededBy() && s[3].Status() == estimated.StatusMet && s[3].Shortfall() == 0
		short := s[3].NeededBy() == 4_000*money.Dollar && s[3].Status() == estimated.StatusShort && s[3].Shortfall() == 1_000*money.Dollar
		if excused != tc.excused || short == tc.excused {
			t.Errorf("filed %v: January needs %v and is %v, short %v; want excused %v", tc.filed, s[3].NeededBy(), s[3].Status(), s[3].Shortfall(), tc.excused)
		}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if p.Value().AsOf() != tc.filed {
			t.Errorf("filed %v: figured to %v", tc.filed, p.Value().AsOf())
		}
		// Filed February 2: January's $1,000 is short for 18 days, all of
		// 2027, which has no published rate.
		if want := !tc.excused; (len(p.Value().Lines()) == 1) != want {
			t.Errorf("filed %v: %d penalty lines", tc.filed, len(p.Value().Lines()))
		}
	}
}

// IT-210's chart for 2025 is the nearest real change of rate: 12.25%
// (0.000336 a day) for 2025 and 11.5% (0.000315) for 2026. Each column runs
// from its due date to April 15, 2026, and June 15, 2025 was a Sunday, so the
// second column starts on Monday, June 16, which is why it has 303 days and
// not 304. Splitting each column at December 31 and charging each year at
// its own factor gives the chart's blended rates. This checks the method the
// package uses, with the shared helpers, against the form.
func TestEstimatedRateChangeMatchesTheBlendedChart(t *testing.T) {
	mustContain(t, "it210-2025.txt",
		"For January 1, 2025 to December 31, 2025 the rate is 12.25%. The resultant daily rate of 0.000336 is applied to all underpayments.",
		"Rate Period Column Line 10 Blended Daily Rate Q1 (a) 365 0.000330 Q2 (b) 303 0.000329 Q3 (c) 212 0.000326 Q4 (d) 90 0.000315")
	mustContain(t, "tsd365.txt", "January 1, 2025 - December 31, 2025 12.25%")
	series := must(estimated.NewFactorSeries(estimated.RoundToCent).
		Period(day(2025, 1, 1), day(2025, 12, 31), must(estimated.ParseFactor("0.000336")), 1225, "IT-210 (2025)").
		Period(day(2026, 1, 1), day(2026, 12, 31), must(estimated.ParseFactor("0.000315")), 1150, "IT-210 (2025)").
		Build())
	weekends := must(estimated.NewCalendar("weekends").Build())
	moved := estimated.MoveDates(weekends, []estimated.Date{day(2025, 4, 15), day(2025, 6, 15), day(2025, 9, 15), day(2026, 1, 15)})
	if moved[1] != day(2025, 6, 16) {
		t.Fatalf("June 15, 2025 moved to %v, want Monday, June 16", moved[1])
	}
	wantDays := []int{365, 303, 212, 90}
	wantRate := []string{"0.000330", "0.000329", "0.000326", "0.000315"}
	const principal = 1_000_000 * money.Dollar
	for i, due := range moved {
		parts, gap, err := estimated.DailyFactor(principal, due, day(2026, 4, 15), series)
		if err != nil || !gap.IsZero() {
			t.Fatalf("column %d: %v, gap %v", i+1, err, gap)
		}
		days := 0
		var charge money.USCents
		for _, p := range parts {
			days += p.Days()
			charge += p.Charge()
		}
		if days != wantDays[i] {
			t.Errorf("column %d: %d days, want %d", i+1, days, wantDays[i])
		}
		// The blended daily rate, to six places: charge ÷ principal ÷ days.
		micro := (int64(charge)*1_000_000/int64(days)*10/int64(principal) + 5) / 10
		if got := fmt.Sprintf("0.%06d", micro); got != wantRate[i] {
			t.Errorf("column %d: blended rate %s, want %s", i+1, got, wantRate[i])
		}
	}
}

// $600 or more of tax after withholding brings the penalty in; $599.99 does
// not. The IT-140ESI's 10% test excuses payments when withholding leaves
// less than 10% of the tax, which is when withholding exceeds 90% of it.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{940_001, true},  // $599.99 after withholding
		{940_000, false}, // $600.00
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld, prior: big}.facts(t)))
		if got := res.Value().Exempt(); got != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, got, tc.exempt, res.Value().Why())
		}
		c, _ := res.Value().Chosen()
		if isException := c.Basis() == estimated.BasisException && c.Amount() == 0 && c.Label() == "Tax less withholding under $600"; isException != tc.exempt {
			t.Errorf("withheld %v: chosen %v at %v, %q", tc.withheld, c.Basis(), c.Amount(), c.Label())
		}
		if strings.Contains(res.Value().Why(), "$600, which is under $600") {
			t.Errorf("withheld %v: the reason rounds to a figure that is not under $600: %s", tc.withheld, res.Value().Why())
		}
	}
	for _, tc := range []struct {
		withheld   money.USCents
		required   bool
		smallShare bool
	}{
		{899_999, true, false},  // $1,000.01 left, above 10%
		{900_000, false, false}, // $1,000.00 left, exactly 10%, and withholding reaches 90%
		{900_001, false, true},  // $999.99 left, under 10%
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld, prior: big}.facts(t)))
		r := res.Value()
		if r.Required() != tc.required || strings.Contains(r.Why(), "less than 10%") != tc.smallShare {
			t.Errorf("withheld %v: required %v, why %q", tc.withheld, r.Required(), r.Why())
		}
	}
	if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, withheld: 940_001, prior: big}.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 || len(p.Value().Lines()) != 0 {
		t.Errorf("under $600 the penalty is %v", p.Value().Total())
	}
}

// The target is the smaller of 90% of this year's tax and 100% of last
// year's, for a 12-month return.
func TestEstimatedTargetOptions(t *testing.T) {
	for name, tc := range map[string]struct {
		prior  *estimated.PriorReturn
		target money.USCents
		chosen estimated.Basis
		ask    working.PositionKind
	}{
		"last year lower":     {prior(8_000*money.Dollar, 12), 8_000 * money.Dollar, estimated.BasisPriorYearTax, working.PositionUnspecified},
		"this year lower":     {prior(9_500*money.Dollar, 12), 9_000 * money.Dollar, estimated.BasisThisYearTax, working.PositionUnspecified},
		"an 11-month return":  {prior(8_000*money.Dollar, 11), 9_000 * money.Dollar, estimated.BasisThisYearTax, working.PositionUnspecified},
		"no return filed":     {notFiled(), 9_000 * money.Dollar, estimated.BasisThisYearTax, working.PositionUnspecified},
		"no return on record": {nil, 9_000 * money.Dollar, estimated.BasisThisYearTax, working.PositionPriorYearReturn},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		r := res.Value()
		c, _ := r.Chosen()
		if r.Target() != tc.target || c.Basis() != tc.chosen || !r.Required() {
			t.Errorf("%s: target %v from %v, required %v; want %v from %v", name, r.Target(), c.Basis(), r.Required(), tc.target, tc.chosen)
		}
		if p := r.Options()[1].Percent(); p != money.Percent(100) {
			t.Errorf("%s: prior-year option at %v", name, p)
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v not asked", name, tc.ask)
		}
	}
}

// IT-210 lowers each installment to a quarter of the prior-year figure. The
// Code instead measures the underpayment against 90% of this year's tax and
// only excuses an installment that reaches the prior-year figure. Paying
// $1,900 in April against a $2,000 prior-year quarter is charged, by the
// form, on $100; by the Code it would be $350 against $2,250.
func TestEstimatedPriorYearTestLowersTheInstallment(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, prior: prior(8_000*money.Dollar, 12),
		payments: quarterly(1_900*money.Dollar, 2_100*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar)}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	if s[0].Shortfall() != 100*money.Dollar || s[0].Status() != estimated.StatusShort || s[1].Status() != estimated.StatusMet {
		t.Errorf("April short %v (%v), June %v; want $100 short in April only", s[0].Shortfall(), s[0].Status(), s[1].Status())
	}
	// $100 for the 61 days to June 15 at 0.000315 is $1.92, so $2.
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 2*money.Dollar {
		t.Errorf("penalty %v, want $2", p.Value().Total())
	}
}

// IT-210 line 8: a zero tax last year makes the target zero while this
// year's tax is not more than $5,000. For a filer who was not required to
// file, the booklet's exception and § 11-10-18a(e)(2) need this year's tax
// under $5,000 and a year as a United States citizen or resident.
func TestEstimatedNoLiabilityLastYear(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tax    money.USCents
		prior  *estimated.PriorReturn
		extras *westvirginiataxcalc.EstimatedFacts
		exempt bool
		ask    bool
	}{
		{"filed, zero, $5,000.00", 500_000, prior(0, 12), nil, true, false},
		{"filed, zero, $5,000.01", 500_001, prior(0, 12), nil, false, false},
		{"filed, zero, 11 months", 400_000, prior(0, 11), nil, false, false},
		{"not required, $4,999.99", 499_999, notRequired(), usResident(true), true, false},
		{"not required, $5,000.00", 500_000, notRequired(), usResident(true), false, false},
		{"not required, not a US resident", 400_000, notRequired(), usResident(false), false, false},
		{"not required, residence not given", 400_000, notRequired(), nil, false, true},
	} {
		res := must(rules(t).Requirement(household{tax: tc.tax, prior: tc.prior, extras: tc.extras}.facts(t)))
		if got := res.Value().Exempt(); got != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", tc.name, got, tc.exempt, res.Value().Why())
		}
		if got := has(res.Required(), working.PositionJurisdictionFact); got != tc.ask {
			t.Errorf("%s: residence asked %v, want %v", tc.name, got, tc.ask)
		}
		if !tc.exempt && res.Value().Target() != money.Percent(90).Of(tc.tax) {
			t.Errorf("%s: target %v, want 90%% of %v", tc.name, res.Value().Target(), tc.tax)
		}
	}
}

// Shortfalls unpaid to December 31, 2026, charged at 0.000315 a day and
// rounded to the dollar: $1,000 for 260, 199 and 107 days is $81.90, $62.69
// and $33.71, so $82, $63 and $34. January's has no 2026 days.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	f := household{tax: thousandAQuarter, prior: big}.facts(t)
	res := must(rules(t).Penalty(f, day(2026, 12, 31)))
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	for i, want := range []money.USCents{82, 63, 34, 0} {
		if lines[i].Shortfall() != 1_000*money.Dollar || lines[i].Total() != want*money.Dollar {
			t.Errorf("line %d: %v on %v, want $%d on $1,000", i+1, lines[i].Total(), lines[i].Shortfall(), want)
		}
	}
	if res.Value().Total() != 179*money.Dollar || has(res.Required(), working.PositionUnpublishedRate) {
		t.Errorf("total %v, want $179 with every day rated", res.Value().Total())
	}
}

// Each column runs to April 15, 2027, as IT-210 line 10 counts: 365, 304,
// 212 and 90 days. The days after December 31, 2026 have no published rate,
// so each line's charge stops there and the gap is reported. The 2026 rate is
// one for the whole calendar year, so no change of rate falls inside the
// 2026 window; the split is at the end of the published period.
func TestEstimatedPenaltyAcrossTheRatePeriodBoundary(t *testing.T) {
	f := household{tax: thousandAQuarter, prior: big}.facts(t)
	res := must(rules(t).Penalty(f, day(2027, 6, 30)))
	if res.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", res.Value().AsOf())
	}
	lines := res.Value().Lines()
	for i, want := range []int{365, 304, 212, 90} {
		if lines[i].Days() != want {
			t.Errorf("line %d: %d days, want %d", i+1, lines[i].Days(), want)
		}
		for _, p := range lines[i].Parts() {
			if p.To().After(day(2026, 12, 31)) {
				t.Errorf("line %d charges past December 31, 2026", i+1)
			}
		}
	}
	if res.Value().Total() != 179*money.Dollar || !has(res.Required(), working.PositionUnpublishedRate) {
		t.Errorf("total %v; want $179 and the unpublished 2027 rate reported", res.Value().Total())
	}
	for _, r := range res.Required() {
		if r.Position() == working.PositionUnpublishedRate && (r.Question() != "" || !strings.Contains(r.Effect(), "rate for the days after Dec 31 ")) {
			t.Errorf("unpublished rate: question %q, effect %q; want no question and a stop at Dec 31", r.Question(), r.Effect())
		}
	}
}

// IT-210's example: a June 15 payment first pays April's underpayment, and
// what is left goes to June. A part payment splits the charge at the day it
// was paid.
func TestEstimatedPaymentsPayTheOldestUnderpaymentFirst(t *testing.T) {
	mustContain(t, "it210-2025.txt",
		"if you have an underpayment for April 15 installment period, the payment you make on June 15, 2025 will first be applied to pay off the April underpayment; any remaining portion of the payment will be applied to the June installment.")
	h := household{tax: thousandAQuarter, prior: big, payments: quarterly(0, 2_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar)}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	if s[0].Status() != estimated.StatusShort || s[1].Status() != estimated.StatusMet {
		t.Errorf("April %v, June %v; want April short and June met", s[0].Status(), s[1].Status())
	}
	// April's $1,000 for 61 days: $19.22, so $19.
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 19*money.Dollar {
		t.Errorf("penalty %v, want $19", p.Value().Total())
	}

	// $400 on May 15 and the rest of April on June 15: $400 for 30 days is
	// $3.78 and $600 for 61 days is $11.53, so $4 and $12.
	h.payments = []estimated.Payment{pay(day(2026, 5, 15), 400*money.Dollar), pay(dues[1], 1_600*money.Dollar), pay(dues[2], 1_000*money.Dollar), pay(dues[3], 1_000*money.Dollar)}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	parts := p.Value().Lines()[0].Parts()
	if len(parts) != 2 || parts[0].Days() != 30 || parts[1].Days() != 61 || p.Value().Total() != 16*money.Dollar {
		t.Errorf("parts %d, total %v; want 30 and 61 days, $16", len(parts), p.Value().Total())
	}
}

// IT-210 prints 0.000315 a day, not 11.5% ÷ 365. On a $100,000 shortfall for
// 260 days the two differ by $2: $8,190 printed, $8,191.78 from the rate.
func TestEstimatedInterestUsesThePrintedDailyFactor(t *testing.T) {
	// 90% of $444,444.44 is $400,000.00, a quarter of which is $100,000.
	f := household{tax: 44_444_444, prior: big}.facts(t)
	parts := must(rules(t).Penalty(f, day(2026, 12, 31))).Value().Lines()[0].Parts()
	if len(parts) != 1 || parts[0].Days() != 260 || parts[0].Charge() != 8_190*money.Dollar || parts[0].Rate() != 1150 {
		t.Errorf("parts %+v; want one of 260 days at 11.5%%, $8,190", parts)
	}
}

// Withholding counts a quarter on each date unless the filer shows the dates
// it was withheld. $9,000 of a $10,000 tax, all withheld on December 15,
// leaves April, June and September $2,250 short until then: 244, 183 and 91
// days, which is $172.94, $129.69 and $64.50, so $173, $130 and $64.
func TestEstimatedWithholdingOnTheDatesWithheld(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, prior: big, withheld: 9_000 * money.Dollar}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("spread evenly: penalty %v, want none", p.Value().Total())
	}
	h.asWithheld = true
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 367*money.Dollar || len(p.Value().Lines()) != 3 {
		t.Errorf("as withheld: penalty %v over %d lines, want $367 over three", p.Value().Total(), len(p.Value().Lines()))
	}
}

// IT-210 Part II: 22.5%, 45%, 67.5% and 90% of each period's annualised tax,
// less what earlier dates required, against the regular quarter plus what
// was carried. The four installments add to the regular total.
func TestEstimatedAnnualisedRecapture(t *testing.T) {
	h := household{tax: thousandAQuarter, prior: big, annualised: []money.USCents{400 * money.Dollar, 2_000 * money.Dollar, 3_000 * money.Dollar, thousandAQuarter},
		payments: quarterly(90*money.Dollar, 810*money.Dollar, 1_125*money.Dollar, 1_975*money.Dollar)}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{90, 900, 2_025, 4_000} {
		if s[i].NeededBy() != want*money.Dollar || s[i].Status() != estimated.StatusMet {
			t.Errorf("installment %d needs %v (%v), want $%d met", i+1, s[i].NeededBy(), s[i].Status(), want)
		}
	}
	h.annualised = nil
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() == 0 {
		t.Error("the same payments owe nothing without the annualised method")
	}
}

// Last year's overpayment applied forward counts on April 15.
func TestEstimatedPriorYearOverpayment(t *testing.T) {
	carried := must(estimated.NewPayment().Amount(1_000 * money.Dollar).FromPriorYear().Build())
	h := household{tax: thousandAQuarter, prior: big, payments: append([]estimated.Payment{carried}, quarterly(0, 1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar)...)}
	s := must(rules(t).Schedule(h.facts(t)))
	if s.Value()[0].Status() != estimated.StatusMet || s.Value()[0].CreditedBy() != 1_000*money.Dollar {
		t.Errorf("April is %v with %v credited", s.Value()[0].Status(), s.Value()[0].CreditedBy())
	}
	if !applied(s.Applied(), "overpayment") {
		t.Error("the overpayment's date is not stated")
	}
}

// A requirement first met after April 1 still owes the quarters IT-210
// prints, and the working says so.
func TestEstimatedLateStartUsesTheQuarters(t *testing.T) {
	for _, tc := range []struct {
		met  estimated.Date
		note bool
	}{{day(2026, 4, 1), false}, {day(2026, 4, 2), true}, {day(2026, 9, 2), true}} {
		res := must(rules(t).Schedule(household{tax: thousandAQuarter, prior: big, firstMet: tc.met}.facts(t)))
		if res.Value()[0].NeededBy() != 1_000*money.Dollar {
			t.Errorf("first met %v: April needs %v", tc.met, res.Value()[0].NeededBy())
		}
		if got := applied(res.Applied(), "first met"); got != tc.note {
			t.Errorf("first met %v: noted %v, want %v", tc.met, got, tc.note)
		}
	}
}

// The penalty is shown before any waiver, and the working names the waivers
// West Virginia has.
func TestEstimatedWaivers(t *testing.T) {
	mustContain(t, "it140-2025-waiver.txt", "REQUEST FOR WAIVER OF ESTIMATED PENALTY")
	mustContain(t, "wvcode-11-10-18a.txt", "by reason of casualty, disaster or other unusual circumstances the imposition of such addition to tax would be against equity and good conscience.")
	casualty, retired := estimated.WaiverCasualty, estimated.WaiverRetiredOrDisabled
	for _, tc := range []struct {
		waiver *estimated.WaiverKind
		says   string
	}{{&casualty, "casualty, disaster"}, {&retired, "no waiver for retirement"}, {nil, "reasonable cause"}} {
		res := must(rules(t).Penalty(household{tax: thousandAQuarter, prior: big, waiver: tc.waiver}.facts(t), day(2027, 4, 15)))
		if res.Value().Total() != 179*money.Dollar || !applied(res.Applied(), tc.says) {
			t.Errorf("waiver %v: penalty %v, working does not say %q", tc.waiver, res.Value().Total(), tc.says)
		}
	}
}

// § 11-10-18a(e)(3) grants the federal casualty waiver. Retiring after 62,
// which meets the federal conditions, is no waiver in West Virginia; the
// booklet's reasonable-cause waiver leaves open whether it counts.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "it140-2025-waiver.txt", "all or part of the penalty may be waived if the West Virginia Tax Division determines thatreasonable cause has been shown")
	disaster := must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build())
	retired := must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build())
	for _, tc := range []struct {
		name string
		pos  estimated.Positions
		want func(estimated.WaiverOutcome) bool
		in   string
		open bool
	}{
		{"a disaster", disaster, estimated.WaiverOutcome.Eligible, "Tax Commissioner can waive", false},
		{"retired in 2026 at 64", retired, estimated.WaiverOutcome.Undecided, "no waiver for retirement or disability", true},
	} {
		res := must(rules(t).Penalty(household{tax: thousandAQuarter, prior: big, positions: &tc.pos}.facts(t), day(2027, 4, 15)))
		o, ok := res.Value().WaiverOutcome()
		if !ok || !tc.want(o) || !strings.Contains(o.Reason(), tc.in) {
			t.Errorf("%s: outcome %q, %v", tc.name, o.Reason(), ok)
		}
		if res.Value().Total() != 179*money.Dollar {
			t.Errorf("%s: penalty %v, want $179 before any waiver", tc.name, res.Value().Total())
		}
		if got := has(res.Required(), working.PositionUnsettledRule); got != tc.open {
			t.Errorf("%s: open point asked %v, want %v", tc.name, got, tc.open)
		}
		if o.Eligible() && res.Value().IfWaived() != 0 {
			t.Errorf("%s: if waived %v, want $0", tc.name, res.Value().IfWaived())
		}
	}
}

// Under an exception, no question says its answer changes the target.
func TestEstimatedExemptAsksNothingAboutTheTarget(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 500 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() {
		t.Fatal("not exempt under $600")
	}
	for _, q := range res.Required() {
		if strings.Contains(strings.ToLower(q.Effect()), "target") {
			t.Errorf("an exempt requirement asks %q", q.Effect())
		}
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"Who must make estimated payments":      {"under 10%", "§ 11-21-55(a)"},
		"The prior-year test":                   {"smaller of 90%", "§ 11-10-18a(b)(1) and (d)(1)"},
		"No liability last year":                {"$5,000 or less", "(e)(2)"},
		"A requirement first met after April 1": {"every filer", "§§ 11-21-55(d), 11-21-56(a)"},
		"A return filed and paid by January 31": {"February 1", "§ 2-2-1(e)"},
		"The daily interest factor":             {"0.000315", "§§ 11-10-17a(c), 11-10-18a(a)"},
		"How the rate is set":                   {"every six months", "§ 11-10-17a(a) and (c)"},
		"Withholding on the dates withheld":     {"One box", "§ 11-10-18a(g)"},
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
	}

	// Which reading each follows, and which these facts reach. $10,000 of
	// tax, nothing withheld or paid, and last year's return not on record:
	// payments are required and every quarter is short, so the answer, the
	// daily factor and nothing else is touched.
	uses := map[string]working.Reading{"A return filed and paid by January 31": working.ReadingStatute, "How the rate is set": working.ReadingStatute}
	affects := map[string]bool{"Who must make estimated payments": true, "The daily interest factor": true}
	for _, d := range res.Divergences() {
		want := working.ReadingForm
		if u, ok := uses[d.Rule()]; ok {
			want = u
		}
		if d.Uses() != want || d.AffectsHousehold() != affects[d.Rule()] {
			t.Errorf("%q uses %v and affects %v; want %v and %v", d.Rule(), d.Uses(), d.AffectsHousehold(), want, affects[d.Rule()])
		}
	}
	// A return filed on Monday, February 1, 2027 is the one the IT-140ESI's
	// January 31 would not excuse; one filed January 29 is excused either way.
	for filed, want := range map[estimated.Date]bool{day(2027, 2, 1): true, day(2027, 1, 29): false} {
		h := household{tax: thousandAQuarter, prior: big, payments: quarterly(1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar), returnFiled: filed}
		for _, d := range must(rules(t).Requirement(h.facts(t))).Divergences() {
			if d.Rule() == "A return filed and paid by January 31" && d.AffectsHousehold() != want {
				t.Errorf("filed %v: the January 31 divergence affects %v, want %v", filed, d.AffectsHousehold(), want)
			}
		}
	}
	mustContain(t, "wvcode-11-10-18a.txt",
		"divided by the number of installments taxpayer was required to make for the taxable year",
		"(2) Separate application. -- The taxpayer may apply subdivision (1) of this subsection separately with respect to: (A) Wage withholding; and (B) All other amounts withheld")
	mustContain(t, "wvcode-11-21-55.txt", "can reasonably be expected to exceed $400 plus the sum of the West Virginia personal exemptions to which he is entitled.")
	mustContain(t, "wvcode-11-21-56.txt", "the estimated tax shall be paid in three equal installments.")
	mustContain(t, "wvcode-11-10-17a.txt", "for taxable years beginning prior to January 1, 2017, such annual rate shall not be less than eight percent per annum.")
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, year := range []int{2025, 2027} {
		if _, err := westvirginiataxcalc.EstimatedTax(year); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", year, err)
		}
	}
	if _, err := rules(t).Requirement(household{tax: 100, farmShare: 6_667}.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer: %v, want ErrUnsupported", err)
	}
	// West Virginia's farmer rule names farmers only.
	if _, err := rules(t).Requirement(household{tax: 100, fishShare: 7_000}.facts(t)); err != nil {
		t.Errorf("a fisher: %v", err)
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	wrongEnd := must(estimated.NewPeriod().End(day(2026, 4, 30)).Build())
	ends := []estimated.Date{day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
	b := estimated.NewFacts(wv, 2026).FilingStatus(filing.Single).ThisYear(this).Periods(wrongEnd).
		Elections(must(estimated.NewElections().Annualised().Build()))
	for _, e := range ends {
		b.Periods(must(estimated.NewPeriod().End(e).Build()))
	}
	for name, f := range map[string]estimated.Facts{
		"Pennsylvania's facts": must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":                 must(estimated.NewFacts(wv, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":         must(estimated.NewFacts(wv, 2026).FilingStatus(filing.Single).ThisYear(this).State("not West Virginia's").Build()),
		"a period ending late": must(b.Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date was figured")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$600", "$5,000", "11.5%", "0.000315", "no 110%", "February 1, 2027", "prime rate plus three points"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if len(e.Sources()) == 0 || e.VersusFederal() == "" {
		t.Error("the explanation has no sources or no comparison with federal")
	}
}

// Without this year's tax the target is 100% of last year's, the penalty is
// an upper bound, and a zero last year cannot be used because it depends on
// this year's tax being $5,000 or less.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(4_000*money.Dollar, 12)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 4_000*money.Dollar {
		t.Errorf("required %v, target %v; want $4,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for")
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{1_000, 2_000, 3_000, 4_000} {
		if got := sched.Value()[i].NeededBy(); got != want*money.Dollar {
			t.Errorf("installment %d needs %v, want $%d", i+1, got, want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if !pen.Value().UpperBound() || pen.Value().Total() != 179*money.Dollar {
		t.Errorf("penalty %v, upper bound %v; want $179 as an upper bound", pen.Value().Total(), pen.Value().UpperBound())
	}

	for name, p := range map[string]*estimated.PriorReturn{"no return on record": nil, "a zero last year": prior(0, 12), "not required to file": notRequired()} {
		h := household{noTax: true, prior: p, extras: usResident(true)}
		res := must(rules(t).Requirement(h.facts(t)))
		if res.Value().TargetKnown() || res.Value().Exempt() {
			t.Errorf("%s: target known %v, exempt %v", name, res.Value().TargetKnown(), res.Value().Exempt())
		}
		if _, err := rules(t).Schedule(h.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
			t.Errorf("%s: schedule %v, want ErrNeedsThisYearTax", name, err)
		}
		if _, err := rules(t).Penalty(h.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
			t.Errorf("%s: penalty %v, want ErrNeedsThisYearTax", name, err)
		}
	}
}

// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
// never as a negative amount.
func TestEstimatedWithholdingOverTheTaxReadsAsARefund(t *testing.T) {
	figures := func(tax money.USCents) estimated.Figures {
		return must(estimated.NewFigures().Tax(tax).FederalAGI(60_000 * money.Dollar).StateAGI(60_000 * money.Dollar).Build())
	}
	last := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800 * money.Dollar)).Build())
	b := estimated.NewFacts("WV", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
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
