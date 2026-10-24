// Minnesota's estimated tax rules for 2026, checked against the Department of
// Revenue's own documents and the Minnesota Statutes.
//
// Minnesota publishes no exhaustive oracle and no worked penalty example for
// estimated tax. Schedule M15 defines the line arithmetic, and these tests
// follow it line by line: days ÷ 365 × 7% × the column's underpayment. Every
// threshold, rate, date and percentage is compared against its extract in
// testdata/. The cases themselves are hand-written.
package minnesotataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/minnesotataxcalc"
	"github.com/nexteggapp/taxcalcs/money"
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
	return must(minnesotataxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

func because(rs []working.Required, k working.PositionKind, phrase string) bool {
	for _, r := range rs {
		if r.Position() == k && (strings.Contains(r.Question(), phrase) || strings.Contains(r.Effect(), phrase)) {
			return true
		}
	}
	return false
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
	tax         money.USCents // Form M1 line 17
	noTax       bool
	withheld    money.USCents // withheld on December 15
	refundable  *money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	residence   []estimated.ResidencePeriod
	returnFiled estimated.Date
	elections   *estimated.Elections
	periods     []money.USCents // annualized tax for each period
	extras      *minnesotataxcalc.EstimatedFacts
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts("MN", 2026).FilingStatus(filing.Single).ThisYear(figures).
		Paid(h.payments...).Residence(h.residence...).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	switch {
	case h.extras != nil:
		b.State(*h.extras)
	default:
		var rc money.USCents
		if h.refundable != nil {
			rc = *h.refundable
		}
		b.State(must(minnesotataxcalc.NewEstimatedFacts().RefundableCredits(rc).Build()))
	}
	if h.elections != nil {
		b.Elections(*h.elections)
	}
	if h.periods != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.periods {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		b.Elections(must(estimated.NewElections().Annualised().Build()))
	}
	if h.waiver != nil {
		b.Positions(must(estimated.NewPositions().Waiver(*h.waiver).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

// prior is a filed twelve-month return by a full-year resident.
func prior(tax, agi money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).FederalAGI(agi).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(true).Build())
	return &p
}

// partYear is a filed return by somebody who was not a Minnesota resident
// all of last year.
func partYear(tax, federalAGI, minnesotaAGI money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).FederalAGI(federalAGI).StateAGI(minnesotaAGI).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(false).Build())
	return &p
}

func cents(a money.USCents) *money.USCents { return &a }

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "m15-2026.txt",
		"FINAL DRAFT — 10/1/26",
		"If less than $500, STOP HERE; you do not owe an underpayment penalty",
		"Multiply line 1 by 90% (0.90).",
		"100% of your prior year’s total tax liability (110% if your federal adjusted gross income is more than $150,000)",
		"If you are a nonresident or part-year resident, you must have had at least $1 of Minnesota tax liability to use 100% of your prior year’s tax.",
		"If you did not file a 2025 return, or it did not cover the full 12-month period, skip line 5",
		"If you are a nonresident or part-year resident, use your adjusted gross income assignable to Minnesota.",
		"Regular Method April 15, 2026 June 15, 2026 Sept. 15, 2026 Jan. 15, 2027",
		"Enter 25% (0.25) of line 6 in each column",
		"Enter the date of payment or April 15, 2027, whichever is earlier",
		"Divide line 18 by 365.",
		"Multiply line 19 by 7% (0.07).",
		"1/1/26–3/31/26 1/1/26–5/31/26 1/1/26–8/31/26 1/1/26–12/31/26",
		"Step 7 Percentage for each period. 22.5% 45% 67.5% 90%",
		"Enter 25% (.25) of line 6 of Schedule M15 in each column.",
		"You are considered to have paid Minnesota withholding evenly during the year unless you show otherwise.",
		"Any refundable credits you are eligible for and claim. You are considered to have received refundable credits evenly during the year unless you show otherwise.",
		"If you filed your 2026 Minnesota return and paid the tax you owed on or before January 31, 2027, you may consider the tax paid as of January 15, 2027.",
		"A payment of estimated tax is considered to be made for any unpaid installment in the earliest period for which the full required payment has not been made.",
		"Otherwise, enter the date you paid your underpayment on line 16 in full.",
		"You did not have a Minnesota tax liability on line 17 of your 2025 Form M1, Individual Income Tax.",
		"You were a Minnesota resident for all of 2025.",
		"Your 2025 return covered a 12-month period.",
		"If the IRS does not require you to pay additional charges for underestimating your federal tax because you are newly retired or disabled",
		"at least two-thirds of your annual gross income by farming or commercial fishing",
		"Multiply line 9 by 2% (0.02)",
		"x .000192",
	)
	mustContain(t, "mndor-estimated-tax.txt",
		"You must pay estimated tax if you expect to owe $500 or more in Minnesota income tax after subtracting your withholding and refundable credits.",
		"If you are a nonresident or part-year resident, you must have had at least $1 of Minnesota tax liability to use 100% of your prior year’s tax.",
		"If the due date falls on a Saturday, Sunday, or legal holiday, the Minnesota Department of Revenue must receive your payment by the next business day.",
		"You do not need to make the January 15 payment if you file your Minnesota income tax return and pay the entire amount by January 31.",
	)
	mustContain(t, "mndor-interest-rates.txt", "Year | 2026 | 2024-2025 |", "Interest Rate | 7% | 8% |")
	mustContain(t, "minn-stat-289A.25.txt",
		"reduced by the credits allowable is less than $500",
		"The amount of any installment required to be paid shall be 25 percent of the required annual payment",
		"90 percent of the tax shown on the return for the taxable year",
		`exceeds $150,000, this clause shall be applied by substituting "110 percent of the total tax liability"`,
		"The 15th day of the fourth month following the close of the taxable year.",
		"With respect to any part of the underpayment, the date on which that part is paid.",
		"the individual was a resident of Minnesota throughout the preceding taxable year.",
		"an equal part of those amounts is considered paid on the installment date",
		"no addition to tax is imposed under subdivision 2 with respect to any underpayment of the fourth required installment",
		"The applicable percentage of the tax is 22.5 percent in the case of the first installment, 45 percent for the second installment, 67.5 percent for the third installment, and 90 percent for the fourth installment.",
		"to the extent the commissioner determines that the provisions of section 6654(e)(3) of the Internal Revenue Code apply",
		"as defined in section 6654(i)(2) of the Internal Revenue Code",
	)
	mustContain(t, "minn-stat-270C.40.txt",
		"The amount in lieu of interest for that taxable year shall be the amount determined in subdivision 5 for January 1 on which begins the taxable year")
	mustContain(t, "minn-stat-270C.395.txt", "the date of the United States postmark stamped on the cover in which the document or payment is mailed")
	mustContain(t, "minn-stat-645.151.txt", "falls on a Saturday, Sunday, or legal holiday, it is timely delivered or filed if it is delivered or filed on the next succeeding day")
}

