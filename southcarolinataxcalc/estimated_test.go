// South Carolina's estimated tax rules for 2026, checked against the
// Department of Revenue's documents and the statute text.
//
// South Carolina publishes no exhaustive oracle and no worked penalty
// example: the SC2210 gives the line arithmetic but no example with an
// answer. The only worked example in the documents is the SC1040ES's tax
// computation, on the schedule Act 110 of 2026 replaced; this package computes
// no tax, so that example is checked only as evidence the worksheet is out of
// date. Every threshold, percentage, date, rate and holiday is compared against
// its extract in testdata/, and the penalty arithmetic is worked by hand in
// each test's comment. Those cases are hand-written, so the green tick shows
// the rules match this author's reading of the documents, not an answer the
// department computed.
package southcarolinataxcalc_test

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/southcarolinataxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

var day = estimated.MustDate

const southCarolina = estimated.Jurisdiction("SC")

// extract reads a testdata file with its whitespace collapsed, so a phrase
// the PDF broke across lines can still be found.
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
	return must(southcarolinataxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

// asks reports whether a required position of kind k mentions s.
func asks(rs []working.Required, k working.PositionKind, s string) bool {
	for _, r := range rs {
		if r.Position() == k && (strings.Contains(r.Question(), s) || strings.Contains(r.Effect(), s)) {
			return true
		}
	}
	return false
}

// applied reports whether a rule's working contains s.
func applied(rs []working.Rule, s string) bool {
	for _, r := range rs {
		if strings.Contains(r.Applied(), s) {
			return true
		}
	}
	return false
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

type household struct {
	status      filing.Status
	tax         money.USCents // SC2210 Part I line 3
	noTax       bool
	withheld    money.USCents
	withheldOn  estimated.Date
	asWithheld  bool
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *southcarolinataxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions
	annualised  []money.USCents // Schedule AI tax for each period
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	status := h.status
	if status == filing.Unspecified {
		status = filing.Single
	}
	fb := estimated.NewFigures()
	if !h.noTax {
		fb.Tax(h.tax)
	}
	b := estimated.NewFacts(southCarolina, 2026).FilingStatus(status).ThisYear(must(fb.Build())).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		on := h.withheldOn
		if on.IsZero() {
			on = day(2026, 12, 15)
		}
		b.Withheld(must(estimated.NewWithheld().On(on).Amount(h.withheld).Build()))
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
		b.State(answered())
	}
	if h.waiver != nil {
		b.Positions(must(estimated.NewPositions().Waiver(*h.waiver).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		b.Elections(must(estimated.NewElections().Annualised().Build()))
	}
	return must(b.Build())
}

// answered is every South Carolina fact answered no.
func answered() southcarolinataxcalc.EstimatedFacts {
	return must(southcarolinataxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).
		UnderpaymentFromOtherStateIncome(false).NonresidentContractor(false).Build())
}

// last is a filed twelve-month 2025 SC1040 with its tax and AGI, federal and
// South Carolina the same.
func last(tax, agi money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).StateAGI(agi).FederalAGI(agi).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).Build())
	return &p
}

