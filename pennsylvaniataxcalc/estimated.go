package pennsylvaniataxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Pennsylvania's estimated tax rules, Tax Reform Code of 1971 §§ 325, 326 and
// 352(d) (72 P.S. §§ 7325, 7326, 7352(d)), as the shared
// [estimated.EstimatedTax] interface.
//
// Pennsylvania differs from the federal rules in shape, not only in numbers.
// Whether payments are needed turns on income received without withholding.
// The prior-year test is last year's taxable income at this year's rate, not
// last year's tax. The penalty is measured against 90% of this year's tax in
// every case, and the prior-year test and the income-to-date test are
// exceptions that take the interest off an installment rather than lowering
// it. A filer who first meets the requirement late can have the earlier
// installments excused, but only under a special exception.
//
// The figure follows the department's forms, REV-413 (I) for 2026 and
// REV-1630 for 2025, and the result's Divergences say where the Tax Reform
// Code reads otherwise. No act since 2022 changed the estimated tax rules
// (research/estimated-payments/pa.md), so no form here predates the law it
// describes.

// EstimatedFacts are the facts Pennsylvania's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
//
// Pennsylvania's REV-1630 starts from the tax before credits and subtracts
// the credits together with withholding. [estimated.Figures.Tax] is after
// credits everywhere, so the credits come here, and the rules add them back.
type EstimatedFacts struct {
	incomeWithoutWithholding optional
	credits                  optional
	priorForgiveness         optional
	priorFullForgiveness     bool
	priorFullAnswered        bool
}

type optional struct {
	v   money.USCents
	set bool
}

// IncomeWithoutWithholding is the year's PA-taxable income from which no
// employer withholds Pennsylvania tax: REV-414 (I) line 1. It decides whether
// the estimated tax rules apply at all.
func (e EstimatedFacts) IncomeWithoutWithholding() (money.USCents, bool) {
	return e.incomeWithoutWithholding.v, e.incomeWithoutWithholding.set
}

// Credits are this year's Tax Forgiveness, resident and other credits, PA-40
// lines 21, 22 and 23. [estimated.Figures.Tax] is already net of them; the
// 90% test is taken on the tax before them.
func (e EstimatedFacts) Credits() (money.USCents, bool) { return e.credits.v, e.credits.set }

// PriorYearForgiveness is last year's Tax Forgiveness Credit, PA-40 line 21,
// which REV-1630 subtracts from the prior-year income test.
func (e EstimatedFacts) PriorYearForgiveness() (money.USCents, bool) {
	return e.priorForgiveness.v, e.priorForgiveness.set
}

// PriorYearFullForgiveness reports whether the filer qualified for 100% Tax
// Forgiveness last year, which takes this year out of the penalty, and
// whether the caller said.
func (e EstimatedFacts) PriorYearFullForgiveness() (qualified, answered bool) {
	return e.priorFullForgiveness, e.priorFullAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) IncomeWithoutWithholding(a money.USCents) *EstimatedFactsBuilder {
	b.e.incomeWithoutWithholding = optional{a, true}
	return b
}

func (b *EstimatedFactsBuilder) Credits(a money.USCents) *EstimatedFactsBuilder {
	b.e.credits = optional{a, true}
	return b
}

func (b *EstimatedFactsBuilder) PriorYearForgiveness(a money.USCents) *EstimatedFactsBuilder {
	b.e.priorForgiveness = optional{a, true}
	return b
}

func (b *EstimatedFactsBuilder) PriorYearFullForgiveness(qualified bool) *EstimatedFactsBuilder {
	b.e.priorFullForgiveness, b.e.priorFullAnswered = qualified, true
	return b
}

// Build refuses negative credits. Income can be negative, as a loss.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	e := b.e
	if e.credits.v < 0 || e.priorForgiveness.v < 0 {
		return EstimatedFacts{}, errors.New("pennsylvaniataxcalc: credits cannot be negative")
	}
	return e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// rate is the personal income tax rate, 72 P.S. § 7302.
	rate money.Rate

	// incomeThreshold is the income not subject to withholding at which the
	// rules start, and taxThreshold the tax after withholding and credits
	// below which they do not.
	incomeThreshold, taxThreshold money.USCents

	dues []estimated.Date

	// laterGroups are the first days of the REV-413 (I) table's second,
	// third and fourth rows: a requirement first met on or after
	// laterGroups[i] starts at installment i+2.
	laterGroups []estimated.Date

	calendar     estimated.Calendar
	fileAndPayBy estimated.Date
	stop         estimated.Date
	periodEnds   []estimated.Date

	// daily is the factor REV-1611 prints for each calendar year, one period
	// a year, so that each year's days are charged and rounded on their own
	// as REV-1630 lines 15a to 15c do.
	daily estimated.FactorSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are REV-1630 line 5: line 4, after all withholding and credits,
