// Oregon's estimated tax rules for 2026, checked against the Department of
// Revenue's own documents and Oregon's statutes and rules.
//
// Oregon publishes no exhaustive oracle for estimated tax. The strongest
// checks are two worked examples: Publication OR-ESTIMATE's Example 3, the
// required annual payment and installments for Allan and Louise, and the Form
// OR-10 instructions' running-balance example for Catelyn, whose interest is
// reproduced to the dollar with 2025's dates and factor. Every threshold,
// rate, date and holiday is compared against its extract in testdata/. The
// exceptions, the late start and the boundaries are hand-written cases.
package oregontaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/oregontaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

const or estimated.Jurisdiction = "OR"

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
	return must(oregontaxcalc.EstimatedTax(2026))
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
	tax         money.USCents // after all credits, Form OR-10 worksheet line 3
	withheld    money.USCents // withheld on December 15, spread by the rules
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *oregontaxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	annualised  []money.USCents // net annualised tax for each period
	noTax       bool
	farm, fish  money.Rate
	waiver      *estimated.WaiverKind
	residence   []estimated.ResidencePeriod
	gross       money.USCents
	asWithheld  bool
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	fb := estimated.NewFigures()
	if !h.noTax {
		fb.Tax(h.tax)
	}
	if h.gross > 0 {
		fb.GrossIncome(h.gross)
	}
	b := estimated.NewFacts(or, 2026).FilingStatus(filing.MarriedFilingJointly).ThisYear(must(fb.Build())).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled).
		FarmShare(h.farm).FishShare(h.fish).Residence(h.residence...)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(oregontaxcalc.NewEstimatedFacts().PriorReturnFiledFirst(true).GrossIncomeTestMet(true).Build()))
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

// prior is a filed 12-month return with its tax after credits.
func prior(tax money.USCents, fullYear bool) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(fullYear).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "or-estimate-2026.txt",
		"150-101-026 (Rev. 10-07-25)",
		"You must make estimated tax payments if you expect to owe tax of $1,000 or more after all credits and withholding.",
		"You estimate that your tax after withholding and credits (including refundable credits) will be $1,000 or more when you file your 2026 Oregon return",
		"• Ninety percent (90%) of the tax after all credits to be shown on your 2026 return; or",
		"• One hundred percent (100%) of the tax after all credits shown on your 2025 return (also known as “safe harbor”).",
		"To use the safe harbor provision, you’ll have to file your 2025 return before you file your 2026 return. Your 2025 return can’t be a short-year return, but it may be a part-year return.",
		"If you didn’t file a 2025 Oregon return, enter the amount from line 20a on",
		"or will be at least two-thirds of your estimated gross income for 2026, you don’t have to make Oregon estimated tax payments.",
		"You have the option to pay all of your 2026 estimated tax by the due date of the first installment or in four equal installments.",
		"If your payment due date falls on a Saturday, Sunday, or an Oregon legal holiday, the payment is due the next business day.",
		"Calendar-year filers First April 15, 2026 Second June 15, 2026 Third September 15, 2026 Fourth January 15, 2027",
		"After April 1 and before June 2 3 June 15, 2026 September 15, 2026 January 15, 2027 After June 1 and before September 2 2 September 15, 2026 January 15, 2027 After September 1 1 January 15, 2027",
		"All estimated tax payments are applied to unpaid installment periods before they’re applied to a later period.",
		"Underpayment interest is charged on each underpaid install- ment until the date the installment is paid or the due date of the return, whichever comes first.",
		"Unlike the federal system, estates and trusts are not required to make Oregon estimated tax payments.",
	)
	mustContain(t, "or40v-instructions.txt",
		"For calendar year 2026, the due dates for estimated tax payments are:",
		"• Quarter 1: April 15, 2026. • Quarter 2: June 15, 2026. • Quarter 3: September 15, 2026. • Quarter 4: January 15, 2027.",
	)
	mustContain(t, "or10-instructions-2025.txt",
		"150-101-031-1 (Rev. 9-16-25)",
		"5. 2025 tax after credits and withholding. Line 3 minus line 4",
		"If the amount on line 5 is less than $1,000, stop here! You don’t owe underpayment interest.",
		"6. Multiply line 3 by 90% (0.90)",
		"10. Required annual payment. Enter the smaller of line 6 or line 9",
		"Regular installments. Divide the amount on line 10 of the required annual payment worksheet by four.",
		"Withholding is treated as if it were paid in equal amounts on the due date for each required install- ment payment unless you can show that it was paid differ- ently.",
		"Underpayment interest is calculated on a running balance of estimated tax due from one event to the next.",
		"Interest accruing on unpaid tax after April 15, 2026 is computed separately.",
		"For 2026, the annual interest rate is 8 percent. Interest is figured daily, based on a 365-day year.",
		"15. 01/01/26 Rate change (if any) 0.000219",
		"One day has been entered for you on the withholding date.",
		"Round each amount to the nearest whole dollar.",
		"Jan 1 to Jan 1 to Jan 1 to Jan 1 to Mar 31 May 31 Aug 31 Dec 31",
		"23. Percentage that applies for each period ........................................23. 22.5% 45% 67.5% 90%",
		"Exception 1—Farmers and commercial fishermen. If your gross income from farming or fishing, including oys- ter farming, was at least 2/3 of the gross income",
		"• You were a full-year Oregon resident for tax year 2024; • Your 2024 Oregon tax after all credits, but before with- holding or other payments, was $0 or you weren’t required to file a 2024 Oregon return; and • Your tax year was a full 12 months.",
		"• During 2024 or 2025, you retired at age 62 or older or became disabled; and • There was reasonable cause",
		"Underpayment interest won’t be charged for one or more installment periods that were underpaid due to a casualty, disaster, or other unusual circumstance.",
	)
	mustContain(t, "ors316-estimated-tax.txt",
		"(b) The gross income for the taxable year can be reasonably expected to exceed: (A) $20,000 in the case of:",
		"(B) $10,000 in the case of a married individual",
		"(C) $5,000 in the case of a married individual not entitled under ORS 316.567 to file a joint declaration with a spouse.",
		"(1) After April 1 and before June 2 of the taxable year, the declaration shall be filed on or before June 15 of the taxable year;",
		"(2) After June 1 and before September 2 of the taxable year, the declaration shall be filed on or before September 15 of the taxable year;",
		"(3) After September 1 of the taxable year, the declaration shall be filed on or before January 15 of the succeeding year.",
		"the estimated tax shall be paid in four equal installments",
		"the estimated tax shall be paid in three equal installments",
		"the estimated tax shall be paid in two equal installments",
		"(a) The 15th day of the fourth month following the close of the taxable year;",
		"a payment of estimated tax shall be credited against unpaid required installments in the order in which such installments are required to be paid",
		"An equal part of the credit shall be considered paid on each installment date for the taxable year, unless the taxpayer establishes the date on which all amounts were actually withheld",
		"(a) Ninety percent of the tax shown on the return for the taxable year",
		"the percentage of the tax shown on the return filed by the individual for the preceding taxable year that is established by the Department of Revenue by rule",
		"(a) Interest accruing under subsection (1) of this section shall not be imposed if the individual was a resident of this state throughout the preceding taxable year and had no tax liability for that year, and the preceding taxable year was a taxable year of 12 months.",
		"is at least two-thirds of the total estimated gross income from all sources for the taxable year",
		"is less than the amount established by rule adopted under ORS 316.563 (2).",
	)
	mustContain(t, "oar150-316-estimated-tax.txt",
		"No declaration of estimated tax is required if the estimated tax as defined ORS 316.557 is less than $1,000.",
		"(A) Ninety percent of the tax shown on the return for the taxable year",
		"(B) One hundred percent of the tax shown on the prior year's return, if qualified.",
		"the prior year’s return must be filed before the current year’s return, and the prior tax year must consist of 12 months.",
		"(b) A part-year resident may use the prior year tax unless disqualified for a reason described in this section.",
		"(d) Estimated tax payments are not required if the amount of the required annual payment minus Oregon tax withheld is less than $1,000.",
		"(A) Regular Installment: The required installment payment for each period is 25 percent of the required annual payment.",
		"(i) 22.5% for the first period; (ii) 45% for the first and second periods; (iii) 67.5% for the first, second and third periods; and (iv) 90 % for the first through fourth periods.",
		"must be added to the required installment payment for the next succeeding period.",
		"the department will apply the overpayment as of the first estimated tax installment due date of the open estimated tax account (generally April 15)",
	)
	mustContain(t, "oar150-305-interest.txt",
		"For deficiencies and delinquencies owing on or after January 1, 2026, unless otherwise provided by law, every deficiency and delinquency arising under any law administered by the department will bear interest at the rate of eight percent annually.",
		"(b) The daily interest rate is calculated by dividing the annual interest rate by 365 without rounding.",
		"January 1, 2026 — 8 — N/A — 0.0219",
		"The comparison will be conducted in July and will use the rates charged by the Internal Revenue Service for the 3rd calendar quarter.",
	)
	mustContain(t, "ors305-220.txt",
		"Notice of intent to adopt adjusted interest rates shall be given in the manner provided in ORS 183.335, not less than three months before the proposed effective date of the adjusted rates.",
	)
	// The rates end with January 1, 2026: nothing is published for 2027.
	if strings.Contains(extract(t, "oar150-305-interest.txt"), "January 1, 2027") {
		t.Error("OAR 150-305-0140 now has a rate from January 1, 2027; add it to estimated_y2026.go")
	}
}

