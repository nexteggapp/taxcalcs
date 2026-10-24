// Nebraska's estimated tax rules for 2026, checked against the Department of
// Revenue's own documents and the Nebraska Revised Statutes.
//
// Nebraska publishes no worked penalty example. Form 2210N defines the line
// arithmetic, and these tests follow it: each column's underpayment, by the
// day ÷ 365 at 8%, from its due date until paid or the next due date, and the
// last column to April 15. Every threshold, rate, date and percentage is
// compared against its extract in testdata/. The cases are hand-written.
package nebraskataxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/nebraskataxcalc"
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
	return must(nebraskataxcalc.EstimatedTax(2026))
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
	status      filing.Status
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extras      *nebraskataxcalc.EstimatedFacts
	returnFiled estimated.Date
	elections   *estimated.Elections
	annualised  []money.USCents
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
	status := h.status
	if status == filing.Unspecified {
		status = filing.Single
	}
	b := estimated.NewFacts("NE", 2026).FilingStatus(status).ThisYear(figures).
		Paid(h.payments...).ReturnFiled(h.returnFiled).FarmShare(h.farmShare).FishShare(h.fishShare)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extras != nil {
		b.State(*h.extras)
	}
	if h.elections != nil {
		b.Elections(*h.elections)
	}
	if h.annualised != nil {
		ends := []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)}
		for i, tax := range h.annualised {
			b.Periods(must(estimated.NewPeriod().End(ends[i]).AnnualisedTax(tax).Build()))
		}
		b.Elections(must(estimated.NewElections().Annualised().Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

func prior(tax, agi money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).FederalAGI(agi).Build())).Build())
	return &p
}

func penalty(t *testing.T, h household) working.Result[estimated.Penalty] {
	t.Helper()
	return must(rules(t).Penalty(h.facts(t), day(2027, 4, 15)))
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "form-2210n-2026.txt",
		"4 Multiply line 3 by 90% (.90) and enter result here",
		"6 Subtract line 5 from line 3. If less than $500, stop here; do not complete this form. You do not owe penalty",
		"If federal AGI was more than $150,000 ($75,000 if married, filing separately), enter 110% of 2025 tax",
		"8 Required annual payment. Enter smaller of line 4 or line 7",
		"April 15, 2026 June 15, 2026",
		"Sept. 15, 2026 Jan. 15, 2027",
		"10 Enter 25% of line 8 in each column",
		"20 Date of payment or next due date (from line 9), whichever is earlier",
		"22 a Penalty (8% per year on the line 19 amount for the number of days in 2026 on line 21)",
		"b Reserved for future use",
		"100% of your 2025 income tax liability (if you filed a 2025 return that covered a full 12 months).",
		"You had no Nebraska income tax liability for 2025, you were a U.S. citizen or resident for the entire year, and your 2025 Nebraska income tax return was (or would have been had you been required to file) for a full 12 months;",
		"You are a first-time filer for Nebraska income tax purposes; or",
		"If you file your return and pay the income tax due by January 31, 2027, include on line 19, January 15 column, the amount of income tax you pay with your Nebraska individual income tax return.",
		"If you filed your Nebraska income tax return and paid the balance of the income tax due by January 31, that balance is considered paid as of January 15.",
		"If the 2025 income tax year was for less than 12 months, do not complete this line.",
		"One-fourth of the Nebraska income tax withheld during the year by your employer is considered paid on each required installment date, unless you establish other dates",
		"Your payments are applied first to any underpayment balance on an earlier installment.",
		"the due date of the next payment (line 9 of the next column); or April 15, whichever is earliest.",
		"The penalty is calculated at 8% per year.",
		"Underpayment due to casualty, disaster, or other unusual circumstance",
		"you retired after reaching age 62, or became disabled, and your underpayment was due to reasonable cause.",
		"Your gross income from farming, ranching, or fishing is at least two-thirds of your annual gross income from all sources for 2025 or 2026",
		"90% of your Nebraska income tax liability based on annualized income",
	)
	mustContain(t, "form-1040n-es-2026.txt",
		"in equal installments on or before April 15, 2026, June 15, 2026, September 15, 2026, and January 15, 2027.",
		"Saturday, Sunday, or legal holiday, taxpayers have until the next business day to make the payment.",
		"enter ½ of the amount on line 18",
	)
	mustContain(t, "nrs-77-2769.txt", "no payment of estimated tax is required by an individual if the estimated tax can reasonably be expected to be less than five hundred dollars")
	mustContain(t, "316-nac-20-001-007.txt",
		"can reasonably be expected to be $300 or more",
		"The penalty is imposed at the same rate specified for interest in section 45-104.02",
		"divided by the number of installment dates prescribed for such year",
		"The tax liability for the preceding taxable year was zero, provided that the preceding tax return was, or would have been, had the taxpayer been required to file, for a full twelve month period",
		"less any income tax withholding is less than $300.",
	)
	mustContain(t, "nrs-45-104.02.txt", "shall be redetermined every other year")
	mustContain(t, "revenue-ruling-99-26-2.txt",
		"For the two-year period beginning January 1, 2027 through December 31, 2028, the rate of interest on payments of any delinquent taxes or special assessments owed to the State of Nebraska will be 8% per year.",
		"1-1-2025 and after 8%",
		"Simple interest",
	)
}

