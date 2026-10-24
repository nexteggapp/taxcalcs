package arizonataxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Arizona's estimated tax rules, A.R.S. § 43-581, with the penalty of
// § 42-1125(Q) and the interest of § 42-1123, as the shared
// [estimated.EstimatedTax] interface.
//
// § 43-581(D) asks the department for rules "similar to" the Internal Revenue
// Code, and the forms follow 26 U.S.C. § 6654 closely: four equal
// installments on the federal dates, withholding deemed paid evenly, and the
// same annualised method. Nothing here imports the federal package; what
// Arizona shares with § 6654 is written again with Arizona's citations.
//
// The figure follows the department's forms, the 2026 Form 140ES and the 2025
// Form 221, and the result's Divergences say where the statutes read
// otherwise. The research found no act since 2021 changing § 43-581
// (research/estimated-payments/az.md). Form 221's 2025 instructions say the
// thresholds "were amended for tax year 2025"; the 2026 Form 140ES prints the
// statute's current $75,000 and $150,000 for both years, so no form used here
// predates the law it describes.

const jurisdiction = estimated.Arizona

// EstimatedFacts are the facts Arizona's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
//
// The first two are federal figures. § 43-581(E)(1) takes the filer out of
// Arizona's penalty when a federal exception under 26 U.S.C. § 6654 applies,
// and Form 221 line 1 names two: no federal tax liability last year, and
// federal tax less federal withholding under $1,000 this year.
type EstimatedFacts struct {
	federalTaxLessWithholding optional
	noFederalLiability        bool
	noFederalAnswered         bool
	priorRequired             bool
	priorRequiredAnswered     bool
	nonresidentAlien          bool
}

type optional struct {
	v   money.USCents
	set bool
}

// FederalTaxLessWithholding is the total tax on this year's federal return
// minus federal income tax withheld, as Form 221's instructions word the
// exception, and whether the caller gave it. It can be negative.
func (e EstimatedFacts) FederalTaxLessWithholding() (money.USCents, bool) {
	return e.federalTaxLessWithholding.v, e.federalTaxLessWithholding.set
}

// NoFederalLiabilityLastYear reports that the filer had no federal tax
// liability last year, was a U.S. citizen or resident for all of it, and
// filed (or would have filed) a return for a full 12 months, and whether the
// caller said.
func (e EstimatedFacts) NoFederalLiabilityLastYear() (none, answered bool) {
	return e.noFederalLiability, e.noFederalAnswered
}

// PriorYearReturnRequired reports whether the filer was required to file
// last year's Arizona return, and whether the caller said. The 2026 Form
// 140ES offers last year's tax as the target only to a filer who was required
// to file and did.
func (e EstimatedFacts) PriorYearReturnRequired() (required, answered bool) {
	return e.priorRequired, e.priorRequiredAnswered
}

// NonresidentAlien reports a filer who elects to be treated as a nonresident
// alien on the federal return, who makes three payments under rules of their
// own.
func (e EstimatedFacts) NonresidentAlien() bool { return e.nonresidentAlien }

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) FederalTaxLessWithholding(a money.USCents) *EstimatedFactsBuilder {
	b.e.federalTaxLessWithholding = optional{a, true}
	return b
}

func (b *EstimatedFactsBuilder) NoFederalLiabilityLastYear(none bool) *EstimatedFactsBuilder {
	b.e.noFederalLiability, b.e.noFederalAnswered = none, true
	return b
}

func (b *EstimatedFactsBuilder) PriorYearReturnRequired(required bool) *EstimatedFactsBuilder {
	b.e.priorRequired, b.e.priorRequiredAnswered = required, true
	return b
}

// NonresidentAlien marks a filer who elects nonresident alien treatment.
func (b *EstimatedFactsBuilder) NonresidentAlien() *EstimatedFactsBuilder {
	b.e.nonresidentAlien = true
	return b
}

