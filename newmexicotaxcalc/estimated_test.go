// New Mexico's estimated tax rules for 2026, checked against the Taxation and
// Revenue Department's own documents and the statute.
//
// New Mexico publishes no worked penalty example. Form RPD-41272 defines the
// line arithmetic, and these tests follow it: each part of each shortfall,
// by the day ÷ 365, at each quarter's rate. Every threshold, rate, date and
// percentage is compared against its extract in testdata/. The cases are
// hand-written.
package newmexicotaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/newmexicotaxcalc"
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
	return must(newmexicotaxcalc.EstimatedTax(2026))
}

func because(rs []working.Required, k working.PositionKind, phrase string) bool {
	for _, r := range rs {
		if r.Position() == k && r.Asked() && (strings.Contains(r.Question(), phrase) || strings.Contains(r.Effect(), phrase)) {
			return true
		}
	}
	return false
}

func has(rs []working.Required, k working.PositionKind) bool { return because(rs, k, "") }

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

var dues = []estimated.Date{day(2026, 4, 15), day(2026, 6, 15), day(2026, 9, 15), day(2027, 1, 15)}

type household struct {
	tax         money.USCents
	noTax       bool
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *newmexicotaxcalc.EstimatedFacts
	firstMet    estimated.Date
	returnFiled estimated.Date
	elections   *estimated.Elections
	farmShare   money.Rate
	fishShare   money.Rate
	positions   *estimated.Positions
}

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	figures := must(estimated.NewFigures().Tax(h.tax).Build())
	if h.noTax {
		figures = must(estimated.NewFigures().Build())
	}
	b := estimated.NewFacts("NM", 2026).FilingStatus(filing.Single).ThisYear(figures).
		Paid(h.payments...).RequirementFirstMet(h.firstMet).ReturnFiled(h.returnFiled).FarmShare(h.farmShare).FishShare(h.fishShare)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	} else {
		b.State(must(newmexicotaxcalc.NewEstimatedFacts().FirstYearResident(false).Build()))
	}
	if h.elections != nil {
		b.Elections(*h.elections)
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

func prior(tax money.USCents, resident bool) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).FullYearResident(resident).Build())
	return &p
}

func penalty(t *testing.T, h household) working.Result[estimated.Penalty] {
	t.Helper()
	return must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "nmsa-7-2-12.2-hb470-2011.txt",
		"ninety percent of the tax shown on the return of the taxable year",
		"one hundred percent of the tax shown on the return for the preceding taxable year if the preceding taxable year was a taxable year of twelve months and the taxpayer filed a New Mexico tax return for that preceding taxable year",
		"There shall be four required installments for each taxable year.",
		"after March 31 and before June 1, fifty percent of the required annual payment must be paid on or before June 15",
		"after May 31, but before September 1, the taxpayer must pay seventy-five percent",
		"after August 31, the taxpayer must pay one hundred percent",
		"April 15, June 15 and September 15 of the taxable year and January 15 of the following taxable year.",
		"An equal part of the amount of withheld tax shall be deemed paid on each due date",
		"the fifteenth day of the fourth month following the close of the taxable year",
		"credited against unpaid or underpaid installments in the order in which the installments are required to be paid.",
		"less than one thousand dollars ($1,000)",
		"the taxpayer was a resident of New Mexico for the entire taxable year;",
		"through either withholding or estimated tax payments, the taxpayer paid the required annual payment",
		"If on or before January 31 of the following taxable year the taxpayer files a return",
		"The provisions of this section do not apply to first-year residents.",
	)
	mustContain(t, "rpd-41272-2025.txt",
		"is less than $1,000. In this case, you do not owe a penalty.",
		"5. Multiply column 2, line 4 by 0.90 (90%) and enter here.",
		"multiply the required annual payment in line 7 by 0.25 (25%). Enter this amount in each column of line 12.",
		"If you were required to make estimated tax payments after March 31 but before June 1, multiply the required annual payment, in line 7 by 0.50 (50%)",
		"Divide the tax withheld for the 2025 tax year by four.",
		"the income tax withheld on 2024 PIT-1 is less than $1,000",
		"Underpayment on line 21 X number of days on line 25 X ___%",
		"365",
	)
	mustContain(t, "rpd-41272-2025-instructions.txt",
		"If the due date falls on a Saturday, Sunday, or a Federal/State holiday the due date is the next business day.",
		"the interest rate that applies to the first quarter following the tax year also applies to the first 15 days of the fourth month (April) following the tax year.",
		"If the required annual payment was not made in timely installments",
		"If you file a return and pay the full amount on or before January 31 of the next year, no penalty applies to the 4th estimated payment.",
		"You are a first-year resident;",
	)
	mustContain(t, "trd-penalty-interest-rates.txt",
		"July 1, 2026 – December 31, 2026 | 7% | 0.019178082% |",
		"April 1, 2026 – June. 30, 2026 | 6% | 0.016438356%",
	)
	mustContain(t, "trd-estimated-payments.txt", "is more than $1,000")
	text := extract(t, "nm-table-of-changes-2026-chapter-7.txt")
	if !strings.Contains(text, "Second Session, 2026") || !strings.Contains(text, "7- 1- 69") || strings.Contains(text, "7- 2- 12. 2") || strings.Contains(text, "7- 1- 67") {
		t.Error("the 2026 Table of Changes extract does not show §§ 7-2-12.2 and 7-1-67 unamended")
	}
}