// divided by four. Withholding is therefore treated as paid in four equal
// parts, and so are the credits.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Pennsylvania's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, estimated.Pennsylvania, year)
	}
	return pennsylvaniaEstimated{y}, nil
}

type pennsylvaniaEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	dues        []estimated.Date

	// taxKnown is whether this year's tax was given. Without it the regular
	// installments are measured against the prior-year test, and the penalty
	// cannot be figured for an installment that misses it.
	taxKnown bool

	// regular is each installment REV-1630 line 5 requires: a quarter of 90%
	// of the tax before credits, less the credits. The penalty is on a
	// shortfall against these, whichever exception the filer meets.
	regular []money.USCents

	// lesser is the running total of the smaller of the two tests, a quarter
	// a time. Credits that reach it by a due date put that installment under
	// Exception 1 if the prior-year test is the smaller.
	lesser []money.USCents

	// exception1 and exception2 are the running totals that excuse an
	// installment, or nil where the exception is not open.
	exception1, exception2 []money.USCents

	// credits are this year's PA-40 lines 21 to 23.
	credits money.USCents

	// first is the installment a late starter's requirement starts at, from
	// 0; special is whether the filer owed no estimates last year, which the
	// special exception needs.
	first   int
	special bool

	excusedJanuary bool

	notes working.Notes
}

func (r pennsylvaniaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("pennsylvaniataxcalc: the facts carry %T, not pennsylvaniataxcalc.EstimatedFacts", s)
	}
}

