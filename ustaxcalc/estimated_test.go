// The federal estimated tax rules for 2026, checked against the IRS's own
// documents.
//
// The oracles are of three kinds, strongest first. The worked examples in the
// Form 2210 instructions and Publication 505 give inputs and an answer, and
// the Form 2210 instructions' chart of days per rate period is a table nobody
// chose. Every threshold, percentage, date and rate is compared against its
// extract in testdata/, so a figure typed wrongly fails here rather than
// matching a test typed the same way. The boundary cases are hand-written.
package ustaxcalc_test

import (
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

var day = estimated.MustDate

// extract reads a testdata file with its whitespace collapsed, so a phrase
// the PDF broke across lines or columns can still be found.
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

func rules(t *testing.T) estimated.EstimatedTax {
	t.Helper()
	r, err := ustaxcalc.EstimatedTax(2026)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

type household struct {
	status      filing.Status
	tax         money.USCents
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	returnFiled estimated.Date
	waiver      *estimated.WaiverKind
	positions   *estimated.Positions // a waiver asked for, with its cause and dates
	taxUnknown  bool
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	status := h.status
	if status == filing.Unspecified {
		status = filing.Single
	}
	fb := estimated.NewFigures()
	if !h.taxUnknown {
		fb.Tax(h.tax)
	}
	this, err := fb.Build()
	if err != nil {
		t.Fatal(err)
	}
	b := estimated.NewFacts(estimated.Federal, 2026).FilingStatus(status).ThisYear(this).
		Paid(h.payments...).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		w, err := estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()
		if err != nil {
			t.Fatal(err)
		}
		b.Withheld(w)
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.waiver != nil {
		p, err := estimated.NewPositions().Waiver(*h.waiver).Build()
		if err != nil {
			t.Fatal(err)
		}
		b.Positions(p)
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	f, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func priorReturn(t *testing.T, tax, agi money.USCents) *estimated.PriorReturn {
	t.Helper()
	f, err := estimated.NewFigures().Tax(tax).FederalAGI(agi).Build()
	if err != nil {
		t.Fatal(err)
	}
	p, err := estimated.NewPriorReturn().Filed(true).Figures(f).FullYearResident(true).Build()
	if err != nil {
		t.Fatal(err)
	}
	return &p
}

func pay(t *testing.T, on estimated.Date, a money.USCents) estimated.Payment {
	t.Helper()
	p, err := estimated.NewPayment().On(on).Amount(a).Build()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "f1040es-2026-general-rule.txt",
		"You expect to owe at least $1,000 in tax for 2026",
		"a. 90% of the tax to be shown on your 2026 tax return",
		"b. 100% of the tax shown on your 2025 tax return. Your 2025 tax return must cover all 12 months.",
		"for 2025 was more than $150,000 ($75,000",
		"substitute 110% for 100%",
		"you had no tax liability for the full 12-month 2025 tax year")
	mustContain(t, "f1040es-2026-due-dates.txt",
		"4th payment . . . . . . . . . . . . . . . . . April 15, 2026 June 15, 2026 Sept. 15, 2026 Jan. 15, 2027*",
		"if you file your 2026 tax return by February 1, 2027, and pay the entire balance due with your return",
		"If any payment date falls on a Saturday, Sunday, or legal holiday, use the next business day.")
	mustContain(t, "f2210-2025.txt",
		"If less than $1,000, stop; you don’t owe a penalty",
		"enter 25% (0.25) of line 9",
		"22.5% 45% 67.5% 90%",
		"1/1/25–3/31/25 1/1/25–5/31/25 1/1/25–8/31/25 1/1/25–12/31/25")
	mustContain(t, "i2210-2025-general.txt",
		"A legal holiday includes any legal holiday in the District of Columbia.")
	mustContain(t, "i2210-2025-examples.txt",
		"If you file your return and pay the tax due by January 31, 2026, include on line 11, column (d), the amount of tax you pay with your tax return.",
		"Underpayment on line 1a × Number of days on line 3 365 × 0.07",
		"Your payments are applied first to any underpayment balance on an earlier installment even if you designate a payment for a later period.")
	mustContain(t, "i2210-2025-penalty-worksheet.txt",
		"on line 1a was paid or 04/15/26, whichever is earlier")

	for name, phrase := range map[string]string{
		"rr-2026-5.txt":  "underpayment rate of 6 percent are established for the calendar quarter beginning April 1, 2026",
		"rr-2026-10.txt": "underpayment rate of 7 percent are established for the calendar quarter beginning July 1, 2026",
		"rr-2026-15.txt": "underpayment rate of 7 percent are established for the calendar quarter beginning October 1, 2026",
	} {
		mustContain(t, name, phrase, "estimated tax underpayments")
	}
}

// The rate on each day of 2026's penalty period is the one its revenue
// ruling published, and the series stops where the published rates stop.
func TestEstimatedRatesMatchTheRevenueRulings(t *testing.T) {
	// A shortfall from April 15, 2026, never paid, to the April 15, 2027 stop,
	// crosses every quarter the series carries.
	f := household{tax: 40_000 * money.Dollar, prior: priorReturn(t, 40_000*money.Dollar, 100_000*money.Dollar)}.facts(t)
	res, err := rules(t).Penalty(f, day(2027, 4, 15))
	if err != nil {
		t.Fatal(err)
	}
	parts := res.Value().Lines()[0].Parts()
	want := []struct {
		from, to estimated.Date
		rate     money.Rate
		ruling   string
		file     string
	}{
		{day(2026, 4, 15), day(2026, 6, 30), money.Percent(6), "Rev. Rul. 2026-5", "rr-2026-5.txt"},
		{day(2026, 6, 30), day(2026, 9, 30), money.Percent(7), "Rev. Rul. 2026-10", "rr-2026-10.txt"},
		{day(2026, 9, 30), day(2026, 12, 31), money.Percent(7), "Rev. Rul. 2026-15", "rr-2026-15.txt"},
	}
	if len(parts) != len(want) {
		t.Fatalf("%d parts, want %d", len(parts), len(want))
	}
	for i, w := range want {
		p := parts[i]
		if p.From() != w.from || p.To() != w.to || p.Rate() != w.rate || !strings.HasPrefix(p.Source(), w.ruling) {
			t.Errorf("part %d: %v to %v at %v from %q; want %v to %v at %v from %s", i, p.From(), p.To(), p.Rate(), p.Source(), w.from, w.to, w.rate, w.ruling)
		}
		mustContain(t, w.file, w.ruling)
		pct := strconv.Itoa(int(w.rate) / 100)
		mustContain(t, w.file, "underpayment rate of "+pct+" percent")
	}
	// The 2027 rate is not published, so the penalty says so.
	if !has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("the penalty to April 15, 2027 does not report the unpublished 2027 rate")
	}
}

// The due dates for 2026, after moving for weekends and holidays, are the
// dates the 2026 Form 1040-ES prints. None of the four falls on a weekend or a
// Publication 509 holiday; January 31, 2027 does, and moves to February 1.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	text := extract(t, "p509-2026-legal-holidays.txt")
	holidays := regexp.MustCompile(`• (January|February|April|May|June|July|September|October|November|December) (\d+)—`).FindAllStringSubmatch(text, -1)
	if len(holidays) != 12 {
		t.Fatalf("Publication 509 lists %d holidays for 2026, the extract gives %d", 12, len(holidays))
	}

	res, err := rules(t).Schedule(household{tax: 20_000 * money.Dollar}.facts(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}
	for i, in := range res.Value() {
		if in.Due() != want[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), want[i])
		}
		switch in.Due().Weekday() {
		case time.Saturday, time.Sunday:
			t.Errorf("installment %d falls on a %v", i+1, in.Due().Weekday())
		}
		for _, h := range holidays {
			m, _ := time.Parse("January", h[1])
			n, _ := strconv.Atoi(h[2])
			if in.Due() == day(2026, m.Month(), n) {
				t.Errorf("installment %d falls on a Publication 509 holiday", i+1)
			}
		}
	}

	// § 6654(h): filing and paying by January 31 excuses the January
	// installment, and January 31, 2027 is a Sunday.
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{
		{day(2027, 1, 29), true},
		{day(2027, 2, 1), true},
		{day(2027, 2, 2), false},
	} {
		res, err := rules(t).Schedule(household{tax: 20_000 * money.Dollar, returnFiled: tc.filed}.facts(t))
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Value()[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: January installment excused is %v, want %v", tc.filed, got, tc.excused)
		}
	}
}

