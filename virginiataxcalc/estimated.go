package virginiataxcalc

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Virginia's estimated tax rules, Code of Virginia §§ 58.1-490 to 58.1-492
// and § 58.1-15, as the shared [estimated.EstimatedTax] interface.
//
// Virginia follows the federal outline with different dates and a different
// order. The four installments are due May 1, June 15, September 15 and
// January 15. Each is measured against the lesser of 90% of this year's tax
// and 100% of last year's. A payment made on an installment date pays that
// installment before any earlier shortfall. Four exceptions cancel the
// addition to tax for an installment without lowering its shortfall, and
// there is no addition when the year's shortfalls total $1,000 or less. The
// addition is interest at the federal underpayment rate plus 2%.
//
// The figure follows the 2026 Form 760ES and the 2025 Form 760C, the latest
// published, and the result's Divergences say where the Code reads otherwise.
// One rule on the 2025 Form 760C predates the law for 2026: 2026 Acts c. 7
// raised the small-underpayment exception from $150 to $1,000 for taxable
// years from January 1, 2026, so the law's $1,000 is computed.

// jurisdiction is Virginia's postal code.
const jurisdiction = estimated.Virginia

// EstimatedFacts are the facts Virginia's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorYearIncomeTax optional
	incomeToDateTax    []money.USCents
	merchantSeaman     money.Rate
	seamanAnswered     bool
}

type optional struct {
	v   money.USCents
	set bool
}

// PriorYearIncomeTax is the tax on last year's income at this year's rates
// and personal exemptions, less last year's Spouse Tax Adjustment and
// credits: the Form 760C Exception 2 Worksheet, line 7. Payments that reach it
// by each date cancel the addition under Exception 2, whether or not a return
// was filed last year.
func (e EstimatedFacts) PriorYearIncomeTax() (money.USCents, bool) {
	return e.priorYearIncomeTax.v, e.priorYearIncomeTax.set
}

// IncomeToDateTax is the Virginia tax on the taxable income actually received
// from January 1 to April 30, May 31 and August 31: Form 760C line 19f, one
// figure for each period. Exception 4 compares payments with 90% of it. It is
// absent when the caller did not give it.
func (e EstimatedFacts) IncomeToDateTax() ([]money.USCents, bool) {
	return append([]money.USCents(nil), e.incomeToDateTax...), e.incomeToDateTax != nil
}

// MerchantSeamanShare is the share of gross income from working as a merchant
// seaman, which counts with farming and fishing toward the two-thirds that
// puts a filer under Form 760F, and whether the caller said.
func (e EstimatedFacts) MerchantSeamanShare() (share money.Rate, answered bool) {
	return e.merchantSeaman, e.seamanAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorYearIncomeTax(a money.USCents) *EstimatedFactsBuilder {
	b.e.priorYearIncomeTax = optional{a, true}
	return b
}

// IncomeToDateTax gives the tax on the income received to April 30, May 31
// and August 31.
func (b *EstimatedFactsBuilder) IncomeToDateTax(apr30, may31, aug31 money.USCents) *EstimatedFactsBuilder {
	b.e.incomeToDateTax = []money.USCents{apr30, may31, aug31}
	return b
}

func (b *EstimatedFactsBuilder) MerchantSeamanShare(share money.Rate) *EstimatedFactsBuilder {
	b.e.merchantSeaman, b.e.seamanAnswered = share, true
	return b
}

// Build refuses a negative tax and a share outside 0% to 100%.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	e := b.e
	if b.e.incomeToDateTax != nil {
		e.incomeToDateTax = append([]money.USCents(nil), b.e.incomeToDateTax...)
	}
	if e.priorYearIncomeTax.v < 0 {
		return EstimatedFacts{}, errors.New("virginiataxcalc: the tax on last year's income cannot be negative")
	}
	for _, t := range e.incomeToDateTax {
		if t < 0 {
			return EstimatedFacts{}, errors.New("virginiataxcalc: the tax on income to date cannot be negative")
		}
	}
	if e.merchantSeaman < 0 || e.merchantSeaman > money.Percent(100) {
		return EstimatedFacts{}, fmt.Errorf("virginiataxcalc: a merchant seaman share of %v is not a share", e.merchantSeaman)
	}
	return e, nil
}

// ratePeriod is the rate for a run of days, and how the form charges it:
// at a daily factor the form prints, or at the annual rate times the days
// over 365 where it prints none.
type ratePeriod struct {
	from, to estimated.Date
	rate     money.Rate
	printed  bool
	factor   estimated.FactorSeries
	series   estimated.RateSeries
	source   string
}