func (r pennsylvaniaEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != estimated.Pennsylvania:
		return fmt.Errorf("pennsylvaniataxcalc: these are Pennsylvania's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("pennsylvaniataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case int64(f.FarmShare())*3 >= 20_000:
		return fmt.Errorf("%w: a farmer with two-thirds of gross income from farming files REV-1630A",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("pennsylvaniataxcalc: REV-1630 Exception 2 period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms and the Tax Reform Code part
// ways on a rule every Pennsylvania computation uses. They are found in
// research/estimated-payments/pa.md, conflicts A to D and F, and checked
// there against both. Every figure follows the form.
func (r pennsylvaniaEstimated) divergences(t touches) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("When the rules start").
			Form("The rules apply at $14,000 or more of income not subject to withholding. They also need $430 or more of tax after withholding and credits.",
				"REV-413 (I) (2026), Who Must Make Estimated Tax Payments, Penalty for Failure to Pay Estimated Taxes and Line 17").
			Statute("Payments are required only if that income is expected to exceed $14,000. Exactly $14,000 doesn't start the rules.",
				"72 P.S. § 7325(a)(1)").
			Uses(working.ReadingForm).Affects(t.start),
		working.NewDivergence("A requirement first met after April 1").
			Form("Every installment is a quarter of the year's amount. Earlier ones are excused only under the special exception. It needs no estimates due the year before. The whole catch-up must be paid in the first period. Groups start April 1, June 1 and September 1.",
				"REV-1630 (2025), Line 5 instructions and Determining the Underpayment Amount; REV-413 (I) (2026), Payment Due Dates Table").
			Statute("Nothing is due before the requirement arises. Every late starter pays in equal parts on the dates left. That's thirds from June 15 and halves from September 15. The groups start the day after April 1, June 1 and September 1.",
				"72 P.S. §§ 7325(d), 7326(a); 61 Pa. Code § 115.9(a)").
			Uses(working.ReadingForm).Affects(t.lateStart),
		working.NewDivergence("A return filed and paid by January 31").
			Form("Any final return filed and paid by January 31 counts as the fourth installment.",
				"REV-1630 (2025), Line 6 instructions").
			Statute("The return replaces the January 15 payment only if that was your first payment. That's when the requirement arose after September 1.",
				"72 P.S. § 7325(h); 61 Pa. Code § 115.9(a)(4)").
			Uses(working.ReadingForm).Affects(t.january),
		working.NewDivergence("The prior-year test after a part-year return").
			Form("Last year's income test is open only if you were a full-year resident that year.",
				"REV-413 (I) (2026), Safe Harbor Rules and Line 10; REV-1630 (2025), Section II").
			Statute("The test uses last year's return, with no condition of full-year residence.",
				"72 P.S. § 7352(d)(2)(A)").
			Uses(working.ReadingForm).Affects(t.partYear),
		working.NewDivergence("The daily interest factor").
			Form("Each day is charged at a printed factor of 0.000192. That's 7% divided by 365, rounded to six places.",
				"REV-1630 (2025), Lines 15a to 15c; REV-1611 (11-25)").
			Statute("Interest is at the annual rate set for the year, 7% for 2026. That's 0.00019178 a day.",
				"72 P.S. § 7352(d); 72 P.S. § 806; Interest Rate Notice, 55 Pa.B. 8829").
			Uses(working.ReadingForm).Affects(t.interest),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// touches is which divergences the facts reach: where the two readings would
// give a different answer, schedule or penalty.
type touches struct {
	start     bool // income without withholding of exactly the threshold
	lateStart bool // a requirement first met after the first group starts
	january   bool // a return filed and paid by January 31, not a September starter
	partYear  bool // a filed return as a part-year resident last year
	interest  bool // a shortfall that bears interest
}

func (r pennsylvaniaEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	var targetQs targetQuestions
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	last := y.year - 1
	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	credits, creditsKnown := extras.Credits()
	a.credits = credits
	prior, hasPrior := f.PriorYear()
	var priorFigures estimated.Figures
	if hasPrior {
		priorFigures, _ = prior.Figures()
	}
	priorTF, priorTFKnown := extras.PriorYearForgiveness()

	// The 90% test, REV-1630 lines 1a to 4, written as what withholding and
	// payments must reach: 90% of the tax before credits, less the credits.
	before := tax + credits
	thisAmount := max(0, money.Percent(90).Of(before)-credits)
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(90), thisAmount)
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(90), 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("72 P.S. § 7352(d)(1); REV-1630 (2025), Section I, lines 1a to 4", fmt.Sprintf("90%% of the %v tax before credits is %v; less %v of credits, withholding and payments must reach %v",
			before, money.Percent(90).Of(before), credits, thisAmount))
	}

	// Exception 1: last year's taxable income at this year's rate, less last
	// year's Tax Forgiveness, for a full-year resident who filed.
	var priorOption *estimated.TargetOptionBuilder
	resident, residenceAnswered := prior.FullYearResident()
	priorIncome, priorIncomeKnown := priorFigures.TaxableIncome()
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearIncome, money.Percent(100), 0).Unavailable(why)
	}
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Pennsylvania return", last))
		targetQs.Ask(working.PositionPriorYearReturn, fmt.Sprintf("What was the taxable income on your %d Pennsylvania return?", last),
			fmt.Sprintf("With it, last year's income at the %d rate could set your target instead.", y.year))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Pennsylvania return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Pennsylvania return covered %d months; this test needs a full year", last, prior.Months()))
	case !residenceAnswered:
		priorOption = unavailable(fmt.Sprintf("Needs whether you were a full-year Pennsylvania resident in %d", last))
		targetQs.Ask(working.PositionResidency, fmt.Sprintf("Were you a full-year Pennsylvania resident in %d?", last),
			"If you were, last year's income at this year's rate could set your target.")
	case !resident:
		priorOption = unavailable(fmt.Sprintf("You weren't a full-year Pennsylvania resident in %d", last))
	case !priorIncomeKnown:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Pennsylvania taxable income", last))
		targetQs.Ask(working.PositionPriorYearReturn, fmt.Sprintf("What was your %d Pennsylvania taxable income, line 11 of your PA-40?", last),
			"With it, last year's income at this year's rate could set your target.")
	default:
		atRate := y.rate.Of(priorIncome)
		ex1 := max(0, atRate-priorTF)
		amount := max(0, ex1-credits)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearIncome, money.Percent(100), amount)
		a.notes.Rule("72 P.S. § 7352(d)(2)(A); REV-1630 (2025), Section II, line 12; REV-413 (I) (2026), lines 10 to 14", fmt.Sprintf("the %d taxable income of %v at the %d rate of %v is %v; less %v of %d Tax Forgiveness, %v; less this year's %v of credits, withholding and payments must reach %v",
			last, priorIncome, y.year, y.rate, atRate, priorTF, last, ex1, credits, amount))
		if !priorTFKnown {
			targetQs.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What was your %d Tax Forgiveness Credit, line 21 of your PA-40?", last),
				"Until then, nothing comes off the test on last year's income. That target can only be too high.")
		}
	}
	priorOpt, err := priorOption.Build()
	if err != nil {
		return assessment{}, err
	}

	rb := estimated.NewRequirement().Option(thisYear).Option(priorOpt)
	chosen, target := estimated.BasisThisYearTax, thisYear.Amount()
	if priorOpt.Available() && (!taxKnown || priorOpt.Amount() < target) {
		chosen, target = estimated.BasisPriorYearIncome, priorOpt.Amount()
	}
	if !taxKnown {
		targetQs.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Pennsylvania tax to be?", y.year),
			map[bool]string{true: "Without it, the target can only come from last year's income.", false: "Without it, there's no target."}[priorOpt.Available()])
	}

	income, incomeKnown := extras.IncomeWithoutWithholding()
	fullForgiveness, forgivenessAnswered := extras.PriorYearFullForgiveness()
	switch {
	case fullForgiveness:
		a.exempt = true
		forgiven, err := estimated.NewException("Full Tax Forgiveness last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You qualified for 100%% Tax Forgiveness for %d. Pennsylvania charges no estimated tax penalty for %d. No estimated payments are needed.", last, y.year)).Exempt(forgiven)
		a.notes.Rule("Act 85 of 2012; REV-1630 (2025), Before You Begin", fmt.Sprintf("100%% Tax Forgiveness for %d takes %d out of the estimated tax penalty", last, y.year))
	case incomeKnown && income < y.incomeThreshold:
		a.exempt = true
		under, err := estimated.ExceptionBelow("Income without withholding", income, y.incomeThreshold)
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your income without Pennsylvania withholding is %s. The estimated tax rules don't apply for %d.", leftUnder(income, y.incomeThreshold), y.year)).Exempt(under)
		a.notes.Rule("72 P.S. § 7325(a); REV-413 (I) (2026), Penalty for Failure to Pay Estimated Taxes", fmt.Sprintf("%v of income not subject to withholding is under %v, so no penalty applies", income, y.incomeThreshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target is 90%% of your %d Pennsylvania tax. Where that test is open, it may be your %d taxable income at the %d rate. Without one of those figures, there's no target to pay toward yet.",
			y.year, last, y.year)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your %d income at the %d rate is %s. Your Pennsylvania withholding of %s already reaches it. No estimated payments are needed, whatever your %d tax is.",
			last, y.year, target.Whole(), withheld.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is your %d income at the %d rate, %s. After %s of withholding, that's %s of estimated payments. Pay a quarter by each date. That keeps you clear of a penalty whatever your %d tax turns out to be.",
			last, y.year, target.Whole(), withheld.Whole(), (target-withheld).Whole(), y.year)).Choose(chosen)
	case tax-withheld < y.taxThreshold:
		a.exempt = true
		under, err := estimated.ExceptionBelow("Tax less withholding and credits", tax-withheld, y.taxThreshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your Pennsylvania tax after credits is %s, and %s was withheld. That leaves %s, so no estimated payments are needed.",
			tax.Whole(), withheld.Whole(), leftUnder(tax-withheld, y.taxThreshold))
		switch {
		case withheld == 0:
			why = fmt.Sprintf("You have no Pennsylvania withholding, and your tax after credits is %s. That's under %s, so no estimated payments are needed.", tax.Whole(), y.taxThreshold.Whole())
		case withheld > tax:
			why = fmt.Sprintf("Your Pennsylvania tax after credits is %s, and %s was withheld. That %s, so no estimated payments are needed.", tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
		case withheld == tax:
			why = fmt.Sprintf("Your Pennsylvania withholding covers your tax after credits of %s. No estimated payments are needed.", tax.Whole())
		}
		rb.Required(false, why).Exempt(under)
		a.notes.Rule("REV-1630 (2025), Section I, line 3; REV-413 (I) (2026), Line 17", fmt.Sprintf("%v of tax less %v of withholding and %v of credits is %v, under %v, so there is no penalty", before, withheld, credits, tax-withheld, y.taxThreshold))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Pennsylvania withholding of %s already reaches your %s target. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	case withheld == 0:
		rb.Required(true, fmt.Sprintf("You have no Pennsylvania withholding, and your tax after credits is %s. That's %s or more, and your target is %s. You need estimated payments of %s, a quarter by each date.",
			tax.Whole(), y.taxThreshold.Whole(), target.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Pennsylvania tax after credits is %s, and %s was withheld. That leaves %s, which is %s or more, and your target is %s. You need %s of estimated payments, a quarter by each date.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.taxThreshold.Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	targetQs.askUnlessExempt(&a.notes, a.exempt)
	if !a.exempt {
		if !creditsKnown {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What are your %d Tax Forgiveness, resident and other credits?", y.year),
				"They're PA-40 lines 21 to 23, and until then none are counted. They can only lower what you need to pay.")
		}
		if !incomeKnown {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("How much of your %d income will have no Pennsylvania tax withheld?", y.year),
				fmt.Sprintf("If it's under %s, the estimated tax rules don't apply to you.", y.incomeThreshold.Whole()))
		}
		if !forgivenessAnswered {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did you qualify for 100%% Tax Forgiveness for %d?", last),
				fmt.Sprintf("If you did, there's no estimated tax penalty for %d.", y.year))
		}
	}

	// What the divergences turn on, apart from the interest, which needs the
	// installments below.
	firstMet := f.RequirementFirstMet()
	filed := f.ReturnFiled()
	t := touches{
		start:     !a.exempt && incomeKnown && income == y.incomeThreshold,
		lateStart: !a.exempt && !firstMet.IsZero() && !firstMet.Before(y.laterGroups[0]),
		january: !a.exempt && !filed.IsZero() && !filed.After(y.fileAndPayBy) &&
			(firstMet.IsZero() || !firstMet.After(y.laterGroups[2])),
		partYear: !a.exempt && hasPrior && prior.Filed() && residenceAnswered && !resident,
	}

	if a.exempt {
		a.regular = make([]money.USCents, len(a.dues))
		a.lesser = make([]money.USCents, len(a.dues))
		if err := r.diverge(&a, t); err != nil {
			return assessment{}, err
		}
		return a, nil
	}

	// The installments. The penalty is always measured against 90%; the
	// smaller test only excuses. Without this year's tax the 90% is not
	// known, and the installments are measured against the target instead.
	regularOf := thisAmount
	if !taxKnown {
		regularOf = target
	}
	cum, err := estimated.CumulativeShares(regularOf, quarters)
	if err != nil {
		return assessment{}, err
	}
	a.regular = estimated.Increments(cum)
	if a.lesser, err = estimated.CumulativeShares(target, quarters); err != nil {
		return assessment{}, err
	}
	if priorOpt.Available() {
		if a.exception1, err = estimated.CumulativeShares(priorOpt.Amount(), quarters); err != nil {
			return assessment{}, err
		}
	}
	a.notes.Rule("72 P.S. § 7326(a)(1); REV-1630 (2025), Section I, line 5; REV-413 (I) (2026), Payment Due Dates Table", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", regularOf, a.regular[0],
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
	if !taxKnown {
		a.notes.Rule("72 P.S. § 7352(d)(2)(A); REV-1630 (2025), Section II, Exception 1",
			fmt.Sprintf("this year's tax was not given, so each installment is measured against a quarter of the %d income at the %d rate, %v; "+
				"REV-1630 charges an installment that misses it on its shortfall against 90%% of this year's tax", last, y.year, a.regular[0]))
	}
	if chosen == estimated.BasisPriorYearIncome && taxKnown {
		a.notes.Rule("72 P.S. § 7352(d)(2)(A); REV-1630 (2025), Section II, Exception 1", fmt.Sprintf("an installment is excused when withholding and payments by its date reach a quarter of %v for each date so far; "+
			"if they do not, the penalty is on the shortfall against %v a quarter", priorOpt.Amount(), a.regular[0]))
	}

	// Exception 2: 90% of the tax on the income actually received to each
	// period end, with no annualising.
	if f.Elections().Annualised() {
		a.exception2 = make([]money.USCents, len(a.dues))
		ccum, err := estimated.CumulativeShares(credits, quarters)
		if err != nil {
			return assessment{}, err
		}
		for i, p := range f.Periods() {
			a.exception2[i] = max(0, money.Percent(90).Of(y.rate.Of(p.Income()))-ccum[i])
		}
		a.notes.Rule("72 P.S. § 7352(d)(2)(B); REV-1630 (2025), Section II, Exception 2", fmt.Sprintf("an installment is also excused when withholding and payments by its date reach 90%% of %v of the income received to the period's end, less a quarter of the credits for each date so far: %v, %v, %v and %v",
			y.rate, a.exception2[0], a.exception2[1], a.exception2[2], a.exception2[3]))
	}

	// A late starter: REV-1630's special exception.
	a.first, a.special = r.lateStart(f, &a, priorFigures, prior, hasPrior, priorTF, priorTFKnown)

	// January 31: REV-1630 counts the payment with a final return filed by
	// then as the fourth installment. January 31, 2027 is a Sunday; the form
	// prints January 31 and says nothing of moving it, so it is not moved.
	if !filed.IsZero() && !filed.After(y.fileAndPayBy) {
		a.excusedJanuary = true
		a.notes.Rule("REV-1630 (2025), Line 6 instructions; 72 P.S. § 7325(h)", fmt.Sprintf("the return was filed and paid on %s, by %s, so the payment with it is the fourth installment", filed.Long(), y.fileAndPayBy.Long()))
	}

	// The daily factor matters only to a shortfall that bears interest.
	ledger, _, err := r.ledger(f, a)
	if err != nil {
		return assessment{}, err
	}
	for i, s := range r.standings(a, ledger) {
		if ledger[i].Shortfall() > 0 && !s.excused {
			t.interest = true
		}
	}
	if err := r.diverge(&a, t); err != nil {
		return assessment{}, err
	}
	return a, nil
}

