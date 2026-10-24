package vermonttaxcalc

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Vermont's estimated income tax rules, 32 V.S.A. §§ 5851, 5852 and 5859,
// with interest at the rate set under § 3108 and the penalty under
// § 3202(b)(2), as the shared [estimated.EstimatedTax] interface.
//
// The required annual payment is the lesser of 90% of this year's tax and
// 100% of last year's, paid a quarter on each due date. A shortfall is
// charged twice: interest by the day at the year's rate, and a penalty of 1%
// for each month or part of a month. No penalty or interest is charged when
// this year's tax less withholding is under $500. Filers with two-thirds of
// their income from farming or fishing make no estimated payments.
//
// The figure follows the Department of Taxes' worksheets, IN-152 and
// IN-152A (Rev. 10/25), the latest published, and the result's Divergences
// say where 32 V.S.A. reads otherwise. The statutes were last amended in
// 2024 (§ 5859, effective July 1, 2024), before the worksheets' revision, so
// no form here predates the law it describes
// (research/estimated-payments/vt.md, 2026 changes).

// jurisdiction is Vermont's postal code. It is not in estimated's list of
// jurisdictions; the registry adds it.
const jurisdiction = estimated.Vermont

// EstimatedFacts are the facts Vermont's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorFarmer         bool
	priorFarmerAnswered bool
	federalRequired     money.USCents
	federalInstallments []money.USCents
}

// PriorYearFarmerOrFisher reports whether at least two-thirds of last year's
// gross income came from farming or fishing, and whether the caller said.
// The IN-111 instructions exempt a filer who meets the test in either year;
// [estimated.Facts.FarmShare] and [estimated.Facts.FishShare] are this
// year's.
func (e EstimatedFacts) PriorYearFarmerOrFisher() (farmer, answered bool) {
	return e.priorFarmer, e.priorFarmerAnswered
}

// FederalAnnualised returns the federal figures IN-152A scales: the federal
// required annual payment, Form 2210 line 9, and the four required
// installments from Form 2210 Schedule AI line 27. It is false when they
// were not given.
func (e EstimatedFacts) FederalAnnualised() (required money.USCents, installments []money.USCents, given bool) {
	if e.federalInstallments == nil {
		return 0, nil, false
	}
	return e.federalRequired, append([]money.USCents(nil), e.federalInstallments...), true
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorYearFarmerOrFisher(farmer bool) *EstimatedFactsBuilder {
	b.e.priorFarmer, b.e.priorFarmerAnswered = farmer, true
	return b
}

// FederalAnnualised records the federal Form 2210 line 9 and Schedule AI
// line 27 figures, for a filer who annualises federally.
func (b *EstimatedFactsBuilder) FederalAnnualised(required money.USCents, installments ...money.USCents) *EstimatedFactsBuilder {
	b.e.federalRequired = required
	b.e.federalInstallments = append([]money.USCents{}, installments...)
	return b
}

// Build refuses federal figures that IN-152A could not divide by: anything
// but four installments that are not negative, and a federal required
// annual payment that is not above zero.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	e := b.e
	if e.federalInstallments != nil {
		if len(e.federalInstallments) != 4 {
			return EstimatedFacts{}, fmt.Errorf("vermonttaxcalc: Schedule AI has four installments, not %d", len(e.federalInstallments))
		}
		if e.federalRequired <= 0 {
			return EstimatedFacts{}, errors.New("vermonttaxcalc: IN-152A divides by the federal required annual payment, which must be above zero")
		}
		for _, a := range e.federalInstallments {
			if a < 0 {
				return EstimatedFacts{}, fmt.Errorf("vermonttaxcalc: a federal installment of %v is negative", a)
			}
		}
		e.federalInstallments = append([]money.USCents(nil), e.federalInstallments...)
	}
	return e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// thisYear and priorYear are the two shares of the required annual
	// payment, 32 V.S.A. § 5851(2).
	thisYear, priorYear money.Rate

	// threshold is the tax less withholding below which no penalty or
	// interest is charged.
	threshold money.USCents

	// shares are the part of the required annual payment due by each date,
	// and the part of the withholding deemed paid on each.
	shares []money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// stop is the last day interest and penalty run, IN-152 line 7.
	stop estimated.Date

	// rates are the annual interest rates, one period for each calendar
	// year, and dayBasis the days IN-152 lines 9a and 9b divide by.
	rates    estimated.RateSeries
	dayBasis int

	// monthly is the penalty for each month or part of a month, IN-152
	// line 13 and 32 V.S.A. § 3202(b)(2).
	monthly money.Rate
}