// newRatePeriod builds a period at a printed daily factor, or at the rate
// alone when factor is empty. It panics on a period that does not build,
// because the periods are written in source.
func newRatePeriod(from, to estimated.Date, rate money.Rate, factor, source string) ratePeriod {
	p := ratePeriod{from: from, to: to, rate: rate, source: source}
	var err error
	if factor != "" {
		var f estimated.Factor
		if f, err = estimated.ParseFactor(factor); err == nil {
			p.printed = true
			p.factor, err = estimated.NewFactorSeries(estimated.RoundToCent).Period(from, to, f, rate, source).Build()
		}
	} else {
		p.series, err = estimated.NewRateSeries().Period(from, to, rate, source).Build()
	}
	if err != nil {
		panic(err)
	}
	return p
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// thisYearShare and priorYearShare are § 58.1-492(A)(1): the
	// underpayment is measured against the lesser of 90% of this year's tax
	// and 100% of last year's.
	thisYearShare, priorYearShare money.Rate

	// filingThreshold is Form 760ES Section I: payments are required when
	// tax after withholding and credits is expected to be more than it.
	filingThreshold money.USCents

	// smallUnderpayment is § 58.1-492(D)(3): no addition when the year's
	// installment underpayments total this or less.
	smallUnderpayment money.USCents

	// agiSingle, agiJoint and agiSeparate are the VAGI below which no return
	// and no estimated payments are required.
	agiSingle, agiJoint, agiSeparate money.USCents

	dues []estimated.Date

	// laterGroups are the first days of the Form 760ES payment schedule's
	// second, third and fourth rows: a requirement first met on or after
	// laterGroups[i] starts at installment i+2.
	laterGroups []estimated.Date

	calendar     estimated.Calendar
	fileAndPayBy estimated.Date
	stop         estimated.Date

	// periodEnds are the Form 760C Exception 3 and 4 periods. The fourth,
	// which estimated.Facts requires, ends the year and is not read.
	periodEnds []estimated.Date

	// exceptionShares are Form 760C lines 16 and 17: the share of last
	// year's figure that payments must reach by each date.
	exceptionShares []money.Rate

	// annualised are Form 760C line 18g, and incomeToDate line 19g.
	annualised   []money.Rate
	incomeToDate money.Rate

	rates []ratePeriod
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters divide withholding into four equal parts, Form 760C line 8.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Virginia's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return virginiaEstimated{y}, nil
}

type virginiaEstimated struct{ y estimatedYear }

// exception is one of Form 760C's Part II exceptions: the running total that
// cancels the addition for an installment, for the installments it covers.
type exception struct {
	name   string
	needed []money.USCents
	covers int
}

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	dues        []estimated.Date

	// taxKnown is whether this year's tax was given. regularKnown is whether
	// the § 58.1-492(A)(1) installments could be measured at all: without
	// this year's tax they are measured against last year's, which can only
	// be too high, and with no return last year they cannot be measured.
	taxKnown, regularKnown bool

	// regular is each installment § 58.1-492(A)(1) and Form 760C line 7
	// require, and lesser their running total.
	regular, lesser []money.USCents

	exceptions []exception

	// first is the installment a late starter's requirement starts at, from 0.
	first          int
	excusedJanuary bool

	notes working.Notes
}

// affects records, for each divergence, whether its readings give these facts
// different figures or answers.
type affects struct {
	required, small, january, lateStart, lateExceptions, withholding, dailyRate bool
}

func (r virginiaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("virginiataxcalc: the facts carry %T, not virginiataxcalc.EstimatedFacts", s)
	}
}

