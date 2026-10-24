// California's estimated tax rules for 2026, checked against the Franchise
// Tax Board's own documents.
//
// California publishes no exhaustive oracle for estimated tax. The strongest
// checks here are FTB 5805's worked payment example and its table of the most
// days in each rate period, which is the days arithmetic for a whole year.
// Every threshold, share, percentage, date and rate is compared against its
// extract in testdata/. The boundary cases are hand-written.
package californiataxcalc_test

import (
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/californiataxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

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
	return must(californiataxcalc.EstimatedTax(2026))
}

type household struct {
	status    filing.Status
	tax       money.USCents
	agi       *money.USCents // this year's California AGI
	withheld  money.USCents
	prior     *estimated.PriorReturn
	payments  []estimated.Payment
	group     bool
	noTax     bool
	positions *estimated.Positions // a waiver asked for, with its cause and dates
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
	if h.agi != nil {
		fb.StateAGI(*h.agi)
	} else {
		fb.StateAGI(100_000 * money.Dollar)
	}
	b := estimated.NewFacts(estimated.California, 2026).FilingStatus(status).ThisYear(must(fb.Build())).Paid(h.payments...)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 11, 30)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.group {
		b.State(must(californiataxcalc.NewEstimatedFacts().GroupReturn().Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

func prior(tax, agi, withheld money.USCents) *estimated.PriorReturn {
	f := must(estimated.NewFigures().Tax(tax).StateAGI(agi).Withheld(withheld).Build())
	p := must(estimated.NewPriorReturn().Filed(true).Figures(f).Build())
	return &p
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

func cents(a money.USCents) *money.USCents { return &a }

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "ftb-540es-2026-instructions.txt",
		"Installments due shall be 30% of the required annual payment for the 1st required installment, 40% of the required annual payment for the 2nd required installment, no installment is due for the 3rd required installment, and 30% of the required annual payment for the 4th required installment.",
		"you expect to owe at least $500 ($250 if married/RDP filing separately) in tax for 2026 (after subtracting withholding and credits)",
		"1. 90% of the tax shown on your 2026 tax return; or 2. 100% of the tax shown on your 2025 tax return including Alternative Minimum Tax (AMT).",
		"whose 2025 California adjusted gross income is more than $150,000 (or $75,000 if married/RDP filing separately) must figure estimated tax based on the lesser of 90% of their tax for 2026 or 110% of their tax for 2025",
		"Taxpayers with 2026 California adjusted gross income equal to or greater than $1,000,000 (or $500,000 if married/RDP filing separately) must figure estimated tax based on their tax for 2026.",
		"April 15, 2026 2nd payment",
		"June 15, 2026 3rd payment",
		"September 15, 2026 4th payment",
		"January 15, 2027 Filing an Early Tax Return",
		"If you file your 2026 tax return by January 31, 2027, and pay the entire balance due, you do not have to make your last estimated tax payment.")
	mustContain(t, "ftb-5805-2025.txt",
		"Applicable percentage . . . . . . . . . . . . . . . . . . . . . . . . . . . 15 27% 63% 63% 90%",
		"1/1/25 to 3/31/25 1/1/25 to 5/31/25 1/1/25 to 8/31/25 1/1/25 to 12/31/25",
		"Enter 30% of the amount shown on form FTB 5805, Part ll, line 6 in columns (a & d), enter 40% of the amount on line 6 in column b, enter -0- in column c.",
		"for either 2024 or 2025 was less than $500 (or less than $250 if married/RDP filing a separate return)",
		"Your 2024 return was for a full 12 months (or would have been if you were required to file) and you did not have any tax liability on that return.")
	mustContain(t, "ftb-5805-2025-instructions.txt",
		"Enter in column (a) and column (d) the result of the amount shown on form FTB 5805, Part ll, line 3 multiplied by 30%, enter -0- in column (c) and 40% for column (b).",
		"When the estimate due date falls on a weekend or holiday, the deadline to file and pay without penalty is extended to the next business day.",
		"Underpayment on line 8 (see instructions) × (Number of days on line 10 ÷ 365) × .08",
		"Note: A year is 365 days, a leap year is 366 days")
	mustContain(t, "ftb-estimate-penalty-rates.txt",
		"Current rates 07/01/2026 – 12/31/2026 7% | Personal income tax under- and overpayments | 7% | Corporation underpayment | 7% | Estimate penalties |",
		"07/01/2025 | 06/30/2026 | 7% |",
		"*Personal income tax underpayment and overpayment, corporation underpayment, and estimate penalty rate.")
	mustContain(t, "ftb-ab1518-analysis.txt",
		"make estimate payment penalties inapplicable for nonresident individuals electing to file in a group return",
		"would be specifically operative for taxable years beginning on or after January 1, 2026.")
}

// The rates in each part come from the FTB's rates page, period by period,
// and nothing after December 31, 2026 is charged.
func TestEstimatedRatesMatchTheFTBPage(t *testing.T) {
	f := household{tax: 40_000 * money.Dollar, prior: prior(40_000*money.Dollar, 100_000*money.Dollar, 0)}.facts(t)
	res := must(rules(t).Penalty(f, day(2027, 4, 15)))
	parts := res.Value().Lines()[0].Parts()
	if len(parts) != 2 {
		t.Fatalf("%d parts, want two rate periods", len(parts))
	}
	for i, w := range []struct {
		to     estimated.Date
		period string
	}{
		{day(2026, 6, 30), "07/01/2025 – 06/30/2026"},
		{day(2026, 12, 31), "07/01/2026 – 12/31/2026"},
	} {
		if parts[i].To() != w.to || parts[i].Rate() != money.Percent(7) || !strings.HasSuffix(parts[i].Source(), w.period) {
			t.Errorf("part %d: to %v at %v from %q", i, parts[i].To(), parts[i].Rate(), parts[i].Source())
		}
	}
	if !has(res.Required(), working.PositionUnpublishedRate) {
		t.Error("the unpublished 2027 rate is not reported")
	}
}

func TestEstimatedScheduleIsThirtyFortyZeroThirty(t *testing.T) {
	// 90% of $20,000 is $18,000, less than 110% of $17,000 = $18,700.
	f := household{tax: 20_000 * money.Dollar, withheld: 10_000 * money.Dollar, prior: prior(17_000*money.Dollar, 200_000*money.Dollar, 0)}.facts(t)
	res := must(rules(t).Schedule(f))
	wantDue := []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}
	wantNeeded := []money.USCents{5_400, 12_600, 12_600, 18_000}
	wantCredited := []money.USCents{3_000, 7_000, 7_000, 10_000} // withholding deemed 30/40/0/30
	for i, in := range res.Value() {
		if in.Due() != wantDue[i] || in.NeededBy() != wantNeeded[i]*money.Dollar || in.CreditedBy() != wantCredited[i]*money.Dollar {
			t.Errorf("installment %d: due %v, needed %v, credited %v", i+1, in.Due(), in.NeededBy(), in.CreditedBy())
		}
		switch in.Due().Weekday() {
		case time.Saturday, time.Sunday:
			t.Errorf("installment %d is due on a %v", i+1, in.Due().Weekday())
		}
	}
	// September adds nothing of its own, but 70% is needed by it, and $7,000
	// credited is short of $12,600.
	if sep := res.Value()[2]; sep.Status() != estimated.StatusShort || sep.Shortfall() != 0 {
		t.Errorf("September is %v with a shortfall of %v, want short with none of its own", sep.Status(), sep.Shortfall())
	}
	if len(res.Divergences()) == 0 {
		t.Error("the schedule carries no divergences")
	}
}

func TestEstimatedThresholdBoundary(t *testing.T) {
	p := prior(50_000*money.Dollar, 100_000*money.Dollar, 0)
	for _, tc := range []struct {
		status   filing.Status
		owed     money.USCents
		required bool
	}{
		{filing.Single, 500*money.Dollar - 1, false},
		{filing.Single, 500 * money.Dollar, true},
		{filing.MarriedFilingSeparately, 250*money.Dollar - 1, false},
		{filing.MarriedFilingSeparately, 250 * money.Dollar, true},
	} {
		// $2,000 withheld; 90% of the tax is more than that, so only the
		// threshold decides.
		f := household{status: tc.status, tax: 2_000*money.Dollar + tc.owed, withheld: 2_000 * money.Dollar, prior: p}.facts(t)
		res := must(rules(t).Requirement(f))
		if res.Value().Required() != tc.required {
			t.Errorf("%v owing %v: required %v, want %v (%s)", tc.status, tc.owed, res.Value().Required(), tc.required, res.Value().Why())
		}
	}
}

// 110% above $150,000 of 2025 California AGI ($75,000 separately), and no
// prior-year test at all from $1,000,000 of 2026 California AGI ($500,000
// separately).
func TestEstimatedHighIncomeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		status    filing.Status
		priorAGI  money.USCents
		thisAGI   money.USCents
		available bool
		percent   money.Rate
	}{
		{filing.Single, 150_000 * money.Dollar, 200_000 * money.Dollar, true, money.Percent(100)},
		{filing.Single, 150_000*money.Dollar + 1, 200_000 * money.Dollar, true, money.Percent(110)},
		{filing.MarriedFilingSeparately, 75_000 * money.Dollar, 200_000 * money.Dollar, true, money.Percent(100)},
		{filing.MarriedFilingSeparately, 75_000*money.Dollar + 1, 200_000 * money.Dollar, true, money.Percent(110)},
		{filing.Single, 900_000 * money.Dollar, 1_000_000*money.Dollar - 1, true, money.Percent(110)},
		{filing.Single, 900_000 * money.Dollar, 1_000_000 * money.Dollar, false, 0},
		{filing.MarriedFilingSeparately, 400_000 * money.Dollar, 500_000*money.Dollar - 1, true, money.Percent(110)},
		{filing.MarriedFilingSeparately, 400_000 * money.Dollar, 500_000 * money.Dollar, false, 0},
	} {
		f := household{status: tc.status, tax: 90_000 * money.Dollar, agi: cents(tc.thisAGI), prior: prior(10_000*money.Dollar, tc.priorAGI, 0)}.facts(t)
		res := must(rules(t).Requirement(f))
		opt := res.Value().Options()[1]
		if opt.Available() != tc.available || (tc.available && opt.Percent() != tc.percent) {
			t.Errorf("%v, prior AGI %v, this AGI %v: available %v at %v; want %v at %v (%s)",
				tc.status, tc.priorAGI, tc.thisAGI, opt.Available(), opt.Percent(), tc.available, tc.percent, opt.WhyNot())
		}
		if !tc.available && res.Value().Target() != 81_000*money.Dollar {
			t.Errorf("high income: target %v, want 90%% of this year's tax", res.Value().Target())
		}
	}
	// This year's AGI unknown: the prior-year test is withheld, and the gap
	// reported.
	f := must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(90_000 * money.Dollar).Build())).
		PriorYear(*prior(10_000*money.Dollar, 100_000*money.Dollar, 0)).Build())
	res := must(rules(t).Requirement(f))
	if res.Value().Options()[1].Available() || !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Error("with this year's California AGI unknown, the prior-year test was offered or the gap not reported")
	}
}

