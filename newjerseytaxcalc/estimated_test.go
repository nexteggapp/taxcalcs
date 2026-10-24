// New Jersey's estimated tax rules for 2026, checked against the Division of
// Taxation's own documents and the New Jersey Statutes.
//
// New Jersey's strongest oracle is GIT-8 (January 2026), which works two
// examples through Form NJ-2210: one taxpayer who meets an exception in every
// quarter but the third, and the same taxpayer meeting none. Its multipliers
// are marked as illustrations, not the current rate, so the tests take its
// amounts, exceptions and structure and apply the 2026 rate: each period's
// balance for its whole months at 10% a year. Every threshold, rate, date and
// percentage is compared against its extract in testdata/. The other cases
// are hand-written.
package newjerseytaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/newjerseytaxcalc"
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
	return must(newjerseytaxcalc.EstimatedTax(2026))
}

func because(rs []working.Required, k working.PositionKind, phrase string) bool {
	for _, r := range rs {
		if r.Position() == k && r.Asked() && (strings.Contains(r.Question(), phrase) || strings.Contains(r.Effect(), phrase)) {
			return true
		}
	}
	return false
}

func has(rs []working.Required, k working.PositionKind) bool { return because(rs, k, "") }

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

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
	tax         money.USCents
	noTax       bool
	gross       money.USCents
	status      filing.Status
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *newjerseytaxcalc.EstimatedFacts
	returnFiled estimated.Date
	annualised  []money.USCents
	farmShare   money.Rate
	fishShare   money.Rate
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	fb := estimated.NewFigures().Tax(h.tax)
	if h.noTax {
		fb = estimated.NewFigures()
	}
	if h.gross > 0 {
		fb.GrossIncome(h.gross)
	}
	status := h.status
	if status == filing.Unspecified {
		status = filing.Single
	}
	b := estimated.NewFacts("NJ", 2026).FilingStatus(status).ThisYear(must(fb.Build())).
		Paid(h.payments...).ReturnFiled(h.returnFiled).FarmShare(h.farmShare).FishShare(h.fishShare)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(newjerseytaxcalc.NewEstimatedFacts().RefundableCredits(0).Build()))
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

func penalty(t *testing.T, h household) working.Result[estimated.Penalty] {
	t.Helper()
	return must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "nj-1040-es-2026.txt",
		"you are required to make estimated tax payments if you estimate that you will owe more than $400.",
		"Filing thresholds: $20,000 or less (filing status married/CU couple, filing joint return or head of household or qualifying widow(er)/surviving CU partner), or $10,000 or less (estates and trusts, or individ­uals with filing status single or married/CU partner, filing separate return).",
		"additional payments must be made on or before June 15, 2026, September 15, 2026, and January 15, 2027.",
		"that overpayment will be automatically credited to the first quarterly installment",
		"as long as you file your 2026 Income Tax return by February 15, 2027, and pay the entire balance due.",
		"Pay the estimated tax in equal installments on the remaining payment dates.",
		"If at least two-thirds of your total estimated income for the tax year is from farming (including oyster farming)",
		"Interest will be assessed at the prime rate plus 3%, compounded annually.",
	)
	mustContain(t, "nj-2210-2025.txt",
		"3. Subtract line 2 from line 1 (If less than $400, do not complete the rest of this form)",
		"4a. Multiply the amount on line 1 by .80 (80%)",
		"5. Use the lesser amount from either line 4a or 4b and divide by four.",
		"25% of 2024 Tax 50% of 2024 Tax 75% of 2024 Tax 100% of 2024 Tax",
		"20% of Tax 40% of Tax 60% of Tax",
		"90% of Tax 90% of Tax 90% of Tax",
		"If the amount of any exception is equal to or less than the corresponding amount at line 14, interest will not be charged for that period",
		"2. Annualization amounts.......",
		"4 2.4 1.5",
		"is imposed each month or fraction thereof the underpayment exists.",
		"4/16/25 – 1/15/26 10.75%",
		"1/16/26 – 4/15/26 10.00%",
	)
	mustContain(t, "nj-2210-2025-instructions.txt",
		"100% of your 2024 tax liability, provided your 2024 New Jersey Income Tax return covered a full 12-month period.",
		"Line 3 – If line 3 is $400 or more, complete lines 4 through 13.",
		"The total amount of tax withheld is considered to have been paid evenly (one-fourth of total amount) on each payment due date, unless you can show otherwise.",
		"The total amount of credit, if any, from your prior year tax return should be included in the amount of your payment in column B.",
		"Your prior year tax liability is zero if a return was not required to be filed.",
	)
	mustContain(t, "tb-21r.txt", "The assessed interest rate on outstanding tax balances for the period January 1 – December 31, 2026, is 10.00%.",
		"that was in effect on December 1 of the year prior to the year in which the tax became due.")
	mustContain(t, "njsa-estimated-tax.txt",
		"can reasonably be expected to be more than $400 in excess of any credits allowable",
		"If on or before February 15 of the succeeding taxable year an individual files his return",
		"the estimated tax shall be paid in three equal installments.",
		"80% of the tax (two-thirds of the tax for farmers",
		"(2) 100% of the tax shown on the tax return of the taxpayer for the preceding taxable year",
		"but not beyond the 15th day of the fourth month following the close of the taxable year.",
		"(C) An amount equal to 80% of the tax for the taxable year",
		"(2) An amount equal to 90% of the tax computed, at the rates applicable to the taxable year, on the basis of the actual income",
		`subparagraphs (A) and (B) of paragraph (1) of this subsection shall be applied by substituting "110%" for "100%".`,
		"three percentage points above the prime rate assessed for each month or fraction thereof, compounded annually at the end of each year",
		"may remit or waive the payment of any interest charge in excess of the rate of three percentage points above the prime rate",
	)
	mustContain(t, "git-8-2026-passages.txt", "If a due date falls on a weekend or legal holiday, that payment is due the following business day.")
	mustContain(t, "nj-notice-over-150000.txt", "only on the difference between what was actually paid and either 100% of last year’s tax or 80% of the current year’s tax")
}

