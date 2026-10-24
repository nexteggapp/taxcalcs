package alabamataxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Alabama's estimated tax rules, Code of Ala. § 40-18-80, as the shared
// [estimated.EstimatedTax] interface.
//
// § 40-18-80(a) adopts 26 U.S.C. § 6654 with three changes: $500 replaces
// the $1,000 small-tax amount in § 6654(e)(1), and § 6654(d)(2)(C)(iii) and
// § 6654(f)(2), both about self-employment tax, do not apply. Everything else
// in § 6654 applies, with Alabama's income tax in place of the federal one.
// This package imports no other jurisdiction, so the § 6654 arithmetic is
// written here again rather than taken from ustaxcalc.
//
// The figure follows the Department of Revenue's forms: the 2026 Form 40ES
// and the 2025 Form 2210AL, the latest penalty form. Those forms leave out
// three rules § 6654 grants and give a late starter a schedule § 6654 does
// not; the result's Divergences say where. § 40-18-80 has not been amended
// since Act 2010-685 (research/estimated-payments/al.md), so no form here
// predates the law it describes.

// jurisdiction is Alabama's postal code.
const jurisdiction = estimated.Alabama

// EstimatedFacts are the facts Alabama's rules read that no other
// jurisdiction built here does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorFarmer         bool
	priorFarmerAnswered bool
}

// PriorYearFarmerOrFisher reports whether at least two-thirds of last year's
// gross income was from farming or fishing, and whether the caller said.
// Form 2210AL sends a filer to its Part IV, for farmers and fishermen, when
// either year's income was, and [estimated.Facts] carries only this year's
// share.
func (e EstimatedFacts) PriorYearFarmerOrFisher() (farmer, answered bool) {
	return e.priorFarmer, e.priorFarmerAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

// PriorYearFarmerOrFisher answers whether two-thirds of last year's gross
// income was from farming or fishing.
func (b *EstimatedFactsBuilder) PriorYearFarmerOrFisher(farmer bool) *EstimatedFactsBuilder {
	b.e.priorFarmer, b.e.priorFarmerAnswered = farmer, true
	return b
}

// Build has nothing to refuse: the only fact is a yes or no.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is § 6654(e)(1) as § 40-18-80(a)(2) amends it: no penalty when
	// tax less withholding is under it.
	threshold money.USCents

	// thisYearPercent, priorYearPercent and highIncomePercent are the 90%,
	// 100% and 110% of § 6654(d)(1)(B) and (C).
	thisYearPercent, priorYearPercent, highIncomePercent money.Rate

	// priorYearStep is last year's AGI above which the prior-year test is
	// highIncomePercent; priorYearStepSeparate is the step for a married
	// filer on a separate return.
	priorYearStep, priorYearStepSeparate money.USCents

	// shares are each installment's part of the required annual payment, and
	// the part of the year's withholding deemed paid on each due date.
	shares []money.Rate

	// annualisedPercentages are Schedule AI line 17.
	annualisedPercentages []money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// lateGroups are the first days of the Form 40ES schedule's second, third
	// and fourth rows: a requirement first met on or after lateGroups[i]
	// starts, on that schedule, at installment i+2.
	lateGroups []estimated.Date

	// fileAndPayBy is § 6654(h)'s January 31, which no Alabama form prints.
	fileAndPayBy estimated.Date

	// stop is the last day the penalty runs, April 15 of the next year.
	stop estimated.Date

	periodEnds []estimated.Date

	// formRate is the rate Form 2210AL line 11 prints for the whole year.
	formRate money.Rate

	// rates are the Department of Revenue's published rates, quarter by
	// quarter, as far as they have been published.
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Alabama's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return alabamaEstimated{y}, nil
}

type alabamaEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents
	notes        working.Notes
}

func (r alabamaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("alabamataxcalc: the facts carry %T, not alabamataxcalc.EstimatedFacts", s)
	}
}

