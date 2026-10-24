// The District of Columbia's estimated tax rules for 2026, checked against
// the Office of Tax and Revenue's own documents and the D.C. Code.
//
// The District publishes no exhaustive oracle and no worked example of the
// underpayment interest. D-2210 defines its line arithmetic and prints its
// four factors, and its instructions work one example of lines 7 and 8 with
// no interest figure. Every threshold, percentage, date and factor is
// compared against its extract in testdata/. The interest arithmetic, the
// boundaries and the exceptions are hand-written cases, worked by hand in
// their comments, and the statute's compounding is computed beside the
// form's to pin the divergence between them.
package districtofcolumbiataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/districtofcolumbiataxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const district = estimated.Jurisdiction("DC")

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
	return must(districtofcolumbiataxcalc.EstimatedTax(2026))
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

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

func quarterly(a ...money.USCents) []estimated.Payment {
	var out []estimated.Payment
	for i, x := range a {
		out = append(out, pay(dues[i], x))
	}
	return out
}

type household struct {
	tax         money.USCents // D-40 line 24 less line 30
	noTax       bool
	withheld    money.USCents
	withheldOn  estimated.Date
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	returnFiled estimated.Date
	firstMet    estimated.Date
	periods     []money.USCents // income to each period end
	asWithheld  bool
	waiver      *estimated.WaiverKind
	farmShare   money.Rate
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	fb := estimated.NewFigures()
	if !h.noTax {
		fb.Tax(h.tax)
	}
	b := estimated.NewFacts(district, 2026).FilingStatus(filing.Single).ThisYear(must(fb.Build())).
		Paid(h.payments...).ReturnFiled(h.returnFiled).RequirementFirstMet(h.firstMet).FarmShare(h.farmShare)
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
	eb := estimated.NewElections()
	if h.periods != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, inc := range h.periods {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).Income(inc).Build()))
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

