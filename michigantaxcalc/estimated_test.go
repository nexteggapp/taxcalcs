// Michigan's estimated tax rules for 2026.
//
// Michigan publishes no exhaustive oracle for estimated tax. The strongest
// checks are worked examples with stated answers: the MI-2210 instructions'
// example of a $1,000 shortfall paid on June 10 (56 days of interest, a $100
// penalty), and RAB 2021-17 Examples 6 and 7, which give each quarter's
// required installment under the annualised method and the penalty on it.
// The examples' text is not checked in, because www.michigan.gov refused
// every download (testdata/SOURCES.md); the tests keep their inputs and
// answers. Thresholds, percentages, dates and rates are compared against MCL
// 206.301, 205.23, 205.24 and 8.6 from the Legislature's own text, and
// against Treasury's documents through the verified quotes in
// testdata/research-mi.md, a copy of research/estimated-payments/mi.md. The
// exceptions, the boundaries and the open questions are hand-written cases.
package michigantaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/michigantaxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const mi = estimated.Jurisdiction("MI")

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
	return must(michigantaxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

// asked reports whether a position of kind k says s in its question or
// effect.
func asked(rs []working.Required, k working.PositionKind, s string) bool {
	for _, r := range rs {
		if r.Position() == k && (strings.Contains(r.Question(), s) || strings.Contains(r.Effect(), s)) {
			return true
		}
	}
	return false
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

// quarterly is one payment of a on each due date, skipping zeros.
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
	tax         money.USCents // after credits
	noTax       bool
	withheld    money.USCents
	asWithheld  bool
	status      filing.Status
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *michigantaxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	annualised  []money.USCents // MI-2210 Worksheet line 8 for each period
	positions   *estimated.Positions
}

var answered = must(michigantaxcalc.NewEstimatedFacts().PriorYearEstimatesRequired(true).PriorYearMarriedFilingSeparately(false).Build())

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
	b := estimated.NewFacts(mi, 2026).FilingStatus(status).ThisYear(figures).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.asWithheld {
		b.Elections(must(estimated.NewElections().WithholdingAsWithheld().Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(answered)
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

// filed is a 12-month 2025 return with its tax and federal AGI.
func filed(tax, agi money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).FederalAGI(agi).Build())).Build())
	return &p
}

func line(t *testing.T, p estimated.Penalty, installment int) (estimated.PenaltyLine, bool) {
	t.Helper()
	for _, l := range p.Lines() {
		if l.Installment() == installment {
			return l, true
		}
	}
	return estimated.PenaltyLine{}, false
}

// split returns a line's interest parts and its flat part.
func split(l estimated.PenaltyLine) (interest []estimated.PenaltyPart, flat *estimated.PenaltyPart) {
	for _, p := range l.Parts() {
		switch p.Kind() {
		case estimated.PartInterest:
			interest = append(interest, p)
		case estimated.PartFlat:
			p := p
			flat = &p
		}
	}
	return interest, flat
}

func flatTotal(p estimated.Penalty) money.USCents {
	var t money.USCents
	for _, l := range p.Lines() {
		if _, f := split(l); f != nil {
			t += f.Charge()
		}
	}
	return t
}

