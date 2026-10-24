package delawaretaxcalc

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

// Delaware's estimated tax rules, 30 Del. C. §§ 1169 and 1170, and the
// addition to tax for an underpayment, § 535(b), as the shared
// [estimated.EstimatedTax] interface.
//
// Delaware's statute is short. § 1169 requires a declaration when the
// estimated tax exceeds $800, § 1170 takes the federal dates unless the State
// Tax Commissioner sets others, and § 535(b) adds 1 1/2% a month, or fraction
// of a month, on an underpayment, determined "in a manner consistent with the
// internal revenue laws". Everything else is on the Division of Revenue's
// forms: the 90%, 100% and 110% tests, the April 30 first date, the $800 stop
// on the required payment, the part-year exception, and a penalty charged at
// 0.0005 a day rather than by the month. Nothing here borrows from the
// federal package.
//
// The figure follows the Division's forms, PIT-EST for 2026 and PIT-UND for
// 2025, and the result's Divergences say where the Delaware Code reads
// otherwise. research/estimated-payments/de.md found no act in 2024 to 2026
// that changes §§ 535, 1169 or 1170, so no form here predates the law it
// describes.

// jurisdiction is Delaware's postal code.
const jurisdiction = estimated.Delaware

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is PIT-UND line E: no penalty when the required annual
	// payment less withholding is this or less.
	threshold money.USCents

	// thisYearPercent is the current-year test; priorPercent and
	// priorPercentHigh the prior-year test at or below and above the step in
	// last year's adjusted gross income.
	thisYearPercent, priorPercent, priorPercentHigh money.Rate
	priorYearStep, priorYearStepSeparate            money.USCents

	dues []estimated.Date

	// line39 is the date PIT-UND line 39 counts each installment's days
	// from: the day after April 30 for the first, the due date for the rest.
	line39 []estimated.Date

	calendar estimated.Calendar

	// periodEnds close the periods PIT-UND line 30 credits withholding to,
	// and the periods Part 3 annualises.
	periodEnds []estimated.Date

	// lateStarts are the first days of PIT-EST item 7's three later groups:
	// a requirement first arising on or after lateStarts[i] is declared by
	// installment i+2.
	lateStarts []estimated.Date

	stop estimated.Date

	// applicable is PIT-UND line 19, the share of the annualised tax due by
	// each installment.
	applicable []money.Rate

	daily           estimated.FactorSeries
	dailyFactor     estimated.Factor
	shortMethodRate money.Rate
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are PIT-UND Part 1 line C and line 23: the required annual payment
// is made in four equal installments.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Delaware's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return delawareEstimated{y}, nil
}

type delawareEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// installments are each installment's own amount: a quarter of the
	// target, or PIT-UND line 27 under the annualised method.
	installments []money.USCents

	notes working.Notes
}

