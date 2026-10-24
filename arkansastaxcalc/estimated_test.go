// Arkansas's estimated tax rules for 2026, checked against the Department of
// Finance and Administration's own documents.
//
// Arkansas publishes no exhaustive oracle for estimated tax. The strongest
// check is the worked penalty example in the AR2210 instructions: inputs and
// answer, $67, both from the department, with one shortfall paid in two
// pieces across the year end. Every threshold, percentage, date, rate and
// factor is compared against its extract in testdata/. The exceptions, the
// annualised method and the boundaries are hand-written cases, worked by hand
// in their comments.
package arkansastaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/arkansastaxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

const ar = estimated.Jurisdiction("AR")

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
	return must(arkansastaxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

// asked reports whether a position of kind k mentions about.
func asked(rs []working.Required, k working.PositionKind, about string) bool {
	for _, r := range rs {
		if r.Position() == k && strings.Contains(r.Question()+" "+r.Effect(), about) {
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
	tax         money.USCents // Arkansas net tax, AR1000F line 38
	noTax       bool
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	returnFiled estimated.Date
	firstMet    estimated.Date
	annualised  []money.USCents // AR2210A line 19, each period
	waiver      *estimated.WaiverKind
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	fb := estimated.NewFigures()
	if !h.noTax {
		fb.Tax(h.tax)
	}
	b := estimated.NewFacts(ar, 2026).FilingStatus(filing.Single).ThisYear(must(fb.Build())).
		Paid(h.payments...).ReturnFiled(h.returnFiled).RequirementFirstMet(h.firstMet)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		b.Elections(must(estimated.NewElections().Annualised().Build()))
	}
	if h.waiver != nil {
		b.Positions(must(estimated.NewPositions().Waiver(*h.waiver).Build()))
	}
	return must(b.Build())
}

// prior is a filed twelve-month return by a full-year resident.
func prior(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).FullYearResident(true).Build())
	return &p
}