// The 2026 figures, found in the statutes that publish them and in the
// research file's quotes of Treasury's documents. testdata/SOURCES.md lists
// the figures no quote covers.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "mcl-206-301.txt",
		"by more than $500.00, shall pay to the department installments of estimated tax under this part on or before April 15, June 15, and September 15 of the person's tax year and January 15 in the following year.",
		"each installment shall be equal to 1/4 the taxpayer's estimated tax",
		"A seafarer may file and pay the tax imposed by this part in the same manner as a farmer or fisherman",
		"(11) Except as provided in subsection (1), the amount of an estimated tax installment shall be computed, payment of estimated tax shall be credited, and a period of underpayment shall be determined in the same manner as provided in the internal revenue code.",
	)
	// Treasury's documents, through the research file's verified quotes;
	// www.michigan.gov refused the downloads (testdata/SOURCES.md).
	mustContain(t, "research-mi.md",
		"1. 2026 MI-1040ES, Michigan Estimated Income Tax for Individuals (Rev. 04-25), tax year 2026.",
		"2. 2025 MI-2210, Underpayment of Estimated Income Tax, with instructions (Rev. 10-25), tax year 2025.",
		`MI-1040ES, TY2026, source 1: "You must make estimated income tax payments if you expect to owe more than $500 when you file your 2026 MI-1040 return."`,
		`MI-1040ES, TY2026, source 1: "110 percent of your total 2025 tax if your 2025 adjusted gross income is more than $150,000 ($75,000 for married filing separately)."`,
		`MI-2210, TY2025, source 2: "110 percent of the tax shown on your 2024 return if 2024 Adjusted Gross Income (AGI) was more than $150,000 if filing joint or single ($75,000 if your 2024 filing status is married filing separately)."`,
		`RAB 2021-17, source 3: "100% of the tax shown on the preceding-year return, if the taxpayer filed a return and the return was for a 12-month tax year."`,
		`RAB 2021-17, source 3: "The 110% rule does not apply to farmers and fishermen or to seafarers for Michigan purposes".`,
		`MI-1040ES, TY2026, source 1: "in equal installments due on or before April 15, 2026, June 15, 2026, September 15, 2026, and January 15, 2027."`,
		`MCL 206.301(1), source 5: "each installment shall be equal to 1/4 the taxpayer's estimated tax".`,
		`The 2025 MI-2210 applies it: June 15, 2025 was a Sunday, so the second due date is "June 16, 2025".`,
		`MI-1040ES 2026: "whether or not you are a Michigan resident".`,
		`RAB 2021-17, source 3: "no penalty is imposed for any underpayment of a Q4 estimated payment if the taxpayer files a return for the taxable year on or before January 31".`,
		`MI-1040ES, TY2026, source 1: "you may file your 2026 income tax return before February 1, 2027, instead."`,
		`MI-2210, TY2025, source 2: "One-fourth of your total withholding is considered paid on each due date unless you can document the dates the tax was withheld."`,
		`MI-2210, TY2025, source 2: "The total tax on your 2025 return minus the amount you paid in withholding and all your credits is $500 or less."`,
		`MI-2210, TY2025, source 2: "You had no tax liability for 2024 (if you had to file), or you were not required to file a 2024 return".`,
		`MI-2210, TY2025, source 2: "No penalty is charged if estimates were not required in the immediately preceding year; however, interest may still be due."`,
		`MI-2210, TY2025, source 2: "You must annualize for the entire year by completing all four columns."`,
		`MCL 205.23(2), source 6: "the same current monthly interest rate of 1 percentage point above the adjusted prime rate per annum from the time the payment was due, until paid."`,
		`RAB 2026-5, source 4: "A daily rate of interest of 0.0002150 based on an adjusted annual rate of 7.85% will be added".`,
		`RAB 2026-10, source 11: "A daily rate of interest of 0.0002129 based on an adjusted annual rate of 7.77% will be added".`,
		`MI-2210, TY2025, source 2: "Penalty is 25 percent of the tax due (minimum $25 per quarter) for failing to file estimated payments or 10 percent (minimum $10 per quarter) for failing to pay enough".`,
		`MI-2210, TY2025, source 2: "Payments are not carried back to offset underpayments in previous quarters."`,
		`MI-2210 2025 says column D covers payments "through January 15, 2026", and also that payments "received after January 16, 2026" are not estimates.`,
		`Notice of March 19, 2026, source 14: "Any relief provided under this Notice will be effectuated by waiving penalties and interest."`,
	)
	mustContain(t, "mcl-205-23.txt",
		"One percentage point shall be added to the adjusted prime rate, and the resulting sum shall be divided by 12 to establish the current monthly interest rate.",
		"a penalty of $10.00 or 10% of the total amount of the deficiency in the tax, whichever is greater",
		"a penalty of $25.00 or 25% of the total amount of the deficiency in the tax, whichever is greater",
	)
	mustContain(t, "mcl-205-24.txt",
		"a penalty shall not be imposed if the taxpayer was not required to make estimated tax payments in the taxpayer's immediately preceding tax year.")
	mustContain(t, "mcl-8-6.txt",
		"If the last day of any period or a fixed or final day is a Saturday, Sunday or legal holiday, the period or day is extended to include the next day which is not a Saturday, Sunday or legal holiday.")
}

// The 2026 dates are the ones the MI-1040ES prints, and none moves. January
// 31, 2027, the file-and-pay date, is a Sunday, and MCL 8.6 moves it to
// Monday, February 1; that is tested with the January 31 rule.
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
	if day(2027, 1, 31).Weekday() != time.Sunday || day(2027, 2, 1).Weekday() != time.Monday || day(2027, 4, 15).Weekday() != time.Thursday {
		t.Error("the 2027 calendar is not what the tests assume")
	}
}

// MCL 206.301(1) and the MI-2210: more than $500 after withholding and
// credits; $500.00 exactly is not.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{950_000, true},  // $500.00 left
		{949_999, false}, // $500.01
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld}.facts(t)))
		if got := res.Value().Exempt(); got != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, got, tc.exempt, res.Value().Why())
		}
		// Exempt, the exception is the chosen option at $0; otherwise a
		// percentage of tax is.
		c, chosen := res.Value().Chosen()
		if exception := c.Basis() == estimated.BasisException; !chosen || exception != tc.exempt {
			t.Errorf("withheld %v: chosen %v, basis %v", tc.withheld, chosen, c.Basis())
		}
		if tc.exempt && (c.Amount() != 0 || c.Label() != "Tax less withholding of $500 or less") {
			t.Errorf("withheld %v: exception %q at %v", tc.withheld, c.Label(), c.Amount())
		}
	}
}

