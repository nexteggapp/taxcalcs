package georgiataxcalc

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Georgia's estimated tax rules for individuals, O.C.G.A. §§ 48-7-114 to
// 48-7-120, as the shared [estimated.EstimatedTax] interface.
//
// Georgia differs from the federal rules in shape. Whether payments are
// required turns on gross income, not on tax. The current-year test is 70% of
// this year's tax, and the prior-year test is 100% of last year's at every
// income. Each payment goes first to the installment of its own period, and
// only what is left over pays an earlier shortfall. Three exceptions excuse an
// installment when the payments to its date reach them. The penalty is a
// fixed 9% a year, by the day.
//
// The statute is published officially only through LexisNexis, and could not
// be read (research/estimated-payments/ga.md). Every rule here comes from the
// Department of Revenue's Form 500 UET (Rev. 07/15/25) and Form 500-ES (Rev.
// 05/12/25), and the 2026 figures from HB 463 (2026). With no statute text
// there is no form-against-statute divergence to report except where an act
// postdates the form.

// jurisdiction is Georgia's postal code.
const jurisdiction = estimated.Georgia

// EstimatedFacts are the facts Georgia's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
//
// They are for the test of whether estimated tax is required at all, which
// compares gross income ([estimated.Figures.GrossIncome]) with the dependent
// exemptions, the estimated deductions and $1,000.
type EstimatedFacts struct {
	dependents    int
	dependentsSet bool
	deductions    money.USCents
	deductionsSet bool
}

// Dependents is the number of dependents the filer will claim an exemption
// for on Form 500, and whether the caller said.
func (e EstimatedFacts) Dependents() (int, bool) { return e.dependents, e.dependentsSet }

// Deductions are the filer's estimated deductions for the year, the standard
// deduction or the itemised total, and whether the caller said.
func (e EstimatedFacts) Deductions() (money.USCents, bool) { return e.deductions, e.deductionsSet }

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) Dependents(n int) *EstimatedFactsBuilder {
	b.e.dependents, b.e.dependentsSet = n, true
	return b
}

func (b *EstimatedFactsBuilder) Deductions(a money.USCents) *EstimatedFactsBuilder {
	b.e.deductions, b.e.deductionsSet = a, true
	return b
}

// Build refuses a negative count of dependents or negative deductions.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.dependents < 0 || b.e.deductions < 0 {
		return EstimatedFacts{}, errors.New("georgiataxcalc: dependents and deductions cannot be negative")
	}
	return b.e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// rate is the income tax rate, O.C.G.A. § 48-7-20(a.1), which Exceptions
	// 1 and 3 apply to income.
	rate money.Rate

	// thisYearShare and priorYearShare are Form 500 UET lines 5 and 4.
	thisYearShare, priorYearShare money.Rate

	// unwithheld is the $1,000 of income not subject to withholding in the
	// test of whether estimated tax is required.
	unwithheld money.USCents

	// exemption is the deduction for each dependent, and standard the
	// standard deduction by filing status, O.C.G.A. § 48-7-26.
	exemption money.USCents
	standard  map[filing.Status]money.USCents

	// formExemption and formStandard are the 2025 figures Form 500-ES still
	// prints, for the divergence.
	formExemption money.USCents
	formStandard  map[filing.Status]money.USCents

	dues []estimated.Date

	// laterGroups are the first days of Form 500 UET's later rows: a
	// requirement first met on or after laterGroups[i] starts at installment
	// i+2.
	laterGroups []estimated.Date

	calendar     estimated.Calendar
	fileAndPayBy estimated.Date
	stop         estimated.Date
	periodEnds   []estimated.Date

	// annualisedShare and actualShare are Exceptions 2 and 3: 70% of the tax
	// on annualised income, and 90% of the tax on the income to date.
	annualisedShare, actualShare money.Rate

	// rates is the 9% a year Form 500 UET line 20 charges.
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters is Form 500 UET line 9: withholding is treated as paid equally
// each quarter. They are also the cumulative shares used for Exceptions 1
// and 2, which the form does not state beyond the first quarter.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Georgia's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return georgiaEstimated{y}, nil
}

type georgiaEstimated struct{ y estimatedYear }

