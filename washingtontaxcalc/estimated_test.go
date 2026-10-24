// Washington's capital gains tax payment rules for 2026, checked against the
// state's own documents.
//
// Washington publishes no worked example with figures for these penalties.
// The oracles are the statute's own arithmetic, a percentage of the late tax
// and a yearly interest rate, and WAC 458-20-300 Examples 3 and 4, which
// state a conclusion without a figure. Every date, percentage, amount and
// rate is compared against its extract in testdata/. The boundaries, the
// exceptions and the open questions are hand-written cases.
package washingtontaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/washingtontaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

const wa = estimated.Jurisdiction("WA")

var day = estimated.MustDate

// extract is a testdata file with its whitespace collapsed, so a phrase
// matches across the line breaks pdftotext puts in.
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
	return must(washingtontaxcalc.EstimatedTax(2026))
}

func asked(rs []working.Required, k working.PositionKind, about string) bool {
	for _, r := range rs {
		if r.Position() == k && r.Asked() && strings.Contains(r.Question()+" "+r.Effect(), about) {
			return true
		}
	}
	return false
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

var (
	yes, no = true, false
	due     = day(2027, 4, 15)
)

type household struct {
	tax         money.USCents
	noTax       bool
	payments    []estimated.Payment
	extension   *bool // nil leaves it unanswered
	returnFiled estimated.Date
	waiver      *estimated.WaiverKind // nil answers "no waiver"
	noWaiver    bool                  // leaves the waiver unanswered
	positions   *estimated.Positions  // replaces waiver when set
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	w := estimated.WaiverNone
	if h.waiver != nil {
		w = *h.waiver
	}
	b := estimated.NewFacts(wa, 2026).FilingStatus(filing.Single).ThisYear(figures).
		Paid(h.payments...).ReturnFiled(h.returnFiled)
	switch {
	case h.positions != nil:
		b.Positions(*h.positions)
	case !h.noWaiver:
		b.Positions(must(estimated.NewPositions().Waiver(w).Build()))
	}
	if h.extension != nil {
		b.State(must(washingtontaxcalc.NewEstimatedFacts().Extension(*h.extension).Build()))
	}
	return must(b.Build())
}

// penalty figures the penalty and splits it into the late payment or low
// estimate penalty, and interest.
func penalty(t *testing.T, h household, asOf estimated.Date) (flat, interest money.USCents, res working.Result[estimated.Penalty]) {
	t.Helper()
	res = must(rules(t).Penalty(h.facts(t), asOf))
	for _, l := range res.Value().Lines() {
		for _, p := range l.Parts() {
			switch p.Kind() {
			case estimated.PartFlat:
				flat += p.Charge()
			case estimated.PartInterest:
				interest += p.Charge()
			default:
				t.Errorf("unexpected %v part", p.Kind())
			}
		}
	}
	if flat+interest != res.Value().Total() {
		t.Errorf("parts add to %v, total is %v", flat+interest, res.Value().Total())
	}
	return flat, interest, res
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "dor-wa-prepayment-2026.txt",
		"Issue Date August 03, 2026",
		"Starting with tax year 2026, you may prepay your capital gains tax up to six months before the filing and payment due date.",
		"Payments are typically due on April 15th, unless this falls on a weekend or holiday. For tax year 2026, you may submit your payment beginning Oct. 15, 2026, for returns due on April 15th, 2027.",
		"You will not be able to make a prepayment using tax preparation software.",
	)
	mustContain(t, "rcw-82-87-extract.txt",
		"must file, on forms prescribed by the department, a return with the department on or before the date the taxpayer's federal income tax return for the taxable year is required to be filed",
		"must, without assessment, notice, or demand, pay any tax due thereon to the department on or before the date fixed for the filing of the return, regardless of any filing extension.",
		"If any tax due under this chapter is not paid by the due date, interest and penalties as provided in chapter 82.32 RCW apply to the deficiency.",
		"An extension under this subsection for the filing of a return under this chapter is not an extension of time to pay the tax due under this chapter.",
		"five percent of the tax due for the taxable year covered by the return for each month or portion of a month that the return remains unfiled. The total penalty assessed under this subsection may not exceed 25 percent",
		"(7) The department must waive or cancel the penalty imposed under RCW 82.32.090(1) on a payment required under this section",
		"The taxpayer has timely remitted payment on all tax returns due under this section during the preceding five calendar years.",
		"may submit payment of the tax beginning six months before the due date for filing and payment for that taxable year.",
	)
	mustContain(t, "rcw-82-32-090.txt",
		"is not received by the department of revenue by the due date, there is assessed a penalty of nine percent of the amount of the tax; and if the tax is not received on or before the last day of the month following the due date, there is assessed a total penalty of 19 percent of the amount of the tax under this subsection; and if the tax is not received on or before the last day of the second month following the due date, there is assessed a total penalty of 29 percent of the amount of the tax under this subsection. No penalty so added may be less than $5.",
		"(10) If a taxpayer substantially underpays an estimated payment of tax imposed under RCW 82.87.040 pursuant to RCW 82.87.110(3), there is assessed a penalty of five percent of the amount of the actual tax due for tax imposed under RCW 82.87.040.",
		"for taxes imposed under RCW 82.87.040 was less than 80 percent of the actual tax due, and at least $1,000.",
		"the amount of underpayment is at least $1,000.",
		`"Return" also includes the submission of any estimated payment of tax as provided in RCW 82.87.110(3) and the confirmation of an extension of the filing due date required under RCW 82.87.110(5).`,
	)
	mustContain(t, "rcw-82-32-050.txt",
		"shall add thereto interest on the tax only.",
		"(ii) For interest associated with annual tax reporting periods having a due date as prescribed in RCW 82.32.045(3), 82.87.110, and 82A.04.550, interest must be computed from the last day of April immediately following each such annual reporting period included in the notice",
		"an average of the federal short-term rate as defined in 26 U.S.C. Sec. 1274(d) plus two percentage points.",
		"The rate so computed shall be adjusted on the first day of January of each year for use in computing interest for that calendar year.",
	)
	mustContain(t, "dor-wa-interest-rate-tables.txt",
		"Excise tax Calendar year Assessment rate Refund rate 2027 6% 6% 2026 6% 6% 2025 7% 7%")
	mustContain(t, "ssb-5314-2025.txt",
		"EFFECTIVE DATE: July 27, 2025—Except for sections 3, 4, 8, 10, and 15, which take effect January 1, 2026.",
		"Sec. 12. RCW 82.32.050 and 2022 c 282 s",
		"Sec. 14. RCW 82.32.090 and 2015 3rd sp.s. c",
		"(10) If a taxpayer substantially underpays an estimated payment of tax imposed under RCW 82.87.040 pursuant to RCW 82.87.110(3),",
		"(ii) For interest associated with annual tax reporting periods having a due date as prescribed in RCW 82.32.045(3) and 82.87.110, interest must be computed from the last day of April",
	)
	mustContain(t, "rcw-1-16-050.txt",
		"(1) The following are state legal holidays:",
		"(e) The last Monday of May, commonly known as Memorial Day;")
	mustContain(t, "rcw-1-12-040.txt", "unless the last day is a holiday, Saturday, or Sunday, and then it is also excluded.")
	mustContain(t, "rcw-1-12-070.txt", "falls upon a Saturday, Sunday or legal holiday, the filing shall be considered timely if performed on the next business day.")
	mustContain(t, "rcw-82-32-105.txt", "was the result of circumstances beyond the control of the taxpayer, the department must waive or cancel any penalties imposed under this chapter with respect to such tax.")
	mustContain(t, "wac-458-20-228.txt",
		"(a) If the date for payment of the tax due on a tax return falls upon a Saturday, Sunday, or legal holiday, the filing will be considered timely if performed on the next business day.",
		"interest will be added from the last day of the month following each calendar year included in a notice",
		"Certified on 2/29/2024")
	mustContain(t, "wac-458-20-300.txt",
		"The extension of time for filing the federal income tax return or capital gains excise tax return does not extend the due date for paying your capital gains excise tax.",
		"Certified on 8/23/2023")
	mustContain(t, "dor-wa-capital-gains-payments.txt",
		"A filing extension does not extend the due date for submitting a payment.",
		"If you submit an estimated payment by the original filing deadline, you will not owe late payment penalties. However, if you substantially underpaid your tax liability, you will owe the substantial underpayment penalty. Substantially underpaid means you have paid less than 80% of the tax due.")
	mustContain(t, "dor-wa-interim-late-payment-2023.txt",
		"April 7, 2023",
		"you will not be assessed any penalty provided in RCW 82.32.090(1) if it is later determined you paid less than was properly due. However, you will be subject to the substantial underpayment penalty provided in RCW 82.32.090(2)",
		"“Substantially underpaid” means you have paid less than 80% of the tax due and the underpayment is at least $1,000.")
}

