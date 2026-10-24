package oklahomataxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Oklahoma's estimated income tax rules, 68 O.S. §§ 2385.7, 2385.9 and
// 2385.13, as the shared [estimated.EstimatedTax] interface.
//
// The shape is federal: the lesser of a share of this year's tax and 100%
// of last year's, four equal quarters, and interest by the day on each
// quarter's shortfall. The numbers are not. This year's share is 70%, there
// is no higher prior-year share at any income, payments are needed from
// $500 of tax over withholding, and the interest is 20% a year, fixed by
// statute. Farmers are not required to make payments at all.
//
// The figure follows the Oklahoma Tax Commission's forms, OW-8-ES (revised
// 6-2025) for 2026 and OW-8-P for 2025, and the result's Divergences say
// where the statutes read otherwise. §§ 2385.7 and 2385.9 were last amended
// by Laws 2000, c. 314, and § 2385.13 by Laws 2021, c. 436
// (testdata/os68-2385.txt), so no form here predates the law it describes.

// jurisdiction is Oklahoma's postal code.
const jurisdiction = estimated.Oklahoma

// EstimatedFacts are the facts Oklahoma's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorFarmShare    money.Rate
	priorFarmAnswered bool
}

// PriorYearFarmShare is last year's gross income from farming as a share of
// last year's gross income from all sources, and whether the caller gave it.
// 68 O.S. § 2385.7(B)(2) excuses a filer who was a farmer in either year.
// This year's share is [estimated.Facts.FarmShare].
func (e EstimatedFacts) PriorYearFarmShare() (money.Rate, bool) {
	return e.priorFarmShare, e.priorFarmAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorYearFarmShare(r money.Rate) *EstimatedFactsBuilder {
	b.e.priorFarmShare, b.e.priorFarmAnswered = r, true
	return b
}

// Build refuses a share below nothing or above the whole.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.priorFarmShare < 0 || b.e.priorFarmShare > 10_000 {
		return EstimatedFacts{}, fmt.Errorf("oklahomataxcalc: a farm share of %v is not a share of one income", b.e.priorFarmShare)
	}
	return b.e, nil
}

// fraction is a share written as the statute writes it, so that two-thirds
// is not rounded to a number of basis points.
type fraction struct{ num, den int64 }

// reaches reports whether r is at least the fraction.
func (f fraction) reaches(r money.Rate) bool { return int64(r)*f.den >= f.num*10_000 }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is § 2385.7(A) and OW-8-P line 6: payments are needed, and
	// interest can arise, only when tax less withholding is this or more.
	threshold money.USCents

	// smallTax is § 2385.13(C) and OW-8-P line 3: no interest when the tax
	// is under it.
	smallTax money.USCents

	// thisYearShare and priorYearShare are § 2385.9(B): 70% of this year's
	// tax, or 100% of the tax on last year's 12-month return.
	thisYearShare, priorYearShare money.Rate

	// shares are § 2385.9(A), four equal installments; OW-8-P line 10 spreads
	// withholding in the same parts.
	shares []money.Rate

	// annualised are the applicable percentages of OW-8-P-SUP-I line 16, and
	// periodEnds the ends of its columns.
	annualised []money.Rate
	periodEnds []estimated.Date

	dues []estimated.Date

	// calendar is the days the OTC is closed: Saturdays, Sundays and the
	// § 82.1 holidays.
	calendar estimated.Calendar

	// fileAndPayBy is § 2385.9(A)(1): a return filed and paid by this date
	// waives the January installment. The forms do not apply it.
	fileAndPayBy estimated.Date

	// stop is § 2385.13(B): the interest runs no later than the fifteenth
	// day of the fourth month after the year.
	stop estimated.Date

	// rates is § 2385.13(A), 20% a year.
	rates estimated.RateSeries

	// farmShare is § 2385.7(B)(2): sixty-six and two-thirds percent.
	farmShare fraction
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Oklahoma's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return oklahomaEstimated{y}, nil
}

