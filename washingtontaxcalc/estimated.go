package washingtontaxcalc

import (
	"errors"
	"fmt"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Washington's payment rules for its capital gains excise tax, RCW 82.87.110
// and 82.87.115 with the penalties and interest of RCW 82.32.090 and
// 82.32.050, as the shared [estimated.EstimatedTax] interface.
//
// Washington has no quarterly estimated tax. The whole capital gains tax is
// due in one payment on the federal return's due date, and a filing extension
// does not move it. So there is one installment, no prior-year safe harbor, no
// withholding and no annualised method. The penalty is not interest on a
// shortfall: it is a late payment penalty of 9%, 19% or 29% by lateness, or,
// for a filer with an extension who paid an estimate on time, 5% of the whole
// tax when the estimate was under 80% of it. Interest runs separately from
// the end of April.
//
// The figures follow the Department of Revenue where its guidance is current,
// and the statute where the guidance predates SSB 5314 (chapter 409, Laws of
// 2025). Each result's Divergences says where the two part ways.

// jurisdiction is Washington's code.
const jurisdiction = estimated.Washington

// EstimatedFacts are the facts Washington's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	extension         bool
	extensionAnswered bool
}

// Extension reports whether the filer had a federal filing extension and
// confirmed it with the department through My DOR by the due date
// (RCW 82.87.110(5); WAC 458-20-300(3)), and whether the caller said. It
// decides which penalty applies to tax paid after the due date.
func (e EstimatedFacts) Extension() (granted, answered bool) {
	return e.extension, e.extensionAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

// Extension answers the extension question. False is an answer.
func (b *EstimatedFactsBuilder) Extension(granted bool) *EstimatedFactsBuilder {
	b.e.extension, b.e.extensionAnswered = granted, true
	return b
}

// Build has nothing to refuse: the one fact is a yes or a no.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// due is the payment due date before any move for a weekend or holiday,
	// and prepayFrom the first day a payment can be made.
	due, prepayFrom estimated.Date

	// calendar is the legal holidays that move a due date.
	calendar estimated.Calendar

	// lateTotals are the RCW 82.32.090(1) totals: on the due date, after the
	// last day of the next month, and after the last day of the month after
	// that. lateMinimum is the least that penalty can be.
	lateTotals  []money.Rate
	lateMinimum money.USCents

	// An estimated payment under underpaidShare of the tax, and short by at
	// least underpaidFloor, owes underpaidRate of the whole tax:
	// RCW 82.32.090(10).
	underpaidShare money.Rate
	underpaidFloor money.USCents
	underpaidRate  money.Rate

	// interestFrom is the day after which interest runs, and interest the
	// yearly rates.
	interestFrom estimated.Date
	interest     estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Washington's capital gains tax payment rules for a tax
// year, or [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return washingtonEstimated{y}, nil
}

type washingtonEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	tax         money.USCents
	taxKnown    bool
	due         estimated.Date
	extension   bool
	paidByDue   money.USCents
	notes       working.Notes
}

// short writes d as "Oct 15" inside the tax year and "Apr 15, 2027" outside it.
func (r washingtonEstimated) short(d estimated.Date) string {
	if d.Year() == r.y.year {
		return fmt.Sprintf("%s %d", d.Month().String()[:3], d.Day())
	}
	return fmt.Sprintf("%s %d, %d", d.Month().String()[:3], d.Day(), d.Year())
}

func (r washingtonEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("washingtontaxcalc: the facts carry %T, not washingtontaxcalc.EstimatedFacts", s)
	}
}

func (r washingtonEstimated) check(f estimated.Facts) error {
	y := r.y
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("washingtontaxcalc: these are Washington's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != y.year:
		return fmt.Errorf("washingtontaxcalc: these are the %d rules, and the facts are for %d", y.year, f.TaxYear())
	case len(f.Withholding()) > 0:
		return errors.New("washingtontaxcalc: Washington's capital gains tax has no withholding; tax paid toward it is a payment")
	case f.Elections().Annualised():
		return errors.New("washingtontaxcalc: Washington's capital gains tax is one payment, so there is nothing to annualise")
	}
	for _, p := range f.Payments() {
		switch {
		case p.FromPriorYear():
			return fmt.Errorf("%w: applying a %d overpayment to the %d capital gains tax is not built; the research found no rule for it",
				estimated.ErrUnsupported, y.year-1, y.year)
		case p.On().Before(y.prepayFrom):
			return fmt.Errorf("washingtontaxcalc: a payment on %s is before %s, the first day RCW 82.87.115(1) allows one for %d",
				p.On().Long(), y.prepayFrom.Long(), y.year)
		}
	}
	return nil
}

