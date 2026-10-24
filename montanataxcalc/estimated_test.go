// Montana's estimated tax rules for 2026, checked against the Department of
// Revenue's own documents and the Montana Code Annotated.
//
// Montana publishes one worked interest example, Example 3 in the 2025 Form
// EST-I instructions. Its columns C and D (122 and 90 days on $3,000) carry
// over to 2026 unchanged and are reproduced to the cent. Its column B is
// recomputed for 2026's 92 days. Its column A does not reconcile with its own
// lines: it charges the whole $4,000 for both 15 and 62 days, and its $186.05
// total leaves out one of those charges. The instructions say to make a
// separate computation for each payment, so column A is charged here on the
// $2,000 each payment paid. Every threshold, rate, date and percentage is
// compared against its extract in testdata/. The other cases are
// hand-written.
package montanataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/montanataxcalc"
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
	return must(montanataxcalc.EstimatedTax(2026))
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

type household struct {
	tax         money.USCents
	noTax       bool
	withheld    money.USCents
	refundable  money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *montanataxcalc.EstimatedFacts
	people      []estimated.Person
	residence   []estimated.ResidencePeriod
	firstMet    estimated.Date
	returnFiled estimated.Date
	farmShare   money.Rate
	fishShare   money.Rate
	annualised  []money.USCents
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts("MT", 2026).FilingStatus(filing.Single).ThisYear(figures).
		Paid(h.payments...).People(h.people...).Residence(h.residence...).
		RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled).FarmShare(h.farmShare).FishShare(h.fishShare)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(montanataxcalc.NewEstimatedFacts().RefundableCredits(h.refundable).CompositeReturn(false).Build()))
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
	mustContain(t, "mca-15-30-2512.txt",
		"(i) 90% of the tax for the current tax year, less tax credits and withholding allowed the taxpayer; or",
		"(ii) an amount equal to 100% of the individual's tax liability for the preceding tax year, if the preceding tax year was a period of 12 months and if the individual filed a return for the tax year.",
		"is less than $500 after reductions for credits and withholding",
		"the individual retired in the tax year after having attained the age of 62 or if the individual became disabled in the tax year.",
		"First | April 15 | Second | June 15 | Third | September 15 | Fourth | January 15 of the following tax year |",
		"each installment must be 25% of the required annual payment",
		"the taxpayer shall pay 50% for that installment and 25% for the third and fourth installments, respectively.",
		"the taxpayer shall pay 75% for that installment and 25% for the fourth installment.",
		"First | 22.5% | Second | 45% | Third | 67.5% | Fourth | 90% |",
		"to the 15th day of the 4th month of the year following the tax year in which the payment was to be made, whichever is earlier.",
		"an estimated payment must be credited against unpaid required installments in the order in which those installments are required to be paid.",
		"the taxpayer files a return on or before the last day of the month following the close of the tax year",
		"has paid approximately equal quarterly installments of estimated taxes.",
		`"farmer or rancher" means a taxpayer who derives at least 66 2/3% of the taxpayer's gross income`,
	)
	mustContain(t, "mca-15-1-216.txt",
		"the interest rate is equal to the underpayment rate for individual taxpayers established by the secretary of the United States department of the treasury pursuant to section 6621 of the Internal Revenue Code, 26 U.S.C. 6621, for the third quarter of the preceding year.",
		"must be assessed a late payment penalty of 1.5% a month on the unpaid tax. The penalty may not exceed 15% of the tax due.",
	)
	mustContain(t, "irs-rev-rul-2026-10.txt", "an underpayment rate of 7 percent are established for the calendar quarter beginning July 1, 2026.")
	mustContain(t, "publication-1-2026-estimated.txt",
		"is more than $500.",
		"You may only use this method if you filed a return for the preceding tax year. You cannot use this method if your preceding return was a short year.",
		"If any of these dates fall on a weekend or a holiday, your payment is due on the next business day.",
		"interest on underpayment of estimated taxes for tax year 2026 will be calculated using the interest rate effective for 2027",
		"(Montana does not apply a “lookback” provision to determine farming and ranching gross income.)",
	)
	mustContain(t, "est-i-2025.txt",
		"Multiply line 1 by 90% (0.90)",
		"Subtract line 3 from line 1. If the result is less than $500, stop here; you do not owe interest on your underpayment of estimated tax.",
		"Add Part I, lines 3a and 3b. Then, divide by 4 and enter the result in each column",
		"Multiply line 10 by line 13. Then, multiply the result by 0.000192.",
		"As of January 1, 2026, the interest rate is 7 percent.",
		"1/1/25 to 1/1/25 to 1/1/25 to 1/1/25 to",
		"3/31/25 5/31/25 8/31/25 12/31/25",
		"Applicable percentage 17 22.5% 45% 67.5% 90%",
	)
	mustContain(t, "est-i-2025-instructions.txt",
		"Your 2024 tax period covered 12 months and your Montana tax liability was zero.",
		"You are a nonresident or part-year resident and were not required to file a 2024 Montana income tax return.",
		"You retired and were at least 62 years of age.",
		"At least two-thirds of your gross income is derived from farming or ranching operations.",
		"If you filed your tax return by January 31 and paid the amount due in full, as computed on your tax return, there is no underpayment interest penalty calculated on the fourth installment payment.",
		"Your payments are applied first to any underpayment balance on an earlier period",
		"If more than one payment was required to fully satisfy an underpayment amount, make a separate computation for each payment.",
	)
	mustContain(t, "mca-1-1-216.txt", "(a) each Sunday;", "(f) Independence Day, July 4;", "fall on a Saturday, the Friday preceding is a holiday.")
	mustContain(t, "mca-1-1-307.txt", "such act may be performed upon the next business day with the same effect as if it had been performed upon the day appointed.")
	mustContain(t, "dor-interest-and-penalties.txt", "1.5% per month, up to 15% of the tax due for: Estimated Taxes and Withholding Taxes", "is 7%, computed daily at 0.019178%.")
}

