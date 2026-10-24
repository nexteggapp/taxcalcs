// Indiana's estimated tax rules for 2026, checked against the Department of
// Revenue's own documents.
//
// Indiana publishes no exhaustive oracle and no worked example that ends in a
// penalty figure. The strongest checks are the examples in the instructions
// for Schedules IT-2210 and IT-2210A whose inputs and answers both come from
// the department: Chris and Kate's 110% prior-year figure, Joe's late third
// payment, Dana's $30 net overpayment and the $80 and $50 of IT-2210A. Every
// threshold, percentage and date is compared against its extract in
// testdata/. The boundaries, the exceptions and the open questions are
// hand-written cases.
package indianataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/indianataxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const indiana = estimated.Jurisdiction("IN")

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
	return must(indianataxcalc.EstimatedTax(2026))
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
		if x > 0 {
			out = append(out, pay(dues[i], x))
		}
	}
	return out
}

type household struct {
	tax         money.USCents // state and county, IT-2210 line 3
	noTax       bool
	noLocal     bool
	status      filing.Status
	withheld    money.USCents
	withheldOn  estimated.Date
	asWithheld  bool
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *indianataxcalc.EstimatedFacts
	returnFiled estimated.Date
	residence   []estimated.ResidencePeriod
	annualised  []money.USCents // IT-2210A line 10, one per column
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions
	farm, fish  money.Rate
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	status := h.status
	if status == filing.Unspecified {
		status = filing.Single
	}
	b := estimated.NewFacts(indiana, 2026).FilingStatus(status).ThisYear(h.figures()).
		Paid(h.payments...).ReturnFiled(h.returnFiled).Residence(h.residence...).FarmShare(h.farm).FishShare(h.fish)
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
		b.State(must(indianataxcalc.NewEstimatedFacts().PassThroughEntityTaxCredit(0).Build()))
	}
	el := estimated.NewElections()
	if h.asWithheld {
		el.WithholdingAsWithheld()
	}
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		el.Annualised()
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

func (h household) figures() estimated.Figures {
	b := estimated.NewFigures()
	if !h.noTax {
		b.Tax(h.tax)
		if !h.noLocal {
			b.LocalTax(0)
		}
	}
	return must(b.Build())
}

// prior is a filed full-year resident return with its tax and federal AGI.
func prior(tax, agi money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).FederalAGI(agi).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(true).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "es40-2026.txt",
		"(R26 / 9-25)",
		"1st Installment payment due April 15, 2026 2nd Installment payment due June 15, 2026 3rd Installment payment due Sept. 15, 2026 4th Installment payment due Jan. 15, 2027",
		"The remaining three payments are due June 15 and Sept. 15, 2026, and Jan. 15, 2027.",
		"Amount of county income tax due (line C multiplied by the appropriate county tax rate(s))",
	)
	mustContain(t, "it2210-2025.txt",
		"(R27 / 9-25)",
		"Multiply line 3 by 90% (.90)",
		"2025 withholding tax and PTET credit",
		"Subtract line 5 from line 3 - If less than $1,000, STOP HERE! You do not owe a penalty",
		"Minimum required annual payment - Enter the lesser of line 4 or line 7 - If less than or equal to the amount on line 5, STOP HERE! You do not owe a penalty",
		"If your federal A G I for 2024 was more than $150,000 ($75,000 if married filing separately) and you are not subject to the special rules for farmers or fishermen, enter the result multiplied by 110% (1.10).",
		"If you did not file a 2024 tax return, enter “N/A” for line 7 and see instructions for line 8.",
		"Divide the amount on line 8 by four and enter the result in each column.",
		"you must divide line 8 by the number of installment periods during which you were a resident of Indiana.",
		"1st Period............ Jan. 1 to March 31 2nd Period........... April 1 to May 31 3rd Period............ June 1 to Aug. 31 4th Period............ Sept. 1 to Dec. 31",
		"divide the amount on line 5 by four and enter the result in each column.",
		"Payments made after the due dates are to be reported in the next column.",
		"This amount should then be added to line 16 in the next column after subtracting any underpayment(s) shown on line 19 in the previous column(s).",
		"The penalty is 10% of the underpayment for each underpaid installment period.",
		"Underpayment penalty. Multiply line 20 by 10%.",
		"If you file your individual income tax return and pay the tax due by Feb. 2, 2026, you will not be required to make a fourth installment estimated tax payment.",
		"Column B 2nd Installment June 16, 2025",
		"If at least two-thirds of your gross income for 2024 or 2025 was from farming or fishing, you have only one payment due date",
	)
	mustContain(t, "it2210a-2025.txt",
		"(R25 / 9-25)",
		"1-1 to 3-31 1-1 to 5-31 1-1 to 8-31 1-1 to 12-31",
		"2. Annualization amounts__________ 4.0 2.4 1.5 1.0",
		"11. Applicable installment percentages__ .225 .450 .675 .900",
		"Multiply line 26 by 10% (.10)",
	)
	mustContain(t, "ib03.txt",
		"Has a total unpaid state and local income tax liability that is $1,000 or more for the taxable year.",
		"Estimated tax installment payments are due on April 15, June 15, September 15, and January 15 of the following calendar year.",
		"If the due date falls on a national or state holiday, Saturday, or Sunday, payment is timely if it is postmarked by the",
		"(1) 90 percent of the tax shown on the current year’s return; (2) 100 percent of the tax shown on the previous year’s return.",
		"the amount is 110 percent of the tax shown on the previous year’s return instead of 100 percent.",
		"the taxes will be presumed to have been remitted evenly across all four quarters of the taxable year.",
		"Withholding payments will be treated as occurring on the date of actual withholding",
		"the penalty will be 10 percent of the underpayment for that period.",
		"If a payment is made after the installment due date, the payment is considered to be made in the following installment period.",
		"the overpayment will be applied first to the prior-period underpayments prior to any carryforward. However, this carryback will not reduce any penalties.",
		"pays the entire tax due by January 31, the taxpayer will not receive a penalty for the installment payment due January 15.",
		"Any installment payment postmarked after January 15 for the preceding calendar year will be either returned to the taxpayer or credited against the taxpayer's liability for the following year.",
		"a failure to allocate a payment properly will not result in a penalty.",
	)
	mustContain(t, "synopsis-2026-ic-6-3-4-4.1.txt",
		"Code: IC 6-3-4-4.1 Enrolled Act: SEA 243, Sec. 17 Effective date: July 1, 2026",
		"Clarifies that the estimated payment tax penalty for individual taxpayers is calculated by the rate prescribed in IC 6-8.1-10-2.1(b)",
	)
	// The statute could not be fetched; these are the research file's
	// verified quotes, copied into testdata and labelled as such.
	mustContain(t, "research-in-statute-quotes.txt",
		"COPIED FROM research/estimated-payments/in.md, NOT FROM A PRIMARY SOURCE.",
		"no such declaration shall be required if the estimated tax can reasonably be expected to be less than one thousand dollars ($1,000).",
		"Section 6654 ... shall be applied as if the tax liability for the preceding taxable year ... was zero dollars ($0).",
		"P.L.205-2025, SEC.10, effective 1-1-2026: the [IC-4.1](a)(3) zero prior-year rule",
		"the penalty described in subsection (a) is ten percent (10%) of",
	)
}

