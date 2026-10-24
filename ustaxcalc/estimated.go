package ustaxcalc

import (
	"fmt"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// The federal estimated tax rules, 26 U.S.C. § 6654, as the shared
// [estimated.EstimatedTax] interface. The required annual payment is
// [RequiredAnnualPayment], the installments are [Instalments] and the
// withholding credit is [WithholdingCreditedBy]: this file adds the dates moved
// for weekends and holidays, the exceptions, the penalty, and the explanation.
//
// A year's figures are in estimated_y2026.go and its successors. A year with
// no file is [estimated.ErrNoRules].

// estimatedYear is one tax year's published figures for § 6654.
type estimatedYear struct {
	year int

	// threshold is § 6654(e)(1): no penalty when tax less withholding is under
	// it.
	threshold money.USCents

	// calendar is the legal holidays that move a due date under § 7503.
	calendar estimated.Calendar

	// fileAndPayBy is § 6654(h): a return filed and paid by this date, before
	// moving for a weekend, excuses the January installment.
	fileAndPayBy estimated.Date

	// stop is § 6654(b)(2)(B): the penalty runs no later than April 15th of
	// the next year.
	stop estimated.Date

	// periodEnds are the Schedule AI annualisation period ends.
	periodEnds []estimated.Date

	// rates is the § 6621(a)(2) underpayment rate, quarter by quarter, as far
	// as it has been published.
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// installmentShares is § 6654(d)(1)(A): each installment is 25% of the
// required annual payment, and § 6654(g)(1) deems withholding paid in the same
// equal parts.
var installmentShares = []money.Rate{Percent(25), Percent(25), Percent(25), Percent(25)}

// annualisedPercentages are § 6654(d)(2)(C)(ii), Schedule AI line 20.
var annualisedPercentages = []money.Rate{2250, 4500, 6750, 9000}

// EstimatedTax returns the federal estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, estimated.Federal, year)
	}
	return federalEstimated{y}, nil
}

type federalEstimated struct{ y estimatedYear }

// assessment is what all three questions share: the requirement, the dates
// and amounts by installment, and the working behind them.
type assessment struct {
	requirement estimated.Requirement

	// exempt means no penalty can arise, so nothing is needed by any date.
	exempt bool

	// taxKnown is whether this year's tax was given. Without it the target is
	// last year's option alone, and the penalty is an upper bound.
	taxKnown bool

	dues         []estimated.Date
	installments []money.USCents // each installment's own amount, before excusing
	excused      bool            // the January installment, for filing early

	notes working.Notes
}

