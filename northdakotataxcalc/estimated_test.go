// North Dakota's estimated tax rules for 2026, checked against the Office of
// State Tax Commissioner's own forms and the Century Code.
//
// North Dakota publishes no worked interest example. Schedule ND-1UT defines
// the line arithmetic, and these tests follow it: each quarter's
// underpayment, the days late ÷ 365 rounded to three places, × 12% rounded to
// three places, × the underpayment. Every threshold, rate, date and
// percentage is compared against its extract in testdata/. The cases are
// hand-written.
package northdakotataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/northdakotataxcalc"
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
	return must(northdakotataxcalc.EstimatedTax(2026))
}

func because(rs []working.Required, k working.PositionKind, phrase string) bool {
	for _, r := range rs {
		if r.Position() == k && r.Asked() && (strings.Contains(r.Question(), phrase) || strings.Contains(r.Effect(), phrase)) {
			return true
		}
	}
	return false
}

func applied[T any](res working.Result[T]) string {
	var s string
	for _, r := range res.Applied() {
		s += r.Applied() + "\n"
	}
	return s
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

func residence(state string, from, to estimated.Date) estimated.ResidencePeriod {
	return must(estimated.NewResidencePeriod(state).From(from).To(to).Build())
}

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

type household struct {
	tax            money.USCents
	noTax          bool
	withheld       money.USCents
	prior          *estimated.PriorReturn
	noPrior        bool
	payments       []estimated.Payment
	extras         *northdakotataxcalc.EstimatedFacts
	returnFiled    estimated.Date
	elections      *estimated.Elections
	annualised     []money.USCents
	farmShare      money.Rate
	residence      []estimated.ResidencePeriod
	priorResidence []estimated.ResidencePeriod
	waiver         *estimated.WaiverKind
	positions      *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts("ND", 2026).FilingStatus(filing.Single).ThisYear(figures).
		Paid(h.payments...).ReturnFiled(h.returnFiled).FarmShare(h.farmShare).
		Residence(h.residence...).PriorResidence(h.priorResidence...)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	switch {
	case h.prior != nil:
		b.PriorYear(*h.prior)
	case !h.noPrior:
		b.PriorYear(*prior(20_000 * money.Dollar))
	}
	if h.extras != nil {
		b.State(*h.extras)
	}
	if h.elections != nil {
		b.Elections(*h.elections)
	}
	if h.waiver != nil {
		b.Positions(must(estimated.NewPositions().Waiver(*h.waiver).Build()))
	}
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		b.Elections(must(estimated.NewElections().Annualised().Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

func prior(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).Build())
	return &p
}

func extras(b *northdakotataxcalc.EstimatedFactsBuilder) *northdakotataxcalc.EstimatedFacts {
	e := must(b.Build())
	return &e
}

func penalty(t *testing.T, h household) working.Result[estimated.Penalty] {
	t.Helper()
	return must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
}

// $8,000 of tax and nothing paid: 90% is $7,200, $1,800 a quarter.
var base = household{tax: 8_000 * money.Dollar}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "ndcc-57-38.txt",
		"An individual, estate, or trust that is subject to section 6654 of the Internal Revenue Code relating to a failure to pay federal estimated income tax shall",
		"whose net tax liability for the preceding taxable year was less than one thousand dollars is not required to pay estimated tax for the current taxable year.",
		"equals ninety percent of the taxpayer's current taxable year's net tax liability.",
		"equals one hundred percent of the taxpayer's net tax liability for the immediately preceding taxable year.",
		"an individual who moved into this state during the immediately preceding taxable year",
		"interest accrues on a per annum basis from the due date of an installment to the fifteenth day of the fourth month following the end of the current taxable year",
		"no interest is due if the estimated tax of an individual, estate, or trust is less than one thousand dollars per income tax return filed.",
		"A taxpayer shall pay no less than one-quarter of the estimated tax to the tax commissioner on April fifteenth, June fifteenth, and September fifteenth of the taxable year, and January fifteenth of the following taxable year;",
		"interest on the tax at the rate of twelve percent per annum",
		"The tax commissioner may for good cause shown waive all or any part of any civil penalty or interest",
	)
	mustContain(t, "nd-1es-2026.txt",
		"You are required to pay estimated federal income tax for the 2026 tax year.",
		"Your net tax liability for the 2025 tax year was equal to or more than $1,000.",
		"net tax liability for 2025 is zero for this purpose.",
		"an amount equal to or more than $1,000 for the 2026 tax year.",
		"a. 90% of your net tax liability for the 2026 tax year.",
		"b. 100% of your net tax liability for the 2025 tax year.",
		"April 15, 2026 June 15, 2026 September 15, 2026 January 15, 2027",
		"one-fourth (25%) of the total estimated tax",
		"If any installment due date falls on a Saturday, Sunday, or legal holiday, the installment is not due until the next business day.",
		"Interest is calculated at the rate of 12% per year from the installment due date to the earlier of the date the estimated tax is paid or the regular due date of the return (generally, April 15, 2027).",
		"If the amount on this line is less than $1,000, stop here; you do not have to pay estimated tax",
		"you must pay the required estimated tax in equal payments over the remaining installment due dates following the receipt of income from North Dakota sources.",
	)
	mustContain(t, "nd-1ut-2025.txt",
		"6. Required payment. Enter smaller of line 2 or line 5. However, if you moved into North Dakota during the 2024 tax year, enter the amount from line 2",
		"7. Enter 25% of line 6 in each column",
		"paid the remaining federal income tax due by February 1, 2026.",
		"If Item C applies, enter 25% of the amount on line 6 in the 1st, 2nd and 3rd columns. Enter zero in the 4th quarter column.",
		"did not receive income subject to North Dakota income tax until after April 15, 2025.",
		"If you received the income after March 31 and before June 1, 2025, enter onethird of the amount on line 6 in the 2nd, 3rd, and 4th quarter columns.",
		"If you received the income after May 31 and before September 1, 2025, enter one-half of the amount on line 6 in the 3rd and 4th quarter columns.",
		"If you received the income after August 31, 2025, and before January 1, 2026, enter the amount from line 6 in the 4th quarter column.",
		"This exception applies only if you used the annualized income installment method",
		"Unless you can show otherwise, one-fourth of the total amount withheld is deemed to have been withheld by each payment due date.",
		"Interest of 12% per year is charged on an underpayment or late payment of estimated tax.",
		"the amount paid for a quarter must be applied first to the amount due for that quarter.",
		"the excess must be applied to offset an outstanding underpayment, if any, from a previous quarter, starting with the earliest underpayment.",
		"the overpayment is considered to have been applied as of the payment due date for the quarter in which the overpayment occurred.",
		"c. Divide line 16b by 365 and round to three decimal places",
		"d. Multiply line 16c by 12% (.12) and round to three decimal places",
		"is $5.00 or more, the interest due on Schedule",
	)
	mustContain(t, "f2210-2025-schedule-ai.txt", "1/1/25–3/31/25 1/1/25–5/31/25 1/1/25–8/31/25 1/1/25–12/31/25", "20 22.5% 45% 67.5% 90%")
	mustContain(t, "ndcc-1-03.txt",
		"that falls upon a Saturday or a holiday, the act may be performed upon the next business day with the same effect as if it had been performed upon the day appointed.")
	mustContain(t, "booklet-2025-estimated-tax.txt",
		"If you are not required to file a North Dakota return for 2025, you do not have to pay estimated tax for 2026.")
	mustContain(t, "ndac-81-03-04-02.txt",
		"utilizes the annualized income installment method for federal purposes as provided in section 6654",
		"derives over two-thirds of gross income from farming")
	mustContain(t, "nd-disaster-extensions.txt", "North Dakota automatically recognizes the federal extension for North Dakota tax purposes.")
}