// The 2026 dates are the ones ES-40 prints, and none moves. The file-and-pay
// date, January 31, 2027, is a Sunday, and IB3 moves it to Monday, February 1.
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
	if wd := day(2027, 1, 31).Weekday(); wd != time.Sunday {
		t.Fatalf("January 31, 2027 is a %v", wd)
	}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 1, 29), true}, {day(2027, 2, 1), true}, {day(2027, 2, 2), false}} {
		s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: tc.filed}.facts(t))).Value()
		if got := s[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: fourth installment excused %v, want %v", tc.filed, got, tc.excused)
		}
	}
}

// IT-2210 line 6: no penalty below $1,000 of state and county tax less
// withholding and the pass-through credit. Exactly $1,000 is covered.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld, ptet money.USCents
		exempt         bool
	}{
		{900_001, 0, true},  // $999.99
		{900_000, 0, false}, // $1,000.00
		{800_000, 100_001, true},
		{800_000, 100_000, false},
	} {
		e := must(indianataxcalc.NewEstimatedFacts().PassThroughEntityTaxCredit(tc.ptet).Build())
		r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld, extras: &e}.facts(t))).Value()
		if r.Exempt() != tc.exempt {
			t.Errorf("withheld %v, credit %v: exempt %v, want %v (%s)", tc.withheld, tc.ptet, r.Exempt(), tc.exempt, r.Why())
		}
		if c, ok := r.Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding and credits under $1,000") {
			t.Errorf("withheld %v, credit %v: chose %v %q at %v, want the exception", tc.withheld, tc.ptet, c.Basis(), c.Label(), c.Amount())
		}
	}
}