type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// required is each installment's own amount: the target divided by the
	// number of installments, from the first one the requirement applies to.
	required []money.USCents
	first    int

	// exception1, exception2 and exception3 are the cumulative amounts that
	// excuse an installment, or nil where the exception is not open.
	// Exception 3 never applies to the fourth.
	exception1, exception2, exception3 []money.USCents

	// paidByJanuary31 is Form 500 UET Instruction F: the balance paid with a
	// return filed by January 31 counts as paid on January 15.
	paidByJanuary31 bool

	notes working.Notes
}

func (r georgiaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("georgiataxcalc: the facts carry %T, not georgiataxcalc.EstimatedFacts", s)
	}
}

func (r georgiaEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("georgiataxcalc: these are Georgia's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("georgiataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: farmers and fishermen file by March 1 or are charged on the last quarter alone under Form 500 UET Instruction G, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("georgiataxcalc: Form 500 UET period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where a Department of Revenue form and the law part ways.
// The statute could not be read, so the only one found is a form printed
// before the act that changed its figures (research/estimated-payments/ga.md).
// The figures follow the act. affects is whether the form's figures would
// decide the requirement differently.
func (r georgiaEstimated) divergences(affects bool) ([]working.Divergence, error) {
	y := r.y
	d, err := working.NewDivergence("The deductions in the test of whether estimated tax is required").
		Form(fmt.Sprintf("The voucher form listed for %d prints the %d standard deduction of %s. It shows %s for a joint return and a dependent exemption of %s. It predates HB 463 and is out of date for %d.",
			y.year, y.year-1, y.formStandard[filing.Single].Whole(), y.formStandard[filing.MarriedFilingJointly].Whole(), y.formExemption.Whole(), y.year),
			"Form 500-ES (Rev. 05/12/25), Standard Deduction and Exemption Amount for Tax Year 2025").
		Statute(fmt.Sprintf("From tax year %d, the standard deduction is %s. It is %s for a joint return, and the dependent exemption is %s. The act was signed on May 11, 2026.",
			y.year, y.standard[filing.Single].Whole(), y.standard[filing.MarriedFilingJointly].Whole(), y.exemption.Whole()),
			"HB 463 (2026), §§ 2-2, 2-3 and 5-1, amending O.C.G.A. § 48-7-26; Governor's press release of May 11, 2026").
		Uses(working.ReadingStatute).Affects(affects).
		Build()
	if err != nil {
		return nil, err
	}
	return []working.Divergence{d}, nil
}

func (r georgiaEstimated) assess(f estimated.Facts) (assessment, error) {
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
	prior, hasPrior := f.PriorYear()

	// Form 500 UET line 5: 70% of this year's tax after credits.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("Form 500 UET (Rev. 07/15/25), lines 3 and 5", fmt.Sprintf("%v of this year's Georgia tax after credits of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// Form 500 UET line 4: 100% of last year's tax, from a 12-month return.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	var priorFigures estimated.Figures
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Georgia return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Georgia return", last))
		a.notes.Ask(working.PositionUnsettledRule, "",
			fmt.Sprintf("The form doesn't say if the %v option is open with no %d Georgia return. It asks only for a 12-month return, and the figures treat the option as closed.", y.priorYearShare, last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d return covered %d months, not 12", last, prior.Months()))
	default:
		priorFigures, _ = prior.Figures()
		priorTax, _ := priorFigures.Tax()
		amount := y.priorYearShare.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, amount)
		a.notes.Rule("Form 500 UET (Rev. 07/15/25), line 4", fmt.Sprintf("%v of the %v tax on the %d return is %v; Georgia has no higher percentage at higher incomes", y.priorYearShare, priorTax, last, amount))
		a.notes.Ask(working.PositionUnsettledRule, "",
			fmt.Sprintf("The form doesn't say if the %v option uses %d tax before or after credits. The figures use the tax your return shows.", y.priorYearShare, last))
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
	// Whether estimated tax is required at all: gross income over the
	// dependent exemptions, the estimated deductions and $1,000.
	gross, grossKnown := f.ThisYear().GrossIncome()
	threshold, formThreshold := r.threshold(f, extras, &a.notes)
	notRequired := grossKnown && gross <= threshold
	divergences, err := r.divergences(grossKnown && notRequired != (gross <= formThreshold))
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)

	switch {
	case notRequired:
		a.exempt = true
		under, err := estimated.ExceptionAtMost("Gross income", gross, threshold)
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your expected gross income of %s is no more than %s. That is your exemptions, deductions and %s together. So Georgia doesn't require estimated tax for %d.",
			gross.Whole(), threshold.Whole(), y.unwithheld.Whole(), y.year)).Exempt(under)
		a.notes.Rule("Form 500-ES (Rev. 05/12/25), Who Must File Estimated Tax; O.C.G.A. § 48-7-114", fmt.Sprintf("gross income of %v does not exceed %v, so no estimated tax is required", gross, threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("With no %d Georgia return, there is no target until your %d tax is figured.", last, y.year)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Withholding of %s already reaches %v of your %d Georgia tax, %s. So no estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), y.priorYearShare, last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d Georgia tax, %s. It keeps you clear of a penalty whatever your %d tax turns out to be. After %s withheld, you pay %s in equal estimated installments.",
			y.priorYearShare, last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your %s of Georgia withholding reaches the %s needed for the year. So no estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		of := fmt.Sprintf("%v of this year's Georgia tax", y.thisYearShare)
		if priorOpt.Available() {
			of = fmt.Sprintf("the smaller of %v of this year's Georgia tax and %v of last year's", y.thisYearShare, y.priorYearShare)
		}
		rb.Required(true, fmt.Sprintf("Your target for the year is %s. It is %s. After %s withheld, you pay %s in equal estimated installments.",
			target.Whole(), of, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// These answers change the target. An exception sets the target, so they
	// are asked only when none applies.
	if !a.exempt && !hasPrior {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Georgia return, and what was its tax?", last),
			fmt.Sprintf("It could make %v of last year's tax your target, if under %v of this year's.", y.priorYearShare, y.thisYearShare))
	}
	if !a.exempt && !taxKnown {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Georgia tax to be?", y.year),
			map[bool]string{true: "Until then, your target can only come from last year's tax.", false: "Until then, there's no target."}[a.requirement.TargetKnown()])
	}
	if a.exempt {
		a.required = make([]money.USCents, len(a.dues))
		return a, nil
	}
	if !grossKnown {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What do you expect your %d gross income to be?", y.year),
			fmt.Sprintf("Estimated tax starts above %s, your exemptions, deductions and %s.", threshold.Whole(), y.unwithheld.Whole()))
	}

	// The installments: the target in equal parts from the first installment
	// the requirement applies to.
	a.first = r.lateStart(f, &a)
	n := len(a.dues) - a.first
	cum, err := estimated.EqualShares(a.requirement.Target(), len(a.dues), a.first)
	if err != nil {
		return assessment{}, err
	}
	a.required = estimated.Increments(cum)
	a.notes.Rule("Form 500 UET (Rev. 07/15/25), lines 6 to 8 and Instruction B; Form 500-ES (Rev. 05/12/25)", fmt.Sprintf("the %v target is due in %d equal installments from %s", a.requirement.Target(), n, a.dues[a.first].Long()))

	// The exceptions, Form 500 UET lines 13 to 16 and Instruction D.
	if err := r.exceptions(f, &a, priorFigures, priorOpt.Available()); err != nil {
		return assessment{}, err
	}

	// Instruction F: the balance paid with a return filed by January 31
	// counts as paid on January 15. January 31, 2027 is a Sunday; the form
	// says nothing of moving it, so it is not moved.
	if filed := f.ReturnFiled(); !filed.IsZero() {
		switch {
		case !filed.After(y.fileAndPayBy):
			a.paidByJanuary31 = true
			a.notes.Rule("Form 500 UET (Rev. 07/15/25), Instruction F", fmt.Sprintf("the return was filed and paid on %s, by %s, so the balance paid with it counts as paid on %s", filed.Long(), y.fileAndPayBy.Long(), a.dues[3].Long()))
		case filed == y.calendar.NextBusinessDay(y.fileAndPayBy):
			a.notes.Ask(working.PositionUnsettledRule, "",
				fmt.Sprintf("The form doesn't say if %s, a %v, moves to the next workday. The figures keep it: a balance paid on %s isn't paid by %s.",
					short(y.fileAndPayBy, y.year), y.fileAndPayBy.Weekday(), short(filed, y.year), short(a.dues[3], y.year)))
		}
	}
	return a, nil
}

// threshold is the dependent exemptions, the estimated deductions and $1,000:
// what gross income must exceed before estimated tax is required. form is the
// same total with the 2025 figures Form 500-ES prints.
//
// The working goes in n whether or not the household is exempt, because an
// exemption can rest on a default it reports.
func (r georgiaEstimated) threshold(f estimated.Facts, e EstimatedFacts, n *working.Notes) (total, form money.USCents) {
	y := r.y
	deductions, known := e.Deductions()
	formDeductions := deductions
	if !known {
		std, listed := y.standard[f.FilingStatus()]
		formStd, formListed := y.formStandard[f.FilingStatus()]
		if !formListed {
			formStd = y.formStandard[filing.Single]
		}
		if !listed {
			std = y.standard[filing.Single]
			n.Ask(working.PositionJurisdictionFact, fmt.Sprintf("How much do you expect to deduct for %d?", y.year),
				fmt.Sprintf("Georgia's law sets no standard deduction for your filing status. Until then, the test for estimated payments uses the %s single amount.", std.Whole()))
		} else {
			n.Ask(working.PositionJurisdictionFact, fmt.Sprintf("How much do you expect to deduct for %d?", y.year),
				fmt.Sprintf("Until then, the estimated tax test uses the %s standard deduction.", std.Whole()))
		}
		deductions, formDeductions = std, formStd
	}
	dependents, known := e.Dependents()
	if !known {
		n.Ask(working.PositionJurisdictionFact, fmt.Sprintf("How many dependents will you claim for %d?", y.year),
			fmt.Sprintf("Each adds %s to the income you can have before estimated tax is required.", y.exemption.Whole()))
	}
	exemptions := money.USCents(dependents) * y.exemption
	total = deductions + exemptions + y.unwithheld
	form = formDeductions + money.USCents(dependents)*y.formExemption + y.unwithheld
	n.Rule("Form 500-ES (Rev. 05/12/25), Who Must File Estimated Tax; HB 463 (2026), §§ 2-2, 2-3", fmt.Sprintf("estimated tax is required when gross income exceeds %v of deductions, %v of exemptions for %d dependents and %v, together %v",
		deductions, exemptions, dependents, y.unwithheld, total))
	return total, form
}

// lateStart returns the installment the requirement first applies to, from
// Form 500 UET Instruction B: a requirement first met from April 1 starts in
// June, from June 1 in September, and from September 1 in January.
func (r georgiaEstimated) lateStart(f estimated.Facts, a *assessment) int {
	firstMet := f.RequirementFirstMet()
	if firstMet.IsZero() {
		a.notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first expect to need estimated payments for %d?", r.y.year),
			fmt.Sprintf("Until then, the figures assume you could expect to pay from Jan 1. Starting on or after %s means fewer, larger installments.", short(r.y.laterGroups[0], r.y.year)))
		return 0
	}
	first := estimated.LateStart(firstMet, r.y.laterGroups)
	if first > 0 {
		a.notes.Rule("Form 500 UET (Rev. 07/15/25), Instruction B", fmt.Sprintf("the requirement was first met on %s, so %d installments are due, the first on %s", firstMet.Long(), len(r.y.dues)-first, r.y.dues[first].Long()))
	}
	return first
}

// exceptions fills in the amounts that excuse an installment.
func (r georgiaEstimated) exceptions(f estimated.Facts, a *assessment, pf estimated.Figures, priorUsable bool) error {
	y := r.y
	last := y.year - 1

	// Exception 1: last year's income at this year's rate.
	if priorUsable {
		if income, ok := pf.TaxableIncome(); ok {
			ex := y.rate.Of(income)
			cum, err := estimated.CumulativeShares(ex, quarters)
			if err != nil {
				return err
			}
			a.exception1 = cum
			a.notes.Rule("Form 500 UET (Rev. 07/15/25), line 14 and Instruction D, Exception 1", fmt.Sprintf("the %d taxable income of %v at the %d rate of %v is %v; an installment is excused when the payments to its date reach %v, %v, %v or %v",
				last, income, y.year, y.rate, ex, cum[0], cum[1], cum[2], cum[3]))
			a.notes.Ask(working.PositionUnsettledRule, "",
				fmt.Sprintf("The form's Exception 1 uses your %d income at current rates and exemptions. It omits each installment's share; the figures use a quarter for each so far.", last))
		} else {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("What was your %d Georgia taxable income?", last),
				"With it, Exception 1 taxes it at this year's rate and can excuse an installment.")
		}
	}

	if !f.Elections().Annualised() {
		return nil
	}
	periods := f.Periods()
	// Exception 2: 70% of the tax on annualised income, by a quarter for
	// each installment so far.
	a.exception2 = make([]money.USCents, len(periods))
	// Exception 3: 90% of the tax on the income to date, for the first three.
	a.exception3 = make([]money.USCents, len(periods)-1)
	var running money.Rate
	for i, p := range periods {
		running += quarters[i]
		a.exception2[i] = running.Of(y.annualisedShare.Of(p.AnnualisedTax()))
		if i < len(a.exception3) {
			a.exception3[i] = y.actualShare.Of(y.rate.Of(p.Income()))
		}
	}
	a.notes.Rule("Form 500 UET (Rev. 07/15/25), line 15 and Instruction D, Exception 2", fmt.Sprintf("an installment is excused when the payments to its date reach %v of the tax on the annualised income, by a quarter for each installment so far: %v, %v, %v and %v",
		y.annualisedShare, a.exception2[0], a.exception2[1], a.exception2[2], a.exception2[3]))
	a.notes.Rule("Form 500 UET (Rev. 07/15/25), line 16 and Instruction D, Exception 3", fmt.Sprintf("the first three installments are also excused when the payments to their dates reach %v of the tax at %v on the taxable income to the period's end: %v, %v and %v; "+
		"the exception does not apply to the fourth", y.actualShare, y.rate, a.exception3[0], a.exception3[1], a.exception3[2]))
	a.notes.Ask(working.PositionUnsettledRule, "",
		"The form doesn't say how much of Exception 2's 70% each later installment needs. The figures follow its example's 25% for the first, then 50%, 75% and 100%.")
	return nil
}

