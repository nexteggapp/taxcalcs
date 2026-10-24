package newmexicotaxcalc

import (
	"errors"
	"fmt"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// New Mexico's estimated tax rules, NMSA 1978 § 7-2-12.2, as the shared
// [estimated.EstimatedTax] interface. New Mexico calls the charge a penalty
// and figures it as interest.
//
// New Mexico follows the federal shape: four 25% installments, the lesser of
// 90% of this year's tax or 100% of last year's, payments applied to the
// earliest installment first, and the federal underpayment rate quarter by
// quarter. It differs in the $1,000 threshold, no 110% tier, a full
// exemption for first-year residents, a no-liability exception only for
// full-year residents, and a late start instead of an annualized method.
//
// The figure follows Form RPD-41272 for 2025, the latest published, with the
// 2026 dates and rates, and the result's Divergences say where the statute
// reads otherwise. The statute is the text Laws 2011, HB 470 enacted for tax
// years from 2012; the 2024 to 2026 Tables of Changes list no amendment to it
// or to § 7-1-67 (research/estimated-payments/nm.md), so the form does not
// predate the law it describes.
//
// The rate for the first quarter after the tax year also covers April 1 to
// 15. The rate from January 1, 2027 is not published, so those days are not
// charged and the penalty reports the gap.
//
// Not built: withholding under the Oil and Gas Proceeds and Pass-Through
// Entity Withholding Tax Act, which a caller passes with the rest; and the
// separate election for wage and other withholding, which here is one
// election for all of it.

// jurisdiction is New Mexico's postal code.
const jurisdiction = estimated.NewMexico

// EstimatedFacts are the facts New Mexico's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	firstYear, firstYearAnswered bool
	farmerLast, farmerAnswered   bool
	moves, movesAnswered         bool
}

// FirstYearResident reports whether this is the filer's first year as a New
// Mexico resident, which § 7-2-12.2(M) takes out of estimated tax, and
// whether the caller said.
func (e EstimatedFacts) FirstYearResident() (first, answered bool) {
	return e.firstYear, e.firstYearAnswered
}

// FarmerLastYear reports whether at least two-thirds of last year's gross
// income was from farming or ranching, which puts this year under the
// farmers' rules, and whether the caller said.
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

func (b *EstimatedFactsBuilder) FirstYearResident(first bool) *EstimatedFactsBuilder {
	b.e.firstYear, b.e.firstYearAnswered = first, true
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

	// threshold is RPD-41272 line 4 and § 7-2-12.2(H)(1).
	threshold money.USCents

	thisYearShare, priorYearShare money.Rate
	shares                        []money.Rate
	dues                          []estimated.Date
	calendar                      estimated.Calendar

	// laterGroups are the first days of the late-start groups in
	// § 7-2-12.2(C): a requirement that first arises on or after
	// laterGroups[i] starts at installment i+2.
	laterGroups []estimated.Date

	// fileAndPayBy is § 7-2-12.2(I).
	fileAndPayBy estimated.Date

	stop  estimated.Date
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns New Mexico's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return newMexicoEstimated{y}, nil
}

type newMexicoEstimated struct{ y estimatedYear }

type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents
	excused      bool
	extras       EstimatedFacts
	notes        working.Notes
}

func (r newMexicoEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("newmexicotaxcalc: the facts carry %T, not newmexicotaxcalc.EstimatedFacts", s)
	}
}

func (r newMexicoEstimated) check(f estimated.Facts, extras EstimatedFacts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("newmexicotaxcalc: these are New Mexico's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("newmexicotaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case int64(f.FarmShare())*3 >= 20_000:
		return fmt.Errorf("%w: a farmer or rancher pays once by January 15 or files and pays by March 1, and owes a penalty only past a one-third underpayment, which is not built", estimated.ErrUnsupported)
	}
	if farmer, _ := extras.FarmerLastYear(); farmer {
		return fmt.Errorf("%w: two-thirds of last year's gross income from farming or ranching puts this year under the farmers' rules, which are not built", estimated.ErrUnsupported)
	}
	return nil
}

