package mainetaxcalc

import (
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Maine's estimated tax rules, 36 M.R.S. § 5228, as the shared
// [estimated.EstimatedTax] interface.
//
// Maine differs from the federal rules in shape. No payments are needed if
// either this year's tax after withholding or last year's liability is under
// $1,000. There is no 110% tier. Instead, income not subject to Maine
// withholding that is $500,000 or more above the same period of last year is
// an "unusual event": 90% of the tax on it is due with that period's
// installment, and last year's liability does not cover it. The penalty is
// interest under § 186, charged by the month, a part of a month counting as a
// whole one, and compounded monthly.
//
// The figure follows Form 2210ME (2025), the latest penalty form, and the
// result's Divergences say where § 5228 reads otherwise. No form here is
// followed against a later change in the law. P.L. 2025, c. 113 amended the
// January 31 relief effective September 24, 2025, before the 2026 Form
// 1040ES-ME was revised in July 2026, and the 2025 Form 2210ME carries the
// relief. P.L. 2025, c. 650 took the pass-through entity tax out of estimated
// tax; the caller's figure leaves it out, so no form's reading is used for it.

const jurisdiction = estimated.Maine

// EstimatedFacts are the facts Maine's rules read that no other jurisdiction
// does. Carry them in [estimated.Facts.State].
//
// They answer whether the year had an unusual event under 36 M.R.S.
// § 5228(1)(D): taxable income not subject to Maine withholding, received in
// one installment's period, that exceeds what was received in the same period
// of last year by at least $500,000. Either all four periods are given or
// [EstimatedFactsBuilder.NoUnusualEvent] says there was none. Left unanswered,
// the rules assume none and say so in Required.
//
// The periods are taken to be those that § 5228(5)(A) and the Annualized
// Income Installment Worksheet close each installment at: January 1 to March
// 31 for the first, April 1 to May 31, June 1 to August 31, and September 1 to
// December 31. § 5228 speaks of "that portion of the tax year applicable to
// the required installment" and does not date it.
type EstimatedFacts struct {
	income      [4]incomePair
	eventTax    [4]optional
	none        bool
	passThrough bool
	ptAnswered  bool
}

type incomePair struct {
	thisYear, lastYear money.USCents
	set                bool
}

type optional struct {
	v   money.USCents
	set bool
}

// IncomeNotWithheld is the Maine taxable income not subject to Maine
// withholding received in installment n's period (1 to 4) this year and in
// the same period last year, and whether it was given.
func (e EstimatedFacts) IncomeNotWithheld(n int) (thisYear, lastYear money.USCents, ok bool) {
	if n < 1 || n > 4 {
		return 0, 0, false
	}
	p := e.income[n-1]
	return p.thisYear, p.lastYear, p.set
}

// UnusualEventTax is the Maine tax on the income that makes installment n's
// period an unusual event, the excess over last year's same period, and
// whether it was given. 90% of it is due with that installment.
func (e EstimatedFacts) UnusualEventTax(n int) (money.USCents, bool) {
	if n < 1 || n > 4 {
		return 0, false
	}
	return e.eventTax[n-1].v, e.eventTax[n-1].set
}

// NoUnusualEvent reports that the caller said no period had an unusual
// event.
func (e EstimatedFacts) NoUnusualEvent() bool { return e.none }

// PassThroughWaiver reports whether all four conditions of § 5228(11) hold
// for the unusual event, and whether the caller said: the filer is an owner in
// a pass-through entity, had no control over the distribution, did not
// actually or constructively receive it, and pays the tax on it by the next
// installment date after receiving it. The assessor must then waive the
// penalty on it or extend the time to pay.
func (e EstimatedFacts) PassThroughWaiver() (holds, answered bool) {
	return e.passThrough, e.ptAnswered
}

func (e EstimatedFacts) answered() bool { return e.none || e.income[0].set }

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct {
	e   EstimatedFacts
	err error
}

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

// IncomeNotWithheld records installment n's period (1 to 4): the income not
// subject to Maine withholding received in it this year and in the same
// period last year.
func (b *EstimatedFactsBuilder) IncomeNotWithheld(n int, thisYear, lastYear money.USCents) *EstimatedFactsBuilder {
	switch {
	case n < 1 || n > 4:
		b.fail(fmt.Errorf("mainetaxcalc: there is no installment %d", n))
	case b.e.income[n-1].set:
		b.fail(fmt.Errorf("mainetaxcalc: installment %d's income is given twice", n))
	default:
		b.e.income[n-1] = incomePair{thisYear, lastYear, true}
	}
	return b
}

// UnusualEventTax records the Maine tax on the excess income in installment
// n's period.
func (b *EstimatedFactsBuilder) UnusualEventTax(n int, tax money.USCents) *EstimatedFactsBuilder {
	switch {
	case n < 1 || n > 4:
		b.fail(fmt.Errorf("mainetaxcalc: there is no installment %d", n))
	case b.e.eventTax[n-1].set:
		b.fail(fmt.Errorf("mainetaxcalc: installment %d's unusual-event tax is given twice", n))
	default:
		b.e.eventTax[n-1] = optional{tax, true}
	}
	return b
}

// NoUnusualEvent says no period had an unusual event.
func (b *EstimatedFactsBuilder) NoUnusualEvent() *EstimatedFactsBuilder {
	b.e.none = true
	return b
}

// PassThroughWaiver answers whether the four conditions of § 5228(11) hold.
func (b *EstimatedFactsBuilder) PassThroughWaiver(holds bool) *EstimatedFactsBuilder {
	b.e.passThrough, b.e.ptAnswered = holds, true
	return b
}

func (b *EstimatedFactsBuilder) fail(err error) {
	if b.err == nil {
		b.err = err
	}
}

// Build requires all four periods or none, refuses periods together with
// NoUnusualEvent, a tax for a period whose income was not given, and a
// negative tax. Income can be negative, as a loss.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.err != nil {
		return EstimatedFacts{}, b.err
	}
	e := b.e
	given := 0
	for _, p := range e.income {
		if p.set {
			given++
		}
	}
	switch {
	case given != 0 && given != 4:
		return EstimatedFacts{}, fmt.Errorf("mainetaxcalc: income is given for %d of the four periods; give all four or none", given)
	case given == 4 && e.none:
		return EstimatedFacts{}, errors.New("mainetaxcalc: periods' income and NoUnusualEvent together")
	}
	for i, t := range e.eventTax {
		switch {
		case t.set && !e.income[i].set:
			return EstimatedFacts{}, fmt.Errorf("mainetaxcalc: installment %d has an unusual-event tax and no income", i+1)
		case t.v < 0:
			return EstimatedFacts{}, fmt.Errorf("mainetaxcalc: an unusual-event tax of %v is negative", t.v)
		}
	}
	return e, nil
}