// No act of the 2026 session amended the estimated tax sections, the interest
// rate section or the holidays, so no form here predates the law for 2026.
func TestEstimatedRulesWereNotAmendedIn2026(t *testing.T) {
	mustContain(t, "orlaws-2026-sections-amended.txt",
		"ORS SECTIONS AMENDED, REPEALED OR “ADDED TO”",
		"316.012 A c.142 §38 (SB 1507)",
		"316.157 A c.142 §40 (SB 1507)")
	text := extract(t, "orlaws-2026-sections-amended.txt")
	for _, s := range []string{"316.557", "316.563", "316.573", "316.577", "316.579", "316.583", "316.587", "316.588", "316.589", "305.220", "187.010", "187.020"} {
		if strings.Contains(text, s) {
			t.Errorf("ORS %s is in the 2026 table of sections amended", s)
		}
	}
}

// The calendar is ORS 187.010's holidays. Each date is checked against the
// rule that sets it, so moving one breaks this test.
func TestEstimatedHolidaysFollowORS187(t *testing.T) {
	mustContain(t, "ors187-legal-holidays.txt",
		"(a) Each Sunday.",
		"(b) New Year’s Day on January 1.",
		"(c) Martin Luther King, Jr.’s Birthday on the third Monday in January.",
		"on the third Monday in February.",
		"(e) Memorial Day on the last Monday in May.",
		"(f) Juneteenth on June 19.",
		"(g) Independence Day on July 4.",
		"(h) Labor Day on the first Monday in September.",
		"(i) Veterans Day on November 11.",
		"(j) Thanksgiving Day on the fourth Thursday in November.",
		"(k) Christmas Day on December 25.",
		"Each time a holiday listed in subsection (1) of this section falls on Saturday, the preceding Friday shall be a legal holiday.",
		"(1) Every day appointed by the Governor as a holiday.",
	)
	nth := func(year int, month time.Month, wd time.Weekday, n int) estimated.Date {
		d := day(year, month, 1)
		for d.Weekday() != wd {
			d = d.AddDays(1)
		}
		return d.AddDays(7 * (n - 1))
	}
	lastMonday := func(year int) estimated.Date {
		d := day(year, 5, 31)
		for d.Weekday() != time.Monday {
			d = d.AddDays(-1)
		}
		return d
	}
	want := map[estimated.Date]string{
		day(2026, 1, 1):                 "New Year's Day",
		nth(2026, 1, time.Monday, 3):    "Martin Luther King, Jr.'s Birthday",
		nth(2026, 2, time.Monday, 3):    "Presidents Day",
		lastMonday(2026):                "Memorial Day",
		day(2026, 6, 19):                "Juneteenth",
		day(2026, 7, 4):                 "Independence Day",
		day(2026, 7, 4).AddDays(-1):     "Independence Day, observed on the Friday before",
		nth(2026, 9, time.Monday, 1):    "Labor Day",
		day(2026, 11, 11):               "Veterans Day",
		nth(2026, 11, time.Thursday, 4): "Thanksgiving Day",
		day(2026, 12, 25):               "Christmas Day",
		day(2027, 1, 1):                 "New Year's Day",
		nth(2027, 1, time.Monday, 3):    "Martin Luther King, Jr.'s Birthday",
		nth(2027, 2, time.Monday, 3):    "Presidents Day",
	}
	if day(2026, 7, 4).Weekday() != time.Saturday {
		t.Fatal("July 4, 2026 is not a Saturday; the observed holiday is wrong")
	}
	cal := must(oregontaxcalc.EstimatedCalendar(2026))
	got := cal.Holidays()
	if len(got) != len(want) {
		t.Errorf("%d holidays, want %d", len(got), len(want))
	}
	for _, h := range got {
		if name, ok := want[h.On()]; !ok || name != h.Name() {
			t.Errorf("%v %q is not an ORS 187.010 holiday in the 2026 window (want %q)", h.On(), h.Name(), name)
		}
	}

	// The calendar moves a date on a weekend or holiday to the next business
	// day. June 15, 2025 was a Sunday, and the 2025 instructions print June 16.
	mustContain(t, "or10-instructions-2025.txt", "Required installment payment due dates for tax year 2025: Calendar-year filers April 15, 2025 June 16, 2025 September 15, 2025 January 15, 2026")
	for _, tc := range []struct{ from, to estimated.Date }{
		{day(2025, 6, 15), day(2025, 6, 16)},   // Sunday
		{day(2026, 6, 19), day(2026, 6, 22)},   // Juneteenth, a Friday
		{day(2026, 7, 3), day(2026, 7, 6)},     // Independence Day observed, then the weekend
		{day(2027, 1, 16), day(2027, 1, 19)},   // Saturday, Sunday, then Martin Luther King, Jr.'s Birthday
		{day(2026, 11, 26), day(2026, 11, 27)}, // Thanksgiving
		{day(2026, 4, 15), day(2026, 4, 15)},   // a Wednesday stays
	} {
		if got := cal.NextBusinessDay(tc.from); got != tc.to {
			t.Errorf("%v moves to %v, want %v", tc.from, got, tc.to)
		}
	}
}