// EstimatedCalendar returns the holidays Oklahoma's rules move a due date
// for in a tax year, from 25 O.S. § 82.1, or [estimated.ErrNoRules]. A
// caller planning payment dates can move them the same way.
func EstimatedCalendar(year int) (estimated.Calendar, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return estimated.Calendar{}, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return y.calendar, nil
}

type oklahomaEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// installments are each installment's own amount.
	installments []money.USCents

	// januaryWaiver is whether the return was filed and paid by the
	// § 2385.9(A)(1) date, moved for a weekend or holiday. The waiver is
	// reported, not applied.
	januaryWaiver bool
	fileBy        estimated.Date

	// What each divergence turns on in these facts.
	residenceOpen, withholdingRule, smallTaxExempt bool

	notes working.Notes
}

func (r oklahomaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("oklahomataxcalc: the facts carry %T, not oklahomataxcalc.EstimatedFacts", s)
	}
}

func (r oklahomaEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("oklahomataxcalc: these are Oklahoma's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("oklahomataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("oklahomataxcalc: OW-8-P-SUP-I column %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the OTC's forms and the statutes part ways on a rule
// an Oklahoma computation uses. They come from
// research/estimated-payments/ok.md, sections 10 and Verification, and were
// checked against the extracts in testdata/. Each is marked as affecting
// these facts only where the readings give them different figures.
func (r oklahomaEstimated) divergences(f estimated.Facts, a assessment, januaryShort bool) ([]working.Divergence, error) {
	married := !a.exempt && f.FilingStatus() == filing.MarriedFilingSeparately
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("No liability last year").
			Form("No interest is due if you didn't have to file last year, wherever you lived.",
				"OW-8-P (2025), Part 1 line 7").
			Statute("No interest if you lived in Oklahoma all of a 12-month prior year. You must also have had no liability for it. Someone who wasn't a resident all year isn't covered.",
				"68 O.S. § 2385.13(C)").
			Uses(working.ReadingForm).Affects(a.residenceOpen),
		working.NewDivergence("A return filed and paid by January 31").
			Form("No form waives the January 15 installment for filing and paying by January 31. OW-8-P has no step for it.",
				"OW-8-ES (revised 6-2025); OW-8-P (2025), Part 3; Form 511 instructions (2025), line 40").
			Statute("Filing and paying the tax due by January 31 waives the January 15 installment.",
				"68 O.S. § 2385.9(A)(1)").
			Uses(working.ReadingForm).Affects(a.januaryWaiver && januaryShort),
		working.NewDivergence("Who must make estimated payments").
			Form("Payments are required when tax will exceed withholding by $500 or more. Withholding must also be under the smaller of two figures. They are 70% of this year's tax and last year's tax.",
				"OW-8-ES (revised 6-2025), Who Must Make Estimated Payments; Form 511 instructions (2025), Estimated Income Tax").
			Statute("Payments are required when tax is expected to top withholding by $500 or more. It sets no condition on withholding reaching the required annual payment.",
				"68 O.S. § 2385.7(A)").
			Uses(working.ReadingForm).Affects(a.withholdingRule),
		working.NewDivergence("Married individuals").
			Form("Each return tests its own tax and withholding, whatever the filing status.",
				"OW-8-ES (revised 6-2025), worksheet and Who Must Make Estimated Payments").
			Statute("For married people the test is their combined tax over withholding. A spouse filing separately is tested with the other spouse's figures too.",
				"68 O.S. § 2385.7(A)(2)").
			Uses(working.ReadingForm).Affects(married),
		working.NewDivergence("The $1,000 test").
			Form("No interest if tax after refundable and nonrefundable credits is under $1,000.",
				"OW-8-P (2025), Part 1 lines 1 to 3; Form 511 instructions (2025), line 40").
			Statute("No interest if the tax shown on the return is under $1,000. It doesn't say whether that is before or after credits.",
				"68 O.S. § 2385.13(C)").
			Uses(working.ReadingForm).Affects(a.smallTaxExempt),
		working.NewDivergence("The farmer share").
			Form("A farmer has at least 66.67% (two-thirds) of gross income from farming. That can be this year or last.",
				"Form 511 instructions (2025), Estimated Income Tax and line 22; OW-8-ES (revised 6-2025), Who Must Make Estimated Payments").
			Statute("At least sixty-six and two-thirds percent (66 2/3%), this year or last year. There is no carry-forward beyond one year. Both come to the same share.",
				"68 O.S. § 2385.7(B)(2)").
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

// targetWhy says what the target leaves to pay after withholding, for a Why.
func targetWhy(target, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("Your target is %s, a quarter by each due date.", target.Whole())
	}
	return fmt.Sprintf("Your target is %s, or %s after withholding. Pay a quarter by each due date.", target.Whole(), (target - withheld).Whole())
}

// leftWhy says what estimated payments the target leaves after
// withholding, for a Why.
func leftWhy(target, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("That's %s of estimated payments, a quarter by each due date.", target.Whole())
	}
	return fmt.Sprintf("After %s of withholding, that's %s of estimated payments. Pay a quarter by each due date.", withheld.Whole(), (target - withheld).Whole())
}

// owedWhy says what tax less withholding comes to, for a Why.
func owedWhy(tax, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("Your Oklahoma tax is %s, with no withholding.", tax.Whole())
	}
	return fmt.Sprintf("Your Oklahoma tax of %s less %s withheld is %s.", tax.Whole(), withheld.Whole(), (tax - withheld).Whole())
}

func (r oklahomaEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment

	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	last := y.year - 1
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
	}

	// § 2385.9(B)(1): 70% of this year's tax.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("68 O.S. § 2385.9(B)(1); OW-8-P (2025), Part 1 line 4",
			fmt.Sprintf("%v of this year's Oklahoma tax of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// § 2385.9(B)(2): 100% of the tax on last year's 12-month return. There
	// is no higher share at any income.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	// Questions about the target, asked only when no exception sets it.
	var unlessExempt []func()
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("No %d Oklahoma return on record", last))
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Oklahoma return, and what was its tax?", last),
				fmt.Sprintf("Your target could then be %v of last year's tax, if under %v of this year's.", y.priorYearShare, y.thisYearShare))
		})
	case prior.NotRequiredToFile():
		priorOption = unavailable(fmt.Sprintf("You weren't required to file a %d Oklahoma return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Oklahoma return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Oklahoma return covered %d months, not 12", last, prior.Months()))
	default:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, y.priorYearShare.Of(priorTax))
		a.notes.Rule("68 O.S. § 2385.9(B)(2); OW-8-P (2025), Part 1 line 7",
			fmt.Sprintf("%v of the %v tax on the 12-month %d return is %v; Oklahoma has no higher share at higher income", y.priorYearShare, priorTax, last, y.priorYearShare.Of(priorTax)))
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
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Oklahoma tax after credits to be?", y.year),
				estimated.WithoutThisYearTax(priorOpt.Available()))
		})
	}

	// § 2385.7(B)(2): a farmer this year or last is not required to pay.
	priorFarm, priorFarmKnown := extras.PriorYearFarmShare()
	farmer := y.farmShare.reaches(f.FarmShare()) || (priorFarmKnown && y.farmShare.reaches(priorFarm))

	switch {
	case farmer:
		a.exempt = true
		year := y.year
		if !y.farmShare.reaches(f.FarmShare()) {
			year = last
		}
		ex, err := estimated.NewException("Two-thirds of gross income from farming").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("At least two-thirds of your %d gross income is from farming. Oklahoma requires no estimated payments for %d and charges no interest. Mark the box on Form 511 line 22 and include your federal return.", year, y.year)).Exempt(ex)
		a.notes.Rule("68 O.S. § 2385.7(B)(2); Form 511 instructions (2025), line 22",
			fmt.Sprintf("farming was at least 66 2/3%% of gross income in %d, so no estimated payments are required for %d", year, y.year))
	case hasPrior && prior.NotRequiredToFile():
		a.exempt = true
		ex, err := estimated.NewException("Not required to file last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You weren't required to file an Oklahoma return for %d. Oklahoma's form then charges no %d interest and needs no estimated payments.", last, y.year)).Exempt(ex)
		a.notes.Rule("OW-8-P (2025), Part 1 line 7", fmt.Sprintf("no %d return was required, so there is no interest for %d", last, y.year))
		resident, answered := prior.FullYearResident()
		a.residenceOpen = !answered || !resident
		switch {
		case !answered:
			a.notes.Ask(working.PositionResidency, fmt.Sprintf("Did you live in Oklahoma all of %d?", last),
				"Until then the figures follow the form, excusing anyone not required to file. The law excuses only a full-year resident.")
		case !resident:
			a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("The form excuses anyone not required to file, and the figures follow it. The law excuses only full-year residents and would use %v of this year's tax.", y.thisYearShare))
		}
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("With no full-year %d Oklahoma return, your target is %v of your %d tax.", last, y.thisYearShare, y.year)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax is.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. That keeps you clear of interest whatever your %d tax is. %s",
			priorOpt.Percent(), last, target.Whole(), y.year, leftWhy(target, withheld))).Choose(chosen)
	case tax < y.smallTax:
		a.exempt, a.smallTaxExempt = true, true
		below, err := estimated.ExceptionBelow("Tax after credits", tax, y.smallTax)
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your Oklahoma tax after credits of %s is under %s. No interest is charged, and no estimated payments are needed.", tax.Whole(), y.smallTax.Whole())).Exempt(below)
		a.notes.Rule("68 O.S. § 2385.13(C); OW-8-P (2025), Part 1 line 3", fmt.Sprintf("%v of tax after credits is under %v, so there is no interest", tax, y.smallTax))
		a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("The law doesn't say if the %s test is on tax before or after credits. The figures follow the form's tax after credits.", y.smallTax.Whole()))
	case tax-withheld < y.threshold:
		a.exempt = true
		below, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("%s That's under %s. No estimated payments are needed, and no interest can arise.", owedWhy(tax, withheld), y.threshold.Whole())
		switch {
		case withheld > tax:
			why = fmt.Sprintf("Your Oklahoma tax of %s less %s withheld %s. No estimated payments are needed.", tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
		case withheld == tax:
			why = fmt.Sprintf("Your Oklahoma withholding of %s covers your %s tax. No estimated payments are needed.", withheld.Whole(), tax.Whole())
		}
		rb.Required(false, why).Exempt(below)
		a.notes.Rule("68 O.S. § 2385.7(A); OW-8-P (2025), Part 1 line 6", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no interest", tax, withheld, tax-withheld, y.threshold))
	case withheld >= target:
		a.withholdingRule = true
		why := fmt.Sprintf("Your Oklahoma withholding of %s reaches the year's %s target. No estimated payments are needed.", withheld.Whole(), target.Whole())
		if target == 0 {
			why = fmt.Sprintf("Your %d Oklahoma tax was $0, so your target is $0. No estimated payments are needed.", last)
		}
		rb.Required(false, why).Choose(chosen)
		a.notes.Rule("OW-8-P (2025), Part 1 line 8", fmt.Sprintf("%v of withholding is at least the required annual payment of %v, so there is no interest", withheld, target))
	default:
		rb.Required(true, fmt.Sprintf("%s That's at least %s, so estimated payments are needed. %s",
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
	if !a.exempt && !priorFarmKnown {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What share of your %d gross income came from farming?", last),
			fmt.Sprintf("If it was two-thirds or more, no estimated payments are needed for %d.", y.year))
	}
	if !a.exempt && f.FilingStatus() == filing.MarriedFilingSeparately {
		a.notes.Rule("68 O.S. § 2385.7(A)(2); OW-8-ES (revised 6-2025)", "this return's own tax and withholding are tested, as the form does; the statute tests married individuals' combined liability")
	}

	// § 2385.9(A) fixes the dates. OW-8-ES moves one that falls on a weekend
	// or a day the OTC is closed to the next business day.
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			a.notes.Rule("OW-8-ES (revised 6-2025), Due Dates; 25 O.S. § 82.1", fmt.Sprintf("the installment due %s falls on a weekend or legal holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
		}
	}
	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("68 O.S. § 2385.9(A); OW-8-P (2025), Part 2 line 9",
		fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", a.requirement.Target(), a.installments[0],
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
		a.notes.Rule("68 O.S. § 2385.9(C); OW-8-P-SUP-I (2025), lines 16 to 24", fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: %v, %v, %v and %v of the annualised tax, "+
			"less earlier installments, and never more than the regular quarter plus any reduction not yet made up", ann[0], ann[1], ann[2], ann[3],
			y.annualised[0], y.annualised[1], y.annualised[2], y.annualised[3]))
	}

	// § 2385.9(A)(1): filing and paying by January 31 waives the January
	// installment. January 31, 2027 is a Sunday, and 25 O.S. § 82.1(C) lets
	// an act due on a holiday be done the next business day. The forms do
	// not apply the waiver, so it is reported, not applied.
	a.fileBy = y.calendar.NextBusinessDay(y.fileAndPayBy)
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(a.fileBy) && !a.exempt {
		a.januaryWaiver = true
		applied := fmt.Sprintf("the return was filed and paid on %s, by %s", filed.Long(), a.fileBy.Long())
		if a.fileBy != y.fileAndPayBy {
			applied += fmt.Sprintf(" (%s is a weekend or legal holiday, so the date moves to %s)", y.fileAndPayBy.Long(), a.fileBy.Long())
		}
		a.notes.Rule("68 O.S. § 2385.9(A)(1); 25 O.S. § 82.1(C)", applied+"; the statute waives the January 15 installment, but the forms have no step for it, so it is not waived")
	}

	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}
	ledger, _, err := r.ledger(f, a)
	if err != nil {
		return assessment{}, err
	}
	divergences, err := r.divergences(f, a, ledger[len(ledger)-1].Shortfall() > 0)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

func (r oklahomaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies withholding and payments to the installments as OW-8-P
// Part 3 does: withholding a quarter on each due date unless the filer shows
// the dates it was withheld, payments on their dates, and each payment to
// the earliest balance first.
func (r oklahomaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
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
		notes.Rule("OW-8-P (2025), line 10", fmt.Sprintf("the %v withheld is credited on the dates it was withheld, as the form allows when you can show them", f.WithheldTotal()))
		notes.Ask(working.PositionUnsettledRule, "", "Oklahoma's form allows withholding on the dates it was taken. It gives no way to show them, and the figures use those dates.")
	} else {
		notes.Rule("OW-8-P (2025), line 10 and Part 3", fmt.Sprintf("the %v withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld", f.WithheldTotal()))
	}
	return ledger, notes, nil
}

// installments is the schedule from the ledger.
func (r oklahomaEstimated) installments(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
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

func (r oklahomaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Oklahoma return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
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

// Penalty figures the interest as OW-8-P Part 3 does: each installment's
// balance, after the payments applied to it earliest first, times the days
// it stays unpaid, times 20%, divided by 365. It runs from the due date to
// the day paid, the day the return is filed with the balance, or April 15 of
// the next year, whichever is earliest.
func (r oklahomaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("oklahomataxcalc: interest needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Oklahoma return to measure against, there is no target to figure interest from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	if !unrated.IsZero() {
		// The statute's rate covers every day to the stop, so this is a
		// defect in the year's table.
		return working.Result[estimated.Penalty]{}, fmt.Errorf("oklahomataxcalc: the 2026 rates do not reach %v", unrated)
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("68 O.S. § 2385.9(B)", fmt.Sprintf("this year's tax was not given, so the interest is figured against %v of the %d tax alone; "+
			"the required annual payment is the lesser of that and %v of this year's tax, so %v is the most the interest can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, y.thisYearShare, p.Total()))
	}
	if paidInFull {
		notes.Rule("68 O.S. § 2385.13(B); OW-8-P (2025), Part 3, Column A", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every balance still outstanding was paid that day", stop.Long()))
	}
	if a.januaryWaiver {
		for _, l := range p.Lines() {
			if l.Installment() == 4 && l.Total() > 0 {
				notes.Rule("68 O.S. § 2385.9(A)(1)", fmt.Sprintf("the January 15 installment carries %v of interest, which the statute's waiver would remove; the forms do not apply it, so it is included", l.Total()))
			}
		}
	}
	notes.Rule("68 O.S. § 2385.13(A), (B); OW-8-P (2025), Part 3", fmt.Sprintf("each installment's balance is charged %v a year, by the day over 365, from its due date until paid or %s, "+
		"with each payment applied to the earliest balance first; the interest to %s is %v", y.rates.Periods()[0].Rate(), y.stop.Long(), stop.Long(), p.Total()))
	if waiverAnswered && waiver != estimated.WaiverNone {
		notes.Rule("68 O.S. §§ 2385.7, 2385.13", fmt.Sprintf("Oklahoma's estimated tax statutes have no waiver for %v; after a disaster the Tax Commission postpones due dates by order, which is not built; the interest shown is not reduced", waiver))
		if p.Total() > 0 {
			o := estimated.WaiverRefused("Oklahoma has no waiver of this interest for retirement or disability.")
			if waiver == estimated.WaiverCasualty {
				o = estimated.WaiverRefused("Oklahoma has no waiver of this interest for a casualty or disaster. After some disasters, the Tax Commission postpones the due dates by order.")
			}
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r oklahomaEstimated) Explanation() estimated.Explanation { return oklahomaExplanation }

// oklahomaExplanation is written by hand and checked against
// research/estimated-payments/ok.md.
var oklahomaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Oklahoma expects estimated payments once tax tops withholding by $500 or more. Paying too little by a due date costs interest of 20% a year on the shortfall.").
		Paragraph("You avoid interest if withholding plus estimated payments reach a target. It's the smaller of 70% of this year's tax or all the tax on last year's return. Last year's figure needs a twelve-month return and is the same at every income.").
		Paragraph("A quarter is due April 15, June 15, September 15 and the next January 15. A date on a weekend or Tax Commission closure shifts to the next business day. Withholding counts as paid evenly across the four dates, whenever it was taken.").
		Paragraph("There is no interest if this year's tax after credits is under $1,000. There is none if it is less than $500 above your withholding. Nor is there any if you didn't have to file an Oklahoma return last year.").
		Paragraph("Oklahoma calls the charge interest. It is 20% a year, set by statute, and charged by the day from each due date. It runs until the shortfall is paid, or April 15 of the next year if sooner. A payment goes first to the oldest shortfall. For uneven income, the annualised method can lower early payments. Its form is OW-8-P-SUP-I. The statute also waives the January payment if you file and pay by January 31. The Tax Commission's forms don't apply that. There is no waiver for a casualty, retirement or disability.").
		VersusFederal("Unlike federal, payments start at $500 over withholding, not $1,000. This year's share is 70%, not 90%. Last year's share is 100% at every income, with no 110% tier. The interest is a fixed 20% a year. The annualised percentages are 17.5%, 35%, 52.5% and 70%. There's no casualty or disability waiver.").
		Source("68 O.S. §§ 2385.7, 2385.9, 2385.13").
		Source("Form OW-8-ES, Oklahoma Individual Estimated Tax, tax year 2026 (revised 6-2025)").
		Source("Form OW-8-P, Underpayment of Estimated Tax Worksheet, 2025").
		Source("Forms OW-8-ES-SUP (2026) and OW-8-P-SUP-I (2025), annualised income installment method").
		Source("2025 Oklahoma Resident Individual Income Tax Forms and Instructions (Form 511 packet), lines 22 and 40").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