// The printed dates, 25% each, and none moves.
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

// January 31, 2027 is a Sunday and Form 2210N does not say whether it
// moves. Filed by it, the balance counts as paid on January 15. Filed on
// Monday, February 1, it does not by default, and the question is asked; the
// answer moves it.
func TestEstimatedJanuaryThirtyFirst(t *testing.T) {
	res := penalty(t, household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 1, 29)})
	if res.Value().AsOf() != day(2027, 1, 15) || res.Value().Total() != 30_131 {
		t.Errorf("filed January 29: to %v, %v; want January 15 and $301.31", res.Value().AsOf(), res.Value().Total())
	}
	res = penalty(t, household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 1)})
	if res.Value().AsOf() != day(2027, 2, 1) || !because(res.Required(), working.PositionUnsettledRule, "a Sunday") {
		t.Errorf("filed February 1: to %v, asked %v", res.Value().AsOf(), because(res.Required(), working.PositionUnsettledRule, "a Sunday"))
	}
	moves := must(nebraskataxcalc.NewEstimatedFacts().JanuaryThirtyFirstMoves(true).Build())
	res = penalty(t, household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 1), extras: &moves})
	if res.Value().AsOf() != day(2027, 1, 15) || because(res.Required(), working.PositionUnsettledRule, "a Sunday") {
		t.Errorf("filed February 1, moved: to %v", res.Value().AsOf())
	}
	if s := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, returnFiled: day(2027, 2, 2), extras: &moves}.facts(t))).Value(); s[3].Status() == estimated.StatusExcused {
		t.Error("filed February 2: January is excused")
	}
}

// Form 2210N line 6: $500 or more after withholding.
func TestEstimatedThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		exempt   bool
	}{{950_001, true}, {950_000, false}} {
		if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: tc.withheld}.facts(t))); res.Value().Exempt() != tc.exempt {
			t.Errorf("withheld %v: exempt %v, want %v", tc.withheld, res.Value().Exempt(), tc.exempt)
		}
	}
	// Under the threshold the exception is the chosen option, at $0.
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: 960_000}.facts(t)))
	if c, ok := res.Value().Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || c.Label() != "Tax less withholding under $500" {
		t.Errorf("chosen %v: %v at %v labelled %q", ok, c.Basis(), c.Amount(), c.Label())
	}
	if !strings.Contains(res.Value().Why(), "leaves $400, under $500") {
		t.Errorf("why %q", res.Value().Why())
	}
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 960_000}); p.Value().Reason() != "Tax less withholding under $500." {
		t.Errorf("penalty reason %q", p.Value().Reason())
	}
	// $400 is under the form's $500 but not the regulation's $300, so the
	// threshold divergence affects this household.
	for _, d := range res.Divergences() {
		if d.Rule() == "The $500 threshold" && !d.AffectsHousehold() {
			t.Error("$400 after withholding: the threshold divergence does not affect the household")
		}
	}
}