// Nothing paid on $9,000 required. April's $2,250 runs 76 days at 6% to June
// 30 and 184 days at 7% to December 31; the rate changes on July 1, so the
// shortfall is charged in two parts. The days from January 1, 2027 have no
// published rate.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	res := penalty(t, household{tax: 10_000 * money.Dollar})
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	want := []struct {
		days    []int
		rates   []money.Rate
		charges []money.USCents
	}{
		{[]int{76, 184}, []money.Rate{money.Percent(6), money.Percent(7)}, []money.USCents{2_811, 7_940}},
		{[]int{15, 184}, []money.Rate{money.Percent(6), money.Percent(7)}, []money.USCents{555, 7_940}},
		{[]int{107}, []money.Rate{money.Percent(7)}, []money.USCents{4_617}},
		{nil, nil, nil},
	}
	for i, l := range lines {
		parts := l.Parts()
		if len(parts) != len(want[i].days) {
			t.Errorf("installment %d: %d parts, want %d", i+1, len(parts), len(want[i].days))
			continue
		}
		for j, p := range parts {
			if p.Days() != want[i].days[j] || p.Rate() != want[i].rates[j] || p.Charge() != want[i].charges[j] {
				t.Errorf("installment %d part %d: %d days at %v, %v; want %d at %v, %v", i+1, j, p.Days(), p.Rate(), p.Charge(), want[i].days[j], want[i].rates[j], want[i].charges[j])
			}
		}
	}
	if res.Value().Total() != 23_863 {
		t.Errorf("penalty %v, want $238.63", res.Value().Total())
	}
	if !because(res.Required(), working.PositionUnpublishedRate, "December 31, 2026") {
		t.Error("the unpublished 2027 rate is not reported")
	}
	p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2026, 12, 31)))
	if p.Value().Total() != 23_863 || has(p.Required(), working.PositionUnpublishedRate) {
		t.Errorf("to December 31: %v; want $238.63 and no gap", p.Value().Total())
	}
}

// A payment stops the charge on what it pays: April's $2,250 paid May 1 runs
// 16 days at 6%, $5.92.
func TestEstimatedPaymentStopsTheCharge(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 5, 1), 2_250*money.Dollar), pay(dues[1], 2_250*money.Dollar), pay(dues[2], 2_250*money.Dollar), pay(dues[3], 2_250*money.Dollar)}}
	res := penalty(t, h)
	if l := res.Value().Lines(); len(l) != 1 || l[0].Days() != 16 || res.Value().Total() != 592 {
		t.Errorf("penalty %v over %v; want $5.92 for 16 days", res.Value().Total(), l)
	}
}

