package northcarolinataxcalc

import (
	"errors"
	"fmt"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// North Carolina's estimated tax rules, N.C. Gen. Stat. § 105-163.15, with the
// rate under § 105-241.21, as the shared [estimated.EstimatedTax] interface.
// North Carolina calls the charge interest.
//
// North Carolina follows the federal rules closely: four 25% installments,
// the lesser of 90% of this year's tax or 100% of last year's, the $1,000
// threshold the statute takes from 26 U.S.C. § 6654(e), and payments applied
// to the earliest installment first. It differs in having no 110% tier, a
// no-liability exception with no twelve-month or residence condition, no
// waiver for casualty, retirement or disability, and a rate the Secretary
// sets for each half-year.
//
// The figure follows Form D-422 for 2025, the latest published, with the
// 2026 dates and rates, and the result's Divergences say where the statute
// reads otherwise. § 105-163.15 has not been amended since 2007
// (research/estimated-payments/nc.md), so the form does not predate the law
// it describes.
//
// The rate from January 1, 2027 is not published, so those days are not
// charged and the penalty reports the gap.

// jurisdiction is North Carolina's postal code.
const jurisdiction = estimated.NorthCarolina

// EstimatedFacts are the facts North Carolina's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	farmerLast, farmerAnswered bool
	moves, movesAnswered       bool
}

// FarmerLastYear reports whether at least two-thirds of the gross income on
// last year's return was from farming or fishing, which puts this year under
// the farmers' rules, and whether the caller said.
func (e EstimatedFacts) FarmerLastYear() (farmer, answered bool) {
	return e.farmerLast, e.farmerAnswered
}

// JanuaryThirtyFirstMoves reports whether a return filed and paid on Monday,
// February 1, 2027 counts as filed by January 31, a Sunday, and whether the
// caller said. No source says.
func (e EstimatedFacts) JanuaryThirtyFirstMoves() (moves, answered bool) {
	return e.moves, e.movesAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

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

	// threshold is D-422 Part I line 6, the § 6654(e) amount § 105-163.15(f)
	// adopts.
	threshold money.USCents

	thisYearShare, priorYearShare money.Rate
	shares                        []money.Rate
	annualised                    []money.Rate
	dues                          []estimated.Date
	calendar                      estimated.Calendar
	periodEnds                    []estimated.Date

	// fileAndPayBy and deemedPaid are D-422 line 17: the tax paid with a
	// return filed by fileAndPayBy counts in the January column.
	fileAndPayBy, deemedPaid estimated.Date

	stop  estimated.Date
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns North Carolina's estimated tax rules for a tax year,
// or [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return northCarolinaEstimated{y}, nil
}

type northCarolinaEstimated struct{ y estimatedYear }

type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents
	filedByJan31 bool
	notes        working.Notes
}

func (r northCarolinaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("northcarolinataxcalc: the facts carry %T, not northcarolinataxcalc.EstimatedFacts", s)
	}
}

