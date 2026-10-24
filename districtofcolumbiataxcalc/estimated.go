package districtofcolumbiataxcalc

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// The District of Columbia's estimated tax rules, D.C. Code § 47-4203 and
// § 47-1812.08(i), as the shared [estimated.EstimatedTax] interface. The
// interest rate is D.C. Code § 47-4201(d).
//
// The District's section is written on the federal pattern, but it is the
// District's own text, not an adoption of 26 U.S.C. § 6654, and nothing here
// borrows from the federal package. The prior-year test is always 110%; the
// threshold is $100; the charge is interest at a fixed 10%, which D-2210
// turns into one printed factor for each span between due dates.
//
// The figure follows the Office of Tax and Revenue's forms, D-40ES for 2026
// and D-2210 for 2025, and the result's Divergences say where the D.C. Code
// reads otherwise. Sections 47-4201, 47-4203 and 47-1812.08(i) were not
// changed in substance in 2024, 2025 or 2026; D.C. Law 26-89 (effective
// February 12, 2026) only moved the power to make regulations under
// § 47-1812.08(i) to the Chief Financial Officer. So no form here predates
// the law it describes.

// jurisdiction is the District's postal code.
const jurisdiction = estimated.DistrictOfColumbia

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is $100: D-40ES asks for payments when the tax after
	// withholding and credits is that or more, and D-2210 charges no
	// interest when the tax less withholding and payments is less.
	threshold money.USCents

	// thisYearPercent and priorPercent are D-2210 lines 2 and 4.
	thisYearPercent, priorPercent money.Rate

	// remaining is D-2210's exception for a balance under this share of the
	// year's tax.
	remaining money.Rate

	dues []estimated.Date

	// laterGroups are the first days of the groups in § 47-1812.08(i)(4)(A)
	// to (C): a requirement first met on or after laterGroups[i] makes the
	// first declaration due on dues[i+1].
	laterGroups []estimated.Date

	calendar estimated.Calendar

	// stop is § 47-4203(c)(2)(B): the period of an underpayment ends no later
	// than the 15th day of the 4th month after the year, which is also the
	// end of D-2210's fourth span.
	stop estimated.Date

	// factors are D-2210 line 10, one for each span from a due date to the
	// next, and from the last to stop.
	factors []estimated.Factor

	// rate is the § 47-4201(d)(2) rate the factors stand for, for the
	// working.
	rate money.Rate

	// periodEnds end the 3, 5 and 8 months of § 47-4203(b)(2), and the year.
	periodEnds []estimated.Date
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are D-2210 line 6 and § 47-4203(b)(1)(B): each installment is 25%
// of the required annual payment. § 47-4203(d)(3) deems withholding paid in
// the same equal parts.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns the District of Columbia's estimated tax rules for a
// tax year, or [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return districtEstimated{y}, nil
}

type districtEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	tax         money.USCents
	dues        []estimated.Date

	// installments are each installment's own amount: a quarter of the
	// target, or the form's proration by income under the annualised method.
	installments []money.USCents

	// excusedJanuary is a return filed with the balance paid by the fourth
	// due date, which takes the place of the fourth payment.
	excusedJanuary bool

	notes working.Notes
}

