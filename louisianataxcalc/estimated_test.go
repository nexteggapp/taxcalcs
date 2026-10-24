// Louisiana's estimated tax rules for 2026, checked against the Department of
// Revenue's documents, the Revised Statutes and Act 307 of 2026.
//
// Louisiana publishes no exhaustive oracle for estimated tax. The strongest
// checks are R-210R-i's two worked examples: the carryforward of four
// periods' payments against a $400 required payment, and the penalty factor
// for an installment 30 days late. Every threshold, percentage, date and
// factor is compared against its extract in testdata/. The exceptions, the
// late start and the boundaries are hand-written cases.
package louisianataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/louisianataxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const la = estimated.Jurisdiction("LA")

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
	return must(louisianataxcalc.EstimatedTax(2026))
}

// applied reports whether a rule applied says s.
func applied(rs []working.Rule, s string) bool {
	for _, r := range rs {
		if strings.Contains(r.Applied(), s) {
			return true
		}
	}
	return false
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
		if x > 0 {
			out = append(out, pay(dues[i], x))
		}
	}
	return out
}

type household struct {
	status      filing.Status
	tax         money.USCents
	noTax       bool
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *louisianataxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	periodTax   []money.USCents // tax on the income earned to each period end, for Exception 5
	asWithheld  bool
	waiver      bool
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	status := h.status
	if status == filing.Unspecified {
		status = filing.Single
	}
	fig := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		fig = must(estimated.NewFigures().Build())
	}
	firstMet := h.firstMet
	if firstMet.IsZero() {
		firstMet = day(2026, 1, 1)
	}
	b := estimated.NewFacts(la, 2026).FilingStatus(status).ThisYear(fig).
		Paid(h.payments...).RequirementFirstMet(firstMet).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	}
	var el []func(*estimated.ElectionsBuilder) *estimated.ElectionsBuilder
	if h.periodTax != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.periodTax {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		el = append(el, (*estimated.ElectionsBuilder).Annualised)
	}
	if h.asWithheld {
		el = append(el, (*estimated.ElectionsBuilder).WithholdingAsWithheld)
	}
	eb := estimated.NewElections()
	for _, e := range el {
		e(eb)
	}
	b.Elections(must(eb.Build()))
	if h.waiver {
		b.Positions(must(estimated.NewPositions().Waiver(estimated.WaiverNone).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

// prior is a filed full-year resident return.
func prior(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).FullYearResident(true).Build())
	return &p
}

func factsTax(a money.USCents) *louisianataxcalc.EstimatedFacts {
	e := must(louisianataxcalc.NewEstimatedFacts().PriorYearFactsTax(a).Build())
	return &e
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "act307-2026.txt",
		"(i) Ninety Seventy percent of the tax shown on the return for the taxable year",
		"(ii) One hundred percent of the tax shown on the return of the individual for",
		"installment shall be twenty-five percent of the required annual payment.",
		"(1) The 15th fifteenth day of the fourth fifth month following the close of the",
		"(2) An amount equal to ninety seventy percent of the tax computed, at the",
		"standard deduction applicable to the current tax year, pursuant to R.S. 47:294, but",
		"Section 2. R.S. 47:118(D)(1)(c) is hereby repealed in its entirety.",
		"Section 3. The provisions of this Act shall be applicable to income tax periods",
		"beginning on or after January 1, 2026.",
	)
	mustContain(t, "ldr-2026-legislative-summaries-act307.txt",
		"It reduces from 90% to 70% the percentage of current-year tax used to determine certain individual estimated tax payment requirements.",
		"repeals the annualized income installment method",
		"applicable to taxable periods beginning on or after January 1, 2026. Effective May 22, 2026.",
	)
	mustContain(t, "it540esi-2026.txt",
		"can reasonably be expected to exceed $1,000 for a single filer or $2,000 for joint filers.",
		"1st payment ............................................ April 15, 2026",
		"2nd payment .......................................... June 15, 2026",
		"3rd payment ........................................... September 15, 2026",
		"4th payment ........................................... January 15, 2027",
		"falls on a weekend or legal holiday, the payment is due on the next business day.",
		"if the taxpayer files their individual income tax return by January 31 and pays the total amount due.",
		"will not relieve you of the underpayment penalty if you failed to pay the estimated income tax that was due earlier in the year.",
		"1/4 of Line 10 (Make 4 installments)", "1/3 of Line 10 (Make 3 installments)", "1/2 of Line 10 (Make 2 installments)",
		"Line 10 must be paid in full",
		"at least two-thirds of their gross income from farming or fishing",
		"Beginning January 1, 2026, the individual income tax is computed at a tax rate of 3 percent.",
		"Single $12,875 Married Filing Joint $25,750",
	)
	mustContain(t, "r210ri-2025.txt",
		"Between Jan 1 and April 1 April 15, 2025 4",
		"After April 1 and before June 2 June 15, 2025 3",
		"After June 1 and before Sept. 2 Sept. 15, 2025 2",
		"After September 1 Jan. 15, 2026 1",
		"divide the amount on Line 3 by the amount on Line 4. Round to nearest dollar",
		"If no return was filed for 2024 or you filed as a part-year resident for 2024, use the amount from Form R-210R, Line 1.",
		"Line 6 above is less than or equal to $1,000, STOP – NO PENALTY IS DUE.",
		"the amount on Line 6 above is less than or equal to $2,000,",
		"This exception does NOT apply to part-year residents for 2024.",
		"must have reflected an income tax liability.",
		"25% 50% 75% 100%",
		"03/31/2025 05/31/2025 08/31/2025 12/31/2025",
		"The penalty rate is 12 percent per year. The daily penalty rate is 0.0329 percent.",
		"you may equally distribute the amounts to each period based upon the number of months in the period.",
		"Carryforward amounts from the previous period can be a positive number or a negative number.",
		"Any credit carry forward from the 2024 tax year to the 2025 tax year must be included in the first installment period.",
		"Line 16 — Enter the date the payment was made that satisfied the underpayment. This date may be the date you filed the return.",
		"OR the 15 th day of the fourth month following the close of the taxable year, whichever is earlier.",
		"Ninety percent of the tax computed at the applicable rates on the basis of the actual taxable income",
	)
	mustContain(t, "rs-47-116.txt", "one thousand dollars for declarations of estimated tax for income tax years starting on and after January 1, 2001, after deducting all allowable credits")
	mustContain(t, "rs-47-117.txt",
		"(1) After April 1 and before June 2 of the taxable year, the declaration shall be filed on or before June 15",
		"(2) After June 1 and before September 2 of the taxable year, the declaration shall be filed on or before September 15",
		"(3) After September 1 of the taxable year, the declaration shall be filed on or before January 15")
	mustContain(t, "rs-47-117.1.txt", "four equal installments", "three equal installments", "two equal installments", "paid in full at the time of the filing of the declaration")
	mustContain(t, "rs-47-118.txt",
		"a penalty of twelve percent per annum upon the amount of the underpayment",
		"(ii) One hundred percent of the tax shown on the return of the individual for the preceding taxable year.",
		"an equal part of such amount shall be deemed paid on each installment date",
		"only to the extent such payment exceeds the amount of the installment determined under Subsection B(1) for such installment date.",
		"on or before one year from the statutory due date of the tax return associated with the underpayment not including any applicable extensions",
		// The legislature's page still prints the text before Act 307.
		"(i) Ninety percent of the tax shown on the return for the taxable year",
		"(1) The 15th day of the fourth month following the close of the taxable year;",
	)
	mustContain(t, "ldr-estimated-tax-page.txt",
		"$1,000 for single filers and $2,000 for joint filers",
		"if the estimated tax was 90 percent",
		"The 15th day of the fourth month following the close of the taxable year; or",
		"Updated February 27, 2025",
	)
}

