// Rhode Island's estimated tax rules for 2026, checked against the Division
// of Taxation's own documents and the General Laws.
//
// Rhode Island publishes no exhaustive oracle and no worked example with an
// answer: neither the RI-1040ES nor the RI-2210 works a case. The nearest
// thing is the RI-2210A shortcut factor, 7.9808%, which the tests derive from
// the 12% rate and the day counts these rules compute. Every threshold,
// percentage, date and rate is compared against its extract in testdata/, and
// the penalty cases are worked by hand from RI-2210 lines 13 to 22. They
// check what this package's author read the form to say, not an answer the
// Division published.
package rhodeislandtaxcalc_test

import (
	"errors"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/rhodeislandtaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

const ri = estimated.Jurisdiction("RI")

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
	return must(rhodeislandtaxcalc.EstimatedTax(2026))
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

func has(rs []working.Required, k working.PositionKind) bool { return count(rs, k) > 0 }

func requiredAbout(rs []working.Required, k working.PositionKind, phrase string) bool {
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

// quarterly is one payment of a on each due date; a zero is no payment.
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
	tax         money.USCents // after credits, RI-2210 line 1
	noTax       bool
	withheld    money.USCents
	asWithheld  bool
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *rhodeislandtaxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	annualised  []money.USCents // RI-2210 Part 5 line 26 for each period
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
	b := estimated.NewFacts(ri, 2026).FilingStatus(filing.Single).ThisYear(fig).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled).
		FarmShare(h.farm).FishShare(h.fish)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(rhodeislandtaxcalc.NewEstimatedFacts().PriorYearNoFederalLiability(false).Build()))
	}
	el := estimated.NewElections()
	if h.asWithheld {
		el.WithholdingAsWithheld()
	}
	if h.annualised != nil {
		el.Annualised()
		ends := h.periodEnds
		if ends == nil {
			ends = periodEnds
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

// prior is a filed twelve-month Rhode Island return with its tax.
func prior(tax money.USCents, resident bool) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).FullYearResident(resident).Build())
	return &p
}

func extras(b *rhodeislandtaxcalc.EstimatedFactsBuilder) *rhodeislandtaxcalc.EstimatedFacts {
	e := must(b.Build())
	return &e
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "ri-1040es-2026.txt",
		"payments if his or her estimated Rhode Island personal income tax can be reasonably expected to be $250 or more in excess of any credits allowable against his or her tax",
		"1st Estimate Due......................April 15, 2026",
		"2nd Estimate Due.....................June 15, 2026",
		"3rd Estimate Due.....................September 15, 2026",
		"4th Estimate Due.....................January 15, 2027",
		"You do not have to make your January 15, 2027 payment if you file your 2026 return by February 15, 2027, and pay the entire balance due with your return.",
		"computed on the amount by which the actual payments and credits applied to the tax are less than 80% of such installment at the time it is due",
		"6. Enter your 2025 Rhode Island income tax",
		"7. Enter the SMALLER of lines 5 or 6",
		"may be deducted from the first installment of your 2026 estimated tax",
	)
	mustContain(t, "ri-2210-2025.txt",
		"2 Enter 80% of the amount shown on line 1",
		"4 Subtract line 3 from line 1. If the result is $250.00 or less, do not complete the rest of this form",
		"5 Enter your 2024 RI tax",
		"6 Enter the SMALLER of line 2 or line 5",
		"13 Divide the Amount on line 6 by four (4)and enter the result in each column",
		"Number of days on line 19a X 12% X underpayment on line 17",
		"21 a Number of days from January 15, 2026 to the date of payment or April 15, 2026, whichever is earlier",
		"03/31/2025 05/31/2025 08/31/2025 12/31/2025",
		"Annualization amount...................... ........................................................................ 4 2.4 1.5 1",
		"Applicable percentage............................................................................................. 20% 40% 60% 80%",
		"You are considered to have paid Rhode Island withholding tax evenly during the year unless you can show otherwise. Therefore, you are considered to have paid one-fourth of your Rhode Island withholding on each installment due date.",
		"If you filed your return on or before January 31, 2026 and paid the tax balance in full, then enter that amount in column D.",
		"Any underpayment on line 17 should be used to decrease the amount on line 16 for the next installment period.",
		"If you had no federal tax liability for 2024 and you were a Rhode Island resident during all of 2024, and your 2024 federal tax return was (or would have been had you been required to file) for a full 12 months, then enter zero (0).",
		"The interest rate to be used is 12% per annum",
		"Gross income from farming or fishing is at least two thirds of your annual gross income",
		"if you complete and use line 14 for any installment you must complete it and use it for all installments.",
	)
	mustContain(t, "ri-2210a-2025.txt",
		"Multiply line 9 by 7.9808% (0.079808)",
		"The amount on line 9 (times) the number of days paid before 4/15/2026 (times) 0.000329",
		"FARMERS AND FISHERMEN DO NOT USE FORM RI-2210A.",
	)
	mustContain(t, "adv-2025-23.txt",
		"the interest rate on delinquent tax payments of other tax types shall be 12.00% per annum",
		"01/01/26 12/31/26 12.00% 01/01/25 12/31/25 12.00% 01/01/24 12/31/24 12.00% 01/01/23 12/31/23 12.00% 01/01/22 12/31/22 18.00%",
	)
	mustContain(t, "ri-1040-2025-instructions.txt",
		"including failure to pay adequate estimated tax, is subject to interest at the rate of 12% (0.1200) per annum.")
	mustContain(t, "adv-2026-15.txt", "Personal income tax – estimate")
	mustContain(t, "adv-2024-16.txt", "Personal income tax - Form RI-1040ES June 17, 2024 July 15, 2024")
	mustContain(t, "rigl-44-30-55.txt",
		"two hundred and fifty dollars ($250) or more in excess of any credits allowable",
		"If on or before the fifteenth day of the second month after the close of his or her taxable year an individual files his or her return for the year and pays with the return the full amount of the tax shown to be due on the return, the return shall be considered as his or her declaration if it was not otherwise required to be filed prior to the close of his or her taxable year.",
	)
	mustContain(t, "rigl-44-30-56.txt",
		"(1) April 15 ......................................... | April 15; June 15; September 15; | | January 15 of following year.",
		"(2) June 15 ......................................... | June 15; September 15; January 15 of |",
	)
	mustContain(t, "rigl-44-30-84.txt",
		"The interest shall not be paid if its amount is less than two dollars ($2.00).",
		"until the fifteenth day of the fourth month following the close of the taxable year",
		"less than eighty percent (80%) of the installment at the time it is due",
		"no interest shall be payable if one of the exceptions specified in 26 U.S.C. § 6654(e)(1) or (2) would apply if the exceptions referred to the corresponding Rhode Island tax amounts and returns",
	)
	mustContain(t, "rigl-44-1-7.txt",
		"Each January 1 the tax administrator shall compute the rate of interest to be in effect for that calendar year by adding two percent (2%) to the prime rate, which was in effect on October 1 of the preceding year",
		"in no event shall the rate of interest exceed twenty-one percent (21%) per annum nor be less than twelve percent (12%) per annum",
	)
	mustContain(t, "rigl-44-30-91.txt",
		"falls on Saturday, Sunday, or a Rhode Island legal holiday, the performance of the act shall be considered timely if it is performed on the next succeeding day which is not a Saturday, Sunday or Rhode Island legal holiday")
	mustContain(t, "ricr-280-20-55-1.txt",
		"In general, the Federal exceptions under which taxpayers will not be charged for underpayment of estimated tax may be followed")
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