// Publication 505 (2026) works one taxpayer's required annual payment: 110% of
// $42,581 is $46,839, less than 90% of $71,253, because 2025 AGI of $180,000
// is over $150,000. The same example the safe harbour test uses, here through
// the estimated tax interface.
func TestEstimatedRequirementMatchesPublication505(t *testing.T) {
	f := household{tax: 71_253 * money.Dollar, prior: priorReturn(t, 42_581*money.Dollar, 180_000*money.Dollar)}.facts(t)
	res, err := rules(t).Requirement(f)
	if err != nil {
		t.Fatal(err)
	}
	r := res.Value()
	chosen, ok := r.Chosen()
	if !ok || chosen.Basis() != estimated.BasisPriorYearTax || chosen.Percent() != money.Percent(110) {
		t.Fatalf("chose %v at %v, want last year's tax at 110%%", chosen.Basis(), chosen.Percent())
	}
	if diff := r.Target() - 46_839*money.Dollar; diff < -100 || diff > 100 {
		t.Errorf("target is %v, Publication 505 says $46,839", r.Target())
	}
	if !r.Required() {
		t.Error("a filer with no withholding and $71,253 of tax is not required to pay")
	}
}

func TestEstimatedThresholdBoundary(t *testing.T) {
	prior := priorReturn(t, 50_000*money.Dollar, 100_000*money.Dollar)
	for _, tc := range []struct {
		owed     money.USCents
		required bool
	}{
		{1_000*money.Dollar - 1, false},
		{1_000 * money.Dollar, true},
	} {
		// $5,000 withheld, and tax exceeding it by owed. 90% of the tax is
		// more than the withholding, so only the threshold decides.
		f := household{tax: 5_000*money.Dollar + tc.owed, withheld: 5_000 * money.Dollar, prior: prior}.facts(t)
		res, err := rules(t).Requirement(f)
		if err != nil {
			t.Fatal(err)
		}
		if res.Value().Required() != tc.required {
			t.Errorf("owing %v after withholding: required is %v, want %v", tc.owed, res.Value().Required(), tc.required)
		}
		// Under the threshold the exception is the chosen option, at $0.
		c, chosen := res.Value().Chosen()
		isException := chosen && c.Basis() == estimated.BasisException && c.Amount() == 0 && c.Label() == "Tax less withholding under $1,000"
		if isException == tc.required || res.Value().Exempt() == tc.required {
			t.Errorf("owing %v: chose %v %q at %v, exempt %v", tc.owed, c.Basis(), c.Label(), c.Amount(), res.Value().Exempt())
		}
		pen, err := rules(t).Penalty(f, day(2026, 12, 31))
		if err != nil {
			t.Fatal(err)
		}
		if !tc.required && pen.Value().Total() != 0 {
			t.Errorf("owing %v: a penalty of %v under the threshold", tc.owed, pen.Value().Total())
		}
	}
}