// The 2026 dates are the ones IT-540ESi prints, and none moves.
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

// Act 307 sets 70% of this year's tax. R-210R-i's 90% would give $9,000.
func TestEstimatedThisYearsShareIsSeventyPercent(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	o := res.Value().Options()[0]
	if o.Basis() != estimated.BasisThisYearTax || o.Percent() != money.Percent(70) || o.Amount() != 7_000*money.Dollar {
		t.Errorf("this year's option is %v of %v = %v; want 70%% of $10,000 = $7,000", o.Basis(), o.Percent(), o.Amount())
	}
	if res.Value().Target() != 7_000*money.Dollar {
		t.Errorf("target %v, want $7,000", res.Value().Target())
	}
}

// The required annual payment is the lesser of 70% of this year's tax and
// 100% of last year's, one cent either side.
func TestEstimatedRequiredAnnualPaymentIsTheLesser(t *testing.T) {
	for _, tc := range []struct {
		priorTax money.USCents
		chosen   estimated.Basis
	}{
		{700_001, estimated.BasisThisYearTax},
		{700_000, estimated.BasisThisYearTax},
		{699_999, estimated.BasisPriorYearTax},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(tc.priorTax)}.facts(t)))
		c, _ := res.Value().Chosen()
		if c.Basis() != tc.chosen || res.Value().Target() != min(tc.priorTax, 700_000) {
			t.Errorf("last year %v: chose %v for %v", tc.priorTax, c.Basis(), res.Value().Target())
		}
		if o := res.Value().Options()[1]; o.Percent() != money.Percent(100) {
			t.Errorf("last year's option is %v, want 100%% with no 110%% tier", o.Percent())
		}
	}
}

