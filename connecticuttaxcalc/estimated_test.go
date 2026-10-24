// Connecticut's estimated tax rules for 2026, checked against the Department
// of Revenue Services' own documents and the General Statutes.
//
// Connecticut publishes no exhaustive oracle and no worked example with
// figures: CT-2210's examples (Anita, Bill, Jim and the others) say which
// test applies, not what it comes to. So every threshold, percentage, date
// and rate is compared against its extract in testdata/, and the arithmetic
// is checked by hand-worked cases, each worked in its comment both the way
// CT-2210 Schedule B does it and installment by installment as § 12-722(b)
// reads. Those two agree for 2026 because every due date is a 15th; the
// cases are the floor, not an oracle.
package connecticuttaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/connecticuttaxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const ct = estimated.Jurisdiction("CT")

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
	return must(connecticuttaxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind, contains string) bool {
	for _, r := range rs {
		if r.Position() == k && r.Asked() && (strings.Contains(r.Question(), contains) || strings.Contains(r.Effect(), contains)) {
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

func extras(pe money.USCents) *connecticuttaxcalc.EstimatedFacts {
	e := must(connecticuttaxcalc.NewEstimatedFacts().PETaxCredit(pe).Build())
	return &e
}

type household struct {
	tax         money.USCents // CT-1040 line 14
	noTax       bool
	withheld    money.USCents
	withheldOn  estimated.Date // December 15, 2026 unless set
	asWithheld  bool
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *connecticuttaxcalc.EstimatedFacts // a PE Tax Credit of nothing unless set
	returnFiled estimated.Date
	annualised  []money.USCents // CT-2210 Schedule A line 12, for each period
	waiver      *estimated.WaiverKind
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts(ct, 2026).FilingStatus(filing.Single).ThisYear(figures).
		Paid(h.payments...).ReturnFiled(h.returnFiled)
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
		b.State(*extras(0))
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
	return must(b.Build())
}

// prior is a filed 12-month return by a full-year resident.
func prior(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).
		Figures(must(estimated.NewFigures().Tax(tax).Build())).FullYearResident(true).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "ct-1040es-2026.txt",
		"is $1,000 or more; and 2. You expect your Connecticut income tax withheld (including any PE Tax Credit) to be less than your required annual payment for the 2026 taxable year.",
		"• 90% of the income tax shown on your 2026 Connecticut income tax return; or • 100% of the income tax shown on your 2025 Connecticut income tax return if you filed a 2025 Connecticut income tax return that covered a 12‑month period.",
		"Estimated payments for the 2026 taxable year are due on or before April 15, June 15, September 15, 2026, and January 15, 2027.",
		"If the due date falls on a Saturday, Sunday, or legal holiday, your estimated payment will be considered timely if filed on the next business day.",
		"Interest at 1% per month or fraction of a month will be added to the tax due until the earlier of April 15, 2027, or the date on which the underpayment is paid.",
		"If you file your income tax return for the 2026 taxable year on or before January 31, 2027, and pay in full the amount computed on the return as payable on or before that date, you will not be charged interest for failing to make the estimated payment due January 15, 2027.",
		"13. Multiply Line 12 by 90%",
		"Subtract Lines 16 and 16a from Line 12. If less than $1,000, no estimated payment is required.",
		"If you were a resident during the 2025 taxable year and you did not file a 2025 Connecticut income tax return because you had no Connecticut income tax liability, enter “0.”",
		"Special Rules for Farmers and Fishermen: If you are a farmer or fisherman (as defined in Internal Revenue Code § 6654(i)(2))",
	)
	mustContain(t, "ct-2210-2025.txt",
		"If the result is less than $1,000, you are not subject to interest on the underpayment.",
		"4. Subtract Line 3 and Line 3a from Line 1. If the result is less than $1,000, stop here.",
		"2. Multiply Line 1 by 90% (.90).",
		// Lines 9, 11a and 12a: installment, withholding and PE Tax Credit
		// percentages.
		"A B C D .25 .50 .75 1.00 .25 .50 .75 1.00 .25 .50 .75 1.00 Total",
		"12a. PE Tax Credit percentages",
		"1-1-2025 to 3-31-2025 1-1-2025 to 5-31-2025 1-1-2025 to 8-31-2025 1-1-2025 to 12-31-2025 4 2.4 1.5 1 0.225 0.45 0.675 0.90",
		"Form CT‑2210, Schedule A, automatically selects the smaller of the annualized income installment or regular installment increased by the amount saved",
		"Line a - Underpayment .01 Line b - Late payment 4 4-16-2025 to 5-15-2025 Line c - Revised underpayment Line d - Late payment Interest .01 5-16-2025 to 6-15-2025",
		"A month is measured from the sixteenth day of the first month to the fifteenth day of the next month. Any fraction of a month is considered a whole month.",
		"Timely installment payments are all payments (other than any tax withheld) made on or before the estimated payment due date(s) including any previous installment payments.",
		"C. Your interest is lower by treating your Connecticut income tax withheld from your income as paid on the dates it was actually withheld instead of in equal amounts on the payment due dates.",
		"Do not complete Schedule B, Worksheet D, if you file your income tax return and pay the tax amount in full by January 31.",
		"If you check Box E, you must use 90% of your 2025 income tax as your required annual payment.",
		"Box F: Check this box if you were a Connecticut resident during the entire 2024 taxable year and you did not file a 2024 Connecticut income tax return because you did not have a Connecticut income tax liability.",
		"Box G: Check this box if you were a part‑year resident or nonresident of Connecticut during the 2024 taxable year and you had Connecticut‑sourced income in 2024",
	)
	mustContain(t, "cgs-chap229.txt",
		"(12) “Required annual payment” means the lesser of (A) ninety per cent of the tax shown on the return for the taxable year, or, if no return is filed, ninety per cent of the tax for such year, or (B) if the preceding taxable year was a taxable year of twelve months and the individual filed a return for the preceding taxable year, one hundred per cent of the tax shown on the return of the individual for such preceding taxable year.",
		"applying interest (1) at the rate of one per cent per month or fraction thereof, (2) to the amount of the underpayment, (3) for the period of the underpayment.",
		"The fifteenth day of the fourth month of the next succeeding taxable year, or, with respect to any portion of the underpayment, the date on which such portion is paid.",
		"a payment of estimated tax shall be credited against unpaid required installments in the order in which such installments are required to be paid.",
		"The due date for the first required installment is the fifteenth day of the fourth month of the taxable year. The due date for the second required installment is the fifteenth day of the sixth month of the taxable year. The due date for the third required installment is the fifteenth day of the ninth month of the taxable year. The due date for the fourth required installment is the fifteenth day of the first month of the next succeeding taxable year.",
		"the amount of any required installment shall be twenty-five per cent of the required annual payment",
		"the applicable percentage for the first required installment is twenty-two and one-half, the applicable percentage for the second required installment is forty-five, the applicable percentage for the third required installment is sixty-seven and one-half, and the applicable percentage for the fourth required installment is ninety.",
		"reduced by the tax withheld under this chapter, is less than one thousand dollars.",
		"(A) the preceding taxable year was a taxable year of twelve months and (B) the individual did not have any liability for tax for the preceding taxable year and throughout such year the individual was (i) a resident individual or (ii) a nonresident individual or part-year resident individual with income, gain, loss or deduction derived from or connected with sources within this state.",
		"an equal part of such tax withheld shall be deemed paid on each due date for such taxable year, unless the taxpayer establishes the dates on which such tax was actually withheld",
		"If, on or before January thirty-first of the following taxable year, the taxpayer files a return for the taxable year and pays in full the amount computed on the return as payable, then no addition to tax shall be imposed under subsection (a) of this section with respect to any underpayment of the fourth required installment for the taxable year.",
		"(1) There shall be only one required installment for the taxable year, (2) the due date for such installment shall be January fifteenth of the following taxable year",
	)
	// The 2026 Supplement amends § 12-701 and leaves (a)(12) as it was; it
	// does not touch § 12-722.
	mustContain(t, "cgs-chap229-2026-supplement.txt",
		"Note: This 2026 Supplement is intended to be used in conjunction with the General Statutes of Connecticut, revised to January 1, 2025.",
		"(12) “Required annual payment” means the lesser of (A) ninety per cent of the tax shown on the return for the taxable year",
	)
	if strings.Contains(extract(t, "cgs-chap229-2026-supplement.txt"), "Sec. 12-722") {
		t.Error("the 2026 Supplement amends § 12-722, and these rules have not been checked against it")
	}
}

// The 2026 dates are the ones CT-1040ES prints. None falls on a weekend, so
// none moves; the test still checks each computed date against the printed
// one.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, prior: prior(20_000 * money.Dollar)}.facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
		if want := money.USCents(i+1) * 2_250 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want a quarter of $9,000 for each date so far, %v", i+1, in.NeededBy(), want)
		}
	}
	// January 31, 2027, the file-and-pay date, is a Sunday and does not move.
	if wd := day(2027, 1, 31).Weekday(); wd != time.Sunday {
		t.Errorf("January 31, 2027 is a %v", wd)
	}
}