// No penalty when either year's tax less withholding was under $500, or last
// year had no liability, or the filer is a nonresident in a group return.
func TestEstimatedExceptions(t *testing.T) {
	zero := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(0).Build())).Build())
	for name, tc := range map[string]struct {
		h      household
		exempt bool
		label  string // the exception chosen, when exempt
	}{
		"last year under $500":   {household{tax: 30_000 * money.Dollar, prior: prior(5_000*money.Dollar, 90_000*money.Dollar, 4_500*money.Dollar+1)}, true, "Last year's tax less withholding under $500"},
		"last year exactly $500": {household{tax: 30_000 * money.Dollar, prior: prior(5_000*money.Dollar, 90_000*money.Dollar, 4_500*money.Dollar)}, false, ""},
		"no liability last year": {household{tax: 30_000 * money.Dollar, prior: &zero}, true, "No California tax liability last year"},
		"group return":           {household{tax: 30_000 * money.Dollar, group: true}, true, "Nonresident in a group return"},
		"this year under $500":   {household{tax: 2_400 * money.Dollar, withheld: 2_000 * money.Dollar}, true, "Tax less withholding under $500"},
		"none of them":           {household{tax: 30_000 * money.Dollar, prior: prior(20_000*money.Dollar, 90_000*money.Dollar, 0)}, false, ""},
	} {
		f := tc.h.facts(t)
		res := must(rules(t).Requirement(f))
		if exempt := res.Value().Exempt(); exempt != tc.exempt {
			t.Errorf("%s: exempt %v, want %v (%s)", name, exempt, tc.exempt, res.Value().Why())
		}
		// The exception is the chosen option, at $0, and the penalty's reason.
		c, chosen := res.Value().Chosen()
		if tc.exempt && (!chosen || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != tc.label) {
			t.Errorf("%s: chose %v %q at %v, want the exception %q", name, c.Basis(), c.Label(), c.Amount(), tc.label)
		}
		pen := must(rules(t).Penalty(f, day(2027, 4, 15)))
		if tc.exempt && (pen.Value().Total() != 0 || pen.Value().Reason() != tc.label+".") {
			t.Errorf("%s: a penalty of %v because %q when none can arise", name, pen.Value().Total(), pen.Value().Reason())
		}
	}
}