// The 2026 dates are the ones M15 prints, a quarter of the target each, and
// none moves. January 31, 2027 is a Sunday: the form prints it and does not
// move it, so a return filed on Monday, February 1 does not count as paid on
// January 15.
func TestEstimatedScheduleForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
		if want := money.USCents(i+1) * 2_250 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	if wd := day(2027, 1, 31).Weekday(); wd != time.Sunday {
		t.Fatalf("January 31, 2027 is a %v", wd)
	}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{{day(2027, 1, 31), true}, {day(2027, 2, 1), false}} {
		s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: tc.filed}.facts(t))).Value()
		if got := s[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
	}
}

// M15 line 3: $500 or more of tax after withholding and refundable credits.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld, refundable money.USCents
		exempt               bool
	}{
		{950_001, 0, true},  // $499.99
		{950_000, 0, false}, // $500.00
		{900_000, 50_001, true},
		{900_000, 50_000, false},
	} {
		h := household{tax: 10_000 * money.Dollar, withheld: tc.withheld, refundable: cents(tc.refundable)}
		res := must(rules(t).Requirement(h.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v, refundable %v: exempt %v, want %v (%s)", tc.withheld, tc.refundable, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding and credits under $500") {
			t.Errorf("withheld %v, refundable %v: chosen %v %q at %v", tc.withheld, tc.refundable, c.Basis(), c.Label(), c.Amount())
		}
	}
}

