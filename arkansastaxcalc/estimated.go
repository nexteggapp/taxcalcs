package arkansastaxcalc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Arkansas's estimated tax rules under the Income Tax Act of 1987, as the
// shared [estimated.EstimatedTax] interface.
//
// The figure follows the Department of Finance and Administration's forms:
// the 2026 AR1000ES, and the 2025 AR2210, its instructions and AR2210A, which
// are the latest published. The Arkansas Code's text was not retrieved, so
// the statutory side of each divergence is the restatement of the law on page
// 1 of the AR2210 instructions, cited as such. No act changing the estimated
// tax rules for 2026 was found (research/estimated-payments/ar.md), so no form
// here predates the law it describes.
//
// Arkansas reads no fact that other jurisdictions do not, so it has no extras
// type, and facts carrying any are refused.

const jurisdiction = estimated.Arkansas

// restatedSource is the statutory source of every divergence: the Code itself was
// not retrieved, and the instructions restate it in the Code's own words.
const restatedSource = "Arkansas Income Tax Act, Ark. Code Ann. title 26, chapter 51, as restated in the AR2210 Instructions (2025), page 1; the Code's own text was not retrieved"

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is AR2210 line 4: no penalty when this year's net tax less
	// Arkansas withholding is this much or less.
	threshold money.USCents

	// thisYearPercent and priorYearPercent are AR2210 lines 2 and 5: the
	// required annual payment is the smaller of 90% of this year's net tax
	// and 100% of last year's.
	thisYearPercent, priorYearPercent money.Rate

	// shares are AR2210 lines 7 and 8: a quarter of the required annual
	// payment is due on each date, and payments are treated as made in the
	// same quarters.
	shares []money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// fileAndPayBy is AR2210 Exception 2: a return filed and paid by this
	// date takes the year out of the penalty.
	fileAndPayBy estimated.Date

	// stop is AR2210 line 15: the penalty runs no later than April 15 of the
	// next year.
	stop estimated.Date

	// rate and yearDays are AR2210 line 16: the shortfall × days ÷ 365 ×
	// 10%. The rate is fixed, not reset each period.
	rate       money.Rate
	rateSource string
	yearDays   int

	// periodEnds, annualisedPercentages and annualisedFactor are AR2210A:
	// the periods on its line 1, the percentages on line 20 and the daily
	// factor on line 43.
	periodEnds            []estimated.Date
	annualisedPercentages []money.Rate
	annualisedFactor      estimated.FactorSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Arkansas's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return arkansasEstimated{y}, nil
}

type arkansasEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement

	// exempt means no penalty can arise, so nothing is needed by any date.
	// filedEarly means it is Exception 2, the return filed and paid by
	// January 31, which excuses every installment.
	exempt, filedEarly bool

	// taxKnown is whether this year's tax was given. Without it the target is
	// last year's tax alone, and the penalty is an upper bound.
	taxKnown bool

	// annualised means the installments are AR2210A's, and the penalty is
	// charged at its printed daily factor.
	annualised bool

	dues         []estimated.Date
	installments []money.USCents // each installment's own amount
	notes        working.Notes
}

