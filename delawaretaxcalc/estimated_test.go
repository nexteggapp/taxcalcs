// Delaware's estimated tax rules for 2026, checked against the Division of
// Revenue's own forms and the Delaware Code.
//
// Delaware publishes no exhaustive oracle and no worked example with a stated
// answer. The PIT-UND instructions carry one illustration under line 43, $300
// short with $200 paid June 1 and $200 September 15, but its "48 days" and
// "93 days" match neither the form's dates nor the due dates, so it is not
// used. Every threshold, percentage, date and factor is compared against its
// extract in testdata/. The penalty arithmetic, the boundaries and the
// exceptions are hand-written cases, worked by hand in their comments, so the
// green tick here is worth the care of those comments and no more.
package delawaretaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/delawaretaxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const delaware = estimated.Jurisdiction("DE")

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
	return must(delawaretaxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

func because(rs []working.Required, k working.PositionKind) string {
	var out []string
	for _, r := range rs {
		if r.Position() == k && r.Asked() {
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

var dues = []estimated.Date{day(2026, 4, 30), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

func quarterly(a ...money.USCents) []estimated.Payment {
	var out []estimated.Payment
	for i, x := range a {
		out = append(out, pay(dues[i], x))
	}
	return out
}

type withheld struct {
	on     estimated.Date
	amount money.USCents
}

type household struct {
	status      filing.Status
	tax         money.USCents // PIT-RES line 33 less line 34
	noTax       bool
	withheld    []withheld
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	residence   []estimated.ResidencePeriod // full-year Delaware when nil
	noResidence bool
	returnFiled estimated.Date
	firstMet    estimated.Date
	annualised  []money.USCents // PIT-UND line 18 for each period
	asWithheld  bool
	waiver      *estimated.WaiverKind
}

func resident(state string, from, to estimated.Date) estimated.ResidencePeriod {
	return must(estimated.NewResidencePeriod(state).From(from).To(to).Build())
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
	b := estimated.NewFacts(delaware, 2026).FilingStatus(status).ThisYear(must(fb.Build())).
		Paid(h.payments...).ReturnFiled(h.returnFiled).RequirementFirstMet(h.firstMet)
	for _, w := range h.withheld {
		b.Withheld(must(estimated.NewWithheld().On(w.on).Amount(w.amount).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	switch {
	case h.residence != nil:
		b.Residence(h.residence...)
	case !h.noResidence:
		b.Residence(resident("DE", day(2026, 1, 1), day(2026, 12, 31)))
	}
	eb := estimated.NewElections()
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
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
	return must(b.Build())
}

// prior is a filed twelve-month Delaware return with its tax and last year's
// Delaware AGI.
func prior(tax, delawareAGI money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).StateAGI(delawareAGI).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).Build())
	return &p
}

func priorWith(f estimated.Figures, months int) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Months(months).Figures(f).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "pit-est-2026.txt",
		"Revised 11/10/25",
		"if the tax due in excess of withholding can reasonably be expected to exceed $800.00.",
		"is at least 90 percent (66 2/3% for farmers and fishermen) of the amount due on the income tax return for the taxable year, or (b) 100% of the tax shown on the prior year’s return, (110% if the federal adjusted gross income for the previous tax year is in excess of $150,000 ($75,000 if married filing separate)).",
		"in equal installments on or before April 30, June 15, September 15, and January 15 of the following year.",
		"June 15, if the change occurs after April 1 and before June 2; September 15 if the change occurs after June 1 and before September 2; January 15 of the following year if the change occurs after September 1.",
		"in equal installments on the remaining payment dates.",
		"Payment of estimated tax is not required if there was no tax liability for the preceding year, provided such year was a twelve-month period.",
		"A penalty of 1 1/2% per month or fraction thereof",
		"If at least two-thirds of your gross income is derived from farming or fishing",
		"file a return and pay the tax in full on or before March 1 of the succeeding year",
		"you must apply the full amount of credit against first and succeeding installments until fully used.",
		"(filing status 3)",
		"The underpayment penalty may be waived if the underpayment is due to casualty, disaster, or other unusual circumstances.",
	)
	mustContain(t, "pit-est-2026-voucher.txt", "TAX YEAR 2026 QUARTER 1 DUE BY Apr 30, 2026")
	mustContain(t, "pit-res-2025-estimated.txt",
		"1/1/2026 through 3/31/2026 April 30, 2026 4/1/2026 through 5/31/2026 June 15, 2026 6/1/2026 through 8/31/2026 September 15, 2026 9/1/2026 through 12/31/2026 January 15, 2027",
		"can reasonably be expected to exceed $800",
		"If your 2025 Delaware AGI exceeded $150,000, or if you are filing status 3 and your 2025 Delaware AGI exceeded $75,000, line 2 must at least equal 110% of your 2025 tax liability.",
		"You will not owe the penalty if your 2025 tax return was for a period of 12 full months AND ONE of the following applies",
	)
	mustContain(t, "pit-und-2025.txt",
		"Revision 20250724",
		"A. Enter 90% of 2025 Delaware tax liability",
		"B. Enter 100% or 110% of 2024 Delaware tax liability",
		"E. Subtract Line “D” from Line “C”. If $800 or less, stop here. You do not owe the penalty.",
		"J. Multiply Line “I” by 12% (times 0.12)",
		"1/1/25 - 4/30/25 5/1/25 - 6/15/25 6/16/25 - 9/15/25 9/16/25 - 1/15/26",
		"39. 5/1/25 6/16/25 9/15/25 1/15/26",
		"41. Multiply Line 40 by .05% (times .0005)",
		"1/1/25 - 3/31/25 1/1/25 - 5/31/25 1/1/25 - 8/31/25 1/1/25 - 12/31/25",
		"19. MULTIPLIER 19. .225 .450 .675 .900",
		"23. Enter 1/4 of the total from Part 1, Line “C”, in each column",
		"27. Enter the smaller of Line 22 or Line 25 here and on Line 28",
	)
	mustContain(t, "pit-und-2025-instructions.txt",
		"Revised 11/10/25",
		"your 2024 Delaware AGI is more than $150,000 or $75,000 if Married",
		"Filing Separate Returns, Filing Status 3).",
		"a farmer or fisherman and your Adjusted Gross Income (AGI) on your",
		"E. Subtract Line D from Line C. Note: If this amount is $800 or less, stop here. You do not owe the penalty.",
		"You had no Delaware tax liability in the previous year",
		"You were a Part-Year resident during 2025",
		"a. You made no estimated payments; or b. You paid estimated tax in four equal amounts on or before the due dates (June 15th, July 15th, September 15th, and January 15th).",
		"You may not use the Short Method if any capital gain tax payments were made on your behalf.",
		"J. Multiply Line I by 0.12.",
		"K. If the amount on Line l was paid on or after April 30, 2026 enter zero (0).",
		"date Line l was paid before April 30, 2026 times 0.0005 times the amount",
		"2. In 2024 or 2025 you retired after age 62 or became disabled.",
		"Tax Conferee, Delaware Division of Revenue, P. O. Box 8714",
		"You will be given a chance to request a",
		"Do not submit a Federal Form 2210 instead of a Delaware Form PIT-UND.",
		"The same amount should be entered in each column.",
		"In the fourth column enter the payments made for the period September 16th through January 16, 2026.",
		"In Column 1, enter the withholding for the period January 1st through March 31st. In the second column enter the withholding for the period April 1st through May 31st. In the third column enter the withholding for the period June 1st through August 31st. In the fourth column enter the withholding for the period September 1st through December 31st.",
		"Apply S Corp payment and Refundable Business Credits evenly to each quarter",
		"Compute the number of days between the due date on Line 39 and the date the amount on Line 37 is paid or April 30, 2026 whichever is earlier.",
		"Multiply Line 40 by 0.0005. Carry out the computation to four decimal places",
	)
	mustContain(t, "delcode-30-1168-1170.txt",
		"shall be filed on or before the thirtieth day of the fourth month following the close of the taxpayer’s taxable year.",
		"shall make a declaration of estimated tax for the taxable year in such form as the Director of Revenue may prescribe, if the estimated tax can reasonably be expected to exceed $800.",
		"less the amount which the individual or trust estimates to be the sum of any credits allowable for tax withheld.",
		"on or before the dates prescribed by the laws of the United States for filing declarations and payment of estimated federal income tax, except that the State Tax Commissioner may establish other dates for filing declarations and payment of estimated tax.",
	)
	mustContain(t, "delcode-30-533-535.txt",
		"fails to file a declaration of estimated tax, or fails to pay all or any part of an installment of tax imposed under Chapter 11 of this title",
		"there shall be added to the tax an amount equal to 1 1/2% per month, or fraction thereof, of the amount of such underpayment for the period of the underpayment. The Director shall determine the amount of an underpayment of estimated tax by an individual in a manner consistent with the internal revenue laws of the United States.",
		"nor shall this section apply to any failure to pay estimated income tax under § 1170",
		"(e) This section shall not apply to any failure to file a declaration of estimated tax or to pay any estimated tax.",
	)
	mustContain(t, "delcode-30-551.txt",
		"falls on Saturday, Sunday or a legal holiday in this State, the performance of such act shall be considered timely if it is performed on the next succeeding day which is not a Saturday, Sunday or a legal holiday in this State.")
}

// The Delaware Code as published lists no 2025 or 2026 session law in the
// history of §§ 533 to 535, 551 or 1168 to 1170; § 535 was last amended by
// 84 Del. Laws c. 301, in subsection (e). So no form here predates a change
// in the law.
func TestEstimatedStatuteHasNoRecentAmendment(t *testing.T) {
	mustContain(t, "delcode-30-533-535.txt", "84 Del. Laws, c. 301, § 16;")
	for _, name := range []string{"delcode-30-533-535.txt", "delcode-30-551.txt", "delcode-30-1168-1170.txt"} {
		if text := extract(t, name); strings.Contains(text, "85 Del. Laws") {
			t.Errorf("%s lists an 85 Del. Laws amendment", name)
		}
	}
}

// The 2026 dates are the ones PIT-EST, its voucher and the PIT-RES
// instructions print. None falls on a weekend, so none moves, and neither
// does April 30, 2027, where the penalty stops.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar}.facts(t)))
	if len(res.Value()) != 4 {
		t.Fatalf("%d installments, want 4", len(res.Value()))
	}
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
	}
	if strings.Contains(applied(res.Applied()), "§ 551(d)") {
		t.Error("a 2026 due date was moved")
	}
	if wd := day(2027, 4, 30).Weekday(); wd != time.Friday {
		t.Errorf("April 30, 2027 is a %v", wd)
	}
}

// PIT-UND line E: no penalty when the required annual payment less
// withholding is $800 or less. $10,000 of tax and no prior return gives a
// required payment of $9,000; $8,200 withheld leaves $800.00, exempt, and
// $8,199.99 leaves $800.01, which is not.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{820_000, true},
		{819_999, false},
	} {
		h := household{tax: 10_000 * money.Dollar, withheld: []withheld{{day(2026, 12, 15), tc.withheld}}}
		res := must(rules(t).Requirement(h.facts(t)))
		if got := res.Value().Exempt(); got != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, got, tc.exempt, res.Value().Why())
		}
		if got := res.Value().Required(); got == tc.exempt {
			t.Errorf("withheld %v: required %v", tc.withheld, got)
		}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30)))
		if got := p.Value().Total() == 0; got != tc.exempt {
			t.Errorf("withheld %v: penalty %v", tc.withheld, p.Value().Total())
		}
	}
	// The test is on the required payment, not this year's tax: $10,000 of
	// tax with nothing withheld, against last year's $800, is exempt.
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(800*money.Dollar, 50_000*money.Dollar)}.facts(t)))
	if !res.Value().Exempt() {
		t.Errorf("a required payment of $800 is not exempt: %s", res.Value().Why())
	}
	// The exception is the chosen option, at $0, and the other options keep
	// their amounts.
	if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Required payment less withholding of $800 or less" {
		t.Errorf("chosen %v %v %q", c.Basis(), c.Amount(), c.Label())
	}
	if o := res.Value().Options()[1]; o.Basis() != estimated.BasisPriorYearTax || o.Amount() != 800*money.Dollar {
		t.Errorf("last year's option is %v at %v, want $800", o.Basis(), o.Amount())
	}
	// The statute's test is on this year's tax, $10,000 with nothing
	// withheld, so it would require payments.
	for _, d := range res.Divergences() {
		if d.Rule() == "The $800 test" && !d.AffectsHousehold() {
			t.Error("the $800 test divergence does not affect a household it decides")
		}
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(80_001, 50_000*money.Dollar)}.facts(t)))
	if res.Value().Exempt() || res.Value().Target() != 80_001 {
		t.Errorf("a required payment of $800.01: exempt %v, target %v", res.Value().Exempt(), res.Value().Target())
	}
}

