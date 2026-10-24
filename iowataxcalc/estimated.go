package iowataxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Iowa's estimated tax rules, Iowa Code § 422.16(12), as the shared
// [estimated.EstimatedTax] interface.
//
// § 422.16(12)(d)(3) adopts the method of 26 U.S.C. § 6654 at Iowa's own
// interest rate under § 421.7. The shape is therefore federal: the lesser of
// 90% of this year's tax and 100% or 110% of last year's, four equal
// installments, and interest by the day. The figures and dates are Iowa's:
// installments at the end of April, June, September and January, a $1,000
// threshold from 2026, a late-start table that gives fewer installments, and
// a penalty that runs to April 30.
//
// The figure follows the Department of Revenue's forms, the 2026 IA 1040ES
// and the 2025 IA 2210, except that the 2025 IA 2210's $200 threshold
// predates the act that set $1,000 for 2026, so the act's figure is used. The
// result's Divergences say where form and law differ.

// jurisdiction is Iowa's postal code.
const jurisdiction = estimated.Iowa

// EstimatedFacts are the facts Iowa's rules read that no other jurisdiction
// does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorModifications    money.USCents
	priorModificationsSet bool
}

// PriorYearModifications is last year's Iowa net modifications, IA 1040 line
// 3. IA 2210 line 7 adds them to last year's federal AGI to decide whether
// the prior-year test is 110% rather than 100%. They can be negative.
func (e EstimatedFacts) PriorYearModifications() (money.USCents, bool) {
	return e.priorModifications, e.priorModificationsSet
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorYearModifications(a money.USCents) *EstimatedFactsBuilder {
	b.e.priorModifications, b.e.priorModificationsSet = a, true
	return b
}

// Build accepts any modifications, since they can be negative.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is § 422.16(12)(a)(1): estimates are required only when the
	// tax after withholding can be expected to reach it.
	threshold money.USCents

	// formThreshold is the 2025 IA 2210's small-balance test, lines 3 and 6,
	// which predates the act that set threshold. It is reported, not used.
	formThreshold money.USCents

	// thisYearShare, priorYearShare and priorYearShareHigh are IA 2210 lines
	// 4 and 7. The high share applies when last year's federal AGI plus Iowa
	// net modifications exceeds priorYearStep, or priorYearStepSeparate for a
	// married filing separate federal return.
	thisYearShare, priorYearShare, priorYearShareHigh money.Rate
	priorYearStep, priorYearStepSeparate              money.USCents

	dues []estimated.Date

	// laterGroups are the first days of the IA 2210 table's second, third and
	// fourth rows: a requirement first met on or after laterGroups[i] starts
	// at installment i+2.
	laterGroups []estimated.Date

	calendar estimated.Calendar

	// stop is IA 2210 line 23b: the penalty runs no later than April 30 of
	// the next year.
	stop estimated.Date

	// daily is the factor IA 2210 line 24 prints for each calendar year.
	daily estimated.FactorSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are IA 2210 line 20: an equal part of the withholding is treated
// as paid on each installment date.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Iowa's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return iowaEstimated{y}, nil
}

type iowaEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// first is the installment a late starter's requirement starts at, from
	// 0. installments are each installment's own amount; those before first
	// are zero.
	first        int
	installments []money.USCents

	notes working.Notes
}

func (r iowaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("iowataxcalc: the facts carry %T, not iowataxcalc.EstimatedFacts", s)
	}
}

func (r iowaEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("iowataxcalc: these are Iowa's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("iowataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: farmers and fishers with two-thirds of gross income from farming or fishing file IA 2210F, which is not built",
			estimated.ErrUnsupported)
	case f.Elections().Annualised():
		return fmt.Errorf("%w: the annualised method is IA 2210 Schedule AI (45-011), which is not built",
			estimated.ErrUnsupported)
	}
	return nil
}

