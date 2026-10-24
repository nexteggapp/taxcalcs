// Maine's estimated tax rules for 2026, checked against Maine's own documents.
//
// Maine publishes no exhaustive oracle for estimated tax, and no worked
// example could be read: Form 2210ME and its instructions, where one would
// be, could not be downloaded (see testdata/SOURCES.md), and the research file
// reproduces none. Every case here is hand-written, with the penalty
// arithmetic worked out independently from the form's quoted rule: .75% a
// month for 2026, a part of a month as a whole one, compounded monthly. Every
// threshold, percentage, date and rate is compared against its extract in
// testdata/: the statutes from the Revisor's site, and for the Maine Revenue
// Services forms the research file's verified quotes.
package mainetaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/mainetaxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const maine = estimated.Jurisdiction("ME")

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
	return must(mainetaxcalc.EstimatedTax(2026))
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

func quarterly(a ...money.USCents) []estimated.Payment {
	var out []estimated.Payment
	for i, x := range a {
		out = append(out, pay(dues[i], x))
	}
	return out
}

type household struct {
	tax         money.USCents // Form 2210ME line 1
	noTax       bool
	withheld    money.USCents // withheld on December 15
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *mainetaxcalc.EstimatedFacts // nil: no unusual event
	noExtras    bool
	returnFiled estimated.Date
	annualised  []money.USCents // annualized tax for each period
	asWithheld  bool
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	fig := estimated.NewFigures()
	if !h.noTax {
		fig.Tax(h.tax)
	}
	b := estimated.NewFacts(maine, 2026).FilingStatus(filing.Single).ThisYear(must(fig.Build())).
		Paid(h.payments...).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	switch {
	case h.extras != nil:
		b.State(*h.extras)
	case !h.noExtras:
		b.State(must(mainetaxcalc.NewEstimatedFacts().NoUnusualEvent().Build()))
	}
	el := estimated.NewElections()
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		el.Annualised()
	}
	if h.asWithheld {
		el.WithholdingAsWithheld()
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

// last is last year's Maine liability, Form 2210ME line 5, for a 12-month
// year.
func last(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).Build())
	return &p
}