// 110% when last year's AGI exceeds $150,000, or $75,000 for filing status
// 3. Delaware AGI is the default, as PIT-UND says.
func TestEstimatedPriorYearStepBoundary(t *testing.T) {
	for _, tc := range []struct {
		status filing.Status
		agi    money.USCents
		want   money.Rate
	}{
		{filing.Single, 15_000_000, money.Percent(100)},
		{filing.Single, 15_000_001, money.Percent(110)},
		{filing.MarriedFilingJointly, 15_000_000, money.Percent(100)},
		{filing.MarriedFilingJointly, 15_000_001, money.Percent(110)},
		{filing.MarriedFilingSeparately, 7_500_000, money.Percent(100)},
		{filing.MarriedFilingSeparately, 7_500_001, money.Percent(110)},
		{filing.MarriedFilingJointly, 7_500_001, money.Percent(100)},
	} {
		res := must(rules(t).Requirement(household{status: tc.status, tax: 100_000 * money.Dollar, prior: prior(10_000*money.Dollar, tc.agi)}.facts(t)))
		o := res.Value().Options()[1]
		if o.Percent() != tc.want || o.Amount() != tc.want.Of(10_000*money.Dollar) {
			t.Errorf("%v, AGI %v: %v of last year's tax, %v; want %v", tc.status, tc.agi, o.Percent(), o.Amount(), tc.want)
		}
	}

	// Which AGI is open: asked whenever the two could disagree.
	both := func(state, federal money.USCents) *estimated.PriorReturn {
		return priorWith(must(estimated.NewFigures().Tax(10_000*money.Dollar).StateAGI(state).FederalAGI(federal).Build()), 12)
	}
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		want  money.Rate
		asked bool
		open  bool
	}{
		"both agree":        {both(140_000*money.Dollar, 145_000*money.Dollar), money.Percent(100), false, false},
		"they disagree":     {both(140_000*money.Dollar, 160_000*money.Dollar), money.Percent(100), false, true},
		"Delaware only":     {prior(10_000*money.Dollar, 140_000*money.Dollar), money.Percent(100), true, false},
		"federal only":      {priorWith(must(estimated.NewFigures().Tax(10_000*money.Dollar).FederalAGI(160_000*money.Dollar).Build()), 12), money.Percent(110), true, false},
		"neither, assumed":  {priorWith(must(estimated.NewFigures().Tax(10_000*money.Dollar).Build()), 12), money.Percent(110), true, false},
		"federal only, low": {priorWith(must(estimated.NewFigures().Tax(10_000*money.Dollar).FederalAGI(100_000*money.Dollar).Build()), 12), money.Percent(100), true, false},
	} {
		res := must(rules(t).Requirement(household{tax: 100_000 * money.Dollar, prior: tc.prior}.facts(t)))
		if got := res.Value().Options()[1].Percent(); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
		if got := has(res.Required(), working.PositionAdjustedGrossIncome); got != tc.asked {
			t.Errorf("%s: AGI asked %v, want %v", name, got, tc.asked)
		}
		// Which AGI applies is the forms' disagreement, not the person's to answer.
		if got := strings.Contains(because(res.Required(), working.PositionUnsettledRule), "prior-year percentage follows"); got != tc.open {
			t.Errorf("%s: which AGI reported open %v, want %v", name, got, tc.open)
		}
	}
}