// prior is a filed twelve-month DC return by a full-year resident.
func prior(tax money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(true).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "d40es-2026.txt",
		"D-40ES (REV. 03/2026)",
		"You must file these vouchers if you are required to file a District of Columbia (DC) income tax return and, you expect to owe $100 or more in taxes.",
		"underpayment interest on the unpaid amount of tax at the rate of 10% per year compounded daily",
		"At least 90% of your 2026 income tax; or",
		"110% of your 2025 DC income tax for a 12-month period.",
		"Voucher 1: April 15, 2026;", "Voucher 2: June 15, 2026;", "Voucher 3: September 15, 2026; and", "Voucher 4: January 15, 2027",
		"If the due date falls on a Saturday, Sunday, or legal holiday, the voucher is due the next business day.",
		"You can apply the full amount of any overpayment of tax from your prior year’s DC income tax return to the first payment of your estimated taxes.",
		"If you file your DC individual tax return on or before January 15, you must pay the balance of any tax due with your return before you make your last estimated tax payment for the year. Do not send in your last voucher.",
	)
	mustContain(t, "d2210-2025.txt",
		"Revised 05/2025",
		"A. Your tax liability on taxable income after deducting your District of Columbia (DC) withholding tax and applicable credits is less than $100, or",
		"Note: You must have been a 12-month DC resident last year in order to use the prior year 110% exception.",
		"2025 DC Tax Liability - Line 24 from your 2025 DC Individual Income Tax Return minus Line 30.",
		"Multiply the amount on Line 1 by 90% (.90).",
		"Multiply Line 3 amount by 110% (1.10).",
		"Multiply Line 5 amount by 25% (.25) for the amount required for each periodic payment.",
		"You must have been a DC resident during all of 2024 to use this exception.",
		"Enter the lesser of the amounts on Line 2 and Line 4.",
		"use only the Line 2 amount.",
		"that year, you lived in DC the entire 12 months;",
		"The tax due for 2025 minus income tax withheld and/or estimated tax payments is less than $100;",
		"Your DC estimated tax payments plus any DC income tax withheld equals at least 110% of your 2024 DC income tax liability; or",
		"Your remaining tax due after totaling all credits, estimated tax payments and withholding, is less than 10% of your total DC tax liability for the year.",
		"determine the percentage for each period (divide the period income by the full year’s income). Multiply Line 5 by each period’s percentage",
		"Include the amounts from the previous period in with the 2nd, 3rd and 4th periods.",
		"For each column, subtract Line 8 from Line 7.",
		"These are the underpayment interest factors by period.",
		"the tax on this taxable income is expected to be more than $100.",
		"For each column, multiply the amount on Line 9 by the penalty factor on Line 10.",
	)
	mustContain(t, "d2210-2025-form.txt",
		"04/15/25 06/15/25 09/15/25 01/15/26",
		"10 Underpayment interest factors .0175 .0265 .0351 .0259",
		"11 Line 9 multiplied by Line 10",
	)
	mustContain(t, "d40-2025-excerpts.txt",
		"(DC) income tax return and you expect to owe $100 or more in taxes.",
		"Do not enter cents. Round to the nearest dollar.",
		"$10,500.50 rounds to $10,501",
		"Line 24 Subtract Line 23 from Line 20",
		"Line 30 Total Refundable Credits",
		"Line 40 Underpayment interest",
	)
	mustContain(t, "dcc-47-4201.txt", "(2) 10% per year, compounded daily, beginning January 1, 2003.")
	mustContain(t, "dcc-47-4203.txt",
		"(i) Ninety percent of the tax shown on the return for the taxable year",
		"(II) For tax years beginning after December 31, 2011, 110% of the tax shown on the return of the individual for the preceding taxable year if the individual filed a return for the preceding taxable year consisting of 12 months; or",
		"(B) Twenty-five percent of the lesser of:",
		"22.5% of the tax for the taxable year based upon the annualized income of the individual for the first 3 months of the taxable year;",
		"45% of the tax for the taxable year based upon the annualized income of the individual for the first 5 months of the taxable year;",
		"67.5% of the tax for the taxable year based upon the annualized income of the individual for the first 8 months of the taxable year; and",
		"(iv) On the fourth installment date, 90% of the tax for the taxable year.",
		"(B) The annualized income method shall not apply to individuals filing a return for part of a taxable year except under regulations as the Mayor may prescribe.",
		"applying the underpayment rate set forth in § 47-4201 to the amount of the underpayment for the period of the underpayment.",
		"the earlier of the 15th day of the 4th month following the close of the taxable year or the date on which the amount of the underpayment is made; provided, that an underpayment that is unpaid during part of a month shall be considered to be paid at the end of the month.",
		"(1) A payment of estimated tax shall be credited against unpaid required installments in the order in which the installments are required to be paid.",
		"An equal part of such amount shall be deemed paid on each due date for the payment of estimated tax for the taxable year unless the taxpayer establishes the dates on which all amounts were actually withheld",
		"(4) The amount of a refund of a prior year’s tax applied against the tax during the taxable year shall be deemed a payment of estimated tax.",
		"reduced by applicable credits and payments of estimated tax that are timely made, is less than $ 100;",
		"(2) The individual did not have any liability for tax for the preceding taxable year;",
		"(A) The taxpayer retired after having attained 62 years of age or developed a disability",
		"by reason of casualty, disaster, or other unusual circumstances",
		"(5) The taxpayer dies during the taxable year.",
	)
	mustContain(t, "dcc-47-1812.08-i.txt",
		"that will result in a tax liability of more than $100.",
		"shall be filed with the Mayor on or before April 15th of the taxable year",
		"(A) after April 1st and before June 2nd of the taxable year, the declaration shall be filed on or before June 15th of the taxable year;",
		"(B) after June 1st and before September 2nd of the taxable year, the declaration shall be filed on or before September 15th of the taxable year;",
		"(C) after September 1st of the taxable year, the declaration shall be filed on or before January 15th of the succeeding taxable year;",
		"(6) If on or before January 15th of the succeeding taxable year the taxpayer files a return",
	)
	mustContain(t, "otr-underpayment-interest.txt",
		"OTR will automatically charge 10 percent interest, compounded daily, on any underpayment of estimated taxes.",
		"There was no DC tax liability for the preceding tax year, or",
	)
}

// Sections 47-4201, 47-4203 and 47-1812.08(i) were not changed for 2026 in
// any way that moves a number: the code's history for § 47-4203 ends at its
// 2015 enactment, § 47-4201's at 2001, and D.C. Law 26-89 changes
// § 47-1812.08(i) only to move the power to make regulations to the Chief
// Financial Officer. So no form here predates a change in the law.
func TestEstimatedStatuteUnchangedForTwentyTwentySix(t *testing.T) {
	mustContain(t, "dcc-47-4203.txt", "(Feb. 26, 2015, D.C. Law 20-155, § 7122(b), 61 DCR 9990.)",
		"Section 7123 of D.C. Law 20-155 provided that this subtitle shall be applicable for tax years beginning after December 31, 2014.")
	mustContain(t, "dcc-47-4201.txt", "(June 9, 2001, D.C. Law 13-305, § 403(b), 48 DCR 334.)")
	mustContain(t, "dcl-26-89.txt",
		`(B) Paragraph (4) is amended by striking the phrase "the Mayor" and inserting the phrase "the Chief Financial Officer" in its place.`,
		"Except as otherwise provided, this act shall apply as of January 1, 2025.",
		"(b) This act shall expire after 225 days of its having taken effect.",
		"Effective Feb. 12, 2026",
	)
	text := extract(t, "dcl-26-89.txt")
	start := strings.Index(text, "(8) Subsection (i) is amended as follows:")
	end := strings.Index(text, "(9) Subsection (k)")
	if start < 0 || end < start {
		t.Fatal("the amendments to subsection (i) are not where expected")
	}
	for _, s := range []string{"$100", "April", "June", "September", "January", "percent"} {
		if strings.Contains(text[start:end], s) {
			t.Errorf("D.C. Law 26-89 changes subsection (i) in a way that mentions %q", s)
		}
	}
	for _, s := range []string{"§ 47-4203", "§ 47-4201"} {
		if strings.Contains(text, s) && !strings.Contains(text, "set forth in "+s) {
			t.Errorf("D.C. Law 26-89 amends %s", s)
		}
	}
}

