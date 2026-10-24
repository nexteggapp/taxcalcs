package illinoistaxcalc

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Illinois's estimated income tax rules, Illinois Income Tax Act §§ 803, 804
// and 806 (35 ILCS 5/803, 5/804, 5/806), with the penalty rates of the
// Uniform Penalty and Interest Act § 3-3 (35 ILCS 735/3-3), as the shared
// [estimated.EstimatedTax] interface.
//
// The penalty is not interest. Each amount paid late is charged once, 2% if
// it is paid within 30 days of the due date and 10% if it is paid later or
// not at all. The rest is close to federal in shape: 90% of this year's tax
// or 100% of last year's, four equal installments, and a $1,000 threshold.
//
// The figure follows the department's forms, the 2026 IL-1040-ES and the 2025
// IL-2210 and its instructions, and the result's Divergences say where the
// Act or the department's own rules read otherwise. No act changed these
// rules for 2026 (research/estimated-payments/il.md, "2026 changes"), so no
// form here predates the law it describes.

// jurisdiction is Illinois's postal code.
const jurisdiction = estimated.Illinois

// EstimatedFacts are the facts Illinois's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
//
// [estimated.Facts.Withholding] is Illinois income tax withheld from wages
// and other income, Form IL-1040 line 25. Pass-through withholding, line 27,
// is dated differently and comes here.
type EstimatedFacts struct {
	nursingHome         bool
	nursingHomeAnswered bool

	passThrough         []PassThroughWithholding
	passThroughAnswered bool

	interest         bool
	interestAnswered bool

	overpaymentLate     estimated.Date
	overpaymentAnswered bool
}

// NursingHome reports whether the filer or spouse is 65 or older and lives
// permanently in a nursing home (Form IL-1040 line 34b), and whether the
// caller said.
func (e EstimatedFacts) NursingHome() (qualifies, answered bool) {
	return e.nursingHome, e.nursingHomeAnswered
}

// PassThroughWithholding is the pass-through withholding on the filer's
// behalf, Form IL-1040 line 27, one entry per entity, and whether the caller
// said.
func (e EstimatedFacts) PassThroughWithholding() ([]PassThroughWithholding, bool) {
	return append([]PassThroughWithholding(nil), e.passThrough...), e.passThroughAnswered
}

// InterestOnLateInstallments reports whether interest is to be charged on a
// late installment on top of the penalty, and whether the caller said. No
// Illinois document settles it; see [Penalty].
func (e EstimatedFacts) InterestOnLateInstallments() (charge, answered bool) {
	return e.interest, e.interestAnswered
}

// OverpaymentElectedLate is the day last year's overpayment was elected to
// this year's estimated tax, when the election came after the extended due
// date of last year's return. The zero Date means it came in time, or was not
// said; answered tells which.
func (e EstimatedFacts) OverpaymentElectedLate() (on estimated.Date, answered bool) {
	return e.overpaymentLate, e.overpaymentAnswered
}

// PassThroughWithholding is Illinois tax a partnership, S corporation or
// trust withheld on the filer's behalf, Schedule K-1-P or K-1-T, and the last
// day of that entity's tax year.
type PassThroughWithholding struct {
	yearEnd estimated.Date
	amount  money.USCents
}

func (p PassThroughWithholding) YearEnd() estimated.Date { return p.yearEnd }
func (p PassThroughWithholding) Amount() money.USCents   { return p.amount }

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

// NursingHome answers whether the filer or spouse is 65 or older and lives
// permanently in a nursing home.
func (b *EstimatedFactsBuilder) NursingHome(qualifies bool) *EstimatedFactsBuilder {
	b.e.nursingHome, b.e.nursingHomeAnswered = qualifies, true
	return b
}

// PassThroughWithholding adds the amount one entity withheld, and the last
// day of its tax year.
func (b *EstimatedFactsBuilder) PassThroughWithholding(entityYearEnd estimated.Date, amount money.USCents) *EstimatedFactsBuilder {
	b.e.passThrough = append(b.e.passThrough, PassThroughWithholding{yearEnd: entityYearEnd, amount: amount})
	b.e.passThroughAnswered = true
	return b
}

// NoPassThroughWithholding answers that no entity withheld on the filer's
// behalf.
func (b *EstimatedFactsBuilder) NoPassThroughWithholding() *EstimatedFactsBuilder {
	b.e.passThroughAnswered = true
	return b
}

// InterestOnLateInstallments answers whether to charge interest on a late
// installment on top of the penalty.
func (b *EstimatedFactsBuilder) InterestOnLateInstallments(charge bool) *EstimatedFactsBuilder {
	b.e.interest, b.e.interestAnswered = charge, true
	return b
}

// OverpaymentElectedOnTime answers that last year's overpayment was elected
// to this year by the extended due date of last year's return.
func (b *EstimatedFactsBuilder) OverpaymentElectedOnTime() *EstimatedFactsBuilder {
	b.e.overpaymentLate, b.e.overpaymentAnswered = estimated.Date{}, true
	return b
}

// OverpaymentElectedLate answers that last year's overpayment was elected to
// this year after the extended due date of last year's return, on the day
// given.
func (b *EstimatedFactsBuilder) OverpaymentElectedLate(on estimated.Date) *EstimatedFactsBuilder {
	b.e.overpaymentLate, b.e.overpaymentAnswered = on, true
	return b
}