// The $1,000 test, as the forms word it: the tax less withholding and the PE
// Tax Credit must be $1,000 or more.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld, pe money.USCents
		exempt       bool
	}{
		{900_001, 0, true},       // $999.99 left
		{900_000, 0, false},      // $1,000.00
		{800_000, 100_001, true}, // $999.99 after the credit
		{800_000, 100_000, false},
	} {
		h := household{tax: 10_000 * money.Dollar, withheld: tc.withheld, extras: extras(tc.pe), prior: prior(100_000 * money.Dollar)}
		r := must(rules(t).Requirement(h.facts(t))).Value()
		if r.Exempt() != tc.exempt {
			t.Errorf("withheld %v, PE Tax Credit %v: exempt %v, want %v (%s)", tc.withheld, tc.pe, r.Exempt(), tc.exempt, r.Why())
		}
	}
}

// 90% of this year's tax or 100% of last year's, whichever is less, with no
// higher percentage at any income; and when last year's is not open.
func TestEstimatedTargetOptions(t *testing.T) {
	r := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: prior(50_000 * money.Dollar)}.facts(t))).Value()
	if c, _ := r.Chosen(); c.Basis() != estimated.BasisThisYearTax || r.Target() != 36_000*money.Dollar {
		t.Errorf("chose %v at %v, want 90%% of $40,000, $36,000", c.Basis(), r.Target())
	}

	// A high earner's prior-year test is still 100%.
	rich := must(estimated.NewPriorReturn().Filed(true).FullYearResident(true).
		Figures(must(estimated.NewFigures().Tax(30_000 * money.Dollar).FederalAGI(2_000_000 * money.Dollar).StateAGI(2_000_000 * money.Dollar).Build())).Build())
	r = must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: &rich}.facts(t))).Value()
	if c, _ := r.Chosen(); c.Basis() != estimated.BasisPriorYearTax || c.Percent() != money.Percent(100) || r.Target() != 30_000*money.Dollar {
		t.Errorf("chose %v at %v of %v, want 100%% of last year's $30,000", c.Basis(), c.Percent(), r.Target())
	}

	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).FullYearResident(true).
		Figures(must(estimated.NewFigures().Tax(1_000 * money.Dollar).Build())).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		asked bool
	}{
		"no return on record": {nil, true},
		"not filed":           {&notFiled, false},
		"an 11-month return":  {&short, false},
	} {
		res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: tc.prior}.facts(t)))
		r := res.Value()
		if o := r.Options()[1]; o.Available() || o.WhyNot() == "" {
			t.Errorf("%s: the prior-year test is offered", name)
		}
		if r.Target() != 36_000*money.Dollar {
			t.Errorf("%s: target %v, want $36,000", name, r.Target())
		}
		if got := has(res.Required(), working.PositionPriorYearReturn, ""); got != tc.asked {
			t.Errorf("%s: last year's return asked for %v, want %v", name, got, tc.asked)
		}
	}
}

