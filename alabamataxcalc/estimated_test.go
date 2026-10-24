// Alabama's estimated tax rules for 2026, checked against the Department of
// Revenue's documents and the statute text.
//
// Alabama publishes no exhaustive oracle and no worked example for estimated
// tax: Form 2210AL and its instructions give the line arithmetic but no
// example with an answer, and the rates page gives only the formula. Every
// threshold, percentage, date and rate is compared against its extract in
// testdata/, and the penalty arithmetic is worked by hand in each test's
// comment. Those cases are hand-written, so the green tick shows the rules
// match this author's reading of the documents, not an answer the department
// computed.
package alabamataxcalc_test

import (
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/alabamataxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

var day = estimated.MustDate

const alabama = estimated.Jurisdiction("AL")

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
	return must(alabamataxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
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
	tax         money.USCents
	noTax       bool
	withheld    money.USCents
	asWithheld  bool
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *alabamataxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions // a waiver asked for, with its cause and dates
	annualised  []money.USCents      // Schedule AI line 16 for each period
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
	b := estimated.NewFacts(alabama, 2026).FilingStatus(status).ThisYear(must(fb.Build())).
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
		b.State(must(alabamataxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).Build()))
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

// prior is a filed twelve-month 2025 return with its tax and its Alabama and
// federal AGI.
func prior(tax, alabamaAGI, federalAGI money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).StateAGI(alabamaAGI).FederalAGI(federalAGI).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(true).Build())
	return &p
}

// lastYear is the usual prior return: $20,000 of tax on $100,000 of AGI.
func lastYear() *estimated.PriorReturn {
	return prior(20_000*money.Dollar, 100_000*money.Dollar, 100_000*money.Dollar)
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "al-40es-2026-instructions.txt",
		"You expect to owe at least $500 in tax for 2026, after subtracting your withholding and credits.",
		"a. 90% of the tax to be shown on your 2026 tax return, or",
		"b. 100% of the tax shown on your 2025 tax return. Your 2025 tax return must cover all 12 months.",
		"If your Alabama AGI for 2025 was more than $150,000 ($75,000 if your filing status for 2026 is Married Filing a Separate Return) substitute 110% for 100% in (2b) under General Rule, above.",
		"paid in full or in equal installments on or before April 15, 2026, June 15, 2026, September 15, 2026 and January 15, 2027. If the 15th falls on a Saturday, Sunday, or",
		"State holiday, the due date will then be considered the following business day.",
		"June 15, if the change occurs after April 1 and before June 2; September 15, if the change occurs after June 1 and before September 2; January 15, if the change occurs after September 1.",
		"1-1-2026 4-1-2026 6-1-2026 9-1-2026 4-2-2026 6-2-2026 9-2-2026 1-1-2027 4-15-2026 6-15-2026 9-15-2026 1-15-2027 1/4 of line 11 1/3 of line 11 1/2 of line 11 All of line 11",
		"Any overpayment credit may be applied to your earliest installment or divided equally among all the installments for the year.",
		"If at least 2/3 of your gross income for the taxable year is derived from farming",
	)
	mustContain(t, "al-40es-2026-worksheet.txt",
		"If less than five hundred dollars ($500), no estimated tax is required to be filed",
		"The Alabama Department of Revenue does not send notices of amounts paid on estimated tax.",
	)
	mustContain(t, "al-2210al-2025.txt",
		"If you are a farmer or fisherman and at least two-thirds of your 2024 or 2025 gross income is from farming or fishing",
		"3 Current tax due. Subtract line 2 from line 1.",
		"4 Multiply line 3 by 90% (.90)",
		"6 Subtract line 5 from line 3. If less than $500, stop; you do not owe a penalty.",
		"8 Required annual payment. Enter the smaller of line 4 or line 7",
		"treating the Alabama income tax withheld from your income as paid on the dates it was actually withheld, instead of in equal amounts on the payment due dates",
		"(a) 4/15/25 (b) 6/15/25 (c) 9/15/25 (d) 1/15/26",
		"25% (.25) of Part I, line 8, Form 2210AL, in each columns",
		"the date the amount on line 8 was paid or 4/15/26,",
		"Underpayment X No. of days on line 10 X .07",
		"1/1/25-3/31/25 1/1/25-5/31/25 1/1/25-8/31/25 1/1/25-12/31/25",
		"17 22.5% 45% 67.5% 90%",
		"21 Enter 25% (.25) of line 8, page 1, Part I, of Form 2210AL in each column.",
	)
	mustContain(t, "al-2210al-2025-instructions.txt",
		"as prescribed in Section 40-2A-11 and 40-18-80 of the Code of Alabama 1975.",
		"If your adjusted gross income (AGI) for 2025 was more than $150,000 ($75,000 if your 2025 filing status is married filing separately), substitute 110% for 100% in (2) above.",
		"NOTE: Please round all amounts to the nearest whole dollar.",
		"If you didn’t file a return for 2024 or if your 2024 tax year was less than 12 months, don’t complete line 7.",
		"you retired after reaching age 62 or became disabled, and your underpayment was due to reasonable cause",
		"The underpayment was due to a casualty, disaster, or other unusual circumstance, and it would be inequitable to impose the penalty.",
	)
	mustContain(t, "al-interest-rates.txt",
		"Formula example: 7% ÷ 365 = daily rate x number of days late x tax = interest due",
		"2026 | 7% | 6% | 7% | 7% |",
		"2025 | 7% | 7% | 7% | 7% |",
		"As established by 26 USCA 6621, According to Section 40-1-44, Code of Alabama 1975.",
	)
	mustContain(t, "al-code-40-18-80.txt",
		"Individuals shall pay estimated income tax in accordance with 26 U.S.C. § 6654 except:",
		"(1) The provisions of 26 U.S.C. § 6654(d)(2)(C)(iii) and 26 U.S.C. § 6654(f)(2) shall not apply;",
		"(2) Five hundred dollars ($500) shall be substituted for the amount provided for in 26 U.S.C. § 6654(e)(1).",
		"(1) Section 40-18-2 shall be substituted when 26 U.S.C. § 6654 refers to Chapter 1 and Chapter 2;",
		"Act 2010-685, p. 1150, §1.)",
	)
	mustContain(t, "al-code-40-1-44.txt",
		"The interest shall be computed based on the underpayment rate established by the Secretary of the Treasury under the authority of 26 U.S.C. §6621.",
	)
	mustContain(t, "usc-6654.txt",
		"the amount of any required installment shall be 25 percent of the required annual payment.",
		"(i) 90 percent of the tax shown on the return for the taxable year",
		"(ii) 100 percent of the tax shown on the return of the individual for the preceding taxable year. Clause (ii) shall not apply if the preceding taxable year was not a taxable year of 12 months or if the individual did not file a return for such preceding taxable year.",
		"If the adjusted gross income shown on the return of the individual for the preceding taxable year beginning in any calendar year exceeds $150,000, clause (ii) of subparagraph (B) shall be applied by substituting \"110 percent\" for \"100 percent\".",
		"clause (i) shall be applied by substituting \"$75,000\" for \"$150,000\".",
		"1st | April 15 | 2nd | June 15 | 3rd | September 15 | 4th | January 15 of the following taxable year. |",
		"(A) the 15th day of the 4th month following the close of the taxable year, or",
		"a payment of estimated tax shall be credited against unpaid required installments in the order in which such installments are required to be paid.",
		"1st | 22.5 | 2nd | 45 | 3rd | 67.5 | 4th | 90. |",
		"(B) the individual did not have any liability for tax for the preceding taxable year, and",
		"an equal part of such amount shall be deemed paid on each due date for such taxable year, unless the taxpayer establishes the dates on which all amounts were actually withheld",
		"(h) Special rule where return filed on or before January 31 If, on or before January 31 of the following taxable year, the taxpayer files a return for the taxable year and pays in full the amount computed on the return as payable, then no addition to tax shall be imposed under subsection (a) with respect to any underpayment of the 4th required installment for the taxable year.",
		"There shall be 3 required installments for the taxable year.",
	)
}