// The prior-year test is 100% at exactly $150,000 of AGI and 110% a cent
// above; $75,000 for a separate return.
func TestEstimatedPriorYearStepBoundary(t *testing.T) {
	for _, tc := range []struct {
		status filing.Status
		agi    money.USCents
		want   money.Rate
	}{
		{filing.Single, 150_000 * money.Dollar, money.Percent(100)},
		{filing.Single, 150_000*money.Dollar + 1, money.Percent(110)},
		{filing.MarriedFilingJointly, 150_000*money.Dollar + 1, money.Percent(110)},
		{filing.MarriedFilingSeparately, 75_000 * money.Dollar, money.Percent(100)},
		{filing.MarriedFilingSeparately, 75_000*money.Dollar + 1, money.Percent(110)},
	} {
		f := household{status: tc.status, tax: 90_000 * money.Dollar, prior: priorReturn(t, 10_000*money.Dollar, tc.agi)}.facts(t)
		res, err := rules(t).Requirement(f)
		if err != nil {
			t.Fatal(err)
		}
		opt := res.Value().Options()[1]
		if opt.Percent() != tc.want || opt.Amount() != tc.want.Of(10_000*money.Dollar) {
			t.Errorf("%v with AGI %v: %v giving %v, want %v", tc.status, tc.agi, opt.Percent(), opt.Amount(), tc.want)
		}
	}
}

// When the prior-year test is not open, it says why, and the target falls
// back on this year's tax.
func TestEstimatedPriorYearOptionAvailability(t *testing.T) {
	short, _ := estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(100).Build())).Build()
	notFiled, _ := estimated.NewPriorReturn().Build()
	noAGI, _ := estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Build()

	for name, tc := range map[string]struct {
		prior     *estimated.PriorReturn
		available bool
		position  working.PositionKind
	}{
		"no return on record": {nil, false, working.PositionPriorYearReturn},
		"eleven months":       {&short, false, working.PositionUnspecified},
		"not filed":           {&notFiled, false, working.PositionUnspecified},
		"AGI not given":       {&noAGI, true, working.PositionAdjustedGrossIncome},
	} {
		res, err := rules(t).Requirement(household{tax: 30_000 * money.Dollar, prior: tc.prior}.facts(t))
		if err != nil {
			t.Fatal(err)
		}
		opt := res.Value().Options()[1]
		if opt.Available() != tc.available {
			t.Errorf("%s: prior-year option available is %v", name, opt.Available())
		}
		if !tc.available {
			if opt.WhyNot() == "" {
				t.Errorf("%s: unavailable without a reason", name)
			}
			if res.Value().Target() != 27_000*money.Dollar {
				t.Errorf("%s: target %v, want 90%% of this year's", name, res.Value().Target())
			}
		}
		if tc.position != working.PositionUnspecified && !has(res.Required(), tc.position) {
			t.Errorf("%s: %v not reported", name, tc.position)
		}
	}
	// AGI not given: 110% is assumed, so the target can only be too high.
	res, _ := rules(t).Requirement(household{tax: 30_000 * money.Dollar, prior: &noAGI}.facts(t))
	if res.Value().Options()[1].Percent() != money.Percent(110) {
		t.Error("with last year's AGI unknown, the prior-year test is not 110%")
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// § 6654(e)(2): no liability last year, as a citizen or resident for all of
// it, means no penalty. Residence unanswered leaves the exception unapplied
// and says so.
func TestEstimatedNoPriorLiabilityException(t *testing.T) {
	zero := must(estimated.NewFigures().Tax(0).FederalAGI(20_000 * money.Dollar).Build())
	resident := must(estimated.NewPriorReturn().Filed(true).Figures(zero).FullYearResident(true).Build())
	unanswered := must(estimated.NewPriorReturn().Filed(true).Figures(zero).Build())
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().FullYearResident(true).Build())
	partYear := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(zero).FullYearResident(true).Build())

	for name, tc := range map[string]struct {
		prior  estimated.PriorReturn
		exempt bool
	}{
		"zero tax, resident":     {resident, true},
		"not required to file":   {notRequired, true},
		"residence not answered": {unanswered, false},
		"six-month prior return": {partYear, false},
	} {
		f := household{tax: 30_000 * money.Dollar, prior: &tc.prior}.facts(t)
		res, err := rules(t).Requirement(f)
		if err != nil {
			t.Fatal(err)
		}
		c, chosen := res.Value().Chosen()
		if exempt := res.Value().Exempt() && res.Value().Target() == 0; exempt != tc.exempt {
			t.Errorf("%s: exempt is %v, want %v", name, exempt, tc.exempt)
		}
		// The exception is the chosen option, at $0.
		if tc.exempt && (!chosen || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "No federal tax liability last year") {
			t.Errorf("%s: chose %v %q at %v, want the no-liability exception", name, c.Basis(), c.Label(), c.Amount())
		}
		if name == "residence not answered" && !has(res.Required(), working.PositionResidency) {
			t.Errorf("%s: residence not reported as required", name)
		}
	}
}

