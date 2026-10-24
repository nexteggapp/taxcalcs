package californiataxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// California's estimated tax rules, Rev. & Tax. Code § 19136 and the sections
// after it, as the shared [estimated.EstimatedTax] interface.
//
// § 19136(a) adopts 26 U.S.C. § 6654 except as California provides otherwise,
// and California provides otherwise often enough that nothing here borrows
// from the federal package: the installments are 30%, 40%, 0% and 30%; the
// threshold is $500; there is no prior-year test at $1,000,000 of AGI; the
// annualised percentages differ; and the rate is set half-yearly.
//
// Where the Franchise Tax Board's forms and the statute disagree, the figure
// follows the form and the result's Divergences say what the statute says.

// EstimatedFacts are the facts California's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	groupReturn bool
	worksheetI  []WorksheetIColumn
}

// WorksheetI is a Form 540NR filer's four FTB 5805 Worksheet I columns, for
// the annualised method, or none.
func (e EstimatedFacts) WorksheetI() []WorksheetIColumn {
	return append([]WorksheetIColumn(nil), e.worksheetI...)
}

// GroupReturn reports a nonresident electing to be included in a group return
// under § 18537, whom § 19136(j) takes out of the penalty from 2026.
func (e EstimatedFacts) GroupReturn() bool { return e.groupReturn }

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

// GroupReturn marks a nonresident who elects to file in a group return.
func (b *EstimatedFactsBuilder) GroupReturn() *EstimatedFactsBuilder {
	b.e.groupReturn = true
	return b
}

// WorksheetI sets a Form 540NR filer's Worksheet I columns, one for each
// annualisation period.
func (b *EstimatedFactsBuilder) WorksheetI(columns ...WorksheetIColumn) *EstimatedFactsBuilder {
	b.e.worksheetI = append([]WorksheetIColumn(nil), columns...)
	return b
}

// Build refuses Worksheet I with other than four columns.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	e := b.e
	e.worksheetI = append([]WorksheetIColumn(nil), e.worksheetI...)
	if n := len(e.worksheetI); n != 0 && n != 4 {
		return EstimatedFacts{}, fmt.Errorf("californiataxcalc: Worksheet I has four columns, not %d", n)
	}
	return e, nil
}

// movedDuringYear reports a part-year resident: some California residence,
// but not for the whole year.
func movedDuringYear(f estimated.Facts) bool {
	for _, p := range f.Residence() {
		if p.State() == string(estimated.California) {
			return files540NR(f)
		}
	}
	return false
}

// regularPartYear is a part-year resident on the regular method, whose
// target and installments FTB 5805 doesn't change for the move.
func regularPartYear(f estimated.Facts) bool {
	return movedDuringYear(f) && !f.Elections().Annualised()
}

// priorNote is the sentence a part-year resident's reason ends with when
// last year's tax sets the target: FTB 5805 Part II line 5 takes it as filed.
func priorNote(f estimated.Facts, chosen estimated.Basis) string {
	if regularPartYear(f) && chosen == estimated.BasisPriorYearTax {
		return " California doesn't reduce last year's tax for the months you lived elsewhere."
	}
	return ""
}

// partYearContext is what a part-year resident's penalty reason adds: FTB
// 5805 keeps the January installment after a move.
const partYearContext = "The January payment still counts after a move."

// files540NR reports a nonresident or part-year resident: residence was
// given, and none of it is California for the whole year.
func files540NR(f estimated.Facts) bool {
	return len(f.Residence()) > 0 && !estimated.ResidentAllYear(f.Residence(), estimated.California, f.TaxYear())
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is what tax less withholding must reach before any penalty
	// applies: $500, or $250 for a separate return.
	threshold, thresholdSeparate money.USCents

	// priorYearStep is the prior-year California AGI above which the
	// prior-year test is 110%, and highIncome the current-year California AGI
	// at or above which there is no prior-year test at all.
	priorYearStep, priorYearStepSeparate money.USCents
	highIncome, highIncomeSeparate       money.USCents

	dues         []estimated.Date
	calendar     estimated.Calendar
	fileAndPayBy estimated.Date
	stop         estimated.Date
	periodEnds   []estimated.Date
	rates        estimated.RateSeries

	// groupReturnExempt is § 19136(j), operative from 2026.
	groupReturnExempt bool
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// installmentShares are § 19136.1(a): 30%, 40%, nothing, and 30% of the
// required annual payment. § 19136(e)(2)(A) deems withholding paid in the
// same shares.
var installmentShares = []money.Rate{money.Percent(30), money.Percent(40), 0, money.Percent(30)}

// annualisedPercentages are § 19136.1(b), FTB 5805 Part III line 15.
var annualisedPercentages = []money.Rate{money.Percent(27), money.Percent(63), money.Percent(63), money.Percent(90)}

// EstimatedTax returns California's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, estimated.California, year)
	}
	return californiaEstimated{y}, nil
}

