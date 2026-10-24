package kansastaxcalc

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Kansas's estimated tax rules, K.S.A. 79-32,101 to 79-32,103 and
// 79-32,107, as the shared [estimated.EstimatedTax] interface.
//
// The figure follows the Department of Revenue's forms: the 2026 K-40ES for
// who must pay and when, and Schedule K-210 (2025) for the penalty, which is
// the latest published. The result's Divergences say where the statutes read
// otherwise. K.S.A. 79-32,107 was last amended by L. 2024, ch. 81, effective
// July 1, 2024, and the 2026 composite listing shows no later change to any
// of these sections, so neither form predates the law it describes.

const jurisdiction = estimated.Kansas

// EstimatedFacts are the facts and positions Kansas's rules read that no
// other jurisdiction does. Carry them in [estimated.Facts.State].
//
// For Kansas, [estimated.Figures.Tax] is the total tax balance, Form K-40
// line 18: the tax after the nonrefundable credits, before withholding, the
// refundable credits and the credit for tax paid on the K-120S.
type EstimatedFacts struct {
	passThrough money.USCents
	refundable  money.USCents

	shortPriorYear, shortPriorYearAnswered bool
	lateStart, lateStartAnswered           bool
	januaryMoves, januaryMovesAnswered     bool
}

// PassThroughCredit is the credit for tax paid on the K-120S by an electing
// pass-through entity, Form K-40 line 25. Schedule K-210 line 7 treats it as
// paid a quarter on each due date, as it treats withholding.
func (e EstimatedFacts) PassThroughCredit() money.USCents { return e.passThrough }

// RefundableCredits are the refundable credits on Form K-40 lines 22 and 23.
// They count against the $500 threshold and not toward the installments,
// because Schedule K-210 line 8 does not add them.
func (e EstimatedFacts) RefundableCredits() money.USCents { return e.refundable }

// ShortPriorYear is the caller's position on whether last year's tax counts
// for the prior-year test when last year's return covered fewer than twelve
// months, and whether the caller took one. The K-40ES requires a twelve-month
// return; no statute or regulation found does.
func (e EstimatedFacts) ShortPriorYear() (use, answered bool) {
	return e.shortPriorYear, e.shortPriorYearAnswered
}

// LateStartInstallments is the caller's position on whether a filer who first
// meets the requirement after April 1 owes the fewer, larger installments of
// K.S.A. 79-32,103(a)(2) to (4), rather than the quarter on each date that
// Schedule K-210 prints, and whether the caller took one.
func (e EstimatedFacts) LateStartInstallments() (use, answered bool) {
	return e.lateStart, e.lateStartAnswered
}

// JanuaryThirtyFirstMoves is the caller's position on whether the January 31
// date for filing and paying in full moves to the next workday when it falls
// on a weekend, as January 31, 2027 does, and whether the caller took one.
func (e EstimatedFacts) JanuaryThirtyFirstMoves() (moves, answered bool) {
	return e.januaryMoves, e.januaryMovesAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PassThroughCredit(a money.USCents) *EstimatedFactsBuilder {
	b.e.passThrough = a
	return b
}

func (b *EstimatedFactsBuilder) RefundableCredits(a money.USCents) *EstimatedFactsBuilder {
	b.e.refundable = a
	return b
}

func (b *EstimatedFactsBuilder) ShortPriorYear(use bool) *EstimatedFactsBuilder {
	b.e.shortPriorYear, b.e.shortPriorYearAnswered = use, true
	return b
}

func (b *EstimatedFactsBuilder) LateStartInstallments(use bool) *EstimatedFactsBuilder {
	b.e.lateStart, b.e.lateStartAnswered = use, true
	return b
}

func (b *EstimatedFactsBuilder) JanuaryThirtyFirstMoves(moves bool) *EstimatedFactsBuilder {
	b.e.januaryMoves, b.e.januaryMovesAnswered = moves, true
	return b
}

// Build refuses negative credits.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.passThrough < 0 || b.e.refundable < 0 {
		return EstimatedFacts{}, errors.New("kansastaxcalc: credits cannot be negative")
	}
	return b.e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is what tax less withholding and credits must reach before
	// estimated payments are required and the penalty can apply.
	threshold money.USCents

	// thisYearShare and priorYearShare are Schedule K-210 lines 2 and 3.
	thisYearShare, priorYearShare money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// laterGroups are the first days of K.S.A. 79-32,102(a)'s later groups:
	// a requirement first met on or after laterGroups[i] starts at
	// installment i+2.
	laterGroups []estimated.Date

	fileAndPayBy estimated.Date
	stop         estimated.Date
	periodEnds   []estimated.Date
	rates        estimated.RateSeries

	// deMinimis is the penalty K.S.A. 79-32,107(a) does not add.
	deMinimis money.USCents
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are Schedule K-210 lines 5, 7 and 9: 25%, 50%, 75% and 100% by
// the four dates.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// annualisedShares are Schedule K-210 line 10a: the share of each period's
// annualised tax due by its date, cumulatively.
var annualisedShares = []money.Rate{2250, 4500, 6750, 9000}