// Each divergence the research found is reported, with both readings and both
// sources, so a change to either is noticed.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 30_000 * money.Dollar}.facts(t)))
	want := map[string]struct {
		form, statute string
		uses          working.Reading
	}{
		"Nonresidents in a group return":  {"This exception isn't in the 2026 Form 540-ES instructions or the 2025 FTB 5805", "§ 19136(j)", working.ReadingStatute},
		"Underpayments caused by the FTB": {"An underpayment the FTB caused isn't mentioned", "§ 19136.7(a)", working.ReadingForm},
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
		if !strings.Contains(d.Form(), w.form) || !strings.Contains(d.StatuteSource(), w.statute) || d.FormSource() == "" || d.Statute() == "" || d.Uses() != w.uses {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s), uses %v", d.Rule(), d.Form(), d.Statute(), d.StatuteSource(), d.Uses())
		}
		// A single resident with no withholding is touched by none of them.
		if d.AffectsHousehold() {
			t.Errorf("%q affects a resident with no withholding and no group return", d.Rule())
		}
	}
	// Withholding is touched by the first, and a group return by the second.
	affects := func(h household, rule string) bool {
		for _, d := range must(rules(t).Requirement(h.facts(t))).Divergences() {
			if d.Rule() == rule {
				return d.AffectsHousehold()
			}
		}
		t.Fatalf("no divergence %q", rule)
		return false
	}
	if !affects(household{tax: 30_000 * money.Dollar, group: true}, "Nonresidents in a group return") {
		t.Error("the group-return divergence does not affect a nonresident in a group return")
	}
	// The form's wording about withholding is in the form.
	mustContain(t, "ftb-5805-2025.txt", "Was your California withholding not withheld in equal installments")
}

