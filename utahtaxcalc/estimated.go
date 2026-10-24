package utahtaxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Utah's prepayment rules, Utah Code §§ 59-10-516 and 59-1-401(5), as the
// shared [estimated.EstimatedTax] interface.
//
// Utah has no estimated tax for individuals. Nothing is due during the year,
// and a return filed and paid by its due date owes no prepayment penalty
// however little was paid before. The one rule is the extension test: a filer
// who uses the automatic six-month extension must have paid, by the original
// due date, the lesser of 90% of this year's tax or 100% of last year's. A
// filer who did not is charged 2% a month of the tax unpaid on that date,
// for the months of the extension.
//
// So Requirement is that one test, Schedule is that one date, and Penalty is
// the extension penalty. Interest and the late-payment penalty on a balance
// paid after the due date are a different charge and are not computed here.
//
// The figure follows the Tax Commission's current documents: the TC-546
// worksheet (Rev. 10/26) for the test, and Publication 58 (Rev. 11/25) for
// the arithmetic. The result's Divergences say where the Utah Code reads
// otherwise. No act changed § 59-10-516 after 2021, and the 2026 amendment to
// § 59-1-401 left subsection (5) as it was (research/estimated-payments/ut.md),
// so no document here predates the law it describes.

// jurisdiction is Utah's postal code.
const jurisdiction = estimated.Utah

// EstimatedFacts are the facts Utah's rules read that no other jurisdiction
// does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	extension         bool
	extensionAnswered bool
}

// FilesOnExtension reports whether the filer will file after the original
// due date under the automatic extension, and whether the caller said. The
// prepayment test applies only then.
func (e EstimatedFacts) FilesOnExtension() (extends, answered bool) {
	return e.extension, e.extensionAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

// FilesOnExtension answers whether the filer will use the extension. False is
// an answer: the return will be filed and paid by the original due date.
func (b *EstimatedFactsBuilder) FilesOnExtension(extends bool) *EstimatedFactsBuilder {
	b.e.extension, b.e.extensionAnswered = extends, true
	return b
}

func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// thisYear and priorYear are § 59-10-516(2)(b): the share of this year's
	// tax and of last year's liability that must be paid by the due date.
	thisYear, priorYear money.Rate

	// due is the original due date of the return, and extensionEnds the last
	// day of the automatic extension.
	due, extensionEnds estimated.Date

	calendar estimated.Calendar

	// penalty is Publication 58's annual rate, 2% a month, charged by the
	// day; penaltyCap is the most it can come to, six months of 2%.
	penalty, penaltyCap money.Rate
	daysInYear          int
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Utah's prepayment rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return utahEstimated{y}, nil
}

type utahEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement

	// exempt means the test cannot apply: the return was or will be filed
	// and paid by the due date.
	exempt bool

	taxKnown bool
	tax      money.USCents
	due      estimated.Date
	ends     estimated.Date

	// credited is withholding and payments made by the due date.
	credited money.USCents

	notes working.Notes
}

func (r utahEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("utahtaxcalc: the facts carry %T, not utahtaxcalc.EstimatedFacts", s)
	}
}

func (r utahEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("utahtaxcalc: these are Utah's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("utahtaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	}
	return nil
}