func (r alabamaEstimated) check(f estimated.Facts, e EstimatedFacts) error {
	farmerLastYear, _ := e.PriorYearFarmerOrFisher()
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("alabamataxcalc: these are Alabama's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("alabamataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher(), farmerLastYear:
		return fmt.Errorf("%w: a farmer or fisherman with two-thirds of gross income from farming or fishing files Form 2210AL Part IV, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("alabamataxcalc: Schedule AI period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// affected says which divergences change these facts' figures or answers.
type affected struct {
	rate, agiYear, january, noLiability, lateStart bool
}

// divergences are where the Department of Revenue's forms and § 40-18-80,
// through the § 6654 it adopts, part ways. They are found in
// research/estimated-payments/al.md, questions 3, 5, 7 and 10 and its
// verification, and in the statute text in testdata/.
func (r alabamaEstimated) divergences(in affected) ([]working.Divergence, error) {
	y := r.y
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The interest rate").
			Form(fmt.Sprintf("The %d Form 2210AL prints one rate, %v, for every day of the penalty. That was the rate for every quarter of %d. "+
				"For %d the department publishes a rate for each quarter. It is 6%% from April to June and 7%% after.", y.year-1, y.formRate, y.year-1, y.year),
				"Form 2210AL (2025), Part III line 11; ADOR, Quarterly Interest Rates").
			Statute("The penalty is interest at the federal underpayment rate. It runs for the time the payment is late. When the rate changes, each rate applies to the days it covers.",
				"Code of Ala. §§ 40-18-80(a), 40-1-44(a); 26 U.S.C. § 6654(a)").
			Uses(working.ReadingStatute).Affects(in.rate),
		working.NewDivergence("Which year's AGI sets the 110% test").
			Form(fmt.Sprintf("The %d Form 2210AL instructions test the penalty year's own AGI. For the %d penalty they ask for \"AGI for %d\". The %d Form 40ES tests your %d Alabama AGI for %d.",
				y.year-1, y.year-1, y.year-1, y.year, y.year-1, y.year),
				"Form 2210AL instructions (2025), Higher income taxpayers; Form 40ES instructions (2026), Special Rule for Higher Income Taxpayers").
			Statute("110% applies if last year's AGI was over $150,000, or $75,000 filing separately.",
				"Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(d)(1)(C)").
			Uses(working.ReadingStatute).Affects(in.agiYear),
		working.NewDivergence("A return filed and paid by January 31").
			Form(fmt.Sprintf("Neither form excuses the January payment for a return filed early. That means filed and paid in full by %s.", shortDate(y.fileAndPayBy, y.year)),
				"Form 40ES instructions (2026); Form 2210AL (2025) and its instructions").
			Statute("The January payment has no penalty if you file and pay in full by January 31. That January 31 is in the next year.",
				"Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(h)").
			Uses(working.ReadingForm).Affects(in.january),
		working.NewDivergence("No tax liability last year").
			Form("Form 2210AL has no exception for a year with no tax liability before it. A full-year return showing no tax already makes the target $0. The gap matters only if you had no return to file.",
				"Form 2210AL (2025), Part I, and its instructions").
			Statute("No penalty follows a full 12-month year with no Alabama income tax liability. You must also have been a U.S. citizen or resident all that year.",
				"Code of Ala. § 40-18-80(a), (b)(1); 26 U.S.C. § 6654(e)(2)").
			Uses(working.ReadingForm).Affects(in.noLiability),
		working.NewDivergence("A requirement first met after April 1").
			Form(fmt.Sprintf("The 40ES covers a requirement that first arises after April 1. It says to pay a third of your estimated tax by June 15. "+
				"After June 1 it says half by Sep 15, and after Sep 1 all of it by Jan 15, %d. "+
				"Form 2210AL still requires 25%% by every date.", y.year+1),
				"Form 40ES instructions (2026), Changes In Tax and the voucher schedule; Form 2210AL (2025), Part III line 1").
			Statute("There are four payments, each 25% of the required annual payment. No rule covers a requirement that first arises late in the year.",
				"Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(c), (d)(1)(A)").
			Uses(working.ReadingStatute).Affects(in.lateStart),
		working.NewDivergence("Nonresident aliens").
			Form("Neither form has separate rules for a nonresident alien.",
				"Form 40ES instructions (2026); Form 2210AL (2025) and its instructions").
			Statute(fmt.Sprintf("Some nonresident aliens have three payments: June 15, Sep 15 and Jan 15, %d. Half of the required annual payment is due in the first.", y.year+1),
				"Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(j)").
			Uses(working.ReadingForm).Affects(false),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (r alabamaEstimated) assess(f estimated.Facts) (assessment, error) {
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
	a.dues = estimated.MoveDates(y.calendar, y.dues)

	step := y.priorYearStep
	if f.FilingStatus() == filing.MarriedFilingSeparately {
		step = y.priorYearStepSeparate
	}
	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	last := y.year - 1

	// 90% of this year's tax: Form 2210AL Part I line 4.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, y.thisYearPercent.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(d)(1)(B)(i); Form 2210AL (2025), Part I line 4",
			fmt.Sprintf("%v of this year's Alabama tax of %v is %v", y.thisYearPercent, tax, thisYear.Amount()))
	}

	// Last year's tax: Form 2210AL Part I line 7.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearPercent, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Alabama return", last))
		targetQs.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Alabama return, and what was its tax?", last),
			fmt.Sprintf("With it, last year's tax could set the target. %v of it may be lower than %v of this year's tax.", y.priorYearPercent, y.thisYearPercent))
	case !prior.Filed() && prior.NotRequiredToFile():
		priorOption = unavailable(fmt.Sprintf("You weren't required to file a %d Alabama return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Alabama return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Alabama return covered %d months, not a full year", last, prior.Months()))
	default:
		pf, _ := prior.Figures()
		priorTax, _ := pf.Tax()
		agi, agiKnown := pf.StateAGI()
		rate := y.priorYearPercent
		why := fmt.Sprintf("%d Alabama AGI of %v is not more than %v", last, agi, step)
		switch {
		case !agiKnown:
			rate = y.highIncomePercent
			why = fmt.Sprintf("%d Alabama AGI was not given", last)
			targetQs.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d Alabama AGI?", last),
				fmt.Sprintf("Until then, the target is %v of last year's tax. That is the rate over %s, rather than %v.", y.highIncomePercent, step.Whole(), y.priorYearPercent))
		case agi > step:
			rate = y.highIncomePercent
			why = fmt.Sprintf("%d Alabama AGI of %v is more than %v", last, agi, step)
		}
		if agiKnown {
			r.federalAGIQuestion(&a, pf, agi, step)
		}
		amount := rate.Of(priorTax)
		in.agiYear = r.agiYearMatters(f, rate, priorTax, step, thisYear, taxKnown)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, rate, amount)
		a.notes.Rule("Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(d)(1)(B)(ii), (C); Form 40ES instructions (2026), General Rule and Special Rule for Higher Income Taxpayers",
			fmt.Sprintf("%s, so the prior-year test is %v of the %v tax on the %d return, or %v", why, rate, priorTax, last, amount))
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
		targetQs.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Alabama tax to be?", y.year), effect)
	}

	// § 6654(e)(2) is in the statute and not on the form. It is reported, and
	// noted when these facts meet it, but not applied.
	if hasPrior && !prior.Filed() && prior.NotRequiredToFile() && prior.Months() == 12 {
		in.noLiability = true
		a.notes.Rule("Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(e)(2)", fmt.Sprintf("no %d Alabama return was required; the statute excuses the penalty after a twelve-month year with no Alabama liability for a U.S. citizen or resident throughout it, "+
			"but Form 2210AL has no such exception, so it is not applied here (see Divergences)", last))
	}

	switch {
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target needs your %d Alabama tax, because your %d return can't set one.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s covers %v of your %d Alabama tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d Alabama tax, %s. It keeps you clear of a penalty whatever your %d tax turns out to be. After %s of withholding, that's %s of estimated payments. Pay a quarter by each date.",
			priorOpt.Percent(), last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case tax-withheld < y.threshold:
		a.exempt = true
		exception, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		under := ", under " + y.threshold.Whole()
		if tax < withheld {
			under = ""
		}
		rb.Required(false, fmt.Sprintf("Your Alabama tax is %s, and %s is withheld. That %s%s. No estimated payments are needed.",
			tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld), under)).Exempt(exception)
		a.notes.Rule("Code of Ala. § 40-18-80(a)(2); 26 U.S.C. § 6654(e)(1); Form 2210AL (2025), Part I line 6", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no penalty", tax, withheld, tax-withheld, y.threshold))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Alabama withholding of %s covers the %s required for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Alabama tax of %s less %s withheld leaves %s. That is at least %s. Withholding falls short of the %s required. You need %s of estimated payments, a quarter by each date.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.threshold.Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	targetQs.askUnlessExempt(&a.notes, a.exempt)

	if !a.exempt {
		if _, answered := extras.PriorYearFarmerOrFisher(); !answered {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Was two-thirds of your %d gross income from farming or fishing?", last),
				"If so, Form 2210AL Part IV applies; until then the figures assume not.")
		}
	}

	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			a.notes.Rule("Form 40ES instructions (2026), Payment of Estimated Tax", fmt.Sprintf("the installment due %s falls on a Saturday, Sunday or State holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
		}
	}
	a.notes.Rule("Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(c), (d)(1)(A); Form 2210AL (2025), Part III line 1",
		fmt.Sprintf("installments of %v of %v, %v each, are due %s, %s, %s and %s", y.shares[0], a.requirement.Target(), a.installments[0],
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
		a.notes.Rule("Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(d)(2); Form 2210AL (2025), Schedule AI",
			fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: %v, %v, %v and %v of the annualised tax, "+
				"less earlier installments, and never more than the regular installment plus any shortfall the method carried forward",
				ann[0], ann[1], ann[2], ann[3], y.annualisedPercentages[0], y.annualisedPercentages[1], y.annualisedPercentages[2], y.annualisedPercentages[3]))
	}

	in.lateStart = r.lateStart(f, &a)

	// § 6654(h) is in the statute and not on the forms. January 31, 2027 is
	// a Sunday; no Alabama document says whether it moves, and since the rule
	// is not applied, nothing here depends on it.
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(y.fileAndPayBy) && !a.exempt {
		a.notes.Rule("Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(h)", fmt.Sprintf("the return was filed and paid on %s, by %s, which the statute says excuses the January installment; "+
			"Alabama's forms do not, so it is still charged here (see Divergences)", filed.Long(), y.fileAndPayBy.Long()))
	}

	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	// Which divergences change these figures turns on the shortfalls.
	if !a.exempt {
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return assessment{}, err
		}
		in.rate = r.rateMatters(ledger)
		filed := f.ReturnFiled()
		in.january = !filed.IsZero() && !filed.After(y.fileAndPayBy) && ledger[len(ledger)-1].Shortfall() > 0
	}
	in.noLiability = in.noLiability && !a.exempt
	divergences, err := r.divergences(in)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

