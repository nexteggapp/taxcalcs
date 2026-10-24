// Hawaii's estimated tax rules for 2026, checked against the Department of
// Taxation's own documents, the Hawaii Revised Statutes and the department's
// rule.
//
// Hawaii publishes no exhaustive oracle and no worked penalty with a stated
// answer. Two printed figures come close. Form N-210 line 19 prints the months
// for a shortfall left unpaid to the April 20 stop, 12, 10, 7 and 3, and the
// instructions work Mr. Aloha's $1,000 April shortfall, paid on June 10, to
// two months; TestEstimatedPenaltyMonthsMatchTheForm and
// TestEstimatedMonthsMatchMrAlohasExample reproduce both. Every threshold,
// percentage, date and rate is compared against its extract in testdata/. The
// penalty arithmetic, the boundaries and the exceptions are hand-written
// cases, worked by hand in their comments.
package hawaiitaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/hawaiitaxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const hawaii = estimated.Jurisdiction("HI")

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
	return must(hawaiitaxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

func asks(rs []working.Required, k working.PositionKind) string {
	var out []string
	for _, r := range rs {
		if r.Position() == k {
			out = append(out, r.Question()+" "+r.Effect())
		}
	}
	return strings.Join(out, " | ")
}

func applied(rs []working.Rule) string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Cite()+": "+r.Applied())
	}
	return strings.Join(out, " | ")
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

// printed are the 2026 dates the department prints; dues are the dates they
// move to.
var (
	printed = []estimated.Date{day(2026, 4, 20), day(2026, 6, 20), day(2026, 9, 20), day(2027, 1, 20)}
	dues    = []estimated.Date{day(2026, 4, 20), day(2026, 6, 22), day(2026, 9, 21), day(2027, 1, 20)}
)