// The 2026 dates are the ones the 40ES prints, and none moves: each is a
// weekday, and none is a State holiday under § 1-3-8. The holidays near them
// are worked out here from the statute's wording: the fourth Monday in April
// is April 27, 2026; the first Monday in June is June 1; Juneteenth is Friday,
// June 19; the first Monday in September is September 7; New Year's Day 2027
// is a Friday; and the third Monday in January 2027 is January 18. No 2026
// due date moves, so no shift is checked; the January 31, 2027 date of
// § 6654(h), a Sunday, is not applied (see TestEstimatedStatutoryRulesTheFormsOmit).
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	mustContain(t, "al-code-1-3-8.txt",
		"(1) New Year’s Day, January 1.",
		"birthday, the third Monday in January.",
		"(4) Confederate Memorial Day, the fourth Monday in April.",
		"(6) Jefferson Davis’ birthday, the first Monday in June.",
		"(7) Juneteenth, June 19.",
		"(9) Labor Day, the first Monday in September.",
		"(b) If any holiday falls on Sunday, the following day is the holiday. If any holiday falls on Saturday, the preceding day is the holiday.",
	)
	nth := func(y int, m time.Month, n int) estimated.Date {
		d := day(y, m, 1)
		for d.Weekday() != time.Monday {
			d = d.AddDays(1)
		}
		return d.AddDays(7 * (n - 1))
	}
	holidays := []estimated.Date{nth(2026, 4, 4), nth(2026, 6, 1), day(2026, 6, 19), nth(2026, 9, 1), day(2027, 1, 1), nth(2027, 1, 3)}
	if holidays[0] != day(2026, 4, 27) || holidays[5] != day(2027, 1, 18) {
		t.Fatalf("worked the holidays out wrongly: %v", holidays)
	}

	res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar, prior: lastYear()}.facts(t)))
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want the printed %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
		for _, h := range holidays {
			if in.Due() == h {
				t.Errorf("installment %d falls on the State holiday %v", i+1, h)
			}
		}
	}
}