// EstimatedTax returns Kansas's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return kansasEstimated{y}, nil
}

type kansasEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// required is the running total needed by each date: Schedule K-210
	// line 9, or the lesser of lines 9 and 10a under the annualised method.
	required []money.USCents

	excusedJanuary bool
	extras         EstimatedFacts
	notes          working.Notes
}

func (r kansasEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("kansastaxcalc: the facts carry %T, not kansastaxcalc.EstimatedFacts", s)
	}
}

func (r kansasEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("kansastaxcalc: these are Kansas's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("kansastaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: a farmer or fisher with two-thirds of gross income from farming or fishing makes one payment by January 15 at 66 2/3%%, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("kansastaxcalc: Schedule K-210 line 10a period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms and the statutes part ways on
// a rule a Kansas computation uses, each marked with whether it changes
// anything for f. Those on last year's return are in
// research/estimated-payments/ks.md, under Conflicts resolved; the late start
// was found by comparing Schedule K-210 with K.S.A. 79-32,103. The $1 floor
// is a penalty rule, reported with the penalty.
func (r kansasEstimated) divergences(f estimated.Facts, extras EstimatedFacts) ([]working.Divergence, error) {
	prior, hasPrior := f.PriorYear()
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
	}
	filedWithTax := hasPrior && prior.Filed() && priorTax > 0
	shortReading, lateReading := working.ReadingForm, working.ReadingForm
	if use, _ := extras.ShortPriorYear(); use {
		shortReading = working.ReadingStatute
	}
	if use, _ := extras.LateStartInstallments(); use {
		lateReading = working.ReadingStatute
	}
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("No return filed last year").
			Form("With no Kansas return last year, the penalty schedule counts its tax as zero. Then no payments are needed and no penalty arises.",
				"Schedule K-210 (2025), instructions for lines 1 through 4").
			Statute("Last year's tax counts as zero only if you weren't required to file. If you had to file and didn't, only the 90% and annualized tests apply.",
				"K.S.A. 79-32,107(b)(1) and (b)(2)").
			Uses(working.ReadingForm).
			Affects(hasPrior && !prior.Filed() && !prior.NotRequiredToFile()),
		working.NewDivergence("Last year's tax under $200").
			Form("The penalty schedule uses last year's tax however small. It counts it as zero only when it was zero.",
				"Schedule K-210 (2025), line 3 and the instructions for lines 1 through 4").
			Statute("Last year's tax counts as zero if it was less than $200.",
				"K.S.A. 79-32,107(b)(2)").
			Uses(working.ReadingForm).
			Affects(filedWithTax && priorTax < 200*money.Dollar),
		working.NewDivergence("A return for less than 12 months").
			Form("Kansas's estimated tax form uses last year's tax only from a 12-month return. That return must also show tax. The penalty schedule itself states no such condition.",
				"2026 K-40ES, Estimated Tax Penalty; Schedule K-210 (2025), line 3").
			Statute("The law uses the tax shown on last year's return, with no 12-month condition. It leaves short years to regulations.",
				"K.S.A. 79-32,107(b)(1); K.S.A. 79-32,101(d), 79-32,102(d), 79-32,103(d)").
			Uses(shortReading).
			Affects(filedWithTax && prior.Months() != 12),
		working.NewDivergence("Needing to pay only after April 1").
			Form("The penalty schedule has everyone pay 25%, 50%, 75% and 100% by the four dates. The worksheet differs for a start after April 15. It has you pay 1/3, 1/2 or all of the year's amount on the later dates.",
				"Schedule K-210 (2025), lines 9 and 10a; 2026 K-40ES, worksheet line 9").
			Statute("First needing to pay April 2 to June 1 means three equal payments from June 15. From June 2 to September 1, it means two from September 15. After September 1, it means all of it on January 15.",
				"K.S.A. 79-32,102(a); K.S.A. 79-32,103(a)(2) to (4)").
			Uses(lateReading).
			Affects(estimated.LateStart(f.RequirementFirstMet(), r.y.laterGroups) > 0),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// floorDivergence is the $1 floor of K.S.A. 79-32,107(a), which Schedule
// K-210 does not have. It affects a penalty of more than zero and at most $1.
func (r kansasEstimated) floorDivergence(total money.USCents) (working.Divergence, error) {
	return working.NewDivergence("A penalty of $1 or less").
		Form("The penalty schedule has no minimum: the penalty is entered whatever its amount.",
			"Schedule K-210 (2025), lines 17 and 18").
		Statute("The penalty is not added if it does not exceed $1.",
			"K.S.A. 79-32,107(a)").
		Uses(working.ReadingForm).
		Affects(total > 0 && total <= r.y.deMinimis).
		Build()
}

// shortDate is a date as a person reads it: "Aug 31" in the tax year, and
// "Jan 15, 2027" outside it.
func (r kansasEstimated) shortDate(d estimated.Date) string {
	if d.Year() == r.y.year {
		return d.Time().Format("Jan 2")
	}
	return d.Time().Format("Jan 2, 2006")
}

func (r kansasEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	a := assessment{extras: extras}
	divergences, err := r.divergences(f, extras)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	last := y.year - 1
	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	credited := withheld + extras.passThrough
	owed := tax - credited - extras.refundable

	// Schedule K-210 line 2: 90% of this year's tax.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("K.S.A. 79-32,107(a); Schedule K-210 (2025), lines 1 and 2", fmt.Sprintf("%v of this year's Kansas tax of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// Schedule K-210 line 3: last year's tax.
	prior, hasPrior := f.PriorYear()
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
	}
	shortUse, shortAnswered := extras.ShortPriorYear()
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	noLiability := false
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Kansas return", last))
	case !prior.Filed():
		noLiability = true
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0)
		a.notes.Rule("Schedule K-210 (2025), instructions for lines 1 through 4", fmt.Sprintf("no %d Kansas return was filed, so line 3 is zero, and so is the required payment", last))
	case priorTax == 0:
		noLiability = true
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0)
		a.notes.Rule("Schedule K-210 (2025), instructions for lines 1 through 4; K.S.A. 79-32,107(b)(2)", fmt.Sprintf("the %d return showed no Kansas tax, so line 3 is zero, and so is the required payment", last))
	case prior.Months() != 12 && !shortUse:
		priorOption = unavailable(fmt.Sprintf("Your %d return covered %d months, and Kansas's form asks for a 12-month return", last, prior.Months()))
	default:
		amount := y.priorYearShare.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, amount)
		a.notes.Rule("K.S.A. 79-32,107(b)(1); Schedule K-210 (2025), line 3; 2026 K-40ES, Estimated Tax Penalty", fmt.Sprintf("%v of the %v tax on the %d return is %v", y.priorYearShare, priorTax, last, amount))
		if prior.Months() != 12 {
			a.notes.Rule("K.S.A. 79-32,107(b)(1)", fmt.Sprintf("by the position given in kansastaxcalc.EstimatedFacts, the %d-month %d return is used, though the K-40ES asks for 12 months", prior.Months(), last))
		}
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

	label := func(o estimated.TargetOption) string { return fmt.Sprintf("%s, %s", o.Label(), o.Amount().Whole()) }
	switch {
	case noLiability:
		a.exempt = true
		name, why := "No Kansas tax last year", fmt.Sprintf("Your %d Kansas return showed no tax. No estimated payments are needed for %d and no penalty can arise.", last, y.year)
		if !prior.Filed() {
			name, why = "No Kansas return filed last year", fmt.Sprintf("You filed no %d Kansas return, so Kansas counts last year's tax as zero. No estimated payments are needed for %d and no penalty can arise.", last, y.year)
		}
		ex, err := estimated.NewException(name).Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, why).Exempt(ex)
	case taxKnown && owed < y.threshold:
		a.exempt = true
		ex, err := estimated.ExceptionBelow("Tax less withholding and credits", owed, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your Kansas tax is %s, and withholding and credits are %s. That leaves %s, under %s, so no estimated payments are needed.",
			tax.Whole(), (tax - owed).Whole(), owed.Whole(), y.threshold.Whole())
		switch {
		case owed < 0:
			why = fmt.Sprintf("Your Kansas tax is %s, and withholding and credits are %s. That %s, so no estimated payments are needed.",
				tax.Whole(), (tax - owed).Whole(), estimated.Leaves(owed))
		case owed == 0:
			why = fmt.Sprintf("Your withholding and credits of %s cover your Kansas tax of %s. No estimated payments are needed.", (tax - owed).Whole(), tax.Whole())
		case owed.Whole() == y.threshold.Whole():
			why = fmt.Sprintf("Your Kansas tax less withholding and credits is under %s. No estimated payments are needed.", y.threshold.Whole())
		}
		rb.Required(false, why).Exempt(ex)
		a.notes.Rule("K.S.A. 79-32,101(a)(1); Schedule K-210 (2025), Who May Use This Schedule; 2026 K-40ES, worksheet line 8",
			fmt.Sprintf("%v of tax less %v of withholding and %v of credits is %v, under %v, so there is no penalty", tax, withheld, extras.passThrough+extras.refundable, owed, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target is set by your %d Kansas tax or your %d return. There's no target until you have one of them.", y.year, last)).NoTarget()
	case !taxKnown && credited >= target:
		rb.Required(false, fmt.Sprintf("Your target is %s. Your withholding and credits of %s already reach it. No estimated payments are needed whatever your %d tax is.",
			label(priorOpt), credited.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %s. It keeps you clear of a penalty whatever your %d tax is. Withholding and credits of %s leave %s to pay in estimated tax.",
			label(priorOpt), y.year, credited.Whole(), (target-credited).Whole())).Choose(chosen)
	case credited >= target:
		rb.Required(false, fmt.Sprintf("Your Kansas withholding and credits of %s reach your %s target. No estimated payments are needed.", credited.Whole(), target.Whole())).Choose(chosen)
	default:
		c := thisYear
		if chosen == estimated.BasisPriorYearTax {
			c = priorOpt
		}
		rb.Required(true, fmt.Sprintf("Your Kansas tax less withholding and credits leaves %s, at least %s. Your target is %s. You need %s of estimated payments.",
			owed.Whole(), y.threshold.Whole(), label(c), (target-credited).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// Under an exception neither return can change the target.
	if !a.exempt {
		if !hasPrior {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Kansas return, and what tax did it show?", last),
				"If you filed none or it showed no tax, no estimated payments are needed. Otherwise its tax may set a lower target.")
		}
		if hasPrior && prior.Filed() && priorTax != 0 && prior.Months() != 12 && !shortUse && !shortAnswered {
			a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("Kansas's estimated tax form won't let your %d-month %d return set your target. The law differs, and the figures follow the form.", prior.Months(), last))
		}
		if !taxKnown {
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Kansas tax to be?", y.year),
				fmt.Sprintf("With it, 90%% of this year's tax could set a lower target. Under %s after withholding and credits, no payments are needed.", y.threshold.Whole()))
		}
	}
	if a.exempt || !a.requirement.TargetKnown() {
		a.required = make([]money.USCents, len(a.dues))
		return a, nil
	}

	if a.required, err = r.regular(f, &a, target); err != nil {
		return assessment{}, err
	}

	// Schedule K-210 line 10a: the lesser of the regular and annualised
	// running totals, column by column.
	if f.Elections().Annualised() {
		periods := f.Periods()
		ann := make([]money.USCents, len(periods))
		for i, p := range periods {
			ann[i] = annualisedShares[i].Of(p.AnnualisedTax())
			a.required[i] = min(a.required[i], ann[i])
		}
		a.notes.Rule("K.S.A. 79-32,107(b)(3); Schedule K-210 (2025), lines 10a and 11", fmt.Sprintf("under the annualised method 22.5%%, 45%%, 67.5%% and 90%% of each period's annualised tax are %v, %v, %v and %v, "+
			"and each date needs the lesser of that and the regular amount: %v, %v, %v and %v", ann[0], ann[1], ann[2], ann[3],
			a.required[0], a.required[1], a.required[2], a.required[3]))
	}

	// January 31. It is a Sunday in 2027.
	payBy := y.fileAndPayBy
	moves, movesAnswered := extras.JanuaryThirtyFirstMoves()
	if moves {
		payBy = y.calendar.NextBusinessDay(payBy)
	}
	if filed := f.ReturnFiled(); !filed.IsZero() {
		if !filed.After(payBy) {
			a.excusedJanuary = true
			a.required[3] = a.required[2]
			a.notes.Rule("K.S.A. 79-32,101(c); 2026 K-40ES, When to File; Schedule K-210 (2025), Who May Use This Schedule",
				fmt.Sprintf("the return was filed and paid on %s, by %s, so no January installment is required", filed.Long(), payBy.Long()))
		} else if !movesAnswered && !filed.After(y.calendar.NextBusinessDay(y.fileAndPayBy)) {
			a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("Kansas hasn't said if its %s file-and-pay date moves off a %v. The figures count your %s return as late, so January's payment is due.",
				r.shortDate(y.fileAndPayBy), y.fileAndPayBy.Weekday(), r.shortDate(filed)))
		}
	}
	return a, nil
}