// The prior-year test is 100% of last year's tax, or 110% when last year's
// AGI was more than $150,000. The lesser of it and 90% is the target.
func TestEstimatedPriorYearStep(t *testing.T) {
	for _, tc := range []struct {
		agi  money.USCents
		want money.USCents
		rate money.Rate
	}{
		{15_000_000, 8_000 * money.Dollar, money.Percent(100)},
		{15_000_001, 8_800 * money.Dollar, money.Percent(110)},
	} {
		res := must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: prior(8_000*money.Dollar, tc.agi)}.facts(t)))
		r := res.Value()
		c, _ := r.Chosen()
		if c.Basis() != estimated.BasisPriorYearTax || r.Target() != tc.want || c.Percent() != tc.rate {
			t.Errorf("AGI %v: chose %v at %v for %v, want last year's tax at %v for %v", tc.agi, c.Basis(), c.Percent(), r.Target(), tc.rate, tc.want)
		}
		if r.Options()[0].Amount() != 18_000*money.Dollar {
			t.Errorf("the 90%% option is %v, want $18,000", r.Options()[0].Amount())
		}
	}
	// 90% of this year's tax is smaller: it is chosen.
	res := must(rules(t).Requirement(household{tax: 5_000 * money.Dollar, prior: prior(8_000*money.Dollar, 100_000*money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 4_500*money.Dollar {
		t.Errorf("chose %v for %v, want this year's $4,500", c.Basis(), res.Value().Target())
	}
	// Without last year's AGI, 110% is assumed and the question is asked.
	noAGI := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(8_000 * money.Dollar).Build())).FullYearResident(true).Build())
	res = must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: &noAGI}.facts(t)))
	if res.Value().Target() != 8_800*money.Dollar || !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Errorf("without AGI: target %v, AGI asked %v; want $8,800 and asked", res.Value().Target(), has(res.Required(), working.PositionAdjustedGrossIncome))
	}
}

// The prior-year test needs a filed twelve-month return that showed some
// Minnesota tax, and $1 of it for a nonresident or part-year resident.
func TestEstimatedPriorYearOptionAvailability(t *testing.T) {
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).
		Figures(must(estimated.NewFigures().Tax(5_000 * money.Dollar).FederalAGI(50_000 * money.Dollar).Build())).FullYearResident(true).Build())
	for name, tc := range map[string]struct {
		prior     *estimated.PriorReturn
		available bool
		ask       working.PositionKind
	}{
		"no return on record":           {nil, false, working.PositionPriorYearReturn},
		"not filed":                     {&notFiled, false, working.PositionUnspecified},
		"eleven months":                 {&short, false, working.PositionUnspecified},
		"no liability":                  {prior(0, 50_000*money.Dollar), false, working.PositionUnspecified},
		"nonresident with $0.99 of tax": {partYear(99, 50_000*money.Dollar, 1_000*money.Dollar), false, working.PositionUnspecified},
		"nonresident with $1.00 of tax": {partYear(100, 50_000*money.Dollar, 1_000*money.Dollar), true, working.PositionUnspecified},
		"resident with $0.99 of tax":    {prior(99, 50_000*money.Dollar), true, working.PositionUnspecified},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		o := res.Value().Options()[1]
		if o.Available() != tc.available {
			t.Errorf("%s: the prior-year test available %v, want %v (%s)", name, o.Available(), tc.available, o.WhyNot())
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
	}
}

