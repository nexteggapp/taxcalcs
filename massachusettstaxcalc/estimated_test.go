// Massachusetts's estimated tax rules for 2026, checked against the
// Department of Revenue's own documents and the General Laws.
//
// Massachusetts publishes no exhaustive oracle and no worked example for
// estimated tax: neither Form 1-ES nor Form M-2210 works a case with an
// answer. The checks are therefore the documents' own words and line
// arithmetic: every threshold, percentage, date and rate is compared against
// its extract in testdata/, and the penalty cases below are worked by hand
// from M-2210 lines 13 to 24. They check what this package's author read the
// form to say, not an answer the department published.
package massachusettstaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/massachusettstaxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const ma = estimated.Jurisdiction("MA")

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
	return must(massachusettstaxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

func count(rs []working.Required, k working.PositionKind) int {
	n := 0
	for _, r := range rs {
		if r.Position() == k {
			n++
		}
	}
	return n
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
	tax         money.USCents // after credits, M-2210 line 3
	noTax       bool
	withheld    money.USCents
	withheldOn  estimated.Date
	asWithheld  bool
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *massachusettstaxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	annualised  []money.USCents // M-2210 Part 3 line 22 for each period
	periodEnds  []estimated.Date
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions
	farm, fish  money.Rate
}

var periodEnds = []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	fig := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		fig = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts(ma, 2026).FilingStatus(filing.Single).ThisYear(fig).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled).
		FarmShare(h.farm).FishShare(h.fish)
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
	} else {
		b.State(must(massachusettstaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).Build()))
	}
	el := estimated.NewElections()
	if h.asWithheld {
		el.WithholdingAsWithheld()
	}
	if h.annualised != nil {
		el.Annualised()
		ends := periodEnds
		if h.periodEnds != nil {
			ends = h.periodEnds
		}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
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

// prior is a filed twelve-month return by a full-year resident.
func prior(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).
		Figures(must(estimated.NewFigures().Tax(tax).Build())).FullYearResident(true).Build())
	return &p
}

func partYear(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).
		Figures(must(estimated.NewFigures().Tax(tax).Build())).FullYearResident(false).Build())
	return &p
}