type household struct {
	status      filing.Status
	tax         money.USCents // Form N-210 line 3
	noTax       bool
	withheld    money.USCents
	withheldOn  estimated.Date
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *hawaiitaxcalc.EstimatedFacts
	noExtras    bool
	returnFiled estimated.Date
	firstMet    estimated.Date
	periods     []money.USCents // N-210 Schedule A line 16 for each period
	asWithheld  bool
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions
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
	b := estimated.NewFacts(hawaii, 2026).FilingStatus(status).ThisYear(must(fb.Build())).
		Paid(h.payments...).ReturnFiled(h.returnFiled).RequirementFirstMet(h.firstMet)
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
	switch {
	case h.extras != nil:
		b.State(*h.extras)
	case !h.noExtras:
		b.State(must(hawaiitaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).NonresidentAlienWithoutWages(false).Build()))
	}
	eb := estimated.NewElections()
	if h.periods != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.periods {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		eb.Annualised()
	}
	if h.asWithheld {
		eb.WithholdingAsWithheld()
	}
	b.Elections(must(eb.Build()))
	if h.waiver != nil {
		b.Positions(must(estimated.NewPositions().Waiver(*h.waiver).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

// prior is a filed twelve-month Hawaii return with its tax, for a full-year
// resident.
func prior(tax money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(true).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "hrs-235-97.txt",
		"The payment voucher shall be filed, in the case of taxpayers on the calendar year basis, on or before April 20.",
		"Thereafter, on the twentieth day of June and September, the taxpayer shall transmit with the payment voucher, payment of one-quarter of the estimated tax.",
		"The fourth quarter payment of the estimated tax shall be transmitted with the payment voucher by January 20 of the year following the taxable year",
		"(6) In the case of a taxpayer whose tax liability is less than $500, the filing of an estimate and the payment of estimated tax shall not be required.",
		"there shall be added to the tax for the taxable year an amount determined at the rate of two-thirds of one per cent a month or fraction of a month upon the amount of the underpayment for the period of the underpayment.",
		"(A) The twentieth day of the fourth month following the close of the taxable year, or",
		"a payment of estimated tax on any installment date shall be credited against unpaid required installments in the order in which the installments are required to be paid.",
		"Sections 6654(d), (e)(2), (e)(3), (h), (i), (j), (k), and (l),",
		"provided that the due dates contained in any of the preceding Internal Revenue Code sections shall be deemed to be the twentieth day of the applicable month",
		"the required annual payment shall be the lesser of sixty per cent of the tax shown on the return for the taxable year",
		"or one hundred per cent of the tax shown on the return of the individual for the preceding taxable year.",
		"an equal part of any such amount shall be deemed paid on each installment date for the taxable year, unless the taxpayer establishes the dates on which all amounts were actually withheld or collected",
		"pay the estimated tax in equal installments computed by allocating the entire amount shown by the estimate for the current taxable year to the remaining quarterly payment dates.",
	)
	mustContain(t, "hrs-231-21.txt",
		"When the due date for any remittance or document required by any law imposing a tax falls on a Saturday, Sunday, or legal holiday, the remittance or document shall not be due until the next succeeding day which is not a Saturday, Sunday, or legal holiday.",
	)
	mustContain(t, "tf2019-3.txt",
		"Revised February 2026",
		"(1) You expect to owe at least $500 in tax for the current year, after subtracting your withholding and credits.",
		"● 60% of the tax to be shown on your current year tax return, or",
		"● 100% of the tax shown on your tax return for the preceding year. Your tax return for the preceding year must cover all 12 months.",
		"for the current year is less than $500; or",
		"● You were a full-year Hawaii resident in the preceding year,",
		"If you were a nonresident or a part-year resident in the preceding year, you cannot use the exception in question 5, line (2).",
		"1st Payment April 20, current year 2nd Payment June 20, current year 3rd Payment September 20, current year 4th Payment January 20, next year",
		"if you file your current year tax return by January 31 of the following year and pay the entire balance due with your return.",
		"If the due date for an estimated tax payment falls on a Saturday, Sunday, or legal holiday, the payment will be on time if you make it on the next day that is not a Saturday, Sunday, or a holiday.",
		"11a Multiply line 10 by 60%",
		"If less than $500, you are not required to make estimated tax payments at this time.",
		"Tax Rate Schedules for 2026",
	)
	mustContain(t, "n11i-2025-estimated-tax.txt",
		"(1) You expect to owe at least $500 in tax for the current year, after subtracting your withholding and credits.",
		"60% of the tax to be shown on your current year tax return, or 100% of the tax shown on your tax return for the preceding year.",
		"four equal amounts by April 20, 2026, June 20, 2026 September 20, 2026, and January 20, 2027.",
	)
	mustContain(t, "n210i-2025.txt",
		"1. 60% of your 2025 tax, or 2. 100% of your 2024 tax. Your 2024 tax return must cover a 12-month period.",
		"minus the amount of tax you paid through withholding and tax credits is less than $500.",
		"(1) You were a full-year Hawaii resident in 2024 (or an estate of a Hawaii decedent or a Hawaii trust), (2) You had no tax liability for 2024, and (3) Your 2024 tax year covered a 12-month period.",
		"You had no tax liability for 2024 if your total tax was zero or you were not required to file an income tax return.",
		"If you were a nonresident or a part-year resident in 2024, you do not meet the exception under number 2.",
		"1. Your gross income from farming or fishing is at least two-thirds of your annual gross income from all sources for 2024 or 2025.",
		"If you did not file a return for 2024, or if your 2024 tax year was less than 12 months, do not complete line 7.",
		"you are considered to have paid one-fourth of these amounts on each payment due date unless you can show otherwise.",
		"If your 2024 return was fully paid by the due date, treat the overpayment as a payment made on April 20, 2025.",
		"6. Any payments made on the next working day after the payment due date because of a Saturday, Sunday, or holiday, will be considered paid on the payment due date.",
		"the penalty is imposed on the amount of the underpayment of estimated tax at the rate of 2/3% (.00667) a month or part of a month.",
		"counting the number of months after the due date of the installment to and including the date of payment or April 20, 2026, whichever is earlier.",
		"A month, for the purposes of computing the penalty, is the period from the 21st day of the current calendar month through the 20th day of the following calendar month.",
		"A part of a month is counted as one month.",
		"If you file your return and pay the tax due by January 31, 2026, you will have no penalty for the January installment, and the periods of underpayment for earlier installments will end on January 20, 2026.",
		"If you use Schedule A for any payment due date, you must use it for all payment due dates.",
		"Schedule A selects the smaller of the annualized income installment or the regular installment (that has been increased by the amount saved by using the annualized income installment method in figuring any earlier installments).",
		"1. You retired after reaching age 62 or became disabled in 2024 or 2025",
		"2. You didn’t make a payment because of a casualty, disaster, or other unusual circumstance and it would be inequitable to impose the penalty.",
	)
	mustContain(t, "n210-2025.txt",
		"5 Balance. Line 3 minus line 4. If this amount is less than $500, stop here; do not complete or file this form.",
		"6 Multiply the amount on line 3 by 60% (.60)",
		"(a) 4/20/2025 (b) 6/20/2025 (c) 9/20/2025 (d) 1/20/2026",
		"If April 20, 2026, is the date entered on line 18, enter 12, 10, 7, and 3, respectively",
		"20 Multiply the following: Number of months on line 19 x .00667 x",
		"1/1/25-3/31/25 1/1/25-5/31/25 1/1/25-8/31/25 1/1/25-12/31/25",
		"17 15% 30% 45% 60%",
		"24 Enter the smaller of line 20 or line 23 here",
		"you treat it as paid for estimated tax purposes when it was actually withheld instead of in equal amounts on the payment due dates.",
	)
	mustContain(t, "har-18-235-97.txt",
		"(Unofficial Compilation as of 12/31/2025)",
		"(A) The taxpayer’s estimated tax liability (after taking into account all taxes withheld or collected at the source) for the taxable year is less than $500;",
		"The taxpayer did not have any tax liability for the preceding taxable year; or",
		"estimated tax liability is $500 or more, the taxpayer shall submit estimated tax payments on a quarterly basis",
		"one payment on or before April 20; one payment on or before June 20; one payment on or before September 20; and one payment on or before January 20 following the close of the taxable year.",
		"occurs after March 31 but before June 1, estimated tax shall be submitted to the department in three equal payments.",
		"occurs after May 31 but before September 1, estimated tax shall be submitted to the department in two equal payments.",
		"files an income tax return for the taxable year on or before January 31 following the close of the taxable year",
		"shall not be assessed any addition to tax with respect to any underpayment of the fourth quarter estimated tax payment of the taxable year.",
		"pursuant to section 235-97(f)(4), HRS, and as set forth in section 6654(e)(3) of the Internal Revenue Code of 1986.",
	)
	mustContain(t, "usc-26-6654-e-h.txt",
		"(A) the preceding taxable year was a taxable year of 12 months,",
		"(B) the individual did not have any liability for tax for the preceding taxable year, and",
		"(C) the individual was a citizen or resident of the United States throughout the preceding taxable year.",
		"by reason of casualty, disaster, or other unusual circumstances",
		"(I) retired after having attained age 62, or",
		"If, on or before January 31 of the following taxable year, the taxpayer files a return for the taxable year and pays in full the amount computed on the return as payable",
	)
}

// HRS § 235-97 was last amended in 2003, and the department's summary of the
// 2026 session lists no change to it or to estimated tax, so no form here
// predates a change in the law.
func TestEstimatedStatuteUnchangedSince2003(t *testing.T) {
	mustContain(t, "hrs-235-97.txt", "am L 2002, c 190, §1; am L 2003, c 14, §1]")
	mustContain(t, "ann26-06.txt", "DEPARTMENT OF TAXATION ANNOUNCEMENT NO. 2026-06", "Tax Law Changes from the 2026 Regular Legislative Session",
		"amends the income tax brackets for tax years 2027 and 2029")
	text := strings.ToLower(extract(t, "ann26-06.txt"))
	for _, s := range []string{"235-97", "estimated tax", "estimated income tax"} {
		if strings.Contains(text, s) {
			t.Errorf("Announcement 2026-06 mentions %q", s)
		}
	}
}

// The department prints April 20, June 20, September 20 and January 20.
// June 20, 2026 is a Saturday and September 20, 2026 a Sunday, so under HRS
// § 231-21 those two are due Monday, June 22 and Monday, September 21. The
// other two are weekdays and do not move. No HRS § 8-1 holiday falls on a due
// date or on the day it moves to: the § 8-1 holidays of those months are
// listed below. § 8-1's election day, November 3, 2026, and Juneteenth, a
// federal holiday that is not in § 8-1, fall on no date that matters here.
//
// January 31, 2027, the file-and-pay date, is a Sunday. Neither HAR
// § 18-235-97(h)(2) nor the forms say it moves, so it is not moved, and a
// return filed on Monday, February 1 is asked about.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	mustContain(t, "hrs-8-1.txt",
		"The first day in January, New Year's Day;",
		"The third Monday in January, Dr. Martin Luther King, Jr., Day;",
		"The last Monday in May, Memorial Day;",
		"The eleventh day in June, King Kamehameha I Day;",
		"The third Friday in August, Statehood Day;",
		"The first Monday in September, Labor Day;",
		"The twenty-sixth day in March, Prince Jonah Kuhio Kalanianaole Day;",
		"The Friday preceding Easter Sunday, Good Friday;",
	)
	holidays := map[estimated.Date]string{
		day(2026, 5, 25): "Memorial Day",
		day(2026, 6, 11): "King Kamehameha I Day",
		day(2026, 8, 21): "Statehood Day",
		day(2026, 9, 7):  "Labor Day",
		day(2027, 1, 1):  "New Year's Day",
		day(2027, 1, 18): "Dr. Martin Luther King, Jr., Day",
		day(2027, 3, 26): "Prince Jonah Kuhio Kalanianaole Day and Good Friday",
	}
	for d := range holidays {
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			t.Errorf("%v is listed as a weekday holiday and is a %v", d, d.Weekday())
		}
	}

	res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar}.facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		moved := printed[i]
		for moved.Weekday() == time.Saturday || moved.Weekday() == time.Sunday {
			moved = moved.AddDays(1)
		}
		if moved != dues[i] {
			t.Errorf("the printed %v moves to %v, not %v", printed[i], moved, dues[i])
		}
		if name, ok := holidays[dues[i]]; ok {
			t.Errorf("installment %d falls on %s", i+1, name)
		}
	}
	if wd := printed[1].Weekday(); wd != time.Saturday {
		t.Errorf("June 20, 2026 is a %v", wd)
	}
	if wd := printed[2].Weekday(); wd != time.Sunday {
		t.Errorf("September 20, 2026 is a %v", wd)
	}
	for _, s := range []string{"June 20, 2026 falls on a Saturday, so a payment on June 22, 2026 is on time", "September 20, 2026 falls on a Sunday, so a payment on September 21, 2026 is on time"} {
		if !strings.Contains(applied(res.Applied()), s) {
			t.Errorf("the working does not say %q", s)
		}
	}
	if _, ok := holidays[day(2027, 2, 1)]; ok {
		t.Error("February 1, 2027 is a holiday")
	}
	if _, ok := holidays[day(2027, 4, 20)]; ok || day(2027, 4, 20).Weekday() == time.Saturday || day(2027, 4, 20).Weekday() == time.Sunday {
		t.Error("April 20, 2027, the stop, is not a business day")
	}

	// A quarter of 60% of $40,000 is $6,000. Paid on the Monday each date
	// moved to, each installment is met.
	paid := household{tax: 40_000 * money.Dollar, payments: []estimated.Payment{
		pay(dues[0], 6_000*money.Dollar), pay(dues[1], 6_000*money.Dollar), pay(dues[2], 6_000*money.Dollar), pay(dues[3], 6_000*money.Dollar)}}
	for i, in := range must(rules(t).Schedule(paid.facts(t))).Value() {
		if in.Status() != estimated.StatusMet {
			t.Errorf("installment %d, paid on %v, is %v", i+1, dues[i], in.Status())
		}
	}
	if p := must(rules(t).Penalty(paid.facts(t), day(2027, 4, 20))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v on payments made on the moved dates", p.Value().Total())
	}

	if wd := day(2027, 1, 31).Weekday(); wd != time.Sunday {
		t.Fatalf("January 31, 2027 is a %v", wd)
	}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
		asked   bool
	}{
		{day(2027, 1, 29), true, false},
		{day(2027, 1, 31), true, false},
		{day(2027, 2, 1), false, true},
		{day(2027, 2, 2), false, false},
	} {
		res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar, returnFiled: tc.filed}.facts(t)))
		if got := res.Value()[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
		if got := strings.Contains(asks(res.Required(), working.PositionUnsettledRule), "Sunday"); got != tc.asked {
			t.Errorf("filed %v: the Sunday January 31 asked %v, want %v", tc.filed, got, tc.asked)
		}
	}
}

