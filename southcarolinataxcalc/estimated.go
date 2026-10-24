package southcarolinataxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// South Carolina's estimated tax rules, S.C. Code §§ 12-6-3910 and 12-54-55,
// as the shared [estimated.EstimatedTax] interface.
//
// § 12-54-55(1) computes the penalty "in the same manner" as 26 U.S.C.
// § 6654, with $100 in place of the $1,000 small-amount figure and no penalty
// on an underpayment caused by personal service income earned in another
// state that withheld its own tax. § 12-6-3910(A) fixes the four due dates.
// The rate is set under § 12-54-25(D) in the same manner and at the same time
// as the federal underpayment rate. This package imports no other
// jurisdiction, so the § 6654 arithmetic is written here again.
//
// The figure follows the department's forms: the 2026 SC1040ES and the 2025
// SC2210, the latest penalty form. Where they part from each other or from the
// statute, the result's Divergences say so. Act 110 of 2026 changed how the
// 2026 tax is computed after the SC1040ES was printed; it did not change the
// estimated tax rules (research/estimated-payments/sc.md).

// jurisdiction is South Carolina's postal code.
const jurisdiction = estimated.SouthCarolina

// EstimatedFacts are the facts South Carolina's rules read that no other
// jurisdiction built here does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorFarmer, priorFarmerAnswered bool
	otherState, otherStateAnswered   bool
	contractor, contractorAnswered   bool
}

// PriorYearFarmerOrFisher reports whether at least two-thirds of last year's
// gross income was from farming and fishing, and whether the caller said. The
// SC2210 puts a filer under its farmer rules when either year's income was,
// and [estimated.Facts] carries only this year's share.
func (e EstimatedFacts) PriorYearFarmerOrFisher() (farmer, answered bool) {
	return e.priorFarmer, e.priorFarmerAnswered
}

// UnderpaymentFromOtherStateIncome reports whether the underpayment comes from
// personal service income earned in another state, on which that state's tax
// was withheld, and whether the caller said. § 12-54-55(1) charges no penalty
// on it. Answer yes only when the whole underpayment comes from that income;
// a share of it is not built.
func (e EstimatedFacts) UnderpaymentFromOtherStateIncome() (yes, answered bool) {
	return e.otherState, e.otherStateAnswered
}

// NonresidentContractor reports whether the filer is a nonresident working in
// South Carolina under contracts over $10,000 with 2% withheld from each
// payment, and whether the caller said. The SC1040ES excuses such a filer from
// making a declaration.
func (e EstimatedFacts) NonresidentContractor() (yes, answered bool) {
	return e.contractor, e.contractorAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

// PriorYearFarmerOrFisher answers whether two-thirds of last year's gross
// income was from farming and fishing.
func (b *EstimatedFactsBuilder) PriorYearFarmerOrFisher(farmer bool) *EstimatedFactsBuilder {
	b.e.priorFarmer, b.e.priorFarmerAnswered = farmer, true
	return b
}

// UnderpaymentFromOtherStateIncome answers whether the whole underpayment
// comes from personal service income earned in another state that withheld
// its tax.
func (b *EstimatedFactsBuilder) UnderpaymentFromOtherStateIncome(yes bool) *EstimatedFactsBuilder {
	b.e.otherState, b.e.otherStateAnswered = yes, true
	return b
}

// NonresidentContractor answers whether the filer is a nonresident contractor
// with 2% withheld from each contract payment.
func (b *EstimatedFactsBuilder) NonresidentContractor(yes bool) *EstimatedFactsBuilder {
	b.e.contractor, b.e.contractorAnswered = yes, true
	return b
}

// Build has nothing to refuse: every fact is a yes or no.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is the small amount of § 12-54-55(1): no penalty when tax less
	// withholding is under it.
	threshold money.USCents

	// thisYearPercent, priorYearPercent and highIncomePercent are the 90%,
	// 100% and 110% of § 6654(d)(1)(B) and (C).
	thisYearPercent, priorYearPercent, highIncomePercent money.Rate

	// priorYearStep is last year's AGI above which the prior-year test is
	// highIncomePercent; priorYearStepSeparate is the step on a married
	// separate return.
	priorYearStep, priorYearStepSeparate money.USCents

	// shares are each installment's part of the required annual payment, and
	// the part of the year's withholding deemed paid on each due date.
	shares []money.Rate

	// annualisedPercentages are § 6654(d)(2)(C)(ii), Schedule AI line 20.
	annualisedPercentages []money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// lateGroups are the first days of the SC1040ES's later groups: a
	// requirement first met on or after lateGroups[i] has, on that form, its
	// first payment at installment i+2.
	lateGroups []estimated.Date

	// fileAndPayBy is § 6654(h)'s January 31, before moving for a weekend.
	fileAndPayBy estimated.Date

	// stop is the last day the penalty runs, April 15 of the next year.
	stop estimated.Date

	periodEnds []estimated.Date

	// formRate is the rate the SC2210 prints in every rate period.
	formRate money.Rate

	// rates are the department's published rates, one period for each of the
	// SC2210's rate periods, as far as they have been published.
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns South Carolina's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return southCarolinaEstimated{y}, nil
}