func (r delawareEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("delawaretaxcalc: these are Delaware's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("delawaretaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.State() != nil:
		return fmt.Errorf("delawaretaxcalc: Delaware's rules read no extras, and the facts carry %T", f.State())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: a filer with two-thirds of gross income from farming or fishing pays 66 2/3%% by January 15, or files and pays by March 1 (PIT-EST item 5), which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("delawaretaxcalc: PIT-UND Part 3 period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the Division's forms and the Delaware Code part ways
// on a rule every Delaware computation uses. They are found in
// research/estimated-payments/de.md and checked against the statute's text in
// testdata/. The figures follow the forms.
func (r delawareEstimated) divergences(af affects) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The penalty by the day or by the month").
			Form("The form charges an installment's underpayment 0.05% for each day it's late. One day costs 0.05%, and thirty days 1.5%.",
				"PIT-UND (2025), lines 40 to 42 and their instructions").
			Statute("The law charges 1.5% a month, or part of a month, on the underpayment. One day late costs a full 1.5%.",
				"30 Del. C. § 535(b)").
			Uses(working.ReadingForm).Affects(af.byDay),
		working.NewDivergence("The day the first installment's penalty starts").
			Form("The form counts the first installment's late days from May 1. That is the day after its Apr 30 due date. It counts the other three from their due dates.",
				"PIT-UND (2025), lines 39 and 40").
			Statute("An installment not paid by its due date is underpaid from that date. That date is Apr 30 for the first. The charge runs for as long as it's underpaid.",
				"30 Del. C. §§ 535(b), 1170(a); PIT-EST (2026), item 8").
			Uses(working.ReadingForm).Affects(af.firstDay),
		working.NewDivergence("The $800 test").
			Form("Your required payment is the smaller of the two tests. There is no penalty when it less withholding is $800 or less.",
				"PIT-UND (2025), Part 1, line E; PIT-UND instructions, Exceptions to the Penalty").
			Statute("A declaration is needed when the tax can be expected to be more than $800. That tax is this year's tax less withholding. The law measures this year's tax, not the required payment.",
				"30 Del. C. §§ 1169(a), (b), 535(b)").
			Uses(working.ReadingForm).Affects(af.threshold),
		working.NewDivergence("Part-year residents").
			Form("No penalty for anyone who was a Delaware resident for only part of the year.",
				"PIT-UND (2025) instructions, Exceptions to the Penalty").
			Statute("Every resident and nonresident must declare an estimated tax over $800. The penalty has no exception for a part-year resident.",
				"30 Del. C. §§ 1169(a), 535(b)").
			Uses(working.ReadingForm).Affects(af.partYear),
		working.NewDivergence("A requirement first met after Apr 1").
			Form("Each installment is a quarter of your required payment. That holds whenever the requirement arose. Only the annualised method lowers the earlier ones.",
				"PIT-UND (2025), Part 1, line C, and line 23").
			Statute("The Commissioner may set the dates. The estimated tax form sets Jun 15, Sep 15 or Jan 15 as the first date. That is for someone first required after Apr 1, Jun 1 or Sep 1. The form sets equal installments on the remaining dates.",
				"30 Del. C. § 1170(a); PIT-EST (2026), item 7").
			Uses(working.ReadingForm).Affects(af.lateStart),
		working.NewDivergence("The optional Short Method").
			Form(fmt.Sprintf("You can use the form's Short Method if you made no estimated payments. You can also use it if you made four equal ones on time. It lets you pay 12%% of the year's total underpayment instead. That is less 0.05%% a day for anything paid before %s.", short(r.y.stop, r.y.year)),
				"PIT-UND (2025), Part 2, lines F to L, and its instructions").
			Statute("The law charges 1.5% a month, or part of a month. It is charged on each installment's underpayment for as long as it's underpaid.",
				"30 Del. C. § 535(b)").
			Uses(working.ReadingForm).Affects(af.shortMethod),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// affects is, for each divergence, whether the statute's reading would give
// these facts a different answer.
type affects struct {
	byDay, firstDay, threshold, partYear, lateStart, shortMethod bool
}

func separate(s filing.Status) bool { return s == filing.MarriedFilingSeparately }

// partYear reports whether the filer lived in Delaware for some of the year
// but not all of it, and whether the caller said where the filer lived.
func partYear(periods []estimated.ResidencePeriod, year int) (part, answered bool) {
	if len(periods) == 0 {
		return false, false
	}
	jan1 := estimated.MustDate(year, time.January, 1)
	dec31 := estimated.MustDate(year, time.December, 31)
	in := 0
	for d := jan1; !d.After(dec31); d = d.AddDays(1) {
		for _, p := range periods {
			if p.State() == string(jurisdiction) && !d.Before(p.From()) && !d.After(p.To()) {
				in++
				break
			}
		}
	}
	return in > 0 && in <= dec31.DaysSince(jan1), true
}

func (r delawareEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment

	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	last := y.year - 1
	var priorFigures estimated.Figures
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorFigures = pf
			priorTax, _ = pf.Tax()
		}
	}

	// 90% of this year's tax. PIT-EST item 10(a); PIT-UND line A.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, y.thisYearPercent.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("PIT-EST (2026), item 10(a); PIT-UND (2025), Part 1, line A", fmt.Sprintf("%v of this year's Delaware tax of %v is %v", y.thisYearPercent, tax, thisYear.Amount()))
	}

	priorOpt, err := r.priorOption(f, &a, hasPrior, prior, priorFigures, priorTax)
	if err != nil {
		return assessment{}, err
	}

	rb := estimated.NewRequirement().Option(thisYear).Option(priorOpt)
	chosen, target := estimated.BasisThisYearTax, thisYear.Amount()
	if priorOpt.Available() && (!taxKnown || priorOpt.Amount() < target) {
		chosen, target = estimated.BasisPriorYearTax, priorOpt.Amount()
	}
	// No liability last year. PIT-UND states it with no condition on the
	// length of the year; PIT-EST (2026) item 10 requires a twelve-month year,
	// and that is the default.
	noLiability := hasPrior && ((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	if noLiability && prior.Months() != 12 {
		a.notes.Ask(working.PositionUnsettledRule, "",
			fmt.Sprintf("The estimated tax form needs a twelve-month year with no Delaware tax. The penalty form sets no length, but your %d-month %d doesn't excuse you.", prior.Months(), last))
		noLiability = false
	}
	part, residenceAnswered := partYear(f.Residence(), y.year)

	switch {
	case noLiability:
		a.exempt = true
		why := fmt.Sprintf("your %d return showed no Delaware tax", last)
		if prior.NotRequiredToFile() {
			why = fmt.Sprintf("no %d Delaware return was required, which is taken as no Delaware liability", last)
		}
		none, err := estimated.NewException("No Delaware tax last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no Delaware tax for %d, so no estimated payments are needed for %d.", last, y.year)).Exempt(none)
		a.notes.Rule("PIT-UND (2025) instructions, Exceptions to the Penalty; PIT-EST (2026), item 10", fmt.Sprintf("%s, a twelve-month year, so there is no penalty for %d", why, y.year))
	case part:
		a.exempt = true
		partYear, err := estimated.NewException("Part-year Delaware resident").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You were a Delaware resident for only part of %d. Delaware charges no estimated tax penalty to a part-year resident. No estimated payments are needed.", y.year)).Exempt(partYear)
		a.notes.Rule("PIT-UND (2025) instructions, Exceptions to the Penalty", fmt.Sprintf("you were a part-year resident during %d, so there is no penalty", y.year))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("No %d Delaware return sets a target. There is no target until your %d Delaware tax is figured.", last, y.year)).NoTarget()
	case target-withheld <= y.threshold:
		a.exempt = true
		small, err := estimated.ExceptionAtMost("Required payment less withholding", target-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		left := "leaves " + (target - withheld).Whole() + ", which is " + y.threshold.Whole() + " or less"
		if target < withheld {
			left = estimated.Leaves(target - withheld)
		}
		if taxKnown {
			rb.Required(false, fmt.Sprintf("Your required payment is %s. Less %s of withholding, that %s. No estimated payments are needed.",
				target.Whole(), withheld.Whole(), left)).Exempt(small)
		} else {
			rb.Required(false, fmt.Sprintf("Your required payment is at most %v of your %d Delaware tax, %s. Less %s of withholding, that %s. No estimated payments are needed, whatever your %d tax turns out to be.",
				priorOpt.Percent(), last, target.Whole(), withheld.Whole(), left, y.year)).Exempt(small)
		}
		a.notes.Rule("PIT-UND (2025), Part 1, line E", fmt.Sprintf("the required annual payment of %v less %v of withholding is %v, which is %v or less, so there is no penalty", target, withheld, target-withheld, y.threshold))
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d Delaware tax, %s. It keeps you clear of a penalty whatever your %d tax turns out to be. After %s of withholding, that's %s of estimated payments. A quarter is due by each date.",
			priorOpt.Percent(), last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your required payment is %s. After %s of withholding, the %s left is more than %s. You need estimated payments of %s, a quarter by each date.",
			target.Whole(), withheld.Whole(), (target-withheld).Whole(), y.threshold.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// These answers change the target. An exception sets the target, so they
	// are asked only when none applies.
	if !a.exempt && !hasPrior {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Delaware return, and what was its tax?", last),
			fmt.Sprintf("With it, %v of last year's tax could be your target. That is if it's lower than %v of this year's.", y.priorPercent, y.thisYearPercent))
	}
	if !a.exempt && !taxKnown {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Delaware tax to be?", y.year),
			map[bool]string{true: "Until then, your target can only come from last year's tax.", false: "Until then, there's no target."}[a.requirement.TargetKnown()])
	}
	if !a.exempt && !residenceAnswered && a.requirement.TargetKnown() {
		a.notes.Ask(working.PositionResidency, fmt.Sprintf("Were you a Delaware resident for only part of %d?", y.year),
			"If you were, Delaware charges you no estimated tax penalty.")
	}

	// The due dates. PIT-EST item 8 prints them; 30 Del. C. § 551(d) moves one
	// that falls on a Saturday, Sunday or Delaware legal holiday.
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			a.notes.Rule("30 Del. C. § 551(d)", fmt.Sprintf("the installment due %s falls on a Saturday, Sunday or legal holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
		}
	}
	// The statute's $800 test is on this year's tax, so it can only differ
	// when that tax is given and no other exception applies.
	af := affects{
		partYear:  part && !noLiability,
		threshold: !noLiability && !part && taxKnown && (target-withheld <= y.threshold) != (tax-withheld <= y.threshold),
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
		if err := r.diverge(&a, af); err != nil {
			return assessment{}, err
		}
		return a, nil
	}
	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), quarters)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("PIT-EST (2026), item 8; PIT-UND (2025), Part 1, line C, and line 23", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", a.requirement.Target(), a.installments[0],
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
	if f.Elections().Annualised() {
		if err := r.annualise(f, &a); err != nil {
			return assessment{}, err
		}
	}
	af.lateStart = r.lateStart(f, &a)
	ledger, _, err := r.ledger(f, a)
	if err != nil {
		return assessment{}, err
	}
	for i, l := range ledger {
		if l.Shortfall() > 0 {
			af.byDay = true
			af.firstDay = af.firstDay || i == 0
		}
	}
	if _, af.shortMethod, err = r.shortMethod(f, a, y.stop); err != nil {
		return assessment{}, err
	}
	if err := r.diverge(&a, af); err != nil {
		return assessment{}, err
	}
	return a, nil
}

