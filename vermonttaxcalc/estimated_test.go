// Vermont's estimated tax rules for 2026, checked against 32 V.S.A. and the
// research's quotes from the Department of Taxes' documents.
//
// Vermont publishes no exhaustive oracle for estimated tax, and IN-152 carries
// no worked example. The strongest checks are the four examples on the
// Department's Calculate Interest and Penalties page, three of which state
// their answer. The thresholds, rates, dates and shares are compared against a
// statute extract in testdata/ or, for the Department's documents, which
// tax.vermont.gov would not serve, against the verified quotes in
// testdata/research-vt.md. testdata/SOURCES.md lists the checks that could
// not be kept. The interest and penalty arithmetic, the payment order, the
// exceptions and the boundaries are hand-computed from IN-152's lines.
package vermonttaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/vermonttaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

const vt estimated.Jurisdiction = "VT"

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
	return must(vermonttaxcalc.EstimatedTax(2026))
}

func asked(rs []working.Required, k working.PositionKind, about string) bool {
	for _, r := range rs {
		if r.Position() == k && (strings.Contains(r.Question(), about) || strings.Contains(r.Effect(), about)) {
			return true
		}
	}
	return false
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

// quarterly is one payment of a on each due date.
func quarterly(a ...money.USCents) []estimated.Payment {
	var out []estimated.Payment
	for i, x := range a {
		out = append(out, pay(dues[i], x))
	}
	return out
}

type household struct {
	tax         money.USCents // IN-111 line 20 minus line 26c
	noTax       bool
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *vermonttaxcalc.EstimatedFacts
	returnFiled estimated.Date
	farm, fish  money.Rate
	annualised  bool
	asWithheld  bool
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	this := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		this = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts(vt, 2026).FilingStatus(filing.Single).ThisYear(this).
		Paid(h.payments...).ReturnFiled(h.returnFiled).FarmShare(h.farm).FishShare(h.fish)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(vermonttaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).Build()))
	}
	el := estimated.NewElections()
	if h.annualised {
		el.Annualised()
		for _, end := range []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)} {
			b.Periods(must(estimated.NewPeriod().End(end).Build()))
		}
	}
	if h.asWithheld {
		el.WithholdingAsWithheld()
	}
	b.Elections(must(el.Build()))
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

// prior is a filed twelve-month return with its tax minus credits.
func prior(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).Build())
	return &p
}

// fourThousand is a household whose 90% is $4,000.00, a quarter of which is
// $1,000: 90% of $4,444.44 is $3,999.996, which rounds to $4,000.00.
func fourThousand() household { return household{tax: 444_444} }