// The prior-year test is 110% above $150,000 of last year's AGI, or $75,000
// if last year's return was separate. RAB 2021-17 Example 7's $150,000 is
// not more than $150,000.
func TestEstimatedPriorYearStepBoundaries(t *testing.T) {
	sep := must(michigantaxcalc.NewEstimatedFacts().PriorYearEstimatesRequired(true).PriorYearMarriedFilingSeparately(true).Build())
	for _, tc := range []struct {
		agi    money.USCents
		extras *michigantaxcalc.EstimatedFacts
		want   money.Rate
	}{
		{15_000_000, nil, money.Percent(100)},
		{15_000_001, nil, money.Percent(110)},
		{7_500_000, &sep, money.Percent(100)},
		{7_500_001, &sep, money.Percent(110)},
	} {
		res := must(rules(t).Requirement(household{tax: 100_000 * money.Dollar, prior: filed(10_000*money.Dollar, tc.agi), extras: tc.extras}.facts(t)))
		o := res.Value().Options()[1]
		if o.Percent() != tc.want || o.Amount() != tc.want.Of(10_000*money.Dollar) {
			t.Errorf("AGI %v: prior-year test %v of $10,000 = %v, want %v", tc.agi, o.Percent(), o.Amount(), tc.want)
		}
	}

	// This year separate and last year's status not said: $75,000 is used,
	// and the question is asked because $100,000 lies between the steps.
	none := must(michigantaxcalc.NewEstimatedFacts().PriorYearEstimatesRequired(true).Build())
	res := must(rules(t).Requirement(household{tax: 100_000 * money.Dollar, status: filing.MarriedFilingSeparately, prior: filed(10_000*money.Dollar, 100_000*money.Dollar), extras: &none}.facts(t)))
	if res.Value().Options()[1].Percent() != money.Percent(110) || !asked(res.Required(), working.PositionJurisdictionFact, "married filing separately") {
		t.Errorf("separate this year, last year not said: %v; asked %v", res.Value().Options()[1].Percent(), res.Required())
	}

	// No AGI: 110% is assumed and the AGI is asked for.
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Build())
	res = must(rules(t).Requirement(household{tax: 100_000 * money.Dollar, prior: &p}.facts(t)))
	if res.Value().Options()[1].Percent() != money.Percent(110) || !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Errorf("no AGI: %v, asked %v", res.Value().Options()[1].Percent(), has(res.Required(), working.PositionAdjustedGrossIncome))
	}
}

// The target is the smaller available option, and the prior-year option
// needs a filed 12-month return.
func TestEstimatedTargetOptions(t *testing.T) {
	// 90% of $10,000 is $9,000; last year's $8,000 is smaller.
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: filed(8_000*money.Dollar, 90_000*money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || res.Value().Target() != 8_000*money.Dollar {
		t.Errorf("chose %v, target %v; want last year's $8,000", c.Basis(), res.Value().Target())
	}
	// Last year's $9,500 is larger.
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: filed(9_500*money.Dollar, 90_000*money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 9_000*money.Dollar {
		t.Errorf("chose %v, target %v; want this year's $9,000", c.Basis(), res.Value().Target())
	}

	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(100).Build())).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		why   string
	}{
		"no return on record":  {nil, "Needs your 2025 Michigan return"},
		"not filed":            {&notFiled, "You didn't file a 2025 Michigan return"},
		"not required to file": {&notRequired, "You weren't required to file a 2025 Michigan return"},
		"eleven months":        {&short, "covered 11 months"},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		o := res.Value().Options()[1]
		if o.Available() || !strings.Contains(o.WhyNot(), tc.why) {
			t.Errorf("%s: available %v, %q", name, o.Available(), o.WhyNot())
		}
		if tc.prior == nil && !has(res.Required(), working.PositionPriorYearReturn) {
			t.Errorf("%s: the prior return is not asked for", name)
		}
	}
}

// The MI-2210 instructions' example: $2,000 due each quarter, $1,000 short
// in April, $2,000 paid June 10. Interest is on $1,000 for 56 days, the June
// payment pays it first and leaves the June installment $1,000 short, and the
// penalty is $100 in the first quarter and nothing in the second. 56 days at
// 0.0002324 on $1,000 is $13.01, so $13. The example's text is not checked
// in (testdata/SOURCES.md).
func TestEstimatedInterestAndPenaltyMatchMI2210Example(t *testing.T) {
	// 90% of $8,888.89 is $8,000.00.
	f := household{tax: 888_889, payments: []estimated.Payment{pay(dues[0], 1_000*money.Dollar), pay(day(2026, 6, 10), 2_000*money.Dollar)}}.facts(t)
	p := must(rules(t).Penalty(f, day(2027, 4, 15))).Value()

	q1, ok := line(t, p, 1)
	if !ok {
		t.Fatal("no line for April")
	}
	interest, flat := split(q1)
	if q1.Shortfall() != 1_000*money.Dollar || len(interest) != 1 || interest[0].Days() != 56 || interest[0].Charge() != 13*money.Dollar {
		t.Errorf("April: shortfall %v, interest %+v; want $1,000 for 56 days, $13", q1.Shortfall(), interest)
	}
	if flat == nil || flat.Charge() != 100*money.Dollar || flat.Rate() != money.Percent(10) {
		t.Errorf("April penalty %+v, want 10%%, $100", flat)
	}

	q2, ok := line(t, p, 2)
	if !ok || q2.Shortfall() != 1_000*money.Dollar {
		t.Fatalf("June: %v, want a $1,000 shortfall for interest", q2.Shortfall())
	}
	if _, flat := split(q2); flat != nil {
		t.Errorf("June penalty %v, want none: the June 10 payment counts in June's quarter", flat.Charge())
	}
}