// FTB 5805's example: $500 short on April 15, then $1,200 paid June 10 for
// June's $1,200. $500 goes to April and $700 to June.
func TestPaymentApplicationMatchesFTB5805Example(t *testing.T) {
	mustContain(t, "ftb-5805-2025-instructions.txt",
		"Example: You had an underpayment for the April 15th installment of $500. The June 15th installment required a payment of $1,200. On June 10th, you sent in a payment of $1,200 to cover the June 15th installment. However, $500 of this payment is considered to be for the April 15th installment. The penalty for the April 15th installment is figured to June 10th. The amount of the payment to be applied to the June 15th installment is $700.")
	dues := []estimated.Date{day(2025, 4, 15), day(2025, 6, 15)}
	got := must(estimated.Apply(dues, []money.USCents{100_000, 120_000}, []estimated.Payment{
		pay(day(2025, 4, 15), 50_000),
		pay(day(2025, 6, 10), 120_000),
	}))
	on, _ := got[0].Pieces()[0].PaidOn()
	if got[0].Shortfall() != 50_000 || on != day(2025, 6, 10) {
		t.Errorf("April: %v short, paid %v", got[0].Shortfall(), on)
	}
	if got[1].Shortfall() != 50_000 {
		t.Errorf("June is %v short, want $1,200 less the $700 left", got[1].Shortfall())
	}
}