// divergences are where the Department of Revenue's forms and Iowa law part
// ways on a rule every Iowa computation uses. They are found in
// research/estimated-payments/ia.md and checked against the extracts in
// testdata/. The affects flags say whether each changes anything for the
// facts.
func (r iowaEstimated) divergences(threshold, factor, waiver bool) ([]working.Divergence, error) {
	y := r.y
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The small-balance threshold").
			Form(fmt.Sprintf("The %d IA 2210 charges nothing below %s of tax after credits. It also charges nothing below %s of tax after withholding. It was printed before the law raised the threshold for %d.", y.year-1, y.formThreshold.Whole(), y.formThreshold.Whole(), y.year),
				"2025 IA 2210 (45-007a, 10/22/2025), lines 3 and 6; no 2026 IA 2210 is published").
			Statute(fmt.Sprintf("From %d, no payments are needed below %s of Iowa tax after withholding. No penalty can arise below it either.", y.year, y.threshold.Whole()),
				"Iowa Code § 422.16(12)(a)(1) (2026), amended by 2025 Iowa Acts ch. 148, § 59, effective January 1, 2026, and applying under § 60 to tax years beginning on or after that date").
			Uses(working.ReadingStatute).Affects(threshold),
		working.NewDivergence("The daily interest factor").
			Form("Each day is charged the printed factor of .00027397. That is 10% divided by 365, rounded to eight places.",
				"2025 IA 2210 (45-007b), lines 24a and 24b, and its instructions for line 24 (01/01/26 - 12/31/26)").
			Statute(fmt.Sprintf("The charge is Iowa's interest rate for the year, 10%% for %d. Divided by 365, that is 0.000273973 a day.", y.year),
				"Iowa Code §§ 422.16(12)(d)(3), 421.7(2); IDR press release, October 21, 2025").
			Uses(working.ReadingForm).Affects(factor),
		working.NewDivergence("The waiver for disability in the year before").
			Form("The waiver covers retiring at 62 or later, or disability, in the tax year. That is the year the payments were for. Disability in the year before isn't mentioned.",
				"2025 IA 2210 instructions (45-007d), Waiver of Penalty").
			Statute("The waiver also covers disability in the year before, with reasonable cause.",
				"Iowa Admin. Code r. 701—308.6(2)(b), implementing Iowa Code § 422.16; IDR, Estimated Income Tax Payments, Can Penalty Be Waived?").
			Uses(working.ReadingForm).Affects(waiver),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// near is amount in whole dollars, or "just over" or "just under" limit
// when rounding would show it as limit.
func near(amount, limit money.USCents) string {
	switch {
	case amount > limit && amount.Whole() == limit.Whole():
		return "just over " + limit.Whole()
	case amount < limit && amount.Whole() == limit.Whole():
		return "just under " + limit.Whole()
	}
	return amount.Whole()
}

// shortDate is "Apr 30" inside the tax year and "Feb 1, 2027" outside it.
func shortDate(d estimated.Date, year int) string {
	m := d.Month().String()[:3]
	if d.Year() == year {
		return fmt.Sprintf("%s %d", m, d.Day())
	}
	return fmt.Sprintf("%s %d, %d", m, d.Day(), d.Year())
}

func (r iowaEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment

	step := y.priorYearStep
	if f.FilingStatus() == filing.MarriedFilingSeparately {
		step = y.priorYearStepSeparate
	}
	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	last := y.year - 1

	// IA 2210 lines 3 and 4: 90% of the tax after refundable credits.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("Iowa Code § 422.16(12)(d)(3); 2025 IA 2210, lines 3 and 4", fmt.Sprintf("%v of this year's Iowa tax after refundable credits of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// IA 2210 line 7: last year's tax, 100% or 110%. A twelve-month year
	// with no tax, or one with no return because none was required, gives a
	// required payment of nothing.
	noLiability := false
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Iowa return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Iowa return covered %d months, not 12", last, prior.Months()))
	case prior.NotRequiredToFile():
		noLiability = true
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0)
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Iowa return", last))
	default:
		pf, _ := prior.Figures()
		priorTax, _ := pf.Tax()
		if priorTax == 0 {
			noLiability = true
			priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0)
			break
		}
		agi, agiKnown := pf.FederalAGI()
		mods, modsKnown := extras.PriorYearModifications()
		share := y.priorYearShare
		var applied string
		switch {
		case !agiKnown:
			share = y.priorYearShareHigh
			applied = fmt.Sprintf("%d federal AGI was not given", last)
			a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d federal AGI?", last),
				fmt.Sprintf("It's %v if AGI plus Iowa net modifications tops %s, and %v otherwise. Until then, last year's test uses %v.",
					y.priorYearShareHigh, step.Whole(), y.priorYearShare, y.priorYearShareHigh))
		case !modsKnown:
			share = y.priorYearShareHigh
			applied = fmt.Sprintf("%d Iowa net modifications were not given", last)
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What were your %d Iowa net modifications (IA 1040 line 3)?", last),
				fmt.Sprintf("With federal AGI they set last year's test, %v or %v; until then it's %v.",
					y.priorYearShare, y.priorYearShareHigh, y.priorYearShareHigh))
		case agi+mods > step:
			share = y.priorYearShareHigh
			applied = fmt.Sprintf("%d federal AGI of %v plus %v of Iowa net modifications is more than %v", last, agi, mods, step)
		default:
			applied = fmt.Sprintf("%d federal AGI of %v plus %v of Iowa net modifications is not more than %v", last, agi, mods, step)
		}
		amount := share.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, share, amount)
		a.notes.Rule("Iowa Code § 422.16(12)(d)(3); 2025 IA 2210, line 7 and How to Avoid Underpayment Penalty",
			fmt.Sprintf("%s, so the prior-year test is %v of the %v tax on the %d return, or %v", applied, share, priorTax, last, amount))
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
	if !taxKnown && !noLiability {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Iowa tax to be?", y.year),
			fmt.Sprintf("It sets the %v option and decides whether you're under the %s threshold.", y.thisYearShare, y.threshold.Whole()))
	}

	switch {
	case noLiability:
		a.exempt = true
		none, err := estimated.NewException("No Iowa tax due last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no Iowa tax due for %d, a full twelve-month year. No estimated payments are needed for %d, and no penalty can arise.", last, y.year)).Exempt(none)
		a.notes.Rule("Iowa Code § 422.16(12)(d)(3); Iowa Admin. Code r. 701—308.6(1)(a); 2025 IA 2210, lines 7 and 8",
			fmt.Sprintf("the %d year covered 12 months with no Iowa tax, so the smaller of the two tests is nothing and there is no penalty for %d", last, y.year))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target needs your %d Iowa tax, because your %d return can't set one.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax is.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. After %s of withholding, that's %s of estimated payments. That keeps you clear of a penalty whatever your %d tax is.",
			priorOpt.Percent(), last, target.Whole(), withheld.Whole(), (target-withheld).Whole(), y.year)).Choose(chosen)
	case tax-withheld < y.threshold:
		a.exempt = true
		under, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		leaves := "is " + (tax - withheld).Whole() + ", under " + y.threshold.Whole()
		if n := near(tax-withheld, y.threshold); n != (tax - withheld).Whole() {
			leaves = "is " + n
		}
		if tax < withheld {
			leaves = estimated.Leaves(tax - withheld)
		}
		rb.Required(false, fmt.Sprintf("Your Iowa tax of %s less %s withheld %s. No estimated payments are needed, and no penalty can arise.",
			tax.Whole(), withheld.Whole(), leaves)).Exempt(under)
		a.notes.Rule("Iowa Code § 422.16(12)(a)(1), as amended by 2025 Iowa Acts ch. 148, § 59; 2026 IA 1040ES, line 15",
			fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so no estimated payments are required and there is no penalty", tax, withheld, tax-withheld, y.threshold))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Iowa withholding of %s already reaches your target of %s. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Iowa tax of %s less %s withheld is %s, at least %s. You need %s of estimated payments across the year. That reaches your target of %s.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.threshold.Whole(), (target-withheld).Whole(), target.Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// Under an exception last year's return can't change the target.
	if !hasPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Iowa return, and what was its tax?", last),
			fmt.Sprintf("With it, your target could be %v of last year's tax, not %v of this year's.", y.priorYearShare, y.thisYearShare))
	}

	// The due dates: § 421.9A moves one on a weekend or holiday.
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			a.notes.Rule("Iowa Code § 421.9A; 2026 IA 1040ES, Due dates", fmt.Sprintf("the installment due %s falls on a weekend or holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
		}
	}

	// A late starter: the IA 2210 table.
	if firstMet := f.RequirementFirstMet(); !firstMet.IsZero() {
		a.first = estimated.LateStart(firstMet, y.laterGroups)
	} else if a.requirement.Required() {
		a.notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first expect to need estimated payments for %d?", y.year),
			fmt.Sprintf("If it was after %s, your installments are fewer and larger. They start at the next due date.", shortDate(y.laterGroups[0].AddDays(-1), y.year)))
	}

	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
		a.first = 0
	} else {
		n := len(a.dues) - a.first
		cum, err := estimated.EqualShares(target, len(a.dues), a.first)
		if err != nil {
			return assessment{}, err
		}
		a.installments = estimated.Increments(cum)
		if a.first == 0 {
			a.notes.Rule("Iowa Code § 422.16(12)(b)(1); 2026 IA 1040ES, line 17 and Estimated Tax Payment Schedule; 2025 IA 2210, line 17",
				fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", target, a.installments[0],
					a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
		} else {
			a.notes.Rule("2025 IA 2210, Period Requirements First Met table and line 17",
				fmt.Sprintf("the requirement was first met on %s, so %v is divided among %d installments, the first due %s; nothing is due before it",
					f.RequirementFirstMet().Long(), target, n, a.dues[a.first].Long()))
		}
	}

	// Whether each divergence changes anything for these facts.
	threshold := taxKnown && tax-withheld >= y.formThreshold && tax-withheld < y.threshold
	factor := false
	if !a.exempt && a.requirement.TargetKnown() {
		if factor, err = r.factorAffects(f, a); err != nil {
			return assessment{}, err
		}
	}
	// The readings differ on the waiver only for disability in the year before.
	pos := f.Positions()
	waiver, _ := pos.Waiver()
	disabledYearBefore := waiver == estimated.WaiverRetiredOrDisabled && pos.WaiverCause() == estimated.CauseDisability && pos.WaiverOn().Year() == last
	divergences, err := r.divergences(threshold, factor, !a.exempt && disabledYearBefore)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

// factorAffects reports whether the printed daily factor and the rate it
// stands for give a different penalty, figured to the latest day the penalty
// can run to.
func (r iowaEstimated) factorAffects(f estimated.Facts, a assessment) (bool, error) {
	y := r.y
	ledger, _, err := r.ledger(f, a)
	if err != nil {
		return false, err
	}
	stop := y.stop
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop = filed
	}
	rb := estimated.NewRateSeries()
	for _, p := range y.daily.Periods() {
		rb.Period(p.From(), p.To(), p.Rate(), p.Source())
	}
	rates, err := rb.Build()
	if err != nil {
		return false, err
	}
	printed, _, err := r.lines(ledger, stop, func(principal money.USCents, from, to estimated.Date) ([]estimated.PenaltyPart, estimated.Date, error) {
		return estimated.DailyFactor(principal, from, to, y.daily)
	})
	if err != nil {
		return false, err
	}
	exact, _, err := r.lines(ledger, stop, func(principal money.USCents, from, to estimated.Date) ([]estimated.PenaltyPart, estimated.Date, error) {
		return estimated.SimpleDaily(principal, from, to, rates)
	})
	if err != nil {
		return false, err
	}
	return total(printed) != total(exact), nil
}