// The 2026 dates are the ones OR-ESTIMATE and OR-40-V print, and none moves.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	cal := must(oregontaxcalc.EstimatedCalendar(2026))
	res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t)))
	if len(res.Value()) != 4 {
		t.Fatalf("%d installments, want four", len(res.Value()))
	}
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if !cal.IsBusinessDay(in.Due()) {
			t.Errorf("installment %d falls on a weekend or holiday", i+1)
		}
	}
}

// Publication OR-ESTIMATE's Example 3: Allan and Louise owe $3,912 after
// credits and expect $721 of withholding. 90% is $3,521, so $2,800 of
// estimates, $700 a quarter. The publication rounds to dollars; this does
// not, so 90% is $3,520.80 and the estimates $2,799.80.
func TestEstimatedRequirementMatchesORESTIMATEExample(t *testing.T) {
	mustContain(t, "or-estimate-2026.txt",
		"Oregon tax after credits $ 3,912 Required annual payment: Tax after credits x 90% (0.90) $ 3,521 Tax to be withheld from Louise’s wages – $ 721 Estimated tax for 2026 $ 2,800",
		"or they can divide it into four equal installments of $700 each ($2,800 ÷ 4 = $700).")
	h := household{tax: 3_912 * money.Dollar, withheld: 721 * money.Dollar}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 352_080 {
		t.Fatalf("required %v, target %v; want $3,520.80: %s", r.Required(), r.Target(), r.Why())
	}
	if est := r.Target() - h.withheld; (est+50)/100*100 != 2_800*money.Dollar {
		t.Errorf("estimates of %v do not round to $2,800", est)
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, in := range sched.Value() {
		need := money.USCents(i+1) * 88_020
		if in.NeededBy() != need || in.CreditedBy() != money.USCents(i+1)*18_025 {
			t.Errorf("installment %d needs %v with %v credited; want %v and %v", i+1, in.NeededBy(), in.CreditedBy(), need, money.USCents(i+1)*18_025)
		}
	}
	// Paying the published $700 on each date leaves nothing short.
	h.payments = quarterly(700*money.Dollar, 700*money.Dollar, 700*money.Dollar, 700*money.Dollar)
	for i, in := range must(rules(t).Schedule(h.facts(t))).Value() {
		if in.Status() != estimated.StatusMet {
			t.Errorf("installment %d is %v after paying $700", i+1, in.Status())
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("interest of %v after paying $700 a quarter", p.Value().Total())
	}
}

// The Form OR-10 instructions' example: Catelyn owes $5,000 a period, has
// $3,000 withheld a period, and pays $1,200 on May 23, $1,300 on June 6,
// $2,500 on July 17 and $1,500 on August 14. With 2025's dates and 0.000247
// the worksheet gives $19, $3 and $11, $33 by September 15.
func TestEstimatedInterestMatchesOR10Example(t *testing.T) {
	mustContain(t, "or10-instructions-2025.txt",
		"Example 1: Catelyn’s required installment is $5,000 every period. Their total withholding is $12,000 for the year ($3,000 each period). Catelyn made estimated tax payments of $1,200",
		"on May 23, $1,300 on June 6, $2,500 on July 17, and $1,500 on August 14.",
		"2. 04/15 Required installment $5,000 $2,000 38 0.000247 $19",
		"3. 05/23 Payment $1,200 $800 14 0.000247 $3",
		"7. 06/16 Required installment $5,000 $1,500 31 0.000247 $11",
		"As of September 15, their total underpayment interest due is $33 ($19 + $3 + $11).")
	f2025 := must(estimated.ParseFactor("0.000247"))
	f2026 := must(estimated.ParseFactor("0.000219"))
	series := must(estimated.NewFactorSeries(estimated.RoundToDollar).
		Period(day(2025, 1, 1), day(2025, 12, 31), f2025, money.Percent(9), "Form OR-10 instructions (2025)").
		Period(day(2026, 1, 1), day(2026, 12, 31), f2026, money.Percent(8), "Form OR-10 instructions (2025)").
		Build())
	dues2025 := []estimated.Date{day(2025, 4, 15), day(2025, 6, 16), day(2025, 9, 15), day(2026, 1, 15)}
	five, three := 5_000*money.Dollar, 3_000*money.Dollar
	payments := []estimated.Payment{
		pay(day(2025, 5, 23), 1_200*money.Dollar), pay(day(2025, 6, 6), 1_300*money.Dollar),
		pay(day(2025, 7, 17), 2_500*money.Dollar), pay(day(2025, 8, 14), 1_500*money.Dollar),
	}
	parts, unrated, err := oregontaxcalc.WorksheetInterest(dues2025, []money.USCents{five, five, five, five}, []money.USCents{three, three, three, three},
		payments, day(2025, 9, 15), series)
	if err != nil || !unrated.IsZero() {
		t.Fatalf("worksheet: %v, unrated %v", err, unrated)
	}
	want := []struct {
		balance money.USCents
		days    int
		charge  money.USCents
	}{{2_000 * money.Dollar, 38, 19 * money.Dollar}, {800 * money.Dollar, 14, 3 * money.Dollar}, {1_500 * money.Dollar, 31, 11 * money.Dollar}}
	if len(parts) != len(want) {
		t.Fatalf("%d lines charged, want %d", len(parts), len(want))
	}
	var total money.USCents
	for i, w := range want {
		p := parts[i]
		if p.Principal() != w.balance || p.Days() != w.days || p.Charge() != w.charge {
			t.Errorf("line %d: %v for %d days is %v; want %v for %d days, %v", i+1, p.Principal(), p.Days(), p.Charge(), w.balance, w.days, w.charge)
		}
		total += p.Charge()
	}
	if total != 33*money.Dollar {
		t.Errorf("total %v, want $33", total)
	}

	// The same household under the 2026 rules: June 15 rather than June 16,
	// so 32 days for the June balance, at 0.000219. $16.64, $2.45 and $10.51
	// round to $17, $2 and $11.
	h := household{tax: 2_222_222, withheld: 12_000 * money.Dollar, payments: []estimated.Payment{
		pay(day(2026, 5, 23), 1_200*money.Dollar), pay(day(2026, 6, 6), 1_300*money.Dollar),
		pay(day(2026, 7, 17), 2_500*money.Dollar), pay(day(2026, 8, 14), 1_500*money.Dollar),
	}}
	if r := must(rules(t).Requirement(h.facts(t))).Value(); r.Target() != 20_000*money.Dollar {
		t.Fatalf("target %v, want $20,000", r.Target())
	}
	p := must(rules(t).Penalty(h.facts(t), day(2026, 9, 15))).Value()
	if p.Total() != 30*money.Dollar {
		t.Errorf("interest to September 15, 2026 is %v, want $30", p.Total())
	}
	lines := p.Lines()
	if len(lines) != 2 || len(lines[0].Parts()) != 2 || lines[1].Parts()[0].Days() != 32 || lines[1].Total() != 11*money.Dollar {
		t.Errorf("lines %+v; want April's $17 and $2 and June's $11 for 32 days", lines)
	}
}

// A balance carried past December 31, 2026 is charged to that day at 8% and
// stops: no rate is published from January 1, 2027. Each due date's
// withholding line charges its own day, so nothing paid on $10,000 a quarter
// gives $131 for April 16 to June 14, $2 for June 15, $399 for June 16 to
// September 14, $4 for September 15 and $703 for September 16 to December 31.
func TestEstimatedInterestStopsWhereTheRatesEnd(t *testing.T) {
	h := household{tax: 4_444_444} // 90% is $40,000
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	lines := p.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	want := [][]money.USCents{{131}, {2, 399}, {4, 703}, nil}
	for i, w := range want {
		parts := lines[i].Parts()
		if len(parts) != len(w) {
			t.Errorf("line %d has %d parts, want %d", i+1, len(parts), len(w))
			continue
		}
		for j, c := range w {
			if parts[j].Charge() != c*money.Dollar || parts[j].Rate() != money.Percent(8) {
				t.Errorf("line %d part %d charges %v at %v, want $%d at 8%%", i+1, j+1, parts[j].Charge(), parts[j].Rate(), c)
			}
		}
	}
	if last := lines[2].Parts()[1]; last.To() != day(2026, 12, 31) || last.Days() != 107 || last.Principal() != 30_000*money.Dollar {
		t.Errorf("September's balance runs to %v for %d days on %v; want December 31, 107 days, $30,000", last.To(), last.Days(), last.Principal())
	}
	if lines[3].Shortfall() != 40_000*money.Dollar {
		t.Errorf("January's balance is %v, want $40,000", lines[3].Shortfall())
	}
	if p.Value().Total() != 1_239*money.Dollar {
		t.Errorf("total %v, want $1,239", p.Value().Total())
	}
	if !has(p.Required(), working.PositionUnpublishedRate) {
		t.Error("the unpublished 2027 rate is not reported")
	}
	// To December 31 every day has a rate, and nothing is reported.
	if p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31))); has(p.Required(), working.PositionUnpublishedRate) || p.Value().Total() != 1_239*money.Dollar {
		t.Errorf("to December 31: %v, unpublished rate reported %v", p.Value().Total(), has(p.Required(), working.PositionUnpublishedRate))
	}
}