// The target is the smaller of 90% of this year's tax and the prior-year
// test, which needs a filed Delaware return showing a tax. No Delaware
// source limits it to a twelve-month year.
func TestEstimatedTargetOptions(t *testing.T) {
	// 90% of $40,000 is $36,000; 100% of $20,000 is $20,000.
	res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: prior(20_000*money.Dollar, 100_000*money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || res.Value().Target() != 20_000*money.Dollar {
		t.Errorf("chose %v at %v, want last year's $20,000", c.Basis(), res.Value().Target())
	}
	// 100% of $37,000 is more than $36,000.
	res = must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: prior(37_000*money.Dollar, 100_000*money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 36_000*money.Dollar {
		t.Errorf("chose %v at %v, want 90%% of this year's, $36,000", c.Basis(), res.Value().Target())
	}
	if o := res.Value().Options()[0]; o.Percent() != money.Percent(90) {
		t.Errorf("this year's option is %v, want 90%%", o.Percent())
	}

	short := priorWith(must(estimated.NewFigures().Tax(5_000*money.Dollar).StateAGI(50_000*money.Dollar).Build()), 6)
	res = must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: short}.facts(t)))
	if o := res.Value().Options()[1]; !o.Available() || res.Value().Target() != 5_000*money.Dollar {
		t.Errorf("a six-month return: available %v, target %v; want $5,000", o.Available(), res.Value().Target())
	}

	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   bool
	}{
		"no return on record": {nil, true},
		"not filed":           {&notFiled, false},
	} {
		res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: tc.prior}.facts(t)))
		if res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
		if res.Value().Target() != 36_000*money.Dollar {
			t.Errorf("%s: target %v, want $36,000", name, res.Value().Target())
		}
		if has(res.Required(), working.PositionPriorYearReturn) != tc.ask {
			t.Errorf("%s: asked for last year's return %v, want %v", name, !tc.ask, tc.ask)
		}
	}
}

