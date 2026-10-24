package missouritaxcalc

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Missouri's estimated tax rules, RSMo §§ 143.521, 143.531 and 143.761, as
// the shared [estimated.EstimatedTax] interface.
//
// Missouri keeps the federal rules of the 1980s. The required annual payment
// is the lesser of 90% of this year's tax and 100% of last year's, with no
// 110% tier. Four exceptions each excuse an installment whose payments reach
// them: last year's tax, last year's income at this year's rates, 90% of the
// tax on annualized income, and 90% of the tax on income to date. A late
// starter pays fewer, larger installments. The rate is set for each calendar
// year.
//
// The figure follows Form MO-2210 for 2025, the latest published, with the
// 2026 dates and rates, and the result's Divergences say where the statute
// reads otherwise. The revisor shows no amendment of §§ 143.521 (1998),
// 143.531 (1990) or 143.761 (1989), so the form does not predate the law it
// describes (research/estimated-payments/mo.md).
//
// Kansas City's and St. Louis's earnings taxes are collected by the cities
// and are not covered.

// jurisdiction is Missouri's postal code.
const jurisdiction = estimated.Missouri

// EstimatedFacts are the facts Missouri's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorIncomeTax optional

	taxToDate    []money.USCents
	hasTaxToDate bool

	combatFrom, combatTo estimated.Date
	combatAnswered       bool

	statutoryThreshold, thresholdAnswered bool
	lineFive, lineFiveAnswered            bool
}

type optional struct {
	v   money.USCents
	set bool
}

// PriorYearIncomeAtThisYearsRates is the tax on last year's income at this
// year's rates and exemptions, MO-2210 Exception 2.
func (e EstimatedFacts) PriorYearIncomeAtThisYearsRates() (money.USCents, bool) {
	return e.priorIncomeTax.v, e.priorIncomeTax.set
}

// TaxOnIncomeToDate is the tax on the taxable income from January 1 to March
// 31, May 31 and August 31, each figured as if it were the year's, MO-2210
// Exception 4.
func (e EstimatedFacts) TaxOnIncomeToDate() ([]money.USCents, bool) {
	return append([]money.USCents(nil), e.taxToDate...), e.hasTaxToDate
}

// CombatZone is service in a combat zone under conditions that qualified for
// hostile fire pay, MO-2210 instruction I, and whether the caller said. The
// installments due during service are exempt. A zero span means no service.
func (e EstimatedFacts) CombatZone() (from, to estimated.Date, answered bool) {
	return e.combatFrom, e.combatTo, e.combatAnswered
}

// StatutoryThreshold reports whether the penalty is figured whenever tax less
// withholding is $100 or more, the statute's declaration threshold, rather
// than the form's $500, and whether the caller said.
func (e EstimatedFacts) StatutoryThreshold() (apply, answered bool) {
	return e.statutoryThreshold, e.thresholdAnswered
}