// The threshold, as the 40ES words it: at least $500 after withholding means
// payments are needed, and under $500 there is no penalty.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{
		{950_001, true},  // $499.99 left
		{950_000, false}, // $500.00
	} {
		f := household{tax: 10_000 * money.Dollar, withheld: tc.withheld}.facts(t)
		res := must(rules(t).Requirement(f))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		// Under $500 the exception is the chosen option, at $0.
		c, _ := res.Value().Chosen()
		if isException := c.Basis() == estimated.BasisException; isException != tc.exempt ||
			(tc.exempt && (c.Amount() != 0 || c.Label() != "Tax less withholding under $500")) {
			t.Errorf("withheld %v: chosen %v %q at %v", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
		if tc.exempt {
			p := must(rules(t).Penalty(f, day(2027, 4, 15))).Value()
			if p.Total() != 0 || p.Reason() != "Tax less withholding under $500." {
				t.Errorf("withheld %v: penalty %v because %q", tc.withheld, p.Total(), p.Reason())
			}
		}
	}
}

// The prior-year test is 100% up to $150,000 of last year's Alabama AGI and
// 110% a cent above, $75,000 on a separate return. It reads last year's AGI,
// as the 40ES and § 6654(d)(1)(C) do, so this year's AGI does not move it.
func TestEstimatedPriorYearStepBoundaries(t *testing.T) {
	for _, tc := range []struct {
		status filing.Status
		agi    money.USCents
		pct    money.Rate
	}{
		{filing.Single, 15_000_000, money.Percent(100)},
		{filing.Single, 15_000_001, money.Percent(110)},
		{filing.MarriedFilingJointly, 15_000_001, money.Percent(110)},
		{filing.MarriedFilingSeparately, 7_500_000, money.Percent(100)},
		{filing.MarriedFilingSeparately, 7_500_001, money.Percent(110)},
	} {
		h := household{status: tc.status, tax: 100_000 * money.Dollar, prior: prior(10_000*money.Dollar, tc.agi, tc.agi)}
		f := h.facts(t)
		res := must(rules(t).Requirement(f))
		o := res.Value().Options()[1]
		if o.Percent() != tc.pct || o.Amount() != tc.pct.Of(10_000*money.Dollar) {
			t.Errorf("%v, AGI %v: %v of last year's tax, %v; want %v", tc.status, tc.agi, o.Percent(), o.Amount(), tc.pct)
		}
		if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax {
			t.Errorf("%v, AGI %v: chose %v", tc.status, tc.agi, c.Basis())
		}
	}
	// This year's AGI is not the test: $1,000,000 this year, $100,000 last.
	b := estimated.NewFacts(alabama, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(100_000 * money.Dollar).StateAGI(1_000_000 * money.Dollar).Build())).
		PriorYear(*lastYear())
	res := must(rules(t).Requirement(must(b.Build())))
	if o := res.Value().Options()[1]; o.Percent() != money.Percent(100) {
		t.Errorf("this year's AGI moved the test to %v", o.Percent())
	}
}

// The target is the smaller option, and the prior-year option needs a filed
// twelve-month return. Without last year's AGI, 110% is assumed and asked.
func TestEstimatedTargetOptions(t *testing.T) {
	// 90% of $40,000 is $36,000; 100% of $20,000 is $20,000.
	res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: lastYear()}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || res.Value().Target() != 20_000*money.Dollar {
		t.Errorf("chose %v at %v, want last year's $20,000", c.Basis(), res.Value().Target())
	}
	// 90% of $20,000 is $18,000, under last year's $20,000.
	res = must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: lastYear()}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 18_000*money.Dollar {
		t.Errorf("chose %v at %v, want this year's $18,000", c.Basis(), res.Value().Target())
	}

	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(9).
		Figures(must(estimated.NewFigures().Tax(1_000 * money.Dollar).StateAGI(50_000 * money.Dollar).Build())).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   working.PositionKind
	}{
		"no return on record": {nil, working.PositionPriorYearReturn},
		"not filed":           {&notFiled, working.PositionUnspecified},
		"not required":        {&notRequired, working.PositionUnspecified},
		"nine months":         {&short, working.PositionUnspecified},
	} {
		res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: tc.prior}.facts(t)))
		o := res.Value().Options()[1]
		if o.Available() || o.WhyNot() == "" {
			t.Errorf("%s: the prior-year test is offered", name)
		}
		if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax {
			t.Errorf("%s: chose %v", name, c.Basis())
		}
		if tc.ask != working.PositionUnspecified && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
	}

	// No Alabama AGI for last year: 110% of $20,000, $22,000, and the AGI is
	// asked for.
	noAGI := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(20_000 * money.Dollar).Build())).Build())
	res = must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: &noAGI}.facts(t)))
	if o := res.Value().Options()[1]; o.Percent() != money.Percent(110) || o.Amount() != 22_000*money.Dollar {
		t.Errorf("without last year's AGI: %v, %v; want 110%%, $22,000", o.Percent(), o.Amount())
	}
	if !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Error("last year's AGI is not asked for")
	}
}