type californiaEstimated struct{ y estimatedYear }

type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	installments []money.USCents
	excused      bool
	notes        working.Notes
}

func (r californiaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("californiataxcalc: the facts carry %T, not californiataxcalc.EstimatedFacts", s)
	}
}

func (r californiaEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != estimated.California:
		return fmt.Errorf("californiataxcalc: these are California's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("californiataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: farmers and fishermen make one payment by January 15 under FTB 5805F",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("californiataxcalc: FTB 5805 Part III period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are the places the FTB's forms and the Revenue and Taxation
// Code part ways on a rule every California computation uses. They are found
// in research/estimated-payments/ca.md and checked there against both.
func (r californiaEstimated) divergences(f estimated.Facts, extras EstimatedFacts) ([]working.Divergence, error) {
	// The FTB 5805 Question 3 wording on withholding "not withheld in equal
	// installments" is not a divergence: Worksheet II and § 19136(e)(2)(A)
	// both credit it 30%, 40%, 0% and 30%, and research/estimated-payments/
	// ca.md finds only the question's wording loose, not the rule.
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Nonresidents in a group return").
			Form("This exception isn't in the 2026 Form 540-ES instructions or the 2025 FTB 5805. It reaches only a nonresident in a group return, not a single resident.",
				"2026 Instructions for Form 540-ES; FTB 5805 (2025) and its instructions").
			Statute("From 2026, a nonresident who elects to join a group return has no penalty.",
				"Cal. Rev. & Tax. Code § 19136(j), added by Stats. 2025, ch. 73 (AB 1518); FTB bill analysis of AB 1518").
			Uses(working.ReadingStatute).Affects(extras.groupReturn && r.y.groupReturnExempt),
		working.NewDivergence("Underpayments caused by the FTB").
			Form("The waivers listed cover a casualty or disaster, and retirement or disability. An underpayment the FTB caused isn't mentioned.",
				"FTB 5805 (2025) instructions, General Information C").
			Statute("An FTB levy, processing action or collection action can be made in error. There's no penalty to the extent it created or increased the underpayment.",
				"Cal. Rev. & Tax. Code § 19136.7(a)").
			// Nothing in the facts says the FTB caused an underpayment.
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

func separate(s filing.Status) bool { return s == filing.MarriedFilingSeparately }

func (r californiaEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	var targetQs targetQuestions
	divergences, err := r.divergences(f, extras)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)

	threshold, step, high := y.threshold, y.priorYearStep, y.highIncome
	if separate(f.FilingStatus()) {
		threshold, step, high = y.thresholdSeparate, y.priorYearStepSeparate, y.highIncomeSeparate
	}
	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	last := y.year - 1

	// 90% of this year's tax. § 6654(d)(1)(B)(i), adopted by § 19136(a).
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(90), money.Percent(90).Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(90), 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("Cal. Rev. & Tax. Code § 19136(a); 26 U.S.C. § 6654(d)(1)(B)(i)", fmt.Sprintf("90%% of this year's California tax of %v is %v", tax, thisYear.Amount()))
	}

	// Last year's tax: 100%, 110% above the step, and none at all at high
	// income this year.
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
	}
	thisAGI, thisAGIKnown := f.ThisYear().StateAGI()
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, money.Percent(100), 0).
			Unavailable(fmt.Sprintf("Needs your %d California return", last))
		targetQs.Ask(working.PositionPriorYearReturn, fmt.Sprintf("What was the total tax on your %d California return?", last),
			"With it, last year's California tax could set your target, not this year's.")
	case !prior.Filed():
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, money.Percent(100), 0).
			Unavailable(fmt.Sprintf("You didn't file a %d California return", last))
	case prior.Months() != 12:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, money.Percent(100), 0).
			Unavailable(fmt.Sprintf("Your %d California return covered %d months, and this test needs all 12", last, prior.Months()))
	case thisAGIKnown && thisAGI >= high:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, money.Percent(100), 0).
			Unavailable(fmt.Sprintf("Not available at %s or more of California AGI", high.Whole()))
		a.notes.Rule("Cal. Rev. & Tax. Code § 19136.3(a)", fmt.Sprintf("California AGI of %v is at least %v, so the prior-year test is not available", thisAGI, high))
	case !thisAGIKnown:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, money.Percent(100), 0).
			Unavailable(fmt.Sprintf("Needs your %d California AGI, as the test is closed at %s or more", y.year, high.Whole()))
		targetQs.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What do you expect your %d California AGI to be?", y.year),
			fmt.Sprintf("If it's under %s, last year's tax could set your target.", high.Whole()))
	default:
		pf, _ := prior.Figures()
		priorAGI, known := pf.StateAGI()
		rate := money.Percent(100)
		switch {
		case !known:
			rate = money.Percent(110)
			targetQs.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d California AGI?", last),
				fmt.Sprintf("Until then, the target is 110%% of last year's tax. At %s or less it would be 100%%.", step.Whole()))
		case priorAGI > step:
			rate = money.Percent(110)
		}
		amount := rate.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, rate, amount)
		why := fmt.Sprintf("%d California AGI of %v is not more than %v", last, priorAGI, step)
		if !known {
			why = fmt.Sprintf("%d California AGI was not given", last)
		} else if rate == money.Percent(110) {
			why = fmt.Sprintf("%d California AGI of %v is more than %v", last, priorAGI, step)
		}
		a.notes.Rule("Cal. Rev. & Tax. Code § 19136(a); 26 U.S.C. § 6654(d)(1)(B)(ii), (C)", fmt.Sprintf("%s, so the prior-year test is %v of the %v tax on the %d return, or %v", why, rate, priorTax, last, amount))
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
		targetQs.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d California tax to be?", y.year),
			estimated.WithoutThisYearTax(priorOpt.Available()))
	}

	// The exceptions that mean no penalty can arise.
	noLiability := hasPrior && prior.Months() == 12 &&
		((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	priorSmall, priorSmallKnown := false, false
	var priorWithheld money.USCents
	if hasPrior && prior.Filed() {
		pf, _ := prior.Figures()
		if w, ok := pf.Withheld(); ok {
			priorWithheld = w
			priorSmall, priorSmallKnown = priorTax-w < threshold, true
		}
		if !priorSmallKnown && !noLiability {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("How much California tax was withheld for you in %d?", last),
				fmt.Sprintf("Under %s of last year's tax after withholding means no estimated payments.", threshold.Whole()))
		}
	}

	switch {
	case extras.groupReturn && y.groupReturnExempt:
		a.exempt = true
		group, err := estimated.NewException("Nonresident in a group return").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You're a nonresident included in a group return. From %d, California charges no penalty for that. No estimated payments are needed.", y.year)).Exempt(group)
		a.notes.Rule("Cal. Rev. & Tax. Code § 19136(j)", fmt.Sprintf("a nonresident electing a group return under § 18537 is not subject to the penalty for %d", y.year))
	case noLiability:
		a.exempt = true
		none, err := estimated.NewException("No California tax liability last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no California tax liability for all of %d. No estimated payments are needed for %d.", last, y.year)).Exempt(none)
		a.notes.Rule("Cal. Rev. & Tax. Code § 19136(a); 26 U.S.C. § 6654(e)(2); FTB 5805 (2025)", fmt.Sprintf("the %d return showed no liability for a full 12 months, so there is no penalty for %d", last, y.year))
	case taxKnown && tax-withheld < threshold:
		a.exempt = true
		under, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your California tax is %s, and %s was withheld. That %s. No estimated payments are needed.",
			tax.Whole(), withheld.Whole(), leftUnder(false, tax-withheld, threshold))
		switch {
		case withheld == 0:
			why = fmt.Sprintf("You have no California withholding, and your California tax is %s. That's under %s, so no estimated payments are needed.", tax.Whole(), threshold.Whole())
		case withheld >= tax:
			why = fmt.Sprintf("Your California withholding of %s covers your %s California tax. No estimated payments are needed.", withheld.Whole(), tax.Whole())
		}
		rb.Required(false, why).Exempt(under)
		a.notes.Rule("Cal. Rev. & Tax. Code § 19136(c)(2); FTB 5805 (2025), Part II line 4", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no penalty", tax, withheld, tax-withheld, threshold))
	case priorSmall:
		a.exempt = true
		under, err := estimated.ExceptionBelow("Last year's tax less withholding", priorTax-priorWithheld, threshold)
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your %d California tax was %s, and %s was withheld. That %s. California charges no penalty when either year's tax is under %s. No estimated payments are needed.",
			last, priorTax.Whole(), priorWithheld.Whole(), leftUnder(true, priorTax-priorWithheld, threshold), threshold.Whole())).Exempt(under)
		a.notes.Rule("Cal. Rev. & Tax. Code § 19136(c)(2); FTB 5805 (2025)", fmt.Sprintf("the %d tax less credits and withholding was under %v, so there is no penalty for %d", last, threshold, y.year))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target is 90%% of your %d California tax. Where that test is open, it may instead be 100%% of your %d tax. Without one of those figures, there's no target to pay toward yet.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your California withholding of %s reaches %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax is.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)+priorNote(f, chosen)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d California tax, %s. After %s of withholding, that's %s of estimated payments. That keeps you clear of a penalty whatever your %d tax turns out to be.",
			priorOpt.Percent(), last, target.Whole(), withheld.Whole(), (target-withheld).Whole(), y.year)+priorNote(f, chosen)).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your California withholding of %s already reaches your %s target. No estimated payments are needed.", withheld.Whole(), target.Whole())+priorNote(f, chosen)).Choose(chosen)
	case withheld == 0:
		rb.Required(true, fmt.Sprintf("You have no California withholding, and your California tax is %s. That's %s or more, and your target is %s. You need estimated payments of %s across the year.",
			tax.Whole(), threshold.Whole(), target.Whole(), target.Whole())+priorNote(f, chosen)).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your California tax of %s less %s withheld leaves %s. That's %s or more, and your target is %s. You need %s of estimated payments across the year.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), threshold.Whole(), target.Whole(), (target-withheld).Whole())+priorNote(f, chosen)).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	targetQs.askUnlessExempt(&a.notes, a.exempt)

	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), installmentShares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	dues := r.dues()
	a.notes.Rule("Cal. Rev. & Tax. Code § 19136.1(a); 2026 Instructions for Form 540-ES, Section D", fmt.Sprintf("30%% of %v, %v, is due %s; 40%%, %v, is due %s; nothing is due %s; and 30%%, %v, is due %s",
		a.requirement.Target(), a.installments[0], dues[0].Long(), a.installments[1], dues[1].Long(), dues[2].Long(), a.installments[3], dues[3].Long()))

	if len(extras.WorksheetI()) > 0 && !files540NR(f) {
		return assessment{}, errors.New("californiataxcalc: Worksheet I is for a Form 540NR filer, and the residence shows a full-year California resident or none")
	}
	if files540NR(f) {
		a.notes.Rule("FTB 5805 (2025) instructions, Part II, lines 1 and 5", "a Form 540NR filer's required annual payment is figured from the tax on Form 540NR, with no further proration; Worksheet I prorates only the annualised method")
	}
	if f.Elections().Annualised() && !a.exempt {
		periods := f.Periods()
		taxes := make([]money.USCents, len(periods))
		for i, p := range periods {
			taxes[i] = p.AnnualisedTax()
		}
		if files540NR(f) {
			if cols := extras.WorksheetI(); len(cols) == 4 {
				for i, c := range cols {
					w, err := WorksheetI(i, c)
					if err != nil {
						return assessment{}, err
					}
					taxes[i] = w.Line14c()
					a.notes.Rule("FTB 5805 (2025) instructions, Worksheet I and Form 540NR Instructions for Part III", fmt.Sprintf("period %d: California AGI annualised to %v, %v of it taxable after the prorated deduction, %v of prorated tax after exemption credits (line P), and %v after credits and other tax (Part III line 14c)",
						i+1, w.F(), w.K(), w.P(), w.Line14c()))
				}
				a.notes.Ask(working.PositionUnsettledRule, "",
					"Worksheet I doesn't say how its rates on lines C, H and N round. The figures keep them exact and round each amount to the cent.")
			} else {
				a.notes.Ask(working.PositionJurisdictionFact, "What are your FTB 5805 Worksheet I figures for each period?",
					"Until then, each period's annualised tax isn't prorated to California.")
			}
		}
		ann, err := estimated.Annualised(taxes, annualisedPercentages, a.installments)
		if err != nil {
			return assessment{}, err
		}
		a.installments = ann
		a.notes.Rule("Cal. Rev. & Tax. Code § 19136.1(b); FTB 5805 (2025), Part III", fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: 27%%, 63%%, 63%% and 90%% of the annualised tax, "+
			"less earlier installments, and never more than the regular 30%%, 40%%, 0%% and 30%% plus any shortfall carried forward", ann[0], ann[1], ann[2], ann[3]))
	}

	// Filing and paying by January 31st excuses the last installment. January
	// 31, 2027 is a Sunday; the 540-ES prints January 31 and says nothing of
	// moving it, so it is not moved here.
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(y.fileAndPayBy) && !a.exempt {
		a.excused = true
		a.notes.Rule("2026 Instructions for Form 540-ES, Section D; 26 U.S.C. § 6654(h)", fmt.Sprintf("the return was filed and paid on %s, by %s, so the January installment is excused", filed.Long(), y.fileAndPayBy.Long()))
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(dues))
	}
	if regularPartYear(f) && !a.exempt {
		a.notes.Ask(working.PositionJurisdictionFact, "Did your income come unevenly through the year?",
			"If so, the annualised method with FTB 5805 Worksheet I can lower your payments.")
	}
	return a, nil
}

