package coloradotaxcalc

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Colorado's estimated tax rules, C.R.S. § 39-22-605, as the shared
// [estimated.EstimatedTax] interface. The interest rate is set under
// C.R.S. § 39-21-110.5.
//
// Colorado's section is written on the federal pattern, but it is Colorado's
// own text, not an adoption of 26 U.S.C. § 6654, and nothing here borrows from
// the federal package. The current-year test is 70%; the penalty is interest
// at Colorado's regular rate for each calendar year; the annualised method is
// the department's own schedule on DR 0204 Part 4, open only to a filer who
// annualises federally.
//
// The figure follows the department's forms, DR 0104EP for 2026 and DR 0204
// for 2025, and the result's Divergences say where C.R.S. § 39-22-605 reads
// otherwise. The section was last amended in 2022 (SB 22-233), and the Red
// Books for 2025 and 2026 list no change to it or to § 39-21-110.5, so no
// form here predates the law it describes. The one place the figure does not
// follow the form is the interest rate: DR 0204 (2025) prints the rates for a
// 2025 underpayment, and a 2026 underpayment is charged 2026's and 2027's.

// jurisdiction is Colorado's postal code.
const jurisdiction = estimated.Colorado

// EstimatedFacts are the facts Colorado's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorFarmer, priorFarmerAnswered             bool
	federalAnnualised, federalAnnualisedAnswered bool
}

// PriorYearFarmerOrFisher reports whether last year's return showed at least
// two-thirds of gross income from farming or fishing, which makes the filer a
// farmer or fisherman this year under C.R.S. § 39-22-605(2)(a)(II), and
// whether the caller said. This year's share is [estimated.Facts.FarmShare]
// and [estimated.Facts.FishShare].
func (e EstimatedFacts) PriorYearFarmerOrFisher() (farmer, answered bool) {
	return e.priorFarmer, e.priorFarmerAnswered
}

// FederalAnnualised reports whether the filer elected annualised installments
// for federal income tax, which C.R.S. § 39-22-605(6)(d) requires before the
// annualised method is open in Colorado, and whether the caller said.
func (e EstimatedFacts) FederalAnnualised() (annualised, answered bool) {
	return e.federalAnnualised, e.federalAnnualisedAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorYearFarmerOrFisher(farmer bool) *EstimatedFactsBuilder {
	b.e.priorFarmer, b.e.priorFarmerAnswered = farmer, true
	return b
}

func (b *EstimatedFactsBuilder) FederalAnnualised(annualised bool) *EstimatedFactsBuilder {
	b.e.federalAnnualised, b.e.federalAnnualisedAnswered = annualised, true
	return b
}

// Build has nothing to refuse: each fact is a yes or a no.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is DR 0204 Exception 2's statutory exemption: no penalty when
	// tax less withholding is not more than it.
	threshold money.USCents

	// thisYearPercent is the current-year test; priorPercent and
	// priorPercentHigh the prior-year test at or below and above the step in
	// last year's federal AGI.
	thisYearPercent, priorPercent, priorPercentHigh money.Rate
	priorYearStep, priorYearStepSeparate            money.USCents

	dues         []estimated.Date
	calendar     estimated.Calendar
	fileAndPayBy estimated.Date
	stop         estimated.Date

	// periodEnds, factors and annualisedPercentages are DR 0204 Part 4 lines
	// 20, 22 and 25, and taxRate the rate line 24 applies.
	periodEnds            []estimated.Date
	factors               []money.Rate
	annualisedPercentages []money.Rate
	taxRate               money.Rate

	// rates is the § 39-21-110.5 regular rate, one period for each calendar
	// year, as far as it has been published.
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are C.R.S. § 39-22-605(6)(a) and DR 0204 line 7: each installment
// is a quarter of the required annual payment.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Colorado's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return coloradoEstimated{y}, nil
}

type coloradoEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// installments are each installment's own amount: a quarter of the
	// target, or DR 0204 line 26 under the annualised method.
	installments []money.USCents
	excused      bool

	notes working.Notes
}

func (r coloradoEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("coloradotaxcalc: the facts carry %T, not coloradotaxcalc.EstimatedFacts", s)
	}
}