// § 6654(g): withholding counts as paid evenly across the four dates, so $4,000
// withheld in December credits $1,000 by April.
func TestEstimatedWithholdingIsSpreadEvenly(t *testing.T) {
	f := household{tax: 8_000 * money.Dollar, withheld: 4_000 * money.Dollar, prior: priorReturn(t, 8_000*money.Dollar, 90_000*money.Dollar)}.facts(t)
	res, err := rules(t).Schedule(f)
	if err != nil {
		t.Fatal(err)
	}
	// 90% of $8,000 is $7,200, so $1,800 a quarter, and $1,000 is credited on
	// each date. Each credit pays the oldest shortfall first, as Form 2210
	// lines 14 to 17 do: April is $800 short; June's $1,000 pays April's $800
	// and leaves June $1,600 short; September's and January's go to older
	// shortfalls and leave each of those $1,800 short.
	wantShort := []money.USCents{800, 1_600, 1_800, 1_800}
	for i, in := range res.Value() {
		want := money.USCents(i+1) * 1_000 * money.Dollar
		if in.CreditedBy() != want {
			t.Errorf("installment %d credited %v, want %v", i+1, in.CreditedBy(), want)
		}
		if in.Shortfall() != wantShort[i]*money.Dollar || in.Status() != estimated.StatusShort {
			t.Errorf("installment %d short %v (%v), want $%d", i+1, in.Shortfall(), in.Status(), wantShort[i])
		}
	}
}

// A shortfall that spans two rate periods is charged each period's rate for
// its days: $10,000 short on April 15, paid August 14, is 76 days at 6% and
// 45 at 7%.
func TestEstimatedPenaltyAcrossTwoRatePeriods(t *testing.T) {
	// $40,000 required, $10,000 a quarter; the April installment paid late,
	// the rest on time.
	f := household{
		tax:   80_000 * money.Dollar,
		prior: priorReturn(t, 40_000*money.Dollar, 100_000*money.Dollar),
		payments: []estimated.Payment{
			pay(t, day(2026, 8, 14), 10_000*money.Dollar),
			pay(t, day(2026, 8, 14), 10_000*money.Dollar),
			pay(t, day(2026, 9, 15), 10_000*money.Dollar),
			pay(t, day(2027, 1, 15), 10_000*money.Dollar),
		},
	}.facts(t)
	res, err := rules(t).Penalty(f, day(2027, 4, 15))
	if err != nil {
		t.Fatal(err)
	}
	lines := res.Value().Lines()
	if len(lines) != 2 {
		t.Fatalf("%d penalty lines, want April and June", len(lines))
	}
	// April: 1,000,000¢ × 76 × 6% / 365 = 12,493.15¢, then × 45 × 7% / 365 =
	// 8,630.14¢. June: 1,000,000¢ × 15 × 6% / 365 = 2,465.75¢, then 45 days
	// at 7% = 8,630.14¢.
	wantApril := []money.USCents{12_493, 8_630}
	wantJune := []money.USCents{2_466, 8_630}
	for i, want := range [][]money.USCents{wantApril, wantJune} {
		parts := lines[i].Parts()
		if len(parts) != len(want) {
			t.Fatalf("line %d has %d parts, want %d", i, len(parts), len(want))
		}
		for j, w := range want {
			if parts[j].Charge() != w {
				t.Errorf("line %d part %d charges %v, want %v", i, j, parts[j].Charge(), w)
			}
		}
	}
	if want := money.USCents(12_493 + 8_630 + 2_466 + 8_630); res.Value().Total() != want {
		t.Errorf("penalty is %v, want %v", res.Value().Total(), want)
	}
	if lines[0].Days() != 121 {
		t.Errorf("April's shortfall was unpaid %d days, want 121", lines[0].Days())
	}
	if has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("a penalty paid off in 2026 reports an unpublished rate")
	}
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("a penalty with no waiver answer does not ask about one")
	}

	none := estimated.WaiverNone
	answered := household{tax: 80_000 * money.Dollar, prior: priorReturn(t, 40_000*money.Dollar, 100_000*money.Dollar), waiver: &none}.facts(t)
	res, err = rules(t).Penalty(answered, day(2026, 12, 31))
	if err != nil {
		t.Fatal(err)
	}
	if has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is still asked after it was answered")
	}
}