// The household most tests start from: $4,444.44 of tax, so 90% is $4,000
// and $1,000 a quarter, and last year's $10,000 is no lower.
func base() household { return household{tax: 444_444, prior: last(10_000 * money.Dollar)} }

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "mrs-36-5228.txt",
		"reduced by allowable credits for the taxable year, is less than $1,000 for the taxable year",
		"The person had less than $1,000 tax liability under this Part for the preceding taxable year. This paragraph does not apply with respect to an unusual event.",
		"An amount equal to the person's tax liability under this Part for the preceding taxable year, if that preceding year was a taxable year of 12 months",
		"An amount equal to 90% of the person's tax liability under this Part for the current taxable year",
		"individual taxpayers encountering an unusual event are subject only to paragraph B with respect to the unusual event",
		"received by the taxpayer during the same period of the previous tax year by at least $500,000",
		"an installment payment is due the 15th day of the 4th, 6th, 9th and 13th month following the beginning of the individual's, trust's or estate's taxable year",
		"The amount of the estimated tax is to be paid in 4 equal installments unless:",
		"completed by the close of the month preceding the installment's due date",
		"the taxpayer shall make an estimated tax payment pursuant to subsection 3, paragraph B equal to the amount of estimated tax with respect to the taxable income that results in the unusual event",
		"from the date the installment is due until the underpayment is satisfied or until the tax return to which the estimate installment applies is due, whichever is less",
		"at the rate provided pursuant to section 186. For cause, the State Tax Assessor may waive or abate all or any part of the penalty.",
		"If, on or before the last day of the first month of the following taxable year, an individual, trust or estate files a return and pays in full the tax liability for the taxable year of the return, a penalty may not be imposed with respect to any underpayment of the 4th required installment for that year.",
		"[PL 2025, c. 113, Pt. C, §12 (AMD).]",
		"The taxpayer is an owner in a pass-through entity;",
		"The taxpayer had no control over the distribution of the unusual event amount;",
		"The taxpayer did not actually or constructively receive payment of the unusual event amount; and",
		"by the installment payment due date following the installment period during which the taxpayer actually or constructively receives payment",
		"an individual who meets the criteria of Section 6654(i)(2) of the Code",
		"exclusive of a withholder's liability for taxes withheld and the tax imposed by chapter 815, subchapter 3",
	)
	mustContain(t, "mrs-36-186.txt",
		"calculated from that date and compounded monthly",
		"The rate of interest for any calendar year beginning on or after January 1, 2018 equals the prime rate as published in the Wall Street Journal on the first day of September of the preceding calendar year",
	)
	mustContain(t, "mrs-36-153.txt",
		"falls on Saturday, Sunday or a legal holiday in this State, the performance of that act is timely if it occurs on the next succeeding day which is not a Saturday, Sunday or legal holiday in this State.")
	mustContain(t, "mrs-36-5227.txt",
		"must be filed on or before the date a federal income tax return, without regard to extension, is due to be filed.")
	mustContain(t, "mrs-4-1051.txt",
		"New Year's Day, January 1st;", "Martin Luther King, Jr., Day, the 3rd Monday in January;",
		"Washington's Birthday, the 3rd Monday in February;", "Patriot's Day, the 3rd Monday in April;",
		"Memorial Day, the last Monday in May", "Juneteenth, June 19th;", "the 4th of July;",
		"Labor Day, the first Monday of September;", "Indigenous Peoples Day, the 2nd Monday in October;",
		"Veterans Day, November 11th;", "or on Christmas Day.", "any day designated for the annual Thanksgiving",
		"When any one of the holidays named in this section falls on Sunday, the Monday following must be observed as a holiday")
	mustContain(t, "mrs-36-187-B-7.txt", "The failure to file or pay resulted directly from a natural disaster;")
	mustContain(t, "research-me-quotes.txt",
		"COPIED FROM THE RESEARCH FILE, NOT FROM A MAINE REVENUE SERVICES DOCUMENT.",
		"[ES-26] estimated Maine income tax for the year, over and above tax withheld and allowable credits, is $1,000 or more",
		"[ES-26] and if your tax liability for the prior year was also $1,000 or more.",
		"[ES-26] b) An amount equal to 90% of the tax liability for the current taxable year.",
		"[ES-26] exceeds the Maine taxable income received during the same estimated tax installment period of the prior tax year by $500,000 or more.",
		"[ES-26] pay in four equal installments on April 15, 2026, June 15, 2026, September 15, 2026, and January 15, 2027.",
		"[2210-25] line 7: Multiply the amount on line 6 by .25",
		"[2210-25] line 8 column B: June 16, 2025",
		"[2210-25] line 10a: Tax withheld. Enter 25% (0.25) of line 3 in each column.",
		"[2210-25] Subtract line 3 from line 1. If the result is less than $1,000, do not complete or file this form.",
		"[2210-25] line 5: If short year, enter the amount from line 2.",
		"[2210-25] For calendar year 2026, the penalty rate is .75% per month or fraction thereof, compounded monthly.",
		"[2210-25] line 13: A part of a month is considered a whole month.",
		"Its line 5 measures 2024 tax after refundable credits (line 24 minus lines 25c, 25d and 25e).",
		"ends at the earlier of payment or the 15th day of the fourth month",
		"Periods are the federal ones: 1/1 to 3/31, 5/31, 8/31 and 12/31.",
		"percentages 22.5%, 45%, 67.5% and 90% [AI-25]",
	)
}

// The 2026 dates are the ones Form 1040ES-ME prints. Each is a weekday and
// none is a Maine legal holiday under 4 M.R.S. § 1051 (Patriot's Day is April
// 20, 2026), so none moves.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(base().facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
		if want := money.USCents(i+1) * 1_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
}