// diverge adds the divergences to the assessment's notes.
func (r pennsylvaniaEstimated) diverge(a *assessment, t touches) error {
	ds, err := r.divergences(t)
	if err != nil {
		return err
	}
	a.notes.Diverges(ds...)
	return nil
}

// leftUnder is an amount under limit, as "$742, under $1,000", or "just
// under $1,000" when whole dollars would show the two as equal.
func leftUnder(amount, limit money.USCents) string {
	if amount.Whole() == limit.Whole() {
		return "just under " + limit.Whole()
	}
	return amount.Whole() + ", under " + limit.Whole()
}

// lateStart returns the installment the requirement first applies to, and
// whether the filer owed no estimated payments last year, which REV-1630's
// special exception needs before it excuses the earlier installments.
func (r pennsylvaniaEstimated) lateStart(f estimated.Facts, a *assessment, pf estimated.Figures, prior estimated.PriorReturn, hasPrior bool, priorTF money.USCents, priorTFKnown bool) (int, bool) {
	y := r.y
	last := y.year - 1
	special, known := false, false
	switch {
	case !hasPrior:
	case prior.NotRequiredToFile():
		special, known = true, true
	case !prior.Filed():
		known = true
	default:
		priorTax, _ := pf.Tax()
		w, wKnown := pf.Withheld()
		switch {
		case wKnown && priorTax-w >= y.taxThreshold:
			// Line A can only be larger with the forgiveness credit added.
			known = true
		case wKnown && priorTFKnown:
			special, known = priorTax+priorTF-w < y.taxThreshold, true
		}
	}
	firstMet := f.RequirementFirstMet()
	if firstMet.IsZero() {
		if special || !known {
			a.notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first expect to need estimated payments for %d?", y.year),
				fmt.Sprintf("The quarters before that date can be excused. That needs no estimated payments due from you for %d.", last))
		}
		return 0, special
	}
	if !known {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("What were your %d Pennsylvania tax, withholding and Tax Forgiveness Credit?", last),
			"They decide whether the quarters before you first needed to pay can be excused.")
	}
	first := 0
	for i, g := range y.laterGroups {
		if !firstMet.Before(g) {
			first = i + 1
		}
	}
	return first, special
}