// IT-2210's CAUTION box: 110% of last year's tax when last year's federal AGI
// was more than $150,000, or $75,000 married filing separately. Its example:
// Chris and Kate's $6,952 less $1,952 is $5,000, and at $158,000 of AGI the
// line 7 figure is $5,500.
func TestEstimatedPriorYearStep(t *testing.T) {
	mustContain(t, "it2210-2025.txt",
		"Chris and Kate’s 2024 federal adjusted gross income is $158,000.",
		"$ 6,952 2024 IT-40 credits (line 13 plus Schedule 5, lines 4 through 10)... - 1,952 Subtotal",
		"Exception to the penalty percentage", "x 110%",
		"$ 5,500",
		"they would enter $5,000 instead of $5,500 on line 7.")
	option := func(h household) estimated.TargetOption {
		return must(rules(t).Requirement(h.facts(t))).Value().Options()[1]
	}
	if o := option(household{tax: 100_000 * money.Dollar, prior: prior(5_000*money.Dollar, 158_000*money.Dollar)}); o.Amount() != 5_500*money.Dollar || o.Percent() != money.Percent(110) {
		t.Errorf("Chris and Kate: %v at %v, want $5,500 at 110%%", o.Amount(), o.Percent())
	}
	sep := must(indianataxcalc.NewEstimatedFacts().PassThroughEntityTaxCredit(0).PriorYearMarriedSeparately(true).Build())
	joint := must(indianataxcalc.NewEstimatedFacts().PassThroughEntityTaxCredit(0).PriorYearMarriedSeparately(false).Build())
	for _, tc := range []struct {
		agi    money.USCents
		extras *indianataxcalc.EstimatedFacts
		want   money.Rate
	}{
		{15_000_000, nil, money.Percent(100)},
		{15_000_001, nil, money.Percent(110)},
		{7_500_000, &sep, money.Percent(100)},
		{7_500_001, &sep, money.Percent(110)},
		{7_500_001, &joint, money.Percent(100)},
	} {
		if o := option(household{tax: 100_000 * money.Dollar, prior: prior(5_000*money.Dollar, tc.agi), extras: tc.extras}); o.Percent() != tc.want {
			t.Errorf("AGI %v: %v, want %v", tc.agi, o.Percent(), tc.want)
		}
	}

	// Last year's status not given: this year's is used, and asked about
	// only where the two steps give different answers.
	res := must(rules(t).Requirement(household{tax: 100_000 * money.Dollar, status: filing.MarriedFilingSeparately,
		prior: prior(5_000*money.Dollar, 100_000*money.Dollar)}.facts(t)))
	if o := res.Value().Options()[1]; o.Percent() != money.Percent(110) || count(res.Required(), working.PositionJurisdictionFact) != 1 {
		t.Errorf("separate this year, $100,000 last year: %v, %d facts asked", o.Percent(), count(res.Required(), working.PositionJurisdictionFact))
	}
	res = must(rules(t).Requirement(household{tax: 100_000 * money.Dollar, prior: prior(5_000*money.Dollar, 200_000*money.Dollar)}.facts(t)))
	if has(res.Required(), working.PositionJurisdictionFact) {
		t.Error("asked for last year's filing status above both steps")
	}

	// Last year's AGI not given: 110%, and asked.
	noAGI := must(estimated.NewPriorReturn().Filed(true).FullYearResident(true).
		Figures(must(estimated.NewFigures().Tax(5_000 * money.Dollar).Build())).Build())
	res = must(rules(t).Requirement(household{tax: 100_000 * money.Dollar, prior: &noAGI}.facts(t)))
	if o := res.Value().Options()[1]; o.Amount() != 5_500*money.Dollar || !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Errorf("no AGI: %v, asked %v", o.Amount(), has(res.Required(), working.PositionAdjustedGrossIncome))
	}
}

// The lesser of 90% of this year's tax and last year's figure.
func TestEstimatedTargetOptions(t *testing.T) {
	for _, tc := range []struct {
		tax, priorTax money.USCents
		want          estimated.Basis
		target        money.USCents
	}{
		{10_000 * money.Dollar, 9_000*money.Dollar + 1, estimated.BasisThisYearTax, 9_000 * money.Dollar},
		{10_000 * money.Dollar, 8_999 * money.Dollar, estimated.BasisPriorYearTax, 8_999 * money.Dollar},
	} {
		r := must(rules(t).Requirement(household{tax: tc.tax, prior: prior(tc.priorTax, 50_000*money.Dollar)}.facts(t))).Value()
		if c, _ := r.Chosen(); c.Basis() != tc.want || r.Target() != tc.target || !r.Required() {
			t.Errorf("tax %v, last year %v: chose %v for %v, want %v for %v", tc.tax, tc.priorTax, c.Basis(), r.Target(), tc.want, tc.target)
		}
	}

	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	partYear := must(estimated.NewPriorReturn().Filed(true).FullYearResident(false).
		Figures(must(estimated.NewFigures().Tax(500 * money.Dollar).FederalAGI(10_000 * money.Dollar).Build())).Build())
	unanswered := must(estimated.NewPriorReturn().Filed(true).
		Figures(must(estimated.NewFigures().Tax(500 * money.Dollar).FederalAGI(10_000 * money.Dollar).Build())).Build())
	for name, tc := range map[string]struct {
		prior     *estimated.PriorReturn
		available bool
		ask       working.PositionKind
	}{
		"no return on record":          {nil, false, working.PositionPriorYearReturn},
		"no return filed":              {&notFiled, false, working.PositionPriorYearReturn},
		"part-year, no annualised tax": {&partYear, false, working.PositionJurisdictionFact},
		"residence not given":          {&unanswered, true, working.PositionResidency},
		"filed, full-year, AGI given":  {prior(500*money.Dollar, 10_000*money.Dollar), true, working.PositionUnspecified},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		o := res.Value().Options()[1]
		if o.Available() != tc.available || (!o.Available() && o.WhyNot() == "") {
			t.Errorf("%s: prior-year option available %v (%q), want %v", name, o.Available(), o.WhyNot(), tc.available)
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
	}

	// IT-2210's part-year example: Jane's annualised 2024 tax is $1,231,
	// worked by the caller, and it is used as last year's tax.
	mustContain(t, "it2210-2025.txt", "Her 2024 Form IT-40PNR line 1 income is $10,000.", "$ 30,786 x 4% $ 1,231*")
	jane := must(indianataxcalc.NewEstimatedFacts().PassThroughEntityTaxCredit(0).PriorYearAnnualisedTax(1_231 * money.Dollar).Build())
	r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &partYear, extras: &jane}.facts(t))).Value()
	if c, _ := r.Chosen(); c.Basis() != estimated.BasisPriorYearTax || r.Target() != 1_231*money.Dollar {
		t.Errorf("Jane: chose %v for %v, want last year's $1,231", c.Basis(), r.Target())
	}
}