// EST-I's Example 3 with the 2026 dates: $4,000 required each quarter;
// $2,000 paid April 30, $3,000 June 15, $4,000 September 15, $4,000 January
// 15 and $3,000 April 15, 2027.
func TestEstimatedInterestFollowsTheFormExample(t *testing.T) {
	mustContain(t, "est-i-2025.txt",
		"Your required installment payment for each due date is $4,000. You made five payments: $2,000 on April 30, 2025; $3,000 on June 16, 2025; $4,000 on September 15, 2025; $4,000 on January 15, 2026; and $3,000 on April 15, 2026.",
		"62 91 122 90",
		"$47.62 $52.42 $70.27 $51.84",
		"$186.05",
	)
	h := household{tax: 20_000 * money.Dollar, prior: prior(16_000 * money.Dollar), payments: []estimated.Payment{
		pay(day(2026, 4, 30), 2_000*money.Dollar), pay(dues[1], 3_000*money.Dollar), pay(dues[2], 4_000*money.Dollar),
		pay(dues[3], 4_000*money.Dollar), pay(day(2027, 4, 15), 3_000*money.Dollar)}}
	res := penalty(t, h)
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	want := []struct {
		short   money.USCents
		days    []int
		charges []money.USCents
	}{
		{400_000, []int{15, 61}, []money.USCents{576, 2_342}},
		{300_000, []int{92}, []money.USCents{5_299}},
		{300_000, []int{122}, []money.USCents{7_027}}, // the example's $70.27
		{300_000, []int{90}, []money.USCents{5_184}},  // the example's $51.84
	}
	for i, l := range lines {
		parts := l.Parts()
		if l.Shortfall() != want[i].short || len(parts) != len(want[i].days) {
			t.Errorf("column %d: %v in %d parts; want %v in %d", i+1, l.Shortfall(), len(parts), want[i].short, len(want[i].days))
			continue
		}
		for j, p := range parts {
			if p.Days() != want[i].days[j] || p.Charge() != want[i].charges[j] {
				t.Errorf("column %d part %d: %d days %v; want %d days %v", i+1, j, p.Days(), p.Charge(), want[i].days[j], want[i].charges[j])
			}
		}
	}
	if res.Value().Total() != 20_428 {
		t.Errorf("interest %v, want $204.28", res.Value().Total())
	}
}