// The threshold is $1,000 after withholding, or the department's $2,000 on
// a joint return, and "not more than" excuses: one cent either side.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	statute := must(louisianataxcalc.NewEstimatedFacts().StatuteJointThreshold(true).Build())
	form := must(louisianataxcalc.NewEstimatedFacts().StatuteJointThreshold(false).Build())
	for _, tc := range []struct {
		status filing.Status
		extras *louisianataxcalc.EstimatedFacts
		owed   money.USCents
		exempt bool
		asked  bool
	}{
		{filing.Single, nil, 100_000, true, false},
		{filing.Single, nil, 100_001, false, false},
		{filing.HeadOfHousehold, nil, 100_001, false, false},
		{filing.MarriedFilingSeparately, nil, 100_000, true, false},
		{filing.MarriedFilingJointly, nil, 200_000, true, true},
		{filing.MarriedFilingJointly, nil, 200_001, false, false},
		{filing.MarriedFilingJointly, nil, 100_000, true, false},
		{filing.MarriedFilingJointly, &form, 200_000, true, false},
		{filing.MarriedFilingJointly, &statute, 100_000, true, false},
		{filing.MarriedFilingJointly, &statute, 100_001, false, false},
	} {
		h := household{status: tc.status, tax: tc.owed + 5_000*money.Dollar, withheld: 5_000 * money.Dollar, extras: tc.extras}
		res := must(rules(t).Requirement(h.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("%v owing %v: exempt %v, want %v (%s)", tc.status, tc.owed, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		// Under the threshold the exception is the chosen option, at $0.
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || !strings.HasPrefix(c.Label(), "Tax less withholding of $")) {
			t.Errorf("%v owing %v: chosen %v %q at %v", tc.status, tc.owed, c.Basis(), c.Label(), c.Amount())
		}
		if got := has(res.Required(), working.PositionUnsettledRule); got != tc.asked {
			t.Errorf("%v owing %v: joint threshold asked %v, want %v", tc.status, tc.owed, got, tc.asked)
		}
	}
}

// Last year's tax needs a filed full-year resident return. A filed return
// showing no tax gives a required annual payment of nothing, as R-210R line
// 3 takes the smaller of lines 1 and 2.
func TestEstimatedPriorYearOptionAvailability(t *testing.T) {
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	unanswered := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(100).Build())).Build())
	partYear := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(100).Build())).FullYearResident(false).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   working.PositionKind
	}{
		"no return on record": {nil, working.PositionPriorYearReturn},
		"not filed":           {&notFiled, working.PositionUnspecified},
		"residence not given": {&unanswered, working.PositionResidency},
		"part-year":           {&partYear, working.PositionUnspecified},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior, extras: factsTax(50)}.facts(t)))
		opts := res.Value().Options()
		if opts[1].Available() || opts[2].Available() {
			t.Errorf("%s: last year's tests are offered", name)
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
	}

	h := household{tax: 10_000 * money.Dollar, prior: prior(0)}
	res := must(rules(t).Requirement(h.facts(t)))
	if res.Value().Required() || res.Value().Target() != 0 {
		t.Errorf("after a $0 return: required %v, target %v", res.Value().Required(), res.Value().Target())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 15))); p.Value().Total() != 0 {
		t.Errorf("after a $0 return: penalty %v", p.Value().Total())
	}
}

