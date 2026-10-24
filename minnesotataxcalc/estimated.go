package minnesotataxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Minnesota's estimated tax rules, Minn. Stat. §§ 289A.25 and 270C.40, as the
// shared [estimated.EstimatedTax] interface.
//
// Minnesota follows the federal shape: four 25% installments, the lesser of
// 90% of this year's tax or 100% of last year's, and 110% above $150,000 of
// last year's AGI. It differs in the $500 threshold, the single rate fixed for
// the whole tax year, refundable credits counted as payments, and the
// no-liability exception, which needs a full year of Minnesota residence.
//
// The figure follows the 2026 Schedule M15, marked "FINAL DRAFT — 10/1/26",
// and the result's Divergences say where the statute reads otherwise. No act
// since 2005 changed §§ 289A.25 or 270C.40 (research/estimated-payments/mn.md),
// so the form does not predate the law it describes.
//
// Not reported in Required, because no 2026 figure turns on them: the 2025
// M15 prints 7% where § 270C.40 gives 8% for tax year 2025, and farmers are
// not built, so the form's 66.7% against the statute's 66⅔% is not used.

// jurisdiction is Minnesota's postal code.
const jurisdiction = estimated.Minnesota

// EstimatedFacts are the facts Minnesota's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	refundableCredits optional
	stepOnShare       bool
	stepAnswered      bool
}

type optional struct {
	v   money.USCents
	set bool
}

// RefundableCredits are this year's refundable credits, Form M1 line 22.
// Schedule M15 counts them with withholding on line 2, and § 289A.25 subd. 8
// treats them as paid evenly on the four due dates.
func (e EstimatedFacts) RefundableCredits() (money.USCents, bool) {
	return e.refundableCredits.v, e.refundableCredits.set
}

// PriorYearStepOnMinnesotaShare reports whether the $150,000 test on last
// year's AGI reads the Minnesota share of AGI rather than federal AGI, and
// whether the caller said. § 289A.25 subd. 5(2)(i) uses the Minnesota share
// "for an individual who is not a Minnesota resident for the entire year"
// without saying which year.
func (e EstimatedFacts) PriorYearStepOnMinnesotaShare() (onShare, answered bool) {
	return e.stepOnShare, e.stepAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) RefundableCredits(a money.USCents) *EstimatedFactsBuilder {
	b.e.refundableCredits = optional{a, true}
	return b
}

// PriorYearStepOnMinnesotaShare answers which AGI the $150,000 test reads for
// a filer who was not a Minnesota resident all of one of the two years.
func (b *EstimatedFactsBuilder) PriorYearStepOnMinnesotaShare(onShare bool) *EstimatedFactsBuilder {
	b.e.stepOnShare, b.e.stepAnswered = onShare, true
	return b
}

// Build refuses negative refundable credits.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.refundableCredits.v < 0 {
		return EstimatedFacts{}, errors.New("minnesotataxcalc: refundable credits cannot be negative")
	}
	return b.e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is M15 line 3: no penalty when tax less withholding and
	// credits is under it.
	threshold money.USCents

	// thisYearShare, priorYearShare and priorYearHigh are M15 lines 4 and 5:
	// 90% of this year's tax, 100% of last year's, or 110% above step.
	thisYearShare, priorYearShare, priorYearHigh money.Rate
	step                                         money.USCents

	// nonresidentMinimum is the least prior-year tax a nonresident or
	// part-year resident needs for the prior-year test.
	nonresidentMinimum money.USCents

	shares     []money.Rate
	annualised []money.Rate
	dues       []estimated.Date
	calendar   estimated.Calendar
	periodEnds []estimated.Date

	// fileAndPayBy and deemedPaid are the M15 line 14 rule: tax paid with a
	// return filed by fileAndPayBy counts as paid on deemedPaid.
	fileAndPayBy, deemedPaid estimated.Date

	stop estimated.Date

	// rates is the one rate § 270C.40 subd. 4 fixes for the whole tax year,
	// over every day a 2026 underpayment can run.
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Minnesota's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return minnesotaEstimated{y}, nil
}