// IC 6-3-4-4.1(a)(3), from 2026: no return last year and no liability for it
// means last year's tax is $0, so nothing is required, even without this
// year's tax. The 2025 IT-2210 predates it and enters N/A.
func TestEstimatedNoReturnAndNoLiabilityLastYear(t *testing.T) {
	none := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	for _, h := range []household{{tax: 50_000 * money.Dollar, prior: &none}, {noTax: true, prior: &none}} {
		res := must(rules(t).Requirement(h.facts(t)))
		if !res.Value().Exempt() || res.Value().Required() || !res.Value().TargetKnown() {
			t.Errorf("tax known %v: exempt %v, required %v", !h.noTax, res.Value().Exempt(), res.Value().Required())
		}
		if has(res.Required(), working.PositionThisYearTax) {
			t.Error("this year's tax is asked for when nothing can be owed")
		}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if p.Value().Total() != 0 || p.Value().UpperBound() {
			t.Errorf("penalty %v, upper bound %v; want an exact zero", p.Value().Total(), p.Value().UpperBound())
		}
	}
}

// Indiana's forms state no 12-month condition; whether § 6654's carries over
// is unresolved. The figures follow the forms and report the open rule; the
// caller can answer either way.
func TestEstimatedTwelveMonthQuestion(t *testing.T) {
	short := must(estimated.NewPriorReturn().Filed(true).Months(9).FullYearResident(true).
		Figures(must(estimated.NewFigures().Tax(2_000 * money.Dollar).FederalAGI(60_000 * money.Dollar).Build())).Build())
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &short}.facts(t)))
	if o := res.Value().Options()[1]; !o.Available() || !has(res.Required(), working.PositionUnsettledRule) {
		t.Errorf("default: offered %v, asked %v", o.Available(), has(res.Required(), working.PositionUnsettledRule))
	}
	for _, applies := range []bool{true, false} {
		e := must(indianataxcalc.NewEstimatedFacts().PassThroughEntityTaxCredit(0).TwelveMonthRule(applies).Build())
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &short, extras: &e}.facts(t)))
		if o := res.Value().Options()[1]; o.Available() == applies || has(res.Required(), working.PositionUnsettledRule) {
			t.Errorf("rule %v: offered %v, asked %v", applies, o.Available(), has(res.Required(), working.PositionUnsettledRule))
		}
	}
}