// The 2026 dates are the ones D-40ES prints. None falls on a weekend, so
// none moves, and none is a federal holiday or DC Emancipation Day, April
// 16. A return filed and paid by January 15, 2027, a Friday, takes the place
// of the fourth payment.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar}.facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
	}
	if !strings.Contains(applied(res.Applied()), "no due date this year is a Saturday or Sunday") {
		t.Error("the weekend rule is not reported")
	}
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{
		{day(2027, 1, 15), true},
		{day(2027, 1, 16), false},
	} {
		res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar, returnFiled: tc.filed}.facts(t)))
		if got := res.Value()[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January excused %v, want %v", tc.filed, got, tc.excused)
		}
		if got := res.Value()[0].Status(); got != estimated.StatusShort {
			t.Errorf("filed %v: April is %v, want short", tc.filed, got)
		}
	}
}

// D-40ES asks for payments at $100 or more of tax after withholding, and
// D-2210 charges no interest under $100. $500 of tax keeps the 10% exception
// ($50) out of the way. $400.01 withheld leaves $99.99: exempt. $400 leaves
// $100.00: required, and short against 90% of $500, $450, by $12.50 a
// quarter. The cumulative underpayments are $12.50, $25, $37.50 and $50:
//
//	12.50 × .0175 = 0.22 → $0;  25 × .0265 = 0.66 → $1
//	37.50 × .0351 = 1.32 → $1;  50 × .0259 = 1.30 → $1
//
// The interest is $3.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
		interest money.USCents
	}{
		{40_001, true, 0},
		{40_000, false, 3 * money.Dollar},
	} {
		h := household{tax: 500 * money.Dollar, withheld: tc.withheld}
		res := must(rules(t).Requirement(h.facts(t)))
		if got := res.Value().Exempt(); got != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, got, tc.exempt, res.Value().Why())
		}
		if got := res.Value().Required(); got == tc.exempt {
			t.Errorf("withheld %v: required %v", tc.withheld, got)
		}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if p.Value().Total() != tc.interest {
			t.Errorf("withheld %v: interest %v, want %v", tc.withheld, p.Value().Total(), tc.interest)
		}
	}
}

// D-2210 excuses a balance under 10% of the year's tax, whenever it was paid.
// $10,000 of tax and $9,000 paid on January 15 leaves $1,000, exactly 10%:
// charged. $9,000.01 leaves $999.99: excused, and the open question asked.
func TestEstimatedTenPercentBoundary(t *testing.T) {
	for _, tc := range []struct {
		paid    money.USCents
		excused bool
	}{
		{900_000, false},
		{900_001, true},
	} {
		h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(dues[3], tc.paid)}}
		res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if got := res.Value().Total() == 0; got != tc.excused {
			t.Errorf("paid %v: excused %v, want %v; interest %v", tc.paid, got, tc.excused, res.Value().Total())
		}
		if got := strings.Contains(because(res.Required(), working.PositionUnsettledRule), "less than 10% of the year's tax"); got != tc.excused {
			t.Errorf("paid %v: the 10%% question asked %v", tc.paid, got)
		}
		s := must(rules(t).Schedule(h.facts(t))).Value()
		if got := s[0].Status() == estimated.StatusExcused && s[0].Shortfall() > 0; got != tc.excused {
			t.Errorf("paid %v: April is %v with %v short", tc.paid, s[0].Status(), s[0].Shortfall())
		}
	}
	// Withholding alone that leaves under 10% makes the year exempt, and the
	// exception is the chosen option.
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: 900_001}.facts(t)))
	if !res.Value().Exempt() {
		t.Errorf("withholding leaving $999.99 of $10,000: %s", res.Value().Why())
	}
	if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding under $1,000" {
		t.Errorf("chosen %v %v %q", c.Basis(), c.Amount(), c.Label())
	}
	for _, d := range res.Divergences() {
		if got := d.AffectsHousehold(); got != (d.Rule() == "A balance under 10% of the year's tax") {
			t.Errorf("%q affects the household %v", d.Rule(), got)
		}
	}
}