// The 5805 instructions print the most days each installment can spend in
// each 2025 rate period. Accruing a shortfall that is never paid, with the
// 2025 rates they print, has to give those days.
func TestDaysPerRatePeriodMatchFTB5805(t *testing.T) {
	text := extract(t, "ftb-5805-2025-instructions.txt")
	mustContain(t, "ftb-5805-2025-instructions.txt", "8% 4-15-25 through 6-30-25 7% 7-1-25 through 4-15-26")
	// The table's header ends "Period 1 | Period 2 |", and its rows follow.
	const header = "Maximum days in a rate period per quarter: Installment | Days in Rate | | Period 1 | Period 2 |"
	i := strings.Index(text, header)
	if i < 0 {
		t.Fatal("the days table is not in the extract")
	}
	i += len(header)
	rows := regexp.MustCompile(`([1-4]) \| (?:(\d+) )?\| (\d+) \|`).FindAllStringSubmatch(text[i:], 4)
	if len(rows) != 4 {
		t.Fatalf("found %d rows of the days table, want 4", len(rows))
	}
	s := must(estimated.NewRateSeries().
		Period(day(2025, 4, 15), day(2025, 6, 30), money.Percent(8), "8% 4-15-25 through 6-30-25").
		Period(day(2025, 7, 1), day(2026, 4, 15), money.Percent(7), "7% 7-1-25 through 4-15-26").
		Build())
	dues := []estimated.Date{day(2025, 4, 15), day(2025, 6, 15), day(2025, 9, 15), day(2026, 1, 15)}
	for n, due := range dues {
		parts, _, err := estimated.SimpleDaily(100_000, due, day(2026, 4, 15), s)
		if err != nil {
			t.Fatal(err)
		}
		var period1, period2 int
		for _, p := range parts {
			if p.Rate() == money.Percent(8) {
				period1 += p.Days()
			} else {
				period2 += p.Days()
			}
		}
		row := rows[n]
		want1, _ := strconv.Atoi(row[2])
		want2, _ := strconv.Atoi(row[3])
		if period1 != want1 || period2 != want2 {
			t.Errorf("installment %s: %d and %d days, FTB 5805 says %d and %d", row[1], period1, period2, want1, want2)
		}
		if len(parts) > 2 {
			t.Errorf("installment %s splits into %d parts; the worksheet has one line per rate period", row[1], len(parts))
		}
	}
}

// A shortfall across both 2026 rate periods: $10,000 short on April 15, paid
// August 14, is 76 days and 45 days at 7%, each over 365.
func TestEstimatedPenaltyAcrossTwoRatePeriods(t *testing.T) {
	// 90% of $40,000 is $36,000 against 100% of last year's $20,000, so
	// $20,000 is required: $6,000, $8,000, $0 and $6,000.
	f := household{
		tax:   40_000 * money.Dollar,
		prior: prior(20_000*money.Dollar, 100_000*money.Dollar, 0),
		payments: []estimated.Payment{
			pay(day(2026, 6, 15), 8_000*money.Dollar),
			pay(day(2026, 8, 14), 6_000*money.Dollar),
			pay(day(2027, 1, 15), 6_000*money.Dollar),
		},
	}.facts(t)
	res := must(rules(t).Penalty(f, day(2027, 4, 15)))
	// June's $8,000 pays April's $6,000 first, leaving June $6,000 short; the
	// August payment then pays June. April: 600,000¢ × 61 × 7% / 365 =
	// 7,019.18¢. June: 600,000¢ × 15 × 7% / 365 = 1,726.03¢ to June 30, then
	// × 45 × 7% / 365 = 5,178.08¢.
	lines := res.Value().Lines()
	if len(lines) != 2 {
		t.Fatalf("%d lines, want April and June", len(lines))
	}
	if got := lines[0].Total(); got != 7_019 {
		t.Errorf("April's penalty is %v, want $70.19", got)
	}
	june := lines[1].Parts()
	if len(june) != 2 || june[0].Charge() != 1_726 || june[1].Charge() != 5_178 || june[0].Source() == june[1].Source() {
		t.Errorf("June's parts: %+v", june)
	}
	if res.Value().Total() != 7_019+1_726+5_178 {
		t.Errorf("penalty %v", res.Value().Total())
	}
}