// The penalty is 10% of each installment's shortfall. It has no rate periods
// and no days: IC 6-8.1-10-2.1(b) fixes 10%, and SEA 243's rewording from
// July 1, 2026 keeps it, so a shortfall that runs across July 1, 2026 or
// January 1, 2027 is charged the same 10%, and no day lacks a published
// rate.
func TestEstimatedPenaltyIsAFlatTenPercent(t *testing.T) {
	// 90% of $10,000 is $9,000: $2,250 a quarter, $225 on each unpaid one.
	f := household{tax: 10_000 * money.Dollar}.facts(t)
	for _, tc := range []struct {
		asOf  estimated.Date
		lines int
		total money.USCents
	}{
		{day(2026, 4, 14), 0, 0},
		{day(2026, 4, 15), 1, 225 * money.Dollar},
		{day(2026, 7, 1), 2, 450 * money.Dollar},
		{day(2027, 1, 15), 4, 900 * money.Dollar},
		{day(2028, 6, 30), 4, 900 * money.Dollar},
	} {
		res := must(rules(t).Penalty(f, tc.asOf))
		p := res.Value()
		if len(p.Lines()) != tc.lines || p.Total() != tc.total || p.AsOf() != tc.asOf {
			t.Errorf("to %v: %d lines, %v; want %d, %v", tc.asOf, len(p.Lines()), p.Total(), tc.lines, tc.total)
		}
		for _, l := range p.Lines() {
			parts := l.Parts()
			if len(parts) != 1 || parts[0].Kind() != estimated.PartFlat || parts[0].Rate() != money.Percent(10) || parts[0].Charge() != 225*money.Dollar {
				t.Errorf("line %d: %+v, want one flat 10%% part of $225", l.Installment(), parts)
			}
		}
		if has(res.Required(), working.PositionUnpublishedRate) {
			t.Errorf("to %v: an unpublished rate is reported", tc.asOf)
		}
	}

	// April paid one day late, or five months late, costs the same: $225 on
	// April, and June and later met by the carry.
	for _, late := range []estimated.Date{day(2026, 4, 16), day(2026, 9, 15)} {
		h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(late, 2_250*money.Dollar)}}
		h.payments = append(h.payments, quarterly(0, 2_250*money.Dollar, 2_250*money.Dollar, 2_250*money.Dollar)...)
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
		if p.Total() != 225*money.Dollar || len(p.Lines()) != 1 || p.Lines()[0].Installment() != 1 {
			t.Errorf("April paid %v: %v over %d lines, want $225 on April alone", late, p.Total(), len(p.Lines()))
		}
	}
}