// Build refuses pass-through withholding with no year end or a negative
// amount, and a late election with no date. Whether a date falls in the tax
// year is checked by the rules, which know the year.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	e := b.e
	e.passThrough = append([]PassThroughWithholding(nil), e.passThrough...)
	for _, p := range e.passThrough {
		if p.yearEnd.IsZero() {
			return EstimatedFacts{}, errors.New("illinoistaxcalc: pass-through withholding needs the entity's year end")
		}
		if p.amount < 0 {
			return EstimatedFacts{}, fmt.Errorf("illinoistaxcalc: pass-through withholding of %v is negative", p.amount)
		}
	}
	return e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is IL-2210 line 7: no installment is required when this
	// year's tax less credits and all withholding is this or less.
	threshold money.USCents

	// ruleThreshold is the figure 86 Ill. Adm. Code 100.8000(a)(1) still
	// prints, superseded by the Act. It is reported, not used.
	ruleThreshold money.USCents

	// thisYear and priorYear are the two measures of the required annual
	// payment, IL-2210 lines 6 and 7.
	thisYear, priorYear money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// periodEnds are IL-2210 Step 6: the first 3, 5 and 8 months, and the
	// year. applicable are its line 48 percentages.
	periodEnds []estimated.Date
	applicable []money.Rate

	// lateDays is the most days late that IL-2210's lower penalty rate
	// covers; low and high are the two rates.
	lateDays  int
	low, high money.Rate

	// interest is the uniform interest rate, used only when the caller asks
	// for interest on late installments.
	interest estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are 35 ILCS 5/803(d) and IL-2210 line 8: four equal installments.
// IL-2210 line 10a credits withholding in the same four parts.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Illinois's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return illinoisEstimated{y}, nil
}

type illinoisEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// installments are each quarter's required installment, IL-2210 line 9b.
	installments []money.USCents

	extras EstimatedFacts
	notes  working.Notes
}

func (r illinoisEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("illinoistaxcalc: the facts carry %T, not illinoistaxcalc.EstimatedFacts", s)
	}
}

