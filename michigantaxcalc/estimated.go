package michigantaxcalc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Michigan's estimated income tax rules, MCL 206.301 with the interest and
// penalties of MCL 205.23 and 205.24, as the shared [estimated.EstimatedTax]
// interface.
//
// MCL 206.301(11) adopts the federal computation of the installments and of
// the period of underpayment, so the required payment, the quarters and the
// interest period look federal. The charge does not. Michigan charges two
// things on each quarter's shortfall: interest by the day at a factor
// Treasury prints for each half-year, and a flat penalty of 25% or 10% with
// $25 and $10 minimums. The two are figured on different underpayments:
// interest applies a later payment to the oldest shortfall first, and the
// penalty counts a payment only in the quarter it arrives.
//
// The figure follows Treasury's forms, the 2026 MI-1040ES and the 2025
// MI-2210, and the result's Divergences say where the statutes read
// otherwise.

const jurisdiction = estimated.Michigan

// EstimatedFacts are the facts Michigan's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	seafaringShare money.Rate

	priorEstimatesRequired bool
	priorEstimatesAnswered bool

	priorSeparate         bool
	priorSeparateAnswered bool
}

// SeafaringShare is the share of gross income from seafaring, in basis
// points. MCL 206.301(6) puts a seafarer under the farmers' and fishermen's
// rules, which take a filer with at least two-thirds of gross income from
// the three together, in this year or last. Pass the larger year's shares,
// here and in [estimated.Facts.FarmShare] and [estimated.Facts.FishShare].
// Zero when not given, as the farm and fish shares are.
func (e EstimatedFacts) SeafaringShare() money.Rate { return e.seafaringShare }

// PriorYearEstimatesRequired reports whether the filer was required to make
// estimated payments for last year, and whether the caller said. MCL
// 205.24(6) charges no penalty, though interest still runs, when they were
// not. When it is not said, the rules work it out from last year's tax and
// withholding if both were given.
func (e EstimatedFacts) PriorYearEstimatesRequired() (required, answered bool) {
	return e.priorEstimatesRequired, e.priorEstimatesAnswered
}

// PriorYearMarriedFilingSeparately reports whether last year's return was
// married filing separately, and whether the caller said. The 2025 MI-2210
// tests last year's AGI against $75,000 for that status and $150,000
// otherwise. When it is not said, this year's filing status is used.
func (e EstimatedFacts) PriorYearMarriedFilingSeparately() (separate, answered bool) {
	return e.priorSeparate, e.priorSeparateAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) SeafaringShare(r money.Rate) *EstimatedFactsBuilder {
	b.e.seafaringShare = r
	return b
}

func (b *EstimatedFactsBuilder) PriorYearEstimatesRequired(required bool) *EstimatedFactsBuilder {
	b.e.priorEstimatesRequired, b.e.priorEstimatesAnswered = required, true
	return b
}

func (b *EstimatedFactsBuilder) PriorYearMarriedFilingSeparately(separate bool) *EstimatedFactsBuilder {
	b.e.priorSeparate, b.e.priorSeparateAnswered = separate, true
	return b
}

// Build refuses a seafaring share outside 0% to 100%.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.seafaringShare < 0 || b.e.seafaringShare > 10_000 {
		return EstimatedFacts{}, fmt.Errorf("michigantaxcalc: a seafaring share of %v is not a share", b.e.seafaringShare)
	}
	return b.e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is the tax after withholding and credits at or below which
	// no penalty or interest arises.
	threshold money.USCents

	// priorYearStep is last year's AGI above which the prior-year test is
	// 110%: $150,000, or $75,000 if last year's return was separate.
	priorYearStep, priorYearStepSeparate money.USCents

	dues     []estimated.Date
	calendar estimated.Calendar

	// fileAndPayBy is the January 31 date before it is moved for a weekend.
	fileAndPayBy estimated.Date
	stop         estimated.Date
	periodEnds   []estimated.Date

	// lateGroups are the first days of the MI-1040ES chart's second, third
	// and fourth rows, for reporting a late start.
	lateGroups []estimated.Date

	daily estimated.FactorSeries

	// The flat penalty: notPaidRate for a quarter with nothing paid,
	// shortRate for one paid short, each with its minimum.
	notPaidRate, shortRate       money.Rate
	notPaidMinimum, shortMinimum money.USCents
}