// divergences are where the Tax Commission's documents and the Utah Code part
// ways on a rule these computations use. They are found in
// research/estimated-payments/ut.md, Conflicts resolved, and checked there
// against both. zeroLast, noReturn and short say which facts each one turns
// on.
func (r utahEstimated) divergences(zeroLast, noReturn, short bool) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Last year's tax was zero").
			Form("TC-546 and Publication 58 use last year's tax, so with none the minimum is zero. The 2025 TC-40 instructions ask for 90% of this year's tax instead.",
				"TC-40 Instructions (2025), Prepayment Requirements for Filing Extension; TC-546 (Rev. 10/26), Payment Worksheet lines 4 and 5; Publication 58 (Rev. 11/25), Insufficient Prepayment").
			Statute("The minimum is the lesser of 90% of this year's tax or 100% of last year's. There is no exception when last year's tax was zero.",
				"Utah Code § 59-10-516(2)(b)").
			Uses(working.ReadingForm).Affects(zeroLast),
		working.NewDivergence("No return last year").
			Form("Publication 58 and the 2025 TC-40 instructions ask for 90% of this year's tax. That applies if you didn't file last year or are filing for the first time.",
				"Publication 58 (Rev. 11/25), Insufficient Prepayment; TC-40 Instructions (2025), Prepayment Requirements for Filing Extension").
			Statute("The minimum is the lesser of 90% of this year's tax or 100% of last year's. It applies whether or not you filed last year.",
				"Utah Code § 59-10-516(2)(b)").
			Uses(working.ReadingForm).Affects(noReturn),
		working.NewDivergence("By the day or by the month").
			Form("Publication 58 charges 24% a year by the day, up to 12%. It runs from the due date until you file or the extension ends.",
				"Publication 58 (Rev. 11/25), Insufficient Prepayment (Extension Penalty)").
			Statute("The penalty is 2% of the unpaid tax for each month of the extension. The law doesn't say how part of a month counts.",
				"Utah Code § 59-1-401(5)(b); TC-546 (Rev. 10/26), Penalties").
			Uses(working.ReadingForm).Affects(short),
		working.NewDivergence("What the penalty is charged on").
			Form("Publication 58 charges the \"underpayment\". That could mean only the amount below the minimum. TC-546 charges the whole tax unpaid on the due date.",
				"Publication 58 (Rev. 11/25), Insufficient Prepayment (Extension Penalty); TC-546 (Rev. 10/26), Penalties").
			Statute("The penalty is 2% of the return's tax still unpaid on its due date.",
				"Utah Code §§ 59-1-401(5)(b), 59-10-516(3)").
			Uses(working.ReadingStatute).Affects(short),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// date is d as a person reads it: "Dec 15" in the tax year, "Apr 15, 2027"
// outside it.
func (r utahEstimated) date(d estimated.Date) string {
	if d.Year() == r.y.year {
		return d.Time().Format("Jan 2")
	}
	return d.Time().Format("Jan 2, 2006")
}