func (r illinoisEstimated) check(f estimated.Facts, e EstimatedFacts) error {
	y := r.y
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("illinoistaxcalc: these are Illinois's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != y.year:
		return fmt.Errorf("illinoistaxcalc: these are the %d rules, and the facts are for %d", y.year, f.TaxYear())
	}
	for _, p := range e.passThrough {
		if p.yearEnd.Year() != y.year {
			return fmt.Errorf("illinoistaxcalc: pass-through withholding for an entity year ending %v does not go on a %d return", p.yearEnd, y.year)
		}
	}
	if on, _ := e.OverpaymentElectedLate(); !on.IsZero() && on.Year() < y.year {
		return fmt.Errorf("illinoistaxcalc: an overpayment elected on %v is before tax year %d", on, y.year)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != y.periodEnds[i] {
				return fmt.Errorf("illinoistaxcalc: IL-2210 Step 6 period %d ends on %v, not %v", i+1, y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// affects says which divergences change anything for the facts.
type affects struct {
	noLiability, shortYear, nonWage, applying, actualDates, threshold, farmer bool
}

// divergences are where the department's forms and the Act, or the
// department's own rules, part ways on a rule every Illinois computation
// uses. Most are in research/estimated-payments/il.md, item 10 and its
// verification; the first and fourth were found reading the forms beside the
// rules for this package.
func (r illinoisEstimated) divergences(af affects) ([]working.Divergence, error) {
	y := r.y
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("No liability last year").
			Form("Subtract credits and all Illinois withholding from last year's tax. If the result is zero, there's no penalty. So a year fully covered by withholding counts.",
				"IL-2210 Instructions (R-12/25), What is late-payment penalty?").
			Statute("There's no penalty only if you had no tax liability for a 12-month year before. Withholding pays the tax; it doesn't reduce the liability.",
				"35 ILCS 5/804(d); 86 Ill. Adm. Code 100.8010(b)(2), (g)(3)").
			Uses(working.ReadingForm).Affects(af.noLiability),
		working.NewDivergence("Last year's return as a measure").
			Form("Column B takes last year's tax less credits with no condition on the return. The no-liability exception says nothing about the year's length.",
				"IL-2210 (R-12/25), Step 2, Column B; IL-2210 Instructions (R-12/25)").
			Statute("100% of last year's tax counts only if you filed a 12-month return showing tax. The no-liability exception also needs a 12-month year.",
				"35 ILCS 5/804(c)(1)(B)(ii), (d); 86 Ill. Adm. Code 100.8010(d)(1)(A)(ii), (g)(3)").
			Uses(working.ReadingStatute).Affects(af.shortYear),
		working.NewDivergence("Withholding not from wages").
			Form("All Illinois withholding is treated as paid in four equal parts. That includes 1099 withholding. Pay stubs or an employer letter can show the actual quarters instead.",
				"IL-2210 Instructions (R-12/25), Line 10a; 86 Ill. Adm. Code 100.8010(e)(2)").
			Statute("Withholding from wages is spread over the due dates. Other Illinois withholding is a payment on the date it is made.",
				"35 ILCS 5/804(g)(1), (g)(3)").
			Uses(working.ReadingForm).Affects(af.nonWage),
		working.NewDivergence("How withholding meets the installments").
			Form("Each quarter's withholding comes off that quarter's installment. Any excess carries to later quarters only. Estimated payments then go to the oldest unpaid quarter.",
				"IL-2210 (R-12/25), Step 2, lines 10a to 13; IL-2210 Instructions (R-12/25), Lines 10a, 10b and Penalty Worksheet 1").
			Statute("Withholding is a payment on each due date. Pass-through withholding is a payment on the last day of the entity's year. Each goes to the oldest unpaid installment first.",
				"35 ILCS 5/804(g); 86 Ill. Adm. Code 100.8010(e)(1), (e)(2), citing IITA § 709.5(b)").
			Uses(working.ReadingForm).Affects(af.applying),
		working.NewDivergence("Choosing actual withholding dates").
			Form("Line 10a takes one-fourth of withholding or the actual amount, for each quarter. The line doesn't say the choice must cover every quarter.",
				"IL-2210 Instructions (R-12/25), Line 10a").
			Statute("You can use actual dates only if you show the dates of everything withheld. Then all withholding counts on its dates.",
				"35 ILCS 5/804(g)(1); 86 Ill. Adm. Code 100.8010(e)(2)").
			Uses(working.ReadingStatute).Affects(af.actualDates),
		working.NewDivergence(fmt.Sprintf("The %s threshold", y.threshold.Whole())).
			Form(fmt.Sprintf("No installment is required at %s or less. The figure is this year's tax after credits and withholding.", y.threshold.Whole()),
				"IL-1040-ES (R-09/26), Am I required to make estimated income tax payments?; IL-2210 (R-12/25), line 7").
			Statute(fmt.Sprintf("The law's threshold has been %s since 2019. The department's rule still says %s, which the law has superseded.", y.threshold.Whole(), y.ruleThreshold.Whole()),
				"35 ILCS 5/803(a); 86 Ill. Adm. Code 100.8000(a)(1), 100.8010(g)(1)(A)").
			Uses(working.ReadingForm).Affects(af.threshold),
		working.NewDivergence("Exceptions the form does not list").
			Form(fmt.Sprintf("The form lists five cases with no penalty. Two are farmers and anyone 65 or older in a nursing home. Two are no return required last year and no liability last year. The fifth is %s or less this year.", y.threshold.Whole()),
				"IL-2210 Instructions (R-12/25), What is late-payment penalty?").
			Statute("The department's rule also excuses an underpayment after the taxpayer's death. Service in a combat zone with a federal extension is excused too. So are an innocent spouse to the extent relieved and a bankruptcy estate.",
				"86 Ill. Adm. Code 100.8010(g)(1)(B)(iv), (g)(6), (g)(7), (g)(8)").
			// No fact these exceptions turn on is carried.
			Uses(working.ReadingForm).Affects(false),
		working.NewDivergence("The farmer test").
			Form("At least two-thirds of your total federal gross income is from farming.",
				"IL-1040-ES (R-09/26), Are there any exceptions?; IL-1040 (R-12/25), line 34a").
			Statute("Two-thirds of your total estimated gross income is from farming. The share is measured as the federal rule does. On a joint return the share is of the couple's combined income.",
				"35 ILCS 5/803(e); 86 Ill. Adm. Code 100.8000(b)(2), (e)(3)").
			Uses(working.ReadingForm).Affects(af.farmer),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// shortDate is "Apr 15" inside the tax year and "Jan 15, 2027" outside it.
func shortDate(d estimated.Date, year int) string {
	m := d.Month().String()[:3]
	if d.Year() == year {
		return fmt.Sprintf("%s %d", m, d.Day())
	}
	return fmt.Sprintf("%s %d, %d", m, d.Day(), d.Year())
}

// passThroughTotal is the year's pass-through withholding.
func passThroughTotal(e EstimatedFacts) money.USCents {
	var t money.USCents
	for _, p := range e.passThrough {
		t += p.amount
	}
	return t
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

func (r illinoisEstimated) assess(f estimated.Facts) (assessment, error) {
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	if err := r.check(f, extras); err != nil {
		return assessment{}, err
	}
	y := r.y
	a := assessment{extras: extras, dues: estimated.MoveDates(y.calendar, y.dues)}
	last := y.year - 1

	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	passThrough := passThroughTotal(extras)
	allWithheld := withheld + passThrough
	prior, hasPrior := f.PriorYear()
	var priorFigures estimated.Figures
	if hasPrior {
		priorFigures, _ = prior.Figures()
	}
	priorTax, _ := priorFigures.Tax()

	// 90% of this year's tax after credits, IL-2210 line 6.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYear, y.thisYear.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYear, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("35 ILCS 5/804(c)(1)(B)(i); IL-2210 (R-12/25), lines 3 and 6", fmt.Sprintf("%v of this year's Illinois tax after credits is %v", y.thisYear, thisYear.Amount()))
	}

	// 100% of last year's tax after credits, IL-2210 Column B line 3, for a
	// filed 12-month return showing a liability.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYear, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Illinois return", last))
	case prior.NotRequiredToFile():
		priorOption = unavailable(fmt.Sprintf("You didn't have to file a %d Illinois return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Illinois return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Illinois return covered %d months, not 12", last, prior.Months()))
	case priorTax == 0:
		priorOption = unavailable(fmt.Sprintf("Your %d Illinois return showed no tax", last))
	default:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYear, y.priorYear.Of(priorTax))
		a.notes.Rule("35 ILCS 5/804(c)(1)(B)(ii); IL-2210 (R-12/25), Column B line 3 and line 7", fmt.Sprintf("%v of the %v tax after credits on the %d return is %v", y.priorYear, priorTax, last, y.priorYear.Of(priorTax)))
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
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Illinois tax after credits to be?", y.year),
			fmt.Sprintf("It sets the %v option and decides if you're within the %s threshold.", y.thisYear, y.threshold.Whole()))
	}

	// Last year's tax less credits and all withholding, the form's test for
	// no liability. Without last year's withholding the test is taken on the
	// tax alone, which the Act's reading also gives.
	priorWithheld, priorWithheldKnown := priorFigures.Withheld()
	priorNoLiability := false
	if hasPrior && prior.Filed() && prior.Months() == 12 {
		switch {
		case priorTax == 0:
			priorNoLiability = true
		case priorWithheldKnown:
			priorNoLiability = priorTax-priorWithheld <= 0
		default:
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("How much Illinois tax was withheld for you in %d?", last),
				fmt.Sprintf("If it covered all of your %d tax, there's no penalty for %d.", last, y.year))
		}
	}
	nursingHome, nursingHomeAnswered := extras.NursingHome()

	// exception is the one that applies, if any.
	var exception estimated.TargetOption
	exempt := func(label string) error {
		var err error
		exception, err = estimated.NewException(label).Build()
		a.exempt = true
		return err
	}
	byNoLiability := false
	switch {
	case int64(f.FarmShare())*3 >= 20_000:
		if err := exempt("Two-thirds of gross income from farming"); err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("At least two-thirds of your gross income is from farming. Illinois requires no estimated payments for %d and charges no penalty.", y.year)).Exempt(exception)
		a.notes.Rule("35 ILCS 5/803(a), (e); IL-1040-ES (R-09/26), Are there any exceptions?; IL-1040 (R-12/25), line 34a", fmt.Sprintf("%v of gross income is from farming, at least two-thirds, so no estimated payments are required; on a joint return the share is of the couple's combined income (86 Ill. Adm. Code 100.8000(e)(3))", f.FarmShare()))
	case nursingHome:
		if err := exempt("Living permanently in a nursing home"); err != nil {
			return assessment{}, err
		}
		rb.Required(false, "You or your spouse are 65 or older and live permanently in a nursing home. Illinois requires no estimated payments and charges no penalty.").Exempt(exception)
		a.notes.Rule("35 ILCS 5/806; IL-1040-ES (R-09/26), Are there any exceptions?; IL-1040 (R-12/25), line 34b", "the filer or spouse is 65 or older and a permanent resident of a nursing home, so no estimated payments are required")
	case hasPrior && prior.NotRequiredToFile():
		if err := exempt("No Illinois return required last year"); err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You didn't have to file an Illinois return for %d. Illinois therefore charges no estimated tax penalty for %d.", last, y.year)).Exempt(exception)
		a.notes.Rule("35 ILCS 5/804(d); IL-2210 Instructions (R-12/25); IL-1040 (R-12/25), line 34d", fmt.Sprintf("no %d Illinois return was required, so there is no penalty for %d", last, y.year))
	case priorNoLiability:
		byNoLiability = true
		if err := exempt("No Illinois tax due last year"); err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your %d Illinois return showed no tax due after credits and withholding. Illinois therefore charges no estimated tax penalty for %d.", last, y.year)).Exempt(exception)
		a.notes.Rule("35 ILCS 5/804(d); IL-2210 Instructions (R-12/25), What is late-payment penalty?", fmt.Sprintf("the %d return's %v of tax after credits less %v of withholding is zero or less, for a 12-month year, so there is no penalty for %d", last, priorTax, priorWithheld, y.year))
	case taxKnown && tax-allWithheld <= y.threshold:
		a.exempt = true
		if exception, err = estimated.ExceptionAtMost("Tax less withholding", tax-allWithheld, y.threshold); err != nil {
			return assessment{}, err
		}
		leaves := "leaves " + near(tax-allWithheld, y.threshold)
		then := fmt.Sprintf("That is %s or less, so you need no estimated payments and have no penalty.", y.threshold.Whole())
		if tax < allWithheld {
			leaves = estimated.Leaves(tax - allWithheld)
			then = "No estimated payments are needed and there's no penalty."
		}
		rb.Required(false, fmt.Sprintf("Your Illinois tax after credits is %s, and %s was withheld. That %s. %s",
			tax.Whole(), allWithheld.Whole(), leaves, then)).Exempt(exception)
		a.notes.Rule("35 ILCS 5/803(a); IL-2210 (R-12/25), lines 5 and 7; IL-1040-ES (R-09/26), Estimated Tax Worksheet line 10", fmt.Sprintf("%v of tax less %v of Illinois withholding and %v of pass-through withholding is %v, not more than %v, so no installment is required",
			tax, withheld, passThrough, tax-allWithheld, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target needs your %d Illinois tax, since your %d return can't set one.", y.year, last)).NoTarget()
	case !taxKnown && allWithheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches %v of your %d tax, %s. No estimated payments are needed whatever your %d tax turns out to be.",
			allWithheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. After %s of withholding that's %s of estimated payments. A quarter by each date keeps you clear of a penalty whatever your %d tax is.",
			priorOpt.Percent(), last, target.Whole(), allWithheld.Whole(), (target-allWithheld).Whole(), y.year)).Choose(chosen)
	case allWithheld >= target:
		rb.Required(false, fmt.Sprintf("Your Illinois withholding of %s already reaches your target of %s. No estimated payments are needed.", allWithheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Illinois tax after credits is %s, and %s was withheld. That leaves %s. That is more than %s, so you need %s of estimated payments. Paying a quarter of that by each date reaches your target of %s.",
			tax.Whole(), allWithheld.Whole(), near(tax-allWithheld, y.threshold), y.threshold.Whole(), (target-allWithheld).Whole(), target.Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// Under an exception last year's return can't change the target.
	if !hasPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Illinois return, and what was its tax?", last),
			fmt.Sprintf("It can make %v of last year's tax your target, not %v of this year's. It also decides two exceptions.", y.priorYear, y.thisYear))
	}

	// What each divergence turns on in these facts.
	af := affects{
		noLiability: byNoLiability && priorTax > 0,
		shortYear: !a.exempt && hasPrior && prior.Filed() && prior.Months() != 12 &&
			(priorTax == 0 || !taxKnown || y.priorYear.Of(priorTax) < thisYear.Amount()),
		threshold: a.exempt && !byNoLiability && taxKnown && tax-allWithheld > y.ruleThreshold && tax-allWithheld <= y.threshold,
		farmer:    f.FarmShare() > 0,
	}
	if a.exempt || !a.requirement.TargetKnown() {
		a.installments = make([]money.USCents, len(a.dues))
		return a, r.diverges(&a, af)
	}
	if !nursingHomeAnswered {
		a.notes.Ask(working.PositionJurisdictionFact, "Are you or your spouse 65 or older and living permanently in a nursing home?",
			"If so, Illinois requires no estimated payments.")
	}
	if _, answered := extras.PassThroughWithholding(); !answered {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did a partnership, S corporation or trust withhold Illinois tax for you in %d?", y.year),
			"Any such withholding counts toward the quarter in which the entity's year ended.")
	}

	cum, err := estimated.CumulativeShares(a.requirement.Target(), quarters)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cum)
	a.notes.Rule("35 ILCS 5/803(d), 5/804(c)(1)(A); IL-2210 (R-12/25), lines 8 and 9a; IL-1040-ES (R-09/26), When are my payments due?", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s",
		a.requirement.Target(), a.installments[0], a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() {
		periods := f.Periods()
		taxes := make([]money.USCents, len(periods))
		for i, p := range periods {
			taxes[i] = p.AnnualisedTax()
		}
		ann, err := estimated.Annualised(taxes, y.applicable, a.installments)
		if err != nil {
			return assessment{}, err
		}
		a.installments = ann
		a.notes.Rule("35 ILCS 5/804(c)(2); IL-2210 (R-12/25), Step 6, lines 38 to 56", fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: %v, %v, %v and %v of the annualised tax after credits, "+
			"less earlier installments, and never more than the regular installment plus any reduction carried forward",
			ann[0], ann[1], ann[2], ann[3], y.applicable[0], y.applicable[1], y.applicable[2], y.applicable[3]))
	}

	// The withholding rules matter only to withholding, and the order it is
	// applied in only when a quarter is still short at a later due date.
	ledger, _, err := r.ledger(f, a)
	if err != nil {
		return assessment{}, err
	}
	if allWithheld > 0 {
		af.nonWage = withheld > 0 && !f.Elections().WithholdingAsWithheld()
		af.actualDates = withheld > 0 && f.Elections().WithholdingAsWithheld()
		for _, q := range ledger[:len(ledger)-1] {
			af.applying = af.applying || q.shortfall() > 0
		}
	}
	return a, r.diverges(&a, af)
}