// Build accepts any combination. Federal tax less withholding can be negative
// when more was withheld than owed.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// incomeThreshold and jointThreshold are the Arizona gross income that
	// must be exceeded this year and last before payments are required,
	// § 43-581(A).
	incomeThreshold, jointThreshold money.USCents

	// taxThreshold is § 43-581(E)(2): no penalty when tax less withholding
	// and credits is under it. federalThreshold is the federal exception Form
	// 221 line 1 names.
	taxThreshold, federalThreshold money.USCents

	// thisYearPercent and priorYearPercent are § 43-581(A)'s 90% and 100%.
	thisYearPercent, priorYearPercent money.Rate

	// penaltyCap is § 42-1125(Q): the penalty on an underpayment never
	// exceeds this share of it.
	penaltyCap money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// fileAndPayBy is the date the 140ES prints for filing and paying in full
	// instead of the fourth installment, already moved off a weekend.
	fileAndPayBy estimated.Date

	// stop is the last day Form 221 charges.
	stop estimated.Date

	periodEnds            []estimated.Date
	annualisedPercentages []money.Rate

	// rates is the § 42-1123 rate by Form 221 rate period, a calendar quarter
	// each, as far as the department has published it.
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are Form 221 line 11, the required annual payment divided by four,
// and its instructions for line 12, which deem a quarter of the withholding
// paid on each due date.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Arizona's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return arizonaEstimated{y}, nil
}

type arizonaEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement

	// exempt means no penalty can arise, so nothing is needed by any date.
	exempt bool

	// taxKnown is whether this year's tax was given. Without it the target is
	// last year's tax alone, and the penalty is an upper bound.
	taxKnown bool

	dues         []estimated.Date
	installments []money.USCents // each installment's own amount, before excusing
	excused      bool            // the January installment, for filing early

	notes working.Notes
}

func (r arizonaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("arizonataxcalc: the facts carry %T, not arizonataxcalc.EstimatedFacts", s)
	}
}

func (r arizonaEstimated) check(f estimated.Facts, e EstimatedFacts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("arizonataxcalc: these are Arizona's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("arizonataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: a farmer or fisherman for federal purposes makes one Arizona installment, due January 15, which is not built",
			estimated.ErrUnsupported)
	case e.NonresidentAlien():
		return fmt.Errorf("%w: a nonresident alien makes three Arizona installments, with 50%% in the first, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("arizonataxcalc: Form 221 annualisation period %d ends on %v, not %v", i+1, p.End(), r.y.periodEnds[i])
			}
		}
	}
	return nil
}

// affected says which divergences change these facts' figures or answers.
type affected struct {
	boundary, priorReturn, stop, rates bool
}