// EstimatedCalendar returns the legal holidays that move a due date for a tax
// year's estimated tax, as SC Revenue Procedure #23-1 lists them, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedCalendar(year int) (estimated.Calendar, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return estimated.Calendar{}, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return y.calendar, nil
}

type southCarolinaEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents

	// excused is the January installment, for a return filed and paid by
	// the moved January 31.
	excused bool

	// otherState is an underpayment from income earned in another state,
	// which bears no penalty.
	otherState bool

	notes working.Notes
}

// affects records, for each divergence, whether its readings give these facts
// different figures or answers.
type affects struct {
	act110, rate, exactStep, separateStep, noLiability, january, earlier, otherState, lateStart bool
}

func (r southCarolinaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("southcarolinataxcalc: the facts carry %T, not southcarolinataxcalc.EstimatedFacts", s)
	}
}

func (r southCarolinaEstimated) check(f estimated.Facts, e EstimatedFacts) error {
	farmerLastYear, _ := e.PriorYearFarmerOrFisher()
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("southcarolinataxcalc: these are South Carolina's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("southcarolinataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher(), farmerLastYear:
		return fmt.Errorf("%w: a farmer or fisherman with two-thirds of gross income from farming and fishing has the SC2210's farmer rules and the federal 2210-F, which are not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("southcarolinataxcalc: Schedule AI period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms part from each other or from
// §§ 12-6-3910 and 12-54-55 and the § 6654 they adopt. They are found in
// research/estimated-payments/sc.md and checked against the extracts in
// testdata/.
func (r southCarolinaEstimated) divergences(af affects) ([]working.Divergence, error) {
	y := r.y
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The 2026 worksheet predates Act 110").
			Form("The 2026 worksheet figures the tax from federal taxable income at 0%, 3% and 6%. It was printed before Act 110 and is out of date. The act didn't change its thresholds, percentages and dates, which still apply.",
				"SC1040ES (Rev. 10/13/25), 2026 Estimated Tax Worksheet lines 1 to 4 and Tax Computation Schedule").
			Statute("Act 110 of 2026 was approved March 30, 2026. It starts from federal AGI. It taxes 2026 income at 1.99%, or 5.21% less $966 from $30,000.",
				"Act No. 110 of 2026 (H.4216), SECTION 8; SC Information Letter #26-20").
			Uses(working.ReadingStatute).Affects(af.act110),
		working.NewDivergence("The interest rate").
			Form(fmt.Sprintf("The 2025 SC2210 prints %v (.07) in all four rate periods. That includes Jan 1 to Apr 15, 2026, when the published rate from Apr 1 was 6%%.", y.formRate),
				"SC2210 (2025), Part III lines 19 to 25; SC Information Letter #26-19").
			Statute("The rate is set in the same manner as the federal underpayment rate. It is set at the same time as the federal underpayment rate. So each day carries the rate in effect that day. For 2026 that is 6% to June 30 and 7% after.",
				"S.C. Code §§ 12-54-25(D), 12-54-55(1); 26 U.S.C. § 6654(a)").
			Uses(working.ReadingStatute).Affects(af.rate),
		working.NewDivergence("The 110% test at exactly $150,000").
			Form("The 2025 SC1040 instructions raise the prior-year test to 110%. They do so when AGI is \"$150,000 or more\". The SC1040ES and SC2210 say \"more than $150,000\".",
				"SC1040 instructions (2025), line 33; SC1040ES (2026), Penalty for failure to file and pay; SC2210 (2025), Part I line 7").
			Statute("110% applies when last year's AGI exceeds $150,000.",
				"S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(1)(C)(i)").
			Uses(working.ReadingForm).Affects(af.exactStep),
		working.NewDivergence("The married filing separately step").
			Form("The SC1040ES states only the $150,000 step. The SC2210 lowers it to $75,000 for a married separate return.",
				"SC1040ES (2026), Penalty for failure to file and pay; SC2210 (2025), Part I line 7").
			Statute("On a separate return for the year the installment is for, the step is $75,000.",
				"S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(1)(C)(ii)").
			Uses(working.ReadingForm).Affects(af.separateStep),
		working.NewDivergence("No tax liability last year").
			Form("No penalty after a full 12-month year with $0 liability. There is no condition of U.S. citizenship or residence.",
				"SC2210 (2025), Exceptions to the penalty; SC1040ES (2026), Exceptions to filing").
			Statute("You must also have been a U.S. citizen or resident all of last year.",
				"S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(e)(2)(C)").
			Uses(working.ReadingForm).Affects(af.noLiability),
		working.NewDivergence("The January 31 date").
			Form("The 2025 SC2210 prints January 31 without moving it for a weekend. The 2026 SC1040ES prints February 1: January 31, 2027, moved past a Sunday.",
				"SC2210 (2025), instructions; SC1040ES (2026), Payment due dates").
			Statute("A return filed and paid in full by January 31 excuses the fourth installment. A deadline on a weekend or legal holiday moves to the next business day.",
				"26 U.S.C. § 6654(h); S.C. Code § 12-60-50(A), quoted in SC Revenue Procedure #23-1").
			Uses(working.ReadingForm).Affects(af.january),
		working.NewDivergence("Earlier installments after a return filed by January 31").
			Form("The earlier installments' periods of underpayment end on January 15. That applies when the return is filed and the tax paid by January 31.",
				"SC2210 (2025), instructions, Period of underpayment").
			Statute("Each period runs until the underpayment is paid. That is the day the return's payment is made, or April 15 if earlier.",
				"S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(b)(2)").
			Uses(working.ReadingForm).Affects(af.earlier),
		working.NewDivergence("Income earned in another state").
			Form("The SC2210 lists a reason to request a waiver. It is an underpayment from personal service income earned in another state. That state withheld its tax. The 2026 SC1040ES says no penalty is charged on it.",
				"SC2210 (2025), Waiver of penalty item 3; SC1040ES (2026), Penalty for failure to file and pay").
			Statute("The law says no interest or penalty is due on such an underpayment.",
				"S.C. Code § 12-54-55(1)").
			Uses(working.ReadingForm).Affects(af.otherState),
		working.NewDivergence("A requirement first met after April 1").
			Form("For a late requirement, the SC1040ES starts payments June 15, Sep 15 or Jan 15. By that date you pay half, three-quarters or all of your estimated tax. The SC2210 requires 25% of the required annual payment by every date.",
				"SC1040ES (2026), Payment due dates and worksheet line 11; SC2210 (2025), Part III line 9").
			Statute("There are four required installments, each 25% of the required annual payment. There is no rule for a requirement that arises late in the year.",
				"S.C. Code §§ 12-6-3910(A)(3)(a), 12-54-55(1); 26 U.S.C. § 6654(c), (d)(1)(A)").
			Uses(working.ReadingForm).Affects(af.lateStart),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// date is d as a person reads it: "Dec 15" in the tax year, "Jan 15, 2027"
// outside it.
func (r southCarolinaEstimated) date(d estimated.Date) string {
	if d.Year() == r.y.year {
		return d.Time().Format("Jan 2")
	}
	return d.Time().Format("Jan 2, 2006")
}

func (r southCarolinaEstimated) assess(f estimated.Facts) (assessment, error) {
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	if err := r.check(f, extras); err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	var af affects
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

	if taxKnown {
		a.notes.Rule("Act No. 110 of 2026, SECTION 8; SC Information Letter #26-20",
			fmt.Sprintf("this year's tax of %v is taken as figured under Act 110, not the SC1040ES worksheet (see Divergences)", tax))
	}

	// 90% of this year's tax: SC2210 Part I line 4.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, y.thisYearPercent.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(1)(B)(i); SC2210 (2025), Part I line 4",
			fmt.Sprintf("%v of this year's South Carolina tax of %v is %v", y.thisYearPercent, tax, thisYear.Amount()))
	}

	// Last year's tax: SC2210 Part I line 7.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearPercent, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	var priorTax money.USCents
	// targetAsks are questions whose answer can change the target, asked
	// only when no exception sets it.
	var targetAsks []func()
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d South Carolina return", last))
		targetAsks = append(targetAsks, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d South Carolina return, and what was its tax?", last),
				fmt.Sprintf("With it, %v of last year's tax could set a lower target.", y.priorYearPercent))
		})
	case !prior.Filed() && prior.NotRequiredToFile():
		priorOption = unavailable(fmt.Sprintf("You didn't need to file a %d South Carolina return. There's no tax to measure against", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d South Carolina return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d South Carolina return covered %d months, not 12", last, prior.Months()))
	default:
		pf, _ := prior.Figures()
		priorTax, _ = pf.Tax()
		agi, agiKnown := pf.StateAGI()
		rate := y.priorYearPercent
		why := fmt.Sprintf("%d South Carolina AGI of %v is not more than %v", last, agi, step)
		switch {
		case !agiKnown:
			rate = y.highIncomePercent
			why = fmt.Sprintf("%d South Carolina AGI was not given", last)
			targetAsks = append(targetAsks, func() {
				a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d South Carolina adjusted gross income?", last),
					fmt.Sprintf("Until then, the target assumes over %s, so last year's test is %v.", step.Whole(), y.highIncomePercent))
			})
		case agi > step:
			rate = y.highIncomePercent
			why = fmt.Sprintf("%d South Carolina AGI of %v is more than %v", last, agi, step)
		}
		if agiKnown {
			targetAsks = append(targetAsks, func() { r.federalAGIQuestion(&a, pf, agi, step) })
			af.exactStep = agi == step
			af.separateStep = f.FilingStatus() == filing.MarriedFilingSeparately && agi > y.priorYearStepSeparate && agi <= y.priorYearStep
		}
		amount := rate.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, rate, amount)
		a.notes.Rule("S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(1)(B)(ii), (C); SC1040ES (2026), Penalty for failure to file and pay; SC2210 (2025), Part I line 7",
			fmt.Sprintf("%s, so the prior-year test is %v of the %v tax on the %d SC1040, or %v", why, rate, priorTax, last, amount))
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
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d South Carolina tax to be?", y.year),
				fmt.Sprintf("With it, %v of this year's tax could be your target. A balance under %s would need no payments.", y.thisYearPercent, y.threshold.Whole()))
		})
	}

	// $0 liability for a full 12 months last year: SC2210 exception 1. The
	// forms state no citizenship or residence condition, so none is applied.
	noLiability := hasPrior && prior.Months() == 12 &&
		((prior.Filed() && priorTax == 0) || (!prior.Filed() && prior.NotRequiredToFile()))
	contractor, contractorAnswered := extras.NonresidentContractor()

	switch {
	case noLiability:
		a.exempt = true
		ex, err := estimated.NewException("No tax due last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your %d South Carolina tax was $0 for a full 12 months. No estimated payments are needed for %d.", last, y.year)).Exempt(ex)
		a.notes.Rule("SC2210 (2025), Exceptions to the penalty 1; SC1040ES (2026), Exceptions to filing; 26 U.S.C. § 6654(e)(2)",
			fmt.Sprintf("the %d liability was $0 for a full 12 months, so there is no penalty for %d (see Divergences on the residence condition)", last, y.year))
	case !taxKnown && !priorOpt.Available():
		why := fmt.Sprintf("Your target is the smaller of two figures. They are %v of your %d South Carolina tax and %v of your %d tax.", y.thisYearPercent, y.year, y.priorYearPercent, last)
		if hasPrior {
			why = fmt.Sprintf("Last year's option isn't open to you. Your target is %v of your %d South Carolina tax.", y.thisYearPercent, y.year)
		}
		rb.Required(false, why).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s covers %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. It keeps you clear of the penalty whatever your %d tax turns out to be. After %s of withholding, that's %s of estimated payments. Pay a quarter by each date.",
			priorOpt.Percent(), last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case tax-withheld < y.threshold:
		a.exempt = true
		ex, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your South Carolina tax of %s less %s withheld is under %s. No estimated payments are needed.",
			tax.Whole(), withheld.Whole(), y.threshold.Whole())
		if tax < withheld {
			why = fmt.Sprintf("Your South Carolina tax is %s, and %s is withheld. That %s. No estimated payments are needed.",
				tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
		}
		rb.Required(false, why).Exempt(ex)
		a.notes.Rule("S.C. Code §§ 12-6-3910(A)(1), 12-54-55(1); SC2210 (2025), Part I lines 3 and 6", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no penalty", tax, withheld, tax-withheld, y.threshold))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your South Carolina withholding is %s. That covers the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	case contractor:
		rb.Required(false, fmt.Sprintf("You're a nonresident working under contracts over $10,000. Each payment has 2%% withheld. So you don't have to file a declaration of estimated tax. "+
			"Withholding is %s short of the %s the penalty form measures against.", (target-withheld).Whole(), target.Whole())).Choose(chosen)
		a.notes.Rule("SC1040ES (2026), Exceptions to filing a Declaration of Estimated Tax", "a nonresident contractor with 2% withheld from each payment on contracts over $10,000 need not file a declaration")
		a.notes.Ask(working.PositionJurisdictionFact, "Has the department said that excusing the declaration also excuses the penalty?",
			"Neither the form nor the law says, so until then the penalty follows the form.")
	default:
		rb.Required(true, fmt.Sprintf("Your South Carolina tax of %s less %s withheld leaves %s due. Withholding falls short of your %s target. You need estimated payments of %s, a quarter by each date.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}

	if !a.exempt {
		// An exception sets the target, so these answers can't change it.
		for _, ask := range targetAsks {
			ask()
		}
		if _, answered := extras.PriorYearFarmerOrFisher(); !answered {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did two-thirds of your %d gross income come from farming and fishing?", last),
				"If so, the farmer rules apply; until then, your target assumes it didn't.")
		}
		if a.requirement.Required() && !contractorAnswered {
			a.notes.Ask(working.PositionJurisdictionFact, "Are you a nonresident contractor in South Carolina, with 2% withheld?",
				"That means 2% withheld from each payment on contracts over $10,000. If so, you don't have to file a declaration of estimated tax.")
		}
	}

	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			a.notes.Rule("S.C. Code § 12-60-50(A); SC Revenue Procedure #23-1", fmt.Sprintf("the installment due %s falls on a Saturday, Sunday or legal holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
		}
	}
	a.notes.Rule("S.C. Code § 12-6-3910(A)(3)(a); 26 U.S.C. § 6654(d)(1)(A); SC2210 (2025), Part III line 9",
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
		a.notes.Rule("S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(2); SC2210 (2025), Annualized Income Installment Method",
			fmt.Sprintf("under the annualised method, on federal Schedule AI for South Carolina purposes, the installments are %v, %v, %v and %v: %v, %v, %v and %v of the annualised tax, "+
				"less earlier installments, and never more than the regular installment plus any shortfall the method carried forward",
				ann[0], ann[1], ann[2], ann[3], y.annualisedPercentages[0], y.annualisedPercentages[1], y.annualisedPercentages[2], y.annualisedPercentages[3]))
	}

	af.lateStart = r.lateStart(f, &a)

	// § 6654(h): a return filed and paid by January 31 excuses the January
	// installment. January 31, 2027 is a Sunday; § 12-60-50 moves it to
	// February 1, as the 2026 SC1040ES prints it.
	fileBy := y.calendar.NextBusinessDay(y.fileAndPayBy)
	filed := f.ReturnFiled()
	af.january = !a.exempt && !filed.IsZero() && filed.After(y.fileAndPayBy) && !filed.After(fileBy)
	if !filed.IsZero() && !filed.After(fileBy) && !a.exempt {
		a.excused = true
		a.notes.Rule("26 U.S.C. § 6654(h); S.C. Code § 12-60-50(A); SC1040ES (2026), Payment due dates; SC2210 (2025), instructions",
			fmt.Sprintf("the return was filed and paid on %s, by %s (%s, moved to the next business day), so the January installment is excused and the earlier installments' periods end on %s",
				filed.Long(), fileBy.Long(), y.fileAndPayBy.Long(), a.dues[3].Long()))
	}

	if yes, _ := extras.UnderpaymentFromOtherStateIncome(); yes && !a.exempt {
		a.otherState = true
		af.otherState = true
		a.notes.Rule("S.C. Code § 12-54-55(1); SC1040ES (2026), Penalty for failure to file and pay",
			"the underpayment comes from personal service income earned in another state, which withheld its own tax, so no penalty is due on it")
	}

	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	// What the rate and January readings turn on: a shortfall outstanding
	// while the rates differ, or after January 15.
	af.act110 = taxKnown && !noLiability
	af.noLiability = noLiability
	if !a.exempt && a.requirement.TargetKnown() {
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return assessment{}, err
		}
		af.rate = !a.otherState && (ledger[0].Shortfall() > 0 || ledger[1].Shortfall() > 0)
		if a.excused && !a.otherState {
			for _, l := range ledger[:3] {
				for _, piece := range l.Pieces() {
					if on, paid := piece.PaidOn(); !paid || on.After(a.dues[3]) {
						af.earlier = true
					}
				}
			}
		}
	}
	divergences, err := r.divergences(af)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

// federalAGIQuestion reports that the 110% test's AGI is not settled, when
// the answer could change the percentage: federal AGI was not given, or lies
// on the other side of the step from South Carolina AGI.
func (r southCarolinaEstimated) federalAGIQuestion(a *assessment, pf estimated.Figures, agi, step money.USCents) {
	last := r.y.year - 1
	fed, known := pf.FederalAGI()
	switch {
	case !known:
		a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d federal adjusted gross income?", last),
			fmt.Sprintf("The law doesn't say which AGI the %s step for the 110%% test reads. It could be federal or South Carolina AGI; your target uses South Carolina's.", step.Whole()))
	case (fed > step) != (agi > step):
		other := r.y.priorYearPercent
		if fed > step {
			other = r.y.highIncomePercent
		}
		a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("Should the 110%% test's %s step use federal AGI, not South Carolina's?", step.Whole()),
			fmt.Sprintf("Your target uses your South Carolina AGI of %s. Your federal AGI of %s would make the test %v.", agi.Whole(), fed.Whole(), other))
	}
}