// ledger applies the credits to the regular installments.
func (r pennsylvaniaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, quarters)
	if err != nil {
		return nil, working.Notes{}, err
	}
	required := append([]money.USCents(nil), a.regular...)
	if a.excusedJanuary {
		required[3] = 0
	}
	ledger, err := estimated.Apply(a.dues, required, credits)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	notes.Rule("REV-1630 (2025), Section I, lines 2 to 10 and the instructions for lines 14a to 14c",
		fmt.Sprintf("the %v withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld, "+
			"and a payment after a due date first reduces the oldest underpayment, on the day it is paid", f.WithheldTotal()))
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("REV-1630 (2025), Section I, line 5", "Pennsylvania has no election to credit withholding on the dates it was withheld, so it is spread evenly")
	}
	return ledger, notes, nil
}

// standing is where each installment stands once the exceptions are applied.
type standing struct {
	neededBy money.USCents // the chosen target's running total
	excused  bool
	applied  string // the exception that excused it, for the working note
}

// The exceptions a standing names, which only the working notes read.
const (
	appliedJanuary    = "the return was filed and paid by January 31"
	appliedSpecial    = "the special exception: no estimates were owed last year, and the whole catch-up was paid by the first date the requirement applied"
	appliedException1 = "Exception 1: withholding and payments reached last year's income at this year's rate"
	appliedException2 = "Exception 2: withholding and payments reached 90% of the tax on the income received to the period's end"
)