// The target is the smaller of 90% of this year's tax and 110% of last
// year's, with no 100% tier and no income test. The 110% test needs a filed
// DC return by someone who lived in DC all of last year.
func TestEstimatedTargetOptions(t *testing.T) {
	// 90% of $20,000 is $18,000; 110% of $15,000 is $16,500.
	res := must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: prior(15_000 * money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || res.Value().Target() != 16_500*money.Dollar || c.Percent() != money.Percent(110) {
		t.Errorf("chose %v at %v, want 110%% of last year's, $16,500", c.Basis(), res.Value().Target())
	}
	// 110% of $17,000 is $18,700, more than $18,000.
	res = must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: prior(17_000 * money.Dollar)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 18_000*money.Dollar || c.Percent() != money.Percent(90) {
		t.Errorf("chose %v at %v, want 90%% of this year's, $18,000", c.Basis(), res.Value().Target())
	}
	// A high income keeps 110%, not more.
	f := must(estimated.NewFigures().Tax(100_000 * money.Dollar).FederalAGI(2_000_000 * money.Dollar).Build())
	rich := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(true).Build())
	res = must(rules(t).Requirement(household{tax: 200_000 * money.Dollar, prior: &rich}.facts(t)))
	if o := res.Value().Options()[1]; o.Percent() != money.Percent(110) || o.Amount() != 110_000*money.Dollar {
		t.Errorf("at $2,000,000 of AGI the prior-year test is %v, %v", o.Percent(), o.Amount())
	}

	tax := must(estimated.NewFigures().Tax(5_000 * money.Dollar).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(tax).FullYearResident(true).Build())
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	unanswered := must(estimated.NewPriorReturn().Filed(true).Figures(tax).Build())
	partYear := must(estimated.NewPriorReturn().Filed(true).Figures(tax).FullYearResident(false).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   working.PositionKind
		rule  string
	}{
		"no return on record": {nil, working.PositionPriorYearReturn, ""},
		"not filed":           {&notFiled, working.PositionUnspecified, ""},
		"not required":        {&notRequired, working.PositionUnspecified, ""},
		"a six-month year":    {&short, working.PositionUnspecified, ""},
		"residence not given": {&unanswered, working.PositionResidency, ""},
		"a part-year return":  {&partYear, working.PositionUnspecified, "would allow 110% of the $5000.00 tax on it, $5500.00"},
	} {
		res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: tc.prior}.facts(t)))
		if res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
		if res.Value().Target() != 36_000*money.Dollar {
			t.Errorf("%s: target %v, want $36,000", name, res.Value().Target())
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
		if name == "a part-year return" {
			if o := res.Value().Options()[1]; o.Amount() != 0 || o.WhyNot() != "You didn't live in DC all of 2025" {
				t.Errorf("%s: last year's option is %v, %q", name, o.Amount(), o.WhyNot())
			}
		}
		if tc.rule != "" && !strings.Contains(applied(res.Applied()), tc.rule) {
			t.Errorf("%s: the statute's reading is not reported: %s", name, applied(res.Applied()))
		}
	}
}

// $40,000 of tax, nothing withheld or paid, no return last year: the target
// is 90%, $36,000, a quarter $9,000. D-2210 lines 7 to 11 charge each
// period's cumulative underpayment at its factor, in whole dollars:
//
//	April 15:      9,000 × .0175 = 157.50 → $158
//	June 15:      18,000 × .0265 = 477.00 → $477
//	September 15: 27,000 × .0351 = 947.70 → $948
//	January 15:   36,000 × .0259 = 932.40 → $932
//
// The total is $2,515. The September span runs from 2026 into 2027 and the
// January span lies wholly in 2027. The District's rate does not change: it
// is 10% compounded daily by statute since 2003, and no change for 2027 is
// published or needed, so no part reports an unpublished rate.
func TestEstimatedInterestAtThePrintedFactors(t *testing.T) {
	res := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), day(2027, 4, 15)))
	p := res.Value()
	want := []struct {
		under  money.USCents
		days   int
		charge money.USCents
	}{
		{9_000 * money.Dollar, 61, 158 * money.Dollar},
		{18_000 * money.Dollar, 92, 477 * money.Dollar},
		{27_000 * money.Dollar, 122, 948 * money.Dollar},
		{36_000 * money.Dollar, 90, 932 * money.Dollar},
	}
	lines := p.Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 4", len(lines))
	}
	for i, l := range lines {
		parts := l.Parts()
		if l.Shortfall() != want[i].under || len(parts) != 1 {
			t.Errorf("line %d is on %v in %d parts, want %v in one", i+1, l.Shortfall(), len(parts), want[i].under)
			continue
		}
		if parts[0].Days() != want[i].days || parts[0].Charge() != want[i].charge || parts[0].Rate() != money.Percent(10) {
			t.Errorf("line %d: %d days at %v is %v; want %d days at 10%%, %v", i+1, parts[0].Days(), parts[0].Rate(), parts[0].Charge(), want[i].days, want[i].charge)
		}
	}
	if p.Total() != 2_515*money.Dollar {
		t.Errorf("interest %v, want $2,515", p.Total())
	}
	if p.AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v", p.AsOf())
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("an unpublished rate is reported")
	}
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the statute's waivers are not asked about")
	}
	// Figured past April 15, 2027, it stops there.
	if p := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), day(2027, 6, 30))).Value(); p.AsOf() != day(2027, 4, 15) || p.Total() != 2_515*money.Dollar {
		t.Errorf("to June 30, 2027: %v figured to %v", p.Total(), p.AsOf())
	}

	// A payment during a span counts only from the next due date. $9,000 on
	// May 1 leaves April's $9,000 charged the whole .0175, $158, and June's
	// column 18,000 − 9,000 = 9,000 × .0265 = 238.50 → $239.
	h := household{tax: 40_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 5, 1), 9_000*money.Dollar)}}
	l := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value().Lines()
	if l[0].Total() != 158*money.Dollar || l[1].Shortfall() != 9_000*money.Dollar || l[1].Total() != 239*money.Dollar {
		t.Errorf("after a May 1 payment: April %v, June %v on %v", l[0].Total(), l[1].Total(), l[1].Shortfall())
	}

	// Figured to May 1, 2026, April's span is 16 of 61 days in:
	// 9,000 × .0175 × 16 / 61 = 41.31 → $41.
	res = must(rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), day(2026, 5, 1)))
	if res.Value().Total() != 41*money.Dollar || !strings.Contains(applied(res.Applied()), "16 of the 61 days") {
		t.Errorf("to May 1: %v; %s", res.Value().Total(), applied(res.Applied()))
	}
	// Nothing before the first due date.
	if p := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), day(2026, 4, 15))).Value(); p.Total() != 0 {
		t.Errorf("on April 15: %v", p.Total())
	}
}