// fixed is a fixed-date holiday, moved to the Monday after a weekend as
// § 25-1-1 says state employees celebrate it.
func fixed(year int, m time.Month, dd int) estimated.Date {
	d := day(year, m, dd)
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDays(2)
	case time.Sunday:
		return d.AddDays(1)
	}
	return d
}

// The calendar is § 25-1-1's holidays from the first due date to February
// 16, 2027, each derived here from the words of the section. No due date is
// one, and none falls on a weekend; the RI-1040ES's February 15, 2027 is
// Washington's Birthday.
func TestEstimatedHolidaysFollowTheStatute(t *testing.T) {
	mustContain(t, "rigl-25-1-1.txt",
		"the third Monday of January (as Dr. Martin Luther King, Jr.’s Birthday), the third Monday of February (as Washington’s Birthday), the fourth day of May (as Rhode Island Independence Day), the last Monday of May (as Memorial Day), the nineteenth day of June",
		"the fourth day of July or, in the event that day is a Saturday or Sunday, then state employees will celebrate the holiday on the following Monday (as Independence Day), the second Monday of August (as Victory Day), the first Monday of September (as Labor Day), the second Monday of October (as Columbus Day), the eleventh day of November",
		"the twenty-fifth day of December",
		"The first day of January or, in the event that day is a Saturday or Sunday, then state employees will celebrate the holiday on the following Monday (as New Year’s Day)",
	)
	want := []estimated.Date{
		fixed(2026, time.May, 4),
		nthMonday(2026, time.May, -1),
		fixed(2026, time.June, 19),
		fixed(2026, time.July, 4),
		nthMonday(2026, time.August, 2),
		nthMonday(2026, time.September, 1),
		nthMonday(2026, time.October, 2),
		fixed(2026, time.November, 11),
		fixed(2026, time.December, 25),
		fixed(2027, time.January, 1),
		nthMonday(2027, time.January, 3),
		nthMonday(2027, time.February, 3),
	}
	got := rhodeislandtaxcalc.Holidays(2026)
	if len(got) != len(want) {
		t.Fatalf("%d holidays, want %d", len(got), len(want))
	}
	for i, h := range got {
		if h.On() != want[i] {
			t.Errorf("holiday %d (%s) is %v, want %v", i+1, h.Name(), h.On(), want[i])
		}
	}
	if want[11] != day(2027, 2, 15) {
		t.Errorf("Washington's Birthday 2027 is %v, want February 15", want[11])
	}
	for _, d := range dues {
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("%v falls on a %v", d, wd)
		}
		for _, h := range want {
			if h == d {
				t.Errorf("%v is a holiday", d)
			}
		}
	}
}