// GIT-8's two examples: $150 due a quarter, $90, $90, $90 and $240 paid on
// the dates. Meeting no exception, the balances are $60, $120, $180 and $90:
// at 10% for 2, 3 and 4 months, $1.00, $3.00 and $6.00. January's balance
// runs from January 16, 2027, whose rate is not yet set. Meeting an
// exception in every quarter but the third, only September's own $60 is
// charged: $2.00.
func TestEstimatedInterestFollowsTheGIT8Examples(t *testing.T) {
	mustContain(t, "git-8-2026-examples.txt",
		"payment due date shown. (Do not include withholdings after December 31, 2025.) (See instructions) ................................ 90 180 270 510",
		"15. Exception 1 – Enter 2024 tax (line 50) ............... $600 15. 150 300 450 600",
		"16. Exception 2 – Tax on 2025 gross income using 2024 exemptions and tax rates........................................................... 16. 105 210 315 420",
		"17. Exception 3 – Tax on annualized 2025 income ............................ 17. 86 178 315",
		"periods 18. 90 154.80 294.30",
		"150.00 0 150.00 90.00 60.00 .031 1.86",
		"150.00 150.00 90.00 60.00 .010 0.60",
		"150.00 60.00 210.00 90.00 120.00 .019 2.28",
		"150.00 120.00 270.00 90.00 180.00 .031 5.58",
		"150.00 180.00 330.00 240.00 90.00 .025 2.25",
	)
	base := household{tax: 750 * money.Dollar, prior: prior(600 * money.Dollar), payments: quarterly(90*money.Dollar, 90*money.Dollar, 90*money.Dollar, 240*money.Dollar)}
	res := penalty(t, base)
	lines := res.Value().Lines()
	want := []struct {
		balance money.USCents
		months  int
		charge  money.USCents
	}{{6_000, 2, 100}, {12_000, 3, 300}, {18_000, 4, 600}}
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	for i, w := range want {
		parts := lines[i].Parts()
		if lines[i].Shortfall() != w.balance || len(parts) != 1 || parts[0].Months() != w.months || parts[0].Charge() != w.charge {
			t.Errorf("period %d: %v in %d parts; want %v for %d months, %v", i+1, lines[i].Shortfall(), len(parts), w.balance, w.months, w.charge)
		}
	}
	if lines[3].Shortfall() != 9_000 || len(lines[3].Parts()) != 0 {
		t.Errorf("January: %v in %d parts; want $90 with no rated month", lines[3].Shortfall(), len(lines[3].Parts()))
	}
	if res.Value().Total() != 1_000 || !because(res.Required(), working.PositionUnpublishedRate, "January 15, 2027") {
		t.Errorf("interest %v, gap reported %v; want $10.00 and the 2027 rate reported", res.Value().Total(), because(res.Required(), working.PositionUnpublishedRate, "January 15, 2027"))
	}

	e := must(newjerseytaxcalc.NewEstimatedFacts().RefundableCredits(0).PriorYearIncomeAtThisYearsRates(420*money.Dollar).
		TaxOnIncomeToDate(100*money.Dollar, 172*money.Dollar, 327*money.Dollar).Build())
	met := base
	met.extras = &e
	met.annualised = []money.USCents{430 * money.Dollar, 445 * money.Dollar, 525 * money.Dollar, 750 * money.Dollar}
	res = penalty(t, met)
	lines = res.Value().Lines()
	if len(lines) != 1 || lines[0].Installment() != 3 || lines[0].Shortfall() != 6_000 || res.Value().Total() != 200 {
		t.Errorf("exceptions met but in September: %v over %v; want $2.00 on September's $60", res.Value().Total(), lines)
	}
	s := must(rules(t).Schedule(met.facts(t))).Value()
	for i, want := range []estimated.Status{estimated.StatusExcused, estimated.StatusExcused, estimated.StatusShort, estimated.StatusExcused} {
		if s[i].Status() != want {
			t.Errorf("quarter %d is %v, want %v", i+1, s[i].Status(), want)
		}
	}
}