// garble writes a phrase the way page 1 of the AR2210 instructions extracts
// in places. Its embedded font maps each character 29 code points down, so a
// space becomes \x03, and the "fi" ligature becomes "¿". SOURCES.md says
// more.
func garble(plain string) string {
	var b strings.Builder
	for i := 0; i < len(plain); i++ {
		if strings.HasPrefix(plain[i:], "fi") {
			b.WriteString("¿")
			i++
			continue
		}
		b.WriteByte(plain[i] - 29)
	}
	return b.String()
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "ar1000es-2026.txt",
		"if the taxpayer can reasonably expect their estimated tax to be more than one thousand dollars ($1,000).",
		"If $1,000 or more, file the Declaration Voucher.",
		"If less than $1,000 no Declaration Voucher is required.",
		"Enter one-fourth (1/4) of line 8 of the worksheet",
		"If you first become liable to file a declaration on June 15, 2026: Enter on voucher one-third (1/3) of line 8.",
		"If you first become liable to file a declaration on September 15, 2026: Enter on voucher one-half (1/2) of line 8.",
		"must estimate an amount not less than ninety percent (90%) of the amount actually due.",
		"a penalty of ten percent (10%) per annum shall be added to the amount of the underestimate.",
		"The Underestimate Penalty is computed on the lesser of the current year’s tax liability or the previous year’s tax liability.",
		"If the due date of a voucher falls on a Saturday, Sunday, or legal holiday, the payment will be considered timely filed if it is postmarked on the next succeeding business day.",
		"at least two thirds (2/3) of the total gross income from all sources",
	)
	mustContain(t, "ar1000es-2026-vouchers.txt",
		"Due Date 04/15/2026", "Due Date 06/15/2026", "Due Date 09/15/2026", "Due Date 01/15/2027")
	mustContain(t, "ar2210-2025.txt",
		"If you qualify for an exception, stop here. Do not complete Part II or Part III.",
		"2. Enter 90% (.90) of the amount shown on line 1:",
		"3. 2025 Arkansas income tax withheld:",
		"4. Subtract line 3 from line 1: (If the result is $1,000 or less, stop here. Do not complete this schedule.)",
		"5. 2024 net tax:",
		"6. Required annual payment. Enter the smaller of line 2 or line 5:",
		"4-15-2025 6-15-2025 9-15-2025 1-15-2026",
		"7. Required installments. Enter 1/4 (.25) of line 6, AR2210 in each column:",
		"8. Estimated tax paid and tax withheld. Enter 1/4 (.25) of line 3, AR2210 in each column.",
		"to the date the amount on line 13 was paid, or 4-15-2026, whichever is earlier:",
		"16. Underpayment Number of from line 13 X days from line 15 X .10",
		"16 365 17. PENALTY.",
	)
	mustContain(t, "ar2210i-2025.txt",
		"taxpayer can reasonably expect the estimated tax to be $1,000 or more and income tax withholdings for the income year do not equal 90% of the estimated tax.",
		"No penalty shall be imposed for a tax year if the tax shown on the return for such tax year is less than $1,000",
		"where such return showing a liability",
		"income year of twelve (12) months.",
		"day of the fourth (4th) month of the income year, shall make",
		"pay on the quarterly due date the equivalent to at least ninety percent (90%) of the amount actually due, a penalty of ten percent (10%) per annum shall be added",
		"The penalty provided in ACA §26-18-208 (Supp.1997)",
		// Page 1's embedded font extracts some passages garbled; see garble.
		// Exception 2: the return in lieu of the fourth installment, by January
		// 31, the day the extraction loses.
		garble("In lieu of filing the fourth"),
		"quarter installment the taxpayer may "+garble("file an income tax return and pay the tax on or before January"),
		garble("or on the last day of the first"),
		// Paragraph 1(d): a late starter files by the next quarterly date.
		garble("the requirements for filing a declaration after the fifteenth"),
		garble("and file the declaration on or before the next regular quarterly"),
		"(a) the preceding tax year was a tax year of twelve (12) months,",
		"(c) the taxpayer was a resident of Arkansas throughout the preceding tax year.",
		"casualty, disaster, or other unusual circumstances",
		"(i) retired after having attained age 62",
		"The penalty is computed using Form AR2210A",
	)
	mustContain(t, "ar2210i-2025-example.txt",
		"divided by 365, times ten percent (10%), times the amount of underpayment.",
		"Payments reported on Line (3) shall be deemed to have been paid in four (4) equal payments on the quarterly due dates.",
	)
	mustContain(t, "ar2210a-2025.txt",
		"March 31, 2025 May 31, 2025 Aug. 31, 2025 Dec. 31, 2025",
		"20. Applicable percentages. 20 22.5% 45% 67.5% 90%",
		"25. Multiply line 24 by 90% (.90).",
		"26. Divide line 25 by four and enter the result in each column.",
		"30. Enter the smaller of line 23 or line 28 here.",
		"41. Enter the date the payment was made or 4-15-2026, whichever is earlier.",
		"43. Multiply the number of days by .00027397 for the penalty percentage.",
	)
	mustContain(t, "subject-603.txt",
		"You filed an income tax return and paid the tax by January 31st",
		"you were a resident of Arkansas throughout the preceding tax year.")
}

// The 2026 dates are the ones the AR1000ES vouchers print. None falls on a
// weekend, so no 2026 date moves. The file-and-pay date, January 31, 2027,
// is a Sunday, and is not moved: TestEstimatedReturnFiledByJanuary31 checks
// both sides of it.
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
	if wd := day(2027, 1, 31).Weekday(); wd != time.Sunday {
		t.Errorf("January 31, 2027 is a %v", wd)
	}
}