// R-210R line 5 divides line 3 by the installments and rounds to the
// nearest dollar. With nothing paid, April's shortfall is that amount.
func TestEstimatedInstallmentsAreWholeDollars(t *testing.T) {
	for _, tc := range []struct {
		line3, each money.USCents
	}{{100_200, 25_100}, {100_198, 25_000}} {
		res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, prior: prior(tc.line3)}.facts(t)))
		if got := res.Value()[0].Shortfall(); got != tc.each {
			t.Errorf("line 3 of %v: %v an installment, want %v", tc.line3, got, tc.each)
		}
	}
}

// R-210R-i's example under line 7: $400 required each period, $300, $600,
// $350 and nothing paid, leaving $100 short in April and $350 in January.
// April's $100 is made up on June 15, 61 days late; January's runs 120 days
// to May 15, 2027. At 0.000329 a day those are $2.01 and $13.82.
func TestEstimatedLedgerMatchesR210RExample(t *testing.T) {
	mustContain(t, "r210ri-2025.txt",
		"Required payment - Amount on Line 3 divided by amount on Line 4 400.00 400.00 400.00 400.00",
		"Amount paid for each period 300.00 600.00 350.00 0",
		"Overpayment (positive number) or Underpayment (negative number) -100.00 100.00 50.00 -350.00")
	h := household{tax: 3_000 * money.Dollar, prior: prior(1_600 * money.Dollar), payments: quarterly(300*money.Dollar, 600*money.Dollar, 350*money.Dollar, 0), waiver: true}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	want := []money.USCents{100 * money.Dollar, 0, 0, 350 * money.Dollar}
	for i, in := range sched {
		if in.Shortfall() != want[i] {
			t.Errorf("installment %d is %v short, want %v", i+1, in.Shortfall(), want[i])
		}
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 12, 31))).Value()
	lines := p.Lines()
	if len(lines) != 2 || lines[0].Days() != 61 || lines[0].Total() != 201 || lines[1].Days() != 120 || lines[1].Total() != 1_382 {
		t.Fatalf("lines %+v", lines)
	}
	if p.Total() != 1_583 {
		t.Errorf("penalty %v, want $15.83", p.Total())
	}
}

// R-210R-i's line 18 example: 30 days late is a factor of 0.00987. On a
// $1,000 shortfall that is $9.87, where 12% over 365 days gives $9.86.
func TestEstimatedPenaltyUsesThePrintedDailyFactor(t *testing.T) {
	mustContain(t, "r210ri-2025.txt", "Assume the installment is 30 days late. The penalty factor is 30 X 0.000329 = 0.00987.")
	h := household{tax: 30_000 * money.Dollar, prior: prior(16_000 * money.Dollar),
		payments: []estimated.Payment{pay(dues[0], 3_000*money.Dollar), pay(day(2026, 5, 15), 1_000*money.Dollar),
			pay(dues[1], 4_000*money.Dollar), pay(dues[2], 4_000*money.Dollar), pay(dues[3], 4_000*money.Dollar)}}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 15))).Value()
	if len(p.Lines()) != 1 || p.Lines()[0].Days() != 30 || p.Total() != 987 {
		t.Errorf("penalty %v over %d lines, want $9.87 for 30 days", p.Total(), len(p.Lines()))
	}
}

// Act 307 runs the penalty to May 15, 2027. On $1,750 short from April 15,
// 2026 that is 395 days, $227.42; R-210R-i's April 15 would stop it at 365.
func TestEstimatedPenaltyStopsOnMayFifteenth(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 6, 30))).Value()
	if p.AsOf() != day(2027, 5, 15) {
		t.Errorf("figured to %v, want May 15, 2027", p.AsOf())
	}
	l := p.Lines()[0]
	if l.Days() != 395 || l.Total() != 22_742 {
		t.Errorf("April's line is %d days and %v, want 395 and $227.42", l.Days(), l.Total())
	}
	if last := p.Lines()[3]; last.Days() != 120 {
		t.Errorf("January's line is %d days, want 120", last.Days())
	}
	if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 3, 1)}.facts(t), day(2027, 5, 15))); p.Value().AsOf() != day(2027, 3, 1) {
		t.Errorf("figured to %v, want the day the return was paid", p.Value().AsOf())
	}
}