// Each exception at its boundary: a cent short leaves the quarter charged,
// and the exact amount excuses it. Exception 3's $86.00 in April, Exception
// 4's $154.80 in June, and Exception 2's $420.00 in January.
func TestEstimatedExceptionBoundaries(t *testing.T) {
	ex2 := must(newjerseytaxcalc.NewEstimatedFacts().RefundableCredits(0).PriorYearIncomeAtThisYearsRates(420 * money.Dollar).Build())
	ex4 := must(newjerseytaxcalc.NewEstimatedFacts().RefundableCredits(0).TaxOnIncomeToDate(0, 172*money.Dollar, 0).Build())
	plain := must(newjerseytaxcalc.NewEstimatedFacts().RefundableCredits(0).Build())
	for name, tc := range map[string]struct {
		extras  newjerseytaxcalc.EstimatedFacts
		annual  []money.USCents
		short   []money.USCents
		exact   []money.USCents
		quarter int
	}{
		"Exception 3 in April": {plain, []money.USCents{430 * money.Dollar, 20_000 * money.Dollar, 20_000 * money.Dollar, 20_000 * money.Dollar},
			[]money.USCents{8_599, 15_001, 15_000, 15_000}, []money.USCents{8_600, 15_000, 15_000, 15_000}, 1},
		"Exception 4 in June": {ex4, nil,
			[]money.USCents{15_000, 479, 30_000, 15_000}, []money.USCents{15_000, 480, 30_000, 15_000}, 2},
		"Exception 2 in January": {ex2, nil,
			[]money.USCents{10_500, 10_500, 10_500, 10_499}, []money.USCents{10_500, 10_500, 10_500, 10_500}, 4},
	} {
		for _, run := range []struct {
			payments []money.USCents
			want     estimated.Status
		}{{tc.short, estimated.StatusShort}, {tc.exact, estimated.StatusExcused}} {
			h := household{tax: 750 * money.Dollar, payments: quarterly(run.payments...), extras: &tc.extras, annualised: tc.annual}
			s := must(rules(t).Schedule(h.facts(t))).Value()
			if got := s[tc.quarter-1].Status(); got != run.want {
				t.Errorf("%s, paid %v: quarter %d is %v, want %v", name, run.payments, tc.quarter, got, run.want)
			}
		}
	}
}