// The interest follows the printed 0.000219, not 8% ÷ 365. On $1,000,000 for
// 60 days the two give $13,140 and $13,151.
func TestEstimatedInterestUsesThePrintedDailyFactor(t *testing.T) {
	h := household{tax: 444_444_444} // 90% is $4,000,000, $1,000,000 a quarter
	p := must(rules(t).Penalty(h.facts(t), day(2026, 6, 15))).Value()
	parts := p.Lines()[0].Parts()
	if len(parts) != 1 || parts[0].Principal() != 1_000_000*money.Dollar || parts[0].Days() != 60 {
		t.Fatalf("parts %+v, want one on $1,000,000 for 60 days", parts)
	}
	if parts[0].Charge() != 13_140*money.Dollar {
		t.Errorf("60 days charge %v, want $13,140 at the printed factor ($13,151 from 8%% ÷ 365)", parts[0].Charge())
	}
}

// The $1,000 test is on tax after credits less withholding, and exactly
// $1,000 is charged.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{900_001, true},  // $999.99 left
		{900_000, false}, // $1,000.00 left
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld}.facts(t)))
		r := res.Value()
		if r.Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, r.Exempt(), tc.exempt, r.Why())
		}
		// The exception is the chosen option, at $0.
		if c, ok := r.Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding under $1,000") {
			t.Errorf("withheld %v: chose %v %q at %v", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
		// Only exactly $1,000 left turns on the "Exactly $1,000" reading.
		for _, d := range res.Divergences() {
			if d.Rule() == "Exactly $1,000" && d.AffectsHousehold() != !tc.exempt {
				t.Errorf("withheld %v: %q affects %v", tc.withheld, d.Rule(), d.AffectsHousehold())
			}
		}
	}
	// Whether payments are needed: withholding one cent short of the
	// required annual payment needs them; reaching it does not.
	for _, tc := range []struct {
		withheld money.USCents
		required bool
	}{{1_799_999, true}, {1_800_000, false}} {
		r := must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, withheld: tc.withheld}.facts(t))).Value()
		if r.Required() != tc.required || r.Exempt() {
			t.Errorf("withheld %v: required %v, want %v", tc.withheld, r.Required(), tc.required)
		}
	}
	// A shortfall under $1,000 after withholding is still charged, as Form
	// OR-10 charges it, though the estimate worksheet says no payments are
	// required.
	h := household{tax: 10_000 * money.Dollar, withheld: 8_500 * money.Dollar}
	r := must(rules(t).Requirement(h.facts(t))).Value()
	if !r.Required() || !strings.Contains(r.Why(), "worksheet") {
		t.Errorf("required %v: %s", r.Required(), r.Why())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() == 0 {
		t.Error("no interest on $500 short when tax less withholding is $1,500")
	}
}