// The AR2210 instructions' example, moved a year on: $5,000 of tax, $2,000
// paid, last year's $3,000. The required annual payment is $3,000, $750 a
// quarter, against $500 a quarter paid. The 2026 dates give the same days as
// the 2025 ones:
//
//	April:     $250, paid June 15,      61 days: 61/365 × .10 × $250 = $4.18, so $4
//	June:      $500, paid September 15, 92 days: $12.60, so $13
//	September: $500, paid January 15,  122 days: $16.71, so $17
//	           $250, unpaid to April 15, 212 days: $14.52, so $15
//	January:   $750, unpaid to April 15,  90 days: $18.49, so $18
//
// $67 in all. September's second piece runs from 2026 into 2027 as one charge.
// Arkansas's 10% is fixed and no change to it is published, so the days in
// 2027 are charged at it and no unpublished rate is reported.
func TestEstimatedPenaltyMatchesTheAR2210Example(t *testing.T) {
	mustContain(t, "ar2210i-2025-example.txt",
		"Line 1 Tax Liability $5,000 Line 2 90% of Tax Liability $4,500 Line 3 Payments $2,000 Line 4 Line (1) less Line (3) $3,000 Line 5 Prior Year Tax Liability $3,000 Line 6 Required Annual Payment $3,000",
		"61/365 X .10 X $250 = $4",
		"92/365 X .10 x $500 = $13",
		"122/365 X .10 X $500 = $17",
		"212/365 X .10 X $250 = $15",
		"90/365 X .10 x $750 = $18",
		"Total Penalty......................$67")
	h := household{tax: 5_000 * money.Dollar, withheld: 2_000 * money.Dollar, prior: prior(3_000 * money.Dollar)}
	if r := must(rules(t).Requirement(h.facts(t))).Value(); r.Target() != 3_000*money.Dollar {
		t.Errorf("target %v, want $3,000", r.Target())
	}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	type part struct {
		amount money.USCents
		days   int
		charge money.USCents
	}
	want := [][]part{
		{{250 * money.Dollar, 61, 4 * money.Dollar}},
		{{500 * money.Dollar, 92, 13 * money.Dollar}},
		{{500 * money.Dollar, 122, 17 * money.Dollar}, {250 * money.Dollar, 212, 15 * money.Dollar}},
		{{750 * money.Dollar, 90, 18 * money.Dollar}},
	}
	lines := res.Value().Lines()
	if len(lines) != len(want) {
		t.Fatalf("%d lines, want %d", len(lines), len(want))
	}
	for i, l := range lines {
		parts := l.Parts()
		if len(parts) != len(want[i]) {
			t.Errorf("line %d has %d parts, want %d", i+1, len(parts), len(want[i]))
			continue
		}
		for j, p := range parts {
			w := want[i][j]
			if p.Principal() != w.amount || p.Days() != w.days || p.Charge() != w.charge || p.Rate() != money.Percent(10) {
				t.Errorf("line %d part %d: %v for %d days at %v is %v; want %v for %d days at 10%%, %v",
					i+1, j+1, p.Principal(), p.Days(), p.Rate(), p.Charge(), w.amount, w.days, w.charge)
			}
		}
	}
	if res.Value().Total() != 67*money.Dollar {
		t.Errorf("penalty %v, want $67", res.Value().Total())
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("an unpublished rate is reported for a fixed 10%")
	}
	// Past the stop, the penalty is figured to April 15, 2027.
	late := must(rules(t).Penalty(h.facts(t), day(2027, 6, 30)))
	if late.Value().AsOf() != day(2027, 4, 15) || late.Value().Total() != 67*money.Dollar {
		t.Errorf("to June 30: %v to %v, want $67 to April 15, 2027", late.Value().Total(), late.Value().AsOf())
	}
}

// The form treats estimated payments made by January 15 as paid in four
// equal parts, whenever they were made, as the example's note says. $2,000
// paid on January 15, 2027 gives the example's $67, and the question is
// reported. A payment after January 15 counts on its day: $3,000 paid March
// 1, 2027 pays each $750 quarter that day, after 320, 259, 167 and 45 days:
// $65.75, $53.22, $34.32 and $9.25, so $66, $53, $34 and $9, $162 in all.
func TestEstimatedPaymentsCountInEqualParts(t *testing.T) {
	h := household{tax: 5_000 * money.Dollar, prior: prior(3_000 * money.Dollar), payments: []estimated.Payment{pay(day(2027, 1, 15), 2_000*money.Dollar)}}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 67*money.Dollar {
		t.Errorf("$2,000 paid January 15: penalty %v, want the example's $67", res.Value().Total())
	}
	if !asked(res.Required(), working.PositionUnsettledRule, "line 8") {
		t.Error("the question of when estimated payments count is not reported")
	}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	if sched[0].CreditedBy() != 500*money.Dollar {
		t.Errorf("credited by April 15: %v, want a quarter of $2,000", sched[0].CreditedBy())
	}

	h.payments = []estimated.Payment{pay(day(2027, 3, 1), 3_000*money.Dollar)}
	res = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() != 162*money.Dollar {
		t.Errorf("$3,000 paid March 1: penalty %v, want $162", res.Value().Total())
	}
	for _, l := range res.Value().Lines() {
		for _, p := range l.Parts() {
			if p.To() != day(2027, 3, 1) {
				t.Errorf("line %d runs to %v, want March 1, 2027", l.Installment(), p.To())
			}
		}
	}
}

