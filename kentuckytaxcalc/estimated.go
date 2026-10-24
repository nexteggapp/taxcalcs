package kentuckytaxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Kentucky's estimated tax rules, KRS 141.305, as the shared
// [estimated.EstimatedTax] interface.
//
// KRS 141.305(3) adopts 26 U.S.C. § 6654 with four exceptions, so the shape is
// the federal one: 90% of this year's tax or 100% of last year's, 110% above
// $150,000 of last year's Kentucky AGI, four quarters on the federal dates,
// and interest by the day. What differs is the threshold, $500 rather than
// $1,000; two income tests that decide whether payments are needed at all;
// a Kentucky rate fixed for each calendar year; and waivers only for
// reasonable cause under KRS 131.175.
//
// The figure follows the department's forms, the 2026 voucher instructions
// (42A740-S4, 9-25) and the 2025 Form 2210-K (42A740-S1, 10-25), and the
// result's Divergences say where KRS 141.305 reads otherwise. KRS 141.305
// has not been amended since 2019. KRS 131.183, which sets the rate, was
// amended by 2026 Ky. Acts ch. 161, effective July 15, 2026, after the 2210-K
// was printed; whether that act changed the estimated tax rate is not
// established (research/estimated-payments/ky.md), so the form's rate is used
// and the question is reported in Required.

// jurisdiction is Kentucky's postal code.
const jurisdiction = estimated.Kentucky

// EstimatedFacts are the facts Kentucky's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	nonWageIncome  optional
	filingRequired answer
	usResident     answer
	combined       answer
	spouseAGI      optional
}

type optional struct {
	v   money.USCents
	set bool
}

type answer struct {
	yes, set bool
}

// NonWageIncome is the year's gross income from sources other than wages on
// which Kentucky tax is withheld. KRS 141.305(1)(a) requires estimated
// payments when it can be expected to exceed $5,000.
func (e EstimatedFacts) NonWageIncome() (money.USCents, bool) {
	return e.nonWageIncome.v, e.nonWageIncome.set
}

// FilingThresholdMet reports whether this year's Kentucky AGI can be expected
// to be at least the amount for which KRS 141.180 requires a return, the
// other test in KRS 141.305(1), and whether the caller said.
func (e EstimatedFacts) FilingThresholdMet() (met, answered bool) {
	return e.filingRequired.yes, e.filingRequired.set
}

// PriorYearUSResident reports whether the filer was a U.S. citizen or
// resident alien for all of last year, which Form 2210-K's no-liability
// exception needs, and whether the caller said. It is not Kentucky residence.
func (e EstimatedFacts) PriorYearUSResident() (resident, answered bool) {
	return e.usResident.yes, e.usResident.set
}

// CombinedReturn reports whether a married couple filing separately does so
// on one combined Form 740, and whether the caller said. KY-TAM-19-02 adds
// the couple's incomes together for the 110% test.
func (e EstimatedFacts) CombinedReturn() (combined, answered bool) {
	return e.combined.yes, e.combined.set
}

// SpousePriorYearAGI is the spouse's Kentucky AGI for last year, for a couple
// on a combined return. It is added to [estimated.Figures.StateAGI] of the
// prior return for the 110% test.
func (e EstimatedFacts) SpousePriorYearAGI() (money.USCents, bool) {
	return e.spouseAGI.v, e.spouseAGI.set
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) NonWageIncome(a money.USCents) *EstimatedFactsBuilder {
	b.e.nonWageIncome = optional{a, true}
	return b
}

func (b *EstimatedFactsBuilder) FilingThresholdMet(met bool) *EstimatedFactsBuilder {
	b.e.filingRequired = answer{met, true}
	return b
}

func (b *EstimatedFactsBuilder) PriorYearUSResident(resident bool) *EstimatedFactsBuilder {
	b.e.usResident = answer{resident, true}
	return b
}

func (b *EstimatedFactsBuilder) CombinedReturn(combined bool) *EstimatedFactsBuilder {
	b.e.combined = answer{combined, true}
	return b
}

func (b *EstimatedFactsBuilder) SpousePriorYearAGI(a money.USCents) *EstimatedFactsBuilder {
	b.e.spouseAGI = optional{a, true}
	return b
}