// The two $1,000 tests, one cent either side. This year's is tax less
// withholding; last year's is its liability.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		withheld money.USCents
		prior    money.USCents
		exempt   bool
	}{
		{"this year $999.99", 400_001, 10_000 * money.Dollar, true},
		{"this year $1,000.00", 400_000, 10_000 * money.Dollar, false},
		{"last year $999.99", 0, 99_999, true},
		{"last year $1,000.00", 0, 100_000, false},
	} {
		h := household{tax: 5_000 * money.Dollar, withheld: tc.withheld, prior: last(tc.prior)}
		res := must(rules(t).Requirement(h.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", tc.name, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		// Under either test the exception is the chosen option, at $0.
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || !strings.HasSuffix(c.Label(), " under $1,000")) {
			t.Errorf("%s: chosen %v %q at %v", tc.name, c.Basis(), c.Label(), c.Amount())
		}
		if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); tc.exempt && p.Value().Total() != 0 {
			t.Errorf("%s: a penalty of %v", tc.name, p.Value().Total())
		}
	}
}

// The target is the smaller of 90% of this year's tax and 100% of last
// year's, and last year's needs a 12-month year with its liability.
func TestEstimatedTargetOptions(t *testing.T) {
	res := must(rules(t).Requirement(base().facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 4_000*money.Dollar {
		t.Errorf("chose %v at %v, want 90%% of this year's, $4,000", c.Basis(), res.Value().Target())
	}
	h := base()
	h.prior = last(3_000 * money.Dollar)
	res = must(rules(t).Requirement(h.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || c.Percent() != money.Percent(100) || res.Value().Target() != 3_000*money.Dollar {
		t.Errorf("chose %v %v at %v, want 100%% of last year's, $3,000", c.Basis(), c.Percent(), res.Value().Target())
	}

	short := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(500 * money.Dollar).Build())).Build())
	noFigures := must(estimated.NewPriorReturn().Build())
	for name, p := range map[string]*estimated.PriorReturn{"no return on record": nil, "no liability given": &noFigures, "a short year": &short} {
		h := base()
		h.prior = p
		res := must(rules(t).Requirement(h.facts(t)))
		o := res.Value().Options()[1]
		if o.Available() || o.WhyNot() == "" {
			t.Errorf("%s: the prior-year option is available", name)
		}
		if res.Value().Exempt() {
			t.Errorf("%s: exempt; the $1,000 test on last year is not applied", name)
		}
		if !has(res.Required(), working.PositionPriorYearReturn) {
			t.Errorf("%s: last year's return is not asked about", name)
		}
	}
}

// An unusual event is income not withheld that is at least $500,000 above the
// same period of last year. 90% of the tax on it is due with that period's
// installment, and last year's liability does not cover it.
func TestEstimatedUnusualEvent(t *testing.T) {
	event := func(increase money.USCents, tax money.USCents, passThrough *bool) *mainetaxcalc.EstimatedFacts {
		b := mainetaxcalc.NewEstimatedFacts().
			IncomeNotWithheld(1, 50_000*money.Dollar, 50_000*money.Dollar).
			IncomeNotWithheld(2, 100_000*money.Dollar+increase, 100_000*money.Dollar).
			IncomeNotWithheld(3, 0, 0).
			IncomeNotWithheld(4, 0, 0)
		if tax > 0 {
			b.UnusualEventTax(2, tax)
		}
		if passThrough != nil {
			b.PassThroughWaiver(*passThrough)
		}
		e := must(b.Build())
		return &e
	}
	no := false
	// $100,000 of tax this year and $20,000 last year; $42,000 of tax on the
	// June period's excess, 90% of which is $37,800.
	h := household{tax: 100_000 * money.Dollar, prior: last(20_000 * money.Dollar)}

	h.extras = event(49_999_999, 42_000*money.Dollar, &no)
	res := must(rules(t).Requirement(h.facts(t)))
	if res.Value().Target() != 20_000*money.Dollar {
		t.Errorf("$499,999.99 more: target %v, want last year's $20,000", res.Value().Target())
	}

	h.extras = event(50_000_000, 42_000*money.Dollar, &no)
	res = must(rules(t).Requirement(h.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || res.Value().Target() != 5_780_000 {
		t.Errorf("$500,000.00 more: chose %v at %v, want last year's $20,000 plus $37,800", c.Basis(), res.Value().Target())
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{500_000, 4_780_000, 5_280_000, 5_780_000} {
		if got := sched.Value()[i].NeededBy(); got != want {
			t.Errorf("installment %d needs %v, want %v", i+1, got, want)
		}
	}
	if has(sched.Required(), working.PositionPenaltyWaiver) {
		t.Error("the pass-through question was answered and is still asked")
	}

	// Under $1,000 last year waives everything but the unusual event.
	h.prior = last(900 * money.Dollar)
	res = must(rules(t).Requirement(h.facts(t)))
	if res.Value().Exempt() || res.Value().Target() != 3_780_000 {
		t.Errorf("last year $900: exempt %v, target %v; want $37,800", res.Value().Exempt(), res.Value().Target())
	}
	sched = must(rules(t).Schedule(h.facts(t)))
	if s := sched.Value(); s[0].Status() != estimated.StatusNothingDue || s[1].NeededBy() != 3_780_000 || s[3].NeededBy() != 3_780_000 {
		t.Errorf("last year $900: April %v, June needs %v, January needs %v", s[0].Status(), s[1].NeededBy(), s[3].NeededBy())
	}

	// The four pass-through conditions take the event out.
	yes := true
	h.extras = event(50_000_000, 42_000*money.Dollar, &yes)
	if res := must(rules(t).Requirement(h.facts(t))); !res.Value().Exempt() {
		t.Errorf("with the pass-through waiver: %s", res.Value().Why())
	}

	// Unanswered pass-through conditions and a missing tax are both asked.
	h.prior = last(20_000 * money.Dollar)
	h.extras = event(50_000_000, 0, nil)
	res = must(rules(t).Requirement(h.facts(t)))
	if !has(res.Required(), working.PositionPenaltyWaiver) || count(res.Required(), working.PositionJurisdictionFact) != 1 || res.Value().Target() != 20_000*money.Dollar {
		t.Errorf("unanswered: required %v, target %v", res.Required(), res.Value().Target())
	}

	// Nothing said at all: asked, and no event assumed.
	h.extras, h.noExtras = nil, true
	if res := must(rules(t).Requirement(h.facts(t))); count(res.Required(), working.PositionJurisdictionFact) != 1 {
		t.Errorf("no extras: %v", res.Required())
	}

	// More unusual-event tax than this year's tax is refused.
	h.noExtras, h.extras = false, event(50_000_000, 100_000*money.Dollar+1, &no)
	if _, err := rules(t).Requirement(h.facts(t)); err == nil {
		t.Error("an unusual-event tax above this year's tax was accepted")
	}
}

// The penalty is .75% a month, a part of a month a whole one, compounded
// monthly. $1,000 short on April 15: one day or one month is $7.50; one month
// and a day is $15.06 ($1,000 × (1.0075² − 1) = $15.05625); to July 10, three
// months, $22.67.
func TestEstimatedPenaltyCompoundsByTheMonth(t *testing.T) {
	f := base().facts(t)
	for _, tc := range []struct {
		asOf   estimated.Date
		months int
		charge money.USCents
	}{
		{day(2026, 4, 16), 1, 750},
		{day(2026, 5, 15), 1, 750},
		{day(2026, 5, 16), 2, 1_506},
		{day(2026, 6, 14), 2, 1_506},
	} {
		p := must(rules(t).Penalty(f, tc.asOf)).Value()
		parts := p.Lines()[0].Parts()
		if len(parts) != 1 || parts[0].Months() != tc.months || parts[0].Charge() != tc.charge || parts[0].Rate() != money.Percent(9) {
			t.Errorf("to %v: %+v, want %d months and %v", tc.asOf, parts, tc.months, tc.charge)
		}
	}
	// To July 10: April's three months, $22.67, and June's one, $7.50.
	if p := must(rules(t).Penalty(f, day(2026, 7, 10))); p.Value().Total() != 3_017 {
		t.Errorf("to July 10: %v, want $30.17", p.Value().Total())
	}
	// $100,000 short for nine months: $6,956.08 to the cent.
	big := household{tax: 44_444_444, prior: last(1_000_000 * money.Dollar)}.facts(t)
	if l := must(rules(t).Penalty(big, day(2026, 12, 31))).Value().Lines()[0]; l.Total() != 695_608 {
		t.Errorf("$100,000 for nine months: %v, want $6,956.08", l.Total())
	}
	// A payment pays the oldest shortfall first and stops its interest.
	paid := base()
	paid.payments = []estimated.Payment{pay(day(2026, 5, 1), 1_000*money.Dollar)}
	p := must(rules(t).Penalty(paid.facts(t), day(2026, 6, 15)))
	if lines := p.Value().Lines(); lines[0].Total() != 750 || p.Value().Total() != 750 {
		t.Errorf("paid May 1: April's line %v, total %v; want $7.50 for one month", lines[0].Total(), p.Value().Total())
	}
}

// The penalty across the end of 2026. Maine's published rate is one for the
// calendar year, so there is no change of rate inside 2026 to test; the
// boundary is December 31, after which no rate is carried. A month that
// begins in 2026 is charged at 2026's rate. To April 15, 2027 the penalty is
// what it is to December 31, 2026, and the months after are reported.
func TestEstimatedPenaltyAcrossTheYearEnd(t *testing.T) {
	f := base().facts(t)
	// April 9 months, $69.56; June 7, $53.70; September 4, $30.34.
	want := []money.USCents{6_956, 5_370, 3_034}
	for _, asOf := range []estimated.Date{day(2026, 12, 31), day(2027, 4, 15)} {
		res := must(rules(t).Penalty(f, asOf))
		lines := res.Value().Lines()
		for i, w := range want {
			if lines[i].Total() != w {
				t.Errorf("to %v, installment %d: %v, want %v", asOf, i+1, lines[i].Total(), w)
			}
		}
		if len(lines) != 4 || lines[3].Total() != 0 {
			t.Errorf("to %v: January's line %+v, want no charge", asOf, lines)
		}
		if got := has(res.Required(), working.PositionUnpublishedRate); got != (asOf.Year() == 2027) {
			t.Errorf("to %v, unpublished rate reported %v", asOf, got)
		}
		if res.Value().Total() != 15_360 {
			t.Errorf("to %v: %v, want $153.60", asOf, res.Value().Total())
		}
	}
	// Never past the return's due date.
	if p := must(rules(t).Penalty(f, day(2027, 6, 30))); p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
}

// § 5228(9): no penalty on the fourth installment when the return is filed
// and paid in full by the last day of January. January 31, 2027 is a Sunday,
// and § 153(2) moves it to Monday, February 1.
func TestEstimatedReturnFiledAndPaid(t *testing.T) {
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 1, 29), true}, {day(2027, 2, 1), true}, {day(2027, 2, 2), false}} {
		h := base()
		h.returnFiled = tc.filed
		res := must(rules(t).Schedule(h.facts(t)))
		if got := res.Value()[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
	}
	h := base()
	h.returnFiled = day(2027, 3, 1)
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().AsOf() != day(2027, 3, 1) {
		t.Errorf("figured to %v, want the day the return was paid", p.Value().AsOf())
	}
}

// Form 2210ME line 10a spreads withholding 25% a quarter whenever it was
// withheld, and has no election to use the dates.
func TestEstimatedWithholdingIsSpreadEvenly(t *testing.T) {
	for _, asWithheld := range []bool{false, true} {
		// 90% of $44,444.44 is $40,000, all withheld on December 15.
		h := household{tax: 4_444_444, prior: last(1_000_000 * money.Dollar), withheld: 40_000 * money.Dollar, asWithheld: asWithheld}
		res := must(rules(t).Schedule(h.facts(t)))
		for i, in := range res.Value() {
			if in.Status() != estimated.StatusMet || in.CreditedBy() != money.USCents(i+1)*10_000*money.Dollar {
				t.Errorf("election %v, installment %d: %v with %v credited", asWithheld, i+1, in.Status(), in.CreditedBy())
			}
		}
	}
}

// The annualized method: 22.5%, 45%, 67.5% and 90% of the annualized tax,
// less earlier installments, capped by the regular installment with what the
// method lowered carried forward.
func TestEstimatedAnnualisedMethod(t *testing.T) {
	// 90% of $40,000 is $36,000, $9,000 a quarter. Annualized tax of $8,000,
	// $20,000, $40,000 and $40,000 gives $1,800, $7,200, $18,000 and $9,000.
	h := household{tax: 40_000 * money.Dollar, prior: last(50_000 * money.Dollar),
		annualised: []money.USCents{8_000 * money.Dollar, 20_000 * money.Dollar, 40_000 * money.Dollar, 40_000 * money.Dollar}}
	res := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{1_800, 9_000, 27_000, 36_000} {
		if got := res.Value()[i].NeededBy(); got != want*money.Dollar {
			t.Errorf("installment %d needs %v, want $%d", i+1, got, want)
		}
	}
	wrong := must(estimated.NewFacts(maine, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(100).Build())).
		Periods(must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 6, 30)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).Build())
	if _, err := rules(t).Requirement(wrong); err == nil {
		t.Error("a period ending June 30 was accepted")
	}
}