// The statute charges 10% compounded daily from the due date until the
// underpayment is paid, a payment during a month counting at the month's
// end; the form charges a printed factor for each whole span. Both readings,
// on $10,000 short on April 15 alone ($100,000 of tax and $36,363.64 last
// year, 110% of which is $40,000, with $10,000 paid on May 1 and on each
// later date):
//
//	form:    10,000 × .0175 = $175
//	statute: paid May 1, so to May 31, 46 days:
//	         10,000 × ((1 + 0.10/365)^46 − 1) = 126.81
//
// And unpaid all year, the four factors add to .1050 against the statute's
// 365 days compounded:
//
//	form:    10,000 × (.0175 + .0265 + .0351 + .0259) = $1,050
//	statute: 10,000 × ((1 + 0.10/365)^365 − 1) = 1,051.56
func TestEstimatedInterestDivergesFromTheStatutesCompounding(t *testing.T) {
	statute := must(estimated.NewRateSeries().
		Period(day(2026, 1, 1), day(2027, 12, 31), money.Percent(10), "D.C. Code § 47-4201(d)(2)").Build())
	compound := func(from, to estimated.Date) money.USCents {
		parts, gap, err := estimated.CompoundDaily(10_000*money.Dollar, from, to, statute)
		if err != nil || !gap.IsZero() {
			t.Fatalf("err %v, gap %v", err, gap)
		}
		var total money.USCents
		for _, p := range parts {
			total += p.Charge()
		}
		return total
	}

	h := household{tax: 100_000 * money.Dollar, prior: prior(3_636_364), payments: []estimated.Payment{
		pay(day(2026, 5, 1), 10_000*money.Dollar), pay(dues[1], 10_000*money.Dollar), pay(dues[2], 10_000*money.Dollar), pay(dues[3], 10_000*money.Dollar)}}
	form := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if form.Total() != 175*money.Dollar || len(form.Lines()) != 1 {
		t.Errorf("form: %v over %d lines, want $175 on April alone", form.Total(), len(form.Lines()))
	}
	if got := compound(dues[0], day(2026, 5, 31)); got != 12_681 {
		t.Errorf("statute: %v, want $126.81", got)
	}

	// April's $10,000 never paid: $10,000 on each later date leaves $10,000
	// in every column.
	h = household{tax: 100_000 * money.Dollar, prior: prior(3_636_364), payments: []estimated.Payment{pay(dues[1], 10_000*money.Dollar), pay(dues[2], 10_000*money.Dollar), pay(dues[3], 10_000*money.Dollar)}}
	form = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if form.Total() != 1_050*money.Dollar {
		t.Errorf("form: %v, want $1,050", form.Total())
	}
	if got := compound(dues[0], day(2027, 4, 15)); got != 105_156 {
		t.Errorf("statute: %v, want $1,051.56", got)
	}

	res := must(rules(t).Requirement(h.facts(t)))
	for _, d := range res.Divergences() {
		if d.Rule() == "The interest factors" {
			if !strings.Contains(d.Form(), ".0175, .0265, .0351 and .0259") || !strings.Contains(d.Statute(), "compounded daily") || !strings.Contains(d.Statute(), "end of that month") {
				t.Errorf("the factor divergence does not state both readings: %q / %q", d.Form(), d.Statute())
			}
			return
		}
	}
	t.Error("the factor divergence is not reported")
}