func (r districtEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("districtofcolumbiataxcalc: these are the District of Columbia's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("districtofcolumbiataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.State() != nil:
		return fmt.Errorf("districtofcolumbiataxcalc: the District's rules read no extras, and the facts carry %T", f.State())
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("districtofcolumbiataxcalc: annualisation period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the Office of Tax and Revenue's forms and the D.C.
// Code part ways on a rule every District computation uses. They are found
// in research/estimated-payments/dc.md and checked against the Code's text in
// testdata/. The figures follow the forms, except on the 110% item in the
// exception list, where they follow the form's lines 6 to 11, which measure
// each installment as the statute does.
func (r districtEstimated) divergences(af affects) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Who must make estimated payments").
			Form("You need payments if you expect $100 or more due after withholding and credits. The penalty form's own instructions say more than $100.",
				"D-40ES (REV. 03/2026), Who must file a Form D-40ES?; 2025 D-40 booklet").
			Statute("The test is the tax on income not subject to withholding. A declaration is needed only when it will be more than $100. Exactly $100 therefore doesn't need one.",
				"D.C. Code § 47-1812.08(i)(1)").
			Uses(working.ReadingForm).Affects(af.who),
		working.NewDivergence("Tax under $100").
			Form("Take the year's tax less withholding and estimated payments. Under $100 means no interest, with no condition that the payments were on time. The front of the form counts only withholding and credits.",
				"D-2210 (05/2025), front, item A, and instructions, Are there any exceptions").
			Statute("Take the tax less credits and estimated payments that were timely made. No interest is due when that is under $100.",
				"D.C. Code § 47-4203(e)(1)").
			Uses(working.ReadingForm).Affects(af.under100),
		working.NewDivergence("The prior-year test").
			Form("110% of last year's tax is open only if you filed a DC return. You must also have lived in DC all of last year.",
				"D-2210 (05/2025), front, item B, and instructions for lines 4 and 5; D-40ES (REV. 03/2026)").
			Statute("The test is 110% of the tax on last year's return. It needs a return consisting of 12 months, with no condition of residence.",
				"D.C. Code § 47-4203(b)(1)(B)(ii)(II)").
			Uses(working.ReadingForm).Affects(af.priorTest),
		working.NewDivergence("No liability last year").
			Form("No interest if you had no DC tax liability last year. You must also have lived in DC the entire 12 months of it.",
				"D-2210 (05/2025), instructions, Are there any exceptions").
			Statute("No interest if last year had no tax liability, with no condition of residence.",
				"D.C. Code § 47-4203(e)(2)").
			Uses(working.ReadingForm).Affects(af.noLiability),
		working.NewDivergence("A balance under 10% of the year's tax").
			Form("Take the tax left after credits, estimated payments and withholding. There's no interest if it is less than 10% of the year's DC tax, whenever paid.",
				"D-2210 (05/2025), instructions, Are there any exceptions").
			Statute("There's no such exception. Interest is excused only in the five cases the law lists.",
				"D.C. Code § 47-4203(e)").
			Uses(working.ReadingForm).Affects(af.remaining),
		working.NewDivergence("The 110% item in the exception list").
			Form("The form's list of exceptions has an item for 110% of last year's tax. It excuses interest once estimated payments and withholding reach that amount. The item is listed without saying by when.",
				"D-2210 (05/2025), front, item B, instructions, Are there any exceptions, and lines 6 to 11").
			Statute("Each installment is 25% of the lesser of two figures. They are 90% of this year's tax and 110% of last year's. An installment is short by whatever wasn't paid by its due date. The form's lines for each period measure it the same way.",
				"D.C. Code § 47-4203(b)(1)(B), (c)(2)(A)").
			Uses(working.ReadingStatute).Affects(af.list110),
		working.NewDivergence("The annualised income method").
			Form("Each period's share of the required payment is a proration. It is the income to that period over the year's income.",
				"D-2210 (05/2025), instructions for line 7").
			Statute("The law uses 22.5%, 45%, 67.5% and 90% of the tax on annualised income. Income is annualised for the first 3, 5 and 8 months and the year. A part-year return can't use it except under regulations.",
				"D.C. Code § 47-4203(b)(2)").
			Uses(working.ReadingForm).Affects(af.annualised),
		working.NewDivergence("The interest factors").
			Form("The form prints a factor for the whole span to the next due date. Each period's running underpayment on its due date is multiplied by it. The factors are .0175, .0265, .0351 and .0259. A payment during a span counts only from the next due date.",
				"D-2210 (05/2025), lines 8 to 11").
			Statute("Interest is 10% a year compounded daily, on each installment's underpayment. It runs from the due date until paid or Apr 15 of the next year. A payment during a month is treated as made at the end of that month.",
				"D.C. Code §§ 47-4201(d)(2), 47-4203(c)").
			Uses(working.ReadingForm).Affects(af.factors),
		working.NewDivergence("A requirement first met after Apr 1").
			Form("A quarter of the year's amount is due on each of the four dates. That holds whenever the requirement arose.",
				"D-2210 (05/2025), lines 6 and 7").
			Statute("The installments fall on the declaration dates. If first met after Apr 1, Jun 1 or Sep 1, those are Jun 15, Sep 15 or Jan 15. The law doesn't say how the installments are then sized.",
				"D.C. Code §§ 47-4203(a), 47-1812.08(i)(4)").
			Uses(working.ReadingForm).Affects(af.lateStart),
		working.NewDivergence("Retirement, disability, casualty and death").
			Form("The form lists no waiver for retirement, disability or a casualty. Nor does it list one for the taxpayer's death.",
				"D-2210 (05/2025), instructions, Are there any exceptions").
			Statute("The Mayor can find you retired after 62 or became disabled this year or last. With reasonable cause, there is then no interest. None is due if a casualty, disaster or other unusual event makes it inequitable. There is none for the year of death.",
				"D.C. Code § 47-4203(e)(3) to (5)").
			Uses(working.ReadingForm).Affects(af.waiver),
		working.NewDivergence("Due dates on a weekend or holiday").
			Form("A voucher due on a weekend or legal holiday is due the next business day. The form doesn't say which holidays count. No 2026 due date moves.",
				"D-40ES (REV. 03/2026), When are your vouchers due?").
			Statute("The declaration is due by Apr 15, Jun 15, Sep 15 or Jan 15 of the next year. There is no rule for a weekend or holiday.",
				"D.C. Code § 47-1812.08(i)(4)").
			Uses(working.ReadingForm).Affects(af.holiday),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// affects is, for each divergence, whether the other reading would give
// these facts a different answer.
type affects struct {
	who, under100, priorTest, noLiability, remaining, list110 bool
	annualised, factors, lateStart, waiver, holiday           bool
}

func (r districtEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment

	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown, a.tax = taxKnown, tax
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	last := y.year - 1
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
	}
	resident, residenceAnswered := prior.FullYearResident()

	if f.FarmerOrFisher() {
		a.notes.Rule("D.C. Code § 47-4203; D-2210 (05/2025)", "neither has a separate rule for farmers or fishermen, so the general rules apply")
	}

	// 90% of this year's tax. D-2210 lines 1 and 2.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, y.thisYearPercent.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("D-2210 (05/2025), lines 1 and 2; D.C. Code § 47-4203(b)(1)(B)(i)", fmt.Sprintf("%v of this year's DC tax of %v is %v", y.thisYearPercent, tax, thisYear.Amount()))
	}

	// 110% of last year's tax, for a filer who filed and lived in DC all
	// year. D-2210 lines 3 to 5.
	var residenceQuestions []string
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorPercent, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d DC return", last))
	case prior.NotRequiredToFile(), !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d DC return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d return covered %d months, not 12", last, prior.Months()))
	case !residenceAnswered:
		priorOption = unavailable(fmt.Sprintf("Needs whether you lived in DC all of %d", last))
		residenceQuestions = append(residenceQuestions, fmt.Sprintf("the %v option is open", y.priorPercent))
	case !resident:
		priorOption = unavailable(fmt.Sprintf("You didn't live in DC all of %d", last))
		a.notes.Rule("D.C. Code § 47-4203(b)(1)(B)(ii)(II)", fmt.Sprintf("the statute asks only for a filed 12-month %d return, which would allow %v of the %v tax on it, %v; D-2210 requires a full year in DC, so the figures here are the form's",
			last, y.priorPercent, priorTax, y.priorPercent.Of(priorTax)))
	default:
		amount := y.priorPercent.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorPercent, amount)
		a.notes.Rule("D-2210 (05/2025), lines 3 and 4; D.C. Code § 47-4203(b)(1)(B)(ii)(II)", fmt.Sprintf("%v of the %v tax on the %d DC return is %v; there is no 100%% tier and no income test", y.priorPercent, priorTax, last, amount))
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
	if taxKnown && priorOpt.Available() {
		a.notes.Rule("D-2210 (05/2025), line 5", fmt.Sprintf("the required annual payment is the lesser of %v and %v, %v", thisYear.Amount(), priorOpt.Amount(), target))
	}
	// No liability last year, for a filer who lived in DC all of it.
	noLiability := hasPrior && prior.Months() == 12 && ((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	if noLiability && !residenceAnswered {
		residenceQuestions = append(residenceQuestions, "having no DC tax that year excuses you")
	}
	if noLiability && residenceAnswered && !resident {
		a.notes.Rule("D.C. Code § 47-4203(e)(2)", fmt.Sprintf("you had no DC liability for %d, and the statute excuses the interest for %d on that alone; D-2210 also requires that you lived in DC all of %d, so the figures here are the form's, and the exception is yours to claim",
			last, y.year, last))
	}
	if len(residenceQuestions) > 0 {
		a.notes.Ask(working.PositionResidency, fmt.Sprintf("Did you live in DC all of %d?", last),
			"Your answer decides whether "+strings.Join(residenceQuestions, ". It also decides whether ")+".")
	}

	remainingExempt := false
	switch {
	case noLiability && residenceAnswered && resident:
		a.exempt = true
		none, err := estimated.NewException("No DC tax last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no DC tax for %d and lived in DC all of it. No estimated payments are needed for %d.", last, y.year)).Exempt(none)
		a.notes.Rule("D-2210 (05/2025), instructions, Are there any exceptions; D.C. Code § 47-4203(e)(2)", fmt.Sprintf("no DC liability for %d, as a resident all 12 months, so there is no interest for %d", last, y.year))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("No %d DC return sets a target. There is none until your %d DC tax is figured.", last, y.year)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s covers %v of your %d DC tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), y.priorPercent, last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d DC tax, %s. It keeps you clear of interest whatever your %d tax turns out to be. After %s of withholding, that's %s of estimated payments. Pay a quarter by each date.",
			y.priorPercent, last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case tax-withheld < y.threshold:
		a.exempt = true
		below, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		under := ", under " + y.threshold.Whole()
		if tax < withheld {
			under = ""
		}
		rb.Required(false, fmt.Sprintf("Your DC tax is %s, and %s is withheld. That %s%s. No estimated payments are needed.",
			tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld), under)).Exempt(below)
		a.notes.Rule("D-40ES (REV. 03/2026); D-2210 (05/2025), front, item A", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no interest", tax, withheld, tax-withheld, y.threshold))
	case r.underRemaining(tax, tax-withheld):
		a.exempt, remainingExempt = true, true
		below, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, r.remainingLimit(tax))
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your withholding of %s leaves %s of your %s of DC tax to pay. That is less than %v of it, so no interest is charged. No estimated payments are needed.",
			withheld.Whole(), (tax-withheld).Whole(), tax.Whole(), y.remaining)).Exempt(below)
		a.notes.Rule("D-2210 (05/2025), instructions, Are there any exceptions", fmt.Sprintf("%v of tax less %v withheld leaves %v, less than %v of the tax, so there is no interest", tax, withheld, tax-withheld, y.remaining))
		r.remainingOpen(&a.notes)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your DC withholding of %s covers the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your DC tax is %s, and %s is withheld. That leaves %s, which is %s or more. You need %s of estimated payments to reach your %s target. Pay a quarter by each date.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.threshold.Whole(), (target-withheld).Whole(), target.Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// These answers change the target. An exception sets the target, so they
	// are asked only when none applies.
	if !a.exempt && !hasPrior {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d DC return, and what was its tax?", last),
			fmt.Sprintf("With it, %v of last year's tax may be a lower target than %v of this year's.", y.priorPercent, y.thisYearPercent))
	}
	if !a.exempt && !taxKnown {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d DC tax to be?", y.year),
			fmt.Sprintf("Until then, %s. Whether the %s and %v exceptions apply stays open.", map[bool]string{true: "your target can only come from last year's tax", false: "there's no target"}[a.requirement.TargetKnown()], y.threshold.Whole(), y.remaining))
	}

	// The due dates. § 47-1812.08(i)(4) fixes them, and D-40ES moves one that
	// falls on a weekend or legal holiday.
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	moved := false
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			moved = true
			a.notes.Rule("D-40ES (REV. 03/2026), When are your vouchers due?", fmt.Sprintf("the voucher due %s falls on a weekend or legal holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
		}
	}
	if !moved {
		a.notes.Rule("D-40ES (REV. 03/2026), When are your vouchers due?", "a voucher due on a Saturday, Sunday or legal holiday is due the next business day; no due date this year is a Saturday or Sunday, and no list of the holidays that count is published")
	}
	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), quarters)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("D-2210 (05/2025), lines 6 and 7; D.C. Code § 47-4203(b)(1)(B)", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", a.requirement.Target(), a.installments[0],
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() && !a.exempt {
		r.annualise(f, &a)
	}

	lateStart := false
	if met := f.RequirementFirstMet(); !met.IsZero() && !a.exempt {
		for i := len(y.laterGroups) - 1; i >= 0; i-- {
			if !met.Before(y.laterGroups[i]) {
				lateStart = true
				a.notes.Ask(working.PositionUnsettledRule, "",
					fmt.Sprintf("First needing to pay on %s makes your first payment due %s. The law doesn't say how much; the figures charge a quarter on all four dates.",
						short(met, y.year), short(a.dues[i+1], y.year)))
				break
			}
		}
	}

	// A return filed with the balance paid by January 15 takes the place of
	// the fourth payment. D-40ES says not to send the last voucher;
	// § 47-1812.08(i)(6) treats the return as the declaration. Neither
	// excuses an earlier shortfall.
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(a.dues[3]) && !a.exempt {
		a.excusedJanuary = true
		a.notes.Rule("D-40ES (REV. 03/2026), Filing a return before last payment; D.C. Code § 47-1812.08(i)(6)", fmt.Sprintf("the return was filed and the balance paid on %s, by %s, so it takes the place of the fourth payment; interest on an earlier shortfall still runs",
			filed.Long(), a.dues[3].Long()))
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	af := affects{
		who:         !a.exempt && taxKnown && tax-withheld == y.threshold,
		priorTest:   !a.exempt && hasPrior && prior.Filed() && prior.Months() == 12 && residenceAnswered && !resident && (!taxKnown || y.priorPercent.Of(priorTax) < thisYear.Amount()),
		noLiability: noLiability && residenceAnswered && !resident,
		remaining:   remainingExempt,
		annualised:  f.Elections().Annualised() && !a.exempt,
		lateStart:   lateStart,
		holiday:     moved,
	}
	if a.requirement.TargetKnown() && !a.exempt {
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return assessment{}, err
		}
		anyShort := false
		for _, l := range ledger {
			anyShort = anyShort || l.Shortfall() > 0
		}
		var scratch working.Notes
		kind := r.excuse(f, a, &scratch)
		paid := withheld
		for _, p := range f.Payments() {
			paid += p.Amount()
		}
		waiver, _ := f.Positions().Waiver()
		af.under100 = anyShort && kind == excuseThreshold
		af.remaining = af.remaining || kind == excuseRemaining
		af.list110 = anyShort && priorOpt.Available() && paid >= priorOpt.Amount()
		af.factors = anyShort && kind == excuseNone
		af.waiver = af.factors && waiver != estimated.WaiverNone
	}
	ds, err := r.divergences(af)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(ds...)
	return a, nil
}