// Every 2026 figure the rules use, found in the document that publishes it
// or, for the Department's documents, in the research's quote from it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	// The Department's documents, through the research file's verified
	// quotes; tax.vermont.gov refused the downloads. See testdata/SOURCES.md
	// for the checks no quote could replace.
	mustContain(t, "research-vt.md",
		"2026 Form IN-114 Instructions, Individual Income Estimated Tax Payment Voucher (Rev. 10/25), tax year 2026.",
		"Worksheet IN-152, Underpayment of 2025 Estimated Individual Income Tax (Rev. 10/25), tax year 2025.",
		// The 90% and 100% options.
		"IN-114 voucher, TY2026, source 2: \"an amount equal to 100% of your prior year tax liability or 90% of your current year tax liability, whichever is less.\"",
		"IN-152 lines 1–3, TY2025, source 3: \"90% of 2025 VERMONT TAX MINUS CREDITS\", \"100% of 2024 VERMONT TAX MINUS CREDITS\", \"Enter the lesser of Line 1 or Line 2.\"",
		// The $500 threshold and what it is measured on.
		"IN-111 Instructions line 31, TY2025, source 5: \"If the tax liability due, less withholding, is less than $500, you will not be subject to penalty or interest charges.\"",
		"Source 17's example: \"this year's tax from Line 26 less withholding and credits is not less than $500.\"",
		"The IN-114 Instructions say \"If the tax liability due is less than $500\"",
		"The IN-114 voucher says penalties apply if you \"owe more than $500 when you file\"",
		// Four equal quarters, and withholding a quarter on each.
		"IN-114 Instructions, TY2026, source 1: \"Estimated income tax payments are made in four equal amounts by the due dates shown below\".",
		"IN-152 line 4, TY2025, source 3: \"Tax payments required per quarter (Multiply Line 3 by .25)\".",
		"IN-152 line 5a, TY2025, source 3: \"From 2025 Form IN-111, add Lines 26a, 26d, and 26e, then multiply by 25% (0.25) per quarter\".",
		// Farmers and fishers, and annualising.
		"IN-114 Instructions, TY2026, source 1: \"Individuals receiving two-thirds of their income from farming or fishing, as defined by the IRS, are not required to make estimated income tax payments.\"",
		"IN-114 Instructions, TY2026, source 1: \"Individuals who make annualized estimated tax payments with the IRS may also make annualized estimated payments in Vermont.\"",
		"IN-152A line 6, TY2025, source 4: \"Vermont payment due. (Divide Line 5 by Line 4, then multiply by Line 3.)\"",
		// The rate and the monthly penalty's part months.
		"Interest Rates page, source 7: \"Rounding up to the nearest quarter of a percent, the annual rate for the overpayment and underpayment of tax for 2026 will be 7.75%.\"",
		"IN-152 line 12, TY2025, source 3: \"a portion of a month counts as a whole month\".",
	)
	mustContain(t, "vsa-32-5851.txt",
		"(A) 90 percent of the tax shown on the return for the taxable year",
		"(B) 100 percent of the tax shown on the return of the taxpayer for the preceding taxable year, if the preceding year was a taxable year of 12 months and the taxpayer filed a return for such preceding taxable year.",
	)
	mustContain(t, "vsa-32-5852.txt",
		"(other than a person receiving at least two-thirds of his or her income from farming or fishing as defined under the laws of the United States)",
		"The amount of each payment shall be 25 percent of the required annual payment.",
		"payments shall be made on or before April 15, June 15, and September 15 of the taxable year and January 15 of the following taxable year.",
	)
	mustContain(t, "vsa-32-3202.txt",
		"a penalty that shall be equal to one percent of the outstanding tax liability for each month, or portion thereof, that the tax liability is not paid in full",
	)
	mustContain(t, "vsa-32-3108.txt",
		"rounded upwards to the nearest quarter percent. The rate established hereunder shall be effective on January 1 of the immediately following year.",
	)
	for _, f := range []string{"vsa-32-5851.txt", "vsa-32-5852.txt", "vsa-32-5857.txt", "vsa-32-5859.txt", "vsa-32-3108.txt", "vsa-32-3202.txt", "vsa-32-3004.txt"} {
		mustContain(t, f, "The Statutes below now include the actions of the 2026 session of the General Assembly.")
	}
}

// The 2026 dates are the ones IN-114 prints. Each is a weekday and none is a
// Vermont state holiday, so none moves.
func TestEstimatedDueDatesForTwentyTwentySix(t *testing.T) {
	res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t)))
	cal := vermonttaxcalc.Calendar(2026)
	for i, in := range res.Value() {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if !cal.IsBusinessDay(in.Due()) {
			t.Errorf("installment %d falls on %v, not a business day", i+1, in.Due())
		}
		if want := money.USCents(i+1) * 2_250 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want a quarter of $9,000 for each date so far, %v", i+1, in.NeededBy(), want)
		}
	}
	// IN-152 line 7 stops on April 15, 2027, a Thursday.
	if !cal.IsBusinessDay(day(2027, 4, 15)) {
		t.Error("April 15, 2027 is not a business day")
	}
}