// The 2026 dates are the ones the RI-1040ES prints, and none moves.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if want := money.USCents(i+1) * 2_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	for _, r := range res.Applied() {
		if strings.HasPrefix(r.Cite(), "R.I. Gen. Laws § 44-30-91(b)") {
			t.Errorf("a date moved: %s", r.Applied())
		}
	}
}

// RI-2210 line 4: no charge at $250.00 or less of tax after withholding. The
// law asks for payments at $250 or more, so $250.00 exactly owes estimates
// and no charge.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{75_001, true},  // $249.99
		{75_000, true},  // $250.00
		{74_999, false}, // $250.01
	} {
		res := must(rules(t).Requirement(household{tax: 1_000 * money.Dollar, withheld: tc.withheld}.facts(t)))
		r := res.Value()
		if r.Exempt() != tc.exempt || r.Required() == tc.exempt {
			t.Errorf("withheld %v: exempt %v, required %v; want exempt %v (%s)", tc.withheld, r.Exempt(), r.Required(), tc.exempt, r.Why())
		}
		if c, _ := r.Chosen(); tc.exempt && (c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding of $250 or less") {
			t.Errorf("withheld %v: chosen %v %q at %v, want the $250 exception", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
		if tc.withheld == 75_000 && !strings.Contains(r.Why(), "asks for estimated payments at $250 or more") {
			t.Errorf("at exactly $250 the reason does not say the law asks for payments: %s", r.Why())
		}
	}
}

// The target is the smaller of 80% of this year's tax and 100% of last
// year's. RI-2210 line 5 sets no condition on last year's return beyond its
// tax, so a short year counts; a return not filed does not.
func TestEstimatedTargetOptions(t *testing.T) {
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(must(estimated.NewFigures().Tax(5_000 * money.Dollar).Build())).Build())
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	for name, tc := range map[string]struct {
		prior     *estimated.PriorReturn
		target    money.USCents
		basis     estimated.Basis
		priorOpen bool
	}{
		"last year's is smaller": {prior(7_000*money.Dollar, true), 7_000 * money.Dollar, estimated.BasisPriorYearTax, true},
		"this year's is smaller": {prior(9_000*money.Dollar, false), 8_000 * money.Dollar, estimated.BasisThisYearTax, true},
		"a six-month year":       {&short, 5_000 * money.Dollar, estimated.BasisPriorYearTax, true},
		"no return filed":        {&notFiled, 8_000 * money.Dollar, estimated.BasisThisYearTax, false},
		"no return on record":    {nil, 8_000 * money.Dollar, estimated.BasisThisYearTax, false},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		r := res.Value()
		c, _ := r.Chosen()
		if r.Target() != tc.target || c.Basis() != tc.basis {
			t.Errorf("%s: target %v on %v, want %v on %v", name, r.Target(), c.Basis(), tc.target, tc.basis)
		}
		opts := r.Options()
		if opts[0].Percent() != money.Percent(80) || opts[1].Percent() != money.Percent(100) {
			t.Errorf("%s: options at %v and %v, want 80%% and 100%%", name, opts[0].Percent(), opts[1].Percent())
		}
		if opts[1].Available() != tc.priorOpen {
			t.Errorf("%s: last year's option available %v, want %v (%s)", name, opts[1].Available(), tc.priorOpen, opts[1].WhyNot())
		}
		if got := has(res.Required(), working.PositionPriorYearReturn); got != (tc.prior == nil) {
			t.Errorf("%s: last year's return asked for %v", name, got)
		}
	}
}

// Without this year's tax the target is 100% of last year's, and the charge
// is the most it can be. With no prior year either there is no target. The
// no-liability exception needs nothing about this year, and its zero is
// exact.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(6_000*money.Dollar, true)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 6_000*money.Dollar {
		t.Errorf("required %v, target %v; want $6,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Error("this year's tax is not asked for")
	}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{150_000, 300_000, 450_000, 600_000} {
		if sched[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, sched[i].NeededBy(), want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if !pen.Value().UpperBound() || pen.Value().Total() == 0 {
		t.Errorf("unpaid without this year's tax: %v, upper bound %v", pen.Value().Total(), pen.Value().UpperBound())
	}

	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() || r.Required() {
		t.Errorf("no tax and no prior year: target known %v, required %v", r.TargetKnown(), r.Required())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}

	exempt := household{noTax: true, prior: prior(900*money.Dollar, true), extras: extras(rhodeislandtaxcalc.NewEstimatedFacts().PriorYearNoFederalLiability(true))}
	p := must(rules(t).Penalty(exempt.facts(t), day(2027, 4, 15))).Value()
	if p.Total() != 0 || p.UpperBound() || p.Reason() != "No federal tax liability last year." {
		t.Errorf("no federal liability last year: %v, upper bound %v, %q; want an exact zero", p.Total(), p.UpperBound(), p.Reason())
	}
}

// RI-2210 line 5 enters zero for last year's tax after a year with no
// federal liability, for a full-year Rhode Island resident.
func TestEstimatedNoFederalLiabilityLastYear(t *testing.T) {
	none := extras(rhodeislandtaxcalc.NewEstimatedFacts().PriorYearNoFederalLiability(true))
	unanswered := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(900 * money.Dollar).Build())).Build())
	for name, tc := range map[string]struct {
		h      household
		exempt bool
		ask    working.PositionKind
	}{
		"resident":            {household{tax: 10_000 * money.Dollar, prior: prior(900*money.Dollar, true), extras: none}, true, working.PositionUnspecified},
		"part-year":           {household{tax: 10_000 * money.Dollar, prior: prior(900*money.Dollar, false), extras: none}, false, working.PositionUnspecified},
		"residence not given": {household{tax: 10_000 * money.Dollar, prior: &unanswered, extras: none}, false, working.PositionResidency},
		"no prior return":     {household{tax: 10_000 * money.Dollar, extras: none}, false, working.PositionResidency},
		"liability not said":  {household{tax: 10_000 * money.Dollar, prior: prior(900*money.Dollar, true), extras: extras(rhodeislandtaxcalc.NewEstimatedFacts())}, false, working.PositionJurisdictionFact},
	} {
		res := must(rules(t).Requirement(tc.h.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", name, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
	}
	// Answered, nothing about federal liability is asked.
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(900*money.Dollar, true)}.facts(t)))
	if has(res.Required(), working.PositionJurisdictionFact) {
		t.Errorf("asked again: %v", res.Required())
	}
}