// N-210 Exception 1: no penalty when the tax less withholding is under $500.
// $1,000 of tax keeps 60% of it, $600, above the withholding, so the
// installment that is not exempt is short and owes a penalty.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{50_001, true},  // $499.99
		{50_000, false}, // $500.00
	} {
		h := household{tax: 1_000 * money.Dollar, withheld: tc.withheld}
		res := must(rules(t).Requirement(h.facts(t)))
		if got := res.Value().Exempt(); got != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, got, tc.exempt, res.Value().Why())
		}
		if got := res.Value().Required(); got == tc.exempt {
			t.Errorf("withheld %v: required %v", tc.withheld, got)
		}
		if c, ok := res.Value().Chosen(); tc.exempt && (!ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding under $500") {
			t.Errorf("withheld %v: chose %v %q at %v, want the exception", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 20)))
		if got := p.Value().Total() == 0; got != tc.exempt {
			t.Errorf("withheld %v: penalty %v", tc.withheld, p.Value().Total())
		}
	}
	// At $500 the worksheet's own test, the $600 target less $500 withheld,
	// is $100, under $500. The working says why payments are still shown.
	res := must(rules(t).Requirement(household{tax: 1_000 * money.Dollar, withheld: 50_000}.facts(t)))
	if !strings.Contains(applied(res.Applied()), "Worksheet line 13") {
		t.Errorf("the worksheet's line 13 is not explained: %s", applied(res.Applied()))
	}
}