func (r coloradoEstimated) check(f estimated.Facts, extras EstimatedFacts) error {
	farmerLastYear, _ := extras.PriorYearFarmerOrFisher()
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("coloradotaxcalc: these are Colorado's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("coloradotaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher(), farmerLastYear:
		return fmt.Errorf("%w: C.R.S. § 39-22-605(10) gives farmers and fishermen one installment by January 15 at 50%%, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("coloradotaxcalc: DR 0204 Part 4 period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// affected says which divergences change these facts' figures or answers.
type affected struct {
	threshold, rates, noLiability, waiver, moved bool
}

// divergences are where the department's forms and C.R.S. § 39-22-605 part
// ways on a rule every Colorado computation uses. They are found in
// research/estimated-payments/co.md and checked against the statute's text in
// testdata/.
func (r coloradoEstimated) divergences(in affected) ([]working.Divergence, error) {
	var out []working.Divergence
	var rates []string
	for _, rp := range r.y.rates.Periods() {
		rates = append(rates, fmt.Sprintf("%v for %d", rp.Rate(), rp.From().Year()))
	}
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Tax of exactly $1,000 after withholding").
			Form("No penalty when tax less withholding is $1,000 or less. DR 0204 subtracts a $1,000 exemption and charges nothing at zero. DR 0104EP asks for payments only above $1,000.",
				"DR 0204 (09/25/25), Part 1, Exception 2, lines 2(a) to 3; DR 0104EP (06/10/25), General Rule").
			Statute("No penalty only when tax less withholding is less than $1,000. Exactly $1,000 is subject to it.",
				"C.R.S. § 39-22-605(7)(a)").
			Uses(working.ReadingForm).Affects(in.threshold),
		working.NewDivergence("The interest rates DR 0204 prints").
			Form(fmt.Sprintf("Lines 15 and 18 charge 12%% to December 31 and 11%% after it. Those are the rates for %d and %d, which fit a %d underpayment.", r.y.year-1, r.y.year, r.y.year-1),
				"DR 0204 (09/25/25), Part 3, lines 15 and 18").
			Statute("The rate is fixed for each calendar year as the prime rate plus three points. It is set each July 1 for the next January 1: "+strings.Join(rates, " and ")+".",
				"C.R.S. §§ 39-22-605(3), 39-21-110.5(2), (3); CDOR, Tax Topics: Penalties and Interest").
			Uses(working.ReadingStatute).Affects(in.rates),
		working.NewDivergence("No liability last year").
			Form("The form lists two exceptions. One is a farmer or fisherman who files and pays by March 1. The other is tax of $1,000 or less after withholding. No liability last year isn't one of them.",
				"DR 0204 (09/25/25), Part 1 and its instructions").
			Statute("No penalty if last year was twelve months and you had no liability for it. You must also have been a Colorado resident throughout it.",
				"C.R.S. § 39-22-605(7)(b)").
			Uses(working.ReadingForm).Affects(in.noLiability),
		working.NewDivergence("Waivers for good cause, retirement or disability").
			Form("The form and its instructions describe no waiver.",
				"DR 0204 (09/25/25) and its instructions").
			Statute("The executive director can find that the underpayment was due to good cause. There is no penalty to that extent. No penalty either if you retired after 62 or became disabled this year or last. That waiver needs reasonable cause, not willful neglect.",
				"C.R.S. § 39-22-605(7)(c), (d)").
			Uses(working.ReadingForm).Affects(in.waiver),
		working.NewDivergence("Due dates on a weekend or holiday").
			Form(fmt.Sprintf("A due date on a weekend or legal holiday moves to the next business day. No %d due date moves.", r.y.year),
				"DR 0104EP (06/10/25), Due Dates; DR 0204 (09/25/25), Part 3 instructions; CDOR, Individual Income Tax | Estimated Payments").
			Statute("A due date on a legal federal holiday moves to the adjusted federal due date. Weekends aren't mentioned.",
				"C.R.S. § 39-22-605(5)(c)").
			Uses(working.ReadingForm).Affects(in.moved),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func separate(s filing.Status) bool { return s == filing.MarriedFilingSeparately }

func (r coloradoEstimated) assess(f estimated.Facts) (assessment, error) {
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	if err := r.check(f, extras); err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	var in affected

	step := y.priorYearStep
	if separate(f.FilingStatus()) {
		step = y.priorYearStepSeparate
	}
	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	last := y.year - 1

	// 70% of this year's tax. § 39-22-605(6)(b)(I).
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, y.thisYearPercent.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("C.R.S. § 39-22-605(6)(b)(I); DR 0204 (09/25/25), line 4(b)", fmt.Sprintf("%v of this year's Colorado tax of %v is %v", y.thisYearPercent, tax, thisYear.Amount()))
	}

	// Last year's tax: 100%, or 110% above the step in last year's federal
	// AGI, for a filed twelve-month Colorado return.
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
	}
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorPercent, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	askAGI := false
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Colorado return", last))
	case prior.NotRequiredToFile():
		priorOption = unavailable(fmt.Sprintf("You weren't required to file a %d Colorado return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Colorado return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Colorado return covered %d months, not a full year", last, prior.Months()))
	default:
		pf, _ := prior.Figures()
		priorAGI, known := pf.FederalAGI()
		rate := y.priorPercent
		why := fmt.Sprintf("%d federal AGI of %v is not more than %v", last, priorAGI, step)
		switch {
		case !known:
			rate = y.priorPercentHigh
			why = fmt.Sprintf("%d federal AGI was not given", last)
			askAGI = true
		case priorAGI > step:
			rate = y.priorPercentHigh
			why = fmt.Sprintf("%d federal AGI of %v is more than %v", last, priorAGI, step)
		}
		amount := rate.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, rate, amount)
		a.notes.Rule("C.R.S. § 39-22-605(6)(b)(II), (c); DR 0204 (09/25/25), lines 5(a) to 5(c)", fmt.Sprintf("%s, so the prior-year test is %v of the %v tax on the %d Colorado return, or %v", why, rate, priorTax, last, amount))
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
	// § 39-22-605(7)(b): no liability last year. DR 0204 does not list it, so
	// it is reported rather than applied; see the divergences.
	noLiability := hasPrior && prior.Months() == 12 &&
		((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	if noLiability {
		resident, answered := prior.FullYearResident()
		in.noLiability = resident || !answered
		switch {
		case answered && resident:
			a.notes.Rule("C.R.S. § 39-22-605(7)(b)", fmt.Sprintf("you had no Colorado liability for %d, a twelve-month year, as a resident throughout it, so the statute excuses any penalty for %d; "+
				"DR 0204 does not list this exception, so the figures here are the form's, and the exception is yours to claim", last, y.year))
		case !answered:
			a.notes.Rule("C.R.S. § 39-22-605(7)(b)", fmt.Sprintf("you had no Colorado liability for %d, a twelve-month year; if you were a Colorado resident throughout it, the statute excuses any penalty for %d, "+
				"though DR 0204 does not list the exception, so the figures here are the form's", last, y.year))
		}
	}

	spread := "Pay a quarter by each date."
	if federal, answered := extras.FederalAnnualised(); f.Elections().Annualised() && (federal || !answered) {
		spread = "Spread them over the annualised installments on DR 0204 Part 4."
	}
	switch {
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target needs your %d Colorado tax, as your %d return can't set one.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already meets %v of your %d Colorado tax. That comes to %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d Colorado tax, %s. Meeting it keeps you clear of a penalty whatever your %d tax is. After %s of withholding, you need %s of estimated payments. %s",
			priorOpt.Percent(), last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole(), spread)).Choose(chosen)
	case tax-withheld <= y.threshold:
		a.exempt = true
		in.threshold = tax-withheld == y.threshold
		exception, err := estimated.ExceptionAtMost("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		atMost := "That's " + y.threshold.Whole() + " or less, so no estimated payments are needed."
		if tax < withheld {
			atMost = "No estimated payments are needed."
		}
		rb.Required(false, fmt.Sprintf("Your Colorado tax of %s less %s withheld %s. %s",
			tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld), atMost)).Exempt(exception)
		a.notes.Rule("DR 0204 (09/25/25), Part 1, Exception 2; C.R.S. § 39-22-605(7)(a)", fmt.Sprintf("%v of tax less the %v exemption and %v withheld is not more than zero, so there is no penalty", tax, y.threshold, withheld))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Colorado withholding is %s. That already meets the %s required for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Colorado tax of %s less %s of withholding leaves %s. That's more than %s, and withholding is short of the %s required. You need %s of estimated payments. %s",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.threshold.Whole(), target.Whole(), (target-withheld).Whole(), spread)).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// These answers change the target. An exception sets the target, so they
	// are asked only when none applies.
	if !a.exempt && !hasPrior {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Colorado return, and what was its tax?", last),
			fmt.Sprintf("With it, %v of last year's tax may set a target below %v of this year's.", y.priorPercent, y.thisYearPercent))
	}
	if !a.exempt && askAGI {
		a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d federal AGI?", last),
			fmt.Sprintf("Until then, the target is %v of last year's tax rather than %v. The higher figure applies to federal AGI over %s.", y.priorPercentHigh, y.priorPercent, step.Whole()))
	}
	if !a.exempt && !taxKnown {
		effect := "Without it, the target can only come from last year's tax."
		if !priorOpt.Available() {
			effect = fmt.Sprintf("Without it, there's no target, because your %d return can't set one.", last)
		}
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Colorado tax to be?", y.year), effect)
	}
	if !a.exempt {
		if _, answered := extras.PriorYearFarmerOrFisher(); !answered {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did your %d return show two-thirds of gross income from farming or fishing?", last),
				"Until then, the figures assume not, as farmers and fishermen have other rules.")
		}
	}

	// The due dates. § 39-22-605(5)(b) fixes them; the forms move one that
	// falls on a weekend or legal holiday.
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			in.moved = true
			a.notes.Rule("DR 0104EP (06/10/25), Due Dates", fmt.Sprintf("the installment due %s falls on a weekend or legal holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
		}
	}
	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), quarters)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("C.R.S. § 39-22-605(5), (6)(a); DR 0204 (09/25/25), line 7", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", a.requirement.Target(), a.installments[0],
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() && !a.exempt {
		if err := r.annualise(f, extras, &a); err != nil {
			return assessment{}, err
		}
	}

	// § 39-22-605(9): filing and paying by January 31 excuses the fourth
	// installment. January 31, 2027 is a Sunday. Neither the statute nor
	// DR 0204 says the date moves, so it is not moved here.
	filed := f.ReturnFiled()
	if !filed.IsZero() && !filed.After(y.fileAndPayBy) && !a.exempt {
		a.excused = true
		a.notes.Rule("C.R.S. § 39-22-605(9); DR 0204 (09/25/25), Part 3 instructions", fmt.Sprintf("the return was filed and paid on %s, by %s, so no penalty is computed on the fourth installment", filed.Long(), y.fileAndPayBy.Long()))
	}
	if moved := y.calendar.NextBusinessDay(y.fileAndPayBy); !a.exempt && filed.After(y.fileAndPayBy) && !filed.After(moved) {
		a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("DR 0204 and the law don't say if the %s deadline, a %v, moves. Filing and paying on %s, the next business day, is counted late.",
			shortDate(y.fileAndPayBy, y.year), y.fileAndPayBy.Weekday(), shortDate(filed, y.year)))
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	// Which divergences change these figures turns on the shortfalls.
	in.noLiability = in.noLiability && !a.exempt
	if !a.exempt {
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return assessment{}, err
		}
		short := false
		for _, l := range ledger {
			short = short || l.Shortfall() > 0
		}
		waiver, _ := f.Positions().Waiver()
		in.rates, in.waiver = short, short && waiver != estimated.WaiverNone
	}
	divergences, err := r.divergences(in)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

