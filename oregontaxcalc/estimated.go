package oregontaxcalc

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Oregon's estimated tax rules, ORS 316.557 to 316.589, as the shared
// [estimated.EstimatedTax] interface.
//
// Oregon's rules look like the federal ones and differ in the details that
// change a number. The prior-year test is 100% at every income. Farmers and
// fishers owe no estimates at all. Due dates move for Oregon's own legal
// holidays. The charge is interest at the rate set under ORS 305.220, and Form
// OR-10 computes it on a running balance from one event to the next, rounding
// each line to a whole dollar.
//
// The figure follows the Department of Revenue's documents, Publication
// OR-ESTIMATE for 2026 and the Form OR-10 instructions for 2025, and the
// result's Divergences say where the statutes or rules read otherwise. No act
// of the 2026 session amended ORS 316.557 to 316.589, 305.220 or 187.010, so
// no form here predates the law it describes.

// oregon is the jurisdiction code these rules answer for.
const oregon = estimated.Oregon

// EstimatedFacts are the facts Oregon's rules read that no other jurisdiction
// does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorFiledFirst, priorFiledFirstAnswered bool
	grossIncomeTest, grossIncomeAnswered     bool
	businessPeriods                          []int
	businessAnswered                         bool
}

// BusinessIncomeIn reports the installment periods, numbered from 1, in
// which a part-year resident had income from a trade or business in Oregon
// while living elsewhere, and whether the caller said. The Form OR-10
// instructions count those periods with the periods of residence.
func (e EstimatedFacts) BusinessIncomeIn() (periods []int, answered bool) {
	return append([]int(nil), e.businessPeriods...), e.businessAnswered
}

// PriorReturnFiledFirst reports whether the 2025 return was or will be filed
// before the 2026 return, and whether the caller said. Publication
// OR-ESTIMATE and OAR 150-316-0493(3)(a)(B) allow the prior-year test only
// then.
func (e EstimatedFacts) PriorReturnFiledFirst() (filedFirst, answered bool) {
	return e.priorFiledFirst, e.priorFiledFirstAnswered
}

// GrossIncomeTestMet reports whether ORS 316.563(1)'s gross income test is
// met, and whether the caller said. The test is more than $1,000 of gross
// income other than wages, or gross income over $20,000 ($10,000 for a joint
// declaration where both spouses have wages, $5,000 for a married filer who
// cannot declare jointly). A caller who gives this year's gross income above
// $20,000 need not answer it.
func (e EstimatedFacts) GrossIncomeTestMet() (met, answered bool) {
	return e.grossIncomeTest, e.grossIncomeAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorReturnFiledFirst(filedFirst bool) *EstimatedFactsBuilder {
	b.e.priorFiledFirst, b.e.priorFiledFirstAnswered = filedFirst, true
	return b
}

func (b *EstimatedFactsBuilder) GrossIncomeTestMet(met bool) *EstimatedFactsBuilder {
	b.e.grossIncomeTest, b.e.grossIncomeAnswered = met, true
	return b
}

// BusinessIncomeIn sets the installment periods, from 1 to 4, with Oregon
// business income while living elsewhere. Calling it with none answers that
// there were none.
func (b *EstimatedFactsBuilder) BusinessIncomeIn(periods ...int) *EstimatedFactsBuilder {
	b.e.businessPeriods, b.e.businessAnswered = append([]int(nil), periods...), true
	return b
}

// Build refuses an installment period outside 1 to 4.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	e := b.e
	e.businessPeriods = append([]int(nil), e.businessPeriods...)
	for _, n := range e.businessPeriods {
		if n < 1 || n > 4 {
			return EstimatedFacts{}, fmt.Errorf("oregontaxcalc: installment period %d is not 1 to 4", n)
		}
	}
	return e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is the tax after credits less withholding below which there
	// is no interest.
	threshold money.USCents

	thisYearShare, priorYearShare money.Rate

	dues []estimated.Date

	// lateStarts are the first days of the late-start chart's three rows: a
	// requirement first met on or after lateStarts[i] is paid in 3-i equal
	// installments, from installment i+2.
	lateStarts []estimated.Date

	calendar   estimated.Calendar
	stop       estimated.Date
	periodEnds []estimated.Date
	annualised []money.Rate

	// grossIncomeTest is the highest of ORS 316.563(1)'s gross income
	// figures. Gross income above it meets the test for every filer.
	grossIncomeTest money.USCents

	// daily is the factor Form OR-10 prints for each calendar year.
	daily estimated.FactorSeries
}

var estimatedYears = map[int]estimatedYear{}

// registerEstimated adds a year's figures. Oregon's rate changes only on
// January 1 (OAR 150-305-0144(2)), so each daily factor must cover one whole
// calendar year.
func registerEstimated(y estimatedYear) {
	for _, p := range y.daily.Periods() {
		jan1 := estimated.MustDate(p.From().Year(), 1, 1)
		if p.From() != jan1 || p.To() != jan1.AddMonths(12).AddDays(-1) {
			panic(fmt.Sprintf("oregontaxcalc: the factor from %v to %v is not one calendar year", p.From(), p.To()))
		}
	}
	estimatedYears[y.year] = y
}