// 100% of last year's tax, 110% above $150,000 of last year's federal AGI
// ($75,000 married filing separately this year).
func TestEstimatedPriorYearStep(t *testing.T) {
	for _, tc := range []struct {
		status filing.Status
		agi    money.USCents
		want   money.USCents
	}{
		{filing.Single, 15_000_000, 8_000 * money.Dollar},
		{filing.Single, 15_000_001, 8_800 * money.Dollar},
		{filing.MarriedFilingSeparately, 7_500_000, 8_000 * money.Dollar},
		{filing.MarriedFilingSeparately, 7_500_001, 8_800 * money.Dollar},
	} {
		res := must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, status: tc.status, prior: prior(8_000*money.Dollar, tc.agi)}.facts(t)))
		if res.Value().Target() != tc.want {
			t.Errorf("%v, AGI %v: target %v, want %v", tc.status, tc.agi, res.Value().Target(), tc.want)
		}
	}
	noAGI := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(8_000 * money.Dollar).Build())).Build())
	if res := must(rules(t).Requirement(household{tax: 20_000 * money.Dollar, prior: &noAGI}.facts(t))); res.Value().Target() != 8_800*money.Dollar || !has(res.Required(), working.PositionAdjustedGrossIncome) {
		t.Errorf("no AGI: target %v", res.Value().Target())
	}
	notFiled := must(estimated.NewPriorReturn().Filed(false).Build())
	short := must(estimated.NewPriorReturn().Filed(true).Months(11).Figures(must(estimated.NewFigures().Tax(100).FederalAGI(1).Build())).Build())
	for name, p := range map[string]*estimated.PriorReturn{"none on record": nil, "not filed": &notFiled, "eleven months": &short} {
		if res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: p}.facts(t))); res.Value().Options()[1].Available() {
			t.Errorf("%s: the prior-year test is offered", name)
		}
	}
}

// Nothing paid on $9,000 required: each column's underpayment to the next
// due date at 8%: April's $2,250 for 61 days, June's $4,500 for 92,
// September's $6,750 for 107 days of 2026 and 15 of 2027, January's $9,000
// for 90. September's column spans the 2026 and 2027 rate periods; Revenue
// Ruling 99-26-2 keeps 8% in both, so the boundary splits the charge
// without changing the rate.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	res := penalty(t, household{tax: 10_000 * money.Dollar})
	lines := res.Value().Lines()
	if len(lines) != 4 {
		t.Fatalf("%d lines, want four", len(lines))
	}
	want := []struct {
		under   money.USCents
		days    []int
		charges []money.USCents
	}{
		{225_000, []int{61}, []money.USCents{3_008}},
		{450_000, []int{92}, []money.USCents{9_074}},
		{675_000, []int{107, 15}, []money.USCents{15_830, 2_219}},
		{900_000, []int{90}, []money.USCents{17_753}},
	}
	for i, l := range lines {
		parts := l.Parts()
		if l.Shortfall() != want[i].under || len(parts) != len(want[i].days) {
			t.Errorf("column %d: %v in %d parts; want %v in %d", i+1, l.Shortfall(), len(parts), want[i].under, len(want[i].days))
			continue
		}
		for j, p := range parts {
			if p.Days() != want[i].days[j] || p.Charge() != want[i].charges[j] || p.Rate() != money.Percent(8) {
				t.Errorf("column %d part %d: %d days %v at %v", i+1, j, p.Days(), p.Charge(), p.Rate())
			}
		}
	}
	if parts := lines[2].Parts(); !strings.Contains(parts[1].Source(), "2027") {
		t.Errorf("September's 2027 part cites %q", parts[1].Source())
	}
	if res.Value().Total() != 47_884 {
		t.Errorf("penalty %v, want $478.84", res.Value().Total())
	}
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is not asked")
	}
	if p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar}.facts(t), day(2027, 7, 1))); p.Value().AsOf() != day(2027, 4, 15) {
		t.Errorf("figured to %v, want April 15, 2027", p.Value().AsOf())
	}
}

// A payment between due dates stops the charge on what it pays: April's
// $2,250 paid April 30 runs 15 days, $7.40.
func TestEstimatedPaymentBetweenDueDates(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(day(2026, 4, 30), 2_250*money.Dollar), pay(dues[1], 2_250*money.Dollar), pay(dues[2], 2_250*money.Dollar), pay(dues[3], 2_250*money.Dollar)}}
	res := penalty(t, h)
	if l := res.Value().Lines(); len(l) != 1 || l[0].Days() != 15 || res.Value().Total() != 740 {
		t.Errorf("penalty %v over %v; want $7.40 for 15 days", res.Value().Total(), l)
	}
}