// The target is the smaller of 60% of this year's tax and 100% of last
// year's, at any income, and the prior-year test needs a filed twelve-month
// Hawaii return.
func TestEstimatedTargetOptions(t *testing.T) {
	// 60% of $10,000 is $6,000.
	for _, tc := range []struct {
		priorTax money.USCents
		basis    estimated.Basis
		target   money.USCents
	}{
		{599_999, estimated.BasisPriorYearTax, 599_999},
		{600_001, estimated.BasisThisYearTax, 600_000},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(tc.priorTax)}.facts(t)))
		if c, _ := res.Value().Chosen(); c.Basis() != tc.basis || res.Value().Target() != tc.target {
			t.Errorf("last year's tax %v: chose %v at %v, want %v at %v", tc.priorTax, c.Basis(), res.Value().Target(), tc.basis, tc.target)
		}
		if o := res.Value().Options()[0]; o.Percent() != money.Percent(60) {
			t.Errorf("this year's option is %v, want 60%%", o.Percent())
		}
	}
	// No 110% step: $1,000,000 of federal AGI last year still gives 100%.
	rich := must(estimated.NewPriorReturn().Filed(true).FullYearResident(true).
		Figures(must(estimated.NewFigures().Tax(50_000 * money.Dollar).FederalAGI(1_000_000 * money.Dollar).Build())).Build())
	res := must(rules(t).Requirement(household{tax: 100_000 * money.Dollar, prior: &rich}.facts(t)))
	if o := res.Value().Options()[1]; o.Percent() != money.Percent(100) || o.Amount() != 50_000*money.Dollar || res.Value().Target() != 50_000*money.Dollar {
		t.Errorf("at $1,000,000 of AGI: %v of last year's tax, %v; target %v", o.Percent(), o.Amount(), res.Value().Target())
	}
	if has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Error("last year's AGI is asked for, though no Hawaii rule reads it")
	}

	f := must(estimated.NewFigures().Tax(1_000 * money.Dollar).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(f).FullYearResident(false).Build())
	notFiled := must(estimated.NewPriorReturn().Filed(false).FullYearResident(false).Build())
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(false).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   bool
	}{
		"no return on record": {nil, true},
		"not filed":           {&notFiled, false},
		"not required":        {&notRequired, false},
		"a six-month year":    {&short, false},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		if res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
		if res.Value().Target() != 6_000*money.Dollar {
			t.Errorf("%s: target %v, want $6,000", name, res.Value().Target())
		}
		if has(res.Required(), working.PositionPriorYearReturn) != tc.ask {
			t.Errorf("%s: asked for last year's return %v, want %v", name, !tc.ask, tc.ask)
		}
	}

	// Withholding that reaches the target needs no payments; a cent short
	// does.
	for _, tc := range []struct {
		withheld money.USCents
		required bool
	}{{600_000, false}, {599_999, true}} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld}.facts(t)))
		if res.Value().Required() != tc.required {
			t.Errorf("withheld %v: required %v, want %v", tc.withheld, res.Value().Required(), tc.required)
		}
	}
}

// $40,000 of tax, nothing withheld or paid, no return last year: the target
// is 60%, $24,000, and each installment $6,000. Figured to April 20, 2027,
// N-210 line 19 prints the months as 12, 10, 7 and 3, counted from the
// printed dates, not the moved ones:
//
//	April 20:     6,000 × .00667 × 12 = 480.24
//	June 20:      6,000 × .00667 × 10 = 400.20
//	September 20: 6,000 × .00667 × 7  = 280.14
//	January 20:   6,000 × .00667 × 3  = 120.06
//
// The total is $1,280.64. The first three shortfalls run past December 31
// into 2027. Hawaii's rate is fixed by HRS § 235-97(f) and no published
// change exists for 2027, so each line is one part at the one rate, with no
// split at the year end and no unpublished rate.
func TestEstimatedPenaltyMonthsMatchTheForm(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), day(2027, 4, 20)))
	p := res.Value()
	lines := p.Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 4", len(lines))
	}
	months := []int{12, 10, 7, 3}
	charges := []money.USCents{48_024, 40_020, 28_014, 12_006}
	for i, l := range lines {
		parts := l.Parts()
		if l.Shortfall() != 6_000*money.Dollar || l.Due() != dues[i] || len(parts) != 1 {
			t.Errorf("line %d: %v short, due %v, %d parts; want $6,000, %v, one part", i+1, l.Shortfall(), l.Due(), len(parts), dues[i])
			continue
		}
		pt := parts[0]
		if pt.Months() != months[i] || pt.Charge() != charges[i] || pt.From() != printed[i] || pt.To() != day(2027, 4, 20) || pt.Rate() != money.Percent(8) {
			t.Errorf("line %d: %d months from %v to %v at %v is %v; want %d months from %v, %v", i+1, pt.Months(), pt.From(), pt.To(), pt.Rate(), pt.Charge(), months[i], printed[i], charges[i])
		}
	}
	if p.Total() != 128_064 {
		t.Errorf("penalty %v, want $1,280.64", p.Total())
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("an unpublished rate is reported for a rate fixed by statute")
	}
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waivers are not asked about")
	}
	// The stop: figured to June 30, 2027, it is still to April 20.
	late := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), day(2027, 6, 30)))
	if late.Value().AsOf() != day(2027, 4, 20) || late.Value().Total() != 128_064 {
		t.Errorf("figured to %v at %v, want April 20, 2027 and $1,280.64", late.Value().AsOf(), late.Value().Total())
	}
}