// base is $10,000 of tax and nothing paid: $9,000 required, $2,250 a
// quarter.
func base() household { return household{tax: 10_000 * money.Dollar} }

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "sc1040es-2026.txt",
		"(Rev. 10/13/25)",
		"You must file a Declaration of Estimated Tax if you expect to owe an Income Tax liability of $100 or more with the filing of your SC1040, Individual Income Tax Return.",
		"If $100 or more, you must make Estimated Tax payments. If less than $100, no payment is required.",
		"Farmers and commercial fishermen whose gross income from farming or fishing for 2025 or 2026 is at least two-thirds (66.67%) of their total gross income from all sources.",
		"Individuals whose prior year tax liability was $0 for a full 12 months",
		"Nonresidents of South Carolina doing business in this state on a contract basis where the contract is more than $10,000 and tax is withheld at 2% from each contract payment",
		"your Estimated Tax payments are due in four equal installments: • 1st quarter: due April 15, 2026 • 2nd quarter: due June 15, 2026 • 3rd quarter: due September 15, 2026 • 4th quarter: due January 15, 2027",
		"Instead of making your last payment on January 15, you can choose to file your SC1040 by February 1 and pay the full balance of Income Tax due.",
		"Your first Estimated Tax payment is due on June 15, 2026 if you meet your Estimated Tax filing requirement after April 1 and before June 1.",
		"Your first Estimated Tax payment is due on September 15, 2026 if you meet your Estimated Tax filing requirement after June 1 and before September 1.",
		"Your first Estimated Tax payment is due on January 15, 2027 if you meet your Estimated Tax filing requirement after September 1.",
		"due April 15, 2026, enter 1/4 due June 15, 2026, enter 1/2 due September 15, 2026, enter 3/4 due January 15, 2027, enter amount",
		"you do not pay at least 90% of the total tax due.",
		"A penalty will not be charged if your underpayment results from personal service income earned in another state and tax was withheld to the other state.",
		"Generally, you can avoid a penalty if you make timely Estimated Tax payments equal to 100% of the tax shown due on your SC1040 for the prior tax year. You must have filed a SC1040 for the prior tax year, and it must have covered a 12-month year.",
		"If the adjusted gross income on your prior year SC1040 was more than $150,000, the 100% rule is adjusted to 110% of last year's tax liability. Calculate adjusted gross income by using federal guidelines and making South Carolina adjustments.",
	)
	mustContain(t, "sc2210-2025.txt",
		"(Rev. 12/02/25)",
		"3. Current year tax: Subtract line 2 from line 1. If less than $100, stop; you do not owe a penalty.",
		"4. Multiply line 3 by 90%",
		"6. Subtract line 5 from line 3. If less than $100, stop; you do not owe a penalty.",
		"7. 2024 tax: 110% of the tax if the adjusted gross income was more than $150,000, or if married filing separately for 2025, more than $75,000",
		"8. Required annual payment: Enter the lesser of line 4 or line 7",
		"multiply SC2210, line 8 by 25% and enter in each column.",
		"4/15/25 6/15/25 9/15/25 1/15/26",
		"treating the South Carolina Income Tax withheld from your wages as paid on the dates it was actually withheld",
		"are considered payments of Estimated Tax in equal installments on the four installment dates, unless you prove otherwise.",
		"Payments are applied to any prior underpayment before being applied to the current installment.",
		"April 15, 2025 - June 30, 2025", "July 1, 2025 - September 30, 2025",
		"October 1, 2025 - December 31, 2025", "January 1, 2026 - April 15, 2026",
		"on line 18 X .07", "on line 24 X .07",
		"Calculate the period of the underpayment by counting the number of days after the due date of the installment through the date of payment or the end of the rate period (12/31/25 or 4/15/26), whichever is earlier.",
		"If you file your return and pay the tax due by January 31, 2026, you will have no penalty for the installment due January 15, 2026 and the periods of underpayment for earlier installments will end on January 15, 2026.",
		"your prior year tax liability was $0 for a full 12 months",
		"the total tax on your 2025 return minus the amount of tax you paid through withholding is less than $100.",
		"your gross income from farming and fishing is at least two-thirds (66.67%) of your annual gross income from all sources for 2024 or 2025",
		"Complete Schedule AI of the federal 2210 for South Carolina purposes.",
		"An underpayment was due to a casualty, disaster, or other unusual circumstance and it would be inequitable to impose the penalty.",
		"In 2024 or 2025, you retired after age 62, or became disabled and your underpayment was due to reasonable cause.",
		"The Estimated Income Tax penalty is attributed to personal service income earned in another state on which Income Tax due the other state was withheld.",
	)
	mustContain(t, "sc1040-instructions-2025-line33.txt",
		"If your adjusted gross income is $150,000 or more, the 100% rule is modified to be 110% of the tax shown on your 2024 Income Tax return.")
	mustContain(t, "sc-code-12-6.txt",
		"except for Sections 6654 and 6655 which are adopted as provided in Section 12-6-3910 and Section 12-54-55.",
		"the small amount provisions in Internal Revenue Code Sections 6654(e)(1) and 6655(f) are one hundred dollars;",
		"First quarter: | April 15 | | Second quarter: | June 15 | | Third quarter: | September 15 | | Fourth quarter: | January 15 of the following taxable year. |",
	)
	mustContain(t, "sc-code-12-54.txt",
		"the rate of interest on underpayments and overpayments is established by the department in the same manner and at the same time as the underpayment rate provided in Internal Revenue Code Sections 6621(a)(2) and 6622.",
		"in the same manner as prescribed by the provisions of Internal Revenue Code Section 6654 and applicable regulations except that the small amount provisions are one hundred dollars. No interest or penalty is due under this item for underpayments attributable to personal service income earned in another state on which income tax due the other state was withheld;",
	)
	mustContain(t, "usc-6654.txt",
		"(i) 90 percent of the tax shown on the return for the taxable year",
		"(ii) 100 percent of the tax shown on the return of the individual for the preceding taxable year.",
		`exceeds $150,000, clause (ii) of subparagraph (B) shall be applied by substituting "110 percent" for "100 percent".`,
		`clause (i) shall be applied by substituting "$75,000" for "$150,000".`,
		"the amount of any required installment shall be 25 percent of the required annual payment.",
		"1st | 22.5 | 2nd | 45 | 3rd | 67.5 | 4th | 90. |",
		"for months in the taxable year ending before the due date for the installment",
		"an equal part of such amount shall be deemed paid on each due date for such taxable year, unless the taxpayer establishes the dates on which all amounts were actually withheld",
		"If, on or before January 31 of the following taxable year, the taxpayer files a return for the taxable year and pays in full the amount computed on the return as payable, then no addition to tax shall be imposed under subsection (a) with respect to any underpayment of the 4th required installment for the taxable year.",
		"(A) the 15th day of the 4th month following the close of the taxable year, or (B) with respect to any portion of the underpayment, the date on which such portion is paid.",
		"(C) the individual was a citizen or resident of the United States throughout the preceding taxable year.",
	)
	mustContain(t, "il26-19.txt",
		"August 24, 2026",
		"simple interest applies to the underpayment of declaration of estimated tax.",
		"01/01/25 – 03/31/26 7%", "04/01/26 – 06/30/26 6%", "07/01/26 – 12/31/26 7%",
	)
	mustContain(t, "rp23-1.txt",
		"when the last day of a specified time period is a Saturday, Sunday, or a legal holiday, the end of the period is extended to the next business day.",
		"A legal holiday falling on Sunday is observed on the following Monday and a legal holiday falling on Saturday is observed on the preceding Friday.",
	)
	mustContain(t, "act110-2026.txt",
		"$0 $30,000 1.99% times the amount", "$30,000 or more 5.21% times the amount minus $966",
		"SECTION 8. This act takes effect upon approval by the Governor and first applies to tax years beginning after 2025.",
		"Approved the 30th day of March, 2026.",
	)
	mustContain(t, "il26-20.txt",
		"The Governor signed Act 110 of 2026 into law on March 30, 2026.",
		"These changes are effective for income tax years beginning after December 31, 2025.",
		"the calculation for South Carolina taxable income will begin with federal AGI instead of federal taxable income for tax years beginning after 2025.",
	)
}