func extras(t *testing.T, b *massachusettstaxcalc.EstimatedFactsBuilder) *massachusettstaxcalc.EstimatedFacts {
	t.Helper()
	e := must(b.PriorYearFarmerOrFisher(false).Build())
	return &e
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "form-1-es-2026.txt",
		"you must make estimated tax payments if you expect to owe more than $400 in taxes on income not subject to withholding",
		"The surtax threshold for tax year 2026 is $1,107,750.",
		"in equal installments on or before April 15, 2026; June 15, 2026; September 15, 2026; and January 15, 2027.",
		"Whenever a due date falls on a Saturday, Sunday or legal holiday, the filing and payment may be made on the next succeeding business day.",
		"June 15, if the change occurs between April 1 and May 31; September 15, if the change occurs between June 1 and August 31; next January 15, if the change occurs after August 31.",
		"in equal installments on the remaining payment dates",
		"enter 25%, 33%, 50% or 100%, respectively, of line 11",
		"All taxpayers subject to the 4% surtax must file their returns and make all payments electronically.",
	)
	for _, name := range []string{"form-m-2210-2025.txt", "form-m-2210-2026-draft.txt"} {
		mustContain(t, name,
			"Enter 80% of line 3 or 66.67% of line 3 if you are a qualified farmer or fisherman",
			"Enter the smaller of line 4 or line 5",
			"divided by the number of installments required for the year",
			"tax due after credits and withholding is $400 or less.",
			"You were a resident of Massachusetts for the full 12 months of the previous taxable year and were not liable for taxes.",
			"tax year was less than 12 months, do not complete line 5.",
			"you may apply an equal part of those taxes as payment on each required installment",
			"you may consider those amounts as payments on the dates they were actually withheld",
			"Overpayments used as payments of prior underpayment amounts do not decrease the actual underpayment amount but serve to reduce instead the period of underpayment subject to penalty.",
			"the 15th day of the fourth month after the close of the taxable year, whichever is earlier",
			"Jan. 1–March 31", "Jan. 1–May 31", "Jan. 1–August 31", "Jan. 1–December 31",
			"20% 40% 60% 80%",
			"Divide line 6 of Form M-2210 by 4",
			"Enter the smaller of line 26 or line 29 here and on Form M-2210, line 8",
			"You retired in", "after reaching age 62",
			"Your underpayment was by reason of casualty, disaster or unusual circumstance",
		)
	}
	mustContain(t, "form-m-2210-2026-draft.txt",
		"DRAFT – DO NOT FILE",
		"a. April 15, 2026",
		"b. June 15, 2026 c. September 15, 2026 d. January 15, 2027",
		"Number of days in line 15 after 4/15/26 and before 7/1/26",
		"Number of days in line 15 after 6/30/26 and before 10/1/26",
		"Number of days in line 15 after 9/30/26 and before 1/1/27",
		"Number of days in line 15 after 12/31/26 and before 4/15/27",
		"in line 16 ÷ 365) × 7%",
		"in line 17 ÷ 365) × 8%",
		"in line 18 ÷ 365) × 8%",
		"in line 19 ÷ 365) × X%",
		"$1,107,750 $1,107,750 $1,107,750 $1,107,750",
		"For tax year 2026 calendar year filers the 15th day of the fourth month after the close of the taxable year is April 15, 2027.",
	)
	mustContain(t, "ap-241.txt",
		"01/29/2025",
		"Between April 1 and May 31 | June 15 | September 15, January 15 of the following year |",
		"Between June 1 and August 31 | September 15 | January 15 of the following year |",
		"September 1 and thereafter | January 15 of the following year |",
		"Exception 1 - The tax due after withholding and credits is $400 or less.",
		"The 80% requirement is reduced to 66.67% for individuals who receive two‑thirds of their income from fishing or farming.",
	)
	mustContain(t, "tir-26-5.txt",
		"09/01/2026",
		"2026 | Fourth | 6% | 8% | Third | 6% | 8% | Second | 5% | 7% | First | 6% | 8% |",
		"The rate for underpayments pursuant to G.L. c. 62C, § 32, is the Federal short-term rate plus four percentage points, compounded daily.",
	)
	mustContain(t, "mgl-62b-13.txt",
		"for whom the amount of estimated tax is more than $400 shall make payments of estimated tax")
	mustContain(t, "mgl-62b-14.txt",
		"at the rate but without daily compounding and on a per annum basis established under section thirty-two of chapter 62C",
		"the period of the underpayment shall run from the due date for the installment to the fifteenth day of the fourth month following the close of the taxable year",
		"A payment of estimated tax shall be credited against unpaid required installments in the order in which such installments are required to be paid.",
		"there shall be four required installments for each taxable year",
		"The amount of any required installment shall be twenty-five per cent of the required annual payment.",
		"(i) eighty per cent, or sixty-six and two-thirds per cent in the case of a farmer or fisherman",
		"(ii) one hundred per cent of the tax shown on the return of the taxpayer for the preceding taxable year, provided the taxpayer filed a return for the preceding taxable year and such preceding year was a taxable year of twelve months.",
		"is less than $400",
		"the taxpayer was an inhabitant of the commonwealth throughout the preceding taxable year",
		"retired after having attained age sixty-two or became disabled",
		"an equal part of such amount shall be deemed paid on each installment date",
		"is at least two-thirds of his total gross income for the taxable year",
	)
	mustContain(t, "mgl-62c-32a.txt",
		"the Federal short-term rate determined under section 6621(b) of the Internal Revenue Code, as amended and in effect for the taxable year, plus four percentage points, compounded daily")
	mustContain(t, "mgl-4-7-eighteenth.txt",
		"January first, June nineteenth, July fourth, November eleventh, and Christmas Day, or the day following when any of said days occurs on Sunday",
		"the third Monday in January, the third Monday in February, the third Monday in April, the last Monday in May, the first Monday in September, the second Monday in October, and Thanksgiving Day",
		"Bunker Hill Day, on June seventeenth",
	)
}

// nthMonday is the nth Monday of a month, or the last for n = -1.
func nthMonday(year int, m time.Month, n int) estimated.Date {
	if n < 0 {
		d := day(year, m+1, 1).AddDays(-1)
		for d.Weekday() != time.Monday {
			d = d.AddDays(-1)
		}
		return d
	}
	d := day(year, m, 1)
	for d.Weekday() != time.Monday {
		d = d.AddDays(1)
	}
	return d.AddDays(7 * (n - 1))
}