// RI-2210 line 19b: days × 12% × underpayment ÷ 365, on each piece of a
// shortfall until it is paid. A payment goes to the oldest shortfall first.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	// 80% of $10,000 is $8,000, a quarter $2,000. April is $1,000 short and
	// paid with June's: 61 days × 12% × $1,000 ÷ 365 = $20.05.
	h := household{tax: 10_000 * money.Dollar, payments: quarterly(1_000*money.Dollar, 3_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar)}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	lines := p.Lines()
	if len(lines) != 1 || lines[0].Installment() != 1 || lines[0].Shortfall() != 1_000*money.Dollar || lines[0].Days() != 61 || p.Total() != 2_005 {
		t.Errorf("penalty %v over %d lines; want $20.05 on April's $1,000 for 61 days", p.Total(), len(lines))
	}
	if parts := lines[0].Parts(); len(parts) != 1 || parts[0].Rate() != money.Percent(12) {
		t.Errorf("parts %+v, want one at 12%%", parts)
	}

	// Half paid May 15, 30 days: $4.93; half with June's, 61 days: $10.03.
	h.payments = []estimated.Payment{pay(dues[0], 1_000*money.Dollar), pay(day(2026, 5, 15), 500*money.Dollar), pay(dues[1], 2_500*money.Dollar),
		pay(dues[2], 2_000*money.Dollar), pay(dues[3], 2_000*money.Dollar)}
	p = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	parts := p.Lines()[0].Parts()
	if len(parts) != 2 || parts[0].Days() != 30 || parts[0].Charge() != 493 || parts[1].Days() != 61 || parts[1].Charge() != 1_003 || p.Total() != 1_496 {
		t.Errorf("penalty %v with parts %+v; want $4.93 for 30 days and $10.03 for 61", p.Total(), parts)
	}

	// Figured to June 1, the shortfall has run 47 days: $15.45.
	h.payments = quarterly(1_000*money.Dollar, 3_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar)
	if p := must(rules(t).Penalty(h.facts(t), day(2026, 6, 1))).Value(); p.Total() != 1_545 || p.AsOf() != day(2026, 6, 1) {
		t.Errorf("to June 1: %v as of %v, want $15.45", p.Total(), p.AsOf())
	}
}

// The rate is set for each calendar year, and the RI-2210 splits the days at
// January 1 (lines 19 and 20). Rhode Island's rate was 12% for 2025 and is
// 12% for 2026, and the 2027 rate is not published, so no published change
// falls inside the 2026 window: this checks the split at the year end and the
// unpublished-rate gap instead.
func TestEstimatedShortfallSpanningTheYearEnd(t *testing.T) {
	// September is $2,000 short. January's $2,000 pays it on January 15, and
	// January's own installment is paid February 1.
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{
		pay(dues[0], 2_000*money.Dollar), pay(dues[1], 2_000*money.Dollar), pay(dues[3], 2_000*money.Dollar), pay(day(2027, 2, 1), 2_000*money.Dollar)}}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	lines := res.Value().Lines()
	if len(lines) != 2 || lines[0].Installment() != 3 || lines[0].Days() != 122 {
		t.Fatalf("lines %+v; want September's for 122 days and January's", lines)
	}
	parts := lines[0].Parts()
	// September 15 to December 31: 107 days × 12% × $2,000 ÷ 365 = $70.36.
	if len(parts) != 1 || parts[0].To() != day(2026, 12, 31) || parts[0].Days() != 107 || parts[0].Charge() != 7_036 {
		t.Errorf("September's parts %+v; want 107 days to December 31, $70.36", parts)
	}
	if len(lines[1].Parts()) != 0 || res.Value().Total() != 7_036 {
		t.Errorf("January's line has %d parts and the total is %v; want no 2027 charge and $70.36", len(lines[1].Parts()), res.Value().Total())
	}
	if !requiredAbout(res.Required(), working.PositionUnpublishedRate, "rate for the days after Dec 31, 2026 isn't published yet") {
		t.Errorf("the 2027 gap is not reported: %v", res.Required())
	}

	// Paid on December 31, nothing runs into 2027.
	h.payments = []estimated.Payment{pay(dues[0], 2_000*money.Dollar), pay(dues[1], 2_000*money.Dollar), pay(day(2026, 12, 31), 2_000*money.Dollar), pay(dues[3], 2_000*money.Dollar)}
	res = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 7_036 || has(res.Required(), working.PositionUnpublishedRate) {
		t.Errorf("paid December 31: %v, gap reported %v; want $70.36 and no gap", res.Value().Total(), has(res.Required(), working.PositionUnpublishedRate))
	}
}