// § 289A.25 subd. 5(2)(i) reads the Minnesota share of AGI for somebody who
// was not a resident all year, without saying which year. The default follows
// last year's residence, and the question is asked.
func TestEstimatedNonresidentStepIsAnOpenQuestion(t *testing.T) {
	last := partYear(8_000*money.Dollar, 200_000*money.Dollar, 100_000*money.Dollar)
	res := must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: last}.facts(t)))
	if res.Value().Target() != 8_000*money.Dollar || !because(res.Required(), working.PositionJurisdictionFact, "Minnesota share of AGI or your federal AGI") {
		t.Errorf("part-year last year: target %v, asked %v; want 100%% on the $100,000 Minnesota share, and asked", res.Value().Target(),
			because(res.Required(), working.PositionJurisdictionFact, "Minnesota share of AGI or your federal AGI"))
	}
	federal := must(minnesotataxcalc.NewEstimatedFacts().RefundableCredits(0).PriorYearStepOnMinnesotaShare(false).Build())
	res = must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: last, extras: &federal}.facts(t)))
	if res.Value().Target() != 8_800*money.Dollar || because(res.Required(), working.PositionJurisdictionFact, "Minnesota share of AGI or your federal AGI") {
		t.Errorf("answered federal AGI: target %v; want 110%% on $200,000 of federal AGI, not asked", res.Value().Target())
	}
	// Part-year this year, full-year last year: federal AGI by default, asked.
	thisYear := must(estimated.NewResidencePeriod("MN").From(day(2026, 1, 1)).To(day(2026, 6, 30)).Build())
	full := prior(8_000*money.Dollar, 200_000*money.Dollar)
	res = must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: full, residence: []estimated.ResidencePeriod{thisYear}}.facts(t)))
	if res.Value().Target() != 8_800*money.Dollar || !because(res.Required(), working.PositionJurisdictionFact, "Minnesota share of AGI or your federal AGI") {
		t.Errorf("part-year this year: target %v; want 110%% on federal AGI, and asked", res.Value().Target())
	}
	// A full-year resident in both years is not asked.
	res = must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: full}.facts(t)))
	if because(res.Required(), working.PositionJurisdictionFact, "Minnesota share of AGI or your federal AGI") {
		t.Error("a full-year resident is asked which AGI the step reads")
	}
}

// M15 lines 16 to 21, with nothing paid: each quarter's $2,250 for its days to
// April 15, 2027, ÷ 365 × 7%. § 270C.40 subd. 4 fixes one rate for the whole
// tax year, so the first quarter's 365 days run into 2027 as one span at the
// 2026 rate; Minnesota's rate never changes inside a tax year's penalty.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 4, 15)))
	lines := res.Value().Lines()
	wantDays := []int{365, 304, 212, 90}
	wantCharge := []money.USCents{15_750, 13_118, 9_148, 3_884}
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	for i, l := range lines {
		parts := l.Parts()
		if len(parts) != 1 || parts[0].Days() != wantDays[i] || parts[0].Charge() != wantCharge[i] || parts[0].Rate() != money.Percent(7) {
			t.Errorf("line %d: %d parts, first %d days %v at %v; want one of %d days, %v at 7%%", i+1, len(parts), parts[0].Days(), parts[0].Charge(), parts[0].Rate(), wantDays[i], wantCharge[i])
		}
	}
	if res.Value().Total() != 41_900 {
		t.Errorf("penalty %v, want $419.00", res.Value().Total())
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("an unpublished rate is reported, but the 2026 rate covers every day to April 15, 2027")
	}
	// Figured to an earlier date, only the days to it count.
	res = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2026, 12, 31)))
	if l := res.Value().Lines()[0]; l.Days() != 260 || l.Total() != 11_219 {
		t.Errorf("to December 31: %d days, %v; want 260 days, $112.19", l.Days(), l.Total())
	}
	// Never past April 15, 2027.
	res = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 6, 30)))
	if res.Value().AsOf() != day(2027, 4, 15) || res.Value().Total() != 41_900 {
		t.Errorf("figured to %v, %v; want April 15, 2027 and $419.00", res.Value().AsOf(), res.Value().Total())
	}
}

// Line 17 takes the date the underpayment was paid in full, so the whole
// $2,250 is charged to June 15: $26.32. Charging each part to the day it was
// paid, as § 289A.25 subd. 3(a)(2) reads, would give $20.38.
func TestEstimatedShortfallPaidInPartFollowsLineSeventeen(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{
		pay(day(2026, 5, 15), 1_000*money.Dollar), pay(dues[1], 3_500*money.Dollar),
		pay(dues[2], 2_250*money.Dollar), pay(dues[3], 2_250*money.Dollar)}}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	lines := res.Value().Lines()
	if len(lines) != 1 || lines[0].Installment() != 1 || lines[0].Days() != 61 {
		t.Fatalf("lines %v; want one for April, 61 days", lines)
	}
	parts := lines[0].Parts()
	if len(parts) != 1 || parts[0].Principal() != 2_250*money.Dollar || res.Value().Total() != 2_632 {
		t.Errorf("penalty %v on %d parts; want $26.32 on the whole $2,250", res.Value().Total(), len(parts))
	}
}