// § 47-4203(d)(3): withholding is paid in equal parts on the due dates,
// unless the filer establishes when all of it was withheld. Last year's
// overpayment applied forward counts toward April.
func TestEstimatedWithholding(t *testing.T) {
	// $8,000 withheld in December, against $9,000 a quarter.
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
	h = household{tax: 40_000 * money.Dollar, payments: []estimated.Payment{must(estimated.NewPayment().Amount(9_000 * money.Dollar).FromPriorYear().Build())}}
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].Status() != estimated.StatusMet {
		t.Errorf("April with last year's overpayment is %v", s[0].Status())
	}
}

// D-2210's annualised method prorates the required annual payment by the
// income to each period's end. $10,000 of tax, 90% is $9,000; income of
// $10,000, $20,000, $60,000 and $100,000 to the period ends gives running
// totals of 10%, 20% and 60% of $9,000, then all of it: $900, $1,800,
// $5,400 and $9,000. The statute's 22.5/45/67.5/90% method is not used, and
// the question is asked.
func TestEstimatedAnnualisedProration(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, periods: []money.USCents{10_000 * money.Dollar, 20_000 * money.Dollar, 60_000 * money.Dollar, 100_000 * money.Dollar}}
	res := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{90_000, 180_000, 540_000, 900_000} {
		if got := res.Value()[i].NeededBy(); got != want {
			t.Errorf("installment %d needs %v by its date, want %v", i+1, got, want)
		}
	}
	if !strings.Contains(because(res.Required(), working.PositionUnsettledRule), "22.5%, 45%, 67.5% and 90%") {
		t.Errorf("the method is not asked about: %v", res.Required())
	}
	// No income for the year: the regular quarters.
	h.periods = []money.USCents{0, 0, 0, 0}
	if s := must(rules(t).Schedule(h.facts(t))).Value(); s[0].NeededBy() != 225_000 {
		t.Errorf("with no income April needs %v, want $2,250", s[0].NeededBy())
	}
	// The periods must end where the statute's months do.
	f := must(estimated.NewFacts(district, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(100).Build())).
		Periods(
			must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 6, 15)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).Build())
	if _, err := rules(t).Schedule(f); err == nil {
		t.Error("a period ending June 15 is accepted")
	}
}

// The exceptions and the dates that change the figure.
func TestEstimatedExceptions(t *testing.T) {
	// No liability last year, a full-year resident: no interest can arise.
	zero := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: &zero}.facts(t)))
	if !res.Value().Exempt() {
		t.Errorf("no liability last year: %s", res.Value().Why())
	}
	// A part-year resident with no liability: the form does not excuse it,
	// and the statute's reading is reported.
	away := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(false).Build())
	pen := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, prior: &away}.facts(t), day(2027, 4, 15)))
	if pen.Value().Total() == 0 || !strings.Contains(applied(pen.Applied()), "§ 47-4203(e)(2): you had no DC liability for 2025") {
		t.Errorf("no liability, part year: %v; %s", pen.Value().Total(), applied(pen.Applied()))
	}
	// Residence not given: asked, and not applied.
	unsaid := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	res = must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: &unsaid}.facts(t)))
	if res.Value().Exempt() || !strings.Contains(because(res.Required(), working.PositionResidency), "no DC tax that year") {
		t.Errorf("no liability, residence unsaid: exempt %v; %v", res.Value().Exempt(), res.Required())
	}

	// Under $100 once payments are counted, though paid late: $900 of tax,
	// $810 paid January 15 leaves $90. (10% of $900 is $90, so the 10%
	// exception does not reach it.) The statute would count only timely
	// payments, and April to September were not.
	h := household{tax: 900 * money.Dollar, payments: []estimated.Payment{pay(dues[3], 810*money.Dollar)}}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 || !strings.Contains(applied(p.Applied()), "leaves $90.00, under $100.00") {
		t.Errorf("$90 left: %v; %s", p.Value().Total(), applied(p.Applied()))
	}
	// Paid after January 15, it is not an estimated payment.
	h.payments = []estimated.Payment{pay(day(2027, 2, 1), 810*money.Dollar)}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() == 0 {
		t.Error("a February payment excused the year")
	}

	// A return filed and paid by January 15 takes the place of the fourth
	// payment; the earlier columns are still charged, $158 + $477 + $948.
	p := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, returnFiled: day(2027, 1, 12)}.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 1_583*money.Dollar || len(p.Value().Lines()) != 3 {
		t.Errorf("filed January 12: %v over %d lines, want $1,583 over three", p.Value().Total(), len(p.Value().Lines()))
	}
	// Filed later, the fourth factor is charged in full whatever the day.
	p = must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, returnFiled: day(2027, 2, 1)}.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 2_515*money.Dollar || !strings.Contains(applied(p.Applied()), "whatever day the balance was paid") {
		t.Errorf("filed February 1: %v", p.Value().Total())
	}

	// A waiver asked for is not subtracted.
	casualty := estimated.WaiverCasualty
	p = must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, waiver: &casualty}.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 2_515*money.Dollar || p.Value().Waiver() != estimated.WaiverCasualty || has(p.Required(), working.PositionPenaltyWaiver) ||
		!strings.Contains(applied(p.Applied()), "the Mayor decides it") {
		t.Errorf("with a casualty waiver asked: %v, %v", p.Value().Total(), p.Value().Waiver())
	}

	// Farmers and fishermen are under the general rules.
	res = must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, farmShare: 8_000}.facts(t)))
	if res.Value().Target() != 36_000*money.Dollar || !strings.Contains(applied(res.Applied()), "separate rule for farmers or fishermen, so the general rules apply") {
		t.Errorf("a farmer: target %v", res.Value().Target())
	}
}