// RAB 2021-17 Example 6, in 2026 dates. The 2020 figures give required
// installments of $873, $1,331, $1,621 and $1,275 under the annualised
// method, against $1,200 paid each quarter: a 10% penalty of $23 on the
// third quarter's $225, and of $10 on the fourth's $75, or none if the
// return is filed and paid by January 31. Interest is on $225 for 107 and
// 15 days and on $300 for 90; the example's 2020 rates gave $4 and $3, and
// the 2026 factors give $5, $1 and $6. The example's text is not checked in
// (testdata/SOURCES.md).
func TestEstimatedPenaltyMatchesRAB2021_17Example6(t *testing.T) {
	h := household{
		tax:        6_003 * money.Dollar,
		prior:      filed(5_100*money.Dollar, 124_400*money.Dollar),
		annualised: []money.USCents{3_878 * money.Dollar, 4_898 * money.Dollar, 8_404 * money.Dollar, 6_003 * money.Dollar},
		payments:   quarterly(1_200*money.Dollar, 1_200*money.Dollar, 1_200*money.Dollar, 1_200*money.Dollar),
	}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	// $872.55, $1,331.55, $1,620.90 and $1,275.00; the RAB rounds to dollars.
	for i, want := range []money.USCents{87_255, 220_410, 382_500, 510_000} {
		if sched[i].NeededBy() != want {
			t.Errorf("installment %d needs %v by its date, want %v", i+1, sched[i].NeededBy(), want)
		}
	}

	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if len(p.Lines()) != 2 {
		t.Fatalf("%d lines, want the third and fourth quarters", len(p.Lines()))
	}
	q3, _ := line(t, p, 3)
	interest, flat := split(q3)
	if q3.Shortfall() != 225*money.Dollar || flat == nil || flat.Charge() != 23*money.Dollar {
		t.Errorf("third quarter: shortfall %v, penalty %+v; want $225 and $23", q3.Shortfall(), flat)
	}
	if len(interest) != 2 || interest[0].Days() != 107 || interest[1].Days() != 15 || interest[0].Charge() != 5*money.Dollar || interest[1].Charge() != 1*money.Dollar {
		t.Errorf("third quarter interest %+v; want 107 days, $5, and 15 days, $1", interest)
	}
	q4, _ := line(t, p, 4)
	interest, flat = split(q4)
	if q4.Shortfall() != 300*money.Dollar || len(interest) != 1 || interest[0].Days() != 90 || interest[0].Charge() != 6*money.Dollar {
		t.Errorf("fourth quarter: shortfall %v, interest %+v; want $300 for 90 days, $6", q4.Shortfall(), interest)
	}
	if flat == nil || flat.Principal() != 75*money.Dollar || flat.Charge() != 10*money.Dollar {
		t.Errorf("fourth quarter penalty %+v; want the $10 minimum on $75", flat)
	}
	if p.Total() != 45*money.Dollar {
		t.Errorf("total %v, want $45", p.Total())
	}

	h.returnFiled = day(2027, 1, 29)
	p = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	q4, _ = line(t, p, 4)
	if interest, flat := split(q4); flat != nil || len(interest) == 0 {
		t.Errorf("filed January 29: fourth quarter penalty %v and %d interest parts; want no penalty and its interest", flat, len(interest))
	}
}