// The 2026 dates are the ones Form 1-ES and the draft M-2210 print. None is
// a weekend or a Massachusetts legal holiday under c. 4 § 7, so none moves:
// Patriots' Day is April 20, 2026, and the June holidays are June 17 and 19.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	holidays := []estimated.Date{
		nthMonday(2026, time.April, 3), nthMonday(2026, time.May, -1), day(2026, 6, 17), day(2026, 6, 19),
		day(2026, 7, 4), nthMonday(2026, time.September, 1), nthMonday(2026, time.October, 2), day(2026, 11, 11),
		day(2026, 12, 25), day(2027, 1, 1), nthMonday(2027, time.January, 3),
	}
	if holidays[0] != day(2026, 4, 20) {
		t.Errorf("Patriots' Day 2026 is %v, want April 20", holidays[0])
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

// January 31, 2027 is a Sunday, and Massachusetts has no rule for it: filing
// and paying by then does not excuse the January installment. The penalty
// stops on the day the return is paid, and never runs past April 15, 2027.
func TestEstimatedNoJanuaryRelief(t *testing.T) {
	h := household{tax: 5_000 * money.Dollar, returnFiled: day(2027, 1, 31)}
	sched := must(rules(t).Schedule(h.facts(t)))
	if s := sched.Value()[3].Status(); s != estimated.StatusShort {
		t.Errorf("January is %v after a return filed January 31; want short", s)
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if p.Value().AsOf() != day(2027, 1, 31) {
		t.Errorf("figured to %v, want the day the return was paid", p.Value().AsOf())
	}
	p = must(rules(t).Penalty(household{tax: 5_000 * money.Dollar}.facts(t), day(2027, 6, 30)))
	if p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
}

// The form's $400 floor: $400.00 after credits and withholding owes no
// penalty, $400.01 does.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{60_000, true},  // $400.00 left
		{59_999, false}, // $400.01
		{60_001, true},  // $399.99
	} {
		res := must(rules(t).Requirement(household{tax: 1_000 * money.Dollar, withheld: tc.withheld}.facts(t)))
		if got := res.Value().Exempt(); got != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, got, tc.exempt, res.Value().Why())
		}
		// The exception is the chosen option, at $0.
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding of $400 or less") {
			t.Errorf("withheld %v: chosen %v %q at %v", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
		// Only exactly $400 left reads differently under the statute.
		for _, d := range res.Divergences() {
			if d.Rule() == "The $400 floor" && d.AffectsHousehold() != (tc.withheld == 60_000) {
				t.Errorf("withheld %v: the $400 floor affects the household %v", tc.withheld, d.AffectsHousehold())
			}
		}
		if !res.Value().Exempt() && !res.Value().Required() {
			t.Errorf("withheld %v: $400.01 left owes estimates under § 13", tc.withheld)
		}
	}
}