// One option, the whole of this year's tax. There is no prior-year safe
// harbor, and any tax at all, down to a cent, is due.
func TestEstimatedRequirementIsTheWholeTax(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 10_000*money.Dollar {
		t.Errorf("required %v, target %v; want the whole $10,000: %s", r.Required(), r.Target(), r.Why())
	}
	opts := r.Options()
	if len(opts) != 2 || opts[0].Basis() != estimated.BasisThisYearTax || opts[0].Percent() != money.Percent(100) || !opts[0].Available() {
		t.Fatalf("options %+v, want 100%% of this year's tax first", opts)
	}
	if opts[1].Basis() != estimated.BasisPriorYearTax || opts[1].Available() || !strings.Contains(opts[1].WhyNot(), "no prior-year safe harbor") {
		t.Errorf("the prior-year option is available %v, %q", opts[1].Available(), opts[1].WhyNot())
	}
	for _, s := range []string{"no quarterly estimated payments", "$10,000", "Apr 15, 2027", "Oct 15"} {
		if !strings.Contains(r.Why(), s) {
			t.Errorf("the reason does not say %q: %s", s, r.Why())
		}
	}

	for _, tc := range []struct {
		tax      money.USCents
		required bool
	}{{0, false}, {1, true}} {
		r := must(rules(t).Requirement(household{tax: tc.tax}.facts(t))).Value()
		if r.Required() != tc.required || r.Target() != tc.tax {
			t.Errorf("tax %v: required %v, target %v; want %v", tc.tax, r.Required(), r.Target(), tc.required)
		}
		// Washington has no exception: even with no tax, the target is the
		// whole of this year's tax.
		if c, ok := r.Chosen(); !ok || r.Exempt() || c.Basis() != estimated.BasisThisYearTax || c.Amount() != tc.tax || c.Label() != "100% of this year's tax" {
			t.Errorf("tax %v: chosen %+v, exempt %v; want this year's tax", tc.tax, c, r.Exempt())
		}
	}
}