// AR2210 line 4: no penalty when tax less withholding is $1,000 or less. At
// exactly $1,000 the documents disagree, and the question is reported.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		tax, withheld money.USCents
		exempt, ask   bool
	}{
		{99_999, 0, true, false},        // $999.99 of tax
		{200_000, 100_000, true, true},  // $1,000.00 after withholding
		{200_000, 99_999, false, false}, // $1,000.01
		{100_001, 0, false, false},      // $1,000.01 of tax
	} {
		res := must(rules(t).Requirement(household{tax: tc.tax, withheld: tc.withheld}.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("tax %v, withheld %v: exempt %v, want %v (%s)", tc.tax, tc.withheld, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		if c, _ := res.Value().Chosen(); tc.exempt && (c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding of $1,000 or less") {
			t.Errorf("tax %v, withheld %v: chosen %v %q at %v", tc.tax, tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
		if got := asked(res.Required(), working.PositionUnsettledRule, "exactly"); got != tc.ask {
			t.Errorf("tax %v, withheld %v: boundary asked %v, want %v", tc.tax, tc.withheld, got, tc.ask)
		}
	}
}

// The target is the smaller of 90% of this year's tax and 100% of last
// year's, with no 110% tier at any income.
func TestEstimatedTargetOptions(t *testing.T) {
	big := must(estimated.NewPriorReturn().Filed(true).FullYearResident(true).
		Figures(must(estimated.NewFigures().Tax(8_000 * money.Dollar).FederalAGI(2_000_000 * money.Dollar).Build())).Build())
	for name, tc := range map[string]struct {
		prior  *estimated.PriorReturn
		target money.USCents
		basis  estimated.Basis
	}{
		"last year is smaller":          {prior(8_000 * money.Dollar), 8_000 * money.Dollar, estimated.BasisPriorYearTax},
		"this year is smaller":          {prior(9_500 * money.Dollar), 9_000 * money.Dollar, estimated.BasisThisYearTax},
		"no 110% at $2,000,000 of AGI":  {&big, 8_000 * money.Dollar, estimated.BasisPriorYearTax},
		"a cent under 90% of this year": {prior(899_999), 899_999, estimated.BasisPriorYearTax},
	} {
		r := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t))).Value()
		c, ok := r.Chosen()
		if !ok || r.Target() != tc.target || c.Basis() != tc.basis {
			t.Errorf("%s: target %v from %v, want %v from %v", name, r.Target(), c.Basis(), tc.target, tc.basis)
		}
		if o := r.Options()[1]; o.Percent() != money.Percent(100) {
			t.Errorf("%s: last year's option is %v", name, o.Percent())
		}
		if o := r.Options()[0]; o.Percent() != money.Percent(90) || o.Amount() != 9_000*money.Dollar {
			t.Errorf("%s: this year's option is %v of $10,000, %v", name, o.Percent(), o.Amount())
		}
	}
}

// Last year's option needs a filed return. The form takes any filed return's
// net tax, including a short year's or a zero, and the question is reported.
func TestEstimatedPriorYearOptionAvailability(t *testing.T) {
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).FullYearResident(true).
		Figures(must(estimated.NewFigures().Tax(5_000 * money.Dollar).Build())).Build())
	zeroPartYear := must(estimated.NewPriorReturn().Filed(true).FullYearResident(false).
		Figures(must(estimated.NewFigures().Tax(0).Build())).Build())

	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	if res.Value().Options()[1].Available() || !has(res.Required(), working.PositionPriorYearReturn) {
		t.Errorf("no return on record: available %v, asked %v", res.Value().Options()[1].Available(), has(res.Required(), working.PositionPriorYearReturn))
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &notFiled}.facts(t)))
	if res.Value().Options()[1].Available() || res.Value().Target() != 9_000*money.Dollar {
		t.Errorf("not filed: available %v, target %v", res.Value().Options()[1].Available(), res.Value().Target())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &short}.facts(t)))
	if res.Value().Target() != 5_000*money.Dollar || !asked(res.Required(), working.PositionUnsettledRule, "6 months") {
		t.Errorf("a six-month return: target %v, asked %v; want $5,000 and the question", res.Value().Target(), asked(res.Required(), working.PositionUnsettledRule, "6 months"))
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &zeroPartYear}.facts(t)))
	r := res.Value()
	if r.Exempt() || r.Required() || r.Target() != 0 || !asked(res.Required(), working.PositionUnsettledRule, "showed no tax") {
		t.Errorf("no tax last year as a part-year resident: exempt %v, required %v, target %v; want a zero target under line 6, not Exception 3", r.Exempt(), r.Required(), r.Target())
	}
}