// § 12-722(j)(2): no liability last year over twelve months, as a resident
// (CT-2210 Box F) or a nonresident with Connecticut-source income (Box G). A
// nonresident without it must pay 90% of this year's tax (Box E).
func TestEstimatedNoLiabilityException(t *testing.T) {
	notRequired := func(resident *bool, months int) *estimated.PriorReturn {
		b := estimated.NewPriorReturn().NotRequiredToFile().Months(months)
		if resident != nil {
			b.FullYearResident(*resident)
		}
		p := must(b.Build())
		return &p
	}
	yes, no := true, false
	sourced := func(had bool) *connecticuttaxcalc.EstimatedFacts {
		e := must(connecticuttaxcalc.NewEstimatedFacts().PETaxCredit(0).PriorYearConnecticutSourceIncome(had).Build())
		return &e
	}
	zeroTax := prior(0)
	for name, tc := range map[string]struct {
		prior  *estimated.PriorReturn
		extras *connecticuttaxcalc.EstimatedFacts
		exempt bool
		ask    working.PositionKind
		says   string
	}{
		"Box F, a resident":                   {notRequired(&yes, 12), nil, true, working.PositionUnspecified, ""},
		"Box G, Connecticut-source income":    {notRequired(&no, 12), sourced(true), true, working.PositionUnspecified, ""},
		"Box E, no Connecticut-source income": {notRequired(&no, 12), sourced(false), false, working.PositionUnspecified, ""},
		"source income not given":             {notRequired(&no, 12), nil, false, working.PositionJurisdictionFact, "Did you have any Connecticut income in 2025?"},
		"residence not given":                 {notRequired(nil, 12), nil, false, working.PositionResidency, "Did you live in Connecticut all of 2025"},
		"a short year":                        {notRequired(&yes, 11), nil, false, working.PositionUnspecified, ""},
		"a filed return showing no tax":       {zeroTax, nil, true, working.PositionUnspecified, ""},
	} {
		h := household{tax: 40_000 * money.Dollar, prior: tc.prior, extras: tc.extras}
		res := must(rules(t).Requirement(h.facts(t)))
		r := res.Value()
		if r.Exempt() != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", name, r.Exempt(), tc.exempt, r.Why())
		}
		if !tc.exempt && name != "a filed return showing no tax" && r.Target() != 36_000*money.Dollar {
			t.Errorf("%s: target %v, want 90%% of this year's tax, $36,000", name, r.Target())
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask, tc.says) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
		if name == "Box E, no Connecticut-source income" {
			if o := r.Options()[1]; o.Available() || o.Amount() != 0 || !strings.Contains(o.WhyNot(), "no Connecticut income in 2025") {
				t.Errorf("%s: last year's option available %v, %v, %q", name, o.Available(), o.Amount(), o.WhyNot())
			}
		}
		if name == "Box F, a resident" {
			// The exception is the chosen option, at $0.
			if c, ok := r.Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "No Connecticut tax last year" || r.Target() != 0 {
				t.Errorf("%s: chosen %v %v %q, target %v", name, c.Basis(), c.Amount(), c.Label(), r.Target())
			}
		}
		if tc.exempt {
			p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
			if p.Value().Total() != 0 || len(p.Value().Lines()) != 0 {
				t.Errorf("%s: interest of %v", name, p.Value().Total())
			}
		}
	}
}