var estimatedYears = map[int]estimatedYear{}

// date prints d for a sentence: "Aug 31" in the tax year, "Jan 15, 2027"
// outside it.
func (y estimatedYear) date(d estimated.Date) string {
	if d.Year() == y.year {
		return fmt.Sprintf("%.3s %d", d.Month(), d.Day())
	}
	return fmt.Sprintf("%.3s %d, %d", d.Month(), d.Day(), d.Year())
}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are MCL 206.301(1) and MI-2210 line 9: a quarter of the required
// payment on each date. The MI-2210 line 10 instructions deem withholding
// paid in the same quarters.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// annualisedPercentages are MI-2210 Annualized Income Worksheet line 9.
var annualisedPercentages = []money.Rate{2_250, 4_500, 6_750, 9_000}

// EstimatedTax returns Michigan's estimated income tax rules for a tax year,
// or [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return michiganEstimated{y}, nil
}

type michiganEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// installments are MI-2210 line 9: what each date requires, for interest
	// and for the penalty alike.
	installments []money.USCents

	// excusedJanuary is the January 31 rule, which takes the flat penalty off
	// the fourth quarter and leaves its interest.
	excusedJanuary bool

	// noFlat says why no flat penalty is charged on any quarter, or is empty.
	noFlat     string
	noFlatCite string

	// priorEstimatesUnknown is whether MCL 205.24(6) could not be decided.
	priorEstimatesUnknown bool

	notes working.Notes
}

func (r michiganEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("michigantaxcalc: the facts carry %T, not michigantaxcalc.EstimatedFacts", s)
	}
}