// The 2026 dates are the ones the SC1040ES and § 12-6-3910(A)(3)(a) give.
// Each is a weekday and none is a South Carolina legal holiday, so none
// moves. Each needs a quarter of the $9,000 required.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	cal := must(southcarolinataxcalc.EstimatedCalendar(2026))
	res := must(rules(t).Schedule(base().facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if !cal.IsBusinessDay(in.Due()) {
			t.Errorf("installment %d falls on %v, which is not a business day", i+1, in.Due())
		}
		if want := money.USCents(i+1) * 2_250 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
		if in.Status() != estimated.StatusShort || in.Shortfall() != 2_250*money.Dollar {
			t.Errorf("installment %d is %v with %v short, want $2,250 short", i+1, in.Status(), in.Shortfall())
		}
	}

	// Last year's overpayment applied forward counts on April 15.
	h := base()
	h.payments = []estimated.Payment{must(estimated.NewPayment().Amount(2_250 * money.Dollar).FromPriorYear().Build())}
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].Status() != estimated.StatusMet || s[1].Status() != estimated.StatusShort {
		t.Errorf("with $2,250 applied forward: April %v, June %v; want met and short", s[0].Status(), s[1].Status())
	}
}

// rp23Holidays is SC Revenue Procedure #23-1's list of legal holidays, read
// from its extract: each line is a name, a run of dots, and the rule for the
// date.
func rp23Holidays(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("testdata/rp23-1.txt")
	if err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile(`^(.+?)\s*\.{5,}(.+)$`)
	out := map[string]string{}
	for _, l := range strings.Split(string(raw), "\n") {
		if m := line.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			out[strings.TrimSuffix(m[1], "*")] = m[2]
		}
	}
	return out
}

// observed resolves one of Revenue Procedure #23-1's rules for a year, and
// moves a Saturday to the Friday before and a Sunday to the Monday after.
func observed(t *testing.T, rule string, year int) estimated.Date {
	t.Helper()
	months := map[string]time.Month{}
	for m := time.January; m <= time.December; m++ {
		months[m.String()] = m
	}
	weekdays := map[string]time.Weekday{"Monday": time.Monday, "Thursday": time.Thursday}
	nth := map[string]int{"First": 1, "Second": 2, "Third": 3, "Fourth": 4}
	var d estimated.Date
	if m := regexp.MustCompile(`^(\w+) (\d+)$`).FindStringSubmatch(rule); m != nil {
		n := 0
		for _, c := range m[2] {
			n = n*10 + int(c-'0')
		}
		d = day(year, months[m[1]], n)
	} else if m := regexp.MustCompile(`^(First|Second|Third|Fourth|Last) (\w+) in (\w+)$`).FindStringSubmatch(rule); m != nil {
		month, wd := months[m[3]], weekdays[m[2]]
		if m[1] == "Last" {
			d = day(year, month+1, 1).AddDays(-1)
			for d.Weekday() != wd {
				d = d.AddDays(-1)
			}
		} else {
			d = day(year, month, 1)
			for d.Weekday() != wd {
				d = d.AddDays(1)
			}
			d = d.AddDays(7 * (nth[m[1]] - 1))
		}
	} else if rule == "Friday following Thanksgiving" {
		return observed(t, "Fourth Thursday in November", year).AddDays(1)
	} else {
		t.Fatalf("Revenue Procedure #23-1 rule %q is not one this test reads", rule)
	}
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDays(-1)
	case time.Sunday:
		return d.AddDays(1)
	}
	return d
}

// The calendar is Revenue Procedure #23-1's fifteen holidays, worked out for
// 2026 and January to April 15, 2027 by its own rules, and it moves a due
// date that falls on one. Confederate Memorial Day, May 10, 2026, is a Sunday
// and is observed on Monday the 11th.
func TestEstimatedCalendarIsRevenueProcedureTwentyThreeOne(t *testing.T) {
	list := rp23Holidays(t)
	if len(list) != 15 || list["Confederate Memorial Day"] != "May 10" || list["Day after Christmas"] != "December 26" {
		t.Fatalf("read %d holidays from Revenue Procedure #23-1: %v", len(list), list)
	}
	want := map[estimated.Date]bool{}
	for _, rule := range list {
		for _, y := range []int{2026, 2027} {
			if d := observed(t, rule, y); !d.Before(day(2026, 1, 1)) && !d.After(day(2027, 4, 15)) {
				want[d] = true
			}
		}
	}
	cal := must(southcarolinataxcalc.EstimatedCalendar(2026))
	got := map[estimated.Date]bool{}
	for _, h := range cal.Holidays() {
		got[h.On()] = true
		if !want[h.On()] {
			t.Errorf("the calendar has %v (%s), which Revenue Procedure #23-1 does not give", h.On(), h.Name())
		}
	}
	for d := range want {
		if !got[d] {
			t.Errorf("the calendar is missing %v", d)
		}
	}
	if !strings.Contains(cal.Source(), "Revenue Procedure #23-1") {
		t.Errorf("the calendar cites %q", cal.Source())
	}

	for _, tc := range []struct{ from, to estimated.Date }{
		{day(2026, 5, 10), day(2026, 5, 12)},   // Sunday, then Confederate Memorial Day observed
		{day(2026, 7, 3), day(2026, 7, 6)},     // Independence Day observed, then the weekend
		{day(2026, 12, 24), day(2026, 12, 28)}, // Christmas Eve, Christmas Day, the weekend
		{day(2027, 1, 31), day(2027, 2, 1)},    // a Sunday
		{day(2026, 6, 15), day(2026, 6, 15)},   // a due date that is a business day
	} {
		if got := cal.NextBusinessDay(tc.from); got != tc.to {
			t.Errorf("a date due %v moves to %v, want %v", tc.from, got, tc.to)
		}
	}
	if _, err := southcarolinataxcalc.EstimatedCalendar(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("the 2025 calendar: %v, want ErrNoRules", err)
	}
}