// The interest is 1% for each month or part of a month, the months running
// from the 16th to the 15th, and stops on April 15, 2027.
//
// Each case is $40,000 of tax and a $50,000 prior year, so 90%, $36,000, is
// the target and $9,000 is due each quarter, with nothing withheld.
//
// Connecticut's rate is fixed by § 12-722(a), not published by notice, so
// there is no rate change in 2026 or 2027 to span. The first case spans the
// calendar year and the change of column anyway, from September 15, 2026 to
// April 15, 2027, at 1% throughout, and no unpublished rate is reported.
func TestEstimatedInterestByTheMonth(t *testing.T) {
	last := prior(50_000 * money.Dollar)

	// September is $5,000 short, and nothing more is paid until $9,000 on
	// January 15.
	//
	// CT-2210: column C's underpayment is $27,000 less $22,000, $5,000, for
	// the four months from September 16 to January 15: 4 × $50 = $200. The
	// January payment comes in the fourth month and does not lower it.
	// Column D is $36,000 less $31,000, $5,000, for January 16 to April 15:
	// 3 × $50 = $150. $350 in all.
	//
	// § 12-722(b): the third installment's $5,000 is paid by the January 15
	// payment, oldest first, 4 months, $200; the fourth installment is then
	// $5,000 short to April 15, 3 months, $150.
	h := household{tax: 40_000 * money.Dollar, prior: last,
		payments: quarterly(9_000*money.Dollar, 9_000*money.Dollar, 4_000*money.Dollar, 9_000*money.Dollar)}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	p := res.Value()
	if p.Total() != 350*money.Dollar || len(p.Lines()) != 2 {
		t.Fatalf("interest %v over %d lines, want $350 over two", p.Total(), len(p.Lines()))
	}
	for i, want := range []struct {
		installment int
		months      int
		total       money.USCents
		days        int
	}{{3, 4, 200 * money.Dollar, 122}, {4, 3, 150 * money.Dollar, 90}} {
		l := p.Lines()[i]
		if l.Installment() != want.installment || l.Shortfall() != 5_000*money.Dollar || len(l.Parts()) != want.months || l.Total() != want.total || l.Days() != want.days {
			t.Errorf("line %d: installment %d, %v short, %d months, %v, %d days; want installment %d, $5,000, %d months, %v, %d days",
				i+1, l.Installment(), l.Shortfall(), len(l.Parts()), l.Total(), l.Days(), want.installment, want.months, want.total, want.days)
		}
		for _, part := range l.Parts() {
			if part.Rate() != money.Percent(1) || part.Months() != 1 || part.Principal() != 5_000*money.Dollar || part.Charge() != 50*money.Dollar {
				t.Errorf("line %d part %v to %v: %v of %v for %d months is %v", i+1, part.From(), part.To(), part.Rate(), part.Principal(), part.Months(), part.Charge())
			}
		}
	}
	if has(res.Required(), working.PositionUnpublishedRate, "") {
		t.Error("an unpublished rate is reported, and § 12-722(a) fixes the rate")
	}

	// A part of a month is a whole one. April's $9,000 paid on April 16 is a
	// day late and charged a month, $90; paid May 15 it is still one month;
	// paid May 16 it is two, $180.
	for _, tc := range []struct {
		paid estimated.Date
		want money.USCents
	}{
		{day(2026, 4, 15), 0},
		{day(2026, 4, 16), 90 * money.Dollar},
		{day(2026, 5, 15), 90 * money.Dollar},
		{day(2026, 5, 16), 180 * money.Dollar},
	} {
		h := household{tax: 40_000 * money.Dollar, prior: last,
			payments: []estimated.Payment{pay(tc.paid, 9_000*money.Dollar), pay(dues[1], 9_000*money.Dollar), pay(dues[2], 9_000*money.Dollar), pay(dues[3], 9_000*money.Dollar)}}
		if got := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value().Total(); got != tc.want {
			t.Errorf("April paid %v: interest %v, want %v", tc.paid, got, tc.want)
		}
	}

	// A payment during a month lowers the next month's charge, not that
	// month's. September is $5,000 short and paid on October 20: September 16
	// to October 15 on $5,000 is $50, and October 16 to November 15 is still
	// on $5,000, $50, because the payment came during it. $100.
	h = household{tax: 40_000 * money.Dollar, prior: last,
		payments: append(quarterly(9_000*money.Dollar, 9_000*money.Dollar, 4_000*money.Dollar, 9_000*money.Dollar), pay(day(2026, 10, 20), 5_000*money.Dollar))}
	p = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if p.Total() != 100*money.Dollar || len(p.Lines()) != 1 || p.Lines()[0].Days() != 35 {
		t.Errorf("interest %v over %d lines, want $100 on one line of 35 days", p.Total(), len(p.Lines()))
	}

	// Figured to a date: nothing on April 15, a month from April 16.
	unpaid := household{tax: 40_000 * money.Dollar, prior: last}
	for _, tc := range []struct {
		asOf estimated.Date
		want money.USCents
	}{{day(2026, 4, 15), 0}, {day(2026, 4, 16), 90 * money.Dollar}, {day(2026, 6, 15), 180 * money.Dollar}} {
		if got := must(rules(t).Penalty(unpaid.facts(t), tc.asOf)).Value().Total(); got != tc.want {
			t.Errorf("unpaid, to %v: %v, want %v", tc.asOf, got, tc.want)
		}
	}
	// Never beyond April 15, 2027.
	if got := must(rules(t).Penalty(unpaid.facts(t), day(2027, 6, 30))).Value().AsOf(); got != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", got)
	}
}