// Nothing paid on $9,000 required: each quarter's $2,250 to April 15, 2027
// at 0.000192 a day. Montana charges one rate for the whole year, 2027's, so
// the first quarter's 365 days are one part across the calendar year.
func TestEstimatedInterestArithmetic(t *testing.T) {
	res := penalty(t, household{tax: 10_000 * money.Dollar})
	wantDays := []int{365, 304, 212, 90}
	wantCharge := []money.USCents{15_768, 13_133, 9_158, 3_888}
	for i, l := range res.Value().Lines() {
		parts := l.Parts()
		if len(parts) != 1 || parts[0].Days() != wantDays[i] || parts[0].Charge() != wantCharge[i] || parts[0].Rate() != money.Percent(7) || !strings.Contains(parts[0].Source(), "2027") {
			t.Errorf("column %d: %d parts, first %d days %v; want one of %d days, %v at the 2027 rate", i+1, len(parts), parts[0].Days(), parts[0].Charge(), wantDays[i], wantCharge[i])
		}
	}
	if res.Value().Total() != 41_947 {
		t.Errorf("interest %v, want $419.47", res.Value().Total())
	}
	if !because(res.Required(), working.PositionUnpublishedRate, "third quarter of 2026, 7%") {
		t.Error("the department's unpublished 2027 rate is not reported")
	}
	if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 6, 1))); p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
}

// The 2026 dates are the statute's and none moves. January 31, 2027 is a
// Sunday, a Montana legal holiday, so § 1-1-307 moves the January rule to
// Monday, February 1.
func TestEstimatedScheduleAndTheJanuaryRule(t *testing.T) {
	s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t))).Value()
	for i, in := range s {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
	}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 1, 31), true}, {day(2027, 2, 1), true}, {day(2027, 2, 2), false}} {
		s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: tc.filed}.facts(t))).Value()
		if got := s[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
	}
	// Filed and paid February 1: the first three quarters run to that day.
	res := penalty(t, household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 1)})
	if res.Value().AsOf() != day(2027, 2, 1) || len(res.Value().Lines()) != 3 {
		t.Errorf("filed February 1: to %v over %d lines", res.Value().AsOf(), len(res.Value().Lines()))
	}
}

// $500 or more after withholding and refundable credits.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld, refundable money.USCents
		exempt               bool
	}{{950_001, 0, true}, {950_000, 0, false}, {900_000, 50_001, true}, {900_000, 50_000, false}} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld, refundable: tc.refundable}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v, refundable %v: exempt %v, want %v", tc.withheld, tc.refundable, res.Value().Exempt(), tc.exempt)
		}
	}
}

// The lesser of 90% of this year's tax and 100% of last year's, which needs a
// filed twelve-month return.
func TestEstimatedTargetOptions(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(8_000 * money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || c.Percent() != money.Percent(100) || res.Value().Target() != 8_000*money.Dollar {
		t.Errorf("chose %v at %v for %v; want 100%% of $8,000", c.Basis(), c.Percent(), res.Value().Target())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(9_500 * money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 9_000*money.Dollar {
		t.Errorf("chose %v for %v; want 90%%, $9,000", c.Basis(), res.Value().Target())
	}
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(100).Build())).Build())
	for name, p := range map[string]*estimated.PriorReturn{"none on record": nil, "not filed": &notFiled, "eleven months": &short} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p}.facts(t)))
		if res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
	}
}