func (r virginiaEstimated) check(f estimated.Facts, e EstimatedFacts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("virginiataxcalc: these are Virginia's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("virginiataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	}
	share := f.FarmShare() + f.FishShare() + e.merchantSeaman
	if share > money.Percent(100) {
		return fmt.Errorf("virginiataxcalc: farming %v, fishing %v and merchant seaman %v are not shares of one income", f.FarmShare(), f.FishShare(), e.merchantSeaman)
	}
	if int64(share)*3 >= 20_000 {
		return fmt.Errorf("%w: a filer with two-thirds of gross income from farming, fishing or work as a merchant seaman files Form 760F, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("%w: Form 760C Exception 3 period %d ends on %v, and a period ending %v is not one Virginia uses",
					estimated.ErrUnsupported, i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the Department's forms and the Code of Virginia or
// its regulations part ways on a rule a Virginia computation uses. They come
// from research/estimated-payments/va.md and from reading the documents in
// testdata/ against each other.
func (r virginiaEstimated) divergences(af affects) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("When estimated payments are required").
			Form("Payments are due if tax after withholding and credits will be more than $1,000. The exception is Virginia AGI under a floor for your filing status. The floor is $11,950 single or separate, and $23,900 married filing jointly.",
				"Form 760ES (2026, Rev. 09/25), Section I").
			Statute("The amount is left to regulation. The regulation still requires a declaration if estimated tax is more than $150. Its AGI floors are $5,000 single and $8,000 married.",
				"Code of Virginia § 58.1-490(A); 23VAC10-112-20").
			Uses(working.ReadingForm).Affects(af.required),
		working.NewDivergence("The small-underpayment exception").
			Form("The 2025 Form 760C cancels the charge when shortfalls total $150 or less. That is the 2025 figure. No 2026 Form 760C is published. The 2026 Form 760ES prints the 2026 law's $1,000.",
				"Form 760C (2025, Rev. 04/26), line 14; Form 760ES (2026, Rev. 09/25), Section VIII").
			Statute("No charge for an underpayment of $1,000 or less for the year. That is as amended by 2026 Acts c. 7, for taxable years from January 1, 2026.",
				"Code of Virginia § 58.1-492(D)(3); 2026 Acts c. 7, fifth and sixth enactments").
			Uses(working.ReadingStatute).Affects(af.small),
		working.NewDivergence("The January installment for a return filed by March 1").
			Form("A return filed and paid in full by March 1 excuses the January 15 payment. That holds for any filer. Form 760C has no such rule.",
				"Form 760ES (2026, Rev. 09/25), Section IV; Form 760C (2025) and its instructions").
			Statute("A return filed and paid by March 1 can count as the declaration. It counts only if your declaration was first due January 15. Otherwise it counts as an amendment of it.",
				"Code of Virginia § 58.1-490(I)").
			Uses(working.ReadingForm).Affects(af.january),
		working.NewDivergence("A requirement first met after April 15").
			Form("A requirement first met after April 15 is paid in thirds, halves or in full. They start June 15, September 15 or January 15, in that order. Even so, withholding is still divided by four. Exceptions 1 and 2 still use 25% steps.",
				"Form 760ES (2026), Payment Schedule; Form 760C (2025), lines 5, 8, 16 and 17, and the instructions for line 5").
			Statute("The June 15 group states no starting date. Withholding is an equal part on each installment date that applies. Each exception is tested against what would have been required by the date.",
				"Code of Virginia §§ 58.1-490(E), 58.1-491(A), 58.1-492(C)(1) and (D)(2)").
			Uses(working.ReadingForm).Affects(af.lateStart),
		working.NewDivergence("Exceptions 3 and 4 and the January installment").
			Form("Exceptions 3 and 4 do not apply to the fourth installment.",
				"Form 760C (2025), Part II, lines 18 and 19, and the instructions for them").
			Statute("The annualized and actual-income exceptions apply to any installment. They use the months ending before the month it's due.",
				"Code of Virginia § 58.1-492(C)(1)(c) and (C)(2)").
			Uses(working.ReadingForm).Affects(af.lateExceptions),
		working.NewDivergence("Withholding on the dates withheld").
			Form("Withholding entered by installment period counts in that period's column. Any excess over the installment pays an earlier shortfall on that date.",
				"Form 760C (2025), lines 8 and 11, and the instructions for them").
			Statute("Withholding counts as paid on the dates it was actually withheld. It pays an earlier shortfall on those dates.",
				"Code of Virginia § 58.1-492(B)(2) and (D)(2)").
			Uses(working.ReadingForm).Affects(af.withholding),
		working.NewDivergence("The daily rate").
			Form("Each day at 9% is charged at a printed daily rate of .00025. That is 9.125% over 365 days.",
				"Form 760C (2025), line 23; Form 760F (2025), line 11").
			Statute("The charge is at the rate for the period, 9% a year for the second half of 2026. The regulation applies it as the rate times the days over 365.",
				"Code of Virginia §§ 58.1-15(A), 58.1-492(A); 23VAC10-112-42, Example 1").
			Uses(working.ReadingForm).Affects(af.dailyRate),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// date is d as a person reads it: "Dec 15" in the tax year, "Jan 15, 2027"
// outside it.
func (r virginiaEstimated) date(d estimated.Date) string {
	if d.Year() == r.y.year {
		return d.Time().Format("Jan 2")
	}
	return d.Time().Format("Jan 2, 2006")
}

// agiFloor is the VAGI below which no return is required, and false for a
// filing status Form 760ES gives none for.
func (y estimatedYear) agiFloor(s filing.Status) (money.USCents, bool) {
	switch s {
	case filing.Single:
		return y.agiSingle, true
	case filing.MarriedFilingJointly:
		return y.agiJoint, true
	case filing.MarriedFilingSeparately:
		return y.agiSeparate, true
	}
	return 0, false
}

func (r virginiaEstimated) assess(f estimated.Facts) (assessment, error) {
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	if err := r.check(f, extras); err != nil {
		return assessment{}, err
	}
	y := r.y
	last := y.year - 1
	var a assessment
	var af affects
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

	agi, agiKnown := f.ThisYear().StateAGI()
	floor, hasFloor := y.agiFloor(f.FilingStatus())
	// Under the income floor no target is needed, so nothing that sets one is asked.
	agiExempt := agiKnown && hasFloor && agi < floor

	// 90% of this year's tax.
	thisOpt := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisOpt = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisOpt.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("Code of Virginia § 58.1-492(A)(1); Form 760C (2025), lines 1 and 2", fmt.Sprintf("%v of this year's Virginia tax of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// 100% of the tax shown on last year's return, § 58.1-492(A)(1) and Form
	// 760C line 3. Neither states a condition of liability or of twelve
	// months; Exception 1 has both.
	var priorOpt *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOpt = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).
			Unavailable(fmt.Sprintf("Needs your %d Virginia return", last))
		if !agiExempt {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Virginia return, and what was its tax?", last),
				fmt.Sprintf("With it, %v of last year's tax could set a lower target.", y.priorYearShare))
		}
	case !prior.Filed():
		priorOpt = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).
			Unavailable(fmt.Sprintf("You didn't file a %d Virginia return", last))
		if !agiExempt {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Was any %d Virginia tax due from you, though you didn't file a return?", last),
				"Neither the Code nor Form 760C says whether last year then counts as zero. Your target leaves it out, which can only raise it.")
		}
	default:
		priorOpt = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, y.priorYearShare.Of(priorTax))
		a.notes.Rule("Code of Virginia § 58.1-492(A)(1); Form 760C (2025), lines 3 and 4", fmt.Sprintf("%v of the %v of tax on the %d return is %v", y.priorYearShare, priorTax, last, y.priorYearShare.Of(priorTax)))
	}
	priorYear, err := priorOpt.Build()
	if err != nil {
		return assessment{}, err
	}

	// Exception 2: last year's income at this year's rates, which needs no
	// return last year.
	incomeTax, incomeKnown := extras.PriorYearIncomeTax()
	incomeOpt := estimated.NewTargetOption(estimated.BasisPriorYearIncome, y.priorYearShare, incomeTax)
	if !incomeKnown {
		incomeOpt = estimated.NewTargetOption(estimated.BasisPriorYearIncome, y.priorYearShare, 0).
			Unavailable(fmt.Sprintf("Needs the tax on your %d income at %d rates", last, y.year))
	}
	income, err := incomeOpt.Build()
	if err != nil {
		return assessment{}, err
	}

	rb := estimated.NewRequirement().Option(thisYear).Option(priorYear).Option(income)
	var chosen estimated.TargetOption
	found := false
	for _, o := range []estimated.TargetOption{thisYear, priorYear, income} {
		if o.Available() && (!found || o.Amount() < chosen.Amount()) {
			chosen, found = o, true
		}
	}
	target := chosen.Amount()

	// The installments § 58.1-492(A)(1) requires: the lesser of the two
	// measures, or last year's alone when this year's tax is not known.
	var regularOf money.USCents
	switch {
	case taxKnown && priorYear.Available():
		regularOf, a.regularKnown = min(thisYear.Amount(), priorYear.Amount()), true
	case taxKnown:
		regularOf, a.regularKnown = thisYear.Amount(), true
	case priorYear.Available():
		regularOf, a.regularKnown = priorYear.Amount(), true
	default:
		regularOf = target
	}

	if !taxKnown && !agiExempt {
		var effect string
		switch {
		case priorYear.Available():
			effect = fmt.Sprintf("Without it, each installment is measured against your %d tax. That can only be too high.", last)
		case found:
			effect = fmt.Sprintf("Without it, your target is the tax on your %d income at %d rates. No charge can be figured for an installment that misses it.", last, y.year)
		default:
			effect = fmt.Sprintf("It's the only figure that can set your target, at %v. It also decides the %s filing test.", y.thisYearShare, y.filingThreshold.Whole())
		}
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Virginia tax to be?", y.year), effect)
	}

	switch {
	case agiExempt:
		a.exempt = true
		ex, err := estimated.ExceptionBelow("Virginia adjusted gross income", agi, floor)
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your Virginia adjusted gross income of %s is under %s. You don't need to file a Virginia return or make estimated payments.", agi.Whole(), floor.Whole())).Exempt(ex)
		a.notes.Rule("Code of Virginia § 58.1-321(A); Form 760ES (2026), Section I; Form 760C (2025) instructions, Who Must File", fmt.Sprintf("VAGI of %v for a %v filer is under %v, so no return is required and no Form 760C is needed", agi, f.FilingStatus(), floor))
	case !found:
		rb.Required(false, fmt.Sprintf("Your target is the smallest of three figures. They are %v of your %d Virginia tax and %v of your %d tax. The third is the tax on your %d income at %d rates.",
			y.thisYearShare, y.year, y.priorYearShare, last, last, y.year)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your target is %s, %s. Your withholding of %s already covers it. No estimated payments are needed, whatever your %d tax turns out to be.",
			target.Whole(), basisWords(chosen.Basis(), last, y.year), withheld.Whole(), y.year)).Choose(chosen.Basis())
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %s, %s. It keeps you clear of the charge whatever your %d tax turns out to be. After %s of withholding, that's %s of estimated payments.",
			target.Whole(), basisWords(chosen.Basis(), last, y.year), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen.Basis())
	case tax-withheld <= y.filingThreshold:
		why := fmt.Sprintf("Your Virginia tax of %s less %s of withholding is %s or less. Form 760ES therefore doesn't require estimated payments.",
			tax.Whole(), withheld.Whole(), y.filingThreshold.Whole())
		if tax < withheld {
			why = fmt.Sprintf("Your Virginia tax of %s less %s withheld %s. Form 760ES therefore doesn't require estimated payments.",
				tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
		}
		rb.Required(false, why).Choose(chosen.Basis())
		a.notes.Rule("Form 760ES (2026), Section I(d)", fmt.Sprintf("%v of tax less %v of withholding is %v, not more than %v, so estimated payments are not required", tax, withheld, tax-withheld, y.filingThreshold))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Virginia withholding of %s covers the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen.Basis())
	default:
		rb.Required(true, fmt.Sprintf("Your Virginia tax of %s less %s withheld leaves %s due. Withholding falls short of your %s target. You need estimated payments of %s.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen.Basis())
	}
	// The regulation's $150 test and AGI floors.
	statuteFloor := 5_000 * money.Dollar
	if f.FilingStatus() == filing.MarriedFilingJointly || f.FilingStatus() == filing.MarriedFilingSeparately {
		statuteFloor = 8_000 * money.Dollar
	}
	af.required = (a.exempt && agi >= statuteFloor) ||
		(!a.exempt && taxKnown && tax-withheld > 150*money.Dollar && tax-withheld <= y.filingThreshold)
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	if agiKnown && !hasFloor {
		a.notes.Rule("Form 760ES (2026), Section I", fmt.Sprintf("Form 760ES gives VAGI floors only for single, married filing jointly and married filing separately filers, so none is applied to a %v filer", f.FilingStatus()))
	}

	if a.exempt {
		a.regular = make([]money.USCents, len(a.dues))
		a.lesser = make([]money.USCents, len(a.dues))
		return r.withDivergences(a, af)
	}
	if !incomeKnown {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What is the tax on your %d income at %d rates and exemptions?", last, y.year),
			"With it, payments that reach that figure cancel the charge under Exception 2.")
	}
	if _, answered := extras.MerchantSeamanShare(); !answered {
		a.notes.Ask(working.PositionJurisdictionFact, "Is two-thirds of gross income from farming, fishing or merchant seaman work?",
			"If so, separate rules apply. Until then, your figures assume no merchant seaman income.")
	}

	// A late starter pays in fewer equal installments.
	// Form 760ES's payment schedule and Form 760C line 5.
	a.first = estimated.LateStart(f.RequirementFirstMet(), y.laterGroups)
	if a.lesser, err = estimated.EqualShares(regularOf, len(a.dues), a.first); err != nil {
		return assessment{}, err
	}
	a.regular = estimated.Increments(a.lesser)
	if a.first > 0 {
		a.notes.Rule("Code of Virginia §§ 58.1-490(E), 58.1-491(A); Form 760ES (2026), Payment Schedule; Form 760C (2025), line 5", fmt.Sprintf("the requirement was first met on %s, so %v is due in %d equal installments from %s",
			f.RequirementFirstMet().Long(), regularOf, len(a.dues)-a.first, a.dues[a.first].Long()))
	} else {
		a.notes.Rule("Code of Virginia § 58.1-491(A)(1); Form 760ES (2026), Section II; Form 760C (2025), line 7", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", regularOf, a.regular[0],
			a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
	}

	// The exceptions, Form 760C Part II.
	// Exception 1 asks for the same amounts as installments measured on last
	// year's tax, so it is reported but rarely decides anything.
	if hasPrior && prior.Filed() {
		if prior.Months() == 12 && priorTax > 0 {
			a.exceptions = append(a.exceptions, exception{name: "Exception 1: payments reached last year's tax", needed: r.shares(priorTax), covers: 4})
			a.notes.Rule("Code of Virginia § 58.1-492(C)(1)(a); Form 760C (2025), line 16", fmt.Sprintf("Exception 1 is open: the %d return covered 12 months and showed %v of tax, and payments that reach 25%%, 50%%, 75%% and 100%% of it by the four dates cancel the addition", last, priorTax))
		} else {
			a.notes.Rule("Code of Virginia § 58.1-492(C)(1)(a); Form 760C (2025) instructions, line 16", fmt.Sprintf("Exception 1 is not open: it needs a 12-month %d return showing tax, and the return covered %d months and showed %v", last, prior.Months(), priorTax))
		}
	}
	if incomeKnown {
		a.exceptions = append(a.exceptions, exception{name: fmt.Sprintf("Exception 2: payments reached the tax on %d income at %d rates", last, y.year), needed: r.shares(incomeTax), covers: 4})
	}
	if f.Elections().Annualised() {
		needed := make([]money.USCents, len(y.annualised))
		for i, p := range f.Periods()[:len(y.annualised)] {
			needed[i] = y.annualised[i].Of(p.AnnualisedTax())
		}
		a.exceptions = append(a.exceptions, exception{name: "Exception 3: payments reached 90% of the tax on annualized income", needed: needed, covers: len(needed)})
		a.notes.Rule("Code of Virginia § 58.1-492(C)(1)(c); Form 760C (2025), line 18", fmt.Sprintf("Exception 3 needs 22.5%%, 45%% and 67.5%% of the tax on annualized income by the first three dates: %v, %v and %v", needed[0], needed[1], needed[2]))
	}
	if taxes, ok := extras.IncomeToDateTax(); ok {
		needed := make([]money.USCents, len(taxes))
		for i, t := range taxes {
			needed[i] = y.incomeToDate.Of(t)
		}
		a.exceptions = append(a.exceptions, exception{name: "Exception 4: payments reached 90% of the tax on the income received to date", needed: needed, covers: len(needed)})
		a.notes.Rule("Code of Virginia § 58.1-492(C)(2); Form 760C (2025), line 19", fmt.Sprintf("Exception 4 needs %v of the tax on the income received to April 30, May 31 and August 31 by the first three dates: %v, %v and %v",
			y.incomeToDate, needed[0], needed[1], needed[2]))
	}

	// The March 1 rule, moved past a weekend or holiday.
	by := y.calendar.NextBusinessDay(y.fileAndPayBy)
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(by) {
		a.excusedJanuary = true
		a.regular[3] = 0
		a.notes.Rule("Form 760ES (2026), Section IV; Code of Virginia § 58.1-490(I)", fmt.Sprintf("the return was filed with the balance paid on %s, by %s, so the %s payment is not required",
			filed.Long(), by.Long(), a.dues[3].Long()))
	}

	// What the January, late-start, withholding and rate readings turn on.
	af.january = a.excusedJanuary && a.first != 3
	af.lateStart = a.first > 0
	if a.requirement.TargetKnown() {
		ledger, _, err := r.ledger(f, a)
		if err != nil {
			return assessment{}, err
		}
		standings, total := r.standings(a, ledger)
		af.small = a.regularKnown && total > 150*money.Dollar && total <= y.smallUnderpayment
		late := false
		for _, e := range a.exceptions {
			late = late || e.covers < len(a.dues)
		}
		af.lateExceptions = late && ledger[3].shortfall > 0 && !standings[3].excused
		for i, s := range standings {
			if !s.charged {
				continue
			}
			af.withholding = af.withholding || (f.Elections().WithholdingAsWithheld() && withheld > 0)
			for _, pc := range ledger[i].pieces {
				to := y.stop
				if !pc.paidOn.IsZero() && pc.paidOn.Before(to) {
					to = pc.paidOn
				}
				af.dailyRate = af.dailyRate || y.printedDays(ledger[i].due, to)
			}
		}
	}
	return r.withDivergences(a, af)
}