// regular is the running total needed by each date before the annualised
// method: a quarter of the target a time, as Schedule K-210 line 9 prints,
// or the late start of K.S.A. 79-32,103 when the caller takes that position.
func (r kansasEstimated) regular(f estimated.Facts, a *assessment, target money.USCents) ([]money.USCents, error) {
	y := r.y
	firstMet := f.RequirementFirstMet()
	group := estimated.LateStart(firstMet, y.laterGroups)
	use, answered := a.extras.LateStartInstallments()
	quarterly, err := estimated.CumulativeShares(target, quarters)
	if err != nil {
		return nil, err
	}
	quarterlyRule := func() {
		a.notes.Rule("K.S.A. 79-32,103(a)(1); Schedule K-210 (2025), line 9; 2026 K-40ES, When to File", fmt.Sprintf("25%%, 50%%, 75%% and 100%% of %v, %v, %v, %v and %v, are needed by %s, %s, %s and %s",
			target, quarterly[0], quarterly[1], quarterly[2], quarterly[3], a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
	}
	switch {
	case group == 0:
		if use && firstMet.IsZero() {
			a.notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first expect %s or more of Kansas tax for %d?", y.threshold.Whole(), y.year),
				"It means tax after withholding and credits. Until then, the figures assume January 1, so a quarter is due on every date.")
		}
		quarterlyRule()
		return quarterly, nil
	case use:
		n := len(a.dues) - group
		out, err := estimated.EqualShares(target, len(a.dues), group)
		if err != nil {
			return nil, err
		}
		a.notes.Rule("K.S.A. 79-32,102(a), 79-32,103(a)(2) to (4); 2026 K-40ES, worksheet line 9", fmt.Sprintf("the requirement was first met on %s, so nothing is needed before %s and %v is paid in %d equal installments from then: %v, %v, %v and %v by the four dates",
			firstMet.Long(), a.dues[group].Long(), target, n, out[0], out[1], out[2], out[3]))
		return out, nil
	default:
		quarterlyRule()
		a.notes.Rule("Schedule K-210 (2025), line 9", fmt.Sprintf("the requirement was first met on %s; Schedule K-210 prints the same quarters for every filer, so they are used", firstMet.Long()))
		if !answered {
			a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("By law, first needing to pay on %s means fewer, larger installments. The penalty schedule differs, and the figures follow its 25%% by every date.",
				r.shortDate(firstMet)))
		}
		return quarterly, nil
	}
}