// The 2026 tax is due April 15, 2027, a Thursday that is no holiday, and
// payments from October 15, 2026 count toward it.
func TestEstimatedScheduleIsOnePayment(t *testing.T) {
	if due.Weekday() != time.Thursday {
		t.Fatalf("April 15, 2027 is a %v", due.Weekday())
	}
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 10, 15), 4_000*money.Dollar), pay(due, 5_000*money.Dollar), pay(due.AddDays(1), 1_000*money.Dollar)}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	if len(s) != 1 {
		t.Fatalf("%d installments, want one", len(s))
	}
	in := s[0]
	if in.Due() != due || in.NeededBy() != 10_000*money.Dollar || in.CreditedBy() != 9_000*money.Dollar ||
		in.Shortfall() != 1_000*money.Dollar || in.Status() != estimated.StatusShort {
		t.Errorf("due %v, needs %v, credited %v, short %v, %v", in.Due(), in.NeededBy(), in.CreditedBy(), in.Shortfall(), in.Status())
	}
	h.payments = h.payments[:2]
	h.payments = append(h.payments, pay(due, 1_000*money.Dollar))
	if in := must(rules(t).Schedule(h.facts(t))).Value()[0]; in.Status() != estimated.StatusMet || in.Shortfall() != 0 {
		t.Errorf("paid in full: %v, short %v", in.Status(), in.Shortfall())
	}
	if in := must(rules(t).Schedule(household{}.facts(t))).Value()[0]; in.Status() != estimated.StatusNothingDue {
		t.Errorf("no tax: %v", in.Status())
	}
}

// RCW 82.87.115(1) opens payment six months before the due date: October 15,
// 2026 and not a day earlier.
func TestEstimatedPrepaymentOpensOctoberFifteenth(t *testing.T) {
	early := household{tax: 100 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 10, 14), 100*money.Dollar)}}
	if _, err := rules(t).Requirement(early.facts(t)); err == nil {
		t.Error("a payment on October 14, 2026 was accepted")
	}
	onTime := household{tax: 100 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 10, 15), 100*money.Dollar)}}
	if _, err := rules(t).Requirement(onTime.facts(t)); err != nil {
		t.Errorf("a payment on October 15, 2026: %v", err)
	}
}

// rcw1_16_050 is the state legal holidays for 2027, worked out here from the
// rules RCW 1.16.050(1) and (5) state rather than read from the package.
func rcw1_16_050(year int) map[estimated.Date]string {
	nth := func(m time.Month, wd time.Weekday, n int) estimated.Date {
		d := day(year, m, 1)
		for d.Weekday() != wd {
			d = d.AddDays(1)
		}
		return d.AddDays(7 * (n - 1))
	}
	lastMonday := day(year, time.May, 31)
	for lastMonday.Weekday() != time.Monday {
		lastMonday = lastMonday.AddDays(-1)
	}
	observed := func(d estimated.Date) estimated.Date {
		switch d.Weekday() {
		case time.Sunday:
			return d.AddDays(1)
		case time.Saturday:
			return d.AddDays(-1)
		}
		return d
	}
	thanksgiving := nth(time.November, time.Thursday, 4)
	return map[estimated.Date]string{
		observed(day(year, 1, 1)):           "New Year's Day",
		nth(time.January, time.Monday, 3):   "Martin Luther King, Jr.",
		nth(time.February, time.Monday, 3):  "Presidents' Day",
		lastMonday:                          "Memorial Day",
		observed(day(year, 6, 19)):          "Juneteenth",
		observed(day(year, 7, 4)):           "Independence Day",
		nth(time.September, time.Monday, 1): "Labor Day",
		observed(day(year, 11, 11)):         "Veterans Day",
		thanksgiving:                        "Thanksgiving Day",
		thanksgiving.AddDays(1):             "Native American Heritage Day",
		observed(day(year, 12, 25)):         "Christmas Day",
	}
}

