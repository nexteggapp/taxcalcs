package nebraskataxcalc

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Nebraska's estimated tax rules, Neb. Rev. Stat. §§ 77-2769 and 77-2790 and
// 316 Neb. Admin. Code ch. 20, as the shared [estimated.EstimatedTax]
// interface.
//
// Nebraska follows the federal rules: four 25% installments, the lesser of
// 90% of this year's tax or 100% of last year's, 110% above $150,000 of last
// year's federal AGI, and payments applied to the earliest installment first.
// It differs in the $500 threshold, an exception for first-time filers, and
// the rate: the delinquent tax rate under § 45-104.02, 8% a year, simple.
//
// The figure follows the 2026 Form 2210N, and the result's Divergences say
// where the statute or the regulation reads otherwise. No amendment to
// §§ 77-2769, 77-2790 or 45-104.02 since 2010 changed these rules
// (research/estimated-payments/ne.md), so the form does not predate the law
// it describes.
//
// Line 22a of Form 2210N charges "the number of days in 2026 on line 21" and
// line 22b is reserved, as the 2025 form did for 2025. Line 21 counts the
// days to April 15, 2027, and the regulation runs the penalty to the return's
// due date; Revenue Ruling 99-26-2 keeps 8% through 2028, so every day is
// charged at 8% whichever line it falls on.
//
// Not built: tax paid for a nonresident by a pass-through entity or
// fiduciary, which REG-20-007.05 counts on the last day of the entity's year;
// a caller passes it as a dated payment.

// jurisdiction is Nebraska's postal code.
const jurisdiction = estimated.Nebraska

// EstimatedFacts are the facts Nebraska's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	firstTime, firstTimeAnswered bool
	citizen, citizenAnswered     bool
	farmerLast, farmerAnswered   bool
	moves, movesAnswered         bool
}

// FirstTimeFiler reports whether this is the filer's first Nebraska income
// tax return, which Form 2210N excuses from the penalty, and whether the
// caller said.
func (e EstimatedFacts) FirstTimeFiler() (first, answered bool) {
	return e.firstTime, e.firstTimeAnswered
}

// PriorYearUSCitizenOrResident reports whether the filer was a U.S. citizen
// or resident for all of last year, which Form 2210N's no-liability
// exception needs, and whether the caller said.
func (e EstimatedFacts) PriorYearUSCitizenOrResident() (all, answered bool) {
	return e.citizen, e.citizenAnswered
}

// FarmerLastYear reports whether at least two-thirds of last year's gross
// income was from farming, ranching or fishing, which puts this year under
// the farmers' rules, and whether the caller said.
func (e EstimatedFacts) FarmerLastYear() (farmer, answered bool) {
	return e.farmerLast, e.farmerAnswered
}

// JanuaryThirtyFirstMoves reports whether a return filed and paid on Monday,
// February 1, 2027 counts as filed by January 31, a Sunday, and whether the
// caller said. Form 2210N does not say.
func (e EstimatedFacts) JanuaryThirtyFirstMoves() (moves, answered bool) {
	return e.moves, e.movesAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) FirstTimeFiler(first bool) *EstimatedFactsBuilder {
	b.e.firstTime, b.e.firstTimeAnswered = first, true
	return b
}

func (b *EstimatedFactsBuilder) PriorYearUSCitizenOrResident(all bool) *EstimatedFactsBuilder {
	b.e.citizen, b.e.citizenAnswered = all, true
	return b
}

func (b *EstimatedFactsBuilder) FarmerLastYear(farmer bool) *EstimatedFactsBuilder {
	b.e.farmerLast, b.e.farmerAnswered = farmer, true
	return b
}

func (b *EstimatedFactsBuilder) JanuaryThirtyFirstMoves(moves bool) *EstimatedFactsBuilder {
	b.e.moves, b.e.movesAnswered = moves, true
	return b
}