// RAB 2021-17 Example 7, in 2026 dates. The required installments, $873,
// $2,221, $815 and $2,279, and the $29 penalty on the second quarter's $294
// are reproduced. The example then charges $59 on $588 in the fourth quarter,
// which applies the third quarter's payment to the second quarter's
// shortfall first. The same RAB's text and the MI-2210 Part 3 instructions say
// payments are never carried back for the penalty, so the fourth quarter's
// penalty here is $29 on $294, and $588 is its shortfall for interest. The
// example's text is not checked in (testdata/SOURCES.md).
func TestEstimatedPenaltyFollowsTheFormAgainstRAB2021_17Example7(t *testing.T) {
	// 90% of $7,277.78 is $6,550.00. The annualised taxes give the table's
	// cumulative $873, $3,122, $3,909 and $6,550.
	h := household{
		tax:        727_778,
		prior:      filed(6_188*money.Dollar, 150_000*money.Dollar),
		annualised: []money.USCents{388_000, 693_778, 579_111, 727_778},
		payments:   quarterly(1_400*money.Dollar, 1_400*money.Dollar, 1_400*money.Dollar, 1_400*money.Dollar),
	}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{87_300, 309_400, 390_900, 618_800} {
		if sched[i].NeededBy() != want {
			t.Errorf("installment %d needs %v by its date, want %v", i+1, sched[i].NeededBy(), want)
		}
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	q2, _ := line(t, p, 2)
	if _, flat := split(q2); flat == nil || flat.Principal() != 294*money.Dollar || flat.Charge() != 29*money.Dollar {
		t.Errorf("second quarter penalty %+v, want $29 on $294", flat)
	}
	q4, _ := line(t, p, 4)
	_, flat := split(q4)
	if q4.Shortfall() != 588*money.Dollar {
		t.Errorf("fourth quarter interest shortfall %v, want $588", q4.Shortfall())
	}
	if flat == nil || flat.Principal() != 294*money.Dollar || flat.Charge() != 29*money.Dollar {
		t.Errorf("fourth quarter penalty %+v, want $29 on $294 under the form, not the example's $59", flat)
	}
}

// A shortfall unpaid from April 15, 2026 to April 15, 2027 runs through
// three rate periods with three different factors: 76 days at 0.0002324, 184
// at 0.0002150 and 105 at 0.0002129. On $10,000 that is $176.62, $395.60 and
// $223.55, each rounded to the dollar. Nothing was paid, so the penalty is
// 25%, $2,500. The published factors run to June 30, 2027, past the stop, so
// no day lacks a rate and no unpublished rate is reported.
func TestEstimatedPenaltyAcrossRatePeriods(t *testing.T) {
	// 90% of $44,444.44 is $40,000, a quarter of which is $10,000.
	p := must(rules(t).Penalty(household{tax: 4_444_444}.facts(t), day(2027, 6, 30))).Value()
	if p.AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.AsOf())
	}
	q1, _ := line(t, p, 1)
	interest, flat := split(q1)
	want := []struct {
		days   int
		rate   money.Rate
		charge money.USCents
	}{{76, 848, 177 * money.Dollar}, {184, 785, 396 * money.Dollar}, {105, 777, 224 * money.Dollar}}
	if len(interest) != len(want) {
		t.Fatalf("%d interest parts, want %d", len(interest), len(want))
	}
	for i, w := range want {
		if interest[i].Days() != w.days || interest[i].Rate() != w.rate || interest[i].Charge() != w.charge {
			t.Errorf("part %d: %d days at %v, %v; want %d at %v, %v", i+1, interest[i].Days(), interest[i].Rate(), interest[i].Charge(), w.days, w.rate, w.charge)
		}
	}
	if flat == nil || flat.Rate() != money.Percent(25) || flat.Charge() != 2_500*money.Dollar {
		t.Errorf("April penalty %+v, want 25%%, $2,500", flat)
	}
	if q1.Total() != 3_297*money.Dollar {
		t.Errorf("April line %v, want $3,297", q1.Total())
	}
	if has(must(rules(t).Penalty(household{tax: 4_444_444}.facts(t), day(2027, 6, 30))).Required(), working.PositionUnpublishedRate) {
		t.Error("an unpublished rate is reported though the factors run to June 30, 2027")
	}
}

// The penalty is 25% with a $25 minimum for a quarter with nothing paid, and
// 10% with a $10 minimum for one paid short, each in whole dollars.
func TestEstimatedFlatPenaltyRatesAndMinimums(t *testing.T) {
	for _, tc := range []struct {
		name     string
		prior    money.USCents // last year's tax, which sets each quarter
		payments []estimated.Payment
		rate     money.Rate
		charge   money.USCents
	}{
		{"nothing paid on $50", 200 * money.Dollar, nil, money.Percent(25), 25 * money.Dollar},
		{"nothing paid on $102", 408 * money.Dollar, nil, money.Percent(25), 26 * money.Dollar},
		{"$5 short", 200 * money.Dollar, quarterly(45 * money.Dollar), money.Percent(10), 10 * money.Dollar},
		{"$95 short", 4_000 * money.Dollar, quarterly(905 * money.Dollar), money.Percent(10), 10 * money.Dollar},
		{"$105 short", 4_000 * money.Dollar, quarterly(895 * money.Dollar), money.Percent(10), 11 * money.Dollar},
	} {
		h := household{tax: 100_000 * money.Dollar, prior: filed(tc.prior, 50_000*money.Dollar), payments: tc.payments}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
		q1, _ := line(t, p, 1)
		if _, flat := split(q1); flat == nil || flat.Rate() != tc.rate || flat.Charge() != tc.charge {
			t.Errorf("%s: penalty %+v, want %v, %v", tc.name, flat, tc.rate, tc.charge)
		}
	}
}

// Withholding is a quarter on each date, and counts as paid when choosing
// between 25% and 10%. By election it counts on the dates withheld.
func TestEstimatedWithholding(t *testing.T) {
	// $4,000 required a quarter. $2,000 withheld in December is $500 on each
	// date, so each quarter is $500 short at 10%, $50.
	h := household{tax: 444_444, withheld: 2_000 * money.Dollar}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	for i := 1; i <= 4; i++ {
		l, _ := line(t, p, i)
		if _, flat := split(l); flat == nil || flat.Rate() != money.Percent(10) || flat.Charge() != 50*money.Dollar {
			t.Errorf("quarter %d penalty %+v, want 10%%, $50", i, flat)
		}
	}

	// $8,000 required, all withheld in December: every quarter is met when
	// spread, and only the last by election.
	h = household{tax: 888_889, withheld: 8_000 * money.Dollar}
	for _, in := range must(rules(t).Schedule(h.facts(t))).Value() {
		if in.Status() != estimated.StatusMet {
			t.Errorf("spread: installment %d is %v", in.Number(), in.Status())
		}
	}
	h.asWithheld = true
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, in := range sched {
		if want := i < 3; (in.Status() == estimated.StatusShort) != want {
			t.Errorf("as withheld: installment %d is %v", i+1, in.Status())
		}
	}
}

