package newyorktaxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// New York's estimated tax rules, Tax Law § 685(c) and (d), with the rate
// under § 697(j), as the shared [estimated.EstimatedTax] interface.
//
// One computation covers four taxes: New York State personal income tax, the
// New York City resident tax, the Yonkers tax and the MCTMT on
// self-employment earnings. They are paid on one voucher, Form IT-2105, and
// the penalty for all of them is figured together on Form IT-2105.9. A
// caller passes their total as [estimated.Figures.Tax], with the City and
// Yonkers part as [estimated.Figures.LocalTax], and says which taxes apply in
// [EstimatedFacts].
//
// New York follows the federal rules: four 25% installments, the lesser of
// 90% of this year's tax or 100% of last year's, 110% above $150,000 of last
// year's New York AGI, and payments applied to the earliest installment
// first. It differs in the $300 threshold for each tax, and its own rate: the
// federal short-term rate plus 5½ points, not below 7½%, simple, set each
// quarter.
//
// The figure follows Form IT-2105.9 for 2025 and its instructions, the
// latest published, with the 2026 dates and rates, and the result's
// Divergences say where the statute reads otherwise. No change to § 685(c)
// or (d) was found for 2026 (research/estimated-payments/ny.md), so the form
// does not predate the law it describes.
//
// The rate for the third month after the tax year also covers April 1 to 15.
// The rate from January 1, 2027 is not published, so those days are not
// charged and the penalty reports the gap.
//
// Two rules the form states and the code follows have no source here beyond
// it: the $300 test multiplied by the number of taxes, since the New York
// City and Yonkers laws were not read; and 110% when last year's MCTD
// self-employment earnings were over $150,000, which § 685(c)(3)(C) does not
// state and which comes from the MCTMT article, not read.
//
// Not built: the exception for an installment due on or after the
// taxpayer's death, § 685(d)(3), as [estimated.Facts] carries no date of
// death; the 90-day extension of the first installment after a spouse's
// death; and estates and trusts.

// jurisdiction is New York's postal code.
const jurisdiction = estimated.NewYork

// EstimatedFacts are the facts New York's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	nyc, yonkers, mctmt, subjectAnswered bool

	mctdEarnings optional

	sourceIncome, sourceAnswered bool
	farmerLast, farmerAnswered   bool
}

type optional struct {
	v   money.USCents
	set bool
}

// SubjectTo reports which taxes besides the state's the filer owes this
// year, and whether the caller said. Each of New York State, New York City
// and Yonkers adds $300 to the threshold; the MCTMT adds nothing.
func (e EstimatedFacts) SubjectTo() (newYorkCity, yonkers, mctmt, answered bool) {
	return e.nyc, e.yonkers, e.mctmt, e.subjectAnswered
}

// PriorYearMCTDEarnings is last year's net earnings from self-employment
// allocated to the Metropolitan Commuter Transportation District, which
// raises the prior-year test to 110% above $150,000.
func (e EstimatedFacts) PriorYearMCTDEarnings() (money.USCents, bool) {
	return e.mctdEarnings.v, e.mctdEarnings.set
}

// PriorYearNewYorkSourceIncome reports whether a filer who was not a New
// York resident all of last year had New York source income throughout it,
// which the no-liability exception needs, and whether the caller said.
func (e EstimatedFacts) PriorYearNewYorkSourceIncome() (had, answered bool) {
	return e.sourceIncome, e.sourceAnswered
}