// The calendar is the Department's State Holiday Closures list, and it moves
// a date on a weekend or a Vermont holiday to the next business day. The
// page itself is not checked in, so the holiday dates are not checked
// against it; testdata/SOURCES.md says why.
func TestEstimatedHolidayCalendarMovesDates(t *testing.T) {
	mustContain(t, "research-vt.md",
		"State Holiday Closures page, source 15: \"Any tax due date that falls on a holiday is automatically extended to the next business day.\"",
		"Timely Filing page, source 16: \"When the due date falls on a weekend, the due date is the next business day.\"",
	)
	if wd := day(2026, 11, 28).Weekday(); wd != time.Saturday {
		t.Errorf("November 28, 2026 is a %v", wd)
	}

	cal := vermonttaxcalc.Calendar(2026)
	if hs := cal.Holidays(); len(hs) != 23 {
		t.Errorf("%d holidays, want the page's 25 less the two 2026 Thanksgiving dates", len(hs))
	}

	for _, tc := range []struct {
		from, want estimated.Date
		why        string
	}{
		{day(2027, 1, 16), day(2027, 1, 19), "a Saturday before Martin Luther King, Jr. Day"},
		{day(2027, 3, 2), day(2027, 3, 3), "Town Meeting Day"},
		{day(2026, 11, 26), day(2026, 11, 26), "Thanksgiving 2026, which the calendar does not carry"},
		{day(2027, 11, 25), day(2027, 11, 29), "Thanksgiving and the day after, then a weekend"},
		{day(2026, 4, 15), day(2026, 4, 15), "a Wednesday"},
	} {
		if got := cal.NextBusinessDay(tc.from); got != tc.want {
			t.Errorf("%v (%s) moves to %v, want %v", tc.from, tc.why, got, tc.want)
		}
	}
}

// The $500 threshold, one cent either side. $500.00 exactly is charged, as
// the instructions' "less than $500" reads, and the question is reported.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
		asked    bool
	}{
		{50_001, true, false},  // $499.99 left
		{50_000, false, true},  // $500.00
		{49_999, false, false}, // $500.01
	} {
		f := household{tax: 1_000 * money.Dollar, withheld: tc.withheld}.facts(t)
		res := must(rules(t).Requirement(f))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v (%s)", tc.withheld, res.Value().Exempt(), tc.exempt, res.Value().Why())
		}
		if c, _ := res.Value().Chosen(); tc.exempt && (c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding under $500") {
			t.Errorf("withheld %v: chosen %v %q at %v, want the $500 exception", tc.withheld, c.Basis(), c.Label(), c.Amount())
		}
		for _, d := range res.Divergences() {
			if d.Rule() == "The $500 threshold" && d.AffectsHousehold() != tc.exempt {
				t.Errorf("withheld %v: the $500 threshold affects these facts %v, want %v", tc.withheld, d.AffectsHousehold(), tc.exempt)
			}
		}
		if got := asked(res.Required(), working.PositionUnsettledRule, "exactly $500"); got != tc.asked {
			t.Errorf("withheld %v: the $500 question asked %v, want %v", tc.withheld, got, tc.asked)
		}
		p := must(rules(t).Penalty(f, day(2027, 4, 15)))
		if (p.Value().Total() == 0) != tc.exempt {
			t.Errorf("withheld %v: penalty %v", tc.withheld, p.Value().Total())
		}
	}
}

// The target is the lesser of 90% of this year's tax and 100% of last
// year's, one cent either side of where they meet.
func TestEstimatedTargetIsTheLesserOption(t *testing.T) {
	for _, tc := range []struct {
		prior  money.USCents
		chosen estimated.Basis
		target money.USCents
	}{
		{899_999, estimated.BasisPriorYearTax, 899_999},
		{900_000, estimated.BasisThisYearTax, 900_000},
		{900_001, estimated.BasisThisYearTax, 900_000},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(tc.prior)}.facts(t)))
		r := res.Value()
		c, _ := r.Chosen()
		if c.Basis() != tc.chosen || r.Target() != tc.target {
			t.Errorf("last year %v: chose %v at %v, want %v at %v", tc.prior, c.Basis(), r.Target(), tc.chosen, tc.target)
		}
		opts := r.Options()
		if opts[0].Percent() != money.Percent(90) || opts[1].Percent() != money.Percent(100) || opts[1].Amount() != tc.prior {
			t.Errorf("options %v %v and %v %v", opts[0].Percent(), opts[0].Amount(), opts[1].Percent(), opts[1].Amount())
		}
	}
}