func (r michiganEstimated) check(f estimated.Facts, e EstimatedFacts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("michigantaxcalc: these are Michigan's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("michigantaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	}
	share := int64(f.FarmShare()) + int64(f.FishShare()) + int64(e.SeafaringShare())
	switch {
	case share > 10_000:
		return fmt.Errorf("michigantaxcalc: farming, fishing and seafaring shares add to more than the whole income")
	case share*3 >= 20_000:
		return fmt.Errorf("%w: a filer with two-thirds of gross income from farming, fishing or seafaring pays once by January 15 or files by March 1 (MCL 206.301(6)), which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("michigantaxcalc: MI-2210 annualised period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where Treasury's forms and the statutes part ways on a
// rule every Michigan computation uses. They are found in the research
// file, and each statute is in testdata/. interest, flat and feb1 say whether
// the facts have a shortfall, a flat penalty, or a return filed on February 1.
func (r michiganEstimated) divergences(interest, flat, feb1 bool) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("How interest accrues").
			Form("Interest is charged at a daily factor Treasury prints, in whole dollars. The factor is 0.0002324 for Jan to Jun 2026 and 0.0002150 for Jul to Dec 2026. "+
				"It is 0.0002129 for Jan to Jun 2027. The first two aren't the annual rate over 365.",
				"2025 MI-2210, Part 2, lines 19 to 21; RAB 2026-5; RAB 2026-10").
			Statute("Interest runs from the time the payment was due until it's paid. Its monthly rate is one point above the adjusted prime rate, divided by 12.",
				"MCL 205.23(2)").
			Uses(working.ReadingForm).Affects(interest),
		working.NewDivergence("Which flat penalty applies").
			Form("A quarter with nothing paid is charged 25% of its underpayment, at least $25. A quarter paid short is charged 10%, at least $10. Either applies whatever the reason.",
				"2025 MI-2210, Part 3 and the Line 24 instructions; RAB 2021-17, Penalties, notes 30 and 31").
			Statute("The 10% penalty applies when the shortfall is due to negligence. It is waived for reasonable cause. "+
				"The 25% penalty applies to intentional disregard of the law or department rules.",
				"MCL 205.23(3), (4)").
			Uses(working.ReadingForm).Affects(flat),
		working.NewDivergence("A return filed and paid by January 31, 2027").
			Form("The 2026 MI-1040ES says \"before February 1, 2027\". It also says \"by February 1, 2027\". Treasury's bulletin says \"on or before January 31\". A return filed and paid by Monday, Feb 1, 2027 counts as in time.",
				"2026 MI-1040ES, Income Changes During the Year; RAB 2021-17, Penalties").
			Statute("Jan 31, 2027 is a Sunday. A deadline on a weekend or legal holiday moves to the next day that isn't one. So the deadline moves to Monday, Feb 1, 2027.",
				"MCL 8.6; MCL 206.301(11), adopting 26 U.S.C. § 6654(h) (RAB 2021-17, note 29)").
			Uses(working.ReadingForm).Affects(feb1),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// assess is the requirement and installments, with the divergences and
// whether each changes anything for f.
func (r michiganEstimated) assess(f estimated.Facts) (assessment, error) {
	a, err := r.measure(f)
	if err != nil {
		return assessment{}, err
	}
	var interest, flat, feb1 bool
	if !a.exempt && a.requirement.TargetKnown() {
		credits, _, err := r.credits(f, a)
		if err != nil {
			return assessment{}, err
		}
		ledger, err := estimated.Apply(a.dues, a.installments, credits)
		if err != nil {
			return assessment{}, err
		}
		for _, l := range ledger {
			interest = interest || l.Shortfall() > 0
		}
		for i, c := range r.columns(credits, a, false) {
			flat = flat || a.flatCharged(i, c)
		}
		fileBy := r.y.calendar.NextBusinessDay(r.y.fileAndPayBy)
		feb1 = fileBy != r.y.fileAndPayBy && f.ReturnFiled() == fileBy
	}
	divergences, err := r.divergences(interest, flat, feb1)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

func (r michiganEstimated) measure(f estimated.Facts) (assessment, error) {
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	if err := r.check(f, extras); err != nil {
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
	var priorFigures estimated.Figures
	if hasPrior {
		priorFigures, _ = prior.Figures()
	}
	priorTax, _ := priorFigures.Tax()

	// 90% of this year's tax, MI-2210 line 6.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(90), money.Percent(90).Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(90), 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("MCL 206.301(11); 2025 MI-2210, Part 1, lines 5 and 6; 2026 MI-1040ES, Who Must File", fmt.Sprintf("90%% of this year's Michigan tax of %v is %v", tax, thisYear.Amount()))
	}

	// Last year's tax, MI-2210 line 4: 100%, or 110% above the step, for a
	// filed 12-month return.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, money.Percent(100), 0).Unavailable(why)
	}
	// asks the prior-year return when no exception sets the target.
	askPrior := false
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Michigan return", last))
		askPrior = true
	case prior.NotRequiredToFile():
		priorOption = unavailable(fmt.Sprintf("You weren't required to file a %d Michigan return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Michigan return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Michigan return covered %d months, not 12", last, prior.Months()))
	default:
		agi, agiKnown := priorFigures.FederalAGI()
		separate, separateAnswered := extras.PriorYearMarriedFilingSeparately()
		if !separateAnswered {
			separate = f.FilingStatus() == filing.MarriedFilingSeparately
		}
		step := y.priorYearStep
		if separate {
			step = y.priorYearStepSeparate
		}
		rate := money.Percent(100)
		why := fmt.Sprintf("%d AGI of %v is not more than %v", last, agi, step)
		switch {
		case !agiKnown:
			rate = money.Percent(110)
			why = fmt.Sprintf("%d AGI was not given", last)
			a.notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d federal adjusted gross income?", last),
				fmt.Sprintf("Until then, last year's tax counts at 110%%, or 100%% if AGI was %s or less.", step.Whole()))
		case agi > step:
			rate = money.Percent(110)
			why = fmt.Sprintf("%d AGI of %v is more than %v", last, agi, step)
		}
		if agiKnown && !separateAnswered && agi > y.priorYearStepSeparate && agi <= y.priorYearStep {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Was your %d return married filing separately?", last),
				fmt.Sprintf("If so, your %d AGI of %s is over its %s step. Last year's tax then counts at 110%%.",
					last, agi.Whole(), y.priorYearStepSeparate.Whole()))
		}
		amount := rate.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, rate, amount)
		a.notes.Rule("MCL 206.301(11); 2025 MI-2210, Part 1, line 4; 2026 MI-1040ES, Who Must File; RAB 2021-17", fmt.Sprintf("%s, so the prior-year test is %v of the %v tax on the %d return, or %v", why, rate, priorTax, last, amount))
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
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Michigan tax after credits to be?", y.year),
			fmt.Sprintf("It sets the 90%% option and the %s test.", y.threshold.Whole()))
	}

	// The exceptions. A filed 12-month return with no tax makes the target
	// nothing at all. A filed return with no tax that was not for 12 months,
	// or no return because none was required, removes the flat penalty, and
	// whether interest is still charged is open.
	noLiabilityFull := hasPrior && prior.Filed() && priorTax == 0 && prior.Months() == 12
	noLiabilityShort := hasPrior && prior.Filed() && priorTax == 0 && prior.Months() != 12
	notRequired := hasPrior && prior.NotRequiredToFile() && prior.Months() == 12
	if noLiabilityShort || notRequired {
		what := fmt.Sprintf("you had no Michigan tax for %d", last)
		because := fmt.Sprintf("You had no Michigan tax for %d.", last)
		if notRequired {
			what = fmt.Sprintf("you needed no %d Michigan return and your %d federal one covered 12 months", last, last)
			because = fmt.Sprintf("You needed no %d Michigan return, and your %d federal one covered 12 months.", last, last)
		}
		a.noFlat = what
		a.noFlatCite = "2025 MI-2210, General Instructions"
		a.notes.Rule(a.noFlatCite, fmt.Sprintf("%s, so no 25%% or 10%% penalty is charged for %d", what, y.year))
		a.notes.Ask(working.PositionUnsettledRule, "", because+" So the 10% or 25% penalty is excused.")
		a.notes.Ask(working.PositionUnsettledRule, "", "No published source says if interest is still charged then. The figures charge it.")
	}

	// MCL 205.24(6): no penalty when no estimates were required last year.
	priorEst, priorEstKnown := extras.PriorYearEstimatesRequired()
	derived := ""
	if !priorEstKnown && hasPrior {
		switch {
		case prior.NotRequiredToFile():
			priorEst, priorEstKnown = false, true
			derived = fmt.Sprintf("no %d return was required", last)
		case prior.Filed():
			if w, ok := priorFigures.Withheld(); ok {
				priorEst, priorEstKnown = priorTax-w > y.threshold, true
				derived = fmt.Sprintf("the %d tax of %v less %v of withholding is %v, which is ", last, priorTax, w, priorTax-w)
				if priorEst {
					derived += fmt.Sprintf("more than %v", y.threshold)
				} else {
					derived += fmt.Sprintf("not more than %v", y.threshold)
				}
			}
		}
	}
	a.priorEstimatesUnknown = !priorEstKnown
	if priorEstKnown && !priorEst && a.noFlat == "" && !noLiabilityFull {
		a.noFlat = fmt.Sprintf("you were not required to make estimated payments for %d", last)
		a.noFlatCite = "MCL 205.24(6); 2025 MI-2210, General Instructions"
		because := "as the caller said"
		if derived != "" {
			because = derived + " (MCL 206.301(1), unchanged since 2011)"
		}
		a.notes.Rule(a.noFlatCite, fmt.Sprintf("%s, %s, so no 25%% or 10%% penalty is charged for %d; interest is still charged", a.noFlat, because, y.year))
	}

	switch {
	case noLiabilityFull:
		a.exempt = true
		none, err := estimated.NewException("No tax due last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your %d Michigan return covered a full 12 months and showed no tax. That means no estimated payments are needed for %d.", last, y.year)).Exempt(none)
		a.notes.Rule("2025 MI-2210, General Instructions and line 7; RAB 2021-17", fmt.Sprintf("100%% of the $0.00 tax on the %d return is $0.00, the smaller test, so no installment can be short", last))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("There's no target yet. It needs your %d Michigan tax or a full-year %d return.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. After %s withheld, you need %s of estimated payments. Pay a quarter by each date, whatever your %d tax turns out to be.",
			priorOpt.Percent(), last, target.Whole(), withheld.Whole(), (target-withheld).Whole(), y.year)).Choose(chosen)
	case tax-withheld <= y.threshold:
		a.exempt = true
		left := max(0, tax-withheld)
		small, err := estimated.ExceptionAtMost("Tax less withholding", left, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		under := fmt.Sprintf(", which is %s or less", y.threshold.Whole())
		if tax < withheld {
			under = ""
		}
		rb.Required(false, fmt.Sprintf("Your Michigan tax after credits is %s, and %s is withheld. That %s%s. So no estimated payments are needed.",
			tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld), under)).Exempt(small)
		a.notes.Rule("MCL 206.301(1); 2025 MI-2210, Line-by-Line Instructions", fmt.Sprintf("%v of tax less %v withheld is %v, not more than %v, so the form is not completed", tax, withheld, tax-withheld, y.threshold))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Michigan withholding of %s already reaches the %s target. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Michigan tax after credits is %s, and %s is withheld. That leaves %s, more than %s. You need %s of estimated payments to reach the %s target. Pay a quarter by each date.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.threshold.Whole(), (target-withheld).Whole(), target.Whole())).Choose(chosen)
	}
	if askPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Michigan return, and what was its tax?", last),
			"With it, last year's tax could be a lower target than 90% of this year's.")
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
		return a, nil
	}

	cum, err := estimated.CumulativeShares(a.requirement.Target(), quarters)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cum)
	a.notes.Rule("MCL 206.301(1); 2025 MI-2210, Part 1, line 9; 2026 MI-1040ES, Payment Due Dates", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s",
		a.requirement.Target(), a.installments[0], a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() {
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
		a.notes.Rule("MCL 206.301(11); 2025 MI-2210, Annualized Income Worksheet, lines 9 to 16; RAB 2021-17", fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: 22.5%%, 45%%, 67.5%% and 90%% of the annualised tax, "+
			"less earlier installments, and never more than the regular quarter plus any amount the annualised method saved earlier", ann[0], ann[1], ann[2], ann[3]))
	}

	// January 31: no penalty on the fourth quarter, interest unchanged.
	fileBy := y.calendar.NextBusinessDay(y.fileAndPayBy)
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(fileBy) {
		a.excusedJanuary = true
		a.notes.Rule("RAB 2021-17, Penalties and Example 6; 2026 MI-1040ES, Income Changes During the Year; MCL 8.6", fmt.Sprintf("the return was filed and paid on %s, by %s (January 31, 2027 is a Sunday), so no penalty is charged on the %s installment; its interest still runs",
			filed.Long(), fileBy.Long(), a.dues[3].Long()))
		if filed == fileBy {
			a.notes.Ask(working.PositionUnsettledRule, "",
				fmt.Sprintf("Treasury's instructions say both \"before\" and \"by\" Jan 31, a Sunday. The figures treat filing and paying on %s as in time.", y.date(filed)))
		}
	}

	// A late start: the MI-1040ES chart, which the MI-2210 does not apply.
	if met := f.RequirementFirstMet(); !met.IsZero() && !met.Before(y.lateGroups[0]) {
		first := estimated.LateStart(met, y.lateGroups)
		a.notes.Ask(working.PositionUnsettledRule, "",
			fmt.Sprintf("Your income changed on %s. The MI-1040ES says to pay first on %s.", y.date(met), y.date(a.dues[first])))
		a.notes.Ask(working.PositionUnsettledRule, "", "The MI-2210 has no late-start rule, and the figures charge all four quarters.")
	}
	return a, nil
}