// EstimatedTax returns Oregon's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
//
// This year's tax, [estimated.Figures.Tax], is the tax after all credits,
// refundable ones included, as line 3 of the Form OR-10 required annual
// payment worksheet: Form OR-40 line 31 less lines 32 and 37 to 39 on the 2025
// form. Last year's figures carry the same amount for that year, and the 2025
// kicker is among those credits. [estimated.Facts.FarmShare] and FishShare are
// the larger of last year's shares and this year's, since either year's
// two-thirds is enough.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, oregon, year)
	}
	return oregonEstimated{y}, nil
}

// EstimatedCalendar returns the Oregon legal holidays that move a tax year's
// estimated tax due dates, or [estimated.ErrNoRules] for a year this build
// does not carry.
func EstimatedCalendar(year int) (estimated.Calendar, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return estimated.Calendar{}, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, oregon, year)
	}
	return y.calendar, nil
}

type oregonEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// first is the installment a late starter's payments start at, from 0.
	first int
	// counted is, for a part-year resident, the installment periods the
	// required annual payment is divided over; nil for anyone else.
	counted []bool

	// required is each installment's required payment.
	required []money.USCents

	// What each divergence turns on in these facts.
	grossNotMet, worksheetDiffers, exactlyThreshold, priorDiffers bool

	notes working.Notes
}

func (r oregonEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("oregontaxcalc: the facts carry %T, not oregontaxcalc.EstimatedFacts", s)
	}
}