// January 31, 2027 is a Sunday. The 540-ES prints January 31 and does not
// say it moves, so a return filed Monday, February 1 does not excuse the
// January installment here.
func TestEstimatedFileAndPayByJanuary31(t *testing.T) {
	for _, tc := range []struct {
		filed   estimated.Date
		excused bool
	}{
		{day(2027, 1, 29), true},
		{day(2027, 2, 1), false},
	} {
		f := must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).
			ThisYear(must(estimated.NewFigures().Tax(20_000 * money.Dollar).StateAGI(100_000 * money.Dollar).Build())).
			ReturnFiled(tc.filed).Build())
		res := must(rules(t).Schedule(f))
		if got := res.Value()[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v: excused %v, want %v", tc.filed, got, tc.excused)
		}
	}
}

// The annualised method uses 27%, 63%, 63% and 90%.
func TestEstimatedAnnualisedPercentages(t *testing.T) {
	ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
	var periods []estimated.Period
	for _, e := range ends {
		periods = append(periods, must(estimated.NewPeriod().End(e).AnnualisedTax(10_000*money.Dollar).Build()))
	}
	f := must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(20_000 * money.Dollar).StateAGI(100_000 * money.Dollar).Build())).
		Elections(must(estimated.NewElections().Annualised().Build())).
		Periods(periods...).Build())
	res := must(rules(t).Schedule(f))
	// Regular: 30/40/0/30 of $18,000 = 5,400, 7,200, 0, 5,400. Annualised on a
	// flat $10,000: 2,700, then 6,300 − 2,700 = 3,600, then 0, then 9,000 −
	// 6,300 = 2,700. The smaller each time, with what the method lowered
	// carried to the next regular amount.
	want := []money.USCents{2_700, 6_300, 6_300, 9_000}
	for i, in := range res.Value() {
		if in.NeededBy() != want[i]*money.Dollar {
			t.Errorf("installment %d needs %v by its date, want %v", i+1, in.NeededBy(), want[i]*money.Dollar)
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := californiataxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	farmer := must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).FishShare(7000).Build())
	if _, err := rules(t).Requirement(farmer); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a fisher: %v, want ErrUnsupported", err)
	}
	wrong := must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).State("not California's").Build())
	if _, err := rules(t).Requirement(wrong); err == nil {
		t.Error("extras of the wrong type were accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	for _, phrase := range []string{"30%, 40%, 0% and 30%", "$500", "$1,000,000", "27%, 63%, 63% and 90%"} {
		if !strings.Contains(e.VersusFederal(), phrase) {
			t.Errorf("the comparison with federal does not mention %s", phrase)
		}
	}
	if len(e.Body()) < 3 {
		t.Error("the explanation is thin")
	}
}

// A return filed with the whole balance paid stops the penalty that day.
func TestEstimatedPenaltyStopsWhenTheReturnIsPaid(t *testing.T) {
	f := must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(20_000 * money.Dollar).StateAGI(100_000 * money.Dollar).Build())).
		ReturnFiled(day(2027, 2, 26)).Build())
	res := must(rules(t).Penalty(f, day(2027, 4, 15)))
	if res.Value().AsOf() != day(2027, 2, 26) || res.Value().Total() == 0 {
		t.Fatalf("penalty %v figured to %v, want one figured to the day the return was paid", res.Value().Total(), res.Value().AsOf())
	}
	for _, l := range res.Value().Lines() {
		if l.Days() != day(2027, 2, 26).DaysSince(l.Due()) {
			t.Errorf("installment %d ran %d days, want to February 26", l.Installment(), l.Days())
		}
	}
}