// Withholding counts a quarter on each date unless the filer shows the dates.
func TestEstimatedWithholding(t *testing.T) {
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar}); p.Value().Total() != 0 {
		t.Errorf("spread evenly: penalty %v", p.Value().Total())
	}
	asWithheld := must(estimated.NewElections().WithholdingAsWithheld().Build())
	if p := penalty(t, household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar, elections: &asWithheld}); p.Value().Total() == 0 {
		t.Error("on its December date: no penalty")
	}
}

// The exceptions: no liability last year as a U.S. citizen or resident, and
// a first-time filer.
func TestEstimatedExceptions(t *testing.T) {
	citizen := must(nebraskataxcalc.NewEstimatedFacts().PriorYearUSCitizenOrResident(true).Build())
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0, 10_000*money.Dollar), extras: &citizen}.facts(t)))
	if !res.Value().Exempt() {
		t.Errorf("no liability as a citizen: %s", res.Value().Why())
	}
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0, 10_000*money.Dollar)}.facts(t)))
	if res.Value().Exempt() || !because(res.Required(), working.PositionJurisdictionFact, "U.S. citizen or resident") {
		t.Errorf("citizenship not given: exempt %v", res.Value().Exempt())
	}
	first := must(nebraskataxcalc.NewEstimatedFacts().FirstTimeFiler(true).Build())
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &first}.facts(t)))
	if !res.Value().Exempt() || !because(res.Required(), working.PositionUnsettledRule, "first-time filer") {
		t.Errorf("first-time filer: exempt %v, noted %v", res.Value().Exempt(), because(res.Required(), working.PositionUnsettledRule, "first-time filer"))
	}
}

// Farmers, ranchers and fishermen in either year are under rules of their
// own.
func TestEstimatedFarmersAreNotBuilt(t *testing.T) {
	for name, h := range map[string]household{
		"farmer": {tax: 10_000 * money.Dollar, farmShare: 7000},
		"fisher": {tax: 10_000 * money.Dollar, fishShare: 7000},
	} {
		if _, err := rules(t).Requirement(h.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported", name, err)
		}
	}
	last := must(nebraskataxcalc.NewEstimatedFacts().FarmerLastYear(true).Build())
	if _, err := rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &last}.facts(t)); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("a farmer last year: %v, want ErrUnsupported", err)
	}
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, farmShare: 1000}.facts(t)))
	if !because(res.Required(), working.PositionJurisdictionFact, "farmers' rules") {
		t.Error("some farm income and last year not given: not asked")
	}
}

// The annualized method, figured as on the federal schedule.
func TestEstimatedAnnualizedMethod(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, annualised: []money.USCents{2_000 * money.Dollar, 4_000 * money.Dollar, 8_000 * money.Dollar, 10_000 * money.Dollar}}
	s := must(rules(t).Schedule(h.facts(t))).Value()
	for i, want := range []money.USCents{450 * money.Dollar, 1_800 * money.Dollar, 5_400 * money.Dollar, 9_000 * money.Dollar} {
		if s[i].NeededBy() != want {
			t.Errorf("installment %d needs %v, want %v", i+1, s[i].NeededBy(), want)
		}
	}
}