// $40,000 of tax, nothing withheld or paid, no return last year: the
// required payment is 90%, $36,000, and each installment $9,000. Each is
// charged 0.0005 a day from line 39's date to April 30, 2027:
//
//	May 1:    364 days × 0.0005 = 0.1820 × 9,000 = $1,638.00
//	June 15:  319 days × 0.0005 = 0.1595 × 9,000 = $1,435.50
//	Sept 15:  227 days × 0.0005 = 0.1135 × 9,000 = $1,021.50
//	Jan 15:   105 days × 0.0005 = 0.0525 × 9,000 =   $472.50
//
// The total is $4,567.50. Delaware's rate is fixed by § 535(b) and by the
// form's factor, and no published change falls inside 2026 or 2027, so the
// first three shortfalls run past December 31 as one span each, as line 40
// counts them, and no day is without a rate.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), day(2027, 4, 30)))
	p := res.Value()
	want := []struct {
		from   estimated.Date
		days   int
		charge money.USCents
	}{
		{day(2026, 5, 1), 364, 163_800},
		{day(2026, 6, 15), 319, 143_550},
		{day(2026, 9, 15), 227, 102_150},
		{day(2027, 1, 15), 105, 47_250},
	}
	lines := p.Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 4", len(lines))
	}
	for i, l := range lines {
		parts := l.Parts()
		if l.Shortfall() != 9_000*money.Dollar || len(parts) != 1 {
			t.Errorf("line %d is on %v in %d parts, want $9,000 in one", i+1, l.Shortfall(), len(parts))
			continue
		}
		w := want[i]
		if parts[0].From() != w.from || parts[0].Days() != w.days || parts[0].Charge() != w.charge || parts[0].Rate() != money.Percent(18) {
			t.Errorf("line %d: from %v, %d days at %v, %v; want from %v, %d days, %v", i+1, parts[0].From(), parts[0].Days(), parts[0].Rate(), parts[0].Charge(), w.from, w.days, w.charge)
		}
	}
	if p.Total() != 456_750 {
		t.Errorf("penalty %v, want $4,567.50", p.Total())
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("a gap in the rate is reported, but the rate is fixed")
	}
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waivers are not asked about")
	}
	// It stops on April 30, 2027.
	later := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), day(2027, 6, 30)))
	if later.Value().AsOf() != day(2027, 4, 30) || later.Value().Total() != 456_750 {
		t.Errorf("to June 30: figured to %v at %v; want April 30, 2027 and $4,567.50", later.Value().AsOf(), later.Value().Total())
	}

	// A payment pays the oldest shortfall first. $9,000 on May 15 pays the
	// first: 14 days from May 1, 9,000 × 0.0070 = $63.00.
	h := household{tax: 40_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 5, 15), 9_000*money.Dollar)}}
	p = must(rules(t).Penalty(h.facts(t), day(2026, 6, 1))).Value()
	if l := p.Lines(); len(l) == 0 || l[0].Installment() != 1 || l[0].Total() != 6_300 || p.Total() != 6_300 {
		t.Errorf("penalty %v to June 1, want $63.00, all on the first", p.Total())
	}
	// The penalty stops on the day the return is filed and paid.
	p = must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, returnFiled: day(2027, 3, 1)}.facts(t), day(2027, 4, 30))).Value()
	if p.AsOf() != day(2027, 3, 1) {
		t.Errorf("figured to %v, want the day the return was paid", p.AsOf())
	}
}