// The target is the lesser of 80% of this year's tax and 100% of last
// year's, with no 110% step at any income.
func TestEstimatedTargetOptions(t *testing.T) {
	// 80% of $10,000 is $8,000; last year's $7,999.99 is less.
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(799_999)}.facts(t)))
	r := res.Value()
	if c, _ := r.Chosen(); c.Basis() != estimated.BasisPriorYearTax || r.Target() != 799_999 || c.Percent() != money.Percent(100) {
		t.Errorf("chose %v at %v, target %v; want last year's $7,999.99 at 100%%", c.Basis(), c.Percent(), r.Target())
	}
	if o := r.Options()[0]; o.Percent() != money.Percent(80) || o.Amount() != 8_000*money.Dollar {
		t.Errorf("this year's option is %v of %v, want 80%% and $8,000", o.Percent(), o.Amount())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(800_001)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 8_000*money.Dollar {
		t.Errorf("chose %v, target %v; want this year's $8,000", c.Basis(), res.Value().Target())
	}

	// A $2,000,000 earner keeps 100%.
	rich := must(estimated.NewPriorReturn().Filed(true).FullYearResident(true).
		Figures(must(estimated.NewFigures().Tax(100_000 * money.Dollar).FederalAGI(2_000_000 * money.Dollar).StateAGI(2_000_000 * money.Dollar).Build())).Build())
	res = must(rules(t).Requirement(household{tax: 200_000 * money.Dollar, prior: &rich}.facts(t)))
	if res.Value().Target() != 100_000*money.Dollar {
		t.Errorf("target %v at high income, want 100%% of last year's $100,000", res.Value().Target())
	}

	notFiled := must(estimated.NewPriorReturn().Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(100).Build())).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   working.PositionKind
	}{
		"no return on record": {nil, working.PositionPriorYearReturn},
		"not filed":           {&notFiled, working.PositionUnspecified},
		"eleven months":       {&short, working.PositionUnspecified},
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

// No source says whether a part-year return opens the prior-year test. It is
// offered and asked about, and the caller can withdraw it.
func TestEstimatedPartYearPriorReturn(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: partYear(1_000 * money.Dollar)}.facts(t)))
	if !res.Value().Options()[1].Available() || res.Value().Target() != 1_000*money.Dollar {
		t.Errorf("part-year prior return: available %v, target %v; want offered at $1,000", res.Value().Options()[1].Available(), res.Value().Target())
	}
	if !has(res.Required(), working.PositionJurisdictionFact) {
		t.Error("the part-year reading is not asked")
	}
	no := extras(t, massachusettstaxcalc.NewEstimatedFacts().PartYearPriorReturnCounts(false))
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: partYear(1_000 * money.Dollar), extras: no}.facts(t)))
	if res.Value().Options()[1].Available() || res.Value().Target() != 8_000*money.Dollar || has(res.Required(), working.PositionJurisdictionFact) {
		t.Errorf("withdrawn: available %v, target %v, asked %v", res.Value().Options()[1].Available(), res.Value().Target(), has(res.Required(), working.PositionJurisdictionFact))
	}
}