// Last year's option needs a filed twelve-month return. A filed return with
// no tax makes the target zero.
func TestEstimatedPriorYearOptionAvailability(t *testing.T) {
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	notRequired := must(estimated.NewPriorReturn().NotRequiredToFile().Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(6).Figures(must(estimated.NewFigures().Tax(100).Build())).Build())
	for name, tc := range map[string]struct {
		prior *estimated.PriorReturn
		ask   bool
	}{
		"no return on record": {nil, true},
		"not filed":           {&notFiled, false},
		"not required":        {&notRequired, false},
		"six months":          {&short, false},
	} {
		res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: tc.prior}.facts(t)))
		if o := res.Value().Options()[1]; o.Available() || o.WhyNot() == "" {
			t.Errorf("%s: last year's option is available %v, %q", name, o.Available(), o.WhyNot())
		}
		if got := asked(res.Required(), working.PositionPriorYearReturn, "2025"); got != tc.ask {
			t.Errorf("%s: last year's return asked %v, want %v", name, got, tc.ask)
		}
	}

	f := household{tax: 10_000 * money.Dollar, prior: prior(0)}.facts(t)
	r := must(rules(t).Requirement(f)).Value()
	if r.Required() || r.Target() != 0 || r.Exempt() {
		t.Errorf("no tax last year: required %v, target %v, exempt %v", r.Required(), r.Target(), r.Exempt())
	}
	if p := must(rules(t).Penalty(f, day(2027, 4, 15))); p.Value().Total() != 0 {
		t.Errorf("no tax last year: a penalty of %v", p.Value().Total())
	}
}