// shortDate is "Aug 31" inside the tax year and "Jan 15, 2027" outside it.
func shortDate(d estimated.Date, year int) string {
	m := d.Month().String()[:3]
	if d.Year() == year {
		return fmt.Sprintf("%s %d", m, d.Day())
	}
	return fmt.Sprintf("%s %d, %d", m, d.Day(), d.Year())
}

// annualise replaces the installments with DR 0204 Part 4 line 26: the
// period's Colorado taxable income, times the factor, times the tax rate,
// times the applicable percentage, less the installments before it. The form
// enters line 26 on line 7 as it stands. It does not compare it with the
// regular installment or carry a reduction forward, as the federal Schedule
// AI does, and § 39-22-605(6)(d) leaves the method to the department.
func (r coloradoEstimated) annualise(f estimated.Facts, extras EstimatedFacts, a *assessment) error {
	y := r.y
	federal, answered := extras.FederalAnnualised()
	switch {
	case answered && !federal:
		a.notes.Rule("C.R.S. § 39-22-605(6)(d); DR 0204 (09/25/25), Part 4 instructions", "the annualised method is open only to a filer who elects annualised installments for federal income tax, which you did not, so the regular installments are used")
		return nil
	case !answered:
		a.notes.Ask(working.PositionJurisdictionFact, "Did you elect annualised installments for federal income tax?",
			"Colorado allows annualising only if you did; until then, the figures assume so.")
	}
	periods := f.Periods()
	out := make([]money.USCents, len(periods))
	var before money.USCents
	lines := make([]string, len(periods))
	for i, p := range periods {
		annualised := y.factors[i].Of(p.Income())
		annualTax := max(0, y.taxRate.Of(annualised))
		out[i] = max(0, y.annualisedPercentages[i].Of(annualTax)-before)
		before += out[i]
		applied := fmt.Sprintf("%v to %s, times %v, is %v, taxed at %v is %v, and %v of that less earlier installments is %v",
			p.Income(), p.End().Long(), factorString(y.factors[i]), annualised, y.taxRate, annualTax, y.annualisedPercentages[i], out[i])
		lines[i] = applied
	}
	a.installments = out
	a.notes.Rule("C.R.S. § 39-22-605(6)(d); DR 0204 (09/25/25), Part 4, lines 20 to 26", "under the annualised method each installment is DR 0204 line 26: "+strings.Join(lines, "; "))
	return nil
}