func (r arkansasEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("arkansastaxcalc: these are Arkansas's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("arkansastaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.State() != nil:
		return fmt.Errorf("arkansastaxcalc: Arkansas's rules read no extras, and the facts carry %T", f.State())
	case int64(f.FarmShare())*3 >= 20_000:
		return fmt.Errorf("%w: a farmer with two-thirds of gross income from farming pays by the fifteenth day of the second month after the year, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("arkansastaxcalc: AR2210A period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// affected says which divergences change these facts' figures or answers.
type affected struct {
	threshold, payments, priorFigure, filedEarly, lateStart, factor bool
}

// divergences are where the department's forms and the instructions'
// restatement of the law part ways on a rule every Arkansas computation uses.
// They are found in research/estimated-payments/ar.md, questions 4 and 10.
func (r arkansasEstimated) divergences(in affected) ([]working.Divergence, error) {
	return build(
		working.NewDivergence("When a penalty can arise").
			Form("Line 4 takes this year's net tax less Arkansas withholding. If that is $1,000 or less, there's no penalty. A declaration is needed only when the estimated tax is more than $1,000.",
				"AR2210 (2025), line 4; AR1000ES Instructions (2026), Who Must File a Declaration").
			Statute("A declaration is needed when the estimated tax is $1,000 or more. That applies only while withholding is under 90% of the estimated tax. The penalty is excused only below $1,000, so exactly $1,000 is inside the rules. The AR1000ES worksheet agrees.",
				restatedSource+", paragraph 1(a); AR1000ES Worksheet (2026), line 8").
			Uses(working.ReadingForm).Affects(in.threshold),
		working.NewDivergence("When estimated payments count as paid").
			Form("Line 8 takes a quarter of line 3, which holds withholding only. The instructions' example puts all payments on line 3. It counts them in four equal parts on the due dates, whenever they were made.",
				"AR2210 (2025), lines 3 and 8; AR2210 Instructions (2025), page 2, Penalty Computation Example").
			Statute("The penalty applies unless 90% of the amount due is paid by each quarterly date. A payment therefore counts from the day it's made.",
				restatedSource+", paragraph 2(b); AR1000ES Instructions (2026), Underestimate of Tax").
			Uses(working.ReadingForm).Affects(in.payments),
		working.NewDivergence("The prior-year figure").
			Form("Line 5 takes last year's net tax with no condition, so any filed return counts. That includes a short year and a return with no tax. A return with no tax makes the required payment $0.",
				"AR2210 (2025), lines 5 and 6").
			Statute("Last year's tax counts only from a filed return showing a liability. That return must be for a 12-month year.",
				restatedSource+", paragraph 1(a)").
			Uses(working.ReadingForm).Affects(in.priorFigure),
		working.NewDivergence("A return filed and paid by January 31").
			Form("Exception 2 is in Part I, and if you qualify you stop there. So a return filed and paid by January 31 removes the penalty for the whole year. Subject 603 lists it as a waiver of the penalty.",
				"AR2210 (2025), Part I; Subject 603 (rev. 12/22/2020), item 3").
			Statute("The return replaces the fourth installment, so it excuses that installment only.",
				restatedSource+", Exceptions, paragraph 2").
			Uses(working.ReadingForm).Affects(in.filedEarly),
		working.NewDivergence("A requirement first met after April 15").
			Form("Line 7 is a quarter of the required annual payment in every column. That holds whenever the requirement arose.",
				"AR2210 (2025), line 7").
			Statute(fmt.Sprintf("Meeting the requirement after April 15 means filing by the next quarterly date. The AR1000ES worksheet spreads your estimated tax over the vouchers left. That is a third from June 15, half from Sep 15, and all on Jan 15, %d.", r.y.year+1),
				restatedSource+", paragraph 1(d); AR1000ES Worksheet (2026)").
			Uses(working.ReadingForm).Affects(in.lateStart),
	)
}

// annualisedDivergence is the one divergence only AR2210A uses.
func (r arkansasEstimated) annualisedDivergence(in affected) ([]working.Divergence, error) {
	return build(working.NewDivergence("The daily penalty factor on AR2210A").
		Form("Each day is charged at a printed .00027397. That is 10% divided by 365, rounded to eight places.",
			"AR2210A (2025), line 43").
		Statute("The penalty is 10% a year. The AR2210 charges the days divided by 365, times 10%.",
			restatedSource+", paragraph 2(b); AR2210 (2025), line 16").
		Uses(working.ReadingForm).Affects(in.factor))
}

func build(bs ...*working.DivergenceBuilder) ([]working.Divergence, error) {
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

func (r arkansasEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	var in affected
	last := y.year - 1

	a.dues = estimated.MoveDates(y.calendar, y.dues)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			a.notes.Rule("AR1000ES (2026), Important Notice", fmt.Sprintf("the installment due %s falls on a weekend or legal holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
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

	// AR2210 line 2: 90% of this year's net tax.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, y.thisYearPercent.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("AR2210 (2025), line 2; AR1000ES (2026), Underestimate of Tax", fmt.Sprintf("%v of this year's Arkansas net tax of %v is %v", y.thisYearPercent, tax, thisYear.Amount()))
	}

	// AR2210 line 5: 100% of last year's net tax, from a filed return.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearPercent, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Arkansas return", last))
	case !prior.Filed():
		why := fmt.Sprintf("You didn't file a %d Arkansas return", last)
		if prior.NotRequiredToFile() {
			why = fmt.Sprintf("You weren't required to file a %d Arkansas return", last)
		}
		priorOption = unavailable(why)
	default:
		amount := y.priorYearPercent.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearPercent, amount)
		a.notes.Rule("AR2210 (2025), lines 5 and 6", fmt.Sprintf("%v of the %v net tax on the %d return is %v; Arkansas has no higher percentage at higher income", y.priorYearPercent, priorTax, last, amount))
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
	// Exception 3: no liability for a twelve-month year, as a resident all
	// of it.
	noLiability := hasPrior && prior.Months() == 12 &&
		((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	resident, answered := prior.FullYearResident()
	if noLiability && !answered {
		a.notes.Ask(working.PositionResidency, fmt.Sprintf("Did you live in Arkansas all of %d?", last),
			"If so, with no Arkansas tax liability for that year, there's no penalty.")
	}

	// Exception 2: a return filed and paid by January 31. January 31, 2027
	// is a Sunday; the instructions print January 31 and say nothing of
	// moving it, so it is not moved.
	filed := f.ReturnFiled()
	a.filedEarly = !filed.IsZero() && !filed.After(y.fileAndPayBy)
	if moved := y.calendar.NextBusinessDay(y.fileAndPayBy); filed == moved && moved != y.fileAndPayBy {
		a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("The instructions don't say if the %s deadline moves off a %v. Filing and paying on %s, the next business day, counts as late.",
			shortDate(y.fileAndPayBy, y.year), y.fileAndPayBy.Weekday(), shortDate(moved, y.year)))
	}

	switch {
	case noLiability && answered && resident:
		a.exempt = true
		exception, err := estimated.NewException("No Arkansas tax liability last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no Arkansas tax liability for %d, a full year as an Arkansas resident. No estimated payments are needed for %d, and no penalty can arise.", last, y.year)).Exempt(exception)
		a.notes.Rule("AR2210 Instructions (2025), Exception 3", fmt.Sprintf("the %d return showed no liability for a full 12 months as a resident, so there is no penalty for %d", last, y.year))
		if prior.NotRequiredToFile() {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did you have any Arkansas tax liability for %d?", last),
				"The figures assume none for now, since you weren't required to file that year.")
		}
	case a.filedEarly:
		a.exempt = true
		exception, err := estimated.NewException("Return filed and paid by the end of January").Build()
		if err != nil {
			return assessment{}, err
		}
		in.filedEarly = true
		rb.Required(false, fmt.Sprintf("You filed your %d return and paid the tax on %s, by %s. No penalty arises for the year.",
			y.year, shortDate(filed, y.year), shortDate(y.fileAndPayBy, y.year))).Exempt(exception)
		a.notes.Rule("AR2210 (2025), Part I; AR2210 Instructions (2025), Exception 2", fmt.Sprintf("the return was filed and paid on %s, by %s, so Exception 2 applies and Part II and Part III are not completed", filed.Long(), y.fileAndPayBy.Long()))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target needs your %d Arkansas tax, as your %d return can't set one.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your target is %v of your %d Arkansas net tax, %s. Your withholding of %s already reaches it. No estimated payments are needed, whatever your %d tax turns out to be.",
			y.priorYearPercent, last, target.Whole(), withheld.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d Arkansas net tax, %s. That keeps you clear of a penalty whatever your %d tax turns out to be. After %s of withholding, you need %s of estimated payments. A quarter is due by each date.",
			y.priorYearPercent, last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case tax-withheld <= y.threshold:
		a.exempt = true
		exception, err := estimated.ExceptionAtMost("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		atMost := " That is " + y.threshold.Whole() + " or less."
		if tax < withheld {
			atMost = ""
		}
		rb.Required(false, fmt.Sprintf("Arkansas net tax of %s less %s withheld %s.",
			tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))+atMost+" No estimated payments are needed, and no penalty arises.").Exempt(exception)
		a.notes.Rule("AR2210 (2025), line 4", fmt.Sprintf("%v of net tax less %v withheld is %v, %v or less, so the form stops and there is no penalty", tax, withheld, tax-withheld, y.threshold))
		if tax-withheld == y.threshold {
			in.threshold = true
			a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("AR2210 line 4 says tax less withholding of exactly %s needs no payments. The AR1000ES and AR2210 instructions differ, and the figures follow line 4.",
				y.threshold.Whole()))
		}
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Arkansas withholding of %s meets the %s required for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Arkansas net tax of %s less %s of withholding leaves %s. That is more than %s, and withholding is short of the %s required. You need %s of estimated payments, a quarter by each date.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.threshold.Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}

	// These answers change the target. An exception sets the target, so they
	// are asked only when none applies.
	if !a.exempt && !hasPrior {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Arkansas return, and what was its net tax?", last),
			fmt.Sprintf("With it, %v of last year's net tax may set a target below %v of this year's.", y.priorYearPercent, y.thisYearPercent))
	}
	if !a.exempt && !taxKnown {
		effect := "Without it, the target can only come from last year's tax."
		if !priorOpt.Available() {
			effect = fmt.Sprintf("Without it, there's no target, because your %d return can't set one.", last)
		}
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Arkansas net tax to be?", y.year), effect)
	}

	// What the form allows that the restatement of the law does not, where
	// it decides the target.
	if c, ok := a.requirement.Chosen(); ok && c.Basis() == estimated.BasisPriorYearTax {
		if prior.Months() != 12 {
			in.priorFigure = true
			a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("Line 5 of the AR2210 takes your %d return of %d months, as do the figures. The instructions count only a 12-month year.",
				last, prior.Months()))
		}
		if priorTax == 0 {
			in.priorFigure = true
			a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("The AR2210's line 6 lets your %d return, which showed no tax, set a $0 target. The figures follow it, though the instructions need a return with a liability.", last))
		}
	}

	// AR2210 line 7: a quarter on each date, whenever the requirement arose.
	cum, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cum)
	if !a.exempt {
		a.notes.Rule("AR2210 (2025), line 7; AR1000ES (2026), vouchers", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", a.requirement.Target(), a.installments[0],
			a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
		if met := f.RequirementFirstMet(); met.After(a.dues[0]) {
			in.lateStart = true
			a.notes.Rule("AR2210 (2025), line 7", fmt.Sprintf("the requirement was first met on %s, but the form still requires a quarter on every date; the divergence on a requirement first met after April 15 says what the law reads", met.Long()))
		}
	}

	// AR2210A: the annualised installments, against a quarter of 90% of this
	// year's tax rather than of the required annual payment.
	if f.Elections().Annualised() && !a.exempt {
		if !taxKnown {
			a.notes.Rule("AR2210A (2025), lines 24 to 26", "the annualised method measures each period against a quarter of 90% of this year's net tax, which was not given, so the regular method is used")
		} else {
			regularCum, err := estimated.CumulativeShares(y.thisYearPercent.Of(tax), y.shares)
			if err != nil {
				return assessment{}, err
			}
			periods := f.Periods()
			taxes := make([]money.USCents, len(periods))
			for i, p := range periods {
				taxes[i] = p.AnnualisedTax()
			}
			ann, err := estimated.Annualised(taxes, y.annualisedPercentages, estimated.Increments(regularCum))
			if err != nil {
				return assessment{}, err
			}
			a.installments, a.annualised = ann, true
			a.notes.Rule("AR2210A (2025), lines 19 to 30; AR2210 Instructions (2025), Exception 6", fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: %v, %v, %v and %v of the annualised tax less earlier installments, "+
				"and never more than a quarter of 90%% of this year's tax plus any shortfall carried forward", ann[0], ann[1], ann[2], ann[3],
				y.annualisedPercentages[0], y.annualisedPercentages[1], y.annualisedPercentages[2], y.annualisedPercentages[3]))
		}
	}

	if f.FishShare() > 0 && int64(f.FishShare()+f.FarmShare())*3 >= 20_000 {
		a.notes.Rule("AR1000ES (2026), Who Must File a Declaration, Exception", "the later dates are for income from farming only; fishing is not mentioned, so the general rules apply")
	}

	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	// Which divergences change these figures turns on the payments and the
	// shortfalls.
	if !a.exempt {
		lastDue := a.dues[len(a.dues)-1]
		for _, p := range f.Payments() {
			if p.FromPriorYear() || !p.On().After(lastDue) {
				in.payments = true
			}
		}
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return assessment{}, err
		}
		for _, l := range ledger {
			in.factor = in.factor || l.Shortfall() > 0
		}
	}
	divergences, err := r.divergences(in)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	if a.annualised {
		ds, err := r.annualisedDivergence(in)
		if err != nil {
			return assessment{}, err
		}
		a.notes.Diverges(ds...)
	}
	return a, nil
}

