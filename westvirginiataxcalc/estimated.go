package westvirginiataxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// West Virginia's estimated tax rules, W. Va. Code §§ 11-21-55, 11-21-56,
// 11-10-17a and 11-10-18a, as the shared [estimated.EstimatedTax] interface.
//
// The figure follows the Tax Division's penalty form, Form IT-210 in the 2025
// IT-140 booklet, which is the latest published, and the IT-140ESI (Sept
// 2023) for who must pay. The result's Divergences say where the Code reads
// otherwise. The bill histories end in 1998 for § 11-10-18a, 1990 for
// § 11-21-55, 1961 for § 11-21-56 and 2015 for § 11-10-17a, so neither form
// predates the law it describes.

const jurisdiction = estimated.WestVirginia

// EstimatedFacts are the facts West Virginia's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
//
// For West Virginia, [estimated.Figures.Tax] is the tax after credits, Form
// IT-210 Part I line 3: Form IT-140 line 8 less lines 9 and 17 to 21.
type EstimatedFacts struct {
	priorUSResident, priorUSResidentAnswered bool
}

// PriorYearUSCitizenOrResident reports whether the filer was a citizen or
// resident of the United States throughout last year, and whether the caller
// said. The exception for a filer who had no West Virginia liability last
// year needs it (W. Va. Code § 11-10-18a(e)(2)(C)). It is United States
// residence, not West Virginia residence, so it is not
// [estimated.PriorReturn.FullYearResident].
func (e EstimatedFacts) PriorYearUSCitizenOrResident() (yes, answered bool) {
	return e.priorUSResident, e.priorUSResidentAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorYearUSCitizenOrResident(yes bool) *EstimatedFactsBuilder {
	b.e.priorUSResident, b.e.priorUSResidentAnswered = yes, true
	return b
}

// Build has nothing to refuse: every combination of answers is possible.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is the tax after credits less withholding below which no
	// penalty applies, IT-210 Part I line 5. smallShare is the IT-140ESI's
	// 10% of this year's tax below which no payments are required.
	threshold  money.USCents
	smallShare money.Rate

	// thisYearShare and priorYearShare are IT-210 Part I lines 6 and 7.
	thisYearShare, priorYearShare money.Rate

	// noLiabilityLimit is this year's tax above which a zero tax last year no
	// longer gives a target of zero, IT-210 Part I line 8.
	noLiabilityLimit money.USCents

	dues     []estimated.Date
	calendar estimated.Calendar

	// lateStartAfter is the day after which a requirement first met gets
	// fewer installments under § 11-21-55(d), which IT-210 does not apply.
	lateStartAfter estimated.Date

	// fileAndPayBy is the last day of January, § 11-10-18a(i), before it is
	// moved for a weekend or holiday.
	fileAndPayBy estimated.Date

	stop       estimated.Date
	periodEnds []estimated.Date

	// applicable is IT-210 Part II line 11: the share of each period's
	// annualised tax due by its date, cumulatively.
	applicable []money.Rate

	// daily is the factor IT-210 prints for each calendar year.
	daily estimated.FactorSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are IT-210 Part IV line 1: line 8 of Part I divided by four.
// Withholding is spread across the dates in the same shares.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns West Virginia's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return westVirginiaEstimated{y}, nil
}

type westVirginiaEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// required is each installment IT-210 Part IV line 1 asks for: a quarter
	// of Part I line 8, or Part II line 19 under the annualised method.
	required []money.USCents

	excusedJanuary bool

	// priorChosen is a target from last year's tax, and lineEightOnly a zero
	// last year that IT-210 line 8 accepts and the Code's exception does not.
	priorChosen   bool
	lineEightOnly bool

	notes working.Notes
}

func (r westVirginiaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("westvirginiataxcalc: the facts carry %T, not westvirginiataxcalc.EstimatedFacts", s)
	}
}