// withDivergences adds the divergences to the assessment's notes.
func (r virginiaEstimated) withDivergences(a assessment, af affects) (assessment, error) {
	ds, err := r.divergences(af)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(ds...)
	return a, nil
}

// printedDays reports whether any day after from up to and including to is
// charged at a printed daily factor.
func (y estimatedYear) printedDays(from, to estimated.Date) bool {
	for _, p := range y.rates {
		if p.printed && to.After(p.from.AddDays(-1)) && from.Before(p.to) && to.After(from) {
			return true
		}
	}
	return false
}

// shares is Form 760C lines 16 and 17: amount times 25%, 50%, 75% and 100%.
func (r virginiaEstimated) shares(amount money.USCents) []money.USCents {
	out := make([]money.USCents, len(r.y.exceptionShares))
	for i, s := range r.y.exceptionShares {
		out[i] = s.Of(amount)
	}
	return out
}

func basisWords(b estimated.Basis, last, year int) string {
	if b == estimated.BasisPriorYearIncome {
		return fmt.Sprintf("the tax on your %d income at %d rates", last, year)
	}
	return fmt.Sprintf("100%% of your %d tax", last)
}

// piece is part of an installment's shortfall and the day it was paid, or
// the zero Date if it never was.
type piece struct {
	amount money.USCents
	paidOn estimated.Date
}