// The exemptions: farmers and ranchers, retirement after 62 or disability, a
// composite return, no liability last year, and a nonresident not required to
// file last year.
func TestEstimatedExemptions(t *testing.T) {
	retired := must(estimated.NewPerson(64).RetiredAfter62().Build())
	disabled := must(estimated.NewPerson(40).Disabled().Build())
	composite := must(montanataxcalc.NewEstimatedFacts().RefundableCredits(0).CompositeReturn(true).Build())
	nr := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	partYear := must(estimated.NewResidencePeriod("MT").From(day(2026, 6, 1)).To(day(2026, 12, 31)).Build())
	fullYear := must(estimated.NewResidencePeriod("MT").From(day(2026, 1, 1)).To(day(2026, 12, 31)).Build())
	for name, tc := range map[string]struct {
		h      household
		exempt bool
	}{
		"farmer":                          {household{tax: 10_000 * money.Dollar, farmShare: 6_667}, true},
		"farm share under two-thirds":     {household{tax: 10_000 * money.Dollar, farmShare: 6_666}, false},
		"fisher":                          {household{tax: 10_000 * money.Dollar, fishShare: 9_000}, false},
		"retired after 62":                {household{tax: 10_000 * money.Dollar, people: []estimated.Person{retired}}, true},
		"disabled":                        {household{tax: 10_000 * money.Dollar, people: []estimated.Person{disabled}}, true},
		"composite return":                {household{tax: 10_000 * money.Dollar, extras: &composite}, true},
		"no liability last year":          {household{tax: 10_000 * money.Dollar, prior: prior(0)}, true},
		"part-year, not required":         {household{tax: 10_000 * money.Dollar, prior: &nr, residence: []estimated.ResidencePeriod{partYear}}, true},
		"full-year resident not required": {household{tax: 10_000 * money.Dollar, prior: &nr, residence: []estimated.ResidencePeriod{fullYear}}, true},
	} {
		res := must(rules(t).Requirement(tc.h.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", name, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
	}
	// The threshold exception is the chosen option, at $0, with its figures.
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: 960_000}.facts(t)))
	if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding and refundable credits under $500" {
		t.Errorf("chosen %v: %v at %v labelled %q", ok, c.Basis(), c.Amount(), c.Label())
	}
	if !strings.Contains(res.Value().Why(), "leaves $400, under $500") {
		t.Errorf("why %q", res.Value().Why())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, farmShare: 6_667}.facts(t)))
	if !strings.Contains(res.Value().Why(), "farming or ranching") {
		t.Errorf("farmer: why %q", res.Value().Why())
	}
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisException || c.Label() != "Two-thirds or more of your income from farming or ranching" {
		t.Errorf("farmer: chosen %v labelled %q", c.Basis(), c.Label())
	}
	if p := penalty(t, household{tax: 10_000 * money.Dollar, farmShare: 6_667}); p.Value().Reason() != "Two-thirds or more of your income from farming or ranching." {
		t.Errorf("farmer: penalty reason %q", p.Value().Reason())
	}
	// Not required to file and no residence given: the no-liability test
	// applies to a twelve-month year, and residence is not needed.
	notRequiredShort := must(estimated.NewPriorReturn().NotRequiredToFile().Months(6).Build())
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &notRequiredShort}.facts(t)))
	if res.Value().Exempt() || !has(res.Required(), working.PositionResidency) {
		t.Errorf("not required to file for six months, residence not given: exempt %v, asked %v", res.Value().Exempt(), has(res.Required(), working.PositionResidency))
	}
}