// The RI-2210A shortcut multiplies the underpayment by 7.9808% and charges
// 0.000329 a day. Both come from the 12% rate: 12% × (365 + 304 + 212 + 90) ÷
// 365 ÷ 4, the days from each due date to April 15, and 12% ÷ 365. The rules
// count the same days for a filer who paid nothing, and charge the 2026 days
// at 12% ÷ 365.
func TestEstimatedShortcutFactorFollowsFromTheRate(t *testing.T) {
	mustContain(t, "ri-2210a-2025.txt", "Multiply line 9 by 7.9808% (0.079808)", "0.000329")
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 4, 15)))
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	wantDays := []int{365, 304, 212, 90}
	want2026 := []money.USCents{17_096, 13_085, 7_036, 0} // 260, 199 and 107 days of 2026
	total := 0
	for i, l := range lines {
		if l.Shortfall() != 2_000*money.Dollar || l.Days() != wantDays[i] {
			t.Errorf("line %d: %v for %d days, want $2,000 for %d", i+1, l.Shortfall(), l.Days(), wantDays[i])
		}
		if l.Total() != want2026[i] {
			t.Errorf("line %d charges %v for 2026, want %v", i+1, l.Total(), want2026[i])
		}
		total += l.Days()
	}
	rate := money.Percent(12)
	factor := new(big.Rat).SetFrac64(int64(rate)*int64(total), 10_000*365*4)
	if got := factor.FloatString(6); got != "0.079808" {
		t.Errorf("12%% over %d days ÷ 365 ÷ 4 is %s, not the printed 0.079808", total, got)
	}
	if got := new(big.Rat).SetFrac64(int64(rate), 10_000*365).FloatString(6); got != "0.000329" {
		t.Errorf("12%% ÷ 365 is %s, not the printed 0.000329", got)
	}
}

// A return filed with the balance paid by January 31 counts in column D, as
// paid by January 15. Between January 31 and the RI-1040ES's February 15,
// moved to February 16 for Washington's Birthday, the reading is asked for.
func TestEstimatedJanuaryReturn(t *testing.T) {
	// September is $2,000 short, January unpaid.
	base := household{tax: 10_000 * money.Dollar, payments: quarterly(2_000*money.Dollar, 2_000*money.Dollar)}
	with := func(filed estimated.Date, reading rhodeislandtaxcalc.JanuaryReturn) household {
		h := base
		h.returnFiled = filed
		h.extras = extras(rhodeislandtaxcalc.NewEstimatedFacts().PriorYearNoFederalLiability(false).JanuaryReturn(reading))
		return h
	}
	for _, tc := range []struct {
		filed   estimated.Date
		reading rhodeislandtaxcalc.JanuaryReturn
		met     bool
		asked   bool
	}{
		{day(2027, 1, 29), rhodeislandtaxcalc.JanuaryReturnUnanswered, true, false},
		{day(2027, 1, 31), rhodeislandtaxcalc.JanuaryReturnUnanswered, true, false},
		{day(2027, 2, 1), rhodeislandtaxcalc.JanuaryReturnUnanswered, false, true},
		{day(2027, 2, 16), rhodeislandtaxcalc.JanuaryReturnUnanswered, false, true},
		{day(2027, 2, 17), rhodeislandtaxcalc.JanuaryReturnUnanswered, false, false},
		{day(2027, 2, 1), rhodeislandtaxcalc.JanuaryReturnPenaltyForm, false, false},
		{day(2027, 2, 16), rhodeislandtaxcalc.JanuaryReturnCoupon, true, false},
		{day(2027, 2, 17), rhodeislandtaxcalc.JanuaryReturnCoupon, false, false},
	} {
		h := with(tc.filed, tc.reading)
		sched := must(rules(t).Schedule(h.facts(t)))
		if got := sched.Value()[3].Status() == estimated.StatusExcused; got != tc.met {
			t.Errorf("filed %v, %v: January met %v, want %v", tc.filed, tc.reading, got, tc.met)
		}
		if got := has(sched.Required(), working.PositionJurisdictionFact); got != tc.asked {
			t.Errorf("filed %v, %v: reading asked %v, want %v", tc.filed, tc.reading, got, tc.asked)
		}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
		wantAsOf := tc.filed
		if tc.met {
			wantAsOf = dues[3]
		}
		if p.AsOf() != wantAsOf {
			t.Errorf("filed %v, %v: figured to %v, want %v", tc.filed, tc.reading, p.AsOf(), wantAsOf)
		}
		if tc.met && (len(p.Lines()) != 1 || p.Lines()[0].Installment() != 3 || p.Lines()[0].Days() != 122) {
			t.Errorf("filed %v: lines %+v; want September's alone, to January 15", tc.filed, p.Lines())
		}
	}
	// The question names both dates and the holiday that moves the later one.
	sched := must(rules(t).Schedule(with(day(2027, 2, 1), rhodeislandtaxcalc.JanuaryReturnUnanswered).facts(t)))
	if !requiredAbout(sched.Required(), working.PositionJurisdictionFact, "moves to Feb 16, 2027 for Washington's Birthday") {
		t.Errorf("the question does not name the move: %v", sched.Required())
	}
}