// Louisiana's 12% is fixed by R.S. 47:118(A), so there is no rate period
// boundary to cross: the factor is the same before and after the year end,
// where other states' rates change. A shortfall from September 15, 2026 paid
// February 1, 2027 is charged in one part for 139 days: $1,750 × 139 ×
// 0.000329 = $80.03.
func TestEstimatedSpanAcrossTheYearEnd(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{
		pay(dues[0], 1_750*money.Dollar), pay(dues[1], 1_750*money.Dollar), pay(day(2027, 2, 1), 3_500*money.Dollar)}}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 15))).Value()
	lines := p.Lines()
	if len(lines) != 2 {
		t.Fatalf("%d lines, want September and January", len(lines))
	}
	parts := lines[0].Parts()
	if len(parts) != 1 || parts[0].Days() != 139 || parts[0].Charge() != 8_003 || parts[0].Rate() != money.Percent(12) {
		t.Errorf("September's parts %+v, want one of 139 days, $80.03, at 12%%", parts)
	}
	if lines[1].Days() != 17 || lines[1].Total() != 979 {
		t.Errorf("January's line is %d days and %v, want 17 and $9.79", lines[1].Days(), lines[1].Total())
	}
}

// Last year's facts at this year's rate excuse an installment; they do not
// lower it. Paying over a quarter of them each date owes nothing; paying
// under in April is charged on April's shortfall against the required $3,500.
func TestEstimatedFactsExceptionExcusesButDoesNotLower(t *testing.T) {
	h := household{tax: 20_000 * money.Dollar, prior: prior(16_000 * money.Dollar), extras: factsTax(10_000 * money.Dollar),
		payments: quarterly(2_600*money.Dollar, 2_600*money.Dollar, 2_600*money.Dollar, 2_600*money.Dollar)}
	req := must(rules(t).Requirement(h.facts(t))).Value()
	if c, _ := req.Chosen(); c.Basis() != estimated.BasisPriorYearIncome || req.Target() != 10_000*money.Dollar {
		t.Errorf("chose %v for %v, want last year's facts, $10,000", c.Basis(), req.Target())
	}
	// Each date needs the regular $3,500 a quarter and is short of it; the
	// exception, $2,500 a quarter, excuses it.
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, in := range sched.Value() {
		if in.Status() != estimated.StatusExcused || in.Shortfall() == 0 {
			t.Errorf("installment %d is %v with %v short; want excused with a shortfall", i+1, in.Status(), in.Shortfall())
		}
		if want := money.USCents(i+1) * 3_500 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
		if want := money.USCents(i+1) * 2_500 * money.Dollar; !applied(sched.Applied(), "which needs "+want.String()+" by then") {
			t.Errorf("installment %d: the exception's %v is not reported", i+1, want)
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 15))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v when every date met Exception 3", p.Value().Total())
	}

	// April $2,400: $1,100 short against $3,500, made up June 15, 61 days:
	// $22.08. Not $100 against the $2,500 test.
	h.payments = quarterly(2_400*money.Dollar, 2_600*money.Dollar, 2_500*money.Dollar, 2_500*money.Dollar)
	p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 15))).Value()
	if len(p.Lines()) != 1 || p.Lines()[0].Shortfall() != 1_100*money.Dollar || p.Total() != 2_208 {
		t.Errorf("penalty %v over %d lines; want $22.08 on April's $1,100", p.Total(), len(p.Lines()))
	}

	// Without the figure the exception is not offered, and it is asked for.
	h.extras = nil
	res := must(rules(t).Requirement(h.facts(t)))
	if res.Value().Options()[2].Available() || !has(res.Required(), working.PositionJurisdictionFact) {
		t.Errorf("without last year's facts: offered %v, asked %v", res.Value().Options()[2].Available(), has(res.Required(), working.PositionJurisdictionFact))
	}
}