// entry is one installment after the credits are applied.
type entry struct {
	due                             estimated.Date
	required, creditedBy, shortfall money.USCents
	pieces                          []piece
}

func (e entry) outstanding() money.USCents {
	owed := e.shortfall
	for _, p := range e.pieces {
		if !p.paidOn.IsZero() {
			owed -= p.amount
		}
	}
	return owed
}

// credit is withholding or a payment. atDue marks one counted on its
// installment date, which pays that installment before an earlier shortfall.
type credit struct {
	on     estimated.Date
	amount money.USCents
	atDue  bool
}

// apply applies credits to the installments the way Form 760C Part I and
// § 58.1-492(B)(2) do, which is not the order estimated.Apply uses.
//
// A credit on an installment date pays that installment first; only the
// excess pays earlier shortfalls, on that date, and the rest carries forward.
// This is Form 760C lines 11 and 12 and 23VAC10-112-42, Example 2. A payment
// between installment dates pays the oldest shortfall first, on its own date,
// and the rest counts toward the next installment, as the line 13 example in
// the Form 760C instructions does. A payment after the last date pays the
// oldest shortfall.
func apply(dues []estimated.Date, required []money.USCents, credits []credit) []entry {
	cs := append([]credit(nil), credits...)
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].on.Before(cs[j].on) })
	out := make([]entry, len(dues))
	payOldest := func(n int, on estimated.Date, amount money.USCents) money.USCents {
		left := amount
		for i := 0; i < n && left > 0; i++ {
			owed := out[i].outstanding()
			if owed <= 0 {
				continue
			}
			paid := min(owed, left)
			out[i].pieces = append(out[i].pieces, piece{amount: paid, paidOn: on})
			left -= paid
		}
		return left
	}
	var pool, credited money.USCents
	next := 0
	for i, due := range dues {
		var atDue money.USCents
		for next < len(cs) && !cs[next].on.After(due) {
			c := cs[next]
			next++
			credited += c.amount
			if c.atDue && c.on == due {
				atDue += c.amount
				continue
			}
			pool += payOldest(i, c.on, c.amount)
		}
		available := pool + atDue
		covered := min(available, required[i])
		out[i] = entry{due: due, required: required[i], creditedBy: credited, shortfall: required[i] - covered}
		pool = payOldest(i, due, available-covered)
	}
	for ; next < len(cs); next++ {
		payOldest(len(dues), cs[next].on, cs[next].amount)
	}
	for i := range out {
		if owed := out[i].outstanding(); owed > 0 {
			out[i].pieces = append(out[i].pieces, piece{amount: owed})
		}
	}
	return out
}