// Without this year's tax the target is last year's, and the penalty is an
// upper bound. Without last year's either there is no target.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000 * money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 8_000*money.Dollar {
		t.Errorf("required %v, target %v; want $8,000: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !asked(res.Required(), working.PositionThisYearTax, "the most it can be") {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, in := range sched.Value() {
		if want := money.USCents(i+1) * 2_000 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if p.Value().Total() == 0 || !(p.Value().UpperBound() || !p.Value().RatedThrough().IsZero()) {
		t.Errorf("unpaid: penalty %v, upper bound %v", p.Value().Total(), p.Value().UpperBound())
	}
	paid := h
	paid.payments = quarterly(2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar, 2_000*money.Dollar)
	p = must(rules(t).Penalty(paid.facts(t), day(2027, 4, 15)))
	if p.Value().Total() != 0 || p.Value().UpperBound() {
		t.Errorf("paid to last year's tax: penalty %v, upper bound %v; want an exact zero", p.Value().Total(), p.Value().UpperBound())
	}

	none := household{noTax: true}
	r = must(rules(t).Requirement(none.facts(t))).Value()
	if r.TargetKnown() || r.Required() {
		t.Errorf("no tax and no return: target known %v, required %v", r.TargetKnown(), r.Required())
	}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// IN-152 lines 8a to 13 on $1,000 short each quarter, figured to December
// 31, 2026. Interest is $1,000 × days ÷ 365 × 7.75%; the penalty is 1% for
// each month or part of one.
func TestEstimatedInterestAndPenaltyArithmetic(t *testing.T) {
	p := must(rules(t).Penalty(fourThousand().facts(t), day(2026, 12, 31))).Value()
	want := []struct {
		days     int
		interest money.USCents
		months   int
	}{
		{260, 5_521, 9}, // $55.205..., and April 15 to December 31 is 8 months and part of a 9th
		{199, 4_225, 7}, // $42.253...
		{107, 2_272, 4}, // $22.719...
	}
	lines := p.Lines()
	if len(lines) != len(want) {
		t.Fatalf("%d lines, want %d: the January installment is not yet due", len(lines), len(want))
	}
	for i, w := range want {
		parts := lines[i].Parts()
		if lines[i].Shortfall() != 1_000*money.Dollar || len(parts) != 2 {
			t.Fatalf("line %d: %v short with %d parts", i+1, lines[i].Shortfall(), len(parts))
		}
		in, pen := parts[0], parts[1]
		if in.Kind() != estimated.PartInterest || in.Days() != w.days || in.Rate() != 775 || in.Charge() != w.interest {
			t.Errorf("line %d interest: %d days at %v is %v; want %d days at 7.75%%, %v", i+1, in.Days(), in.Rate(), in.Charge(), w.days, w.interest)
		}
		if pen.Kind() != estimated.PartMonthly || pen.Months() != w.months || pen.Charge() != money.USCents(w.months)*10*money.Dollar {
			t.Errorf("line %d penalty: %d months is %v; want %d months, $%d", i+1, pen.Months(), pen.Charge(), w.months, w.months*10)
		}
	}
	if p.Total() != 32_018 {
		t.Errorf("total %v, want $320.18", p.Total())
	}
}

// A shortfall that runs past January 1, 2027 spans two rate periods. IN-152
// counts the days to January 1 at the due year's rate and the rest at the
// next year's. The 2027 rate is not published, so those days are reported
// rather than charged, and the 1% a month is charged in full.
func TestEstimatedShortfallSpanningTwoRatePeriods(t *testing.T) {
	res := must(rules(t).Penalty(fourThousand().facts(t), day(2027, 4, 15)))
	p := res.Value()
	want := []struct {
		days     int
		interest money.USCents
		months   int
	}{
		{261, 5_542, 12}, // April 15 to January 1 inclusive, IN-152 line 8a
		{200, 4_247, 10},
		{108, 2_293, 7},
		{0, 0, 3}, // the January installment's days are all in 2027
	}
	lines := p.Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	for i, w := range want {
		var interest money.USCents
		days, months := 0, 0
		for _, part := range lines[i].Parts() {
			switch part.Kind() {
			case estimated.PartInterest:
				interest += part.Charge()
				days += part.Days()
				if part.To() != day(2027, 1, 1) {
					t.Errorf("line %d: the 2026 interest runs to %v, want January 1, 2027", i+1, part.To())
				}
			case estimated.PartMonthly:
				months = part.Months()
			}
		}
		if days != w.days || interest != w.interest || months != w.months {
			t.Errorf("line %d: %d days, %v interest, %d months; want %d, %v, %d", i+1, days, interest, months, w.days, w.interest, w.months)
		}
	}
	if p.Total() != 44_082 {
		t.Errorf("total %v, want $440.82", p.Total())
	}
	if !asked(res.Required(), working.PositionUnpublishedRate, "Interest for 2027 isn't charged") {
		t.Errorf("the 2027 rate is not reported: %v", res.Required())
	}

	// Paid on January 1, 2027, everything is at the 2026 rate. Paid a day
	// later, one day falls in 2027 and the gap is reported.
	for _, tc := range []struct {
		filed estimated.Date
		gap   bool
	}{{day(2027, 1, 1), false}, {day(2027, 1, 2), true}} {
		h := fourThousand()
		h.returnFiled = tc.filed
		res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		first := res.Value().Lines()[0].Parts()[0]
		if first.Days() != 261 {
			t.Errorf("paid %v: %d days at 2026's rate, want 261", tc.filed, first.Days())
		}
		if got := asked(res.Required(), working.PositionUnpublishedRate, "Interest for 2027"); got != tc.gap {
			t.Errorf("paid %v: unpublished rate reported %v, want %v", tc.filed, got, tc.gap)
		}
	}
}

// A payment between due dates pays the oldest underpayment first, on its
// date. A payment on a due date pays that installment first, and only the
// excess reaches an earlier one (32 V.S.A. § 5859(b)(2)(B)). Either way the
// whole underpayment is charged until it is paid in full (IN-152 line 7).
func TestEstimatedLatePaymentsAndFullPayment(t *testing.T) {
	// April missed, caught up on May 1: 16 days, $3.40, and one month, $10.
	h := fourThousand()
	h.payments = append([]estimated.Payment{pay(day(2026, 5, 1), 1_000*money.Dollar)}, quarterly(0, 1_000*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar)...)
	res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
	if lines := res.Value().Lines(); len(lines) != 1 || lines[0].Days() != 16 || res.Value().Total() != 1_340 {
		t.Errorf("caught up May 1: %d lines, total %v; want one of 16 days and $13.40", len(lines), res.Value().Total())
	}
	if !asked(res.Required(), working.PositionUnsettledRule, "the figures follow the law") {
		t.Errorf("the payment order is not reported: %v", res.Required())
	}

	// April missed, $1,500 on June 15: June is paid first, and $500 reaches
	// April. April's whole $1,000 runs to April 15, 2027. By the running
	// totals June is still short, $1,500 of $2,000, with no shortfall of its
	// own.
	h = fourThousand()
	h.payments = quarterly(0, 1_500*money.Dollar, 1_000*money.Dollar, 1_000*money.Dollar)
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	if sched[0].Status() != estimated.StatusShort || sched[1].Status() != estimated.StatusShort || sched[1].Shortfall() != 0 {
		t.Errorf("April %v, June %v with %v short; want both short by the running totals, June with no shortfall of its own", sched[0].Status(), sched[1].Status(), sched[1].Shortfall())
	}
	p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if len(p.Lines()) != 1 || p.Lines()[0].Shortfall() != 1_000*money.Dollar || p.Total() != 5_542+12_000 {
		t.Errorf("%d lines, total %v; want April's $1,000 at $55.42 and $120", len(p.Lines()), p.Total())
	}

	// The last $500 paid February 1, 2027: the whole $1,000 runs to then, 10
	// months, not just the $500 still owed.
	h.payments = append(h.payments, pay(day(2027, 2, 1), 500*money.Dollar))
	p = must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
	if p.Total() != 5_542+10_000 {
		t.Errorf("paid in full February 1: total %v, want $155.42", p.Total())
	}
}

// Withholding counts 25% on each due date, whenever it was withheld, and
// IN-152 has no line for the actual dates.
func TestEstimatedWithholdingIsSpreadEvenly(t *testing.T) {
	for _, asWithheld := range []bool{false, true} {
		// $9,000 required, all withheld December 15, $1,000 left to pay.
		h := household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar, asWithheld: asWithheld}
		res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		if res.Value().Total() != 0 {
			t.Errorf("election %v: a penalty of %v", asWithheld, res.Value().Total())
		}
		noted := false
		for _, a := range res.Applied() {
			noted = noted || strings.Contains(a.Applied(), "no line for the dates withholding was taken")
		}
		if noted != asWithheld {
			t.Errorf("election %v: the missing line noted %v", asWithheld, noted)
		}
	}
}