// diverges adds the divergences to a's notes.
func (r illinoisEstimated) diverges(a *assessment, af affects) error {
	ds, err := r.divergences(af)
	if err != nil {
		return err
	}
	a.notes.Diverges(ds...)
	return nil
}

// quarter is one installment after IL-2210 has applied withholding and the
// estimated payments to it.
type quarter struct {
	number   int
	due      estimated.Date
	required money.USCents // line 9b
	withheld money.USCents // line 10c

	// unpaid is line 13 when positive: the installment less this quarter's
	// withholding and any excess carried from earlier quarters.
	unpaid money.USCents

	// pieces are the estimated payments that paid the unpaid amount, in
	// order; a piece with no date was never paid.
	pieces []piece

	creditedBy money.USCents
}

// shortfall is what was still unpaid on the due date.
func (q quarter) shortfall() money.USCents {
	s := q.unpaid
	for _, p := range q.pieces {
		if !p.paidOn.IsZero() && !p.paidOn.After(q.due) {
			s -= p.amount
		}
	}
	return s
}

// payment is an estimated payment as IL-2210 line 20 lists it.
type payment struct {
	on     estimated.Date
	amount money.USCents
}

// ledger applies withholding and payments to the installments as IL-2210 does.
//
// Withholding, IL-2210 lines 10a to 13: a quarter of the Illinois withholding
// on each due date, or by election what was withheld after the previous due
// date up to this one; and pass-through withholding in the quarter whose due
// date is the first on or after the entity's year end. Each quarter's
// withholding comes off that quarter's installment, and an excess carries to
// the next quarter, never back.
//
// Payments, Penalty Worksheet 1: estimated payments in date order, each to
// the oldest quarter with an unpaid amount, whatever quarter it was meant
// for. Last year's overpayment counts on the first due date, or on the day it
// was elected if that was late. A payment after every quarter is paid is not
// used.
func (r illinoisEstimated) ledger(f estimated.Facts, a assessment) ([]quarter, working.Notes, error) {
	var notes working.Notes
	n := len(a.dues)
	withheld := make([]money.USCents, n)
	asWithheld := f.Elections().WithholdingAsWithheld()
	if asWithheld {
		for _, w := range f.Withholding() {
			withheld[quarterOf(a.dues, w.On())] += w.Amount()
		}
		notes.Rule("35 ILCS 5/804(g)(1); IL-2210 Instructions (R-12/25), Line 10a", fmt.Sprintf("by election, backed by pay stubs or an employer letter, the %v withheld counts in the quarter in which it was withheld: %v, %v, %v and %v; IL-2210 must then be filed",
			f.WithheldTotal(), withheld[0], withheld[1], withheld[2], withheld[3]))
	} else {
		cum, err := estimated.CumulativeShares(f.WithheldTotal(), quarters)
		if err != nil {
			return nil, working.Notes{}, err
		}
		copy(withheld, estimated.Increments(cum))
		notes.Rule("35 ILCS 5/804(g)(1); IL-2210 Instructions (R-12/25), Line 10a", fmt.Sprintf("the %v of Illinois withholding is treated as paid in four equal parts, %v a quarter, whenever it was actually withheld", f.WithheldTotal(), withheld[0]))
	}
	pass, _ := a.extras.PassThroughWithholding()
	for _, p := range pass {
		i := quarterOf(a.dues, p.YearEnd())
		withheld[i] += p.Amount()
		notes.Rule("IL-2210 Instructions (R-12/25), Line 10b; 35 ILCS 5/804(g)(2)", fmt.Sprintf("%v of pass-through withholding for an entity year ending %s counts in full toward the %s installment", p.Amount(), p.YearEnd().Long(), a.dues[i].Long()))
	}

	var pays []payment
	priorYear := false
	late, lateAnswered := a.extras.OverpaymentElectedLate()
	for _, p := range f.Payments() {
		on := p.On()
		if p.FromPriorYear() {
			priorYear = true
			on = a.dues[0]
			if !late.IsZero() {
				on = late
			}
		}
		pays = append(pays, payment{on: on, amount: p.Amount()})
	}
	sort.SliceStable(pays, func(i, j int) bool { return pays[i].on.Before(pays[j].on) })
	if priorYear {
		if late.IsZero() {
			notes.Rule("IL-2210 Instructions (R-12/25), Line 20; IL-1040-ES (R-09/26), Estimated Tax Worksheet line 11", fmt.Sprintf("last year's overpayment applied to this year counts as paid on %s, as it does when elected by the extended due date of last year's return", a.dues[0].Long()))
		} else {
			notes.Rule("IL-2210 Instructions (R-12/25), Line 20; IL-1040-ES (R-09/26), Estimated Tax Worksheet line 11", fmt.Sprintf("last year's overpayment was elected after the extended due date of last year's return, so it counts as paid on %s, the day of the election", late.Long()))
		}
		if !lateAnswered {
			notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("When did you elect to apply last year's overpayment to %d?", r.y.year),
				fmt.Sprintf("It counts on %s if elected by your %d return's extended due date. Otherwise it counts on the day you elected it.",
					shortDate(a.dues[0], r.y.year), r.y.year-1))
		}
	}

	out := make([]quarter, n)
	var carry money.USCents
	for i := range out {
		line13 := a.installments[i] - withheld[i] - carry
		carry = 0
		if line13 < 0 {
			carry, line13 = -line13, 0
		}
		out[i] = quarter{number: i + 1, due: a.dues[i], required: a.installments[i], withheld: withheld[i], unpaid: line13}
	}
	for _, p := range pays {
		left := p.amount
		for i := 0; i < n && left > 0; i++ {
			owed := out[i].unpaid
			for _, pc := range out[i].pieces {
				owed -= pc.amount
			}
			if owed <= 0 {
				continue
			}
			paid := min(owed, left)
			out[i].pieces = append(out[i].pieces, piece{amount: paid, paidOn: p.on})
			left -= paid
		}
	}
	var runningWithheld money.USCents
	for i := range out {
		owed := out[i].unpaid
		for _, pc := range out[i].pieces {
			owed -= pc.amount
		}
		if owed > 0 {
			out[i].pieces = append(out[i].pieces, piece{amount: owed})
		}
		runningWithheld += out[i].withheld
		out[i].creditedBy = runningWithheld
		for _, p := range pays {
			if !p.on.After(out[i].due) {
				out[i].creditedBy += p.amount
			}
		}
	}
	notes.Rule("IL-2210 (R-12/25), Step 2 lines 10c to 13 and Penalty Worksheet 1; IL-1040-ES (R-09/26), What if I do not make my payments?",
		"each quarter's withholding comes off that quarter's installment, with any excess carried to the next; each estimated payment then goes to the oldest quarter still unpaid, in the order paid")
	return out, notes, nil
}