// § 44-30-84(a)(1) makes interest under $2.00 not payable. The RI-2210 has
// no minimum, so a charge under $2 is shown and the reading asked for.
func TestEstimatedTwoDollarMinimum(t *testing.T) {
	// A quarter of 80% of the tax, paid with June's: 61 days at 12%.
	late := func(tax money.USCents, e *rhodeislandtaxcalc.EstimatedFacts) household {
		q := money.Percent(20).Of(tax)
		return household{tax: tax, extras: e, payments: []estimated.Payment{pay(dues[1], 2*q), pay(dues[2], q), pay(dues[3], q)}}
	}
	// $497.40: a quarter of $99.48, which costs $2.00.
	res := must(rules(t).Penalty(late(49_740, nil).facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 200 || has(res.Required(), working.PositionJurisdictionFact) {
		t.Errorf("$99.48 late: %v, asked %v; want $2.00 and no question", res.Value().Total(), has(res.Required(), working.PositionJurisdictionFact))
	}
	// $497.35: a quarter of $99.47, which costs $1.99.
	res = must(rules(t).Penalty(late(49_735, nil).facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 199 || !requiredAbout(res.Required(), working.PositionJurisdictionFact, "under $2 isn't payable") {
		t.Errorf("$99.47 late: %v, asked %v; want $1.99 and the question", res.Value().Total(), res.Required())
	}
	applied := extras(rhodeislandtaxcalc.NewEstimatedFacts().PriorYearNoFederalLiability(false).TwoDollarMinimum(true))
	if p := must(rules(t).Penalty(late(49_735, applied).facts(t), day(2027, 4, 15))).Value(); p.Total() != 0 || len(p.Lines()) != 0 {
		t.Errorf("with the minimum applied: %v over %d lines, want nothing", p.Total(), len(p.Lines()))
	}
	notApplied := extras(rhodeislandtaxcalc.NewEstimatedFacts().PriorYearNoFederalLiability(false).TwoDollarMinimum(false))
	if p := must(rules(t).Penalty(late(49_735, notApplied).facts(t), day(2027, 4, 15))); p.Value().Total() != 199 || has(p.Required(), working.PositionJurisdictionFact) {
		t.Errorf("with the minimum not applied: %v, asked %v", p.Value().Total(), p.Required())
	}
}

// Withholding counts one-fourth on each date unless the filer shows when it
// was withheld. Last year's overpayment counts on the first date.
func TestEstimatedWithholding(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, withheld: 4_000 * money.Dollar,
		payments: []estimated.Payment{must(estimated.NewPayment().Amount(500 * money.Dollar).FromPriorYear().Build())}}
	even := must(rules(t).Schedule(h.facts(t))).Value()
	if even[0].CreditedBy() != 1_500*money.Dollar || even[3].CreditedBy() != 4_500*money.Dollar {
		t.Errorf("spread evenly: %v by April and %v by January, want $1,500 and $4,500", even[0].CreditedBy(), even[3].CreditedBy())
	}
	h.asWithheld = true
	dated := must(rules(t).Schedule(h.facts(t))).Value()
	if dated[0].CreditedBy() != 500*money.Dollar || dated[2].CreditedBy() != 500*money.Dollar || dated[3].CreditedBy() != 4_500*money.Dollar {
		t.Errorf("as withheld on December 15: %v, %v, %v", dated[0].CreditedBy(), dated[2].CreditedBy(), dated[3].CreditedBy())
	}
}

// RI-2210 Part 5: 20%, 40%, 60% and 80% of the tax on annualised income, less
// earlier installments, never more than the regular installment plus what
// the method carried forward.
func TestEstimatedAnnualised(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, annualised: []money.USCents{2_000 * money.Dollar, 5_000 * money.Dollar, 8_000 * money.Dollar, 10_000 * money.Dollar}}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	// 20% of $2,000 is $400; 40% of $5,000 less $400 is $1,600; 60% of
	// $8,000 less $2,000 is $2,800; 80% of $10,000 less $4,800 is $3,200.
	for i, want := range []money.USCents{400 * money.Dollar, 2_000 * money.Dollar, 4_800 * money.Dollar, 8_000 * money.Dollar} {
		if sched[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, sched[i].NeededBy(), want)
		}
	}
	// With $9,000 for the year, the last is 80% of $9,000 less $4,800,
	// $2,400, under the $3,200 the regular method would carry.
	h.annualised[3] = 9_000 * money.Dollar
	if got := must(rules(t).Schedule(h.facts(t))).Value()[3].NeededBy(); got != 7_200*money.Dollar {
		t.Errorf("the last installment brings the total to %v, want $7,200", got)
	}
	h.periodEnds = []estimated.Date{day(2026, 3, 31), day(2026, 6, 30), day(2026, 8, 31), day(2026, 12, 31)}
	if _, err := rules(t).Schedule(h.facts(t)); err == nil {
		t.Error("a period ending June 30 is accepted")
	}
}

// RI-2210 line 13 divides by four whenever the requirement arose; the
// statute's late-start table is reported as a divergence.
func TestEstimatedLateStartIsFourQuarters(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, firstMet: day(2026, 7, 1), payments: quarterly(0, 0, 6_000*money.Dollar, 2_000*money.Dollar)}
	sched := must(rules(t).Schedule(h.facts(t)))
	if s := sched.Value(); s[0].Status() != estimated.StatusShort || s[1].Status() != estimated.StatusShort || s[0].NeededBy() != 2_000*money.Dollar {
		t.Errorf("April and June are %v and %v, needing %v; want both short of a quarter", s[0].Status(), s[1].Status(), s[0].NeededBy())
	}
	noted := false
	for _, r := range sched.Applied() {
		noted = noted || strings.Contains(r.Applied(), "the requirement first arose on July 1, 2026")
	}
	if !noted {
		t.Error("the late start is not noted")
	}
}