// The dates the statute gives, a quarter each, and none moves.
func TestEstimatedScheduleForTwentyTwentySix(t *testing.T) {
	s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar}.facts(t))).Value()
	for i, in := range s {
		if in.Due() != dues[i] {
			t.Errorf("installment %d is due %v, want %v", i+1, in.Due(), dues[i])
		}
		if wd := in.Due().Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("installment %d falls on a %v", i+1, wd)
		}
		if want := money.USCents(i+1) * 2_250 * money.Dollar; in.NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, in.NeededBy(), want)
		}
	}
}

// A filer first required to pay after March 31 catches up: 50% by June 15
// from April 1, 75% by September 15 from June 1, all by January 15 from
// September 1.
func TestEstimatedLateStart(t *testing.T) {
	for _, tc := range []struct {
		met  estimated.Date
		need []money.USCents
	}{
		{day(2026, 3, 31), []money.USCents{225_000, 450_000, 675_000, 900_000}},
		{day(2026, 4, 1), []money.USCents{0, 450_000, 675_000, 900_000}},
		{day(2026, 5, 31), []money.USCents{0, 450_000, 675_000, 900_000}},
		{day(2026, 6, 1), []money.USCents{0, 0, 675_000, 900_000}},
		{day(2026, 9, 1), []money.USCents{0, 0, 0, 900_000}},
	} {
		s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, firstMet: tc.met}.facts(t))).Value()
		for i, want := range tc.need {
			if s[i].NeededBy() != want {
				t.Errorf("first met %v: installment %d needs %v, want %v", tc.met, i+1, s[i].NeededBy(), want)
			}
		}
	}
	if p := penalty(t, household{tax: 10_000 * money.Dollar}); !has(p.Required(), working.PositionRequirementFirstMet) {
		t.Error("when the requirement arose is not asked")
	}
}

// Tax less withholding under $1,000 owes nothing; exactly $1,000 can.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{{900_001, true}, {900_000, false}} {
		if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld}.facts(t))); res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v", tc.withheld, res.Value().Exempt(), tc.exempt)
		}
	}
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: 900_001}.facts(t)))
	if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding under $1,000" {
		t.Errorf("chosen %v: %v at %v labelled %q", ok, c.Basis(), c.Amount(), c.Label())
	}
	// Exactly $1,000 with nothing paid: the department's page says no
	// penalty, the form and the statute say there can be one.
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: 900_000}.facts(t)))
	for _, d := range res.Divergences() {
		if d.Rule() == "The $1,000 test" && !d.AffectsHousehold() {
			t.Error("exactly $1,000: the $1,000 test does not affect the household")
		}
	}
}

// The lesser of 90% of this year's tax and 100% of last year's, from a filed
// twelve-month New Mexico return.
func TestEstimatedTargetOptions(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(8_999*money.Dollar, true)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisPriorYearTax || res.Value().Target() != 8_999*money.Dollar {
		t.Errorf("chose %v for %v; want last year's $8,999", c.Basis(), res.Value().Target())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(9_001*money.Dollar, true)}.facts(t)))
	if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisThisYearTax || res.Value().Target() != 9_000*money.Dollar {
		t.Errorf("chose %v for %v; want 90%%, $9,000", c.Basis(), res.Value().Target())
	}
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(100).Build())).Build())
	for name, p := range map[string]*estimated.PriorReturn{"none on record": nil, "not filed": &notFiled, "eleven months": &short} {
		if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p}.facts(t))); res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
	}
}