// A requirement that arises after April 15 catches up at June 15 with 50%,
// then 25% and 25%. One that arises on April 15 itself is not after it.
func TestEstimatedLateStarter(t *testing.T) {
	s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, firstMet: day(2026, 6, 1)}.facts(t))).Value()
	for i, want := range []money.USCents{0, 4_500 * money.Dollar, 6_750 * money.Dollar, 9_000 * money.Dollar} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, s[i].NeededBy(), want)
		}
	}
	s = must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, firstMet: day(2026, 7, 1)}.facts(t))).Value()
	if s[1].NeededBy() != 0 || s[2].NeededBy() != 6_750*money.Dollar {
		t.Errorf("first met July 1: June needs %v, September %v; want nothing and 75%%", s[1].NeededBy(), s[2].NeededBy())
	}
	s = must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, firstMet: day(2026, 4, 15)}.facts(t))).Value()
	if s[0].NeededBy() != 2_250*money.Dollar {
		t.Errorf("first met April 15: April needs %v, want $2,250", s[0].NeededBy())
	}
	if p := penalty(t, household{tax: 10_000 * money.Dollar}); !has(p.Required(), working.PositionRequirementFirstMet) {
		t.Error("when the requirement arose is not asked")
	}
}

// The annualized method: 22.5%, 45%, 67.5% and 90%, with recapture.
func TestEstimatedAnnualizedMethod(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, annualised: []money.USCents{2_000 * money.Dollar, 4_000 * money.Dollar, 8_000 * money.Dollar, 10_000 * money.Dollar}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{450 * money.Dollar, 1_800 * money.Dollar, 5_400 * money.Dollar, 9_000 * money.Dollar} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, s[i].NeededBy(), want)
		}
	}
}

// § 15-30-2512(5)(e): payments reaching the target in approximately equal
// installments owe nothing. EST-I omits it, so it is asked and applied only
// when answered.
func TestEstimatedApproximatelyEqualInstallments(t *testing.T) {
	late := []estimated.Payment{pay(day(2026, 4, 25), 2_250*money.Dollar), pay(day(2026, 6, 25), 2_250*money.Dollar), pay(day(2026, 9, 25), 2_250*money.Dollar), pay(day(2027, 1, 25), 2_250*money.Dollar)}
	res := penalty(t, household{tax: 10_000 * money.Dollar, payments: late})
	if res.Value().Total() == 0 || !because(res.Required(), working.PositionJurisdictionFact, "approximately equal") {
		t.Errorf("not answered: interest %v, asked %v", res.Value().Total(), because(res.Required(), working.PositionJurisdictionFact, "approximately equal"))
	}
	e := must(montanataxcalc.NewEstimatedFacts().RefundableCredits(0).CompositeReturn(false).ApproximatelyEqualInstallments(true).Build())
	if p := penalty(t, household{tax: 10_000 * money.Dollar, payments: late, extras: &e}); p.Value().Total() != 0 {
		t.Errorf("answered: interest %v", p.Value().Total())
	}
	// The answer does not help when the year's payments fall short.
	if p := penalty(t, household{tax: 10_000 * money.Dollar, payments: late[:3], extras: &e}); p.Value().Total() == 0 {
		t.Error("three of four payments: no interest")
	}
}