// The $100 threshold, one cent either side: tax less withholding of $99.99
// owes no penalty and $100.00 does (SC2210 line 6, "If less than $100").
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		tax, withheld money.USCents
		exempt        bool
	}{
		{9_999, 0, true},
		{10_000, 0, false},
		{50_000, 40_001, true},
		{50_000, 40_000, false},
	} {
		h := household{tax: tc.tax, withheld: tc.withheld}
		res := must(rules(t).Requirement(h.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("tax %v, withheld %v: exempt %v, want %v (%s)", tc.tax, tc.withheld, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		if c, _ := res.Value().Chosen(); tc.exempt && (c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding under $100") {
			t.Errorf("tax %v, withheld %v: chosen %v %q at %v, want the $100 exception", tc.tax, tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if tc.exempt && p.Value().Total() != 0 {
			t.Errorf("tax %v, withheld %v: exempt with a penalty of %v", tc.tax, tc.withheld, p.Value().Total())
		}
		if !tc.exempt && p.Value().Total() == 0 {
			t.Errorf("tax %v, withheld %v: owes no penalty on an unpaid quarter", tc.tax, tc.withheld)
		}
	}
}

// The 110% step, one cent either side: "more than $150,000", or more than
// $75,000 on a married separate return.
func TestEstimatedPriorYearStepBoundaries(t *testing.T) {
	for _, tc := range []struct {
		status filing.Status
		agi    money.USCents
		want   money.Rate
	}{
		{filing.Single, 15_000_000, money.Percent(100)},
		{filing.Single, 15_000_001, money.Percent(110)},
		{filing.MarriedFilingJointly, 15_000_001, money.Percent(110)},
		{filing.MarriedFilingSeparately, 7_500_000, money.Percent(100)},
		{filing.MarriedFilingSeparately, 7_500_001, money.Percent(110)},
	} {
		h := household{status: tc.status, tax: 20_000 * money.Dollar, prior: last(10_000*money.Dollar, tc.agi)}
		res := must(rules(t).Requirement(h.facts(t)))
		o := res.Value().Options()[1]
		if o.Percent() != tc.want || o.Amount() != tc.want.Of(10_000*money.Dollar) {
			t.Errorf("%v with AGI %v: %v of last year's tax, %v; want %v", tc.status, tc.agi, o.Percent(), o.Amount(), tc.want)
		}
		if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || res.Value().Target() != o.Amount() {
			t.Errorf("%v with AGI %v: chose %v at %v", tc.status, tc.agi, c.Basis(), res.Value().Target())
		}
		if has(res.Required(), working.PositionAdjustedGrossIncome) {
			t.Errorf("%v with AGI %v: asks about AGI when federal and state agree", tc.status, tc.agi)
		}
	}
}

// The target is the smaller of 90% of this year's tax and 100% of last
// year's, and last year's needs a filed twelve-month SC1040.
func TestEstimatedTargetOptions(t *testing.T) {
	res := must(rules(t).Requirement(base().facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || c.Percent() != money.Percent(90) || res.Value().Target() != 9_000*money.Dollar {
		t.Errorf("chose %v %v at %v, want 90%% of this year's, $9,000", c.Basis(), c.Percent(), res.Value().Target())
	}
	if !res.Value().Required() || !has(res.Required(), working.PositionPriorYearReturn) {
		t.Errorf("required %v, asks for last year's return %v", res.Value().Required(), has(res.Required(), working.PositionPriorYearReturn))
	}
	h := base()
	h.prior = last(8_000*money.Dollar, 90_000*money.Dollar)
	res = must(rules(t).Requirement(h.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || c.Percent() != money.Percent(100) || res.Value().Target() != 8_000*money.Dollar {
		t.Errorf("chose %v %v at %v, want 100%% of last year's, $8,000", c.Basis(), c.Percent(), res.Value().Target())
	}
	h.prior = last(9_000*money.Dollar, 90_000*money.Dollar)
	if c, _ := must(rules(t).Requirement(h.facts(t))).Value().Chosen(); c.Basis() != estimated.BasisThisYearTax {
		t.Errorf("at $9,000 each the target is %v; want this year's, which is not larger", c.Basis())
	}

	notFiled := must(estimated.NewPriorReturn().Build())
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Months(11).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(500 * money.Dollar).Build())).Build())
	for name, p := range map[string]*estimated.PriorReturn{"not filed": &notFiled, "not required for a short year": &notRequired, "a short year": &short} {
		h := base()
		h.prior = p
		res := must(rules(t).Requirement(h.facts(t)))
		if o := res.Value().Options()[1]; o.Available() || o.WhyNot() == "" {
			t.Errorf("%s: the prior-year option is available %v (%q)", name, o.Available(), o.WhyNot())
		}
		if res.Value().Exempt() || res.Value().Target() != 9_000*money.Dollar {
			t.Errorf("%s: exempt %v, target %v", name, res.Value().Exempt(), res.Value().Target())
		}
	}

	// Without last year's AGI, 110% is assumed and the question asked.
	noAGI := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(8_000 * money.Dollar).Build())).Build())
	h.prior = &noAGI
	res = must(rules(t).Requirement(h.facts(t)))
	if o := res.Value().Options()[1]; o.Percent() != money.Percent(110) || o.Amount() != 8_800*money.Dollar || !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Errorf("without AGI: %v, %v, asked %v; want 110%%, $8,800 and the question", o.Percent(), o.Amount(), has(res.Required(), working.PositionAdjustedGrossIncome))
	}

	// South Carolina AGI under the step and federal AGI over it: the
	// SC1040ES figure is used, and the other reading is reported.
	split := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(8_000 * money.Dollar).
		StateAGI(140_000 * money.Dollar).FederalAGI(160_000 * money.Dollar).Build())).Build())
	h.prior = &split
	res = must(rules(t).Requirement(h.facts(t)))
	if o := res.Value().Options()[1]; o.Percent() != money.Percent(100) || !asks(res.Required(), working.PositionAdjustedGrossIncome, "would make the test 110%") {
		t.Errorf("split AGI: %v, asked %v", o.Percent(), res.Required())
	}
}

