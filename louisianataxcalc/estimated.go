package louisianataxcalc

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Louisiana's estimated tax rules, La. R.S. 47:116 to 47:118, as the shared
// [estimated.EstimatedTax] interface.
//
// The penalty is computed as Form R-210R computes it: a required annual
// payment, the lesser of this year's share and last year's tax, divided into
// the installments due; payments carried forward from one period to the next;
// exceptions that excuse an installment outright; and a printed daily factor
// on each shortfall for the days it was unpaid.
//
// Act 307 of 2026 changed the rules for tax periods beginning on or after
// January 1, 2026. It took effect on May 22, 2026, after the department's
// latest penalty form, R-210R-i (1/26), and its estimated tax page (updated
// February 27, 2025) were published, and the legislature's statute page for
// R.S. 47:118 still prints the earlier text. Where those documents describe
// the old law, the figure follows the Act, and the result's Divergences name
// the out-of-date document. Everywhere else the figure follows the form, and
// the Divergences say where the statute reads otherwise.

// jurisdiction is Louisiana's postal code.
const jurisdiction = estimated.Louisiana

// EstimatedFacts are the facts Louisiana's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorFactsTax          optional
	jointStatuteThreshold  bool
	jointThresholdAnswered bool
}

type optional struct {
	v   money.USCents
	set bool
}

// PriorYearFactsTax is the tax for R.S. 47:118(D)(1)(b) as Act 307 words it
// from 2026: last year's return figures taxed at this year's 3% rate and
// this year's standard deduction ($12,875, or $25,750 for a joint return or
// a head of household, IT-540ESi (2026) line 3), less last year's credits as
// R-210R Exception 3 line 5 subtracts them. The caller computes it; this
// package holds no Louisiana income tax tables.
func (e EstimatedFacts) PriorYearFactsTax() (money.USCents, bool) {
	return e.priorFactsTax.v, e.priorFactsTax.set
}

// StatuteJointThreshold reports whether a joint return is held to the
// statute's $1,000 rather than the department's $2,000, and whether the
// caller said. R.S. 47:116(A) states one $1,000 figure; IT-540ESi and R-210R
// Exception 1 use $2,000 for a joint return.
func (e EstimatedFacts) StatuteJointThreshold() (statute, answered bool) {
	return e.jointStatuteThreshold, e.jointThresholdAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorYearFactsTax(a money.USCents) *EstimatedFactsBuilder {
	b.e.priorFactsTax = optional{a, true}
	return b
}

// StatuteJointThreshold answers the joint threshold question: true holds a
// joint return to $1,000, false to $2,000.
func (b *EstimatedFactsBuilder) StatuteJointThreshold(statute bool) *EstimatedFactsBuilder {
	b.e.jointStatuteThreshold, b.e.jointThresholdAnswered = statute, true
	return b
}

// Build refuses a negative tax.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.priorFactsTax.v < 0 {
		return EstimatedFacts{}, errors.New("louisianataxcalc: last year's facts cannot give a negative tax")
	}
	return b.e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// thisYearShare is the share of this year's tax in the required annual
	// payment and in R-210R Exception 5.
	thisYearShare money.Rate

	// threshold is what this year's tax less withholding must exceed before
	// any penalty can arise; thresholdJoint is the department's figure for a
	// joint return.
	threshold, thresholdJoint money.USCents

	dues []estimated.Date

	// laterGroups are the first days of R-210R's second, third and fourth
	// rows: a requirement first met on or after laterGroups[i] starts at
	// installment i+2.
	laterGroups []estimated.Date

	calendar     estimated.Calendar
	fileAndPayBy estimated.Date
	stop         estimated.Date
	periodEnds   []estimated.Date
	daily        estimated.FactorSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are the 25%, 50%, 75% and 100% R-210R Exceptions 2 and 3 apply to
// last year's figures, and the equal part of withholding R.S. 47:118(E)(2)
// deems paid on each installment date.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Louisiana's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return louisianaEstimated{y}, nil
}

type louisianaEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	dues        []estimated.Date

	// taxKnown is whether this year's tax was given. upperBound is whether
	// the regular installments rest on last year's tax alone, so the penalty
	// is the most it can be.
	taxKnown, upperBound bool

	// regular is R-210R line 5 in each column: the required annual payment
	// divided by the number of installments, in whole dollars, and nothing
	// before the requirement was first met.
	regular []money.USCents

	// exceptions are the running totals that excuse an installment, R-210R
	// Exceptions 2, 3 and 5, each with the reason it gives.
	exceptions []exception

	excusedJanuary bool
	notes          working.Notes
}