// quarterOf is the index of the first due date on or after d.
func quarterOf(dues []estimated.Date, d estimated.Date) int {
	for i, due := range dues {
		if !d.After(due) {
			return i
		}
	}
	return len(dues) - 1
}

// piece is part of a quarter's unpaid amount and the day a payment paid it.
// The zero Date means it was never paid.
type piece struct {
	amount money.USCents
	paidOn estimated.Date
}

// flatPart is the IL-2210 Penalty Worksheet 1 charge on one amount paid
// late: the low rate for up to lateDays days, the high rate after that or
// when never paid, rounded to the cent. It returns false for an amount paid
// on or before its due date.
func (y estimatedYear) flatPart(principal money.USCents, due, paidOrStop estimated.Date) (estimated.PenaltyPart, bool, error) {
	days := paidOrStop.DaysSince(due)
	if days <= 0 {
		return estimated.PenaltyPart{}, false, nil
	}
	rate := y.low
	if days > y.lateDays {
		rate = y.high
	}
	p, err := estimated.NewPenaltyPart(estimated.PartFlat, principal, rate).Span(due, paidOrStop).Days(days).
		Charge(rate.Of(principal)).Source(fmt.Sprintf("IL-2210 (R-12/25), Penalty rates: %d days late, %v; 35 ILCS 735/3-3(b-25)(1)", days, rate)).Build()
	return p, err == nil, err
}