// PIT-UND line 30 counts withholding in the period it was withheld: January
// to March toward April 30, April and May toward June 15, June to August
// toward September 15, September to December toward January 15. $1,000 on
// each side of each period end shows where it lands.
func TestEstimatedWithholdingCountsInItsPeriod(t *testing.T) {
	for _, tc := range []struct {
		on    estimated.Date
		first int // the first installment it is credited by
	}{
		{day(2026, 3, 31), 0}, {day(2026, 4, 1), 1},
		{day(2026, 5, 31), 1}, {day(2026, 6, 1), 2},
		{day(2026, 8, 31), 2}, {day(2026, 9, 1), 3},
		{day(2026, 12, 31), 3},
	} {
		h := household{tax: 40_000 * money.Dollar, withheld: []withheld{{tc.on, 1_000 * money.Dollar}}}
		s := must(rules(t).Schedule(h.facts(t))).Value()
		for i, in := range s {
			want := money.USCents(0)
			if i >= tc.first {
				want = 1_000 * money.Dollar
			}
			if in.CreditedBy() != want {
				t.Errorf("withheld %v: installment %d credited %v, want %v", tc.on, i+1, in.CreditedBy(), want)
			}
		}
	}
	// There is no election; asking for one changes nothing.
	h := household{tax: 40_000 * money.Dollar, withheld: []withheld{{day(2026, 4, 1), 1_000 * money.Dollar}}, asWithheld: true}
	res := must(rules(t).Schedule(h.facts(t)))
	if res.Value()[0].CreditedBy() != 0 || !strings.Contains(applied(res.Applied()), "no election about withholding") {
		t.Errorf("with the election: %v credited by April 30", res.Value()[0].CreditedBy())
	}
	// Last year's overpayment counts toward the first installment.
	h = household{tax: 40_000 * money.Dollar, payments: []estimated.Payment{must(estimated.NewPayment().Amount(9_000 * money.Dollar).FromPriorYear().Build())}}
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].Status() != estimated.StatusMet {
		t.Errorf("April with last year's overpayment is %v", s[0].Status())
	}
}

// PIT-UND Part 3 with line 18 of $8,000, $20,000, $30,000 and $40,000, and
// a required payment of $36,000, $9,000 a quarter:
//
//	line 20: 22.5% → $1,800; 45% → $9,000; 67.5% → $20,250; 90% → $36,000
//	col 1: 22 = 1,800;  25 = 9,000;  26 = 7,200; 27 = 1,800
//	col 2: 22 = 7,200;  25 = 16,200; 26 = 9,000; 27 = 7,200
//	col 3: 22 = 11,250; 25 = 18,000; 26 = 6,750; 27 = 11,250
//	col 4: 22 = 15,750; 25 = 15,750; 26 = 0;     27 = 15,750
//
// so $1,800, $9,000, $20,250 and $36,000 are needed by the four dates.
func TestEstimatedAnnualisedInstallments(t *testing.T) {
	h := household{tax: 40_000 * money.Dollar, annualised: []money.USCents{8_000 * money.Dollar, 20_000 * money.Dollar, 30_000 * money.Dollar, 40_000 * money.Dollar}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{180_000, 900_000, 2_025_000, 3_600_000} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v by its date, want %v", i+1, s[i].NeededBy(), want)
		}
	}
	// With line 18 for the full year at $32,000, below this year's tax as the
	// arithmetic allows, column 4's 90% binds: line 22 is 28,800 − 20,250 =
	// $8,550, under line 25's $15,750, so $28,800 is needed by January 15.
	h.annualised[3] = 32_000 * money.Dollar
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[3].NeededBy() != 2_880_000 {
		t.Errorf("installment 4 needs %v by its date, want $28,800", s[3].NeededBy())
	}
	// The periods must be the form's.
	f := must(estimated.NewFacts(delaware, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(100).Build())).
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