// § 14(d)(ii): no liability last year and residence all of it excuses the
// penalty. Without the residence it is asked, and a part-year resident does
// not get the exception.
func TestEstimatedNoLiabilityLastYear(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0)}.facts(t)))
	if !res.Value().Exempt() {
		t.Errorf("no liability last year: %s", res.Value().Why())
	}
	if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "No tax due last year as a full-year resident" {
		t.Errorf("no liability last year: chosen %v %q at %v", c.Basis(), c.Label(), c.Amount())
	}
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &notRequired}.facts(t))); !res.Value().Exempt() {
		t.Errorf("no return required last year: %s", res.Value().Why())
	}
	unanswered := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &unanswered}.facts(t)))
	if res.Value().Exempt() || !has(res.Required(), working.PositionResidency) {
		t.Errorf("residence not given: exempt %v, asked %v", res.Value().Exempt(), has(res.Required(), working.PositionResidency))
	}
	moved := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(false).Build())
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &moved}.facts(t))); res.Value().Exempt() || res.Value().Target() != 8_000*money.Dollar {
		t.Errorf("a new resident: exempt %v, target %v; want 80%% of this year's", res.Value().Exempt(), res.Value().Target())
	}
	// The exception needs nothing about this year, so its zero is exact.
	p := must(rules(t).Penalty(household{noTax: true, prior: prior(0)}.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 0 || p.Value().UpperBound() || p.Value().Reason() != "No tax due last year as a full-year resident." {
		t.Errorf("no liability, no tax given: %v, upper bound %v, %q; want an exact zero", p.Value().Total(), p.Value().UpperBound(), p.Value().Reason())
	}
}

// M-2210 lines 16 to 23 by hand: 80% of $5,000 is $4,000, so $1,000 a
// quarter, none paid. TIR 26-5's 7% for April to June and 8% after:
//
//	April 15:  76 days at 7% $14.58, 92 at 8% $20.16, 92 at 8% $20.16
//	June 15:   15 days at 7%  $2.88, 92 at 8% $20.16, 92 at 8% $20.16
//	September: 15 days at 8%  $3.29, 92 at 8% $20.16
//
// $121.55 in all. The days of 2027 have no published rate.
func TestEstimatedPenaltyAcrossRatePeriods(t *testing.T) {
	f := household{tax: 5_000 * money.Dollar}.facts(t)
	want := [][]money.USCents{{1_458, 2_016, 2_016}, {288, 2_016, 2_016}, {329, 2_016}}
	wantDays := [][]int{{76, 92, 92}, {15, 92, 92}, {15, 92}}
	wantRates := [][]money.Rate{{700, 800, 800}, {700, 800, 800}, {800, 800}}
	for _, asOf := range []estimated.Date{day(2026, 12, 31), day(2027, 4, 15)} {
		res := must(rules(t).Penalty(f, asOf))
		lines := res.Value().Lines()
		if len(lines) != 4 {
			t.Fatalf("%d lines, want four", len(lines))
		}
		for i := range want {
			parts := lines[i].Parts()
			if len(parts) != len(want[i]) {
				t.Errorf("line %d has %d parts, want %d", i+1, len(parts), len(want[i]))
				continue
			}
			for j, p := range parts {
				if p.Charge() != want[i][j] || p.Days() != wantDays[i][j] || p.Rate() != wantRates[i][j] {
					t.Errorf("line %d part %d: %v for %d days at %v; want %v for %d at %v", i+1, j+1, p.Charge(), p.Days(), p.Rate(), want[i][j], wantDays[i][j], wantRates[i][j])
				}
			}
		}
		if res.Value().Total() != 12_155 {
			t.Errorf("to %v the penalty is %v, want $121.55", asOf, res.Value().Total())
		}
		if got := has(res.Required(), working.PositionUnpublishedRate); got != (asOf.Year() == 2027) {
			t.Errorf("to %v, unpublished rate reported %v", asOf, got)
		}
		if asOf.Year() == 2027 && lines[0].Days() != 365 {
			t.Errorf("April's line counts %d days, want line 15's 365", lines[0].Days())
		}
	}
}

// A June shortfall paid July 15 spans the change from 7% to 8%: 15 days at
// 7% on $1,000 is $2.88, 15 days at 8% is $3.29. A payment pays the oldest
// shortfall first.
func TestEstimatedShortfallSpanningTwoRates(t *testing.T) {
	h := household{tax: 5_000 * money.Dollar, payments: []estimated.Payment{
		pay(dues[0], 1_000*money.Dollar), pay(day(2026, 7, 15), 1_000*money.Dollar),
		pay(dues[2], 1_000*money.Dollar), pay(dues[3], 1_000*money.Dollar),
	}}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	lines := p.Value().Lines()
	if len(lines) != 1 || lines[0].Installment() != 2 {
		t.Fatalf("lines %+v, want June's alone", lines)
	}
	parts := lines[0].Parts()
	if len(parts) != 2 || parts[0].Charge() != 288 || parts[1].Charge() != 329 || parts[0].Rate() != 700 || parts[1].Rate() != 800 {
		t.Errorf("June's parts %+v; want $2.88 at 7%% and $3.29 at 8%%", parts)
	}
	if p.Value().Total() != 617 {
		t.Errorf("penalty %v, want $6.17", p.Value().Total())
	}
}

// Withholding counts in equal parts on the four dates, whenever it was
// withheld, unless the filer shows the dates.
func TestEstimatedWithholding(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, withheld: 8_000 * money.Dollar, withheldOn: day(2026, 12, 1)}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("December withholding spread: penalty %v, want none", p.Value().Total())
	}
	h.asWithheld = true
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	if sched[0].Status() != estimated.StatusShort || sched[0].Shortfall() != 2_000*money.Dollar {
		t.Errorf("withholding on its date: April is %v, %v short; want $2,000 short", sched[0].Status(), sched[0].Shortfall())
	}
}