// The form multiplies by .00667 a month; the statute's two-thirds of one
// percent is .006666…. 60% of $66,666.67 is $40,000.00, $10,000 a quarter.
// Nothing is paid until $10,000 on January 20, 2027, which pays April's
// shortfall first, nine months after it fell due. The form gives
// 10,000 × .00667 × 9 = $600.30 and the statute 10,000 × 2/3% × 9 = $600.00.
// The figure is the form's.
func TestEstimatedChargeUsesThePrintedFactor(t *testing.T) {
	h := household{tax: 6_666_667, payments: []estimated.Payment{pay(day(2027, 1, 20), 10_000*money.Dollar)}}
	lines := must(rules(t).Penalty(h.facts(t), day(2027, 4, 20))).Value().Lines()
	if len(lines) == 0 || lines[0].Installment() != 1 {
		t.Fatalf("no April line: %v", lines)
	}
	parts := lines[0].Parts()
	if len(parts) != 1 || parts[0].Months() != 9 || parts[0].To() != day(2027, 1, 20) || parts[0].Charge() != 60_030 {
		t.Fatalf("April: %+v; want one part of 9 months to January 20, $600.30", parts)
	}
	statute := money.USCents(10_000 * 100 * 2 * 9 / 300)
	if statute != 60_000 || parts[0].Charge()-statute != 30 {
		t.Errorf("the form's %v and the statute's %v differ by %v, want 30¢", parts[0].Charge(), statute, parts[0].Charge()-statute)
	}
}

// A month runs from the 21st to the 20th. $1,200 short in April (60% of
// $8,000 is $4,800, $1,200 a quarter):
//
//	paid May 20:  1 month,  1,200 × .00667     = 8.004  → $8.00
//	paid May 21:  2 months, 1,200 × .00667 × 2 = 16.008 → $16.01
//	paid June 21 or 22, the Sunday and Monday after June 20, a Saturday: the
//	  form counts it as paid June 20, so 2 months, $16.01
//	paid June 23: 3 months, 1,200 × .00667 × 3 = 24.012 → $24.01
func TestEstimatedMonthsRunFromThe21st(t *testing.T) {
	for _, tc := range []struct {
		paid   estimated.Date
		months int
		charge money.USCents
	}{
		{day(2026, 5, 20), 1, 800},
		{day(2026, 5, 21), 2, 1_601},
		{day(2026, 6, 21), 2, 1_601},
		{day(2026, 6, 22), 2, 1_601},
		{day(2026, 6, 23), 3, 2_401},
	} {
		h := household{tax: 8_000 * money.Dollar, payments: []estimated.Payment{pay(tc.paid, 1_200*money.Dollar)}}
		lines := must(rules(t).Penalty(h.facts(t), day(2026, 7, 31))).Value().Lines()
		if len(lines) == 0 || lines[0].Installment() != 1 {
			t.Fatalf("paid %v: no April line", tc.paid)
		}
		parts := lines[0].Parts()
		if len(parts) != 1 || parts[0].Months() != tc.months || parts[0].Charge() != tc.charge {
			t.Errorf("paid %v: %d parts, first %d months and %v; want %d months, %v", tc.paid, len(parts), parts[0].Months(), parts[0].Charge(), tc.months, tc.charge)
		}
	}
}

// The N-210 instructions' example: Mr. Aloha is $1,000 short in April, and
// June's installment is $2,500. His $2,500 on June 10 pays April's $1,000
// first, which the form figures "to June 10th (2 months)", and $1,500 toward
// June, which is left $1,000 short. 1,000 × .00667 × 2 = $13.34.
func TestEstimatedMonthsMatchMrAlohasExample(t *testing.T) {
	mustContain(t, "n210i-2025.txt",
		"For example, Mr. Aloha has an underpayment for the April 20th installment of $1,000. The June 20th installment requires a payment of $2,500. On June 10th, Mr. Aloha pays $2,500 to cover the June 20th installment. However, $1,000 of this payment will be considered to be for the April 20th installment. The penalty for the April 20th installment is figured to June 10th (2 months).",
		"The amount of the payment to be applied to the June 20th installment will be the remaining $1,500.")
	// 60% of $16,666.67 is $10,000.00, $2,500 a quarter.
	h := household{tax: 1_666_667, payments: []estimated.Payment{pay(dues[0], 1_500*money.Dollar), pay(day(2026, 6, 10), 2_500*money.Dollar)}}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	if sched[0].Shortfall() != 1_000*money.Dollar || sched[1].Shortfall() != 1_000*money.Dollar {
		t.Errorf("April short %v, June short %v; want $1,000 each", sched[0].Shortfall(), sched[1].Shortfall())
	}
	lines := must(rules(t).Penalty(h.facts(t), day(2026, 6, 30))).Value().Lines()
	parts := lines[0].Parts()
	if len(parts) != 1 || parts[0].Months() != 2 || parts[0].To() != day(2026, 6, 10) || parts[0].Charge() != 1_334 {
		t.Errorf("April: %+v; want one part of 2 months to June 10, $13.34", parts)
	}
}