// diverge adds the divergences to the assessment's notes.
func (r delawareEstimated) diverge(a *assessment, af affects) error {
	ds, err := r.divergences(af)
	if err != nil {
		return err
	}
	a.notes.Diverges(ds...)
	return nil
}

// priorOption is PIT-UND line B: 100% of last year's Delaware tax, or 110%
// above the step in last year's adjusted gross income.
func (r delawareEstimated) priorOption(f estimated.Facts, a *assessment, hasPrior bool, prior estimated.PriorReturn, pf estimated.Figures, priorTax money.USCents) (estimated.TargetOption, error) {
	y := r.y
	last := y.year - 1
	unavailable := func(why string) (estimated.TargetOption, error) {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorPercent, 0).Unavailable(why).Build()
	}
	switch {
	case !hasPrior:
		return unavailable(fmt.Sprintf("Needs your %d Delaware return", last))
	case prior.NotRequiredToFile():
		return unavailable(fmt.Sprintf("You didn't need to file a %d Delaware return", last))
	case !prior.Filed():
		return unavailable(fmt.Sprintf("You didn't file a %d Delaware return", last))
	}

	step := y.priorYearStep
	if separate(f.FilingStatus()) {
		step = y.priorYearStepSeparate
	}
	rate := func(agi money.USCents) money.Rate {
		if agi > step {
			return y.priorPercentHigh
		}
		return y.priorPercent
	}
	stateAGI, stateKnown := pf.StateAGI()
	federalAGI, federalKnown := pf.FederalAGI()
	var percent money.Rate
	var why string
	switch {
	case stateKnown:
		percent = rate(stateAGI)
		why = fmt.Sprintf("the %d Delaware AGI of %v is not more than %v", last, stateAGI, step)
		if percent == y.priorPercentHigh {
			why = fmt.Sprintf("the %d Delaware AGI of %v is more than %v", last, stateAGI, step)
		}
		switch {
		case !federalKnown:
			a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d federal adjusted gross income?", last),
				fmt.Sprintf("Until then, your Delaware AGI is tested against the %s step. The penalty form uses Delaware AGI, and the estimated tax form uses federal AGI.", step.Whole()))
		case rate(federalAGI) != percent:
			a.notes.Ask(working.PositionUnsettledRule, "",
				fmt.Sprintf("Delaware AGI of %s gives %v, but federal AGI of %s gives %v. The prior-year percentage follows the penalty form's Delaware AGI.",
					stateAGI.Whole(), percent, federalAGI.Whole(), rate(federalAGI)))
		}
	case federalKnown:
		percent = rate(federalAGI)
		why = fmt.Sprintf("the %d federal AGI of %v is not more than %v", last, federalAGI, step)
		if percent == y.priorPercentHigh {
			why = fmt.Sprintf("the %d federal AGI of %v is more than %v", last, federalAGI, step)
		}
		a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d Delaware adjusted gross income?", last),
			fmt.Sprintf("Until then, your federal AGI is tested against the %s step. The estimated tax form uses federal AGI, and the penalty form uses Delaware AGI.", step.Whole()))
	default:
		percent = y.priorPercentHigh
		why = fmt.Sprintf("the %d AGI was not given", last)
		a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d adjusted gross income?", last),
			fmt.Sprintf("Until then, the prior-year option is %v of last year's tax. That is the figure above %s, rather than %v.", y.priorPercentHigh, step.Whole(), y.priorPercent))
	}
	amount := percent.Of(priorTax)
	a.notes.Rule("PIT-EST (2026), item 10(b); PIT-UND (2025), Part 1, line B", fmt.Sprintf("%s, so the prior-year test is %v of the %v tax on the %d Delaware return, or %v; no Delaware source limits it to a twelve-month return",
		why, percent, priorTax, last, amount))
	return estimated.NewTargetOption(estimated.BasisPriorYearTax, percent, amount).Build()
}