func (r arkansasEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies the credits to the installments.
//
// AR2210 line 8 and the instructions' example treat withholding and every
// payment made by the last due date as paid in four equal parts on the due
// dates. A payment after the last due date counts on the day it was made and
// pays the oldest shortfall first.
func (r arkansasEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	y := r.y
	last := a.dues[len(a.dues)-1]
	spread := f.WithheldTotal()
	var estimates money.USCents
	var after []estimated.Payment
	for _, p := range f.Payments() {
		if p.FromPriorYear() || !p.On().After(last) {
			spread += p.Amount()
			estimates += p.Amount()
		} else {
			after = append(after, p)
		}
	}
	cum, err := estimated.CumulativeShares(spread, y.shares)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var credits []estimated.Payment
	for i, amount := range estimated.Increments(cum) {
		if amount == 0 {
			continue
		}
		p, err := estimated.NewPayment().On(a.dues[i]).Amount(amount).Build()
		if err != nil {
			return nil, working.Notes{}, err
		}
		credits = append(credits, p)
	}
	credits = append(credits, after...)
	ledger, err := estimated.Apply(a.dues, a.installments, credits)
	if err != nil {
		return nil, working.Notes{}, err
	}

	var notes working.Notes
	notes.Rule("AR2210 (2025), line 8; AR2210 Instructions (2025), Penalty Computation Example",
		fmt.Sprintf("the %v withheld and %v of estimated payments made by %s are treated as paid in four equal parts on the due dates, whenever they were made; "+
			"a payment after that counts on its day and first pays the oldest underpayment", f.WithheldTotal(), estimates, last.Long()))
	if estimates > 0 {
		notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("AR2210 line 8, its example and the law differ on when estimates count as paid. As in the example, the figures spread your %s evenly over the four dates.",
			estimates.Whole()))
	}
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("AR2210 (2025), line 8", "Arkansas has no election to credit withholding on the dates it was withheld, so it is spread evenly")
	}
	return ledger, notes, nil
}