// Of the 2027 holidays, Memorial Day alone falls on a date the rules use:
// May 31, the last day of the month after the due date. The step to 19%
// moves to June 1, so a payment that day is still at 9%, and the rules say
// that reading is an open question.
func TestEstimatedMemorialDayMovesTheSecondStep(t *testing.T) {
	holidays := rcw1_16_050(2027)
	for _, d := range []estimated.Date{due, day(2027, 5, 31), day(2027, 6, 30)} {
		name, ok := holidays[d]
		if ok != (d == day(2027, 5, 31)) || (ok && name != "Memorial Day") {
			t.Errorf("%v is holiday %v (%q)", d, ok, name)
		}
	}
	for _, tc := range []struct {
		paid  estimated.Date
		flat  money.USCents
		asked bool
	}{
		{day(2027, 5, 31), 900 * money.Dollar, false},
		{day(2027, 6, 1), 900 * money.Dollar, true},
		{day(2027, 6, 2), 1_900 * money.Dollar, false},
	} {
		h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(tc.paid, 10_000*money.Dollar)}}
		flat, _, res := penalty(t, h, day(2027, 12, 31))
		if flat != tc.flat {
			t.Errorf("paid %v: late payment penalty %v, want %v", tc.paid, flat, tc.flat)
		}
		if got := asked(res.Required(), working.PositionUnsettledRule, "is a legal holiday"); got != tc.asked {
			t.Errorf("paid %v: the holiday question asked %v, want %v", tc.paid, got, tc.asked)
		}
	}
}

// 9% the day after the due date, 19% after June 1 and 29% after June 30, on
// $10,000 never paid. Interest starts only after April 30.
func TestEstimatedLatePaymentSteps(t *testing.T) {
	for _, tc := range []struct {
		asOf estimated.Date
		flat money.USCents
	}{
		{due, 0},
		{due.AddDays(1), 900 * money.Dollar},
		{day(2027, 6, 1), 900 * money.Dollar},
		{day(2027, 6, 2), 1_900 * money.Dollar},
		{day(2027, 6, 30), 1_900 * money.Dollar},
		{day(2027, 7, 1), 2_900 * money.Dollar},
		{day(2027, 12, 31), 2_900 * money.Dollar},
	} {
		flat, _, res := penalty(t, household{tax: 10_000 * money.Dollar}, tc.asOf)
		if flat != tc.flat {
			t.Errorf("to %v: late payment penalty %v, want %v", tc.asOf, flat, tc.flat)
		}
		if tc.flat == 0 && len(res.Value().Lines()) != 0 {
			t.Errorf("to %v: %d lines before anything is late", tc.asOf, len(res.Value().Lines()))
		}
	}
}

// RCW 82.32.090(1): no late payment penalty is less than $5. 9% of $55.44 is
// $4.99, raised to $5.00; of $55.56, $5.00; of $55.67, $5.01.
func TestEstimatedLatePaymentMinimum(t *testing.T) {
	for _, tc := range []struct{ tax, flat money.USCents }{{5_544, 500}, {5_556, 500}, {5_567, 501}} {
		flat, _, _ := penalty(t, household{tax: tc.tax}, due.AddDays(1))
		if flat != tc.flat {
			t.Errorf("%v late: penalty %v, want %v", tc.tax, flat, tc.flat)
		}
	}
}

// $6,000 of a $10,000 tax paid May 10 and the rest never paid. 9% of
// $10,000, then 10% and 10% more of the $4,000 still out: $1,700. The rules
// ask whether the later steps reach the part already paid.
func TestEstimatedPartPaymentBetweenSteps(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(day(2027, 5, 10), 6_000*money.Dollar)}}
	flat, interest, res := penalty(t, h, day(2027, 7, 1))
	if flat != 1_700*money.Dollar {
		t.Errorf("late payment penalty %v, want $1,700", flat)
	}
	// $6,000 for the 10 days to May 10, $9.86; $4,000 for the 62 days to
	// July 1, $40.77; both at 6% over 365 days.
	if interest != 986+4_077 {
		t.Errorf("interest %v, want $50.63", interest)
	}
	if !asked(res.Required(), working.PositionUnsettledRule, "part payment") {
		t.Error("the part payment question is not asked")
	}
	if _, _, res := penalty(t, household{tax: 10_000 * money.Dollar}, day(2027, 7, 1)); asked(res.Required(), working.PositionUnsettledRule, "part payment") {
		t.Error("the part payment question is asked with nothing paid")
	}
}