// leftUnder words an amount under limit: "leaves $742, under $1,000",
// "leaves just under $1,000" when whole dollars would show the two as equal,
// or "leaves a $136 refund" below zero. past uses "left".
func leftUnder(past bool, amount, limit money.USCents) string {
	verb, words := "leaves", estimated.Leaves
	if past {
		verb, words = "left", estimated.Left
	}
	if amount < 0 {
		return words(amount)
	}
	if amount.Whole() == limit.Whole() {
		return verb + " just under " + limit.Whole()
	}
	return words(amount) + ", under " + limit.Whole()
}

func (r californiaEstimated) dues() []estimated.Date {
	return estimated.MoveDates(r.y.calendar, r.y.dues)
}

func (r californiaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r californiaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	dues := r.dues()
	credits, err := estimated.Credits(f, dues, installmentShares)
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, dues)
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	required := append([]money.USCents(nil), a.installments...)
	if a.excused {
		required[3] = 0
	}
	ledger, err := estimated.Apply(dues, required, credits)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("FTB 5805 (2025), Part I, Question 3",
			fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("Cal. Rev. & Tax. Code § 19136(e)(2)(A); FTB 5805 (2025) Worksheet II, line 2",
			fmt.Sprintf("the %v withheld is treated as paid 30%%, 40%%, 0%% and 30%% on the due dates, whenever it was actually withheld",
				f.WithheldTotal()))
	}
	return ledger, notes, nil
}