// Oregon has no 110% tier: last year's tax is 100% at any income.
func TestEstimatedTargetOptions(t *testing.T) {
	for _, tc := range []struct {
		priorTax money.USCents
		basis    estimated.Basis
		target   money.USCents
	}{
		{15_000 * money.Dollar, estimated.BasisPriorYearTax, 15_000 * money.Dollar},
		{19_000 * money.Dollar, estimated.BasisThisYearTax, 18_000 * money.Dollar},
	} {
		h := household{tax: 20_000 * money.Dollar, prior: prior(tc.priorTax, true), gross: 2_000_000 * money.Dollar}
		r := must(rules(t).Requirement(h.facts(t))).Value()
		c, _ := r.Chosen()
		if c.Basis() != tc.basis || r.Target() != tc.target {
			t.Errorf("last year %v: chose %v for %v, want %v for %v", tc.priorTax, c.Basis(), r.Target(), tc.basis, tc.target)
		}
		if o := r.Options()[1]; o.Percent() != money.Percent(100) || o.Amount() != tc.priorTax {
			t.Errorf("last year's option is %v of %v, want 100%%", o.Percent(), o.Amount())
		}
	}

	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(9).Figures(must(estimated.NewFigures().Tax(100).Build())).Build())
	late := must(oregontaxcalc.NewEstimatedFacts().PriorReturnFiledFirst(false).GrossIncomeTestMet(true).Build())
	unsaid := must(oregontaxcalc.NewEstimatedFacts().GrossIncomeTestMet(true).Build())
	for name, tc := range map[string]struct {
		h         household
		available bool
		ask       working.PositionKind
	}{
		"no return on record":     {household{}, false, working.PositionPriorYearReturn},
		"not filed":               {household{prior: &notFiled}, false, working.PositionUnspecified},
		"short year":              {household{prior: &short}, false, working.PositionUnspecified},
		"filed after this year's": {household{prior: prior(100, true), extras: &late}, false, working.PositionUnspecified},
		"filing order not said":   {household{prior: prior(100, true), extras: &unsaid}, true, working.PositionJurisdictionFact},
		"part-year return":        {household{prior: prior(100, false)}, true, working.PositionUnspecified},
	} {
		tc.h.tax = 10_000 * money.Dollar
		res := must(rules(t).Requirement(tc.h.facts(t)))
		if got := res.Value().Options()[1].Available(); got != tc.available {
			t.Errorf("%s: the prior-year test is available %v, want %v", name, got, tc.available)
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
		if o := res.Value().Options()[1]; !o.Available() && o.Amount() != 0 {
			t.Errorf("%s: the unavailable prior-year option carries %v", name, o.Amount())
		}
	}
	for _, tc := range []struct {
		h    household
		want string
	}{
		{household{tax: 10_000 * money.Dollar}, "No 2025 Oregon return on record"},
		{household{tax: 10_000 * money.Dollar, prior: &notFiled}, "You didn't file a 2025 Oregon return"},
	} {
		want := tc.want
		if o := must(rules(t).Requirement(tc.h.facts(t))).Value().Options()[1]; o.Available() || o.Amount() != 0 || o.WhyNot() != want {
			t.Errorf("prior-year option: available %v, %v, %q; want none and %q", o.Available(), o.Amount(), o.WhyNot(), want)
		}
	}
}

// Without this year's tax, the target is last year's tax, and the interest
// figured against it is the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000*money.Dollar, true)}
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
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, in := range sched.Value() {
		if want := money.USCents(i+1) * 2_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if !pen.Value().UpperBound() || pen.Value().Total() == 0 {
		t.Errorf("unpaid: interest %v, upper bound %v", pen.Value().Total(), pen.Value().UpperBound())
	}

	// No return either: no target, and nothing to schedule.
	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() || r.Required() {
		t.Errorf("no tax and no return: target known %v, required %v", r.TargetKnown(), r.Required())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}

	// Exception 2 needs nothing about this year.
	zero := household{noTax: true, prior: prior(0, true)}
	if r := must(rules(t).Requirement(zero.facts(t))).Value(); !r.Exempt() {
		t.Errorf("no tax last year, this year unknown: %s", r.Why())
	}
}