// credits lists what counts as paid, dated: withholding a quarter on each
// due date, or on the dates withheld by election, payments on their dates,
// and last year's overpayment on the first due date.
func (r michiganEstimated) credits(f estimated.Facts, a assessment) ([]estimated.Payment, working.Notes, error) {
	var notes working.Notes
	if f.Elections().WithholdingAsWithheld() {
		credits, err := estimated.CreditsAsWithheld(f, a.dues)
		notes.Rule("2025 MI-2210, Line 10 instructions; RAB 2021-17, Withholding and Overpayments",
			fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
		return credits, notes, err
	}
	credits, err := estimated.Credits(f, a.dues, quarters)
	notes.Rule("2025 MI-2210, Line 10 instructions; RAB 2021-17, Withholding and Overpayments",
		fmt.Sprintf("one-fourth of the %v withheld is treated as paid on each due date, whenever it was actually withheld, and last year's overpayment applied forward counts toward the first", f.WithheldTotal()))
	return credits, notes, err
}

// column is one quarter of MI-2210 Part 3.
type column struct {
	paid    money.USCents // line 10: credited in the quarter's window
	carried money.USCents // line 11: overpayment carried in
	under   money.USCents // line 23
	rate    money.Rate    // line 24
	minimum money.USCents
	charge  money.USCents // line 25, in whole dollars
}

// columns is MI-2210 Part 3. A credit counts only in the quarter whose
// window it falls in: on or before April 15, then after each due date up to
// and including the next. An overpayment is carried forward and never back,
// and a credit after the last due date counts in no quarter. With jan16, a
// credit on the day after the last due date counts in the fourth quarter.
func (r michiganEstimated) columns(credits []estimated.Payment, a assessment, jan16 bool) []column {
	y := r.y
	out := make([]column, len(a.dues))
	var carry money.USCents
	for i, due := range a.dues {
		var paid money.USCents
		for _, c := range credits {
			on := c.On()
			inWindow := (i == 0 || on.After(a.dues[i-1])) && !on.After(due)
			if jan16 && i == len(a.dues)-1 && on == due.AddDays(1) {
				inWindow = true
			}
			if inWindow {
				paid += c.Amount()
			}
		}
		col := column{paid: paid, carried: carry}
		have := paid + carry
		required := a.installments[i]
		if have >= required {
			carry = have - required
		} else {
			carry = 0
			col.under = required - have
			col.rate, col.minimum = y.shortRate, y.shortMinimum
			if have == 0 {
				col.rate, col.minimum = y.notPaidRate, y.notPaidMinimum
			}
			// The MI-2210 rounds every money item to the whole dollar.
			charge := estimated.RoundToDollar.Div(big.NewInt(int64(col.under)*int64(col.rate)), big.NewInt(10_000))
			col.charge = max(col.minimum, charge)
		}
		out[i] = col
	}
	return out
}

// paidLate returns the first quarter charged the 25% penalty after which a
// payment was made, and whether there is one.
func (r michiganEstimated) paidLate(f estimated.Facts, a assessment, cols []column, stop estimated.Date) (estimated.Date, bool) {
	for i, c := range cols {
		if !a.flatCharged(i, c) || c.rate != r.y.notPaidRate || !stop.After(a.dues[i]) {
			continue
		}
		for _, pay := range f.Payments() {
			if !pay.FromPriorYear() && pay.On().After(a.dues[i]) {
				return a.dues[i], true
			}
		}
	}
	return estimated.Date{}, false
}

// flatCharged reports whether quarter i bears a flat penalty.
func (a assessment) flatCharged(i int, c column) bool {
	return c.under > 0 && a.noFlat == "" && !(a.excusedJanuary && i == len(a.dues)-1)
}

func (r michiganEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r michiganEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Michigan return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	out, _, _, notes, err := r.schedule(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments after the credits, with the credits and the
// ledger they come from.
func (r michiganEstimated) schedule(f estimated.Facts, a assessment) ([]estimated.Installment, []estimated.Payment, []estimated.LedgerEntry, working.Notes, error) {
	credits, notes, err := r.credits(f, a)
	if err != nil {
		return nil, nil, nil, working.Notes{}, err
	}
	ledger, err := estimated.Apply(a.dues, a.installments, credits)
	if err != nil {
		return nil, nil, nil, working.Notes{}, err
	}
	out := make([]estimated.Installment, len(ledger))
	var needed money.USCents
	for i, l := range ledger {
		needed += a.installments[i]
		if out[i], err = estimated.NewInstallment(i+1, l.Due()).NeededBy(needed).CreditedBy(l.CreditedBy()).
			Shortfall(l.Shortfall()).Build(); err != nil {
			return nil, nil, nil, working.Notes{}, err
		}
	}
	return out, credits, ledger, notes, nil
}

func (r michiganEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("michigantaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Michigan return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	sched, credits, ledger, notes, err := r.schedule(f, a)
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
	cols := r.columns(credits, a, false)

	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, sched)
	var unrated estimated.Date
	var interest, flat money.USCents
	for i, l := range ledger {
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
				interest += p.Charge()
				lb.Part(p)
			}
		}
		if c := cols[i]; a.flatCharged(i, c) && stop.After(l.Due()) {
			part, err := estimated.NewPenaltyPart(estimated.PartFlat, c.under, c.rate).Span(l.Due(), l.Due()).Charge(c.charge).
				Source(fmt.Sprintf("2025 MI-2210, Part 3, lines 23 to 25: %v, at least %v; MCL 205.23(3), (4)", c.rate, c.minimum)).Build()
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			flat += c.charge
			lb.Part(part)
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

	if paidInFull {
		notes.Rule("MCL 205.23(2); 2025 MI-2210, Part 2", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("MCL 205.23(2); 2025 MI-2210, Part 2; RAB 2021-17, Interest", fmt.Sprintf("each shortfall is charged the daily factor Treasury prints for its half-year, in whole dollars, from its due date until paid or %s, "+
		"with each payment applied to the oldest shortfall first; the interest to %s is %v", y.stop.Long(), stop.Long(), interest))
	if a.noFlat != "" {
		notes.Rule(a.noFlatCite, fmt.Sprintf("%s, so no 25%% or 10%% penalty is charged; the total is the interest alone", a.noFlat))
	} else {
		notes.Rule("MCL 205.23(3), (4); 2025 MI-2210, Part 3; RAB 2021-17, Penalties", fmt.Sprintf("each quarter's underpayment, counting a payment only in the quarter it is received and carrying any overpayment forward, is charged %v (at least %v) when nothing was paid for the quarter and %v (at least %v) when it was paid short; "+
			"the penalty is %v, for a total of %v", y.notPaidRate, y.notPaidMinimum, y.shortRate, y.shortMinimum, flat, p.Total()))
	}
	if !a.taxKnown {
		notes.Rule("MCL 206.301(11); 2025 MI-2210, Part 1, line 7", fmt.Sprintf("this year's Michigan tax was not given, so interest and penalty are figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most they can be, and they are less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}

	if flat > 0 {
		// A credit on the day after the last due date: the MI-2210 counts the
		// fourth quarter's payments through January 15 and calls only those
		// after January 16 not estimates.
		alt := r.columns(credits, a, true)
		var altFlat money.USCents
		for i, c := range alt {
			if a.flatCharged(i, c) && stop.After(a.dues[i]) {
				altFlat += c.charge
			}
		}
		if altFlat != flat {
			jan16 := a.dues[len(a.dues)-1].AddDays(1)
			notes.Ask(working.PositionUnsettledRule, "",
				fmt.Sprintf("The MI-2210 doesn't say if paying on %s counts toward January. The figures leave it out, for a penalty of %s rather than %s.",
					y.date(jan16), flat.Whole(), altFlat.Whole()))
		}

		// A quarter charged 25% that was paid after its due date.
		if due, late := r.paidLate(f, a, cols, stop); late {
			notes.Ask(working.PositionUnsettledRule, "",
				fmt.Sprintf("The MI-2210 charges 25%% when the %s quarter is paid late. The MI-1040ES charges 10%% for paying late, and the figures charge 25%%.", y.date(due)))
		}

		if a.priorEstimatesUnknown {
			notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Were you required to make estimated payments for %d?", y.year-1),
				"If you weren't, there's no 10% or 25% penalty, only interest.")
		}
	}

	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", y.date(unrated.AddDays(-1))))
	}
	switch {
	case p.Total() > 0 && !waiverAnswered:
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking Treasury to waive the penalty?",
			"Reasonable cause shown in writing waives the 10% or 25% penalty, not interest. After a declared disaster, Treasury waives interest too, for people who ask.")
	case waiver == estimated.WaiverRetiredOrDisabled:
		notes.Rule("RAB 2025-15, Waiver for Reasonable Cause; 2025 MI-2210, General Instructions", "Michigan has no waiver for retirement after 62 or disability; a written request showing reasonable cause can waive the 10% or 25% penalty, not the interest")
	case waiver == estimated.WaiverCasualty:
		notes.Rule("RAB 2025-15, Waiver for Reasonable Cause; Treasury notice of March 19, 2026", "Treasury grants disaster relief by a notice for each event, which waives penalty and interest for taxpayers who ask; a casualty that destroyed records is reasonable cause, which waives the 10% or 25% penalty only")
	}
	if o, ok := michiganWaiver(f); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		if o.Undecided() {
			notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("Treasury's %d disaster notices grant relief for an affected period. They don't say whether it covers estimated payments due in that period.", y.year))
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// michiganWaiver decides a waiver asked for. Michigan has no standing
// casualty, disaster, retirement or disability waiver. Treasury waives
// penalty and interest for a declared disaster by a notice for each event,
// and RAB 2025-15 waives the 10% or 25% penalty, not the interest, for
// reasonable cause, which includes records destroyed by casualty.
func michiganWaiver(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	k, answered := f.Positions().Waiver()
	switch {
	case !answered || k == estimated.WaiverNone:
		return estimated.WaiverOutcome{}, false
	case k == estimated.WaiverCasualty:
		return estimated.WaiverUndecided("Records destroyed by a casualty can waive the 10% or 25% penalty, not interest. Treasury waives both only for a disaster it has granted relief for by notice."), true
	default:
		return estimated.WaiverRefused("Michigan has no waiver for retirement or disability. For reasonable cause, Treasury can waive the 10% or 25% penalty, not interest."), true
	}
}

func (r michiganEstimated) Explanation() estimated.Explanation { return michiganExplanation }

// michiganExplanation is written by hand and checked against
// research/estimated-payments/mi.md.
var michiganExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Michigan expects estimated payments when more than $500 will be due. The $500 is measured after withholding and credits. " +
			"Falling short costs interest on each quarter's shortfall. A flat penalty of 10% or 25% of that shortfall is added.").
		Paragraph("You are clear if withholding and estimated payments reach a target. It is the lesser of 90% of this year's Michigan tax or 100% of last year's. " +
			"Last year's figure is 110% when its adjusted gross income was over $150,000. The limit is $75,000 if last year's return was married filing separately. " +
			"Last year's figure also needs a return you filed for a full 12 months.").
		Paragraph("The payments are four equal quarters. They are due April 15, June 15, September 15 and January 15 of the next year. " +
			"A date on a weekend or legal holiday shifts to the next business day. Withholding counts a quarter on each date, unless you show when it was taken. " +
			"Form MI-2210's annualised worksheet can lower early quarters for uneven income.").
		Paragraph("Interest runs by the day on each quarter's shortfall from its due date. It stops when the shortfall is paid, or on April 15 of the next year. " +
			"Treasury sets the rate each January and July at one point above the prime rate. A later payment pays the oldest shortfall first. " +
			"For the flat penalty, a payment counts only for the quarter in which it arrives. Catching up later stops the interest but not the penalty. " +
			"It is 25% of the shortfall, at least $25, for a quarter with nothing paid. It is 10%, at least $10, for a quarter paid short.").
		Paragraph("This year's tax after withholding and credits may be $500 or less. If so, nothing is charged. Nothing is charged if last year's return covered 12 months and showed no tax. " +
			"No flat penalty applies if last year needed no estimated payments or no return. Interest can still be due. " +
			"You can file and pay in full by the end of January. That removes the January quarter's penalty but not its interest. " +
			"Treasury waives the penalty for reasonable cause shown in a written request. That waiver doesn't cover interest. " +
			"After a declared disaster, Treasury can waive both for people who ask. There is no waiver for retirement or disability.").
		VersusFederal("Michigan follows the federal required payment, quarters and interest period. Its threshold is $500 rather than $1,000. " +
			"It adds a flat 10% or 25% penalty on each quarter's shortfall to the interest. That penalty is figured without applying later payments to earlier quarters. " +
			"The interest rate is prime plus one point, set twice a year. There is no waiver for retirement or disability.").
		Source("MCL 206.301; MCL 205.23(2) to (4); MCL 205.24(6); MCL 8.6").
		Source("2026 MI-1040ES, Michigan Estimated Income Tax for Individuals (Rev. 04-25)").
		Source("2025 MI-2210, Underpayment of Estimated Income Tax, and its instructions (Rev. 10-25)").
		Source("Revenue Administrative Bulletin 2021-17, Estimated Payments for Individuals and Fiduciaries").
		Source("Revenue Administrative Bulletins 2026-5 and 2026-10 (interest rates)").
		Source("Revenue Administrative Bulletin 2025-15, Penalty Provisions").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