// § 12-722(l): a return filed and paid by January 31 takes the interest off
// the January installment. CT-2210 skips Worksheet D, so an earlier
// shortfall still unpaid is not charged after January 15 either.
func TestEstimatedReturnFiledAndPaid(t *testing.T) {
	last := prior(50_000 * money.Dollar)
	payments := quarterly(9_000*money.Dollar, 9_000*money.Dollar, 4_000*money.Dollar)

	// Filed and paid on January 25: September's $5,000 is charged for the
	// four months to January 15, $200. The statute would charge the fifth
	// month too, January 16 to February 15, for $250.
	h := household{tax: 40_000 * money.Dollar, prior: last, payments: payments, returnFiled: day(2027, 1, 25)}
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[3].Status() != estimated.StatusExcused {
		t.Errorf("January is %v, want excused", s[3].Status())
	}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 200*money.Dollar || res.Value().AsOf() != day(2027, 1, 25) {
		t.Errorf("interest %v to %v, want $200 to January 25", res.Value().Total(), res.Value().AsOf())
	}
	for _, d := range res.Divergences() {
		if want := d.Rule() == "A return filed and paid by Jan 31, 2027"; d.AffectsHousehold() != want {
			t.Errorf("%q affects the household %v, want %v", d.Rule(), d.AffectsHousehold(), want)
		}
	}

	// January 31, a Sunday, is the last day; February 1 is too late. Then
	// column D is $36,000 less $22,000, $14,000, for January 16 to February
	// 15, $140, on top of the $200.
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
		want    money.USCents
	}{{day(2027, 1, 31), true, 200 * money.Dollar}, {day(2027, 2, 1), false, 340 * money.Dollar}} {
		h := household{tax: 40_000 * money.Dollar, prior: last, payments: payments, returnFiled: tc.filed}
		if got := must(rules(t).Schedule(h.facts(t))).Value()[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
		if got := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value().Total(); got != tc.want {
			t.Errorf("filed %v: interest %v, want %v", tc.filed, got, tc.want)
		}
	}
}

// Withholding is spread in four equal parts unless the filer shows the dates
// it was withheld (§ 12-722(k), CT-2210 Box C).
//
// $30,000 of tax and a $40,000 prior year: the target is $27,000, $6,750 a
// quarter. $18,000 was withheld on January 5 and nothing else paid.
//
// Spread: $4,500 a quarter, so each column is $2,250 further short: $2,250
// for 2 months, $4,500 for 3, $6,750 for 4 and $9,000 for 3, which is $45 +
// $135 + $270 + $270 = $720. Installment by installment, $2,250 short for 12,
// 10, 7 and 3 months is $270 + $225 + $157.50 + $67.50 = $720.
//
// On the date withheld: the $18,000 covers April and June, and September is
// $2,250 short and January $9,000, which is 4 × $22.50 + 3 × $90 = $360.
func TestEstimatedWithholdingOnTheDatesWithheld(t *testing.T) {
	h := household{tax: 30_000 * money.Dollar, prior: prior(40_000 * money.Dollar), withheld: 18_000 * money.Dollar, withheldOn: day(2026, 1, 5)}
	if got := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value().Total(); got != 720*money.Dollar {
		t.Errorf("spread: %v, want $720", got)
	}
	h.asWithheld = true
	if got := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value().Total(); got != 360*money.Dollar {
		t.Errorf("as withheld: %v, want $360", got)
	}
}

