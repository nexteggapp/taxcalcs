package montanataxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Montana's estimated tax rules, Mont. Code Ann. § 15-30-2512 and
// § 15-1-216(7), as the shared [estimated.EstimatedTax] interface. Montana
// calls the charge interest on underpayment of estimated tax.
//
// Montana follows the federal shape: four 25% installments, the lesser of 90%
// of this year's tax or 100% of last year's, and payments applied to the
// earliest installment first. It differs in the $500 threshold, no 110% tier,
// a full exemption for farmers and ranchers and for anybody who retired after
// 62 or became disabled this year or last, and one rate for the whole tax
// year: the rate in effect when the return is due, so 2027's for 2026.
//
// The figure follows Form EST-I for 2025, the latest published, with the 2026
// dates, and the result's Divergences say where the statute reads otherwise.
// 2025 House Bill 863 added only § 15-30-2512(8), a waiver for tax year 2024,
// so the form does not predate the law it describes
// (research/estimated-payments/mt.md).
//
// Not built: the hardship extension of § 15-30-2512(7), which the department
// grants on a written request, and short tax years.

// jurisdiction is Montana's postal code.
const jurisdiction = estimated.Montana

// EstimatedFacts are the facts Montana's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	refundableCredits optional

	composite, compositeAnswered bool
	equal, equalAnswered         bool
}

type optional struct {
	v   money.USCents
	set bool
}

// RefundableCredits are this year's refundable credits, Form 2 lines 15 to
// 17. EST-I Part I line 3b counts them with withholding, a quarter on each
// date.
func (e EstimatedFacts) RefundableCredits() (money.USCents, bool) {
	return e.refundableCredits.v, e.refundableCredits.set
}

// CompositeReturn reports whether the filer takes part in a partnership's or
// S corporation's composite return for this year, which ARM 42.17.314(5)
// takes out of estimated tax, and whether the caller said.
func (e EstimatedFacts) CompositeReturn() (participant, answered bool) {
	return e.composite, e.compositeAnswered
}

// ApproximatelyEqualInstallments reports whether the filer paid
// approximately equal quarterly installments, which § 15-30-2512(5)(e)
// excuses from interest when the year's payments reach the required annual
// payment, and whether the caller said. Whether installments are
// approximately equal is a judgement the statute does not define.
func (e EstimatedFacts) ApproximatelyEqualInstallments() (equal, answered bool) {
	return e.equal, e.equalAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) RefundableCredits(a money.USCents) *EstimatedFactsBuilder {
	b.e.refundableCredits = optional{a, true}
	return b
}

func (b *EstimatedFactsBuilder) CompositeReturn(participant bool) *EstimatedFactsBuilder {
	b.e.composite, b.e.compositeAnswered = participant, true
	return b
}

func (b *EstimatedFactsBuilder) ApproximatelyEqualInstallments(equal bool) *EstimatedFactsBuilder {
	b.e.equal, b.e.equalAnswered = equal, true
	return b
}

// Build refuses negative refundable credits.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.refundableCredits.v < 0 {
		return EstimatedFacts{}, errors.New("montanataxcalc: refundable credits cannot be negative")
	}
	return b.e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is EST-I Part I line 4 and § 15-30-2512(1)(b)(i).
	threshold money.USCents

	thisYearShare, priorYearShare money.Rate
	shares                        []money.Rate
	annualised                    []money.Rate

	dues       []estimated.Date
	calendar   estimated.Calendar
	periodEnds []estimated.Date

	// fileAndPayBy is § 15-30-2512(5)(d)(ii): the last day of the month after
	// the tax year, before moving for a weekend or holiday.
	fileAndPayBy estimated.Date

	stop estimated.Date

	// daily is the factor EST-I prints for the rate in effect on the return's
	// due date, over every day a 2026 underpayment can run.
	daily estimated.FactorSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Montana's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return montanaEstimated{y}, nil
}

type montanaEstimated struct{ y estimatedYear }

type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents
	refundable   money.USCents
	excused      bool
	paidForYear  money.USCents
	extras       EstimatedFacts
	notes        working.Notes
}

func (r montanaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("montanataxcalc: the facts carry %T, not montanataxcalc.EstimatedFacts", s)
	}
}