// The exceptions: the $800 test (TestEstimatedThresholdBoundary), no
// liability last year, a part-year resident, and farmers (refused). The
// waivers are asked for after assessment, so the penalty is shown before
// them.
func TestEstimatedExceptions(t *testing.T) {
	zero := priorWith(must(estimated.NewFigures().Tax(0).Build()), 12)
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	for name, p := range map[string]*estimated.PriorReturn{"no tax shown": zero, "no return required": &notRequired} {
		h := household{tax: 40_000 * money.Dollar, prior: p}
		res := must(rules(t).Requirement(h.facts(t)))
		if !res.Value().Exempt() {
			t.Errorf("%s: not exempt: %s", name, res.Value().Why())
		}
		if pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30))); pen.Value().Total() != 0 {
			t.Errorf("%s: penalty %v", name, pen.Value().Total())
		}
	}
	// No liability for a six-month year: PIT-EST's twelve-month condition is
	// the default, so the exception is not applied, and the question is
	// asked. (A filed return showing no tax also gives a prior-year test of
	// nothing, which the $800 test exempts on its own, so the case that
	// matters is a year with no return required.)
	shortZero := must(estimated.NewPriorReturn().NotRequiredToFile().Months(6).Build())
	res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: &shortZero}.facts(t)))
	if res.Value().Exempt() || !strings.Contains(because(res.Required(), working.PositionUnsettledRule), "twelve-month") {
		t.Errorf("a six-month year with no liability: exempt %v; asked %q", res.Value().Exempt(), because(res.Required(), working.PositionUnsettledRule))
	}

	// A part-year resident owes no penalty; a full-year nonresident does.
	part := household{tax: 40_000 * money.Dollar, residence: []estimated.ResidencePeriod{
		resident("PA", day(2026, 1, 1), day(2026, 6, 30)), resident("DE", day(2026, 7, 1), day(2026, 12, 31))}}
	if res := must(rules(t).Requirement(part.facts(t))); !res.Value().Exempt() || !strings.Contains(res.Value().Why(), "part-year resident") {
		t.Errorf("a part-year resident is not exempt: %s", res.Value().Why())
	}
	split := household{tax: 40_000 * money.Dollar, residence: []estimated.ResidencePeriod{
		resident("DE", day(2026, 1, 1), day(2026, 6, 30)), resident("DE", day(2026, 7, 1), day(2026, 12, 31))}}
	if res := must(rules(t).Requirement(split.facts(t))); res.Value().Exempt() {
		t.Error("a full year in two periods is treated as part-year")
	}
	non := household{tax: 40_000 * money.Dollar, residence: []estimated.ResidencePeriod{resident("PA", day(2026, 1, 1), day(2026, 12, 31))}}
	if res := must(rules(t).Requirement(non.facts(t))); res.Value().Exempt() || !res.Value().Required() || has(res.Required(), working.PositionResidency) {
		t.Errorf("a nonresident: exempt %v, required %v", res.Value().Exempt(), res.Value().Required())
	}
	unknown := household{tax: 40_000 * money.Dollar, noResidence: true}
	if res := must(rules(t).Requirement(unknown.facts(t))); res.Value().Exempt() || !has(res.Required(), working.PositionResidency) {
		t.Errorf("residence not given: exempt %v, asked %v", res.Value().Exempt(), has(res.Required(), working.PositionResidency))
	}

	// A waiver asked for is not subtracted.
	casualty := estimated.WaiverCasualty
	pen := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, waiver: &casualty}.facts(t), day(2027, 4, 30)))
	if pen.Value().Total() != 456_750 || pen.Value().Waiver() != estimated.WaiverCasualty || has(pen.Required(), working.PositionPenaltyWaiver) {
		t.Errorf("with a casualty waiver asked: %v, %v", pen.Value().Total(), pen.Value().Waiver())
	}
	if !strings.Contains(applied(pen.Applied()), "Tax Conferee") {
		t.Error("the waiver request is not answered")
	}
}

// PIT-UND Part 2, open to a filer who made no payments or four equal ones on
// time:
//
//	no payments, return paid March 1, 2027: 36,000 × 12% = $4,320.00, less
//	60 days × 0.0005 × 36,000 = $1,080.00, is $3,240.00
//	four $5,000 payments on time, to April 30, 2027: 16,000 × 12% = $1,920.00
//
// The second household's regular figure is 4,000 × (0.1820 + 0.1595 +
// 0.1135 + 0.0525) = $2,030.00, so the Short Method is the cheaper.
func TestEstimatedShortMethod(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, returnFiled: day(2027, 3, 1)}.facts(t), day(2027, 4, 30)))
	if a := applied(res.Applied()); !strings.Contains(a, "less $1080.00 for 60 days before April 30, 2027, is $3240.00") {
		t.Errorf("the Short Method is not reported: %s", a)
	}
	h := household{tax: 40_000 * money.Dollar, payments: quarterly(5_000*money.Dollar, 5_000*money.Dollar, 5_000*money.Dollar, 5_000*money.Dollar)}
	res = must(rules(t).Penalty(h.facts(t), day(2027, 4, 30)))
	if res.Value().Total() != 203_000 {
		t.Errorf("the regular method gives %v, want $2,030.00", res.Value().Total())
	}
	if a := applied(res.Applied()); !strings.Contains(a, "12% of the $16000.00 underpayment is $1920.00, less $0.00 for 0 days") {
		t.Errorf("the Short Method is not reported: %s", a)
	}
	// One payment a day late, or unequal payments, close it.
	for name, ps := range map[string][]estimated.Payment{
		"late":    {pay(day(2026, 5, 1), 5_000*money.Dollar), pay(dues[1], 5_000*money.Dollar), pay(dues[2], 5_000*money.Dollar), pay(dues[3], 5_000*money.Dollar)},
		"unequal": quarterly(5_000*money.Dollar, 5_000*money.Dollar, 5_000*money.Dollar, 6_000*money.Dollar),
		"three":   quarterly(5_000*money.Dollar, 5_000*money.Dollar, 5_000*money.Dollar),
	} {
		res := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, payments: ps}.facts(t), day(2027, 4, 30)))
		if strings.Contains(applied(res.Applied()), "Short Method instead") {
			t.Errorf("%s: the Short Method is offered", name)
		}
	}
}

// PIT-EST item 7: a requirement first arising after April 1 is declared by
// June 15, after June 1 by September 15, after September 1 by January 15.
// PIT-UND still charges a quarter on each date, so the schedule is unchanged
// and the rule is reported.
func TestEstimatedLateStart(t *testing.T) {
	for _, tc := range []struct {
		met  estimated.Date
		want string
	}{
		{day(2026, 4, 1), ""},
		{day(2026, 4, 2), "declare by June 15, 2026"},
		{day(2026, 6, 1), "declare by June 15, 2026"},
		{day(2026, 6, 2), "declare by September 15, 2026"},
		{day(2026, 9, 1), "declare by September 15, 2026"},
		{day(2026, 9, 2), "declare by January 15, 2027"},
	} {
		res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar, firstMet: tc.met}.facts(t)))
		a := applied(res.Applied())
		if got := strings.Contains(a, "PIT-EST (2026), item 7"); got != (tc.want != "") || !strings.Contains(a, tc.want) {
			t.Errorf("first met %v: want %q in %s", tc.met, tc.want, a)
		}
		if res.Value()[0].NeededBy() != 9_000*money.Dollar {
			t.Errorf("first met %v: the first installment needs %v", tc.met, res.Value()[0].NeededBy())
		}
	}
}