func (r utahEstimated) assess(f estimated.Facts) (assessment, error) {
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
	a.due = y.calendar.NextBusinessDay(y.due)
	a.ends = y.calendar.NextBusinessDay(y.extensionEnds)

	tax, taxKnown := f.ThisYear().Tax()
	a.tax, a.taxKnown = tax, taxKnown
	withheld := f.WithheldTotal()
	a.credited = withheld
	for _, p := range f.Payments() {
		if p.FromPriorYear() || !p.On().After(a.due) {
			a.credited += p.Amount()
		}
	}

	// 90% of this year's tax.
	thisOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYear, y.thisYear.Of(tax))
	if !taxKnown {
		thisOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYear, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("Utah Code § 59-10-516(2)(b)(i); TC-546 (Rev. 10/26), Payment Worksheet lines 1 to 3",
			fmt.Sprintf("%v of this year's Utah tax of %v is %v", y.thisYear, tax, thisYear.Amount()))
	}

	// 100% of last year's liability. TC-546 and Publication 58 take it as
	// filed, zero included; Publication 58 refuses it when no return was
	// filed.
	prior, hasPrior := f.PriorYear()
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYear, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Utah return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Utah return", last))
		a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("With no %d return, Publication 58 and the Utah Code differ on last year's tax. The figures use Publication 58's %v of this year's tax.", last, y.thisYear))
	default:
		pf, _ := prior.Figures()
		priorTax, _ := pf.Tax()
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYear, y.priorYear.Of(priorTax))
		a.notes.Rule("Utah Code § 59-10-516(2)(b)(ii); TC-546 (Rev. 10/26), Payment Worksheet line 4",
			fmt.Sprintf("%v of the %d Utah tax liability of %v is %v", y.priorYear, last, priorTax, y.priorYear.Of(priorTax)))
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
	if taxKnown || priorOpt.Available() {
		a.notes.Rule("Utah Code § 59-10-516(2)(b); TC-546 (Rev. 10/26), Payment Worksheet line 5",
			fmt.Sprintf("the minimum due by %s is the lesser of the options, %v", a.due.Long(), target))
	}

	extends, answered := extras.FilesOnExtension()
	filed := f.ReturnFiled()
	filedByDue := !filed.IsZero() && !filed.After(a.due)
	plain := "Utah does not require estimated payments."
	// reach says the minimum meets the test whatever this year's tax is, when
	// it is last year's tax alone.
	reach := ""
	if !taxKnown {
		reach = fmt.Sprintf(" That is 100%% of your %d tax, which is enough whatever your %d tax is.", last, y.year)
	}
	switch {
	case filedByDue:
		a.exempt = true
		ex, err := estimated.NewException("Return filed and paid by the due date").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, plain+fmt.Sprintf(" You filed and paid your %d return on %s. That is by the %s due date. So the extension prepayment test doesn't apply. There's no prepayment penalty.",
			y.year, r.date(filed), r.date(a.due))).Exempt(ex)
		a.notes.Rule("Utah Code §§ 59-10-516(2), 59-1-401(5)(a); Publication 58 (Rev. 11/25)", fmt.Sprintf("the return was filed and paid on %s, so no extension was used", filed.Long()))
	case answered && !extends:
		a.exempt = true
		ex, err := estimated.NewException("Filing and paying by the due date").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, plain+fmt.Sprintf(" You plan to file and pay your %d return by %s. The extension prepayment test then doesn't apply. There's no prepayment penalty, however little you paid during the year.",
			y.year, r.date(a.due))).Exempt(ex)
		a.notes.Rule("Utah Code §§ 59-10-516(2), 59-1-401(5)(a)", fmt.Sprintf("the return will be filed by %s, so the extension prepayment test does not apply", a.due.Long()))
	case !taxKnown && !priorOpt.Available():
		measure := fmt.Sprintf("the lesser of 90%% of your %d Utah tax or 100%% of your %d tax", y.year, last)
		if hasPrior && !prior.Filed() {
			measure = fmt.Sprintf("90%% of your %d Utah tax, since you didn't file for %d", y.year, last)
		}
		rb.Required(false, plain+fmt.Sprintf(" To file on the automatic extension, you must pay a minimum by %s. It is %s.",
			r.date(a.due), measure)).NoTarget()
	case a.credited >= target:
		rb.Required(false, plain+fmt.Sprintf(" To extend your %d return, have %s withheld or prepaid by %s.", y.year, target.Whole(), r.date(a.due))+
			reach+fmt.Sprintf(" Your withholding and prepayments already come to %s. Nothing more is needed.", a.credited.Whole())).Choose(chosen)
	default:
		rb.Required(true, plain+fmt.Sprintf(" To avoid the extension penalty, pay or withhold %s by %s.", target.Whole(), r.date(a.due))+
			reach+fmt.Sprintf(" You have %s so far. Prepay %s more by then, or file and pay by then.", a.credited.Whole(), (target-a.credited).Whole())).Choose(chosen)
	}
	// Filing by the due date sets the minimum at nothing, so neither figure
	// can change it.
	if !hasPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Utah return, and what was its tax?", last),
			fmt.Sprintf("With it, %v of last year's tax could set a lower extension minimum.", y.priorYear))
	}
	if thisShare := y.thisYear; !taxKnown && !a.exempt {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Utah tax to be?", y.year),
			fmt.Sprintf("It may set a %v extension minimum, and the extension penalty is charged on it.", thisShare))
	}
	if !answered && !filedByDue {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Will you file your %d return on the automatic extension, after %s?", y.year, r.date(a.due)),
			"Until you answer, the minimum assumes you will, the only case the test covers.")
	}
	priorTax := money.USCents(-1)
	if hasPrior && prior.Filed() {
		pf, _ := prior.Figures()
		if t, ok := pf.Tax(); ok {
			priorTax = t
		}
	}
	short := !a.exempt && a.credited < target
	divergences, err := r.divergences(!a.exempt && priorTax == 0, !a.exempt && hasPrior && !prior.Filed(), short)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	return a, nil
}