// The exceptions for last year, and what each leaves.
func TestEstimatedExceptions(t *testing.T) {
	interestOnly := func(t *testing.T, name string, h household) working.Result[estimated.Penalty] {
		t.Helper()
		res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if flatTotal(res.Value()) != 0 || res.Value().Total() == 0 {
			t.Errorf("%s: penalty %v of %v; want interest alone", name, flatTotal(res.Value()), res.Value().Total())
		}
		return res
	}

	// A filed 12-month return with no tax: the target is nothing.
	res := must(rules(t).Requirement(household{tax: 4_000 * money.Dollar, prior: filed(0, 20_000*money.Dollar)}.facts(t)))
	if !res.Value().Exempt() || res.Value().Required() {
		t.Errorf("no 2025 liability: %s", res.Value().Why())
	}
	if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "No tax due last year" {
		t.Errorf("no 2025 liability: chosen %v %q at %v", c.Basis(), c.Label(), c.Amount())
	}
	if p := must(rules(t).Penalty(household{tax: 4_000 * money.Dollar, prior: filed(0, 20_000*money.Dollar)}.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 || p.Value().Reason() != "No tax due last year." {
		t.Errorf("no 2025 liability: penalty %v, %q", p.Value().Total(), p.Value().Reason())
	}

	// No return required, or a short return with no tax: no penalty, and
	// interest is the open question, charged by default.
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(8).Figures(must(estimated.NewFigures().Tax(0).Build())).Build())
	for name, prior := range map[string]*estimated.PriorReturn{"not required to file": &notRequired, "no tax on an 8-month return": &short} {
		res := interestOnly(t, name, household{tax: 4_000 * money.Dollar, prior: prior})
		if !asked(res.Required(), working.PositionUnsettledRule, "interest is still charged") {
			t.Errorf("%s: the interest question is not asked: %v", name, res.Required())
		}
	}

	// MCL 205.24(6): no estimates required last year, as said, or worked out
	// from last year's $500.00 of tax with nothing withheld. $500.01 needed
	// them.
	notReq := must(michigantaxcalc.NewEstimatedFacts().PriorYearEstimatesRequired(false).Build())
	interestOnly(t, "said not required", household{tax: 4_000 * money.Dollar, extras: &notReq})
	silent := must(michigantaxcalc.NewEstimatedFacts().PriorYearMarriedFilingSeparately(false).Build())
	lastYear := func(tax money.USCents) *estimated.PriorReturn {
		p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).FederalAGI(50_000 * money.Dollar).Withheld(0).Build())).Build())
		return &p
	}
	interestOnly(t, "$500.00 last year", household{tax: 4_000 * money.Dollar, prior: lastYear(50_000), extras: &silent})
	if p := must(rules(t).Penalty(household{tax: 4_000 * money.Dollar, prior: lastYear(50_001), extras: &silent}.facts(t), day(2027, 4, 15))); flatTotal(p.Value()) == 0 {
		t.Error("$500.01 last year: no penalty")
	}

	// Not said and not worked out: asked, and the penalty is charged.
	res2 := must(rules(t).Penalty(household{tax: 4_000 * money.Dollar, extras: &silent}.facts(t), day(2027, 4, 15)))
	if flatTotal(res2.Value()) == 0 || !asked(res2.Required(), working.PositionJurisdictionFact, "required to make estimated payments for 2025") {
		t.Errorf("not said: penalty %v, asked %v", flatTotal(res2.Value()), res2.Required())
	}
	if !has(res2.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked about")
	}
}

// Filing and paying by January 31 takes the penalty off the fourth quarter
// and leaves its interest. January 31, 2027 is a Sunday, so February 1 is in
// time, and that reading is asked about.
func TestEstimatedFileAndPayByJanuary31(t *testing.T) {
	// $1,000 a quarter, the first three paid, nothing in January.
	base := household{tax: 444_444, payments: quarterly(1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar)}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
		asked   bool
	}{
		{day(2027, 1, 29), true, false},
		{day(2027, 2, 1), true, true},
		{day(2027, 2, 2), false, false},
	} {
		h := base
		h.returnFiled = tc.filed
		res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		q4, ok := line(t, res.Value(), 4)
		if !ok {
			t.Fatalf("filed %v: no January line", tc.filed)
		}
		interest, flat := split(q4)
		if (flat == nil) != tc.excused || len(interest) == 0 {
			t.Errorf("filed %v: January penalty %v, %d interest parts; want excused %v and interest", tc.filed, flat, len(interest), tc.excused)
		}
		if got := asked(res.Required(), working.PositionUnsettledRule, "Feb 1, 2027"); got != tc.asked {
			t.Errorf("filed %v: February 1 asked %v, want %v", tc.filed, got, tc.asked)
		}
		if res.Value().AsOf() != tc.filed {
			t.Errorf("filed %v: figured to %v", tc.filed, res.Value().AsOf())
		}
	}
	// February 1: 17 days at 0.0002129 on $1,000, $3.62, so $4.
	h := base
	h.returnFiled = day(2027, 2, 1)
	q4, _ := line(t, must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(), 4)
	if interest, _ := split(q4); len(interest) != 1 || interest[0].Days() != 17 || interest[0].Charge() != 4*money.Dollar {
		t.Errorf("January interest %+v, want 17 days, $4", interest)
	}
}