// Without this year's tax the target is last year's figure alone, and the
// penalty against it is an upper bound. With no last year either there is no
// target. The no-liability exception needs nothing about this year.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: last(8_000*money.Dollar, 90_000*money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 8_000*money.Dollar || !r.TargetKnown() {
		t.Errorf("required %v, target %v; want $8,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for")
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	if got := sched.Value()[0].NeededBy(); got != 2_000*money.Dollar {
		t.Errorf("April needs %v, want $2,000", got)
	}
	p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if !p.Value().UpperBound() || p.Value().Total() == 0 {
		t.Errorf("penalty %v, upper bound %v; want a bound above zero", p.Value().Total(), p.Value().UpperBound())
	}

	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() || r.Required() {
		t.Errorf("with nothing known: target known %v, required %v", r.TargetKnown(), r.Required())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}

	zero := household{noTax: true, prior: last(0, 20_000*money.Dollar)}
	if p := must(rules(t).Penalty(zero.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 || p.Value().UpperBound() {
		t.Errorf("after a $0 year: penalty %v, upper bound %v; want an exact zero", p.Value().Total(), p.Value().UpperBound())
	}
}

// A shortfall that spans the July 1 rate change is charged 6% for the days to
// June 30 and 7% after. April's $2,250, paid August 14: 76 days × 6% ÷ 365 is
// $28.11 and 45 days × 7% ÷ 365 is $19.42. The 2025 SC2210's printed .07
// would make the first part $32.79.
func TestEstimatedPenaltyAcrossTheJulyRateChange(t *testing.T) {
	h := base()
	h.payments = []estimated.Payment{pay(day(2026, 8, 14), 2_250*money.Dollar)}
	p := must(rules(t).Penalty(h.facts(t), day(2026, 8, 14)))
	lines := p.Value().Lines()
	if len(lines) == 0 || lines[0].Installment() != 1 {
		t.Fatalf("lines %v", lines)
	}
	parts := lines[0].Parts()
	if len(parts) != 2 {
		t.Fatalf("April has %d parts, want two", len(parts))
	}
	want := []struct {
		to    estimated.Date
		days  int
		rate  money.Rate
		charg money.USCents
	}{
		{day(2026, 6, 30), 76, money.Percent(6), 2_811},
		{day(2026, 8, 14), 45, money.Percent(7), 1_942},
	}
	for i, w := range want {
		if parts[i].To() != w.to || parts[i].Days() != w.days || parts[i].Rate() != w.rate || parts[i].Charge() != w.charg || !strings.Contains(parts[i].Source(), "#26-19") {
			t.Errorf("part %d: to %v, %d days at %v, %v (%s); want to %v, %d days at %v, %v",
				i+1, parts[i].To(), parts[i].Days(), parts[i].Rate(), parts[i].Charge(), parts[i].Source(), w.to, w.days, w.rate, w.charg)
		}
	}
	if lines[0].Total() != 4_753 {
		t.Errorf("April's penalty is %v, want $47.53", lines[0].Total())
	}
}

// Nothing paid, figured to December 31: each quarter is charged in the
// SC2210's rate periods. April: $28.11 + $39.70 + $39.70. June: 15 days at
// 6%, $5.55, + $39.70 + $39.70. September: 15 days at 7%, $6.47, + $39.70.
// January has no day in 2026. $238.63 in all. The 2027 days have no published
// rate, so figuring to April 15, 2027 gives the same total and reports the
// gap; a later date stops at April 15.
func TestEstimatedPenaltyToTheYearEndAndTheUnpublishedRate(t *testing.T) {
	f := base().facts(t)
	p := must(rules(t).Penalty(f, day(2026, 12, 31)))
	wantLines := []money.USCents{10_751, 8_495, 4_617, 0}
	lines := p.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	for i, w := range wantLines {
		if lines[i].Total() != w {
			t.Errorf("line %d is %v, want %v", i+1, lines[i].Total(), w)
		}
	}
	if p.Value().Total() != 23_863 || has(p.Required(), working.PositionUnpublishedRate) {
		t.Errorf("to December 31: %v, unpublished rate asked %v; want $238.63", p.Value().Total(), has(p.Required(), working.PositionUnpublishedRate))
	}
	if !has(p.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver question is not asked")
	}

	p = must(rules(t).Penalty(f, day(2027, 4, 15)))
	if p.Value().Total() != 23_863 || !asks(p.Required(), working.PositionUnpublishedRate, "Dec 31, 2026") {
		t.Errorf("to April 15, 2027: %v, %v", p.Value().Total(), p.Required())
	}
	if p := must(rules(t).Penalty(f, day(2027, 6, 30))); p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
}

// A return filed and paid by February 1, 2027, which is January 31 moved past
// a Sunday, excuses the January installment, and the SC2210 ends the earlier
// installments' periods on January 15. Filed January 25, April's line runs
// 275 days, not 285; filed February 2, nothing is excused and the penalty
// runs to the day paid.
func TestEstimatedReturnFiledByFebruaryFirst(t *testing.T) {
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
		asOf    estimated.Date
		days    int
	}{
		{day(2027, 1, 25), true, day(2027, 1, 15), 275},
		{day(2027, 2, 1), true, day(2027, 1, 15), 275},
		{day(2027, 2, 2), false, day(2027, 2, 2), 293},
		{day(2027, 1, 10), true, day(2027, 1, 10), 270},
	} {
		h := base()
		h.returnFiled = tc.filed
		s := must(rules(t).Schedule(h.facts(t))).Value()
		if got := s[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
		if tc.excused && !applied(must(rules(t).Requirement(h.facts(t))).Applied(), "by February 1, 2027 (January 31, 2027, moved to the next business day)") {
			t.Errorf("filed %v: the moved January 31 is not stated", tc.filed)
		}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if p.Value().AsOf() != tc.asOf || p.Value().Lines()[0].Days() != tc.days {
			t.Errorf("filed %v: figured to %v, April for %d days; want %v and %d", tc.filed, p.Value().AsOf(), p.Value().Lines()[0].Days(), tc.asOf, tc.days)
		}
		if lines := p.Value().Lines(); tc.excused && len(lines) != 3 {
			t.Errorf("filed %v: %d lines, want three with January excused", tc.filed, len(lines))
		}
	}
}

// No penalty after a $0 year of twelve months, on a filed return or with none
// required. The forms set no condition of U.S. residence, so a filer who was
// not resident all of last year is excused too. An 11-month year is not.
func TestEstimatedNoLiabilityLastYear(t *testing.T) {
	zeroNonresident := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(0).Build())).FullYearResident(false).Build())
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	shortZero := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(0).Build())).Build())
	for name, tc := range map[string]struct {
		prior  *estimated.PriorReturn
		exempt bool
	}{
		"$0 on a filed return":      {last(0, 30_000*money.Dollar), true},
		"$0, not resident all year": {&zeroNonresident, true},
		"no return required":        {&notRequired, true},
		"$0 for 11 months":          {&shortZero, false},
	} {
		h := base()
		h.prior = tc.prior
		res := must(rules(t).Requirement(h.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", name, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); (p.Value().Total() == 0) != tc.exempt {
			t.Errorf("%s: penalty %v", name, p.Value().Total())
		}
	}
}