// A filer with two-thirds of gross income from farming or fishing, this year
// or last, makes no estimated payments. One basis point either side.
func TestEstimatedFarmersAndFishersAreExempt(t *testing.T) {
	for _, tc := range []struct {
		farm, fish money.Rate
		priorYear  bool
		exempt     bool
	}{
		{6_667, 0, false, true},
		{6_666, 0, false, false},
		{4_000, 2_667, false, true}, // farming and fishing together
		{3_333, 3_333, false, false},
		{0, 0, true, true},
	} {
		e := must(vermonttaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(tc.priorYear).Build())
		h := household{tax: 10_000 * money.Dollar, farm: tc.farm, fish: tc.fish, extras: &e}
		res := must(rules(t).Requirement(h.facts(t)))
		if res.Value().Exempt() != tc.exempt {
			t.Errorf("farm %v, fish %v, last year %v: exempt %v, want %v", tc.farm, tc.fish, tc.priorYear, res.Value().Exempt(), tc.exempt)
		}
		// The farmer rule is stated in the reason, not the explanation.
		if tc.exempt && !strings.Contains(res.Value().Why(), "two-thirds") {
			t.Errorf("farm %v, fish %v, last year %v: the reason does not state the rule: %s", tc.farm, tc.fish, tc.priorYear, res.Value().Why())
		}
		if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); (p.Value().Total() == 0) != tc.exempt {
			t.Errorf("farm %v, fish %v, last year %v: penalty %v", tc.farm, tc.fish, tc.priorYear, p.Value().Total())
		}
	}
	// Last year's farming not said: asked, and taken as not.
	f := must(estimated.NewFacts(vt, 2026).FilingStatus(filing.Single).ThisYear(must(estimated.NewFigures().Tax(10_000 * money.Dollar).Build())).Build())
	res := must(rules(t).Requirement(f))
	if res.Value().Exempt() || !asked(res.Required(), working.PositionJurisdictionFact, "come from farming or fishing") {
		t.Errorf("not said: exempt %v, asked %v", res.Value().Exempt(), res.Required())
	}
}