// easter is the Gregorian Easter Sunday, for Good Friday.
func easter(y int) estimated.Date {
	a, b, c := y%19, y/100, y%100
	d, e := b/4, b%4
	g := (8*b + 13) / 25
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	return day(y, time.Month(month), (h+l-7*m+114)%31+1)
}

func nthWeekday(y int, m time.Month, wd time.Weekday, n int) estimated.Date {
	d := day(y, m, 1)
	for d.Weekday() != wd {
		d = d.AddDays(1)
	}
	return d.AddDays(7 * (n - 1))
}

// The § 1-03-01 holidays from April 2026 to April 2027. None is a due date,
// and nor is April 15, 2027, so a weekends-only calendar moves nothing a
// full one would.
func TestEstimatedNoDateIsANorthDakotaHoliday(t *testing.T) {
	mustContain(t, "ndcc-1-03.txt", "1. Every Sunday.", "The Friday next preceding Easter Sunday and commonly known as Good Friday.",
		"The third Monday of January, which is Martin Luther King Day", "The third Monday in February", "The last Monday in May", "The first Monday in September",
		"The fourth Thursday in November", "If any of the holidays enumerated in section 1-03-02 fall on a Saturday, the Friday immediately before shall be the holiday.")
	if easter(2026) != day(2026, 4, 5) || easter(2027) != day(2027, 3, 28) {
		t.Fatalf("Easter %v, %v", easter(2026), easter(2027))
	}
	var holidays []estimated.Date
	for _, y := range []int{2026, 2027} {
		lastMay := day(y, 5, 31)
		for lastMay.Weekday() != time.Monday {
			lastMay = lastMay.AddDays(-1)
		}
		holidays = append(holidays, day(y, 1, 1), nthWeekday(y, 1, time.Monday, 3), nthWeekday(y, 2, time.Monday, 3), easter(y).AddDays(-2), lastMay,
			day(y, 7, 4), day(y, 7, 3), nthWeekday(y, 9, time.Monday, 1), day(y, 11, 11), nthWeekday(y, 11, time.Thursday, 4), day(y, 12, 25))
	}
	s := must(rules(t).Schedule(base.facts(t))).Value()
	dates := []estimated.Date{day(2027, 4, 15)}
	for i, in := range s {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		dates = append(dates, in.Due())
	}
	for _, d := range dates {
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("%v is a %v", d, wd)
		}
		for _, h := range holidays {
			if d == h {
				t.Errorf("%v is a North Dakota holiday", d)
			}
		}
	}
}

