package massachusettstaxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Massachusetts's estimated tax rules, M.G.L. c. 62B §§ 13 and 14, as the
// shared [estimated.EstimatedTax] interface.
//
// The shape is federal: the lesser of a share of this year's tax and all of
// last year's, four equal installments, simple interest on each shortfall.
// The numbers are not: 80% rather than 90%, 100% of last year's tax at every
// income, a $400 threshold, annualised percentages of 20%, 40%, 60% and 80%,
// and a rate set each quarter.
//
// The figure follows the department's forms, the 2026 Form 1-ES and Form
// M-2210, and the result's Divergences say where c. 62B reads otherwise.

// jurisdiction is Massachusetts's postal code.
const jurisdiction = estimated.Massachusetts

// LateStart is the reading used for a filer whose requirement first arises
// after March 31. No Massachusetts source settles it.
type LateStart int

const (
	// LateStartUnanswered is no answer. The rules then use
	// LateStartRemainingDates and say so in Required.
	LateStartUnanswered LateStart = iota

	// LateStartRemainingDates is the department's reading: the required
	// annual payment is divided equally over the dates that remain, from
	// June 15, September 15 or January 15, and nothing is required before.
	// Form 1-ES (2026), AP 241 § 241.3, Form M-2210 line 8.
	LateStartRemainingDates

	// LateStartFourQuarters is the statute's reading: four required
	// installments of 25% whenever the requirement arose, so the earlier ones
	// are short until caught up. M.G.L. c. 62B § 14(c).
	LateStartFourQuarters
)

func (l LateStart) String() string {
	switch l {
	case LateStartRemainingDates:
		return "equal parts over the remaining dates"
	case LateStartFourQuarters:
		return "four installments of 25%"
	default:
		return "unanswered"
	}
}

// EstimatedFacts are the facts and readings Massachusetts's rules read that
// no other jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	lateStart             LateStart
	priorFarmer           bool
	priorFarmerAnswered   bool
	partYearPrior         bool
	partYearPriorAnswered bool
}

// LateStart is the reading to use when [estimated.Facts.RequirementFirstMet]
// is after March 31.
func (e EstimatedFacts) LateStart() LateStart { return e.lateStart }

// PriorYearFarmerOrFisher reports whether last year's return showed at least
// two-thirds of gross income from farming or fishing, which makes the filer a
// farmer or fisherman for this year under c. 62B § 14(h), and whether the
// caller said. [estimated.Facts.FarmShare] covers this year only.
func (e EstimatedFacts) PriorYearFarmerOrFisher() (farmer, answered bool) {
	return e.priorFarmer, e.priorFarmerAnswered
}

// PartYearPriorReturnCounts is the caller's reading of whether last year's
// part-year or nonresident return opens the 100% prior-year test, and whether
// the caller gave one. No Massachusetts source says.
func (e EstimatedFacts) PartYearPriorReturnCounts() (counts, answered bool) {
	return e.partYearPrior, e.partYearPriorAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) LateStart(l LateStart) *EstimatedFactsBuilder {
	b.e.lateStart = l
	return b
}

func (b *EstimatedFactsBuilder) PriorYearFarmerOrFisher(farmer bool) *EstimatedFactsBuilder {
	b.e.priorFarmer, b.e.priorFarmerAnswered = farmer, true
	return b
}

func (b *EstimatedFactsBuilder) PartYearPriorReturnCounts(counts bool) *EstimatedFactsBuilder {
	b.e.partYearPrior, b.e.partYearPriorAnswered = counts, true
	return b
}

// Build refuses a reading this package does not know.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.lateStart < LateStartUnanswered || b.e.lateStart > LateStartFourQuarters {
		return EstimatedFacts{}, fmt.Errorf("massachusettstaxcalc: late-start reading %d is not one this package knows", b.e.lateStart)
	}
	return b.e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// floor is the tax after credits and withholding at or under which no
	// penalty applies.
	floor money.USCents

	thisYearShare, priorYearShare money.Rate

	dues []estimated.Date

	// laterGroups are the first days of the 1-ES's late-start groups: a
	// requirement first met on or after laterGroups[i] starts at installment
	// i+2.
	laterGroups []estimated.Date

	calendar estimated.Calendar

	// stop is the fifteenth day of the fourth month after the year, and
	// lastCharged the last day the M-2210 rate lines count, the day before.
	stop, lastCharged estimated.Date

	periodEnds []estimated.Date
	applicable []money.Rate
	rates      estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