// IN-152A: each federal Schedule AI installment over the federal required
// annual payment, times the Vermont one.
func TestEstimatedAnnualisedInstallments(t *testing.T) {
	e := must(vermonttaxcalc.NewEstimatedFacts().PriorYearFarmerOrFisher(false).
		FederalAnnualised(20_000*money.Dollar, 1_000*money.Dollar, 3_000*money.Dollar, 6_000*money.Dollar, 10_000*money.Dollar).Build())
	h := household{tax: 10_000 * money.Dollar, annualised: true, extras: &e}
	sched := must(rules(t).Schedule(h.facts(t))).Value()
	// $9,000 ÷ $20,000 of $1,000, $3,000, $6,000 and $10,000.
	for i, want := range []money.USCents{450 * money.Dollar, 1_800 * money.Dollar, 4_500 * money.Dollar, 9_000 * money.Dollar} {
		if sched[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, sched[i].NeededBy(), want)
		}
	}

	// Without the federal figures, the regular quarters, and the question.
	h.extras = nil
	res := must(rules(t).Schedule(h.facts(t)))
	if res.Value()[0].NeededBy() != 2_250*money.Dollar || !asked(res.Required(), working.PositionJurisdictionFact, "Schedule AI") {
		t.Errorf("no federal figures: April needs %v, asked %v", res.Value()[0].NeededBy(), res.Required())
	}

	for name, b := range map[string]*vermonttaxcalc.EstimatedFactsBuilder{
		"three installments": vermonttaxcalc.NewEstimatedFacts().FederalAnnualised(100, 1, 2, 3),
		"no federal payment": vermonttaxcalc.NewEstimatedFacts().FederalAnnualised(0, 1, 2, 3, 4),
		"negative":           vermonttaxcalc.NewEstimatedFacts().FederalAnnualised(100, 1, -2, 3, 4),
	} {
		if _, err := b.Build(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The four examples on the Department's Calculate Interest and Penalties
// page. The first three state their answer; the fourth stops before it, and
// under the $500 rule it owes nothing.
func TestEstimatedDepartmentExamples(t *testing.T) {
	// The page is not checked in. The research quotes one phrase from these
	// examples, which TestEstimatedFiguresAreInTheirSources checks; the
	// examples' own figures are not checked against any text.
	last := prior(800 * money.Dollar)
	q := func(a money.USCents) []estimated.Payment { return quarterly(a, a, a, a) }
	for _, tc := range []struct {
		name     string
		h        household
		penalty  money.USCents
		required bool
	}{
		// $800 a quarter is $200, and $150 leaves $50 short each quarter:
		// $2.77 + $6, $2.12 + $5, $1.15 + $3.50 and $1.50, with the 2027 days
		// unpriced.
		{"one", household{tax: 1_000 * money.Dollar, prior: last, payments: q(150 * money.Dollar)}, 2_204, true},
		{"two", household{tax: 400 * money.Dollar, withheld: 100 * money.Dollar, prior: last}, 0, false},
		{"three", household{tax: 1_000 * money.Dollar, withheld: 400 * money.Dollar, prior: last, payments: q(100 * money.Dollar)}, 0, true},
		{"four", household{tax: 600 * money.Dollar, withheld: 110 * money.Dollar, prior: last}, 0, false},
	} {
		f := tc.h.facts(t)
		r := must(rules(t).Requirement(f)).Value()
		if r.Required() != tc.required {
			t.Errorf("example %s: required %v, want %v (%s)", tc.name, r.Required(), tc.required, r.Why())
		}
		if p := must(rules(t).Penalty(f, day(2027, 4, 15))); p.Value().Total() != tc.penalty {
			t.Errorf("example %s: penalty %v, want %v", tc.name, p.Value().Total(), tc.penalty)
		}
	}
}

// The penalty stops on the day the return is filed and paid, and never runs
// past April 15, 2027.
func TestEstimatedPenaltyStops(t *testing.T) {
	h := fourThousand()
	h.returnFiled = day(2027, 3, 1)
	if p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))); p.Value().AsOf() != day(2027, 3, 1) {
		t.Errorf("figured to %v, want the day the return was paid", p.Value().AsOf())
	}
	p := must(rules(t).Penalty(fourThousand().facts(t), day(2027, 6, 30)))
	if p.Value().AsOf() != day(2027, 4, 15) || p.Value().Total() != 44_082 {
		t.Errorf("figured to %v at %v, want April 15, 2027 and $440.82", p.Value().AsOf(), p.Value().Total())
	}
	if _, err := rules(t).Penalty(fourThousand().facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date was figured")
	}
}