// § 6654(d)(1)(C) does not say whether it tests federal or Alabama AGI. The
// question is asked whenever the answer could change the percentage.
func TestEstimatedFederalOrAlabamaAGIIsAnOpenQuestion(t *testing.T) {
	noFederal := must(estimated.NewPriorReturn().Filed(true).
		Figures(must(estimated.NewFigures().Tax(20_000 * money.Dollar).StateAGI(100_000 * money.Dollar).Build())).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		asked working.PositionKind
		pct   money.Rate
	}{
		"both under":        {prior(20_000*money.Dollar, 140_000*money.Dollar, 145_000*money.Dollar), working.PositionUnspecified, money.Percent(100)},
		"both over":         {prior(20_000*money.Dollar, 160_000*money.Dollar, 170_000*money.Dollar), working.PositionUnspecified, money.Percent(110)},
		"federal over":      {prior(20_000*money.Dollar, 140_000*money.Dollar, 160_000*money.Dollar), working.PositionUnsettledRule, money.Percent(100)},
		"federal under":     {prior(20_000*money.Dollar, 160_000*money.Dollar, 140_000*money.Dollar), working.PositionUnsettledRule, money.Percent(110)},
		"federal not given": {&noFederal, working.PositionAdjustedGrossIncome, money.Percent(100)},
	} {
		res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: tc.prior}.facts(t)))
		for _, k := range []working.PositionKind{working.PositionAdjustedGrossIncome, working.PositionUnsettledRule} {
			if got, want := has(res.Required(), k), k == tc.asked; got != want {
				t.Errorf("%s: %v asked %v, want %v", name, k, got, want)
			}
		}
		if o := res.Value().Options()[1]; o.Percent() != tc.pct {
			t.Errorf("%s: %v, want %v from Alabama AGI", name, o.Percent(), tc.pct)
		}
	}
}

// The 2026 rates the penalty charges are the ones on the department's page:
// an April shortfall left unpaid to December 31 is charged the second, third
// and fourth quarters' rates in turn.
func TestEstimatedRatesMatchTheADORPage(t *testing.T) {
	row := regexp.MustCompile(`2026 \| (\d+)% \| (\d+)% \| (\d+)% \| (\d+)% \|`).FindStringSubmatch(extract(t, "al-interest-rates.txt"))
	if row == nil {
		t.Fatal("no 2026 row on the rates page")
	}
	var want []money.Rate
	for _, s := range row[2:] { // the first quarter ends before any 2026 installment is due
		want = append(want, money.Percent(int32(must(strconv.Atoi(s)))))
	}
	f := household{tax: 40_000 * money.Dollar, prior: lastYear()}.facts(t)
	parts := must(rules(t).Penalty(f, day(2026, 12, 31))).Value().Lines()[0].Parts()
	wantDays := []int{76, 92, 92}
	if len(parts) != len(want) {
		t.Fatalf("%d parts, want one for each of %d quarters", len(parts), len(want))
	}
	for i, p := range parts {
		if p.Rate() != want[i] || p.Days() != wantDays[i] {
			t.Errorf("part %d: %v for %d days, want %v for %d", i+1, p.Rate(), p.Days(), want[i], wantDays[i])
		}
	}
}

// A shortfall that spans the change from 6% to 7% on July 1, 2026. The
// required annual payment is last year's $20,000, so $5,000 a quarter.
// Nothing is paid by April 15; $5,000 on June 15 pays April's shortfall
// first, so June's is paid only on August 14.
//
//	April: 500,000¢ × 61 days × 6% ÷ 365            = 5,013.70¢, $50.14
//	June:  500,000¢ × 15 days (June 16–30) × 6% ÷ 365 = 1,232.88¢, $12.33
//	       500,000¢ × 45 days (July 1–August 14) × 7% ÷ 365 = 4,315.07¢, $43.15
//	Total                                                       $105.62
//
// Form 2210AL's printed 7% for all 121 days would give $58.49 and $57.53,
// $116.02; the divergence test pins that reading.
func TestEstimatedPenaltyAcrossTwoRatePeriods(t *testing.T) {
	f := household{
		tax:   40_000 * money.Dollar,
		prior: lastYear(),
		payments: []estimated.Payment{
			pay(day(2026, 6, 15), 5_000*money.Dollar),
			pay(day(2026, 8, 14), 5_000*money.Dollar),
			pay(day(2026, 9, 15), 5_000*money.Dollar),
			pay(day(2027, 1, 15), 5_000*money.Dollar),
		},
	}.facts(t)
	res := must(rules(t).Penalty(f, day(2027, 4, 15)))
	lines := res.Value().Lines()
	if len(lines) != 2 {
		t.Fatalf("%d lines, want April and June", len(lines))
	}
	if p := lines[0].Parts(); len(p) != 1 || p[0].Days() != 61 || p[0].Charge() != 5_014 {
		t.Errorf("April's parts: %+v", p)
	}
	june := lines[1].Parts()
	if len(june) != 2 || june[0].Days() != 15 || june[0].Charge() != 1_233 || june[1].Days() != 45 || june[1].Charge() != 4_315 {
		t.Errorf("June's parts: %+v", june)
	}
	if june[0].Rate() != money.Percent(6) || june[1].Rate() != money.Percent(7) {
		t.Errorf("June's rates are %v and %v, want 6%% and 7%%", june[0].Rate(), june[1].Rate())
	}
	if res.Value().Total() != 10_562 {
		t.Errorf("penalty %v, want $105.62", res.Value().Total())
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("an unpublished rate is reported for days that all have one")
	}
}