// Without this year's tax, the target is last year's alone and the penalty is
// the most it can be.
func TestEstimatedWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000*money.Dollar, 100_000*money.Dollar)}
	res := must(rules(t).Requirement(h.facts(t)))
	if r := res.Value(); !r.Required() || r.Target() != 8_000*money.Dollar || r.Options()[0].WhyNot() != estimated.NeedsThisYearTax || !has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("required %v, target %v", r.Required(), r.Target())
	}
	if p := penalty(t, h); !p.Value().UpperBound() || p.Value().Total() == 0 {
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
		"The $500 threshold":             {"under $500", "REG-20-001.01"},
		"No liability last year":         {"U.S. citizenship", "REG-20-007.06A"},
		"First-time filers":              {"first-time filer", "REG-20-007.06"},
		"A requirement that arises late": {"25% in every column", "REG-20-007.01"},
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
		// The household claims no exception and starts on time, so neither
		// reading changes its figures.
		if d.Uses() != working.ReadingForm || d.AffectsHousehold() {
			t.Errorf("%q uses %v, affects %v; want the form, not affecting", d.Rule(), d.Uses(), d.AffectsHousehold())
		}
	}
	first := must(nebraskataxcalc.NewEstimatedFacts().FirstTimeFiler(true).Build())
	res = must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, extras: &first}.facts(t)))
	for _, d := range res.Divergences() {
		if d.AffectsHousehold() != (d.Rule() == "First-time filers") {
			t.Errorf("first-time filer: %q affects %v", d.Rule(), d.AffectsHousehold())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := nebraskataxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100_000).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts("NE", 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts("NE", 2026).FilingStatus(filing.Single).ThisYear(this).State(true).Build()),
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
	for _, s := range []string{"$500", "8%", "110%", "$150,000", "first Nebraska return", "January 15", "every other year"} {
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
	f := must(estimated.NewFacts(estimated.Jurisdiction("NE"), 2026).FilingStatus(filing.Single).
		ThisYear(figures(3_000, 60_000)).PriorYear(prior).Withheld(withheld...).Build())
	got := must(rules(t).Requirement(f)).Value().Why()
	if !strings.Contains(got, "a $600 refund") || strings.Contains(got, "−$") || strings.Contains(got, "-$") {
		t.Errorf("why = %q, want a $600 refund and no minus sign", got)
	}
}

func waived(t *testing.T, pos estimated.Positions) estimated.Penalty {
	t.Helper()
	return must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, positions: &pos}.facts(t), day(2027, 4, 15))).Value()
}

func retiredOn(on, born estimated.Date) estimated.Positions {
	return must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, on).Born(born).Build())
}

// Nebraska grants the federal waivers, with the same window and age.
func TestEstimatedWaiverFollowsTheFederalOne(t *testing.T) {
	mustContain(t, "form-2210n-2026.txt", "In 2025 or 2026, you retired after reaching age 62, or became disabled, and your underpayment was due to reasonable cause.")
	p := waived(t, retiredOn(day(2026, 6, 1), day(1962, 3, 1)))
	if o, ok := p.WaiverOutcome(); !ok || !o.Eligible() || p.Total() != 47_884 || p.IfWaived() != 0 {
		t.Errorf("retired in 2026 at 64: outcome %v %+v, total %v, if waived %v", ok, o, p.Total(), p.IfWaived())
	}
	p = waived(t, retiredOn(day(2020, 6, 1), day(1955, 3, 1)))
	if o, ok := p.WaiverOutcome(); !ok || !o.Refused() || !strings.Contains(o.Reason(), "2025 or 2026") || p.IfWaived() != p.Total() {
		t.Errorf("retired in 2020: outcome %v %+v, if waived %v", ok, o, p.IfWaived())
	}
	p = waived(t, must(estimated.NewPositions().WaiverFor(estimated.CauseDisability, day(2025, 9, 1)).Build()))
	if o, ok := p.WaiverOutcome(); !ok || !o.Eligible() || !strings.Contains(o.Reason(), "reasonable cause") {
		t.Errorf("disabled in 2025: outcome %v %+v", ok, o)
	}
	p = waived(t, must(estimated.NewPositions().WaiverFor(estimated.CauseCasualty, day(2026, 8, 1)).Build()))
	if o, ok := p.WaiverOutcome(); !ok || !o.Eligible() || !strings.Contains(o.Reason(), "Nebraska's Department of Revenue") {
		t.Errorf("a casualty: outcome %v %+v", ok, o)
	}
}

// Under an exception no question may say its answer changes the target, so
// last year's return isn't asked for.
func TestEstimatedExemptAsksNothingOfTheTarget(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 100 * money.Dollar}.facts(t)))
	if !res.Value().Exempt() {
		t.Fatal("$100 of tax is not exempt")
	}
	if has(res.Required(), working.PositionPriorYearReturn) {
		t.Error("last year's return is asked for under an exception")
	}
	for _, q := range res.Required() {
		if strings.Contains(strings.ToLower(q.Effect()), "target") {
			t.Errorf("under an exception a question says %q", q.Effect())
		}
	}
}