// A quarter of $7,200 by each date.
func TestEstimatedScheduleForTwentyTwentySix(t *testing.T) {
	s := must(rules(t).Schedule(base.facts(t))).Value()
	for i, in := range s {
		if want := money.USCents(i+1) * 1_800 * money.Dollar; in.NeededBy() != want || in.Shortfall() != 1_800*money.Dollar || in.Status() != estimated.StatusShort {
			t.Errorf("installment %d needs %v short %v (%v), want %v short $1,800", i+1, in.NeededBy(), in.Shortfall(), in.Status(), want)
		}
	}
}

// Nothing paid on $7,200: $1,800 a quarter at 12% to April 15, 2027. The
// days are 365, 304, 212 and 90; ÷ 365 they round to 1.000, .833, .581 and
// .247, and × 12% to .120, .100, .070 and .030. That is $216, $180, $126 and
// $54, $576 in all, where the unrounded fraction gives $574.62. The 12% is
// fixed, so a span across December 31 is one part at one rate.
func TestEstimatedInterestArithmetic(t *testing.T) {
	res := penalty(t, base)
	wantDays := []int{365, 304, 212, 90}
	wantCharge := []money.USCents{21_600, 18_000, 12_600, 5_400}
	wantFactor := []string{"= 1.000, × 12% = 0.120", "= 0.833, × 12% = 0.100", "= 0.581, × 12% = 0.070", "= 0.247, × 12% = 0.030"}
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines", len(lines))
	}
	for i, w := range wantDays {
		parts := lines[i].Parts()
		if len(parts) != 1 || parts[0].Days() != w || parts[0].Charge() != wantCharge[i] || parts[0].Rate() != money.Percent(12) || !strings.Contains(parts[0].Source(), wantFactor[i]) {
			t.Errorf("installment %d: %v; want one part of %d days, %v at 12%% (%s)", i+1, parts, w, wantCharge[i], wantFactor[i])
		}
	}
	if res.Value().Total() != 57_600 {
		t.Errorf("interest %v; want $576.00", res.Value().Total())
	}
}

// A day fraction of .004 or less rounds to a factor of zero: January's
// $1,800 one day late is .003 and nothing, two days late .005 and .001,
// $1.80.
func TestEstimatedRoundingToThreePlaces(t *testing.T) {
	for _, tc := range []struct {
		paid estimated.Date
		want money.USCents
	}{{day(2027, 1, 16), 0}, {day(2027, 1, 17), 180}} {
		h := onTimeButJanuary(tc.paid)
		if res := penalty(t, h); res.Value().Total() != tc.want {
			t.Errorf("paid %v: %v, want %v", tc.paid, res.Value().Total(), tc.want)
		}
	}
}