// Filing and paying by January 31, 2027 counts the tax as paid on January 15:
// the earlier shortfalls stop then and January's bears nothing. April's 275
// days give $118.66, June's 214 $92.34, September's 122 $52.64.
func TestEstimatedReturnFiledByJanuaryThirtyFirst(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 1, 29)}.facts(t), day(2027, 4, 15)))
	if res.Value().AsOf() != day(2027, 1, 15) || res.Value().Total() != 26_364 {
		t.Errorf("filed January 29: figured to %v, %v; want January 15 and $263.64", res.Value().AsOf(), res.Value().Total())
	}
	// Filed February 1: the penalty runs to the day it was paid.
	res = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 1)}.facts(t), day(2027, 4, 15)))
	if res.Value().AsOf() != day(2027, 2, 1) {
		t.Errorf("filed February 1: figured to %v", res.Value().AsOf())
	}
	// Filed January 10: the penalty stops on January 10.
	res = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 1, 10)}.facts(t), day(2027, 4, 15)))
	if res.Value().AsOf() != day(2027, 1, 10) {
		t.Errorf("filed January 10: figured to %v", res.Value().AsOf())
	}
}

// Withholding and refundable credits count as paid evenly unless the filer
// shows the dates.
func TestEstimatedWithholdingAndRefundableCredits(t *testing.T) {
	if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar}.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("December withholding spread evenly: penalty %v", p.Value().Total())
	}
	asWithheld := must(estimated.NewElections().WithholdingAsWithheld().Build())
	p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar, elections: &asWithheld}.facts(t), day(2027, 4, 15)))
	if p.Value().Total() == 0 || len(p.Value().Lines()) != 3 {
		t.Errorf("withholding on its December date: penalty %v over %d lines; want three short quarters", p.Value().Total(), len(p.Value().Lines()))
	}
	// Refundable credits spread evenly even then.
	p = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, refundable: cents(9_000 * money.Dollar), elections: &asWithheld}.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 0 {
		t.Errorf("refundable credits: penalty %v", p.Value().Total())
	}
	// Not given: asked for, none assumed.
	f := must(estimated.NewFacts("MN", 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Build())
	if res := must(rules(t).Requirement(f)); !because(res.Required(), working.PositionJurisdictionFact, "refundable credits") {
		t.Error("refundable credits are not asked for")
	}
}

// § 289A.25 subd. 6: no liability last year, twelve months, and a full year
// of Minnesota residence.
func TestEstimatedNoLiabilityException(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0, 20_000*money.Dollar)}.facts(t)))
	if !res.Value().Exempt() {
		t.Errorf("no liability as a full-year resident: %s", res.Value().Why())
	}
	if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Label() != "No tax due last year as a full-year resident" {
		t.Errorf("no liability as a full-year resident: chosen %v %q", c.Basis(), c.Label())
	}
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &notRequired}.facts(t))); !res.Value().Exempt() {
		t.Errorf("not required to file: %s", res.Value().Why())
	}
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: partYear(0, 20_000*money.Dollar, 5_000*money.Dollar)}.facts(t))); res.Value().Exempt() {
		t.Error("no liability as a part-year resident is exempt")
	}
	unanswered := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(0).Build())).Build())
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &unanswered}.facts(t)))
	if res.Value().Exempt() || !has(res.Required(), working.PositionResidency) {
		t.Errorf("residence not given: exempt %v, asked %v; want not exempt and asked", res.Value().Exempt(), has(res.Required(), working.PositionResidency))
	}
}

// The annualized worksheet: 22.5%, 45%, 67.5% and 90% of each period's
// annualized tax, less earlier installments, capped by the regular $2,250
// plus what earlier periods saved.
func TestEstimatedAnnualizedMethod(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, periods: []money.USCents{2_000 * money.Dollar, 4_000 * money.Dollar, 8_000 * money.Dollar, 10_000 * money.Dollar}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{450 * money.Dollar, 1_800 * money.Dollar, 5_400 * money.Dollar, 9_000 * money.Dollar} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, s[i].NeededBy(), want)
		}
	}
	f := must(estimated.NewFacts("MN", 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(100).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).
		Periods(must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 6, 30)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).Build())
	if _, err := rules(t).Schedule(f); err == nil {
		t.Error("a period ending June 30 is accepted")
	}
}

