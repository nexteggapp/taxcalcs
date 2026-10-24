package newjerseytaxcalc

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// New Jersey's estimated gross income tax rules, N.J.S.A. 54A:8-4, 54A:8-5
// and 54A:9-6(c) and (d), with interest under N.J.S.A. 54:49-3, as the
// shared [estimated.EstimatedTax] interface. New Jersey calls the charge
// interest.
//
// New Jersey keeps the older federal rules. The underpayment is measured
// against the lesser of 80% of this year's tax and 100% of last year's, a
// quarter by each date. Four exceptions each take the interest off a quarter
// whose payments reach them: last year's tax, last year's income at this
// year's rates, 80% of the tax on annualized income, and 90% of the tax on
// income to date. Interest is prime plus 3% for each month or part of a
// month, 10% for 2026.
//
// The figure follows Form NJ-2210 for 2025, the latest published, with the
// 2026 dates and rate, and the result's Divergences say where the statute
// reads otherwise. N.J.S.A. 54A:8-4, 54A:8-5, 54A:9-6 and 54:49-3 carry no
// amendment after 2005 (research/estimated-payments/nj.md), so the form does
// not predate the law it describes.
//
// Option 2 of the NJ-2210 instructions counts each month or part of a month
// from the due date, and its rate schedule gives a month the rate of the
// period it starts in: for 2025, 10.75% to January 15, 2026 and 10.00% from
// January 16. A 2026 underpayment is charged 10.00% for months starting up
// to January 15, 2027. The rate from January 16, 2027 is set from the prime
// rate on December 1, 2026 and is not published, so later months are not
// charged and the penalty reports the gap. Every due date is the fifteenth,
// so the months each quarter's balance runs line up with the due dates, and
// figuring each period's balance, as Option 1 does, gives Option 2's months.
//
// Interest compounds at the end of each calendar year; only months from
// January 16, 2027 would carry 2026's interest, and they are not charged.
//
// Not built: the exception for an installment due on or after the taxpayer's
// death (54A:9-6(c)), as [estimated.Facts] carries no date of death; Form
// NJ-2210NR, which scales Exceptions 2 to 4 by a nonresident's income
// percentage, which a caller can apply to the amounts it passes; and
// estates and trusts.

// jurisdiction is New Jersey's postal code.
const jurisdiction = estimated.NewJersey

// EstimatedFacts are the facts New Jersey's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	refundableCredits optional
	priorIncomeTax    optional

	taxToDate    []money.USCents
	hasTaxToDate bool

	overpaymentApril, overpaymentAnswered bool
	moves, movesAnswered                  bool
}

type optional struct {
	v   money.USCents
	set bool
}

// RefundableCredits are this year's refundable credits and excess
// contributions, Form NJ-1040 lines 56 and 58 to 65. NJ-2210 line 6 counts
// them with the tax withheld, line 55, a quarter on each date.
func (e EstimatedFacts) RefundableCredits() (money.USCents, bool) {
	return e.refundableCredits.v, e.refundableCredits.set
}

// PriorYearIncomeAtThisYearsRates is the tax on last year's gross income
// less this year's exemptions at this year's rates, less the credit for tax
// paid to other jurisdictions: the NJ-2210 Exception II worksheet, line 6.
func (e EstimatedFacts) PriorYearIncomeAtThisYearsRates() (money.USCents, bool) {
	return e.priorIncomeTax.v, e.priorIncomeTax.set
}

// TaxOnIncomeToDate is the tax on the actual taxable income from January 1
// to March 31, May 31 and August 31, less the credit for tax paid to other
// jurisdictions: the NJ-2210 Exception IV worksheet, line 4, before the 90%.
func (e EstimatedFacts) TaxOnIncomeToDate() ([]money.USCents, bool) {
	return append([]money.USCents(nil), e.taxToDate...), e.hasTaxToDate
}

// PriorYearOverpaymentInApril reports whether last year's overpayment
// applied forward counts toward the April installment, as Form NJ-1040-ES
// says, rather than the June one, as NJ-2210 line 6 says; and whether the
// caller said.
func (e EstimatedFacts) PriorYearOverpaymentInApril() (april, answered bool) {
	return e.overpaymentApril, e.overpaymentAnswered
}