func (r pennsylvaniaEstimated) standings(a assessment, ledger []estimated.LedgerEntry) []standing {
	out := make([]standing, len(ledger))
	// The special exception holds when the filer paid, by the first date the
	// requirement applied, everything due by then.
	specialHolds := false
	if a.special && a.first > 0 {
		need := a.lesser[a.first]
		if a.exception2 != nil {
			need = min(need, a.exception2[a.first])
		}
		specialHolds = ledger[a.first].CreditedBy() >= need
	}
	for i, l := range ledger {
		s := standing{neededBy: a.lesser[i]}
		if a.exception2 != nil {
			s.neededBy = min(s.neededBy, a.exception2[i])
		}
		if specialHolds && i < a.first {
			s.neededBy = 0
		}
		switch {
		case a.excusedJanuary && i == 3:
			s.excused, s.applied = true, appliedJanuary
		case l.Shortfall() == 0:
		case specialHolds && i < a.first:
			s.excused, s.applied = true, appliedSpecial
		case a.exception1 != nil && l.CreditedBy() >= a.exception1[i]:
			s.excused, s.applied = true, appliedException1
		case a.exception2 != nil && l.CreditedBy() >= a.exception2[i]:
			s.excused, s.applied = true, appliedException2
		}
		out[i] = s
	}
	return out
}