type exception struct {
	cum []money.USCents
	why string
}

func (r louisianaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("louisianataxcalc: the facts carry %T, not louisianataxcalc.EstimatedFacts", s)
	}
}

func (r louisianaEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("louisianataxcalc: these are Louisiana's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("louisianataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: a farmer or fisherman with two-thirds of gross income from farming or fishing makes one payment by January 15 at 66 2/3%%, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("louisianataxcalc: R-210R Exception 5 period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// outOfDate cites when Act 307 took effect against the documents that
// predate it.
const outOfDate = "Act 307 of 2026 took effect May 22, 2026, for tax periods beginning on or after January 1, 2026; R-210R-i is revised 1/26, LDR's estimated tax page was updated February 27, 2025, and the legislature's R.S. 47:118 page still prints the text before the Act"

// predates is what an out-of-date document's reading says of itself.
const predates = " These documents predate Act 307 and are out of date for 2026."

// affected says which divergences change anything for one household's
// facts.
type affected struct {
	share, annualised, facts, joint, withholding, partYear, lateStart bool
	jointStatute                                                      bool
}

// divergences are where Louisiana's documents and R.S. 47:116 to 47:118 part
// ways on a rule the requirement uses. The first three are where a document
// predates Act 307 and the figure follows the Act. They are found in
// research/estimated-payments/la.md. The rules the ledger and the penalty
// use are in ledgerDivergence and penaltyDivergences.
func (r louisianaEstimated) divergences(a affected) ([]working.Divergence, error) {
	joint := working.ReadingForm
	if a.jointStatute {
		joint = working.ReadingStatute
	}
	return buildAll(
		working.NewDivergence("This year's share of the tax").
			Form("The required payment and Exception 5 use 90% of this year's tax."+predates,
				"R-210R-i (1/26), Section 1 and Line 14; LDR, Declaration of Estimated Income Taxes (updated February 27, 2025); "+outOfDate).
			Statute("The required payment and income-to-date exception use 70% of this year's tax.",
				"La. R.S. 47:118(B)(1)(a), (B)(2)(b)(i) and (D)(2), as amended by Act 307 of 2026, §§ 1 and 3").
			Uses(working.ReadingStatute).Affects(a.share),
		working.NewDivergence("The annualized income exception").
			Form("Exception 4 annualizes income to each calendar quarter. It excuses a payment that reaches 90% of the tax on it."+predates,
				"R-210R-i (1/26), Line 13; LDR, Declaration of Estimated Income Taxes (updated February 27, 2025); "+outOfDate).
			Statute("Exception 4 is repealed for 2026. The income-to-date exception, at 70%, remains.",
				"Act 307 of 2026, § 2, repealing La. R.S. 47:118(D)(1)(c)").
			Uses(working.ReadingStatute).Affects(a.annualised),
		working.NewDivergence("Last year's figures at this year's rate").
			Form("Exception 3 uses last year's taxable income, after last year's deductions. It taxes that at this year's rate and filing status, less last year's credits. This form predates Act 307 and is out of date for 2026.",
				"R-210R-i (1/26), Line 12, Exception 3 worksheet; "+outOfDate).
			Statute("Last year's figures are taxed at this year's rate and standard deduction.",
				"La. R.S. 47:118(D)(1)(b), as amended by Act 307 of 2026, §§ 1 and 3").
			Uses(working.ReadingStatute).Affects(a.facts),
		working.NewDivergence("The threshold for a joint return").
			Form("No penalty when this year's tax less credits and withholding is $1,000 or less. On a joint return the figure is $2,000.",
				"IT-540ESi (2026), Who Must Make Estimated Tax Payments; R-210R-i (1/26), Line 10, Exception 1").
			Statute("One threshold of $1,000 for everyone. Act 307 didn't change it.",
				"La. R.S. 47:116(A); La. R.S. 47:118(D)").
			Uses(joint).Affects(a.joint),
		working.NewDivergence("When withholding counts as paid").
			Form("Withholding may be spread over the periods by the number of months in each. Or it may be counted on the dates withheld. The form doesn't define the months.",
				"R-210R-i (1/26), General Information, Applying Credits and Withholding").
			Statute("An equal part counts as paid on each due date. You can show the dates withheld instead.",
				"La. R.S. 47:118(E)(2)").
			Uses(working.ReadingStatute).Affects(a.withholding),
		working.NewDivergence("Last year's tests after a part-year return").
			Form("Last year's tax and figures aren't used after a part-year resident return.",
				"R-210R-i (1/26), Line 3, Line 11 Exception 2 and Line 12 Exception 3").
			Statute("The required payment and the exceptions use last year's return. They set no condition of full-year residence.",
				"La. R.S. 47:118(B)(2)(b)(ii) and (D)(1)(a), (b)").
			Uses(working.ReadingForm).Affects(a.partYear),
		working.NewDivergence("Needing to pay only after April 1").
			Form("If you first need to pay after April 1, only the dates left are due. Each is 1/3, 1/2 or all of the required payment.",
				"R-210R-i (1/26), General Information table, Lines 4 and 5; IT-540ESi (2026), Line 11").
			Statute("Each installment for the penalty is 25% of the required payment. The fewer, equal installments are how a late declaration is paid.",
				"La. R.S. 47:118(B)(2)(a); La. R.S. 47:117(A), 47:117.1(A)").
			Uses(working.ReadingForm).Affects(a.lateStart),
	)
}

// ledgerDivergence is the order payments are applied in, read from the
// checked-in R-210R-i and R.S. 47:118. It affects a ledger in which a
// payment on or after a later due date made up an earlier shortfall.
func ledgerDivergence(ledger []estimated.LedgerEntry) (working.Divergence, error) {
	affects := false
	for i, l := range ledger {
		for _, p := range l.Pieces() {
			if on, paid := p.PaidOn(); paid && i+1 < len(ledger) && !on.Before(ledger[i+1].Due()) {
				affects = true
			}
		}
	}
	return working.NewDivergence("Which shortfall a payment pays first").
		Form("Each period's overpayment or underpayment is carried to the next. A payment first makes up an earlier shortfall.",
			"R-210R-i (1/26), Lines 7 to 9 and the example under Line 7").
		Statute("A payment on a due date goes first to that date's installment. Only what exceeds that date's installment pays an earlier shortfall.",
			"La. R.S. 47:118(C)(2)").
		Uses(working.ReadingForm).Affects(affects).Build()
}

// penaltyDivergences are the stop date, which Act 307 moved, and the daily
// rate. The stop date affects a penalty charged after the old April 15 stop,
// and the rate any penalty charged.
func (r louisianaEstimated) penaltyDivergences(p estimated.Penalty) ([]working.Divergence, error) {
	old := estimated.MustDate(r.y.year+1, time.April, 15)
	late := false
	for _, l := range p.Lines() {
		for _, part := range l.Parts() {
			if part.To().After(old) {
				late = true
			}
		}
	}
	return buildAll(
		working.NewDivergence("When the penalty stops").
			Form("The penalty stops April 15, the 15th day of the fourth month after year end."+predates,
				"R-210R-i (1/26), Section 1 and Line 17; LDR, Declaration of Estimated Income Taxes (updated February 27, 2025); "+outOfDate).
			Statute(fmt.Sprintf("The penalty runs to the 15th day of the fifth month after the year ends. For %d that is %s.", r.y.year, r.y.stop.Long()),
				"La. R.S. 47:118(C)(1), as amended by Act 307 of 2026, §§ 1 and 3").
			Uses(working.ReadingStatute).Affects(late),
		working.NewDivergence("The daily rate").
			Form("Each day is charged at a printed 0.0329%, or 0.000329 a day.",
				"R-210R-i (1/26), Line 18").
			Statute("Twelve percent a year, 0.000328767 a day over 365 days.",
				"La. R.S. 47:118(A)").
			Uses(working.ReadingForm).Affects(p.Total() > 0),
	)
}

func buildAll(bs ...*working.DivergenceBuilder) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range bs {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// shortDate is a date as a person reads it: "Aug 31" in the tax year, and
// "Jan 15, 2027" outside it.
func (r louisianaEstimated) shortDate(d estimated.Date) string {
	if d.Year() == r.y.year {
		return d.Time().Format("Jan 2")
	}
	return d.Time().Format("Jan 2, 2006")
}

// optionLabel is an option's label and amount, for the middle of a sentence.
func optionLabel(o estimated.TargetOption) string {
	l := o.Label()
	if l != "" {
		l = strings.ToLower(l[:1]) + l[1:]
	}
	return fmt.Sprintf("%s, %s", l, o.Amount().Whole())
}

// thresholdFor is R-210R Exception 1's figure for the filing status, and
// whether the joint question needs asking.
func (r louisianaEstimated) thresholdFor(s filing.Status, e EstimatedFacts) (money.USCents, bool) {
	if s != filing.MarriedFilingJointly {
		return r.y.threshold, false
	}
	statute, answered := e.StatuteJointThreshold()
	if statute {
		return r.y.threshold, false
	}
	return r.y.thresholdJoint, !answered
}

func (r louisianaEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
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

	// R-210R line 1, at Act 307's 70%.
	share := y.thisYearShare
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, share, share.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, share, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("La. R.S. 47:118(B)(2)(b)(i), as amended by Act 307 of 2026; R-210R-i (1/26), Line 1", fmt.Sprintf("%v of this year's Louisiana tax after credits of %v is %v", share, tax, thisYear.Amount()))
	}

	// Questions whose answer can change the target, asked only when no
	// exception sets it.
	var targetAsks []func()

	// Why last year's return cannot be used, or "" when it can.
	priorBlocked := ""
	switch {
	case !hasPrior:
		priorBlocked = fmt.Sprintf("Needs your %d Louisiana return", last)
		targetAsks = append(targetAsks, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Louisiana return, and what tax did it show?", last),
				fmt.Sprintf("With it, last year's tax could set your target instead of %v of this year's.", y.thisYearShare))
		})
	case !prior.Filed():
		priorBlocked = fmt.Sprintf("You didn't file a %d Louisiana return", last)
	case !residenceAnswered:
		priorBlocked = fmt.Sprintf("Needs whether you were a full-year Louisiana resident in %d", last)
		targetAsks = append(targetAsks, func() {
			a.notes.Ask(working.PositionResidency, fmt.Sprintf("Were you a full-year Louisiana resident in %d?", last),
				"If you were, last year's return can set your target; a part-year return can't.")
		})
	case !resident:
		priorBlocked = fmt.Sprintf("You filed a part-year resident return for %d", last)
	}

	// R-210R line 2: 100% of last year's tax.
	priorOption := estimated.NewTargetOption(estimated.BasisPriorYearTax, money.Percent(100), priorTax)
	if priorBlocked != "" {
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, money.Percent(100), 0).Unavailable(priorBlocked)
	}
	priorOpt, err := priorOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if priorOpt.Available() {
		a.notes.Rule("La. R.S. 47:118(B)(2)(b)(ii); R-210R-i (1/26), Line 2", fmt.Sprintf("100%% of the %v tax on the %d return is %v", priorTax, last, priorTax))
	}

	// R.S. 47:118(D)(1)(b), Exception 3: last year's facts at this year's
	// rate and standard deduction, supplied by the caller.
	factsTax, factsKnown := extras.PriorYearFactsTax()
	const factsLabel = "Last year's figures at this year's rate"
	factsOption := estimated.NewTargetOption(estimated.BasisPriorYearIncome, money.Percent(100), factsTax).Label(factsLabel)
	switch {
	case priorBlocked != "":
		factsOption = estimated.NewTargetOption(estimated.BasisPriorYearIncome, money.Percent(100), 0).Label(factsLabel).Unavailable(priorBlocked)
	case !factsKnown:
		factsOption = estimated.NewTargetOption(estimated.BasisPriorYearIncome, money.Percent(100), 0).Label(factsLabel).
			Unavailable(fmt.Sprintf("Needs the Louisiana tax on your %d figures at the %d rate. It uses the %d standard deduction", last, y.year, y.year))
	}
	factsOpt, err := factsOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if factsOpt.Available() {
		a.notes.Rule("La. R.S. 47:118(D)(1)(b), as amended by Act 307 of 2026", fmt.Sprintf("the %d return's facts at the %d rate and standard deduction give %v; an installment is excused when payments by its date reach 25%%, 50%%, 75%% or 100%% of it",
			last, y.year, factsTax))
	}

	rb := estimated.NewRequirement().Option(thisYear).Option(priorOpt).Option(factsOpt)
	chosen, target, found := estimated.BasisUnspecified, money.USCents(0), false
	var chosenOpt estimated.TargetOption
	for _, o := range []estimated.TargetOption{thisYear, priorOpt, factsOpt} {
		if o.Available() && (!found || o.Amount() < target) {
			chosen, target, found, chosenOpt = o.Basis(), o.Amount(), true, o
		}
	}

	threshold, askJoint := r.thresholdFor(f.FilingStatus(), extras)
	if !taxKnown {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Louisiana tax to be?", y.year),
			fmt.Sprintf("No payments are needed at %s or less after withholding. The target may be %v of your tax, if lower.", threshold.Whole(), share))
	}

	switch {
	case taxKnown && tax-withheld <= threshold:
		a.exempt = true
		ex, err := estimated.ExceptionAtMost("Tax less withholding", tax-withheld, threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your Louisiana tax after credits is %s, and %s is withheld. That leaves %s, which is %s or less. No estimated payments are needed.",
			tax.Whole(), withheld.Whole(), (tax - withheld).Whole(), threshold.Whole())
		switch {
		case tax < withheld:
			why = fmt.Sprintf("Your Louisiana tax after credits is %s, and %s is withheld. That %s, so no estimated payments are needed.",
				tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
		case tax == withheld:
			why = fmt.Sprintf("Your withholding of %s covers your Louisiana tax after credits, %s. No estimated payments are needed.", withheld.Whole(), tax.Whole())
		}
		rb.Required(false, why).Exempt(ex)
		a.notes.Rule("La. R.S. 47:116(A), 47:118(D); IT-540ESi (2026); R-210R-i (1/26), Line 10, Exception 1", fmt.Sprintf("%v of tax less %v withheld is %v, not more than %v, so there is no penalty", tax, withheld, tax-withheld, threshold))
	case !found:
		rb.Required(false, fmt.Sprintf("Your target comes from your %d Louisiana tax or your %d return. Until you have one, there's no target.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your target is %s. Your withholding of %s already reaches it. So no estimated payments are needed whatever your %d tax turns out to be.",
			optionLabel(chosenOpt), withheld.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %s. It keeps you clear of a penalty whatever your %d tax turns out to be. After %s of withholding, you need %s of estimated payments.",
			optionLabel(chosenOpt), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case target == 0:
		rb.Required(false, fmt.Sprintf("Your target is %s. No estimated payments are needed.", optionLabel(chosenOpt))).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Louisiana withholding of %s already reaches your target of %s. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		lead := fmt.Sprintf("Your Louisiana tax less withholding leaves %s, which is more than %s.", (tax - withheld).Whole(), threshold.Whole())
		if (tax - withheld).Whole() == threshold.Whole() {
			lead = fmt.Sprintf("Your Louisiana tax less withholding is more than %s.", threshold.Whole())
		}
		rb.Required(true, fmt.Sprintf("%s Your target is %s. After withholding, you need %s of estimated payments.", lead, optionLabel(chosenOpt), (target-withheld).Whole())).Choose(chosen)
	}
	if !a.exempt {
		for _, ask := range targetAsks {
			ask()
		}
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	jointStatute, _ := extras.StatuteJointThreshold()
	met := f.RequirementFirstMet()
	live := !a.exempt && found
	divergences, err := r.divergences(affected{
		share:        live && taxKnown && (!priorOpt.Available() || share.Of(tax) < priorOpt.Amount() || f.Elections().Annualised()),
		annualised:   live && f.Elections().Annualised(),
		facts:        live && factsOpt.Available(),
		joint:        f.FilingStatus() == filing.MarriedFilingJointly && taxKnown && tax-withheld > y.threshold && tax-withheld <= y.thresholdJoint,
		withholding:  live && withheld > 0 && !f.Elections().WithholdingAsWithheld(),
		partYear:     !a.exempt && hasPrior && prior.Filed() && residenceAnswered && !resident,
		lateStart:    live && !met.IsZero() && !met.Before(y.laterGroups[0]),
		jointStatute: jointStatute,
	})
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	if askJoint && taxKnown && tax-withheld > y.threshold && tax-withheld <= y.thresholdJoint {
		a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("The law sets one %s threshold; the department gives joint returns %s. With %s due after withholding, your figures follow the department.",
			y.threshold.Whole(), y.thresholdJoint.Whole(), (tax-withheld).Whole()))
	}
	if a.exempt || !found {
		a.regular = make([]money.USCents, len(a.dues))
		return a, nil
	}
	if priorBlocked == "" && !factsKnown {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What is the tax on your %d figures at the %d rate and standard deduction?", last, y.year),
			"Paying 25%, 50%, 75% and 100% of it by the four dates also avoids the penalty.")
	}

	// R-210R line 3: the lesser of lines 1 and 2. Without this year's tax it
	// is line 2, the most line 3 can be. Last year's facts are open only
	// with last year's return, so there is always a line 2 when there is a
	// target.
	line3 := thisYear.Amount()
	switch {
	case !taxKnown:
		line3, a.upperBound = priorOpt.Amount(), true
	case priorOpt.Available() && priorOpt.Amount() < line3:
		line3 = priorOpt.Amount()
	}

	first := r.lateStart(f, &a)
	n := len(a.dues) - first
	a.regular = make([]money.USCents, len(a.dues))
	// R-210R line 5: line 3 divided by n, rounded to the whole dollar.
	each := estimated.RoundToDollar.Div(big.NewInt(int64(line3)), big.NewInt(int64(n)))
	for i := first; i < len(a.dues); i++ {
		a.regular[i] = each
	}
	a.notes.Rule("La. R.S. 47:118(B)(2); R-210R-i (1/26), Lines 3 to 5", fmt.Sprintf("the required annual payment of %v divided by %d installments is %v, in whole dollars, due on each of %s",
		line3, n, each, dueList(a.dues[first:])))

	// Exceptions 2 and 3, a quarter of last year's figure a date.
	if priorOpt.Available() && prior.Months() == 12 && priorTax > 0 {
		cum, err := estimated.CumulativeShares(priorTax, quarters)
		if err != nil {
			return assessment{}, err
		}
		a.exceptions = append(a.exceptions, exception{cum, "Exception 2: payments reached last year's tax for the dates so far"})
	}
	if factsOpt.Available() {
		cum, err := estimated.CumulativeShares(factsTax, quarters)
		if err != nil {
			return assessment{}, err
		}
		a.exceptions = append(a.exceptions, exception{cum, "Exception 3: payments so far reached last year's facts at this year's rate"})
	}

	// Exception 5: 70% of the tax on the income actually earned to each
	// period end. The caller's Period.AnnualisedTax carries that tax.
	if f.Elections().Annualised() {
		cum := make([]money.USCents, len(a.dues))
		for i, p := range f.Periods() {
			cum[i] = share.Of(p.AnnualisedTax())
		}
		a.exceptions = append(a.exceptions, exception{cum, "Exception 5: payments reached 70% of the tax on income earned to period end"})
		a.notes.Rule("La. R.S. 47:118(D)(2), as amended by Act 307 of 2026; R-210R-i (1/26), Line 14", fmt.Sprintf("an installment is also excused when payments by its date reach %v of the tax on the income earned to March 31, May 31, August 31 and December 31: %v, %v, %v and %v",
			share, cum[0], cum[1], cum[2], cum[3]))
	}

	// January 31: R.S. 47:116(F). January 31, 2027 is a Sunday; IT-540ESi
	// prints January 31 and says nothing of moving it, so it is not moved.
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(y.fileAndPayBy) {
		a.excusedJanuary = true
		a.notes.Rule("La. R.S. 47:116(F); IT-540ESi (2026), Exception to the January 15th Declaration Requirement", fmt.Sprintf("the return was filed and paid on %s, by %s, so the January installment is excused; earlier shortfalls are not", filed.Long(), y.fileAndPayBy.Long()))
	}
	return a, nil
}

// lateStart returns the first installment the requirement applies to, from 0.
func (r louisianaEstimated) lateStart(f estimated.Facts, a *assessment) int {
	y := r.y
	met := f.RequirementFirstMet()
	if met.IsZero() {
		a.notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first expect to need estimated payments for %d?", y.year),
			fmt.Sprintf("Until then, Jan 1 is assumed. A start after %s means fewer, larger payments.", r.shortDate(y.laterGroups[0].AddDays(-1))))
		return 0
	}
	first := 0
	for i, g := range y.laterGroups {
		if !met.Before(g) {
			first = i + 1
		}
	}
	if first > 0 {
		a.notes.Rule("La. R.S. 47:117(A), 47:117.1(A); R-210R-i (1/26), General Information table and Line 4", fmt.Sprintf("the requirement was first met on %s, so %d installments are required, from %s", met.Long(), len(y.dues)-first, a.dues[first].Long()))
	}
	return first
}