// FebruaryFifteenthMoves reports whether a return filed and paid on Tuesday,
// February 16, 2027 counts as filed by February 15, which is Presidents'
// Day, and whether the caller said. No 2026 source says.
func (e EstimatedFacts) FebruaryFifteenthMoves() (moves, answered bool) {
	return e.moves, e.movesAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) RefundableCredits(a money.USCents) *EstimatedFactsBuilder {
	b.e.refundableCredits = optional{a, true}
	return b
}

func (b *EstimatedFactsBuilder) PriorYearIncomeAtThisYearsRates(a money.USCents) *EstimatedFactsBuilder {
	b.e.priorIncomeTax = optional{a, true}
	return b
}

// TaxOnIncomeToDate gives the tax on the income to March 31, May 31 and
// August 31.
func (b *EstimatedFactsBuilder) TaxOnIncomeToDate(march, may, august money.USCents) *EstimatedFactsBuilder {
	b.e.taxToDate, b.e.hasTaxToDate = []money.USCents{march, may, august}, true
	return b
}

func (b *EstimatedFactsBuilder) PriorYearOverpaymentInApril(april bool) *EstimatedFactsBuilder {
	b.e.overpaymentApril, b.e.overpaymentAnswered = april, true
	return b
}

func (b *EstimatedFactsBuilder) FebruaryFifteenthMoves(moves bool) *EstimatedFactsBuilder {
	b.e.moves, b.e.movesAnswered = moves, true
	return b
}

// Build refuses negative amounts.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	e := b.e
	if e.refundableCredits.v < 0 || e.priorIncomeTax.v < 0 {
		return EstimatedFacts{}, errors.New("newjerseytaxcalc: credits and taxes cannot be negative")
	}
	for _, t := range e.taxToDate {
		if t < 0 {
			return EstimatedFacts{}, errors.New("newjerseytaxcalc: the tax on income to date cannot be negative")
		}
	}
	e.taxToDate = append([]money.USCents(nil), e.taxToDate...)
	return e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is NJ-2210 line 3.
	threshold money.USCents

	// filingThreshold is the gross income at or below which no estimated
	// payments are needed: the higher figure for joint, head of household
	// and surviving spouse returns, the lower for the others.
	filingThreshold, filingThresholdSingle money.USCents

	thisYearShare, priorYearShare money.Rate

	// annualisedShare and incomeShare are Exceptions 3 and 4: 80% of the
	// annualized tax and 90% of the tax on income to date.
	annualisedShare, incomeShare money.Rate

	shares     []money.Rate
	dues       []estimated.Date
	calendar   estimated.Calendar
	periodEnds []estimated.Date

	// fileAndPayBy is N.J.S.A. 54A:8-4(h): the return filed and paid by then
	// takes the place of the January installment.
	fileAndPayBy estimated.Date

	stop estimated.Date

	// rate is the annual rate for the months that start by rateTo, from
	// TB-21(R) and the NJ-2210 rate schedule's pattern. unrated says why the
	// months after have none.
	rate       money.Rate
	rateTo     estimated.Date
	rateSource string
	unrated    string
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns New Jersey's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return newJerseyEstimated{y}, nil
}

type newJerseyEstimated struct{ y estimatedYear }

type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents

	// exceptions are the running totals that excuse a quarter, by exception,
	// or nil where it is not open; -1 where it does not apply to a quarter.
	exceptions [4][]money.USCents

	filedByFeb15 bool
	extras       EstimatedFacts
	notes        working.Notes
}

func (r newJerseyEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("newjerseytaxcalc: the facts carry %T, not newjerseytaxcalc.EstimatedFacts", s)
	}
}