func (r westVirginiaEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("westvirginiataxcalc: these are West Virginia's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("westvirginiataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case int64(f.FarmShare())*3 >= 20_000:
		return fmt.Errorf("%w: a farmer with two-thirds of gross income from farming makes one payment by January 15 at 66 2/3%%, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("westvirginiataxcalc: IT-210 Part II period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the Tax Division's documents and the Code part ways
// on a rule every West Virginia computation uses. Each affects the household
// only when its facts reach the rule.
func (r westVirginiaEstimated) divergences(f estimated.Facts, a assessment) ([]working.Divergence, error) {
	y := r.y
	scheduled := a.requirement.TargetKnown() && !a.exempt
	short := false
	if scheduled {
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return nil, err
		}
		for _, l := range ledger {
			short = short || l.Shortfall() > 0
		}
	}
	met, filed := f.RequirementFirstMet(), f.ReturnFiled()
	payBy := y.calendar.NextBusinessDay(y.fileAndPayBy)
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		// The Code's test reads income other than wages, which the facts do
		// not split out; the penalty is the same under both.
		working.NewDivergence("Who must make estimated payments").
			Form("Payments are required when tax less West Virginia withholding is $600 or more. The exception is a balance under 10% of this year's tax.",
				"IT-140ESI (Sept 2023), Who must make estimated tax payments?; 2025 IT-140 booklet, Credit for Estimated Tax").
			Statute("The test is your West Virginia income other than withheld wages. Payments are required if it's expected to exceed $400 plus personal exemptions. The law has no 10% test, and $600 is only the penalty exception.",
				"W. Va. Code § 11-21-55(a); § 11-10-18a(e)(1)").
			Uses(working.ReadingForm).Affects(a.requirement.Required()),
		working.NewDivergence("The prior-year test").
			Form("The target is the smaller of 90% of this year's tax and 100% of last year's. Each payment is a quarter of it. A quarter that misses the prior-year amount is charged only on what it missed.",
				"IT-210 (2025), Part I lines 6 to 8 and Part IV line 1").
			Statute("The shortfall is measured against a quarter of 90% of this year's tax. Last year's tax excuses a quarter only if payments to date reach its share.",
				"W. Va. Code § 11-10-18a(b)(1) and (d)(1)").
			Uses(working.ReadingForm).Affects(a.priorChosen && short),
		working.NewDivergence("No liability last year").
			Form("A zero tax last year means no payments while this year's tax is $5,000 or less. There's no condition of US citizenship or residence.",
				"IT-210 (2025), Part I line 8").
			Statute("The penalty is excused only if last year's West Virginia tax was zero. Last year must be 12 months, spent as a US citizen or resident. This year's tax must be under $5,000. Otherwise 90% of this year's tax applies. The booklet says the same.",
				"W. Va. Code § 11-10-18a(d)(1) and (e)(2); 2025 IT-140 booklet, Exceptions to the Penalty").
			Uses(working.ReadingForm).Affects(a.lineEightOnly),
		working.NewDivergence("A requirement first met after April 1").
			Form("A quarter of the year's amount is due by every date for every filer.",
				"IT-210 (2025), Part II line 15 and Part IV line 1").
			Statute("First meeting the requirement after April 1 gives fewer, equal installments. That's three from June 15, two from September 15, or all on January 15. The penalty divides 90% of the tax by that number.",
				"W. Va. Code §§ 11-21-55(d), 11-21-56(a); § 11-10-18a(b)(1)").
			Uses(working.ReadingForm).Affects(scheduled && !met.IsZero() && met.After(y.lateStartAfter)),
		working.NewDivergence("A return filed and paid by January 31").
			Form("The booklet excuses the January payment if you file and pay by February 1. The IT-140ESI says by January 31.",
				"2025 IT-140 booklet, Exceptions to the Penalty; IT-210 (2025), Part IV line 2 instructions; IT-140ESI (Sept 2023), For Your Information").
			Statute("Filing and paying in full by the end of January excuses the January payment. A weekend or holiday moves that date to the next business day. Jan 31, 2027 is a Sunday, so the date is Monday, Feb 1, 2027.",
				"W. Va. Code § 11-10-18a(i); § 2-2-1(e)").
			Uses(working.ReadingStatute).Affects(scheduled && filed == payBy && payBy != y.fileAndPayBy),
		working.NewDivergence("The daily interest factor").
			Form("Each day is charged at a printed factor of 0.000315. That's 11.5% divided by 365, rounded to six places.",
				"IT-210 (2025), Part IV Section B and line 11 instructions").
			Statute("The charge is the yearly rate on the shortfall for its period, 11.5% for 2026. That's 0.00031507 a day.",
				"W. Va. Code §§ 11-10-17a(c), 11-10-18a(a); TSD-365").
			Uses(working.ReadingForm).Affects(short),
		// The IT-140ESI's 8% floor is below the 2026 rate, and no six-month
		// rate is published, so both readings give the same rate.
		working.NewDivergence("How the rate is set").
			Form("The IT-140ESI says the rate is set every six months and can't be less than 8%.",
				"IT-140ESI (Sept 2023), What if I don't pay enough estimated tax?").
			Statute("Since 2017 the rate is set once a year, from January 1. It's the adjusted prime rate plus three points. The 8% floor applies only to years before 2017.",
				"W. Va. Code § 11-10-17a(a) and (c)").
			Uses(working.ReadingStatute).Affects(false),
		working.NewDivergence("Withholding on the dates withheld").
			Form("One box in Part I puts all withholding on the dates it was withheld. Otherwise a quarter of it counts on each date.",
				"IT-210 (2025), Part I and Part IV line 2 instructions").
			Statute("Wage withholding and other withholding may each be put on their actual dates. Either may instead be spread evenly, separately from the other.",
				"W. Va. Code § 11-10-18a(g)").
			Uses(working.ReadingForm).Affects(scheduled && f.WithheldTotal() > 0),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// assess answers the three questions and adds the divergences.
func (r westVirginiaEstimated) assess(f estimated.Facts) (assessment, error) {
	a, err := r.figure(f)
	if err != nil {
		return assessment{}, err
	}
	ds, err := r.divergences(f, a)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(ds...)
	return a, nil
}

// figure is the requirement and the required installments.
func (r westVirginiaEstimated) figure(f estimated.Facts) (assessment, error) {
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

	// IT-210 Part I line 6: 90% of this year's tax after credits.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("W. Va. Code § 11-10-18a(b)(1); IT-210 (2025), Part I lines 3 and 6", fmt.Sprintf("%v of this year's West Virginia tax after credits of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// IT-210 Part I lines 7 and 8: last year's tax after credits, for a
	// 12-month return, and the rule for a zero.
	prior, hasPrior := f.PriorYear()
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
	}
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	noLiability := false
	var taxNeededFor string // a sentence for a rule that could not be applied without this year's tax
	// Questions asked only when no exception sets the target.
	type ask struct {
		kind             working.PositionKind
		question, effect string
	}
	var asks []ask
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d West Virginia return", last))
		asks = append(asks, ask{working.PositionPriorYearReturn, fmt.Sprintf("What was the tax after credits on your %d West Virginia return?", last),
			"With it, 100% of last year's tax could be your target."})
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d West Virginia return covered %d months, not a full 12", last, prior.Months()))
	case prior.NotRequiredToFile():
		// IT-210 line 7 has no figure without a return. The booklet's
		// Exceptions to the Penalty, which agrees with § 11-10-18a(e)(2),
		// covers a filer who was not required to file.
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d West Virginia return", last))
		us, answered := extras.PriorYearUSCitizenOrResident()
		switch {
		case !taxKnown:
			taxNeededFor = fmt.Sprintf("With no West Virginia tax for %d, you're excused if it's under %s.", last, y.noLiabilityLimit.Whole())
		case tax >= y.noLiabilityLimit:
		case !answered:
			asks = append(asks, ask{working.PositionJurisdictionFact, fmt.Sprintf("Were you a US citizen or resident all of %d?", last),
				fmt.Sprintf("If so, a zero tax that year and under %s this year excuses the penalty.", y.noLiabilityLimit.Whole())})
		case us:
			noLiability = true
			a.notes.Rule("W. Va. Code § 11-10-18a(e)(2); 2025 IT-140 booklet, Exceptions to the Penalty", fmt.Sprintf("no return was required for the 12 months of %d, spent as a United States citizen or resident, and this year's tax of %v is less than %v, so no penalty can arise",
				last, tax, y.noLiabilityLimit))
		}
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d West Virginia return", last))
	case priorTax == 0 && !taxKnown:
		priorOption = unavailable(fmt.Sprintf("A zero %d West Virginia tax counts only if this year's tax is %s or less", last, y.noLiabilityLimit.Whole()))
		taxNeededFor = fmt.Sprintf("Your zero %d tax counts only if it's %s or less.", last, y.noLiabilityLimit.Whole())
	case priorTax == 0 && tax > y.noLiabilityLimit:
		priorOption = unavailable(fmt.Sprintf("A zero %d West Virginia tax doesn't count if this year's tax is over %s", last, y.noLiabilityLimit.Whole()))
	case priorTax == 0:
		noLiability = true
		us, answered := extras.PriorYearUSCitizenOrResident()
		a.lineEightOnly = !(answered && us && tax < y.noLiabilityLimit)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0)
		a.notes.Rule("IT-210 (2025), Part I lines 7 and 8", fmt.Sprintf("the %d return showed no tax, and this year's tax of %v is not more than %v, so line 8 is zero and no penalty can arise",
			last, tax, y.noLiabilityLimit))
	default:
		amount := y.priorYearShare.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, amount)
		a.notes.Rule("W. Va. Code § 11-10-18a(d)(1); IT-210 (2025), Part I line 7", fmt.Sprintf("%v of the %v tax after credits on the 12-month %d return is %v", y.priorYearShare, priorTax, last, amount))
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
		effect := fmt.Sprintf("With it, 90%% of this year's tax could be your target and the %s test used.", y.threshold.Whole())
		if taxNeededFor != "" {
			effect += " " + taxNeededFor
		}
		asks = append(asks, ask{working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d West Virginia tax after credits to be?", y.year), effect})
	}

	owed := tax - withheld
	switch {
	case taxKnown && owed < y.threshold:
		a.exempt = true
		under, err := estimated.ExceptionBelow("Tax less withholding", owed, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your West Virginia tax of %s less %s withheld leaves %s. That's under %s, so no estimated payments are needed.",
			tax.Whole(), withheld.Whole(), owed.Whole(), y.threshold.Whole())
		switch {
		case owed < 0:
			why = fmt.Sprintf("Withholding of %s covers your West Virginia tax of %s. That %s, so no estimated payments are needed.", withheld.Whole(), tax.Whole(), estimated.Leaves(owed))
		case owed == 0:
			why = fmt.Sprintf("Your withholding of %s covers your West Virginia tax of %s. No estimated payments are needed.", withheld.Whole(), tax.Whole())
		case owed.Whole() == y.threshold.Whole():
			why = fmt.Sprintf("Your West Virginia tax less withholding is just under %s. No estimated payments are needed.", y.threshold.Whole())
		}
		rb.Required(false, why).Exempt(under)
		a.notes.Rule("W. Va. Code § 11-10-18a(e)(1); IT-210 (2025), Part I line 5", fmt.Sprintf("%v of tax less %v of withholding is %v, under %v, so there is no penalty", tax, withheld, owed, y.threshold))
	case noLiability:
		a.exempt = true
		none, err := estimated.NewException("No West Virginia tax due last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no West Virginia tax for %d, and your %d tax is %s. No estimated payments are needed, and no penalty can arise.", last, y.year, tax.Whole())).Exempt(none)
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your %d return doesn't give a target. Your target is 90%% of your %d West Virginia tax, unless an exception applies.", last, y.year)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s reaches %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax is.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		why := fmt.Sprintf("Your target is %v of your %d West Virginia tax, %s. That keeps you clear of a penalty whatever your %d tax turns out to be. ",
			priorOpt.Percent(), last, target.Whole(), y.year)
		if withheld > 0 {
			why += fmt.Sprintf("After %s of withholding, that's %s of estimated payments. Pay a quarter by each date.", withheld.Whole(), (target - withheld).Whole())
		} else {
			why += "Pay it in estimated payments, a quarter by each date."
		}
		rb.Required(true, why).Choose(chosen)
	case withheld >= target && owed < y.smallShare.Of(tax):
		why := fmt.Sprintf("Your West Virginia withholding of %s leaves %s of tax. That's less than %v of your %s tax, so no estimated payments are needed. Withholding already reaches the %s required.",
			withheld.Whole(), owed.Whole(), y.smallShare, tax.Whole(), target.Whole())
		if owed.Whole() == y.smallShare.Of(tax).Whole() {
			why = fmt.Sprintf("Your West Virginia withholding leaves just less than %v of your %s tax. No estimated payments are needed. Withholding already reaches the %s required.",
				y.smallShare, tax.Whole(), target.Whole())
		}
		rb.Required(false, why).Choose(chosen)
		a.notes.Rule("IT-140ESI (Sept 2023), Who must make estimated tax payments?", fmt.Sprintf("%v of tax after withholding is less than %v of the %v tax, so no payments are required", owed, y.smallShare, tax))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your West Virginia withholding of %s covers the year's %s target. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	case withheld == 0:
		rb.Required(true, fmt.Sprintf("Your West Virginia tax of %s is %s or more, with no withholding. You need estimated payments of %s, a quarter by each date.",
			tax.Whole(), y.threshold.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your West Virginia tax of %s less %s withheld leaves %s. That's %s or more, and withholding is short of the %s required. You need estimated payments of %s, a quarter by each date.",
			tax.Whole(), withheld.Whole(), owed.Whole(), y.threshold.Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	if !a.exempt {
		for _, q := range asks {
			a.notes.Ask(q.kind, q.question, q.effect)
		}
	}
	if c, ok := a.requirement.Chosen(); ok && c.Basis() == estimated.BasisPriorYearTax {
		a.priorChosen = true
	}
	if a.exempt || !a.requirement.TargetKnown() {
		a.required = make([]money.USCents, len(a.dues))
		return a, nil
	}

	cum, err := estimated.CumulativeShares(target, quarters)
	if err != nil {
		return assessment{}, err
	}
	a.required = estimated.Increments(cum)
	a.notes.Rule("W. Va. Code § 11-21-56(a)(1); IT-210 (2025), Part IV line 1", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", target, a.required[0],
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	// IT-210 Part II: the annualised method with its recapture, lines 12 to
	// 19. The caller supplies line 10 for each period.
	if f.Elections().Annualised() {
		periods := f.Periods()
		taxes := make([]money.USCents, len(periods))
		for i, p := range periods {
			taxes[i] = p.AnnualisedTax()
		}
		regular := a.required
		if a.required, err = estimated.Annualised(taxes, y.applicable, regular); err != nil {
			return assessment{}, err
		}
		a.notes.Rule("W. Va. Code § 11-10-18a(d)(2); IT-210 (2025), Part II lines 11 to 19", fmt.Sprintf("under the annualised method 22.5%%, 45%%, 67.5%% and 90%% of each period's annualised tax, less what earlier dates required, against the regular quarter plus any amount carried, give %v, %v, %v and %v",
			a.required[0], a.required[1], a.required[2], a.required[3]))
	}

	if met := f.RequirementFirstMet(); !met.IsZero() && met.After(y.lateStartAfter) {
		a.notes.Rule("IT-210 (2025), Part IV line 1", fmt.Sprintf("the requirement was first met on %s, after %s; IT-210 asks every filer for a quarter by every date, so the quarters are used",
			met.Long(), y.lateStartAfter.Long()))
	}

	// January 31, 2027 is a Sunday. § 2-2-1(e) moves it to February 1, the
	// date the 2025 booklet prints.
	payBy := y.calendar.NextBusinessDay(y.fileAndPayBy)
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(payBy) {
		a.excusedJanuary = true
		a.required[3] = 0
		a.notes.Rule("W. Va. Code § 11-10-18a(i), § 2-2-1(e); IT-210 (2025), Part IV line 2 instructions",
			fmt.Sprintf("the return was filed and paid on %s, by %s, so there is no penalty on the January installment", filed.Long(), payBy.Long()))
	}
	return a, nil
}

func (r westVirginiaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies the credits to the required installments, oldest shortfall
// first, as IT-210 Part IV Section B says.
func (r westVirginiaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	var notes working.Notes
	var credits []estimated.Payment
	var err error
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
		notes.Rule("W. Va. Code § 11-10-18a(g)(1); IT-210 (2025), Part I box and Part IV line 2 instructions",
			fmt.Sprintf("the %v withheld counts on the dates it was withheld, as the box in Part I elects; estimated payments count on the day they were paid", f.WithheldTotal()))
	} else {
		credits, err = estimated.Credits(f, a.dues, quarters)
		notes.Rule("W. Va. Code § 11-10-18a(g)(1); IT-210 (2025), Part IV line 2 instructions",
			fmt.Sprintf("the %v withheld is treated as paid a quarter on each due date, whenever it was actually withheld; estimated payments count on the day they were paid", f.WithheldTotal()))
	}
	if err != nil {
		return nil, notes, err
	}
	for _, p := range f.Payments() {
		if p.FromPriorYear() {
			notes.Rule("IT-210 (2025), Part IV line 2 instructions", fmt.Sprintf("last year's overpayment of %v applied forward is treated as paid on %s, as IT-210 does for a return filed by its due date",
				p.Amount(), a.dues[0].Long()))
			break
		}
	}
	ledger, err := estimated.Apply(a.dues, a.required, credits)
	if err != nil {
		return nil, notes, err
	}
	return ledger, notes, nil
}

func (r westVirginiaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d West Virginia return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
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

// installments are the ledger as due dates. A January installment the
// return excuses is marked so.
func installments(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(l.NeededBy()).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if a.excusedJanuary && i == 3 {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r westVirginiaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("westvirginiataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d West Virginia return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	schedule, err := installments(a, ledger)
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
	if !a.taxKnown {
		pb.UpperBound()
	}
	var unrated estimated.Date
	for _, l := range ledger {
		if l.Shortfall() <= 0 {
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
	notes.Rule("W. Va. Code § 11-10-18a(a) and (c); IT-210 (2025), Part IV Section B", fmt.Sprintf("each shortfall is charged the daily factor for its calendar year, by the day, from its due date until paid or %s; "+
		"each charge is rounded to the whole dollar, as the booklet's entries are; the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !a.taxKnown {
		notes.Rule("IT-210 (2025), Part I line 8", fmt.Sprintf("this year's West Virginia tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be", a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("W. Va. Code § 11-10-18a(c)(2); IT-210 (2025), Part IV Section B", fmt.Sprintf("the return was filed with the whole balance paid on %s, so the penalty stops that day", stop.Long()))
	}
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", r.short(unrated.AddDays(-1))))
	}
	switch {
	case waiverAnswered && waiver == estimated.WaiverCasualty:
		notes.Rule("W. Va. Code § 11-10-18a(e)(3)", "the Tax Commissioner may waive the penalty for a casualty, disaster or other unusual circumstance; the penalty shown is before any waiver")
	case waiverAnswered && waiver == estimated.WaiverRetiredOrDisabled:
		notes.Rule("W. Va. Code § 11-10-18a(e); 2025 IT-140 booklet, Waiver of Penalty", "West Virginia has no waiver for retirement or disability; the Tax Division can waive for reasonable cause on a written request, and the penalty shown is before any waiver")
	case p.Total() > 0:
		notes.Rule("W. Va. Code § 11-10-18a(e)(3); 2025 IT-140 booklet, Waiver of Penalty", "the Tax Division can waive the penalty for a casualty or for reasonable cause on a written request; the penalty shown is before any waiver")
	}
	if p.Total() > 0 {
		if o, ok := waiverOutcome(f, &notes); ok {
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// waiverOutcome decides the waiver asked for. § 11-10-18a(e)(3) gives the
// federal casualty waiver. There is no retirement or disability waiver; the
// 2025 IT-140 booklet offers a waiver for reasonable cause, and neither says
// whether retiring or becoming disabled is one. It reports false when no
// waiver was asked for.
func waiverOutcome(f estimated.Facts, notes *working.Notes) (estimated.WaiverOutcome, bool) {
	kind, answered := f.Positions().Waiver()
	if !answered || kind == estimated.WaiverNone {
		return estimated.WaiverOutcome{}, false
	}
	if kind == estimated.WaiverCasualty {
		return estimated.StandardWaiver(f, "the Tax Commissioner", 62)
	}
	notes.Ask(working.PositionUnsettledRule, "", "West Virginia's rules don't say if retiring or disability is reasonable cause. Reasonable cause lets the Tax Division waive the penalty.")
	return estimated.WaiverUndecided("West Virginia has no waiver for retirement or disability. The Tax Division can waive the penalty if you show reasonable cause."), true
}

// short prints a date as "Aug 31" in the tax year and "Jan 15, 2027" outside it.
func (r westVirginiaEstimated) short(d estimated.Date) string {
	if d.Year() == r.y.year {
		return fmt.Sprintf("%s %d", d.Month().String()[:3], d.Day())
	}
	return fmt.Sprintf("%s %d, %d", d.Month().String()[:3], d.Day(), d.Year())
}

func (r westVirginiaEstimated) Explanation() estimated.Explanation { return westVirginiaExplanation }

// westVirginiaExplanation is written by hand and checked against
// research/estimated-payments/wv.md.
var westVirginiaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"West Virginia expects estimated payments if withholding leaves $600 or more due. The penalty is interest on each quarter's shortfall, at 11.5% a year for 2026.").
		Paragraph("You avoid the penalty if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. A quarter of it is due by each date. Last year's figure counts only if that return covered a full 12 months. There is no 110% tier for higher incomes. A zero tax last year counts only while this year's tax is $5,000 or less. Then the penalty form asks for nothing. Above that, you need 90% of this year's tax.").
		Paragraph("The due dates are April 15, June 15, September 15 and the next January 15. A date on a weekend or a legal holiday becomes the next business day. Withholding counts as paid evenly across the four dates. You can instead show when it was actually withheld. Filing and paying in full by the end of January excuses the January payment. For 2026 that means by Monday, February 1, 2027.").
		Paragraph("The penalty is charged daily on each date's shortfall. It runs until the shortfall is paid or April 15 of the next year. A payment goes first to the oldest shortfall. The rate is set once a year at the prime rate plus three points. The form prints it as a daily factor, 0.000315 for 2026. Form IT-210's annualized method can lower early quarters for uneven income. The Tax Division can waive the penalty for a casualty or for reasonable cause.").
		VersusFederal("West Virginia differs from federal in ways that change a number. Its threshold is $600, not the federal $1,000. It has no 110% prior-year tier. A zero tax last year counts only while this year's tax is $5,000 or less. The rate is set once a year, 11.5% for 2026, not each quarter. There's no waiver for retirement or disability.").
		Source("W. Va. Code §§ 11-21-55, 11-21-56, 11-10-17a, 11-10-18a and 2-2-1").
		Source("West Virginia Tax Division, IT-140ESI, Estimated Tax Payments Instructions (Sept 2023)").
		Source("West Virginia Tax Division, 2025 Personal Income Tax Forms and Instructions, including Form IT-210").
		Source("West Virginia Tax Division, TSD-365, How Interest and Additions to Tax are Assessed").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