// IT-2210's Joe: four $200 payments, the third on October 1, which is
// reported in the fourth column. September is $200 short, and January's
// $400 makes it up without reducing the $20.
func TestEstimatedLatePaymentCountsInTheNextPeriod(t *testing.T) {
	mustContain(t, "it2210-2025.txt", "Joe paid $800 in estimated taxes for 2025. His third installment payment of $200 was not made until Oct. 1 (after the Sept. 15 due date). The $200 payment should be reported as a 4th period installment payment.")
	// Last year's $800 is less than 90% of $2,000.
	h := household{tax: 2_000 * money.Dollar, prior: prior(800*money.Dollar, 40_000*money.Dollar),
		payments: []estimated.Payment{pay(dues[0], 20_000), pay(dues[1], 20_000), pay(day(2026, 10, 1), 20_000), pay(dues[3], 20_000)}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	wantShort := []money.USCents{0, 0, 20_000, 0}
	for i, in := range s {
		if in.Shortfall() != wantShort[i] {
			t.Errorf("installment %d short %v, want %v", i+1, in.Shortfall(), wantShort[i])
		}
	}
	if s[2].Status() != estimated.StatusShort || s[3].Status() != estimated.StatusMet {
		t.Errorf("September %v, January %v", s[2].Status(), s[3].Status())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(); p.Total() != 20*money.Dollar {
		t.Errorf("penalty %v, want $20", p.Total())
	}
}

// IT-2210's Dana: $100 short in the first column and $130 over in the
// second leaves $30 for the third. IT-2210A's $80 short and $50 over leaves
// nothing to carry. Neither catch-up reduces the penalty on the first
// column, and a column that is only met makes up nothing.
func TestEstimatedOverpaymentMakesUpEarlierShortfalls(t *testing.T) {
	mustContain(t, "it2210-2025.txt", "Dana had a $100 underpayment on line 19, Column A. She had a $130 overpayment on line 18, Column B. The net overpayment from the first two installment periods is $30 ($130 minus $100).")
	mustContain(t, "it2210a-2025.txt", "a first period underpayment of $80", "plus a second period overpayment of $50 results in a $30 net",
		"underpayment. This amount is not to be used to decrease any", "credits for the next installment period.")
	// 90% of $4,444.44 is $4,000.00: $1,000 a quarter.
	for name, tc := range map[string]struct {
		paid  []money.USCents
		short []money.USCents
	}{
		"Dana, with $970 in September":    {[]money.USCents{90_000, 113_000, 97_000, 100_000}, []money.USCents{10_000, 0, 0, 0}},
		"Dana, with $969.99 in September": {[]money.USCents{90_000, 113_000, 96_999, 100_000}, []money.USCents{10_000, 0, 1, 0}},
		// A short column is not made up by a later one that is only met:
		// the flat penalty is not charged twice on the same $100.
		"$100 short, then exactly $1,000":    {[]money.USCents{90_000, 100_000, 100_000, 100_000}, []money.USCents{10_000, 0, 0, 0}},
		"IT-2210A, $80 then $50":             {[]money.USCents{92_000, 105_000, 100_000, 100_000}, []money.USCents{8_000, 0, 0, 0}},
		"IT-2210A, $80 then $50, then $9.99": {[]money.USCents{92_000, 105_000, 99_001, 100_000}, []money.USCents{8_000, 0, 999, 0}},
	} {
		h := household{tax: 444_444, payments: quarterly(tc.paid...)}
		s := must(rules(t).Schedule(h.facts(t))).Value()
		var want money.USCents
		for i, in := range s {
			if in.Shortfall() != tc.short[i] {
				t.Errorf("%s: installment %d short %v, want %v", name, i+1, in.Shortfall(), tc.short[i])
			}
			want += money.Percent(10).Of(tc.short[i])
		}
		if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(); p.Total() != want {
			t.Errorf("%s: penalty %v, want %v", name, p.Total(), want)
		}
	}
}

// IB3: a payment after January 15 is returned or credited to the next year.
func TestEstimatedPaymentsAfterJanuaryFifteenDoNotCount(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, payments: quarterly(2_250*money.Dollar, 2_250*money.Dollar, 2_250*money.Dollar)}
	before := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	h.payments = append(h.payments, pay(day(2027, 1, 16), 2_250*money.Dollar))
	after := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if before.Value().Total() != 225*money.Dollar || after.Value().Total() != before.Value().Total() {
		t.Errorf("penalty %v without the late payment and %v with it; want $225 both", before.Value().Total(), after.Value().Total())
	}
	found := false
	for _, r := range after.Applied() {
		found = found || strings.Contains(r.Applied(), "is not a 2026 installment payment")
	}
	if !found {
		t.Error("the late payment is not explained")
	}
}

// IT-2210 line 14: a part-year resident divides the required annual payment
// by the installment periods of residence. Moving in on June 10 is two
// periods, June to August and September to December.
func TestEstimatedPartYearResident(t *testing.T) {
	in := must(estimated.NewResidencePeriod("IN").From(day(2026, 6, 10)).To(day(2026, 12, 31)).Build())
	before := must(estimated.NewResidencePeriod("OH").From(day(2026, 1, 1)).To(day(2026, 6, 9)).Build())
	h := household{tax: 10_000 * money.Dollar, residence: []estimated.ResidencePeriod{before, in}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	wantNeeded := []money.USCents{0, 0, 4_500 * money.Dollar, 9_000 * money.Dollar}
	wantStatus := []estimated.Status{estimated.StatusNothingDue, estimated.StatusNothingDue, estimated.StatusShort, estimated.StatusShort}
	for i, in := range s {
		if in.NeededBy() != wantNeeded[i] || in.Status() != wantStatus[i] {
			t.Errorf("installment %d needs %v and is %v; want %v and %v", i+1, in.NeededBy(), in.Status(), wantNeeded[i], wantStatus[i])
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(); p.Total() != 900*money.Dollar {
		t.Errorf("penalty %v, want 10%% of two $4,500 shortfalls", p.Total())
	}
	if why := must(rules(t).Requirement(h.facts(t))).Value().Why(); !strings.Contains(why, "they must come to $9,000. Spread them over the 2 quarters you lived in Indiana.") {
		t.Errorf("the reason does not say how the payments are spread: %s", why)
	}
	// A full-year resident is divided by four.
	full := must(estimated.NewResidencePeriod("IN").From(day(2026, 1, 1)).To(day(2026, 12, 31)).Build())
	h.residence = []estimated.ResidencePeriod{full}
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].NeededBy() != 2_250*money.Dollar {
		t.Errorf("full year: April needs %v", s[0].NeededBy())
	}
}

// IB3 credits withholding evenly unless the filer shows the dates; IT-2210
// line 8 stops before Section E when withholding and the credit reach the
// required payment, whenever they came.
func TestEstimatedWithholding(t *testing.T) {
	// $8,000 withheld in December: even, $2,000 a quarter against $2,250.
	h := household{tax: 10_000 * money.Dollar, withheld: 8_000 * money.Dollar}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(); p.Total() != 100*money.Dollar {
		t.Errorf("even: penalty %v, want 10%% of four $250 shortfalls", p.Total())
	}
	h.asWithheld = true
	// On its date: April, June and September are wholly short, $675; the
	// December $8,000 covers January's $2,250 and makes up $5,750 of the
	// rest without reducing it.
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(); p.Total() != 675*money.Dollar {
		t.Errorf("as withheld: penalty %v, want $675", p.Total())
	}
	// $9,000 withheld in December reaches the target: no penalty under
	// either method.
	h.withheld = 9_000 * money.Dollar
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 0 {
		t.Errorf("covered: penalty %v", res.Value().Total())
	}
	if r := must(rules(t).Requirement(h.facts(t))).Value(); r.Required() {
		t.Errorf("covered: %s", r.Why())
	}

	// The pass-through credit counts with withholding, in four parts.
	e := must(indianataxcalc.NewEstimatedFacts().PassThroughEntityTaxCredit(4_000 * money.Dollar).Build())
	h = household{tax: 10_000 * money.Dollar, extras: &e, payments: quarterly(1_250*money.Dollar, 1_250*money.Dollar, 1_250*money.Dollar, 1_250*money.Dollar)}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(); p.Total() != 0 {
		t.Errorf("credit of $1,000 a quarter and $1,250 paid: penalty %v", p.Total())
	}
}

// IT-2210A: 22.5%, 45%, 67.5% and 90% of the annualised tax, less earlier
// installments, capped by the regular installment plus what was carried.
func TestEstimatedAnnualisedMethod(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, annualised: []money.USCents{0, 2_000 * money.Dollar, 8_000 * money.Dollar, 10_000 * money.Dollar}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	// $0; 45% of $2,000 is $900; 67.5% of $8,000 is $5,400, less $900 is
	// $4,500; 90% of $10,000 less $5,400 is $3,600, the regular $2,250 plus
	// $1,350 carried.
	want := []money.USCents{0, 900 * money.Dollar, 5_400 * money.Dollar, 9_000 * money.Dollar}
	for i, in := range s {
		if in.NeededBy() != want[i] {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want[i])
		}
	}
	wrong := must(estimated.NewFacts(indiana, 2026).FilingStatus(filing.Single).ThisYear(h.figures()).
		Periods(must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 6, 30)).Build()),
			must(estimated.NewPeriod().End(day(2026, 9, 30)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).Build())
	if _, err := rules(t).Schedule(wrong); err == nil {
		t.Error("quarter-end periods accepted for IT-2210A")
	}
}

// Whether the federal waivers apply in Indiana is unresolved. The penalty is
// shown in full. The waiver question is asked until it is answered; a waiver
// asked for is undecided, with the open point reported. A retiree the IRS
// can excuse is undecided in Indiana.
func TestEstimatedWaiverQuestion(t *testing.T) {
	mustContain(t, "research-in-statute-quotes.txt", "failure to file a return, pay the full amount of tax shown ... or pay the deficiency")
	none, casualty := estimated.WaiverNone, estimated.WaiverCasualty
	retired := must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build())
	for _, tc := range []struct {
		name      string
		waiver    *estimated.WaiverKind
		positions *estimated.Positions
		ask       bool
		undecided bool
	}{
		{"unanswered", nil, nil, true, false},
		{"none", &none, nil, false, false},
		{"a casualty", &casualty, nil, false, true},
		{"retired in 2026 at 64", nil, &retired, false, true},
	} {
		res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, waiver: tc.waiver, positions: tc.positions}.facts(t), day(2027, 4, 15)))
		if res.Value().Total() != 900*money.Dollar || has(res.Required(), working.PositionPenaltyWaiver) != tc.ask {
			t.Errorf("%s: penalty %v, asked %v", tc.name, res.Value().Total(), has(res.Required(), working.PositionPenaltyWaiver))
		}
		if tc.waiver != nil && res.Value().Waiver() != *tc.waiver {
			t.Errorf("%s: recorded as %v", tc.name, res.Value().Waiver())
		}
		o, ok := res.Value().WaiverOutcome()
		if ok != tc.undecided || (ok && !o.Undecided()) || res.Value().IfWaived() != res.Value().Total() {
			t.Errorf("%s: outcome %v %q, if waived %v", tc.name, ok, o.Reason(), res.Value().IfWaived())
		}
		unsettled := false
		for _, q := range res.Required() {
			unsettled = unsettled || (q.Position() == working.PositionUnsettledRule && strings.Contains(q.Effect(), "federal waivers"))
		}
		if unsettled != tc.undecided {
			t.Errorf("%s: open point reported %v", tc.name, unsettled)
		}
	}
	// No penalty, no question.
	if res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar}.facts(t), day(2027, 4, 15))); has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("asked about a waiver with no penalty")
	}
}