// Build refuses negative gross income, and a spouse's AGI without a combined
// return.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	e := b.e
	switch {
	case e.nonWageIncome.set && e.nonWageIncome.v < 0:
		return EstimatedFacts{}, fmt.Errorf("kentuckytaxcalc: gross income of %v is negative", e.nonWageIncome.v)
	case e.spouseAGI.set && !(e.combined.set && e.combined.yes):
		return EstimatedFacts{}, errors.New("kentuckytaxcalc: a spouse's AGI is read only for a combined return")
	}
	return e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// incomeTest is KRS 141.305(1)(a): non-wage gross income above it
	// requires payments. threshold is KRS 141.305(2) and Form 2210-K line 5:
	// tax after withholding of it or less requires none.
	incomeTest, threshold money.USCents

	// priorYearStep is last year's Kentucky AGI above which the prior-year
	// test is 110%, and priorYearStepSeparate the same for married filing
	// separately.
	priorYearStep, priorYearStepSeparate money.USCents

	dues     []estimated.Date
	calendar estimated.Calendar

	// lateStart is the day after which a requirement that first arises lets a
	// return filed and paid by fileAndPayBy, moved for a weekend, take the
	// place of the January voucher.
	lateStart    estimated.Date
	fileAndPayBy estimated.Date

	stop       estimated.Date
	periodEnds []estimated.Date
	rates      estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are Form 2210-K line 9, 25% of the required annual payment for
// each date, and line 10, withholding spread 25% to each.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// annualisedPercentages are Form 2210-K Part III.
var annualisedPercentages = []money.Rate{2250, 4500, 6750, 9000}

// EstimatedTax returns Kentucky's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return kentuckyEstimated{y}, nil
}

type kentuckyEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents
	excused      bool
	notes        working.Notes
}

func (r kentuckyEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("kentuckytaxcalc: the facts carry %T, not kentuckytaxcalc.EstimatedFacts", s)
	}
}