var estimatedYears = map[int]estimatedYear{}

// registerEstimated adds a year's figures. Each rate period must be one
// calendar year, because § 3108 sets one rate for each and interest looks
// a rate up by its year.
func registerEstimated(y estimatedYear) {
	for _, p := range y.rates.Periods() {
		if p.From() != estimated.MustDate(p.From().Year(), 1, 1) || p.To() != estimated.MustDate(p.From().Year(), 12, 31) {
			panic(fmt.Sprintf("vermonttaxcalc: the rate from %v to %v is not one calendar year", p.From(), p.To()))
		}
	}
	estimatedYears[y.year] = y
}

// EstimatedTax returns Vermont's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return vermontEstimated{y}, nil
}

type vermontEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// required is each installment: IN-152 line 4, or IN-152A line 6 for a
	// filer who annualises.
	required []money.USCents

	notes working.Notes
}

// affects records, for each divergence, whether its readings give these facts
// different figures or answers.
type affects struct {
	threshold, measured, priorFigure, withholding, later, annualised, moved bool
}

func (r vermontEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("vermonttaxcalc: the facts carry %T, not vermonttaxcalc.EstimatedFacts", s)
	}
}

func (r vermontEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("vermonttaxcalc: these are Vermont's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("vermonttaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	}
	return nil
}