// Exception 2 needs a liability on a twelve-month return; the $16,000 here
// is above 70% of this year's tax, so it excuses nothing a payment did not
// already meet, and the April shortfall is charged.
func TestEstimatedExceptionTwoNeedsTheWholeQuarter(t *testing.T) {
	h := household{tax: 30_000 * money.Dollar, prior: prior(16_000 * money.Dollar), payments: quarterly(3_999*money.Dollar, 4_001*money.Dollar, 4_000*money.Dollar, 4_000*money.Dollar)}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	if s[0].Status() != estimated.StatusShort || s[0].Shortfall() != 100 {
		t.Errorf("April is %v with %v short, want $1 short", s[0].Status(), s[0].Shortfall())
	}
	h.payments = quarterly(4_000*money.Dollar, 4_000*money.Dollar, 4_000*money.Dollar, 4_000*money.Dollar)
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].Status() != estimated.StatusMet {
		t.Errorf("April is %v when paid in full", s[0].Status())
	}
}

// Exception 5 is 70% of the tax on the income actually earned to each
// period end, with no annualizing.
func TestEstimatedExceptionFiveUsesIncomeToDate(t *testing.T) {
	h := household{tax: 20_000 * money.Dollar,
		periodTax: []money.USCents{1_000 * money.Dollar, 3_000 * money.Dollar, 8_000 * money.Dollar, 20_000 * money.Dollar},
		payments:  quarterly(700*money.Dollar, 1_400*money.Dollar, 3_500*money.Dollar, 8_400*money.Dollar)}
	sched := must(rules(t).Schedule(h.facts(t)))
	// Each date needs the regular amount, and Exception 5's 70% of the tax
	// to date excuses the first three.
	wantNeeded := []money.USCents{3_500 * money.Dollar, 7_000 * money.Dollar, 10_500 * money.Dollar, 14_000 * money.Dollar}
	wantException := []money.USCents{700 * money.Dollar, 2_100 * money.Dollar, 5_600 * money.Dollar}
	wantStatus := []estimated.Status{estimated.StatusExcused, estimated.StatusExcused, estimated.StatusExcused, estimated.StatusMet}
	for i, in := range sched.Value() {
		if in.NeededBy() != wantNeeded[i] || in.Status() != wantStatus[i] {
			t.Errorf("installment %d needs %v and is %v; want %v and %v", i+1, in.NeededBy(), in.Status(), wantNeeded[i], wantStatus[i])
		}
		if i < len(wantException) && !applied(sched.Applied(), "which needs "+wantException[i].String()+" by then") {
			t.Errorf("installment %d: Exception 5's %v is not reported", i+1, wantException[i])
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 15))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v when Exception 5 covers every short date", p.Value().Total())
	}
	h.periodTax = nil
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 15))); p.Value().Total() == 0 {
		t.Error("without Exception 5 the same payments owe nothing")
	}
	// The periods must be R-210R's.
	bad := must(estimated.NewFacts(la, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(100).Build())).
		Periods(must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 6, 30)).Build()),
			must(estimated.NewPeriod().End(day(2026, 9, 30)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).Build())
	if _, err := rules(t).Requirement(bad); err == nil {
		t.Error("calendar-quarter periods, Exception 4's, were accepted")
	}
}

// A requirement first met later divides line 3 among the dates left, at the
// boundaries R-210R prints: April 1 and 2, June 1 and 2, September 1 and 2.
func TestEstimatedLateStart(t *testing.T) {
	for _, tc := range []struct {
		met  estimated.Date
		want []money.USCents
	}{
		{day(2026, 4, 1), []money.USCents{175_000, 350_000, 525_000, 700_000}},
		{day(2026, 4, 2), []money.USCents{0, 233_300, 466_600, 699_900}},
		{day(2026, 6, 1), []money.USCents{0, 233_300, 466_600, 699_900}},
		{day(2026, 6, 2), []money.USCents{0, 0, 350_000, 700_000}},
		{day(2026, 9, 1), []money.USCents{0, 0, 350_000, 700_000}},
		{day(2026, 9, 2), []money.USCents{0, 0, 0, 700_000}},
	} {
		s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, firstMet: tc.met}.facts(t))).Value()
		for i, in := range s {
			if in.NeededBy() != tc.want[i] {
				t.Errorf("first met %v: installment %d needs %v, want %v", tc.met, i+1, in.NeededBy(), tc.want[i])
			}
			if tc.want[i] == 0 && in.Status() != estimated.StatusNothingDue {
				t.Errorf("first met %v: installment %d is %v", tc.met, i+1, in.Status())
			}
		}
	}
	f := must(estimated.NewFacts(la, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Build())
	if res := must(rules(t).Requirement(f)); !has(res.Required(), working.PositionRequirementFirstMet) {
		t.Error("when the requirement was first met is not asked")
	}
}