// short writes a date as a person reads it: "Jun 15" in the tax year, and
// "Jan 15, 2027" outside it.
func short(d estimated.Date, year int) string {
	if d.Year() == year {
		return fmt.Sprintf("%s %d", d.Month().String()[:3], d.Day())
	}
	return fmt.Sprintf("%s %d, %d", d.Month().String()[:3], d.Day(), d.Year())
}

// underRemaining reports D-2210's 10% exception: what is left to pay is less
// than the share of the tax.
func (r districtEstimated) underRemaining(tax, left money.USCents) bool {
	return int64(max(0, left))*10_000 < int64(r.y.remaining)*int64(tax)
}

// remainingLimit is the share of the tax, rounded up to the cent, so that an
// amount is under it exactly when [underRemaining] holds.
func (r districtEstimated) remainingLimit(tax money.USCents) money.USCents {
	n := int64(r.y.remaining) * int64(max(0, tax))
	return money.USCents((n + 9_999) / 10_000)
}

func (r districtEstimated) remainingOpen(n *working.Notes) {
	n.Ask(working.PositionUnsettledRule, "",
		fmt.Sprintf("The form excuses a balance less than %v of the year's tax. The law doesn't include that exception, and the figures follow the form.", r.y.remaining))
}

// annualise replaces the installments with D-2210's proration: the required
// annual payment times the income to each period's end over the year's
// income. The form does not say where the periods end; the statute's 3, 5 and
// 8 months are used.
func (r districtEstimated) annualise(f estimated.Facts, a *assessment) {
	periods := f.Periods()
	total := periods[len(periods)-1].Income()
	target := a.requirement.Target()
	if total <= 0 {
		a.notes.Rule("D-2210 (05/2025), instructions for line 7", fmt.Sprintf("the year's income of %v cannot be divided by, so the regular installments are used", total))
		return
	}
	cumulative := make([]money.USCents, len(periods))
	var prev money.USCents
	lines := make([]string, len(periods))
	for i, p := range periods {
		share := min(max(p.Income(), 0), total)
		cumulative[i] = max(prev, mulDiv(target, share, total))
		if i == len(periods)-1 {
			cumulative[i] = target
		}
		prev = cumulative[i]
		lines[i] = fmt.Sprintf("%v of income to %s gives %v", p.Income(), p.End().Long(), cumulative[i])
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("D-2210 (05/2025), instructions for line 7", fmt.Sprintf("under the annualised method each period's running total is %v times the income to the period's end over the year's %v: %s",
		target, total, strings.Join(lines, "; ")))
	a.notes.Ask(working.PositionUnsettledRule, "",
		"The form spreads your required payment by the income earned to each period. The figures use it, not the law's 22.5%, 45%, 67.5% and 90% of annualised tax.")
}

// mulDiv is a × b ÷ c, rounded half up, for amounts that are not negative.
func mulDiv(a, b, c money.USCents) money.USCents {
	n := new(big.Int).Mul(big.NewInt(int64(a)), big.NewInt(int64(b)))
	return estimated.RoundToCent.Div(n, big.NewInt(int64(c)))
}

func (r districtEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies the credits to the installments.
func (r districtEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
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
		notes.Rule("D.C. Code § 47-4203(d)(3)", fmt.Sprintf("the %v withheld is credited on the dates it was actually withheld, which the filer must establish for all of it", f.WithheldTotal()))
	} else {
		notes.Rule("D.C. Code § 47-4203(d)(3)", fmt.Sprintf("the %v withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld; D-2210 does not say", f.WithheldTotal()))
	}
	notes.Rule("D.C. Code § 47-4203(d)(1), (4); D-40ES (REV. 03/2026), Applying an overpayment",
		"a payment is credited against the unpaid installments in the order they fall due, and last year's overpayment applied forward counts toward the first")
	return ledger, notes, nil
}

// excuse reports whether D-2210's $100 or 10% exception excuses the whole
// year once the estimated payments are counted, and adds the working. The
// payments counted are those made by the fourth due date, and last year's
// overpayment applied forward.
func (r districtEstimated) excuse(f estimated.Facts, a assessment, notes *working.Notes) excuseKind {
	if !a.taxKnown || a.exempt {
		return excuseNone
	}
	y := r.y
	paid := f.WithheldTotal()
	for _, p := range f.Payments() {
		if p.FromPriorYear() || !p.On().After(a.dues[3]) {
			paid += p.Amount()
		}
	}
	left := max(0, a.tax-paid)
	switch {
	case left < y.threshold:
		notes.Rule("D-2210 (05/2025), instructions, Are there any exceptions", fmt.Sprintf("%v of tax less %v of withholding and estimated payments made by %s leaves %v, under %v, so there is no interest",
			a.tax, paid, a.dues[3].Long(), left, y.threshold))
		return excuseThreshold
	case r.underRemaining(a.tax, left):
		notes.Rule("D-2210 (05/2025), instructions, Are there any exceptions", fmt.Sprintf("%v of tax less %v of withholding and estimated payments made by %s leaves %v, less than %v of the tax, so there is no interest",
			a.tax, paid, a.dues[3].Long(), left, y.remaining))
		r.remainingOpen(notes)
		return excuseRemaining
	}
	return excuseNone
}

// excuseKind is which of D-2210's exceptions excuses the year once the
// estimated payments are counted.
type excuseKind int

const (
	excuseNone excuseKind = iota
	excuseThreshold
	excuseRemaining
)

func (r districtEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d DC return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	excused := r.excuse(f, a, &notes) != excuseNone
	out, err := installments(a, ledger, excused)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// installments are the ledger's due dates, excused where a return filed by
// January 15 replaces the fourth, or where an exception excuses the year.
func installments(a assessment, ledger []estimated.LedgerEntry, excused bool) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(l.NeededBy()).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if excused || (a.excusedJanuary && i == 3) {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// spanEnd is where D-2210's span from installment i ends: the next due date,
// or April 15 after the last.
func (r districtEstimated) spanEnd(a assessment, i int) estimated.Date {
	if i+1 < len(a.dues) {
		return a.dues[i+1]
	}
	return r.y.stop
}

func (r districtEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("districtofcolumbiataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d DC return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := asOf
	if y.stop.Before(stop) {
		stop = y.stop
	}
	waiver, waiverAnswered := f.Positions().Waiver()
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	excused := r.excuse(f, a, &notes) != excuseNone
	schedule, err := installments(a, ledger, excused)
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
	if excused {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		return working.NewResult(p).With(a.notes).With(notes).Build()
	}

	// D-2210 lines 7 to 11: each column's required running total less what
	// was paid by its due date, times the factor for the span that follows.
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	var cut []string
	for i, l := range ledger {
		under := max(0, l.NeededBy()-l.CreditedBy())
		if i == 3 && a.excusedJanuary {
			under = 0
		}
		if under == 0 || !stop.After(l.Due()) {
			continue
		}
		end := r.spanEnd(a, i)
		to := end
		if stop.Before(end) {
			to = stop
			cut = append(cut, fmt.Sprintf("%d of the %d days from %s", to.DaysSince(l.Due()), end.DaysSince(l.Due()), l.Due().Long()))
		}
		part, err := spanCharge(under, l.Due(), to, end.DaysSince(l.Due()), y.factors[i], y.rate,
			fmt.Sprintf("D-2210 (05/2025), line 10: %s for the period from %s", y.factors[i], l.Due().Long()))
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		line, err := estimated.NewPenaltyLine(i+1, l.Due(), under).Days(to.DaysSince(l.Due())).Part(part).Build()
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
		notes.Rule("D-2210 (05/2025), line 5", fmt.Sprintf("this year's DC tax was not given, so the interest is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and %v of this year's tax, and the $100 and 10%% exceptions can only excuse more, so %v is the most the interest can be",
			y.priorPercent, y.year-1, y.thisYearPercent, p.Total()))
	}
	factors := make([]string, len(y.factors))
	for i, fa := range y.factors {
		factors[i] = fa.String()
	}
	notes.Rule("D-2210 (05/2025), lines 8 to 11; D.C. Code §§ 47-4201(d)(2), 47-4203(c)", fmt.Sprintf("each period's underpayment, the running total required less withholding and payments credited by its due date, is multiplied by the factor for the span to the next due date or %s, %s, "+
		"and a payment during a span counts from the next due date; D-2210 does not say how to round, and the D-40 booklet says to enter whole dollars, so each charge is rounded to the whole dollar; "+
		"D-2210 gives no derivation of the factors, but they match 10%% compounded daily as an annual rate, 10.5156%%, times 61, 92, 122 and 90 days over 365, and 2026's spans have the same lengths, so the 2025 factors are used; the interest to %s is %v",
		y.stop.Long(), strings.Join(factors, ", "), stop.Long(), p.Total()))
	if len(cut) > 0 {
		notes.Rule("D-2210 (05/2025), line 10", fmt.Sprintf("D-2210 prints a factor only for a whole span; figured to %s, a span still running is charged that share of its factor by days: %s",
			stop.Long(), strings.Join(cut, "; ")))
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.After(a.dues[3]) {
		notes.Rule("D-2210 (05/2025), lines 8 to 11", fmt.Sprintf("the return was filed and paid on %s, after %s; D-2210 charges the fourth period's factor to %s whatever day the balance was paid",
			filed.Long(), a.dues[3].Long(), y.stop.Long()))
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking for the interest to be waived?",
			"It can be for retiring after 62, becoming disabled, or a casualty or disaster. The Office of Tax and Revenue decides; the interest shown is before any waiver.")
	}
	if waiverAnswered && waiver != estimated.WaiverNone {
		notes.Rule("D.C. Code § 47-4203(e)(3), (4)", fmt.Sprintf("you ask for a waiver for %v; the Mayor decides it, and D-2210 does not list it, so the interest shown is before it", waiver))
	}
	// § 47-4203(e)(3) and (4) are the federal pair.
	if o, ok := estimated.StandardWaiver(f, "the Office of Tax and Revenue", 62); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// spanCharge charges principal at a factor D-2210 prints for a whole span of
// spanDays days. A span cut short at to is charged the share of the factor
// its days are of the span's. Each charge is rounded to the whole dollar.
func spanCharge(principal money.USCents, from, to estimated.Date, spanDays int, f estimated.Factor, annual money.Rate, source string) (estimated.PenaltyPart, error) {
	days := to.DaysSince(from)
	if days >= spanDays {
		return estimated.SpanFactor(principal, from, to, f, annual, estimated.RoundToDollar, source)
	}
	units, places, err := factorDigits(f)
	if err != nil {
		return estimated.PenaltyPart{}, err
	}
	n := new(big.Int).Mul(big.NewInt(int64(principal)), big.NewInt(units))
	n.Mul(n, big.NewInt(int64(days)))
	d := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(places)), nil)
	d.Mul(d, big.NewInt(int64(spanDays)))
	charge := estimated.RoundToDollar.Div(n, d)
	return estimated.NewPenaltyPart(estimated.PartInterest, principal, annual).
		Span(from, to).Days(days).Charge(charge).Source(source).Build()
}

// factorDigits reads a printed factor back into its digits and the number
// of them after the point, which [estimated.Factor] keeps unexported.
func factorDigits(f estimated.Factor) (units int64, places int, err error) {
	whole, frac, _ := strings.Cut(f.String(), ".")
	for _, c := range whole + frac {
		if c < '0' || c > '9' {
			return 0, 0, fmt.Errorf("districtofcolumbiataxcalc: %q is not a printed factor", f.String())
		}
		units = units*10 + int64(c-'0')
	}
	return units, len(frac), nil
}

func (r districtEstimated) Explanation() estimated.Explanation { return districtExplanation }

// districtExplanation is written by hand and checked against
// research/estimated-payments/dc.md.
var districtExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Take your DC tax due after withholding and credits. If it is $100 or more, the District expects estimated payments. The charge for paying too little or too late is called underpayment interest. Form D-2210 figures it with a printed factor for each period.").
		Paragraph("There is no interest if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's DC tax or 110% of last year's. The District has no 100% tier: last year's figure is 110% at every income. The form lets you use it only if you filed a DC return for last year. Otherwise only the 90% figure counts.").
		Paragraph("Pay a quarter each by April 15, June 15, September 15 and the next January 15. A date on a weekend or legal holiday becomes the next business day. Withholding counts as paid evenly on the four dates. You can instead show when all of it was actually withheld. An overpayment carried from last year counts toward April. File your return and pay the balance by January 15 to skip the January payment. Interest on any earlier shortfall still runs.").
		Paragraph("D-2210 takes the amount still short at each due date. That counts everything required and paid so far. It is multiplied by a factor for the span to the next due date. The factors are .0175, .0265, .0351 and .0259, the last running to April 15. A payment made between due dates counts only from the next due date. The factors appear to spread the law's rate over each span's days. That rate is 10% a year compounded daily, about 10.5% a year. The law charges the actual days until payment, rounded up to the month's end. Its figure can therefore differ from the form's.").
		Paragraph("No interest is due if tax less withholding and estimated payments is under $100. None is due either if you had no DC tax last year and lived in DC all of it. The form also excuses a balance under 10% of the year's tax. The law doesn't mention that. The law excuses a casualty or disaster. So is someone who retired after 62 or became disabled, with reasonable cause. None of these is on D-2210, so you have to ask the Office of Tax and Revenue.").
		VersusFederal("The threshold is $100, not $1,000. Last year's figure is always 110%, with no income test. The charge is a fixed 10% compounded daily. The form excuses a balance under 10% of the year's tax. Its annualised method is a proration by income. Filing and paying by January 15, not January 31, replaces the last payment.").
		Source("D.C. Code §§ 47-4201(d), 47-4203, 47-1812.08(i)").
		Source("D-40ES, Estimated Payment for Individual Income Tax, 2026 (REV. 03/2026)").
		Source("D-2210, Underpayment of Estimated Income Tax By Individuals, 2025 (revised 05/2025)").
		Source("2025 D-40 booklet").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