// annualise replaces the installments with PIT-UND line 27: the applicable
// percentage of the period's annualised tax less the installments before it,
// or the regular quarter plus what the annualised method left out earlier,
// whichever is smaller. That is the federal recapture, which
// [estimated.Annualised] computes.
func (r delawareEstimated) annualise(f estimated.Facts, a *assessment) error {
	periods := f.Periods()
	tax := make([]money.USCents, len(periods))
	for i, p := range periods {
		tax[i] = p.AnnualisedTax()
	}
	out, err := estimated.Annualised(tax, r.y.applicable, a.installments)
	if err != nil {
		return err
	}
	a.installments = out
	a.notes.Rule("PIT-UND (2025), Part 3, lines 18 to 27", fmt.Sprintf("under the annualised method the installments are the smaller of %v, %v, %v and %v of the annualised tax, %v, %v, %v and %v, less the installments before, "+
		"and the regular quarter with any shortfall carried forward: %v, %v, %v and %v",
		r.y.applicable[0], r.y.applicable[1], r.y.applicable[2], r.y.applicable[3], tax[0], tax[1], tax[2], tax[3], out[0], out[1], out[2], out[3]))
	return nil
}

// lateStart notes PIT-EST item 7 for a filer who first expected to owe after
// April 1, and reports whether it applies. PIT-UND does not lower the earlier
// installments for it; see the divergences.
func (r delawareEstimated) lateStart(f estimated.Facts, a *assessment) bool {
	met := f.RequirementFirstMet()
	first := 0
	for i, g := range r.y.lateStarts {
		if !met.IsZero() && !met.Before(g) {
			first = i + 1
		}
	}
	if first == 0 {
		return false
	}
	a.notes.Rule("PIT-EST (2026), item 7; PIT-UND (2025), line 23", fmt.Sprintf("you first expected to owe on %s, so PIT-EST lets you declare by %s and pay in equal installments from then; "+
		"PIT-UND still charges a quarter on each date, and the figures here follow it, so the earlier installments are short unless the annualised method lowers them",
		met.Long(), a.dues[first].Long()))
	return true
}