// HRS § 235-97(g): withholding is paid in equal parts on the due dates,
// unless the filer shows when it was actually withheld. Last year's
// overpayment applied forward counts on April 20.
func TestEstimatedWithholding(t *testing.T) {
	// $8,000 withheld in December, against $6,000 a quarter.
	h := household{tax: 40_000 * money.Dollar, withheld: 8_000 * money.Dollar}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, in := range s {
		if want := money.USCents(i+1) * 2_000 * money.Dollar; in.CreditedBy() != want {
			t.Errorf("evenly: installment %d credited %v, want %v", i+1, in.CreditedBy(), want)
		}
	}
	h.asWithheld = true
	s = must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{0, 0, 0, 8_000 * money.Dollar} {
		if s[i].CreditedBy() != want {
			t.Errorf("as withheld: installment %d credited %v, want %v", i+1, s[i].CreditedBy(), want)
		}
	}
	h = household{tax: 40_000 * money.Dollar, payments: []estimated.Payment{must(estimated.NewPayment().Amount(6_000 * money.Dollar).FromPriorYear().Build())}}
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].Status() != estimated.StatusMet {
		t.Errorf("April with last year's overpayment is %v", s[0].Status())
	}
}

// N-210 Schedule A: 15%, 30%, 45% and 60% of the tax after credits on
// annualised income, less earlier installments, capped by the regular
// installment plus what was lowered earlier. $20,000 of tax, so a regular
// $3,000 a quarter; annualised tax of $4,000, $8,000, $12,000 and $20,000:
//
//	(a) 15% × 4,000 = 600;                cap 3,000;                    600
//	(b) 30% × 8,000 = 2,400, less 600 = 1,800; cap 3,000 + 2,400 = 5,400; 1,800
//	(c) 45% × 12,000 = 5,400, less 2,400 = 3,000; cap 3,000 + 3,600 = 6,600; 3,000
//	(d) 60% × 20,000 = 12,000, less 5,400 = 6,600; cap 3,000 + 3,600 = 6,600; 6,600
func TestEstimatedAnnualisedInstallments(t *testing.T) {
	h := household{tax: 20_000 * money.Dollar, periods: []money.USCents{4_000 * money.Dollar, 8_000 * money.Dollar, 12_000 * money.Dollar, 20_000 * money.Dollar}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{60_000, 240_000, 540_000, 1_200_000} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v by its date, want %v", i+1, s[i].NeededBy(), want)
		}
	}
	// $15,000 for the year instead: (d) is 60% × 15,000 = 9,000, less 5,400
	// = 3,600, under the cap of 6,600, so $9,000 is needed by January.
	h.periods[3] = 15_000 * money.Dollar
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[3].NeededBy() != 900_000 {
		t.Errorf("January needs %v by its date, want $9,000", s[3].NeededBy())
	}
	// $40,000 of annualised tax by March 31 is 15%, $6,000, capped at the
	// regular $3,000.
	h.periods[0] = 40_000 * money.Dollar
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].NeededBy() != 300_000 {
		t.Errorf("April needs %v, want the regular $3,000", s[0].NeededBy())
	}
	// The periods must be Schedule A's.
	f := must(estimated.NewFacts(hawaii, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(100).Build())).
		Periods(
			must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 6, 30)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).Build())
	if _, err := rules(t).Schedule(f); err == nil {
		t.Error("a period ending June 30 is accepted")
	}
}

// The exceptions: under $500 (TestEstimatedThresholdBoundary), the fourth
// installment for a return paid by January 31, no liability last year, the
// waivers, and farmers and nonresident aliens (refused).
func TestEstimatedExceptions(t *testing.T) {
	// No liability last year, as a full-year Hawaii resident: no return was
	// required, or one showed no tax. Exempt, and the zero is exact.
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	for name, pr := range map[string]*estimated.PriorReturn{"not required": &notRequired, "no tax": prior(0)} {
		h := household{tax: 40_000 * money.Dollar, prior: pr}
		res := must(rules(t).Requirement(h.facts(t)))
		if !res.Value().Exempt() || res.Value().Required() {
			t.Errorf("%s: exempt %v: %s", name, res.Value().Exempt(), res.Value().Why())
		}
		if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 20))); p.Value().Total() != 0 || p.Value().UpperBound() {
			t.Errorf("%s: penalty %v", name, p.Value().Total())
		}
	}
	// The same, not a Hawaii resident all year: the form's exception does
	// not apply, and the adopted federal text is reported as the filer's to
	// claim.
	away := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(false).Build())
	res := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, prior: &away}.facts(t), day(2027, 4, 20)))
	if res.Value().Total() != 128_064 {
		t.Errorf("a part-year resident: penalty %v, want $1,280.64", res.Value().Total())
	}
	if !strings.Contains(applied(res.Applied()), "U.S. citizen or resident throughout 2025, and if you were, the exception is yours to claim") {
		t.Errorf("the statute's exception is not reported: %s", applied(res.Applied()))
	}
	// Residence not said: asked, and not applied.
	unsaid := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	res = must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, prior: &unsaid}.facts(t), day(2027, 4, 20)))
	if !has(res.Required(), working.PositionResidency) || res.Value().Total() == 0 {
		t.Errorf("residence unsaid: asked %v, penalty %v", has(res.Required(), working.PositionResidency), res.Value().Total())
	}

	// A waiver asked for is not subtracted.
	casualty := estimated.WaiverCasualty
	res = must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, waiver: &casualty}.facts(t), day(2027, 4, 20)))
	if res.Value().Total() != 128_064 || res.Value().Waiver() != estimated.WaiverCasualty || has(res.Required(), working.PositionPenaltyWaiver) {
		t.Errorf("with a casualty waiver asked: %v, %v", res.Value().Total(), res.Value().Waiver())
	}

	// Filed and paid by January 31: January is excused, and the earlier
	// shortfalls stop on January 20. $6,000 each for 9, 7 and 4 months:
	// 360.18 + 280.14 + 160.08 = $800.40.
	p := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, returnFiled: day(2027, 1, 29)}.facts(t), day(2027, 4, 20))).Value()
	if p.AsOf() != day(2027, 1, 20) || p.Total() != 80_040 || len(p.Lines()) != 3 {
		t.Errorf("filed January 29: to %v, %v over %d lines; want January 20, $800.40 over 3", p.AsOf(), p.Total(), len(p.Lines()))
	}
	// Filed and paid March 1: every shortfall is paid that day. 11, 9, 6 and
	// 2 months: 440.22 + 360.18 + 240.12 + 80.04 = $1,120.56.
	p = must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, returnFiled: day(2027, 3, 1)}.facts(t), day(2027, 4, 20))).Value()
	if p.AsOf() != day(2027, 3, 1) || p.Total() != 112_056 {
		t.Errorf("filed March 1: to %v, %v; want March 1 and $1,120.56", p.AsOf(), p.Total())
	}

	// A late start: the rule's schedule is reported and the form's quarters
	// are charged.
	late := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, firstMet: day(2026, 7, 1)}.facts(t), day(2027, 4, 20)))
	if late.Value().Total() != 128_064 || !strings.Contains(applied(late.Applied()), "HAR § 18-235-97(e)(2)(A)") {
		t.Errorf("a late start: penalty %v; working %s", late.Value().Total(), applied(late.Applied()))
	}
	early := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, firstMet: day(2026, 3, 31)}.facts(t), day(2027, 4, 20)))
	if strings.Contains(applied(early.Applied()), "HAR § 18-235-97(e)(2)(A)") {
		t.Error("a start on March 31 is reported as late")
	}
}