// RCW 82.32.050(1)(c)(ii): interest from the last day of April, by the day.
// Paid April 30, none; paid May 1, one day on $10,000 at 6%, $1.64.
func TestEstimatedInterestStartsAfterAprilThirty(t *testing.T) {
	for _, tc := range []struct {
		paid     estimated.Date
		interest money.USCents
	}{{day(2027, 4, 30), 0}, {day(2027, 5, 1), 164}} {
		h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(tc.paid, 10_000*money.Dollar)}}
		_, interest, res := penalty(t, h, day(2027, 12, 31))
		if interest != tc.interest {
			t.Errorf("paid %v: interest %v, want %v", tc.paid, interest, tc.interest)
		}
		if got := asked(res.Required(), working.PositionUnsettledRule, "how days are counted"); got != (tc.interest > 0) {
			t.Errorf("paid %v: the day-count question asked %v", tc.paid, got)
		}
	}
}

// No published rate change falls in the 2026 penalty window: 2026 and 2027
// are both 6%, and 2028's is not out. The interest stops at December 31,
// 2027 and says so. $10,000 for the 245 days from April 30 is $402.74.
func TestEstimatedInterestStopsWhereTheRatesEnd(t *testing.T) {
	for _, asOf := range []estimated.Date{day(2027, 12, 31), day(2028, 3, 31)} {
		flat, interest, res := penalty(t, household{tax: 10_000 * money.Dollar}, asOf)
		if flat != 2_900*money.Dollar || interest != 40_274 {
			t.Errorf("to %v: penalty %v and interest %v, want $2,900 and $402.74", asOf, flat, interest)
		}
		var parts []estimated.PenaltyPart
		for _, p := range res.Value().Lines()[0].Parts() {
			if p.Kind() == estimated.PartInterest {
				parts = append(parts, p)
			}
		}
		if len(parts) != 1 || parts[0].To() != day(2027, 12, 31) || parts[0].Days() != 245 || parts[0].Rate() != money.Percent(6) {
			t.Errorf("to %v: interest parts %+v, want one of 245 days at 6%% to December 31, 2027", asOf, parts)
		}
		if got := asked(res.Required(), working.PositionUnpublishedRate, "Dec 31, 2027"); got != (asOf.Year() == 2028) {
			t.Errorf("to %v: the unpublished 2028 rate reported %v", asOf, got)
		}
	}
}

// With an extension, an estimate paid by April 15 avoids the late payment
// penalty. Paying 80% of the tax avoids the 5% penalty too; a cent less owes
// 5% of the whole $10,000. The balance, paid with the return on October 15,
// bears interest for the 168 days from April 30 either way.
func TestEstimatedExtensionWithATimelyEstimate(t *testing.T) {
	rest := func(estimate money.USCents) household {
		return household{tax: 10_000 * money.Dollar, extension: &yes, returnFiled: day(2027, 10, 15),
			payments: []estimated.Payment{pay(due, estimate)}}
	}
	for _, tc := range []struct {
		estimate, flat, interest money.USCents
	}{
		{8_000 * money.Dollar, 0, 5_523},
		{799_999, 500 * money.Dollar, 5_523},
	} {
		flat, interest, res := penalty(t, rest(tc.estimate), day(2027, 12, 31))
		if flat != tc.flat || interest != tc.interest {
			t.Errorf("estimate %v: penalty %v and interest %v, want %v and %v", tc.estimate, flat, interest, tc.flat, tc.interest)
		}
		if res.Value().AsOf() != day(2027, 10, 15) {
			t.Errorf("figured to %v, want the day the return was filed", res.Value().AsOf())
		}
		if asked(res.Required(), working.PositionJurisdictionFact, "extension") {
			t.Error("the extension is asked although it was answered")
		}
	}

	// Without an extension the same payments are late: 9% of the $2,000,
	// then 10% and 10% more, $580.
	h := rest(8_000 * money.Dollar)
	h.extension = &no
	if flat, _, _ := penalty(t, h, day(2027, 12, 31)); flat != 580*money.Dollar {
		t.Errorf("no extension: penalty %v, want $580", flat)
	}
	h.extension = nil
	if flat, _, res := penalty(t, h, day(2027, 12, 31)); flat != 580*money.Dollar || !asked(res.Required(), working.PositionJurisdictionFact, "extension") {
		t.Errorf("extension not said: penalty %v, asked %v; want $580 and the question", flat, asked(res.Required(), working.PositionJurisdictionFact, "extension"))
	}
	// An extension with nothing paid by April 15 is late in full.
	if flat, _, _ := penalty(t, household{tax: 10_000 * money.Dollar, extension: &yes}, day(2027, 12, 31)); flat != 2_900*money.Dollar {
		t.Errorf("extension, nothing paid: penalty %v, want $2,900", flat)
	}
}