// Nothing paid on $8,000 required: $2,000 a quarter at 10% for 2, 3 and 4
// months. The months from January 16, 2027 carry the 2027 rate, which is not
// published, so they are not charged. New Jersey's rate changes only between
// years, and this is the boundary.
func TestEstimatedInterestArithmetic(t *testing.T) {
	res := penalty(t, household{tax: 10_000 * money.Dollar})
	want := []money.USCents{3_333, 10_000, 20_000}
	lines := res.Value().Lines()
	for i, w := range want {
		if lines[i].Total() != w || lines[i].Parts()[0].Rate() != money.Percent(10) {
			t.Errorf("period %d: %v; want %v at 10%%", i+1, lines[i].Total(), w)
		}
	}
	if res.Value().Total() != 33_333 {
		t.Errorf("interest %v, want $333.33", res.Value().Total())
	}
	// To December 31: September's period runs 3½ months, counted as 4.
	p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2026, 12, 31)))
	if l := p.Value().Lines()[2]; l.Parts()[0].Months() != 4 || l.Total() != 20_000 {
		t.Errorf("to December 31: September %d months, %v; want 4 and $200", l.Parts()[0].Months(), l.Total())
	}
	if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 7, 1))); p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
}

// April's $2,000 paid April 30 runs half a month, counted as one: $16.67.
func TestEstimatedPartOfAMonthCountsAsAMonth(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 4, 30), 2_000*money.Dollar), pay(dues[1], 2_000*money.Dollar), pay(dues[2], 2_000*money.Dollar), pay(dues[3], 2_000*money.Dollar)}}
	res := penalty(t, h)
	if l := res.Value().Lines(); len(l) != 1 || l[0].Parts()[0].Months() != 1 || res.Value().Total() != 1_667 {
		t.Errorf("interest %v over %v; want $16.67 for one month", res.Value().Total(), l)
	}
}

// The dates are the printed ones and none moves.
func TestEstimatedScheduleForTwentyTwentySix(t *testing.T) {
	s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t))).Value()
	for i, in := range s {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
		if want := money.USCents(i+1) * 2_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
}

// February 15, 2027 is Presidents' Day. Filed and paid by it, the January
// installment is not needed. Filed on Tuesday, February 16, it is by
// default, and the question is asked; the answer moves the day.
func TestEstimatedFebruaryFifteenth(t *testing.T) {
	if wd := day(2027, 2, 15).Weekday(); wd != time.Monday {
		t.Fatalf("February 15, 2027 is a %v", wd)
	}
	s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 12)}.facts(t))).Value()
	if s[3].Status() != estimated.StatusExcused {
		t.Errorf("filed February 12: January is %v", s[3].Status())
	}
	// The earlier quarters' $6,000 still runs from January 15 to the day the
	// return was paid; January's own $2,000 is not added to it.
	res := penalty(t, household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 12)})
	if l := res.Value().Lines(); res.Value().AsOf() != day(2027, 2, 12) || len(l) != 4 || l[3].Shortfall() != 6_000*money.Dollar {
		t.Errorf("filed February 12: to %v over %v; want the carried $6,000 in the last period", res.Value().AsOf(), l)
	}
	res = penalty(t, household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 16)})
	if len(res.Value().Lines()) != 4 || !because(res.Required(), working.PositionUnsettledRule, "Presidents' Day") {
		t.Errorf("filed February 16: %d lines, asked %v", len(res.Value().Lines()), because(res.Required(), working.PositionUnsettledRule, "Presidents' Day"))
	}
	moves := must(newjerseytaxcalc.NewEstimatedFacts().RefundableCredits(0).FebruaryFifteenthMoves(true).Build())
	if s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 16), extras: &moves}.facts(t))).Value(); s[3].Status() != estimated.StatusExcused {
		t.Error("filed February 16, moved: January is not excused")
	}
}