func (r newJerseyEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("newjerseytaxcalc: these are New Jersey's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("newjerseytaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case int64(f.FarmShare())*3 >= 20_000:
		return fmt.Errorf("%w: a farmer with two-thirds of income from farming pays in full by January 15 at two-thirds of the tax, which is not built", estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("newjerseytaxcalc: NJ-2210 Exception III period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the Division's forms and the statutes part ways on
// a rule every New Jersey computation uses. They are in
// research/estimated-payments/nj.md, except the late start, Exception 2's
// 110% and the rate's start, found reading NJ-2210 against the statutes and
// TB-21(R).
func (r newJerseyEstimated) divergences(t touches) ([]working.Divergence, error) {
	overpaymentReading := working.ReadingForm
	if t.overpaymentApril {
		overpaymentReading = working.ReadingStatute
	}
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The $400 boundary").
			Form("The form stops only if tax less withholding and credits is under $400. Exactly $400 goes on to the computation.",
				"Form NJ-2210 (2025), line 3 and its instruction").
			Statute("A declaration is needed only if tax is expected to exceed credits by over $400. Exactly $400 requires none.",
				"N.J.S.A. 54A:8-4(a), (f)").
			Uses(working.ReadingForm).Affects(t.atThreshold),
		working.NewDivergence("A twelve-month prior year").
			Form("100% of last year's tax counts only if that return covered a full 12 months.",
				"Form NJ-2210 (2025) instructions, In General").
			Statute("The underpayment is measured against 100% of the tax on last year's return. There's no twelve-month condition. The condition is in Exception 1 only.",
				"N.J.S.A. 54A:9-6(c)(2), (d)(1)(A)").
			Uses(working.ReadingForm).Affects(t.shortPriorYear),
		working.NewDivergence("A requirement that arises late").
			Form("The form divides the required amount by four in every case. Form NJ-1040-ES tells a late starter to pay equal parts on the dates left.",
				"Form NJ-2210 (2025), line 5; 2026 Form NJ-1040-ES, Changes in Income, Exemptions, etc.").
			Statute("A declaration first due June 15 is paid in three equal installments. One due September 15 is paid in two, and one due January 15 in full.",
				"N.J.S.A. 54A:8-4(d), 54A:8-5(a)(2)-(4)").
			Uses(working.ReadingForm).Affects(t.lateStart),
		// A zero prior year also makes the required amount zero, so the
		// readings give the same figures.
		working.NewDivergence("Exception 1 with no prior-year return").
			Form("Last year's tax is zero if no return was required, so Exception 1 is met.",
				"Form NJ-2210 (2025) instructions, Exception 1").
			Statute("Exception 1 needs a return showing a liability filed for a twelve-month year.",
				"N.J.S.A. 54A:9-6(d)(1)(A)").
			Uses(working.ReadingForm).Affects(false),
		working.NewDivergence("110% in Exceptions 1 and 2").
			Form("Exceptions 1 and 2 are 25%, 50%, 75% and 100%, at any income. They apply to last year's tax and to the tax on last year's income. The Division's notice charges interest only against the lesser of 80% and 100%.",
				"Form NJ-2210 (2025), lines 15 and 16; Notice Concerning Estimated Tax for Taxpayers Earning Over $150,000").
			Statute("Above $150,000 of last year's taxable gross income, both are figured at 110%. The line is $75,000 if married filing separately.",
				"N.J.S.A. 54A:9-6(d)(3)").
			Uses(working.ReadingForm).Affects(t.highIncomeExceptions),
		working.NewDivergence("When a year's rate starts").
			Form("The rate schedule gives each month the rate of the period it starts in. The new year's rate starts January 16.",
				"Form NJ-2210 (2025), Option 2, Interest Rate Schedule").
			Statute("Each calendar year's rate starts January 1, at three points over prime. It applies for each month or fraction of a month.",
				"TB-21(R), Interest Rate Assessed on Tax Balances for 2026; N.J.S.A. 54:49-3").
			Uses(working.ReadingForm).Affects(t.unpaidAtYearEnd),
		working.NewDivergence("Last year's overpayment").
			Form("Form NJ-2210 counts last year's overpayment applied forward in column B. That's the June installment.",
				"Form NJ-2210 (2025), Line 6 instructions").
			Statute("Form NJ-1040-ES credits it to the first quarterly installment. The estimated tax statutes don't address it.",
				"2026 Form NJ-1040-ES, Completing the NJ-1040-ES Voucher; N.J.S.A. 54A:9-6").
			Uses(overpaymentReading).Affects(t.overpayment),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// touches are the facts each divergence's readings turn on.
type touches struct {
	atThreshold          bool
	shortPriorYear       bool
	lateStart            bool
	highIncomeExceptions bool
	unpaidAtYearEnd      bool
	overpayment          bool
	overpaymentApril     bool
}

// highIncome and highIncomeSeparate are N.J.S.A. 54A:9-6(d)(3)'s last-year
// income above which Exceptions 1 and 2 are figured at 110%. They decide
// only whether that divergence affects a household.
const (
	highIncome         = 150_000 * money.Dollar
	highIncomeSeparate = 75_000 * money.Dollar
)

func (r newJerseyEstimated) assess(f estimated.Facts) (assessment, error) {
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
	refundable, refundableKnown := extras.RefundableCredits()
	credited := f.WithheldTotal() + refundable
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
		a.notes.Rule("N.J.S.A. 54A:9-6(c)(1); Form NJ-2210 (2025), line 4a", fmt.Sprintf("%v of this year's New Jersey tax of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	exceptionOne := false
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d New Jersey return", last))
	case prior.NotRequiredToFile() && prior.Months() == 12:
		exceptionOne = true
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0)
		a.notes.Rule("Form NJ-2210 (2025) instructions, Exception 1", fmt.Sprintf("no %d return was required, so last year's tax is zero", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d New Jersey return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d New Jersey return covered %d months, not a full year", last, prior.Months()))
	default:
		exceptionOne = true
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, y.priorYearShare.Of(priorTax))
		a.notes.Rule("N.J.S.A. 54A:9-6(c)(2); Form NJ-2210 (2025), line 4b", fmt.Sprintf("the prior-year test is %v of the %v tax on the %d return", y.priorYearShare, priorTax, last))
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

	gross, grossKnown := f.ThisYear().GrossIncome()
	filingThreshold := y.filingThresholdSingle
	switch f.FilingStatus() {
	case filing.MarriedFilingJointly, filing.HeadOfHousehold, filing.SurvivingSpouse:
		filingThreshold = y.filingThreshold
	}
	grossExempt := grossKnown && gross <= filingThreshold
	switch {
	case grossExempt:
		exception, err := estimated.ExceptionAtMost("Gross income", gross, filingThreshold)
		if err != nil {
			return assessment{}, err
		}
		a.exempt = true
		rb.Required(false, fmt.Sprintf("Your gross income of %s is at or below the %s filing threshold. You don't need to make New Jersey estimated payments.", gross.Whole(), filingThreshold.Whole())).Exempt(exception)
		a.notes.Rule("2026 Form NJ-1040-ES, Minimum Income Filing Threshold", fmt.Sprintf("gross income of %v is at or below %v, so no estimated payments are needed", gross, filingThreshold))
	case taxKnown && tax-credited < y.threshold:
		exception, err := estimated.ExceptionBelow("Tax less withholding and credits", tax-credited, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		a.exempt = true
		under := fmt.Sprintf(", under %s", y.threshold.Whole())
		if tax < credited {
			under = ""
		}
		rb.Required(false, fmt.Sprintf("Your New Jersey tax is %s, and withholding and credits come to %s. That %s%s. No estimated payments are needed.",
			tax.Whole(), credited.Whole(), estimated.Leaves(tax-credited), under)).Exempt(exception)
		a.notes.Rule("Form NJ-2210 (2025), line 3", fmt.Sprintf("%v of tax less %v of withholding and credits is %v, under %v, so there is no interest", tax, credited, tax-credited, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target needs your %d New Jersey tax. A full-year %d New Jersey return could also set it. There's no target yet.", y.year, last)).NoTarget()
	case !taxKnown && credited >= target:
		rb.Required(false, fmt.Sprintf("Your withholding and credits of %s already reach your %d tax, %s. No estimated payments are needed, whatever your %d tax is.",
			credited.Whole(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is your %d tax, %s. That keeps you clear of interest whatever your %d tax turns out to be. Withholding and credits of %s leave %s of estimated payments. Pay a quarter by each due date.",
			last, target.Whole(), y.year, credited.Whole(), (target-credited).Whole())).Choose(chosen)
	case credited >= target:
		rb.Required(false, fmt.Sprintf("Your New Jersey withholding and credits come to %s. That reaches the %s needed for the year. No estimated payments are needed.", credited.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your New Jersey tax is %s, and withholding and credits come to %s. That leaves %s, which is %s or more. You need estimated payments of %s, a quarter by each due date.",
			tax.Whole(), credited.Whole(), (tax-credited).Whole(), y.threshold.Whole(), (target-credited).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// Under an exception neither return can change the target.
	if !hasPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d New Jersey return, and what was its tax?", last),
			fmt.Sprintf("With it, 100%% of your %d tax could be a lower target than 80%% of this year's.", last))
	}
	if !taxKnown && !a.exempt {
		if priorOpt.Available() {
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d New Jersey tax to be?", y.year),
				fmt.Sprintf("With it, 80%% of it could be a lower target than your %d tax. It also decides whether the %s threshold excuses you.", last, y.threshold.Whole()))
		} else {
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d New Jersey tax to be?", y.year),
				fmt.Sprintf("Without it there's no target. There's no full-year %d New Jersey return to measure against.", last))
		}
	}
	if !a.exempt && !refundableKnown {
		a.notes.Ask(working.PositionJurisdictionFact, "What New Jersey refundable credits and excess contributions do you expect?",
			fmt.Sprintf("For %d they count with withholding, a quarter on each due date. Until then, the figures count none.", y.year))
	}

	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("N.J.S.A. 54A:8-5(a)(1); Form NJ-2210 (2025), line 5", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", a.requirement.Target(), a.installments[0],
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	// The four exceptions, as running totals by each date.
	quarters := []money.Rate{money.Percent(25), money.Percent(50), money.Percent(75), money.Percent(100)}
	if exceptionOne {
		a.exceptions[0] = make([]money.USCents, len(a.dues))
		for i := range a.dues {
			a.exceptions[0][i] = quarters[i].Of(priorTax)
		}
	}
	if amount, ok := extras.PriorYearIncomeAtThisYearsRates(); ok {
		a.exceptions[1] = make([]money.USCents, len(a.dues))
		for i := range a.dues {
			a.exceptions[1][i] = quarters[i].Of(amount)
		}
		a.notes.Rule("N.J.S.A. 54A:9-6(d)(1)(B); Form NJ-2210 (2025), line 16, Exception 2", fmt.Sprintf("a quarter is excused when payments by its date reach 25%%, 50%%, 75%% and 100%% of %v, the tax on %d income at %d rates", amount, last, y.year))
	}
	if f.Elections().Annualised() {
		a.exceptions[2] = []money.USCents{-1, -1, -1, -1}
		for i, p := range f.Periods()[:3] {
			a.exceptions[2][i] = y.annualisedShare.Of(quarters[i].Of(p.AnnualisedTax()))
		}
		a.notes.Rule("N.J.S.A. 54A:9-6(d)(1)(C); Form NJ-2210 (2025), line 17, Exception 3", fmt.Sprintf("the first three quarters are also excused when payments reach %v, %v and %v: 20%%, 40%% and 60%% of the tax on annualized income",
			a.exceptions[2][0], a.exceptions[2][1], a.exceptions[2][2]))
	}
	if taxes, ok := extras.TaxOnIncomeToDate(); ok {
		a.exceptions[3] = []money.USCents{-1, -1, -1, -1}
		for i, t := range taxes {
			a.exceptions[3][i] = y.incomeShare.Of(t)
		}
		a.notes.Rule("N.J.S.A. 54A:9-6(d)(2); Form NJ-2210 (2025), line 18, Exception 4", fmt.Sprintf("the first three quarters are also excused when payments reach %v, %v and %v: 90%% of the tax on the income to date",
			a.exceptions[3][0], a.exceptions[3][1], a.exceptions[3][2]))
	}

	// N.J.S.A. 54A:8-4(h). February 15, 2027 is a Monday and Presidents' Day,
	// and no source yet says whether the day moves.
	fileBy := y.fileAndPayBy
	moves, movesAnswered := extras.FebruaryFifteenthMoves()
	if moves {
		fileBy = fileBy.AddDays(1)
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && !a.exempt {
		if !movesAnswered && filed == fileBy.AddDays(1) {
			a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("You filed and paid on %s, the day after Presidents' Day. No 2026 source says if that counts as on time, and the figures treat it as late.", filed.Long()))
		}
		if !filed.After(fileBy) {
			a.filedByFeb15 = true
			a.notes.Rule("N.J.S.A. 54A:8-4(h); 2026 Form NJ-1040-ES, Changes in Income, Exemptions, etc.", fmt.Sprintf("the return was filed and paid on %s, by %s, so no January installment is needed", filed.Long(), fileBy.Long()))
		}
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	t := touches{
		atThreshold:    !grossExempt && taxKnown && tax-credited == y.threshold,
		shortPriorYear: !a.exempt && hasPrior && prior.Filed() && prior.Months() != 12,
	}
	t.overpaymentApril, _ = extras.PriorYearOverpaymentInApril()
	if met := f.RequirementFirstMet(); !a.exempt && !met.IsZero() && met.After(a.dues[0]) {
		t.lateStart = true
	}
	if !a.exempt {
		for _, p := range f.Payments() {
			if p.FromPriorYear() {
				t.overpayment = true
			}
		}
		_, exceptionTwo := extras.PriorYearIncomeAtThisYearsRates()
		if exceptionOne || exceptionTwo {
			limit := highIncome
			if f.FilingStatus() == filing.MarriedFilingSeparately {
				limit = highIncomeSeparate
			}
			income, known := money.USCents(0), false
			if hasPrior {
				if pf, ok := prior.Figures(); ok {
					if income, known = pf.GrossIncome(); !known {
						income, known = pf.StateAGI()
					}
				}
			}
			t.highIncomeExceptions = !known || income > limit
		}
	}
	if !a.exempt && a.requirement.TargetKnown() {
		credits, _, err := r.credits(f, a)
		if err != nil {
			return assessment{}, err
		}
		yearEnd := date(y.year, time.December, 31)
		var byYearEnd, bySeptember money.USCents
		for _, c := range credits {
			if !c.On().After(yearEnd) {
				byYearEnd += c.Amount()
			}
			if !c.On().After(a.dues[2]) {
				bySeptember += c.Amount()
			}
		}
		needed := a.installments[0] + a.installments[1] + a.installments[2]
		t.unpaidAtYearEnd = byYearEnd < needed && a.excused(2, bySeptember) == ""
	}
	divergences, err := r.divergences(t)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

// excused names the exception that takes the interest off quarter i, or "".
func (a assessment) excused(i int, paidBy money.USCents) string {
	names := []string{"Exception 1, last year's tax", "Exception 2, last year's income at this year's rates", "Exception 3, annualized income", "Exception 4, income to date"}
	for k, ex := range a.exceptions {
		if ex != nil && ex[i] >= 0 && paidBy >= ex[i] {
			return names[k]
		}
	}
	return ""
}

func (r newJerseyEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// credits are withholding, refundable credits and payments, dated as NJ-2210
// line 6 counts them, earliest first.
func (r newJerseyEstimated) credits(f estimated.Facts, a assessment) ([]estimated.Payment, working.Notes, error) {
	var notes working.Notes
	var out []estimated.Payment
	add := func(on estimated.Date, amount money.USCents) error {
		if amount <= 0 {
			return nil
		}
		p, err := estimated.NewPayment().On(on).Amount(amount).Build()
		if err != nil {
			return err
		}
		out = append(out, p)
		return nil
	}
	refundable, _ := a.extras.RefundableCredits()
	spread := refundable
	if f.Elections().WithholdingAsWithheld() {
		for _, w := range f.Withholding() {
			if err := add(w.On(), w.Amount()); err != nil {
				return nil, working.Notes{}, err
			}
		}
		notes.Rule("Form NJ-2210 (2025), Line 6 instructions", fmt.Sprintf("as shown, the %v withheld is counted on the dates it was withheld; the %v of credits is counted a quarter on each date", f.WithheldTotal(), refundable))
	} else {
		spread += f.WithheldTotal()
		notes.Rule("Form NJ-2210 (2025), Line 6 instructions", fmt.Sprintf("the %v withheld and %v of credits are treated as paid a quarter on each due date", f.WithheldTotal(), refundable))
	}
	cum, err := estimated.CumulativeShares(spread, r.y.shares)
	if err != nil {
		return nil, working.Notes{}, err
	}
	for i, amt := range estimated.Increments(cum) {
		if err := add(a.dues[i], amt); err != nil {
			return nil, working.Notes{}, err
		}
	}
	april, answered := a.extras.PriorYearOverpaymentInApril()
	for _, p := range f.Payments() {
		on := p.On()
		if p.FromPriorYear() {
			on = a.dues[1]
			if april {
				on = a.dues[0]
			}
			if !answered {
				notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("Form NJ-1040-ES counts your %d overpayment toward the April installment. Form NJ-2210 counts it toward June's, and the figures follow Form NJ-2210.", r.y.year-1))
			}
		}
		if err := add(on, p.Amount()); err != nil {
			return nil, working.Notes{}, err
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].On().Before(out[j].On()) })
	return out, notes, nil
}

func (r newJerseyEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d New Jersey return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	credits, notes, err := r.credits(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := r.schedule(a, credits, &notes)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments, with the credits applied earliest first. A
// quarter an exception covers is excused.
func (r newJerseyEstimated) schedule(a assessment, credits []estimated.Payment, notes *working.Notes) ([]estimated.Installment, error) {
	required := append([]money.USCents(nil), a.installments...)
	if a.filedByFeb15 {
		required[3] = 0
	}
	ledger, err := estimated.Apply(a.dues, required, credits)
	if err != nil {
		return nil, err
	}
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		why := a.excused(i, l.CreditedBy())
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(l.NeededBy()).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if (a.filedByFeb15 && i == 3) || why != "" {
			b.Excused()
		}
		if l.Shortfall() > 0 && why != "" && !(a.filedByFeb15 && i == 3) {
			notes.Rule("N.J.S.A. 54A:9-6(d); Form NJ-2210 (2025), Part II", fmt.Sprintf("the %s quarter was %v short and bears no interest under %s", l.Due().Long(), l.Shortfall(), why))
		}
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// monthly charges principal for each month or part of a month from from to
// to at the year's rate, for the months that start by the rate's last day.
// It returns the part, if any month is rated, and the first day of a month
// with no rate, or the zero Date.
func (r newJerseyEstimated) monthly(principal money.USCents, from, to estimated.Date) (estimated.PenaltyPart, bool, estimated.Date, error) {
	y := r.y
	months := estimated.MonthsOrPart(from, to)
	rated := 0
	for k := 0; k < months; k++ {
		start := from.AddMonths(k).AddDays(1)
		if start.After(y.rateTo) {
			break
		}
		rated++
	}
	var unrated estimated.Date
	if rated < months {
		unrated = from.AddMonths(rated).AddDays(1)
	}
	if rated == 0 {
		return estimated.PenaltyPart{}, false, unrated, nil
	}
	end := to
	if rated < months {
		end = from.AddMonths(rated)
	}
	n := int64(principal) * int64(y.rate) * int64(rated)
	charge := money.USCents((n + 60_000) / 120_000)
	p, err := estimated.NewPenaltyPart(estimated.PartInterest, principal, y.rate).
		Span(from, end).Days(end.DaysSince(from)).Months(rated).Charge(charge).Source(y.rateSource).Build()
	return p, true, unrated, err
}

func (r newJerseyEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("newjerseytaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d New Jersey return to measure against, there is no target to figure the interest from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := asOf
	if y.stop.Before(stop) {
		stop = y.stop
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop = filed
	}
	waiver, waiverAsked := f.Positions().Waiver()
	credits, notes, err := r.credits(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	var scheduleNotes working.Notes
	schedule, err := r.schedule(a, credits, &scheduleNotes)
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

	// NJ-2210 Option 1's columns, with Option 2's months: each period's
	// balance, this quarter's amount and what an unexcused earlier quarter
	// left, less what was paid in the period, charged for each month or part
	// of a month until paid or the next due date. A quarter an exception
	// excuses carries nothing forward, as GIT-8's example does.
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	var unrated estimated.Date
	var carry, paidBy money.USCents
	prev := estimated.Date{}
	for k, due := range a.dues {
		var paidIn money.USCents
		for _, c := range credits {
			if c.On().After(prev) && !c.On().After(due) {
				paidIn += c.Amount()
			}
		}
		paidBy += paidIn
		prev = due
		amount := a.installments[k]
		if a.filedByFeb15 && k == 3 {
			amount = 0
		}
		balance := amount + carry - paidIn
		carry = balance
		if balance <= 0 || !due.Before(stop) {
			continue
		}
		if why := a.excused(k, paidBy); why != "" {
			carry = 0
			notes.Rule("N.J.S.A. 54A:9-6(d); Form NJ-2210 (2025), Part II; GIT-8 (January 2026)", fmt.Sprintf("no interest is charged for the %s quarter, under %s", due.Long(), why))
			continue
		}
		end := y.stop
		if k+1 < len(a.dues) {
			end = a.dues[k+1]
		}
		if stop.Before(end) {
			end = stop
		}
		lb := estimated.NewPenaltyLine(k+1, due, balance)
		left, lastDay := balance, due
		charge := func(amount money.USCents, to estimated.Date) error {
			part, ok, gap, err := r.monthly(amount, due, to)
			if err != nil {
				return err
			}
			if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
				unrated = gap
			}
			if ok {
				lb.Part(part)
			}
			return nil
		}
		for _, c := range credits {
			if left <= 0 || !c.On().After(due) || !c.On().Before(end) {
				continue
			}
			piece := min(left, c.Amount())
			if err := charge(piece, c.On()); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			left -= piece
			lastDay = c.On()
		}
		if left > 0 {
			if err := charge(left, end); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			lastDay = end
		}
		line, err := lb.Days(lastDay.DaysSince(due)).Build()
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
		notes.Rule("N.J.S.A. 54A:9-6(c)", fmt.Sprintf("this year's New Jersey tax was not given, so the interest is figured against the %d tax alone; "+
			"the required amount is the lesser of that and 80%% of this year's tax, so %v is the most the interest can be, and it is less if this year's tax turns out lower", y.year-1, p.Total()))
	}
	notes.Rule("N.J.S.A. 54A:9-6(c), 54:49-3; Form NJ-2210 (2025), Option 2; TB-21(R)", fmt.Sprintf("each period's balance is charged %v a year for each month or part of a month, from its due date until paid or the next due date, and the last to %s; "+
		"the interest to %s is %v", y.rate, y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet. %s.", unrated.AddDays(-1).Long(), y.unrated))
	}
	if waiverAsked && waiver != estimated.WaiverNone {
		notes.Rule("N.J.S.A. 54:49-11(a)", "the Director may waive only interest above three points over prime, so there is no waiver of this interest for casualty, retirement or disability; it is shown in full")
		if p.Total() > 0 {
			o := estimated.WaiverRefused("New Jersey can waive only interest above three points over prime. This interest can't be waived for retirement, disability, casualty or disaster.")
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r newJerseyEstimated) Explanation() estimated.Explanation { return newJerseyExplanation }

// newJerseyExplanation is written by hand and checked against
// research/estimated-payments/nj.md.
var newJerseyExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"New Jersey expects estimated payments above a $400 threshold. That's your tax after withholding and credits. Falling short costs interest at prime plus 3%, 10% for 2026. It's charged for each month or part of a month.").
		Paragraph("Withholding, refundable credits and estimated payments must reach a target. The target is the smaller of 80% of this year's tax or 100% of last year's. A quarter is due by April 15, June 15, September 15 and January 15. There is no 110% figure in practice. The Division charges interest only against those two amounts.").
		Paragraph("A quarter also avoids interest if payments so far reach one of four measures. The first is last year's tax. The second is the tax on last year's income at this year's rates. The third is 80% of the tax on your income annualized to date. The fourth is 90% of the tax on your actual income to date. The last two do not apply to January. Filing and paying in full by February 15 replaces the January payment.").
		Paragraph("Interest runs from each due date until the shortfall is paid. It stops on April 15 of the next year. Any part of a month counts as a whole month. Withholding and refundable credits count as paid a quarter on each date. You can show other dates instead. The Division cannot waive this interest for reasonable cause. Nor can it waive it for a casualty, retirement or disability.").
		VersusFederal("New Jersey's current-year test is 80% rather than 90%. Its threshold is $400 rather than $1,000, and the 110% tier never applies. Four exceptions excuse a quarter outright. Interest is charged by the month at prime plus 3%. Filing by February 15, not January 31, replaces the January payment. There is no casualty, retirement or disability waiver.").
		Source("N.J.S.A. 54A:8-4, 54A:8-5, 54A:9-6, 54:49-3, 54:49-11").
		Source("Form NJ-2210, 2025 Underpayment of Estimated Tax, and its instructions").
		Source("2026 Form NJ-1040-ES").
		Source("GIT-8, Estimating Income Taxes (January 2026)").
		Source("TB-21(R), Interest Rate Assessed on Tax Balances for 2026").
		Source("Notice Concerning Estimated Tax for Taxpayers Earning Over $150,000").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()

// date is a shorthand for the year files.
func date(y int, m time.Month, d int) estimated.Date { return estimated.MustDate(y, m, d) }