// The 5% penalty needs the estimate to be $1,000 or more short, read as the
// shortfall. $3,000 of $4,000 owes 5% of $4,000, $200; a cent more does not.
// Where the two readings of "at least $1,000" part, the rules ask.
func TestEstimatedThousandDollarFloor(t *testing.T) {
	for _, tc := range []struct {
		tax, estimate, flat money.USCents
		asked               bool
	}{
		{4_000 * money.Dollar, 3_000 * money.Dollar, 200 * money.Dollar, false},
		{4_000 * money.Dollar, 300_001, 0, true},
		{5_000 * money.Dollar, 500 * money.Dollar, 250 * money.Dollar, true},
		{1_200 * money.Dollar, 900 * money.Dollar, 0, false},
	} {
		h := household{tax: tc.tax, extension: &yes, payments: []estimated.Payment{pay(due, tc.estimate)}}
		flat, _, res := penalty(t, h, day(2027, 4, 20))
		if flat != tc.flat {
			t.Errorf("%v of %v: penalty %v, want %v", tc.estimate, tc.tax, flat, tc.flat)
		}
		if got := asked(res.Required(), working.PositionUnsettledRule, "at least $1,000"); got != tc.asked {
			t.Errorf("%v of %v: the $1,000 question asked %v, want %v", tc.estimate, tc.tax, got, tc.asked)
		}
	}
}

// WAC 458-20-300 Examples 3 and 4, moved to 2026's dates. Both state only
// that the filer owes penalties and interest. Jeannette files on time and
// pays five days late: 9%, and under the 2025 act no interest, because it
// starts after April 30. Gil has an extension and pays nothing until he
// files on October 13: 29% and interest.
func TestEstimatedWAC45820300Examples(t *testing.T) {
	mustContain(t, "wac-458-20-300.txt",
		"Example 3 - Late payment - No federal extension Facts: Jeannette filed her federal income tax return on April 18, 2023. Jeannette files a capital gains excise tax return on April 18, 2023. She later remits her capital gains excise tax to the department on April 20, 2023.",
		"Jeannette paid the capital gains excise tax late and is subject to penalties and interest.",
		"Gil files a capital gains excise tax return and pays his capital gains excise tax on October 13, 2023.",
		"Gil paid his capital gains excise tax late and is subject to penalties and interest with respect to the late payment.")

	jeannette := household{tax: 10_000 * money.Dollar, extension: &no, payments: []estimated.Payment{pay(day(2027, 4, 20), 10_000*money.Dollar)}}
	if flat, interest, _ := penalty(t, jeannette, day(2027, 12, 31)); flat != 900*money.Dollar || interest != 0 {
		t.Errorf("Jeannette: penalty %v, interest %v; want $900 and none", flat, interest)
	}
	gil := household{tax: 10_000 * money.Dollar, extension: &yes, returnFiled: day(2027, 10, 13),
		payments: []estimated.Payment{pay(day(2027, 10, 13), 10_000*money.Dollar)}}
	// 166 days from April 30 to October 13 on $10,000 at 6%: $272.88.
	if flat, interest, _ := penalty(t, gil, day(2027, 12, 31)); flat != 2_900*money.Dollar || interest != 27_288 {
		t.Errorf("Gil: penalty %v, interest %v; want $2,900 and $272.88", flat, interest)
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][4]string{
		"Late payment penalty after a timely estimated payment": {"no late payment penalty", "Interim statement", "apply to the balance", "RCW 82.87.110(3)"},
		"The penalty on a low estimated payment":                {"assessment penalty", "April 7, 2023", "5% of the actual tax due", "July 27, 2025"},
		"When interest starts":                                  {"Jan 31", "2/29/2024", "last day of the next April", "July 27, 2025"},
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
	}

	// Which reading each follows, and which these facts reach. Nothing paid
	// and no extension: only the start of interest matters. An extension
	// with 90% paid by the due date reaches the first too, and a payment
	// under 80% the second.
	uses := map[string]working.Reading{
		"Late payment penalty after a timely estimated payment": working.ReadingForm,
		"The penalty on a low estimated payment":                working.ReadingStatute,
		"When interest starts":                                  working.ReadingStatute,
	}
	for _, tc := range []struct {
		name    string
		h       household
		affects [3]bool
	}{
		{"nothing paid", household{tax: 10_000 * money.Dollar}, [3]bool{false, false, true}},
		{"paid in full", household{tax: 10_000 * money.Dollar, extension: &yes, payments: []estimated.Payment{pay(due, 10_000*money.Dollar)}}, [3]bool{false, false, false}},
		{"extension, 90%", household{tax: 10_000 * money.Dollar, extension: &yes, payments: []estimated.Payment{pay(due, 9_000*money.Dollar)}}, [3]bool{true, false, true}},
		{"extension, 70%", household{tax: 10_000 * money.Dollar, extension: &yes, payments: []estimated.Payment{pay(due, 7_000*money.Dollar)}}, [3]bool{true, true, true}},
	} {
		ds := must(rules(t).Requirement(tc.h.facts(t))).Divergences()
		for i, d := range ds {
			if d.Uses() != uses[d.Rule()] || d.AffectsHousehold() != tc.affects[i] {
				t.Errorf("%s: %q uses %v and affects %v; want %v and %v", tc.name, d.Rule(), d.Uses(), d.AffectsHousehold(), uses[d.Rule()], tc.affects[i])
			}
		}
	}

	// The figures follow the department on the first and the statute on the
	// other two. The department's reading would charge a late payment penalty
	// for the first only if the extension were ignored; the statute's
	// interest starts April 30, not January 31 or April 15.
	h := household{tax: 10_000 * money.Dollar, extension: &yes, payments: []estimated.Payment{pay(due, 9_000*money.Dollar), pay(day(2027, 4, 30), 1_000*money.Dollar)}}
	if flat, interest, _ := penalty(t, h, day(2027, 12, 31)); flat != 0 || interest != 0 {
		t.Errorf("timely 90%% estimate, balance April 30: penalty %v and interest %v, want none", flat, interest)
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, year := range []int{2025, 2027} {
		if _, err := washingtontaxcalc.EstimatedTax(year); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", year, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	base := func(j estimated.Jurisdiction, year int) *estimated.FactsBuilder {
		return estimated.NewFacts(j, year).FilingStatus(filing.Single).ThisYear(this)
	}
	prior := must(estimated.NewPayment().FromPriorYear().Amount(100).Build())
	if _, err := rules(t).Requirement(must(base(wa, 2026).Paid(prior).Build())); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("last year's overpayment applied forward: %v, want ErrUnsupported", err)
	}
	periods := []estimated.Period{
		must(estimated.NewPeriod().End(day(2026, 3, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 5, 31)).Build()),
		must(estimated.NewPeriod().End(day(2026, 8, 31)).Build()), must(estimated.NewPeriod().End(day(2026, 12, 31)).Build()),
	}
	for name, f := range map[string]estimated.Facts{
		"Pennsylvania's facts": must(base(estimated.Pennsylvania, 2026).Build()),
		"2027":                 must(base(wa, 2027).Build()),
		"other extras":         must(base(wa, 2026).State("not Washington's").Build()),
		"withholding":          must(base(wa, 2026).Withheld(must(estimated.NewWithheld().On(day(2026, 6, 1)).Amount(100).Build())).Build()),
		"annualised":           must(base(wa, 2026).Elections(must(estimated.NewElections().Annualised().Build())).Periods(periods...).Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil || errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want a refusal", name, err)
		}
	}
	// Washington has no separate rules for farmers.
	if _, err := rules(t).Requirement(must(base(wa, 2026).FarmShare(10_000).Build())); err != nil {
		t.Errorf("a farmer: %v", err)
	}
}