// divergences are where the department's guidance and the statute part ways
// on a rule these computations use. Two of the department's documents
// predate SSB 5314, so for those the statute is followed. Each affects the
// household only when its facts reach the rule: an extension with a payment
// by the due date for the first, a payment under 80% of the tax with one for
// the second, and tax unpaid at the due date for the third.
func (r washingtonEstimated) divergences(a assessment) ([]working.Divergence, error) {
	short := a.taxKnown && a.paidByDue < a.tax
	estimate := a.taxKnown && a.extension && a.paidByDue > 0 && short
	low := estimate && int64(a.paidByDue)*10_000 < int64(r.y.underpaidShare)*int64(a.tax)
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Late payment penalty after a timely estimated payment").
			Form("The department charges no late payment penalty on a balance paid later. That needs an extension, and your own estimate paid by the original due date. Only interest runs on that balance.",
				"Department of Revenue, Capital gains tax page, Payments (retrieved 2026-10-20); Interim statement regarding late payment penalties (April 7, 2023)").
			Statute("After the due date, interest and the usual penalties apply to the balance. Read alone, that includes the 9%, 19% and 29% late payment penalty.",
				"RCW 82.87.110(3); RCW 82.32.090(1)").
			Uses(working.ReadingForm).Affects(estimate),
		working.NewDivergence("The penalty on a low estimated payment").
			Form("A 2023 department statement charges the assessment penalty for a low payment. Low means under 80% of the tax and at least $1,000 short. The penalty is 5% of the tax it finds due, rising to 15% and 25% if unpaid. Its web page gives the 80% test with no $1,000 floor. Both predate the 2025 act.",
				"Department of Revenue, Interim statement regarding late payment penalties (April 7, 2023); Capital gains tax page, Payments (retrieved 2026-10-20)").
			Statute("The penalty is 5% of the actual tax due. It applies if your estimated payment was under 80% of that tax. You must also have been at least $1,000 short. The 2025 act added this from July 27, 2025.",
				"RCW 82.32.090(10), added by SSB 5314, chapter 409, Laws of 2025, § 14, in effect from July 27, 2025").
			Uses(working.ReadingStatute).Affects(low),
		working.NewDivergence("When interest starts").
			Form("The department's rules start interest on Jan 31 of the next year. One of their examples charges interest on a payment two days late. Both rules were certified before the 2025 act took effect.",
				"WAC 458-20-228(7)(a) (certified 2/29/2024); WAC 458-20-300(4)(a), Example 3 (certified 8/23/2023)").
			Statute("Interest on unpaid capital gains tax runs from the last day of the next April.",
				"RCW 82.32.050(1)(c)(ii), added by SSB 5314, chapter 409, Laws of 2025, § 12, in effect from July 27, 2025").
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

func (r washingtonEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	a.due = y.calendar.NextBusinessDay(y.due)
	if a.due != y.due {
		a.notes.Rule("RCW 1.12.070(3); WAC 458-20-228(4)(a)", fmt.Sprintf("%s falls on a weekend or legal holiday, so the payment is due %s", y.due.Long(), a.due.Long()))
	}
	a.tax, a.taxKnown = f.ThisYear().Tax()
	var answered bool
	a.extension, answered = extras.Extension()
	a.paidByDue = paidBy(f, a.due)
	divergences, err := r.divergences(a)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)

	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(100), a.tax)
	if !a.taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, money.Percent(100), 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	priorYear, err := estimated.NewTargetOption(estimated.BasisPriorYearTax, 0, 0).Label("Last year's tax").
		Unavailable("Washington's capital gains tax has no prior-year safe harbor").Build()
	if err != nil {
		return assessment{}, err
	}

	rb := estimated.NewRequirement().Option(thisYear).Option(priorYear)
	switch {
	case !a.taxKnown:
		rb.Required(false, fmt.Sprintf("Washington has no prior-year safe harbor. Your target is your whole %d capital gains tax. It is due in one payment by %s.",
			y.year, r.short(a.due))).NoTarget()
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Washington capital gains tax to be?", y.year),
			fmt.Sprintf("Without it there's no target. All of it is due by %s, and no other figure stands in for it.", r.short(a.due)))
	case a.tax == 0:
		rb.Required(false, fmt.Sprintf("With no Washington capital gains tax for %d, nothing is due on %s.", y.year, r.short(a.due))).Choose(estimated.BasisThisYearTax)
	default:
		rb.Required(true, fmt.Sprintf("Washington has no quarterly estimated payments for its capital gains tax. Your whole %d tax of %s is due in one payment by %s. You can pay it from %s.",
			y.year, a.tax.Whole(), r.short(a.due), r.short(y.prepayFrom))).Choose(estimated.BasisThisYearTax)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}

	if a.taxKnown {
		a.notes.Rule("RCW 82.87.110(3); WAC 458-20-300(4)(a)", fmt.Sprintf("the whole %v is due on or before %s, the federal return's due date, whatever extension is granted", a.tax, a.due.Long()))
	}
	a.notes.Rule("RCW 82.87.115(1); Department of Revenue special notice, Prepayment of capital gains tax (August 3, 2026)",
		fmt.Sprintf("a payment for %d can be made from %s, six months before the due date", y.year, y.prepayFrom.Long()))
	if !answered && !(a.taxKnown && a.tax == 0) {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did you have a filing extension confirmed with the department by %s?", r.short(a.due)),
			"With one, and a payment by then, a later balance has no late payment penalty.")
	}
	return a, nil
}