// The PE Tax Credit counts like withholding, a quarter by each date even
// under Box C, and the open question about its statutory basis is reported.
func TestEstimatedPETaxCredit(t *testing.T) {
	// $20,000 of tax and a $30,000 prior year: $18,000, $4,500 a quarter. A
	// $4,000 credit is $1,000 a quarter, and $3,500 paid each date meets it.
	for _, asWithheld := range []bool{false, true} {
		h := household{tax: 20_000 * money.Dollar, prior: prior(30_000 * money.Dollar), extras: extras(4_000 * money.Dollar),
			payments: quarterly(3_500*money.Dollar, 3_500*money.Dollar, 3_500*money.Dollar, 3_500*money.Dollar), asWithheld: asWithheld}
		sched := must(rules(t).Schedule(h.facts(t)))
		for i, in := range sched.Value() {
			if want := money.USCents(i+1) * 4_500 * money.Dollar; in.CreditedBy() != want || in.Status() != estimated.StatusMet {
				t.Errorf("Box C %v: installment %d credited %v and %v, want %v and met", asWithheld, i+1, in.CreditedBy(), in.Status(), want)
			}
		}
		if !has(sched.Required(), working.PositionUnsettledRule, "PE Tax Credit as a payment on each due date") {
			t.Errorf("Box C %v: the credit's statutory basis is not reported as open", asWithheld)
		}
		if got := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value().Total(); got != 0 {
			t.Errorf("Box C %v: interest %v", asWithheld, got)
		}
	}

	// The credit takes $4,800 of tax under $1,000 on the forms, which the
	// statute's words would not; the exemption follows the forms and says so.
	res := must(rules(t).Requirement(household{tax: 4_800 * money.Dollar, extras: extras(3_900 * money.Dollar), prior: prior(10_000 * money.Dollar)}.facts(t)))
	if !res.Value().Exempt() || !has(res.Required(), working.PositionUnsettledRule, "PE Tax Credit in the $1,000 test") {
		t.Errorf("exempt %v, asked %v: %s", res.Value().Exempt(), res.Required(), res.Value().Why())
	}
	// The exception is chosen, at $0, labelled with the figures it tested.
	if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding and PE Tax Credit under $1,000" {
		t.Errorf("chosen %v %v %q", c.Basis(), c.Amount(), c.Label())
	}
	// Both PE Tax Credit divergences change this household's answer.
	for _, d := range res.Divergences() {
		if want := strings.HasPrefix(d.Rule(), "The PE Tax Credit"); d.AffectsHousehold() != want {
			t.Errorf("%q affects the household %v, want %v", d.Rule(), d.AffectsHousehold(), want)
		}
	}

	// Not given: none is assumed, and it is asked for.
	f := must(estimated.NewFacts(ct, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(20_000 * money.Dollar).Build())).Build())
	if res := must(rules(t).Requirement(f)); !has(res.Required(), working.PositionJurisdictionFact, "Pass-Through Entity Tax Credit") {
		t.Errorf("the credit is not asked for: %v", res.Required())
	}
}

// Schedule A: 22.5%, 45%, 67.5% and 90% of the annualised tax, less earlier
// installments, never more than the regular $9,000 plus what earlier
// installments saved.
//
// Annualised tax of $8,000, $16,000, $30,000 and $40,000 gives $1,800 (9,000
// regular, $7,200 saved); $7,200 less $1,800 is $5,400 (16,200 regular,
// $10,800 saved); $20,250 less $7,200 is $13,050 (19,800 regular, $6,750
// saved); $36,000 less $20,250 is $15,750, which is the regular $9,000 plus
// the $6,750 saved.
func TestEstimatedAnnualisedRecapture(t *testing.T) {
	h := household{tax: 40_000 * money.Dollar, prior: prior(50_000 * money.Dollar),
		annualised: []money.USCents{8_000 * money.Dollar, 16_000 * money.Dollar, 30_000 * money.Dollar, 40_000 * money.Dollar}}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{1_800 * money.Dollar, 7_200 * money.Dollar, 20_250 * money.Dollar, 36_000 * money.Dollar} {
		if got := sched.Value()[i].NeededBy(); got != want {
			t.Errorf("installment %d needs %v, want %v", i+1, got, want)
		}
	}
	// While this year's tax is known, the regular amount always binds the
	// fourth installment, because the target is at most 90% of the tax. It
	// is the 90% that binds when the target is last year's tax alone: a
	// $50,000 prior year is $12,500 a quarter, and the same periods give
	// $1,800, $5,400 and $13,050 as before, then $15,750 against the regular
	// $12,500 plus $17,250 saved, so $36,000 by January.
	h = household{noTax: true, prior: prior(50_000 * money.Dollar), annualised: h.annualised}
	if got := must(rules(t).Schedule(h.facts(t))).Value()[3].NeededBy(); got != 36_000*money.Dollar {
		t.Errorf("without this year's tax, January needs %v, want $36,000", got)
	}
	// A period that does not end where Schedule A's does is refused.
	bad := must(estimated.NewFacts(ct, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(100).Build())).
		Periods(must(estimated.NewPeriod().End(day(2026, 2, 28)).Build()), must(estimated.NewPeriod().End(day(2026, 4, 30)).Build()),
			must(estimated.NewPeriod().End(day(2026, 7, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 11, 30)).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).Build())
	if _, err := rules(t).Requirement(bad); err == nil {
		t.Error("the trusts' periods are accepted for an individual")
	}
}