// Exception 1: two-thirds of gross income from farming or fishing, together.
// OR-ESTIMATE's Felicia has $50,000 of $90,000 from fishing, under it.
func TestEstimatedFarmersAndFishers(t *testing.T) {
	mustContain(t, "or-estimate-2026.txt",
		"The gross profit from her Oregon fishing business in 2025 was $50,000.",
		"Her total gross income from all sources in 2025 was $90,000.",
		"she doesn’t qualify for the exception for farmers and fishermen.")
	for _, tc := range []struct {
		farm, fish money.Rate
		exempt     bool
	}{
		{6_667, 0, true},
		{6_666, 0, false},
		{0, 6_667, true},
		{3_334, 3_333, true},
		{0, 5_556, false}, // Felicia
	} {
		h := household{tax: 10_000 * money.Dollar, farm: tc.farm, fish: tc.fish}
		r := must(rules(t).Requirement(h.facts(t))).Value()
		if r.Exempt() != tc.exempt || (tc.exempt && !strings.Contains(r.Why(), "farming or fishing")) {
			t.Errorf("farm %v, fish %v: exempt %v, want %v: %s", tc.farm, tc.fish, r.Exempt(), tc.exempt, r.Why())
		}
		if tc.exempt {
			if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
				t.Errorf("farm %v, fish %v: interest %v", tc.farm, tc.fish, p.Value().Total())
			}
		}
	}
}

// Exception 2: no tax last year as a full-year resident. A part-year
// resident can't claim it, but last year's $0 still sets a $0 target.
func TestEstimatedNoLiabilityLastYear(t *testing.T) {
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	unanswered := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(0).Build())).Build())
	for name, tc := range map[string]struct {
		prior  *estimated.PriorReturn
		exempt bool
		ask    working.PositionKind
	}{
		"$0 as a full-year resident": {prior(0, true), true, working.PositionUnspecified},
		"not required to file":       {&notRequired, true, working.PositionUnspecified},
		"$0 as a part-year resident": {prior(0, false), false, working.PositionUnspecified},
		"residence not given":        {&unanswered, false, working.PositionResidency},
		"$1 as a full-year resident": {prior(100, true), false, working.PositionUnspecified},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("%s: exempt %v, want %v", name, res.Value().Exempt(), tc.exempt)
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
	}
	r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0, false)}.facts(t))).Value()
	if r.Target() != 0 || r.Required() {
		t.Errorf("part-year $0 last year: target %v, required %v", r.Target(), r.Required())
	}
}

// Exceptions 3 and 4 are the department's decision, so the interest is shown
// before them and the request is recorded.
func TestEstimatedWaiversAreRecordedNotApplied(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar}
	base := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !has(base.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked about")
	}
	for _, k := range []estimated.WaiverKind{estimated.WaiverCasualty, estimated.WaiverRetiredOrDisabled} {
		h.waiver = &k
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if p.Value().Total() != base.Value().Total() || p.Value().Waiver() != k || has(p.Required(), working.PositionPenaltyWaiver) {
			t.Errorf("%v: interest %v, waiver %v", k, p.Value().Total(), p.Value().Waiver())
		}
	}
}

// A late starter pays in 3, 2 or 1 equal installments, by OR-ESTIMATE's
// chart: after April 1 and before June 2, after June 1 and before September
// 2, after September 1. Withholding is spread over the same dates.
func TestEstimatedLateStart(t *testing.T) {
	for _, tc := range []struct {
		met  estimated.Date
		need []money.USCents
	}{
		{day(2026, 4, 1), []money.USCents{225_000, 450_000, 675_000, 900_000}},
		{day(2026, 4, 2), []money.USCents{0, 300_000, 600_000, 900_000}},
		{day(2026, 6, 1), []money.USCents{0, 300_000, 600_000, 900_000}},
		{day(2026, 6, 2), []money.USCents{0, 0, 450_000, 900_000}},
		{day(2026, 9, 1), []money.USCents{0, 0, 450_000, 900_000}},
		{day(2026, 9, 2), []money.USCents{0, 0, 0, 900_000}},
	} {
		h := household{tax: 10_000 * money.Dollar, firstMet: tc.met}
		res := must(rules(t).Schedule(h.facts(t)))
		for i, in := range res.Value() {
			if in.NeededBy() != tc.need[i] {
				t.Errorf("first met %v: installment %d needs %v, want %v", tc.met, i+1, in.NeededBy(), tc.need[i])
			}
		}
		if got := has(res.Required(), working.PositionUnsettledRule); got != (tc.need[0] == 0) {
			t.Errorf("first met %v: the late-start question reported %v", tc.met, got)
		}
	}
	h := household{tax: 10_000 * money.Dollar, withheld: 3_000 * money.Dollar, firstMet: day(2026, 7, 1)}
	for i, in := range must(rules(t).Schedule(h.facts(t))).Value() {
		if want := []money.USCents{0, 0, 150_000, 300_000}[i]; in.CreditedBy() != want {
			t.Errorf("installment %d credited %v, want %v", i+1, in.CreditedBy(), want)
		}
	}
	// Not told, the rules ask and use four.
	if res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t))); !has(res.Required(), working.PositionRequirementFirstMet) {
		t.Error("when the requirement was first met is not asked")
	}
}

// The annualised method: 22.5%, 45%, 67.5% and 90% of the net annualised
// tax, less earlier installments, capped by the regular installment plus
// what the method lowered before.
func TestEstimatedAnnualisedMethod(t *testing.T) {
	h := household{tax: 1_111_111, annualised: []money.USCents{200_000, 400_000, 800_000, 1_111_111}} // 90% is $10,000
	res := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{45_000, 180_000, 540_000, 1_000_000} {
		if got := res.Value()[i].NeededBy(); got != want {
			t.Errorf("installment %d needs %v, want %v", i+1, got, want)
		}
	}
	// A lower last period: 90% of $10,000 of annualised tax is $9,000.
	h.annualised = []money.USCents{200_000, 400_000, 800_000, 1_000_000}
	if got := must(rules(t).Schedule(h.facts(t))).Value()[3].NeededBy(); got != 900_000 {
		t.Errorf("a lower last period needs %v by January, want $9,000", got)
	}
	// A large first period is capped at the regular $2,500.
	h.annualised = []money.USCents{2_000_000, 2_000_000, 2_000_000, 1_111_111}
	if got := must(rules(t).Schedule(h.facts(t))).Value()[0].NeededBy(); got != 250_000 {
		t.Errorf("a large first period needs %v, want the regular $2,500", got)
	}
	// Periods that do not end on the worksheet's dates are refused.
	f := must(estimated.NewFacts(or, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(100).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).
		Periods(must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 6, 30)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).Build())
	if _, err := rules(t).Requirement(f); err == nil {
		t.Error("a period ending June 30 is accepted")
	}
}