func (r californiaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d California return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := schedule(a, ledger)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments from the ledger, with the January
// installment excused when filing early excused it. September needs nothing
// of its own, but 70% of the year is needed by it, so it is met or short.
func schedule(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
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

func (r californiaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("californiataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d California return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	sched, err := schedule(a, ledger)
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

	p, unrated, err := estimated.SimplePenalty(ledger, stop, y.rates, waiver, a.requirement, sched)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if regularPartYear(f) {
		if p, err = p.ToBuilder().Context(partYearContext).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("Cal. Rev. & Tax. Code § 19136(a); 26 U.S.C. § 6654(d)(1)(B)", fmt.Sprintf("this year's California tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("Cal. Rev. & Tax. Code § 19136(a); 26 U.S.C. § 6654(b)(2); FTB 5805 (2025) Worksheet II", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("Cal. Rev. & Tax. Code §§ 19136(b), 19521; FTB 5805 (2025) Worksheet II, Part II", fmt.Sprintf("each shortfall is charged simple interest at the FTB's half-yearly rate, by the day, from its due date until paid or %s, "+
		"with each payment applied to the oldest shortfall first; the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", unrated.AddDays(-1).Long()))
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking the FTB to waive the penalty?",
			"The FTB can waive it for a casualty, disaster or other unusual event. Retiring after 62 or disability counts with reasonable cause; then none is due.")
	}
	if o, ok := estimated.StandardWaiver(f, "the Franchise Tax Board", 62); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r californiaEstimated) Explanation() estimated.Explanation { return californiaExplanation }

// californiaExplanation is written by hand and checked against
// research/estimated-payments/ca.md.
var californiaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"California follows the federal idea of paying tax through the year. Payments are front-loaded: 70% of the year's requirement is due by mid-June. No penalty applies unless at least $500 is due after withholding.").
		Paragraph("Withholding and estimated payments must reach a target to avoid the penalty. The target is the smaller of 90% of this year's tax or 100% of last year's. As federally, last year's figure needs a full-year return. It becomes 110% if last year's California AGI was over $150,000. That's $75,000 if married filing separately. Unlike federal, it's unavailable once this year's California AGI is $1,000,000. That's $500,000 if married filing separately. Then only 90% of this year's tax will do.").
		Paragraph("The payments are not equal quarters. The split is 30% April 15, 40% June 15, none September 15 and 30% January 15. Withholding counts as paid in the same proportions on those dates. That holds whenever it was actually taken. A date on a weekend or holiday shifts to the next business day. Filing and paying in full by January 31 replaces the January payment.").
		Paragraph("There's no penalty if this year's tax less withholding is under $500. That's $250 if married filing separately. There's also none if last year's tax was under that. No liability on a full-year California return last year also means no penalty.").
		Paragraph("The penalty is interest on each shortfall for the days it stays unpaid. The Franchise Tax Board sets the rate each half-year from the federal rate. It stops on April 15 of the next year. The annualised method on FTB 5805 can lower early payments for uneven income. It uses 27%, 63%, 63% and 90% of annualised tax. The FTB can waive the penalty for a casualty or disaster. It can also waive it for retiring after 62 or disability, with reasonable cause. There's none to the extent a law passed during the year caused the underpayment.").
		VersusFederal("California's payments are 30%, 40%, 0% and 30%, not four equal quarters. So 70% is due by June. The threshold is $500, not $1,000. Either year's balance under it excuses the penalty. Last year's test isn't available at $1,000,000 of California AGI. The annualised percentages are 27%, 63%, 63% and 90%. The rate is set half-yearly.").
		Source("Cal. Rev. & Tax. Code §§ 19136, 19136.1, 19136.3, 19136.7, 19521").
		Source("2026 Instructions for Form 540-ES").
		Source("FTB 5805 (2025) and its instructions").
		Source("FTB, Interest and estimate penalty rates (updated 06/22/2026)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()

// targetQuestion is a question whose answer can change the target. It is
// asked only when no exception sets the target.
type targetQuestion struct {
	position         working.PositionKind
	question, effect string
}

type targetQuestions []targetQuestion

func (q *targetQuestions) Ask(position working.PositionKind, question, effect string) {
	*q = append(*q, targetQuestion{position, question, effect})
}

// askUnlessExempt adds the questions to notes unless an exception set the
// target.
func (q targetQuestions) askUnlessExempt(notes *working.Notes, exempt bool) {
	if exempt {
		return
	}
	for _, t := range q {
		notes.Ask(t.position, t.question, t.effect)
	}
}