// rateMatters reports whether a shortfall runs into a quarter whose
// published rate is not the rate Form 2210AL prints.
func (r alabamaEstimated) rateMatters(ledger []estimated.LedgerEntry) bool {
	for _, l := range ledger {
		if l.Shortfall() <= 0 {
			continue
		}
		for _, p := range r.y.rates.Periods() {
			if p.Rate() != r.y.formRate && p.To().After(l.Due()) {
				return true
			}
		}
	}
	return false
}

// agiYearMatters reports whether testing this year's Alabama AGI against the
// step, as the 2025 Form 2210AL does, would give a different target. When
// this year's AGI is not in the facts, it reports whether the other
// percentage would.
func (r alabamaEstimated) agiYearMatters(f estimated.Facts, rate money.Rate, priorTax, step money.USCents, thisYear estimated.TargetOption, taxKnown bool) bool {
	other := r.y.priorYearPercent
	if rate == other {
		other = r.y.highIncomePercent
	}
	if agi, known := f.ThisYear().StateAGI(); known {
		other = r.y.priorYearPercent
		if agi > step {
			other = r.y.highIncomePercent
		}
	}
	if other == rate {
		return false
	}
	lesser := func(a money.USCents) money.USCents {
		if taxKnown && thisYear.Amount() < a {
			return thisYear.Amount()
		}
		return a
	}
	return lesser(rate.Of(priorTax)) != lesser(other.Of(priorTax))
}