// piece is part of an installment's shortfall and the day it was paid, or
// the zero Date if it never was.
type piece struct {
	amount money.USCents
	paidOn estimated.Date
}

// entry is one installment after the credits have been applied.
type entry struct {
	due        estimated.Date
	required   money.USCents
	creditedBy money.USCents
	shortfall  money.USCents
	pieces     []piece
}

// ledger applies credits as Form 500 UET lines 9 to 12 do. A credit counts
// first toward the installment of its own period, the days after the last
// due date up to and including this one. What is left of it pays the oldest
// earlier shortfall still outstanding, on the credit's date, and then carries
// forward as line 10's overpayment. Credits after the last due date pay what
// is outstanding.
//
// This differs from [estimated.Apply], which pays the oldest shortfall first
// whichever period a credit falls in.
func ledger(dues []estimated.Date, required []money.USCents, credits []estimated.Payment) []entry {
	cs := append([]estimated.Payment(nil), credits...)
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].On().Before(cs[j].On()) })
	out := make([]entry, len(dues))
	for i := range dues {
		out[i] = entry{due: dues[i], required: required[i]}
	}
	owed := func(i int) money.USCents {
		paid := money.USCents(0)
		for _, p := range out[i].pieces {
			paid += p.amount
		}
		return out[i].shortfall - paid
	}
	payOlder := func(before int, amount money.USCents, on estimated.Date) money.USCents {
		for i := 0; i < before && amount > 0; i++ {
			if o := owed(i); o > 0 {
				p := min(o, amount)
				out[i].pieces = append(out[i].pieces, piece{amount: p, paidOn: on})
				amount -= p
			}
		}
		return amount
	}
	var carry, credited money.USCents
	next := 0
	for i, due := range dues {
		covered := min(carry, required[i])
		carry -= covered
		for next < len(cs) && !cs[next].On().After(due) {
			c := cs[next]
			next++
			credited += c.Amount()
			take := min(c.Amount(), required[i]-covered)
			covered += take
			carry += payOlder(i, c.Amount()-take, c.On())
		}
		out[i].creditedBy = credited
		out[i].shortfall = required[i] - covered
	}
	for ; next < len(cs); next++ {
		payOlder(len(dues), cs[next].Amount(), cs[next].On())
	}
	for i := range out {
		if o := owed(i); o > 0 {
			out[i].pieces = append(out[i].pieces, piece{amount: o})
		}
	}
	return out
}