// NJ-2210 line 3 stops under $400; exactly $400 goes on. The filing
// threshold takes a low income out entirely.
func TestEstimatedThresholds(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{{960_001, true}, {960_000, false}} {
		if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld}.facts(t))); res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v", tc.withheld, res.Value().Exempt(), tc.exempt)
		}
	}
	for _, tc := range []struct {
		status filing.Status
		gross  money.USCents
		exempt bool
	}{
		{filing.Single, 1_000_000, true}, {filing.Single, 1_000_001, false},
		{filing.MarriedFilingSeparately, 1_000_000, true},
		{filing.MarriedFilingJointly, 2_000_000, true}, {filing.MarriedFilingJointly, 2_000_001, false},
		{filing.HeadOfHousehold, 2_000_000, true}, {filing.SurvivingSpouse, 2_000_000, true},
	} {
		if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, status: tc.status, gross: tc.gross}.facts(t))); res.Value().Exempt() != tc.exempt {
			t.Errorf("%v, gross %v: exempt %v, want %v", tc.status, tc.gross, res.Value().Exempt(), tc.exempt)
		}
	}
	// Each exception is the chosen option, at $0, labelled with its figures.
	for _, tc := range []struct {
		h     household
		label string
	}{
		{household{tax: 10_000 * money.Dollar, withheld: 960_001}, "Tax less withholding and credits under $400"},
		{household{tax: 10_000 * money.Dollar, gross: 1_000_000}, "Gross income of $10,000 or less"},
	} {
		res := must(rules(t).Requirement(tc.h.facts(t)))
		if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != tc.label {
			t.Errorf("chosen %v: %v at %v labelled %q, want %q", ok, c.Basis(), c.Amount(), c.Label(), tc.label)
		}
		if p := penalty(t, tc.h); p.Value().Reason() != tc.label+"." {
			t.Errorf("penalty reason %q", p.Value().Reason())
		}
	}
}

// The lesser of 80% of this year's tax and 100% of last year's, from a
// twelve-month return; zero when no return was required.
func TestEstimatedTargetOptions(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(7_999 * money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || res.Value().Target() != 7_999*money.Dollar {
		t.Errorf("chose %v for %v; want last year's $7,999", c.Basis(), res.Value().Target())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(8_001 * money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 8_000*money.Dollar {
		t.Errorf("chose %v for %v; want 80%%, $8,000", c.Basis(), res.Value().Target())
	}
	nr := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &nr}.facts(t))); res.Value().Target() != 0 {
		t.Errorf("not required to file last year: target %v, want zero", res.Value().Target())
	}
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(100).Build())).Build())
	for name, p := range map[string]*estimated.PriorReturn{"none on record": nil, "not filed": &notFiled, "eleven months": &short} {
		if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p}.facts(t))); res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
	}
}

// Last year's overpayment counts toward June, as NJ-2210 line 6 says, and
// the question is asked; answered, it counts toward April as Form
// NJ-1040-ES says.
func TestEstimatedPriorYearOverpayment(t *testing.T) {
	over := must(estimated.NewPayment().Amount(2_000 * money.Dollar).FromPriorYear().Build())
	payments := append([]estimated.Payment{over}, quarterly(0, 2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar)...)
	res := penalty(t, household{tax: 10_000 * money.Dollar, payments: payments})
	if l := res.Value().Lines(); len(l) == 0 || l[0].Installment() != 1 || l[0].Total() != 3_333 || !because(res.Required(), working.PositionUnsettledRule, "overpayment") {
		t.Errorf("counted in June: %v; want April short for two months, $33.33, and asked", l)
	}
	april := must(newjerseytaxcalc.NewEstimatedFacts().RefundableCredits(0).PriorYearOverpaymentInApril(true).Build())
	if res := penalty(t, household{tax: 10_000 * money.Dollar, payments: payments, extras: &april}); res.Value().Total() != 0 {
		t.Errorf("counted in April: interest %v", res.Value().Total())
	}
}

// Withholding and refundable credits count a quarter on each date.
func TestEstimatedWithholdingAndCredits(t *testing.T) {
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 8_000 * money.Dollar}); p.Value().Total() != 0 {
		t.Errorf("withholding spread: interest %v", p.Value().Total())
	}
	e := must(newjerseytaxcalc.NewEstimatedFacts().RefundableCredits(8_000 * money.Dollar).Build())
	if p := penalty(t, household{tax: 10_000 * money.Dollar, extras: &e}); p.Value().Total() != 0 {
		t.Errorf("credits spread: interest %v", p.Value().Total())
	}
	none := must(newjerseytaxcalc.NewEstimatedFacts().Build())
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &none}.facts(t))); !because(res.Required(), working.PositionJurisdictionFact, "refundable credits") {
		t.Error("refundable credits are not asked")
	}
}