func (r oregonEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != oregon:
		return fmt.Errorf("oregontaxcalc: these are Oregon's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("oregontaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("oregontaxcalc: annualised income worksheet period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// partYear reports residence in Oregon for part of the year only. A filer
// who gives no residence, or none in Oregon, is not a part-year resident.
func partYear(f estimated.Facts) bool {
	var days int
	for _, p := range f.Residence() {
		if p.State() == string(oregon) {
			days += p.To().DaysSince(p.From()) + 1
		}
	}
	jan1 := estimated.MustDate(f.TaxYear(), 1, 1)
	return days > 0 && days < jan1.AddMonths(12).DaysSince(jan1)
}

// divergences are where the department's documents and Oregon's statutes or
// rules part ways on a rule every Oregon computation uses. They are found in
// research/estimated-payments/or.md, section 10 and its verification, and
// checked against the extracts in testdata/. Each is marked as affecting
// these facts only where the readings give them different figures.
func (r oregonEstimated) divergences(f estimated.Facts, a assessment) ([]working.Divergence, error) {
	short := false
	if !a.exempt && a.requirement.TargetKnown() {
		schedule, _, err := r.installments(f, a)
		if err != nil {
			return nil, err
		}
		for _, i := range schedule {
			short = short || i.Shortfall() > 0
		}
	}
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Who must declare").
			Form("Only the $1,000 test on tax after credits less withholding decides who must pay. There is no gross income test.",
				"Publication OR-ESTIMATE (2026), In general; Form OR-10 instructions (2025), Do I owe interest").
			Statute("Only two cases require a declaration. One is expected gross income with more than $1,000 other than wages. The other is expected gross income over $20,000, $10,000 or $5,000, by case. The interest sections don't refer to this test.",
				"ORS 316.563(1); ORS 316.587, 316.588").
			Uses(working.ReadingForm).Affects(a.grossNotMet),
		working.NewDivergence("When payments are required").
			Form("Publication OR-ESTIMATE worksheet line 23 looks at the required annual payment. It requires payments only when that less withholding is $1,000 or more. Form OR-10 instead tests tax after credits less withholding. It charges interest on any shortfall once that is $1,000 or more.",
				"Publication OR-ESTIMATE (2026), worksheet line 23; Form OR-10 instructions (2025), required annual payment worksheet line 5").
			Statute("No interest only when tax after credits, less withholding, is under $1,000. The rule looks at the required annual payment less withholding. Payments aren't required when that is under $1,000.",
				"ORS 316.588(1); OAR 150-316-0493(3)(d)").
			Uses(working.ReadingForm).Affects(a.worksheetDiffers),
		working.NewDivergence("Exactly $1,000").
			Form("Publication OR-ESTIMATE page 6 tests tax after credits less withholding. It charges interest when that is more than $1,000. Its page 1 and Form OR-10 line 5 use $1,000 or more.",
				"Publication OR-ESTIMATE (2026), pages 1 and 6; Form OR-10 instructions (2025), required annual payment worksheet line 5").
			Statute("The requirement and the interest are waived only below $1,000. Exactly $1,000 is therefore charged.",
				"ORS 316.563(2), 316.588(1); OAR 150-316-0465(1)(a)").
			Uses(working.ReadingForm).Affects(a.exactlyThreshold),
		working.NewDivergence("The prior-year return").
			Form("The prior-year test needs a 12-month 2025 return filed before the 2026 return. A part-year return qualifies.",
				"Publication OR-ESTIMATE (2026), Safe harbor; OAR 150-316-0493(3)(a)(B), (b)").
			Statute("The test needs only a 12-month prior year and the return filed for it.",
				"ORS 316.587(8)(b)").
			Uses(working.ReadingForm).Affects(a.priorDiffers),
		working.NewDivergence("A requirement first met after April 1").
			Form("Publication OR-ESTIMATE has a late starter pay in 3, 2 or 1 equal installments. They start June 15, September 15 or January 15. The Form OR-10 instructions divide the required annual payment by four. That applies to everyone but part-year residents and annualisers.",
				"Publication OR-ESTIMATE (2026), If income starts or changes after April 1; Form OR-10 instructions (2025), line 3").
			Statute("A declaration may first be required after April 1, June 1 or September 1. It is then paid in three, two or one equal installments. Each required installment follows that schedule.",
				"ORS 316.577, 316.579(3) to (5), 316.587(8)").
			Uses(working.ReadingStatute).Affects(a.first > 0),
		working.NewDivergence("The daily interest factor").
			Form("Each day is charged at a printed factor of 0.000219 for 2026. That is 8% divided by 365 and rounded to six places. Each worksheet line is rounded to a whole dollar.",
				"Form OR-10 instructions (2025), line 4 and the underpayment interest worksheet").
			Statute("Interest is 8% a year from January 1, 2026. The daily rate is the annual rate divided by 365, without rounding.",
				"ORS 316.587(1), 305.220; OAR 150-305-0140(1), (3)(b)").
			Uses(working.ReadingForm).Affects(short),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// shortDate is "Jan 15" inside the tax year and "Jan 15, 2027" outside it.
func shortDate(d estimated.Date, year int) string {
	m := d.Month().String()[:3]
	if d.Year() == year {
		return fmt.Sprintf("%s %d", m, d.Day())
	}
	return fmt.Sprintf("%s %d, %d", m, d.Day(), d.Year())
}

// targetWhy says what the target leaves to pay after withholding, for a Why.
func targetWhy(target, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("Your target is %s, in equal installments.", target.Whole())
	}
	return fmt.Sprintf("Your target is %s. After withholding that's %s, in equal installments.", target.Whole(), (target - withheld).Whole())
}

// leftWhy says what estimated payments the target leaves after
// withholding, for a Why.
func leftWhy(target, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("That's %s of estimated payments, in equal installments.", target.Whole())
	}
	return fmt.Sprintf("After %s of withholding, that's %s of estimated payments. Pay it in equal installments.", withheld.Whole(), (target - withheld).Whole())
}

// owedWhy says what tax less withholding leaves, for a Why.
func owedWhy(tax, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("Your Oregon tax is %s, with no withholding.", tax.Whole())
	}
	return fmt.Sprintf("Your Oregon tax of %s less %s withheld %s.", tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
}

// assess is the requirement and installments, with the divergences for
// these facts.
func (r oregonEstimated) assess(f estimated.Facts) (assessment, error) {
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

func (r oregonEstimated) measure(f estimated.Facts) (assessment, error) {
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

	// 90% of this year's tax after all credits.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("ORS 316.587(8)(a); OAR 150-316-0493(3)(a)(A); Publication OR-ESTIMATE (2026), worksheet line 20a",
			fmt.Sprintf("%v of this year's Oregon tax after credits of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// 100% of last year's tax after all credits, on a 12-month return filed
	// before this year's.
	filedFirst, filedFirstAnswered := extras.PriorReturnFiledFirst()
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	// Questions about the target, asked only when no exception sets it.
	var unlessExempt []func()
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("No %d Oregon return on record", last))
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Oregon return, and what was its tax?", last),
				fmt.Sprintf("With it, %v of last year's tax may be a lower target than %v of this year's.", y.priorYearShare, y.thisYearShare))
		})
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Oregon return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Oregon return covered %d months, not 12", last, prior.Months()))
	case filedFirstAnswered && !filedFirst:
		priorOption = unavailable(fmt.Sprintf("Your %d Oregon return wasn't filed before your %d return", last, y.year))
		if pa := y.priorYearShare.Of(priorTax); !taxKnown || pa < thisYear.Amount() {
			a.priorDiffers = true
		}
	default:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, y.priorYearShare.Of(priorTax))
		a.notes.Rule("ORS 316.587(8)(b); OAR 150-316-0493(3)(a)(B), (b); Publication OR-ESTIMATE (2026), worksheet line 20b",
			fmt.Sprintf("%v of the %v tax after all credits on the %d return is %v; Oregon has no higher percentage at any income, a part-year return qualifies, "+
				"and the credits Form OR-10 subtracts include the kicker (2025 Form OR-40 line 32), so a %d kicker lowers this figure",
				y.priorYearShare, priorTax, last, y.priorYearShare.Of(priorTax), last))
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
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Oregon tax after credits to be?", y.year),
				estimated.WithoutThisYearTax(priorOpt.Available()))
		})
	}
	if priorOpt.Available() && chosen == estimated.BasisPriorYearTax && !filedFirstAnswered {
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Will you file your %d Oregon return before your %d one?", last, y.year),
				fmt.Sprintf("Only then can %v of last year's tax set your target; the figures assume it.", y.priorYearShare))
		})
	}

	// Exception 2: no liability last year, as a full-year resident, for 12
	// months.
	noLiability := hasPrior && prior.Months() == 12 && ((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	resident, residenceAnswered := prior.FullYearResident()
	if noLiability && !residenceAnswered {
		a.notes.Ask(working.PositionResidency, fmt.Sprintf("Were you an Oregon resident all of %d?", last),
			fmt.Sprintf("If you were, having no Oregon tax that year means no interest for %d.", y.year))
	}
	if noLiability && residenceAnswered && !resident {
		a.notes.Rule("ORS 316.587(5)(a); Form OR-10 instructions (2025), Exception 2",
			fmt.Sprintf("Exception 2 is not open to a filer who was not a full-year resident in %d, though the prior-year test still is", last))
	}

	// ORS 316.563(1)'s gross income test, which the forms do not apply.
	grossMet := true
	if gross, ok := f.ThisYear().GrossIncome(); !ok || gross <= y.grossIncomeTest {
		met, answered := extras.GrossIncomeTestMet()
		if answered {
			grossMet = met
		}
		if !answered && !f.FarmerOrFisher() && !(noLiability && resident) && !(taxKnown && tax-withheld < y.threshold) {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Is your gross income over %s, or over $1,000 other than wages?", y.grossIncomeTest.Whole()),
				fmt.Sprintf("Some married filers use $10,000 or $5,000 in place of %s. If not, the law needs no declaration; forms still charge interest on shortfalls.", y.grossIncomeTest.Whole()))
		}
	}

	switch {
	case f.FarmerOrFisher():
		a.exempt = true
		ex, err := estimated.NewException("Two-thirds of gross income from farming or fishing").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("At least two-thirds of your gross income is from farming or fishing. Oregon requires no estimated payments for %d. It charges no underpayment interest.", y.year)).Exempt(ex)
		a.notes.Rule("ORS 316.573(1); Form OR-10 instructions (2025), Exception 1; Publication OR-ESTIMATE (2026), Exception for farmers and commercial fishermen",
			fmt.Sprintf("farming and fishing are %v and %v of gross income, at least two-thirds together, so no estimated payments are required and there is no interest", f.FarmShare(), f.FishShare()))
	case noLiability && residenceAnswered && resident:
		a.exempt = true
		ex, err := estimated.NewException("No Oregon tax last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no Oregon tax for %d as a full-year resident on a 12-month return. No estimated payments are needed. Oregon charges no underpayment interest for %d.", last, y.year)).Exempt(ex)
		a.notes.Rule("ORS 316.587(5)(a); Form OR-10 instructions (2025), Exception 2", fmt.Sprintf("the %d return showed no tax after credits for a full-year resident over 12 months, so there is no interest for %d", last, y.year))
	case taxKnown && tax-withheld < y.threshold:
		a.exempt = true
		below, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("%s That is under %s, so no estimated payments are needed. No underpayment interest is charged.", owedWhy(tax, withheld), y.threshold.Whole())
		switch {
		case withheld > tax:
			why = fmt.Sprintf("Your Oregon tax of %s less %s withheld %s. No estimated payments are needed.", tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
		case withheld == tax:
			why = fmt.Sprintf("Your Oregon withholding of %s covers your tax of %s. No estimated payments are needed.", withheld.Whole(), tax.Whole())
		}
		rb.Required(false, why).Exempt(below)
		a.notes.Rule("ORS 316.588(1); OAR 150-316-0465(1)(a); Form OR-10 instructions (2025), required annual payment worksheet line 5",
			fmt.Sprintf("%v of tax after credits less %v of withholding is %v, under %v, so there is no interest", tax, withheld, tax-withheld, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("With no %d Oregon return, your target is %v of your %d tax.", last, y.thisYearShare, y.year)).NoTarget()
	case !grossMet:
		a.grossNotMet = true
		rb.Required(false, fmt.Sprintf("You don't meet Oregon's gross income test. The law then requires no declaration of estimated tax. Oregon's forms don't apply that test. Form OR-10 still charges interest on a shortfall against the %s needed.",
			target.Whole())).Choose(chosen)
		a.notes.Rule("ORS 316.563(1); ORS 316.587, 316.588", "the gross income test is not met, so no declaration is required; the interest sections do not refer to the test, so the interest is figured as Form OR-10 figures it")
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s covers %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), y.priorYearShare, last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. It keeps you clear of interest whatever your %d tax turns out to be. %s",
			y.priorYearShare, last, target.Whole(), y.year, leftWhy(target, withheld))).Choose(chosen)
	case withheld >= target:
		why := fmt.Sprintf("Your Oregon withholding of %s covers the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())
		if target == 0 {
			why = fmt.Sprintf("Your %d Oregon tax was $0, so your target is $0. No estimated payments are needed.", last)
		}
		rb.Required(false, why).Choose(chosen)
	case target-withheld < y.threshold:
		a.worksheetDiffers = true
		rb.Required(true, fmt.Sprintf("%s That is %s or more, so any shortfall is charged interest. Your target is %s. Paying the %s left after withholding in equal installments avoids it. Oregon's worksheet doesn't require it.",
			owedWhy(tax, withheld), y.threshold.Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("%s That is %s or more, so estimated payments are needed. %s",
			owedWhy(tax, withheld), y.threshold.Whole(), targetWhy(target, withheld))).Choose(chosen)
	}
	a.exactlyThreshold = taxKnown && tax-withheld == y.threshold
	if a.exempt {
		a.priorDiffers = false
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	if !a.exempt {
		for _, ask := range unlessExempt {
			ask()
		}
	}
	if a.exempt || !a.requirement.TargetKnown() {
		a.required = make([]money.USCents, len(a.dues))
		return a, nil
	}

	var regular []money.USCents
	if partYear(f) {
		// A part-year resident: the target in equal parts over the
		// installment periods of residence or Oregon business income.
		extras, err := r.extras(f)
		if err != nil {
			return assessment{}, err
		}
		a.counted = r.periodsCounted(f, extras, &a)
		cum, err := sharesOver(a.requirement.Target(), a.counted)
		if err != nil {
			return assessment{}, err
		}
		regular = estimated.Increments(cum)
		a.notes.Rule("Form OR-10 instructions (2025), Line 3, Note for part-year residents",
			fmt.Sprintf("%v is divided over the %d installment periods of Oregon residence or Oregon business income, %v; the other periods need $0",
				a.requirement.Target(), count(a.counted), countedList(a.counted, a.dues)))
	} else {
		// The installments: equal parts of the target, from the first date
		// the filer's requirement applies.
		a.first = r.firstInstallment(f, &a)
		cum, err := estimated.EqualShares(a.requirement.Target(), len(a.dues), a.first)
		if err != nil {
			return assessment{}, err
		}
		regular = estimated.Increments(cum)
		n := len(a.dues) - a.first
		a.notes.Rule("ORS 316.579(2) to (5); OAR 150-316-0493(4)(a)(A); Publication OR-ESTIMATE (2026), Estimated tax payment due dates",
			fmt.Sprintf("%v is due in %d equal installments of about %v, from %s; a date on a Saturday, Sunday or Oregon legal holiday moves to the next business day",
				a.requirement.Target(), n, regular[a.first], a.dues[a.first].Long()))
	}
	a.required = regular

	if f.Elections().Annualised() {
		periods := f.Periods()
		taxes := make([]money.USCents, len(periods))
		for i, p := range periods {
			taxes[i] = p.AnnualisedTax()
		}
		ann, err := estimated.Annualised(taxes, y.annualised, regular)
		if err != nil {
			return assessment{}, err
		}
		a.required = ann
		a.notes.Rule("ORS 316.587(8)(c); OAR 150-316-0493(4); Form OR-10 instructions (2025), annualized income worksheet lines 23 to 31",
			fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: 22.5%%, 45%%, 67.5%% and 90%% of the net annualised tax, less earlier installments, "+
				"and never more than the regular installment plus any amount the method lowered before", ann[0], ann[1], ann[2], ann[3]))
	}
	return a, nil
}