// The penalty stops on April 15, 2027, or on the day the return is filed and
// paid.
func TestEstimatedPenaltyStops(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 6, 30))).Value(); p.AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.AsOf())
	}
	h.returnFiled = day(2027, 3, 1)
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value(); p.AsOf() != day(2027, 3, 1) {
		t.Errorf("figured to %v, want the day the return was paid", p.AsOf())
	}
}

// Rhode Island's law adopts no casualty, retirement or disability waiver,
// and its regulation lets the Division follow the federal ones. The charge is
// shown before any.
func TestEstimatedWaiver(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar}
	res := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked about")
	}
	k := estimated.WaiverCasualty
	h.waiver = &k
	asked := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if has(asked.Required(), working.PositionPenaltyWaiver) || asked.Value().Waiver() != estimated.WaiverCasualty || asked.Value().Total() != res.Value().Total() {
		t.Errorf("asked for a casualty waiver: %v, total %v", asked.Value().Waiver(), asked.Value().Total())
	}
}

// The statute adopts only § 6654(e)(1) and (2), and the regulation says the
// federal exceptions may be followed, so a waiver the federal rules would
// allow is left to the Division, and the open point is reported.
func TestEstimatedWaiverIsUndecided(t *testing.T) {
	mustContain(t, "rigl-44-30-84.txt", "exceptions specified in 26 U.S.C. § 6654(e)(1) or (2)")
	mustContain(t, "ricr-280-20-55-1.txt", "the Federal exceptions under which taxpayers will not be charged for underpayment of estimated tax may be followed")
	for name, pos := range map[string]estimated.Positions{
		"casualty":      must(estimated.NewPositions().Waiver(estimated.WaiverCasualty).Build()),
		"retired at 64": must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, time.March, 1)).Build()),
	} {
		h := household{tax: 10_000 * money.Dollar, positions: &pos}
		res := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
		p := res.Value()
		o, ok := p.WaiverOutcome()
		if p.Total() == 0 || !ok || !o.Undecided() || !strings.Contains(o.Reason(), "Division decides") {
			t.Errorf("%s: penalty %v, outcome %v %+v", name, p.Total(), ok, o)
		}
		if p.IfWaived() != p.Total() {
			t.Errorf("%s: %v if waived, want the whole %v", name, p.IfWaived(), p.Total())
		}
		if !requiredAbout(res.Required(), working.PositionUnsettledRule, "federal waiver") {
			t.Errorf("%s: the open point is not reported: %v", name, res.Required())
		}
	}
}

// A waiver the federal rules refuse stays refused, whichever reading the
// Division takes.
func TestEstimatedWaiverOutsideTheFederalWindowIsRefused(t *testing.T) {
	pos := must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, time.March, 1)).Build())
	res := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, positions: &pos}.facts(t), day(2026, 12, 31)))
	o, ok := res.Value().WaiverOutcome()
	if !ok || !o.Refused() || !strings.Contains(o.Reason(), "2020") || has(res.Required(), working.PositionUnsettledRule) {
		t.Errorf("outcome %v %+v, required %v", ok, o, res.Required())
	}
}