// divergences are where the department's documents and the statute part
// ways on a rule every New Mexico computation uses. The web page, the
// line 4 wording and FYI-320 are in research/estimated-payments/nm.md; the
// (H)(3) exception was found reading RPD-41272 against the statute.
func (r newMexicoEstimated) divergences(t touches) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Paying the required annual payment").
			Form("No penalty if the required annual payment was paid in timely installments.",
				"Form RPD-41272 (2025) instructions, How To Avoid a Penalty").
			Statute("No penalty if you paid the required annual payment, on time or not. Withholding and estimated payments both count.",
				"NMSA 1978 § 7-2-12.2(H)(3), as amended by Laws 2011, ch. 156 (HB 470)").
			Uses(working.ReadingForm).Affects(t.paidLate),
		working.NewDivergence("The withholding line 4 compares").
			Form("Line 4 compares this year's tax with the income tax withheld on the 2024 PIT-1. The form's own header note uses this year's withholding.",
				"Form RPD-41272 (2025), line 4 and How To Use This Form").
			Statute("The test is the tax for the year less the amount withheld for that year.",
				"NMSA 1978 § 7-2-12.2(H)(1)").
			Uses(working.ReadingStatute).Affects(t.withholdingYear),
		working.NewDivergence("The $1,000 test").
			Form("The department's page subtracts estimated payments and withholding from the tax. It computes a penalty when the rest is more than $1,000. Form RPD-41272 stops when tax less withholding is under $1,000.",
				"Taxation and Revenue Department, Estimated Payments; Form RPD-41272 (2025), line 4").
			Statute("No penalty if tax less withholding alone is under $1,000. At exactly $1,000 the penalty can apply.",
				"NMSA 1978 § 7-2-12.2(H)(1)").
			Uses(working.ReadingForm).Affects(t.pageThreshold),
		working.NewDivergence("An annualized method").
			Form("Form RPD-41272 has no annualized method. It has only a late start for a filer not required to pay until after March 31. FYI-320 (Rev. 6/2014) describes a modified annualization.",
				"Form RPD-41272 (2025), Section 3 instruction 9; FYI-320 (Rev. 6/2014)").
			Statute("The statute has only the late start. It is 50%, 75% or 100% at the first installment that applies.",
				"NMSA 1978 § 7-2-12.2(C)").
			Uses(working.ReadingForm).Affects(t.annualised),
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
	paidLate        bool
	withholdingYear bool
	pageThreshold   bool
	annualised      bool
}