// credits lists withholding and payments as Form 760C counts them.
func (r virginiaEstimated) credits(f estimated.Facts, dues []estimated.Date) ([]credit, error) {
	var out []credit
	period := func(on estimated.Date) (estimated.Date, bool) {
		for _, d := range dues {
			if !on.After(d) {
				return d, on == d || on.Before(dues[0])
			}
		}
		return on, false
	}
	if f.Elections().WithholdingAsWithheld() {
		for _, w := range f.Withholding() {
			d, _ := period(w.On())
			out = append(out, credit{on: d, amount: w.Amount(), atDue: true})
		}
	} else {
		cum, err := estimated.CumulativeShares(f.WithheldTotal(), quarters)
		if err != nil {
			return nil, err
		}
		for i, a := range estimated.Increments(cum) {
			if a > 0 {
				out = append(out, credit{on: dues[i], amount: a, atDue: true})
			}
		}
	}
	for _, p := range f.Payments() {
		switch d, onDue := period(p.On()); {
		case p.FromPriorYear():
			out = append(out, credit{on: dues[0], amount: p.Amount(), atDue: true})
		case onDue:
			out = append(out, credit{on: d, amount: p.Amount(), atDue: true})
		default:
			out = append(out, credit{on: p.On(), amount: p.Amount()})
		}
	}
	return out, nil
}

func (r virginiaEstimated) ledger(f estimated.Facts, a assessment) ([]entry, working.Notes, error) {
	credits, err := r.credits(f, a.dues)
	if err != nil {
		return nil, working.Notes{}, err
	}
	ledger := apply(a.dues, a.regular, credits)
	var notes working.Notes
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("Form 760C (2025), instructions for lines 8 to 10", fmt.Sprintf("by election, the %v withheld is entered in the installment period it was withheld in", f.WithheldTotal()))
	} else {
		notes.Rule("Code of Virginia § 58.1-492(D)(2); Form 760C (2025), instructions for lines 8 to 10", fmt.Sprintf("the %v withheld is divided by four and treated as paid on the installment dates", f.WithheldTotal()))
	}
	notes.Rule("Code of Virginia § 58.1-492(B)(2); Form 760C (2025), lines 11 to 14; 23VAC10-112-42, Example 2",
		"a payment on an installment date pays that installment first and then the oldest shortfall; a payment between dates pays the oldest shortfall on its own date")
	return ledger, notes, nil
}