// Nothing paid at all. The penalty runs into 2027, whose rate the department
// has not published, so the days from January 1 are not charged and the gap
// is reported. To April 15, 2027 it is therefore the same as to December 31.
func TestEstimatedPenaltyIntoAnUnpublishedRate(t *testing.T) {
	f := household{tax: 40_000 * money.Dollar, prior: lastYear()}.facts(t)
	year := must(rules(t).Penalty(f, day(2026, 12, 31)))
	full := must(rules(t).Penalty(f, day(2027, 4, 15)))
	if has(year.Required(), working.PositionUnpublishedRate) || !has(full.Required(), working.PositionUnpublishedRate) {
		t.Errorf("unpublished rate reported to December 31 %v, to April 15 %v",
			has(year.Required(), working.PositionUnpublishedRate), has(full.Required(), working.PositionUnpublishedRate))
	}
	if full.Value().Total() != year.Value().Total() || full.Value().Total() == 0 {
		t.Errorf("penalty %v to April 15, %v to December 31", full.Value().Total(), year.Value().Total())
	}
	// September's $5,000: 15 days at 7% is 1,438.36¢ and 92 days at 7% is
	// 8,821.92¢, $102.60. January's has no rated day at all.
	lines := full.Value().Lines()
	if len(lines) != 4 || lines[2].Total() != 10_260 || lines[3].Total() != 0 {
		t.Errorf("lines: %+v", lines)
	}
}

// The penalty stops on April 15, 2027, or on the day the return is filed and
// paid if that is earlier.
func TestEstimatedPenaltyStops(t *testing.T) {
	h := household{tax: 40_000 * money.Dollar, prior: lastYear()}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 6, 30))); p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
	h.returnFiled = day(2027, 2, 26)
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if p.Value().AsOf() != day(2027, 2, 26) {
		t.Errorf("figured to %v, want the day the return was paid", p.Value().AsOf())
	}
	for _, l := range p.Value().Lines() {
		if l.Days() != day(2027, 2, 26).DaysSince(l.Due()) {
			t.Errorf("installment %d ran %d days, want to February 26", l.Installment(), l.Days())
		}
	}
	if _, err := rules(t).Penalty(h.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date was figured")
	}
}

// Withholding counts in four equal parts on the due dates, whenever it was
// withheld, unless the filer elects the dates it was actually withheld (Form
// 2210AL box D).
func TestEstimatedWithholdingIsCreditedEvenlyUnlessElected(t *testing.T) {
	h := household{tax: 40_000 * money.Dollar, prior: lastYear(), withheld: 20_000 * money.Dollar}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, in := range sched.Value() {
		if in.Status() != estimated.StatusMet || in.CreditedBy() != money.USCents(i+1)*5_000*money.Dollar {
			t.Errorf("installment %d is %v with %v credited", i+1, in.Status(), in.CreditedBy())
		}
	}
	h.asWithheld = true
	sched = must(rules(t).Schedule(h.facts(t)))
	for i, in := range sched.Value()[:3] {
		if in.Status() != estimated.StatusShort {
			t.Errorf("by election, installment %d is %v; the withholding came on December 15", i+1, in.Status())
		}
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() == 0 {
		t.Error("withholding in December credited on its date owes nothing")
	}
}

// Schedule AI uses 22.5%, 45%, 67.5% and 90% of the annualised tax. On a
// flat $10,000 each installment is $2,250, against a regular $5,000.
func TestEstimatedAnnualisedPercentages(t *testing.T) {
	flat := []money.USCents{10_000 * money.Dollar, 10_000 * money.Dollar, 10_000 * money.Dollar, 10_000 * money.Dollar}
	res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar, prior: lastYear(), annualised: flat}.facts(t)))
	for i, in := range res.Value() {
		if want := money.USCents(i+1) * 2_250 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v by its date, want %v", i+1, in.NeededBy(), want)
		}
	}
	// A period that ends on another day is refused.
	f := must(estimated.NewFacts(alabama, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(40_000*money.Dollar).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).
		Periods(
			must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 6, 30)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).Build())
	if _, err := rules(t).Schedule(f); err == nil {
		t.Error("a Schedule AI period ending June 30 was accepted")
	}
}

// The waivers are the filer's to ask for and the department's to grant, so
// the penalty is shown before any waiver.
func TestEstimatedWaiverIsAskedNotApplied(t *testing.T) {
	h := household{tax: 40_000 * money.Dollar, prior: lastYear()}
	before := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if !has(before.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked")
	}
	casualty := estimated.WaiverCasualty
	h.waiver = &casualty
	after := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
	if has(after.Required(), working.PositionPenaltyWaiver) || after.Value().Waiver() != estimated.WaiverCasualty || after.Value().Total() != before.Value().Total() {
		t.Errorf("with a casualty waiver asked: %v, total %v against %v", after.Value().Waiver(), after.Value().Total(), before.Value().Total())
	}
}