// The Form 2210 instructions' Example 3 and 4: $4,000 required each quarter,
// and payments of $2,000 on April 30, $3,000 on June 15, $4,000 on September
// 15 and $4,000 on January 15. The instructions give the underpayments, which
// payment pays which, and the days.
func TestPaymentApplicationMatchesForm2210Examples(t *testing.T) {
	mustContain(t, "i2210-2025-examples.txt",
		"Example 3. Your required installment for each payment due date is $4,000.",
		"Date 04/30/25 06/15/25 09/15/25 01/15/26", "$2,000 $3,000 $4,000 $4,000",
		"On line 1a, column (a) shows $4,000 and columns (b) through (d) show $3,000.",
		"Enter “04/30 $2,000” and “06/15 $2,000” on line 1b, column (a).",
		"enter “09/15 $3,000” on line 1b, column (b)",
		"enter “01/15 $3,000” on line 1b, column (c)",
		"Do not enter any payment on line 1b, column (d).",
		"enter “15” (number of days from 04/15 to 04/30) and “61” (number of days from 04/15 to 06/15) on line 3, column (a).")

	dues := []estimated.Date{day(2025, 4, 15), day(2025, 6, 15), day(2025, 9, 15), day(2026, 1, 15)}
	got, err := estimated.Apply(dues, []money.USCents{400_000, 400_000, 400_000, 400_000}, []estimated.Payment{
		pay(t, day(2025, 4, 30), 200_000),
		pay(t, day(2025, 6, 15), 300_000),
		pay(t, day(2025, 9, 15), 400_000),
		pay(t, day(2026, 1, 15), 400_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	type piece struct {
		amount money.USCents
		paid   estimated.Date
	}
	want := []struct {
		shortfall money.USCents
		pieces    []piece
	}{
		{400_000, []piece{{200_000, day(2025, 4, 30)}, {200_000, day(2025, 6, 15)}}},
		{300_000, []piece{{300_000, day(2025, 9, 15)}}},
		{300_000, []piece{{300_000, day(2026, 1, 15)}}},
		{300_000, []piece{{300_000, estimated.Date{}}}},
	}
	for i, w := range want {
		if got[i].Shortfall() != w.shortfall {
			t.Errorf("column %c underpayment %v, want %v", 'a'+i, got[i].Shortfall(), w.shortfall)
		}
		ps := got[i].Pieces()
		if len(ps) != len(w.pieces) {
			t.Fatalf("column %c has %d pieces, want %d", 'a'+i, len(ps), len(w.pieces))
		}
		for j, wp := range w.pieces {
			on, _ := ps[j].PaidOn()
			if ps[j].Amount() != wp.amount || on != wp.paid {
				t.Errorf("column %c piece %d: %v on %v, want %v on %v", 'a'+i, j, ps[j].Amount(), on, wp.amount, wp.paid)
			}
		}
	}
	if a, b := day(2025, 4, 30).DaysSince(dues[0]), day(2025, 6, 15).DaysSince(dues[0]); a != 15 || b != 61 {
		t.Errorf("days late are %d and %d, the instructions say 15 and 61", a, b)
	}
}

// Example 2: a $500 shortfall left on April 15, then $1,200 paid on June 10
// for June's $1,200. $500 of it goes to April, 56 days late, and $700 to June.
func TestPaymentApplicationMatchesForm2210Example2(t *testing.T) {
	mustContain(t, "i2210-2025-examples.txt",
		"Example 2. You had a $500 underpayment remaining after your April 15 payment.",
		"The penalty for the April 15 installment is figured from April 15 to June 10 (56 days).",
		"The amount remaining to be applied to the June 15 installment is $700.")
	dues := []estimated.Date{day(2025, 4, 15), day(2025, 6, 15)}
	got, err := estimated.Apply(dues, []money.USCents{120_000, 120_000}, []estimated.Payment{
		pay(t, day(2025, 4, 15), 70_000),
		pay(t, day(2025, 6, 10), 120_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	on, _ := got[0].Pieces()[0].PaidOn()
	if got[0].Shortfall() != 50_000 || on.DaysSince(dues[0]) != 56 {
		t.Errorf("April: %v short, paid after %d days", got[0].Shortfall(), on.DaysSince(dues[0]))
	}
	if got[1].Shortfall() != 50_000 { // $1,200 needed, $700 left to apply
		t.Errorf("June is %v short, want $500 ($1,200 less the $700 left)", got[1].Shortfall())
	}
}

// Table 2 in the Form 2210 instructions is the days in each rate period for a
// shortfall never paid, by column. It is the days arithmetic for a whole year,
// published, and nobody chose its cases.
func TestDaysPerRatePeriodMatchForm2210Table2(t *testing.T) {
	text := extract(t, "i2210-2025-penalty-worksheet.txt")
	row := regexp.MustCompile(`(\d\d/\d\d/\d\d)–(\d\d/\d\d/\d\d) ((?:(?:\d+|—) ?){4})`)
	rows := row.FindAllStringSubmatch(text, -1)
	if len(rows) != 4 {
		t.Fatalf("Table 2 has %d rows in the extract, want 4", len(rows))
	}

	// The 2025 quarters, all at the 7% the worksheet prints.
	s, err := estimated.NewRateSeries().
		Period(day(2025, 4, 1), day(2025, 6, 30), 700, "Q2").
		Period(day(2025, 7, 1), day(2025, 9, 30), 700, "Q3").
		Period(day(2025, 10, 1), day(2025, 12, 31), 700, "Q4").
		Period(day(2026, 1, 1), day(2026, 4, 15), 700, "Q1 and April 1 to 15").
		Build()
	if err != nil {
		t.Fatal(err)
	}
	dues := []estimated.Date{day(2025, 4, 15), day(2025, 6, 15), day(2025, 9, 15), day(2026, 1, 15)}
	for col, due := range dues {
		parts, _, err := estimated.SimpleDaily(100_000, due, day(2026, 4, 15), s)
		if err != nil {
			t.Fatal(err)
		}
		// Line up each part with the table row whose period it falls in.
		for _, p := range parts {
			found := false
			for _, r := range rows {
				end, _ := time.Parse("01/02/06", r[2])
				if p.To() != estimated.DateOf(end) {
					continue
				}
				found = true
				cell := strings.Fields(r[3])[col]
				if strconv.Itoa(p.Days()) != cell {
					t.Errorf("column %c, period to %s: %d days, Table 2 says %s", 'a'+col, r[2], p.Days(), cell)
				}
			}
			if !found {
				t.Errorf("column %c has a part ending %v that no Table 2 row ends on", 'a'+col, p.To())
			}
		}
	}
}

func TestEstimatedRefusesOtherYearsAndJurisdictions(t *testing.T) {
	for _, year := range []int{2025, 2027} {
		if _, err := ustaxcalc.EstimatedTax(year); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", year, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	ca := must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build())
	if _, err := rules(t).Requirement(ca); err == nil {
		t.Error("the federal rules accepted California's facts")
	}
	farmer := must(estimated.NewFacts(estimated.Federal, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(6667).Build())
	if _, err := rules(t).Requirement(farmer); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer: %v, want ErrUnsupported", err)
	}
	notQuite := must(estimated.NewFacts(estimated.Federal, 2026).FilingStatus(filing.Single).ThisYear(this).FarmShare(6666).Build())
	if _, err := rules(t).Requirement(notQuite); err != nil {
		t.Errorf("66.66%% from farming is under two-thirds and was refused: %v", err)
	}
}

func TestEstimatedExplanationReadsAsProse(t *testing.T) {
	e := rules(t).Explanation()
	if len(e.Body()) < 3 || e.Summary() == "" || len(e.Sources()) == 0 {
		t.Fatalf("explanation is thin: %+v", e)
	}
	if e.VersusFederal() != "" {
		t.Error("the federal explanation compares itself with federal")
	}
}

// A return filed with the whole balance paid pays every shortfall still
// outstanding that day, so the penalty stops there rather than on April 15.
// $10,000 short on April 15 and on June 15, nothing paid, and the return filed
// and paid on March 1, 2027.
func TestEstimatedPenaltyStopsWhenTheReturnIsPaid(t *testing.T) {
	h := household{tax: 80_000 * money.Dollar, prior: priorReturn(t, 40_000*money.Dollar, 100_000*money.Dollar),
		payments: []estimated.Payment{
			pay(t, day(2026, 9, 15), 20_000*money.Dollar),
			pay(t, day(2027, 1, 15), 10_000*money.Dollar),
		}}
	unfiled, err := rules(t).Penalty(h.facts(t), day(2026, 12, 31))
	if err != nil {
		t.Fatal(err)
	}
	h.returnFiled = day(2026+1, 1, 4)
	filed, err := rules(t).Penalty(h.facts(t), day(2027, 4, 15))
	if err != nil {
		t.Fatal(err)
	}
	// Both shortfalls were paid on September 15, before either stop, so the
	// filing date changes nothing here.
	if unfiled.Value().Total() != filed.Value().Total() {
		t.Errorf("a return filed after the shortfalls were paid changed the penalty from %v to %v", unfiled.Value().Total(), filed.Value().Total())
	}

	h = household{tax: 80_000 * money.Dollar, prior: priorReturn(t, 40_000*money.Dollar, 100_000*money.Dollar), returnFiled: day(2027, 3, 1)}
	res, err := rules(t).Penalty(h.facts(t), day(2027, 4, 15))
	if err != nil {
		t.Fatal(err)
	}
	if res.Value().AsOf() != day(2027, 3, 1) {
		t.Errorf("the penalty is figured to %v, want the day the return was paid", res.Value().AsOf())
	}
	for _, l := range res.Value().Lines() {
		for _, p := range l.Parts() {
			if p.To().After(day(2027, 3, 1)) {
				t.Errorf("installment %d is charged to %v, after the balance was paid", l.Installment(), p.To())
			}
		}
	}
}

// By election, withholding counts on the dates it was withheld: $4,000
// withheld on December 15 credits nothing by April, and all of it by January.
func TestEstimatedWithholdingAsWithheldByElection(t *testing.T) {
	this, err := estimated.NewFigures().Tax(8_000 * money.Dollar).Build()
	if err != nil {
		t.Fatal(err)
	}
	w, err := estimated.NewWithheld().On(day(2026, 12, 15)).Amount(4_000 * money.Dollar).Build()
	if err != nil {
		t.Fatal(err)
	}
	elected, err := estimated.NewElections().WithholdingAsWithheld().Build()
	if err != nil {
		t.Fatal(err)
	}
	f, err := estimated.NewFacts(estimated.Federal, 2026).FilingStatus(filing.Single).ThisYear(this).
		PriorYear(*priorReturn(t, 8_000*money.Dollar, 90_000*money.Dollar)).Withheld(w).Elections(elected).Build()
	if err != nil {
		t.Fatal(err)
	}
	res, err := rules(t).Schedule(f)
	if err != nil {
		t.Fatal(err)
	}
	want := []money.USCents{0, 0, 0, 4_000 * money.Dollar}
	for i, in := range res.Value() {
		if in.CreditedBy() != want[i] {
			t.Errorf("installment %d credited %v, want %v", i+1, in.CreditedBy(), want[i])
		}
	}
}

// Schedule AI: 22.5%, 45%, 67.5% and 90% of each period's annualised tax,
// less what earlier installments required, and never more than the regular
// installment plus what the method carried forward.
func TestEstimatedAnnualisedPercentages(t *testing.T) {
	ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
	taxes := []money.USCents{0, 4_000, 12_000, 12_000}
	var periods []estimated.Period
	for i, e := range ends {
		p, err := estimated.NewPeriod().End(e).AnnualisedTax(taxes[i] * money.Dollar).Build()
		if err != nil {
			t.Fatal(err)
		}
		periods = append(periods, p)
	}
	this, err := estimated.NewFigures().Tax(12_000 * money.Dollar).Build()
	if err != nil {
		t.Fatal(err)
	}
	ann, err := estimated.NewElections().Annualised().Build()
	if err != nil {
		t.Fatal(err)
	}
	f, err := estimated.NewFacts(estimated.Federal, 2026).FilingStatus(filing.Single).ThisYear(this).
		PriorYear(*priorReturn(t, 20_000*money.Dollar, 90_000*money.Dollar)).Elections(ann).Periods(periods...).Build()
	if err != nil {
		t.Fatal(err)
	}
	res, err := rules(t).Schedule(f)
	if err != nil {
		t.Fatal(err)
	}
	// Regular: 25% of 90% of $12,000 is $2,700. (a) 22.5% of 0 is 0. (b) 45% of
	// $4,000 is $1,800, under $2,700 + $2,700. (c) 67.5% of $12,000 less $1,800
	// is $6,300; the regular $2,700 + $3,600 carried is $6,300 too. (d) 90% of
	// $12,000 less $8,100 is $2,700.
	want := []money.USCents{0, 1_800, 8_100, 10_800}
	for i, in := range res.Value() {
		if in.NeededBy() != want[i]*money.Dollar {
			t.Errorf("installment %d needs %v by its date, want $%d", i+1, in.NeededBy(), want[i])
		}
	}
	if _, err := rules(t).Schedule(must(estimated.NewFacts(estimated.Federal, 2026).FilingStatus(filing.Single).ThisYear(this).
		Elections(ann).Periods(append(periods[:3:3], must(estimated.NewPeriod().End(day(2026, 11, 30)).Build()))...).Build())); err == nil {
		t.Error("a Schedule AI period ending November 30 was accepted")
	}
}

// Without this year's tax, the target is last year's tax alone: it avoids the
// penalty whatever this year's tax turns out to be. The option on this year's
// tax is reported unavailable, the question goes in Required, and the penalty
// is the most it can be.
func TestEstimatedWithoutThisYearsTaxUsesLastYearsAlone(t *testing.T) {
	h := household{taxUnknown: true, withheld: 2_000 * money.Dollar, prior: priorReturn(t, 10_000*money.Dollar, 100_000*money.Dollar)}
	res, err := rules(t).Requirement(h.facts(t))
	if err != nil {
		t.Fatal(err)
	}
	r := res.Value()
	if !r.Required() || r.Target() != 10_000*money.Dollar || !r.TargetKnown() || r.Exempt() {
		t.Errorf("required %v, target %v, known %v, exempt %v; want required, $10,000, known, not exempt: %s", r.Required(), r.Target(), r.TargetKnown(), r.Exempt(), r.Why())
	}
	opts := r.Options()
	if opts[0].Basis() != estimated.BasisThisYearTax || opts[0].Available() || opts[0].WhyNot() != "Needs this year's tax" || opts[0].Amount() != 0 {
		t.Errorf("this year's option is %v available %v %q %v; want unavailable, \"Needs this year's tax\"", opts[0].Basis(), opts[0].Available(), opts[0].WhyNot(), opts[0].Amount())
	}
	if c, ok := r.Chosen(); !ok || c.Basis() != estimated.BasisPriorYearTax || c.Amount() != 10_000*money.Dollar {
		t.Errorf("chose %v %v, want last year's $10,000", c.Basis(), c.Amount())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	for _, a := range res.Applied() {
		if strings.Contains(a.Applied(), "90% of this year's") {
			t.Errorf("a rule names a figure from a tax nobody gave: %s", a.Applied())
		}
	}

	sched, err := rules(t).Schedule(h.facts(t))
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []money.USCents{2_500, 5_000, 7_500, 10_000} {
		if got := sched.Value()[i].NeededBy(); got != want*money.Dollar {
			t.Errorf("installment %d needs %v by its date, want $%d", i+1, got, want)
		}
	}

	bound, err := rules(t).Penalty(h.facts(t), day(2027, 4, 15))
	if err != nil {
		t.Fatal(err)
	}
	if !(bound.Value().UpperBound() || !bound.Value().RatedThrough().IsZero()) || bound.Value().Total() == 0 {
		t.Fatalf("penalty %v, upper bound %v; want a positive upper bound", bound.Value().Total(), bound.Value().UpperBound())
	}
	// A tax large enough that last year's figure is the smaller option gives
	// the same penalty; a smaller one gives less.
	for _, tc := range []struct {
		tax  money.USCents
		same bool
	}{{1_000_000 * money.Dollar, true}, {9_000 * money.Dollar, false}} {
		k := h
		k.taxUnknown, k.tax = false, tc.tax
		known, err := rules(t).Penalty(k.facts(t), day(2027, 4, 15))
		if err != nil {
			t.Fatal(err)
		}
		if known.Value().UpperBound() {
			t.Errorf("with this year's tax of %v the penalty is marked an upper bound", tc.tax)
		}
		if got := known.Value().Total(); (got == bound.Value().Total()) != tc.same || got > bound.Value().Total() {
			t.Errorf("with this year's tax of %v the penalty is %v against the bound of %v", tc.tax, got, bound.Value().Total())
		}
	}
}

// With neither this year's tax nor a usable return for last year, no option
// can be measured. Requirement says so; Schedule and Penalty refuse.
func TestEstimatedWithNoOptionHasNoTarget(t *testing.T) {
	f := household{taxUnknown: true, withheld: 2_000 * money.Dollar}.facts(t)
	res, err := rules(t).Requirement(f)
	if err != nil {
		t.Fatal(err)
	}
	r := res.Value()
	if r.TargetKnown() || r.Required() || r.Target() != 0 || r.Exempt() {
		t.Errorf("known %v, required %v, target %v, exempt %v; want no target", r.TargetKnown(), r.Required(), r.Target(), r.Exempt())
	}
	if _, ok := r.Chosen(); ok {
		t.Error("an option was chosen")
	}
	if !has(res.Required(), working.PositionThisYearTax) || !has(res.Required(), working.PositionPriorYearReturn) {
		t.Errorf("want both this year's tax and last year's return asked for: %v", res.Required())
	}
	if _, err := rules(t).Schedule(f); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("Schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(f, day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("Penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// The no-liability exception needs nothing about this year, so it still
// applies without this year's tax, and the zero penalty is exact.
func TestEstimatedWithoutThisYearsTaxStillAppliesNoLiability(t *testing.T) {
	f := household{taxUnknown: true, prior: priorReturn(t, 0, 40_000*money.Dollar)}.facts(t)
	res, err := rules(t).Requirement(f)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Value().Exempt() || res.Value().Required() {
		t.Errorf("not exempt: %s", res.Value().Why())
	}
	pen, err := rules(t).Penalty(f, day(2027, 4, 15))
	if err != nil {
		t.Fatal(err)
	}
	if pen.Value().Total() != 0 || pen.Value().UpperBound() {
		t.Errorf("penalty %v, upper bound %v; want an exact zero", pen.Value().Total(), pen.Value().UpperBound())
	}
}

// The waivers are decided from the conditions the Form 2210 instructions states: a
// casualty or disaster, or retiring after 62 or becoming disabled in the tax
// year or the year before. The penalty is still figured in full.
func TestEstimatedWaiverIsDecidedFromItsConditions(t *testing.T) {
	mustContain(t, "i2210-2025-waiver.txt", "In 2024 or 2025, you retired after reaching age 62 or became disabled")
	base := household{tax: 80_000 * money.Dollar, prior: priorReturn(t, 40_000*money.Dollar, 100_000*money.Dollar)}
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
		case c.eligible && !strings.Contains(strings.ToLower(o.Reason()), "the irs"):
			t.Errorf("%s: the reason %q does not name %s", c.name, o.Reason(), "the irs")
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
		{"no liability last year, this year's tax not given", household{taxUnknown: true, prior: priorReturn(t, 0, 50_000*money.Dollar)}, true},
		{"tax last year, this year's tax not given", household{taxUnknown: true, prior: priorReturn(t, 4_000*money.Dollar, 50_000*money.Dollar)}, false},
	} {
		res := must(rules(t).Requirement(c.h.facts(t)))
		if res.Value().Exempt() != c.exempt {
			t.Fatalf("%s: exempt is %v, want %v", c.name, res.Value().Exempt(), c.exempt)
		}
		if has(res.Required(), working.PositionThisYearTax) == c.exempt {
			t.Errorf("%s: asked about %v is %v", c.name, working.PositionThisYearTax, !c.exempt)
		}
		for _, q := range res.Required() {
			if c.exempt && strings.Contains(strings.ToLower(q.Effect()), "target") {
				t.Errorf("%s: an exception sets the target, yet a question says %q", c.name, q.Effect())
			}
		}
	}
}