// Form 1-ES and AP 241 put a late starter on the remaining dates; the
// statute keeps four quarters. The default is the department's, asked about.
func TestEstimatedLateStart(t *testing.T) {
	// 80% of $5,000 is $4,000. From May 1 it is thirds from June 15.
	pays := []estimated.Payment{pay(dues[1], 133_333), pay(dues[2], 133_334), pay(dues[3], 133_333)}
	h := household{tax: 5_000 * money.Dollar, firstMet: day(2026, 5, 1), payments: pays}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{0, 133_333, 266_667, 400_000} {
		if got := sched.Value()[i].NeededBy(); got != want {
			t.Errorf("installment %d needs %v, want %v", i+1, got, want)
		}
	}
	if s := sched.Value()[0].Status(); s != estimated.StatusNothingDue {
		t.Errorf("April is %v, want nothing due", s)
	}
	if !has(sched.Required(), working.PositionJurisdictionFact) {
		t.Error("the late-start reading is not asked")
	}
	for _, d := range sched.Divergences() {
		if d.Rule() == "A requirement first met after March 31" && (!d.AffectsHousehold() || d.Uses() != working.ReadingForm) {
			t.Errorf("late start: affects %v, uses %v; want the form's reading, affecting this household", d.AffectsHousehold(), d.Uses())
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("thirds paid on time: penalty %v", p.Value().Total())
	}

	// The statute's reading: April is $1,000 short until June 15's payment,
	// 61 days at 7%, $11.70.
	h.extras = extras(t, massachusettstaxcalc.NewEstimatedFacts().LateStart(massachusettstaxcalc.LateStartFourQuarters))
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if lines := p.Value().Lines(); len(lines) == 0 || lines[0].Total() != 1_170 {
		t.Errorf("four quarters: lines %+v; want April's $11.70 first", lines)
	}
	if has(p.Required(), working.PositionJurisdictionFact) {
		t.Error("the reading is answered and still asked")
	}
	for _, d := range p.Divergences() {
		if d.Rule() == "A requirement first met after March 31" && d.Uses() != working.ReadingStatute {
			t.Errorf("four quarters chosen, but the figures use %v", d.Uses())
		}
	}

	// The groups: March 31 is the first, April 1 the second, June 1 the third
	// and September 1 the last.
	for _, tc := range []struct {
		met  estimated.Date
		need []money.USCents
	}{
		{day(2026, 3, 31), []money.USCents{100_000, 200_000, 300_000, 400_000}},
		{day(2026, 4, 1), []money.USCents{0, 133_333, 266_667, 400_000}},
		{day(2026, 5, 31), []money.USCents{0, 133_333, 266_667, 400_000}},
		{day(2026, 6, 1), []money.USCents{0, 0, 200_000, 400_000}},
		{day(2026, 8, 31), []money.USCents{0, 0, 200_000, 400_000}},
		{day(2026, 9, 1), []money.USCents{0, 0, 0, 400_000}},
	} {
		s := must(rules(t).Schedule(household{tax: 5_000 * money.Dollar, firstMet: tc.met}.facts(t))).Value()
		for i, want := range tc.need {
			if s[i].NeededBy() != want {
				t.Errorf("first met %v: installment %d needs %v, want %v", tc.met, i+1, s[i].NeededBy(), want)
			}
		}
	}

	// Not told when the requirement arose: four quarters, and asked.
	res := must(rules(t).Schedule(household{tax: 5_000 * money.Dollar}.facts(t)))
	if !has(res.Required(), working.PositionRequirementFirstMet) {
		t.Error("the date the requirement arose is not asked")
	}
}

// Part 3: 20%, 40%, 60% and 80% of the annualised tax, less earlier
// installments, never more than the regular quarter plus what was carried.
func TestEstimatedAnnualised(t *testing.T) {
	// 80% of $10,000 is $8,000, $2,000 a quarter. Annualised taxes of $2,000,
	// $5,000, $10,000 and $10,000 give $400, $1,600, $4,000 and $2,000.
	h := household{tax: 10_000 * money.Dollar, annualised: []money.USCents{2_000 * money.Dollar, 5_000 * money.Dollar, 10_000 * money.Dollar, 10_000 * money.Dollar},
		payments: quarterly(400*money.Dollar, 1_600*money.Dollar, 4_000*money.Dollar, 2_000*money.Dollar)}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{400, 2_000, 6_000, 8_000} {
		if got := sched.Value()[i].NeededBy(); got != want*money.Dollar {
			t.Errorf("installment %d needs %v, want $%d", i+1, got, want)
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("annualised and paid: penalty %v", p.Value().Total())
	}
	h.annualised = nil
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() == 0 {
		t.Error("the same payments owe nothing without annualising")
	}
	h.annualised = []money.USCents{1, 2, 3, 4}
	h.periodEnds = []estimated.Date{day(2026, 3, 31), day(2026, 6, 30), day(2026, 8, 31), day(2026, 12, 31)}
	if _, err := rules(t).Schedule(h.facts(t)); err == nil {
		t.Error("a period ending June 30 is accepted")
	}
}

// The commissioner decides a waiver, so the penalty is shown before it.
func TestEstimatedWaiver(t *testing.T) {
	f := household{tax: 5_000 * money.Dollar}.facts(t)
	p := must(rules(t).Penalty(f, day(2027, 4, 15)))
	if !has(p.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked")
	}
	k := estimated.WaiverCasualty
	p = must(rules(t).Penalty(household{tax: 5_000 * money.Dollar, waiver: &k}.facts(t), day(2027, 4, 15)))
	if has(p.Required(), working.PositionPenaltyWaiver) || p.Value().Waiver() != estimated.WaiverCasualty || p.Value().Total() != 12_155 {
		t.Errorf("casualty asked: waiver %v, total %v", p.Value().Waiver(), p.Value().Total())
	}
}

// Massachusetts grants the federal pair: a casualty or disaster, and
// retirement after 62 or disability in this year or last. The draft 2026
// Form M-2210 and § 14(e)(ii) state the window and the age.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "form-m-2210-2026-draft.txt", "You retired in 2025 or 2026 after reaching age 62, or you became disabled")
	mustContain(t, "mgl-62b-14.txt", "retired after having attained age sixty-two or became disabled in the taxable year for which estimated payments were required to be made or in the taxable year preceding such taxable year")
	for _, tc := range []struct {
		name  string
		pos   *estimated.PositionsBuilder
		state string
		want  string
	}{
		{"disaster", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)), "eligible", "a disaster"},
		{"retired in 2026 at 64", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)), "eligible", "retired in 2026 after reaching 62"},
		{"retired in 2025 at 61", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2025, 6, 1)).Born(day(1964, 3, 1)), "refused", "You were 61"},
		{"retired in 2020", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, 3, 1)), "refused", "retiring in 2025 or 2026"},
		{"retired, no birth date", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)), "undecided", "date of birth"},
	} {
		pos := must(tc.pos.Build())
		p := must(rules(t).Penalty(household{tax: 5_000 * money.Dollar, positions: &pos}.facts(t), day(2027, 4, 15))).Value()
		o, ok := p.WaiverOutcome()
		state := map[bool]string{true: "eligible"}[o.Eligible()] + map[bool]string{true: "refused"}[o.Refused()] + map[bool]string{true: "undecided"}[o.Undecided()]
		if !ok || state != tc.state || !strings.Contains(o.Reason(), tc.want) {
			t.Errorf("%s: outcome %v %s, %q", tc.name, ok, state, o.Reason())
		}
		want := p.Total()
		if tc.state == "eligible" {
			want = 0
		}
		if p.Total() != 12_155 || p.IfWaived() != want {
			t.Errorf("%s: total %v, if waived %v", tc.name, p.Total(), p.IfWaived())
		}
	}
}