// Exception 3: no liability for a twelve-month year as a resident throughout.
func TestEstimatedNoLiabilityLastYear(t *testing.T) {
	unanswered := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(0).Build())).Build())
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	notRequiredAway := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(false).Build())

	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0)}.facts(t)))
	if !res.Value().Exempt() {
		t.Errorf("no liability as a resident: %s", res.Value().Why())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &unanswered}.facts(t)))
	if res.Value().Exempt() || !has(res.Required(), working.PositionResidency) {
		t.Errorf("residence not given: exempt %v, asked %v", res.Value().Exempt(), has(res.Required(), working.PositionResidency))
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &notRequired}.facts(t)))
	if !res.Value().Exempt() || !asked(res.Required(), working.PositionJurisdictionFact, "weren't required to file") {
		t.Errorf("not required to file, resident: exempt %v, default stated %v", res.Value().Exempt(), asked(res.Required(), working.PositionJurisdictionFact, "weren't required to file"))
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &notRequiredAway}.facts(t)))
	if res.Value().Exempt() || res.Value().Target() != 9_000*money.Dollar {
		t.Errorf("not required to file, not resident: exempt %v, target %v; want 90%%", res.Value().Exempt(), res.Value().Target())
	}
}

// Exception 2, as the form applies it: a return filed and paid by January 31
// takes the whole year out of the penalty. January 31, 2027 is a Sunday and is
// not moved, so a return on Monday, February 1 is charged, up to that day, and
// the question is reported.
func TestEstimatedReturnFiledByJanuary31(t *testing.T) {
	for _, filed := range []estimated.Date{day(2027, 1, 29), day(2027, 1, 31)} {
		h := household{tax: 10_000 * money.Dollar, returnFiled: filed}
		r := must(rules(t).Requirement(h.facts(t))).Value()
		if !r.Exempt() {
			t.Errorf("filed %v: not exempt", filed)
		}
		// The exception is the chosen option, so nothing is needed by any date.
		if c, _ := r.Chosen(); c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Return filed and paid by the end of January" {
			t.Errorf("filed %v: chosen %v %q at %v", filed, c.Basis(), c.Label(), c.Amount())
		}
		for i, in := range must(rules(t).Schedule(h.facts(t))).Value() {
			if in.Status() != estimated.StatusNothingDue {
				t.Errorf("filed %v: installment %d is %v, want nothing due", filed, i+1, in.Status())
			}
		}
		if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().Total() != 0 || p.Value().Reason() != "Return filed and paid by the end of January." {
			t.Errorf("filed %v: penalty %v because %q", filed, p.Value().Total(), p.Value().Reason())
		}
	}
	h := household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 1)}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if res.Value().Total() == 0 || res.Value().AsOf() != day(2027, 2, 1) {
		t.Errorf("filed February 1: penalty %v to %v, want a penalty to February 1", res.Value().Total(), res.Value().AsOf())
	}
	if !asked(res.Required(), working.PositionUnsettledRule, "Sunday") {
		t.Error("filed February 1: the moved date is not reported")
	}
}

// Exceptions 4 and 5 are the Commissioner's to grant: the penalty is shown
// before the waiver, which is carried with it.
func TestEstimatedWaiversAreTheCommissioners(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked about")
	}
	total := res.Value().Total()
	for _, k := range []estimated.WaiverKind{estimated.WaiverCasualty, estimated.WaiverRetiredOrDisabled} {
		h.waiver = &k
		res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if res.Value().Waiver() != k || res.Value().Total() != total || has(res.Required(), working.PositionPenaltyWaiver) {
			t.Errorf("%v: waiver %v, total %v; want %v before the waiver", k, res.Value().Waiver(), res.Value().Total(), total)
		}
	}
}