// LineFiveAsPrinted reports whether last year's tax counts toward the
// required annual payment whatever last year's return showed, as MO-2210
// Part I line 5 is printed, rather than only for a twelve-month return
// showing a liability, as Exception 1 and the statute say; and whether the
// caller said.
func (e EstimatedFacts) LineFiveAsPrinted() (asPrinted, answered bool) {
	return e.lineFive, e.lineFiveAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorYearIncomeAtThisYearsRates(a money.USCents) *EstimatedFactsBuilder {
	b.e.priorIncomeTax = optional{a, true}
	return b
}

// TaxOnIncomeToDate gives the tax on the income to March 31, May 31 and
// August 31.
func (b *EstimatedFactsBuilder) TaxOnIncomeToDate(march, may, august money.USCents) *EstimatedFactsBuilder {
	b.e.taxToDate, b.e.hasTaxToDate = []money.USCents{march, may, august}, true
	return b
}

// CombatZone records service in a combat zone from one day to another.
func (b *EstimatedFactsBuilder) CombatZone(from, to estimated.Date) *EstimatedFactsBuilder {
	b.e.combatFrom, b.e.combatTo, b.e.combatAnswered = from, to, true
	return b
}

// NoCombatZone answers that there was no service in a combat zone.
func (b *EstimatedFactsBuilder) NoCombatZone() *EstimatedFactsBuilder {
	b.e.combatFrom, b.e.combatTo, b.e.combatAnswered = estimated.Date{}, estimated.Date{}, true
	return b
}

func (b *EstimatedFactsBuilder) StatutoryThreshold(apply bool) *EstimatedFactsBuilder {
	b.e.statutoryThreshold, b.e.thresholdAnswered = apply, true
	return b
}

func (b *EstimatedFactsBuilder) LineFiveAsPrinted(asPrinted bool) *EstimatedFactsBuilder {
	b.e.lineFive, b.e.lineFiveAnswered = asPrinted, true
	return b
}

// Build refuses negative taxes and a combat zone span that does not run
// forward.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	e := b.e
	if e.priorIncomeTax.v < 0 {
		return EstimatedFacts{}, errors.New("missouritaxcalc: the tax on last year's income cannot be negative")
	}
	for _, t := range e.taxToDate {
		if t < 0 {
			return EstimatedFacts{}, errors.New("missouritaxcalc: the tax on income to date cannot be negative")
		}
	}
	if e.combatFrom.IsZero() != e.combatTo.IsZero() || e.combatTo.Before(e.combatFrom) {
		return EstimatedFacts{}, errors.New("missouritaxcalc: combat zone service needs a start and an end, in order")
	}
	e.taxToDate = append([]money.USCents(nil), e.taxToDate...)
	return e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is MO-2210 line 4, and declaration RSMo 143.521.1.
	threshold, declaration money.USCents

	// thisYearShare and priorYearShare are MO-2210 lines 2 and 5;
	// incomeShare is the 90% of Exceptions 3 and 4.
	thisYearShare, priorYearShare, incomeShare money.Rate

	// withholdingShares are RSMo 143.761.5(2): an equal part on each date.
	withholdingShares []money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// laterGroups are the first days of the MO-2210 instruction B table's
	// second, third and fourth rows: a requirement first met on or after
	// laterGroups[i] starts at installment i+2.
	laterGroups []estimated.Date
	periodEnds  []estimated.Date

	// fileAndPayBy and deemedPaid are MO-2210 instruction H: the balance
	// paid with a return filed by fileAndPayBy counts as paid on deemedPaid.
	fileAndPayBy, deemedPaid estimated.Date

	stop  estimated.Date
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Missouri's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return missouriEstimated{y}, nil
}

type missouriEstimated struct{ y estimatedYear }

type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// cumulative is MO-2210 line 15 added up: what is required by each date.
	cumulative []money.USCents

	// first is the installment a late starter's requirement starts at.
	first int

	// exceptions are the running totals that excuse an installment, by
	// exception, or nil where it is not open.
	exceptions [4][]money.USCents

	combatFrom, combatTo estimated.Date
	combatAnswered       bool
	filedByJan31         bool

	// What the divergences turn on: tax less withholding between the
	// statute's threshold and the form's, a prior-year return whose line 5
	// reading would change the target, and the readings answered.
	inThresholdGap, lineFiveChanges bool
	statutory, lineFivePrinted      bool
	taxToDate                       []money.USCents

	notes working.Notes
}

func (r missouriEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("missouritaxcalc: the facts carry %T, not missouritaxcalc.EstimatedFacts", s)
	}
}

