package wisconsintaxcalc

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Wisconsin's estimated tax rules, Wis. Stat. §§ 71.09 and 71.84, as the
// shared [estimated.EstimatedTax] interface.
//
// Wisconsin calls its charge underpayment interest. It is 12% a year, set by
// § 71.84(1), on each installment's shortfall. The required annual payment
// is the smaller of 90% of this year's tax and 100% of last year's, with no
// 110% tier, in four equal installments. The threshold is $500.
//
// Schedule U applies payments in its own order. A payment counts first
// toward the installment of the period it was made in. Only what is left
// over goes back to an earlier shortfall, the most recent one first, and then
// forward. A payment after January 15 goes to the January shortfall first.
// That is why this package keeps its own ledger rather than using
// [estimated.Apply], which pays the oldest shortfall first.
//
// The figure follows the department's forms: the 2026 Form 1-ES instructions
// and the 2025 Schedule U and its instructions, which are the latest
// published. The result's Divergences say where Chapter 71 reads otherwise.
// The statute text published on October 1, 2026 still has the $500
// threshold, the 90% and 100% tests, the four dates and the 12% rate both
// forms use, so neither form predates a change in the law for 2026.

// jurisdiction is Wisconsin's postal code.
const jurisdiction = estimated.Wisconsin

// EstimatedFacts are the facts Wisconsin's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorYearFarmer   answer
	disasterExtension answer
	activeDutyAbroad  answer
}

// answer is a yes or no, and whether the caller gave it.
type answer struct{ v, set bool }

func (a answer) get() (bool, bool) { return a.v, a.set }

// PriorYearFarmerOrFisher reports whether at least two-thirds of last year's
// gross income was from farming or fishing, and whether the caller said.
// § 71.09(1)(a) makes either year's income enough to be a farmer or fisher.
// [estimated.Facts.FarmerOrFisher] covers this year.
func (e EstimatedFacts) PriorYearFarmerOrFisher() (bool, bool) { return e.priorYearFarmer.get() }

// FederalDisasterExtension reports whether the filer qualifies for a federal
// extension of time to file or pay under 26 U.S.C. § 7508A, for a federally
// declared disaster or terroristic or military action, and whether the
// caller said. Schedule U exception code 10.
func (e EstimatedFacts) FederalDisasterExtension() (bool, bool) { return e.disasterExtension.get() }

// ActiveDutyOutsideUS reports whether the filer was on active duty with the
// U.S. armed forces while stationed outside the continental United States,
// and whether the caller said. Schedule U exception code 07.
func (e EstimatedFacts) ActiveDutyOutsideUS() (bool, bool) { return e.activeDutyAbroad.get() }

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorYearFarmerOrFisher(v bool) *EstimatedFactsBuilder {
	b.e.priorYearFarmer = answer{v, true}
	return b
}

func (b *EstimatedFactsBuilder) FederalDisasterExtension(v bool) *EstimatedFactsBuilder {
	b.e.disasterExtension = answer{v, true}
	return b
}

func (b *EstimatedFactsBuilder) ActiveDutyOutsideUS(v bool) *EstimatedFactsBuilder {
	b.e.activeDutyAbroad = answer{v, true}
	return b
}

// Build has nothing to refuse: every fact is a yes or no.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is what tax less withholding must reach before any interest
	// applies.
	threshold money.USCents

	thisYearShare, priorYearShare money.Rate

	// shares are the installments' shares of the required annual payment, and
	// the shares of withholding deemed paid on each date.
	shares []money.Rate

	// annualised are Schedule U Part IV line 42.
	annualised []money.Rate
	periodEnds []estimated.Date

	dues     []estimated.Date
	calendar estimated.Calendar

	// fileAndPayBy is § 71.09(14)'s January 31, before it is moved.
	fileAndPayBy estimated.Date

	// stop is § 71.84(1)'s 15th day of the 4th month after the year ends.
	stop  estimated.Date
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Wisconsin's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return wisconsinEstimated{y}, nil
}

type wisconsinEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// installments are Schedule U line 16, or line 50 under the annualised
	// method.
	installments   []money.USCents
	excusedJanuary bool
	extras         EstimatedFacts
	notes          working.Notes
}

func (r wisconsinEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("wisconsintaxcalc: the facts carry %T, not wisconsintaxcalc.EstimatedFacts", s)
	}
}

func (r wisconsinEstimated) check(f estimated.Facts, extras EstimatedFacts) error {
	last := r.y.year - 1
	priorFarmer, _ := extras.PriorYearFarmerOrFisher()
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("wisconsintaxcalc: these are Wisconsin's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("wisconsintaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: a farmer or fisher with two-thirds of gross income from farming or fishing pays by January 15 or files by March 1 (Schedule U exception code 04), which is not built",
			estimated.ErrUnsupported)
	case priorFarmer:
		return fmt.Errorf("%w: two-thirds of %d gross income from farming or fishing makes a farmer or fisher for %d under Wis. Stat. § 71.09(1)(a) (Schedule U exception code 04), which is not built",
			estimated.ErrUnsupported, last, r.y.year)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("wisconsintaxcalc: Schedule U Part IV period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms and Chapter 71 part ways on a
// rule a Wisconsin computation uses. The first two are in the verification
// section of the research file. The third is the 1-ES's table of later first
// payment dates, which neither the statute nor Schedule U has. Each affects
// the household only when its facts reach the rule.
func (r wisconsinEstimated) divergences(f estimated.Facts, a assessment) ([]working.Divergence, error) {
	scheduled := a.requirement.TargetKnown() && !a.exempt
	waiver, _ := f.Positions().Waiver()
	abroad, _ := a.extras.ActiveDutyOutsideUS()
	first := f.RequirementFirstMet()
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Retirement or disability").
			Form("You can apply for a waiver if you retired after age 62 or became disabled. "+
				"The retirement or disability must be this year or last. "+
				"The underpayment must also be due to reasonable cause, not willful neglect.",
				"Schedule U Instructions (2025), Waiver of Underpayment Interest").
			Statute("The department can find that you retired after age 62 or became disabled. "+
				"If that was in the year or the year before, no interest is due. There's no reasonable-cause condition.",
				"Wis. Stat. § 71.09(11)(d)").
			Uses(working.ReadingForm).Affects(scheduled && waiver == estimated.WaiverRetiredOrDisabled),
		working.NewDivergence("Active duty outside the continental United States").
			Form("You can apply for a waiver of interest for active duty in the US armed forces. "+
				"You must have been stationed outside the continental United States.",
				"Schedule U Instructions (2025), Waiver of Underpayment Interest").
			Statute("The estimated tax law does not apply to such a person. No estimated tax is due and no interest arises.",
				"Wis. Stat. § 71.09(2)").
			Uses(working.ReadingForm).Affects(scheduled && abroad),
		working.NewDivergence("A requirement first met after March 31").
			Form("If you first need to pay after March 31, Form 1-ES lets you start later. "+
				"Your first payment is 1/2, 3/4 or all, on June 15, September 15 or January 15. "+
				"Schedule U still asks for a quarter by every date.",
				"2026 Form 1-ES Instructions, When to Pay Your Estimated Tax, item 1; Schedule U (2025), line 16, and its instructions").
			Statute("Each of the four payments is 25% of the required annual payment. There is no later first date. The annualized method is the only way to lower one.",
				"Wis. Stat. § 71.09(12), (13)(a) and (d)").
			Uses(working.ReadingStatute).Affects(scheduled && !first.IsZero() && first.After(r.y.periodEnds[0])),
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
func (r wisconsinEstimated) assess(f estimated.Facts) (assessment, error) {
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
func (r wisconsinEstimated) figure(f estimated.Facts) (assessment, error) {
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	if err := r.check(f, extras); err != nil {
		return assessment{}, err
	}
	y := r.y
	last := y.year - 1
	a := assessment{extras: extras}
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

	// 90% of this year's tax: Schedule U lines 3 and 4.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("Wis. Stat. § 71.09(13)(a)1.; Schedule U (2025), Part I, lines 3 and 4",
			fmt.Sprintf("%v of this year's Wisconsin tax of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// Questions asked only when no exception sets the target.
	type ask struct {
		kind             working.PositionKind
		question, effect string
	}
	var asks []ask

	// 100% of last year's tax: Schedule U line 7. No 110% tier.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Wisconsin return", last))
		asks = append(asks, ask{working.PositionPriorYearReturn, fmt.Sprintf("What was the tax on your %d Wisconsin return?", last),
			"Your target could be 100% of last year's tax, or a $0 tax could excuse interest."})
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Wisconsin return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Wisconsin return covered %d months, not all 12", last, prior.Months()))
	default:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, y.priorYearShare.Of(priorTax))
		a.notes.Rule("Wis. Stat. § 71.09(13)(a)2., (b); Schedule U (2025), Part I, line 7",
			fmt.Sprintf("the %d return covered 12 months, so the prior-year test is %v of its %v tax, %v; Wisconsin has no 110%% tier", last, y.priorYearShare, priorTax, y.priorYearShare.Of(priorTax)))
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
		asks = append(asks, ask{working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Wisconsin tax to be?", y.year),
			fmt.Sprintf("It could make 90%% of this year's tax your target and test the %s threshold.", y.threshold.Whole())})
	}

	// Exception code 02: a 12-month year last year with no liability, as a
	// full-year resident.
	zeroLastYear := hasPrior && prior.Months() == 12 && ((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	resident, residenceAnswered := prior.FullYearResident()
	if zeroLastYear && !residenceAnswered {
		asks = append(asks, ask{working.PositionResidency, fmt.Sprintf("Were you a full-year Wisconsin resident in %d?", last),
			"If you were, having no Wisconsin tax for those 12 months excuses the interest."})
	}
	disaster, disasterAnswered := extras.FederalDisasterExtension()

	owed := tax - withheld
	switch {
	case disaster:
		a.exempt = true
		ext, err := estimated.NewException("Federal disaster extension").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You qualify for a federal disaster extension. Wisconsin charges no underpayment interest for %d. No estimated payments are needed.", y.year)).Exempt(ext)
		a.notes.Rule("Wis. Stat. § 71.09(11)(e); Schedule U Instructions (2025), Exceptions (4), code 10",
			"a federal extension under 26 U.S.C. § 7508A for a declared disaster or terroristic or military action means no interest")
	case zeroLastYear && resident:
		a.exempt = true
		none, err := estimated.NewException("No Wisconsin tax due last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no Wisconsin tax for all 12 months of %d as a full-year resident. No estimated payments are needed, and no interest can arise.", last)).Exempt(none)
		a.notes.Rule("Wis. Stat. § 71.09(11)(b); Schedule U Instructions (2025), Exceptions (1), code 02",
			fmt.Sprintf("the %d return showed no liability for 12 months as a full-year resident, so there is no interest for %d", last, y.year))
	case taxKnown && owed < y.threshold:
		a.exempt = true
		under, err := estimated.ExceptionBelow("Tax less withholding", owed, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your Wisconsin tax of %s less %s of withholding leaves %s. The rest is under %s, so no estimated payments are needed.",
			tax.Whole(), withheld.Whole(), owed.Whole(), y.threshold.Whole())
		switch {
		case owed < 0:
			why = fmt.Sprintf("Your Wisconsin tax of %s less %s withheld %s. No estimated payments are needed.", tax.Whole(), withheld.Whole(), estimated.Leaves(owed))
		case owed == 0:
			why = fmt.Sprintf("Your withholding of %s covers your Wisconsin tax of %s. No estimated payments are needed.", withheld.Whole(), tax.Whole())
		case owed.Whole() == y.threshold.Whole():
			why = fmt.Sprintf("Your Wisconsin tax less withholding is just under %s. No estimated payments are needed.", y.threshold.Whole())
		}
		rb.Required(false, why).Exempt(under)
		a.notes.Rule("Wis. Stat. § 71.09(11)(a); Schedule U (2025), Part I, line 6",
			fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no interest", tax, withheld, tax-withheld, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your %d return gives no target. Unless an exception applies, your target is 90%% of your %d Wisconsin tax.", last, y.year)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		why := fmt.Sprintf("Your target is %v of your %d Wisconsin tax, %s. It keeps you clear of interest whatever your %d tax turns out to be. ",
			priorOpt.Percent(), last, target.Whole(), y.year)
		if withheld > 0 {
			why += fmt.Sprintf("After %s of withholding, you need %s of estimated payments. Pay a quarter by each date.", withheld.Whole(), (target - withheld).Whole())
		} else {
			why += "Pay it in estimated payments, a quarter by each date."
		}
		rb.Required(true, why).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Wisconsin withholding of %s reaches the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
		a.notes.Rule("Schedule U Instructions (2025), Line 8", fmt.Sprintf("withholding of %v is at least the required annual payment of %v, so there is no underpayment interest", withheld, target))
	case withheld == 0:
		rb.Required(true, fmt.Sprintf("Your Wisconsin tax of %s is %s or more, and you have no withholding. You need estimated payments of all %s required, a quarter by each date.",
			tax.Whole(), y.threshold.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Wisconsin tax of %s less %s of withholding leaves %s. The rest is %s or more, and withholding is short of the %s required. "+
			"You need estimated payments of %s, a quarter by each date.",
			tax.Whole(), withheld.Whole(), owed.Whole(), y.threshold.Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}

	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
		return a, nil
	}
	for _, q := range asks {
		a.notes.Ask(q.kind, q.question, q.effect)
	}
	if _, answered := extras.PriorYearFarmerOrFisher(); !answered {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Was at least two-thirds of your %d gross income from farming or fishing?", last),
			fmt.Sprintf("If so, Wisconsin's farmer and fisher rules apply for %d.", y.year))
	}
	if !disasterAnswered {
		a.notes.Ask(working.PositionJurisdictionFact, "Do you qualify for a federal disaster extension of time to file or pay?",
			"If you do, Wisconsin charges no underpayment interest.")
	}

	// Schedule U line 16: a quarter of line 8 by each date.
	cum, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cum)
	a.notes.Rule("Wis. Stat. § 71.09(12), (13)(a); Schedule U (2025), line 16", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s",
		a.requirement.Target(), a.installments[0], a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if first := f.RequirementFirstMet(); !first.IsZero() && first.After(y.periodEnds[0]) {
		a.notes.Rule("Schedule U (2025), line 16, and its instructions; Wis. Stat. § 71.09(13)",
			fmt.Sprintf("the requirement was first met on %s, but Schedule U still requires a quarter by every date from %s; only the annualised method in Part IV lowers the earlier installments",
				first.Long(), a.dues[0].Long()))
	}

	if f.Elections().Annualised() {
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
		a.notes.Rule("Wis. Stat. § 71.09(13)(d); Schedule U (2025), Part IV, lines 42 to 50",
			fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: %v, %v, %v and %v of the annualised tax, less earlier installments, "+
				"and never more than the regular quarter plus any amount the method lowered before", ann[0], ann[1], ann[2], ann[3],
				y.annualised[0], y.annualised[1], y.annualised[2], y.annualised[3]))
	}

	// § 71.09(14): a return filed and paid in full by January 31 takes the
	// place of the January payment, when nothing was required by the earlier
	// dates. January 31, 2027 is a Sunday and moves to February 1 under
	// § 990.001(4)(b), as the 2025 Schedule U moved Saturday January 31,
	// 2026 to February 2.
	if filed := f.ReturnFiled(); !filed.IsZero() {
		by := y.calendar.NextBusinessDay(y.fileAndPayBy)
		earlier := a.installments[0] + a.installments[1] + a.installments[2]
		switch {
		case filed.After(by):
		case earlier == 0:
			a.excusedJanuary = true
			a.notes.Rule("Wis. Stat. §§ 71.09(14), 990.001(4)(b); Schedule U Instructions (2025), Line 17",
				fmt.Sprintf("the return was filed and paid on %s, by %s, and nothing was required by %s or earlier, so the payment with it counts as the January installment",
					filed.Long(), by.Long(), a.dues[2].Long()))
		default:
			a.notes.Rule("Wis. Stat. § 71.09(14); Schedule U Instructions (2025), Line 17",
				fmt.Sprintf("the return was filed and paid on %s, by %s, but %v was required by %s, so the payment with it does not replace the January installment",
					filed.Long(), by.Long(), earlier, a.dues[2].Long()))
		}
	}
	return a, nil
}

func (r wisconsinEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// piece is part of an installment's shortfall and the day it was paid, or
// the zero Date if it never was.
type piece struct {
	amount money.USCents
	paidOn estimated.Date
}

// column is one installment after Schedule U lines 16 to 24.
type column struct {
	due        estimated.Date
	required   money.USCents
	creditedBy money.USCents

	// underpaid is line 20 and forward is line 23.
	underpaid, forward money.USCents

	// paid are line 22: what was carried back or paid late, and when.
	paid []piece
}

// shortfall is what was unpaid on the due date: line 20 less line 23.
func (c column) shortfall() money.USCents { return c.underpaid - c.forward }

func (c column) outstanding() money.USCents {
	o := c.shortfall()
	for _, p := range c.paid {
		o -= p.amount
	}
	return o
}

// pieces are the shortfall broken up by when it was paid, with the unpaid
// rest last and undated.
func (c column) pieces() []piece {
	out := append([]piece(nil), c.paid...)
	if o := c.outstanding(); o > 0 {
		out = append(out, piece{amount: o})
	}
	return out
}

// scheduleU applies credits to the installments as Schedule U lines 16 to 24
// and their instructions do.
//
// A credit belongs to the period it falls in: after the previous due date,
// up to and including this one. Each period's credits, in date order, first
// cover that period's own installment. An overpayment is what is left, dated
// by the credit that created it. Taking the periods in order, each
// overpayment is carried back to the earlier shortfalls, the most recent
// first (line 22), and what remains is carried forward to the next periods
// with an underpayment (line 23). A credit after the last due date is a late
// payment and goes to the last shortfall first, then back.
//
// This is not [estimated.Apply], which pays the oldest shortfall first and
// lets a credit pay an earlier shortfall before its own period's.
func scheduleU(dues []estimated.Date, required []money.USCents, credits []estimated.Payment) []column {
	cs := append([]estimated.Payment(nil), credits...)
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].On().Before(cs[j].On()) })
	n := len(dues)
	cols := make([]column, n)
	covered := make([]money.USCents, n)
	over := make([][]piece, n)
	var late []piece
	k := 0
	for _, c := range cs {
		for k < n && c.On().After(dues[k]) {
			k++
		}
		if k == n {
			late = append(late, piece{c.Amount(), c.On()})
			continue
		}
		take := min(c.Amount(), required[k]-covered[k])
		covered[k] += take
		if rest := c.Amount() - take; rest > 0 {
			over[k] = append(over[k], piece{rest, c.On()})
		}
	}
	for i := range cols {
		cols[i] = column{due: dues[i], required: required[i], underpaid: required[i] - covered[i]}
		for _, c := range cs {
			if !c.On().After(dues[i]) {
				cols[i].creditedBy += c.Amount()
			}
		}
	}
	back := func(from int, p piece) money.USCents {
		left := p.amount
		for j := from; j >= 0 && left > 0; j-- {
			if o := cols[j].outstanding(); o > 0 {
				pay := min(o, left)
				cols[j].paid = append(cols[j].paid, piece{pay, p.paidOn})
				left -= pay
			}
		}
		return left
	}
	for i := range cols {
		for _, p := range over[i] {
			left := back(i-1, p)
			for j := i + 1; j < n && left > 0; j++ {
				fw := min(cols[j].shortfall(), left)
				cols[j].forward += fw
				left -= fw
			}
		}
	}
	for _, p := range late {
		back(n-1, p)
	}
	return cols
}

func (r wisconsinEstimated) ledger(f estimated.Facts, a assessment) ([]column, working.Notes, error) {
	var notes working.Notes
	credits, err := estimated.Credits(f, a.dues, r.y.shares)
	switch {
	case f.Elections().WithholdingAsWithheld() && f.WithheldTotal() >= a.requirement.Target():
		// Schedule U stops at line 8 when withholding reaches the required
		// annual payment, so the election is never reached.
		notes.Rule("Schedule U Instructions (2025), Lines 8 and 18",
			fmt.Sprintf("the %v withheld reaches the required annual payment, so it is treated as paid one-fourth on each due date and the election to credit it when withheld does not arise", f.WithheldTotal()))
	case f.Elections().WithholdingAsWithheld():
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
		notes.Rule("Schedule U Instructions (2025), Line 18, code 08",
			fmt.Sprintf("by election, the %v withheld is credited to the period it was actually withheld in", f.WithheldTotal()))
	default:
		notes.Rule("Schedule U Instructions (2025), Line 18",
			fmt.Sprintf("the %v withheld is treated as paid one-fourth on each due date, whenever it was actually withheld", f.WithheldTotal()))
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	required := append([]money.USCents(nil), a.installments...)
	if a.excusedJanuary {
		required[3] = 0
	}
	notes.Rule("Schedule U (2025), lines 17 to 24, and its instructions for lines 17, 22 and 23",
		"a payment counts first toward the installment of the period it was made in; an overpayment is carried back to the most recent earlier shortfall first, then forward; "+
			"a payment after the January due date goes to the January shortfall first, then back")
	return scheduleU(a.dues, required, credits), notes, nil
}

// installments are the columns as due dates. A January installment the
// return replaces is excused.
func (r wisconsinEstimated) installments(a assessment, cols []column) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(cols))
	var needed money.USCents
	for i, c := range cols {
		needed += a.installments[i]
		b := estimated.NewInstallment(i+1, c.due).NeededBy(needed).CreditedBy(c.creditedBy).Shortfall(c.shortfall())
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

func (r wisconsinEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Wisconsin return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	cols, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := r.installments(a, cols)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

func (r wisconsinEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("wisconsintaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Wisconsin return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	cols, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule, err := r.installments(a, cols)
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
	for i, c := range cols {
		if (a.excusedJanuary && i == 3) || c.shortfall() <= 0 {
			continue
		}
		lb := estimated.NewPenaltyLine(i+1, c.due, c.shortfall())
		lastDay := c.due
		for _, p := range c.pieces() {
			to := stop
			if !p.paidOn.IsZero() && p.paidOn.Before(stop) {
				to = p.paidOn
			}
			if !to.After(c.due) {
				continue
			}
			if to.After(lastDay) {
				lastDay = to
			}
			parts, unrated, err := estimated.SimpleDaily(p.amount, c.due, to, y.rates)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			if !unrated.IsZero() {
				// The rate is statutory and the series runs to the stop.
				return working.Result[estimated.Penalty]{}, fmt.Errorf("wisconsintaxcalc: no rate for %v", unrated)
			}
			for _, part := range parts {
				lb.Part(part)
			}
		}
		line, err := lb.Days(lastDay.DaysSince(c.due)).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
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
		notes.Rule("Wis. Stat. § 71.09(13)(a); Schedule U (2025), Part I, line 8", fmt.Sprintf("this year's Wisconsin tax was not given, so the interest is figured against %v of the %d tax alone; "+
			"the required payment is the smaller of that and 90%% of this year's tax, so %v is the most the interest can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("Wis. Stat. § 71.84(1); Schedule U (2025), line 26", fmt.Sprintf("the balance due on the return was paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("Wis. Stat. § 71.84(1); Schedule U (2025), lines 25 to 29", fmt.Sprintf("each shortfall is charged %v a year, days ÷ 365, from its due date until paid or %s; the interest to %s is %v",
		y.rates.Periods()[0].Rate(), y.stop.Long(), stop.Long(), p.Total()))
	if k, asked := f.Positions().Waiver(); asked && k != estimated.WaiverNone {
		code := "05"
		if k == estimated.WaiverRetiredOrDisabled {
			code = "06"
		}
		notes.Rule("Wis. Stat. § 71.09(11)(c), (d); Schedule U Instructions (2025), Waiver of Underpayment Interest",
			fmt.Sprintf("a waiver for %v is applied for with code %s and decided by the department; the interest shown is before it", k, code))
	}
	// § 71.09(11)(c) and (d) grant the federal pair, at 62.
	if o, ok := estimated.StandardWaiver(f, "the Department of Revenue", 62); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if abroad, _ := a.extras.ActiveDutyOutsideUS(); abroad {
		notes.Rule("Schedule U Instructions (2025), Waiver of Underpayment Interest, code 07; Wis. Stat. § 71.09(2)",
			"active duty outside the continental United States is applied for as a waiver with code 07; the interest shown is before it, and § 71.09(2) says none is owed")
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Will you ask to waive interest, as for active duty outside the continental US?",
			"The department can waive it for a casualty, disaster or other unusual event too. "+
				"Retiring after 62 or disability can qualify too, with reasonable cause.")
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r wisconsinEstimated) Explanation() estimated.Explanation { return wisconsinExplanation }

// wisconsinExplanation is written by hand and checked against
// research/estimated-payments/wi.md.
var wisconsinExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Wisconsin expects estimated payments if $500 or more would be due for 2026. " +
			"The $500 is the tax left after withholding and credits. " +
			"It charges underpayment interest of 12% a year on each quarter's shortfall.").
		Paragraph("You avoid interest once withholding and estimated payments reach a target. " +
			"It is the smaller of 90% of this year's Wisconsin tax or 100% of last year's. " +
			"Last year's figure needs a Wisconsin return that covered 12 months. There is no 110% figure for higher incomes.").
		Paragraph("There are four equal payments, due April 15, June 15 and September 15. " +
			"The last is due January 15 of the next year. " +
			"A date on a weekend or Wisconsin legal holiday becomes the next business day. " +
			"Withholding counts as paid a quarter on each date. " +
			"You can instead show when it was actually withheld. " +
			"With uneven income, Schedule U's annualized method can lower the early quarters.").
		Paragraph("Interest runs daily on each quarter's shortfall until it is paid. " +
			"It stops on April 15 of the next year. " +
			"A payment first covers the quarter it was made in. Only the extra goes back to an earlier shortfall, the most recent one first. " +
			"A payment after January 15 goes to the January shortfall first. " +
			"Schedule U expects a quarter from April 15, even if you first need to pay later. " +
			"Only the annualized method lowers those earlier quarters.").
		Paragraph("There is no interest if this year's tax less withholding is under $500. " +
			"There is also none if you had no Wisconsin tax for all 12 months of last year. " +
			"You must have been a full-year Wisconsin resident that year. " +
			"A federal disaster extension also means no interest. " +
			"Filing and paying in full by January 31 replaces the January payment. " +
			"It does so only if nothing was due by the earlier dates. " +
			"For 2026 the date is February 1, 2027, because January 31 is a Sunday. " +
			"You can apply for a waiver for a casualty or disaster. " +
			"You can also apply for retiring after 62 or becoming disabled.").
		VersusFederal("Wisconsin differs from federal in six ways that change a number. " +
			"Its threshold is $500, not $1,000, and it has no 110% figure at higher incomes. " +
			"Its 12% rate is fixed by law. " +
			"A payment first covers its own quarter, not the oldest shortfall as in federal. " +
			"The no-tax-last-year exception needs full-year residence. " +
			"Filing by January 31 replaces the last payment only if nothing was due earlier.").
		Source("Wis. Stat. §§ 71.09, 71.84, 990.001(4), 995.20").
		Source("2026 Form 1-ES Instructions (D-101A, R. 1-26)").
		Source("2025 Schedule U (D-104, R. 05-25) and its instructions (D-114, R. 11-25)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