// A return filed and paid by January 31 excuses the January installment.
// January 31, 2027 is a Sunday; nothing says it moves, so February 1 does not.
func TestEstimatedReturnFiledByJanuaryThirtyFirst(t *testing.T) {
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 1, 29), true}, {day(2027, 1, 31), true}, {day(2027, 2, 1), false}} {
		s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: tc.filed}.facts(t))).Value()
		if got := s[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
		if s[0].Status() != estimated.StatusShort {
			t.Errorf("filed %v: April is %v, want short", tc.filed, s[0].Status())
		}
	}
}

// Withholding is an equal part on each date, or on its dates by election.
func TestEstimatedWithholding(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, withheld: 4_000 * money.Dollar}
	if got := must(rules(t).Schedule(h.facts(t))).Value()[0].CreditedBy(); got != 1_000*money.Dollar {
		t.Errorf("April credited %v, want a quarter of $4,000", got)
	}
	h.asWithheld = true
	if got := must(rules(t).Schedule(h.facts(t))).Value()[0].CreditedBy(); got != 0 {
		t.Errorf("April credited %v, want nothing before December 15", got)
	}
}

// The waiver is the secretary's to grant, so it is asked and not applied.
func TestEstimatedWaiverIsAsked(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar}
	if res := must(rules(t).Penalty(h.facts(t), day(2027, 5, 15))); !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked")
	}
	h.waiver = true
	if res := must(rules(t).Penalty(h.facts(t), day(2027, 5, 15))); has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is asked after it was answered")
	}
}

// Circumstances beyond your control are presumed good faith; retirement and
// disability have no waiver, at any age or date, unlike the federal ones.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pos      *estimated.PositionsBuilder
		eligible bool
		want     string
	}{
		{"disaster", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)), true, "beyond your control"},
		{"retired in 2026 at 64", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)), false, "no waiver for retirement or disability"},
		{"retired in 2020", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, 3, 1)), false, "no waiver for retirement or disability"},
		{"disabled in 2026", estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2026, 6, 1)), false, "no waiver for retirement or disability"},
	} {
		pos := must(tc.pos.Build())
		p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, positions: &pos}.facts(t), day(2027, 5, 15))).Value()
		o, ok := p.WaiverOutcome()
		if !ok || o.Eligible() != tc.eligible || o.Refused() == tc.eligible || !strings.Contains(o.Reason(), tc.want) {
			t.Errorf("%s: outcome %v eligible %v, %q", tc.name, ok, o.Eligible(), o.Reason())
		}
		if p.Total() == 0 {
			t.Errorf("%s: the penalty is before any waiver, got $0", tc.name)
		}
		want := p.Total()
		if tc.eligible {
			want = 0
		}
		if p.IfWaived() != want {
			t.Errorf("%s: if waived %v, want %v", tc.name, p.IfWaived(), want)
		}
	}
}

// An exception that sets the target leaves no question about last year's
// return, whose answer can't change it.
func TestEstimatedExemptAsksNothingOfTheTarget(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 500 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() || has(res.Required(), working.PositionPriorYearReturn) {
		t.Errorf("exempt %v, asked %v", res.Value().Exempt(), res.Required())
	}
}