// Payments go to the oldest unpaid installment first, and a balance that is
// paid stops accruing interest.
func TestEstimatedPaymentsPayTheOldestFirst(t *testing.T) {
	// $2,000 a quarter. Nothing in April; $4,000 on June 15.
	h := household{tax: 888_889, payments: []estimated.Payment{pay(dues[1], 4_000*money.Dollar)}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	if s[0].Shortfall() != 2_000*money.Dollar || s[0].Status() != estimated.StatusShort || s[1].Status() != estimated.StatusMet {
		t.Errorf("April %v short %v, June %v", s[0].Status(), s[0].Shortfall(), s[1].Status())
	}
	// April's $2,000 runs April 16 to June 14, 60 days: $26.28, so $26. June
	// 15 is the withholding line's own day: $0.44, so nothing.
	p := must(rules(t).Penalty(h.facts(t), day(2026, 9, 14))).Value()
	if p.Total() != 26*money.Dollar {
		t.Errorf("interest %v, want $26", p.Total())
	}
	// Last year's overpayment applied forward counts on April 15.
	fwd := must(estimated.NewPayment().FromPriorYear().Amount(8_000 * money.Dollar).Build())
	h = household{tax: 888_889, payments: []estimated.Payment{fwd}}
	for i, in := range must(rules(t).Schedule(h.facts(t))).Value() {
		if in.Status() != estimated.StatusMet {
			t.Errorf("installment %d is %v with last year's $8,000 applied", i+1, in.Status())
		}
	}
	// Withholding on its dates, when the filer shows them: December's
	// withholding pays nothing in April.
	h = household{tax: 20_000 * money.Dollar, withheld: 18_000 * money.Dollar, asWithheld: true}
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].Status() != estimated.StatusShort || s[3].Status() != estimated.StatusMet {
		t.Errorf("withholding on December 15: April %v, January %v", s[0].Status(), s[3].Status())
	}
}

// Interest stops on April 15, 2027, or the day the return is filed and paid.
func TestEstimatedInterestStops(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 6, 30))); p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
	h.returnFiled = day(2027, 2, 1)
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().AsOf() != day(2027, 2, 1) {
		t.Errorf("figured to %v, want the day the return was paid", p.Value().AsOf())
	}
}