// Without this year's tax, the target is last year's figure alone and the
// penalty is the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000*money.Dollar, 100_000*money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 8_000*money.Dollar {
		t.Errorf("required %v, target %v; want $8,000", r.Required(), r.Target())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Error("this year's tax is not asked for")
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !p.Value().UpperBound() || p.Value().Total() == 0 {
		t.Errorf("penalty %v, upper bound %v", p.Value().Total(), p.Value().UpperBound())
	}
	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() {
		t.Error("a target with neither year's tax")
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// The penalty is before any waiver, and the waiver is asked about when there
// is one to waive.
func TestEstimatedWaiverIsAsked(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 4, 15)))
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked about")
	}
	k := estimated.WaiverRetiredOrDisabled
	res = must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, waiver: &k}.facts(t), day(2027, 4, 15)))
	if has(res.Required(), working.PositionPenaltyWaiver) || res.Value().Waiver() != k || res.Value().Total() != 41_900 {
		t.Errorf("waiver answered: asked %v, waiver %v, total %v", has(res.Required(), working.PositionPenaltyWaiver), res.Value().Waiver(), res.Value().Total())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"When a payment counts as made":                  {"must receive", "§ 270C.395"},
		"A shortfall paid in part":                       {"paid in full", "subd. 3(a)(2)"},
		"A return filed and paid by January 31":          {"Jan 15, 2027", "subd. 9"},
		"January 31, 2027 is a Sunday":                   {"not moved", "§ 645.151"},
		"The optional short method":                      {"2%", "subd. 2"},
		"Waivers for retirement, disability or casualty": {"IRS waived", "subd. 7"},
		"Who counts as a farmer":                         {"this year only", "§ 6654(i)(2)"},
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
		// Nothing paid on $10,000: short on every date, with no payments,
		// no early return and no waiver asked for.
		wantAffects := d.Rule() == "The optional short method"
		if d.AffectsHousehold() != wantAffects {
			t.Errorf("%q: affects %v, want %v", d.Rule(), d.AffectsHousehold(), wantAffects)
		}
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.StatuteSource(), w[1]) || d.FormSource() == "" || d.Statute() == "" {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s)", d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := minnesotataxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	farmer := must(estimated.NewFacts("MN", 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(7000).Build())
	if _, err := rules(t).Requirement(farmer); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer: %v, want ErrUnsupported", err)
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts("MN", 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts("MN", 2026).FilingStatus(filing.Single).ThisYear(this).State("not Minnesota's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}
	if _, err := minnesotataxcalc.NewEstimatedFacts().RefundableCredits(-1).Build(); err == nil {
		t.Error("negative refundable credits are accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$500", "7%", "$150,000", "refundable credits", "full year of Minnesota residence", "January 15", "paid in full"} {
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("MN"), 2026).FilingStatus(filing.Single).
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

// Minnesota adopts the federal waivers: retiring after 62 or becoming
// disabled in 2025 or 2026, or a casualty or disaster.
func TestEstimatedWaiverFollowsTheFederalOne(t *testing.T) {
	mustContain(t, "minn-stat-289A.25.txt", "to the extent the commissioner determines that the provisions of section 6654(e)(3) of the Internal Revenue Code apply")
	p := waived(t, retiredOn(day(2026, 6, 1), day(1962, 3, 1)))
	if o, ok := p.WaiverOutcome(); !ok || !o.Eligible() || p.Total() != 41_900 || p.IfWaived() != 0 {
		t.Errorf("retired in 2026 at 64: outcome %v %+v, total %v, if waived %v", ok, o, p.Total(), p.IfWaived())
	}
	p = waived(t, retiredOn(day(2020, 6, 1), day(1955, 3, 1)))
	if o, ok := p.WaiverOutcome(); !ok || !o.Refused() || !strings.Contains(o.Reason(), "2025 or 2026") || p.IfWaived() != p.Total() {
		t.Errorf("retired in 2020: outcome %v %+v, if waived %v", ok, o, p.IfWaived())
	}
	p = waived(t, retiredOn(day(2026, 6, 1), day(1970, 3, 1)))
	if o, ok := p.WaiverOutcome(); !ok || !o.Refused() || !strings.Contains(o.Reason(), "62") {
		t.Errorf("retired at 56: outcome %v %+v", ok, o)
	}
	p = waived(t, must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()))
	if o, ok := p.WaiverOutcome(); !ok || !o.Eligible() || !strings.Contains(o.Reason(), "Minnesota's Department of Revenue") {
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