// date prints d for a sentence: "Aug 31" in the tax year, "Jan 15, 2027"
// outside it.
func (y estimatedYear) date(d estimated.Date) string {
	if d.Year() == y.year {
		return fmt.Sprintf("%.3s %d", d.Month(), d.Day())
	}
	return fmt.Sprintf("%.3s %d, %d", d.Month(), d.Day(), d.Year())
}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are c. 62B § 14(c): each required installment is 25% of the
// required annual payment. § 14(f) deems withholding paid in the same equal
// parts.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Massachusetts's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return massachusettsEstimated{y}, nil
}

type massachusettsEstimated struct{ y estimatedYear }

type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// installments are M-2210 line 8, the required installments.
	installments []money.USCents

	notes working.Notes
}

func (r massachusettsEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("massachusettstaxcalc: the facts carry %T, not massachusettstaxcalc.EstimatedFacts", s)
	}
}

func (r massachusettsEstimated) check(f estimated.Facts, e EstimatedFacts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("massachusettstaxcalc: these are Massachusetts's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("massachusettstaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: a farmer or fisherman owes 66 2/3%% and no penalty after filing and paying by March 1 (c. 62B § 14(c), (h)), which is not built",
			estimated.ErrUnsupported)
	}
	if farmer, _ := e.PriorYearFarmerOrFisher(); farmer {
		return fmt.Errorf("%w: last year's return makes the filer a farmer or fisherman under c. 62B § 14(h), whose rules are not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("massachusettstaxcalc: Form M-2210 Part 3 period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms and c. 62B part ways on a
// rule every Massachusetts computation uses: conflicts C1, C2, C3 and C5 of
// the research file. Each says whether it changes anything for f.
func (r massachusettsEstimated) divergences(f estimated.Facts, extras EstimatedFacts, a assessment, atFloor bool) ([]working.Divergence, error) {
	y := r.y
	measured := !a.exempt && a.requirement.TargetKnown()
	lateStart := measured && !f.Elections().Annualised() && estimated.LateStart(f.RequirementFirstMet(), y.laterGroups) > 0
	lateReading := working.ReadingForm
	if extras.LateStart() == LateStartFourQuarters {
		lateReading = working.ReadingStatute
	}
	// A shortfall changes the charge between simple and compound interest,
	// and one still unpaid on the stop date changes the last day charged.
	short, unpaidAtStop := false, false
	if measured {
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return nil, err
		}
		filed := f.ReturnFiled()
		filedEarly := !filed.IsZero() && filed.Before(y.stop)
		for _, l := range ledger {
			if l.Shortfall() <= 0 {
				continue
			}
			short = true
			for _, piece := range l.Pieces() {
				if on, paid := piece.PaidOn(); !filedEarly && (!paid || !on.Before(y.stop)) {
					unpaidAtStop = true
				}
			}
		}
	}
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The $400 floor").
			Form("No penalty if tax after credits and withholding is $400 or less. Exactly $400 bears none.",
				"Form M-2210 (2025; draft 2026), Exceptions which avoid the penalty; AP 241 § 241.4, Exception 1").
			Statute("No penalty only if tax less credits and withholding is under $400. Exactly $400 is charged.",
				"M.G.L. c. 62B § 14(d)(i)").
			Uses(working.ReadingForm).Affects(atFloor),
		working.NewDivergence("Daily compounding").
			Form("The charge is simple interest. It's the underpayment, times the days over 365, times the quarter's rate.",
				"Form M-2210 (2025; draft 2026), Part 2, lines 20 to 23").
			Statute("Unpaid tax bears the federal short-term rate plus four points, compounded daily. The estimated tax section applies it without compounding, matching the form.",
				"M.G.L. c. 62C § 32(a); TIR 26-5; M.G.L. c. 62B § 14(a)").
			Uses(working.ReadingForm).Affects(short),
		working.NewDivergence("A requirement first met after March 31").
			Form("You pay the year's amount in equal parts on the remaining dates. A change from Apr 1 to May 31 starts at Jun 15, and Jun 1 to Aug 31 at Sep 15. After that, it's all on Jan 15, 2027.",
				"Form 1-ES (2026), What if my tax liability changes during the year?; AP 241 § 241.3; Form M-2210 (2025; draft 2026), line 8").
			Statute("There are four installments, each 25% of the required annual payment. That holds whenever the requirement arose. Only the annualised method lowers the earlier ones.",
				"M.G.L. c. 62B § 14(c)").
			Uses(lateReading).Affects(lateStart),
		working.NewDivergence("The last day charged").
			Form("Line 15 counts the days to Apr 15, 2027. The rate lines charge only days before 4/15/27, so Apr 15 bears no interest.",
				"Form M-2210 (draft 2026), Part 2, lines 14, 15 and 19").
			Statute("The underpayment runs to the 15th day of the fourth month after the year. A part paid earlier stops on the day it's paid.",
				"M.G.L. c. 62B § 14(b)").
			Uses(working.ReadingForm).Affects(unpaidAtStop),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (r massachusettsEstimated) assess(f estimated.Facts) (assessment, error) {
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	if err := r.check(f, extras); err != nil {
		return assessment{}, err
	}
	y := r.y
	last := y.year - 1
	var a assessment
	a.dues = estimated.MoveDates(y.calendar, y.dues)

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
	resident, residenceAnswered := prior.FullYearResident()

	// 80% of this year's tax, M-2210 line 4.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("M.G.L. c. 62B § 14(c)(i); Form M-2210 (2025; draft 2026), line 4", fmt.Sprintf("%v of this year's Massachusetts tax after credits of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// 100% of last year's tax, M-2210 line 5, for a filed twelve-month return.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	// Questions whose answer can change the target, asked only when no
	// exception sets it.
	var targetAsks []func()
	var priorOption *estimated.TargetOptionBuilder
	partYearCounts, partYearAnswered := extras.PartYearPriorReturnCounts()
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Massachusetts return", last))
		targetAsks = append(targetAsks, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Massachusetts return, and what was its tax?", last),
				fmt.Sprintf("With it, 100%% of last year's tax could be a target below %v of this year's.", y.thisYearShare))
		})
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Massachusetts return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Massachusetts return covered %d months, not twelve", last, prior.Months()))
	case residenceAnswered && !resident && partYearAnswered && !partYearCounts:
		priorOption = unavailable(fmt.Sprintf("Your %d Massachusetts part-year or nonresident return doesn't count", last))
	default:
		amount := y.priorYearShare.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, amount)
		a.notes.Rule("M.G.L. c. 62B § 14(c)(ii); Form M-2210 (2025; draft 2026), line 5", fmt.Sprintf("%v of the %v tax on the %d return is %v", y.priorYearShare, priorTax, last, amount))
		if residenceAnswered && !resident && !partYearAnswered {
			targetAsks = append(targetAsks, func() {
				a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Should your %d part-year or nonresident Massachusetts return set the target?", last),
					"No Massachusetts source says whether it counts for the last-year target. The figures count it.")
			})
		}
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
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Massachusetts tax after credits to be?", y.year),
			fmt.Sprintf("It sets the %v option and the %s test.", y.thisYearShare, y.floor.Whole()))
	}

	// No liability last year: c. 62B § 14(d)(ii) needs a twelve-month year and
	// residence throughout it.
	noTaxLastYear := hasPrior && prior.Months() == 12 && ((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	if noTaxLastYear && !residenceAnswered {
		a.notes.Ask(working.PositionResidency, fmt.Sprintf("Did you live in Massachusetts all of %d?", last),
			fmt.Sprintf("If you did, having no Massachusetts tax for %d means no penalty for %d.", last, y.year))
	}

	atFloor := false
	switch {
	case noTaxLastYear && resident:
		a.exempt = true
		none, err := estimated.NewException("No tax due last year as a full-year resident").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You lived in Massachusetts all of %d and had no Massachusetts tax for it. No estimated payments are needed for %d.", last, y.year)).Exempt(none)
		a.notes.Rule("M.G.L. c. 62B § 14(d)(ii); Form M-2210 (2025; draft 2026), Exceptions which avoid the penalty", fmt.Sprintf("no liability for %d, a twelve-month year of residence, so there is no penalty for %d", last, y.year))
	case taxKnown && tax-withheld <= y.floor:
		a.exempt, atFloor = true, tax-withheld == y.floor
		left := max(0, tax-withheld)
		small, err := estimated.ExceptionAtMost("Tax less withholding", left, y.floor)
		if err != nil {
			return assessment{}, err
		}
		under := fmt.Sprintf(", which is %s or less", y.floor.Whole())
		if tax < withheld {
			under = ""
		}
		rb.Required(false, fmt.Sprintf("Your Massachusetts tax after credits is %s, and %s was withheld. That %s%s. No estimated payments are needed.",
			tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld), under)).Exempt(small)
		a.notes.Rule("Form M-2210 (2025; draft 2026), Exceptions which avoid the penalty; M.G.L. c. 62B §§ 13, 14(d)(i)", fmt.Sprintf("%v of tax less %v withheld is %v, not more than %v, so there is no penalty", tax, withheld, max(0, tax-withheld), y.floor))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("A target needs your %d Massachusetts tax or a full-year %d return. There's no target yet.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s reaches 100%% of your %d tax, %s. No estimated payments are needed, whatever your %d tax is.",
			withheld.Whole(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is 100%% of your %d tax, %s. That holds whatever your %d tax turns out to be. After %s of withholding, that's %s of estimated payments. Pay a quarter by each date.",
			last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Massachusetts withholding of %s already reaches the %s target. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Massachusetts tax after credits is %s, and %s was withheld. That leaves %s, more than %s, and the target is %s. You need %s of estimated payments, a quarter by each date.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.floor.Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if !a.exempt {
		for _, ask := range targetAsks {
			ask()
		}
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	if !a.exempt && a.requirement.TargetKnown() {
		if _, answered := extras.PriorYearFarmerOrFisher(); !answered {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did at least two-thirds of your %d gross income come from farming or fishing?", last),
				fmt.Sprintf("If so, Massachusetts's farmer and fisherman rules apply to you for %d instead.", y.year))
		}
	}

	if a.exempt || !a.requirement.TargetKnown() {
		a.installments = make([]money.USCents, len(a.dues))
	} else if a.installments, err = r.installments(f, extras, &a, target); err != nil {
		return assessment{}, err
	}
	divergences, err := r.divergences(f, extras, a, atFloor)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

// installments are M-2210 line 8: Part 3 line 31 for the annualised method,
// and otherwise line 6 divided by the number of installments required.
func (r massachusettsEstimated) installments(f estimated.Facts, extras EstimatedFacts, a *assessment, target money.USCents) ([]money.USCents, error) {
	y := r.y
	cum, err := estimated.CumulativeShares(target, quarters)
	if err != nil {
		return nil, err
	}
	regular := estimated.Increments(cum)

	if f.Elections().Annualised() {
		periods := f.Periods()
		taxes := make([]money.USCents, len(periods))
		for i, p := range periods {
			taxes[i] = p.AnnualisedTax()
		}
		ann, err := estimated.Annualised(taxes, y.applicable, regular)
		if err != nil {
			return nil, err
		}
		a.notes.Rule("M.G.L. c. 62B § 14(c); Form M-2210 (2025; draft 2026), Part 3, lines 23 to 31", fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: 20%%, 40%%, 60%% and 80%% of the annualised tax after credits, "+
			"less earlier installments, and never more than a quarter of %v plus any reduction carried forward", ann[0], ann[1], ann[2], ann[3], target))
		return ann, nil
	}

	firstMet := f.RequirementFirstMet()
	reading := extras.LateStart()
	first := estimated.LateStart(firstMet, y.laterGroups)
	if firstMet.IsZero() && a.requirement.Required() && reading != LateStartFourQuarters {
		a.notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first expect to need estimated payments for %d?", y.year),
			fmt.Sprintf("If it was after %s, you pay fewer, larger installments. They start from a later date.", y.date(y.laterGroups[0].AddDays(-1))))
	}
	if first == 0 {
		a.notes.Rule("M.G.L. c. 62B § 14(c); Form 1-ES (2026); Form M-2210 (2025; draft 2026), line 8", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s",
			target, regular[0], a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
		return regular, nil
	}
	if reading == LateStartUnanswered {
		reading = LateStartRemainingDates
		a.notes.Ask(working.PositionJurisdictionFact, "Do you want your installments figured as four of 25% each, as the statute reads?",
			fmt.Sprintf("Until then, the year's amount is spread over the dates from %s. That's what the department says.", y.date(a.dues[first])))
	}
	if reading == LateStartFourQuarters {
		a.notes.Rule("M.G.L. c. 62B § 14(c)", fmt.Sprintf("by the reading given, the requirement arising on %s changes nothing: a quarter of %v, %v, is still required on each of the four dates", firstMet.Long(), target, regular[0]))
		return regular, nil
	}
	late, err := estimated.EqualShares(target, len(a.dues), first)
	if err != nil {
		return nil, err
	}
	out := estimated.Increments(late)
	a.notes.Rule("Form 1-ES (2026), What if my tax liability changes during the year?; AP 241 § 241.3; Form M-2210 (2025; draft 2026), line 8",
		fmt.Sprintf("the requirement first arose on %s, so nothing is required before %s and %v is divided over the %d remaining dates: %v, %v, %v and %v",
			firstMet.Long(), a.dues[first].Long(), target, len(a.dues)-first, out[0], out[1], out[2], out[3]))
	return out, nil
}

func (r massachusettsEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies the credits to the required installments.
func (r massachusettsEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
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
		notes.Rule("M.G.L. c. 62B § 14(f); Form M-2210 (2025; draft 2026), line 9 instructions",
			fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("M.G.L. c. 62B § 14(f); Form M-2210 (2025; draft 2026), line 9 instructions",
			fmt.Sprintf("the %v withheld is treated as paid in equal parts on the four due dates, whenever it was actually withheld", f.WithheldTotal()))
	}
	notes.Rule("M.G.L. c. 62B § 14(b); Form M-2210 (2025; draft 2026), line 12 instructions",
		"a payment is credited to the oldest unpaid installment first, and an overpayment shortens the period of an earlier underpayment rather than reducing it")
	return ledger, notes, nil
}

func (r massachusettsEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Massachusetts return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	out, _, notes, err := r.schedule(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments after the credits, with the ledger they come
// from.
func (r massachusettsEstimated) schedule(f estimated.Facts, a assessment) ([]estimated.Installment, []estimated.LedgerEntry, working.Notes, error) {
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return nil, nil, working.Notes{}, err
	}
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		if out[i], err = estimated.NewInstallment(i+1, l.Due()).NeededBy(l.NeededBy()).CreditedBy(l.CreditedBy()).
			Shortfall(l.Shortfall()).Build(); err != nil {
			return nil, nil, working.Notes{}, err
		}
	}
	return out, ledger, notes, nil
}

func (r massachusettsEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("massachusettstaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Massachusetts return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	sched, ledger, notes, err := r.schedule(f, a)
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
			// Lines 16 to 19 count only days before April 15; line 15 counts
			// to it.
			charged := to
			if y.lastCharged.Before(charged) {
				charged = y.lastCharged
			}
			if !charged.After(l.Due()) {
				continue
			}
			parts, gap, err := estimated.SimpleDaily(piece.Amount(), l.Due(), charged, y.rates)
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
	if !a.taxKnown {
		pb.UpperBound()
	}
	p, err := pb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		notes.Rule("M.G.L. c. 62B § 14(c)", fmt.Sprintf("this year's Massachusetts tax was not given, so the penalty is figured against 100%% of the %d tax alone; "+
			"the required annual payment is the lesser of that and 80%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("M.G.L. c. 62B § 14(b); Form M-2210 (2025; draft 2026), line 14", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("M.G.L. c. 62B § 14(a), (b); Form M-2210 (2025; draft 2026), Part 2", fmt.Sprintf("each shortfall is charged simple interest by the day at the quarter's rate, from its due date until paid or %s, "+
		"with each payment applied to the oldest shortfall first; the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", y.date(unrated.AddDays(-1))))
	}
	switch {
	case p.Total() > 0 && !waiverAnswered:
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking for the penalty to be waived?",
			"The commissioner can waive it for a casualty, disaster or other unusual event. Retiring after 62 or disability this year or last counts with reasonable cause.")
	case waiverAnswered && waiver != estimated.WaiverNone:
		notes.Rule("M.G.L. c. 62B § 14(e); Form M-2210 (2025; draft 2026), Waiver of underpayment penalty", fmt.Sprintf("a waiver for %v is asked for; the commissioner decides it, so the penalty shown is before it", waiver))
	}
	// § 14(e) grants the federal pair: a casualty, disaster or unusual
	// circumstance, and retirement after 62 or disability in this year or
	// last.
	if o, ok := estimated.StandardWaiver(f, "the Massachusetts commissioner", 62); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r massachusettsEstimated) Explanation() estimated.Explanation { return massachusettsExplanation }

// massachusettsExplanation is written by hand and checked against
// research/estimated-payments/ma.md.
var massachusettsExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Massachusetts expects estimated payments if over $400 is due after withholding. The penalty is simple interest on each quarter's shortfall, at a quarterly rate.").
		Paragraph("You avoid the penalty if withholding and estimated payments reach a target. The target is the smaller of 80% of this year's tax or 100% of last year's. Last year's figure needs a return you filed for a full twelve-month year. It stays at 100% however high your income. The 4% surtax on income over $1,107,750 is part of the tax to cover.").
		Paragraph("The payments are four equal quarters. They're due April 15, June 15, September 15 and the next January 15. A weekend or Massachusetts legal holiday shifts a date to the next business day. None of the 2026 dates is shifted. Withholding counts as paid in equal parts on the four dates. You can instead show when it was actually withheld. Filing your return early does not replace the January payment. You may only learn after March 31 that you must pay. The department's instructions then spread the year's amount over the dates left.").
		Paragraph("Tax after credits and withholding of $400 or less this year means no penalty. Nor is there one if you lived in Massachusetts all of last year and had no tax. Otherwise the penalty is interest, not compounded, on each shortfall. It runs from the due date until paid or April 15 of the next year. The rate is the federal short-term rate plus four points. That's 7% from April to June 2026 and 8% after. Form M-2210's annualised method can lower early quarters for late income. The department can waive the penalty for a casualty or disaster. It can also waive it for retiring after 62 or disability, with reasonable cause.").
		VersusFederal("Massachusetts uses 80% of this year's tax, not 90%. It uses 100% of last year's at every income, with no 110% tier. The threshold is $400, not $1,000. The annualised percentages are 20%, 40%, 60% and 80%. There's no relief for filing and paying by January 31. The rate is the federal short-term rate plus four points, set each quarter. The retirement waiver needs age 62.").
		Source("M.G.L. c. 62B §§ 13, 14; c. 62C § 32(a); c. 4 § 7, clause Eighteenth").
		Source("Form 1-ES, Massachusetts Estimated Income Tax, 2026").
		Source("Form M-2210, Underpayment of Massachusetts Estimated Income Tax, and its instructions, 2025, and the draft 2026 Form M-2210").
		Source("AP 241, Estimated Income Tax Payments (01/29/2025)").
		Source("TIR 26-5, Interest Rate on Overpayments and Underpayments (09/01/2026)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