func (r illinoisEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r illinoisEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Illinois return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
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

// schedule is the installments the ledger gives.
func schedule(ledger []quarter) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	var needed money.USCents
	for i, q := range ledger {
		needed += q.required
		in, err := estimated.NewInstallment(q.number, q.due).NeededBy(needed).CreditedBy(q.creditedBy).
			Shortfall(q.shortfall()).Build()
		if err != nil {
			return nil, err
		}
		out[i] = in
	}
	return out, nil
}

// Penalty is IL-2210 Penalty Worksheet 1 up to asOf, or to the day the return
// was filed and paid if that is earlier.
//
// Each amount paid late is charged once, at the low rate if it was paid
// within lateDays of its due date and the high rate otherwise. An amount
// still unpaid on the stop date is charged as if paid that day: the low rate
// while it is within lateDays, rising to the high rate after. No date stops
// the penalty earlier; IL-2210's worked example charges a payment made with
// a return filed after April 15.
//
// Interest is charged as well only when [EstimatedFacts] says so, because no
// Illinois document says whether it runs on a late installment. The
// penalty's [estimated.Penalty.Total] then includes it.
func (r illinoisEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("illinoistaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Illinois return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := asOf
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
	interest, interestAnswered := a.extras.InterestOnLateInstallments()

	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, sched)
	if !a.taxKnown {
		pb.UpperBound()
	}
	var unrated estimated.Date
	var flat, accrued money.USCents
	for _, q := range ledger {
		if q.shortfall() <= 0 || !stop.After(q.due) {
			continue
		}
		lb := estimated.NewPenaltyLine(q.number, q.due, q.shortfall())
		lastDay := q.due
		for _, pc := range q.pieces {
			to := stop
			if !pc.paidOn.IsZero() && pc.paidOn.Before(stop) {
				to = pc.paidOn
			}
			part, late, err := y.flatPart(pc.amount, q.due, to)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			if !late {
				continue
			}
			lb.Part(part)
			flat += part.Charge()
			if to.After(lastDay) {
				lastDay = to
			}
			if interest {
				parts, gap, err := estimated.SimpleDaily(pc.amount, q.due, to, y.interest)
				if err != nil {
					return working.Result[estimated.Penalty]{}, err
				}
				if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
					unrated = gap
				}
				for _, p := range parts {
					lb.Part(p)
					accrued += p.Charge()
				}
			}
		}
		line, err := lb.Days(lastDay.DaysSince(q.due)).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		pb.Line(line)
	}
	p, err := pb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}

	if paidInFull {
		notes.Rule("IL-2210 Instructions (R-12/25), Penalty Worksheet 1, Column F", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every amount still unpaid was paid that day", stop.Long()))
	}
	notes.Rule("35 ILCS 5/804(a); 35 ILCS 735/3-3(b-25)(1); IL-2210 (R-12/25), Penalty Worksheet 1", fmt.Sprintf("each amount paid after its due date is charged once, %v if paid 1 to %d days late and %v if paid later; "+
		"an amount unpaid on %s is charged as of that day; the penalty is %v, which line 22 rounds to whole dollars for Form IL-1040 line 34",
		y.low, y.lateDays, y.high, stop.Long(), flat))
	if len(p.Lines()) > 0 {
		if interest {
			notes.Rule("35 ILCS 735/3-2; Publication 103 (R-09/25), Interest; IDOR, Interest Rates", fmt.Sprintf("as asked, simple interest at the uniform rate is charged on each late amount for the days after its due date until paid, divided by the days in the year: %v, included in the total", accrued))
		}
		if !interestAnswered {
			notes.Ask(working.PositionJurisdictionFact, "Do you want interest on a late quarter counted on top of the penalty?",
				"Nothing Illinois publishes says if a late quarter bears interest. The Illinois penalty form figures none.")
		}
		if !waiverAnswered {
			notes.Ask(working.PositionPenaltyWaiver, "Do you claim reasonable cause, like a casualty or a federal disaster extension?",
				"A casualty counts if it destroyed your home or records. Until Illinois lifts the penalty, the figures include it in full.")
		}
	}
	if !a.taxKnown {
		notes.Rule("35 ILCS 5/804(c)(1)(B); IL-2210 (R-12/25), line 7", fmt.Sprintf("this year's Illinois tax was not given, so the penalty is figured against 100%% of the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower", y.year-1, p.Total()))
	}
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", shortDate(unrated.AddDays(-1), y.year)))
	}
	if o, asked := waiverOutcome(f); asked && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	switch waiver {
	case estimated.WaiverCasualty:
		notes.Rule("35 ILCS 5/804(e); 86 Ill. Adm. Code 700.400(e)(5)", "Illinois has no casualty waiver as such; a casualty can be reasonable cause, which the department decides on a detailed explanation")
	case estimated.WaiverRetiredOrDisabled:
		notes.Rule("35 ILCS 5/804(e); 86 Ill. Adm. Code 100.8010(g)", "Illinois has no exception for retirement or disability; the only route is reasonable cause, which the department decides")
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// waiverOutcome decides a waiver asked for. Illinois has no waiver for
// retirement or disability. A casualty or disaster can be reasonable cause:
// 86 Ill. Adm. Code 700.400(e)(5) names a fire or other casualty that
// destroyed the home or business records, (f)(3) and (4) weigh a natural
// disaster, and (e)(13) makes an IRS disaster extension reasonable cause
// for a payment made by the extended date. It reports false when no waiver
// was asked for.
func waiverOutcome(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	pos := f.Positions()
	kind, answered := pos.Waiver()
	switch {
	case !answered || kind == estimated.WaiverNone:
		return estimated.WaiverOutcome{}, false
	case kind == estimated.WaiverRetiredOrDisabled:
		return estimated.WaiverRefused("Illinois has no waiver for retirement or disability, only for reasonable cause. The Illinois Department of Revenue decides reasonable cause."), true
	case pos.WaiverCause() == estimated.CauseDisaster:
		return estimated.WaiverEligible("Paying Illinois within an IRS disaster extension is reasonable cause. The Illinois Department of Revenue decides when a disaster lifts the penalty."), true
	default:
		return estimated.WaiverEligible("Losing home or business records to fire or other casualty is reasonable cause. The Illinois Department of Revenue decides, and can lift the penalty."), true
	}
}

func (r illinoisEstimated) Explanation() estimated.Explanation { return illinoisExplanation }

// illinoisExplanation is written by hand and checked against
// research/estimated-payments/il.md.
var illinoisExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Illinois expects estimated payments if more than $1,000 will be due for 2026. The amount due is your tax after credits and withholding. A late amount costs a flat 2% if paid within 30 days, and 10% after that.").
		Paragraph("To avoid the penalty, withholding and estimated payments must reach a target. It is the lower of 90% of this year's tax after credits or 100% of last year's. There is no higher prior-year figure at high income. Last year's figure counts only if you filed a full-year return that showed tax.").
		Paragraph("The payments are four equal quarters. They are due April 15, June 15 and September 15, 2026, and January 15, 2027. A date on a weekend or holiday shifts to the next business day. Withholding is treated as paid in four equal parts. If pay stubs or an employer letter show when it was taken, it counts then. A partnership, S corporation or trust may withhold for you. That withholding counts in the quarter the entity's year ended. Each payment goes to the oldest unpaid quarter first.").
		Paragraph("The penalty is not interest. Each late amount is charged once. The charge is 2% within 30 days of the due date, and 10% if paid later or never. A week late costs little, but 31 days late costs as much as never paying. The department will work out the penalty and bill you if you prefer. Nothing it publishes says whether it also charges interest on a late quarter. Its penalty form works out none.").
		Paragraph("Tax after credits and withholding of $1,000 or less this year means no penalty. There's also no penalty if you did not have to file last year. The same holds if last year's return showed no tax due after withholding. Uneven income can lower the early quarters through the annualised method. That method uses 22.5%, 45%, 67.5% and 90% of the tax on your income to date. Otherwise the department lifts the penalty only for reasonable cause. A fire that destroyed your records is one example.").
		VersusFederal("Illinois charges a flat 2% or 10% of each late amount, not interest by the day. Past 30 days, the cost is the same however late you pay. There's no 110% figure at high income. Filing by January 31 doesn't waive the January quarter.").
		Source("Illinois Income Tax Act §§ 803, 804, 806 (35 ILCS 5/803, 5/804, 5/806); Uniform Penalty and Interest Act §§ 3-2, 3-3, 3-8 (35 ILCS 735)").
		Source("IL-1040-ES, Estimated Income Tax Payments for Individuals, 2026 (R-09/26)").
		Source("IL-2210, Computation of Penalties for Individuals, and its instructions, 2025 (R-12/25)").
		Source("Publication 103, Penalties and Interest for Illinois Taxes (R-09/25)").
		Source("86 Ill. Adm. Code 100.8000, 100.8010, 700.220, 700.400").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