// divergences are where the department's forms and the Arizona Revised
// Statutes part ways on a rule every Arizona computation uses.
func (r arizonaEstimated) divergences(in affected) ([]working.Divergence, error) {
	y := r.y
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Arizona gross income of exactly the threshold").
			Form(fmt.Sprintf("Form 221's instructions excuse the penalty only below the threshold. That's Arizona gross income under it in either year. So exactly %s (%s joint) would be subject to it. "+
				"The 2026 Form 140ES, and Form 221's own table, require more than the threshold.", y.incomeThreshold.Whole(), y.jointThreshold.Whole()),
				"Form 221 (2025) instructions, Am I Subject to the Penalty?; 2026 Form 140ES instructions, Required Payments").
			Statute(fmt.Sprintf("Payments are required only if Arizona gross income exceeds %s this year. It must also have been greater than that last year. The joint figure is %s, and exactly %s isn't subject.",
				y.incomeThreshold.Whole(), y.jointThreshold.Whole(), y.incomeThreshold.Whole()),
				"A.R.S. § 43-581(A)").
			Uses(working.ReadingStatute).Affects(in.boundary),
		working.NewDivergence("The prior-year option needs a filed return").
			Form("The 2026 Form 140ES offers 100% of last year's tax under two conditions. You must have been required to file a 2025 Arizona return, and filed it. Form 221 (2025) line 8 offers it only if you filed. "+
				"Neither asks for a 12-month return.",
				"2026 Form 140ES instructions, How Much Should My Estimated Payments Total?, and worksheet line 23; Form 221 (2025) instructions, line 8").
			Statute("Payments must reach 100% of the tax for the year before. There's no condition that a return was required, filed or covered 12 months. "+
				"The law asks the department for rules similar to the federal ones.",
				"A.R.S. § 43-581(A), (D)").
			Uses(working.ReadingForm).Affects(in.priorReturn),
		working.NewDivergence("When the penalty stops").
			Form("Each shortfall is charged until it's paid or April 15 of the next year. The charge stops at whichever comes first. A balance paid with a late return counts as paid on April 15.",
				"Form 221 (2025), Part B lines 26 and 27, and its instructions for Rate Periods 1 to 4").
			Statute(fmt.Sprintf("The penalty is the interest the unpaid amount would build up while unpaid. It can't exceed %v of that amount, and it has no April 15 end.", y.penaltyCap),
				"A.R.S. § 42-1125(Q)").
			Uses(working.ReadingForm).Affects(in.stop),
		working.NewDivergence("The 2026 rates").
			Form("Form 221 (2025) prints 7% for each of its 2025 rate periods. It leaves Jan 1 to Apr 15, 2026 to be set later. "+
				"For 2026 the department publishes 6% from April 1 to June 30. It publishes 7% from July 1 to December 31.",
				"Form 221 (2025), Part B lines 16 to 27").
			Statute("The rate is the federal short-term rate plus three percentage points. It's applied as the federal rate is, so it's set for each quarter.",
				"A.R.S. § 42-1123(A); ADOR, Interest Rates").
			Uses(working.ReadingStatute).Affects(in.rates),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// grossIncome is a year's Arizona gross income: [estimated.Figures.GrossIncome],
// or federal AGI when only that was given, which fromAGI reports.
func grossIncome(fig estimated.Figures) (v money.USCents, known, fromAGI bool) {
	if gi, ok := fig.GrossIncome(); ok {
		return gi, true, false
	}
	if agi, ok := fig.FederalAGI(); ok {
		return agi, true, true
	}
	return 0, false, false
}

func (r arizonaEstimated) assess(f estimated.Facts) (assessment, error) {
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	if err := r.check(f, extras); err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	var targetQs targetQuestions
	var in affected

	last := y.year - 1
	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	var priorFigures estimated.Figures
	if hasPrior {
		priorFigures, _ = prior.Figures()
	}
	threshold := y.incomeThreshold
	if f.FilingStatus() == filing.MarriedFilingJointly {
		threshold = y.jointThreshold
	}

	// 90% of this year's tax after credits: Form 221 lines 4 and 7.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, y.thisYearPercent.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("A.R.S. § 43-581(A); Form 221 (2025), lines 4 and 7", fmt.Sprintf("%v of this year's Arizona tax after credits of %v is %v", y.thisYearPercent, tax, thisYear.Amount()))
	}

	// 100% of last year's tax after credits: Form 221 line 8, the 140ES
	// worksheet line 23. There is no 110% tier.
	requiredToFile, requiredAnswered := extras.PriorYearReturnRequired()
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearPercent, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Arizona return", last))
		targetQs.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Arizona return, and what was its tax?", last),
			fmt.Sprintf("With it, %v of last year's tax could be a target below %v of this year's.", y.priorYearPercent, y.thisYearPercent))
	case prior.NotRequiredToFile():
		in.priorReturn = true
		priorOption = unavailable(fmt.Sprintf("You weren't required to file a %d Arizona return", last))
	case !prior.Filed():
		in.priorReturn = true
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Arizona return", last))
	case requiredAnswered && !requiredToFile:
		in.priorReturn = true
		priorOption = unavailable(fmt.Sprintf("You weren't required to file your %d Arizona return", last))
	default:
		priorTax, _ := priorFigures.Tax()
		amount := y.priorYearPercent.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearPercent, amount)
		a.notes.Rule("A.R.S. § 43-581(A); 2026 Form 140ES worksheet, line 23; Form 221 (2025), line 8", fmt.Sprintf("%v of the %v tax after credits on the %d return is %v; Arizona has no higher percentage at higher income",
			y.priorYearPercent, priorTax, last, amount))
		if prior.Months() != 12 {
			targetQs.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("The federal test needs a 12-month return, and Arizona's law and forms don't. So the figures let your %d return of %d months set the target.",
				last, prior.Months()))
		}
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
		effect := "Without it, the target can only come from last year's tax."
		if !priorOpt.Available() {
			effect = fmt.Sprintf("Without it, there's no target, because your %d return can't set one.", last)
		}
		targetQs.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Arizona tax to be?", y.year), effect)
	}

	// The income test, in both years.
	thisGI, thisGIKnown, thisFromAGI := grossIncome(f.ThisYear())
	priorGI, priorGIKnown, priorFromAGI := grossIncome(priorFigures)
	measured := func(year int, fromAGI bool) string {
		if fromAGI {
			return fmt.Sprintf("%d federal AGI, taken as Arizona gross income,", year)
		}
		return fmt.Sprintf("%d Arizona gross income", year)
	}
	noFederal, noFederalAnswered := extras.NoFederalLiabilityLastYear()
	federal, federalKnown := extras.FederalTaxLessWithholding()

	in.boundary = (thisGIKnown && thisGI == threshold) || (priorGIKnown && priorGI == threshold)
	var exception estimated.TargetOption
	switch {
	case thisGIKnown && thisGI <= threshold:
		a.exempt = true
		if exception, err = estimated.ExceptionAtMost(fmt.Sprintf("%d Arizona gross income", y.year), thisGI, threshold); err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your %d Arizona gross income of %s isn't more than %s. Arizona doesn't require estimated payments for %d and charges no penalty.",
			y.year, thisGI.Whole(), threshold.Whole(), y.year)).Exempt(exception)
		a.notes.Rule("A.R.S. § 43-581(A); 2026 Form 140ES instructions, Required Payments", fmt.Sprintf("the %s %v does not exceed %v, so no estimated payments are required", measured(y.year, thisFromAGI), thisGI, threshold))
	case priorGIKnown && priorGI <= threshold:
		a.exempt = true
		if exception, err = estimated.ExceptionAtMost(fmt.Sprintf("%d Arizona gross income", last), priorGI, threshold); err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your %d Arizona gross income of %s wasn't more than %s. Arizona doesn't require estimated payments for %d and charges no penalty.",
			last, priorGI.Whole(), threshold.Whole(), y.year)).Exempt(exception)
		a.notes.Rule("A.R.S. § 43-581(A); 2026 Form 140ES instructions, Required Payments", fmt.Sprintf("the %s %v was not greater than %v, so no estimated payments are required for %d", measured(last, priorFromAGI), priorGI, threshold, y.year))
	case noFederal:
		a.exempt = true
		if exception, err = estimated.NewException("No federal tax liability last year").Build(); err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no federal tax liability for %d. That was a full year as a U.S. citizen or resident. Arizona follows that federal exception, so no penalty can arise for %d.", last, y.year)).Exempt(exception)
		a.notes.Rule("A.R.S. § 43-581(E)(1); 26 U.S.C. § 6654(e)(2); Form 221 (2025), line 1", fmt.Sprintf("no federal tax liability for %d on a 12-month return is a federal exception, so there is no Arizona penalty for %d", last, y.year))
	case federalKnown && federal < y.federalThreshold:
		a.exempt = true
		if exception, err = estimated.ExceptionBelow("Federal tax less federal withholding", federal, y.federalThreshold); err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your %d federal tax less federal withholding is %s, under %s. Arizona follows that federal exception, so no Arizona penalty can arise.",
			y.year, federal.Whole(), y.federalThreshold.Whole())).Exempt(exception)
		a.notes.Rule("A.R.S. § 43-581(E)(1); 26 U.S.C. § 6654(e)(1); Form 221 (2025), line 1", fmt.Sprintf("federal tax less federal withholding of %v is under %v, a federal exception, so there is no Arizona penalty", federal, y.federalThreshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target needs your %d Arizona tax, because your %d return can't set one.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s reaches %v of your %d Arizona tax, %s. No estimated payments are needed, whatever your %d tax is.",
			withheld.Whole(), y.priorYearPercent, last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d Arizona tax, %s. That keeps you clear of a penalty whatever your %d tax turns out to be. After %s of withholding, that's %s of estimated payments. Pay a quarter by each date.",
			y.priorYearPercent, last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case tax-withheld < y.taxThreshold:
		a.exempt = true
		if exception, err = estimated.ExceptionBelow("Tax after credits less withholding", tax-withheld, y.taxThreshold); err != nil {
			return assessment{}, err
		}
		under := ", which is under " + y.taxThreshold.Whole()
		if tax < withheld {
			under = ""
		}
		rb.Required(false, fmt.Sprintf("Your Arizona tax after credits is %s, and %s was withheld. That %s%s. No estimated payments are needed.",
			tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld), under)).Exempt(exception)
		a.notes.Rule("A.R.S. § 43-581(E)(2); Form 221 (2025), line 6", fmt.Sprintf("%v of tax after credits less %v withheld is %v, under %v, so there is no penalty", tax, withheld, tax-withheld, y.taxThreshold))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Arizona withholding of %s covers the %s required for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		why := fmt.Sprintf("Your Arizona tax of %s less %s withheld leaves %s. That's %s or more, and withholding falls short of the %s required. You need %s of estimated payments, a quarter by each date.",
			tax.Whole(), withheld.Whole(), (tax - withheld).Whole(), y.taxThreshold.Whole(), target.Whole(), (target - withheld).Whole())
		if thisGIKnown && priorGIKnown {
			why = fmt.Sprintf("Your Arizona gross income is over %s in both years. ", threshold.Whole()) + why
		}
		rb.Required(true, why).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	targetQs.askUnlessExempt(&a.notes, a.exempt)

	if !a.exempt {
		if thisGIKnown && priorGIKnown {
			a.notes.Rule("A.R.S. § 43-581(A); 2026 Form 140ES instructions, Required Payments", fmt.Sprintf("the %s %v exceeds %v and the %s %v was greater than it, so estimated payments are required",
				measured(y.year, thisFromAGI), thisGI, threshold, measured(last, priorFromAGI), priorGI))
		}
		for _, g := range []struct {
			year           int
			known, fromAGI bool
		}{{y.year, thisGIKnown, thisFromAGI}, {last, priorGIKnown, priorFromAGI}} {
			switch {
			case !g.known:
				a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d Arizona gross income?", g.year),
					fmt.Sprintf("Until then, the figures take it as over %s. Payments are needed only when it's over %s in both %d and %d.", threshold.Whole(), threshold.Whole(), last, y.year))
			case g.fromAGI:
				a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d Arizona gross income?", g.year),
					"Until then, the figures use your federal AGI. That's your Arizona gross income if you lived in Arizona all year.")
			}
		}
		if !noFederalAnswered {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did you have no federal tax liability for %d?", last),
				"That needs a full-year return as a U.S. citizen or resident. If so, there's no Arizona penalty.")
		}
		if !federalKnown {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What is your %d federal tax less federal withholding?", y.year),
				fmt.Sprintf("If it's under %s, there's no Arizona penalty.", y.federalThreshold.Whole()))
		}
		if c, ok := a.requirement.Chosen(); ok && c.Basis() == estimated.BasisPriorYearTax && !requiredAnswered {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Were you required to file your %d Arizona return?", last),
				fmt.Sprintf("If not, the %d Form 140ES doesn't offer %v of last year's tax as a target.", y.year, y.priorYearPercent))
		}
	}

	// The due dates are the federal ones, § 43-581(A), moved off a weekend or
	// legal holiday as the 140ES says.
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			a.notes.Rule("2026 Form 140ES", fmt.Sprintf("the installment due %s falls on a weekend or legal holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
		}
	}
	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), quarters)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("A.R.S. § 43-581(A); 2026 Form 140ES instructions; Form 221 (2025), line 11", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", a.requirement.Target(), a.installments[0],
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() && !a.exempt {
		periods := f.Periods()
		taxes := make([]money.USCents, len(periods))
		for i, p := range periods {
			taxes[i] = p.AnnualisedTax()
		}
		ann, err := estimated.Annualised(taxes, y.annualisedPercentages, a.installments)
		if err != nil {
			return assessment{}, err
		}
		a.installments = ann
		p := y.annualisedPercentages
		a.notes.Rule("A.R.S. § 43-581(D); Form 221 (2025), Annualized Income Installment Worksheet", fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: %v, %v, %v and %v of the annualised tax, "+
			"less earlier installments, and never more than the regular installment plus any shortfall the method carried forward", ann[0], ann[1], ann[2], ann[3], p[0], p[1], p[2], p[3]))
	}

	// Filing and paying in full by the date the 140ES prints excuses the
	// fourth installment.
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(y.fileAndPayBy) && !a.exempt {
		a.excused = true
		a.notes.Rule("2026 Form 140ES instructions, item 1; Form 221 (2025) instructions, line 11", fmt.Sprintf("the return was filed and paid on %s, by %s, so the fourth installment is excused", filed.Long(), y.fileAndPayBy.Long()))
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	// Which divergences change these figures turns on the shortfalls.
	in.priorReturn = in.priorReturn && !a.exempt
	if !a.exempt {
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return assessment{}, err
		}
		in.stop, in.rates = r.shortfallsMatter(f, ledger)
	}
	divergences, err := r.divergences(in)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