// monthlyRate is the § 186 rate for the days from through to, both included,
// as a rate per month.
type monthlyRate struct {
	from, to estimated.Date
	monthly  money.Rate
	source   string
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is § 5228(2)(C) and (D): no estimates when this year's tax
	// after withholding, or last year's liability, is under it.
	threshold money.USCents

	// thisYearShare and priorYearShare are § 5228(3)(B) and (A).
	thisYearShare, priorYearShare money.Rate

	// unusualEvent is § 5228(1)(D): the increase over last year's same period
	// at which non-withheld income is an unusual event.
	unusualEvent money.USCents

	// dues, fileAndPayBy and stop are as printed or as the statute counts
	// them, before § 153(2) moves them.
	dues         []estimated.Date
	fileAndPayBy estimated.Date
	stop         estimated.Date
	calendar     estimated.Calendar

	periodEnds []estimated.Date
	annualised []money.Rate

	rates []monthlyRate
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) {
	for i, r := range y.rates {
		switch {
		case r.from.IsZero() || r.to.Before(r.from) || r.monthly <= 0 || r.source == "":
			panic(fmt.Sprintf("mainetaxcalc: rate %d for %d is malformed", i, y.year))
		case i > 0 && r.from != y.rates[i-1].to.AddDays(1):
			panic(fmt.Sprintf("mainetaxcalc: rate %d for %d does not follow the one before", i, y.year))
		}
	}
	estimatedYears[y.year] = y
}