// The two open questions about a single day. Line 39 counts the first
// installment from May 1, so $9,000 paid on May 1 costs nothing and on May 2
// one day, 9,000 × 0.0005 = $4.50. The instructions count fourth-period
// payments through January 16 where the form heads its column January 15;
// the form is followed, so $9,000 paid on January 16, 2027 is one day late,
// $4.50, and the question is asked.
func TestEstimatedOpenQuestionsOnTheFirstAndLastDays(t *testing.T) {
	for _, tc := range []struct {
		on   estimated.Date
		want money.USCents
	}{{day(2026, 5, 1), 0}, {day(2026, 5, 2), 450}} {
		h := household{tax: 40_000 * money.Dollar, payments: []estimated.Payment{pay(tc.on, 9_000*money.Dollar)}}
		res := must(rules(t).Penalty(h.facts(t), day(2026, 6, 1)))
		if res.Value().Total() != tc.want {
			t.Errorf("paid %v: penalty %v, want %v", tc.on, res.Value().Total(), tc.want)
		}
		if asked := strings.Contains(because(res.Required(), working.PositionUnsettledRule), "from May 1"); asked != (tc.want > 0) {
			t.Errorf("paid %v: line 39 asked %v", tc.on, asked)
		}
	}
	ps := append(quarterly(9_000*money.Dollar, 9_000*money.Dollar, 9_000*money.Dollar), pay(day(2027, 1, 16), 9_000*money.Dollar))
	res := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, payments: ps}.facts(t), day(2027, 4, 30)))
	if res.Value().Total() != 450 {
		t.Errorf("paid January 16: penalty %v, want $4.50", res.Value().Total())
	}
	if !strings.Contains(because(res.Required(), working.PositionUnsettledRule), "Jan 16, 2027 payment") {
		t.Errorf("the January 16 question is not asked: %v", res.Required())
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar}.facts(t)))
	want := map[string][4]string{
		"The penalty by the day or by the month":         {"0.05% for each day", "lines 40 to 42", "1.5% a month, or part of a month", "§ 535(b)"},
		"The day the first installment's penalty starts": {"from May 1", "lines 39 and 40", "Apr 30 for the first", "§§ 535(b), 1170(a)"},
		"The $800 test":                       {"required payment", "line E", "this year's tax less withholding", "§§ 1169(a), (b), 535(b)"},
		"Part-year residents":                 {"only part of the year", "Exceptions to the Penalty", "no exception for a part-year resident", "§§ 1169(a), 535(b)"},
		"A requirement first met after Apr 1": {"whenever the requirement arose", "line 23", "equal installments on the remaining dates", "§ 1170(a)"},
		"The optional Short Method":           {"12% of the year's total underpayment", "Part 2", "each installment's underpayment", "§ 535(b)"},
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
		// Nothing is paid, so every installment is short and the Short Method
		// is open; the household is a full-year resident who started on time,
		// and this year's tax less withholding is over $800 on either reading.
		switch d.Rule() {
		case "Part-year residents", "A requirement first met after Apr 1", "The $800 test":
			if d.AffectsHousehold() {
				t.Errorf("%q affects a household it does not touch", d.Rule())
			}
		default:
			if !d.AffectsHousehold() {
				t.Errorf("%q does not affect a household with every installment short", d.Rule())
			}
		}
	}

	// The figure follows the form on each. By the statute, $9,000 one day
	// late costs a full month, 9,000 × 1.5% = $135.00, from April 30; by the
	// form it costs nothing on May 1 and $4.50 on May 2.
	h := household{tax: 40_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 5, 2), 9_000*money.Dollar)}}
	pen := must(rules(t).Penalty(h.facts(t), day(2026, 6, 1)))
	if pen.Value().Total() != 450 {
		t.Errorf("by the form: %v, want $4.50", pen.Value().Total())
	}
	statute := must(estimated.Monthly(9_000*money.Dollar, day(2026, 4, 30), day(2026, 5, 2), 150, "30 Del. C. § 535(b)"))
	if statute.Months() != 1 || statute.Charge() != 13_500 {
		t.Errorf("by the statute: %d months, %v; want one month, $135.00", statute.Months(), statute.Charge())
	}
	// The part-year exception and the $800 test on the required payment are
	// applied as the form applies them (TestEstimatedExceptions,
	// TestEstimatedThresholdBoundary); the late start is not
	// (TestEstimatedLateStart); the Short Method is reported, not used
	// (TestEstimatedShortMethod).
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := delawaretaxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(40_000 * money.Dollar).Build())
	base := func() *estimated.FactsBuilder {
		return estimated.NewFacts(delaware, 2026).FilingStatus(filing.Single).ThisYear(this)
	}
	for name, f := range map[string]estimated.Facts{
		"a farmer":            must(base().FarmShare(7000).Build()),
		"a fisherman":         must(base().FishShare(7000).Build()),
		"farming and fishing": must(base().FarmShare(3400).FishShare(3300).Build()),
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
		"2027":               must(estimated.NewFacts(delaware, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"extras":             must(base().State("not Delaware's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"$800", "90%", "$150,000", "April 30, not April 15", "1.5% a month", "0.05% a day", "period it was taken out",
		"22.5%, 45%, 67.5% and 90%", "Short Method", "12%", "Tax Conferee", "Form 2210"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if e.VersusFederal() == "" || len(e.Sources()) == 0 {
		t.Error("the explanation has no comparison with federal or no sources")
	}
}

// Without this year's tax the target is last year's tax alone, and the
// penalty against it is an upper bound. The $800 test is on the required
// payment, which can only be lower than last year's figure, so an exemption
// on last year's figure is exact.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000*money.Dollar, 100_000*money.Dollar)}
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
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 30)))
	if !pen.Value().UpperBound() || pen.Value().Total() == 0 {
		t.Errorf("unpaid: penalty %v, upper bound %v", pen.Value().Total(), pen.Value().UpperBound())
	}
	paid := h
	paid.payments = quarterly(2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar)
	if pen := must(rules(t).Penalty(paid.facts(t), day(2027, 4, 30))); pen.Value().Total() != 0 {
		t.Errorf("paid to last year's tax: penalty %v", pen.Value().Total())
	}

	small := household{noTax: true, prior: prior(800*money.Dollar, 50_000*money.Dollar)}
	res = must(rules(t).Requirement(small.facts(t)))
	if !res.Value().Exempt() {
		t.Errorf("last year's $800: not exempt: %s", res.Value().Why())
	}
	if pen := must(rules(t).Penalty(small.facts(t), day(2027, 4, 30))); pen.Value().Total() != 0 || pen.Value().UpperBound() {
		t.Errorf("last year's $800: penalty %v, upper bound %v; want an exact zero", pen.Value().Total(), pen.Value().UpperBound())
	}

	none := household{noTax: true}
	res = must(rules(t).Requirement(none.facts(t)))
	if res.Value().TargetKnown() || res.Value().Required() {
		t.Errorf("no target expected: %s", res.Value().Why())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 30)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// An exception sets the target, so no question may say its answer changes
// the target. $500 of tax last year, with this year's not given.
func TestEstimatedExceptionAsksNothingOfTheTarget(t *testing.T) {
	b := estimated.NewFacts(estimated.Jurisdiction("DE"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Build())).
		PriorYear(must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(500 * money.Dollar).Build())).FullYearResident(true).Build()))
	res := must(must(delawaretaxcalc.EstimatedTax(2026)).Requirement(must(b.Build())))
	if !res.Value().Exempt() {
		t.Fatal("no exception applies")
	}
	for _, r := range res.Required() {
		if strings.Contains(strings.ToLower(r.Effect()), "target") {
			t.Errorf("%v: %q", r.Position(), r.Effect())
		}
	}
}