func (r arkansasEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Arkansas return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
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

// installments are the schedule the ledger gives.
func (r arkansasEstimated) installments(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
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

func (r arkansasEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("arkansastaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Arkansas return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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

	charge := func(amount money.USCents, from, to estimated.Date) ([]estimated.PenaltyPart, error) {
		if a.annualised {
			parts, gap, err := estimated.DailyFactor(amount, from, to, y.annualisedFactor)
			if err == nil && !gap.IsZero() {
				err = fmt.Errorf("arkansastaxcalc: AR2210A's factor does not cover %v", gap)
			}
			return parts, err
		}
		p, err := dailyToDollar(amount, from, to, y.rate, y.yearDays, y.rateSource)
		return []estimated.PenaltyPart{p}, err
	}
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
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
			parts, err := charge(piece.Amount(), l.Due(), to)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
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
	// AR2210 Instructions, Exceptions 4 and 5: the federal pair, decided by
	// the Commissioner.
	if o, ok := estimated.StandardWaiver(f, "the Commissioner", 62); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}

	if !a.taxKnown {
		notes.Rule("AR2210 (2025), line 6", fmt.Sprintf("this year's tax was not given, so the penalty is figured against %v of the %d net tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			y.priorYearPercent, y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("AR2210 (2025), line 15", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	if a.annualised {
		notes.Rule("AR2210A (2025), lines 40 to 46", fmt.Sprintf("each shortfall is charged %s for each day from its due date until paid or %s, rounded to the whole dollar as the AR2210 example rounds; the penalty to %s is %v",
			y.annualisedFactor.Periods()[0].Factor(), y.stop.Long(), stop.Long(), p.Total()))
	} else {
		notes.Rule("AR2210 (2025), lines 15 to 17; AR2210 Instructions (2025), Penalty Computation Example", fmt.Sprintf("each shortfall is charged the days from its due date until paid or %s, divided by %d, times %v, "+
			"rounded to the whole dollar as the example rounds each charge; the penalty to %s is %v", y.stop.Long(), y.yearDays, y.rate, stop.Long(), p.Total()))
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking the Commissioner to excuse the penalty?",
			"The Commissioner can excuse it for a casualty, disaster or other unusual event. Retiring after 62 or becoming disabled, with reasonable cause, also counts.")
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// dailyToDollar is AR2210 line 16 as the instructions' example works it: the
// amount × days ÷ yearDays × rate, rounded to the whole dollar, half up. The
// example charges each piece of a shortfall on its own, so a quarter paid in
// two pieces is two charges, each rounded.
//
// estimated.SimpleDaily rounds to the cent and divides by 366 in a leap year,
// and neither is what this form does.
func dailyToDollar(amount money.USCents, from, to estimated.Date, rate money.Rate, yearDays int, source string) (estimated.PenaltyPart, error) {
	days := to.DaysSince(from)
	n := new(big.Int).Mul(big.NewInt(int64(amount)), big.NewInt(int64(days)))
	n.Mul(n, big.NewInt(int64(rate)))
	charge := estimated.RoundToDollar.Div(n, big.NewInt(int64(10_000)*int64(yearDays)))
	return estimated.NewPenaltyPart(estimated.PartInterest, amount, rate).
		Span(from, to).Days(days).Charge(charge).Source(source).Build()
}

// shortDate is "Aug 31" inside the tax year and "Jan 15, 2027" outside it.
func shortDate(d estimated.Date, year int) string {
	m := d.Month().String()[:3]
	if d.Year() == year {
		return fmt.Sprintf("%s %d", m, d.Day())
	}
	return fmt.Sprintf("%s %d, %d", m, d.Day(), d.Year())
}

func (r arkansasEstimated) Explanation() estimated.Explanation { return arkansasExplanation }

// arkansasExplanation is written by hand and checked against
// research/estimated-payments/ar.md.
var arkansasExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Arkansas expects estimated payments if your tax after withholding tops $1,000. The penalty is a flat 10% a year on each quarter's shortfall while it's unpaid.").
		Paragraph("You avoid the penalty if withholding and estimated payments reach a target. It is the smaller of 90% of this year's Arkansas tax and 100% of last year's. There is no 110% tier, so last year's tax works at any income. There's no penalty if tax less Arkansas withholding is $1,000 or less this year. There is also none if you had no Arkansas tax liability last year. That year must be a full year in which you lived in Arkansas throughout.").
		Paragraph("A quarter is due April 15, June 15 and September 15, and January 15 next year. The department's penalty form spreads withholding evenly across the four dates. It does the same with estimated payments made by January 15. Both count that way whenever they were actually made. So paying late in the year still counts toward the earlier dates. Filing your return and paying the tax by January 31 removes the year's penalty.").
		Paragraph("The penalty works like interest at a fixed 10% a year. Each shortfall is charged for each day from its due date until it's paid. The charge stops at April 15 of the next year if that comes first. A payment goes to the oldest shortfall first. If your income came unevenly, Form AR2210A can lower the early installments. The Commissioner can excuse the penalty for a casualty or disaster. The Commissioner can also excuse it if you retired after 62 or became disabled. That needs reasonable cause.").
		Paragraph("The department's own documents disagree on a few details. One is whether exactly $1,000 is inside the rules. Another is whether an estimated payment counts from the day it's made.").
		VersusFederal("Arkansas differs from federal in five ways that can change a number. The penalty is a fixed 10% a year rather than the quarterly federal rate. There's no 110% tier. Estimated payments count as made evenly through the year. Filing and paying by January 31 removes the penalty for the whole year. The no-liability exception needs Arkansas residence all of last year.").
		Source("AR1000ES, Estimated Tax Declaration Vouchers and Instructions for Tax Year 2026").
		Source("AR2210, Penalty for Underpayment of Estimated Tax, and its instructions, 2025").
		Source("AR2210A, Annualized Penalty for Underpayment of Estimated Income Tax, 2025").
		Source("DFA Subject 603, Penalty for Underpayment of Estimated Tax (rev. 12/22/2020)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