// § 47-1812.08(i)(4) moves the first declaration for a requirement first
// met after April 1, June 1 or September 1. How the installments are then
// sized is not settled, so the form's four quarters are kept and the
// question asked, naming the statute's date.
func TestEstimatedLateStart(t *testing.T) {
	for _, tc := range []struct {
		met  estimated.Date
		date string
	}{
		{day(2026, 4, 1), ""},
		{day(2026, 4, 2), "due Jun 15."},
		{day(2026, 6, 1), "due Jun 15."},
		{day(2026, 6, 2), "due Sep 15."},
		{day(2026, 9, 1), "due Sep 15."},
		{day(2026, 9, 2), "due Jan 15, 2027."},
	} {
		res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar, firstMet: tc.met}.facts(t)))
		got := because(res.Required(), working.PositionUnsettledRule)
		if (tc.date == "") != (got == "") || !strings.Contains(got, tc.date) {
			t.Errorf("first met %v: asked %q, want %q", tc.met, got, tc.date)
		}
		if res.Value()[0].NeededBy() != 9_000*money.Dollar {
			t.Errorf("first met %v: April needs %v", tc.met, res.Value()[0].NeededBy())
		}
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar}.facts(t)))
	want := map[string][4]string{
		"Who must make estimated payments":           {"$100 or more", "D-40ES", "more than $100", "§ 47-1812.08(i)(1)"},
		"Tax under $100":                             {"no condition that the payments were on time", "item A", "timely made", "§ 47-4203(e)(1)"},
		"The prior-year test":                        {"lived in DC all of last year", "lines 4 and 5", "consisting of 12 months, with no condition of residence", "§ 47-4203(b)(1)(B)(ii)(II)"},
		"No liability last year":                     {"entire 12 months", "exceptions", "no condition of residence", "§ 47-4203(e)(2)"},
		"A balance under 10% of the year's tax":      {"less than 10%", "exceptions", "no such exception", "§ 47-4203(e)"},
		"The 110% item in the exception list":        {"without saying by when", "lines 6 to 11", "25% of the lesser", "(c)(2)(A)"},
		"The annualised income method":               {"proration", "line 7", "22.5%, 45%, 67.5% and 90%", "§ 47-4203(b)(2)"},
		"The interest factors":                       {".0175, .0265, .0351 and .0259", "lines 8 to 11", "compounded daily", "§§ 47-4201(d)(2), 47-4203(c)"},
		"A requirement first met after Apr 1":        {"each of the four dates", "lines 6 and 7", "Jun 15, Sep 15 or Jan 15", "47-1812.08(i)(4)"},
		"Retirement, disability, casualty and death": {"no waiver", "exceptions", "retired after 62", "§ 47-4203(e)(3) to (5)"},
		"Due dates on a weekend or holiday":          {"legal holiday", "D-40ES", "no rule for a weekend or holiday", "§ 47-1812.08(i)(4)"},
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
		// The figures follow the form, except that they measure each
		// installment as lines 6 to 11 and the statute do, not by the
		// exception list's 110% item.
		want := working.ReadingForm
		if d.Rule() == "The 110% item in the exception list" {
			want = working.ReadingStatute
		}
		if d.Uses() != want {
			t.Errorf("%q uses %v, want %v", d.Rule(), d.Uses(), want)
		}
		// Nothing is paid on $40,000 of tax, so the interest factors decide
		// the charge; the household has no prior return, no late start and
		// no waiver, and no date moves.
		if got := d.AffectsHousehold(); got != (d.Rule() == "The interest factors") {
			t.Errorf("%q affects the household %v", d.Rule(), got)
		}
	}
	// The forms' readings are what the figures follow: exactly $100 after
	// withholding requires payments (TestEstimatedThresholdBoundary), a late
	// payment counts toward the $100 and 10% exceptions
	// (TestEstimatedExceptions, TestEstimatedTenPercentBoundary), and a
	// part-year resident has no 110% test (TestEstimatedTargetOptions).
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := districtofcolumbiataxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(40_000 * money.Dollar).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(district, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(district, 2026).FilingStatus(filing.Single).ThisYear(this).State("not the District's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 40_000 * money.Dollar}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"$100", "110%", "no 100% tier", "underpayment interest", ".0175, .0265, .0351 and .0259",
		"compounded daily", "next due date", "10% of the year's tax", "retired after 62", "January 15", "proration by income"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if e.VersusFederal() == "" || len(e.Sources()) == 0 {
		t.Error("the explanation has no comparison with federal or no sources")
	}
}

// Without this year's tax the target is 110% of last year's alone, and the
// interest against it is an upper bound.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	// 110% of $8,000 is $8,800, $2,200 a quarter.
	h := household{noTax: true, prior: prior(8_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 8_800*money.Dollar || r.Exempt() {
		t.Errorf("required %v, target %v; want $8,800: %s", r.Required(), r.Target(), r.Why())
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
	for i, want := range []money.USCents{220_000, 440_000, 660_000, 880_000} {
		if got := sched.Value()[i].NeededBy(); got != want {
			t.Errorf("installment %d needs %v, want %v", i+1, got, want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !pen.Value().UpperBound() || pen.Value().Total() == 0 {
		t.Errorf("unpaid: interest %v, upper bound %v", pen.Value().Total(), pen.Value().UpperBound())
	}
	paid := h
	paid.payments = quarterly(2_200*money.Dollar, 2_200*money.Dollar, 2_200*money.Dollar, 2_200*money.Dollar)
	if pen := must(rules(t).Penalty(paid.facts(t), day(2027, 4, 15))); pen.Value().Total() != 0 {
		t.Errorf("paid to last year's tax: interest %v", pen.Value().Total())
	}

	// No liability last year still exempts, exactly.
	zero := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	res = must(rules(t).Requirement(household{noTax: true, prior: &zero}.facts(t)))
	if !res.Value().Exempt() {
		t.Errorf("no liability without this year's tax: %s", res.Value().Why())
	}
	if p := must(rules(t).Penalty(household{noTax: true, prior: &zero}.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 || p.Value().UpperBound() {
		t.Errorf("no liability: %v, upper bound %v", p.Value().Total(), p.Value().UpperBound())
	}

	// With no return last year either, there is no target.
	none := household{noTax: true}
	res = must(rules(t).Requirement(none.facts(t)))
	if res.Value().TargetKnown() || res.Value().Required() {
		t.Errorf("no target expected: %s", res.Value().Why())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// An exception sets the target, so no question may say its answer changes
// the target. $50 of tax, with last year's return not given.
func TestEstimatedExceptionAsksNothingOfTheTarget(t *testing.T) {
	b := estimated.NewFacts(estimated.Jurisdiction("DC"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(50 * money.Dollar).Build()))
	res := must(must(districtofcolumbiataxcalc.EstimatedTax(2026)).Requirement(must(b.Build())))
	if !res.Value().Exempt() {
		t.Fatal("no exception applies")
	}
	for _, r := range res.Required() {
		if strings.Contains(strings.ToLower(r.Effect()), "target") {
			t.Errorf("%v: %q", r.Position(), r.Effect())
		}
	}
}

// The waiver asked for is decided from D.C. Code § 47-4203(e)(3) and (4).
// The penalty stays the figure before any waiver, and IfWaived shows $0 only
// when the waiver is open to the facts.

// waiverPenalty is the penalty on $10,000 of tax with nothing paid or
// withheld, figured to Apr 15, 2027, with the waiver asked for as pos.
func waiverPenalty(t *testing.T, pos estimated.Positions) working.Result[estimated.Penalty] {
	t.Helper()
	f := must(estimated.NewFacts(estimated.Jurisdiction("DC"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Positions(pos).Build())
	return must(must(districtofcolumbiataxcalc.EstimatedTax(2026)).Penalty(f, day(2027, 4, 15)))
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

// § 47-4203(e)(3) and (4) are the federal pair. The statute names the Mayor;
// the Office of Tax and Revenue decides for the Mayor.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "dcc-47-4203.txt",
		"(A) The taxpayer retired after having attained 62 years of age or developed a disability in the taxable year for which estimated payments were required to be made or in the taxable year preceding such taxable year; and",
		"(B) The underpayment was due to reasonable cause and not to willful neglect;",
		"by reason of casualty, disaster, or other unusual circumstances, the imposition of the addition to tax would be against equity and good conscience;")
	checkWaivers(t, []waiverCase{
		{"retired in 2026 at 64", waiverFor(estimated.CauseRetirement, day(2026, 6, 1), day(1962, 3, 1)), "eligible", "The Office of Tax and Revenue needs"},
		{"retired in 2020", waiverFor(estimated.CauseRetirement, day(2020, 6, 1), day(1955, 3, 1)), "refused", "2025 or 2026"},
		{"retired, no birth date", waiverFor(estimated.CauseRetirement, day(2026, 6, 1), estimated.Date{}), "undecided", "date of birth"},
		{"disabled in 2025", waiverFor(estimated.CauseDisability, day(2025, 2, 1), estimated.Date{}), "eligible", "disabled in 2025"},
		{"a disaster", waiverFor(estimated.CauseDisaster, day(2026, 8, 1), estimated.Date{}), "eligible", "a disaster"},
	})
}