// Each divergence is reported with both readings and both sources. The
// figures follow the forms: TestEstimatedChargeUsesThePrintedFactor pins the
// factor, TestEstimatedExceptions the residence test, and the late start in
// TestEstimatedExceptions the quarters.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar}.facts(t)))
	want := map[string][4]string{
		"No liability last year":                 {"full-year Hawaii resident", "Part I box A", "U.S. citizen or resident", "6654(e)(2)"},
		"The monthly rate":                       {".00667", "line 20", "two-thirds of one per cent", "HRS § 235-97(f)"},
		"A requirement first met after March 31": {"a quarter of the required annual payment", "line 9", "three equal parts from June 20", "§ 18-235-97(e)(2)(A)"},
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
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.FormSource(), w[1]) ||
			!strings.Contains(d.Statute(), w[2]) || !strings.Contains(d.StatuteSource(), w[3]) {
			t.Errorf("%q does not pin both readings: form %q (%s), statute %q (%s)", d.Rule(), d.Form(), d.FormSource(), d.Statute(), d.StatuteSource())
		}
		if d.Uses() != working.ReadingForm {
			t.Errorf("%q uses %v, want the form", d.Rule(), d.Uses())
		}
		// $40,000 unpaid: the rate changes the charge, and neither last
		// year's residence nor a late start is in the facts.
		if got, want := d.AffectsHousehold(), d.Rule() == "The monthly rate"; got != want {
			t.Errorf("%q affects the household %v, want %v", d.Rule(), got, want)
		}
	}
	// No tax last year and not a resident all of it: the residence test is
	// what decides the exception.
	away := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(false).Build())
	for _, d := range must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: &away}.facts(t))).Divergences() {
		if d.Rule() == "No liability last year" && !d.AffectsHousehold() {
			t.Errorf("%q does not affect a part-year resident with no tax last year", d.Rule())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := hawaiitaxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(40_000 * money.Dollar).Build())
	base := func() *estimated.FactsBuilder {
		return estimated.NewFacts(hawaii, 2026).FilingStatus(filing.Single).ThisYear(this)
	}
	priorFarmer := must(hawaiitaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(true).Build())
	alien := must(hawaiitaxcalc.NewEstimatedFacts().NonresidentAlienWithoutWages(true).Build())
	for name, f := range map[string]estimated.Facts{
		"a farmer":                    must(base().FarmShare(7000).Build()),
		"a fisherman":                 must(base().FishShare(7000).Build()),
		"farming and fishing":         must(base().FarmShare(3400).FishShare(3300).Build()),
		"a farmer last year":          must(base().State(priorFarmer).Build()),
		"a nonresident alien no wage": must(base().State(alien).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	if _, err := rules(t).Requirement(must(base().FarmShare(6666).Build())); err != nil {
		t.Errorf("66.66%% from farming: %v", err)
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(hawaii, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(base().State("not Hawaii's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(must(base().Build()), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}
	// Neither Hawaii fact said: both asked for.
	res := must(rules(t).Requirement(must(base().Build())))
	asked := asks(res.Required(), working.PositionJurisdictionFact)
	if !strings.Contains(asked, "farming or fishing") || !strings.Contains(asked, "nonresident alien") {
		t.Errorf("the Hawaii facts are not asked about: %s", asked)
	}
}

// The residence test of the no-liability exception is in its divergence,
// pinned by TestEstimatedDivergencesPinBothReadings.
func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"$500", "60%", "100% of last year's", "no 110%", "on the 20th", "June 22", "September 21",
		"two-thirds of one percent", "month or part of a month", "21st to the 20th", ".00667", "April 20",
		"January 31", "January 20", "15%, 30%, 45% and 60%", "retired after 62"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if e.VersusFederal() == "" || len(e.Sources()) == 0 {
		t.Error("the explanation has no comparison with federal or no sources")
	}
}

// Without this year's tax the target is last year's tax alone, and the
// penalty against it is an upper bound. $8,000 last year, $2,000 a quarter,
// unpaid to April 20, 2027: 2,000 × .00667 × (12 + 10 + 7 + 3) = $426.88.
func TestEstimatedWithoutThisYearsTaxUsesLastYearsAlone(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 8_000*money.Dollar || r.Exempt() {
		t.Errorf("required %v, target %v; want $8,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if c, ok := r.Chosen(); !ok || c.Basis() != estimated.BasisPriorYearTax {
		t.Errorf("chose %v", c.Basis())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{200_000, 400_000, 600_000, 800_000} {
		if got := sched.Value()[i].NeededBy(); got != want {
			t.Errorf("installment %d needs %v, want %v", i+1, got, want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 20)))
	if !pen.Value().UpperBound() || pen.Value().Total() != 42_688 {
		t.Errorf("unpaid: penalty %v, upper bound %v; want $426.88, an upper bound", pen.Value().Total(), pen.Value().UpperBound())
	}
	paid := h
	paid.payments = []estimated.Payment{pay(dues[0], 2_000*money.Dollar), pay(dues[1], 2_000*money.Dollar), pay(dues[2], 2_000*money.Dollar), pay(dues[3], 2_000*money.Dollar)}
	if pen := must(rules(t).Penalty(paid.facts(t), day(2027, 4, 20))); pen.Value().Total() != 0 {
		t.Errorf("paid to last year's tax: penalty %v", pen.Value().Total())
	}

	// No liability last year still exempts, exactly.
	none := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	free := household{noTax: true, prior: &none}
	if res := must(rules(t).Requirement(free.facts(t))); !res.Value().Exempt() {
		t.Errorf("no liability last year, no tax this year: %s", res.Value().Why())
	}
	if pen := must(rules(t).Penalty(free.facts(t), day(2027, 4, 20))); pen.Value().Total() != 0 || pen.Value().UpperBound() {
		t.Errorf("no liability last year: penalty %v, upper bound %v; want an exact zero", pen.Value().Total(), pen.Value().UpperBound())
	}

	// With no return last year either, there is no target.
	nothing := household{noTax: true}
	res = must(rules(t).Requirement(nothing.facts(t)))
	if res.Value().TargetKnown() || res.Value().Required() {
		t.Errorf("no target expected: %s", res.Value().Why())
	}
	if _, err := rules(t).Schedule(nothing.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(nothing.facts(t), day(2027, 4, 20)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// Hawaii adopts the federal waivers, and Form N-210's instructions state
// them: retirement after 62 or disability in 2024 or 2025 for the 2025 form,
// so 2025 or 2026 for 2026, and a casualty, disaster or other unusual
// circumstance. The penalty is the same $1,280.64 in each case; only the
// outcome differs.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "n210i-2025.txt",
		"You retired after reaching age 62 or became disabled in 2024 or 2025",
		"You didn’t make a payment because of a casualty, disaster, or other unusual circumstance and it would be inequitable to impose the penalty.")
	for _, tc := range []struct {
		name string
		pos  estimated.Positions
		want string // eligible, refused or undecided
		says string
	}{
		{"retired in 2026 at 64", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build()), "eligible", "after reaching 62"},
		{"retired in 2025", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2025, 6, 1)).Born(day(1960, 3, 1)).Build()), "eligible", "retired in 2025"},
		{"retired in 2020", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, 3, 1)).Build()), "refused", "2025 or 2026"},
		{"retired at 60", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1966, 3, 1)).Build()), "refused", "reached 62"},
		{"disabled in 2025", must(estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2025, 9, 1)).Build()), "eligible", "became disabled in 2025"},
		{"a disaster", must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()), "eligible", "Hawaii Department of Taxation"},
		{"retirement date not given", must(estimated.NewPositions().Waiver(estimated.WaiverRetiredOrDisabled).Build()), "undecided", "date you retired"},
	} {
		pos := tc.pos
		p := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, positions: &pos}.facts(t), day(2027, 4, 20))).Value()
		o, ok := p.WaiverOutcome()
		got := map[bool]string{true: "eligible"}[o.Eligible()] + map[bool]string{true: "refused"}[o.Refused()] + map[bool]string{true: "undecided"}[o.Undecided()]
		switch {
		case p.Total() != 128_064:
			t.Errorf("%s: penalty %v, want $1,280.64 before any waiver", tc.name, p.Total())
		case !ok || got != tc.want || !strings.Contains(o.Reason(), tc.says):
			t.Errorf("%s: %s %q, want %s saying %q", tc.name, got, o.Reason(), tc.want, tc.says)
		case o.Eligible() != (p.IfWaived() == 0):
			t.Errorf("%s: if waived %v", tc.name, p.IfWaived())
		}
	}
}

// Under the $500 exception the target is $0, so last year's return isn't
// asked for as a way to change it. Above it, it is.
func TestEstimatedPriorReturnAskedOnlyWhenItCanSetTheTarget(t *testing.T) {
	under := must(rules(t).Requirement(household{tax: 400 * money.Dollar}.facts(t)))
	if !under.Value().Exempt() || has(under.Required(), working.PositionPriorYearReturn) {
		t.Errorf("under $500: exempt %v, asks %q", under.Value().Exempt(), asks(under.Required(), working.PositionPriorYearReturn))
	}
	over := must(rules(t).Requirement(household{tax: 4_000 * money.Dollar}.facts(t)))
	if !strings.Contains(asks(over.Required(), working.PositionPriorYearReturn), "your target could be") {
		t.Errorf("over $500: asks %q", asks(over.Required(), working.PositionPriorYearReturn))
	}
}