// firstInstallment is the installment a filer's payments start at, from 0,
// under the late-start chart in Publication OR-ESTIMATE and ORS 316.579(3)
// to (5).
func (r oregonEstimated) firstInstallment(f estimated.Facts, a *assessment) int {
	y := r.y
	met := f.RequirementFirstMet()
	if met.IsZero() {
		a.notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first expect to need estimated payments for %d?", y.year),
			fmt.Sprintf("Until then, the figures use four installments. A start after %s would mean three, two or one.", shortDate(y.lateStarts[0].AddDays(-1), y.year)))
		return 0
	}
	first := estimated.LateStart(met, y.lateStarts)
	if first > 0 {
		a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("The law and OR-ESTIMATE have you pay %d equal installments from %s. The Form OR-10 instructions say four, and the figures follow the law.",
			len(y.dues)-first, shortDate(y.dues[first], y.year)))
	}
	return first
}

// periodsCounted is which installment periods a part-year resident's
// required annual payment is divided over: each period with a day of Oregon
// residence, and each the filer had Oregon business income in. A period runs
// from the day after the previous due date, or January 1, to its own due
// date, as the instructions' "use the payment due dates as a guide" reads.
func (r oregonEstimated) periodsCounted(f estimated.Facts, extras EstimatedFacts, a *assessment) []bool {
	y := r.y
	counted := make([]bool, len(a.dues))
	partial := false
	from := estimated.MustDate(y.year, 1, 1)
	dec31 := estimated.MustDate(y.year, 12, 31)
	for i, due := range a.dues {
		to := due
		if to.After(dec31) {
			to = dec31
		}
		days := 0
		for _, p := range f.Residence() {
			if p.State() != string(oregon) {
				continue
			}
			start, end := p.From(), p.To()
			if start.Before(from) {
				start = from
			}
			if end.After(to) {
				end = to
			}
			if !end.Before(start) {
				days += end.DaysSince(start) + 1
			}
		}
		if days > 0 {
			counted[i] = true
			partial = partial || days < to.DaysSince(from)+1
		}
		from = due.AddDays(1)
	}
	business, answered := extras.BusinessIncomeIn()
	for _, n := range business {
		counted[n-1] = true
	}
	if !answered {
		a.notes.Ask(working.PositionJurisdictionFact, "Did you have Oregon business income in an installment period spent elsewhere?",
			"If so, that period counts too; until then, only your Oregon periods count.")
	}
	if partial {
		a.notes.Ask(working.PositionUnsettledRule, "", "Form OR-10 doesn't say if a partly resident period counts; the figures count it.")
	}
	if !f.RequirementFirstMet().IsZero() {
		a.notes.Ask(working.PositionUnsettledRule, "", "The Form OR-10 instructions don't say if the late-start chart applies part-year. The figures divide by your periods of residence alone.")
	}
	return counted
}