// Each divergence is reported with both readings and both sources, and each
// statute reading is in its extract.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	mustContain(t, "vsa-32-5857.txt", "if the $500.00 minimum tax requirement is met")
	mustContain(t, "vsa-32-5859.txt",
		"(A) the amount of the installment that would be required to be paid if the estimated tax were equal to 90 percent of the tax shown on the return for the taxable year",
		"(1) an amount equal to the tax computed at the rate applicable to the taxable year but otherwise on the basis of the facts shown on the return for, and the law applicable to, the preceding taxable year",
		"(ii) with respect to any portion of the underpayment, the date on which such portion is paid.",
		"a payment of estimated tax on any installment date shall be considered a payment of any previous underpayment only to the extent such payment exceeds the amount of the installment",
		"the date a U.S. income tax return is required to be filed for that year by that corporation",
	)
	mustContain(t, "vsa-32-5852.txt",
		"an amount equal to the 24 percent of the taxpayer’s required payment for federal income tax purposes, reduced by a percentage equal to the percentage of the taxpayer’s adjusted gross income for the taxable year that is not Vermont income",
		"An equal part of such amount shall be deemed paid on each due date for such taxable year unless the taxpayer establishes the dates on which all amounts were actually withheld",
	)
	mustContain(t, "vsa-32-3004.txt", "falls on Sunday, such act shall be valid if done on the following Monday.")

	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"The $500 threshold":                   {"less than $500", "§ 5857"},
		"What a shortfall is measured against": {"lesser of 90%", "§ 5859(b)(1)(A)"},
		"The prior-year figure":                {"100% of last year", "§ 5859(c)(1)"},
		"When withholding counts":              {"25% on each due date", "§ 5852(c)"},
		"How a later payment is applied":       {"paid in full", "§ 5859(b)(2)(A)(ii)"},
		"The annualised installment":           {"Schedule AI", "§ 5852(b)"},
		"A due date on a weekend or holiday":   {"next business day", "§ 3004"},
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
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.StatuteSource(), w[1]) || d.FormSource() == "" || d.Statute() == "" {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s)", d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
		}
		// Every figure follows the forms. With no prior return, no payments
		// and no election, nothing these facts hold turns on a divergence.
		if d.Uses() != working.ReadingForm || d.AffectsHousehold() {
			t.Errorf("%q uses %v, affects these facts %v; want the form, and no", d.Rule(), d.Uses(), d.AffectsHousehold())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := vermonttaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(vt, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(vt, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Vermont's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := rules(t).Schedule(f); err == nil {
			t.Errorf("%s: scheduled", name)
		}
		if _, err := rules(t).Penalty(f, day(2027, 4, 15)); err == nil {
			t.Errorf("%s: penalty figured", name)
		}
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$500", "1% a month", "7.75%", "110%", "Child Care Contribution", "paid in full"} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
	if len(e.Sources()) == 0 {
		t.Error("the explanation cites nothing")
	}
}

// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
// never as a negative amount.
func TestEstimatedWithholdingOverTheTaxReadsAsARefund(t *testing.T) {
	figures := func(tax money.USCents) estimated.Figures {
		return must(estimated.NewFigures().Tax(tax).FederalAGI(60_000 * money.Dollar).StateAGI(60_000 * money.Dollar).Build())
	}
	last := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800 * money.Dollar)).Build())
	b := estimated.NewFacts("VT", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
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

// Vermont has no casualty, disaster, retirement or disability waiver, so a
// waiver the federal rules would allow is refused and the penalty stands.
func TestEstimatedWaiverIsRefused(t *testing.T) {
	for name, pos := range map[string]estimated.Positions{
		"casualty":           must(estimated.NewPositions().Waiver(estimated.WaiverCasualty).Build()),
		"disaster":           must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()),
		"retired at 64":      must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, time.March, 1)).Build()),
		"disabled this year": must(estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2026, 6, 1)).Build()),
	} {
		h := household{tax: 10_000 * money.Dollar, prior: prior(20_000 * money.Dollar), positions: &pos}
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value()
		o, ok := p.WaiverOutcome()
		if p.Total() == 0 || !ok || !o.Refused() || !strings.Contains(o.Reason(), "no waiver") {
			t.Errorf("%s: penalty %v, outcome %v %+v", name, p.Total(), ok, o)
		}
		if p.IfWaived() != p.Total() {
			t.Errorf("%s: %v if waived, want the whole %v", name, p.IfWaived(), p.Total())
		}
	}
}

// An exception sets the target, so last year's return is not asked for.
func TestEstimatedExemptDoesNotAskForLastYear(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 400 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() || asked(res.Required(), working.PositionPriorYearReturn, "2025") {
		t.Errorf("exempt %v, asked for last year %v", res.Value().Exempt(), res.Required())
	}
}