type minnesotaEstimated struct{ y estimatedYear }

type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents
	refundable   money.USCents
	filedByJan31 bool
	notes        working.Notes
}

func (r minnesotaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("minnesotataxcalc: the facts carry %T, not minnesotataxcalc.EstimatedFacts", s)
	}
}

func (r minnesotaEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("minnesotataxcalc: these are Minnesota's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("minnesotataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: farmers and commercial fishermen make one payment by January 15 at 66.7%%, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("minnesotataxcalc: M15 annualized worksheet period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's publications and the statutes part
// ways on a rule every Minnesota computation uses. The first five and the
// last are in the research file; the line 17 rule was found reading the 2026
// M15 against § 289A.25 subd. 3. Each says whether it changes anything for f.
func (r minnesotaEstimated) divergences(f estimated.Facts, a assessment) ([]working.Divergence, error) {
	y := r.y
	var short, partPaid, earlyReturn, february1, waiverAsked bool
	if !a.exempt && a.requirement.TargetKnown() {
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return nil, err
		}
		for i, l := range ledger {
			if l.Shortfall() <= 0 {
				continue
			}
			short = true
			ends := map[estimated.Date]bool{}
			for _, piece := range l.Pieces() {
				on, paid := piece.PaidOn()
				ends[on] = true
				// An earlier shortfall still unpaid on January 15 runs on
				// under the statute.
				if a.filedByJan31 && i < 3 && (!paid || on.After(y.deemedPaid)) {
					earlyReturn = true
				}
			}
			partPaid = partPaid || len(ends) > 1
		}
		next := y.calendar.NextBusinessDay(y.fileAndPayBy)
		february1 = next != y.fileAndPayBy && f.ReturnFiled() == next
		waiver, answered := f.Positions().Waiver()
		waiverAsked = short && answered && waiver != estimated.WaiverNone
	}
	paid := short && len(f.Payments()) > 0
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("When a payment counts as made").
			Form("A payment can be due on a weekend or holiday. If so, the department must receive it by the next business day.",
				"Minnesota Department of Revenue, Estimated Tax Payments for Individuals (updated January 7, 2026)").
			Statute("A payment sent by US mail counts as made on its postmark date. An electronic one counts as made on its confirmation time-and-date stamp.",
				"Minn. Stat. § 270C.395 subd. 1").
			Uses(working.ReadingForm).Affects(paid),
		working.NewDivergence("A shortfall paid in part").
			Form("Line 17 takes the date the underpayment was paid in full. The whole shortfall is charged until then.",
				"2026 Schedule M15, lines 16 to 21 and the Line 17 instructions").
			Statute("The underpayment period ends, for any part of it, on the date that part is paid.",
				"Minn. Stat. § 289A.25 subd. 3(a)(2)").
			Uses(working.ReadingForm).Affects(partPaid),
		working.NewDivergence("A return filed and paid by January 31").
			Form("Tax paid with a return filed by Jan 31, 2027 counts as paid on Jan 15, 2027. Every shortfall still unpaid on that date stops being charged that day.",
				"2026 Schedule M15, Line 14 instructions").
			Statute("No penalty is charged on a shortfall in the fourth installment. Earlier installments are charged until paid.",
				"Minn. Stat. § 289A.25 subd. 9").
			Uses(working.ReadingForm).Affects(earlyReturn),
		working.NewDivergence("January 31, 2027 is a Sunday").
			Form("The form prints Jan 31, 2027 and says nothing of moving it for the weekend. The date is therefore not moved.",
				"2026 Schedule M15, Line 14 instructions").
			Statute("A document due on a Saturday, Sunday or legal holiday can be filed later. It is on time if filed the next day that is none of these. A return filed and paid on Feb 1, 2027 would therefore be on time.",
				"Minn. Stat. § 645.151").
			Uses(working.ReadingForm).Affects(february1),
		working.NewDivergence("The optional short method").
			Form("The short method charges 2% of the year's underpayment. It subtracts 0.000192 a day for amounts paid before Apr 15, 2027. The form also offers the regular method.",
				"2026 Schedule M15, lines 7 to 12").
			Statute("The charge is the year's rate on each underpayment for the time it's unpaid. At 7%, that is about 4.66% for four quarters held to Apr 15, 2027.",
				"Minn. Stat. § 289A.25 subd. 2; § 270C.40 subd. 4").
			Uses(working.ReadingStatute).Affects(short),
		working.NewDivergence("Waivers for retirement, disability or casualty").
			Form("If the IRS waived your federal penalty, you don't complete Schedule M15. Retirement, disability, casualty, disaster and unusual circumstances all count.",
				"2026 Schedule M15 instructions, Exceptions to the Penalty").
			Statute("The federal waiver covers retirement, disability, casualty or disaster. No penalty is charged to the extent the commissioner finds it applies.",
				"Minn. Stat. § 289A.25 subd. 7").
			Uses(working.ReadingForm).Affects(waiverAsked),
		working.NewDivergence("Who counts as a farmer").
			Form("A farmer or commercial fisherman meets a two-thirds test on this year only. At least two-thirds of annual gross income must come from farming or fishing.",
				"2026 Schedule M15 instructions, Farmers and commercial fishermen").
			Statute("The federal definition applies. It also counts two-thirds of gross income shown on last year's return.",
				"Minn. Stat. § 289A.25 subd. 10; 26 U.S.C. § 6654(i)(2)(B)").
			// Last year's farm share is not among the facts.
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

// assess is the requirement and installments, with the divergences.
func (r minnesotaEstimated) assess(f estimated.Facts) (assessment, error) {
	a, err := r.measure(f)
	if err != nil {
		return assessment{}, err
	}
	divergences, err := r.divergences(f, a)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

func (r minnesotaEstimated) measure(f estimated.Facts) (assessment, error) {
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
	refundable, refundableKnown := extras.RefundableCredits()
	a.refundable = refundable
	credited := withheld + refundable
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
		a.notes.Rule("Minn. Stat. § 289A.25 subd. 5(1); 2026 Schedule M15, line 4", fmt.Sprintf("%v of this year's Minnesota tax of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// Residence in each year decides the $1 minimum and which AGI the step
	// reads. This year's residence counts as full-year when not given.
	priorResident, priorAnswered := prior.FullYearResident()
	thisResident := len(f.Residence()) == 0 || estimated.ResidentAllYear(f.Residence(), jurisdiction, y.year)
	nonresident := (priorAnswered && !priorResident) || !thisResident

	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Minnesota return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Minnesota return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Minnesota return covered %d months, not 12", last, prior.Months()))
	case priorTax == 0:
		priorOption = unavailable(fmt.Sprintf("Your %d return showed no Minnesota tax", last))
	case nonresident && priorTax < y.nonresidentMinimum:
		priorOption = unavailable(fmt.Sprintf("This test has a minimum for a part-year resident or nonresident. You need at least %s of %d Minnesota tax for it", y.nonresidentMinimum.Whole(), last))
	default:
		onShare, answered := extras.PriorYearStepOnMinnesotaShare()
		if !answered {
			onShare = priorAnswered && !priorResident
			if nonresident {
				which := "your federal AGI"
				if onShare {
					which = "your Minnesota share"
				}
				a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Should the %s step use your Minnesota share of AGI or your federal AGI?", y.step.Whole()),
					fmt.Sprintf("For anyone not resident all year, the law uses the share but names no year. The figures go by your %d residence and use %s.", last, which))
			}
		}
		agi, agiKnown := priorFigures.FederalAGI()
		agiName, agiAsk := "federal AGI", "federal adjusted gross income"
		if onShare {
			agi, agiKnown = priorFigures.StateAGI()
			agiName, agiAsk = "Minnesota share of AGI", "Minnesota share of adjusted gross income"
		}
		rate := y.priorYearShare
		why := fmt.Sprintf("%d %s of %v is not more than %v", last, agiName, agi, y.step)
		switch {
		case !agiKnown:
			rate = y.priorYearHigh
			why = fmt.Sprintf("%d %s was not given", last, agiName)
			a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d %s?", last, agiAsk),
				fmt.Sprintf("Until then, the last-year option is %v of last year's tax. At %s or less it would be %v.", y.priorYearHigh, y.step.Whole(), y.priorYearShare))
		case agi > y.step:
			rate = y.priorYearHigh
			why = fmt.Sprintf("%d %s of %v is more than %v", last, agiName, agi, y.step)
		}
		amount := rate.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, rate, amount)
		a.notes.Rule("Minn. Stat. § 289A.25 subd. 5(2); 2026 Schedule M15, line 5", fmt.Sprintf("%s, so the prior-year test is %v of the %v tax on the %d return, or %v", why, rate, priorTax, last, amount))
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
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Minnesota tax to be?", y.year),
			fmt.Sprintf("It sets the %v option and the %s test.", y.thisYearShare, y.threshold.Whole()))
	}

	// § 289A.25 subd. 6: no liability last year, a 12-month year, and a full
	// year of Minnesota residence.
	noLiability := hasPrior && prior.Months() == 12 && ((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	if noLiability && !priorAnswered {
		a.notes.Ask(working.PositionResidency, fmt.Sprintf("Did you live in Minnesota all of %d?", last),
			fmt.Sprintf("If you did, having no Minnesota tax for %d means no penalty for %d.", last, y.year))
	}

	switch {
	case noLiability && priorAnswered && priorResident:
		a.exempt = true
		none, err := estimated.NewException("No tax due last year as a full-year resident").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no Minnesota tax for %d, a full year as a Minnesota resident. That means no estimated payments are needed for %d.", last, y.year)).Exempt(none)
		a.notes.Rule("Minn. Stat. § 289A.25 subd. 6; 2026 Schedule M15 instructions", fmt.Sprintf("the %d return showed no liability for a full 12 months of Minnesota residence, so there is no penalty for %d", last, y.year))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("A target needs your %d Minnesota tax or a full-year %d return. There's no target yet.", y.year, last)).NoTarget()
	case !taxKnown && credited >= target:
		rb.Required(false, fmt.Sprintf("Your target is %v of your %d tax, %s. Your withholding and refundable credits of %s already reach it. No estimated payments are needed, whatever your %d tax turns out to be.",
			priorOpt.Percent(), last, target.Whole(), credited.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. After %s of withholding and credits, pay %s in estimated payments. Pay a quarter by each date. This holds whatever your %d tax turns out to be.",
			priorOpt.Percent(), last, target.Whole(), credited.Whole(), (target-credited).Whole(), y.year)).Choose(chosen)
	case tax-credited < y.threshold:
		a.exempt = true
		small, err := estimated.ExceptionBelow("Tax less withholding and credits", tax-credited, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		so := fmt.Sprintf("That's under %s, so no estimated payments are needed.", y.threshold.Whole())
		if tax < credited {
			so = "That means no estimated payments are needed."
		}
		rb.Required(false, fmt.Sprintf("Your withholding and refundable credits are %s. Your Minnesota tax of %s less that %s. %s",
			credited.Whole(), tax.Whole(), estimated.Leaves(tax-credited), so)).Exempt(small)
		a.notes.Rule("Minn. Stat. § 289A.25 subd. 4; 2026 Schedule M15, line 3", fmt.Sprintf("%v of tax less %v of withholding and credits is %v, under %v, so there is no penalty", tax, credited, tax-credited, y.threshold))
	case credited >= target:
		rb.Required(false, fmt.Sprintf("Your Minnesota withholding and refundable credits are %s. That already reaches the %s target, so no estimated payments are needed.", credited.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your withholding and refundable credits are %s. Your Minnesota tax of %s less that leaves %s. That's %s or more, so estimated payments are needed. You need %s of them to reach the %s target. Pay a quarter by each date.",
			credited.Whole(), tax.Whole(), (tax-credited).Whole(), y.threshold.Whole(), (target-credited).Whole(), target.Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// Under an exception last year's return can't change the target.
	if !hasPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Minnesota return, and what was its tax?", last),
			fmt.Sprintf("With it, last year's tax could be a lower target than %v of this year's.", y.thisYearShare))
	}
	if !a.exempt && !refundableKnown {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What are your %d Minnesota refundable credits, from Form M1 line 22?", y.year),
			"They count as paid evenly on the four dates; until then only withholding counts.")
	}

	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("Minn. Stat. § 289A.25 subds. 3(b), 5; 2026 Schedule M15, line 13", fmt.Sprintf("installments of 25%% of %v are due %s, %s, %s and %s", a.requirement.Target(),
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
		a.notes.Rule("Minn. Stat. § 289A.25 subd. 5(3); 2026 Schedule M15, Worksheet - Annualized Income Installment", fmt.Sprintf("under the annualized method the installments are %v, %v, %v and %v: 22.5%%, 45%%, 67.5%% and 90%% of the annualized tax, "+
			"less earlier installments, and never more than the regular 25%% plus any reduction carried forward", ann[0], ann[1], ann[2], ann[3]))
	}

	// The M15 line 14 rule. January 31, 2027 is a Sunday; the form prints
	// January 31 and says nothing of moving it, so it is not moved.
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(y.fileAndPayBy) && !a.exempt {
		a.filedByJan31 = true
		a.notes.Rule("2026 Schedule M15, Line 14 instructions; Minn. Stat. § 289A.25 subd. 9", fmt.Sprintf("the return was filed and paid on %s, by %s, so the tax paid with it counts as paid on %s",
			filed.Long(), y.fileAndPayBy.Long(), y.deemedPaid.Long()))
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}
	return a, nil
}

func (r minnesotaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies withholding, refundable credits and payments to the
// installments.
func (r minnesotaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, r.y.shares)
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	// § 289A.25 subd. 8: refundable credits are paid evenly on the due dates.
	cum, err := estimated.CumulativeShares(a.refundable, r.y.shares)
	if err != nil {
		return nil, working.Notes{}, err
	}
	for i, amt := range estimated.Increments(cum) {
		if amt == 0 {
			continue
		}
		p, err := estimated.NewPayment().On(a.dues[i]).Amount(amt).Build()
		if err != nil {
			return nil, working.Notes{}, err
		}
		credits = append(credits, p)
	}
	ledger, err := estimated.Apply(a.dues, a.installments, credits)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("Minn. Stat. § 289A.25 subd. 8; 2026 Schedule M15, Line 14 instructions",
			fmt.Sprintf("the %v withheld is credited on the dates it was withheld, as shown; the %v of refundable credits is treated as paid evenly on the due dates", f.WithheldTotal(), a.refundable))
	} else {
		notes.Rule("Minn. Stat. § 289A.25 subd. 8; 2026 Schedule M15, Line 14 instructions",
			fmt.Sprintf("the %v withheld and the %v of refundable credits are treated as paid in four equal parts on the due dates, whenever they actually came", f.WithheldTotal(), a.refundable))
	}
	return ledger, notes, nil
}

func (r minnesotaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Minnesota return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	out, _, notes, err := r.schedule(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments after the credits, with the ledger they come
// from. A return filed and paid by January 31 excuses the fourth.
func (r minnesotaEstimated) schedule(f estimated.Facts, a assessment) ([]estimated.Installment, []estimated.LedgerEntry, working.Notes, error) {
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return nil, nil, working.Notes{}, err
	}
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(l.NeededBy()).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if a.filedByJan31 && i == 3 {
			b.Excused()
		}
		if out[i], err = b.Build(); err != nil {
			return nil, nil, working.Notes{}, err
		}
	}
	return out, ledger, notes, nil
}

func (r minnesotaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("minnesotataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Minnesota return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	if a.filedByJan31 && y.deemedPaid.Before(stop) {
		stop = y.deemedPaid
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

	// M15 lines 16 to 21: each column's whole underpayment, from its due date
	// to the date it was paid in full or the stop, at the year's rate.
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, sched)
	var unrated estimated.Date
	for _, l := range ledger {
		if l.Shortfall() <= 0 {
			continue
		}
		to := l.Due()
		for _, piece := range l.Pieces() {
			on, paid := piece.PaidOn()
			if !paid || on.After(stop) {
				on = stop
			}
			if on.After(to) {
				to = on
			}
		}
		lb := estimated.NewPenaltyLine(l.Number(), l.Due(), l.Shortfall())
		if to.After(l.Due()) {
			parts, gap, err := estimated.SimpleDaily(l.Shortfall(), l.Due(), to, y.rates)
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
		line, err := lb.Days(to.DaysSince(l.Due())).Build()
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
		notes.Rule("Minn. Stat. § 289A.25 subd. 5", fmt.Sprintf("this year's Minnesota tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("Minn. Stat. § 289A.25 subd. 3(a); 2026 Schedule M15, Line 17 instructions", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", f.ReturnFiled().Long()))
	}
	notes.Rule("Minn. Stat. §§ 289A.25 subds. 2, 3, 270C.40 subd. 4; 2026 Schedule M15, lines 16 to 22", fmt.Sprintf("each shortfall is charged the %d rate for the days from its due date until it was paid in full or %s, "+
		"with each payment applied to the oldest shortfall first; the penalty to %s is %v", y.year, y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", unrated.AddDays(-1).Long()))
	}
	// § 289A.25 subd. 7 adopts the federal waivers of § 6654(e)(3).
	if o, ok := estimated.StandardWaiver(f, "Minnesota's Department of Revenue", 62); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking Minnesota to waive the penalty?",
			"It follows the IRS's decision, for a casualty, disaster or unusual circumstance. The same goes for retirement after 62 or disability with reasonable cause.")
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r minnesotaEstimated) Explanation() estimated.Explanation { return minnesotaExplanation }

// minnesotaExplanation is written by hand and checked against
// research/estimated-payments/mn.md.
var minnesotaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Minnesota expects estimated payments once $500 or more would be due. That figure is after withholding and refundable credits. The penalty is interest on each quarter's shortfall. It uses one rate for the year, 7% for 2026.").
		Paragraph("Withholding, refundable credits and estimated payments count toward a target. It is the smaller of 90% of this year's Minnesota tax or 100% of last year's. Reaching it keeps you clear of the penalty. Last year's figure becomes 110% if last year's federal AGI was over $150,000. There is no lower figure for married filing separately. Last year's figure needs a full-year return that showed some Minnesota tax.").
		Paragraph("The payments are a quarter each. They are due April 15, June 15, September 15 and January 15 of the next year. Withholding and refundable credits count as paid evenly across those dates. This holds unless you show when they were actually paid. A return filed with the tax paid by January 31 counts as paid on January 15.").
		Paragraph("No penalty is due if this year's tax less withholding and credits is under $500. The same holds if you had no Minnesota tax last year and lived there all of it. If the IRS waives your federal penalty, Minnesota does too. That covers retiring, becoming disabled, a casualty or a disaster.").
		Paragraph("The penalty is figured by the day on Schedule M15. It runs from each due date until the shortfall is paid in full. It stops on April 15 of the next year. The rate is set once from the prime rate and holds for the whole tax year. An annualized method can lower early payments if income comes late in the year.").
		VersusFederal("Minnesota's threshold is $500, not $1,000. The $150,000 step has no $75,000 figure for married filing separately. Refundable credits count as payments spread over the year. The no-liability exception needs a full year of Minnesota residence. One rate covers the whole tax year. A return filed by January 31 counts as paid on January 15.").
		Source("Minn. Stat. §§ 289A.25, 270C.40, 270C.395, 645.151").
		Source("2026 Schedule M15 and its instructions (final draft, October 1, 2026)").
		Source("Minnesota Department of Revenue, Estimated Tax Payments for Individuals (updated January 7, 2026)").
		Source("Minnesota Department of Revenue, Penalties and Interest for Individuals (interest rate table)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