// federalAGIQuestion reports that § 6654(d)(1)(C), read into Alabama law,
// does not say whose adjusted gross income it tests, when the answer could
// change the percentage: federal AGI was not given, or lies on the other side
// of the step from Alabama AGI.
func (r alabamaEstimated) federalAGIQuestion(a *assessment, pf estimated.Figures, agi, step money.USCents) {
	last := r.y.year - 1
	fed, known := pf.FederalAGI()
	switch {
	case !known:
		a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d federal AGI?", last),
			fmt.Sprintf("Alabama law doesn't say if the %s test reads federal or Alabama AGI. Until then, the figures use the Alabama AGI the %d Form 40ES reads.", step.Whole(), r.y.year))
	case (fed > step) != (agi > step):
		other := r.y.priorYearPercent
		if fed > step {
			other = r.y.highIncomePercent
		}
		a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("With your %d federal AGI of %s, the %s test would give %v. The law doesn't say which AGI; the %d Form 40ES and the figures use Alabama's.",
			last, fed.Whole(), step.Whole(), other, r.y.year))
	}
}

// lateStart notes what the Form 40ES tells a filer whose requirement first
// arose after April 1, when the caller said so, and reports whether it did.
// The figure does not use it: Form 2210AL and § 6654(d)(1)(A) charge 25% by
// every date.
func (r alabamaEstimated) lateStart(f estimated.Facts, a *assessment) bool {
	met := f.RequirementFirstMet()
	if met.IsZero() || a.exempt {
		return false
	}
	first := 0
	for i, g := range r.y.lateGroups {
		if !met.Before(g) {
			first = i + 1
		}
	}
	if first == 0 {
		return false
	}
	share := "all"
	if remaining := len(a.dues) - first; remaining > 1 {
		share = fmt.Sprintf("1/%d", remaining)
	}
	a.notes.Rule("Form 40ES instructions (2026), Changes In Tax; Form 2210AL (2025), Part III line 1",
		fmt.Sprintf("the requirement first arose on %s; the 40ES would have you pay %s of the estimate by %s, but Form 2210AL charges %v by every date, so the earlier installments are charged here (see Divergences)",
			met.Long(), share, a.dues[first].Long(), r.y.shares[0]))
	return true
}