func (r federalEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != estimated.Federal:
		return fmt.Errorf("ustaxcalc: these are the federal rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("ustaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: § 6654(i) puts farmers and fishermen under rules of their own",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("ustaxcalc: Schedule AI period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

func (r federalEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	var targetQs targetQuestions
	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	last := y.year - 1

	// The two ways of measuring the required annual payment. The prior-year
	// test goes through RequiredAnnualPayment, which carries the § 6654(d)
	// arithmetic and its working.
	usable := hasPrior && prior.Filed() && prior.Months() == 12
	var priorTax, priorAGI money.USCents
	agiKnown := false
	if usable {
		pf, _ := prior.Figures()
		priorTax, _ = pf.Tax()
		priorAGI, agiKnown = pf.FederalAGI()
	}
	harbour, err := RequiredAnnualPayment(y.year, f.FilingStatus(), tax, priorTax, priorAGI, usable && agiKnown)
	if err != nil {
		return assessment{}, err
	}
	// RequiredAnnualPayment's working is the current-year rule, then either
	// the prior-year rule and the lesser of the two, or that there is no
	// prior-year test. Only what applies to these facts is kept: without
	// last year's AGI the prior-year test is figured below with its
	// assumption stated, and without this year's tax neither the current-year
	// rule nor the lesser of the two applies.
	harbourRules := harbour.Applied()
	switch {
	case usable && !agiKnown && taxKnown:
		a.notes.Applied(harbourRules[0])
	case usable && !agiKnown:
	case !taxKnown && usable:
		a.notes.Applied(harbourRules[1])
	case !taxKnown:
		a.notes.Applied(harbourRules[1:]...)
	default:
		a.notes.Applied(harbourRules...)
	}

	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, Percent(90), harbour.Value().CurrentYearTest)
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, Percent(90), 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	priorOption := estimated.NewTargetOption(estimated.BasisPriorYearTax, harbour.Value().PriorYearRate, harbour.Value().PriorYearTest)
	switch {
	case !hasPrior:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, Percent(100), 0).
			Unavailable(fmt.Sprintf("Needs your %d return", last))
		targetQs.Ask(working.PositionPriorYearReturn, fmt.Sprintf("What was the total tax on your %d return?", last),
			"With it, last year's tax could set your target instead of this year's.")
	case !prior.Filed():
		why := fmt.Sprintf("You didn't file a %d return", last)
		if prior.NotRequiredToFile() {
			why = fmt.Sprintf("You didn't need to file a %d return, so it has no tax to measure against", last)
		}
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, Percent(100), 0).Unavailable(why)
	case prior.Months() != 12:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, Percent(100), 0).
			Unavailable(fmt.Sprintf("Your %d return covered %d months, and this test needs all 12", last, prior.Months()))
	case !agiKnown:
		// § 6654(d)(1)(C) needs last year's AGI. Without it, assume the higher
		// percentage: the target can only be too high, never too low.
		table, err := TableFor(y.year, f.FilingStatus())
		if err != nil {
			return assessment{}, err
		}
		amount := Percent(110).Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, Percent(110), amount)
		a.notes.Rule("26 U.S.C. § 6654(d)(1)(C)(i)", fmt.Sprintf("110%% of the %v tax on the %d return is %v; 110%% rather than 100%% because the %d AGI was not given", priorTax, last, amount, last))
		targetQs.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d adjusted gross income?", last),
			fmt.Sprintf("Until then, the target is 110%% of last year's tax. If it was %s or less, the target would be 100%%.", table.PriorYearAGIStep.Whole()))
	}
	priorOpt, err := priorOption.Build()
	if err != nil {
		return assessment{}, err
	}

	rb := estimated.NewRequirement().Option(thisYear).Option(priorOpt)
	chosen := estimated.BasisThisYearTax
	if priorOpt.Available() && (!taxKnown || priorOpt.Amount() < thisYear.Amount()) {
		chosen = estimated.BasisPriorYearTax
	}
	target := thisYear.Amount()
	if chosen == estimated.BasisPriorYearTax {
		target = priorOpt.Amount()
	}
	if !taxKnown {
		targetQs.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d tax to be?", y.year),
			estimated.WithoutThisYearTax(priorOpt.Available()))
	}

	// § 6654(e)(2): no liability last year means no penalty this year.
	noLiability := hasPrior && prior.Months() == 12 &&
		((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	resident, answered := prior.FullYearResident()
	if noLiability && !answered {
		a.notes.Ask(working.PositionResidency, fmt.Sprintf("Were you a U.S. citizen or resident for all of %d?", last),
			fmt.Sprintf("If you were, having no tax liability for %d means no penalty for %d.", last, y.year))
	}

	switch {
	case noLiability && answered && resident:
		a.exempt = true
		none, err := estimated.NewException("No federal tax liability last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You were a U.S. citizen or resident all of %d, with no federal tax liability. So no estimated payments are needed for %d.", last, y.year)).Exempt(none)
		a.notes.Rule("26 U.S.C. § 6654(e)(2)", fmt.Sprintf("the %d return showed no liability for a full 12 months, so there is no penalty for %d", last, y.year))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target is 90%% of your %d tax, or 100%% of a full-year %d return's tax. Without either figure, there is no target to pay toward yet.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your %s of withholding already reaches %v of your %d tax, %s. So no estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. After %s of withholding, that's %s of estimated payments. Paying it keeps you clear of a penalty whatever your %d tax turns out to be.",
			priorOpt.Percent(), last, target.Whole(), withheld.Whole(), (target-withheld).Whole(), y.year)).Choose(chosen)
	case tax-withheld < y.threshold:
		// § 6654(e)(1) tests tax less withholding only; estimated payments do
		// not count toward it.
		a.exempt = true
		under, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your tax is %s, and %s is withheld. That %s. So no estimated payments are needed.",
			tax.Whole(), withheld.Whole(), leftUnder(false, tax-withheld, y.threshold))
		switch {
		case withheld == 0:
			why = fmt.Sprintf("You have no withholding, and your tax of %s is under %s. So no estimated payments are needed.", tax.Whole(), y.threshold.Whole())
		case withheld >= tax:
			why = fmt.Sprintf("Your %s withheld covers your %s tax. So no estimated payments are needed.", withheld.Whole(), tax.Whole())
		}
		rb.Required(false, why).Exempt(under)
		a.notes.Rule("26 U.S.C. § 6654(e)(1)", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no penalty", tax, withheld, tax-withheld, y.threshold))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Withholding of %s already reaches your %s target. So no estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	case withheld == 0:
		rb.Required(true, fmt.Sprintf("You have no withholding, and your %s tax is %s or more. So you need estimated payments that reach your %s target across the year.",
			tax.Whole(), y.threshold.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your tax is %s, and %s is withheld. That leaves %s, which is %s or more. Withholding is %s short of your %s target. So you need %s of estimated payments across the year.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.threshold.Whole(), (target-withheld).Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	a.requirement, err = rb.Build()
	if err != nil {
		return assessment{}, err
	}
	targetQs.askUnlessExempt(&a.notes, a.exempt)

	// The due dates. § 6654(c)(2) fixes them and § 7503 moves one that falls
	// on a weekend or legal holiday.
	instalments := Instalments(y.year, a.requirement.Target())
	statutory := make([]estimated.Date, len(instalments))
	cumulative := make([]money.USCents, len(instalments))
	for i, in := range instalments {
		statutory[i] = estimated.DateOf(in.Due)
		cumulative[i] = in.CumulativeTarget
	}
	a.dues = estimated.MoveDates(y.calendar, statutory)
	for i := range a.dues {
		if a.dues[i] != statutory[i] {
			a.notes.Rule("26 U.S.C. § 7503", fmt.Sprintf("the installment due %s falls on a weekend or legal holiday, so it is due %s", statutory[i].Long(), a.dues[i].Long()))
		}
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("26 U.S.C. § 6654(c)(2), (d)(1)(A)", fmt.Sprintf("installments of 25%% of %v are due %s, %s, %s and %s", a.requirement.Target(),
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() && !a.exempt {
		periods := f.Periods()
		taxes := make([]money.USCents, len(periods))
		for i, p := range periods {
			taxes[i] = p.AnnualisedTax()
		}
		ann, err := estimated.Annualised(taxes, annualisedPercentages, a.installments)
		if err != nil {
			return assessment{}, err
		}
		a.installments = ann
		a.notes.Rule("26 U.S.C. § 6654(d)(2); Form 2210 Schedule AI", fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: 22.5%%, 45%%, 67.5%% and 90%% of the annualised tax, "+
			"less earlier installments, and never more than the regular installment plus any shortfall the method carried forward", ann[0], ann[1], ann[2], ann[3]))
	}

	// § 6654(h): filing and paying by January 31st excuses the last
	// installment. January 31, 2027 is a Sunday, so § 7503 moves it to
	// February 1st, as the 2026 Form 1040-ES prints it.
	fileBy := y.calendar.NextBusinessDay(y.fileAndPayBy)
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(fileBy) && !a.exempt {
		a.excused = true
		a.notes.Rule("26 U.S.C. § 6654(h)", fmt.Sprintf("the return was filed and paid on %s, by %s, so the January installment is excused", filed.Long(), fileBy.Long()))
	}

	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
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

func (r federalEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies the credits to the installments, with the January
// installment set to nothing when filing early excused it.
func (r federalEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, installmentShares)
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
		notes.Rule("26 U.S.C. § 6654(g)(1)", fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("26 U.S.C. § 6654(g)(1)", fmt.Sprintf("the %v withheld is treated as paid in four equal parts on the due dates, %v by the first, whenever it was actually withheld",
			f.WithheldTotal(), WithholdingCreditedBy(f.WithheldTotal(), 1)))
	}
	return ledger, notes, nil
}

func (r federalEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
// installment excused when filing early excused it.
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

func (r federalEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("ustaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("26 U.S.C. § 6654(d)(1)(B)", fmt.Sprintf("this year's tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("26 U.S.C. § 6654(b)(2)", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("26 U.S.C. § 6654(a), (b)(2); 26 U.S.C. § 6621(a)(2)", fmt.Sprintf("each shortfall is charged simple interest at the underpayment rate for each quarter, by the day, from its due date until paid or %s, "+
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
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking the IRS to waive the penalty?",
			"A waiver for a casualty, disaster or other unusual event leaves no penalty due. So does one for retiring after 62 or disability, with reasonable cause.")
	}
	if o, ok := estimated.StandardWaiver(f, "the IRS", 62); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r federalEstimated) Explanation() estimated.Explanation {
	return federalExplanation
}

// federalExplanation is written by hand. It describes § 6654 as it stands for
// the years this file carries; a change in the law changes it here.
var federalExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"The IRS expects tax to be paid as income is earned. If withholding does not cover enough of it, you make four estimated payments. Paying too little or too late costs interest on the shortfall.").
		Paragraph("To avoid the penalty, withholding and estimated payments must reach a target. The target is 90% of this year's tax or, if lower, 100% of last year's tax. That 100% becomes 110% if last year's adjusted gross income was over $150,000. For married filing separately, the limit is $75,000. Most people with uneven or rising income use last year's figure. They do so because it is fixed before the year starts. Meeting it protects you however this year turns out. It is available only if last year's return covered a full twelve months.").
		Paragraph("There is no penalty if this year's tax, less withholding, is under $1,000. There is also no penalty if you had no tax liability last year. That applies only if you were a U.S. citizen or resident for all of last year.").
		Paragraph("Each payment is a quarter of the yearly figure. They are due April 15, June 15, September 15 and January 15 of the next year. A date on a weekend or holiday shifts to the next business day. Withholding counts as paid evenly across those dates, whenever it was taken. So raising withholding late in the year still counts toward the earlier dates. You can skip the January payment by filing and paying in full by January 31.").
		Paragraph("The penalty is interest, not a fine. Each shortfall costs the IRS underpayment rate for each day it stays unpaid. The rate is reset every quarter. The charge stops on April 15 of the next year. A payment goes first to the oldest shortfall, whichever date you meant it for. Schedule AI's annualised method can lower early installments for uneven income. The IRS can waive the penalty for a casualty or disaster. It can also waive it for retiring after 62 or disability, with reasonable cause.").
		Source("26 U.S.C. § 6654").
		Source("2026 Form 1040-ES, Estimated Tax for Individuals").
		Source("2025 Form 2210 and its instructions").
		Source("Rev. Rul. 2026-5, 2026-10 and 2026-15 (underpayment rates)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()

// date is a shorthand for the year files.
func date(y int, m time.Month, d int) estimated.Date { return estimated.MustDate(y, m, d) }

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