// An underpayment from personal service income earned in another state,
// which withheld its own tax, bears no penalty. Not answered, it is asked
// when there is a penalty, and taken as not.
func TestEstimatedIncomeEarnedInAnotherState(t *testing.T) {
	yes := must(southcarolinataxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).UnderpaymentFromOtherStateIncome(true).Build())
	h := base()
	h.extras = &yes
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 || len(p.Value().Lines()) != 0 || p.Value().Reason() != "The due dates that fall short are excused." {
		t.Errorf("a penalty of %v, %q", p.Value().Total(), p.Value().Reason())
	}
	for i, in := range must(rules(t).Schedule(h.facts(t))).Value() {
		if in.Status() != estimated.StatusExcused || in.Shortfall() == 0 {
			t.Errorf("installment %d is %v with %v short, want excused with a shortfall", i+1, in.Status(), in.Shortfall())
		}
	}

	unanswered := must(southcarolinataxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).Build())
	h.extras = &unanswered
	p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if p.Value().Total() != 23_863 || !asks(p.Required(), working.PositionJurisdictionFact, "another state") {
		t.Errorf("unanswered: penalty %v, asked %v", p.Value().Total(), p.Required())
	}
}

// The casualty and retirement waivers are the department's to grant: the
// penalty is reported before them, with the waiver asked for named.
func TestEstimatedWaiverIsReportedNotApplied(t *testing.T) {
	for _, k := range []estimated.WaiverKind{estimated.WaiverCasualty, estimated.WaiverRetiredOrDisabled} {
		h := base()
		h.waiver = &k
		p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
		if p.Value().Total() != 23_863 || p.Value().Waiver() != k || has(p.Required(), working.PositionPenaltyWaiver) {
			t.Errorf("%v: penalty %v, waiver %v, asked %v", k, p.Value().Total(), p.Value().Waiver(), has(p.Required(), working.PositionPenaltyWaiver))
		}
	}
}