// Without this year's tax there is no target: Washington has no prior-year
// figure to fall back on.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	f := household{noTax: true}.facts(t)
	res := must(rules(t).Requirement(f))
	r := res.Value()
	if r.TargetKnown() || r.Required() || r.Target() != 0 {
		t.Errorf("known %v, required %v, target %v; want no target", r.TargetKnown(), r.Required(), r.Target())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !asked(res.Required(), working.PositionThisYearTax, "") || !asked(res.Required(), working.PositionJurisdictionFact, "extension") {
		t.Errorf("this year's tax and the extension are not both asked: %v", res.Required())
	}
	if _, err := rules(t).Schedule(f); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(f, day(2027, 12, 31)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// The penalty is shown before any waiver. Unanswered, the waiver is asked;
// answered, it is carried and the total does not move.
func TestEstimatedWaiverIsReportedNotApplied(t *testing.T) {
	casualty := estimated.WaiverCasualty
	_, _, res := penalty(t, household{tax: 10_000 * money.Dollar, noWaiver: true}, day(2027, 5, 1))
	if !asked(res.Required(), working.PositionPenaltyWaiver, "five on-time years") {
		t.Error("the waiver is not asked")
	}
	_, _, waived := penalty(t, household{tax: 10_000 * money.Dollar, waiver: &casualty}, day(2027, 5, 1))
	if waived.Value().Total() != res.Value().Total() || waived.Value().Waiver() != estimated.WaiverCasualty {
		t.Errorf("with a casualty waiver: total %v, waiver %v; want %v and casualty", waived.Value().Total(), waived.Value().Waiver(), res.Value().Total())
	}
}

// RCW 82.32.105(1) and WAC 458-20-228(9)(a): the department must waive the
// penalties for circumstances beyond the filer's control, such as a casualty
// or a serious illness. Retirement is not one, and federal law's retirement
// and disability waiver has no Washington counterpart.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "rcw-82-32-105.txt", "circumstances beyond the control of the taxpayer, the department must waive or cancel any penalties")
	mustContain(t, "wac-458-20-228.txt", "death or serious illness of the taxpayer")
	h := household{tax: 10_000 * money.Dollar}
	plain := must(rules(t).Penalty(h.facts(t), day(2027, 5, 1)))
	disaster := must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build())
	retired := must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, 3, 1)).Build())
	disabled := must(estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2026, 6, 1)).Build())
	for _, tc := range []struct {
		name string
		pos  estimated.Positions
		want func(estimated.WaiverOutcome) bool
		in   string
	}{
		{"a disaster", disaster, estimated.WaiverOutcome.Eligible, "beyond your control"},
		{"retired in 2026 at 64", retired, estimated.WaiverOutcome.Refused, "no waiver for retirement"},
		{"disabled in 2026", disabled, estimated.WaiverOutcome.Undecided, "no waiver for disability as such"},
	} {
		h.positions = &tc.pos
		res := must(rules(t).Penalty(h.facts(t), day(2027, 5, 1)))
		o, ok := res.Value().WaiverOutcome()
		if !ok || !tc.want(o) || !strings.Contains(o.Reason(), tc.in) {
			t.Errorf("%s: outcome %q, %v", tc.name, o.Reason(), ok)
		}
		if res.Value().Total() != plain.Value().Total() || plain.Value().Total() == 0 {
			t.Errorf("%s: total %v, without a waiver %v", tc.name, res.Value().Total(), plain.Value().Total())
		}
		// The waiver lifts the penalty, so what's left if it's granted is
		// the interest, and only that.
		if tc.name == "a disaster" {
			var interest money.USCents
			for _, l := range res.Value().Lines() {
				for _, part := range l.Parts() {
					if part.Kind() == estimated.PartInterest {
						interest += part.Charge()
					}
				}
			}
			if res.Value().IfWaived() != interest || interest == 0 || interest == res.Value().Total() {
				t.Errorf("a disaster: if waived %v, interest %v of %v", res.Value().IfWaived(), interest, res.Value().Total())
			}
		}
		got := false
		for _, r := range res.Required() {
			got = got || (r.Position() == working.PositionUnsettledRule && strings.Contains(r.Effect(), "disability"))
		}
		if got != (tc.name == "disabled in 2026") {
			t.Errorf("%s: the open point on disability asked: %v", tc.name, got)
		}
	}
}