func (r kentuckyEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("kentuckytaxcalc: these are Kentucky's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("kentuckytaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: a farmer or fisher with two-thirds of gross income from farming or fishing pays 66 2/3%% of the tax by January 15 or files and pays by March 1, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("kentuckytaxcalc: Form 2210-K Part III period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's documents and KRS 141.305 part ways
// on a rule a Kentucky computation uses, each marked with whether it changes
// anything for f. They are found in research/estimated-payments/ky.md, items
// 1 and 10 and its verification. The rate is a penalty rule, reported with
// the penalty.
func (r kentuckyEstimated) divergences(f estimated.Facts) ([]working.Divergence, error) {
	y := r.y
	tax, taxKnown := f.ThisYear().Tax()
	withheld := f.WithheldTotal()
	filed := f.ReturnFiled()
	byFebruary := !filed.IsZero() && !filed.After(y.calendar.NextBusinessDay(y.fileAndPayBy))
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Exactly $500").
			Form("Form 2210-K stops at $500 or less of tax. That tax is after withholding and refundable credits. Kentucky's voucher instructions say payments are needed from $500. The 2210-K instructions say under $500.",
				"Form 2210-K (2025), 42A740-S1 (10-25), line 5 and its instructions; 42A740-S4 (9-25)").
			Statute("No estimated tax is needed if you can reasonably expect $500 or less of tax.",
				"KRS 141.305(2)").
			Uses(working.ReadingForm).
			Affects(taxKnown && tax-withheld == y.threshold),
		working.NewDivergence("A return filed and paid by February 1").
			Form(fmt.Sprintf("Filing and paying in full by %s replaces the January payment. That holds only if you first needed to pay after %s.",
				y.calendar.NextBusinessDay(y.fileAndPayBy).Long(), y.lateStart.Long()),
				"42A740-S4 (9-25), the note to the 2026 due dates").
			Statute(fmt.Sprintf("Kentucky uses federal timing. Anyone who files and pays in full by January 31 can skip the January payment. For %d returns, that date is %s.",
				y.year, y.calendar.NextBusinessDay(y.fileAndPayBy).Long()),
				"KRS 141.305(3); 26 U.S.C. § 6654(h)").
			Uses(working.ReadingForm).
			Affects(byFebruary && !f.RequirementFirstMet().After(y.lateStart)),
		working.NewDivergence("When withholding counts as paid").
			Form("Withholding counts as paid 25% on each due date. You can't count it on the dates it was withheld.",
				"Form 2210-K (2025), 42A740-S1 (10-25), line 10").
			Statute("Kentucky adopts the federal method. It lets you count withholding on the dates it was actually withheld.",
				"KRS 141.305(3); 26 U.S.C. § 6654(g)(1)").
			Uses(working.ReadingForm).
			Affects(withheld > 0),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// rateDivergence is the 2026 penalty rate. It affects a penalty charged on
// any day of 2026.
func (r kentuckyEstimated) rateDivergence(charged bool) (working.Divergence, error) {
	return working.NewDivergence("The penalty rate for 2026").
		Form("Every 2026 day is charged at 9%, the 7% tax interest rate plus 2%. The form was revised in October 2025, before a 2026 act amended the rate law. That act took effect on July 15, 2026.",
			"Form 2210-K (2025), 42A740-S1 (10-25), line 21").
		Statute("The rate is the tax interest rate, set at 7% for 2026. The added 2% is for taxes not paid on time. The law keeps the estimated tax penalty out of it.",
			"KRS 141.305(3)(d); KRS 131.183(1)(a), (1)(b)1, (2)(a), as amended by 2026 Ky. Acts ch. 161, sec. 2, effective July 15, 2026; DOR, Tax Interest Rate Update for 01-01-26").
		Uses(working.ReadingForm).
		Affects(charged).
		Build()
}

// shortDate is a date as a person reads it: "Aug 31" in the tax year, and
// "Jan 15, 2027" outside it.
func (r kentuckyEstimated) shortDate(d estimated.Date) string {
	if d.Year() == r.y.year {
		return d.Time().Format("Jan 2")
	}
	return d.Time().Format("Jan 2, 2006")
}

func (r kentuckyEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	divergences, err := r.divergences(f)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)

	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	last := y.year - 1
	separate := f.FilingStatus() == filing.MarriedFilingSeparately

	// 90% of this year's tax.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(90), money.Percent(90).Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(90), 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("KRS 141.305(3); 26 U.S.C. § 6654(d)(1)(B)(i); 42A740-S4 (9-25)", fmt.Sprintf("90%% of this year's Kentucky tax of %v is %v", tax, thisYear.Amount()))
	}

	// Last year's tax: 100%, or 110% above the step.
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
	}
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, money.Percent(100), 0).Unavailable(why)
	}
	// Questions whose answer can change the target, asked only when no
	// exception sets it.
	var targetAsks []func()
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Kentucky return", last))
		targetAsks = append(targetAsks, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Kentucky return, and what tax did it show?", last),
				"With it, last year's tax could set your target instead of 90% of this year's.")
		})
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Kentucky return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Kentucky return covered %d months, and this option needs all 12", last, prior.Months()))
	default:
		priorOption = r.priorYearOption(f, extras, prior, priorTax, separate, &a.notes)
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
		targetAsks = append(targetAsks, func() {
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Kentucky tax to be?", y.year),
				fmt.Sprintf("90%% of it may lower your target. Tax of %s or less after withholding needs no payments.", y.threshold.Whole()))
		})
	}

	// KRS 141.305(1): payments are needed only when an income test is met.
	incomeMet := true
	nonWage, nonWageKnown := extras.NonWageIncome()
	filingMet, filingAnswered := extras.FilingThresholdMet()
	switch {
	case nonWageKnown && nonWage > y.incomeTest:
		a.notes.Rule("KRS 141.305(1)(a)", fmt.Sprintf("%v of gross income other than wages with Kentucky withholding is more than %v, so the estimated tax rules apply", nonWage, y.incomeTest))
	case filingAnswered && filingMet:
		a.notes.Rule("KRS 141.305(1)(b)", "Kentucky AGI is expected to reach the amount for which KRS 141.180 requires a return, so the estimated tax rules apply")
	case nonWageKnown && filingAnswered:
		incomeMet = false
	default:
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Will you need a Kentucky return or have over %s not withheld for Kentucky?", y.incomeTest.Whole()),
			"Payments are needed only if one is true, and the figures assume one is for now.")
	}

	// Form 2210-K: no liability last year, as a U.S. citizen or resident
	// alien, on a full-year return.
	noLiability := hasPrior && prior.Months() == 12 &&
		((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	resident, residentAnswered := extras.PriorYearUSResident()
	if noLiability && !residentAnswered {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Were you a U.S. citizen or resident for all of %d?", last),
			fmt.Sprintf("If so, no %d Kentucky tax means no estimated payments or penalty for %d.", last, y.year))
	}

	label := func(o estimated.TargetOption) string { return fmt.Sprintf("%s, %s", o.Label(), o.Amount().Whole()) }
	exempt := func(why string, ex estimated.TargetOption, err error) error {
		if err != nil {
			return err
		}
		a.exempt = true
		rb.Required(false, why).Exempt(ex)
		return nil
	}
	switch {
	case !incomeMet:
		ex, err := estimated.NewException("Income too low for Kentucky estimated payments").Build()
		if err := exempt(fmt.Sprintf("Your %s of income without Kentucky withholding is %s or less. Your Kentucky AGI won't reach the level that requires a return. Kentucky therefore needs no estimated payments for %d.",
			nonWage.Whole(), y.incomeTest.Whole(), y.year), ex, err); err != nil {
			return assessment{}, err
		}
		a.notes.Rule("KRS 141.305(1)", fmt.Sprintf("%v of non-wage gross income is not more than %v and AGI does not reach the KRS 141.180 filing amount, so no estimated payments are required", nonWage, y.incomeTest))
	case noLiability && residentAnswered && resident:
		ex, err := estimated.NewException("No Kentucky tax last year").Build()
		if err := exempt(fmt.Sprintf("You had no Kentucky tax for %d, a full year as a U.S. citizen or resident. No estimated payments are needed for %d, and no penalty can arise.", last, y.year), ex, err); err != nil {
			return assessment{}, err
		}
		a.notes.Rule("Form 2210-K (2025), 42A740-S1 (10-25), exceptions; 42A740-S4 (9-25)", fmt.Sprintf("the %d return showed no liability for a full 12 months, so there is no penalty for %d", last, y.year))
	case taxKnown && tax-withheld <= y.threshold:
		ex, err := estimated.ExceptionAtMost("Tax less withholding", tax-withheld, y.threshold)
		why := fmt.Sprintf("Your Kentucky tax of %s less %s of withholding leaves %s. That is %s or less, so no estimated payments are needed.",
			tax.Whole(), withheld.Whole(), (tax - withheld).Whole(), y.threshold.Whole())
		switch {
		case tax < withheld:
			why = fmt.Sprintf("Your Kentucky tax of %s less %s withheld %s. No estimated payments are needed.",
				tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
		case tax == withheld:
			why = fmt.Sprintf("Your withholding of %s covers your Kentucky tax of %s. No estimated payments are needed.", withheld.Whole(), tax.Whole())
		}
		if err := exempt(why, ex, err); err != nil {
			return assessment{}, err
		}
		a.notes.Rule("KRS 141.305(2); Form 2210-K (2025), 42A740-S1 (10-25), line 5", fmt.Sprintf("%v of tax less %v withheld is %v, %v or less, so there is no penalty", tax, withheld, tax-withheld, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("There's no target until you have your %d Kentucky tax or your %d return.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your target is %s. Your withholding of %s already reaches it. No estimated payments are needed whatever your %d tax turns out to be.",
			label(priorOpt), withheld.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %s. It avoids a penalty whatever your %d tax turns out to be. After %s of withholding, you need %s of estimated payments.",
			label(priorOpt), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case target == 0:
		rb.Required(false, fmt.Sprintf("Your target is %s. No estimated payments are needed.", label(priorOpt))).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Kentucky withholding of %s already reaches your target of %s. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		c := thisYear
		if chosen == estimated.BasisPriorYearTax {
			c = priorOpt
		}
		lead := fmt.Sprintf("Your Kentucky tax less withholding leaves %s, which is more than %s.", (tax - withheld).Whole(), y.threshold.Whole())
		if (tax - withheld).Whole() == y.threshold.Whole() {
			lead = fmt.Sprintf("Your Kentucky tax less withholding is more than %s.", y.threshold.Whole())
		}
		rb.Required(true, fmt.Sprintf("%s Your target is %s. You need %s of estimated payments.",
			lead, label(c), (target-withheld).Whole())).Choose(chosen)
	}
	if !a.exempt {
		for _, ask := range targetAsks {
			ask()
		}
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}

	// The dates. KY-TAM-19-02 moves one on a weekend or holiday to the next
	// working day.
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			a.notes.Rule("KY-TAM-19-02", fmt.Sprintf("the installment due %s falls on a weekend or holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
		}
	}
	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), quarters)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("KRS 141.305(3); 42A740-S4 (9-25); Form 2210-K (2025), 42A740-S1 (10-25), line 9", fmt.Sprintf("25%% of %v, %v, is due on each of %s, %s, %s and %s",
		a.requirement.Target(), a.installments[0], a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() && !a.exempt {
		periods := f.Periods()
		taxes := make([]money.USCents, len(periods))
		for i, p := range periods {
			taxes[i] = p.AnnualisedTax()
		}
		ann, err := estimated.Annualised(taxes, annualisedPercentages, a.installments)
		if err != nil {
			return assessment{}, err
		}
		a.installments = ann
		a.notes.Rule("KRS 141.305(3); 26 U.S.C. § 6654(d)(2); Form 2210-K (2025), 42A740-S1 (10-25), Part III", fmt.Sprintf("under the annualised method, used for every date, the installments are %v, %v, %v and %v: 22.5%%, 45%%, 67.5%% and 90%% of the annualised tax, "+
			"less earlier installments, and never more than the regular 25%% plus any shortfall carried forward", ann[0], ann[1], ann[2], ann[3]))
	}

	if !a.exempt {
		r.january(f, &a)
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}
	return a, nil
}

// priorYearOption is 100% or 110% of last year's tax on a filed full-year
// return.
func (r kentuckyEstimated) priorYearOption(f estimated.Facts, extras EstimatedFacts, prior estimated.PriorReturn, priorTax money.USCents, separate bool, notes *working.Notes) *estimated.TargetOptionBuilder {
	y := r.y
	last := y.year - 1
	pf, _ := prior.Figures()
	agi, known := pf.StateAGI()
	step := y.priorYearStep
	if separate {
		step = y.priorYearStepSeparate
	}
	whose := fmt.Sprintf("%d Kentucky AGI", last)
	spouseMissing := false
	if separate {
		combined, answered := extras.CombinedReturn()
		switch {
		case !answered:
			notes.Ask(working.PositionJurisdictionFact, "Do you and your spouse file separately on one combined Kentucky return?",
				"Until then, only your own income counts for the 110% test. A combined return adds your spouse's.")
		case combined:
			spouse, spouseKnown := extras.SpousePriorYearAGI()
			if !spouseKnown {
				known, spouseMissing = false, true
			} else {
				agi += spouse
				whose = fmt.Sprintf("the couple's combined %d Kentucky AGI", last)
				if known && agi > step && agi <= y.priorYearStep {
					notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("Kentucky doesn't say which limit tests your combined %d AGI of %s. The figures use %s for married filing separately, not %s.",
						last, agi.Whole(), step.Whole(), y.priorYearStep.Whole()))
				}
			}
		}
	}
	rate := money.Percent(100)
	switch {
	case !known:
		rate = money.Percent(110)
		question := fmt.Sprintf("What was your %d Kentucky AGI?", last)
		effect := fmt.Sprintf("Until then, last year's option is figured at 110%%. That is the rate when your AGI is more than %s.", step.Whole())
		if spouseMissing {
			question = fmt.Sprintf("What was your spouse's %d Kentucky AGI?", last)
			effect = fmt.Sprintf("Until then, last year's option is figured at 110%%. Your spouse's AGI is added to yours for the test at %s.", step.Whole())
		}
		notes.Ask(working.PositionAdjustedGrossIncome, question, effect)
	case agi > step:
		rate = money.Percent(110)
	}
	amount := rate.Of(priorTax)
	why := fmt.Sprintf("%s of %v is not more than %v", whose, agi, step)
	switch {
	case !known:
		why = fmt.Sprintf("%s was not given", whose)
	case rate == money.Percent(110):
		why = fmt.Sprintf("%s of %v is more than %v", whose, agi, step)
	}
	notes.Rule("KRS 141.305(3); 26 U.S.C. § 6654(d)(1)(B)(ii), (C); 42A740-S4 (9-25)", fmt.Sprintf("%s, so the prior-year test is %v of the %v tax on the %d return, or %v", why, rate, priorTax, last, amount))
	return estimated.NewTargetOption(estimated.BasisPriorYearTax, rate, amount)
}

// january applies the voucher instructions' note: a requirement that first
// arises after September 1 needs no January voucher if the return is filed
// and paid by February 1, 2027.
func (r kentuckyEstimated) january(f estimated.Facts, a *assessment) {
	y := r.y
	filed := f.ReturnFiled()
	fileBy := y.calendar.NextBusinessDay(y.fileAndPayBy)
	if filed.IsZero() || filed.After(fileBy) {
		return
	}
	firstMet := f.RequirementFirstMet()
	switch {
	case firstMet.IsZero():
		a.notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first expect to need estimated payments for %d?", y.year), fmt.Sprintf("Until then, the January payment is still due. If after %s, a return filed and paid by %s replaces it.",
			r.shortDate(y.lateStart), r.shortDate(fileBy)))
	case firstMet.After(y.lateStart):
		a.excused = true
		a.notes.Rule("42A740-S4 (9-25), the note to the 2026 due dates", fmt.Sprintf("the requirement first arose on %s, after %s, and the return was filed and paid on %s, by %s, so the January installment is excused",
			firstMet.Long(), y.lateStart.Long(), filed.Long(), fileBy.Long()))
	default:
		a.notes.Rule("42A740-S4 (9-25), the note to the 2026 due dates", fmt.Sprintf("the return was filed and paid on %s, by %s, but the requirement arose on %s, not after %s, so the January installment is still due; "+
			"the statute's reading is in Divergences", filed.Long(), fileBy.Long(), firstMet.Long(), y.lateStart.Long()))
	}
}

func (r kentuckyEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies the credits to the installments, with withholding spread
// 25% to each date as Form 2210-K line 10 does.
func (r kentuckyEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, quarters)
	if err != nil {
		return nil, working.Notes{}, err
	}
	required := append([]money.USCents(nil), a.installments...)
	if a.excused {
		required[3] = 0
	}
	ledger, err := estimated.Apply(a.dues, required, credits)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	notes.Rule("Form 2210-K (2025), 42A740-S1 (10-25), line 10", fmt.Sprintf("the %v withheld is treated as paid 25%% on each due date, whenever it was actually withheld", f.WithheldTotal()))
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("Form 2210-K (2025), 42A740-S1 (10-25), line 10", "Form 2210-K offers no election to credit withholding on the dates withheld, so it is spread evenly; the statute's reading is in Divergences")
	}
	return ledger, notes, nil
}