// onTimeButJanuary pays the first three quarters on their dates and
// January's on paid.
func onTimeButJanuary(paid estimated.Date) household {
	h := base
	h.payments = []estimated.Payment{pay(dues[0], 1_800*money.Dollar), pay(dues[1], 1_800*money.Dollar), pay(dues[2], 1_800*money.Dollar), pay(paid, 1_800*money.Dollar)}
	return h
}

// Schedule ND-1UT applies a payment to its own quarter first. $3,600 paid on
// May 1 counts in June's column: June is met, and the $1,800 left pays
// April's shortfall as of June 15, 61 days, .167 and .020, $36. The federal
// order would stop April's on May 1. A payment between due dates while an
// earlier quarter is short is the open reading.
func TestEstimatedPaymentPaysItsOwnQuarterFirst(t *testing.T) {
	h := base
	h.payments = []estimated.Payment{pay(day(2026, 5, 1), 3_600*money.Dollar)}
	res := penalty(t, h)
	lines := res.Value().Lines()
	if len(lines) != 3 || lines[0].Installment() != 1 || lines[0].Days() != 61 || lines[0].Total() != 3_600 || lines[1].Installment() != 3 {
		t.Errorf("lines %v", lines)
	}
	if res.Value().Total() != 3_600+12_600+5_400 || !because(res.Required(), working.PositionUnsettledRule, "between due dates") {
		t.Errorf("interest %v; want $216.00 and the reading asked", res.Value().Total())
	}

	// $1,800 on June 15 pays June, and April runs the whole year.
	h.payments = []estimated.Payment{pay(dues[1], 1_800*money.Dollar)}
	res = penalty(t, h)
	if l := res.Value().Lines(); l[0].Installment() != 1 || l[0].Total() != 21_600 || l[1].Installment() != 3 || because(res.Required(), working.PositionUnsettledRule, "between due dates") {
		t.Errorf("June 15: %v", l)
	}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	// June's own column is paid, but by June 15 less than half the year's
	// payment has been credited, so June is short with no shortfall of its own.
	if s[0].Status() != estimated.StatusShort || s[1].Status() != estimated.StatusShort || s[1].Shortfall() != 0 {
		t.Errorf("June 15: %v %v, June's shortfall %v", s[0].Status(), s[1].Status(), s[1].Shortfall())
	}
}

// $2,700 in June's column pays June and half of April: April's shortfall is
// $900 to June 15, $18, and $900 to April 15, $108.
func TestEstimatedPartOfAShortfallOffset(t *testing.T) {
	h := base
	h.payments = []estimated.Payment{pay(day(2026, 6, 1), 2_700*money.Dollar)}
	res := penalty(t, h)
	parts := res.Value().Lines()[0].Parts()
	if len(parts) != 2 || parts[0].Principal() != 900*money.Dollar || parts[0].Charge() != 1_800 || parts[1].Principal() != 900*money.Dollar || parts[1].Charge() != 10_800 {
		t.Errorf("April: %v", parts)
	}
}

// After January 15 a payment pays the earliest shortfall on its own date:
// $1,800 on February 15 stops April's after 306 days, .838 and .101,
// $181.80.
func TestEstimatedPaymentAfterTheLastDueDate(t *testing.T) {
	h := base
	h.payments = []estimated.Payment{pay(day(2027, 2, 15), 1_800*money.Dollar)}
	res := penalty(t, h)
	if l := res.Value().Lines()[0]; l.Days() != 306 || l.Total() != 18_180 {
		t.Errorf("April: %d days, %v; want 306 and $181.80", l.Days(), l.Total())
	}
	if res.Value().Total() != 18_180+18_000+12_600+5_400 {
		t.Errorf("interest %v", res.Value().Total())
	}
}

// The interest stops on April 15, 2027, or on the day the return is filed and
// paid.
func TestEstimatedInterestStops(t *testing.T) {
	if p := must(rules(t).Penalty(base.facts(t), day(2027, 12, 31))).Value(); p.AsOf() != day(2027, 4, 15) || p.Total() != 57_600 {
		t.Errorf("figured to December: %v, %v", p.AsOf(), p.Total())
	}
	h := base
	h.returnFiled = day(2027, 3, 1)
	if p := penalty(t, h).Value(); p.AsOf() != day(2027, 3, 1) || p.Lines()[3].Days() != 45 {
		t.Errorf("filed March 1: %v", p.AsOf())
	}
}

// This year's tax less withholding under $1,000 owes nothing.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{{700_001, true}, {700_000, false}} {
		if res := must(rules(t).Requirement(household{tax: 8_000 * money.Dollar, withheld: tc.withheld}.facts(t))); res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v", tc.withheld, res.Value().Exempt(), tc.exempt)
		}
	}
}