// schedule is the installments from the ledger. An installment credited
// with what it needs is met and shows no shortfall: the shortfall against
// 90% that REV-1630 computes is excused by an exception and bears no
// interest. One short of what it needs keeps the form's shortfall.
func (r pennsylvaniaEstimated) schedule(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, working.Notes, error) {
	var notes working.Notes
	out := make([]estimated.Installment, len(ledger))
	for i, s := range r.standings(a, ledger) {
		l := ledger[i]
		shortfall := l.Shortfall()
		if l.CreditedBy() >= s.neededBy {
			shortfall = 0
		}
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(s.neededBy).CreditedBy(l.CreditedBy()).Shortfall(shortfall)
		if s.excused {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, working.Notes{}, err
		}
		if s.applied != "" && l.Shortfall() > 0 {
			notes.Rule("72 P.S. § 7352(d)(2); REV-1630 (2025), Section II", fmt.Sprintf("the %s installment was %v short and bears no interest under %s", l.Due().Long(), l.Shortfall(), s.applied))
		}
	}
	return out, notes, nil
}

func (r pennsylvaniaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r pennsylvaniaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Pennsylvania return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, excusals, err := r.schedule(a, ledger)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).With(excusals).Build()
}

func (r pennsylvaniaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("pennsylvaniataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Pennsylvania return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := asOf
	if y.stop.Before(stop) {
		stop = y.stop
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop = filed
	}
	waiver, _ := f.Positions().Waiver()
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	sched, _, err := r.schedule(a, ledger)
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

	standings := r.standings(a, ledger)
	if !a.taxKnown {
		// REV-1630 charges an installment that misses both exceptions on its
		// shortfall against 90% of this year's tax. Without that tax the
		// penalty has no bound, unless no installment missed them.
		for i, s := range standings {
			if ledger[i].Shortfall() > 0 && !s.excused {
				return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: the %s installment did not reach the %d income at the %d rate, and REV-1630 charges it on its shortfall against 90%% of this year's tax",
					estimated.ErrNeedsThisYearTax, ledger[i].Due().Long(), y.year-1, y.year)
			}
		}
		notes.Rule("72 P.S. § 7352(d)(2)(A); REV-1630 (2025), Section II, Exception 1",
			fmt.Sprintf("this year's tax was not given, but every installment reached the %d income at the %d rate by its date, so Exception 1 excuses it whatever this year's tax is", y.year-1, y.year))
	}
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, sched)
	var unrated estimated.Date
	for i, s := range standings {
		l := ledger[i]
		if l.Shortfall() == 0 || s.excused {
			continue
		}
		lb := estimated.NewPenaltyLine(l.Number(), l.Due(), l.Shortfall())
		lastDay := l.Due()
		for _, piece := range l.Pieces() {
			to := stop
			if on, paid := piece.PaidOn(); paid && on.Before(stop) {
				to = on
			}
			if !to.After(l.Due()) {
				continue
			}
			if to.After(lastDay) {
				lastDay = to
			}
			parts, gap, err := estimated.DailyFactor(piece.Amount(), l.Due(), to, y.daily)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
				unrated = gap
			}
			for _, p := range parts {
				lb.Part(p)
			}
		}
		line, err := lb.Days(lastDay.DaysSince(l.Due())).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		pb.Line(line)
	}
	p, err := pb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	notes.Rule("72 P.S. § 7352(d)(1); REV-1630 (2025), Section III", fmt.Sprintf("each shortfall no exception covers is charged the daily factor for its calendar year, in whole dollars, from its due date until paid or %s; "+
		"the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", unrated.AddDays(-1).Long()))
	}
	if k, asked := f.Positions().Waiver(); asked && k != estimated.WaiverNone {
		notes.Rule("Tax Reform Code § 2706; PA Personal Income Tax Guide (DSM-12)", "Pennsylvania's estimated tax rules have no casualty, retirement or disability waiver; a request to abate the penalty goes to the Board of Appeals")
		if p.Total() > 0 {
			o := estimated.WaiverRefused("Pennsylvania's estimated tax rules have no waiver for a casualty or disaster. They have none for retirement or disability; ask the Board of Appeals to abate.")
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r pennsylvaniaEstimated) Explanation() estimated.Explanation { return pennsylvaniaExplanation }

// pennsylvaniaExplanation is written by hand and checked against
// research/estimated-payments/pa.md.
var pennsylvaniaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Pennsylvania expects estimated payments for 2026 if two things are true. You have $14,000 or more of income with no Pennsylvania tax withheld. You also have $430 or more due after withholding and credits. The penalty is interest on each quarter's shortfall.").
		Paragraph("Withholding, credits and estimated payments must reach a target. That avoids the penalty. The target is the smaller of two figures. One is 90% of this year's tax. The other is last year's Pennsylvania taxable income at this year's 3.07% rate. Last year's Tax Forgiveness Credit comes off that one. Pennsylvania does not use last year's tax itself, and there is no 110% tier.").
		Paragraph("The payments are a quarter each. They're due April 15, June 15, September 15 and the next January 15. Withholding and credits count evenly across the four dates, whenever they came. A final return filed and paid by January 31 counts as the January payment.").
		Paragraph("The penalty is figured against 90% of this year's tax in every quarter. The prior-year figure does not lower that amount. It excuses a quarter outright if payments by its date reach that figure so far. So does paying 90% of the tax on income actually received by each period's end. That helps when income arrives late in the year. The interest is charged by the day, at a rate set once each calendar year. It stops on April 15 of the next year.").
		Paragraph("You may only learn partway through the year that you must pay. The department still expects a quarter for each date. It charges interest on the earlier ones until you catch up. If none were due from you the year before, it excuses the earlier quarters. You must pay the whole catch-up by the first date that applies to you. There's no penalty in the year after one with 100% Tax Forgiveness.").
		VersusFederal("Pennsylvania's rules start from income without withholding, not $1,000 of tax. The prior-year test is last year's income at this year's rate. There's no 110% tier. It and the income-to-date test excuse a quarter rather than lowering it. The income-to-date test uses actual income without annualising. The rate is set for each calendar year. There's no casualty, retirement or disability waiver.").
		Source("Tax Reform Code of 1971 §§ 325, 326, 352(d) (72 P.S. §§ 7325, 7326, 7352(d)); Fiscal Code § 806").
		Source("REV-413 (I), Instructions for Estimating PA Personal Income Tax, 2026").
		Source("REV-1630, Underpayment of Estimated Tax by Individuals, and its instructions, 2025").
		Source("REV-1611, Interest Rate and Calculation Method for Title 72 Taxes (11-25)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()

// targetQuestion is a question whose answer can change the target. It is
// asked only when no exception sets the target.
type targetQuestion struct {
	position         working.PositionKind
	question, effect string
}

type targetQuestions []targetQuestion

func (q *targetQuestions) Ask(position working.PositionKind, question, effect string) {
	*q = append(*q, targetQuestion{position, question, effect})
}

// askUnlessExempt adds the questions to notes unless an exception set the
// target.
func (q targetQuestions) askUnlessExempt(notes *working.Notes, exempt bool) {
	if exempt {
		return
	}
	for _, t := range q {
		notes.Ask(t.position, t.question, t.effect)
	}
}