// First-year residents and full-year residents with no liability last year
// owe nothing.
func TestEstimatedExemptions(t *testing.T) {
	first := must(newmexicotaxcalc.NewEstimatedFacts().FirstYearResident(true).Build())
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &first}.facts(t))); !res.Value().Exempt() || !strings.Contains(res.Value().Why(), "first year as a New Mexico resident") {
		t.Errorf("first-year resident: %s", res.Value().Why())
	} else if c, _ := res.Value().Chosen(); c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "First year as a New Mexico resident" {
		t.Errorf("first-year resident: chosen %v at %v labelled %q", c.Basis(), c.Amount(), c.Label())
	}
	if p := penalty(t, household{tax: 10_000 * money.Dollar, extras: &first}); p.Value().Reason() != "First year as a New Mexico resident." {
		t.Errorf("first-year resident: penalty reason %q", p.Value().Reason())
	}
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0, true)}.facts(t))); !res.Value().Exempt() {
		t.Errorf("no liability as a resident: %s", res.Value().Why())
	}
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0, false)}.facts(t))); res.Value().Exempt() {
		t.Error("no liability as a part-year resident is exempt")
	}
	unanswered := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(0).Build())).Build())
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: &unanswered}.facts(t)))
	if res.Value().Exempt() || !has(res.Required(), working.PositionResidency) {
		t.Errorf("residence not given: exempt %v", res.Value().Exempt())
	}
	none := must(newmexicotaxcalc.NewEstimatedFacts().Build())
	if p := penalty(t, household{tax: 10_000 * money.Dollar, extras: &none}); !because(p.Required(), working.PositionJurisdictionFact, "first year as a New Mexico resident") || !has(p.Required(), working.PositionPenaltyWaiver) {
		t.Error("first-year residence or the waiver is not asked")
	}
}

// January 31, 2027 is a Sunday and no source says whether it moves.
func TestEstimatedJanuaryThirtyFirst(t *testing.T) {
	for _, tc := range []struct {
		filed   estimated.Date
		moves   bool
		excused bool
		asked   bool
	}{
		{day(2027, 1, 29), false, true, false},
		{day(2027, 2, 1), false, false, true},
		{day(2027, 2, 1), true, true, false},
		{day(2027, 2, 2), true, false, false},
	} {
		e := must(newmexicotaxcalc.NewEstimatedFacts().FirstYearResident(false).Build())
		if tc.moves {
			e = must(newmexicotaxcalc.NewEstimatedFacts().FirstYearResident(false).JanuaryThirtyFirstMoves(true).Build())
		}
		res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: tc.filed, extras: &e}.facts(t)))
		if got := res.Value()[3].Status() == estimated.StatusExcused; got != tc.excused {
			t.Errorf("filed %v, moves %v: excused %v, want %v", tc.filed, tc.moves, got, tc.excused)
		}
		if got := because(res.Required(), working.PositionUnsettledRule, "a Sunday"); got != tc.asked {
			t.Errorf("filed %v, moves %v: asked %v, want %v", tc.filed, tc.moves, got, tc.asked)
		}
	}
}

// Withholding counts in equal parts unless the filer shows the dates.
func TestEstimatedWithholding(t *testing.T) {
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar}); p.Value().Total() != 0 {
		t.Errorf("spread evenly: penalty %v", p.Value().Total())
	}
	asWithheld := must(estimated.NewElections().WithholdingAsWithheld().Build())
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar, elections: &asWithheld}); p.Value().Total() == 0 {
		t.Error("on its December date: no penalty")
	}
}

// Without this year's tax, the target is last year's alone and the penalty is
// the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000*money.Dollar, true)}
	res := must(rules(t).Requirement(h.facts(t)))
	if r := res.Value(); !r.Required() || r.Target() != 8_000*money.Dollar || r.Options()[0].WhyNot() != estimated.NeedsThisYearTax || !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("required %v, target %v", r.Required(), r.Target())
	}
	if p := penalty(t, h); !(p.Value().UpperBound() || !p.Value().RatedThrough().IsZero()) || p.Value().Total() == 0 {
		t.Errorf("penalty %v, upper bound %v", p.Value().Total(), p.Value().UpperBound())
	}
	none := household{noTax: true}
	if _, err := rules(t).Schedule(none.facts(t)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("schedule: %v", err)
	}
	if _, err := rules(t).Penalty(none.facts(t), day(2027, 4, 15)); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("penalty: %v", err)
	}
}