// Maine's waiver is the assessor's, "for cause". It is asked for when there
// is a penalty, and a waiver asked for is reported without changing the
// total.
func TestEstimatedWaiver(t *testing.T) {
	res := must(rules(t).Penalty(base().facts(t), day(2027, 4, 15)))
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked about")
	}
	k := estimated.WaiverCasualty
	h := base()
	h.waiver = &k
	asked := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if asked.Value().Waiver() != k || asked.Value().Total() != res.Value().Total() || has(asked.Required(), working.PositionPenaltyWaiver) {
		t.Errorf("waiver %v, total %v", asked.Value().Waiver(), asked.Value().Total())
	}
}

// Maine's "for cause" waiver lists no causes, so a casualty or disaster is
// left open and the open point asked. Retirement and disability have no
// waiver, even where the federal one applies.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pos     *estimated.PositionsBuilder
		refused bool
		want    string
	}{
		{"disaster", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)), false, "doesn't say if a casualty or disaster is cause for this penalty"},
		{"retired in 2026 at 64", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)), true, "no waiver for retirement or disability"},
		{"retired in 2020", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, 3, 1)), true, "no waiver for retirement or disability"},
	} {
		h := base()
		pos := must(tc.pos.Build())
		h.positions = &pos
		res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
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

// Under last year's small-liability exception, this year's tax can't change
// the target, so it isn't asked for.
func TestEstimatedExemptAsksNothingOfTheTarget(t *testing.T) {
	res := must(rules(t).Requirement(household{noTax: true, prior: last(500 * money.Dollar)}.facts(t)))
	if !res.Value().Exempt() || has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("exempt %v, asked %v", res.Value().Exempt(), res.Required())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(base().facts(t)))
	want := map[string][2]string{
		"Last year's liability":                 {"after refundable credits", "§ 5228(2)(D), (3)(A)"},
		"The $1,000 test on this year's tax":    {"tax less withholding", "§ 5228(1)(A), (2)(C)"},
		"When withholding counts as paid":       {"25% on each due date", "§ 5228(1)(A), (5)(A)"},
		"A return filed and paid by January 31": {"doesn't mention the relief", "P.L. 2025, c. 113"},
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
		// The base household has no withholding, no payments and no return
		// filed, and 90% of this year's tax sets its target: none reaches it.
		if d.AffectsHousehold() {
			t.Errorf("%q affects the base household", d.Rule())
		}
		wantUses := working.ReadingForm
		if d.Rule() == "A return filed and paid by January 31" {
			wantUses = working.ReadingStatute
		}
		if d.Uses() != wantUses {
			t.Errorf("%q uses %v, want %v", d.Rule(), d.Uses(), wantUses)
		}
	}

	// Each one affects the facts it turns on.
	for _, tc := range []struct {
		rule string
		h    household
	}{
		{"Last year's liability", household{tax: 444_444, prior: last(3_000 * money.Dollar)}},
		{"The $1,000 test on this year's tax", household{tax: 444_444, prior: last(10_000 * money.Dollar), payments: []estimated.Payment{pay(dues[0], 3_500*money.Dollar)}}},
		{"When withholding counts as paid", household{tax: 444_444, prior: last(10_000 * money.Dollar), withheld: 1_000 * money.Dollar}},
		{"A return filed and paid by January 31", household{tax: 444_444, prior: last(10_000 * money.Dollar), returnFiled: day(2027, 1, 29)}},
	} {
		found := false
		for _, d := range must(rules(t).Requirement(tc.h.facts(t))).Divergences() {
			if d.Rule() == tc.rule {
				found = true
				if !d.AffectsHousehold() {
					t.Errorf("%q does not affect the facts it turns on", tc.rule)
				}
			}
		}
		if !found {
			t.Errorf("%q is not reported", tc.rule)
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := mainetaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for _, share := range []func(*estimated.FactsBuilder){
		func(b *estimated.FactsBuilder) { b.FarmShare(7000) },
		func(b *estimated.FactsBuilder) { b.FishShare(7000) },
	} {
		b := estimated.NewFacts(maine, 2026).FilingStatus(filing.Single).ThisYear(this)
		share(b)
		if _, err := rules(t).Requirement(must(b.Build())); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("a farmer or fisher: %v, want ErrUnsupported", err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(maine, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(maine, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Maine's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, b := range map[string]*mainetaxcalc.EstimatedFactsBuilder{
		"one period":           mainetaxcalc.NewEstimatedFacts().IncomeNotWithheld(1, 0, 0),
		"installment 5":        mainetaxcalc.NewEstimatedFacts().IncomeNotWithheld(5, 0, 0),
		"periods and none":     mainetaxcalc.NewEstimatedFacts().IncomeNotWithheld(1, 0, 0).IncomeNotWithheld(2, 0, 0).IncomeNotWithheld(3, 0, 0).IncomeNotWithheld(4, 0, 0).NoUnusualEvent(),
		"a tax with no income": mainetaxcalc.NewEstimatedFacts().UnusualEventTax(1, 100),
		"a period twice":       mainetaxcalc.NewEstimatedFacts().IncomeNotWithheld(1, 0, 0).IncomeNotWithheld(1, 0, 0),
	} {
		if _, err := b.Build(); err == nil {
			t.Errorf("%s: built", name)
		}
	}
	if _, err := rules(t).Penalty(base().facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date was figured")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$1,000", "90%", "100%", "$500,000", "unusual event", "0.75% a month", "compounded monthly", "part of a month", "February 1, 2027", "no 110% tier"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Without this year's tax the target is last year's liability alone, and the
// penalty against it is an upper bound. Under $1,000 last year still exempts
// exactly; with no liability on record there is no target.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: last(20_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 20_000*money.Dollar {
		t.Errorf("required %v, target %v; want $20,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !(pen.Value().UpperBound() || !pen.Value().RatedThrough().IsZero()) || pen.Value().Total() == 0 {
		t.Errorf("unpaid: penalty %v, upper bound %v", pen.Value().Total(), pen.Value().UpperBound())
	}

	h.prior = last(999 * money.Dollar)
	pen = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if pen.Value().Total() != 0 || pen.Value().UpperBound() {
		t.Errorf("last year under $1,000: penalty %v, upper bound %v; want an exact zero", pen.Value().Total(), pen.Value().UpperBound())
	}

	h.prior = nil
	if res := must(rules(t).Requirement(h.facts(t))); res.Value().TargetKnown() {
		t.Error("a target with neither year's tax")
	}
	if _, err := rules(t).Schedule(h.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(h.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("ME"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}