// Each divergence is reported with both readings and both sources. The
// four that name a document predating Act 307 follow the Act.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 5, 15)))
	want := map[string][3]string{
		"This year's share of the tax":               {"90%", "70%", "Act 307"},
		"When the penalty stops":                     {"April 15", "May 15, 2027", "Act 307"},
		"The annualized income exception":            {"Exception 4", "repealed", "Act 307"},
		"Last year's figures at this year's rate":    {"filing status", "standard deduction", "Act 307"},
		"The threshold for a joint return":           {"$2,000", "$1,000", "47:116(A)"},
		"When withholding counts as paid":            {"number of months", "equal part", "47:118(E)(2)"},
		"Last year's tests after a part-year return": {"part-year", "no condition", "47:118(B)(2)(b)(ii)"},
		"Needing to pay only after April 1":          {"1/3, 1/2", "25%", "47:118(B)(2)(a)"},
		"Which shortfall a payment pays first":       {"carried to the next", "exceeds that date's installment", "47:118(C)(2)"},
		"The daily rate":                             {"0.000329", "0.000328767", "47:118(A)"},
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
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.Statute(), w[1]) || !strings.Contains(d.StatuteSource(), w[2]) || d.FormSource() == "" {
			t.Errorf("%q does not pin both readings: form %q (%s), statute %q (%s)", d.Rule(), d.Form(), d.FormSource(), d.Statute(), d.StatuteSource())
		}
		if strings.Contains(d.StatuteSource(), "Act 307") {
			for _, s := range []string{"out of date", "predate"} {
				if !strings.Contains(d.Form(), s) {
					t.Errorf("%q does not say the form is %s", d.Rule(), s)
				}
			}
			for _, s := range []string{"May 22, 2026", "January 1, 2026", "1/26", "February 27, 2025", "R.S. 47:118 page"} {
				if !strings.Contains(d.FormSource(), s) {
					t.Errorf("%q does not cite %q", d.Rule(), s)
				}
			}
			if d.Uses() != working.ReadingStatute {
				t.Errorf("%q uses %v, want the Act", d.Rule(), d.Uses())
			}
		}
	}

	// $10,000 of tax, nothing paid and no return on record: the 70% share,
	// the stop date and the daily rate change the figures, and nothing else.
	for _, d := range res.Divergences() {
		want := d.Rule() == "This year's share of the tax" || d.Rule() == "When the penalty stops" || d.Rule() == "The daily rate"
		if d.AffectsHousehold() != want {
			t.Errorf("%q affects %v, want %v", d.Rule(), d.AffectsHousehold(), want)
		}
	}
	// A joint return between $1,000 and $2,000 is affected by the joint
	// threshold, and held to the statute when the position says so.
	statute := must(louisianataxcalc.NewEstimatedFacts().StatuteJointThreshold(true).Build())
	for _, tc := range []struct {
		extras *louisianataxcalc.EstimatedFacts
		uses   working.Reading
	}{{nil, working.ReadingForm}, {&statute, working.ReadingStatute}} {
		h := household{status: filing.MarriedFilingJointly, tax: 6_500 * money.Dollar, withheld: 5_000 * money.Dollar, extras: tc.extras}
		for _, d := range must(rules(t).Requirement(h.facts(t))).Divergences() {
			if d.Rule() == "The threshold for a joint return" && (!d.AffectsHousehold() || d.Uses() != tc.uses) {
				t.Errorf("joint, $1,500 after withholding: affects %v, uses %v; want it to affect, using %v", d.AffectsHousehold(), d.Uses(), tc.uses)
			}
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := louisianataxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"a farmer":    must(estimated.NewFacts(la, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(7000).Build()),
		"a fisherman": must(estimated.NewFacts(la, 2026).FilingStatus(filing.Single).ThisYear(this).FishShare(7000).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(la, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(la, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Louisiana's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := louisianataxcalc.NewEstimatedFacts().PriorYearFactsTax(-1).Build(); err == nil {
		t.Error("a negative tax on last year's facts was accepted")
	}
}

// Without this year's tax the target is last year's tax, or last year's
// facts when smaller, and the penalty against last year's tax is an upper
// bound. With no return to use there is no target.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000 * money.Dollar)}
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
	p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 15))).Value()
	if !p.UpperBound() || p.Total() == 0 {
		t.Errorf("unpaid: penalty %v, upper bound %v", p.Total(), p.UpperBound())
	}

	h.extras = factsTax(6_000 * money.Dollar)
	h.payments = quarterly(1_500*money.Dollar, 1_500*money.Dollar, 1_500*money.Dollar, 1_500*money.Dollar)
	if r := must(rules(t).Requirement(h.facts(t))).Value(); r.Target() != 6_000*money.Dollar {
		t.Errorf("target %v, want last year's facts, $6,000", r.Target())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 5, 15))).Value(); p.Total() != 0 {
		t.Errorf("paid to last year's facts: penalty %v", p.Total())
	}

	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() {
		t.Error("a target with no return to use")
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 5, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$1,000", "$2,000", "70%", "Act 307", "May 15", "12%", "no 110%", "R-20128"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("LA"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}