// FarmerLastYear reports whether at least two-thirds of last year's federal
// gross income was from farming or fishing, which puts this year under the
// farmers' rules, and whether the caller said.
func (e EstimatedFacts) FarmerLastYear() (farmer, answered bool) {
	return e.farmerLast, e.farmerAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

// SubjectTo says which of the New York City tax, the Yonkers tax and the
// MCTMT the filer owes this year.
func (b *EstimatedFactsBuilder) SubjectTo(newYorkCity, yonkers, mctmt bool) *EstimatedFactsBuilder {
	b.e.nyc, b.e.yonkers, b.e.mctmt, b.e.subjectAnswered = newYorkCity, yonkers, mctmt, true
	return b
}

func (b *EstimatedFactsBuilder) PriorYearMCTDEarnings(a money.USCents) *EstimatedFactsBuilder {
	b.e.mctdEarnings = optional{a, true}
	return b
}

func (b *EstimatedFactsBuilder) PriorYearNewYorkSourceIncome(had bool) *EstimatedFactsBuilder {
	b.e.sourceIncome, b.e.sourceAnswered = had, true
	return b
}

func (b *EstimatedFactsBuilder) FarmerLastYear(farmer bool) *EstimatedFactsBuilder {
	b.e.farmerLast, b.e.farmerAnswered = farmer, true
	return b
}

// Build refuses negative earnings.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.mctdEarnings.v < 0 {
		return EstimatedFacts{}, errors.New("newyorktaxcalc: MCTD earnings cannot be negative")
	}
	return b.e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is § 685(d)(1) and IT-2105.9 line 15: $300 for each of the
	// state, City and Yonkers taxes that apply.
	threshold money.USCents

	thisYearShare, priorYearShare, priorYearHigh money.Rate
	step, stepSeparate                           money.USCents

	shares     []money.Rate
	annualised []money.Rate
	dues       []estimated.Date
	calendar   estimated.Calendar
	periodEnds []estimated.Date

	// fileAndPayBy is § 685(c)(5)(B).
	fileAndPayBy estimated.Date

	stop estimated.Date

	// rates are the § 697(j) underpayment rate by quarter, split also at
	// each due date, where IT-2105.9 ends a penalty period.
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns New York's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return newYorkEstimated{y}, nil
}

type newYorkEstimated struct{ y estimatedYear }

type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents
	excused      bool
	notes        working.Notes
}

func (r newYorkEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("newyorktaxcalc: the facts carry %T, not newyorktaxcalc.EstimatedFacts", s)
	}
}