// Last year's overpayment applied forward counts on April 15, and the
// question of its date is reported.
func TestEstimatedPriorYearOverpayment(t *testing.T) {
	applied := must(estimated.NewPayment().Amount(9_000 * money.Dollar).FromPriorYear().Build())
	h := household{tax: 40_000 * money.Dollar, prior: prior(50_000 * money.Dollar),
		payments: []estimated.Payment{applied, pay(dues[1], 9_000*money.Dollar), pay(dues[2], 9_000*money.Dollar), pay(dues[3], 9_000*money.Dollar)}}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 0 || !has(res.Required(), working.PositionUnsettledRule, "overpayment") {
		t.Errorf("interest %v, asked %v", res.Value().Total(), res.Required())
	}
}

// Connecticut has no casualty, retirement or disability waiver. A filer who
// asks is told so and the interest is not reduced; one who does not is told
// the question of other chapter 229 provisions is open.
func TestEstimatedNoWaiver(t *testing.T) {
	h := household{tax: 40_000 * money.Dollar, prior: prior(50_000 * money.Dollar)}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !has(res.Required(), working.PositionPenaltyWaiver, "interest shown stands unless another law lets the department waive it") {
		t.Errorf("the waiver question is not reported: %v", res.Required())
	}
	k := estimated.WaiverCasualty
	h.waiver = &k
	asked := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if asked.Value().Total() != res.Value().Total() || has(asked.Required(), working.PositionPenaltyWaiver, "") {
		t.Errorf("a casualty waiver changed the interest to %v from %v", asked.Value().Total(), res.Value().Total())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][4]string{
		"The PE Tax Credit in the $1,000 test":            {"subtracts withholding and the PE Tax Credit", "Part 2 line 4", "reduced by the tax withheld", "§ 12-722(j)(1)"},
		"The PE Tax Credit as a payment by each due date": {"a quarter of the PE Tax Credit as paid by each due date", "Part 3 lines 12 to 13", "only tax withheld", "§ 12-722(k)"},
		"A return filed and paid by Jan 31, 2027":         {"no interest after Jan 15, 2027 on any installment", "Do not complete Schedule B, Worksheet D", "only the fourth installment's underpayment", "§ 12-722(b), (l)"},
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
		// The figures follow the forms, and with no PE Tax Credit and no
		// early return neither reading changes anything.
		if d.Uses() != working.ReadingForm || d.AffectsHousehold() {
			t.Errorf("%q uses %v and affects the household %v; want the form, and false", d.Rule(), d.Uses(), d.AffectsHousehold())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := connecticuttaxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, share := range map[string]func(*estimated.FactsBuilder) *estimated.FactsBuilder{
		"a farmer":    func(b *estimated.FactsBuilder) *estimated.FactsBuilder { return b.FarmShare(7_000) },
		"a fisherman": func(b *estimated.FactsBuilder) *estimated.FactsBuilder { return b.FishShare(7_000) },
	} {
		f := must(share(estimated.NewFacts(ct, 2026).FilingStatus(filing.Single).ThisYear(this)).Build())
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"Pennsylvania's facts": must(estimated.NewFacts(estimated.Pennsylvania, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":                 must(estimated.NewFacts(ct, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":         must(estimated.NewFacts(ct, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Connecticut's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := rules(t).Penalty(f, day(2027, 4, 15)); err == nil {
			t.Errorf("%s: interest figured", name)
		}
	}
	if _, err := connecticuttaxcalc.NewEstimatedFacts().PETaxCredit(-1).Build(); err == nil {
		t.Error("a negative PE Tax Credit is accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"$1,000", "1% a month", "part of a month", "16th to the 15th", "no 110%", "Pass-Through Entity Tax Credit",
		"no waiver", "interest rather than a penalty", "January 31", "April 15"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if len(e.Sources()) == 0 || e.VersusFederal() == "" {
		t.Error("the explanation has no sources or no comparison with federal")
	}
}

// Without this year's tax, the target is 100% of last year's, and the
// interest figured against it is the most it can be.
//
// A $30,000 prior year is $7,500 a quarter. Nothing paid, to April 15, 2027:
// columns A to D are $7,500, $15,000, $22,500 and $30,000 short for 2, 3, 4
// and 3 months, $150 + $450 + $900 + $900 = $2,400. Installment by
// installment, $7,500 for 12, 10, 7 and 3 months: $900 + $750 + $525 + $225.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(30_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 30_000*money.Dollar {
		t.Errorf("required %v, target %v; want $30,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax, "") {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if pen.Total() != 2_400*money.Dollar || !pen.UpperBound() {
		t.Errorf("interest %v, upper bound %v; want at most $2,400", pen.Total(), pen.UpperBound())
	}

	// No prior year either: no target, and nothing to schedule.
	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() || r.Required() {
		t.Errorf("target known %v, required %v", r.TargetKnown(), r.Required())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("interest: %v, want ErrNeedsThisYearTax", err)
	}

	// The no-liability exception needs nothing about this year, and its zero
	// is exact.
	boxF := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	exempt := household{noTax: true, prior: &boxF}
	if p := must(rules(t).Penalty(exempt.facts(t), day(2027, 4, 15))).Value(); p.Total() != 0 || p.UpperBound() {
		t.Errorf("Box F without this year's tax: %v, upper bound %v; want an exact zero", p.Total(), p.UpperBound())
	}
}

// An exception sets the target, so no question may say its answer changes
// the target. $500 of tax, with last year's return not given.
func TestEstimatedExceptionAsksNothingOfTheTarget(t *testing.T) {
	b := estimated.NewFacts(estimated.Jurisdiction("CT"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(500 * money.Dollar).Build()))
	res := must(must(connecticuttaxcalc.EstimatedTax(2026)).Requirement(must(b.Build())))
	if !res.Value().Exempt() {
		t.Fatal("no exception applies")
	}
	for _, r := range res.Required() {
		if strings.Contains(strings.ToLower(r.Effect()), "target") {
			t.Errorf("%v: %q", r.Position(), r.Effect())
		}
	}
}

// The waiver asked for is decided from Conn. Gen. Stat. § 12-722 and the
// CT-2210 instructions, which give none. The penalty stays the figure before
// any waiver, and IfWaived shows $0 only when the waiver is open to the
// facts.

// waiverPenalty is the penalty on $10,000 of tax with nothing paid or
// withheld, figured to Apr 15, 2027, with the waiver asked for as pos.
func waiverPenalty(t *testing.T, pos estimated.Positions) working.Result[estimated.Penalty] {
	t.Helper()
	f := must(estimated.NewFacts(estimated.Jurisdiction("CT"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Positions(pos).Build())
	return must(must(connecticuttaxcalc.EstimatedTax(2026)).Penalty(f, day(2027, 4, 15)))
}

func waiverFor(c estimated.WaiverCause, on, born estimated.Date) estimated.Positions {
	return must(estimated.NewPositions().WaiverFor(c, on).Born(born).Build())
}

type waiverCase struct {
	name   string
	pos    estimated.Positions
	want   string // eligible, refused or undecided
	reason string // a phrase of the outcome's reason
}

// checkWaivers checks each case's outcome, and that the penalty is the one
// figured with no waiver asked for.
func checkWaivers(t *testing.T, cases []waiverCase) {
	t.Helper()
	before := waiverPenalty(t, must(estimated.NewPositions().Waiver(estimated.WaiverNone).Build())).Value().Total()
	if before == 0 {
		t.Fatal("no penalty arises, so no waiver is decided")
	}
	for _, c := range cases {
		p := waiverPenalty(t, c.pos).Value()
		o, ok := p.WaiverOutcome()
		if !ok {
			t.Errorf("%s: no waiver outcome", c.name)
			continue
		}
		got, ifWaived := "undecided", p.Total()
		switch {
		case o.Eligible():
			got, ifWaived = "eligible", 0
		case o.Refused():
			got = "refused"
		}
		if got != c.want || !strings.Contains(o.Reason(), c.reason) {
			t.Errorf("%s: %s, %q; want %s, mentioning %q", c.name, got, o.Reason(), c.want, c.reason)
		}
		if p.Total() != before || p.IfWaived() != ifWaived {
			t.Errorf("%s: penalty %v, %v if waived; want %v, %v if waived", c.name, p.Total(), p.IfWaived(), before, ifWaived)
		}
	}
}

// Connecticut gives no waiver of the interest, so each is refused, including
// a retirement the federal waiver would cover.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	checkWaivers(t, []waiverCase{
		{"retired in 2026 at 64", waiverFor(estimated.CauseRetirement, day(2026, 6, 1), day(1962, 3, 1)), "refused", "no waiver of the interest for retirement or disability"},
		{"retired in 2020", waiverFor(estimated.CauseRetirement, day(2020, 6, 1), day(1955, 3, 1)), "refused", "retirement or disability"},
		{"disabled in 2026", waiverFor(estimated.CauseDisability, day(2026, 2, 1), estimated.Date{}), "refused", "retirement or disability"},
		{"a casualty", waiverFor(estimated.CauseCasualty, day(2026, 8, 1), estimated.Date{}), "refused", "for a casualty or disaster"},
		{"a disaster", waiverFor(estimated.CauseDisaster, day(2026, 8, 1), estimated.Date{}), "refused", "for a casualty or disaster"},
	})
}