func (r alabamaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies the credits to the installments.
func (r alabamaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, r.y.shares)
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	ledger, err := estimated.Apply(a.dues, a.installments, credits)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(g)(1); Form 2210AL (2025), Part II box D",
			fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(g)(1); Form 2210AL (2025), Part II box D",
			fmt.Sprintf("the %v withheld is treated as paid in equal parts on the due dates, whenever it was actually withheld", f.WithheldTotal()))
	}
	for _, p := range f.Payments() {
		if p.FromPriorYear() {
			notes.Rule("Form 40ES instructions (2026), item 4", fmt.Sprintf("last year's overpayment of %v applied forward counts on %s; the 40ES lets it go to the earliest installment, which is never later than dividing it among them",
				p.Amount(), a.dues[0].Long()))
		}
	}
	notes.Rule("Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(b)(3); Form 2210AL (2025), Part III lines 3 to 9",
		"a payment after a due date pays the oldest unpaid installment first, whichever date it was meant for")
	return ledger, notes, nil
}

func (r alabamaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Alabama return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
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

// installments are the schedule the ledger gives.
func (r alabamaEstimated) installments(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	var needed money.USCents
	for i, l := range ledger {
		needed += a.installments[i]
		var err error
		if out[i], err = estimated.NewInstallment(i+1, l.Due()).NeededBy(needed).CreditedBy(l.CreditedBy()).
			Shortfall(l.Shortfall()).Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r alabamaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("alabamataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Alabama return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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

	p, unrated, err := estimated.SimplePenalty(ledger, stop, y.rates, waiver, a.requirement, schedule)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(d)(1)(B)", fmt.Sprintf("this year's Alabama tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and %v of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, y.thisYearPercent, p.Total()))
	}
	if paidInFull {
		notes.Rule("Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(b)(2)", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("Code of Ala. §§ 40-18-80(a), 40-1-44(a); 26 U.S.C. § 6654(a), (b)(2); Form 2210AL (2025), Part III lines 10 to 12; ADOR, Quarterly Interest Rates",
		fmt.Sprintf("each shortfall is charged the department's rate for each quarter, divided by 365, for each day from its due date until paid or %s; "+
			"Form 2210AL asks for whole dollars, and these figures are to the cent; the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", shortDate(unrated.AddDays(-1), 0)))
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Will you ask the department to waive the penalty?",
			"It can waive it for a casualty, disaster or other unusual event. Retiring after 62 or becoming disabled also counts, with reasonable cause.")
	}
	if o, ok := estimated.StandardWaiver(f, "the Alabama Department of Revenue", 62); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// shortDate is "Aug 31" inside the tax year and "Jan 15, 2027" outside it.
func shortDate(d estimated.Date, year int) string {
	m := d.Month().String()[:3]
	if d.Year() == year {
		return fmt.Sprintf("%s %d", m, d.Day())
	}
	return fmt.Sprintf("%s %d, %d", m, d.Day(), d.Year())
}

func (r alabamaEstimated) Explanation() estimated.Explanation { return alabamaExplanation }

// alabamaExplanation is written by hand and checked against
// research/estimated-payments/al.md.
var alabamaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Alabama uses the federal estimated tax rules, with a $500 threshold, not $1,000. The penalty is interest on each quarter's shortfall.").
		Paragraph("Withholding and estimated payments avoid the penalty if they reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. The second is 110% if last year's Alabama AGI was over $150,000. The AGI limit is $75,000 for married filing separately. Last year's figure is open only if you filed a return for a full twelve months. There is no penalty if this year's tax less withholding is under $500.").
		Paragraph("Pay a quarter each by April 15, June 15, September 15 and the next January 15. A date on a Saturday, Sunday or State holiday shifts to the next business day. Withholding counts as paid evenly on the four dates. You can instead show when it was actually withheld. The department sends no record of your payments, so keep your own.").
		Paragraph("The penalty is interest on each shortfall for the days it stays unpaid. The department's rate follows the federal underpayment rate each quarter. Interest stops on April 15 of the next year. A payment goes first to the oldest shortfall. The 2025 penalty form prints 7% for the whole year. For 2026 the rate is 6% from April to June and 7% after. If income came unevenly, Schedule AI's annualised method can cut early payments. The department can waive the penalty for a casualty or disaster. It can also do so for retirement after 62 or disability, with reasonable cause.").
		Paragraph("By adopting the federal rules, the statute grants two things the forms omit. The January payment has no penalty if you file and pay in full by January 31. There is no penalty after a full year with no Alabama tax liability. The department's forms apply neither. If you first need to pay after April 1, Form 40ES says a third by June 15. After June 1 it says half by September 15. The penalty form still charges a quarter by every date.").
		VersusFederal("Alabama's threshold is $500 rather than $1,000, the federal figure. The 110% test reads last year's Alabama AGI. Alabama's State holidays move a due date. The forms charge the January payment even if you file and pay by January 31. They also charge someone who had no tax last year. The statute excuses both.").
		Source("Code of Ala. §§ 40-18-80, 40-1-44 and 1-3-8; 26 U.S.C. § 6654").
		Source("Form 40ES and its instructions, 2026").
		Source("Form 2210AL and its instructions, 2025").
		Source("Alabama Department of Revenue, Quarterly Interest Rates").
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