// Last year's net tax liability under $1,000 means no estimates; a filer not
// required to file counts as zero; no return on record is asked.
func TestEstimatedPriorYearGate(t *testing.T) {
	for _, tc := range []struct {
		tax    money.USCents
		exempt bool
	}{{99_999, true}, {100_000, false}} {
		res := must(rules(t).Requirement(household{tax: 8_000 * money.Dollar, prior: prior(tc.tax)}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("last year %v: exempt %v, want %v", tc.tax, res.Value().Exempt(), tc.exempt)
		}
		// The exception is the chosen option, at $0.
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Last year's North Dakota tax under $1,000") {
			t.Errorf("last year %v: chose %v %q at %v", tc.tax, c.Basis(), c.Label(), c.Amount())
		}
	}
	nr := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	if res := must(rules(t).Requirement(household{tax: 8_000 * money.Dollar, prior: &nr}.facts(t))); !res.Value().Exempt() {
		t.Errorf("not required to file: %s", res.Value().Why())
	}
	res := must(rules(t).Requirement(household{tax: 8_000 * money.Dollar, noPrior: true}.facts(t)))
	if res.Value().Exempt() || res.Value().Options()[1].Available() || !because(res.Required(), working.PositionPriorYearReturn, "Under $1,000, no estimates are needed") {
		t.Errorf("no prior return: %s", res.Value().Why())
	}
	if p := penalty(t, household{tax: 8_000 * money.Dollar, prior: &nr}); p.Value().Total() != 0 {
		t.Errorf("exempt but charged %v", p.Value().Total())
	}
}

// Only a filer who must pay federal estimates owes North Dakota's.
func TestEstimatedFederalCondition(t *testing.T) {
	h := base
	h.extras = extras(northdakotataxcalc.NewEstimatedFacts().FederalEstimatesRequired(false))
	if res := must(rules(t).Requirement(h.facts(t))); !res.Value().Exempt() {
		t.Errorf("no federal requirement: %s", res.Value().Why())
	}
	res := must(rules(t).Requirement(base.facts(t)))
	if res.Value().Exempt() || !because(res.Required(), working.PositionJurisdictionFact, "pay federal estimated tax") {
		t.Error("federal requirement not given: not asked")
	}
	h.extras = extras(northdakotataxcalc.NewEstimatedFacts().FederalEstimatesRequired(true))
	if res := must(rules(t).Requirement(h.facts(t))); res.Value().Exempt() || because(res.Required(), working.PositionJurisdictionFact, "pay federal estimated tax") {
		t.Error("federal requirement given: still asked")
	}
}