// AR2210A, worked by hand. $10,000 of tax, so line 26 is a quarter of
// $9,000, $2,250. Annualised tax of $2,000, $4,000, $8,000 and $10,000:
//
//	line 21: 22.5%, 45%, 67.5%, 90% of it:       $450, $1,800, $5,400, $9,000
//	line 23: less earlier installments:           $450, $1,350, $3,600, $3,600
//	line 28: $2,250 plus what line 29 carried:  $2,250, $4,050, $4,950, $3,600
//	line 30: the smaller:                         $450, $1,350, $3,600, $3,600
//
// $6,000 withheld is $1,500 a quarter. April and June are met. September is
// $900 short, paid January 15 after 122 days: $900 × 122 × .00027397 =
// $30.08, so $30. January is $3,000 short to April 15, 90 days: $73.97, so
// $74. $104 in all.
func TestEstimatedAnnualisedMethod(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, withheld: 6_000 * money.Dollar,
		annualised: []money.USCents{2_000 * money.Dollar, 4_000 * money.Dollar, 8_000 * money.Dollar, 10_000 * money.Dollar}}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	wantNeeded := []money.USCents{450 * money.Dollar, 1_800 * money.Dollar, 5_400 * money.Dollar, 9_000 * money.Dollar}
	wantShort := []money.USCents{0, 0, 900 * money.Dollar, 3_000 * money.Dollar}
	for i, in := range sched {
		if in.NeededBy() != wantNeeded[i] || in.Shortfall() != wantShort[i] {
			t.Errorf("installment %d needs %v and is %v short; want %v and %v", i+1, in.NeededBy(), in.Shortfall(), wantNeeded[i], wantShort[i])
		}
	}
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	lines := res.Value().Lines()
	if len(lines) != 2 || lines[0].Parts()[0].Charge() != 30*money.Dollar || lines[1].Parts()[0].Charge() != 74*money.Dollar {
		t.Errorf("annualised penalty lines %+v, want $30 and $74", lines)
	}
	if res.Value().Total() != 104*money.Dollar {
		t.Errorf("annualised penalty %v, want $104", res.Value().Total())
	}
	for _, l := range lines {
		if r := l.Parts()[0].Rate(); r != money.Percent(10) {
			t.Errorf("line %d stands for %v a year, want the 10%% the factor is worked from", l.Installment(), r)
		}
	}

	// All the income in the last period, whose annualised tax is $8,000:
	// line 21 is 90% of it, $7,200, under line 28's $9,000, so nothing is due
	// until January and then $7,200.
	h.annualised = []money.USCents{0, 0, 0, 8_000 * money.Dollar}
	sched = must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{0, 0, 0, 7_200 * money.Dollar} {
		if sched[i].NeededBy() != want {
			t.Errorf("all income late: installment %d needs %v, want %v", i+1, sched[i].NeededBy(), want)
		}
	}

	// A period that does not end where AR2210A's does is refused.
	f := must(estimated.NewFacts(ar, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(100).Build())).
		Periods(must(estimated.NewPeriod().End(day(2026, 3, 30)).Build()), must(estimated.NewPeriod().End(day(2026, 5, 31)).Build()),
			must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).Build())
	if _, err := rules(t).Requirement(f); err == nil {
		t.Error("a first period ending March 30 is accepted")
	}
}

// A requirement first met in July still needs a quarter on every date under
// line 7; the divergence reports the law's reading.
func TestEstimatedLateStarterStillOwesAQuarterADate(t *testing.T) {
	sched := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, firstMet: day(2026, 7, 1)}.facts(t))).Value()
	for i, in := range sched {
		if want := money.USCents(i+1) * 2_250 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
}