// quarters are § 5228(5): four equal installments. Form 2210ME line 10a
// spreads withholding the same way.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Maine's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return maineEstimated{y}, nil
}

type maineEstimated struct{ y estimatedYear }

type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents
	excused      bool
	notes        working.Notes
}

func (r maineEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("mainetaxcalc: the facts carry %T, not mainetaxcalc.EstimatedFacts", s)
	}
}

func (r maineEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("mainetaxcalc: these are Maine's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("mainetaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: § 5228(3)(B), (4) and (10) give farmers and persons who fish commercially one installment at 66 2/3%%, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("mainetaxcalc: Annualized Income Installment Worksheet period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// affected says which divergences change anything for one household's
// facts.
type affected struct {
	liability, thisYearTest, withholding, january bool
}

// divergences are where Maine Revenue Services' documents and § 5228 part
// ways on a rule a Maine computation uses. They are found in
// research/estimated-payments/me.md, item 10 and its verification.
func (r maineEstimated) divergences(a affected) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Last year's liability").
			Form("Last year's liability is the tax after refundable credits.",
				"Form 2210ME (2025), line 5").
			Statute("The law reads last year's \"tax liability\". It doesn't say whether refundable credits reduce it.",
				"36 M.R.S. § 5228(2)(D), (3)(A)").
			Uses(working.ReadingForm).Affects(a.liability),
		working.NewDivergence("The $1,000 test on this year's tax").
			Form("This year's tax less withholding is tested against $1,000.",
				"Form 2210ME (2025), lines 1 to 4").
			Statute("This year's tax is reduced by allowable credits. No payments are needed when the result is under $1,000. Those credits include estimated payments as well as withholding.",
				"36 M.R.S. § 5228(1)(A), (2)(C)").
			Uses(working.ReadingForm).Affects(a.thisYearTest),
		working.NewDivergence("When withholding counts as paid").
			Form("Withholding is entered as 25% on each due date, whenever it was withheld. There's no line for the actual dates.",
				"Form 2210ME (2025), line 10a").
			Statute("The installments can follow when the tax and the credits actually arose. The credits include withholding. You must show it with adequate records.",
				"36 M.R.S. § 5228(1)(A), (5)(A)").
			Uses(working.ReadingForm).Affects(a.withholding),
		working.NewDivergence("A return filed and paid by January 31").
			Form("The estimated tax voucher doesn't mention the relief. The return instructions mention only paying in full. Form 2210ME skips the January penalty when the tax is paid by January 31.",
				"Form 1040ES-ME (revised July 2026); Form 1040ME General Instructions (2025); Form 2210ME (2025)").
			Statute("You can file your return and pay in full by the last day of January. Then there's no penalty on the January payment.",
				"36 M.R.S. § 5228(9), as amended by P.L. 2025, c. 113, Pt. C, § 12, effective September 24, 2025").
			Uses(working.ReadingStatute).Affects(a.january),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// optionLabel is an option's label and amount, for the middle of a sentence.
func optionLabel(o estimated.TargetOption) string {
	l := o.Label()
	if l != "" && !strings.HasPrefix(l, "Maine") {
		l = strings.ToLower(l[:1]) + l[1:]
	}
	return fmt.Sprintf("%s, %s", l, o.Amount().Whole())
}

// events is the unusual events in the facts, after the pass-through waiver.
type events struct {
	// part is 90% of each installment's unusual-event tax, and total their
	// sum.
	part  [4]money.USCents
	total money.USCents
	found bool
}

func (r maineEstimated) unusualEvents(f estimated.Facts, extras EstimatedFacts, a *assessment) (events, error) {
	y := r.y
	var ev events
	if !extras.answered() {
		return ev, nil
	}
	var taxes money.USCents
	for i := range ev.part {
		this, last, _ := extras.IncomeNotWithheld(i + 1)
		if this-last < y.unusualEvent {
			continue
		}
		ev.found = true
		tax, ok := extras.UnusualEventTax(i + 1)
		if !ok {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What is the Maine tax on the extra %s of income without Maine withholding?", (this-last).Whole()),
				fmt.Sprintf("90%% of its tax is due with installment %d, for the period it came in. For now, none of it is counted.", i+1))
		}
		taxes += tax
		ev.part[i] = y.thisYearShare.Of(tax)
		ev.total += ev.part[i]
		a.notes.Rule("36 M.R.S. § 5228(1)(D), (3), (5)(D); Form 1040ES-ME (2026)", fmt.Sprintf("installment %d's period had %v of income not subject to Maine withholding against %v in the same period of %d, %v more, at least %v, so it is an unusual event: "+
			"90%% of the %v tax on it, %v, is due with installment %d, and last year's liability does not cover it", i+1, this, last, y.year-1, this-last, y.unusualEvent, tax, ev.part[i], i+1))
	}
	if !ev.found {
		return ev, nil
	}
	if tax, known := f.ThisYear().Tax(); known && taxes > tax {
		return events{}, fmt.Errorf("mainetaxcalc: the unusual-event tax of %v is more than this year's tax of %v", taxes, tax)
	}
	a.notes.Rule("36 M.R.S. § 5228(1)(D), (5)(A)", "an installment's period is taken as the months since the last one closed: January to March, April and May, June to August, and September to December; § 5228 does not date it")
	holds, answered := extras.PassThroughWaiver()
	switch {
	case holds:
		a.notes.Rule("36 M.R.S. § 5228(11)", "the four pass-through conditions hold, so the assessor must waive the penalty on the unusual event or extend the time to pay it; the installments are figured without it")
		return events{}, nil
	case !answered:
		a.notes.Ask(working.PositionPenaltyWaiver, "Did the extra income come from a pass-through business you own?",
			"Its penalty must be waived if you didn't control or receive the distribution. Its tax must also be paid by the next due date; the figures charge it for now.")
	}
	return ev, nil
}

func (r maineEstimated) assess(f estimated.Facts) (assessment, error) {
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
	var paid money.USCents
	for _, p := range f.Payments() {
		paid += p.Amount()
	}
	by := y.calendar.NextBusinessDay(y.fileAndPayBy)
	filedBy := !f.ReturnFiled().IsZero() && !f.ReturnFiled().After(by)
	var creditedBy money.USCents
	for _, p := range f.Payments() {
		if !p.On().After(by) {
			creditedBy += p.Amount()
		}
	}
	diverge := func(liability bool) error {
		ds, err := r.divergences(affected{
			liability:    liability,
			thisYearTest: taxKnown && tax-withheld >= y.threshold && tax-withheld-paid < y.threshold,
			withholding:  !a.exempt && withheld > 0,
			january:      !a.exempt && (filedBy || (taxKnown && withheld+creditedBy >= tax)),
		})
		if err != nil {
			return err
		}
		a.notes.Diverges(ds...)
		return nil
	}

	// The $1,000 test on this year, Form 2210ME lines 1 to 4. It waives the
	// requirement whatever else is true, unusual events included.
	if taxKnown && tax-withheld < y.threshold {
		a.exempt = true
		thisYear, err := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax)).Build()
		if err != nil {
			return assessment{}, err
		}
		ex, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		none := fmt.Sprintf(" So no estimated payments are needed for %d, and no penalty applies.", y.year)
		why := fmt.Sprintf("Your Maine tax of %s less %s withheld leaves %s, under %s.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), y.threshold.Whole()) + none
		switch {
		case tax < withheld:
			why = fmt.Sprintf("Your Maine tax of %s less %s withheld %s.",
				tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld)) + none
		case tax == withheld:
			why = fmt.Sprintf("Your withholding of %s covers your Maine tax of %s.", withheld.Whole(), tax.Whole()) + none
		case (tax - withheld).Whole() == y.threshold.Whole():
			why = fmt.Sprintf("Your Maine tax less withholding is under %s.", y.threshold.Whole()) + none
		}
		if a.requirement, err = estimated.NewRequirement().Option(thisYear).Required(false, why).Exempt(ex).Build(); err != nil {
			return assessment{}, err
		}
		if err := diverge(false); err != nil {
			return assessment{}, err
		}
		a.notes.Rule("36 M.R.S. § 5228(2)(C); Form 2210ME (2025), lines 1 to 4", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no requirement and no penalty", tax, withheld, tax-withheld, y.threshold))
		a.installments = make([]money.USCents, len(a.dues))
		return a, nil
	}

	ev, err := r.unusualEvents(f, extras, &a)
	if err != nil {
		return assessment{}, err
	}

	// 90% of this year's tax, § 5228(3)(B).
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("36 M.R.S. § 5228(3)(B); Form 1040ES-ME (2026)", fmt.Sprintf("%v of this year's Maine tax of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// Last year's liability, § 5228(3)(A), and the $1,000 test on it,
	// § 5228(2)(D). Maine does not require last year's return to have been
	// filed; the test reads its liability.
	prior, hasPrior := f.PriorYear()
	pf, hasFigures := prior.Figures()
	priorTax, _ := pf.Tax()
	priorSmall := false
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	liabilityQuestion := fmt.Sprintf("What was your %d Maine tax liability?", last)
	liabilityEffect := fmt.Sprintf("Under %s, no estimated payments are needed, or else it can set your target.", y.threshold.Whole())
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Maine return", last))
		a.notes.Ask(working.PositionPriorYearReturn, liabilityQuestion, liabilityEffect)
	case !hasFigures:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Maine tax liability", last))
		a.notes.Ask(working.PositionPriorYearReturn, liabilityQuestion, liabilityEffect)
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Maine return covered %d months, and this option needs a 12-month year", last, prior.Months()))
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("What liability does Form 2210ME give for your %d-month %d return?", prior.Months(), last),
			fmt.Sprintf("For a short year, Form 2210ME uses another figure for the %s test. Without it, last year can't set your target.", y.threshold.Whole()))
	case priorTax < y.threshold:
		priorSmall = true
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, ev.total).
			Label(fmt.Sprintf("Unusual events only; last year's tax was under %s", y.threshold.Whole()))
		a.notes.Rule("36 M.R.S. § 5228(2)(D); Form 2210ME (2025), line 5", fmt.Sprintf("the %d Maine liability of %v is under %v, which waives the requirement except for an unusual event, whose share is %v", last, priorTax, y.threshold, ev.total))
	default:
		amount := y.priorYearShare.Of(priorTax) + ev.total
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, amount)
		if ev.found {
			priorOption.Label(fmt.Sprintf("%v of last year's tax plus the unusual event's share", y.priorYearShare))
		}
		applied := fmt.Sprintf("%v of the %v liability on the %d return is %v", y.priorYearShare, priorTax, last, y.priorYearShare.Of(priorTax))
		if ev.found {
			applied += fmt.Sprintf("; with %v for the unusual event, which it does not cover, the option is %v", ev.total, amount)
		}
		a.notes.Rule("36 M.R.S. § 5228(3)(A); Form 1040ES-ME (2026)", applied)
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
	if !extras.answered() {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did any quarter's income without Maine withholding jump by %s or more?", y.unusualEvent.Whole()),
			fmt.Sprintf("The jump is measured against the same part of %d. If so, 90%% of its tax is due that quarter; until then, the figures assume none.", last))
	}

	chosenOpt := thisYear
	if chosen == estimated.BasisPriorYearTax {
		chosenOpt = priorOpt
	}
	switch {
	case priorSmall && ev.total == 0:
		a.exempt = true
		ex, err := estimated.ExceptionBelow("Last year's tax", priorTax, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		none := fmt.Sprintf(" So no estimated payments are needed for %d, and no penalty applies.", y.year)
		why := fmt.Sprintf("Your %d Maine tax of %s was under %s.", last, priorTax.Whole(), y.threshold.Whole()) + none
		if priorTax.Whole() == y.threshold.Whole() {
			why = fmt.Sprintf("Your %d Maine tax was under %s.", last, y.threshold.Whole()) + none
		}
		rb.Required(false, why).Exempt(ex)
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target comes from your %d Maine tax or your %d liability. There's no target until you have one of them.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your target is %s. Your withholding of %s already reaches it. So no estimated payments are needed, whatever your %d tax turns out to be.",
			optionLabel(priorOpt), withheld.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %s. It keeps you clear of a penalty whatever your %d tax turns out to be. After %s of withholding, you need %s of estimated payments.",
			optionLabel(priorOpt), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Maine withholding of %s already reaches your target of %s. So no estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Maine tax less withholding leaves %s, which is %s or more. Your target is %s. To reach it, you need %s of estimated payments.",
			(tax-withheld).Whole(), y.threshold.Whole(), optionLabel(chosenOpt), (target-withheld).Whole())).Choose(chosen)
	}
	// Under last year's exception this year's tax can't change the target.
	if !taxKnown && !a.exempt {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Maine tax to be?", y.year),
			fmt.Sprintf("%v of it may lower your target; under %s after withholding, none are due.", y.thisYearShare, y.threshold.Whole()))
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	liabilityRead := hasPrior && hasFigures && prior.Months() == 12
	if err := diverge(liabilityRead && (priorSmall || chosen == estimated.BasisPriorYearTax)); err != nil {
		return assessment{}, err
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
		return a, nil
	}

	// The installments: a quarter of the target less the unusual events, and
	// each event's 90% with its own installment, § 5228(5) and (5)(D).
	base := max(0, a.requirement.Target()-ev.total)
	cum, err := estimated.CumulativeShares(base, quarters)
	if err != nil {
		return assessment{}, err
	}
	var events money.USCents
	for i := range cum {
		events += ev.part[i]
		cum[i] += events
	}
	a.installments = estimated.Increments(cum)
	applied := fmt.Sprintf("the installments due %s, %s, %s and %s are %v, %v, %v and %v: a quarter of %v each",
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long(), a.installments[0], a.installments[1], a.installments[2], a.installments[3], base)
	if ev.found {
		applied += ", with each unusual event's 90% share in the installment for its period"
	}
	a.notes.Rule("36 M.R.S. § 5228(4), (5); Form 1040ES-ME (2026); Form 2210ME (2025), line 7", applied)

	if f.Elections().Annualised() {
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
		a.notes.Rule("36 M.R.S. § 5228(5)(A); Annualized Income Installment Worksheet (2025); Form 2210ME (2025), line 17", fmt.Sprintf("under the annualized method the installments are %v, %v, %v and %v: 22.5%%, 45%%, 67.5%% and 90%% of the annualized tax, "+
			"less earlier installments, and never more than the regular installment plus any shortfall carried forward; the worksheet must be filed with Form 2210ME, line 17 checked, even when no penalty is due", ann[0], ann[1], ann[2], ann[3]))
	}

	// § 5228(9): a return filed with the tax paid in full by the last day of
	// January. January 31, 2027 is a Sunday, and § 153(2) moves it.
	if filed := f.ReturnFiled(); filedBy {
		a.excused = true
		a.notes.Rule("36 M.R.S. §§ 5228(9), 153(2); Form 2210ME (2025)", fmt.Sprintf("the return was filed with the tax paid in full on %s, by %s (%s moved past the weekend), so the fourth installment bears no penalty",
			filed.Long(), by.Long(), y.fileAndPayBy.Long()))
	}
	return a, nil
}