// The lesser of 90% of this year's tax and 100% of last year's, with no
// twelve-month test; a filer who moved in last year has only the 90% test.
func TestEstimatedTargetOptions(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 8_000 * money.Dollar, prior: prior(7_199 * money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || c.Percent() != money.Percent(100) || res.Value().Target() != 7_199*money.Dollar {
		t.Errorf("chose %v for %v; want 100%% of last year's $7,199", c.Basis(), res.Value().Target())
	}
	res = must(rules(t).Requirement(household{tax: 8_000 * money.Dollar, prior: prior(7_201 * money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || c.Percent() != money.Percent(90) || res.Value().Target() != 7_200*money.Dollar {
		t.Errorf("chose %v for %v; want 90%%, $7,200", c.Basis(), res.Value().Target())
	}
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(must(estimated.NewFigures().Tax(5_000 * money.Dollar).Build())).Build())
	if res := must(rules(t).Requirement(household{tax: 8_000 * money.Dollar, prior: &short}.facts(t))); res.Value().Target() != 5_000*money.Dollar {
		t.Errorf("six-month return: target %v, want $5,000", res.Value().Target())
	}
	notFiled := must(estimated.NewPriorReturn().Filed(false).Figures(must(estimated.NewFigures().Tax(5_000 * money.Dollar).Build())).Build())
	if res := must(rules(t).Requirement(household{tax: 8_000 * money.Dollar, prior: &notFiled}.facts(t))); res.Value().Options()[1].Available() {
		t.Error("not filed: the prior-year test is offered")
	}
	mover := household{tax: 8_000 * money.Dollar, prior: prior(5_000 * money.Dollar),
		priorResidence: []estimated.ResidencePeriod{residence("MN", day(2025, 1, 1), day(2025, 6, 30)), residence("ND", day(2025, 7, 1), day(2025, 12, 31))}}
	if res := must(rules(t).Requirement(mover.facts(t))); res.Value().Options()[1].Available() || res.Value().Target() != 7_200*money.Dollar ||
		res.Value().Options()[1].WhyNot() != "You moved into North Dakota during 2025" {
		t.Errorf("moved in last year: target %v, %q", res.Value().Target(), res.Value().Options()[1].WhyNot())
	}
	stayed := mover
	stayed.priorResidence = []estimated.ResidencePeriod{residence("ND", day(2025, 1, 1), day(2025, 12, 31))}
	if res := must(rules(t).Requirement(stayed.facts(t))); res.Value().Target() != 5_000*money.Dollar {
		t.Errorf("resident all year: target %v", res.Value().Target())
	}
	unsure := must(estimated.NewPriorReturn().Filed(true).FullYearResident(false).Figures(must(estimated.NewFigures().Tax(5_000 * money.Dollar).Build())).Build())
	res = must(rules(t).Requirement(household{tax: 8_000 * money.Dollar, prior: &unsure}.facts(t)))
	if res.Value().Target() != 5_000*money.Dollar || !because(res.Required(), working.PositionResidency, "If you moved in that year") {
		t.Errorf("part-year last year with no dates: target %v", res.Value().Target())
	}
}

// Withholding reaching the target needs no payments; it counts in equal
// parts unless the filer shows the dates.
func TestEstimatedWithholding(t *testing.T) {
	if res := must(rules(t).Requirement(household{tax: 12_000 * money.Dollar, withheld: 10_800 * money.Dollar}.facts(t))); res.Value().Required() || res.Value().Exempt() {
		t.Errorf("withholding reaches 90%%: %s", res.Value().Why())
	}
	if p := penalty(t, household{tax: 12_000 * money.Dollar, withheld: 10_800 * money.Dollar}); p.Value().Total() != 0 {
		t.Errorf("spread evenly: interest %v", p.Value().Total())
	}
	s := must(rules(t).Schedule(household{tax: 12_000 * money.Dollar, withheld: 10_800 * money.Dollar}.facts(t))).Value()
	for i, in := range s {
		if want := money.USCents(i+1) * 2_700 * money.Dollar; in.CreditedBy() != want {
			t.Errorf("by installment %d credited %v, want a quarter each, %v", i+1, in.CreditedBy(), want)
		}
	}
	asWithheld := must(estimated.NewElections().WithholdingAsWithheld().Build())
	if p := penalty(t, household{tax: 12_000 * money.Dollar, withheld: 10_800 * money.Dollar, elections: &asWithheld}); p.Value().Total() == 0 {
		t.Error("on its December date: no interest")
	}
}

// Item C: the federal January installment excused zeroes North Dakota's.
func TestEstimatedFederalFourthQuarterException(t *testing.T) {
	h := base
	h.extras = extras(northdakotataxcalc.NewEstimatedFacts().FederalFourthQuarterExcused(true))
	s := must(rules(t).Schedule(h.facts(t))).Value()
	if s[3].Status() != estimated.StatusExcused || s[3].NeededBy() != 5_400*money.Dollar {
		t.Errorf("January: %v, needs %v", s[3].Status(), s[3].NeededBy())
	}
	if p := penalty(t, h); p.Value().Total() != 21_600+18_000+12_600 {
		t.Errorf("interest %v; want $522.00", p.Value().Total())
	}
	for _, tc := range []struct {
		filed estimated.Date
		asked bool
	}{{day(2027, 2, 1), true}, {day(2027, 2, 2), false}} {
		h := base
		h.returnFiled = tc.filed
		if got := because(must(rules(t).Requirement(h.facts(t))).Required(), working.PositionJurisdictionFact, "nothing is due on your North Dakota January payment"); got != tc.asked {
			t.Errorf("filed %v: asked %v, want %v", tc.filed, got, tc.asked)
		}
	}
}

// Item D: a nonresident or new resident whose North Dakota income starts
// after April 15 pays thirds, halves or all in January.
func TestEstimatedPartYearException(t *testing.T) {
	nonresident := []estimated.ResidencePeriod{residence("MN", day(2026, 1, 1), day(2026, 12, 31))}
	newcomer := []estimated.ResidencePeriod{residence("MN", day(2026, 1, 1), day(2026, 4, 30)), residence("ND", day(2026, 5, 1), day(2026, 12, 31))}
	for _, tc := range []struct {
		first estimated.Date
		want  []money.USCents
	}{
		{day(2026, 4, 15), []money.USCents{1_800, 3_600, 5_400, 7_200}},
		{day(2026, 4, 16), []money.USCents{0, 2_400, 4_800, 7_200}},
		{day(2026, 5, 31), []money.USCents{0, 2_400, 4_800, 7_200}},
		{day(2026, 6, 1), []money.USCents{0, 0, 3_600, 7_200}},
		{day(2026, 8, 31), []money.USCents{0, 0, 3_600, 7_200}},
		{day(2026, 9, 1), []money.USCents{0, 0, 0, 7_200}},
	} {
		for name, res := range map[string][]estimated.ResidencePeriod{"nonresident": nonresident, "moved in": newcomer} {
			h := base
			h.residence = res
			h.extras = extras(northdakotataxcalc.NewEstimatedFacts().FirstNorthDakotaIncome(tc.first))
			s := must(rules(t).Schedule(h.facts(t))).Value()
			for i, w := range tc.want {
				if s[i].NeededBy() != w*money.Dollar {
					t.Errorf("%s, first income %v: installment %d needs %v, want $%d", name, tc.first, i+1, s[i].NeededBy(), w)
				}
			}
		}
	}
	h := base
	h.residence = nonresident
	if !because(must(rules(t).Requirement(h.facts(t))).Required(), working.PositionRequirementFirstMet, "first receive North Dakota income") {
		t.Error("nonresident with no first-income date: not asked")
	}
	h = base
	h.residence = []estimated.ResidencePeriod{residence("ND", day(2026, 1, 1), day(2026, 6, 30)), residence("MN", day(2026, 7, 1), day(2026, 12, 31))}
	h.extras = extras(northdakotataxcalc.NewEstimatedFacts().FirstNorthDakotaIncome(day(2026, 9, 1)))
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].NeededBy() != 1_800*money.Dollar {
		t.Error("moved out: Item D applied")
	}
	h = base
	h.extras = extras(northdakotataxcalc.NewEstimatedFacts().FirstNorthDakotaIncome(day(2026, 9, 1)))
	res := must(rules(t).Schedule(h.facts(t)))
	if res.Value()[0].NeededBy() != 1_800*money.Dollar || !because(res.Required(), working.PositionResidency, "fewer, larger installments") {
		t.Error("residence not given: Item D applied or not asked")
	}
}

// The annualized method only if it was used federally: 22.5%, 45%, 67.5% and
// 90%, with recapture.
func TestEstimatedAnnualizedMethod(t *testing.T) {
	h := base
	h.annualised = []money.USCents{2_000 * money.Dollar, 4_000 * money.Dollar, 8_000 * money.Dollar, 8_000 * money.Dollar}
	res := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{450 * money.Dollar, 1_800 * money.Dollar, 5_400 * money.Dollar, 7_200 * money.Dollar} {
		if res.Value()[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, res.Value()[i].NeededBy(), want)
		}
	}
	if !because(res.Required(), working.PositionJurisdictionFact, "Form 2210") {
		t.Error("federal use not given: not asked")
	}
	h.extras = extras(northdakotataxcalc.NewEstimatedFacts().AnnualisedFederally(false))
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].NeededBy() != 1_800*money.Dollar {
		t.Errorf("not used federally: April needs %v", s[0].NeededBy())
	}
	h.extras = extras(northdakotataxcalc.NewEstimatedFacts().AnnualisedFederally(true))
	if res := must(rules(t).Schedule(h.facts(t))); res.Value()[0].NeededBy() != 450*money.Dollar || because(res.Required(), working.PositionJurisdictionFact, "Form 2210") {
		t.Error("used federally: not annualized, or still asked")
	}
}

// Without this year's tax, the target is last year's alone and the interest
// is the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(6_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	if r := res.Value(); !r.Required() || r.Target() != 6_000*money.Dollar || r.Options()[0].WhyNot() != estimated.NeedsThisYearTax || !because(res.Required(), working.PositionThisYearTax, "") {
		t.Errorf("required %v, target %v", r.Required(), r.Target())
	}
	if p := penalty(t, h); !p.Value().UpperBound() || p.Value().Total() == 0 {
		t.Errorf("interest %v, upper bound %v", p.Value().Total(), p.Value().UpperBound())
	}
	none := household{noTax: true, noPrior: true}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v", err)
	}
}