// The open questions a non-zero interest carries.
func TestEstimatedOpenQuestions(t *testing.T) {
	res := penalty(t, household{tax: 10_000 * money.Dollar})
	if !because(res.Required(), working.PositionUnsettledRule, "1.5% a month") {
		t.Error("the late payment penalty is not reported as unsettled")
	}
	if !because(res.Required(), working.PositionJurisdictionFact, "retire after reaching 62") {
		t.Error("retirement after 62 is not asked")
	}
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the casualty waiver is not asked")
	}
	none := must(montanataxcalc.NewEstimatedFacts().Build())
	res = penalty(t, household{tax: 10_000 * money.Dollar, extras: &none})
	if !because(res.Required(), working.PositionJurisdictionFact, "composite return") || !because(res.Required(), working.PositionJurisdictionFact, "refundable credits") {
		t.Error("composite return or refundable credits are not asked")
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
	if p := penalty(t, h); !p.Value().UpperBound() || p.Value().Total() == 0 {
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
		"The daily factor":                 {"0.000192", "§ 15-1-216(7)(a)(i)"},
		"No liability last year":           {"was zero", "§ 15-30-2512(1)(b)(ii)"},
		"Not required to file last year":   {"nonresident or part-year resident", "ARM 42.17.314(1)"},
		"When payments are required":       {"more than $500", "§ 15-30-2512(1)(b)(i)"},
		"Retirement after 62":              {"at the end of the previous tax year", "§ 15-30-2512(1)(b)(iv)"},
		"Approximately equal installments": {"no exception", "§ 15-30-2512(5)(e)"},
		"The late payment penalty":         {"interest only", "§ 15-1-216(2)(b)"},
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
		// Nothing paid, so every installment is short: the daily factor and
		// the late payment penalty change the figures, and the exceptions
		// this household does not claim do not.
		wantUses := working.ReadingForm
		if d.Rule() == "Retirement after 62" {
			wantUses = working.ReadingStatute
		}
		wantAffects := d.Rule() == "The daily factor" || d.Rule() == "The late payment penalty"
		if d.Uses() != wantUses || d.AffectsHousehold() != wantAffects {
			t.Errorf("%q uses %v, affects %v; want %v, %v", d.Rule(), d.Uses(), d.AffectsHousehold(), wantUses, wantAffects)
		}
	}
	// Exactly $500 after withholding: only the readings of the threshold
	// differ.
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: 950_000}.facts(t)))
	for _, d := range res.Divergences() {
		if d.Rule() == "When payments are required" && !d.AffectsHousehold() {
			t.Error("exactly $500: the threshold divergence does not affect the household")
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := montanataxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100_000).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts("MT", 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts("MT", 2026).FilingStatus(filing.Single).ThisYear(this).State(3).Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}
	if _, err := montanataxcalc.NewEstimatedFacts().RefundableCredits(-1).Build(); err == nil {
		t.Error("negative refundable credits are accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$500", "110%", "retired after 62", "2027 rate", "February 1"} {
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("MT"), 2026).FilingStatus(filing.Single).
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

// Montana's window and age are the federal ones, but retiring after 62 or
// becoming disabled needs no reasonable cause, and a casualty needs good
// cause.
func TestEstimatedWaiverDiffersFromTheFederalOne(t *testing.T) {
	mustContain(t, "mca-15-30-2512.txt",
		"the underpayment was caused by reason of casualty, disaster, or other unusual circumstances that the department determines to constitute good cause",
		"the individual retired in the tax year after having attained the age of 62 or if the individual became disabled in the tax year",
		"not required in the tax year following the tax year in which the individual retired or became disabled")
	p := waived(t, retiredOn(day(2025, 6, 1), day(1962, 3, 1)))
	if o, ok := p.WaiverOutcome(); !ok || !o.Eligible() || strings.Contains(o.Reason(), "reasonable cause") || !strings.Contains(o.Reason(), "the year after") || p.Total() == 0 || p.IfWaived() != 0 {
		t.Errorf("retired in 2025 at 63: outcome %v %+v, total %v, if waived %v", ok, o, p.Total(), p.IfWaived())
	}
	p = waived(t, retiredOn(day(2020, 6, 1), day(1955, 3, 1)))
	if o, ok := p.WaiverOutcome(); !ok || !o.Refused() || !strings.Contains(o.Reason(), "2025 or 2026") || p.IfWaived() != p.Total() {
		t.Errorf("retired in 2020: outcome %v %+v, if waived %v", ok, o, p.IfWaived())
	}
	p = waived(t, retiredOn(day(2026, 6, 1), day(1970, 3, 1)))
	if o, ok := p.WaiverOutcome(); !ok || !o.Refused() {
		t.Errorf("retired at 56: outcome %v %+v", ok, o)
	}
	p = waived(t, must(estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2026, 2, 1)).Build()))
	if o, ok := p.WaiverOutcome(); !ok || !o.Eligible() || strings.Contains(o.Reason(), "reasonable cause") {
		t.Errorf("disabled in 2026: outcome %v %+v", ok, o)
	}
	p = waived(t, must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()))
	if o, ok := p.WaiverOutcome(); !ok || !o.Eligible() || !strings.Contains(o.Reason(), "good cause") {
		t.Errorf("a disaster: outcome %v %+v", ok, o)
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