func (r maineEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r maineEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, quarters)
	if err != nil {
		return nil, working.Notes{}, err
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
	notes.Rule("Form 2210ME (2025), line 10a", fmt.Sprintf("the %v withheld is entered as 25%% on each due date, whenever it was actually withheld; "+
		"a payment after a due date is applied to the oldest shortfall first, as Form 2210 does, since the research for this package does not reproduce Form 2210ME's own lines for it", f.WithheldTotal()))
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("Form 2210ME (2025), line 10a; 36 M.R.S. § 5228(5)(A)", "Form 2210ME has no line for crediting withholding on the dates it was withheld, so it is spread evenly; see the divergence on when withholding counts as paid")
	}
	return ledger, notes, nil
}

func (r maineEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Maine liability to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := installments(a, ledger)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// installments are the due dates with what was needed and credited by each.
func installments(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	var needed money.USCents
	for i, l := range ledger {
		needed += a.installments[i]
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(needed).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if a.excused && i == 3 {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r maineEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("mainetaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Maine liability to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := y.calendar.NextBusinessDay(y.stop)
	if asOf.Before(stop) {
		stop = asOf
	}
	paidInFull := false
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop, paidInFull = filed, true
	}
	waiver, waiverAnswered := f.Positions().Waiver()
	if a.exempt {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, nil).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		return working.NewResult(p).With(a.notes).Build()
	}

	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule, err := installments(a, ledger)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	var unrated estimated.Date
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
			parts, gap, err := compoundMonthly(piece.Amount(), l.Due(), to, y.rates)
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
		notes.Rule("36 M.R.S. § 5228(3)", fmt.Sprintf("this year's Maine tax was not given, so the penalty is figured against the %d option alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower", y.year-1, p.Total()))
	}
	if paidInFull {
		notes.Rule("36 M.R.S. § 5228(1)(C); Form 2210ME (2025), lines 12 and 13", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("36 M.R.S. §§ 5228(1)(C), (5), 186; Form 2210ME (2025), Part II", fmt.Sprintf("each shortfall is charged %s for each month or part of a month from its due date until paid or %s, compounded monthly, "+
		"worked out exactly and rounded to the cent, since the research for this package does not reproduce the form's precomputed table; the penalty to %s is %v",
		rateText(y.rates), y.calendar.NextBusinessDay(y.stop).Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The Maine rate for the days after %s isn't published yet.", unrated.AddDays(-1).Long()))
	}
	if k, asked := f.Positions().Waiver(); asked && k != estimated.WaiverNone {
		notes.Rule("36 M.R.S. § 5228(5)", fmt.Sprintf("Maine has no %s waiver of its own; the State Tax Assessor may waive or abate the penalty \"for cause\", and the penalty shown is before any waiver", k))
	}
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Are you asking Maine to waive your penalty for cause?",
			"Maine's assessor may waive the penalty for cause. Maine counts a natural disaster as reasonable cause for other penalties.")
	}
	if o, ok := maineWaiver(f); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		if o.Undecided() {
			notes.Ask(working.PositionUnsettledRule, "", "Maine lists reasonable causes for other penalties, including a natural disaster. Its law doesn't say whether they count as cause to waive this one.")
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// maineWaiver decides a waiver asked for. 36 M.R.S. § 5228(5) lets the
// State Tax Assessor waive the penalty "for cause", without listing causes.
// § 187-B(7) counts a natural disaster as reasonable cause for the penalties
// it governs, and doesn't say it governs this one. Neither lists retirement
// or disability.
func maineWaiver(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	k, answered := f.Positions().Waiver()
	switch {
	case !answered || k == estimated.WaiverNone:
		return estimated.WaiverOutcome{}, false
	case k == estimated.WaiverCasualty:
		return estimated.WaiverUndecided("Maine's law doesn't say if a casualty or disaster is cause for this penalty. Maine's State Tax Assessor can waive the penalty for cause."), true
	default:
		return estimated.WaiverRefused("Maine has no waiver for retirement or disability as such, only one for cause. The State Tax Assessor decides it."), true
	}
}

func rateText(rates []monthlyRate) string {
	if len(rates) == 1 {
		return fmt.Sprintf("%v a month, the rate for %d,", rates[0].monthly, rates[0].from.Year())
	}
	return "the § 186 monthly rate in force"
}

// compoundMonthly charges principal for each month or part of a month after
// from up to and including to, compounded monthly, as § 186 and Form 2210ME
// Part II do. Month k runs from from.AddMonths(k-1) to from.AddMonths(k) and
// is charged at the rate in force on the day it begins. Months under one rate
// are one part.
//
// unrated is the first day of the first month that begins on a day no rate
// covers, or the zero Date. That month and those after it are not charged,
// and the caller must report them.
//
// This belongs in estimated beside [estimated.Monthly], which charges a
// simple rate by the month.
func compoundMonthly(principal money.USCents, from, to estimated.Date, rates []monthlyRate) (parts []estimated.PenaltyPart, unrated estimated.Date, err error) {
	months := estimated.MonthsOrPart(from, to)
	rateOn := func(d estimated.Date) (monthlyRate, bool) {
		for _, r := range rates {
			if !d.Before(r.from) && !d.After(r.to) {
				return r, true
			}
		}
		return monthlyRate{}, false
	}
	balance := new(big.Rat).SetInt64(int64(principal))
	k := 1
	for k <= months {
		begins := from.AddMonths(k - 1).AddDays(1)
		rate, ok := rateOn(begins)
		if !ok {
			return parts, begins, nil
		}
		start := from.AddMonths(k - 1)
		n := 0
		for k <= months {
			b := from.AddMonths(k - 1).AddDays(1)
			if b.Before(rate.from) || b.After(rate.to) {
				break
			}
			n++
			k++
		}
		end := from.AddMonths(k - 1)
		if end.After(to) {
			end = to
		}
		step := new(big.Rat).Add(big.NewRat(1, 1), big.NewRat(int64(rate.monthly), 10_000))
		factor := big.NewRat(1, 1)
		for i := 0; i < n; i++ {
			factor.Mul(factor, step)
		}
		grown := new(big.Rat).Mul(balance, factor)
		charge := roundCents(new(big.Rat).Sub(grown, balance))
		part, err := estimated.NewPenaltyPart(estimated.PartInterest, principal, rate.monthly*12).
			Span(start, end).Days(end.DaysSince(start)).Months(n).Charge(charge).Source(rate.source).Build()
		if err != nil {
			return nil, estimated.Date{}, err
		}
		parts = append(parts, part)
		balance.Add(balance, new(big.Rat).SetInt64(int64(charge)))
	}
	return parts, estimated.Date{}, nil
}

// roundCents rounds a non-negative amount of cents half up.
func roundCents(x *big.Rat) money.USCents {
	twice := new(big.Rat).Mul(x, big.NewRat(2, 1))
	q := new(big.Int).Quo(twice.Num(), twice.Denom()) // floor of 2x for x >= 0
	q.Add(q, big.NewInt(1))
	q.Quo(q, big.NewInt(2))
	return money.USCents(q.Int64())
}

func (r maineEstimated) Explanation() estimated.Explanation { return maineExplanation }

// maineExplanation is written by hand and checked against
// research/estimated-payments/me.md.
var maineExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Maine expects estimated payments when two figures are both $1,000 or more. They are your tax after withholding this year and last year's tax. The penalty is interest on each quarter's shortfall, counted in whole months.").
		Paragraph("You avoid the penalty if withholding and estimated payments reach a target. The target is the lesser of 90% of this year's Maine tax or 100% of last year's. Last year can set it only if it was a full 12-month year. There's no higher figure for high incomes. Maine asks for nothing if this year's tax after withholding is under $1,000. It also asks for nothing if last year's tax was under $1,000.").
		Paragraph("The exception is a sudden jump in income without Maine withholding. A rise of $500,000 or more on the same part of last year is an \"unusual event\". That quarter's payment must include 90% of the tax on the extra income. Last year's tax doesn't cover it, even when that tax was under $1,000.").
		Paragraph("A quarter is due April 15, June 15 and September 15, 2026, and January 15, 2027. A date on a weekend or Maine holiday is due the next business day. Withholding counts as paid evenly across the four dates. You can file and pay in full by the end of January. Then there's no penalty on the January payment. For 2026, the end of January is Monday, February 1, 2027.").
		Paragraph("The penalty is interest at 0.75% a month for 2026, compounded monthly. A part of a month counts as a whole one. A payment one day late costs a full month. It runs until the shortfall is paid or the return is due on April 15. The annualized worksheet can lower early payments for uneven income. The assessor may waive the penalty for cause.").
		VersusFederal("Federal needs no payments only if last year's tax was zero. Maine needs none if it was under $1,000. Maine has no 110% tier. A $500,000 income jump without withholding has 90% of its tax due that quarter. That holds whatever last year's tax was. The penalty is charged by the month, compounded monthly, at a fixed annual rate.").
		Source("36 M.R.S. §§ 153, 186, 5227, 5228").
		Source("Form 1040ES-ME, Estimated Tax for Individuals, 2026 (revised July 2026)").
		Source("Form 2210ME, Underpayment of Estimated Tax by Individuals, Estates, or Trusts, and its instructions, 2025").
		Source("Annualized Income Installment Worksheet for Underpayment of Estimated Tax, 2025").
		Source("4 M.R.S. § 1051, Legal holidays").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