// Each divergence is reported with both readings and both sources, follows
// the form, and says whether it changes these facts.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	want := map[string][2]string{
		"When a penalty can arise":               {"$1,000 or less", "paragraph 1(a)"},
		"When estimated payments count as paid":  {"four equal parts", "paragraph 2(b)"},
		"The prior-year figure":                  {"with no condition", "paragraph 1(a)"},
		"A return filed and paid by January 31":  {"whole year", "Exceptions, paragraph 2"},
		"A requirement first met after April 15": {"a quarter of the required annual payment in every column", "paragraph 1(d)"},
		"The daily penalty factor on AR2210A":    {".00027397", "paragraph 2(b)"},
	}
	check := func(name string, ds []working.Divergence, n int, affects map[string]bool) {
		if len(ds) != n {
			t.Errorf("%s: %d divergences, want %d", name, len(ds), n)
		}
		for _, d := range ds {
			w, ok := want[d.Rule()]
			if !ok {
				t.Errorf("%s: unexpected divergence %q", name, d.Rule())
				continue
			}
			if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.StatuteSource(), w[1]) ||
				!strings.Contains(d.StatuteSource(), "not retrieved") || d.FormSource() == "" || d.Statute() == "" {
				t.Errorf("%s: %q does not pin both readings: form %q, statute %q (%s)", name, d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
			}
			if d.Uses() != working.ReadingForm || d.AffectsHousehold() != affects[d.Rule()] {
				t.Errorf("%s: %q uses %v and affects these facts %v; want the form and %v", name, d.Rule(), d.Uses(), d.AffectsHousehold(), affects[d.Rule()])
			}
		}
	}
	// $10,000 of tax, nothing paid, no return on record: none of these rules
	// changes the figures except the daily factor, which charges every
	// shortfall.
	check("regular", must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t))).Divergences(), 5, nil)
	// No annualised tax in any period: nothing is needed by any date, so
	// nothing is short and the factor changes nothing.
	h := household{tax: 10_000 * money.Dollar, withheld: 8_000 * money.Dollar, annualised: []money.USCents{0, 0, 0, 0}}
	check("annualised, met", must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Divergences(), 6, nil)
	h = household{tax: 10_000 * money.Dollar, withheld: 6_000 * money.Dollar,
		annualised: []money.USCents{2_000 * money.Dollar, 4_000 * money.Dollar, 8_000 * money.Dollar, 10_000 * money.Dollar}}
	check("annualised, short", must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Divergences(), 6, map[string]bool{"The daily penalty factor on AR2210A": true})
	paid := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 6, 1), 2_000*money.Dollar)}, firstMet: day(2026, 7, 1)}
	check("paid, late start", must(rules(t).Requirement(paid.facts(t))).Divergences(), 5,
		map[string]bool{"When estimated payments count as paid": true, "A requirement first met after April 15": true})
	early := household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 1, 29)}
	check("filed early", must(rules(t).Requirement(early.facts(t))).Divergences(), 5, map[string]bool{"A return filed and paid by January 31": true})
	edge := household{tax: 200_000, withheld: 100_000}
	check("exactly $1,000", must(rules(t).Requirement(edge.facts(t))).Divergences(), 5, map[string]bool{"When a penalty can arise": true})
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, y := range []int{2025, 2027} {
		if _, err := arkansastaxcalc.EstimatedTax(y); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", y, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	farmer := must(estimated.NewFacts(ar, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(6_667).Build())
	if _, err := rules(t).Requirement(farmer); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer: %v, want ErrUnsupported", err)
	}
	// Just under two-thirds is not a farmer.
	almost := must(estimated.NewFacts(ar, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(6_666).Build())
	if _, err := rules(t).Requirement(almost); err != nil {
		t.Errorf("66.66%% from farming: %v", err)
	}
	// The AR1000ES's exception is for farming alone.
	fisher := must(estimated.NewFacts(ar, 2026).FilingStatus(filing.Single).ThisYear(this).FishShare(7_000).Build())
	if _, err := rules(t).Requirement(fisher); err != nil {
		t.Errorf("a fisher: %v", err)
	}
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(ar, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"extras":             must(estimated.NewFacts(ar, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Arkansas's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty to no date is accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"$1,000 or less", "fixed 10% a year", "no 110% tier", "January 31", "evenly", "Arkansas residence", "AR2210A", "Commissioner"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if e.VersusFederal() == "" || len(e.Sources()) == 0 {
		t.Error("the explanation has no comparison with federal or no sources")
	}
}

// Without this year's tax the target is last year's, the installments are a
// quarter of it, and the penalty is an upper bound. $4,000 last year, nothing
// paid: each $1,000 quarter runs to April 15, 2027, for 365, 304, 212 and 90
// days: $100, $83.29, $58.08 and $24.66, so $100, $83, $58 and $25, $266.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(4_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 4_000*money.Dollar || !r.TargetKnown() {
		t.Errorf("required %v, target %v: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Error("this year's tax is not asked for")
	}
	for i, in := range must(rules(t).Schedule(h.facts(t))).Value() {
		if want := money.USCents(i+1) * 1_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	pen := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if !pen.Value().UpperBound() || pen.Value().Total() != 266*money.Dollar {
		t.Errorf("penalty %v, upper bound %v; want $266 as an upper bound", pen.Value().Total(), pen.Value().UpperBound())
	}

	// With no return either, there is no target.
	none := household{noTax: true}
	if r := must(rules(t).Requirement(none.facts(t))).Value(); r.TargetKnown() || r.Required() {
		t.Errorf("no tax and no return: target known %v, required %v", r.TargetKnown(), r.Required())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}

	// Exception 3 needs nothing about this year, so its zero is exact.
	exempt := household{noTax: true, prior: prior(0)}
	pen = must(rules(t).Penalty(exempt.facts(t), day(2027, 4, 15)))
	if pen.Value().Total() != 0 || pen.Value().UpperBound() {
		t.Errorf("no liability last year: penalty %v, upper bound %v; want an exact zero", pen.Value().Total(), pen.Value().UpperBound())
	}
}

// An exception sets the target, so no question may say its answer changes
// the target. A return filed and paid by Jan 31, 2027, with this year's tax
// and last year's return not given.
func TestEstimatedExceptionAsksNothingOfTheTarget(t *testing.T) {
	b := estimated.NewFacts(estimated.Jurisdiction("AR"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Build())).
		ReturnFiled(day(2027, 1, 20))
	res := must(must(arkansastaxcalc.EstimatedTax(2026)).Requirement(must(b.Build())))
	if !res.Value().Exempt() {
		t.Fatal("no exception applies")
	}
	for _, r := range res.Required() {
		if strings.Contains(strings.ToLower(r.Effect()), "target") {
			t.Errorf("%v: %q", r.Position(), r.Effect())
		}
	}
}

// The waiver asked for is decided from Exceptions 4 and 5 of the AR2210
// instructions. The penalty stays the figure before any waiver, and IfWaived
// shows $0 only when the waiver is open to the facts.

// waiverPenalty is the penalty on $10,000 of tax with nothing paid or
// withheld, figured to Apr 15, 2027, with the waiver asked for as pos.
func waiverPenalty(t *testing.T, pos estimated.Positions) working.Result[estimated.Penalty] {
	t.Helper()
	f := must(estimated.NewFacts(estimated.Jurisdiction("AR"), 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Positions(pos).Build())
	return must(must(arkansastaxcalc.EstimatedTax(2026)).Penalty(f, day(2027, 4, 15)))
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

// Exceptions 4 and 5 are the federal pair, decided by the Commissioner:
// retirement after 62 or disability in the tax year or the year before.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "ar2210i-2025.txt",
		"casualty, disaster, or other unusual circumstances the imposition of such penalty would be against equity and good conscience.",
		"(i) retired after having attained age 62, or (ii) became disabled in the tax year for which such estimated payments were required to be made or in the tax year preceding such tax year, and (b) such underpayment was due to reasonable cause and not to willful neglect.")
	checkWaivers(t, []waiverCase{
		{"retired in 2026 at 64", waiverFor(estimated.CauseRetirement, day(2026, 6, 1), day(1962, 3, 1)), "eligible", "The Commissioner needs"},
		{"retired in 2025 at 65", waiverFor(estimated.CauseRetirement, day(2025, 6, 1), day(1960, 3, 1)), "eligible", "retired in 2025"},
		{"retired in 2020", waiverFor(estimated.CauseRetirement, day(2020, 6, 1), day(1955, 3, 1)), "refused", "2025 or 2026"},
		{"retired at 60", waiverFor(estimated.CauseRetirement, day(2026, 6, 1), day(1966, 3, 1)), "refused", "reached 62"},
		{"retired, no date", waiverFor(estimated.CauseRetirement, estimated.Date{}, estimated.Date{}), "undecided", "the date you retired"},
		{"disabled in 2026", waiverFor(estimated.CauseDisability, day(2026, 2, 1), estimated.Date{}), "eligible", "disabled in 2026"},
		{"a casualty", waiverFor(estimated.CauseCasualty, day(2026, 8, 1), estimated.Date{}), "eligible", "a casualty"},
	})
}