// lateStart notes what the SC1040ES tells a filer whose requirement first
// arose after April 1, when the caller said so, and reports whether it did.
// The figure does not use it: the SC2210 and § 6654(d)(1)(A) require 25% by
// every date.
func (r southCarolinaEstimated) lateStart(f estimated.Facts, a *assessment) bool {
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
	share := []string{"", "1/2", "3/4", "all"}[first]
	a.notes.Rule("SC1040ES (2026), Payment due dates and worksheet line 11; SC2210 (2025), Part III line 9",
		fmt.Sprintf("the requirement first arose on %s; the SC1040ES would have your first payment be %s of the estimate on %s, but the SC2210 requires %v by every date, so the earlier installments are charged here (see Divergences)",
			met.Long(), share, a.dues[first].Long(), r.y.shares[0]))
	return true
}

func (r southCarolinaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies the credits to the installments, with the January
// installment set to nothing when filing early excused it.
func (r southCarolinaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
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
		notes.Rule("26 U.S.C. § 6654(g)(1); SC2210 (2025), Part II box D",
			fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("26 U.S.C. § 6654(g)(1); SC2210 (2025), How to calculate your underpayment",
			fmt.Sprintf("the %v withheld is treated as paid in equal parts on the four due dates, whenever it was actually withheld", f.WithheldTotal()))
	}
	notes.Rule("26 U.S.C. § 6654(b)(3); SC2210 (2025), Part III lines 10 to 17",
		"a payment after a due date pays the oldest unpaid installment first, whichever date it was meant for")
	return ledger, notes, nil
}

func (r southCarolinaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d South Carolina return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
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

// schedule is the installments from the ledger. The January installment of a
// return filed early, and every shortfall from income earned in another
// state, is excused.
func (r southCarolinaEstimated) schedule(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	var needed money.USCents
	for i, l := range ledger {
		needed += a.installments[i]
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(needed).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if (a.excused && i == 3) || a.otherState {
			b.Excused()
		}
		in, err := b.Build()
		if err != nil {
			return nil, err
		}
		out[i] = in
	}
	return out, nil
}

func (r southCarolinaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("southcarolinataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d South Carolina return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	if a.excused && a.dues[3].Before(stop) {
		// SC2210 instructions: the earlier periods end on January 15.
		stop = a.dues[3]
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
	if a.exempt || a.otherState {
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
		notes.Rule("S.C. Code § 12-54-55(1); 26 U.S.C. § 6654(d)(1)(B)", fmt.Sprintf("this year's South Carolina tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and %v of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, y.thisYearPercent, p.Total()))
	}
	if paidInFull && !a.excused {
		notes.Rule("26 U.S.C. § 6654(b)(2)", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("S.C. Code §§ 12-54-25(D), 12-54-55(1); 26 U.S.C. § 6654(a), (b)(2); SC2210 (2025), Part III lines 18 to 26; SC Information Letter #26-19",
		fmt.Sprintf("each shortfall is charged simple interest at the department's rate for each period, divided by 365, for each day from its due date until paid or %s; "+
			"the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", y.rates.Ends().Time().Format("Jan 2, 2006")))
	}
	if p.Total() > 0 {
		// SC2210 waiver items 1 and 2 are the federal pair, at age 62.
		if o, ok := estimated.StandardWaiver(f, "the department", 62); ok {
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
		if !waiverAnswered {
			notes.Ask(working.PositionPenaltyWaiver, "Will you ask the department to waive the penalty?",
				"Casualty, disaster, unusual events, retiring after 62 or disability can qualify. The last two need reasonable cause; the penalty shown is before any waiver.")
		}
		extras, _ := r.extras(f)
		if _, answered := extras.UnderpaymentFromOtherStateIncome(); !answered {
			notes.Ask(working.PositionJurisdictionFact, "Is the underpayment from personal service income earned in another state?",
				"Personal service income includes wages. If that state withheld its own tax, South Carolina charges no penalty on it.")
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r southCarolinaEstimated) Explanation() estimated.Explanation { return southCarolinaExplanation }

// southCarolinaExplanation is written by hand and checked against
// research/estimated-payments/sc.md.
var southCarolinaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"South Carolina uses the federal estimated tax rules, but its threshold is $100. The federal threshold is $1,000. The penalty is interest on each quarter's shortfall.").
		Paragraph("There is no penalty if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. The second figure is 110% if last year's AGI was over $150,000. The AGI limit is $75,000 for married filing separately. Last year's figure needs a South Carolina return covering a full twelve months. There is no penalty if this year's tax less withholding is under $100. There is also none after a full twelve months with $0 of South Carolina tax.").
		Paragraph("Pay a quarter each by April 15, June 15, September 15 and the next January 15. A weekend or South Carolina legal holiday moves a date to the next business day. The state list has days the federal one lacks, such as Confederate Memorial Day. Withholding counts as paid evenly on the four dates. You can instead show when it was actually withheld. Filing and paying the balance by February 1, 2027 excuses the January payment. The earlier shortfalls then stop accruing on January 15.").
		Paragraph("The penalty is simple interest on a shortfall for the days it stays unpaid. The department's rate follows the federal underpayment rate. Each day is charged the rate divided by 365. Interest stops on April 15 of the next year. A payment goes first to the oldest shortfall. For 2026 the rate is 6% from April to June and 7% after. If income came unevenly, the federal Schedule AI can lower the early payments. You complete it for South Carolina.").
		Paragraph("The department can waive the penalty for a casualty or disaster. It can also do so for retirement after 62 or disability, with reasonable cause. The 2026 estimate worksheet predates the 2026 rate cut. Use the new rates for this year's tax before comparing it with these targets.").
		VersusFederal("South Carolina differs from federal in four ways that can change a number. Its threshold is $100 rather than $1,000. There is no penalty on an underpayment from income earned in another state. That state must have withheld its tax. South Carolina has its own legal holidays. A return can be filed and paid by the end of January. Then earlier shortfalls stop accruing on January 15.").
		Source("S.C. Code §§ 12-6-3910, 12-54-25 and 12-54-55; 26 U.S.C. § 6654").
		Source("SC1040ES, Individual Declaration of Estimated Tax, 2026 (Rev. 10/13/25)").
		Source("SC2210, Underpayment of Estimated Tax by Individuals, Estates, and Trusts, 2025 (Rev. 12/02/25)").
		Source("SC Information Letter #26-19 (interest rate); SC Revenue Procedure #23-1 (legal holidays)").
		Source("Act No. 110 of 2026; SC Information Letter #26-20").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