func (r georgiaEstimated) ledger(f estimated.Facts, a assessment) ([]entry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, quarters)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	if a.paidByJanuary31 {
		// The balance paid with the return counts as paid on January 15:
		// enough to bring what was credited by then to what was required.
		var need, by money.USCents
		for _, x := range a.required {
			need += x
		}
		for _, c := range credits {
			if !c.On().After(a.dues[3]) {
				by += c.Amount()
			}
		}
		if need > by {
			deemed, err := estimated.NewPayment().On(a.dues[3]).Amount(need - by).Build()
			if err != nil {
				return nil, working.Notes{}, err
			}
			credits = append(credits, deemed)
			notes.Rule("Form 500 UET (Rev. 07/15/25), Instruction F", fmt.Sprintf("%v of the balance paid with the return is counted as paid on %s", need-by, a.dues[3].Long()))
		}
	}
	notes.Rule("Form 500 UET (Rev. 07/15/25), lines 9 to 13", fmt.Sprintf("the %v withheld is treated as paid equally each quarter, whenever it was actually withheld; "+
		"a payment counts first toward the installment of its own period, and only what is left over pays an earlier shortfall", f.WithheldTotal()))
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("Form 500 UET (Rev. 07/15/25), line 9", "Georgia has no election to credit withholding on the dates it was withheld, so it is spread equally")
	}
	return ledger(a.dues, a.required, credits), notes, nil
}