// factorString prints an annualisation factor as the form does: 4, 2.4, 1.5.
func factorString(r money.Rate) string {
	whole, frac := int(r)/10_000, int(r)%10_000
	if frac == 0 {
		return fmt.Sprint(whole)
	}
	return strings.TrimRight(fmt.Sprintf("%d.%04d", whole, frac), "0")
}

func (r coloradoEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies the credits to the installments, with the fourth set to
// nothing when filing early excused it.
func (r coloradoEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
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
		notes.Rule("C.R.S. § 39-22-605(8)(a)", fmt.Sprintf("the %v withheld is credited on the dates it was actually withheld, which the filer must establish", f.WithheldTotal()))
	} else {
		notes.Rule("C.R.S. § 39-22-605(8)(a)", fmt.Sprintf("the %v withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld", f.WithheldTotal()))
	}
	notes.Rule("C.R.S. § 39-22-605(4)(c); DR 0104EP (06/10/25), Carryforward of Estimated Overpayment",
		"a payment is credited against the unpaid installments in the order they fall due, and last year's overpayment carried forward counts toward the first")
	return ledger, notes, nil
}

func (r coloradoEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Colorado return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
func (r coloradoEstimated) installments(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
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

func (r coloradoEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("coloradotaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Colorado return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
		notes.Rule("C.R.S. § 39-22-605(6)(b)", fmt.Sprintf("this year's Colorado tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and %v of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, y.thisYearPercent, p.Total()))
	}
	if paidInFull {
		notes.Rule("C.R.S. § 39-22-605(4)(b)(II)", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	var rates []string
	for _, rp := range y.rates.Periods() {
		rates = append(rates, fmt.Sprintf("%v from %s to %s", rp.Rate(), rp.From().Long(), rp.To().Long()))
	}
	notes.Rule("C.R.S. §§ 39-22-605(3), (4), 39-21-110.5; DR 0204 (09/25/25), Part 3, lines 12 to 19", fmt.Sprintf("each shortfall is charged simple interest at Colorado's regular rate for each calendar year (%s), by the day over the days in the year, from its due date until paid or %s; "+
		"the discounted rate does not apply to estimated tax (C.R.S. § 39-21-109(5)); the penalty to %s is %v", strings.Join(rates, "; "), y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", shortDate(unrated.AddDays(-1), 0)))
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking the executive director to waive the penalty?",
			"The executive director can waive it for good cause for the underpayment. Retiring after 62 or becoming disabled, with reasonable cause, also counts.")
	}
	if waiver == estimated.WaiverCasualty {
		notes.Rule("C.R.S. § 39-22-605(7)(c)", "Colorado has no waiver for a casualty or disaster as such; it is waived only if the executive director finds the underpayment was due to good cause")
	}
	if o, ok := coloradoWaiver(f); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// coloradoWaiver decides the waiver asked for under § 39-22-605(7)(c) and
// (d). Retirement after 62 or disability is the federal waiver. There is no
// casualty waiver as such, only one for good cause.
func coloradoWaiver(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	kind, answered := f.Positions().Waiver()
	switch {
	case !answered || kind == estimated.WaiverNone:
		return estimated.WaiverOutcome{}, false
	case kind == estimated.WaiverCasualty:
		return estimated.WaiverEligible("Colorado has no waiver for a casualty or disaster as such, only for good cause. The executive director decides how far you show good cause for the shortfall."), true
	}
	return estimated.StandardWaiver(f, "the executive director", 62)
}

func (r coloradoEstimated) Explanation() estimated.Explanation { return coloradoExplanation }

// coloradoExplanation is written by hand and checked against
// research/estimated-payments/co.md.
var coloradoExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Colorado expects estimated payments when more than $1,000 of tax will be due. That is the tax left after withholding and credits. The penalty is interest on each quarter's shortfall, at Colorado's own rate.").
		Paragraph("You avoid the penalty if withholding and estimated payments reach a target. The target is the smaller of 70% of this year's tax or 100% of last year's. Above $150,000 of federal adjusted gross income last year, the 100% is 110%. The limit is $75,000 if you are married filing separately. Last year's figure needs a Colorado return for a full twelve-month year.").
		Paragraph("Pay a quarter by April 15, June 15, September 15 and the next January 15. A date on a weekend or legal holiday shifts to the next business day. Withholding counts as paid evenly across the four dates. You can instead show when it was actually withheld. An overpayment carried forward from last year counts toward April. Filing and paying in full by January 31 removes the January payment's penalty.").
		Paragraph("The penalty form looks at this year's tax less withholding. It charges nothing when that is $1,000 or less. The statute says less than $1,000, so the two differ only at exactly $1,000. The statute also excuses you if you had no Colorado tax liability last year. You must have lived in Colorado all of that year. The department can also waive the penalty for good cause. It can waive it too if you retired after 62 or became disabled. None of those three is on the form, DR 0204, so you have to ask for them.").
		Paragraph("The penalty is interest at Colorado's regular rate, set once each calendar year. That is 11% for 2026 and 10% for 2027. It runs by the day from each due date until the shortfall is paid. It stops on April 15 of the next year at the latest. A shortfall carried past December 31 changes rate on January 1. The lower discounted rate Colorado gives on other late tax doesn't apply. If you annualise uneven income federally, you can annualise for Colorado too. DR 0204 Part 4 uses 17.5%, 35%, 52.5% and 70% of the tax on annualised income.").
		VersusFederal("Colorado differs from federal where a number can change. The current-year test is 70%, not 90%. Last year's figure needs a filed Colorado return. The rate is Colorado's own, set once a year rather than every quarter. It is 11% for 2026 and 10% for 2027. You can annualise only if you annualise federally. The no-liability exception needs Colorado residence all year. The form omits it.").
		Source("C.R.S. §§ 39-22-605, 39-21-109, 39-21-110.5").
		Source("DR 0104EP, Individual Estimated Income Tax, 2026 (06/10/25)").
		Source("DR 0204, Computation of Penalty Due Based on Underpayment of Colorado Individual Estimated Tax, 2025 (09/25/25)").
		Source("CDOR, Tax Topics: Penalties and Interest").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