func dueList(ds []estimated.Date) string {
	s := ""
	for i, d := range ds {
		switch {
		case i == 0:
		case i == len(ds)-1:
			s += " and "
		default:
			s += ", "
		}
		s += d.Long()
	}
	return s
}

// ledger applies the credits to the regular installments as R-210R lines 6
// to 9 do.
func (r louisianaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, quarters)
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	required := append([]money.USCents(nil), a.regular...)
	if a.excusedJanuary {
		required[3] = 0
	}
	ledger, err := estimated.Apply(a.dues, required, credits)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("La. R.S. 47:118(E)(2); R-210R-i (1/26), Applying Credits and Withholding",
			fmt.Sprintf("by election, the %v withheld is counted on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("La. R.S. 47:118(E)(2)",
			fmt.Sprintf("an equal part of the %v withheld is treated as paid on each installment date, whenever it was actually withheld", f.WithheldTotal()))
	}
	notes.Rule("R-210R-i (1/26), Lines 6 to 9", "each period's overpayment is carried to the next, and a payment after a due date first makes up the oldest shortfall, on the day it is paid")
	return ledger, notes, nil
}

// standing is whether an installment is excused, and why: the January
// installment for a return filed and paid by January 31, or a date an
// exception covers, with what the exception needed by it.
type standing struct {
	excused bool
	why     string
	needs   money.USCents
}