func (r montanaEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("montanataxcalc: these are Montana's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("montanataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("montanataxcalc: EST-I Part III period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's form and publication and the
// statutes part ways on a rule every Montana computation uses. They are in
// research/estimated-payments/mt.md, checked there against both, except the
// daily factor, found reading EST-I against § 15-1-216(7).
func (r montanaEstimated) divergences(t touches) ([]working.Divergence, error) {
	equalReading := working.ReadingForm
	if t.equalAnswered {
		equalReading = working.ReadingStatute
	}
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The daily factor").
			Form("Each day is charged at a printed factor of 0.000192. That is 7% divided by 365, rounded to six places.",
				"Form EST-I (2025), Part II line 14 and 2026 Interest Rate").
			Statute("Interest is the federal underpayment rate for last year's third quarter, 7%. The department computes it daily at 0.019178%.",
				"Mont. Code Ann. § 15-1-216(7)(a)(i); Montana Department of Revenue, Interest and Penalties").
			Uses(working.ReadingForm).Affects(t.short),
		working.NewDivergence("No liability last year").
			Form("No estimated payments are required if last year's Montana liability was zero. That year's tax period must have covered 12 months.",
				"Form EST-I (2025) instructions, Exceptions to Interest").
			Statute("The exception also needs you to be a U.S. citizen or resident all that year.",
				"Mont. Code Ann. § 15-30-2512(1)(b)(ii)").
			Uses(working.ReadingForm).Affects(t.exemptBy == byNoLiability),
		working.NewDivergence("Not required to file last year").
			Form("A nonresident or part-year resident who didn't have to file last year is exempt. Publication 1 extends this to anybody not required to file.",
				"Form EST-I (2025) instructions, Exceptions to Interest; 2026 Publication 1, Who Must Pay Estimated Tax").
			Statute("The statute lists no such exception. A department rule grants it to nonresidents and part-year residents.",
				"Mont. Code Ann. § 15-30-2512(1)(b); ARM 42.17.314(1)").
			Uses(working.ReadingForm).Affects(t.notRequiredToFile && (t.exemptBy == byNone || t.exemptBy == byNoLiability || t.exemptBy == byNotRequired)),
		working.NewDivergence("When payments are required").
			Form("Publication 1 measures tax after withholding and nonrefundable credits. It requires payments when that is more than $500. Form EST-I stops only when it's under $500.",
				"2026 Publication 1, Who Must Pay Estimated Tax; Form EST-I (2025), Part I line 4").
			Statute("Payments aren't required when tax after credits and withholding is under $500. Exactly $500 requires them.",
				"Mont. Code Ann. § 15-30-2512(1)(b)(i)").
			Uses(working.ReadingForm).Affects(t.atThreshold && t.exemptBy == byNone),
		working.NewDivergence("Retirement after 62").
			Form("Publication 1 exempts a retiree at least 62 at the end of the previous tax year. It covers the year of retirement and one more. Form EST-I says retired and at least 62.",
				"2026 Publication 1, Who Must Pay Estimated Tax; Form EST-I (2025) instructions, Exceptions to Interest").
			Statute("Retiring after 62, or becoming disabled, exempts that tax year and the next.",
				"Mont. Code Ann. § 15-30-2512(1)(b)(iv)").
			Uses(working.ReadingStatute).Affects(t.exemptBy == byRetired),
		working.NewDivergence("Approximately equal installments").
			Form("The form lists no exception for approximately equal installments.",
				"Form EST-I (2025) instructions, Exceptions to Interest").
			Statute("Your withholding and estimated payments must meet the required annual payment. If you also paid approximately equal quarterly installments, no interest is due.",
				"Mont. Code Ann. § 15-30-2512(5)(e)").
			Uses(equalReading).Affects(t.short && t.paidTarget && (t.equal || !t.equalAnswered)),
		working.NewDivergence("The late payment penalty").
			Form("The form figures interest only.",
				"Form EST-I (2025), Part II").
			Statute("Estimated tax paid late is charged a late payment penalty as well as interest. It is 1.5% a month, up to 15%, from the return's original due date.",
				"Mont. Code Ann. § 15-1-216(2)(b), (c); ARM 42.17.306(3)").
			Uses(working.ReadingForm).Affects(t.short),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// exemption is the exception that took the filer out of estimated tax.
type exemption int

const (
	byNone exemption = iota
	byFarm
	byRetired
	byComposite
	byNoLiability
	byNotRequired
	byThreshold
)

// touches are the facts each divergence's readings turn on.
type touches struct {
	exemptBy          exemption
	notRequiredToFile bool
	atThreshold       bool
	short             bool
	paidTarget        bool
	equal             bool
	equalAnswered     bool
}

func (r montanaEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	a := assessment{extras: extras}
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	last := y.year - 1

	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	refundable, refundableKnown := extras.RefundableCredits()
	a.refundable = refundable
	credited := withheld + refundable
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
		a.notes.Rule("Mont. Code Ann. § 15-30-2512(1)(a)(i); Form EST-I (2025), Part I line 2", fmt.Sprintf("%v of this year's Montana tax after nonrefundable credits of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Montana return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Montana return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Montana return covered %d months, not a full year", last, prior.Months()))
	default:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, y.priorYearShare.Of(priorTax))
		a.notes.Rule("Mont. Code Ann. § 15-30-2512(1)(a)(ii); Form EST-I (2025), Part I line 5", fmt.Sprintf("the prior-year test is %v of the %v tax after nonrefundable credits on the %d return", y.priorYearShare, priorTax, last))
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

	// The exceptions that take the filer out of estimated tax.
	noLiability := hasPrior && prior.Months() == 12 && ((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	thisResident := len(f.Residence()) == 0 || estimated.ResidentAllYear(f.Residence(), jurisdiction, y.year)
	notRequired := hasPrior && prior.NotRequiredToFile() && !thisResident
	retired := false
	for _, p := range f.People() {
		if p.RetiredAfter62() || p.Disabled() {
			retired = true
		}
	}
	composite, _ := extras.CompositeReturn()
	var exemptBy exemption
	var exceptionErr error
	exempt := func(by exemption, exception estimated.TargetOption, why, cite, applied string) {
		a.exempt, exemptBy = true, by
		rb.Required(false, why).Exempt(exception)
		a.notes.Rule(cite, applied)
	}
	named := func(label string) estimated.TargetOption {
		o, err := estimated.NewException(label).Build()
		if err != nil && exceptionErr == nil {
			exceptionErr = err
		}
		return o
	}
	switch {
	case int64(f.FarmShare())*3 >= 20_000:
		exempt(byFarm, named("Two-thirds or more of your income from farming or ranching"),
			"At least two-thirds of your gross income is from farming or ranching. Montana requires no estimated payments from you and charges no interest.",
			"Mont. Code Ann. § 15-30-2512(1)(a), (6)", fmt.Sprintf("%v of gross income from farming or ranching is at least 66 2/3%%, so a farmer or rancher owes no estimated tax", f.FarmShare()))
	case retired:
		exempt(byRetired, named("Retired after reaching sixty-two, or disabled, this year or last"),
			fmt.Sprintf("You retired after 62 or became disabled in %d or %d. Montana requires no estimated payments for %d.", last, y.year, y.year),
			"Mont. Code Ann. § 15-30-2512(1)(b)(iv)", "a filer who retired after reaching 62 or became disabled this year or last owes no estimated tax")
	case composite:
		exempt(byComposite, named("Part of a composite return this year"),
			"You're part of a composite return this year, so you need no estimated payments.",
			"ARM 42.17.314(5)", "a participant in a composite return for the current year is not required to pay estimated tax")
	case noLiability:
		exempt(byNoLiability, named("No Montana tax due last year"),
			fmt.Sprintf("Your %d Montana tax period covered 12 months and your liability was zero. No estimated payments are required for %d.", last, y.year),
			"Mont. Code Ann. § 15-30-2512(1)(b)(ii); Form EST-I (2025) instructions, Exceptions to Interest", fmt.Sprintf("the %d return showed no liability for a full 12 months, so there is no interest for %d", last, y.year))
	case notRequired:
		exempt(byNotRequired, named("Not required to file a Montana return last year"),
			fmt.Sprintf("You aren't a full-year resident and didn't have to file a %d Montana return. No estimated payments are required.", last),
			"ARM 42.17.314(1); Form EST-I (2025) instructions, Exceptions to Interest", fmt.Sprintf("a nonresident or part-year resident not required to file for %d owes no estimated tax", last))
	case taxKnown && tax-credited < y.threshold:
		exception, err := estimated.ExceptionBelow("Tax less withholding and refundable credits", tax-credited, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		left := fmt.Sprintf("That leaves %s, under %s.", (tax - credited).Whole(), y.threshold.Whole())
		if tax < credited {
			left = fmt.Sprintf("That leaves a %s refund.", (credited - tax).Whole())
		}
		exempt(byThreshold, exception,
			fmt.Sprintf("Your Montana tax is %s; withholding and refundable credits are %s. %s No estimated payments are needed.",
				tax.Whole(), credited.Whole(), left),
			"Mont. Code Ann. § 15-30-2512(1)(b)(i); Form EST-I (2025), Part I line 4", fmt.Sprintf("%v of tax less %v of withholding and credits is %v, under %v, so there is no interest", tax, credited, tax-credited, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target needs your %d Montana tax or a full-year %d Montana return. There's no target yet.", y.year, last)).NoTarget()
	case !taxKnown && credited >= target:
		rb.Required(false, fmt.Sprintf("Your withholding and refundable credits are %s. They already reach %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			credited.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. It keeps you clear of interest whatever your %d tax turns out to be. Withholding and refundable credits cover %s of it. The other %s is due in estimated payments, a quarter by each due date.",
			priorOpt.Percent(), last, target.Whole(), y.year, credited.Whole(), (target-credited).Whole())).Choose(chosen)
	case credited >= target:
		rb.Required(false, fmt.Sprintf("Your Montana withholding and refundable credits are %s. They already reach the %s needed for the year. No estimated payments are needed.", credited.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Montana tax is %s; withholding and refundable credits are %s. That leaves %s, at least %s, so estimated payments are needed. You need %s of them, a quarter by each due date.",
			tax.Whole(), credited.Whole(), (tax-credited).Whole(), y.threshold.Whole(), (target-credited).Whole())).Choose(chosen)
	}
	if exceptionErr != nil {
		return assessment{}, exceptionErr
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// Under an exception neither return can change the target.
	if !hasPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Montana return, and what was its tax?", last),
			fmt.Sprintf("With it, 100%% of your %d tax could be a lower target than 90%% of this year's.", last))
	}
	if !taxKnown && !a.exempt {
		if priorOpt.Available() {
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Montana tax to be?", y.year),
				fmt.Sprintf("With it, 90%% of it could be a lower target than your %d tax. It also decides whether the %s threshold excuses you.", last, y.threshold.Whole()))
		} else {
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Montana tax to be?", y.year),
				fmt.Sprintf("Without it there's no target. There's no full-year %d Montana return to measure against.", last))
		}
	}
	if !a.exempt && hasPrior && prior.NotRequiredToFile() && len(f.Residence()) == 0 {
		a.notes.Ask(working.PositionResidency, fmt.Sprintf("Will you live in Montana all of %d?", y.year),
			fmt.Sprintf("If you won't, you need no estimated payments. That is because you didn't have to file a %d Montana return.", last))
	}
	if !a.exempt && !refundableKnown {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What Montana refundable credits do you expect for %d?", y.year),
			"They count with withholding, a quarter on each due date. Until then the figures count none.")
	}

	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	// § 15-30-2512(3)(b): a requirement that arises after an installment date
	// catches up at the first installment that applies.
	if met := f.RequirementFirstMet(); !met.IsZero() {
		for i := 0; i < len(a.dues)-1; i++ {
			if met.After(a.dues[i]) {
				cumulative[i] = 0
			}
		}
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("Mont. Code Ann. § 15-30-2512(2)(a), (3); Form EST-I (2025), Part II line 1", fmt.Sprintf("of %v, %v, %v, %v and %v are due on %s, %s, %s and %s", a.requirement.Target(),
		a.installments[0], a.installments[1], a.installments[2], a.installments[3], a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

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
		a.notes.Rule("Mont. Code Ann. § 15-30-2512(4); Form EST-I (2025), Part III", fmt.Sprintf("under the annualized method the installments are %v, %v, %v and %v: 22.5%%, 45%%, 67.5%% and 90%% of the annualized tax, "+
			"less earlier installments, and never more than the regular installment plus any reduction carried forward", ann[0], ann[1], ann[2], ann[3]))
	}

	// § 15-30-2512(5)(d). January 31, 2027 is a Sunday, a legal holiday in
	// Montana, so § 1-1-307 moves the day to Monday, February 1.
	fileBy := y.calendar.NextBusinessDay(y.fileAndPayBy)
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(fileBy) && !a.exempt {
		a.excused = true
		a.notes.Rule("Mont. Code Ann. §§ 15-30-2512(5)(d), 1-1-307; Form EST-I (2025), Lines 12 through 15", fmt.Sprintf("the return was filed and paid on %s, by %s, so the January installment bears no interest", filed.Long(), fileBy.Long()))
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	a.paidForYear = credited
	for _, p := range f.Payments() {
		a.paidForYear += p.Amount()
	}
	t := touches{exemptBy: exemptBy, notRequiredToFile: hasPrior && prior.NotRequiredToFile(),
		atThreshold: taxKnown && tax-credited == y.threshold, paidTarget: a.paidForYear >= a.requirement.Target()}
	t.equal, t.equalAnswered = extras.ApproximatelyEqualInstallments()
	if !a.exempt && a.requirement.TargetKnown() {
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return assessment{}, err
		}
		for _, l := range ledger {
			if l.Shortfall() > 0 {
				t.short = true
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

func (r montanaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// ledger applies withholding, refundable credits and payments to the
// installments, earliest first.
func (r montanaEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, r.y.shares)
	if err != nil {
		return nil, working.Notes{}, err
	}
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
	required := append([]money.USCents(nil), a.installments...)
	if a.excused {
		required[3] = 0
	}
	ledger, err := estimated.Apply(a.dues, required, credits)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	notes.Rule("Form EST-I (2025), Part II lines 2 and 12; Mont. Code Ann. § 15-30-2512(5)(c)",
		fmt.Sprintf("the %v withheld and the %v of refundable credits are treated as paid a quarter on each due date, and a payment goes first to the earliest unpaid installment", f.WithheldTotal(), a.refundable))
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("Form EST-I (2025), Part II line 2", "Montana has no election to credit withholding on the dates it was withheld, so it is spread evenly")
	}
	return ledger, notes, nil
}

func (r montanaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Montana return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	out, _, notes, err := r.schedule(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments and the ledger they come from.
func (r montanaEstimated) schedule(f estimated.Facts, a assessment) ([]estimated.Installment, []estimated.LedgerEntry, working.Notes, error) {
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return nil, nil, working.Notes{}, err
	}
	out := make([]estimated.Installment, len(ledger))
	var needed money.USCents
	for i, l := range ledger {
		needed += a.installments[i]
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(needed).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if a.excused && i == 3 {
			b.Excused()
		}
		if out[i], err = b.Build(); err != nil {
			return nil, nil, working.Notes{}, err
		}
	}
	return out, ledger, notes, nil
}

func (r montanaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("montanataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Montana return to measure against, there is no target to figure the interest from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	schedule, ledger, notes, err := r.schedule(f, a)
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

	// § 15-30-2512(5)(e): approximately equal installments that reach the
	// required annual payment owe nothing. EST-I does not list it.
	paidForYear := a.paidForYear
	equal, equalAnswered := a.extras.ApproximatelyEqualInstallments()
	if equal && paidForYear >= a.requirement.Target() {
		notes.Rule("Mont. Code Ann. § 15-30-2512(5)(e)", fmt.Sprintf("the year's %v reaches the %v required and, as the caller answered, was paid in approximately equal quarterly installments, so no interest is assessed",
			paidForYear, a.requirement.Target()))
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		return working.NewResult(p).With(a.notes).With(notes).Build()
	}

	// EST-I Part II lines 12 to 14: each part of each shortfall for the days
	// until it was paid, at the printed factor, in cents.
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	var unrated estimated.Date
	early := false
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
			parts, gap, err := estimated.DailyFactor(piece.Amount(), l.Due(), to, y.daily)
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
		if line.Total() > 0 && l.Number() < len(ledger) {
			early = true
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
		notes.Rule("Mont. Code Ann. § 15-30-2512(1)(a)", fmt.Sprintf("this year's Montana tax was not given, so the interest is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the interest can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	notes.Rule("Mont. Code Ann. §§ 15-30-2512(5), 15-1-216(7); ARM 42.2.505(5); Form EST-I (2025), Part II lines 12 to 15", fmt.Sprintf("each part of each shortfall is charged the daily factor for the rate in effect on the return's due date, from its due date until paid or %s; "+
		"the interest to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", unrated.AddDays(-1).Long()))
	}
	if o, ok := waiverOutcome(f); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if p.Total() > 0 {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("Montana hasn't published its %d rate, which applies to %d estimates. The figures use the law's: the federal rate for the third quarter of %d, 7%%.", y.year+1, y.year, y.year))
		notes.Ask(working.PositionUnsettledRule, "", "Montana's 1.5% a month late payment penalty may apply to a missed installment. No published source says whether it does, and the figures leave it out.")
		if !waiverAnswered {
			notes.Ask(working.PositionPenaltyWaiver, "Did a casualty, disaster or other unusual circumstance cause the shortfall?",
				"If so and you ask, Montana can waive the interest for good cause. Until it does, the figures include the full interest.")
		}
		if len(f.People()) == 0 {
			notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did you retire after reaching 62, or become disabled, in %d or %d?", y.year-1, y.year),
				"If you did, no estimated payments are needed and no interest is due.")
		}
		if _, answered := a.extras.CompositeReturn(); !answered {
			notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Are you part of a partnership's or S corporation's composite return for %d?", y.year),
				"If you are, no estimated payments are needed and no interest is due.")
		}
		if !equalAnswered && paidForYear >= a.requirement.Target() {
			notes.Ask(working.PositionJurisdictionFact, "Did you pay in approximately equal quarterly installments?",
				fmt.Sprintf("Your payments reach the %s needed. If they were about equal, Montana law charges no interest.", a.requirement.Target().Whole()))
		}
		if early && f.RequirementFirstMet().IsZero() {
			notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When in %d did your income first make estimated payments necessary?", y.year),
				"Until then, the figures assume you needed them from January 1. Montana lets you catch up at the next due date after that.")
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// waiverOutcome decides the waivers of § 15-30-2512(1)(b)(iii) and (iv).
// The window and the age are the federal ones: retiring after reaching 62,
// or becoming disabled, this year or last. Unlike federal law, that needs
// no reasonable cause. A casualty or disaster needs the department to find
// good cause.
func waiverOutcome(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	o, ok := estimated.StandardWaiver(f, "the Montana Department of Revenue", 62)
	if !ok || !o.Eligible() {
		return o, ok
	}
	p := f.Positions()
	kind, _ := p.Waiver()
	if kind == estimated.WaiverCasualty {
		what := "a casualty, disaster or other unusual event"
		switch p.WaiverCause() {
		case estimated.CauseCasualty:
			what = "a casualty"
		case estimated.CauseDisaster:
			what = "a disaster"
		}
		return estimated.WaiverEligible(fmt.Sprintf("The interest can be waived for %s. The Montana Department of Revenue decides if it is good cause for the shortfall.", what)), true
	}
	if p.WaiverCause() == estimated.CauseDisability {
		return estimated.WaiverEligible(fmt.Sprintf("You became disabled in %d. Montana charges no interest for the year you become disabled or the year after.", p.WaiverOn().Year())), true
	}
	return estimated.WaiverEligible(fmt.Sprintf("You retired in %d after reaching 62. Montana charges no interest for the year you retire or the year after.", p.WaiverOn().Year())), true
}

func (r montanaEstimated) Explanation() estimated.Explanation { return montanaExplanation }

// montanaExplanation is written by hand and checked against
// research/estimated-payments/mt.md.
var montanaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Montana expects estimated payments. They start once your tax after withholding and credits is $500 or more. Falling short costs interest, figured by the day on each quarter's shortfall.").
		Paragraph("Your target is 90% of this year's Montana tax or 100% of last year's. It is the smaller of the two. You are clear if withholding, refundable credits and estimated payments meet it. A quarter is due by April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. Last year's figure needs a full-year return. Withholding counts as paid evenly across the four dates. A payment goes first to the earliest quarter still short.").
		Paragraph("Two groups need no estimated payments at all. One is anyone who retired after 62 or became disabled this year or last. The other is anyone with no Montana liability on last year's full-year return. Filing and paying in full by January 31 clears the January payment's interest. In 2027 that day is a Sunday, so Monday, February 1 counts.").
		Paragraph("Interest runs from each due date until the shortfall is paid. It stops on April 15 of the next year. Montana uses one rate all year, the rate in effect when the return is due. For 2026 estimates that is the 2027 rate, 7%. It is the federal rate for the third quarter of 2026. An annualized method can cut early payments when income comes late in the year.").
		VersusFederal("Montana's threshold is $500 rather than $1,000, and there is no 110% tier. Retiring after 62 or becoming disabled exempts you outright, not by waiver. The rate is set once for the year from the return's due date, not each quarter. A January 31 that falls on a weekend moves to the next business day.").
		Source("Mont. Code Ann. §§ 15-30-2512, 15-1-216, 1-1-216, 1-1-307").
		Source("Form EST-I, 2025 Interest on Underpayment of Estimated Tax, and its instructions").
		Source("2026 Montana Publication 1, Withholding and Estimated Payments").
		Source("ARM 42.17.314, 42.17.306, 42.2.505").
		Source("Montana Department of Revenue, Interest and Penalties; Rev. Rul. 2026-10").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