// shortfallsMatter reports whether a shortfall is still unpaid after the
// form's April 15 stop, and whether one accrues in 2026, whose rates no
// Form 221 prints.
func (r arizonaEstimated) shortfallsMatter(f estimated.Facts, ledger []estimated.LedgerEntry) (pastStop, in2026 bool) {
	filed := f.ReturnFiled()
	paidByStop := !filed.IsZero() && !filed.After(r.y.stop)
	yearEnd := estimated.MustDate(r.y.year, 12, 31)
	for _, l := range ledger {
		if l.Shortfall() <= 0 {
			continue
		}
		if l.Due().Before(yearEnd) {
			in2026 = true
		}
		for _, p := range l.Pieces() {
			if on, paid := p.PaidOn(); !paidByStop && (!paid || on.After(r.y.stop)) {
				pastStop = true
			}
		}
	}
	return pastStop, in2026
}

func (r arizonaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies the credits to the installments, with the fourth set to
// nothing when filing early excused it.
func (r arizonaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, quarters)
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
		notes.Rule("Form 221 (2025) instructions, line 12", fmt.Sprintf("the %v withheld is credited on the dates it was withheld, as the instructions allow a filer who can show it", f.WithheldTotal()))
	} else {
		notes.Rule("Form 221 (2025) instructions, line 12", fmt.Sprintf("the %v withheld is treated as paid a quarter on each due date, whenever it was actually withheld", f.WithheldTotal()))
	}
	return ledger, notes, nil
}