func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is Form 2210N line 6.
	threshold money.USCents

	thisYearShare, priorYearShare, priorYearHigh money.Rate
	step, stepSeparate                           money.USCents

	shares     []money.Rate
	annualised []money.Rate
	dues       []estimated.Date
	calendar   estimated.Calendar
	periodEnds []estimated.Date

	// fileAndPayBy and deemedPaid are the Form 2210N line 9 rule: the balance
	// paid with a return filed by fileAndPayBy counts as paid on deemedPaid.
	fileAndPayBy, deemedPaid estimated.Date

	stop  estimated.Date
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Nebraska's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return nebraskaEstimated{y}, nil
}

type nebraskaEstimated struct{ y estimatedYear }

type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents
	filedByJan31 bool
	notes        working.Notes
}

func (r nebraskaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("nebraskataxcalc: the facts carry %T, not nebraskataxcalc.EstimatedFacts", s)
	}
}

func (r nebraskaEstimated) check(f estimated.Facts, extras EstimatedFacts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("nebraskataxcalc: these are Nebraska's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("nebraskataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: farmers, ranchers and fishermen pay once by January 15 at 66 2/3%% or file and pay by March 1, which is not built", estimated.ErrUnsupported)
	}
	if farmer, _ := extras.FarmerLastYear(); farmer {
		return fmt.Errorf("%w: two-thirds of last year's gross income from farming, ranching or fishing puts this year under the farmers' rules, which are not built", estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("nebraskataxcalc: annualization period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms and the statute or regulation
// part ways on a rule every Nebraska computation uses. They are in
// research/estimated-payments/ne.md and checked there against both.
func (r nebraskaEstimated) divergences(t touches) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The $500 threshold").
			Form("No penalty when your tax after credits less withholding is under $500.",
				"Form 2210N (2026), line 6 and Exceptions to the Penalty").
			Statute("The statute agrees at $500, and the regulation still says $300.",
				"Neb. Rev. Stat. § 77-2769(1)(a), amended by Laws 2008, LB915; 316 Neb. Admin. Code ch. 20, REG-20-001.01 and REG-20-007.06B (March 7, 2006)").
			Uses(working.ReadingForm).Affects(t.betweenThresholds),
		working.NewDivergence("No liability last year").
			Form(fmt.Sprintf("The exception needs no Nebraska liability for a full twelve-month %d. It also needs U.S. citizenship or residence for the whole year.", r.y.year-1),
				"Form 2210N (2026), Exceptions to the Penalty, item 1").
			Statute("The regulation's exception needs only zero liability for a full 12-month year.",
				"316 Neb. Admin. Code ch. 20, REG-20-007.06A").
			Uses(working.ReadingForm).Affects(t.noLiabilityUnproved),
		working.NewDivergence("First-time filers").
			Form("A first-time filer for Nebraska income tax has no penalty.",
				"Form 2210N (2026), Exceptions to the Penalty, item 2; 2026 Form 1040N-ES").
			Statute("Neither the statute nor the regulation's list of exceptions has it.",
				"Neb. Rev. Stat. §§ 77-2769, 77-2790; 316 Neb. Admin. Code ch. 20, REG-20-007.06").
			Uses(working.ReadingForm).Affects(t.firstTime),
		working.NewDivergence("A requirement that arises late").
			Form("Form 2210N charges 25% in every column. The 1040N-ES worksheet has its own rule for a late starter. The first payment is ½, ¾ or all of the amount.",
				"Form 2210N (2026), line 10; 2026 Form 1040N-ES, worksheet line 19").
			Statute("The required amount is divided by the number of the year's installment dates.",
				"316 Neb. Admin. Code ch. 20, REG-20-007.01").
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
	betweenThresholds   bool
	noLiabilityUnproved bool
	firstTime           bool
	lateStart           bool
}

// regulationThreshold is the $300 REG-20-001.01 still prints, which only
// decides whether the threshold divergence affects a household.
const regulationThreshold = 300 * money.Dollar

func (r nebraskaEstimated) assess(f estimated.Facts) (assessment, error) {
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
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Was at least 2/3 of your %d gross income from farming, ranching or fishing?", last),
				"If so, Nebraska's farmers' rules apply instead.")
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
		a.notes.Rule("316 Neb. Admin. Code ch. 20, REG-20-007.01; Form 2210N (2026), line 4", fmt.Sprintf("%v of this year's Nebraska tax after credits of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	step := y.step
	if f.FilingStatus() == filing.MarriedFilingSeparately {
		step = y.stepSeparate
	}
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	askAGI := false
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Nebraska return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Nebraska return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Nebraska return covered %d months, not a full year", last, prior.Months()))
	default:
		agi, known := priorFigures.FederalAGI()
		rate := y.priorYearShare
		why := fmt.Sprintf("%d federal AGI of %v is not more than %v", last, agi, step)
		switch {
		case !known:
			rate = y.priorYearHigh
			why = fmt.Sprintf("%d federal AGI was not given", last)
			askAGI = true
		case agi > step:
			rate = y.priorYearHigh
			why = fmt.Sprintf("%d federal AGI of %v is more than %v", last, agi, step)
		}
		amount := rate.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, rate, amount)
		a.notes.Rule("316 Neb. Admin. Code ch. 20, REG-20-007.01; Form 2210N (2026), line 7", fmt.Sprintf("%s, so the prior-year test is %v of the %v tax on the %d return, or %v", why, rate, priorTax, last, amount))
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

	noLiability := hasPrior && prior.Months() == 12 && ((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	citizen, citizenAnswered := extras.PriorYearUSCitizenOrResident()
	if noLiability && !citizenAnswered {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Were you a U.S. citizen or resident for all of %d?", last),
			fmt.Sprintf("If so, having no Nebraska tax liability for %d means no penalty for %d.", last, y.year))
	}
	first, _ := extras.FirstTimeFiler()

	switch {
	case first:
		exception, err := estimated.NewException("First Nebraska income tax return").Build()
		if err != nil {
			return assessment{}, err
		}
		a.exempt = true
		rb.Required(false, "Nebraska charges a first-time filer no penalty. This is your first Nebraska income tax return. No estimated payments are required.").Exempt(exception)
		a.notes.Rule("Form 2210N (2026), Exceptions to the Penalty, item 2", "a first-time filer for Nebraska income tax owes no penalty")
		a.notes.Ask(working.PositionUnsettledRule, "", "Nebraska's first-time filer exception is on Forms 2210N and 1040N-ES. No statute or regulation has it, and the figures follow the forms.")
	case noLiability && citizenAnswered && citizen:
		exception, err := estimated.NewException("No Nebraska tax due last year").Build()
		if err != nil {
			return assessment{}, err
		}
		a.exempt = true
		rb.Required(false, fmt.Sprintf("You had no Nebraska tax liability for a full twelve-month %d. You were also a U.S. citizen or resident all year. That means no estimated payments are required for %d.", last, y.year)).Exempt(exception)
		a.notes.Rule("Form 2210N (2026), Exceptions to the Penalty, item 1; 316 Neb. Admin. Code ch. 20, REG-20-007.06A", fmt.Sprintf("the %d return showed no liability for a full 12 months, so there is no penalty for %d", last, y.year))
	case taxKnown && tax-withheld < y.threshold:
		exception, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		a.exempt = true
		under := fmt.Sprintf(", under %s", y.threshold.Whole())
		if tax < withheld {
			under = ""
		}
		rb.Required(false, fmt.Sprintf("Your Nebraska tax after credits is %s, and %s was withheld. That %s%s. So no estimated payments are needed.",
			tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld), under)).Exempt(exception)
		a.notes.Rule("Neb. Rev. Stat. § 77-2769(1)(a); Form 2210N (2026), line 6", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no penalty", tax, withheld, tax-withheld, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target needs your %d Nebraska tax or a full-year %d Nebraska return. There's no target yet.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. That keeps you clear of a penalty whatever your %d tax turns out to be. After %s of withholding, you need %s of estimated payments. A quarter is due by each date.",
			priorOpt.Percent(), last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Nebraska withholding of %s covers the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Nebraska tax after credits is %s, and %s was withheld. That leaves %s, which is %s or more. You need %s of estimated payments, a quarter by each due date.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.threshold.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// Under an exception none of these answers can change the target.
	if !a.exempt {
		if !hasPrior {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Nebraska return, and what was its tax?", last),
				fmt.Sprintf("With it, 100%% of your %d tax could be a lower target than 90%% of this year's.", last))
		}
		if highShare, lowShare := y.priorYearHigh, y.priorYearShare; askAGI {
			a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d federal adjusted gross income?", last),
				fmt.Sprintf("Until then, the target is %v of your %d tax. With AGI of %s or less, it would be %v.", highShare, last, step.Whole(), lowShare))
		}
		if !taxKnown {
			if priorOpt.Available() {
				a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Nebraska tax to be?", y.year),
					fmt.Sprintf("With it, 90%% of it could be a lower target than your %d tax. It also decides whether the %s threshold excuses you.", last, y.threshold.Whole()))
			} else {
				a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Nebraska tax to be?", y.year),
					fmt.Sprintf("Without it there's no target, as there's no full-year %d Nebraska return.", last))
			}
		}
	}

	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("Neb. Rev. Stat. § 77-2769(5); Form 2210N (2026), lines 9 and 10", fmt.Sprintf("25%% of %v is due on each of %s, %s, %s and %s", a.requirement.Target(),
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() && !a.exempt {
		periods := f.Periods()
		taxes := make([]money.USCents, len(periods))
		for i, p := range periods {
			taxes[i] = p.AnnualisedTax()
		}
		ann, err := estimated.Annualised(taxes, y.annualised, a.installments)
		if err != nil {
			return assessment{}, err
		}
		a.installments = ann
		a.notes.Rule("Form 2210N (2026), Nebraska Income Tax on Annualized Income; Form 2210 Schedule AI", fmt.Sprintf("under the annualized method, figured as on the federal schedule Form 2210N points to, the installments are %v, %v, %v and %v",
			ann[0], ann[1], ann[2], ann[3]))
	}

	// The line 9 rule. January 31, 2027 is a Sunday, and Form 2210N does not
	// say whether the day moves.
	fileBy := y.fileAndPayBy
	moves, movesAnswered := extras.JanuaryThirtyFirstMoves()
	if moves {
		fileBy = y.calendar.NextBusinessDay(fileBy)
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && !a.exempt {
		if !movesAnswered && filed.After(fileBy) && !filed.After(y.calendar.NextBusinessDay(fileBy)) {
			a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("%s is a Sunday, and Form 2210N doesn't say if the date moves. The figures treat a return filed and paid on %s as late.", fileBy.Long(), filed.Long()))
		}
		if !filed.After(fileBy) {
			a.filedByJan31 = true
			a.notes.Rule("Form 2210N (2026), instructions for line 9 and Exceptions to the Penalty, Note", fmt.Sprintf("the return was filed and paid on %s, by %s, so the balance counts as paid on %s", filed.Long(), fileBy.Long(), y.deemedPaid.Long()))
		}
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	t := touches{
		betweenThresholds:   !first && !(noLiability && citizenAnswered && citizen) && taxKnown && tax-withheld >= regulationThreshold && tax-withheld < y.threshold,
		noLiabilityUnproved: noLiability && !first && !(citizenAnswered && citizen),
		firstTime:           first,
	}
	if met := f.RequirementFirstMet(); !a.exempt && !met.IsZero() && met.After(a.dues[0]) {
		t.lateStart = true
	}
	divergences, err := r.divergences(t)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

func (r nebraskaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// credits are withholding and payments, earliest first.
func (r nebraskaEstimated) credits(f estimated.Facts, a assessment) ([]estimated.Payment, working.Notes, error) {
	var notes working.Notes
	credits, err := estimated.Credits(f, a.dues, r.y.shares)
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
		notes.Rule("316 Neb. Admin. Code ch. 20, REG-20-007.04; Form 2210N (2026), line 11", fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("316 Neb. Admin. Code ch. 20, REG-20-007.04; Form 2210N (2026), line 11", fmt.Sprintf("one-fourth of the %v withheld is treated as paid on each due date, whenever it was actually withheld", f.WithheldTotal()))
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	sort.SliceStable(credits, func(i, j int) bool { return credits[i].On().Before(credits[j].On()) })
	return credits, notes, nil
}

func (r nebraskaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Nebraska return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	credits, notes, err := r.credits(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := r.schedule(a, credits)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments, with the credits applied earliest first.
func (r nebraskaEstimated) schedule(a assessment, credits []estimated.Payment) ([]estimated.Installment, error) {
	ledger, err := estimated.Apply(a.dues, a.installments, credits)
	if err != nil {
		return nil, err
	}
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(l.NeededBy()).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if a.filedByJan31 && i == 3 {
			b.Excused()
		}
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r nebraskaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("nebraskataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Nebraska return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	waiver, waiverAnswered := f.Positions().Waiver()
	credits, notes, err := r.credits(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule, err := r.schedule(a, credits)
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

	// Form 2210N lines 12 to 22: each column's underpayment, this one's and
	// any left from earlier, from its due date until paid or the next due
	// date, and the last column to April 15.
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	var unrated estimated.Date
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
	required := make([]money.USCents, len(a.dues))
	var sum money.USCents
	for k := range a.dues {
		sum += a.installments[k]
		required[k] = sum
	}
	cols, err := estimated.Columns(a.dues, required, credits, y.stop, stop)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	for _, col := range cols {
		if col.Under() <= 0 {
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
		notes.Rule("Form 2210N (2026), line 8", fmt.Sprintf("this year's Nebraska tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	notes.Rule("316 Neb. Admin. Code ch. 20, REG-20-007.03; Neb. Rev. Stat. § 45-104.02; Form 2210N (2026), lines 19 to 23", fmt.Sprintf("each column's underpayment is charged 8%% a year, simple, by the day ÷ 365, from its due date until paid or the next due date, and the last column to %s; "+
		"the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", unrated.AddDays(-1).Long()))
	}
	// REG-20-007.03 and Form 2210N grant the federal pair of waivers.
	if o, ok := estimated.StandardWaiver(f, "Nebraska's Department of Revenue", 62); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Do you want a waiver for an unusual event, retiring after 62 or disability?",
			"A casualty or disaster is an unusual event, and all three need reasonable cause. Until Nebraska waives it, the figures include the full penalty.")
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r nebraskaEstimated) Explanation() estimated.Explanation { return nebraskaExplanation }

// nebraskaExplanation is written by hand and checked against
// research/estimated-payments/ne.md.
var nebraskaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Nebraska expects estimated payments once tax after credits exceeds withholding. The excess must be $500 or more. The penalty is 8% a year, simple interest, on each quarter's shortfall.").
		Paragraph("Your target is the lesser of two figures. They are 90% of this year's Nebraska tax and 100% of last year's. You are clear if withholding and estimated payments reach the target. A quarter is due by April 15, June 15, September 15 and January 15. As federally, 100% becomes 110% if last year's federal AGI was over $150,000. That limit is $75,000 if married filing separately. Last year's figure needs a full-year return. Withholding counts as paid a quarter on each date. That holds unless you show when it was withheld.").
		Paragraph("There is no penalty if this year's tax less withholding is under $500. There is none if you had no Nebraska tax due on last year's full-year return. That case also needs U.S. citizenship or residence all last year. Your first Nebraska return has no penalty either. Filing and paying by January 31 counts the balance as paid on January 15.").
		Paragraph("The penalty runs daily from each due date until the shortfall is paid. It stops on April 15 of the next year. Each payment goes first to the oldest shortfall. The rate is Nebraska's delinquent tax rate, set every other year. It is 8% for 2026 and 2027. The department can waive the penalty for a casualty or disaster. It can also waive it for retiring after 62 or disability with reasonable cause.").
		VersusFederal("Nebraska's threshold is $500 rather than $1,000. A first-time Nebraska filer has no penalty. The rate is a flat 8% a year, set every other year, not a quarterly rate.").
		Source("Neb. Rev. Stat. §§ 77-2769, 77-2790, 45-104.02").
		Source("316 Neb. Admin. Code ch. 20, REG-20-001 and REG-20-007").
		Source("Form 2210N, Individual Underpayment of Estimated Tax, 2026, and its instructions").
		Source("2026 Form 1040N-ES").
		Source("Revenue Ruling 99-26-2, Interest Rate Assessed on State Taxes (August 7, 2026)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()

// date is a shorthand for the year files.
func date(y int, m time.Month, d int) estimated.Date { return estimated.MustDate(y, m, d) }