func (r kentuckyEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Kentucky return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := installments(a, ledger)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// installments are the due dates with what was needed and credited by each.
func installments(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	var needed money.USCents
	for i, l := range ledger {
		needed += a.installments[i]
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(needed).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if a.excused && i == 3 {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r kentuckyEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("kentuckytaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Kentucky return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := asOf
	if y.stop.Before(stop) {
		stop = y.stop
	}
	paidInFull := false
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop, paidInFull = filed, true
	}
	waiver, waiverAnswered := f.Positions().Waiver()
	if a.exempt {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, nil).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		rate, err := r.rateDivergence(false)
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		var rn working.Notes
		rn.Diverges(rate)
		return working.NewResult(p).With(a.notes).With(rn).Build()
	}

	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule, err := installments(a, ledger)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	p, unrated, err := estimated.SimplePenalty(ledger, stop, y.rates, waiver, a.requirement, schedule)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("KRS 141.305(3); 26 U.S.C. § 6654(d)(1)(B); 42A740-S4 (9-25)", fmt.Sprintf("this year's Kentucky tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("KRS 141.305(3); 26 U.S.C. § 6654(b)(2); Form 2210-K (2025), 42A740-S1 (10-25)", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("KRS 141.305(3)(d); Form 2210-K (2025), 42A740-S1 (10-25), lines 17 to 21", fmt.Sprintf("each shortfall is charged simple interest by the day at the rate the form prints for its calendar year, from its due date until paid or %s, "+
		"with each payment applied to the oldest shortfall first; the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	charged := false
	for _, l := range p.Lines() {
		if len(l.Parts()) > 0 {
			charged = true
		}
	}
	rate, err := r.rateDivergence(charged)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	notes.Diverges(rate)
	if charged {
		first := y.rates.Periods()[0]
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The form charges %v on your %d shortfalls, the 7%% tax interest rate plus 2%%. The figures follow the form, though the law may mean 7%%, about 7/9 the penalty.",
			first.Rate(), y.year))
	}
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The Kentucky rate for the days after %s isn't published yet.", unrated.AddDays(-1).Long()))
	}
	if p.Total() > 0 {
		switch {
		case !waiverAnswered:
			notes.Ask(working.PositionPenaltyWaiver, "Are you asking Kentucky to waive your penalty for reasonable cause?",
				"It's before any waiver, which a disaster that destroyed your records can earn. So can the death or serious illness of you or your immediate family.")
		case waiver == estimated.WaiverRetiredOrDisabled:
			notes.Rule("KRS 141.305(3)(e); KRS 131.175; 103 KAR 1:040 Section 1", "Kentucky has no waiver for retirement or disability as such; a waiver needs reasonable cause under 103 KAR 1:040, and the penalty shown is before any waiver")
		case waiver == estimated.WaiverCasualty:
			notes.Rule("KRS 141.305(3)(e); KRS 131.175; 103 KAR 1:040 Section 1(5)", "a casualty is reasonable cause only as 103 KAR 1:040 describes it, such as a catastrophic event that destroys or withholds records; the department decides, and the penalty shown is before any waiver")
		}
		if o, ok := kentuckyWaiver(f); ok {
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// kentuckyWaiver decides a waiver asked for. KRS 141.305(3)(e) sends every
// waiver through KRS 131.175, and 103 KAR 1:040 Section 1 lists the
// reasonable causes: a catastrophic event that destroys or withholds records
// is one, and retirement and disability are not.
func kentuckyWaiver(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	k, answered := f.Positions().Waiver()
	switch {
	case !answered || k == estimated.WaiverNone:
		return estimated.WaiverOutcome{}, false
	case k == estimated.WaiverCasualty:
		return estimated.WaiverEligible("Records a casualty or disaster destroyed or kept from you are reasonable cause. Kentucky's Department of Revenue decides."), true
	default:
		return estimated.WaiverRefused("Kentucky waives only for reasonable cause, not retirement or disability alone. A serious illness or a disaster that destroyed your records can qualify."), true
	}
}

func (r kentuckyEstimated) Explanation() estimated.Explanation { return kentuckyExplanation }

// kentuckyExplanation is written by hand and checked against
// research/estimated-payments/ky.md.
var kentuckyExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Kentucky follows the federal rules closely, with a lower threshold. You need estimated payments once tax after withholding is more than $500. The penalty is interest on each quarter's shortfall.").
		Paragraph("Payments are needed if you expect over $500 of tax after withholding. Refundable credits count with withholding. You must also expect over $5,000 of income without Kentucky withholding. Expecting income high enough to need a Kentucky return counts instead. Your target is 90% of this year's Kentucky tax or 100% of last year's, if less. Withholding and payments that reach it avoid the penalty. Last year's figure needs a full-year return. It's 110% if last year's Kentucky AGI was over $150,000. That limit is $75,000 for married filing separately.").
		Paragraph("A quarter is due April 15, June 15, September 15 and the next January 15. A date on a weekend or holiday shifts to the next working day. Withholding counts as paid evenly on the four dates, whenever it was taken.").
		Paragraph("There's no penalty if this year's tax after withholding is $500 or less. There's none either if you had no Kentucky tax last year on a full-year return. As federally, each shortfall is charged for each day it stays unpaid. Charges stop at the next April 15. Each payment goes to the oldest shortfall first. Form 2210-K's annualized method can lower early payments for uneven income. Once used, it must be used for every date.").
		Paragraph("The rate is fixed for each calendar year. The department's form charges 9% for 2026. Its announced 2026 tax interest rate is 7%, and the law can be read either way. The penalty can be waived only for reasonable cause. A serious illness or a disaster that destroys your records can be one. Retirement or disability alone is not.").
		VersusFederal("Kentucky differs in its $500 threshold. Its income tests decide whether payments are needed at all. The 110% test is on Kentucky AGI, adding a combined return's incomes. The rate is fixed for the year, 9% on the form for 2026. Filing by Feb 1 excuses January only if you first had to pay after September 1. You can't elect to count withholding when withheld. Waivers need reasonable cause.").
		Source("KRS 141.305; KRS 131.183; KRS 131.175; 103 KAR 1:040").
		Source("Instructions for Filing Estimated Tax Vouchers 2026, 42A740-S4 (9-25), and Form 740-ES 2026, 42A740ES (10/25)").
		Source("Form 2210-K, Underpayment of Estimated Tax by Individuals, 2025, 42A740-S1 (10-25), and its instructions").
		Source("KY-TAM-19-02, Requirement to Pay Estimated Tax Installments").
		Source("Department of Revenue, Tax Interest Rate Update for 01-01-26").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