func (r arizonaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Arizona return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := r.installments(a, ledger)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// installments are the schedule the ledger gives, with January excused when
// the return was filed and paid early.
func (r arizonaEstimated) installments(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
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

func (r arizonaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("arizonataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Arizona return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule, err := r.installments(a, ledger)
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

	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	var unrated estimated.Date
	for _, l := range ledger {
		if l.Shortfall() <= 0 {
			continue
		}
		line, gap, err := r.line(l, stop, &notes)
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
			unrated = gap
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
		notes.Rule("A.R.S. § 43-581(A); Form 221 (2025), line 9", fmt.Sprintf("this year's Arizona tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and %v of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			y.priorYearPercent, y.year-1, y.thisYearPercent, p.Total()))
	}
	if paidInFull {
		notes.Rule("Form 221 (2025) instructions, Rate Period 4", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("A.R.S. §§ 42-1123, 42-1125(Q); Form 221 (2025), Part B", fmt.Sprintf("each shortfall is charged the department's rate for each quarter, by the day over 365, from its due date until paid or %s, "+
		"with each payment applied to the oldest shortfall first; the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if p.Total() > 0 {
		notes.Rule("Form 221 (2025) instructions, Box 30c", fmt.Sprintf("the form rounds the total penalty to the nearest whole dollar, %v", estimated.RoundToDollar.Round(p.Total())))
	}
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", shortDate(unrated.AddDays(-1), 0)))
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking for the penalty to be waived?",
			"The department can waive it for a casualty, disaster or other unusual event. Retiring after 62 or becoming disabled also counts, with reasonable cause.")
	}
	if o, ok := estimated.StandardWaiver(f, "the Arizona Department of Revenue", 62); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// line is the penalty on one installment's shortfall, as a column of Form
// 221 Part B computes it.
//
// Each piece of the shortfall is charged simple interest by the day from the
// due date until it was paid or stop, one part for each quarter's rate period.
// If any of it is still unpaid on January 1, the penalty accrued on the
// installment through December 31 is added to its principal, as line 27 and
// A.R.S. § 42-1123(B) do, and charged from then until the last of the
// shortfall is paid. The line is then capped at the share of the shortfall
// § 42-1125(Q) allows, line 29, by cutting the charge where the cap is
// reached.
func (r arizonaEstimated) line(l estimated.LedgerEntry, stop estimated.Date, notes *working.Notes) (estimated.PenaltyLine, estimated.Date, error) {
	y := r.y
	var unrated estimated.Date
	gapAt := func(g estimated.Date) {
		if !g.IsZero() && (unrated.IsZero() || g.Before(unrated)) {
			unrated = g
		}
	}
	var parts []estimated.PenaltyPart
	last := l.Due()
	for _, piece := range l.Pieces() {
		to := stop
		if on, paid := piece.PaidOn(); paid && on.Before(stop) {
			to = on
		}
		if !to.After(l.Due()) {
			continue
		}
		if to.After(last) {
			last = to
		}
		ps, gap, err := estimated.SimpleDaily(piece.Amount(), l.Due(), to, y.rates)
		if err != nil {
			return estimated.PenaltyLine{}, estimated.Date{}, err
		}
		gapAt(gap)
		parts = append(parts, ps...)
	}

	yearEnd := estimated.MustDate(y.year, 12, 31)
	if last.After(yearEnd) {
		var accrued money.USCents
		for _, p := range parts {
			if !p.To().After(yearEnd) {
				accrued += p.Charge()
			}
		}
		if accrued > 0 {
			ps, gap, err := estimated.SimpleDaily(accrued, yearEnd, last, y.rates)
			if err != nil {
				return estimated.PenaltyLine{}, estimated.Date{}, err
			}
			gapAt(gap)
			parts = append(parts, ps...)
			notes.Rule("A.R.S. § 42-1123(B); Form 221 (2025), line 27", fmt.Sprintf("the %v of penalty on the %s installment outstanding on %s is added to its principal and charged until %s",
				accrued, l.Due().Long(), yearEnd.AddDays(1).Long(), last.Long()))
		}
	}

	limit := y.penaltyCap.Of(l.Shortfall())
	parts, capped, err := capParts(parts, limit)
	if err != nil {
		return estimated.PenaltyLine{}, estimated.Date{}, err
	}
	lb := estimated.NewPenaltyLine(l.Number(), l.Due(), l.Shortfall()).Days(last.DaysSince(l.Due()))
	for _, p := range parts {
		lb.Part(p)
	}
	line, err := lb.Build()
	if err != nil {
		return estimated.PenaltyLine{}, estimated.Date{}, err
	}
	how := "under"
	if capped {
		how = "held to"
	}
	notes.Rule("A.R.S. § 42-1125(Q); Form 221 (2025), line 29", fmt.Sprintf("the penalty on the %s installment's %v shortfall is %v, %s the cap of %v of it, %v",
		l.Due().Long(), l.Shortfall(), line.Total(), how, y.penaltyCap, limit))
	return line, unrated, nil
}

// capParts cuts the charges so that together they come to no more than
// limit, keeping the earlier parts whole. It reports whether any was cut.
func capParts(parts []estimated.PenaltyPart, limit money.USCents) ([]estimated.PenaltyPart, bool, error) {
	out := make([]estimated.PenaltyPart, 0, len(parts))
	var sum money.USCents
	capped := false
	for _, p := range parts {
		charge := p.Charge()
		if sum+charge > limit {
			charge, capped = max(0, limit-sum), true
		}
		sum += charge
		if charge == p.Charge() {
			out = append(out, p)
			continue
		}
		c, err := estimated.NewPenaltyPart(p.Kind(), p.Principal(), p.Rate()).Span(p.From(), p.To()).Days(p.Days()).
			Charge(charge).Source(p.Source()).Build()
		if err != nil {
			return nil, false, err
		}
		out = append(out, c)
	}
	return out, capped, nil
}

// shortDate is "Aug 31" inside the tax year and "Jan 15, 2027" outside it.
func shortDate(d estimated.Date, year int) string {
	m := d.Month().String()[:3]
	if d.Year() == year {
		return fmt.Sprintf("%s %d", m, d.Day())
	}
	return fmt.Sprintf("%s %d, %d", m, d.Day(), d.Year())
}

func (r arizonaEstimated) Explanation() estimated.Explanation { return arizonaExplanation }

// arizonaExplanation is written by hand and checked against
// research/estimated-payments/az.md.
var arizonaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Arizona asks for estimated payments only above a gross income threshold. Your Arizona gross income must be over $75,000 both last year and this year. On a joint return the threshold is $150,000. The penalty is interest on each quarter's shortfall. It can never be more than 10% of the shortfall.").
		Paragraph("Your Arizona gross income may be at or under the threshold in either year. Then Arizona doesn't expect estimated payments and charges no penalty. That holds however much tax is due. There's also no penalty for a small balance. That's Arizona tax after credits, less Arizona withholding, under $1,000.").
		Paragraph("If you do have to pay, withholding and estimated payments must reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. There is no 110% rule for higher incomes. The department's 2026 instructions limit last year's figure. You must have been required to file a 2025 Arizona return, and filed it. Federal relief carries over. With no federal tax liability last year, there's no Arizona penalty either. Federal tax less federal withholding under $1,000 this year does the same.").
		Paragraph("The payments are a quarter each. They're due April 15, June 15 and September 15, 2026, and January 15, 2027. A date on a weekend or legal holiday shifts to the next business day. Withholding counts as paid evenly on those dates. You can instead show when it was actually taken. You can file your 2026 return and pay in full by February 1, 2027. That replaces the January payment.").
		Paragraph("The penalty is figured like interest. The rate is the federal short-term rate plus three points. That's 6% for April to June 2026 and 7% from July. Each shortfall is charged by the day until paid. The department's form stops counting on April 15 of the next year. Penalty still unpaid on January 1 is added to the amount it's charged on. Form 221's annualised worksheet can lower early payments for uneven income. The department can waive the penalty for a casualty or disaster. Retiring after 62 or becoming disabled also counts, with reasonable cause.").
		VersusFederal("Arizona differs from federal where a number can change. Whether you must pay turns on Arizona gross income in both years. The line is over $75,000, or $150,000 joint. There's no 110% tier. The penalty can never exceed 10% of the shortfall. Penalty unpaid on January 1 is itself charged. A federal exception removes the Arizona penalty too.").
		Source("A.R.S. §§ 43-581, 42-1123, 42-1125(Q)").
		Source("Arizona Form 140ES and its instructions, 2026").
		Source("Arizona Form 221, Underpayment of Estimated Tax by Individuals, and its instructions, 2025").
		Source("ADOR, Interest Rates").
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