func (r utahEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// Schedule is the one date: the original due date of the return.
func (r utahEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Utah return to measure against, there is no minimum to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	in, err := r.installment(a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	var notes working.Notes
	notes.Rule("TC-40 Instructions (2025), When to File and Pay and Prepayment Requirements for Filing Extension; TC-546 (Rev. 10/26)",
		fmt.Sprintf("Utah has no installments; the whole minimum is due on the original due date, %s, and withholding of %v counts toward it whenever it was withheld", a.due.Long(), f.WithheldTotal()))
	return working.NewResult([]estimated.Installment{in}).With(a.notes).With(notes).Build()
}

// installment is the one date, with what is needed and credited by it.
func (r utahEstimated) installment(a assessment) (estimated.Installment, error) {
	needed, short := a.requirement.Target(), money.USCents(0)
	switch {
	case a.exempt:
		needed = 0
	case a.credited < needed:
		short = needed - a.credited
	}
	return estimated.NewInstallment(1, a.due).NeededBy(needed).CreditedBy(a.credited).Shortfall(short).Build()
}

// Penalty is the extension penalty: 2% a month, by the day, of the tax unpaid
// on the due date, from then until the return is filed or the extension
// ends.
func (r utahEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("utahtaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Utah return to measure against, there is no minimum to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	filed := f.ReturnFiled()
	if !filed.IsZero() && filed.After(a.ends) {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: a return filed on %s is after the extension ended on %s, so the late-filing and late-payment penalties of Utah Code § 59-1-401(6) apply instead, and they are not built",
			estimated.ErrUnsupported, filed.Long(), a.ends.Long())
	}
	stop := asOf
	if a.ends.Before(stop) {
		stop = a.ends
	}
	if !filed.IsZero() && filed.Before(stop) {
		stop = filed
	}
	waiver, waiverAnswered := f.Positions().Waiver()
	in, err := r.installment(a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule := []estimated.Installment{in}
	var notes working.Notes
	empty := func() (working.Result[estimated.Penalty], error) {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		return working.NewResult(p).With(a.notes).With(notes).Build()
	}
	if a.exempt {
		return empty()
	}
	if a.credited >= a.requirement.Target() {
		notes.Rule("Utah Code § 59-10-516(2)(b); TC-546 (Rev. 10/26)", fmt.Sprintf("%v was paid by %s, which meets the %v minimum, so there is no extension penalty", a.credited, a.due.Long(), a.requirement.Target()))
		return empty()
	}
	if !a.taxKnown {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: the %v paid by %s is under the minimum, and the extension penalty is charged on the whole tax then unpaid, which needs this year's tax",
			estimated.ErrNeedsThisYearTax, a.credited, a.due.Long())
	}
	unpaid := a.tax - a.credited
	if !stop.After(a.due) {
		notes.Rule("Utah Code § 59-1-401(5)(b); Publication 58 (Rev. 11/25)", fmt.Sprintf("the penalty is figured to %s, which is not after the %s due date, so nothing has accrued", stop.Long(), a.due.Long()))
		return empty()
	}
	days := stop.DaysSince(a.due)
	charge := dailyCharge(unpaid, y.penalty, days, y.daysInYear)
	capped := y.penaltyCap.Of(unpaid)
	source := "Publication 58 (Rev. 11/25), Insufficient Prepayment (Extension Penalty): 2 percent per month, annual 24 percent, at most 12 percent"
	if charge > capped {
		charge = capped
		notes.Rule("Publication 58 (Rev. 11/25), Insufficient Prepayment (Extension Penalty)", fmt.Sprintf("%d days at %v a year would be more than six months of 2%%, so the penalty is capped at %v of %v, %v", days, y.penalty, y.penaltyCap, unpaid, capped))
	}
	part, err := estimated.NewPenaltyPart(estimated.PartInterest, unpaid, y.penalty).Span(a.due, stop).Days(days).Charge(charge).Source(source).Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	line, err := estimated.NewPenaltyLine(1, a.due, unpaid).Days(days).Part(part).Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	p, err := estimated.NewPenalty(stop).Waiver(waiver).Line(line).Because(a.requirement, schedule).Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	notes.Rule("Utah Code §§ 59-10-516(3), 59-1-401(5)(b); Publication 58 (Rev. 11/25)", fmt.Sprintf("%v paid by %s misses the %v minimum, so the whole %v of tax then unpaid is charged %v a year for the %d days to %s, %v; "+
		"a payment after the due date does not shorten it, because the penalty runs until the return is filed or the extension ends",
		a.credited, a.due.Long(), a.requirement.Target(), unpaid, y.penalty, days, stop.Long(), p.Total()))
	if filed.IsZero() && asOf.After(a.ends) {
		notes.Rule("Utah Code § 59-1-401(6); Publication 58 (Rev. 11/25)", fmt.Sprintf("the penalty is figured to %s, the end of the extension; a return not filed by then loses the extension, "+
			"and the late-filing and late-payment penalties replace this one", a.ends.Long()))
	}
	if !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Will you ask the Tax Commission for a waiver or reduction for reasonable cause?", "The penalty shown is before any waiver.")
	}
	if waiverAnswered && waiver != estimated.WaiverNone && p.Total() > 0 {
		// § 59-1-401(14) and Publication 58 give one waiver, for reasonable
		// cause, and name no casualty, retirement or disability waiver.
		what := "a casualty or disaster"
		if waiver == estimated.WaiverRetiredOrDisabled {
			what = "retirement or disability"
		}
		o := estimated.WaiverUndecided(fmt.Sprintf("Utah has no waiver for %s as such. The Tax Commission can waive or reduce the penalty for reasonable cause shown.", what))
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("Utah Code § 59-1-401(14); Publication 58 (Rev. 11/25), Waivers", "the only waiver is the Tax Commission's for reasonable cause shown")
		notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("The extension penalty can be waived for reasonable cause. Utah's law and Publication 58 don't say if %s counts.", what))
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// dailyCharge is principal × rate × days ÷ days in the year, rounded half up
// to the cent: Publication 58's formula.
func dailyCharge(principal money.USCents, rate money.Rate, days, year int) money.USCents {
	n := int64(principal) * int64(rate) * int64(days)
	d := int64(10_000 * year)
	return money.USCents((n + d/2) / d)
}

func (r utahEstimated) Explanation() estimated.Explanation { return utahExplanation }

// utahExplanation is written by hand and checked against
// research/estimated-payments/ut.md.
var utahExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Utah has no estimated tax, and nothing is due during the year. The only prepayment rule is for a return filed late on an extension. Utah's extension is automatic and lasts six months.").
		Paragraph("A 2026 Utah return filed and paid by April 15, 2027 has no prepayment penalty. That holds however little was withheld or paid during the year. You can still prepay at any time if you want to.").
		Paragraph("Utah gives six more months to file without asking, to October 15, 2027. But by April 15 your withholding and prepayments must reach a minimum. It is the lesser of 90% of your 2026 Utah tax or 100% of your 2025 Utah tax. Withholding counts in full whenever it was taken. If you filed for 2025 with no tax due, the minimum is nothing. If you didn't file at all, the Tax Commission asks for 90% of this year's tax.").
		Paragraph("Miss the minimum and the penalty is steep. It is 2% a month of all the tax still unpaid on April 15. It is worked out by the day until you file, for up to six months, or 12%. It is charged on the whole unpaid balance, not just the shortfall. The late-payment penalty and interest on unpaid tax are added from April 15. If you do not file by October 15, the extension is lost. The late-filing and late-payment penalties then apply instead. The Tax Commission can waive a penalty for reasonable cause.").
		VersusFederal("Utah differs from federal in kind. There are no quarterly payments and no quarterly penalty. A return filed by April 15 has no prepayment penalty at all. The one test is on April 15, for an extended return. It has no 110% tier for high income. The penalty is 2% a month of everything unpaid, not interest on each shortfall. So a small shortfall can bring a large penalty.").
		Source("Utah Code §§ 59-10-514, 59-10-516, 59-1-401").
		Source("TC-40, Utah Individual Income Tax Instructions, 2025").
		Source("TC-546, Individual Income Tax Prepayment Coupon (Rev. 10/26)").
		Source("Publication 58, Utah Interest and Penalties (Rev. 11/25)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