func (r washingtonEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// paidBy is the total paid on or before d.
func paidBy(f estimated.Facts, d estimated.Date) money.USCents {
	var t money.USCents
	for _, p := range f.Payments() {
		if !p.On().After(d) {
			t += p.Amount()
		}
	}
	return t
}

// installment is the one payment: the whole tax, due on the due date.
func (a assessment) installment() (estimated.Installment, error) {
	return estimated.NewInstallment(1, a.due).NeededBy(a.tax).CreditedBy(a.paidByDue).Shortfall(max(0, a.tax-a.paidByDue)).Build()
}

func (r washingtonEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: Washington has no other figure to schedule against", estimated.ErrNeedsThisYearTax)
	}
	in, err := a.installment()
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	var notes working.Notes
	notes.Rule("RCW 82.87.110(3)", fmt.Sprintf("one payment of %v is due %s; %v was paid by then, so %v is short", a.tax, a.due.Long(), a.paidByDue, in.Shortfall()))
	return working.NewResult([]estimated.Installment{in}).With(a.notes).With(notes).Build()
}

// lastDayOfMonthAfter is the last day of the nth month after d's month.
func lastDayOfMonthAfter(d estimated.Date, n int) estimated.Date {
	return estimated.DateOf(time.Date(d.Year(), d.Month()+time.Month(n+1), 0, 0, 0, 0, 0, time.UTC))
}

// piece is part of the shortfall and the day it stopped being unpaid: the day
// it was paid, or the day the penalty is figured to.
type piece struct {
	amount money.USCents
	until  estimated.Date
}

// unpaidAfter is how much of the pieces was still unpaid at the end of d.
func unpaidAfter(pieces []piece, d estimated.Date) money.USCents {
	var t money.USCents
	for _, p := range pieces {
		if p.until.After(d) {
			t += p.amount
		}
	}
	return t
}