// divergences are where the Department's documents and 32 V.S.A. part ways
// on a rule every Vermont computation uses. They are found in
// research/estimated-payments/vt.md, question 10 and its verification, and
// in the statute extracts in testdata/.
func (r vermontEstimated) divergences(af affects) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The $500 threshold").
			Form("No penalty or interest when this year's tax less withholding is less than $500. The IN-114 voucher instead charges those with more than $500 due.",
				"IN-111 Instructions (2025), Line 31; IN-114 Instructions (2026), How Do I Determine My Estimated Tax Liability?; IN-114 (2026), Underpayment Interest and Penalties").
			Statute("The individual rules set no dollar threshold. The only $500 minimum tax requirement is in a separate declaration schedule.",
				"32 V.S.A. §§ 5851, 5852; 32 V.S.A. § 5857").
			Uses(working.ReadingForm).Affects(af.threshold),
		working.NewDivergence("What a shortfall is measured against").
			Form("The target is the lesser of 90% of this year's tax and 100% of last year's. Each quarter's shortfall is measured against a quarter of it.",
				"IN-152 (2025), lines 1 to 6").
			Statute("The shortfall is measured against a quarter of 90% of this year's tax. Interest alone is excused if payments reach that or a second figure, if lower. The second figure is last year's facts at this year's rates.",
				"32 V.S.A. § 5859(b)(1)(A), (c)").
			Uses(working.ReadingForm).Affects(af.measured),
		working.NewDivergence("The prior-year figure").
			Form("100% of last year's Vermont tax minus credits.",
				"IN-152 (2025), line 2; 32 V.S.A. § 5851(2)(B)").
			Statute("For the interest exception, it's the tax at this year's rates. That tax uses the facts on last year's return, under last year's law.",
				"32 V.S.A. § 5859(c)(1)").
			Uses(working.ReadingForm).Affects(af.priorFigure),
		working.NewDivergence("When withholding counts").
			Form("Withholding counts as paid 25% on each due date. The form has no line for the dates it was withheld.",
				"IN-152 (2025), line 5a").
			Statute("An equal part counts as paid on each due date. You can instead show the dates it was actually withheld.",
				"32 V.S.A. § 5852(c)").
			Uses(working.ReadingForm).Affects(af.withholding),
		working.NewDivergence("How a later payment is applied").
			Form("A quarter's payments above its installment go to the next quarter. The whole shortfall is charged until it's paid in full. The form doesn't say how a later payment reaches an earlier quarter.",
				"IN-152 (2025), lines 5c, 6 and 7").
			Statute("Each portion of the shortfall is charged until that portion is paid. A payment on a due date goes first to that date's installment. Only the part beyond that date's installment goes to an earlier shortfall.",
				"32 V.S.A. § 5859(b)(2)(A)(ii), (B)").
			Uses(working.ReadingForm).Affects(af.later),
		working.NewDivergence("The annualised installment").
			Form("Each installment starts from the federal Schedule AI installment. It's divided by the federal required annual payment and multiplied by Vermont's.",
				"IN-152A (2025), lines 3 to 6").
			Statute("The law says \"24 percent\" of the federal required payment. That's reduced by the share of adjusted gross income that isn't Vermont income.",
				"32 V.S.A. § 5852(b)").
			Uses(working.ReadingForm).Affects(af.annualised),
		working.NewDivergence("A due date on a weekend or holiday").
			Form("A weekend or federal or state holiday moves a due date to the next business day.",
				"Vermont Department of Taxes, Timely Filing and Payments and State Holiday Closures pages").
			Statute("An act due on a Sunday is valid on the following Monday. Saturdays and holidays aren't mentioned.",
				"32 V.S.A. § 3004").
			Uses(working.ReadingForm).Affects(af.moved),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (r vermontEstimated) assess(f estimated.Facts) (assessment, error) {
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
	var af affects
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] && (y.dues[i].Weekday() != time.Sunday || a.dues[i] != y.dues[i].AddDays(1)) {
			af.moved = true
		}
	}
	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()

	// IN-152 line 1: 90% of this year's tax minus credits.
	thisAmount := y.thisYear.Of(tax)
	thisOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYear, thisAmount)
	if !taxKnown {
		thisOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYear, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("32 V.S.A. § 5851(2)(A); IN-152 (2025), line 1", fmt.Sprintf("%v of the %v tax minus credits is %v", y.thisYear, tax, thisAmount))
	}

	// IN-152 line 2: 100% of last year's tax minus credits, from a filed
	// return covering twelve months.
	prior, hasPrior := f.PriorYear()
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYear, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Vermont return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Vermont return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Vermont return covered %d months, not 12", last, prior.Months()))
	default:
		pf, _ := prior.Figures()
		priorTax, _ := pf.Tax()
		amount := y.priorYear.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYear, amount)
		a.notes.Rule("32 V.S.A. § 5851(2)(B); IN-152 (2025), line 2", fmt.Sprintf("%v of the %v %d tax minus credits is %v", y.priorYear, priorTax, last, amount))
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
	priorFarmer, priorFarmerAnswered := extras.PriorYearFarmerOrFisher()
	owed := tax - withheld
	switch {
	case f.FarmerOrFisher() || priorFarmer:
		a.exempt = true
		which := fmt.Sprintf("%d", y.year)
		if !f.FarmerOrFisher() {
			which = fmt.Sprintf("%d", last)
		}
		ex, err := estimated.NewException("Two-thirds of income from farming or fishing").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("At least two-thirds of your %s income was from farming or fishing. So Vermont doesn't require estimated payments.", which)).Exempt(ex)
		a.notes.Rule("32 V.S.A. § 5852(a); IN-114 Instructions (2026); IN-111 Instructions (2025), Farmer/Fisherman",
			fmt.Sprintf("two-thirds or more of the %s gross income was from farming or fishing, so no estimated payments are required", which))
	case !taxKnown && !priorOpt.Available():
		why := fmt.Sprintf("Your target is the smaller of two figures. They're %v of your %d Vermont tax and %v of your %d tax.", y.thisYear, y.year, y.priorYear, last)
		if hasPrior {
			why = fmt.Sprintf("Your target is %v of your %d Vermont tax. Last year's option isn't open to you.", y.thisYear, y.year)
		}
		rb.Required(false, why).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches your %d tax of %s. No estimated payments are needed, whatever your %d tax is.",
			withheld.Whole(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is your %d tax, %s. That keeps you clear of the penalty whatever your %d tax turns out to be. After %s of withholding, that's %s of estimated payments. Pay a quarter by each date.",
			last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case owed < y.threshold:
		a.exempt = true
		ex, err := estimated.ExceptionBelow("Tax less withholding", owed, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your Vermont tax of %s less %s withheld is under %s. Vermont charges no penalty or interest, and no estimated payments are needed.",
			tax.Whole(), withheld.Whole(), y.threshold.Whole())
		if owed < 0 {
			why = fmt.Sprintf("Your Vermont tax of %s less %s withheld %s. Vermont charges no penalty or interest, and no estimated payments are needed.",
				tax.Whole(), withheld.Whole(), estimated.Leaves(owed))
		}
		rb.Required(false, why).Exempt(ex)
		a.notes.Rule("IN-111 Instructions (2025), Line 31; IN-114 Instructions (2026)", fmt.Sprintf("%v of tax less %v of withholding is %v, less than %v, so there is no penalty or interest", tax, withheld, owed, y.threshold))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Vermont withholding of %s covers the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Vermont tax of %s less %s withheld leaves %s due. Withholding falls short of your %s target. You need estimated payments of %s, a quarter by each date.",
			tax.Whole(), withheld.Whole(), owed.Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// An exception sets the target, so last year's return can't change it.
	if !hasPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a full-year %d Vermont return, and what was its tax?", last),
			fmt.Sprintf("With it, %v of last year's tax could set a lower target.", y.priorYear))
	}
	if !taxKnown && !a.exempt {
		effect := fmt.Sprintf("Without it, your target is your %d tax. The penalty shown is the most it can be.", last)
		if !priorOpt.Available() {
			effect = fmt.Sprintf("With it, %v of this year's tax can set your target. A balance under %s would need no payments.", y.thisYear, y.threshold.Whole())
		}
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Vermont tax to be?", y.year), effect)
	}

	if taxKnown && !a.exempt && owed == y.threshold {
		a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("At exactly %s due, the instructions excuse only less than %s. The voucher charges only more, and the figures follow the instructions.",
			y.threshold.Whole(), y.threshold.Whole()))
	}
	if !a.exempt && !priorFarmerAnswered {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did two-thirds of your %d gross income come from farming or fishing?", last),
			"If so, Vermont doesn't require estimated payments.")
	}

	// What each divergence turns on in these facts.
	af.threshold = taxKnown && !f.FarmerOrFisher() && !priorFarmer && owed > 0 && owed < y.threshold
	af.measured = !a.exempt && a.requirement.TargetKnown() && chosen == estimated.BasisPriorYearTax
	af.priorFigure = af.measured
	af.withholding = !a.exempt && f.Elections().WithholdingAsWithheld() && withheld > 0

	if a.exempt {
		a.required = make([]money.USCents, len(a.dues))
		return r.withDivergences(a, af)
	}

	cum, err := estimated.CumulativeShares(target, y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.required = estimated.Increments(cum)
	a.notes.Rule("32 V.S.A. § 5852(a); IN-152 (2025), line 4; IN-114 Instructions (2026)", fmt.Sprintf("%v of %v, %v, is due on each of %s, %s, %s and %s", y.shares[0], target, a.required[0],
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() {
		fedRequired, fedInstallments, given := extras.FederalAnnualised()
		if !given {
			a.notes.Ask(working.PositionJurisdictionFact, "What are your federal Schedule AI installments and required annual payment?",
				"Without them, the regular quarters apply. Vermont scales its annualised installments from the Form 2210 ones.")
		} else {
			af.annualised = true
			for i, fi := range fedInstallments {
				a.required[i] = scale(fi, target, fedRequired)
			}
			a.notes.Rule("IN-152A (2025), lines 3 to 6; IN-114 Instructions (2026)", fmt.Sprintf("each federal Schedule AI installment divided by the federal %v and multiplied by the Vermont %v gives %v, %v, %v and %v",
				fedRequired, target, a.required[0], a.required[1], a.required[2], a.required[3]))
		}
	}
	if !f.RequirementFirstMet().IsZero() {
		a.notes.Rule("IN-152 (2025), line 4", "IN-152 has no rule for a requirement first met during the year; every installment is due from the first date")
	}
	if a.requirement.TargetKnown() {
		cs, _, err := r.credits(f, a)
		if err != nil {
			return assessment{}, err
		}
		for _, e := range ledger(a.dues, a.required, cs) {
			if e.pieces > 1 || (e.pieces == 1 && e.outstanding > 0) {
				af.later = true
			}
		}
	}
	return r.withDivergences(a, af)
}

// withDivergences adds the divergences to the assessment's notes.
func (r vermontEstimated) withDivergences(a assessment, af affects) (assessment, error) {
	ds, err := r.divergences(af)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(ds...)
	return a, nil
}

// scale is a × num ÷ den, rounded to the cent with halves up: IN-152A line
// 6. den is above zero.
func scale(a, num, den money.USCents) money.USCents {
	n := new(big.Int).Mul(big.NewInt(int64(a)), big.NewInt(int64(num)))
	return estimated.RoundToCent.Div(n, big.NewInt(int64(den)))
}

// entry is one IN-152 column after the credits have been applied.
type entry struct {
	due        estimated.Date
	required   money.USCents
	neededBy   money.USCents
	creditedBy money.USCents
	shortfall  money.USCents

	// outstanding is what is left of the shortfall, and paidOn the day a
	// later credit paid the last of it.
	outstanding money.USCents
	paidOn      estimated.Date

	// repaid is how much of the shortfall later credits paid, and pieces how
	// many credits paid some of it.
	repaid money.USCents
	pieces int
}

// ledger applies the credits to the installments as IN-152 lines 5a to 7
// do, with 32 V.S.A. § 5859(b)(2) where the form is silent.
//
// A credit counts toward the first installment due on or after its date
// (line 5b). Credits dated on a due date go to that installment first, and
// what is left pays any earlier underpayment, oldest first, as
// § 5859(b)(2)(B) says. A credit between due dates is not on an installment
// date, so it pays any earlier underpayment first, on its own date. What is
// left over is held toward the next installment (line 5c). Credits after the
// last due date pay the oldest underpayment first.
//
// This belongs beside [estimated.Apply], which pays the oldest underpayment
// first with every credit, including one dated on a due date.
func ledger(dues []estimated.Date, required []money.USCents, credits []estimated.Payment) []entry {
	out := make([]entry, len(dues))
	payOld := func(n int, amount money.USCents, on estimated.Date) money.USCents {
		for j := 0; j < n && amount > 0; j++ {
			if out[j].outstanding == 0 {
				continue
			}
			paid := min(out[j].outstanding, amount)
			out[j].outstanding -= paid
			out[j].repaid += paid
			out[j].pieces++
			if out[j].outstanding == 0 {
				out[j].paidOn = on
			}
			amount -= paid
		}
		return amount
	}
	var pool, credited, needed money.USCents
	next := 0
	for i, due := range dues {
		for next < len(credits) && credits[next].On().Before(due) {
			credited += credits[next].Amount()
			pool += payOld(i, credits[next].Amount(), credits[next].On())
			next++
		}
		var onDue money.USCents
		for next < len(credits) && credits[next].On() == due {
			credited += credits[next].Amount()
			onDue += credits[next].Amount()
			next++
		}
		needed += required[i]
		avail := pool + onDue
		covered := min(avail, required[i])
		pool = payOld(i, avail-covered, due)
		out[i] = entry{due: due, required: required[i], neededBy: needed, creditedBy: credited,
			shortfall: required[i] - covered, outstanding: required[i] - covered}
	}
	for ; next < len(credits); next++ {
		payOld(len(dues), credits[next].Amount(), credits[next].On())
	}
	return out
}

// credits are what IN-152 lines 5a and 5b count, in date order.
func (r vermontEstimated) credits(f estimated.Facts, a assessment) ([]estimated.Payment, working.Notes, error) {
	cs, err := estimated.Credits(f, a.dues, r.y.shares)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	notes.Rule("IN-152 (2025), lines 5a to 5c", fmt.Sprintf("the %v withheld is treated as paid %v on each due date, whenever it was withheld; a payment counts toward the next due date on or after it, "+
		"and a quarter's payments above its installment go to the next quarter", f.WithheldTotal(), r.y.shares[0]))
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("IN-152 (2025), line 5a; 32 V.S.A. § 5852(c)", "IN-152 has no line for the dates withholding was taken, so it is spread evenly; the statute allows the actual dates, which the Divergences report")
	}
	return cs, notes, nil
}

// repaidNote is the open question a later payment raises.
func repaidNote(n *working.Notes, entries []entry) {
	for _, e := range entries {
		if e.repaid > 0 {
			n.Ask(working.PositionUnsettledRule, "", "With the worksheet silent on later payments, the figures follow the law. A due-date payment goes to its quarter first, others to the oldest shortfall.")
			return
		}
	}
}

func (r vermontEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r vermontEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Vermont return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	cs, notes, err := r.credits(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	entries := ledger(a.dues, a.required, cs)
	out, err := schedule(entries)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	repaidNote(&notes, entries)
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments from the IN-152 columns.
func schedule(entries []entry) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(entries))
	for i, e := range entries {
		in, err := estimated.NewInstallment(i+1, e.due).NeededBy(e.neededBy).CreditedBy(e.creditedBy).Shortfall(e.shortfall).Build()
		if err != nil {
			return nil, err
		}
		out[i] = in
	}
	return out, nil
}

// interest is IN-152 lines 8a to 9b on one shortfall: the days after the due
// date up to the full payment or January 1 of the next year at the due
// year's rate, and the days after that January 1 at the next year's. Each
// part is the shortfall × days ÷ the day basis × the rate, rounded to the
// cent.
//
// unrated is the first day no published rate covers, or the zero Date.
func (r vermontEstimated) interest(shortfall money.USCents, due, paid estimated.Date) ([]estimated.PenaltyPart, estimated.Date, error) {
	y := r.y
	newYear := estimated.MustDate(due.Year()+1, 1, 1)
	type span struct {
		from, to estimated.Date
		year     int
	}
	spans := []span{{due, paid, due.Year()}}
	if paid.After(newYear) {
		spans = []span{{due, newYear, due.Year()}, {newYear, paid, due.Year() + 1}}
	}
	var parts []estimated.PenaltyPart
	for _, s := range spans {
		period, ok := y.rates.On(estimated.MustDate(s.year, 1, 1))
		if !ok {
			return parts, s.from.AddDays(1), nil
		}
		days := s.to.DaysSince(s.from)
		n := new(big.Int).Mul(big.NewInt(int64(shortfall)), big.NewInt(int64(days)))
		n.Mul(n, big.NewInt(int64(period.Rate())))
		charge := estimated.RoundToCent.Div(n, big.NewInt(int64(10_000*y.dayBasis)))
		p, err := estimated.NewPenaltyPart(estimated.PartInterest, shortfall, period.Rate()).
			Span(s.from, s.to).Days(days).Charge(charge).Source(period.Source()).Build()
		if err != nil {
			return nil, estimated.Date{}, err
		}
		parts = append(parts, p)
	}
	return parts, estimated.Date{}, nil
}

func (r vermontEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("vermonttaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Vermont return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := asOf
	if y.stop.Before(stop) {
		stop = y.stop
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop = filed
	}
	waiver, _ := f.Positions().Waiver()
	cs, notes, err := r.credits(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	entries := ledger(a.dues, a.required, cs)
	sched, err := schedule(entries)
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

	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, sched)
	var unrated estimated.Date
	for i, e := range entries {
		if e.shortfall == 0 {
			continue
		}
		// IN-152 line 7: the date full payment was made, or April 15.
		paid := stop
		if e.outstanding == 0 && e.paidOn.Before(stop) {
			paid = e.paidOn
		}
		if !paid.After(e.due) {
			continue
		}
		lb := estimated.NewPenaltyLine(i+1, e.due, e.shortfall).Days(paid.DaysSince(e.due))
		parts, gap, err := r.interest(e.shortfall, e.due, paid)
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
			unrated = gap
		}
		for _, p := range parts {
			lb.Part(p)
		}
		monthly, err := estimated.Monthly(e.shortfall, e.due, paid, y.monthly, "32 V.S.A. § 3202(b)(2); IN-152 (2025), lines 12 and 13")
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		lb.Part(monthly)
		line, err := lb.Build()
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
	if p.Total() == 0 && p.UpperBound() {
		// No installment was short of the prior-year figure, which the
		// lesser can only lower, so the zero is exact.
		if p, err = estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, sched).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	notes.Rule("32 V.S.A. §§ 3108, 3202(b)(2), 5859(b); IN-152 (2025), lines 6 to 15", fmt.Sprintf("each quarter's whole underpayment is charged interest by the day, over %d days a year at each calendar year's rate, and %v for each month or part of a month, "+
		"from its due date until it is paid in full or %s; the total to %s is %v", y.dayBasis, y.monthly, y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("Interest for %d isn't charged until its rate is published. The 1%% a month penalty is charged in full.", unrated.Year()))
	}
	repaidNote(&notes, entries)
	if k, asked := f.Positions().Waiver(); asked && k != estimated.WaiverNone {
		notes.Rule("32 V.S.A. §§ 3202, 5851 to 5859; IN-152 (2025)", "Vermont's estimated tax statutes and worksheets provide no casualty, retirement or disability waiver; the penalty is shown in full")
		if p.Total() > 0 {
			o := estimated.WaiverRefused("Vermont's law has no waiver of the estimated tax penalty or interest. That holds for a casualty, disaster, retirement or disability.")
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r vermontEstimated) Explanation() estimated.Explanation { return vermontExplanation }

// vermontExplanation is written by hand and checked against
// research/estimated-payments/vt.md.
var vermontExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Vermont expects estimated payments when withholding will not cover your tax. If you fall short, it charges interest on each quarter's shortfall. It also charges a separate penalty of 1% a month. Neither applies if this year's tax less withholding is under $500.").
		Paragraph("You are clear if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. Last year's figure counts only if you filed a full-year return. There is no 110% tier for high incomes.").
		Paragraph("The payments are a quarter each. They're due April 15, June 15, September 15 and the next January 15. Withholding counts as paid evenly across the four dates, whenever it was taken. If you annualise your federal estimates, you can annualise Vermont's too. They follow the federal installments in proportion.").
		Paragraph("Each quarter's shortfall is charged two ways. Both run until it is paid in full or until April 15 of the next year. Interest runs by the day at a rate set once each calendar year, 7.75% for 2026. The penalty is 1% of the shortfall for each month or part of a month. So a payment one day late costs a full 1%.").
		Paragraph("The Child Care Contribution applies to self-employed people working in Vermont. They should include it in their payments.").
		VersusFederal("Vermont differs from federal in six ways that change a number. Its threshold is $500, not $1,000. There's no 110% tier. A 1% monthly penalty comes on top of the interest. The whole shortfall is charged until it is paid in full. Withholding is always spread evenly. There's no casualty, retirement or disability waiver.").
		Source("32 V.S.A. §§ 3108, 3202(b)(2), 5851, 5852, 5859").
		Source("IN-114, Vermont Individual Income Estimated Tax Payment Voucher, and its instructions, 2026").
		Source("IN-152 and IN-152A, Underpayment of Estimated Individual Income Tax, 2025").
		Source("Vermont Department of Taxes, 2026 Interest Rate memo, November 4, 2025").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