func total(lines []estimated.PenaltyLine) money.USCents {
	var t money.USCents
	for _, l := range lines {
		t += l.Total()
	}
	return t
}

// lines charges each piece of each shortfall from its due date to the day it
// was paid or to stop, by charge.
func (r iowaEstimated) lines(ledger []estimated.LedgerEntry, stop estimated.Date,
	charge func(principal money.USCents, from, to estimated.Date) ([]estimated.PenaltyPart, estimated.Date, error)) ([]estimated.PenaltyLine, estimated.Date, error) {
	var out []estimated.PenaltyLine
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
			parts, gap, err := charge(piece.Amount(), l.Due(), to)
			if err != nil {
				return nil, estimated.Date{}, err
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
			return nil, estimated.Date{}, err
		}
		out = append(out, line)
	}
	return out, unrated, nil
}

func (r iowaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies the credits to the installments.
func (r iowaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, quarters)
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
		notes.Rule("2025 IA 2210, line 20 instructions", fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("2025 IA 2210, line 20 instructions", fmt.Sprintf("the %v withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld", f.WithheldTotal()))
	}
	notes.Rule("2025 IA 2210, line 21 instructions", "each payment goes to the earliest unpaid installment, whichever one it was meant for, and any excess is carried to the next")
	return ledger, notes, nil
}