func (r louisianaEstimated) standings(a assessment, ledger []estimated.LedgerEntry) []standing {
	out := make([]standing, len(ledger))
	for i, l := range ledger {
		var s standing
		if a.excusedJanuary && i == 3 {
			s = standing{excused: true, why: "the return was filed and paid by January 31"}
		} else {
			for _, e := range a.exceptions {
				if l.CreditedBy() >= e.cum[i] {
					s = standing{excused: true, why: e.why, needs: e.cum[i]}
					break
				}
			}
		}
		out[i] = s
	}
	return out
}

// installments are the due dates with the regular running total needed by
// each, excused where an exception covers it.
func (r louisianaEstimated) installments(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	for i, s := range r.standings(a, ledger) {
		l := ledger[i]
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(l.NeededBy()).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if s.excused {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r louisianaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r louisianaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Louisiana return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := r.installments(a, ledger)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	for i, s := range r.standings(a, ledger) {
		l := ledger[i]
		switch {
		case !s.excused || l.Shortfall() == 0:
		case s.needs > 0:
			notes.Rule("La. R.S. 47:118(D); R-210R-i (1/26), Section 3", fmt.Sprintf("the %s installment was %v short and bears no penalty under %s, which needs %v by then", l.Due().Long(), l.Shortfall(), s.why, s.needs))
		default:
			notes.Rule("La. R.S. 47:118(D); R-210R-i (1/26), Section 3", fmt.Sprintf("the %s installment was %v short and bears no penalty under %s", l.Due().Long(), l.Shortfall(), s.why))
		}
	}
	order, err := ledgerDivergence(ledger)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	notes.Diverges(order)
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

func (r louisianaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("louisianataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	y := r.y
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Louisiana return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, y.year-1)
	}
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
	order, err := ledgerDivergence(ledger)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if a.exempt {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		ds, err := r.penaltyDivergences(p)
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		var dn working.Notes
		dn.Diverges(append([]working.Divergence{order}, ds...)...)
		return working.NewResult(p).With(a.notes).With(dn).Build()
	}
	standings := r.standings(a, ledger)
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	for i, s := range standings {
		l := ledger[i]
		if s.excused || l.Shortfall() == 0 {
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
			parts, gap, err := estimated.DailyFactor(piece.Amount(), l.Due(), to, y.daily)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			if !gap.IsZero() {
				return working.Result[estimated.Penalty]{}, fmt.Errorf("louisianataxcalc: the daily factor does not cover %v", gap)
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
	if a.upperBound {
		pb.UpperBound()
	}
	p, err := pb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if a.upperBound {
		notes.Rule("La. R.S. 47:118(B)(2)(b); R-210R-i (1/26), Line 3", fmt.Sprintf("this year's Louisiana tax was not given, so the penalty is figured against 100%% of the %d tax alone; "+
			"the required annual payment is the lesser of that and 70%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower", y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("La. R.S. 47:118(C)(2); R-210R-i (1/26), Line 16", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("La. R.S. 47:118(A), (C)(1), as amended by Act 307 of 2026; R-210R-i (1/26), Lines 15 to 19", fmt.Sprintf("each shortfall no exception covers is charged 0.000329 a day, to the cent, from its due date until paid or %s; "+
		"the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking Louisiana on Form R-20128 to waive your penalty for good faith?",
			"Circumstances beyond your control are presumed good faith. So is declaring and paying before the return is due; apply within a year after.")
	}
	if o, ok := louisianaWaiver(f); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	ds, err := r.penaltyDivergences(p)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	notes.Diverges(append([]working.Divergence{order}, ds...)...)
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// louisianaWaiver decides a waiver asked for. La. R.S. 47:118(I) lets the
// secretary waive the penalty for good faith, presumed for extraordinary
// circumstances beyond the individual's control, on an application within a
// year of the return's due date. It has no retirement or disability waiver.
func louisianaWaiver(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	k, answered := f.Positions().Waiver()
	switch {
	case !answered || k == estimated.WaiverNone:
		return estimated.WaiverOutcome{}, false
	case k == estimated.WaiverCasualty:
		return estimated.WaiverEligible("You'd apply for a waiver on Form R-20128 within a year of the return's due date. Louisiana's revenue secretary may grant it for events beyond your control."), true
	default:
		return estimated.WaiverRefused("Louisiana has no waiver for retirement or disability. The department waives only for good faith, such as events beyond your control."), true
	}
}

func (r louisianaEstimated) Explanation() estimated.Explanation { return louisianaExplanation }

// louisianaExplanation is written by hand and checked against
// research/estimated-payments/la.md.
var louisianaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Louisiana expects estimated payments once you'll have more than $1,000 due. That's after credits and withholding, and $2,000 for a joint return. From 2026 you avoid the penalty by paying the lesser of two amounts. One is 70% of this year's tax, and the other is all of last year's.").
		Paragraph("Act 307, a 2026 law, cut the required share of this year's tax from 90% to 70%. The other figure is 100% of last year's Louisiana tax. There's no higher figure for high incomes. A third test taxes last year's figures at this year's rate. It uses this year's standard deduction too. A payment date is also excused if what you paid by then reaches that tax.").
		Paragraph("Equal payments are due April 15, June 15, September 15 and the next January 15. A date on a weekend or holiday is due the next business day instead. Withholding counts as paid in equal parts on the four dates. You can instead show when it was actually withheld. Filing your return with full payment by January 31 replaces the January payment. Earlier payments are still due.").
		Paragraph("The penalty is 12% a year on each shortfall, charged by the day until it's paid. From 2026 it stops on May 15 of the next year rather than April 15. A payment first makes up an earlier shortfall, then counts toward the next date. Another test taxes the income you actually earned to the end of each period. Paying 70% of that tax by the next due date also excuses that date. The department can waive the penalty if you acted in good faith. Good faith is presumed for circumstances beyond your control. It is also presumed if you declared and paid before the return was due. You apply on Form R-20128 within a year of the return's due date.").
		VersusFederal("Unlike federal, Louisiana uses 70% of this year's tax, not 90%. It has no 110% figure at higher incomes. Its threshold is $1,000 after withholding, or $2,000 on a joint return. A third test taxes last year's figures at this year's rate. Starting late means fewer, larger installments. The income-to-date test has no annualizing. The rate is a fixed 12%, and the penalty runs to May 15.").
		Source("La. R.S. 47:116, 47:117, 47:117.1, 47:118").
		Source("Act 307 of the 2026 Regular Session (HB 633)").
		Source("IT-540ESi (2026), Declaration of Estimated Tax for Individuals: General Information and Instructions").
		Source("R-210R-i (1/26), Underpayment of Individual Income Tax Penalty Computation, 2025").
		Source("Louisiana Department of Revenue, 2026 Legislative Summaries (June 26, 2026)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