// The SC2210's first two waivers are the federal pair, at age 62, in 2025
// or 2026: the penalty stays in full, with what the department can waive.
func TestEstimatedWaiverOutcome(t *testing.T) {
	mustContain(t, "sc2210-2025.txt",
		"An underpayment was due to a casualty, disaster, or other unusual circumstance and it would be inequitable to impose the penalty.",
		"In 2024 or 2025, you retired after age 62, or became disabled and your underpayment was due to reasonable cause.")
	for name, tc := range map[string]struct {
		pos      estimated.Positions
		eligible bool
		reason   string
	}{
		"a disaster":       {must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()), true, "a disaster"},
		"retired at 64":    {must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, time.March, 1)).Build()), true, "after reaching 62"},
		"disabled in 2025": {must(estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2025, 6, 1)).Build()), true, "reasonable cause"},
		"retired in 2020":  {must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, time.March, 1)).Build()), false, "2025 or 2026"},
		"retired at 60":    {must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1966, time.March, 1)).Build()), false, "reached 62"},
	} {
		h := base()
		h.positions = &tc.pos
		p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31))).Value()
		o, ok := p.WaiverOutcome()
		if p.Total() != 23_863 || !ok || o.Eligible() != tc.eligible || o.Refused() == tc.eligible || !strings.Contains(o.Reason(), tc.reason) {
			t.Errorf("%s: penalty %v, outcome %v %+v", name, p.Total(), ok, o)
		}
		want := p.Total()
		if tc.eligible {
			want = 0
		}
		if p.IfWaived() != want {
			t.Errorf("%s: %v if waived, want %v", name, p.IfWaived(), want)
		}
	}
}

// An exception sets the target, so last year's return is not asked for.
func TestEstimatedExemptDoesNotAskForLastYear(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 50 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() || has(res.Required(), working.PositionPriorYearReturn) {
		t.Errorf("exempt %v, required %v", res.Value().Exempt(), res.Required())
	}
}

// A nonresident contractor with 2% withheld need not file a declaration, but
// the SC2210 has no matching exception, so the penalty is still figured and
// the question is left open.
func TestEstimatedNonresidentContractor(t *testing.T) {
	yes := must(southcarolinataxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).UnderpaymentFromOtherStateIncome(false).NonresidentContractor(true).Build())
	h := base()
	h.extras = &yes
	res := must(rules(t).Requirement(h.facts(t)))
	if res.Value().Required() || res.Value().Exempt() || res.Value().Target() != 9_000*money.Dollar {
		t.Errorf("required %v, exempt %v, target %v", res.Value().Required(), res.Value().Exempt(), res.Value().Target())
	}
	if !asks(res.Required(), working.PositionJurisdictionFact, "excuses the penalty") {
		t.Errorf("the open question is not asked: %v", res.Required())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31))); p.Value().Total() != 23_863 {
		t.Errorf("penalty %v, want $238.63", p.Value().Total())
	}

	// Not answered: asked, and taken as not.
	unanswered := must(southcarolinataxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).Build())
	h.extras = &unanswered
	res = must(rules(t).Requirement(h.facts(t)))
	if !res.Value().Required() || !asks(res.Required(), working.PositionJurisdictionFact, "nonresident contractor") {
		t.Errorf("unanswered: required %v, asked %v", res.Value().Required(), res.Required())
	}
}

// Withholding counts in four equal parts unless the filer elects the dates it
// was withheld (SC2210 box D). $9,000 withheld on December 15 meets every
// quarter spread evenly, and only January by its dates.
func TestEstimatedWithholdingCreditedAsWithheld(t *testing.T) {
	h := base()
	h.withheld = 9_000 * money.Dollar
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("spread evenly: a penalty of %v", p.Value().Total())
	}
	h.asWithheld = true
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []estimated.Status{estimated.StatusShort, estimated.StatusShort, estimated.StatusShort, estimated.StatusMet} {
		if s[i].Status() != want {
			t.Errorf("by its dates, installment %d is %v, want %v", i+1, s[i].Status(), want)
		}
	}
	// The December 15 withholding pays April, June and September that day.
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if lines := p.Value().Lines(); len(lines) != 3 || lines[0].Days() != 244 {
		t.Errorf("by its dates: %d lines; want three, April's for 244 days", len(lines))
	}
}

// The annualised method takes 22.5%, 45%, 67.5% and 90% of each period's
// annualised tax, less earlier installments, capped by the regular amount plus
// what it carried. Annualised tax of $2,000, $4,000, $8,000 and $10,000 gives
// $450, $1,350, $3,600 and $3,600.
func TestEstimatedAnnualisedMethod(t *testing.T) {
	h := base()
	h.annualised = []money.USCents{2_000 * money.Dollar, 4_000 * money.Dollar, 8_000 * money.Dollar, 10_000 * money.Dollar}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{450 * money.Dollar, 1_800 * money.Dollar, 5_400 * money.Dollar, 9_000 * money.Dollar} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, s[i].NeededBy(), want)
		}
	}
	wrong := estimated.NewFacts(southCarolina, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).State(answered())
	for _, end := range []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 11, 30)} {
		wrong.Periods(must(estimated.NewPeriod().End(end).AnnualisedTax(0).Build()))
	}
	if _, err := rules(t).Schedule(must(wrong.Build())); err == nil {
		t.Error("a fourth period ending November 30 is accepted")
	}
}