func (r northCarolinaEstimated) check(f estimated.Facts, extras EstimatedFacts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("northcarolinataxcalc: these are North Carolina's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("northcarolinataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: farmers and fishermen make one installment by January 15 or file and pay by March 1, which is not built", estimated.ErrUnsupported)
	}
	if farmer, _ := extras.FarmerLastYear(); farmer {
		return fmt.Errorf("%w: two-thirds of last year's gross income from farming or fishing puts this year under the farmers' rules, which are not built", estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("northcarolinataxcalc: D-422A period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms and the statute part ways on
// a rule every North Carolina computation uses. The weekend rule and the
// farmer test are in research/estimated-payments/nc.md; the January 31 rule
// was found reading D-422 against § 105-163.15(h). Each is marked as
// affecting these facts only where the two readings give them different
// figures.
func (r northCarolinaEstimated) divergences(f estimated.Facts, a assessment, extras EstimatedFacts) ([]working.Divergence, error) {
	moved := false
	for i, d := range a.dues {
		moved = moved || d != r.y.dues[i]
	}
	lateStart := false
	if first := f.RequirementFirstMet(); !first.IsZero() && first.After(a.dues[0]) && !a.exempt {
		lateStart = true
	}
	_, farmerAnswered := extras.FarmerLastYear()
	farmOpen := f.FarmShare()+f.FishShare() > 0 && !farmerAnswered
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("A return filed and paid by January 31").
			Form("Tax paid with a return filed by Jan 31 goes in the January 15 column. It counts as paid on Jan 15. Interest on every earlier shortfall stops then.",
				"Form D-422 (2025), Line 17 instructions").
			Statute("No interest is charged on a short fourth installment. Earlier installments are charged until paid.",
				"N.C. Gen. Stat. § 105-163.15(h)").
			Uses(working.ReadingForm).Affects(a.filedByJan31),
		working.NewDivergence("Due dates on a weekend or holiday").
			Form("A due date on a Saturday, Sunday or holiday moves to the next business day.",
				"Form D-422 (2025), Part III; Form NC-40 (2026), When To Pay").
			Statute("The installments are due April 15, June 15, September 15 and January 15. There is no shift for a weekend.",
				"N.C. Gen. Stat. § 105-163.15(c)").
			Uses(working.ReadingForm).Affects(moved),
		working.NewDivergence("A requirement that arises late").
			Form("NC-40 says to pay half your estimate in June if you first need to pay then. It says three-quarters if you first need to pay in September. D-422 charges a quarter in every column.",
				"Form NC-40 (2026), When To Pay; Form D-422 (2025), line 16").
			Statute("Every installment is 25% of the required annual payment. Only the annualized method lowers one.",
				"N.C. Gen. Stat. § 105-163.15(d), (e)").
			Uses(working.ReadingStatute).Affects(lateStart),
		working.NewDivergence("Who counts as a farmer").
			Form("NC-40 tests two-thirds of this year's estimated gross income. D-422 tests 2024 or 2025.",
				"Form NC-40 (2026), Farmers and Fishermen; Form D-422 (2025), Exceptions").
			Statute("The test is this year's gross income or the gross income on last year's return.",
				"N.C. Gen. Stat. § 105-163.15(i)(3)").
			Uses(working.ReadingForm).Affects(farmOpen),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// shortDate is "Jan 15" inside the tax year and "Jan 15, 2027" outside it.
func shortDate(d estimated.Date, year int) string {
	m := d.Month().String()[:3]
	if d.Year() == year {
		return fmt.Sprintf("%s %d", m, d.Day())
	}
	return fmt.Sprintf("%s %d, %d", m, d.Day(), d.Year())
}

// targetWhy says what the target leaves to pay after withholding, for a Why.
func targetWhy(target, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("Your target is %s, a quarter by each due date.", target.Whole())
	}
	return fmt.Sprintf("Your target is %s, which leaves %s after withholding. Pay a quarter of that by each due date.", target.Whole(), (target - withheld).Whole())
}

// leftWhy says what estimated payments the target leaves after
// withholding, for a Why.
func leftWhy(target, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("That's %s of estimated payments, a quarter by each due date.", target.Whole())
	}
	return fmt.Sprintf("After %s of withholding, you need %s of estimated payments. Pay a quarter by each due date.", withheld.Whole(), (target - withheld).Whole())
}

// owedWhy says what tax less withholding leaves, as a sentence for a Why.
func owedWhy(tax, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("Your North Carolina tax is %s, with no withholding.", tax.Whole())
	}
	return fmt.Sprintf("North Carolina tax of %s less %s withheld %s.", tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
}

func (r northCarolinaEstimated) assess(f estimated.Facts) (assessment, error) {
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
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did farming or fishing make up two-thirds of your %d return's gross income?", last),
				"Two-thirds or more counts. Until then, the figures use the four-installment rules, not the farmers' rules.")
		}
	}

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
		a.notes.Rule("N.C. Gen. Stat. § 105-163.15(d)(1); Form D-422 (2025), line 4", fmt.Sprintf("%v of this year's North Carolina tax after credits of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	// Questions about the target, asked only when no exception sets it.
	var unlessExempt []func()
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("No %d North Carolina return on record", last))
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d North Carolina return, and what was its tax?", last),
				fmt.Sprintf("With it, %v of last year's tax may set a target below %v of this year's.", y.priorYearShare, y.thisYearShare))
		})
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d North Carolina return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d North Carolina return covered %d months, not 12", last, prior.Months()))
	default:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, y.priorYearShare.Of(priorTax))
		a.notes.Rule("N.C. Gen. Stat. § 105-163.15(d)(2); Form D-422 (2025), line 7", fmt.Sprintf("the prior-year test is %v of the %v tax after credits on the %d return", y.priorYearShare, priorTax, last))
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
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d North Carolina tax after credits to be?", y.year),
				estimated.WithoutThisYearTax(priorOpt.Available()))
		})
	}

	// § 105-163.15(f): no liability last year, with no twelve-month or
	// residence condition.
	noLiability := hasPrior && ((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	switch {
	case noLiability:
		a.exempt = true
		none, err := estimated.NewException("No North Carolina tax liability last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no North Carolina income tax liability for %d. No interest is charged on underpaid estimates for %d.", last, y.year)).Exempt(none)
		a.notes.Rule("N.C. Gen. Stat. § 105-163.15(f); Form D-422 (2025), Exceptions", fmt.Sprintf("no liability for %d means no interest for %d", last, y.year))
	case taxKnown && tax-withheld < y.threshold:
		a.exempt = true
		below, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("%s That's under %s, so no estimated payments are needed.", owedWhy(tax, withheld), y.threshold.Whole())
		switch {
		case withheld > tax:
			why = owedWhy(tax, withheld) + " No estimated payments are needed."
		case withheld == tax:
			why = fmt.Sprintf("Your North Carolina withholding of %s covers your tax of %s. No estimated payments are needed.", withheld.Whole(), tax.Whole())
		}
		rb.Required(false, why).Exempt(below)
		a.notes.Rule("N.C. Gen. Stat. § 105-163.15(f); Form D-422 (2025), line 6", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no interest", tax, withheld, tax-withheld, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("You have no full-year %d North Carolina return to measure against. Your target is %v of your %d tax.", last, y.thisYearShare, y.year)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches 100%% of your %d tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is 100%% of your %d tax, %s. Meeting it keeps you clear of interest whatever your %d tax is. %s",
			last, target.Whole(), y.year, leftWhy(target, withheld))).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your North Carolina withholding is %s. That already meets the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("%s That's %s or more, so estimated payments are needed. %s",
			owedWhy(tax, withheld), y.threshold.Whole(), targetWhy(target, withheld))).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	if !a.exempt {
		for _, ask := range unlessExempt {
			ask()
		}
	}

	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("N.C. Gen. Stat. § 105-163.15(c), (d); Form D-422 (2025), line 16", fmt.Sprintf("25%% of %v is due on each of %s, %s, %s and %s", a.requirement.Target(),
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
		a.notes.Rule("N.C. Gen. Stat. § 105-163.15(e); Form D-422A (2025)", fmt.Sprintf("under the annualized method the installments are %v, %v, %v and %v: 22.5%%, 45%%, 67.5%% and 90%% of the annualized tax, "+
			"less earlier installments, and never more than the regular installment plus any reduction carried forward", ann[0], ann[1], ann[2], ann[3]))
	}

	// The D-422 line 17 rule. January 31, 2027 is a Sunday, and no source
	// says whether the day moves.
	fileBy := y.fileAndPayBy
	moves, movesAnswered := extras.JanuaryThirtyFirstMoves()
	if moves {
		fileBy = y.calendar.NextBusinessDay(fileBy)
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && !a.exempt {
		if !movesAnswered && filed.After(fileBy) && !filed.After(y.calendar.NextBusinessDay(fileBy)) {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Does North Carolina count your return filed and paid on %s as on time?", shortDate(filed, y.year)),
				fmt.Sprintf("The deadline is %s, a Sunday. Until then, it's treated as late and its tax stays out of the January column.", shortDate(fileBy, y.year)))
		}
		if !filed.After(fileBy) {
			a.filedByJan31 = true
			a.notes.Rule("Form D-422 (2025), Line 17 instructions; N.C. Gen. Stat. § 105-163.15(h)", fmt.Sprintf("the return was filed and paid on %s, by %s, so the tax paid with it counts in the January column, as paid on %s",
				filed.Long(), fileBy.Long(), y.deemedPaid.Long()))
		}
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}
	divergences, err := r.divergences(f, a, extras)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

func (r northCarolinaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r northCarolinaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
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
		notes.Rule("N.C. Gen. Stat. § 105-163.15(g)", fmt.Sprintf("as established, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("N.C. Gen. Stat. § 105-163.15(b), (g); Form D-422 (2025), lines 17 and 25", fmt.Sprintf("an equal part of the %v withheld is treated as paid on each due date, and a payment goes first to the earliest underpayment", f.WithheldTotal()))
	}
	return ledger, notes, nil
}

// installments is the schedule from the ledger. The January installment is
// excused when the return was filed and paid by January 31.
func (r northCarolinaEstimated) installments(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(l.NeededBy()).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if a.filedByJan31 && i == 3 {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r northCarolinaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d North Carolina return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
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

func (r northCarolinaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("northcarolinataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d North Carolina return to measure against, there is no target to figure the interest from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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

	// D-422 Section B: each part of each shortfall, by the day ÷ 365, at the
	// rate for the days, until paid or April 15.
	p, unrated, err := estimated.SimplePenalty(ledger, stop, y.rates, waiver, a.requirement, schedule)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("N.C. Gen. Stat. § 105-163.15(d)", fmt.Sprintf("this year's North Carolina tax was not given, so the interest is figured against the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the interest can be, and it is less if this year's tax turns out lower", y.year-1, p.Total()))
	}
	notes.Rule("N.C. Gen. Stat. §§ 105-163.15(a), (b), 105-241.21; Form D-422 (2025), lines 25 to 27", fmt.Sprintf("each part of each shortfall is charged the rate by the day ÷ 365, from its due date until paid or %s; "+
		"the interest to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", shortDate(unrated.AddDays(-1), 0)))
	}
	if waiverAsked && waiver != estimated.WaiverNone {
		notes.Rule("N.C. Gen. Stat. § 105-237(a)(2)", "the Secretary may waive interest only for a period of bankruptcy, so there is no waiver for casualty, retirement or disability; the interest is shown in full")
		if p.Total() > 0 {
			o := estimated.WaiverRefused("A casualty, disaster, retirement or disability doesn't waive this interest. The Secretary of Revenue can waive it only for a period of bankruptcy.")
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r northCarolinaEstimated) Explanation() estimated.Explanation { return northCarolinaExplanation }

// northCarolinaExplanation is written by hand and checked against
// research/estimated-payments/nc.md.
var northCarolinaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"North Carolina expects estimated payments if $1,000 or more of tax would be due. That is the tax left after withholding and credits. Falling short costs interest, 7% a year for 2026, on each quarter's shortfall.").
		Paragraph("You're clear if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. A quarter is due by each of April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. Last year's figure needs a full-year return you filed. Withholding counts as paid evenly unless you show when it was taken.").
		Paragraph("There is no interest if this year's tax less withholding is under $1,000. Nor is there any if you had no North Carolina income tax liability last year. A return filed with the balance paid by January 31 counts in the January column. It counts as paid on January 15.").
		Paragraph("The interest runs by the day from each due date until the shortfall is paid. It stops on April 15 of the next year. Each payment goes first to the oldest shortfall. The Secretary of Revenue sets the rate for each half-year. North Carolina has no waiver for a casualty, retirement or disability. Annualizing can lower early payments when income arrives late in the year.").
		VersusFederal("Unlike federal, there is no 110% tier. The no-liability exception has no twelve-month or residence condition. There is no waiver for casualty, retirement or disability. The state sets the rate each half-year. A return filed by January 31 counts as paid on January 15. The $1,000 threshold is the federal one.").
		Source("N.C. Gen. Stat. §§ 105-163.15, 105-241.21, 105-237").
		Source("Form D-422, 2025 Underpayment of Estimated Tax by Individuals, and its instructions").
		Source("Form NC-40, Individual Estimated Income Tax (for tax years beginning on or after January 1, 2026)").
		Source("NCDOR, Interest Rate; Estimated Income Tax").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()

// date is a shorthand for the year files.
func date(y int, m time.Month, d int) estimated.Date { return estimated.MustDate(y, m, d) }