// § 6654(h) and § 6654(e)(2) are in the statute and not on the forms. The
// figure follows the forms: neither is applied, and when the facts meet one
// the working says so.
func TestEstimatedStatutoryRulesTheFormsOmit(t *testing.T) {
	// Filed and paid by January 31: January is still short and charged.
	for _, tc := range []struct {
		filed estimated.Date
		noted bool
	}{{day(2027, 1, 29), true}, {day(2027, 1, 31), true}, {day(2027, 2, 1), false}} {
		f := household{tax: 40_000 * money.Dollar, prior: lastYear()}
		f.returnFiled = tc.filed
		res := must(rules(t).Schedule(f.facts(t)))
		if s := res.Value()[3].Status(); s != estimated.StatusShort {
			t.Errorf("filed %v: January is %v, want short", tc.filed, s)
		}
		if got := applied(res.Applied(), "which the statute says excuses the January installment"); got != tc.noted {
			t.Errorf("filed %v: § 6654(h) noted %v, want %v", tc.filed, got, tc.noted)
		}
	}

	// No return required last year: no exemption, and the statute's exception
	// is noted.
	none := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	h := household{tax: 40_000 * money.Dollar, prior: &none}
	res := must(rules(t).Requirement(h.facts(t)))
	if res.Value().Exempt() || !res.Value().Required() || !applied(res.Applied(), "Form 2210AL has no such exception") {
		t.Errorf("no return required last year: exempt %v, required %v", res.Value().Exempt(), res.Value().Required())
	}
	if p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31))); p.Value().Total() == 0 {
		t.Error("the § 6654(e)(2) exception was applied")
	}

	// A filed full-year return showing no tax needs nothing on the form's own
	// arithmetic, so no penalty arises either way.
	zero := prior(0, 30_000*money.Dollar, 30_000*money.Dollar)
	if p := must(rules(t).Penalty(household{tax: 40_000 * money.Dollar, prior: zero}.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("a penalty of %v after a year with no tax on a filed return", p.Value().Total())
	}
}

// The 40ES gives a late starter fewer, larger payments; Form 2210AL charges
// 25% by every date. The figure follows Form 2210AL and the working names the
// 40ES schedule, whose groups start April 2, June 2 and September 2.
func TestEstimatedLateStartFollowsForm2210AL(t *testing.T) {
	for _, tc := range []struct {
		met  estimated.Date
		note string
	}{
		{day(2026, 4, 1), ""},
		{day(2026, 4, 2), "pay 1/3 of the estimate by June 15, 2026"},
		{day(2026, 6, 1), "pay 1/3 of the estimate by June 15, 2026"},
		{day(2026, 6, 2), "pay 1/2 of the estimate by September 15, 2026"},
		{day(2026, 9, 1), "pay 1/2 of the estimate by September 15, 2026"},
		{day(2026, 9, 2), "pay all of the estimate by January 15, 2027"},
	} {
		res := must(rules(t).Schedule(household{tax: 40_000 * money.Dollar, prior: lastYear(), firstMet: tc.met}.facts(t)))
		if tc.note == "" {
			if applied(res.Applied(), "the 40ES would have you pay") {
				t.Errorf("first met %v: a late start is noted", tc.met)
			}
		} else if !applied(res.Applied(), tc.note) {
			t.Errorf("first met %v: %q is not in the working", tc.met, tc.note)
		}
		if s := res.Value()[0]; s.NeededBy() != 5_000*money.Dollar || s.Status() != estimated.StatusShort {
			t.Errorf("first met %v: April needs %v and is %v; want $5,000 and short", tc.met, s.NeededBy(), s.Status())
		}
	}
}

// Each divergence is reported with both readings, both sources, the reading
// the figures follow, and whether it changes these facts.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 40_000 * money.Dollar, prior: lastYear()}.facts(t)))
	want := map[string]struct {
		form, statute string
		uses          working.Reading
	}{
		"The interest rate":                     {"prints one rate, 7%, for every day", "§ 6654(a)", working.ReadingStatute},
		"Which year's AGI sets the 110% test":   {"they ask for \"AGI for 2025\"", "§ 6654(d)(1)(C)", working.ReadingStatute},
		"A return filed and paid by January 31": {"Neither form excuses the January payment", "§ 6654(h)", working.ReadingForm},
		"No tax liability last year":            {"Form 2210AL has no exception", "§ 6654(e)(2)", working.ReadingForm},
		"A requirement first met after April 1": {"Form 2210AL still requires 25% by every date", "§ 6654(c), (d)(1)(A)", working.ReadingStatute},
		"Nonresident aliens":                    {"Neither form has separate rules", "§ 6654(j)", working.ReadingForm},
	}
	// Nothing paid, so April's shortfall runs through the 6% quarter. This
	// year's AGI is not in the facts, and 110% of last year's tax would raise
	// the target. The return was not filed by January 31, and the
	// requirement was met from the start of the year.
	affects := map[string]bool{
		"The interest rate":                     true,
		"Which year's AGI sets the 110% test":   true,
		"A return filed and paid by January 31": false,
		"No tax liability last year":            false,
		"A requirement first met after April 1": false,
		"Nonresident aliens":                    false,
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
		if !strings.Contains(d.Form(), w.form) || !strings.Contains(d.StatuteSource(), w.statute) || d.FormSource() == "" || d.Statute() == "" {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s)", d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
		}
		if d.Uses() != w.uses {
			t.Errorf("%q uses %v, want %v", d.Rule(), d.Uses(), w.uses)
		}
		if d.AffectsHousehold() != affects[d.Rule()] {
			t.Errorf("%q affects these facts %v, want %v", d.Rule(), d.AffectsHousehold(), affects[d.Rule()])
		}
	}
	// The form's readings are in the forms.
	mustContain(t, "al-2210al-2025.txt", "Underpayment X No. of days on line 10 X .07")
	mustContain(t, "al-2210al-2025-instructions.txt", "If your adjusted gross income (AGI) for 2025 was more than $150,000")
}