// The SC1040ES would put a late starter's first payment on a later date; the
// SC2210 still charges 25% by every date. The groups start after April 1,
// after June 1 and after September 1.
func TestEstimatedLateStartIsNotedNotApplied(t *testing.T) {
	for _, tc := range []struct {
		met  estimated.Date
		note string
	}{
		{day(2026, 4, 1), ""},
		{day(2026, 4, 2), "1/2 of the estimate on June 15, 2026"},
		{day(2026, 6, 2), "3/4 of the estimate on September 15, 2026"},
		{day(2026, 9, 2), "all of the estimate on January 15, 2027"},
	} {
		h := base()
		h.firstMet = tc.met
		res := must(rules(t).Schedule(h.facts(t)))
		if got := applied(res.Applied(), "the requirement first arose"); got != (tc.note != "") || (tc.note != "" && !applied(res.Applied(), tc.note)) {
			t.Errorf("first met %v: noted %v, want %q", tc.met, got, tc.note)
		}
		if res.Value()[0].NeededBy() != 2_250*money.Dollar || res.Value()[0].Status() != estimated.StatusShort {
			t.Errorf("first met %v: April needs %v and is %v", tc.met, res.Value()[0].NeededBy(), res.Value()[0].Status())
		}
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(base().facts(t)))
	want := map[string][4]string{
		"The 2026 worksheet predates Act 110":                     {"0%, 3% and 6%", "Rev. 10/13/25", "1.99%", "Act No. 110 of 2026"},
		"The interest rate":                                       {"7% (.07) in all four rate periods", "#26-19", "at the same time as the federal underpayment rate", "§§ 12-54-25(D)"},
		"The 110% test at exactly $150,000":                       {"\"$150,000 or more\"", "line 33", "exceeds $150,000", "§ 6654(d)(1)(C)(i)"},
		"The married filing separately step":                      {"$75,000", "Part I line 7", "$75,000", "§ 6654(d)(1)(C)(ii)"},
		"No tax liability last year":                              {"no condition of U.S. citizenship or residence", "Exceptions to the penalty", "citizen or resident", "§ 6654(e)(2)(C)"},
		"The January 31 date":                                     {"February 1", "SC1040ES (2026)", "next business day", "§ 12-60-50(A)"},
		"Earlier installments after a return filed by January 31": {"end on January 15", "Period of underpayment", "until the underpayment is paid", "§ 6654(b)(2)"},
		"Income earned in another state":                          {"request a waiver", "Waiver of penalty item 3", "no interest or penalty is due", "§ 12-54-55(1)"},
		"A requirement first met after April 1":                   {"25% of the required annual payment by every date", "worksheet line 11", "no rule for a requirement that arises late", "§ 6654(c), (d)(1)(A)"},
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
	}
	// The tax and the rate follow the statute; the rest follow the forms.
	// With nothing paid, April and June are short while the rates differ,
	// and the tax is given, so those two affect these facts; nothing else
	// they turn on is present.
	for _, d := range res.Divergences() {
		statute := d.Rule() == "The 2026 worksheet predates Act 110" || d.Rule() == "The interest rate"
		if want := map[bool]working.Reading{true: working.ReadingStatute, false: working.ReadingForm}[statute]; d.Uses() != want {
			t.Errorf("%q uses %v, want %v", d.Rule(), d.Uses(), want)
		}
		if d.AffectsHousehold() != statute {
			t.Errorf("%q affects these facts %v, want %v", d.Rule(), d.AffectsHousehold(), statute)
		}
	}
	// The tax given is taken as figured under Act 110, and said so.
	if !applied(res.Applied(), "taken as figured under Act 110") {
		t.Error("the Act 110 reading of the tax is not stated")
	}
	// The out-of-date worksheet's own example: $25,000 at 6% less $656 is
	// $844 under the schedule Act 110 replaced.
	mustContain(t, "sc1040es-2026.txt",
		"0% times the amount 3% times the amount minus $109 6% times the amount minus $656",
		"$25,000 income from line 3 of worksheet .06 percent from tax computation schedule 1,500 (1,500 rounded to the nearest dollar) 656 amount from tax computation schedule 844 tax to be entered on line 4 of worksheet",
		"1. Enter the federal taxable income from your 2026 federal 1040ES, line 3")
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := southcarolinataxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())
	facts := func(j estimated.Jurisdiction, year int) *estimated.FactsBuilder {
		return estimated.NewFacts(j, year).FilingStatus(filing.Single).ThisYear(this)
	}
	// Two-thirds of gross income from farming and fishing together, in
	// either year, puts a filer under rules this build does not have.
	for name, f := range map[string]estimated.Facts{
		"a farmer at 66.67%":           must(facts(southCarolina, 2026).FarmShare(6_667).State(answered()).Build()),
		"farming and fishing together": must(facts(southCarolina, 2026).FarmShare(3_334).FishShare(3_333).State(answered()).Build()),
		"a farmer last year":           must(facts(southCarolina, 2026).State(must(southcarolinataxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(true).Build())).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	if _, err := rules(t).Requirement(must(facts(southCarolina, 2026).FarmShare(6_666).State(answered()).Build())); err != nil {
		t.Errorf("a farmer at 66.66%%: %v", err)
	}
	for name, f := range map[string]estimated.Facts{
		"Pennsylvania's facts": must(facts(estimated.Pennsylvania, 2026).Build()),
		"2027":                 must(facts(southCarolina, 2027).Build()),
		"other extras":         must(facts(southCarolina, 2026).State("not South Carolina's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(base().facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}

	// Last year's farming share not given: asked, and taken as under
	// two-thirds.
	f := must(facts(southCarolina, 2026).Build())
	if res := must(rules(t).Requirement(f)); !asks(res.Required(), working.PositionJurisdictionFact, "farmer rules") {
		t.Errorf("last year's farming share is not asked: %v", res.Required())
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"$100", "another state", "January 15", "Confederate Memorial Day", "110%", "$75,000", "6% from April to June", "February 1, 2027"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if e.VersusFederal() == "" || len(e.Sources()) == 0 {
		t.Error("the explanation has no comparison with federal or no sources")
	}
}

// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
// never as a negative amount.
func TestEstimatedWithholdingOverTheTaxReadsAsARefund(t *testing.T) {
	figures := func(tax money.USCents) estimated.Figures {
		return must(estimated.NewFigures().Tax(tax).FederalAGI(60_000 * money.Dollar).StateAGI(60_000 * money.Dollar).Build())
	}
	last := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800 * money.Dollar)).Build())
	b := estimated.NewFacts("SC", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
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