// A payment counts for the penalty only in the quarter it arrives. Nothing
// by April 15 and $1,000 on May 1 is 25% on April's $1,000, $250, plus 16
// days of interest, $3.72, so $4. Whether a late payment is 25% or 10% is
// asked.
func TestEstimatedLatePaymentCountsInItsOwnQuarter(t *testing.T) {
	h := household{tax: 444_444, payments: []estimated.Payment{
		pay(day(2026, 5, 1), 1_000*money.Dollar), pay(dues[1], 1_000*money.Dollar), pay(dues[2], 1_000*money.Dollar), pay(dues[3], 1_000*money.Dollar),
	}}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 254*money.Dollar || len(res.Value().Lines()) != 1 {
		t.Errorf("penalty %v over %d lines, want $254 on April alone", res.Value().Total(), len(res.Value().Lines()))
	}
	if !asked(res.Required(), working.PositionUnsettledRule, "paying late") {
		t.Errorf("the late-payment rate is not asked: %v", res.Required())
	}
}

// A payment on January 16, 2027 is the open question the verification left:
// the MI-2210 counts the fourth quarter through January 15 and calls only
// later payments not estimates. It is left out of the fourth quarter by
// default, so the quarter has nothing paid: $250. Counted, it would be none.
func TestEstimatedJanuarySixteenthPaymentIsAsked(t *testing.T) {
	h := household{tax: 444_444, payments: []estimated.Payment{
		pay(dues[0], 1_000*money.Dollar), pay(dues[1], 1_000*money.Dollar), pay(dues[2], 1_000*money.Dollar), pay(day(2027, 1, 16), 1_000*money.Dollar),
	}}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	q4, _ := line(t, res.Value(), 4)
	if _, flat := split(q4); flat == nil || flat.Charge() != 250*money.Dollar {
		t.Errorf("January penalty %+v, want $250", flat)
	}
	if !asked(res.Required(), working.PositionUnsettledRule, "Jan 16, 2027") {
		t.Errorf("January 16 is not asked: %v", res.Required())
	}
	// Paid on January 15, nothing is asked.
	h.payments[3] = pay(dues[3], 1_000*money.Dollar)
	if res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); asked(res.Required(), working.PositionUnsettledRule, "Jan 16, 2027") || res.Value().Total() != 0 {
		t.Errorf("paid January 15: penalty %v, asked %v", res.Value().Total(), res.Required())
	}
}

// A late start is reported; the MI-2210 still charges the earlier quarters.
func TestEstimatedLateStartIsAsked(t *testing.T) {
	h := household{tax: 444_444, firstMet: day(2026, 7, 1), payments: quarterly(0, 0, 3_000*money.Dollar, 1_000*money.Dollar)}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !asked(res.Required(), working.PositionUnsettledRule, "pay first on Sep 15") {
		t.Errorf("the late start is not asked: %v", res.Required())
	}
	if _, ok := line(t, res.Value(), 1); !ok {
		t.Error("April is not charged")
	}
	// The chart's rows start April 1, June 1 and September 1.
	for _, tc := range []struct {
		met   estimated.Date
		first string
	}{{day(2026, 5, 31), "Jun 15."}, {day(2026, 6, 1), "Sep 15."}, {day(2026, 9, 1), "Jan 15, 2027."}} {
		h.firstMet = tc.met
		if res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); !asked(res.Required(), working.PositionUnsettledRule, "pay first on "+tc.first) {
			t.Errorf("first met %v: want the chart's %s", tc.met, tc.first)
		}
	}
	h.firstMet = day(2026, 3, 31)
	if res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); asked(res.Required(), working.PositionUnsettledRule, "to pay first on") {
		t.Error("first met March 31: a late start is reported")
	}
}

// Michigan has no standing waiver. A casualty or disaster is left open,
// since relief comes by notice for each event or waives only the penalty,
// and the notices' reach is asked. Retirement and disability have no
// waiver, even where the federal one applies.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pos     *estimated.PositionsBuilder
		refused bool
		want    string
	}{
		{"disaster", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)), false, "granted relief for by notice"},
		{"retired in 2026 at 64", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)), true, "no waiver for retirement or disability"},
		{"retired in 2020", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, 3, 1)), true, "no waiver for retirement or disability"},
	} {
		pos := must(tc.pos.Build())
		res := must(rules(t).Penalty(household{tax: 4_000 * money.Dollar, positions: &pos}.facts(t), day(2027, 4, 15)))
		p := res.Value()
		o, ok := p.WaiverOutcome()
		if !ok || o.Refused() != tc.refused || o.Undecided() == tc.refused || !strings.Contains(o.Reason(), tc.want) {
			t.Errorf("%s: outcome %v refused %v, %q", tc.name, ok, o.Refused(), o.Reason())
		}
		if p.Total() == 0 || p.IfWaived() != p.Total() {
			t.Errorf("%s: total %v, if waived %v; nothing is eligible", tc.name, p.Total(), p.IfWaived())
		}
		if has(res.Required(), working.PositionUnsettledRule) == tc.refused {
			t.Errorf("%s: the open point is asked %v", tc.name, !tc.refused)
		}
	}
}