// Each divergence is reported with both readings and both sources.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar}.facts(t)))
	want := map[string][2]string{
		"Paying the required annual payment": {"timely installments", "(H)(3)"},
		"The withholding line 4 compares":    {"2024 PIT-1", "(H)(1)"},
		"The $1,000 test":                    {"more than $1,000", "(H)(1)"},
		"An annualized method":               {"late start", "§ 7-2-12.2(C)"},
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
		// $10,000 due, nothing withheld or paid, not annualized: no reading
		// changes the figures.
		wantUses := working.ReadingForm
		if d.Rule() == "The withholding line 4 compares" {
			wantUses = working.ReadingStatute
		}
		if d.Uses() != wantUses || d.AffectsHousehold() {
			t.Errorf("%q uses %v, affects %v; want %v, not affecting", d.Rule(), d.Uses(), d.AffectsHousehold(), wantUses)
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := newmexicotaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	if _, err := rules(t).Requirement(household{tax: 10_000 * money.Dollar, farmShare: 7000}.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer: %v, want ErrUnsupported", err)
	}
	last := must(newmexicotaxcalc.NewEstimatedFacts().FarmerLastYear(true).Build())
	if _, err := rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &last}.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer last year: %v, want ErrUnsupported", err)
	}
	if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, farmShare: 1000}.facts(t))); !because(res.Required(), working.PositionJurisdictionFact, "farmers' rules") {
		t.Error("some farm income and last year not given: not asked")
	}
	if _, err := rules(t).Requirement(household{tax: 10_000 * money.Dollar, fishShare: 7000}.facts(t)); err != nil {
		t.Errorf("a fisher: %v", err)
	}
	this := must(estimated.NewFigures().Tax(100_000).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts("NM", 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts("NM", 2026).FilingStatus(filing.Single).ThisYear(this).State([]int{}).Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date is accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + strings.Join(e.Body(), " ") + e.VersusFederal()
	for _, s := range []string{"$1,000", "110%", "March 31", "6%", "7%", "April 1 to 15"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// Withholding over the tax leaves a refund, never a negative balance.
func TestWithholdingOverTheTaxReadsAsARefund(t *testing.T) {
	var withheld []estimated.Withheld
	for m := time.January; m <= time.December; m++ {
		withheld = append(withheld, must(estimated.NewWithheld().On(day(2026, m, 28)).Amount(money.Dollars(300)).Build()))
	}
	figures := func(tax, agi int64) estimated.Figures {
		return must(estimated.NewFigures().Tax(money.Dollars(tax)).FederalAGI(money.Dollars(agi)).StateAGI(money.Dollars(agi)).Build())
	}
	prior := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800, 55_000)).Build())
	f := must(estimated.NewFacts(estimated.Jurisdiction("NM"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}

// New Mexico has no casualty, retirement or disability waiver. Its one
// waiver, NMSA 1978 § 7-2-12.2(H)(4), turns on the department finding no
// fraud, negligence or disregard, so a retirement the federal rules would
// waive is undecided.
func TestEstimatedWaiverOutcomes(t *testing.T) {
	mustContain(t, "nmsa-7-2-12.2-hb470-2011.txt", "underpayment was not due to fraud, negligence or disregard of")
	h := household{tax: 10_000 * money.Dollar}
	before := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15))).Value().Total()
	if before == 0 {
		t.Fatal("no penalty to waive")
	}
	born := day(1962, 3, 1)
	for _, tc := range []struct {
		name, want, phrase string
		pos                *estimated.PositionsBuilder
	}{
		{"retired in 2026 at 64", "undecided", "no waiver for retirement or disability", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(born)},
		{"retired in 2020", "undecided", "fraud, negligence or disregard", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(born)},
		{"disaster", "undecided", "no waiver for a casualty or disaster as such", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1))},
	} {
		pos := must(tc.pos.Build())
		h.positions = &pos
		res := must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
		p := res.Value()
		o, ok := p.WaiverOutcome()
		got := map[bool]string{true: "eligible"}[o.Eligible()]
		if o.Refused() {
			got = "refused"
		}
		if o.Undecided() {
			got = "undecided"
		}
		waived := before
		if got == "eligible" {
			waived = 0
		}
		if !ok || got != tc.want || !strings.Contains(o.Reason(), tc.phrase) || p.Total() != before || p.IfWaived() != waived {
			t.Errorf("%s: %s, %q, total %v, if waived %v; want %s with %q, total %v", tc.name, got, o.Reason(), p.Total(), p.IfWaived(), tc.want, tc.phrase, before)
		}
		if !has(res.Required(), working.PositionUnsettledRule) {
			t.Errorf("%s: the open point is not reported", tc.name)
		}
	}
}