// sharesOver is the running total of total paid in equal parts on the
// counted installments, and nothing on the others.
func sharesOver(total money.USCents, counted []bool) ([]money.USCents, error) {
	n := count(counted)
	out := make([]money.USCents, len(counted))
	if n == 0 {
		return out, nil
	}
	eq, err := estimated.EqualShares(total, n, 0)
	if err != nil {
		return nil, err
	}
	k := 0
	for i, c := range counted {
		if c {
			k++
		}
		if k > 0 {
			out[i] = eq[k-1]
		}
	}
	return out, nil
}

func count(counted []bool) int {
	n := 0
	for _, c := range counted {
		if c {
			n++
		}
	}
	return n
}

// countedList names the counted installments by due date.
func countedList(counted []bool, dues []estimated.Date) string {
	var s []string
	for i, c := range counted {
		if c {
			s = append(s, dues[i].Long())
		}
	}
	return strings.Join(s, " and ")
}

// credits are the withholding deemed paid on each due date, and the dated
// payments. Withholding is spread in equal parts over the installments the
// filer owes, unless the filer shows the dates it was withheld; then it counts
// on those dates and none is deemed. Last year's overpayment applied forward
// counts on the first due date.
func (r oregonEstimated) credits(f estimated.Facts, a assessment) ([]money.USCents, []estimated.Payment, working.Notes, error) {
	var notes working.Notes
	deemed := make([]money.USCents, len(a.dues))
	var payments []estimated.Payment
	if f.Elections().WithholdingAsWithheld() {
		for _, w := range f.Withholding() {
			p, err := estimated.NewPayment().On(w.On()).Amount(w.Amount()).Build()
			if err != nil {
				return nil, nil, working.Notes{}, err
			}
			payments = append(payments, p)
		}
		notes.Rule("ORS 316.587(6); Form OR-10 instructions (2025), lines 1, 6, 11 and 17",
			fmt.Sprintf("the %v withheld is credited on the dates it was withheld, as shown", f.WithheldTotal()))
	} else if a.counted != nil {
		cum, err := sharesOver(f.WithheldTotal(), a.counted)
		if err != nil {
			return nil, nil, working.Notes{}, err
		}
		deemed = estimated.Increments(cum)
		notes.Rule("Form OR-10 instructions (2025), lines 1, 6, 11 and 17",
			fmt.Sprintf("the %v withheld is divided by the %d installment periods used for the required installments, and treated as paid in equal parts on their due dates", f.WithheldTotal(), count(a.counted)))
	} else {
		cum, err := estimated.EqualShares(f.WithheldTotal(), len(a.dues), a.first)
		if err != nil {
			return nil, nil, working.Notes{}, err
		}
		deemed = estimated.Increments(cum)
		notes.Rule("ORS 316.587(6); Form OR-10 instructions (2025), lines 1, 6, 11 and 17",
			fmt.Sprintf("the %v withheld is treated as paid in %d equal parts on the due dates, whenever it was actually withheld", f.WithheldTotal(), len(a.dues)-a.first))
	}
	for _, p := range f.Payments() {
		if p.FromPriorYear() {
			moved, err := estimated.NewPayment().On(a.dues[0]).Amount(p.Amount()).Build()
			if err != nil {
				return nil, nil, working.Notes{}, err
			}
			p = moved
		}
		payments = append(payments, p)
	}
	return deemed, payments, notes, nil
}