func (r kansasEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// credits are what Schedule K-210 line 8 counts, dated: withholding and the
// credit for tax paid on the K-120S a quarter on each due date, and estimated
// payments on the days they were paid.
func (r kansasEstimated) credits(f estimated.Facts, a assessment) ([]estimated.Payment, working.Notes, error) {
	var notes working.Notes
	out, err := estimated.Credits(f, a.dues, quarters)
	if err != nil {
		return nil, notes, err
	}
	if p := a.extras.passThrough; p > 0 {
		cum, err := estimated.CumulativeShares(p, quarters)
		if err != nil {
			return nil, notes, err
		}
		for i, inc := range estimated.Increments(cum) {
			pay, err := estimated.NewPayment().On(a.dues[i]).Amount(inc).Build()
			if err != nil {
				return nil, notes, err
			}
			out = append(out, pay)
		}
	}
	notes.Rule("Schedule K-210 (2025), lines 5 to 8", fmt.Sprintf("the %v withheld and the %v credit for tax paid on the K-120S are treated as paid a quarter on each due date, whenever they were actually paid; "+
		"estimated payments count on the day they were paid", f.WithheldTotal(), a.extras.passThrough))
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("Schedule K-210 (2025), line 5", "Kansas has no election to credit withholding on the dates it was withheld, so it is spread evenly")
	}
	for _, p := range f.Payments() {
		if p.FromPriorYear() {
			notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("Kansas doesn't say when your %s overpayment from last year counts as paid. The figures count it on the first due date, %s, as federal rules do.",
				p.Amount().Whole(), r.shortDate(a.dues[0])))
			break
		}
	}
	return out, notes, nil
}