// A divergence affects only the facts it turns on.
func TestEstimatedDivergencesAffectOnlyTheirFacts(t *testing.T) {
	affects := func(h household, rule string) bool {
		t.Helper()
		for _, d := range must(rules(t).Requirement(h.facts(t))).Divergences() {
			if d.Rule() == rule {
				return d.AffectsHousehold()
			}
		}
		t.Fatalf("no divergence %q", rule)
		return false
	}
	paid := household{tax: 40_000 * money.Dollar, prior: lastYear(), withheld: 20_000 * money.Dollar}
	if affects(paid, "The interest rate") {
		t.Error("the interest rate affects a household that met every due date")
	}
	jan := household{tax: 40_000 * money.Dollar, prior: lastYear(), returnFiled: day(2027, 1, 29)}
	if !affects(jan, "A return filed and paid by January 31") {
		t.Error("the January 31 rule does not affect a return filed January 29 with January short")
	}
	late := household{tax: 40_000 * money.Dollar, prior: lastYear(), firstMet: day(2026, 6, 2)}
	if !affects(late, "A requirement first met after April 1") {
		t.Error("the late-start rule does not affect a requirement first met June 2")
	}
	// 2025 AGI under $150,000 and this year's over it: the 2025 form's reading
	// would make the prior-year test 110%, which is still above 90% of this
	// year's tax here, so the target does not change.
	if affects(household{tax: 10_000 * money.Dollar, prior: lastYear()}, "Which year's AGI sets the 110% test") {
		t.Error("the AGI-year rule affects a household whose target is 90% of this year's tax either way")
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := alabamataxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())
	for name, f := range map[string]estimated.Facts{
		"a farmer": must(estimated.NewFacts(alabama, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(7000).Build()),
		"a fisher": must(estimated.NewFacts(alabama, 2026).FilingStatus(filing.Single).ThisYear(this).FishShare(7000).Build()),
		"a farmer last year": must(estimated.NewFacts(alabama, 2026).FilingStatus(filing.Single).ThisYear(this).
			State(must(alabamataxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(true).Build())).Build()),
	} {
		if _, err := rules(t).Requirement(f); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(alabama, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(alabama, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Alabama's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := rules(t).Penalty(f, day(2027, 4, 15)); err == nil {
			t.Errorf("%s: a penalty was figured", name)
		}
	}
	// Last year's farming share, not given, is asked for.
	f := must(estimated.NewFacts(alabama, 2026).FilingStatus(filing.Single).ThisYear(this).Build())
	if res := must(rules(t).Requirement(f)); !has(res.Required(), working.PositionJurisdictionFact) {
		t.Error("last year's farming share is not asked for")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	for _, s := range []string{"$500 rather than $1,000", "last year's Alabama AGI", "State holidays", "January 31", "no tax last year"} {
		if !strings.Contains(e.VersusFederal(), s) {
			t.Errorf("the comparison with federal does not mention %q", s)
		}
	}
	body := strings.Join(e.Body(), " ")
	for _, s := range []string{"90%", "110%", "$150,000", "6% from April to June", "Schedule AI", "a third by June 15"} {
		if !strings.Contains(body, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Without this year's tax the target is last year's tax alone, the
// installments are a quarter of it, and the penalty is the most it can be.
func TestEstimatedWithoutThisYearsTaxUsesLastYearsAlone(t *testing.T) {
	h := household{noTax: true, prior: prior(5_000*money.Dollar, 100_000*money.Dollar, 100_000*money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 5_000*money.Dollar {
		t.Errorf("required %v, target %v; want $5,000 of last year's tax: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, in := range sched.Value() {
		if want := money.USCents(i+1) * 1_250 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	bound := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !(bound.Value().UpperBound() || !bound.Value().RatedThrough().IsZero()) || bound.Value().Total() == 0 {
		t.Errorf("penalty %v, upper bound %v", bound.Value().Total(), bound.Value().UpperBound())
	}
	k := h
	k.noTax, k.tax = false, 4_000*money.Dollar
	if known := must(rules(t).Penalty(k.facts(t), day(2027, 4, 15))); known.Value().Total() >= bound.Value().Total() || known.Value().UpperBound() {
		t.Errorf("with this year's tax of $4,000 the penalty is %v against the bound of %v", known.Value().Total(), bound.Value().Total())
	}
}

// Without this year's tax or a usable 2025 return there is no target, and
// Schedule and Penalty say so.
func TestEstimatedWithoutThisYearsTaxOrLastYearsHasNoTarget(t *testing.T) {
	f := household{noTax: true}.facts(t)
	res := must(rules(t).Requirement(f))
	if res.Value().TargetKnown() || res.Value().Required() {
		t.Errorf("target known %v, required %v; want no target", res.Value().TargetKnown(), res.Value().Required())
	}
	if !has(res.Required(), working.PositionThisYearTax) || !has(res.Required(), working.PositionPriorYearReturn) {
		t.Errorf("want this year's tax and last year's return asked for: %v", res.Required())
	}
	if _, err := rules(t).Schedule(f); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("Schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(f, day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("Penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// The waivers are decided from the conditions the Form 2210AL instructions states: a
// casualty or disaster, or retiring after 62 or becoming disabled in the tax
// year or the year before. The penalty is still figured in full.
func TestEstimatedWaiverIsDecidedFromItsConditions(t *testing.T) {
	mustContain(t, "al-2210al-2025-instructions.txt", "In 2024 or 2025, you retired after reaching age 62 or became disabled")
	base := household{tax: 40_000 * money.Dollar, prior: lastYear()}
	before := must(rules(t).Penalty(base.facts(t), day(2026, 12, 31)))
	if before.Value().Total() == 0 {
		t.Fatal("the household has no penalty to waive")
	}
	for _, c := range []struct {
		name      string
		positions estimated.Positions
		eligible  bool
		refused   bool
		reason    string
	}{
		{"retired in 2026 at 64", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build()), true, false, "after reaching 62"},
		{"retired in 2025, the year before", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2025, 6, 1)).Born(day(1960, 3, 1)).Build()), true, false, "You retired in 2025"},
		{"retired in 2024", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2024, 6, 1)).Born(day(1955, 3, 1)).Build()), false, true, "2025 or 2026"},
		{"retired in 2020", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, 3, 1)).Build()), false, true, "You retired in 2020"},
		{"retired at 61", must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1965, 3, 1)).Build()), false, true, "You were 61"},
		{"disabled in 2026", must(estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2026, 2, 1)).Build()), true, false, "became disabled in 2026"},
		{"a disaster", must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()), true, false, "a disaster"},
	} {
		h := base
		h.positions = &c.positions
		p := must(rules(t).Penalty(h.facts(t), day(2026, 12, 31)))
		o, ok := p.Value().WaiverOutcome()
		switch {
		case !ok:
			t.Errorf("%s: the penalty has no waiver outcome", c.name)
		case o.Eligible() != c.eligible || o.Refused() != c.refused:
			t.Errorf("%s: eligible %v, refused %v: %q", c.name, o.Eligible(), o.Refused(), o.Reason())
		case !strings.Contains(o.Reason(), c.reason):
			t.Errorf("%s: the reason %q does not say %q", c.name, o.Reason(), c.reason)
		case c.eligible && !strings.Contains(strings.ToLower(o.Reason()), "the alabama department of revenue"):
			t.Errorf("%s: the reason %q does not name %s", c.name, o.Reason(), "the alabama department of revenue")
		}
		if p.Value().Total() != before.Value().Total() {
			t.Errorf("%s: the penalty is %v, and %v before any waiver", c.name, p.Value().Total(), before.Value().Total())
		}
		want := p.Value().Total()
		if c.eligible {
			want = 0
		}
		if p.Value().IfWaived() != want {
			t.Errorf("%s: if waived the penalty is %v, want %v", c.name, p.Value().IfWaived(), want)
		}
		if has(p.Required(), working.PositionPenaltyWaiver) {
			t.Errorf("%s: the waiver is still asked after it was answered", c.name)
		}
	}
}

// An exception sets a $0 target, so a question whose answer would only
// change the target is not asked under one.
func TestEstimatedExceptionAsksNothingAboutTheTarget(t *testing.T) {
	for _, c := range []struct {
		name   string
		h      household
		exempt bool
	}{
		{"tax under $500, no 2025 return", household{tax: 400 * money.Dollar}, true},
		{"tax over $500, no 2025 return", household{tax: 10_000 * money.Dollar}, false},
	} {
		res := must(rules(t).Requirement(c.h.facts(t)))
		if res.Value().Exempt() != c.exempt {
			t.Fatalf("%s: exempt is %v, want %v", c.name, res.Value().Exempt(), c.exempt)
		}
		if has(res.Required(), working.PositionPriorYearReturn) == c.exempt {
			t.Errorf("%s: asked about %v is %v", c.name, working.PositionPriorYearReturn, !c.exempt)
		}
		for _, q := range res.Required() {
			if c.exempt && strings.Contains(strings.ToLower(q.Effect()), "target") {
				t.Errorf("%s: an exception sets the target, yet a question says %q", c.name, q.Effect())
			}
		}
	}
}