// County tax is in the state's figure. Not given, it is asked about.
func TestEstimatedCountyTax(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, noLocal: true}.facts(t)))
	if count(res.Required(), working.PositionJurisdictionFact) != 1 {
		t.Errorf("county tax not asked for: %v", res.Required())
	}
	f := must(estimated.NewFacts(indiana, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).LocalTax(2_000 * money.Dollar).Build())).Build())
	res = must(rules(t).Requirement(f))
	found := false
	for _, r := range res.Applied() {
		found = found || strings.Contains(r.Applied(), "$2000.00 of it county tax")
	}
	if !found {
		t.Error("the county part is not shown")
	}
	// Nothing in the extras: the county question is not asked, the credit is.
	if n := count(res.Required(), working.PositionJurisdictionFact); n != 1 {
		t.Errorf("%d Indiana facts asked for, want the pass-through credit", n)
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][4]string{
		"Exactly $1,000 due after withholding":     {"over $1,000 due", "ES-40", "expected under $1,000", "IC 6-3-4-4.1(b)"},
		"Last year's tax when no return was filed": {"N/A", "R27 / 9-25", "$0", "P.L.205-2025, SEC.10, effective January 1, 2026"},
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
		// $10,000 due with a return on record touches neither rule.
		if d.AffectsHousehold() {
			t.Errorf("%q affects a household it does not touch", d.Rule())
		}
		wantUses := working.ReadingForm
		if d.Rule() == "Last year's tax when no return was filed" {
			wantUses = working.ReadingStatute
		}
		if d.Uses() != wantUses {
			t.Errorf("%q uses %v, want %v", d.Rule(), d.Uses(), wantUses)
		}
	}
	// Exactly $1,000 left, and no return last year with none required, are
	// the facts each rule turns on.
	none := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	for rule, h := range map[string]household{
		"Exactly $1,000 due after withholding":     {tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar},
		"Last year's tax when no return was filed": {tax: 10_000 * money.Dollar, prior: &none},
	} {
		for _, d := range must(rules(t).Requirement(h.facts(t))).Divergences() {
			if d.Rule() == rule && !d.AffectsHousehold() {
				t.Errorf("%q does not affect the facts it turns on", rule)
			}
		}
	}
	// Both readings, in their sources.
	mustContain(t, "es40-2026.txt", "You expect to owe more than $1,000 when you file your tax return")
	mustContain(t, "ib03.txt", "taxes due after credits exceed $1,000")
	mustContain(t, "research-in-statute-quotes.txt", "less than one thousand dollars ($1,000)", "was zero dollars ($0)")
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, year := range []int{2025, 2027} {
		if _, err := indianataxcalc.EstimatedTax(year); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", year, err)
		}
	}
	for name, h := range map[string]household{
		"a farmer":    {tax: 100, farm: 7000},
		"a fisherman": {tax: 100, fish: 7000},
	} {
		if _, err := rules(t).Requirement(h.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"Pennsylvania's facts": must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":                 must(estimated.NewFacts(indiana, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":         must(estimated.NewFacts(indiana, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Indiana's").Build()),
		"county over the tax": must(estimated.NewFacts(indiana, 2026).FilingStatus(filing.Single).
			ThisYear(must(estimated.NewFigures().Tax(100).LocalTax(101).Build())).Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := indianataxcalc.NewEstimatedFacts().PassThroughEntityTaxCredit(-1).Build(); err == nil {
		t.Error("a negative credit accepted")
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty to no date accepted")
	}
}

// The part-year spread is in the reason, pinned by
// TestEstimatedPartYearResident.
func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$1,000", "flat 10%", "county", "110%", "$150,000", "February 1, 2027", "after January 15"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if len(e.Sources()) == 0 || e.VersusFederal() == "" {
		t.Error("no sources or no comparison with federal")
	}
}

// Without this year's tax the target is last year's figure alone, and the
// penalty figured against it is an upper bound. With no last year either
// there is no target.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(4_000*money.Dollar, 60_000*money.Dollar), payments: quarterly(1_000*money.Dollar, 1_000*money.Dollar, 900*money.Dollar, 1_000*money.Dollar)}
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
	s := must(rules(t).Schedule(h.facts(t))).Value()
	if s[2].Shortfall() != 100*money.Dollar {
		t.Errorf("September short %v, want $100", s[2].Shortfall())
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if p.Total() != 10*money.Dollar || !p.UpperBound() {
		t.Errorf("penalty %v, upper bound %v; want at most $10", p.Total(), p.UpperBound())
	}

	h.prior = nil
	if r := must(rules(t).Requirement(h.facts(t))).Value(); r.TargetKnown() || r.Required() {
		t.Errorf("no target expected: %s", r.Why())
	}
	if _, err := rules(t).Schedule(h.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v", err)
	}
	if _, err := rules(t).Penalty(h.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v", err)
	}
}

// Under the $1,000 exception the target is $0, so last year's return isn't
// asked for as a way to change it. Above it, it is.
func TestEstimatedPriorReturnAskedOnlyWhenItCanSetTheTarget(t *testing.T) {
	under := must(rules(t).Requirement(household{tax: 800 * money.Dollar}.facts(t)))
	if !under.Value().Exempt() || has(under.Required(), working.PositionPriorYearReturn) {
		t.Errorf("under $1,000: exempt %v, asked %v", under.Value().Exempt(), has(under.Required(), working.PositionPriorYearReturn))
	}
	over := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	if !has(over.Required(), working.PositionPriorYearReturn) {
		t.Error("over $1,000: last year's return is not asked for")
	}
}