// Without this year's tax, the target is last year's alone and the interest
// is the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	if r := res.Value(); !r.Required() || r.Target() != 8_000*money.Dollar || r.Options()[0].WhyNot() != estimated.NeedsThisYearTax || !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("required %v, target %v", r.Required(), r.Target())
	}
	if p := penalty(t, h); !(p.Value().UpperBound() || !p.Value().RatedThrough().IsZero()) || p.Value().Total() == 0 {
		t.Errorf("interest %v, upper bound %v", p.Value().Total(), p.Value().UpperBound())
	}
	none := household{noTax: true}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v", err)
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"The $400 boundary":                     {"under $400", "54A:8-4(a)"},
		"A twelve-month prior year":             {"full 12 months", "54A:9-6(c)(2)"},
		"A requirement that arises late":        {"by four", "54A:8-5(a)(2)-(4)"},
		"Exception 1 with no prior-year return": {"zero if no return was required", "54A:9-6(d)(1)(A)"},
		"110% in Exceptions 1 and 2":            {"at any income", "54A:9-6(d)(3)"},
		"When a year's rate starts":             {"January 16", "54:49-3"},
		"Last year's overpayment":               {"column B", "54A:9-6"},
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
		// Nothing paid, so a balance is open at the end of 2026 and only the
		// rate's start changes the figures.
		if d.Uses() != working.ReadingForm || d.AffectsHousehold() != (d.Rule() == "When a year's rate starts") {
			t.Errorf("%q uses %v, affects %v", d.Rule(), d.Uses(), d.AffectsHousehold())
		}
	}
	// Paid in full on time: no divergence affects the household.
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, payments: quarterly(2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar)}.facts(t)))
	for _, d := range res.Divergences() {
		if d.AffectsHousehold() {
			t.Errorf("paid in full: %q affects the household", d.Rule())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := newjerseytaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	if _, err := rules(t).Requirement(household{tax: 10_000 * money.Dollar, farmShare: 7000}.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer: %v, want ErrUnsupported", err)
	}
	if _, err := rules(t).Requirement(household{tax: 10_000 * money.Dollar, fishShare: 7000}.facts(t)); err != nil {
		t.Errorf("a fisher: %v", err)
	}
	this := must(estimated.NewFigures().Tax(100_000).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts("NJ", 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts("NJ", 2026).FilingStatus(filing.Single).ThisYear(this).State(1.5).Build()),
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
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$400", "80%", "10%", "part of a month", "February 15", "last year's income at this year's rates", "110%"} {
		if !strings.Contains(text, s) {
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("NJ"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}

func waived(t *testing.T, pos estimated.Positions) estimated.Penalty {
	t.Helper()
	return must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, positions: &pos}.facts(t), day(2027, 4, 15))).Value()
}

func retiredOn(on, born estimated.Date) estimated.Positions {
	return must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, on).Born(born).Build())
}

// New Jersey has no waiver of this interest: the Director may waive only
// interest above prime plus three points, which is the whole rate.
func TestEstimatedWaiverIsRefused(t *testing.T) {
	mustContain(t, "njsa-estimated-tax.txt", "may remit or waive the payment of any interest charge in excess of the rate of three percentage points above the prime rate")
	for name, pos := range map[string]estimated.Positions{
		"retired in 2026 at 64": retiredOn(day(2026, 6, 1), day(1962, 3, 1)),
		"a disaster":            must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()),
	} {
		p := waived(t, pos)
		if o, ok := p.WaiverOutcome(); !ok || !o.Refused() || !strings.Contains(o.Reason(), "three points over prime") || p.Total() == 0 || p.IfWaived() != p.Total() {
			t.Errorf("%s: outcome %v %+v, total %v, if waived %v", name, ok, o, p.Total(), p.IfWaived())
		}
	}
}

// Under an exception no question may say its answer changes the target, so
// last year's return isn't asked for.
func TestEstimatedExemptAsksNothingOfTheTarget(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 100 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() {
		t.Fatal("$100 of tax is not exempt")
	}
	if has(res.Required(), working.PositionPriorYearReturn) {
		t.Error("last year's return is asked for under an exception")
	}
	for _, q := range res.Required() {
		if strings.Contains(strings.ToLower(q.Effect()), "target") {
			t.Errorf("under an exception a question says %q", q.Effect())
		}
	}
}