func (r newYorkEstimated) check(f estimated.Facts, extras EstimatedFacts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("newyorktaxcalc: these are New York's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("newyorktaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: farmers and fishermen have one installment by January 15 at 66 2/3%% and file federal Form 2210F, which is not built", estimated.ErrUnsupported)
	}
	if farmer, _ := extras.FarmerLastYear(); farmer {
		return fmt.Errorf("%w: two-thirds of last year's federal gross income from farming or fishing puts this year under the farmers' rules, which are not built", estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("newyorktaxcalc: IT-2105.9 Worksheet 1 period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms and the statutes part ways
// on a rule every New York computation uses. They are in
// research/estimated-payments/ny.md and checked there against both.
func (r newYorkEstimated) divergences(t touches) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("January 31, 2027 is a Sunday").
			Form("The instructions print January 31, 2027 and say nothing of moving it. The date is not moved.",
				"IT-2105-I (2026), When to pay your estimated tax").
			Statute("An act due on a weekend or public holiday may be done on the next business day. That would make a return filed and paid on February 1, 2027 timely.",
				"N.Y. Gen. Constr. Law § 25-a(1); Tax Law § 685(c)(5)(B)").
			Uses(working.ReadingForm).Affects(t.filedNextBusinessDay),
		working.NewDivergence("Casualty or disaster").
			Form("The instructions list no waiver for a casualty or disaster. The retirement and disability waiver is listed.",
				"IT-2105.9-I (2025), Exceptions to penalty").
			Statute("The Department can find a penalty against equity and good conscience. That finding must rest on a casualty, disaster or other unusual circumstance. To that extent, no penalty is added.",
				"Tax Law § 685(d)(4)(A)").
			Uses(working.ReadingStatute).Affects(t.casualty),
		working.NewDivergence("A requirement that arises late").
			Form("IT-2105-I covers a filer who first has to pay after April 15. It has them pay 50%, 75% or all of the estimated tax at the first payment. IT-2105.9 charges 25% in every column.",
				"IT-2105-I (2026), worksheet line 28, Computation of installments; IT-2105.9-I (2025), Line 25").
			Statute("Each required installment is 25% of the required annual payment. Only the annualized method lowers one.",
				"Tax Law § 685(c)(3)(A), (4)").
			Uses(working.ReadingForm).Affects(t.lateStart),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// touches are the facts each divergence's readings turn on.
type touches struct {
	filedNextBusinessDay bool
	casualty             bool
	lateStart            bool
}

func (r newYorkEstimated) assess(f estimated.Facts) (assessment, error) {
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	if err := r.check(f, extras); err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	last := y.year - 1

	if share := f.FarmShare() + f.FishShare(); share > 0 {
		if _, answered := extras.FarmerLastYear(); !answered {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Was at least 2/3 of your %d federal gross income from farming or fishing?", last),
				"If so, New York's farmers' rules apply instead.")
		}
	}

	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	var priorFigures estimated.Figures
	var priorTax money.USCents
	if hasPrior {
		priorFigures, _ = prior.Figures()
		priorTax, _ = priorFigures.Tax()
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
		a.notes.Rule("Tax Law § 685(c)(3)(B)(i); IT-2105.9-I (2025), Part 1", fmt.Sprintf("%v of this year's New York State, City, Yonkers and MCTMT tax of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	nyc, yonkers, mctmt, subjectAnswered := extras.SubjectTo()
	step := y.step
	if f.FilingStatus() == filing.MarriedFilingSeparately {
		step = y.stepSeparate
	}
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	// Questions about the target, asked only when no exception sets it.
	var unlessExempt []func()
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d New York return", last))
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d New York return, and what was its tax?", last),
				fmt.Sprintf("With it, 100%% of your %d tax could be a lower target than 90%% of this year's.", last))
		})
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d New York return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d New York return covered %d months, not a full year", last, prior.Months()))
	default:
		agi, agiKnown := priorFigures.StateAGI()
		earnings, earningsKnown := extras.PriorYearMCTDEarnings()
		rate := y.priorYearShare
		var applied string
		switch {
		case !agiKnown:
			rate = y.priorYearHigh
			applied = fmt.Sprintf("%d New York AGI was not given", last)
			unlessExempt = append(unlessExempt, func() {
				a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d New York adjusted gross income?", last),
					fmt.Sprintf("The target is %v of your %d tax until then, or %v at %s or less.", y.priorYearHigh, last, y.priorYearShare, step.Whole()))
			})
		case agi > step:
			rate = y.priorYearHigh
			applied = fmt.Sprintf("%d New York AGI of %v is more than %v", last, agi, step)
		case earningsKnown && earnings > step:
			rate = y.priorYearHigh
			applied = fmt.Sprintf("%d net earnings from self-employment allocated to the MCTD of %v are more than %v", last, earnings, step)
		case mctmt && !earningsKnown:
			rate = y.priorYearHigh
			applied = fmt.Sprintf("%d MCTD earnings were not given", last)
			unlessExempt = append(unlessExempt, func() {
				a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What were your %d self-employment earnings allocated to the MCTD?", last),
					fmt.Sprintf("The MCTD is the Metropolitan Commuter Transportation District. Until then, the target is %v of your %d tax, the rate above %s.", y.priorYearHigh, last, step.Whole()))
			})
		default:
			applied = fmt.Sprintf("%d New York AGI of %v is not more than %v", last, agi, step)
		}
		amount := rate.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, rate, amount)
		a.notes.Rule("Tax Law § 685(c)(3)(B)(ii), (C); IT-2105.9-I (2025), Line 16 worksheet", fmt.Sprintf("%s, so the prior-year test is %v of the %v tax on the %d return, or %v", applied, rate, priorTax, last, amount))
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

	taxes := 1
	if nyc {
		taxes++
	}
	if yonkers {
		taxes++
	}
	threshold := y.threshold * money.USCents(taxes)
	if !taxKnown {
		unlessExempt = append(unlessExempt, func() {
			if priorOpt.Available() {
				a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d New York tax to be?", y.year),
					fmt.Sprintf("90%% of it may be a lower target than your %d tax. It also decides whether the %s threshold excuses you.", last, threshold.Whole()))
			} else {
				a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d New York tax to be?", y.year),
					fmt.Sprintf("Without it there's no target, as there's no full-year %d New York return.", last))
			}
		})
	}

	// § 685(d)(2): no liability last year, twelve months, and residence or
	// New York source income throughout it.
	noLiability := hasPrior && prior.Months() == 12 && ((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	qualifies := false
	if noLiability {
		resident, residenceAnswered := prior.FullYearResident()
		source, sourceAnswered := extras.PriorYearNewYorkSourceIncome()
		switch {
		case residenceAnswered && resident:
			qualifies = true
		case sourceAnswered && source:
			qualifies = true
		case !residenceAnswered || !sourceAnswered:
			a.notes.Ask(working.PositionResidency, fmt.Sprintf("All through %d, did you have New York residency or New York source income?", last),
				fmt.Sprintf("If so, having no New York tax liability for %d means no penalty for %d.", last, y.year))
		}
	}

	switch {
	case qualifies:
		exception, err := estimated.NewException("No New York tax due last year").Build()
		if err != nil {
			return assessment{}, err
		}
		a.exempt = true
		rb.Required(false, fmt.Sprintf("You had no New York tax liability for %d. You were a resident or had New York source income all year. That means no estimated payments are required for %d.", last, y.year)).Exempt(exception)
		a.notes.Rule("Tax Law § 685(d)(2); IT-2105.9-I (2025), Exceptions to penalty (2)", fmt.Sprintf("the %d return showed no liability for a full 12 months, so there is no penalty for %d", last, y.year))
	case taxKnown && tax-withheld < threshold:
		exception, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, threshold)
		if err != nil {
			return assessment{}, err
		}
		a.exempt = true
		text := fmt.Sprintf("Your New York tax of %s less %s withheld %s.", tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
		if tax >= withheld {
			text += fmt.Sprintf(" That is under %s.", threshold.Whole()) +
				fmt.Sprintf(" The threshold is %s for each of the state, City and Yonkers taxes you pay.", y.threshold.Whole())
		}
		text += " No estimated payments are needed."
		rb.Required(false, text).Exempt(exception)
		a.notes.Rule("Tax Law § 685(d)(1); IT-2105.9-I (2025), Line 15 worksheet", fmt.Sprintf("%v of tax less %v withheld is %v, under %v for %d taxes, so there is no penalty", tax, withheld, tax-withheld, threshold, taxes))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Without your %d tax or a full-year %d return, there's no New York target.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s reaches %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. It keeps you clear of a penalty whatever your %d tax turns out to be. After %s of withholding, you need %s of estimated payments. Pay a quarter by each due date.",
			priorOpt.Percent(), last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your New York withholding of %s reaches the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your New York tax of %s less %s withheld leaves %s. That is %s or more, so you need %s of estimated payments. Pay a quarter by each due date.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), threshold.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	if !a.exempt {
		for _, ask := range unlessExempt {
			ask()
		}
	}
	if !a.exempt && !subjectAnswered {
		a.notes.Ask(working.PositionJurisdictionFact, "Which of the New York City and Yonkers taxes do you pay?",
			fmt.Sprintf("Each adds %s to the threshold. Until then, only the state's %s counts.", y.threshold.Whole(), y.threshold.Whole()))
	}

	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("Tax Law § 685(c)(1), (3)(A); IT-2105.9-I (2025), Line 25", fmt.Sprintf("25%% of %v is due on each of %s, %s, %s and %s", a.requirement.Target(),
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() && !a.exempt {
		periods := f.Periods()
		annual := make([]money.USCents, len(periods))
		for i, p := range periods {
			annual[i] = p.AnnualisedTax()
		}
		ann, err := estimated.Annualised(annual, y.annualised, a.installments)
		if err != nil {
			return assessment{}, err
		}
		a.installments = ann
		a.notes.Rule("Tax Law § 685(c)(4); IT-2105.9-I (2025), Worksheets 1 to 4c", fmt.Sprintf("under the annualized method the installments are %v, %v, %v and %v: 22.5%%, 45%%, 67.5%% and 90%% of the annualized tax, "+
			"less earlier installments, and never more than the regular installment plus any reduction carried forward", ann[0], ann[1], ann[2], ann[3]))
	}

	// § 685(c)(5)(B). January 31, 2027 is a Sunday; IT-2105-I prints the date
	// and says nothing of moving it, so it is not moved.
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(y.fileAndPayBy) && !a.exempt {
		a.excused = true
		a.notes.Rule("Tax Law § 685(c)(5)(B); IT-2105-I (2026)", fmt.Sprintf("the return was filed and paid on %s, by %s, so the January installment bears no penalty", filed.Long(), y.fileAndPayBy.Long()))
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	var t touches
	if !a.exempt {
		filed := f.ReturnFiled()
		t.filedNextBusinessDay = !filed.IsZero() && filed.After(y.fileAndPayBy) && !filed.After(y.calendar.NextBusinessDay(y.fileAndPayBy))
		waiver, _ := f.Positions().Waiver()
		t.casualty = waiver == estimated.WaiverCasualty
		met := f.RequirementFirstMet()
		t.lateStart = !met.IsZero() && met.After(a.dues[0])
	}
	divergences, err := r.divergences(t)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

func (r newYorkEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r newYorkEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, r.y.shares)
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
	}
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
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("Tax Law § 685(c)(5)(A)", fmt.Sprintf("as shown, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("Tax Law § 685(c)(1), (5)(A); IT-2105.9-I (2025), Line 26", fmt.Sprintf("the %v withheld is treated as paid in four equal parts on the due dates, and a payment goes first to the earliest installment", f.WithheldTotal()))
	}
	return ledger, notes, nil
}

func (r newYorkEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d New York return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := r.schedule(a, ledger)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments the ledger gives.
func (r newYorkEstimated) schedule(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
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

func (r newYorkEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("newyorktaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d New York return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := asOf
	if y.stop.Before(stop) {
		stop = y.stop
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop = filed
	}
	waiver, waiverAnswered := f.Positions().Waiver()
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule, err := r.schedule(a, ledger)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if a.exempt {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		return working.NewResult(p).With(a.notes).Build()
	}

	// IT-2105.9 Part 3: each part of each shortfall, by the day ÷ 365, at
	// each period's rate, each period ending at the payment, the end of the
	// rate period or the next due date.
	p, unrated, err := estimated.SimplePenalty(ledger, stop, y.rates, waiver, a.requirement, schedule)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("Tax Law § 685(c)(3)(B)", fmt.Sprintf("this year's New York tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	notes.Rule("Tax Law §§ 685(c)(1), 697(j); IT-2105.9-I (2025), Period of underpayment and Penalty rate", fmt.Sprintf("each part of each shortfall is charged the quarter's rate, simple, by the day ÷ 365, from its due date until paid or %s; "+
		"the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", unrated.AddDays(-1).Long()))
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking New York to waive the penalty, shown in full until you say?",
			fmt.Sprintf("A casualty, disaster or other unusual circumstance can get it waived. So can retiring after 62 or disability in %d or %d, with reasonable cause.", y.year-1, y.year))
	}
	// Tax Law § 685(d)(4) grants the federal pair of waivers.
	if o, ok := estimated.StandardWaiver(f, "the New York Tax Department", 62); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r newYorkEstimated) Explanation() estimated.Explanation { return newYorkExplanation }

// newYorkExplanation is written by hand and checked against
// research/estimated-payments/ny.md.
var newYorkExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"New York expects estimated payments once tax after withholding is $300 or more. That is your state, New York City or Yonkers tax. It also expects them if you have any MCTMT. All four taxes go on one voucher, and the penalty is figured on them together.").
		Paragraph("The target is the smaller of 90% of this year's total or 100% of last year's. You are clear if withholding and estimated payments for all four taxes reach it. A quarter is due by each of April 15, June 15, September 15 and January 15. As federally, last year's figure can become 110%. That happens if last year's New York AGI was over $150,000. The step is $75,000 if married filing separately. New York adds the same step for MCTD self-employment earnings. Last year's figure needs a full-year return.").
		Paragraph("There is no penalty if your tax due after withholding is under $300 a tax. That is $300 for each of the state, City and Yonkers taxes you pay. Nor is there one for a New York resident all last year with no New York tax due. Filing and paying in full by January 31 clears the January payment's penalty.").
		Paragraph("The penalty is simple interest on each shortfall for the days it stays unpaid. It stops on April 15 of the next year. Each payment goes first to the oldest shortfall. New York sets its own rate each quarter. It is the federal short-term rate plus 5½ points, never below 7½%. For 2026 that was 8.5% from April to June and 9.5% from July. The Department can waive the penalty for a casualty. It can also waive it for retiring after 62 or disability with reasonable cause.").
		VersusFederal("New York's threshold is $300 for each tax rather than $1,000. The City, Yonkers and MCTMT taxes are added in and figured together. MCTD self-employment earnings over $150,000 can trigger 110% on their own. New York uses its own rate, not the federal underpayment rate. It is the federal short-term rate plus 5½ points, with a 7½% floor.").
		Source("N.Y. Tax Law §§ 685(c), (d), 697(j); N.Y. Gen. Constr. Law § 25-a").
		Source("IT-2105-I (2026), Instructions for Form IT-2105, Estimated Tax Payment Voucher for Individuals").
		Source("IT-2105.9-I (2025), Instructions for Form IT-2105.9, Underpayment of Estimated Tax").
		Source("Department of Taxation and Finance, Interest rates for each quarter of 2026").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