// An exception sets the target, so last year's return is not asked for.
func TestEstimatedExemptDoesNotAskForLastYear(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 200 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() || has(res.Required(), working.PositionPriorYearReturn) {
		t.Errorf("exempt %v, required %v", res.Value().Exempt(), res.Required())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"The prior-year option":  {"100% of last year's Rhode Island tax", "§ 44-30-84(b)"},
		"The $250 floor":         {"$250 or less", "§§ 44-30-55(a), 44-30-84(b)"},
		"No liability last year": {"no federal tax liability", "§ 44-30-84(b)"},
		"A return filed in place of the January installment": {"on or before January 31", "§§ 44-30-55(e), 44-30-91(b), 25-1-1"},
		"A requirement first met after April 15":             {"divided by four", "§§ 44-30-55(c), 44-30-56(a)"},
		"Interest under $2":                                  {"no minimum", "§ 44-30-84(a)(1)"},
	}
	statute := map[string]string{
		"The prior-year option":  "creates no prior-year option",
		"The $250 floor":         "$250 or the federal $1,000",
		"No liability last year": "no Rhode Island liability",
		"A return filed in place of the January installment": "fifteenth day of the second month",
		"A requirement first met after April 15":             "pay equal installments on June 15, September 15 and January 15",
		"Interest under $2":                                  "under $2 is not payable",
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
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.StatuteSource(), w[1]) || !strings.Contains(d.Statute(), statute[d.Rule()]) || d.FormSource() == "" {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s)", d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
		}
	}
	// Every reading followed is the forms'. Nothing in these facts turns on
	// a divergence: no prior return, a balance over $1,000, no early return,
	// no late start.
	for _, d := range res.Divergences() {
		if d.Uses() != working.ReadingForm || d.AffectsHousehold() {
			t.Errorf("%q uses %v, affects these facts %v; want the form, and no", d.Rule(), d.Uses(), d.AffectsHousehold())
		}
	}
	// A late start is touched by the late-start reading.
	late := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, firstMet: day(2026, 7, 1)}.facts(t)))
	for _, d := range late.Divergences() {
		if want := d.Rule() == "A requirement first met after April 15"; d.AffectsHousehold() != want {
			t.Errorf("late start: %q affects these facts %v, want %v", d.Rule(), d.AffectsHousehold(), want)
		}
	}
	// The $2 minimum applied follows the statute.
	applied := extras(rhodeislandtaxcalc.NewEstimatedFacts().PriorYearNoFederalLiability(false).TwoDollarMinimum(true))
	q := money.Percent(20).Of(49_735)
	small := household{tax: 49_735, extras: applied, payments: []estimated.Payment{pay(dues[1], 2*q), pay(dues[2], q), pay(dues[3], q)}}
	for _, d := range must(rules(t).Penalty(small.facts(t), day(2027, 4, 15))).Divergences() {
		if d.Rule() == "Interest under $2" && (d.Uses() != working.ReadingStatute || !d.AffectsHousehold()) {
			t.Errorf("the $2 minimum applied: uses %v, affects %v", d.Uses(), d.AffectsHousehold())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := rhodeislandtaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	for name, h := range map[string]household{
		"a farmer":    {tax: 100, farm: 7_000},
		"a fisherman": {tax: 100, fish: 7_000},
	} {
		if _, err := rules(t).Requirement(h.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"Massachusetts's facts": must(estimated.NewFacts("MA", 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":                  must(estimated.NewFacts(ri, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":          must(estimated.NewFacts(ri, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Rhode Island's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rhodeislandtaxcalc.NewEstimatedFacts().JanuaryReturn(rhodeislandtaxcalc.JanuaryReturn(7)).Build(); err == nil {
		t.Error("an unknown January reading is accepted")
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"$250", "80%", "100%", "12%", "April 15", "January 31", "February 16, 2027", "Rhode Island legal holiday", "no 110% tier"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if len(e.Sources()) == 0 || e.VersusFederal() == "" {
		t.Error("the explanation has no sources or no comparison with federal")
	}
}

// The worked example in README.md.
func TestEstimatedReadmeExample(t *testing.T) {
	f := household{tax: 6_000 * money.Dollar, withheld: 2_000 * money.Dollar, prior: prior(4_400*money.Dollar, true),
		payments: quarterly(500*money.Dollar, 500*money.Dollar, 500*money.Dollar, 500*money.Dollar)}.facts(t)
	if r := must(rules(t).Requirement(f)); r.Value().Target() != 4_400*money.Dollar {
		t.Errorf("target %v, want $4,400", r.Value().Target())
	}
	sched := must(rules(t).Schedule(f)).Value()
	for i, want := range []money.USCents{100, 200, 300, 400} {
		if sched[i].Shortfall() != want*money.Dollar {
			t.Errorf("installment %d is %v short, want $%d", i+1, sched[i].Shortfall(), want)
		}
	}
	// $100 for 61 days, $2.01; $200 for 92, $6.05; $300 for the 107 days of
	// 2026, $10.55. The 2027 days have no published rate.
	res := must(rules(t).Penalty(f, day(2027, 4, 15)))
	if res.Value().Total() != 1_861 || !has(res.Required(), working.PositionUnpublishedRate) {
		t.Errorf("penalty %v, gap reported %v; want $18.61 and the gap", res.Value().Total(), has(res.Required(), working.PositionUnpublishedRate))
	}
}

// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
// never as a negative amount.
func TestEstimatedWithholdingOverTheTaxReadsAsARefund(t *testing.T) {
	figures := func(tax money.USCents) estimated.Figures {
		return must(estimated.NewFigures().Tax(tax).FederalAGI(60_000 * money.Dollar).StateAGI(60_000 * money.Dollar).Build())
	}
	last := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800 * money.Dollar)).Build())
	b := estimated.NewFacts("RI", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
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