// ORS 316.563(1)'s gross income test: gross income over $20,000 meets it for
// every filer. At $20,000 or below the rules ask, and a filer who says it is
// not met needs no declaration, though the interest is still figured.
func TestEstimatedGrossIncomeTest(t *testing.T) {
	unsaid := must(oregontaxcalc.NewEstimatedFacts().PriorReturnFiledFirst(true).Build())
	for _, tc := range []struct {
		gross money.USCents
		ask   bool
	}{{2_000_000, true}, {2_000_001, false}, {0, true}} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, gross: tc.gross, extras: &unsaid}.facts(t)))
		if got := has(res.Required(), working.PositionJurisdictionFact); got != tc.ask {
			t.Errorf("gross income %v: asked %v, want %v", tc.gross, got, tc.ask)
		}
	}
	notMet := must(oregontaxcalc.NewEstimatedFacts().PriorReturnFiledFirst(true).GrossIncomeTestMet(false).Build())
	h := household{tax: 10_000 * money.Dollar, extras: &notMet}
	r := must(rules(t).Requirement(h.facts(t))).Value()
	if r.Required() || r.Exempt() || r.Target() != 9_000*money.Dollar {
		t.Errorf("gross income test not met: required %v, exempt %v, target %v", r.Required(), r.Exempt(), r.Target())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() == 0 {
		t.Error("no interest figured when the gross income test is not met")
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"Who must declare":                      {"no gross income test", "ORS 316.563(1)"},
		"When payments are required":            {"worksheet line 23", "OAR 150-316-0493(3)(d)"},
		"Exactly $1,000":                        {"more than $1,000", "ORS 316.563(2), 316.588(1)"},
		"The prior-year return":                 {"filed before the 2026 return", "ORS 316.587(8)(b)"},
		"A requirement first met after April 1": {"3, 2 or 1 equal installments", "ORS 316.577, 316.579(3) to (5)"},
		"The daily interest factor":             {"0.000219", "OAR 150-305-0140(1), (3)(b)"},
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
		// A household with no late start, no prior return and $10,000 left
		// after withholding is untouched by these readings.
		switch d.Rule() {
		case "Who must declare", "When payments are required", "Exactly $1,000", "The prior-year return", "A requirement first met after April 1":
			if d.AffectsHousehold() {
				t.Errorf("%q affects the household", d.Rule())
			}
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := oregontaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	if _, err := oregontaxcalc.EstimatedCalendar(2027); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("the 2027 calendar: %v, want ErrNoRules", err)
	}
	for name, res := range map[string][]estimated.ResidencePeriod{
		"full year":   {must(estimated.NewResidencePeriod("OR").From(day(2026, 1, 1)).To(day(2026, 12, 31)).Build())},
		"nonresident": {must(estimated.NewResidencePeriod("WA").From(day(2026, 1, 1)).To(day(2026, 12, 31)).Build())},
	} {
		if _, err := rules(t).Requirement(household{tax: 10_000 * money.Dollar, residence: res}.facts(t)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(or, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(or, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Oregon's").Build()),
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
	all := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$1,000", "90%", "100%", "no 110% tier", "Oregon legal holiday", "running balance", "8%", "whole dollar"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// movedIn is a household that moved from California to Oregon on Jul 15,
// 2026.
var movedIn = []estimated.ResidencePeriod{
	must(estimated.NewResidencePeriod("CA").From(day(2026, 1, 1)).To(day(2026, 7, 14)).Build()),
	must(estimated.NewResidencePeriod("OR").From(day(2026, 7, 15)).To(day(2026, 12, 31)).Build()),
}

// Form OR-10 instructions, Line 3, note for part-year residents: the
// required annual payment is divided by the installment periods of Oregon
// residence. Moving in on Jul 15 puts residence in the periods ending Sep 15
// and Jan 15, so 90% of $4,000, $3,600, is $1,800 on each, and nothing is
// needed by Apr 15 or Jun 15. The $1,000 withheld is divided the same way.
func TestEstimatedPartYearResidentDividesByPeriodsOfResidence(t *testing.T) {
	h := household{tax: 4_000 * money.Dollar, withheld: 1_000 * money.Dollar, residence: movedIn}
	res := must(rules(t).Schedule(h.facts(t)))
	want := []money.USCents{0, 0, 1_800 * money.Dollar, 3_600 * money.Dollar}
	credited := []money.USCents{0, 0, 500 * money.Dollar, 1_000 * money.Dollar}
	got := res.Value()
	if len(got) != 4 {
		t.Fatalf("%d installments", len(got))
	}
	for i, inst := range got {
		if inst.NeededBy() != want[i] || inst.CreditedBy() != credited[i] {
			t.Errorf("installment %d: needed %v, credited %v; want %v and %v", i+1, inst.NeededBy(), inst.CreditedBy(), want[i], credited[i])
		}
	}
	if got[0].Status() != estimated.StatusNothingDue || got[1].Status() != estimated.StatusNothingDue {
		t.Errorf("April and June are %v and %v, want nothing due", got[0].Status(), got[1].Status())
	}
	// The period ending Sep 15 has residence from Jul 15 only, which the
	// instructions don't settle, and business income while living elsewhere
	// would add a period.
	if !has(res.Required(), working.PositionUnsettledRule) || !has(res.Required(), working.PositionJurisdictionFact) {
		t.Errorf("the partial period and business income are not asked: %v", res.Required())
	}

	// Oregon business income in the period ending Jun 15 adds that period:
	// $1,200 by each of the last three.
	extras := must(oregontaxcalc.NewEstimatedFacts().PriorReturnFiledFirst(true).GrossIncomeTestMet(true).BusinessIncomeIn(2).Build())
	h.extras = &extras
	got = must(rules(t).Schedule(h.facts(t))).Value()
	for i, w := range []money.USCents{0, 1_200 * money.Dollar, 2_400 * money.Dollar, 3_600 * money.Dollar} {
		if got[i].NeededBy() != w {
			t.Errorf("with business income, installment %d needs %v, want %v", i+1, got[i].NeededBy(), w)
		}
	}
	if _, err := oregontaxcalc.NewEstimatedFacts().BusinessIncomeIn(5).Build(); err == nil {
		t.Error("installment period 5 was accepted")
	}
}

// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
// never as a negative amount.
func TestEstimatedWithholdingOverTheTaxReadsAsARefund(t *testing.T) {
	figures := func(tax money.USCents) estimated.Figures {
		return must(estimated.NewFigures().Tax(tax).FederalAGI(60_000 * money.Dollar).StateAGI(60_000 * money.Dollar).Build())
	}
	last := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800 * money.Dollar)).Build())
	b := estimated.NewFacts("OR", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
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

// ORS 316.587(5)(b) and (c) grant the federal pair: a casualty or disaster,
// and retirement at 62 or older or disability in the tax year or the one
// before.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "or10-instructions-2025.txt", "During 2024 or 2025, you retired at age 62 or older or")
	mustContain(t, "ors316-estimated-tax.txt", "preceding such tax year, the taxpayer (i) retired after having attained age 62")
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
		{"retired in 2026 at 64", "eligible", "retired in 2026 after reaching 62", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(born)},
		{"retired in 2020", "refused", "covers retiring in 2025 or 2026", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(born)},
		{"retired at 61", "refused", "reached 62", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1965, 1, 1))},
		{"disaster", "eligible", "The Oregon Department of Revenue can waive", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1))},
		{"disabled in 2025", "eligible", "became disabled in 2025", estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2025, 11, 1))},
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

// A part-year resident $10 short on Sep 15 owes no interest: neither the
// $1,000 threshold nor the division over periods of residence excuses the
// installment, but the worksheet rounds each line's interest to the
// nearest whole dollar, and 107 days on $10 is 23 cents. The installment
// stays short, and the penalty sentence says why there is none.
func TestEstimatedShortfallWhoseInterestRoundsToNothing(t *testing.T) {
	mustContain(t, "or10-instructions-2025.txt", "Round each amount to the nearest", "whole dollar.")
	pay := func(m time.Month, d int, v money.USCents) estimated.Payment {
		return must(estimated.NewPayment().On(day(2026, m, d)).Amount(v).Build())
	}
	h := household{tax: 4_000 * money.Dollar, withheld: 1_000 * money.Dollar, residence: movedIn,
		payments: []estimated.Payment{pay(time.September, 15, 1_290*money.Dollar), pay(time.December, 1, 1_400*money.Dollar)}}
	f := h.facts(t)
	sep := must(rules(t).Schedule(f)).Value()[2]
	if sep.Status() != estimated.StatusShort || sep.Shortfall() != 10*money.Dollar {
		t.Fatalf("Sep 15 is %v by %v, want short by $10", sep.Status(), sep.Shortfall())
	}
	p := must(rules(t).Penalty(f, day(2027, 12, 31))).Value()
	if p.Total() != 0 {
		t.Fatalf("penalty %v, want $0", p.Total())
	}
	if want := "No penalty: Oregon's worksheet rounds the interest on the shortfall to $0."; p.Summary() != want {
		t.Errorf("summary %q, want %q", p.Summary(), want)
	}
	// Met on every date, the sentence is the plain one.
	h.payments = []estimated.Payment{pay(time.September, 15, 1_300*money.Dollar), pay(time.December, 1, 1_400*money.Dollar)}
	if got := must(rules(t).Penalty(h.facts(t), day(2027, 12, 31))).Value().Summary(); got != "No penalty." {
		t.Errorf("met on every date: summary %q, want \"No penalty.\"", got)
	}
}