func (r washingtonEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("washingtontaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: Washington has no other figure to measure a late payment against", estimated.ErrNeedsThisYearTax)
	}
	y := r.y
	stop := asOf
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop = filed
	}
	waiver, waiverAnswered := f.Positions().Waiver()
	in, err := a.installment()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, []estimated.Installment{in})
	var notes working.Notes

	ledger, err := estimated.Apply([]estimated.Date{a.due}, []money.USCents{a.tax}, f.Payments())
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	l := ledger[0]
	if l.Shortfall() == 0 || !stop.After(a.due) {
		p, err := pb.Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		if l.Shortfall() == 0 {
			notes.Rule("RCW 82.87.110(3)", fmt.Sprintf("the whole %v was paid by %s, so no penalty or interest arises", a.tax, a.due.Long()))
		}
		return working.NewResult(p).With(a.notes).With(notes).Build()
	}

	var pieces []piece
	lastDay := a.due
	for _, pc := range l.Pieces() {
		until := stop
		if on, paid := pc.PaidOn(); paid && on.Before(stop) {
			until = on
		}
		pieces = append(pieces, piece{amount: pc.Amount(), until: until})
		if until.After(lastDay) {
			lastDay = until
		}
	}
	lb := estimated.NewPenaltyLine(1, a.due, l.Shortfall()).Days(lastDay.DaysSince(a.due))

	estimate := l.CreditedBy()
	if a.extension && estimate > 0 {
		// The department's reading: no late payment penalty, only the 5%
		// penalty on a low estimate. See the first divergence.
		notes.Rule("Department of Revenue, Capital gains tax page, Payments; RCW 82.32.090(12)",
			fmt.Sprintf("with an extension, the %v paid by %s is an estimated payment, so the %v paid later owes no late payment penalty", estimate, a.due.Long(), l.Shortfall()))
		low := int64(estimate)*10_000 < int64(y.underpaidShare)*int64(a.tax)
		if low && (l.Shortfall() >= y.underpaidFloor) != (estimate >= y.underpaidFloor) {
			outcome := "draws no penalty"
			if l.Shortfall() >= y.underpaidFloor {
				outcome = "draws the penalty"
			}
			notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("The 5%% test's \"at least %s\" could mean the shortfall or the payment. The figures read it as the shortfall, as the department does.",
				y.underpaidFloor.Whole()))
			notes.Rule("RCW 82.32.090(10)", fmt.Sprintf("the 5%% penalty applies to a payment under %v of the tax \"and at least %s\"; the figures read that as the shortfall, as the department does, and being %v short of %v %s",
				y.underpaidShare, y.underpaidFloor.Whole(), l.Shortfall(), a.tax, outcome))
		}
		if low && l.Shortfall() >= y.underpaidFloor {
			charge := y.underpaidRate.Of(a.tax)
			// PenaltyLine refuses a part whose principal is more than the
			// shortfall, so the part carries the shortfall although the
			// charge is on the whole tax.
			part, err := estimated.NewPenaltyPart(estimated.PartFlat, l.Shortfall(), y.underpaidRate).Charge(charge).
				Source(fmt.Sprintf("RCW 82.32.090(10): %v of the %v actual tax due, not of the shortfall", y.underpaidRate, a.tax)).Build()
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			lb.Part(part)
			notes.Rule("RCW 82.32.090(10)", fmt.Sprintf("the estimated payment of %v is under %v of the %v tax and %v short, so the penalty is %v of the whole tax, %v",
				estimate, y.underpaidShare, a.tax, l.Shortfall(), y.underpaidRate, charge))
		} else {
			notes.Rule("RCW 82.32.090(10)", fmt.Sprintf("the estimated payment of %v is at least %v of the %v tax, or short by under %v, so there is no penalty on it",
				estimate, y.underpaidShare, a.tax, y.underpaidFloor))
		}
	} else {
		var parts []estimated.PenaltyPart
		var total money.USCents
		var prior money.Rate
		partial := false
		for i, totalRate := range y.lateTotals {
			raw, step := a.due, a.due
			if i > 0 {
				raw = lastDayOfMonthAfter(a.due, i)
				step = y.calendar.NextBusinessDay(raw)
				for _, p := range pieces {
					if p.until.After(raw) && !p.until.After(step) {
						notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("The law doesn't say if the %v step moves when %s is a legal holiday. The figures move it to %s, as Washington's day-count rule does.",
							totalRate, r.short(raw), r.short(step)))
						notes.Rule("RCW 1.12.070(3); WAC 458-20-228(4)(a)", fmt.Sprintf("the step to %v is moved from %s to %s, as Washington's rule for counting days moves a deadline that falls on a legal holiday",
							totalRate, raw.Long(), step.Long()))
						break
					}
				}
			}
			unpaid := unpaidAfter(pieces, step)
			inc := totalRate - prior
			prior = totalRate
			if unpaid == 0 {
				continue
			}
			if i > 0 && unpaid < l.Shortfall() {
				partial = true
			}
			part, err := estimated.NewPenaltyPart(estimated.PartFlat, unpaid, inc).Span(a.due, step).Charge(inc.Of(unpaid)).
				Source(fmt.Sprintf("RCW 82.32.090(1): a total of %v of tax not received by %s", totalRate, step.Long())).Build()
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			parts = append(parts, part)
			total += part.Charge()
			notes.Rule("RCW 82.32.090(1)", fmt.Sprintf("%v was still unpaid after %s, so the late payment penalty reaches a total of %v; this step adds %v of it, %v",
				unpaid, step.Long(), totalRate, inc, part.Charge()))
		}
		if partial {
			notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("The law doesn't say how a part payment counts for steps %v and %v. Later steps are charged only on what was unpaid, not on the whole %s.",
				y.lateTotals[1], y.lateTotals[2], l.Shortfall().Whole()))
			notes.Rule("RCW 82.32.090(1)", fmt.Sprintf("each later step is charged only on what was still unpaid, not on the whole %s", l.Shortfall().Whole()))
		}
		if total > 0 && total < y.lateMinimum {
			first := parts[0]
			raised, err := estimated.NewPenaltyPart(estimated.PartFlat, first.Principal(), first.Rate()).Span(first.From(), first.To()).
				Charge(first.Charge() + y.lateMinimum - total).Source(first.Source() + fmt.Sprintf("; raised so the penalty is not less than %v", y.lateMinimum)).Build()
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			parts[0] = raised
			notes.Rule("RCW 82.32.090(1)", fmt.Sprintf("the late payment penalty of %v is raised to the %v minimum", total, y.lateMinimum))
		}
		for _, p := range parts {
			lb.Part(p)
		}
	}

	// Interest, on the tax only, for the days after the end of April.
	var unrated estimated.Date
	interestCharged := false
	for _, p := range pieces {
		if !p.until.After(y.interestFrom) {
			continue
		}
		parts, gap, err := estimated.SimpleDaily(p.amount, y.interestFrom, p.until, y.interest)
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
			unrated = gap
		}
		for _, part := range parts {
			lb.Part(part)
			interestCharged = true
		}
	}
	if interestCharged {
		notes.Rule("RCW 82.32.050(1)(c)(ii), (2)", fmt.Sprintf("interest runs on the unpaid tax from %s until it is paid or %s, at the department's yearly rate for each calendar year, charged by the day at the yearly rate divided by the days in the year", y.interestFrom.Long(), stop.Long()))
		notes.Ask(working.PositionUnsettledRule, "", "Washington doesn't say how days are counted for its yearly interest rate. The figures charge it daily, at the yearly rate divided by the days in the year.")
	} else {
		notes.Rule("RCW 82.32.050(1)(c)(ii)", fmt.Sprintf("interest runs only after %s, so none has accrued", y.interestFrom.Long()))
	}
	if !unrated.IsZero() {
		pb.RatedThrough(unrated.AddDays(-1))
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", r.short(unrated.AddDays(-1))))
	}

	line, err := lb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	p, err := pb.Line(line).Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if p.Total() > 0 {
		switch {
		case !waiverAnswered:
			notes.Ask(working.PositionPenaltyWaiver, "Will you ask the department to waive the penalty?",
				"It must waive it after five on-time years with no contact about the year. It must also waive it if things beyond your control caused it.")
		case waiver == estimated.WaiverCasualty:
			notes.Rule("RCW 82.32.105(1)", "a casualty or disaster is a circumstance beyond the filer's control, for which the department must waive the penalties; the penalty is shown before the waiver")
		case waiver == estimated.WaiverRetiredOrDisabled:
			notes.Rule("RCW 82.32.105; RCW 82.87.110(7)", "Washington has no waiver for retirement or disability as such; the penalty is shown before any waiver")
		}
		if o, ok := waiverOutcome(f, &notes); ok {
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.After(a.due) && !a.extension {
		notes.Rule("RCW 82.87.110(6)", "a return filed late also owes 5% of the tax for each month or part of one, up to 25%; that penalty is on the return, not the payment, and is not figured here")
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// waiverOutcome decides the waiver asked for under RCW 82.32.105(1) and WAC
// 458-20-228(9)(a): the department must waive the penalties when
// circumstances beyond the filer's control caused the late payment. A
// casualty or a serious illness is such a circumstance; retirement is not.
// It reports false when no waiver was asked for.
func waiverOutcome(f estimated.Facts, notes *working.Notes) (estimated.WaiverOutcome, bool) {
	p := f.Positions()
	kind, answered := p.Waiver()
	if !answered || kind == estimated.WaiverNone {
		return estimated.WaiverOutcome{}, false
	}
	if kind == estimated.WaiverCasualty {
		return estimated.WaiverEligibleExcept("A casualty or disaster beyond your control waives the penalty, but not interest. The department must waive it if it finds one caused the late payment.", estimated.PartInterest), true
	}
	switch p.WaiverCause() {
	case estimated.CauseRetirement:
		return estimated.WaiverRefused("Washington has no waiver for retirement, only for causes beyond your control. The department decides if one, such as a serious illness, made you pay late."), true
	case estimated.CauseDisability:
		notes.Ask(working.PositionUnsettledRule, "", "Washington's rules count a sudden serious illness as beyond your control. They don't say whether a disability does.")
		return estimated.WaiverUndecided("There is no waiver for disability as such, only for causes beyond your control. The department decides if one, such as a serious illness, made you pay late."), true
	default:
		notes.Ask(working.PositionUnsettledRule, "", "Washington's rules count a sudden serious illness as beyond your control. They don't say whether a disability does.")
		return estimated.WaiverUndecided("Only causes beyond your control qualify, not retirement or disability as such. The department decides if one, such as a serious illness, made you pay late."), true
	}
}

func (r washingtonEstimated) Explanation() estimated.Explanation { return washingtonExplanation }

// washingtonExplanation is written by hand and checked against
// research/estimated-payments/wa.md and the documents in testdata/.
var washingtonExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Washington has no income tax for 2026 and no quarterly estimated payments. Its capital gains tax is one payment, due April 15, 2027 for the 2026 tax year.").
		Paragraph("The capital gains tax is due on the day your federal return is due. A filing extension, federal or Washington, does not move that date. From the 2026 tax year you can pay early, from October 15, 2026, through My DOR. You can't prepay through tax preparation software.").
		Paragraph("For a late payment, the department adds a penalty of 9% of the unpaid tax. It rises to 19% if still unpaid at the end of May, and 29% at the end of June. The penalty is at least $5. Separately, interest runs on unpaid tax from April 30, 2027. It runs at the department's yearly rate, which is 6% for 2027.").
		Paragraph("If you have an extension, pay what you expect to be due by April 15. The department then charges no late payment penalty on a balance you pay later. Interest still runs on that balance. The penalty is instead 5% of the whole tax if you paid under 80% of it. That applies only if you were also at least $1,000 short.").
		Paragraph("You can ask the department to waive the late payment penalty. It must do so if you paid this tax on time in each of the previous five years. The department also must not have contacted you about the year. It also waives penalties caused by circumstances beyond your control. A late return has its own penalty of 5% of the tax a month, up to 25%.").
		VersusFederal("Washington differs from federal in kind. It has no quarterly payments or annualised method. It has no withholding and no safe harbor from last year's tax. The penalty is a share of the late tax, rising with lateness. Federal instead charges interest on each quarter's shortfall. Interest is charged separately from the end of April. The 80% test applies only with an extension.").
		Source("RCW 82.87.110 and 82.87.115; RCW 82.32.050, 82.32.090 and 82.32.105").
		Source("SSB 5314, chapter 409, Laws of 2025").
		Source("WAC 458-20-300 and WAC 458-20-228").
		Source("Department of Revenue, special notice, Prepayment of capital gains tax (August 3, 2026)").
		Source("Department of Revenue, Capital gains tax page and Interest rate tables (retrieved 2026-10-20)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