// standing is where an installment stands once the exceptions are applied:
// excused when an exception covers it, and charged when it is short and no
// exception does.
type standing struct {
	excused, charged bool
	why              string
}

// standings applies Form 760C line 14 and Part II, and returns the year's
// total underpayment. The small-underpayment exception is applied only when
// the regular installments are known.
func (r virginiaEstimated) standings(a assessment, ledger []entry) ([]standing, money.USCents) {
	var total money.USCents
	for _, l := range ledger {
		total += l.shortfall
	}
	small := a.regularKnown && total <= r.y.smallUnderpayment
	out := make([]standing, len(ledger))
	for i, l := range ledger {
		var s standing
		met := ""
		for _, e := range a.exceptions {
			if i < e.covers && met == "" && l.creditedBy >= e.needed[i] {
				met = e.name
			}
		}
		switch {
		case a.excusedJanuary && i == 3:
			s.excused, s.why = true, "the return was filed and paid by March 1"
		case small:
			s.excused, s.why = true, fmt.Sprintf("the year's underpayments total %v, which is %v or less", total, r.y.smallUnderpayment)
		case met != "":
			s.excused, s.why = true, met
		}
		s.charged = l.shortfall > 0 && !s.excused
		out[i] = s
	}
	return out, total
}

// installments are the schedule: each date's running total of the regular
// installments, what was credited by it, and its own shortfall.
func (r virginiaEstimated) installments(a assessment, ledger []entry, standings []standing) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		b := estimated.NewInstallment(i+1, l.due).NeededBy(a.lesser[i]).CreditedBy(l.creditedBy).Shortfall(l.shortfall)
		if standings[i].excused {
			b.Excused()
		}
		in, err := b.Build()
		if err != nil {
			return nil, err
		}
		out[i] = in
	}
	return out, nil
}

// askFirstMet reports the late-start question when an early installment is
// charged and the caller did not say when the requirement arose.
func (r virginiaEstimated) askFirstMet(f estimated.Facts, s []standing, notes *working.Notes) {
	if !f.RequirementFirstMet().IsZero() {
		return
	}
	for _, x := range s[:3] {
		if x.charged {
			by := r.date(r.y.laterGroups[0].AddDays(-1))
			notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first expect to need estimated payments for %d?", r.y.year),
				fmt.Sprintf("Until then, your schedule assumes it was by %s. A later date would give you fewer installments.", by))
			return
		}
	}
}