// When the $500 exception sets the target, last year's return can't change
// it, so it isn't asked for.
func TestEstimatedExemptAsksNothingOfTheTarget(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 400 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() || has(res.Required(), working.PositionPriorYearReturn) {
		t.Errorf("exempt %v, asked %v", res.Value().Exempt(), res.Required())
	}
}

// Each divergence is reported with both readings and both sources, and the
// interest follows the printed factors: $1,000,000 short for July to
// December 2026 is $39,560 at 0.0002150 a day, where 7.85% ÷ 365 would give
// $39,573.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"How interest accrues":                        {"0.0002150", "MCL 205.23(2)"},
		"Which flat penalty applies":                  {"at least $25", "MCL 205.23(3), (4)"},
		"A return filed and paid by January 31, 2027": {"by February 1, 2027", "MCL 8.6"},
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
		// Nothing paid on $10,000: interest and a flat penalty on every
		// quarter, and no return filed on February 1.
		if want := d.Rule() != "A return filed and paid by January 31, 2027"; d.AffectsHousehold() != want || d.Uses() != working.ReadingForm {
			t.Errorf("%q: affects %v, uses %v; want %v and the form", d.Rule(), d.AffectsHousehold(), d.Uses(), want)
		}
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.StatuteSource(), w[1]) || d.FormSource() == "" || d.Statute() == "" {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s)", d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
		}
	}

	// 90% of $4,444,444.44 is $4,000,000, a quarter of which is $1,000,000.
	p := must(rules(t).Penalty(household{tax: 444_444_444}.facts(t), day(2027, 4, 15))).Value()
	q1, _ := line(t, p, 1)
	interest, _ := split(q1)
	// January to June: 76 days at 0.0002324 is $17,662.40, where 8.48% ÷ 365
	// would give $17,656.99.
	if len(interest) < 2 || interest[0].Days() != 76 || interest[0].Charge() != 17_662*money.Dollar {
		t.Errorf("January to June parts %+v, want 76 days, $17,662", interest)
	}
	if len(interest) < 2 || interest[1].Days() != 184 || interest[1].Charge() != 39_560*money.Dollar {
		t.Errorf("July to December parts %+v, want 184 days, $39,560", interest)
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := michigantaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	base := func() *estimated.FactsBuilder {
		return estimated.NewFacts(mi, 2026).FilingStatus(filing.Single).ThisYear(this)
	}
	sea := func(r money.Rate) michigantaxcalc.EstimatedFacts {
		return must(michigantaxcalc.NewEstimatedFacts().SeafaringShare(r).Build())
	}
	for name, f := range map[string]estimated.Facts{
		"a farmer":                    must(base().FarmShare(6_667).Build()),
		"a fisherman":                 must(base().FishShare(6_667).Build()),
		"a seafarer":                  must(base().State(sea(6_667)).Build()),
		"farming and seafaring":       must(base().FarmShare(3_333).State(sea(3_334)).Build()),
		"seafaring and fishing, 6666": must(base().FishShare(3_333).State(sea(3_333)).Build()),
	} {
		_, err := rules(t).Requirement(f)
		if want := name != "seafaring and fishing, 6666"; errors.Is(err, estimated.ErrUnsupported) != want {
			t.Errorf("%s: %v, want unsupported %v", name, err, want)
		}
	}
	if _, err := michigantaxcalc.NewEstimatedFacts().SeafaringShare(10_001).Build(); err == nil {
		t.Error("a seafaring share over 100% was accepted")
	}
	wrongPeriods := base()
	for _, end := range []estimated.Date{day(2026, 3, 31), day(2026, 6, 30), day(2026, 8, 31), day(2026, 12, 31)} {
		wrongPeriods.Periods(must(estimated.NewPeriod().End(end).Build()))
	}
	wrongPeriods.Elections(must(estimated.NewElections().Annualised().Build()))
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(mi, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(base().State("not Michigan's").Build()),
		"periods off by one": must(wrongPeriods.Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"$500", "10%", "25%", "$25", "$10", "only for the quarter in which it arrives", "110%", "$150,000", "no waiver for retirement or disability", "but not its interest"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Without this year's tax, the target is last year's tax, and the penalty is
// the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: filed(4_000*money.Dollar, 100_000*money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 4_000*money.Dollar {
		t.Errorf("required %v, target %v; want $4,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option: available %v, %q", o.Available(), o.WhyNot())
	}
	if c, ok := r.Chosen(); !ok || c.Basis() != estimated.BasisPriorYearTax {
		t.Errorf("chose %v", c.Basis())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{100_000, 200_000, 300_000, 400_000} {
		if sched[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, sched[i].NeededBy(), want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if !pen.UpperBound() || pen.Total() == 0 {
		t.Errorf("unpaid: penalty %v, upper bound %v", pen.Total(), pen.UpperBound())
	}

	// No return to measure against: no target.
	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() {
		t.Error("a target with neither year's tax")
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v", err)
	}

	// No 2025 liability on a full-year return needs nothing about this year.
	zero := household{noTax: true, prior: filed(0, 20_000*money.Dollar)}
	if p := must(rules(t).Penalty(zero.facts(t), day(2027, 4, 15))).Value(); p.Total() != 0 || p.UpperBound() {
		t.Errorf("no 2025 liability: penalty %v, upper bound %v; want an exact zero", p.Total(), p.UpperBound())
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("MI"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}