func (r newMexicoEstimated) assess(f estimated.Facts) (assessment, error) {
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	if err := r.check(f, extras); err != nil {
		return assessment{}, err
	}
	y := r.y
	a := assessment{extras: extras}
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	last := y.year - 1

	if f.FarmShare() > 0 {
		if _, answered := extras.FarmerLastYear(); !answered {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Was at least two-thirds of your %d gross income from farming or ranching?", last),
				"If so, New Mexico's farmers' rules apply instead.")
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
		a.notes.Rule("NMSA 1978 § 7-2-12.2(B)(1)(a); Form RPD-41272 (2025), line 5", fmt.Sprintf("%v of this year's New Mexico tax after rebates and credits of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	// Questions about the target, asked only when no exception sets it.
	var unlessExempt []func()
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d New Mexico return", last))
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d New Mexico return, and what was its tax?", last),
				fmt.Sprintf("With it, 100%% of your %d tax could be a lower target than 90%% of this year's.", last))
		})
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d New Mexico return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d New Mexico return covered %d months, not a full year", last, prior.Months()))
	default:
		amount := y.priorYearShare.Of(max(0, priorTax))
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, amount)
		a.notes.Rule("NMSA 1978 § 7-2-12.2(B)(1)(b); Form RPD-41272 (2025), line 6", fmt.Sprintf("the prior-year test is %v of the %v tax after rebates and credits on the %d return", y.priorYearShare, priorTax, last))
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
			if priorOpt.Available() {
				a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d New Mexico tax to be?", y.year),
					fmt.Sprintf("90%% of it could be a lower target than your %d tax. It also decides if the %s threshold excuses you.", last, y.threshold.Whole()))
			} else {
				a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d New Mexico tax to be?", y.year),
					fmt.Sprintf("With no full-year %d New Mexico return, there's no target without it.", last))
			}
		})
	}

	first, _ := extras.FirstYearResident()
	noLiability := hasPrior && prior.Months() == 12 && ((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	resident, residenceAnswered := prior.FullYearResident()
	if noLiability && !residenceAnswered {
		a.notes.Ask(working.PositionResidency, fmt.Sprintf("Did you live in New Mexico all of %d?", last),
			fmt.Sprintf("If you did, no New Mexico tax liability for %d means no penalty for %d.", last, y.year))
	}
	exemptByOther := first || (noLiability && residenceAnswered && resident)
	switch {
	case first:
		exception, err := estimated.NewException("First year as a New Mexico resident").Build()
		if err != nil {
			return assessment{}, err
		}
		a.exempt = true
		rb.Required(false, fmt.Sprintf("This is your first year as a New Mexico resident. New Mexico requires no estimated payments of first-year residents. None are required for %d.", y.year)).Exempt(exception)
		a.notes.Rule("NMSA 1978 § 7-2-12.2(M); Form RPD-41272 (2025), Section 2 note", "the section does not apply to first-year residents")
	case noLiability && residenceAnswered && resident:
		exception, err := estimated.NewException("No New Mexico tax due last year as a full-year resident").Build()
		if err != nil {
			return assessment{}, err
		}
		a.exempt = true
		rb.Required(false, fmt.Sprintf("You had no New Mexico tax liability for all 12 months of %d as a resident. No estimated payments are required for %d.", last, y.year)).Exempt(exception)
		a.notes.Rule("NMSA 1978 § 7-2-12.2(H)(2)", fmt.Sprintf("the %d return showed no liability for a full 12 months of residence, so there is no penalty for %d", last, y.year))
	case taxKnown && tax-withheld < y.threshold:
		exception, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		a.exempt = true
		why := fmt.Sprintf("That's under %s, so no estimated payments are needed.", y.threshold.Whole())
		if tax < withheld {
			why = "No estimated payments are needed."
		}
		rb.Required(false, fmt.Sprintf("Your %s New Mexico tax less %s withheld %s. %s",
			tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld), why)).Exempt(exception)
		a.notes.Rule("NMSA 1978 § 7-2-12.2(H)(1); Form RPD-41272 (2025), line 4", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no penalty", tax, withheld, tax-withheld, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("There's no target yet. It needs your %d New Mexico tax or a full-year %d New Mexico return.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches your %d tax, %s. No estimated payments are needed, whatever your %d tax is.",
			withheld.Whole(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is your %d tax, %s. It keeps you clear of a penalty whatever your %d tax turns out to be. After %s of withholding, that's %s of estimated payments. Pay a quarter by each due date.",
			last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your New Mexico withholding of %s covers the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your %s New Mexico tax less %s withheld leaves %s. That's at least %s, so you need %s of estimated payments. Pay a quarter by each due date.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.threshold.Whole(), (target-withheld).Whole())).Choose(chosen)
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
	// § 7-2-12.2(C): a filer first required to pay after March 31 catches up
	// at the first installment that applies.
	start := 0
	if met := f.RequirementFirstMet(); !met.IsZero() {
		for i, g := range y.laterGroups {
			if !met.Before(g) {
				start = i + 1
			}
		}
	}
	for i := 0; i < start; i++ {
		cumulative[i] = 0
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("NMSA 1978 § 7-2-12.2(C), (D); Form RPD-41272 (2025), Section 3 instructions 8 and 9", fmt.Sprintf("of %v, %v, %v, %v and %v are due on %s, %s, %s and %s", a.requirement.Target(),
		a.installments[0], a.installments[1], a.installments[2], a.installments[3], a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
	if f.Elections().Annualised() {
		a.notes.Rule("NMSA 1978 § 7-2-12.2(C); Form RPD-41272 (2025)", "New Mexico has no annualized method; a filer whose income came late gives the date the requirement arose, and the late-start schedule applies")
	}

	// § 7-2-12.2(I). January 31, 2027 is a Sunday, and no source says
	// whether the day moves.
	fileBy := y.fileAndPayBy
	moves, movesAnswered := extras.JanuaryThirtyFirstMoves()
	if moves {
		fileBy = y.calendar.NextBusinessDay(fileBy)
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && !a.exempt {
		if !movesAnswered && filed.After(fileBy) && !filed.After(y.calendar.NextBusinessDay(fileBy)) {
			a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("%s, the date to file and pay by, is a %s. No source says if %s counts, and the figures treat it as late.", fileBy.Long(), fileBy.Weekday(), filed.Long()))
		}
		if !filed.After(fileBy) {
			a.excused = true
			a.notes.Rule("NMSA 1978 § 7-2-12.2(I); Form RPD-41272 (2025) instructions, How To Avoid a Penalty", fmt.Sprintf("the return was filed and paid on %s, by %s, so the January installment bears no penalty", filed.Long(), fileBy.Long()))
		}
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	var paid money.USCents
	for _, p := range f.Payments() {
		paid += p.Amount()
	}
	t := touches{annualised: f.Elections().Annualised() && !a.exempt}
	if taxKnown && !exemptByOther {
		t.pageThreshold = tax-withheld >= y.threshold && tax-withheld-paid <= y.threshold
		t.withholdingYear = tax-withheld < y.threshold
		if hasPrior {
			if pf, ok := prior.Figures(); ok {
				if priorWithheld, known := pf.Withheld(); known {
					t.withholdingYear = (tax-priorWithheld < y.threshold) != (tax-withheld < y.threshold)
				}
			}
		}
	}
	if !a.exempt && a.requirement.TargetKnown() && withheld+paid >= a.requirement.Target() {
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return assessment{}, err
		}
		for _, l := range ledger {
			if l.Shortfall() > 0 {
				t.paidLate = true
			}
		}
	}
	divergences, err := r.divergences(t)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

func (r newMexicoEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r newMexicoEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
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
		notes.Rule("NMSA 1978 § 7-2-12.2(F); Form RPD-41272 (2025), exception 1", fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("NMSA 1978 § 7-2-12.2(F), (G)(3); Form RPD-41272 (2025), Section 3 instruction 11", fmt.Sprintf("an equal part of the %v withheld is treated as paid on each due date, and a payment goes first to the earliest installment", f.WithheldTotal()))
	}
	return ledger, notes, nil
}

func (r newMexicoEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d New Mexico return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
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

// schedule is the installments the ledger gives.
func (r newMexicoEstimated) schedule(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
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

func (r newMexicoEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("newmexicotaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d New Mexico return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := asOf
	if y.stop.Before(stop) {
		stop = y.stop
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop = filed
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
	if a.exempt {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		return working.NewResult(p).With(a.notes).Build()
	}

	// RPD-41272 Sections 5 and 6: each part of each shortfall, by the day ÷
	// 365, at each quarter's rate, until paid or April 15.
	p, unrated, err := estimated.SimplePenalty(ledger, stop, y.rates, waiver, a.requirement, schedule)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("NMSA 1978 § 7-2-12.2(B)(1)", fmt.Sprintf("this year's New Mexico tax was not given, so the penalty is figured against the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower", y.year-1, p.Total()))
	}
	notes.Rule("NMSA 1978 §§ 7-2-12.2(G), 7-1-67(B); Form RPD-41272 (2025), lines 21 to 34", fmt.Sprintf("each part of each shortfall is charged the quarter's rate by the day ÷ 365, from its due date until paid or %s; "+
		"the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", unrated.AddDays(-1).Long()))
	}
	if p.Total() > 0 {
		if !waiverAnswered {
			notes.Ask(working.PositionPenaltyWaiver, "Are you asking for a waiver for lack of fraud, negligence or disregard of rules?",
				"The figures show the full penalty until the department finds that and waives it.")
		}
		if _, answered := a.extras.FirstYearResident(); !answered {
			notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Is %d your first year as a New Mexico resident?", y.year),
				"If it is, you need no estimated payments and no penalty is due.")
		}
		if f.RequirementFirstMet().IsZero() {
			notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When in %d did your income first make estimated payments necessary?", y.year),
				"If after March 31, you catch up at a later due date. Until then the figures assume you needed them from January 1.")
		}
	}
	if o, ok := newMexicoWaiver(f); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Ask(working.PositionUnsettledRule, "", "Waivers need the department to find no fraud, negligence or disregard of rules. The law doesn't say if casualty, disaster, retirement or disability shows that.")
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// newMexicoWaiver decides a waiver asked for. New Mexico has no casualty,
// retirement or disability waiver; NMSA 1978 § 7-2-12.2(H)(4) waives the
// penalty when the secretary finds the underpayment wasn't due to fraud,
// negligence or disregard of the rules, and names no causes, so every
// request is undecided.
func newMexicoWaiver(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	k, answered := f.Positions().Waiver()
	switch {
	case !answered || k == estimated.WaiverNone:
		return estimated.WaiverOutcome{}, false
	case k == estimated.WaiverCasualty:
		return estimated.WaiverUndecided("New Mexico has no waiver for a casualty or disaster as such. The department waives it if it finds no fraud, negligence or disregard of rules."), true
	default:
		return estimated.WaiverUndecided("New Mexico has no waiver for retirement or disability. The department waives it if it finds no fraud, negligence or disregard of rules."), true
	}
}

func (r newMexicoEstimated) Explanation() estimated.Explanation { return newMexicoExplanation }

// newMexicoExplanation is written by hand and checked against
// research/estimated-payments/nm.md.
var newMexicoExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"New Mexico expects estimated payments from anyone who files a return. It charges no penalty unless your tax less withholding is $1,000 or more. The penalty is interest on each quarter's shortfall. It is charged at the federal underpayment rate.").
		Paragraph("You are clear if withholding and estimated payments reach your target on time. It is the smaller of 90% of this year's New Mexico tax or 100% of last year's. A quarter is due by each of April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. Last year's figure needs a full-year New Mexico return. Withholding counts as paid evenly unless you show when it was taken.").
		Paragraph("You need no estimated payments if last year's full-year return had no tax due. You must also have lived in New Mexico all of that year. Late income can mean you first have to pay after March 31. If so, you catch up at the next date instead. Pay half by June 15, three-quarters by September 15, or all of it by January 15. Filing and paying in full by January 31 clears the January payment's penalty.").
		Paragraph("The penalty runs daily from each due date until paid or the next April 15. Each payment goes first to the oldest shortfall. The rate follows the federal one quarter by quarter. It is 6% from April to June 2026 and 7% from July. The department can waive the penalty. That needs an underpayment not due to fraud or negligence.").
		VersusFederal("New Mexico has no 110% tier. The no-liability exception needs a full year of New Mexico residence. There is no annualized method. A late start catches up at the first date that applies. The first quarter's rate after the year also covers April 1 to 15.").
		Source("NMSA 1978 § 7-2-12.2, as amended by Laws 2011, HB 470").
		Source("Form RPD-41272, 2025 Calculation of Estimated Personal Income Tax Underpayment Penalty, and its instructions").
		Source("Instructions for PIT-ES, New Mexico Estimated Tax Payment Voucher").
		Source("Taxation and Revenue Department, Estimated Payments and Penalty Interest Rates").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()

// date is a shorthand for the year files.
func date(y int, m time.Month, d int) estimated.Date { return estimated.MustDate(y, m, d) }