// When the $400 exception sets the target, last year's return can't change
// it, so it isn't asked for.
func TestEstimatedExemptAsksNothingOfTheTarget(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 300 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() || has(res.Required(), working.PositionPriorYearReturn) {
		t.Errorf("exempt %v, asked %v", res.Value().Exempt(), res.Required())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][4]string{
		"The $400 floor":                         {"$400 or less", "Form M-2210", "under $400", "§ 14(d)(i)"},
		"Daily compounding":                      {"simple interest", "lines 20 to 23", "without compounding", "c. 62C § 32(a)"},
		"A requirement first met after March 31": {"remaining dates", "AP 241 § 241.3", "four installments, each 25%", "§ 14(c)"},
		"The last day charged":                   {"before 4/15/27", "line", "15th day of the fourth month", "§ 14(b)"},
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
		// $10,000 of tax, nothing paid: short on every date, not at the $400
		// floor, and not a late start.
		wantAffects := d.Rule() == "Daily compounding" || d.Rule() == "The last day charged"
		if d.AffectsHousehold() != wantAffects || d.Uses() != working.ReadingForm {
			t.Errorf("%q: affects %v, uses %v; want %v and the form", d.Rule(), d.AffectsHousehold(), d.Uses(), wantAffects)
		}
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.FormSource(), w[1]) ||
			!strings.Contains(d.Statute(), w[2]) || !strings.Contains(d.StatuteSource(), w[3]) {
			t.Errorf("%q does not pin both readings: form %q (%s), statute %q (%s)", d.Rule(), d.Form(), d.FormSource(), d.Statute(), d.StatuteSource())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := massachusettstaxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	for name, h := range map[string]household{
		"a farmer":                {tax: 100, farm: 7_000},
		"a fisherman":             {tax: 100, fish: 7_000},
		"farming and fishing":     {tax: 100, farm: 3_400, fish: 3_400},
		"a farmer on last year's": {tax: 100, extras: extras2(t)},
	} {
		if _, err := rules(t).Requirement(h.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"Pennsylvania's facts": must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":                 must(estimated.NewFacts(ma, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":         must(estimated.NewFacts(ma, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Massachusetts's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := massachusettstaxcalc.NewEstimatedFacts().LateStart(massachusettstaxcalc.LateStart(9)).Build(); err == nil {
		t.Error("an unknown late-start reading is accepted")
	}
}

func extras2(t *testing.T) *massachusettstaxcalc.EstimatedFacts {
	e := must(massachusettstaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(true).Build())
	return &e
}

// Without this year's tax the target is 100% of last year's, and the
// penalty against it is the most the penalty can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(4_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 4_000*money.Dollar {
		t.Errorf("required %v, target %v; want $4,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Error("this year's tax is not asked for")
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{1_000, 2_000, 3_000, 4_000} {
		if got := sched.Value()[i].NeededBy(); got != want*money.Dollar {
			t.Errorf("installment %d needs %v, want $%d", i+1, got, want)
		}
	}
	p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if !p.Value().UpperBound() || p.Value().Total() != 12_155 {
		t.Errorf("unpaid: %v, upper bound %v; want at most $121.55", p.Value().Total(), p.Value().UpperBound())
	}

	none := household{noTax: true}
	res = must(rules(t).Requirement(none.facts(t)))
	if res.Value().TargetKnown() || res.Value().Required() {
		t.Errorf("no tax and no prior return: target known %v", res.Value().TargetKnown())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// A typical filer is asked only what changes the figure.
func TestEstimatedAsksOnlyWhatMatters(t *testing.T) {
	f := household{tax: 10_000 * money.Dollar, prior: prior(9_000 * money.Dollar), firstMet: day(2026, 1, 1)}.facts(t)
	res := must(rules(t).Requirement(f))
	if len(res.Required()) != 0 {
		t.Errorf("asked %v", res.Required())
	}
	bare := must(estimated.NewFacts(ma, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Build())
	res = must(rules(t).Requirement(bare))
	if count(res.Required(), working.PositionJurisdictionFact) != 1 {
		t.Errorf("last year's farm share is not asked once: %v", res.Required())
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$400", "80%", "100% of last year's", "no 110% tier", "20%, 40%, 60% and 80%", "January 31", "7%", "8%", "$1,107,750", "dates left", "age 62"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// The README's example. April's $250 runs 61 days at 7% to June 15, $2.92;
// June's $500 runs 15 days at 7% and 77 at 8% to September 15, $1.44 and
// $8.44; September's $750 runs 15 days and 92 days at 8% to the end of
// 2026, $2.47 and $15.12. $30.39.
func TestEstimatedReadmeExample(t *testing.T) {
	f := household{tax: 12_000 * money.Dollar, withheld: 4_000 * money.Dollar, prior: prior(9_000 * money.Dollar),
		payments: quarterly(1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar)}.facts(t)
	if r := must(rules(t).Requirement(f)); r.Value().Target() != 9_000*money.Dollar {
		t.Errorf("target %v, want $9,000", r.Value().Target())
	}
	sched := must(rules(t).Schedule(f)).Value()
	for i, want := range []money.USCents{250, 500, 750, 1_000} {
		if sched[i].Shortfall() != want*money.Dollar {
			t.Errorf("installment %d is %v short, want $%d", i+1, sched[i].Shortfall(), want)
		}
	}
	if p := must(rules(t).Penalty(f, day(2027, 4, 15))); p.Value().Total() != 3_039 {
		t.Errorf("penalty %v, want $30.39", p.Value().Total())
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("MA"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}