func (r virginiaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r virginiaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Virginia figure to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	standings, _ := r.standings(a, ledger)
	out, err := r.installments(a, ledger, standings)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	for i, s := range standings {
		l := ledger[i]
		if s.why != "" && l.shortfall > 0 {
			notes.Rule("Code of Virginia § 58.1-492(C), (D)(3); Form 760C (2025), line 14 and Part II", fmt.Sprintf("the %s installment was %v short and bears no addition: %s", l.due.Long(), l.shortfall, s.why))
		}
	}
	if !a.exempt {
		r.askFirstMet(f, standings, &notes)
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// rateOn is the rate period covering d.
func (y estimatedYear) rateOn(d estimated.Date) (ratePeriod, bool) {
	for _, p := range y.rates {
		if !d.Before(p.from) && !d.After(p.to) {
			return p, true
		}
	}
	return ratePeriod{}, false
}

// accrue charges principal for the days after from up to and including to,
// one part for each rate period. It returns the first day no rate covers, or
// the zero Date, and whether any day was at a rate with no printed factor.
func (y estimatedYear) accrue(principal money.USCents, from, to estimated.Date) ([]estimated.PenaltyPart, estimated.Date, bool, error) {
	var parts []estimated.PenaltyPart
	computed := false
	cur := from
	for cur.Before(to) {
		day := cur.AddDays(1)
		p, ok := y.rateOn(day)
		if !ok {
			return parts, day, computed, nil
		}
		end := to
		if p.to.Before(end) {
			end = p.to
		}
		var ps []estimated.PenaltyPart
		var err error
		if p.printed {
			ps, _, err = estimated.DailyFactor(principal, cur, end, p.factor)
		} else {
			ps, _, err = estimated.SimpleDaily(principal, cur, end, p.series)
			computed = true
		}
		if err != nil {
			return nil, estimated.Date{}, false, err
		}
		parts = append(parts, ps...)
		cur = end
	}
	return parts, estimated.Date{}, computed, nil
}

func (r virginiaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("virginiataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Virginia figure to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	waiver, waiverAsked := f.Positions().Waiver()
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	standings, total := r.standings(a, ledger)
	schedule, err := r.installments(a, ledger, standings)
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
	if !a.regularKnown {
		// Without this year's tax or last year's return, the installments are
		// measured against 90% of this year's tax, which is not known. Only an
		// exception that covers every short installment gives an answer.
		for i, s := range standings {
			if s.charged {
				return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: the %s installment met no exception, and Form 760C measures it against 90%% of this year's tax",
					estimated.ErrNeedsThisYearTax, ledger[i].due.Long())
			}
		}
		notes.Rule("Code of Virginia § 58.1-492(C); Form 760C (2025), Part II", "this year's tax was not given, but every installment met an exception by its date, so no addition arises whatever this year's tax is")
	} else if total <= y.smallUnderpayment && total > 0 {
		notes.Rule("Code of Virginia § 58.1-492(D)(3); Form 760ES (2026), Section VIII(e)", fmt.Sprintf("the year's underpayments total %v, which is %v or less, so there is no addition to tax", total, y.smallUnderpayment))
	}

	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	var unrated estimated.Date
	computed := false
	for i, s := range standings {
		l := ledger[i]
		if !s.charged {
			continue
		}
		lb := estimated.NewPenaltyLine(i+1, l.due, l.shortfall)
		lastDay := l.due
		for _, p := range l.pieces {
			to := stop
			if !p.paidOn.IsZero() && p.paidOn.Before(stop) {
				to = p.paidOn
			}
			if !to.After(l.due) {
				continue
			}
			if to.After(lastDay) {
				lastDay = to
			}
			parts, gap, used, err := y.accrue(p.amount, l.due, to)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			computed = computed || used
			if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
				unrated = gap
			}
			for _, part := range parts {
				lb.Part(part)
			}
		}
		line, err := lb.Days(lastDay.DaysSince(l.due)).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		pb.Line(line)
	}
	p, err := pb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown && p.Total() > 0 {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("Code of Virginia § 58.1-492(A)(1)", fmt.Sprintf("this year's Virginia tax was not given, so each installment is measured against last year's tax alone; "+
			"the installment is the lesser of that and 90%% of this year's tax, so %v is the most the addition can be", p.Total()))
	}
	if paidInFull {
		notes.Rule("Code of Virginia § 58.1-492(B)(2)", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("Code of Virginia §§ 58.1-15, 58.1-492(A), (B); Form 760C (2025), Part III", fmt.Sprintf("each shortfall no exception covers is charged for the days from its installment date until paid or %s, at the rate for each period; "+
		"the addition to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if computed {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("No %d Form 760C is out, so the regulation's method applies. From %s to %s, 8%% is charged as 8%% times the days over 365.",
			y.year, r.date(y.rates[0].from), r.date(y.rates[0].to)))
	}
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", y.rates[len(y.rates)-1].to.Time().Format("Jan 2, 2006")))
	}
	if waiverAsked && waiver != estimated.WaiverNone {
		notes.Rule("Code of Virginia § 58.1-492; Form 760C (2025) and its instructions", "neither the Code nor the Department's forms provide a waiver of the addition for a casualty, retirement or disability, so the addition is shown in full")
		if p.Total() > 0 {
			if p, err = p.ToBuilder().WaiverOutcome(estimated.WaiverRefused("Virginia waives no penalty for a casualty, disaster, retirement or disability.")).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	r.askFirstMet(f, standings, &notes)
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r virginiaEstimated) Explanation() estimated.Explanation { return virginiaExplanation }

// virginiaExplanation is written by hand and checked against
// research/estimated-payments/va.md.
var virginiaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Take your 2026 Virginia tax after withholding and credits. If it will be more than $1,000, Virginia expects estimated payments. The charge for paying too little is interest on each installment's shortfall. There is none when the year's shortfalls total $1,000 or less.").
		Paragraph("There is no charge if withholding and estimated payments reach a target. The target is the smaller of two figures. One is 90% of this year's Virginia tax. The other is 100% of the tax on last year's return. There is no 110% tier for high incomes. A return last year that showed no tax sets last year's figure at zero.").
		Paragraph("Four equal quarters are due in 2026 on May 1, June 15 and September 15. The fourth is due January 15, 2027. The first is due May 1, not April 15. Withholding counts as paid in equal parts on those dates. You can instead show when it was withheld. If you first need to pay later in the year, you pay fewer installments. They are three, two or one equal installments from the next date. You can file your return and pay in full by March 1, 2027. That takes the place of the January payment.").
		Paragraph("Each installment is measured on its own date. A payment made on an installment date counts toward that installment first. Only the rest goes back to an earlier shortfall. Four exceptions cancel an installment's charge without changing its shortfall. One is payments that reach last year's tax. That needs last year's return to cover twelve months and show tax. Another is last year's income taxed at this year's rates. The third is 90% of the tax on annualized income. The fourth uses income actually received to April 30, May 31 or August 31. It too is 90% of the tax on that income. The last two do not apply to the January installment.").
		Paragraph("The charge is interest at the federal underpayment rate plus 2%. It runs from the installment date until paid or May 1, 2027. For 2026 that is 8% from April to June and 9% from July to December. The rate for 2027 is not yet published. Virginia's form charges 9% as .00025 a day.").
		VersusFederal("The first payment is due May 1, and the charge runs to May 1. There's no 110% tier, and two more exceptions apply. There's no charge when the year's shortfalls total $1,000 or less. A payment on an installment date pays that installment before an earlier one. The rate is the federal rate plus 2%.").
		Source("Code of Virginia §§ 58.1-15, 58.1-321, 58.1-490, 58.1-491, 58.1-492; 2026 Acts c. 7").
		Source("23VAC10-112-20, -42 and -43").
		Source("Form 760ES, 2026 Virginia Estimated Income Tax Payment Vouchers and Instructions for Individuals (Rev. 09/25)").
		Source("Form 760C, Underpayment of Virginia Estimated Tax by Individuals, Estates and Trusts, and its instructions, 2025").
		Source("Tax Bulletins 26-2, 26-3 and 26-4").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