func (r delawareEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// credits are what PIT-UND Part 4 counts toward each installment: withholding
// in the period it was withheld (line 30), credited on that period's due
// date; estimated payments on their dates (line 29); and last year's
// overpayment on the first date, as PIT-EST Method 1 applies it.
func (r delawareEstimated) credits(f estimated.Facts, dues []estimated.Date) ([]estimated.Payment, error) {
	byPeriod := make([]money.USCents, len(dues))
	for _, w := range f.Withholding() {
		for i, end := range r.y.periodEnds {
			if !w.On().After(end) {
				byPeriod[i] += w.Amount()
				break
			}
		}
	}
	var out []estimated.Payment
	for i, amount := range byPeriod {
		if amount == 0 {
			continue
		}
		p, err := estimated.NewPayment().On(dues[i]).Amount(amount).Build()
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	for _, p := range f.Payments() {
		if p.FromPriorYear() {
			moved, err := estimated.NewPayment().On(dues[0]).Amount(p.Amount()).Build()
			if err != nil {
				return nil, err
			}
			p = moved
		}
		out = append(out, p)
	}
	return out, nil
}

// ledger applies the credits to the installments.
func (r delawareEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := r.credits(f, a.dues)
	if err != nil {
		return nil, working.Notes{}, err
	}
	ledger, err := estimated.Apply(a.dues, a.installments, credits)
	if err != nil {
		return nil, working.Notes{}, err
	}
	y := r.y
	var notes working.Notes
	notes.Rule("PIT-UND (2025) instructions, line 30", fmt.Sprintf("the %v withheld counts in the period it was withheld, January 1 to %s toward %s, then to %s, %s and %s toward the later dates; it is not spread evenly",
		f.WithheldTotal(), monthDay(y.periodEnds[0]), a.dues[0].Long(), monthDay(y.periodEnds[1]), monthDay(y.periodEnds[2]), monthDay(y.periodEnds[3])))
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("PIT-UND (2025) instructions, line 30", "Delaware has no election about withholding; PIT-UND always counts it in the period it was withheld")
	}
	notes.Rule("PIT-UND (2025), lines 32 to 38; PIT-EST (2026), Method of Payments", "a payment first pays the oldest underpayment, on the day it is paid, and last year's overpayment applied forward counts toward the first installment")
	return ledger, notes, nil
}

func monthDay(d estimated.Date) string { return fmt.Sprintf("%s %d", d.Month(), d.Day()) }

// short writes a date as a person reads it: "Apr 30" in the tax year, and
// "Jan 15, 2027" outside it.
func short(d estimated.Date, year int) string {
	if d.Year() == year {
		return fmt.Sprintf("%s %d", d.Month().String()[:3], d.Day())
	}
	return fmt.Sprintf("%s %d, %d", d.Month().String()[:3], d.Day(), d.Year())
}

func (r delawareEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Delaware return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	out, notes, err := r.schedule(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments and the notes on how they were credited.
func (r delawareEstimated) schedule(f estimated.Facts, a assessment) ([]estimated.Installment, working.Notes, error) {
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return nil, working.Notes{}, err
	}
	out := make([]estimated.Installment, len(ledger))
	var needed money.USCents
	for i, l := range ledger {
		needed += a.installments[i]
		if out[i], err = estimated.NewInstallment(i+1, l.Due()).NeededBy(needed).CreditedBy(l.CreditedBy()).
			Shortfall(l.Shortfall()).Build(); err != nil {
			return nil, working.Notes{}, err
		}
	}
	return out, notes, nil
}

func (r delawareEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("delawaretaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Delaware return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	schedule, _, err := r.schedule(f, a)
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

	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	firstShort := false
	for i, l := range ledger {
		if l.Shortfall() <= 0 {
			continue
		}
		from := y.line39[i]
		lb := estimated.NewPenaltyLine(l.Number(), l.Due(), l.Shortfall())
		last := from
		for _, piece := range l.Pieces() {
			to := stop
			if on, paid := piece.PaidOn(); paid && on.Before(stop) {
				to = on
			}
			if !to.After(from) {
				continue
			}
			if to.After(last) {
				last = to
			}
			parts, gap, err := estimated.DailyFactor(piece.Amount(), from, to, y.daily)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			if !gap.IsZero() {
				return working.Result[estimated.Penalty]{}, fmt.Errorf("delawaretaxcalc: the daily factor has no period for %v", gap)
			}
			for _, p := range parts {
				lb.Part(p)
			}
		}
		line, err := lb.Days(last.DaysSince(from)).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		if i == 0 && line.Total() > 0 {
			firstShort = true
		}
		pb.Line(line)
	}
	p, err := pb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("PIT-UND (2025), Part 1, lines A to C", fmt.Sprintf("this year's Delaware tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required annual payment is the lesser of that and %v of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, y.thisYearPercent, p.Total()))
	}
	if paidInFull {
		notes.Rule("PIT-UND (2025) instructions, line 40", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every underpayment still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("30 Del. C. § 535(b); PIT-UND (2025), lines 39 to 43", fmt.Sprintf("each underpayment is charged %v for each day from %s (the first installment), %s, %s or %s until paid or %s, to the cent; the penalty to %s is %v",
		y.dailyFactor, y.line39[0].Long(), y.line39[1].Long(), y.line39[2].Long(), y.line39[3].Long(), y.stop.Long(), stop.Long(), p.Total()))
	if firstShort {
		notes.Ask(working.PositionUnsettledRule, "",
			fmt.Sprintf("The %d penalty form isn't published, so the figures follow the %d form. Its first installment's late days run from %s, not %s.",
				y.year, y.year-1, short(y.line39[0], y.year), short(a.dues[0], y.year)))
	}
	if day := a.dues[3].AddDays(1); r.paidOn(f, day) {
		notes.Ask(working.PositionUnsettledRule, "",
			fmt.Sprintf("The form's instructions apply your %s payment to the last installment. Its column stops at %s, so the figures count the payment a day late.",
				short(day, y.year), short(y.dues[3], y.year)))
	}
	if short, ok, err := r.shortMethod(f, a, stop); err != nil {
		return working.Result[estimated.Penalty]{}, err
	} else if ok {
		notes.Rule("PIT-UND (2025), Part 2, lines F to L", short)
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking the Division to waive the penalty?",
			"The reasons are a casualty, disaster, retirement after 62 or disability. You ask in writing after assessment, and the penalty shown is before any waiver.")
	}
	if waiver != estimated.WaiverNone {
		notes.Rule("PIT-UND (2025) instructions, Waiver of the Penalty; PIT-EST (2026), item 11", fmt.Sprintf("a waiver for %v is asked for in writing to the Tax Conferee, P.O. Box 8714, once the penalty has been assessed; PIT-UND is not filed for it", waiver))
	}
	if o, ok := delawareWaiver(f); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// delawareWaiver decides the waiver asked for, from the reasons PIT-UND's
// instructions and PIT-EST item 11 list. Neither sets the federal conditions
// that the penalty be inequitable or that the shortfall have reasonable
// cause. Retirement or disability must fall in the tax year or the year
// before, retirement after age 62.
func delawareWaiver(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	p := f.Positions()
	kind, answered := p.Waiver()
	if !answered || kind == estimated.WaiverNone {
		return estimated.WaiverOutcome{}, false
	}
	if kind == estimated.WaiverCasualty {
		what := "a casualty, disaster or other unusual circumstance"
		switch p.WaiverCause() {
		case estimated.CauseCasualty:
			what = "a casualty"
		case estimated.CauseDisaster:
			what = "a disaster"
		}
		return estimated.WaiverEligible(fmt.Sprintf("A shortfall due to %s qualifies. The Division of Revenue can waive the penalty for it.", what)), true
	}
	o, ok := estimated.StandardWaiver(f, "the Division of Revenue", 62)
	if !o.Eligible() {
		return o, ok
	}
	what := "retired after reaching 62"
	if p.WaiverCause() == estimated.CauseDisability {
		what = "became disabled"
	}
	return estimated.WaiverEligible(fmt.Sprintf("You %s in %d. The Division of Revenue lists that as a reason to waive the penalty.", what, p.WaiverOn().Year())), true
}

func (r delawareEstimated) paidOn(f estimated.Facts, d estimated.Date) bool {
	for _, p := range f.Payments() {
		if !p.FromPriorYear() && p.On() == d {
			return true
		}
	}
	return false
}

// shortMethod is PIT-UND Part 2, when it is open: the required annual payment
// less payments and withholding, times 12%, less 0.0005 a day times that
// underpayment for each day before April 30 it was paid. It is open to a
// filer who made no estimated payments, or exactly four equal ones each on
// or before its due date. Last year's overpayment applied forward is taken
// to close it, since it is not one of four equal payments. PIT-UND also
// closes it when capital gain tax was paid on the filer's behalf, which the
// facts do not record; such a payment is an estimated payment here, and it
// closes the method the same way.
func (r delawareEstimated) shortMethod(f estimated.Facts, a assessment, stop estimated.Date) (string, bool, error) {
	y := r.y
	payments := f.Payments()
	for _, p := range payments {
		if p.FromPriorYear() {
			return "", false, nil
		}
	}
	sort.Slice(payments, func(i, j int) bool { return payments[i].On().Before(payments[j].On()) })
	var paid money.USCents
	switch len(payments) {
	case 0:
	case len(a.dues):
		for i, p := range payments {
			if p.Amount() != payments[0].Amount() || p.On().After(a.dues[i]) || (i > 0 && !p.On().After(a.dues[i-1])) {
				return "", false, nil
			}
			paid += p.Amount()
		}
	default:
		return "", false, nil
	}
	underpaid := a.requirement.Target() - paid - f.WithheldTotal()
	if underpaid <= 0 {
		return "", false, nil
	}
	charge := y.shortMethodRate.Of(underpaid)
	var early int
	var credit money.USCents
	if stop.Before(y.stop) {
		early = y.stop.DaysSince(stop)
		credit = y.dailyFactor.Of(underpaid, early, estimated.RoundToCent)
	}
	applied := fmt.Sprintf("you made no estimated payments or four equal ones on time, so you may use the Short Method instead: %v of the %v underpayment is %v, less %v for %d days before %s, is %v",
		y.shortMethodRate, underpaid, charge, credit, early, y.stop.Long(), charge-credit)
	return applied, true, nil
}

func (r delawareEstimated) Explanation() estimated.Explanation { return delawareExplanation }

// delawareExplanation is written by hand and checked against
// research/estimated-payments/de.md.
var delawareExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Delaware expects estimated payments when your tax will be more than $800. This is your Delaware tax after withholding and credits. The penalty is 1.5% a month on each late installment. The Division's form charges it as 0.05% a day.").
		Paragraph("To avoid the penalty, withholding and estimated payments must reach a target. The target is the smaller of two figures. They are 90% of this year's Delaware tax and 100% of last year's. The 100% is 110% if last year's adjusted gross income (AGI) was over $150,000. That line is $75,000 if you file separately on separate forms (filing status 3). The penalty form tests Delaware AGI. The estimated tax instructions say federal AGI. If the two fall on different sides of the line, settle it with the Division. No penalty is due if the target less your Delaware withholding is $800 or less. There is also none if you had no Delaware tax liability last year.").
		Paragraph("A quarter is due by April 30, June 15, September 15 and the next January 15. The first is April 30, not April 15, because Delaware's return is due April 30. A date on a weekend or Delaware holiday becomes the next business day. Withholding counts in the period it was taken out, not evenly across the year. Withholding from January to March counts toward April 30. April and May withholding counts toward June 15, and so on. If your income came unevenly, Form PIT-UND has an annualised method. It uses 22.5%, 45%, 67.5% and 90% of the tax on annualised income.").
		Paragraph("The law sets the penalty at 1.5% a month, or part of a month. The rate doesn't change with interest rates. The form charges by the day instead, 0.05% for each day an installment is late. Thirty days come to 1.5%. It runs from the due date, or May 1 for the first installment. It stops when the shortfall is paid, or on April 30 of the next year. The form's Short Method lets you pay 12% of the total shortfall instead. It is open if you made no estimated payments, or four equal ones on time.").
		Paragraph("The Division can waive the penalty in two cases. One is a casualty, disaster or other unusual circumstance. The other is that you retired after 62 or became disabled this year or last. You don't claim either on the form. You wait for the penalty to be assessed, then write to the Tax Conferee.").
		VersusFederal("Delaware's line is $800 of required payment less withholding, not $1,000 of tax. The penalty is a fixed 1.5% a month, not a rate set each quarter. It is charged at 0.05% a day, about 18% a year. Withholding counts when it was taken out, and there is a 12% Short Method. Federal Form 2210 is not accepted in place of PIT-UND.").
		Source("30 Del. C. §§ 535(b), 551(d), 1168, 1169, 1170").
		Source("Form PIT-EST, Declaration of Estimated Income Tax for Individuals, 2026 (revised 11/10/25)").
		Source("Form PIT-UND, Delaware Underpayment of Estimated Taxes, and its instructions, 2025 (revised 11/10/25)").
		Source("Form PIT-RES Resident Instructions, 2025, Requirement to File Estimated Taxes").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