// A return filed with the balance paid stops the penalty that day.
func TestEstimatedReturnFiledStopsThePenalty(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, extension: &no, returnFiled: day(2027, 5, 10)}
	flat, interest, res := penalty(t, h, day(2027, 12, 31))
	if res.Value().AsOf() != day(2027, 5, 10) || flat != 900*money.Dollar || interest != 1_644 {
		t.Errorf("to %v: penalty %v and interest %v; want May 10, 2027, $900 and $16.44", res.Value().AsOf(), flat, interest)
	}
	found := false
	for _, r := range res.Applied() {
		found = found || r.Cite() == "RCW 82.87.110(6)"
	}
	if !found {
		t.Error("the late filing penalty is not mentioned")
	}
}

func TestEstimatedExplanationNamesTheRules(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + " " + strings.Join(e.Body(), " ")
	for _, s := range []string{"no quarterly estimated payments", "April 15, 2027", "October 15, 2026", "9%", "19%", "29%", "$5", "80%", "$1,000", "5% of the whole tax", "April 30, 2027", "6%", "five years"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if !strings.Contains(e.VersusFederal(), "no quarterly payments") || len(e.Sources()) == 0 {
		t.Errorf("versus federal %q, %d sources", e.VersusFederal(), len(e.Sources()))
	}
}

// Washington's capital gains tax has no withholding, so paying more than the
// tax is the nearest case: $3,600 paid on $3,000 never reads as a negative
// amount.
func TestEstimatedOverpaymentHasNoNegativeAmount(t *testing.T) {
	h := household{tax: 3_000 * money.Dollar, payments: []estimated.Payment{pay(estimated.MustDate(2027, 4, 1), 3_600*money.Dollar)}}
	req := must(rules(t).Requirement(h.facts(t)))
	_, _, pen := penalty(t, h, estimated.MustDate(2027, 12, 31))
	texts := []string{req.Value().Why(), pen.Value().Reason()}
	for _, o := range req.Value().Options() {
		texts = append(texts, o.Label(), o.WhyNot())
	}
	for _, q := range append(req.Required(), pen.Required()...) {
		texts = append(texts, q.Because(), q.Question(), q.Effect())
	}
	for _, s := range texts {
		if strings.Contains(s, "−$") || strings.Contains(s, "-$") {
			t.Errorf("a negative amount: %q", s)
		}
	}
}