// The waiver asked for is decided from the reasons PIT-UND's instructions
// and PIT-EST item 11 list. The penalty stays the figure before any waiver,
// and IfWaived shows $0 only when the waiver is open to the facts.

// waiverPenalty is the penalty on $10,000 of tax with nothing paid or
// withheld, figured to Apr 15, 2027, with the waiver asked for as pos.
func waiverPenalty(t *testing.T, pos estimated.Positions) working.Result[estimated.Penalty] {
	t.Helper()
	f := must(estimated.NewFacts(estimated.Jurisdiction("DE"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Positions(pos).Build())
	return must(must(delawaretaxcalc.EstimatedTax(2026)).Penalty(f, day(2027, 4, 15)))
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

// PIT-UND lists retirement after 62 or disability in the year or the year
// before, and a casualty, disaster or other unusual circumstance. Unlike the
// federal pair, neither names reasonable cause or a penalty that would be
// inequitable.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "pit-und-2025-instructions.txt",
		"Once the penalty has been assessed you may request a waiver of the penalty",
		"1. The underpayment was due to a casualty, disaster, or other unusual",
		"2. In 2024 or 2025 you retired after age 62 or became disabled.")
	mustContain(t, "pit-est-2026.txt",
		"The underpayment penalty may be waived if the underpayment is due to casualty, disaster, or other unusual circumstances.")
	checkWaivers(t, []waiverCase{
		{"retired in 2026 at 64", waiverFor(estimated.CauseRetirement, day(2026, 6, 1), day(1962, 3, 1)), "eligible", "retired after reaching 62 in 2026"},
		{"retired in 2020", waiverFor(estimated.CauseRetirement, day(2020, 6, 1), day(1955, 3, 1)), "refused", "2025 or 2026"},
		{"retired at 60", waiverFor(estimated.CauseRetirement, day(2026, 6, 1), day(1966, 3, 1)), "refused", "reached 62"},
		{"disabled in 2025", waiverFor(estimated.CauseDisability, day(2025, 2, 1), estimated.Date{}), "eligible", "became disabled in 2025"},
		{"a disaster", waiverFor(estimated.CauseDisaster, day(2026, 8, 1), estimated.Date{}), "eligible", "due to a disaster"},
	})
	// The difference from federal: no reasonable-cause or unfairness test.
	for _, pos := range []estimated.Positions{
		waiverFor(estimated.CauseRetirement, day(2026, 6, 1), day(1962, 3, 1)),
		waiverFor(estimated.CauseCasualty, day(2026, 8, 1), estimated.Date{}),
	} {
		o, _ := waiverPenalty(t, pos).Value().WaiverOutcome()
		if strings.Contains(o.Reason(), "reasonable cause") || strings.Contains(o.Reason(), "unfair") {
			t.Errorf("%q sets a condition Delaware's instructions don't", o.Reason())
		}
	}
}