// The waivers North Dakota has, and the $5.00 rule.
func TestEstimatedWaiversAndSmallInterest(t *testing.T) {
	res := penalty(t, base)
	if !because(res.Required(), working.PositionPenaltyWaiver, "good cause") {
		t.Error("waiver not asked")
	}
	k := estimated.WaiverCasualty
	h := base
	h.waiver = &k
	res = penalty(t, h)
	if !strings.Contains(applied(res), "IRS disaster extension") || res.Value().Total() != 57_600 || res.Value().Waiver() != k {
		t.Error("casualty: interest not shown in full, or the disaster rule not stated")
	}
	// January's $1,800 fifteen days late is .041 and .005, $9.00; five
	// days late is .014 and .002, $3.60, under $5.00.
	res = penalty(t, onTimeButJanuary(day(2027, 1, 30)))
	if res.Value().Total() != 900 || strings.Contains(applied(res), "under $5.00 need not be sent") {
		t.Errorf("15 days late: %v; want $9.00 and the $5.00 rule not mentioned", res.Value().Total())
	}
	res = penalty(t, onTimeButJanuary(day(2027, 1, 20)))
	if res.Value().Total() != 360 || !strings.Contains(applied(res), "under $5.00 need not be sent") {
		t.Errorf("5 days late: %v; want $3.60 and the $5.00 rule", res.Value().Total())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(base.facts(t)))
	want := map[string][2]string{
		"A filer who moved into North Dakota last year":                                  {"income before the move", "§ 57-38-62(1)(b)(1)"},
		"The federal fourth-quarter exception":                                           {"enters zero for the fourth quarter", "§§ 57-38-62(1), 57-38-63"},
		"A nonresident or new resident with no North Dakota income until after April 15": {"June 1 to 15 or September 1 to 15", "§ 57-38-63"},
		"Which shortfall a payment pays":                                                 {"goes first to that quarter", "§ 57-38-62(3)"},
		"How the days are counted":                                                       {"rounded to three decimal places", "§ 57-38-62(3)"},
	}
	if len(res.Divergences()) != len(want) {
		t.Errorf("%d divergences, want %d", len(res.Divergences()), len(want))
	}
	// A full-year resident with no federal exception and no late start: only
	// the rounding and the payment order change these figures.
	for _, d := range res.Divergences() {
		switch d.Rule() {
		case "How the days are counted", "Which shortfall a payment pays":
		default:
			if d.AffectsHousehold() {
				t.Errorf("%q affects the base household", d.Rule())
			}
		}
	}
	h := base
	h.extras = extras(northdakotataxcalc.NewEstimatedFacts().FederalFourthQuarterExcused(true))
	for _, d := range must(rules(t).Requirement(h.facts(t))).Divergences() {
		if d.Rule() == "The federal fourth-quarter exception" && (!d.AffectsHousehold() || d.Uses() != working.ReadingForm) {
			t.Errorf("fourth quarter excused: %q affects %v, uses %v", d.Rule(), d.AffectsHousehold(), d.Uses())
		}
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
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := northdakotataxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	if _, err := rules(t).Requirement(household{tax: 8_000 * money.Dollar, farmShare: 7000}.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("farmer: %v, want ErrUnsupported", err)
	}
	this := must(estimated.NewFigures().Tax(100_000).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts("ND", 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts("ND", 2026).FilingStatus(filing.Single).ThisYear(this).State(7).Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(base.facts(t), estimated.Date{}); err == nil {
		t.Error("interest with no date is accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$1,000", "12%", "110%", "federal estimated tax", "three decimal places", "its own quarter", "April 15"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
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
	b := estimated.NewFacts("ND", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
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

// North Dakota has no casualty, retirement or disability waiver. The Tax
// Commissioner may waive interest for good cause shown, N.D.C.C.
// § 57-38-45(5), which names no causes, so a retirement the federal rules
// would waive is undecided.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "ndcc-57-38.txt", "The tax commissioner may for good cause shown waive all or any part of any civil")
	h := base
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
		{"retired in 2020", "undecided", "good cause shown", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(born)},
		{"disaster", "undecided", "follows IRS disaster extensions", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1))},
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
		if !because(res.Required(), working.PositionUnsettledRule, "good cause") {
			t.Errorf("%s: the open point is not reported", tc.name)
		}
	}
}