func (r oregonEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// installments is the schedule: withholding deemed paid on the due dates
// and the dated payments, applied to the earliest installment first.
func (r oregonEstimated) installments(f estimated.Facts, a assessment) ([]estimated.Installment, working.Notes, error) {
	deemed, payments, notes, err := r.credits(f, a)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var credits []estimated.Payment
	for i, w := range deemed {
		if w > 0 {
			p, err := estimated.NewPayment().On(a.dues[i]).Amount(w).Build()
			if err != nil {
				return nil, working.Notes{}, err
			}
			credits = append(credits, p)
		}
	}
	ledger, err := estimated.Apply(a.dues, a.required, append(credits, payments...))
	if err != nil {
		return nil, working.Notes{}, err
	}
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		if out[i], err = estimated.NewInstallment(i+1, l.Due()).NeededBy(l.NeededBy()).CreditedBy(l.CreditedBy()).
			Shortfall(l.Shortfall()).Build(); err != nil {
			return nil, working.Notes{}, err
		}
	}
	notes.Rule("ORS 316.579(10), 316.587(4)", "a payment is applied to the earliest unpaid installment first, and any excess to the next one due")
	return out, notes, nil
}

func (r oregonEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Oregon return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	out, notes, err := r.installments(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

func (r oregonEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("oregontaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Oregon return to measure against, there is no target to figure interest from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	schedule, _, err := r.installments(f, a)
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

	deemed, payments, notes, err := r.credits(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	periods, unrated, err := worksheet(a.dues, a.required, deemed, payments, stop, y.daily)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	if !a.taxKnown {
		pb.UpperBound()
	}
	for i, p := range periods {
		if !p.reached || p.shortfall <= 0 {
			continue
		}
		lb := estimated.NewPenaltyLine(i+1, a.dues[i], p.shortfall)
		days := 0
		for _, part := range p.parts {
			lb.Part(part)
			days += part.Days()
		}
		line, err := lb.Days(days).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		pb.Line(line)
	}
	pen, err := pb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		notes.Rule("ORS 316.587(8); OAR 150-316-0493(3)(a)", fmt.Sprintf("this year's Oregon tax was not given, so the interest is figured against %v of the %d tax alone; "+
			"the required annual payment is the lesser of that and 90%% of this year's tax, so %v is the most the interest can be, and it is less if this year's tax turns out lower",
			y.priorYearShare, y.year-1, pen.Total()))
	}
	if paidInFull {
		notes.Rule("ORS 316.587(3)(b); Form OR-10 instructions (2025), line 4", fmt.Sprintf("the return was filed with the whole balance paid on %s, so the balance stopped accruing interest that day", stop.Long()))
	}
	notes.Rule("ORS 316.587(1) to (4); Form OR-10 instructions (2025), line 4 and the underpayment interest worksheet",
		fmt.Sprintf("interest is charged on the running balance of installments less withholding and payments, from one event to the next, at the printed daily rate, "+
			"each line rounded to a whole dollar, until the balance is paid or %s; Oregon has no January 31 waiver; the interest to %s is %v", y.stop.Long(), stop.Long(), pen.Total()))
	if !unrated.IsZero() {
		if pen, err = pen.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", shortDate(unrated.AddDays(-1), 0)))
	}
	if pen.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Was the shortfall due to a casualty, disaster or other unusual circumstance?",
			"Retiring at 62 or older or a disability, with reasonable cause, counts too. Oregon decides case by case; until then, the interest is shown in full.")
	}
	// ORS 316.587(5)(b), (c) and Form OR-10 Exceptions 3 and 4 grant the
	// federal pair of waivers.
	if o, ok := estimated.StandardWaiver(f, "the Oregon Department of Revenue", 62); ok && pen.Total() > 0 {
		if pen, err = pen.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	return working.NewResult(pen).With(a.notes).With(notes).Build()
}

// eventKind orders the events on one date as the Form OR-10 worksheet lists
// them: withholding, then the required installment, then payments.
type eventKind int

const (
	withholdingEvent eventKind = iota
	installmentEvent
	paymentEvent
)

type worksheetEvent struct {
	on     estimated.Date
	kind   eventKind
	amount money.USCents
}

// periodInterest is one installment's block of the worksheet.
type periodInterest struct {
	// reached is whether the installment fell due by the stop date.
	reached bool

	// shortfall is the running balance just after the installment fell due,
	// which includes any earlier installment still unpaid.
	shortfall money.USCents

	// parts are the worksheet lines in the block that charged interest.
	parts []estimated.PenaltyPart
}

// worksheet is the Form OR-10 underpayment interest worksheet. It is the one
// place Oregon's arithmetic differs from estimated.Apply and
// estimated.SimplePenalty: the interest is on the running balance between
// events, and each line is rounded to a whole dollar, rather than on each
// installment's shortfall piece by piece.
//
// Each due date has a withholding line, then a required installment line;
// each payment is a line. The withholding line charges the balance after the
// withholding for that one day. Every other line charges its balance for the
// days after its date up to and including the next event's date, or the day
// before it when the next event is a withholding line. A line whose days fall
// in two factor periods is split, as the worksheet's rate change line splits
// it. Lines after stop are not reached.
//
// unrated is the first day no factor covers, or the zero Date.
func worksheet(dues []estimated.Date, required, deemed []money.USCents, payments []estimated.Payment, stop estimated.Date, s estimated.FactorSeries) ([]periodInterest, estimated.Date, error) {
	if len(required) != len(dues) || len(deemed) != len(dues) {
		return nil, estimated.Date{}, errors.New("oregontaxcalc: one required installment and one withholding amount per due date")
	}
	var evs []worksheetEvent
	for i, d := range dues {
		evs = append(evs, worksheetEvent{d, withholdingEvent, deemed[i]}, worksheetEvent{d, installmentEvent, required[i]})
	}
	for _, p := range payments {
		evs = append(evs, worksheetEvent{p.On(), paymentEvent, p.Amount()})
	}
	sort.SliceStable(evs, func(i, j int) bool {
		if evs[i].on != evs[j].on {
			return evs[i].on.Before(evs[j].on)
		}
		return evs[i].kind < evs[j].kind
	})

	out := make([]periodInterest, len(dues))
	period := -1
	var balance money.USCents
	var unrated estimated.Date
	for k, e := range evs {
		if e.on.After(stop) {
			break
		}
		switch e.kind {
		case withholdingEvent:
			period++
			balance -= e.amount
		case installmentEvent:
			balance += e.amount
			out[period].reached = true
			out[period].shortfall = max(balance, 0)
		case paymentEvent:
			balance -= e.amount
		}
		from, to := e.on, stop
		switch {
		case e.kind == withholdingEvent:
			from = e.on.AddDays(-1)
			to = e.on
		case k+1 < len(evs) && evs[k+1].kind == withholdingEvent:
			to = evs[k+1].on.AddDays(-1)
		case k+1 < len(evs):
			to = evs[k+1].on
		}
		if stop.Before(to) {
			to = stop
		}
		if period < 0 || balance <= 0 || !to.After(from) {
			continue
		}
		parts, gap, err := estimated.DailyFactor(balance, from, to, s)
		if err != nil {
			return nil, estimated.Date{}, err
		}
		if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
			unrated = gap
		}
		out[period].parts = append(out[period].parts, parts...)
	}
	return out, unrated, nil
}

func (r oregonEstimated) Explanation() estimated.Explanation { return oregonExplanation }

// oregonExplanation is written by hand and checked against
// research/estimated-payments/or.md.
var oregonExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Take your 2026 Oregon tax after credits, less withholding. If that is $1,000 or more, Oregon expects estimated payments. The charge for paying late is interest on the unpaid balance, by the day.").
		Paragraph("There is no interest if withholding and estimated payments reach a target. It is the lesser of 90% of this year's tax after credits or 100% of last year's. Last year's figure is 100% at every income, with no 110% tier. It needs a full 12-month return filed before this year's. If you had no Oregon return last year, only the 90% figure is available.").
		Paragraph("Pay a quarter by April 15, June 15, September 15 and the next January 15. You can instead pay the whole amount by April 15. A date on a weekend or an Oregon legal holiday shifts to the next business day. Withholding counts as paid evenly on those dates. You can instead show when it was withheld. If you first need to pay after April 1, you pay fewer, larger installments. They are three, two or one equal installments from the next date.").
		Paragraph("The interest runs on the unpaid balance from one event to the next. Each installment raises the balance, and withholding and payments lower it. Payments go to the oldest unpaid installment first. The rate is 8% for 2026, charged at a daily rate the department prints. Interest stops when the balance is paid or on April 15 of the next year. Uneven income can use the annualised method on Form OR-10. It uses 22.5%, 45%, 67.5% and 90% of the annualised tax.").
		Paragraph("There is no interest if your tax after credits less withholding is under $1,000. There is none either for a full-year resident with no Oregon tax last year. The department can also excuse it for a casualty or disaster. It can do the same for retiring at 62 or older or becoming disabled. That needs reasonable cause.").
		VersusFederal("Unlike federal, the prior-year test is 100% at every income, with no 110% tier. Due dates move for Oregon's own legal holidays. The charge is interest on a running balance, with each line in whole dollars. A late starter pays in fewer, larger installments.").
		Source("ORS 316.557 to 316.589, 305.220 and 187.010 (2025 edition)").
		Source("OAR 150-316-0465 to 150-316-0497; OAR 150-305-0140 and 150-305-0144").
		Source("Publication OR-ESTIMATE, Oregon Estimated Income Tax Instructions, 2026 (150-101-026, Rev. 10-07-25)").
		Source("Form OR-10 Instructions, Underpayment of Oregon Estimated Tax, 2025 (150-101-031-1, Rev. 9-16-25)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