// schedule is the installments the ledger gives.
func schedule(ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		in, err := estimated.NewInstallment(i+1, l.Due()).NeededBy(l.NeededBy()).CreditedBy(l.CreditedBy()).
			Shortfall(l.Shortfall()).Build()
		if err != nil {
			return nil, err
		}
		out[i] = in
	}
	return out, nil
}

func (r iowaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Iowa return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := schedule(ledger)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

func (r iowaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("iowataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Iowa return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	sched, err := schedule(ledger)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if a.exempt {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, sched).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		return working.NewResult(p).With(a.notes).Build()
	}

	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, sched)
	if !a.taxKnown {
		pb.UpperBound()
	}
	lines, unrated, err := r.lines(ledger, stop, func(principal money.USCents, from, to estimated.Date) ([]estimated.PenaltyPart, estimated.Date, error) {
		return estimated.DailyFactor(principal, from, to, y.daily)
	})
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	for _, l := range lines {
		pb.Line(l)
	}
	p, err := pb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		notes.Rule("Iowa Code § 422.16(12)(d)(3); 2025 IA 2210, line 8", fmt.Sprintf("this year's Iowa tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the smaller of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("2025 IA 2210, line 22 instructions", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("Iowa Code §§ 422.16(12)(d)(3), 421.7; 2025 IA 2210, lines 23a to 26", fmt.Sprintf("each shortfall is charged the printed daily factor for its calendar year, to the cent, for the days after its due date until paid or %s; "+
		"the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", shortDate(unrated.AddDays(-1), y.year)))
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking for a waiver for a casualty, disaster, retirement or disability?",
			"Retiring must be at 62 or later; it and disability need reasonable cause. Other unusual events count; the figures show the full penalty until a waiver.")
	}
	if p.Total() > 0 {
		if o, asked := r.waiverOutcome(f, &notes); asked {
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// waiverOutcome decides a waiver asked for under Iowa Admin. Code r.
// 701—308.6(2) and the 2025 IA 2210 instructions: a casualty, disaster or
// other unusual circumstance; or retirement at 62 or later, or disability, in
// the tax year, with reasonable cause. The form's tax year is followed, and
// the rule's preceding year for disability is the waiver divergence. Whether
// retiring in the preceding year counts is open in both, and is asked. It
// reports false when no waiver was asked for.
func (r iowaEstimated) waiverOutcome(f estimated.Facts, notes *working.Notes) (estimated.WaiverOutcome, bool) {
	const authority = "the Iowa Department of Revenue"
	const authorityTitle = "The Iowa Department of Revenue"
	pos := f.Positions()
	kind, answered := pos.Waiver()
	switch {
	case !answered || kind == estimated.WaiverNone:
		return estimated.WaiverOutcome{}, false
	case kind == estimated.WaiverCasualty:
		return estimated.StandardWaiver(f, authority, 62)
	}
	year, on := r.y.year, pos.WaiverOn()
	if pos.WaiverCause() == estimated.CauseDisability {
		switch {
		case on.IsZero():
			return estimated.WaiverUndecided(fmt.Sprintf("The disability waiver needs the date you became disabled, which must be in %d.", year)), true
		case on.Year() != year:
			return estimated.WaiverRefused(fmt.Sprintf("You became disabled in %d. Iowa's form waives the penalty only for becoming disabled in %d.", on.Year(), year)), true
		}
		return estimated.WaiverEligible(fmt.Sprintf("You became disabled in %d. %s can waive the penalty for reasonable cause.", on.Year(), authorityTitle)), true
	}
	switch {
	case on.IsZero():
		return estimated.WaiverUndecided(fmt.Sprintf("The waiver for retirement needs the date you retired, which must be in %d.", year)), true
	case on.Year() == year-1:
		notes.Ask(working.PositionUnsettledRule, "",
			fmt.Sprintf("Iowa's form waives the penalty for retiring at 62 or later in %d. Neither it nor Iowa's rule says whether retiring in %d also counts.", year, year-1))
		return estimated.WaiverUndecided(fmt.Sprintf("You retired in %d. Iowa hasn't said whether its waiver covers retiring in the year before.", on.Year())), true
	case on.Year() != year:
		return estimated.WaiverRefused(fmt.Sprintf("You retired in %d, and Iowa's waiver covers retiring in %d.", on.Year(), year)), true
	}
	born := pos.Born()
	if born.IsZero() {
		return estimated.WaiverUndecided("The retirement waiver needs your date of birth. It shows you were 62 or older when you retired."), true
	}
	age := on.Year() - born.Year()
	if on.Month() < born.Month() || (on.Month() == born.Month() && on.Day() < born.Day()) {
		age--
	}
	if age < 62 {
		return estimated.WaiverRefused(fmt.Sprintf("You were %d when you retired. Iowa's waiver needs you to have been 62 or older.", age)), true
	}
	return estimated.WaiverEligible(fmt.Sprintf("You retired in %d at %d. %s can waive the penalty for reasonable cause.", on.Year(), age, authorityTitle)), true
}

func (r iowaEstimated) Explanation() estimated.Explanation { return iowaExplanation }

// iowaExplanation is written by hand and checked against
// research/estimated-payments/ia.md.
var iowaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Iowa expects estimated payments once you expect $1,000 or more of tax due. That is Iowa tax beyond your withholding for 2026, up from $200 before. The penalty is interest, by the day, on each installment's shortfall.").
		Paragraph("You avoid the penalty if withholding and estimated payments reach a target. It is the smaller of 90% of this year's Iowa tax or 100% of last year's. The 100% becomes 110% if federal AGI plus Iowa net modifications tops $150,000. The limit is $75,000 if married filing separately. Last year must be a full twelve months. If you had no Iowa tax due for it, no penalty is due this year.").
		Paragraph("A quarter is due at the end of each of April, June, September and January. For 2026 that is April 30, June 30 and September 30, and February 1, 2027. January 31 is a Sunday. Withholding is spread evenly over those dates unless you show when it was taken. If you first expected to need payments after April 1, start at the next date. The year's amount is then split over fewer, larger installments.").
		Paragraph("The penalty is charged at Iowa's interest rate, 10% a year for 2026. It is charged for each day an installment stays short. It stops on April 30 of the next year, when the return is due. A payment goes to the oldest shortfall first. The department can waive the penalty for a casualty or disaster. It can also do so if you retired at 62 or later or became disabled. That needs reasonable cause.").
		VersusFederal("Iowa uses the federal method with its own figures and dates. Payments fall at the end of April, June, September and January. The 110% test adds Iowa's net modifications to federal AGI. A late starter pays in fewer installments. The rate is set yearly, not quarterly. The penalty runs to April 30, not April 15. Farmers and fishers use IA 2210F.").
		Source("Iowa Code §§ 422.16(12), 421.7, 421.9A; 2025 Iowa Acts ch. 148, §§ 59, 60").
		Source("Iowa Admin. Code r. 701—308.6").
		Source("2026 IA 1040ES, Iowa Individual Estimated Income Tax Instructions").
		Source("2025 IA 2210, Iowa Underpayment of Estimated Tax by Individuals, and its instructions").
		Source("Iowa Department of Revenue, IDR Announces 2026 Individual Income Tax and Interest Rates (October 21, 2025)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