// creditedBy is the total of the credits paid on or before d.
func creditedBy(cs []estimated.Payment, d estimated.Date) money.USCents {
	var t money.USCents
	for _, c := range cs {
		if !c.On().After(d) {
			t += c.Amount()
		}
	}
	return t
}

func (r kansasEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Kansas return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	credits, notes, err := r.credits(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := installments(a, credits)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// installments are the due dates with what was needed and credited by each.
func installments(a assessment, credits []estimated.Payment) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(a.dues))
	var before money.USCents
	for i, due := range a.dues {
		paid := creditedBy(credits, due)
		short := max(0, a.required[i]-paid)
		// What is still unpaid of the earlier dates is that date's
		// shortfall, not this one's.
		own := max(0, short-max(0, before-paid))
		b := estimated.NewInstallment(i+1, due).NeededBy(a.required[i]).CreditedBy(paid).Shortfall(own)
		if a.excusedJanuary && i == 3 {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
		before = a.required[i]
	}
	return out, nil
}

func (r kansasEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("kansastaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Kansas return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	credits, notes, err := r.credits(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule, err := installments(a, credits)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if a.exempt {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		floor, err := r.floorDivergence(0)
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		var fn working.Notes
		fn.Diverges(floor)
		return working.NewResult(p).With(a.notes).With(fn).Build()
	}
	p, unrated, err := columnPenalty(a.dues, a.required, credits, stop, y.rates, waiver, a.requirement, schedule)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("Schedule K-210 (2025), lines 9 and 11", fmt.Sprintf("this year's Kansas tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("K.S.A. 79-32,107(a); Schedule K-210 (2025), instructions for line 14", fmt.Sprintf("the return was filed with the whole balance paid on %s, so the penalty stops that day", stop.Long()))
	}
	notes.Rule("K.S.A. 79-32,107(a), 79-2968; Schedule K-210 (2025), Part II", fmt.Sprintf("at each due date the total still short is charged %v a year, by the day over 365, until the next due date or until paid, and after January 15 until paid or %s; "+
		"each charge is rounded to the cent; the penalty to %s is %v", y.rates.Periods()[0].Rate(), y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The Kansas rate for the days after %s isn't published yet.", unrated.AddDays(-1).Long()))
	}
	if t := p.Total(); t > 0 && t <= y.deMinimis {
		notes.Rule("K.S.A. 79-32,107(a)", fmt.Sprintf("the penalty of %v does not exceed %v, and the statute does not add a penalty that small; Schedule K-210 has no such floor, so the figure shows it", t, y.deMinimis))
	}
	floor, err := r.floorDivergence(p.Total())
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	notes.Diverges(floor)
	switch {
	case p.Total() > 0 && !waiverAnswered:
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking Kansas to waive or reduce your penalty for reasonable cause?",
			"Kansas hasn't said if the law's reasonable-cause waiver reaches this penalty. The penalty shown is before any waiver.")
	case waiverAnswered && waiver != estimated.WaiverNone:
		notes.Rule("K.S.A. 79-32,107(g)", fmt.Sprintf("Kansas publishes no waiver for %v; the secretary can waive for reasonable cause, and the penalty shown is before any waiver", waiver))
		if p.Total() > 0 {
			// No Kansas document has a casualty, disaster, retirement or
			// disability waiver, and § 79-32,107(g) doesn't say whether its
			// reasonable-cause waiver reaches the subsection (a) penalty.
			if p, err = p.ToBuilder().WaiverOutcome(estimated.WaiverUndecided("Kansas has no casualty, disaster, retirement or disability waiver. It hasn't said if the secretary's reasonable-cause waiver covers this penalty.")).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			notes.Ask(working.PositionUnsettledRule, "",
				"Kansas law lets the department waive a penalty for reasonable cause. It doesn't say whether that reaches the estimated tax penalty.")
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// columnPenalty is Schedule K-210 Part II. Each due date is a column: what
// was still short in total on that date (line 11) is charged by the day,
// falling as later credits pay it, until the next due date, or for the last
// column until stop. A column's days in each rate period are charged at that
// period's rate and rounded to the cent.
//
// unrated is the first day the series has no rate for, when any column runs
// into it, and the zero Date otherwise.
//
// req and schedule are what the penalty was figured from, for its reason.
func columnPenalty(dues []estimated.Date, required []money.USCents, credits []estimated.Payment, stop estimated.Date, rates estimated.RateSeries, waiver estimated.WaiverKind,
	req estimated.Requirement, schedule []estimated.Installment) (estimated.Penalty, estimated.Date, error) {
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(req, schedule)
	var unrated estimated.Date
	for i, due := range dues {
		if !due.Before(stop) {
			break
		}
		end := stop
		if i+1 < len(dues) && dues[i+1].Before(end) {
			end = dues[i+1]
		}
		owed := required[i] - creditedBy(credits, due)
		if owed <= 0 {
			continue
		}
		lb := estimated.NewPenaltyLine(i+1, due, owed)
		cur := due
		for _, d := range append(paidBetween(credits, due, end), end) {
			parts, gap, err := estimated.SimpleDaily(owed, cur, d, rates)
			if err != nil {
				return estimated.Penalty{}, estimated.Date{}, err
			}
			if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
				unrated = gap
			}
			for _, p := range parts {
				lb.Part(p)
			}
			cur = d
			if owed = required[i] - creditedBy(credits, d); owed <= 0 {
				break
			}
		}
		line, err := lb.Days(cur.DaysSince(due)).Build()
		if err != nil {
			return estimated.Penalty{}, estimated.Date{}, err
		}
		pb.Line(line)
	}
	p, err := pb.Build()
	return p, unrated, err
}

// paidBetween is the days after from and before to on which a credit was
// paid, earliest first.
func paidBetween(cs []estimated.Payment, from, to estimated.Date) []estimated.Date {
	seen := map[estimated.Date]bool{}
	var out []estimated.Date
	for _, c := range cs {
		if d := c.On(); d.After(from) && d.Before(to) && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out
}

func (r kansasEstimated) Explanation() estimated.Explanation { return kansasExplanation }

// kansasExplanation is written by hand and checked against
// research/estimated-payments/ks.md.
var kansasExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"You pay estimated tax once Kansas tax less withholding and credits reaches $500. The penalty is interest on what was short at each quarter. The rate is set for each calendar year.").
		Paragraph("Your target is 90% of this year's Kansas tax or 100% of last year's, if smaller. Withholding and estimated payments should reach it a quarter at a time. If they do, you're clear of the penalty. There's no 110% tier for higher incomes. If you filed no Kansas return last year, or it showed no tax, no penalty arises. Kansas's penalty schedule counts last year's tax as zero in either case. The law is narrower. It gives zero only if no return was required, or last year's tax was under $200.").
		Paragraph("Four equal quarters are due April 15, June 15, September 15 and next January 15. A date on a weekend or legal holiday is due the next workday instead. Withholding counts as paid evenly across the four dates, whenever it was taken. Filing and paying in full by January 31 replaces the January payment.").
		Paragraph("The penalty is figured on Schedule K-210. At each date, interest is charged by the day on the total still short. It runs until the next date or until it's paid. After January 15, it runs until it's paid or April 15. Each year's rate is the prior July's federal underpayment rate plus one point. It is 8% for 2026. If your income came unevenly, the annualized method can lower early quarters.").
		VersusFederal("Unlike federal, the threshold is $500, not $1,000, and there's no 110% tier. No return or no tax last year means no penalty, with no federal full-year test. The rate is set once a year at the federal rate plus one point. Withholding can't be credited on the dates it was taken. Kansas has no casualty, retirement or disability waiver.").
		Source("K.S.A. 79-32,101, 79-32,102, 79-32,103, 79-32,107 and 79-2968").
		Source("Kansas Department of Revenue, K-40ES, Kansas 2026 Individual Estimated Tax (rev. 8-26-25)").
		Source("Kansas Department of Revenue, Schedule K-210, Kansas Underpayment of Estimated Tax, 2025, and its instructions (rev. 9-15-25)").
		Source("Kansas Department of Revenue, 2025 Kansas Individual Income Tax booklet, Form K-40 lines 18 to 31").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