func (r missouriEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("missouritaxcalc: these are Missouri's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("missouritaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case int64(f.FarmShare())*3 >= 20_000:
		return fmt.Errorf("%w: a farmer with two-thirds of Missouri gross income from farming pays once at 66 2/3%%, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("missouritaxcalc: MO-2210 Exception 3 period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where Form MO-2210 and the statute part ways on a rule
// every Missouri computation uses. The $500 stop and the zero prior year are
// in the research file; the other three were found reading MO-2210 against
// § 143.761. Each says whether it changes anything for f.
func (r missouriEstimated) divergences(f estimated.Facts, a assessment) ([]working.Divergence, error) {
	var priorShort, toDate, carried bool
	if !a.exempt && a.requirement.TargetKnown() {
		credits, _, err := r.credits(f, a)
		if err != nil {
			return nil, err
		}
		ledger, err := estimated.Apply(a.dues, estimated.Increments(a.cumulative), credits)
		if err != nil {
			return nil, err
		}
		c, _ := a.requirement.Chosen()
		for i, l := range ledger {
			short := l.CreditedBy() < a.cumulative[i]
			priorShort = priorShort || (short && c.Basis() == estimated.BasisPriorYearTax)
			if short && i < len(ledger)-1 && a.excused(i, l.CreditedBy()) != "" {
				carried = true
			}
			// Exception 4 by the form's 90% and by the statute's share of it.
			if short && i < len(a.taxToDate) && i >= a.first {
				shares, err := estimated.EqualShares(r.y.incomeShare.Of(a.taxToDate[i]), len(a.dues), a.first)
				if err != nil {
					return nil, err
				}
				if (l.CreditedBy() >= a.exceptions[3][i]) != (l.CreditedBy() >= shares[i]) {
					toDate = true
				}
			}
		}
	}
	stopReading, lineFiveReading := working.ReadingForm, working.ReadingStatute
	if a.statutory {
		stopReading = working.ReadingStatute
	}
	if a.lineFivePrinted {
		lineFiveReading = working.ReadingForm
	}
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("What the penalty is measured against").
			Form("The required annual payment is the smaller of two amounts. They are 90% of this year's tax and last year's tax. The penalty is on each installment's shortfall against a quarter of it.",
				"Form MO-2210 (2025), Part I lines 2 to 6 and Part III line 15").
			Statute("The shortfall is measured against installments of 90% of this year's tax. Last year's tax and the other tests only excuse an installment paid up to them.",
				"RSMo 143.761.2, 143.761.4").
			Uses(working.ReadingForm).Affects(priorShort),
		working.NewDivergence("The $500 stop").
			Form("No penalty when tax after credits less withholding is under $500.",
				"Form MO-2210 (2025), Part I line 4").
			Statute("Estimated payments are required once estimated tax reaches $100. That is the tax after credits and withholding. The penalty applies whenever they were required.",
				"RSMo 143.521.1; 12 CSR 10-2.067(3)").
			Uses(stopReading).Affects(a.inThresholdGap),
		working.NewDivergence("A prior-year return with no tax, or a short year").
			Form("Line 5 takes last year's tax with no condition. A $0 tax there gives a required payment of $0. Exception 1 on the same form needs a twelve-month return showing tax.",
				"Form MO-2210 (2025), Part I line 5 and instruction D, Exception 1").
			Statute("Last year's tax counts only from a twelve-month return that showed tax.",
				"RSMo 143.761.4(1)").
			Uses(lineFiveReading).Affects(a.lineFiveChanges),
		working.NewDivergence("Exception 4, tax on income to date").
			Form("Payments by each of the first 3 dates must reach 90% of the tax on the income. That income runs to the end of the month before the date.",
				"Form MO-2210 (2025), line 24 and instruction D, Exception 4 and Example II").
			Statute("Payments must reach a quarter, a half and three-quarters of 90% of that tax. That is what each date would require if 90% of that tax were the estimated tax.",
				"RSMo 143.761.4(3)").
			Uses(working.ReadingForm).Affects(toDate),
		working.NewDivergence("An excused installment's shortfall").
			Form("Line 19d carries an excused installment's shortfall into the next column. Line 25 charges it there unless that column's own exception applies.",
				"Form MO-2210 (2025), lines 19b to 19d and 25, and the instructions for lines 19c, 19d and 25").
			Statute("An exception lifts the penalty on its installment's shortfall until it's paid.",
				"RSMo 143.761.3, 143.761.4").
			Uses(working.ReadingForm).Affects(carried),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// assess is the requirement and installments, with the divergences.
func (r missouriEstimated) assess(f estimated.Facts) (assessment, error) {
	a, err := r.measure(f)
	if err != nil {
		return assessment{}, err
	}
	divergences, err := r.divergences(f, a)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

// date prints d for a sentence: "Aug 31" in the tax year, "Jan 15, 2027"
// outside it.
func (y estimatedYear) date(d estimated.Date) string {
	if d.Year() == y.year {
		return fmt.Sprintf("%.3s %d", d.Month(), d.Day())
	}
	return fmt.Sprintf("%.3s %d, %d", d.Month(), d.Day(), d.Year())
}

func (r missouriEstimated) measure(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	last := y.year - 1

	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
	}

	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("RSMo 143.761.2; Form MO-2210 (2025), Part I line 2", fmt.Sprintf("%v of this year's Missouri tax after credits of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	exceptionOne := false
	var priorOption *estimated.TargetOptionBuilder
	asPrinted, lineFiveAnswered := extras.LineFiveAsPrinted()
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Missouri return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Missouri return", last))
	case (prior.Months() != 12 || priorTax == 0) && asPrinted:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, y.priorYearShare.Of(priorTax))
		a.notes.Rule("Form MO-2210 (2025), Part I line 5", fmt.Sprintf("as the caller answered, line 5 takes the %v shown on the %d return though it covered %d months", priorTax, last, prior.Months()))
	case prior.Months() != 12 || priorTax == 0:
		why, what := fmt.Sprintf("Your %d Missouri return showed no tax", last), "showed no tax"
		if prior.Months() != 12 {
			why = fmt.Sprintf("Your %d Missouri return covered %d months, not twelve", last, prior.Months())
			what = fmt.Sprintf("covered %d months", prior.Months())
		}
		priorOption = unavailable(why)
		if !lineFiveAnswered {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Should MO-2210 line 5 count your %d tax though that return %s?", last, what),
				"The form's Exception 1 and the statute need a full-year return showing tax. The figures follow them and don't count it.")
		}
	default:
		exceptionOne = true
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, y.priorYearShare.Of(priorTax))
		a.notes.Rule("RSMo 143.761.4(1); Form MO-2210 (2025), Part I line 5", fmt.Sprintf("the prior-year test is %v of the %v tax on the %d return, or %v", y.priorYearShare, priorTax, last, y.priorYearShare.Of(priorTax)))
	}
	priorOpt, err := priorOption.Build()
	if err != nil {
		return assessment{}, err
	}

	rb := estimated.NewRequirement().Option(thisYear).Option(priorOpt)
	chosen, target := estimated.BasisThisYearTax, thisYear.Amount()
	if priorOpt.Available() && (!taxKnown || priorOpt.Amount() < target) {
		chosen, target = estimated.BasisPriorYearTax, priorOpt.Amount()
	}
	if !taxKnown {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Missouri tax after credits to be?", y.year),
			fmt.Sprintf("It sets the %v option and the %s test.", y.thisYearShare, y.threshold.Whole()))
	}
	// Line 5 as printed takes last year's tax from any filed return; the
	// statute only from a twelve-month return showing a liability.
	if hasPrior && prior.Filed() && (prior.Months() != 12 || priorTax == 0) {
		a.lineFiveChanges = !taxKnown || y.priorYearShare.Of(priorTax) < thisYear.Amount()
	}
	a.lineFivePrinted = asPrinted

	threshold := y.threshold
	statutory, thresholdAnswered := extras.StatutoryThreshold()
	if statutory {
		threshold = y.declaration
	}
	a.statutory = statutory
	a.inThresholdGap = taxKnown && tax-withheld >= y.declaration && tax-withheld < y.threshold
	switch {
	case taxKnown && tax-withheld < threshold:
		a.exempt = true
		small, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, threshold)
		if err != nil {
			return assessment{}, err
		}
		under := fmt.Sprintf(", under %s", threshold.Whole())
		if tax < withheld {
			under = ""
		}
		rb.Required(false, fmt.Sprintf("Your Missouri tax is %s, and %s was withheld. That %s%s. No estimated payments are needed.",
			tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld), under)).Exempt(small)
		a.notes.Rule("Form MO-2210 (2025), Part I line 4", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no penalty", tax, withheld, tax-withheld, threshold))
		if !thresholdAnswered && tax-withheld >= y.declaration {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Should the penalty start at %s due after withholding, as the statute says?", y.declaration.Whole()),
				fmt.Sprintf("The figures use Form MO-2210's %s start, for which no authority was found.", y.threshold.Whole()))
		}
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("There's no target yet without your %d Missouri tax or a full-year %d return.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s, whatever your %d tax is. After %s of withholding, you need %s of estimated payments.",
			priorOpt.Percent(), last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Missouri withholding of %s already reaches the %s target. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Missouri tax of %s less %s of withholding leaves %s. That is %s or more, so estimated payments are required. You need %s of them to reach the %s target.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), threshold.Whole(), (target-withheld).Whole(), target.Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// Under an exception last year's return can't change the target.
	if !hasPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Missouri return, and what was its tax?", last),
			fmt.Sprintf("With it, last year's tax could be a lower target than %v of this year's.", y.thisYearShare))
	}

	// A late starter: MO-2210 instruction B and line 15.
	a.first = estimated.LateStart(f.RequirementFirstMet(), y.laterGroups)
	a.cumulative = make([]money.USCents, len(a.dues))
	if !a.exempt {
		if a.cumulative, err = estimated.EqualShares(a.requirement.Target(), len(a.dues), a.first); err != nil {
			return assessment{}, err
		}
	}
	installments := estimated.Increments(a.cumulative)
	a.notes.Rule("RSMo 143.531.1; Form MO-2210 (2025), instruction B and line 15", fmt.Sprintf("%v is due in %d equal installments from %s: %v, %v, %v and %v on %s, %s, %s and %s",
		a.requirement.Target(), 4-a.first, a.dues[a.first].Long(), installments[0], installments[1], installments[2], installments[3],
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	// The exceptions, as running totals by each date.
	if exceptionOne {
		if a.exceptions[0], err = estimated.EqualShares(priorTax, len(a.dues), a.first); err != nil {
			return assessment{}, err
		}
	}
	if amount, ok := extras.PriorYearIncomeAtThisYearsRates(); ok && !(hasPrior && prior.NotRequiredToFile()) {
		if a.exceptions[1], err = estimated.EqualShares(amount, len(a.dues), a.first); err != nil {
			return assessment{}, err
		}
		a.notes.Rule("RSMo 143.761.4(4); Form MO-2210 (2025), line 22, Exception 2", fmt.Sprintf("an installment is excused when payments by its date reach its share of %v, the tax on %d income at %d rates", amount, last, y.year))
	}
	if f.Elections().Annualised() {
		a.exceptions[2] = make([]money.USCents, len(a.dues))
		for i, p := range f.Periods()[:3] {
			shares, err := estimated.EqualShares(y.incomeShare.Of(p.AnnualisedTax()), len(a.dues), a.first)
			if err != nil {
				return assessment{}, err
			}
			a.exceptions[2][i] = shares[i]
		}
		a.exceptions[2][3] = -1
		a.notes.Rule("RSMo 143.761.4(2); Form MO-2210 (2025), line 23, Exception 3", fmt.Sprintf("the first three installments are also excused when payments reach %v, %v and %v: 90%% of the tax on annualized income, a quarter, half and three-quarters of it",
			a.exceptions[2][0], a.exceptions[2][1], a.exceptions[2][2]))
	}
	if taxes, ok := extras.TaxOnIncomeToDate(); ok {
		a.taxToDate = taxes
		a.exceptions[3] = make([]money.USCents, len(a.dues))
		for i, t := range taxes {
			if i >= a.first {
				a.exceptions[3][i] = y.incomeShare.Of(t)
			}
		}
		a.exceptions[3][3] = -1
		a.notes.Rule("RSMo 143.761.4(3); Form MO-2210 (2025), line 24, Exception 4", fmt.Sprintf("the first three installments are also excused when payments reach %v, %v and %v: 90%% of the tax on the income to date",
			a.exceptions[3][0], a.exceptions[3][1], a.exceptions[3][2]))
	}
	a.combatFrom, a.combatTo, a.combatAnswered = extras.CombatZone()

	// Instruction H. January 31, 2027 is a Sunday; the 2025 form prints
	// January 31, 2026, a Saturday, and does not move it, so it is not moved.
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(y.fileAndPayBy) && !a.exempt {
		a.filedByJan31 = true
		a.notes.Rule("RSMo 143.521.6; Form MO-2210 (2025), instruction H", fmt.Sprintf("the return was filed and paid on %s, by %s, so the balance counts as paid on %s", filed.Long(), y.fileAndPayBy.Long(), y.deemedPaid.Long()))
	}
	return a, nil
}

// excused says why installment i bears no penalty, or "" when it does.
func (a assessment) excused(i int, creditedBy money.USCents) string {
	if !a.combatFrom.IsZero() && !a.dues[i].Before(a.combatFrom) && !a.dues[i].After(a.combatTo) {
		return "instruction I: the installment fell due during service in a combat zone"
	}
	names := []string{"Exception 1, last year's tax", "Exception 2, last year's income at this year's rates", "Exception 3, annualized income", "Exception 4, income to date"}
	for k, ex := range a.exceptions {
		if ex != nil && ex[i] >= 0 && i >= a.first && creditedBy >= ex[i] {
			return names[k]
		}
	}
	return ""
}

func (r missouriEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// credits are withholding and payments, earliest first.
func (r missouriEstimated) credits(f estimated.Facts, a assessment) ([]estimated.Payment, working.Notes, error) {
	var notes working.Notes
	credits, err := estimated.Credits(f, a.dues, r.y.withholdingShares)
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
		notes.Rule("RSMo 143.761.5(2); Form MO-2210 (2025), instruction F", fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("RSMo 143.761.5(2); Form MO-2210 (2025), instruction F", fmt.Sprintf("the %v withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld", f.WithheldTotal()))
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	sort.SliceStable(credits, func(i, j int) bool { return credits[i].On().Before(credits[j].On()) })
	return credits, notes, nil
}

func (r missouriEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Missouri return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	out, _, notes, err := r.schedule(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments after the credits, with the credits they come
// from. An installment an exception covers is excused, and so is January's
// for a return filed and paid by January 31.
func (r missouriEstimated) schedule(f estimated.Facts, a assessment) ([]estimated.Installment, []estimated.Payment, working.Notes, error) {
	credits, notes, err := r.credits(f, a)
	if err != nil {
		return nil, nil, working.Notes{}, err
	}
	ledger, err := estimated.Apply(a.dues, estimated.Increments(a.cumulative), credits)
	if err != nil {
		return nil, nil, working.Notes{}, err
	}
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(a.cumulative[i]).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		why := a.excused(i, l.CreditedBy())
		switch {
		case a.filedByJan31 && i == 3:
			b.Excused()
		case why != "":
			b.Excused()
			if l.Shortfall() > 0 {
				notes.Rule("RSMo 143.761.4; Form MO-2210 (2025), instruction D", fmt.Sprintf("the %s installment was %v short and bears no penalty under %s", l.Due().Long(), l.Shortfall(), why))
			}
		}
		if out[i], err = b.Build(); err != nil {
			return nil, nil, working.Notes{}, err
		}
	}
	return out, credits, notes, nil
}

func (r missouriEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("missouritaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Missouri return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := asOf
	if y.stop.Before(stop) {
		stop = y.stop
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop = filed
	}
	if a.filedByJan31 && y.deemedPaid.Before(stop) {
		stop = y.deemedPaid
	}
	waiver, waiverAsked := f.Positions().Waiver()
	sched, credits, notes, err := r.schedule(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if a.exempt {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, sched).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		return working.NewResult(p).With(a.notes).Build()
	}

	// MO-2210 lines 19d to 28c: each column's total underpayment, from its
	// due date until paid or the next due date, each calendar year's days at
	// that year's rate.
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, sched)
	var unrated estimated.Date
	early := false
	accrue := func(lb *estimated.PenaltyLineBuilder, amount money.USCents, from, to estimated.Date) error {
		parts, gap, err := estimated.SimpleDaily(amount, from, to, y.rates)
		if err != nil {
			return err
		}
		if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
			unrated = gap
		}
		for _, p := range parts {
			lb.Part(p)
		}
		return nil
	}
	cols, err := estimated.Columns(a.dues, a.cumulative, credits, y.stop, stop)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	for _, col := range cols {
		k := col.Number() - 1
		if col.Under() <= 0 {
			continue
		}
		if why := a.excused(k, col.CreditedBy()); why != "" {
			notes.Rule("RSMo 143.761.4; Form MO-2210 (2025), instruction D", fmt.Sprintf("no penalty is figured in the %s column, under %s", col.Due().Long(), why))
			continue
		}
		lb := estimated.NewPenaltyLine(col.Number(), col.Due(), col.Under())
		for _, piece := range col.Pieces() {
			to, paid := piece.PaidOn()
			if !paid {
				to = col.End()
			}
			if err := accrue(lb, piece.Amount(), col.Due(), to); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
		line, err := lb.Days(col.Through().DaysSince(col.Due())).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		if line.Total() > 0 && k < len(a.dues)-1 {
			early = true
		}
		pb.Line(line)
	}
	if !a.taxKnown {
		pb.UpperBound()
	}
	p, err := pb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		notes.Rule("Form MO-2210 (2025), Part I line 6", fmt.Sprintf("this year's Missouri tax was not given, so the penalty is figured against the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower", y.year-1, p.Total()))
	}
	notes.Rule("RSMo 143.761.1, 143.761.3, 32.065; Form MO-2210 (2025), lines 25 to 28c", fmt.Sprintf("each column's total underpayment is charged by the day ÷ 365 at each calendar year's rate, from its due date until paid or the next due date, and the last column to %s; "+
		"the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", y.date(unrated.AddDays(-1))))
	}
	if p.Total() > 0 && !a.combatAnswered {
		notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did you serve in a combat zone with hostile fire pay during %d?", y.year),
			"Installments due during that service are exempt.")
	}
	if early && f.RequirementFirstMet().IsZero() {
		notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first have to make estimated payments for %d?", y.year),
			"If it was after Apr 1, you pay fewer, larger installments.")
	}
	if waiverAsked && waiver != estimated.WaiverNone {
		notes.Rule("12 CSR 10-2.067(3)", "Missouri imposes the addition without regard to reasonable cause, and has no waiver for casualty, retirement or disability; the penalty is shown in full")
		if p.Total() > 0 {
			o := estimated.WaiverRefused("Missouri has no waiver for retirement, disability, casualty or disaster. The Department of Revenue charges the penalty even with reasonable cause.")
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r missouriEstimated) Explanation() estimated.Explanation { return missouriExplanation }

// missouriExplanation is written by hand and checked against
// research/estimated-payments/mo.md.
var missouriExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Missouri expects estimated payments once $100 or more is due after withholding. Its penalty form charges nothing under $500. The penalty is interest on each quarter's shortfall. The rate is set each calendar year, 7% for 2026 and 2027.").
		Paragraph("Your target is 90% of this year's Missouri tax or 100% of last year's. It is whichever is smaller. You're clear if withholding and estimated payments cover a quarter by each date. The dates are April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. Last year's figure counts only from a twelve-month return that showed tax. Withholding counts as paid evenly on the four dates. You can instead show when it was taken.").
		Paragraph("Missouri also excuses a quarter when payments so far reach one of four measures. Two are last year's tax and last year's income taxed at this year's rates. The other two are 90% of the tax on your income to date, annualized or actual. The last two do not apply to the January payment.").
		Paragraph("If you first had to pay later in the year, you pay fewer, larger installments. That is three from June 15, two from September 15, or one in January. A return filed and paid by January 31 counts as paid on January 15. The interest runs by the day from each due date until paid or April 15. Missouri does not waive it for reasonable cause.").
		VersusFederal("Missouri's prior-year test is always 100% and needs a return that showed tax. Last year's income at this year's rates is still a safe harbor. The annualized and to-date income tests excuse a quarter rather than lower it. They do not apply to the fourth. A late starter has fewer installments. The rate is set each calendar year. There is no waiver for casualty, retirement or disability.").
		Source("RSMo §§ 143.521, 143.531, 143.761, 32.065").
		Source("Form MO-2210, 2025 Underpayment of Estimated Tax by Individuals, and its instructions").
		Source("Form MO-1040ES, 2026 Declaration of Estimated Tax for Individuals (Revised 12-2025)").
		Source("12 CSR 10-41.010 (emergency amendment, 2026 rate); Missouri DOR, Statutory Interest Rates").
		Source("12 CSR 10-2.067").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()

// date is a shorthand for the year files.
func date(y int, m time.Month, d int) estimated.Date { return estimated.MustDate(y, m, d) }