// standing is where an installment stands once the exceptions are applied:
// the exception that covers it, if any, and whether its own shortfall bears
// a penalty.
type standing struct {
	why     string
	charged bool
}

func (a assessment) standings(l []entry) []standing {
	out := make([]standing, len(l))
	for i, e := range l {
		var s standing
		switch {
		case a.exception1 != nil && e.creditedBy >= a.exception1[i]:
			s.why = "Exception 1: the payments reached last year's income at this year's rate"
		case a.exception2 != nil && e.creditedBy >= a.exception2[i]:
			s.why = "Exception 2: the payments reached 70% of the tax on the annualised income"
		case i < len(a.exception3) && e.creditedBy >= a.exception3[i]:
			s.why = "Exception 3: the payments reached 90% of the tax on the income to date"
		}
		s.charged = e.shortfall > 0 && s.why == ""
		out[i] = s
	}
	return out
}

// schedule is the installments, with the status [estimated.StatusOf] their
// running totals, excused where an exception covers them.
func (a assessment) schedule(l []entry) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(l))
	var needed money.USCents
	for i, s := range a.standings(l) {
		needed += l[i].required
		b := estimated.NewInstallment(i+1, l[i].due).NeededBy(needed).CreditedBy(l[i].creditedBy).Shortfall(l[i].shortfall)
		if s.why != "" {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r georgiaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r georgiaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Georgia return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	l, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := a.schedule(l)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	for i, s := range a.standings(l) {
		if s.why != "" && l[i].shortfall > 0 {
			notes.Rule("Form 500 UET (Rev. 07/15/25), Instruction D", fmt.Sprintf("the %s installment was %v short and bears no penalty under %s", l[i].due.Long(), l[i].shortfall, s.why))
		}
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

func (r georgiaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("georgiataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Georgia return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	l, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule, err := a.schedule(l)
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
	laterExcess := false
	for i, s := range a.standings(l) {
		e := l[i]
		if !s.charged {
			continue
		}
		lb := estimated.NewPenaltyLine(i+1, e.due, e.shortfall)
		lastDay := e.due
		for _, p := range e.pieces {
			to := stop
			if !p.paidOn.IsZero() {
				laterExcess = true
				if p.paidOn.Before(stop) {
					to = p.paidOn
				}
			}
			if !to.After(e.due) {
				continue
			}
			if to.After(lastDay) {
				lastDay = to
			}
			parts, _, err := estimated.SimpleDaily(p.amount, e.due, to, y.rates)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			for _, part := range parts {
				lb.Part(part)
			}
		}
		line, err := lb.Days(lastDay.DaysSince(e.due)).Build()
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
		notes.Rule("Form 500 UET (Rev. 07/15/25), lines 4 to 8", fmt.Sprintf("this year's tax was not given, so the penalty is figured against 100%% of the %d tax alone; "+
			"the required payment is the lesser of that and 70%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower", y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("Form 500 UET (Rev. 07/15/25), line 18", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	if laterExcess {
		notes.Ask(working.PositionUnsettledRule, "",
			"The form doesn't say which payment pays off an earlier shortfall. The figures pay it when a later period first pays more than its own installment.")
	}
	notes.Rule("Form 500 UET (Rev. 07/15/25), lines 17 to 21; IT-511 (2025)", fmt.Sprintf("each underpayment no exception covers is charged 9%% a year, by the day, from its due date to the date paid or %s, whichever is earlier; "+
		"the penalty to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if waiverAnswered && waiver != estimated.WaiverNone {
		notes.Rule("Form 500 UET (Rev. 07/15/25)", "Georgia's forms have no waiver for a casualty, retirement or disability; the Department grants disaster relief by announcement, so the penalty shown is before any such relief")
		if p.Total() > 0 {
			o := georgiaWaiver(waiver, f.Positions().WaiverCause())
			if o.Undecided() {
				notes.Ask(working.PositionUnsettledRule, "",
					"Georgia postpones due dates for a disaster only if the Department announces it.")
			}
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// georgiaWaiver decides the waiver asked for. Georgia's forms have no
// waiver for a casualty, retirement or disability. For a disaster the
// Department postpones due dates by announcement, so a disaster, or a
// casualty that may be one, is left open.
func georgiaWaiver(k estimated.WaiverKind, c estimated.WaiverCause) estimated.WaiverOutcome {
	switch {
	case k == estimated.WaiverRetiredOrDisabled:
		return estimated.WaiverRefused("Georgia's forms have no waiver of the penalty for retirement or disability.")
	case c == estimated.CauseCasualty:
		return estimated.WaiverRefused("Georgia's forms have no waiver of the penalty for a casualty.")
	case c == estimated.CauseDisaster:
		return estimated.WaiverUndecided("Georgia has no disaster waiver. The Department postpones due dates for a disaster when it announces relief.")
	default:
		return estimated.WaiverUndecided("Georgia has no casualty waiver. The Department postpones due dates for a disaster when it announces relief.")
	}
}

// short writes a date as a person reads it: "Apr 1" in the tax year, and
// "Jan 31, 2027" outside it.
func short(d estimated.Date, year int) string {
	if d.Year() == year {
		return fmt.Sprintf("%s %d", d.Month().String()[:3], d.Day())
	}
	return fmt.Sprintf("%s %d, %d", d.Month().String()[:3], d.Day(), d.Year())
}

func (r georgiaEstimated) Explanation() estimated.Explanation { return georgiaExplanation }

// georgiaExplanation is written by hand and checked against
// research/estimated-payments/ga.md.
var georgiaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Georgia expects estimated payments on income that tax isn't withheld from. It charges 9% a year on each installment you underpay. Its safe harbours are lower than federal. They are 70% of this year's tax or 100% of last year's.").
		Paragraph("Whether you have to pay at all turns on income, not on tax. Estimated tax is required when expected gross income exceeds three amounts. They are your dependent exemptions, your deductions and $1,000, added together. For 2026, each exemption is $5,000 and the standard deduction is $15,000. A joint return's standard deduction is $30,000. Payments aren't needed if your employer agrees to withhold enough extra.").
		Paragraph("You avoid the penalty if withholding and payments reach a target. The target is 70% of this year's Georgia tax or, if lower, 100% of last year's. Last year's counts only from a return covering twelve months. There is no 110% tier for higher incomes. The amount is paid in equal installments on April 15, June 15 and September 15. The last is due January 15 of the next year. Becoming required later in the year means fewer, larger installments. Withholding counts as paid equally each quarter, whenever it was taken. If you file and pay the balance by January 31, it counts as paid on January 15.").
		Paragraph("Each payment counts first toward its own period's installment. Only what is left over makes up an earlier shortfall. Three tests can also excuse an installment, based on what you paid by its date. One is last year's income taxed at this year's 4.99% rate. Another is 70% of the tax on your annualised income. The third, except in January, is 90% of the tax on income actually received.").
		Paragraph("The penalty is 9% a year on each shortfall, by the day from its due date. It runs until the shortfall is paid or April 15 of the next year. The rate is fixed rather than tied to the federal rate. Georgia's forms have no waiver for a casualty, retirement or disability. The Department does postpone deadlines for declared disasters.").
		VersusFederal("Georgia's test is gross income over exemptions, deductions and $1,000. It is not $1,000 of tax. The tests are 70% of this year's tax and 100% of last year's, with no 110% tier. A payment goes first to its own period's installment. Three exceptions can excuse an installment. The penalty is a fixed 9% a year. There is no casualty, retirement or disability waiver.").
		Source("O.C.G.A. §§ 48-7-114 to 48-7-120, as applied by the Department of Revenue's forms").
		Source("Form 500 UET, Underpayment of Estimated Tax by Individuals/Fiduciary (Rev. 07/15/25)").
		Source("Form 500-ES, Individual and Fiduciary Estimated Tax (Rev. 05/12/25)").
		Source("HB 463 (2026), §§ 2-1 to 2-3 and 5-1").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