// Without this year's tax, California's target is last year's tax alone, the
// installments are 30%, 40%, 0% and 30% of it, and the penalty is the most it
// can be.
func TestEstimatedWithoutThisYearsTaxUsesLastYearsAlone(t *testing.T) {
	h := household{noTax: true, prior: prior(5_000*money.Dollar, 100_000*money.Dollar, 0)}
	res := must(rules(t).Requirement(h.facts(t)))
	r := res.Value()
	if !r.Required() || r.Target() != 5_000*money.Dollar {
		t.Errorf("required %v, target %v; want $5,000 of last year's tax: %s", r.Required(), r.Target(), r.Why())
	}
	if o := r.Options()[0]; o.Available() || o.WhyNot() != "Needs this year's tax" {
		t.Errorf("this year's option is available %v, %q", o.Available(), o.WhyNot())
	}
	if !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("this year's tax is not asked for: %v", res.Required())
	}
	sched := must(rules(t).Schedule(h.facts(t)))
	for i, want := range []money.USCents{1_500, 3_500, 3_500, 5_000} {
		if got := sched.Value()[i].NeededBy(); got != want*money.Dollar {
			t.Errorf("installment %d needs %v, want $%d", i+1, got, want)
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

// Without this year's tax or this year's California AGI, the prior-year test
// cannot be offered either, because § 19136.3 withdraws it at $1,000,000:
// there is no target, and both questions are asked.
func TestEstimatedWithoutThisYearsTaxOrAGIHasNoTarget(t *testing.T) {
	// household always gives an AGI, so these facts are built here.
	noFigures := must(estimated.NewFigures().Build())
	facts := must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(noFigures).
		PriorYear(*prior(5_000*money.Dollar, 100_000*money.Dollar, 0)).Build())
	res := must(rules(t).Requirement(facts))
	if res.Value().TargetKnown() || res.Value().Required() {
		t.Errorf("target known %v, required %v; want no target", res.Value().TargetKnown(), res.Value().Required())
	}
	if !has(res.Required(), working.PositionThisYearTax) || !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Errorf("want this year's tax and AGI asked for: %v", res.Required())
	}
	if _, err := rules(t).Schedule(facts); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("Schedule: %v, want ErrNeedsThisYearTax", err)
	}
	if _, err := rules(t).Penalty(facts, day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("Penalty: %v, want ErrNeedsThisYearTax", err)
	}
}

// The waivers are decided from the conditions the FTB 5805 instructions states: a
// casualty or disaster, or retiring after 62 or becoming disabled in the tax
// year or the year before. The penalty is still figured in full.
func TestEstimatedWaiverIsDecidedFromItsConditions(t *testing.T) {
	mustContain(t, "ftb-5805-2025-instructions.txt", "You retired after age 62 or became disabled in 2024 or 2025 and your underpayment was due to reasonable cause.")
	base := household{tax: 40_000 * money.Dollar, prior: prior(40_000*money.Dollar, 100_000*money.Dollar, 0)}
	before := must(rules(t).Penalty(base.facts(t), day(2027, 4, 15)))
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
		p := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		o, ok := p.Value().WaiverOutcome()
		switch {
		case !ok:
			t.Errorf("%s: the penalty has no waiver outcome", c.name)
		case o.Eligible() != c.eligible || o.Refused() != c.refused:
			t.Errorf("%s: eligible %v, refused %v: %q", c.name, o.Eligible(), o.Refused(), o.Reason())
		case !strings.Contains(o.Reason(), c.reason):
			t.Errorf("%s: the reason %q does not say %q", c.name, o.Reason(), c.reason)
		case c.eligible && !strings.Contains(strings.ToLower(o.Reason()), "the franchise tax board"):
			t.Errorf("%s: the reason %q does not name %s", c.name, o.Reason(), "the franchise tax board")
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
		{"little due last year, this year's tax not given", household{noTax: true, prior: prior(600*money.Dollar, 30_000*money.Dollar, 400*money.Dollar)}, true},
		{"much due last year, this year's tax not given", household{noTax: true, prior: prior(6_000*money.Dollar, 70_000*money.Dollar, 1_000*money.Dollar)}, false},
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
