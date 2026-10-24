package indianataxcalc

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

// Indiana's estimated tax rules, IC 6-3-4-4.1 and IC 6-8.1-10-2.1(b), as the
// shared [estimated.EstimatedTax] interface.
//
// IC 6-3-4-4.1(a) has the payments made as 26 U.S.C. § 6654 provides, so the
// tests look federal: 90% of this year's tax or 100% of last year's, 110%
// above $150,000 of federal AGI, four quarters, nothing under $1,000. The
// charge does not. It is a flat 10% of each quarter's shortfall, so a payment
// one day late costs as much as one made months late, and making up a
// shortfall later does not reduce it. County income tax is collected with the
// state's and counts in every figure.
//
// The figure follows the Department of Revenue's forms: Form ES-40 for 2026,
// and Schedules IT-2210 and IT-2210A for 2025, the latest published. Where
// the forms and the Indiana Code differ the result's Divergences say so. The
// 2025 IT-2210 predates IC 6-3-4-4.1(a)(3), effective January 1, 2026, so
// that rule is computed from the law.

const jurisdiction = estimated.Indiana

// EstimatedFacts are the facts Indiana's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	ptet            optional
	priorAnnualised optional
	priorSeparate   answer
	twelveMonth     answer
}

type optional struct {
	v   money.USCents
	set bool
}

type answer struct {
	v, set bool
}

// PassThroughEntityTaxCredit is this year's credit for pass-through entity
// tax paid on the filer's behalf, Schedule 5 line 3. IT-2210 line 5 counts it
// with withholding, in four equal parts, and [estimated.Figures.Tax] is
// before it.
func (e EstimatedFacts) PassThroughEntityTaxCredit() (money.USCents, bool) {
	return e.ptet.v, e.ptet.set
}

// PriorYearAnnualisedTax is last year's tax worked out on a full-year basis,
// for a filer who was a part-year Indiana resident last year: the IT-2210
// line 7 figure that its part-year example reaches in four steps. When given
// it is used as last year's tax.
func (e EstimatedFacts) PriorYearAnnualisedTax() (money.USCents, bool) {
	return e.priorAnnualised.v, e.priorAnnualised.set
}

// PriorYearMarriedSeparately reports whether last year's return was married
// filing separately, and whether the caller said. IT-2210's CAUTION box tests
// the $75,000 step on last year's filing status.
func (e EstimatedFacts) PriorYearMarriedSeparately() (separate, answered bool) {
	return e.priorSeparate.v, e.priorSeparate.set
}

// TwelveMonthRule reports whether the caller wants the prior-year test
// refused for a return that covered fewer than twelve months, and whether the
// caller said. Indiana's forms state no such condition; 26 U.S.C. § 6654,
// which IC 6-3-4-4.1(a) applies, has one. Which reading holds is unsettled.
func (e EstimatedFacts) TwelveMonthRule() (applies, answered bool) {
	return e.twelveMonth.v, e.twelveMonth.set
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PassThroughEntityTaxCredit(a money.USCents) *EstimatedFactsBuilder {
	b.e.ptet = optional{a, true}
	return b
}

func (b *EstimatedFactsBuilder) PriorYearAnnualisedTax(a money.USCents) *EstimatedFactsBuilder {
	b.e.priorAnnualised = optional{a, true}
	return b
}

func (b *EstimatedFactsBuilder) PriorYearMarriedSeparately(separate bool) *EstimatedFactsBuilder {
	b.e.priorSeparate = answer{separate, true}
	return b
}

func (b *EstimatedFactsBuilder) TwelveMonthRule(applies bool) *EstimatedFactsBuilder {
	b.e.twelveMonth = answer{applies, true}
	return b
}

// Build refuses a negative credit or tax.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	e := b.e
	if e.ptet.v < 0 || e.priorAnnualised.v < 0 {
		return EstimatedFacts{}, errors.New("indianataxcalc: a credit or tax cannot be negative")
	}
	return e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is IT-2210 line 6: no penalty when tax less withholding and
	// the pass-through credit is under it.
	threshold money.USCents

	// thisYear is the IT-2210 line 4 share of this year's tax, and prior and
	// priorHigh the line 7 shares of last year's.
	thisYear, prior, priorHigh money.Rate

	// step and stepSeparate are the prior-year federal AGI above which the
	// line 7 share is priorHigh.
	step, stepSeparate money.USCents

	// rate is the flat penalty on each underpaid installment, IT-2210 line
	// 21.
	rate money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// fileAndPayBy is the IB3 date by which a return filed and paid in full
	// excuses the fourth installment, before moving for a weekend.
	fileAndPayBy estimated.Date

	// periodEnds end the installment periods of IT-2210 line 14 and the
	// IT-2210A columns, which are the same.
	periodEnds []estimated.Date

	// annualised are the IT-2210A line 11 percentages.
	annualised []money.Rate

	// zeroPriorTax is IC 6-3-4-4.1(a)(3), in force from 2026.
	zeroPriorTax bool
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are IT-2210 lines 14 and 15: the required annual payment and the
// withholding are each divided by four.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Indiana's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return indianaEstimated{y}, nil
}

type indianaEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool

	// covered is IT-2210 line 8: withholding and the pass-through credit
	// reach the required annual payment, so there is no penalty whenever
	// the withholding came.
	covered bool

	dues         []estimated.Date
	installments []money.USCents // each installment's own amount, before excusing
	excused      bool            // the fourth installment, for filing early
	ptet         money.USCents

	notes working.Notes
}

func (r indianaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("indianataxcalc: the facts carry %T, not indianataxcalc.EstimatedFacts", s)
	}
}

func (r indianaEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("indianataxcalc: these are Indiana's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("indianataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: a farmer or fisherman with two-thirds of gross income from farming or fishing has one due date and a March 1 filing option under IT-2210 Section A, which is not built",
			estimated.ErrUnsupported)
	}
	if tax, ok := f.ThisYear().Tax(); ok {
		if local, ok := f.ThisYear().LocalTax(); ok && local > tax {
			return fmt.Errorf("indianataxcalc: county tax of %v is more than the %v of state and county tax together", local, tax)
		}
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("indianataxcalc: IT-2210A column %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms and the Indiana Code part
// ways on a rule every Indiana computation uses. They are found in
// research/estimated-payments/in.md, items 1, 2 and 10 and the verification's
// conflicts. exactly and zeroPrior say whether each changes anything for the
// facts.
func (r indianaEstimated) divergences(exactly, zeroPrior bool) ([]working.Divergence, error) {
	t := r.y.threshold.Whole()
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence(fmt.Sprintf("Exactly %s due after withholding", t)).
			Form(fmt.Sprintf("Form ES-40 and Bulletin #3 want estimated tax if you expect over %s due. That leaves out exactly %s. Schedule IT-2210 line 6 stops the penalty only below %s.", t, t, t),
				"Form ES-40 (R26 / 9-25), Instructions; Information Bulletin #3 (March 2023), Penalties; Schedule IT-2210 (R27 / 9-25), line 6").
			Statute(fmt.Sprintf("Estimated payments are excused only for tax reasonably expected under %s. So exactly %s is covered.", t, t),
				"IC 6-3-4-4.1(b)").
			Uses(working.ReadingForm).Affects(exactly),
		working.NewDivergence("Last year's tax when no return was filed").
			Form("If you filed no return for the year before, you enter N/A on line 7. Only 90% of this year's tax is then open. The form was revised in September 2025, before the 2026 change.",
				"Schedule IT-2210 (R27 / 9-25), instructions for lines 7 and 8").
			Statute("From 2026, last year's tax counts as $0 if you filed no return for it. You must also have had no tax due for it. So the required annual payment is $0.",
				"IC 6-3-4-4.1(a)(3), added by P.L.205-2025, SEC.10, effective January 1, 2026").
			Uses(working.ReadingStatute).Affects(zeroPrior),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// near is amount in whole dollars, or "just over" or "just under" limit
// when rounding would show it as limit.
func near(amount, limit money.USCents) string {
	switch {
	case amount > limit && amount.Whole() == limit.Whole():
		return "just over " + limit.Whole()
	case amount < limit && amount.Whole() == limit.Whole():
		return "just under " + limit.Whole()
	}
	return amount.Whole()
}

func (r indianaEstimated) assess(f estimated.Facts) (assessment, error) {
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
	ptet, ptetKnown := extras.PassThroughEntityTaxCredit()
	a.ptet = ptet
	withheld := f.WithheldTotal()
	credited := withheld + ptet
	prior, hasPrior := f.PriorYear()
	var priorFigures estimated.Figures
	if hasPrior {
		priorFigures, _ = prior.Figures()
	}

	// IT-2210 line 4: 90% of this year's state and county tax after credits.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYear, y.thisYear.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYear, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		county := ""
		if local, ok := f.ThisYear().LocalTax(); ok {
			county = fmt.Sprintf(", %v of it county tax,", local)
		}
		a.notes.Rule("IC 6-3-4-4.1(a); Schedule IT-2210 (2025), Section C, lines 1 to 4", fmt.Sprintf("%v of this year's state and county tax after credits%s at %v is %v", tax, county, y.thisYear, thisYear.Amount()))
	}

	// IT-2210 line 7: last year's tax, 100% or 110%.
	zeroPrior := false
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.prior, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	priorAnnualised, annualisedKnown := extras.PriorYearAnnualisedTax()
	resident, residenceAnswered := prior.FullYearResident()
	twelve, twelveAnswered := extras.TwelveMonthRule()
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Indiana return", last))
	case !prior.Filed() && prior.NotRequiredToFile() && y.zeroPriorTax:
		zeroPrior = true
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.prior, 0)
		a.notes.Rule("IC 6-3-4-4.1(a)(3)", fmt.Sprintf("no %d return was filed and none was required, so the %d tax is treated as $0 and the required annual payment is $0", last, last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Indiana return", last))
		if y.zeroPriorTax {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you have to file a %d Indiana return?", last),
				fmt.Sprintf("If not, and no %d tax was due, last year's tax and your target count as $0.", last))
		}
	case prior.Months() != 12 && twelveAnswered && twelve:
		priorOption = unavailable(fmt.Sprintf("Your %d Indiana return covered %d months, not 12", last, prior.Months()))
	case residenceAnswered && !resident && !annualisedKnown:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Indiana tax on a full-year basis", last))
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What would your %d Indiana tax have been on a full-year basis?", last),
			fmt.Sprintf("Last year's test uses it if you were a %d part-year resident. A full-year nonresident uses the tax as filed instead.", last))
	default:
		base, _ := priorFigures.Tax()
		if annualisedKnown {
			base = priorAnnualised
			a.notes.Rule("Schedule IT-2210 (2025), line 7, part-year example", fmt.Sprintf("the %d tax annualised for a part-year resident is %v, and is used as last year's tax", last, base))
		} else if !residenceAnswered {
			a.notes.Ask(working.PositionResidency, fmt.Sprintf("Were you a full-year Indiana resident in %d?", last),
				fmt.Sprintf("A part-year resident's %d tax is annualised for last year's test.", last))
		}
		if prior.Months() != 12 && !twelveAnswered {
			a.notes.Ask(working.PositionUnsettledRule, "",
				fmt.Sprintf("Indiana's forms let your %d-month return set last year's test. The federal rule Indiana follows needs 12 months; the figures follow the forms.",
					prior.Months()))
		}
		rate, why := r.priorShare(f, extras, priorFigures, &a.notes)
		amount := rate.Of(base)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, rate, amount)
		a.notes.Rule("IC 6-3-4-4.1(a); Schedule IT-2210 (2025), line 7 and CAUTION box", fmt.Sprintf("%s, so the prior-year test is %v of the %v %d tax, or %v", why, rate, base, last, amount))
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
	if thisShare := y.thisYear; !taxKnown && !zeroPrior {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Indiana tax to be?", y.year),
			fmt.Sprintf("It sets the %v option and decides whether you're under the %s threshold.", thisShare, y.threshold.Whole()))
	}

	// IT-2210 line 14 divides by the installment periods of residence.
	share, periods, err := r.residenceShares(f)
	if err != nil {
		return assessment{}, err
	}
	spread := "Pay a quarter by each date."
	if periods != 4 {
		spread = fmt.Sprintf("Spread them over the %d quarters you lived in Indiana.", periods)
	}
	switch {
	case zeroPrior:
		a.exempt = true
		none, err := estimated.NewException("No Indiana return or tax due last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You filed no %d Indiana return and none was required. Indiana then counts your %d tax as $0. No estimated payments are needed for %d.", last, last, y.year)).Exempt(none)
	case taxKnown && tax-credited < y.threshold:
		a.exempt = true
		under, err := estimated.ExceptionBelow("Tax less withholding and credits", tax-credited, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		leaves := fmt.Sprintf("Withholding and pass-through credit cover %s, leaving %s. That is under %s.", credited.Whole(), near(tax-credited, y.threshold), y.threshold.Whole())
		if tax < credited {
			leaves = fmt.Sprintf("Withholding and pass-through credit of %s leave a %s refund.", credited.Whole(), (credited - tax).Whole())
		}
		rb.Required(false, fmt.Sprintf("Your Indiana state and county tax is %s. %s No estimated payments are needed, and no penalty can arise.",
			tax.Whole(), leaves)).Exempt(under)
		a.notes.Rule("IC 6-3-4-4.1(b); Schedule IT-2210 (2025), line 6", fmt.Sprintf("%v of tax less %v of withholding and pass-through credit is %v, under %v, so there is no penalty", tax, credited, tax-credited, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target needs your %d Indiana tax, because your %d return can't set one.", y.year, last)).NoTarget()
	case target == 0 && chosen == estimated.BasisPriorYearTax:
		a.covered = true
		rb.Required(false, fmt.Sprintf("Your %d Indiana tax was $0, so your target is $0. No estimated payments are needed.", last)).Choose(chosen)
	case !taxKnown && credited >= target:
		a.covered = true
		rb.Required(false, fmt.Sprintf("Your target is %v of your %d tax, %s. Your withholding and pass-through credit of %s already reach it. No estimated payments are needed, whatever your %d tax turns out to be.",
			priorOpt.Percent(), last, target.Whole(), credited.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. Withholding and pass-through credit cover %s of it. So you need %s of estimated payments. ",
			priorOpt.Percent(), last, target.Whole(), credited.Whole(), (target-credited).Whole())+
			spread+fmt.Sprintf(" That keeps you clear of a penalty whatever your %d tax turns out to be.", y.year)).Choose(chosen)
	case credited >= target:
		a.covered = true
		rb.Required(false, fmt.Sprintf("Your target is %s. Your Indiana withholding and pass-through credit of %s already reach it. No estimated payments are needed.", target.Whole(), credited.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Indiana state and county tax is %s. Withholding and pass-through credit cover %s, leaving %s. That is %s or more, so you need estimated payments. To reach your target of %s, they must come to %s. %s",
			tax.Whole(), credited.Whole(), near(tax-credited, y.threshold), y.threshold.Whole(), target.Whole(), (target-credited).Whole(), spread)).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// Under an exception last year's return can't change the target.
	if lastShare, thisShare := y.prior, y.thisYear; !hasPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Indiana return, and what was its tax?", last),
			fmt.Sprintf("With it, your target could be %v of last year's tax, not %v of this year's.", lastShare, thisShare))
	}
	if a.covered {
		a.notes.Rule("Schedule IT-2210 (2025), line 8", fmt.Sprintf("the required annual payment of %v is no more than the %v of withholding and pass-through credit, so there is no penalty", target, credited))
	}
	if !zeroPrior {
		if taxKnown {
			if _, ok := f.ThisYear().LocalTax(); !ok {
				a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Does your %d tax include your county income tax?", y.year),
					"Indiana's tests count county tax too, and the figures assume yours includes it.")
			}
		}
		if !ptetKnown {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Do you have a pass-through entity tax credit for %d?", y.year),
				"It counts with your withholding and can lower what's needed.")
		}
	}

	// IT-2210 line 14: the required annual payment divided by four, or by the
	// number of installment periods of residence for a part-year resident.
	cum := make([]money.USCents, len(share))
	var k int64
	for i, in := range share {
		if in {
			k++
		}
		cum[i] = money.USCents((int64(a.requirement.Target())*k*2 + periods) / (2 * periods))
	}
	a.installments = estimated.Increments(cum)
	if periods == 4 {
		a.notes.Rule("IC 6-3-4-4.1(a); Schedule IT-2210 (2025), line 14; Form ES-40 (2026)", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", a.requirement.Target(), a.installments[0],
			a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
	} else {
		a.notes.Rule("Schedule IT-2210 (2025), instructions for line 14", fmt.Sprintf("you were an Indiana resident in %d of the four installment periods, so %v is divided by %d: %v, %v, %v and %v are due on %s, %s, %s and %s",
			periods, a.requirement.Target(), periods, a.installments[0], a.installments[1], a.installments[2], a.installments[3],
			a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
	}

	if f.Elections().Annualised() && !a.exempt {
		ps := f.Periods()
		taxes := make([]money.USCents, len(ps))
		for i, p := range ps {
			taxes[i] = p.AnnualisedTax()
		}
		ann, err := estimated.Annualised(taxes, y.annualised, a.installments)
		if err != nil {
			return assessment{}, err
		}
		a.installments = ann
		a.notes.Rule("Schedule IT-2210A (2025), Section 3, lines 11 to 19", fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: %v, %v, %v and %v of the annualised tax, "+
			"less earlier installments, and never more than the regular installment plus any amount carried forward", ann[0], ann[1], ann[2], ann[3],
			y.annualised[0], y.annualised[1], y.annualised[2], y.annualised[3]))
	}

	// IB3: a return filed and paid in full by January 31 excuses the fourth
	// installment. January 31, 2027 is a Sunday, and IB3 moves a weekend date
	// to the next business day, as the 2025 IT-2210 prints Feb. 2, 2026.
	fileBy := y.calendar.NextBusinessDay(y.fileAndPayBy)
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(fileBy) && !a.exempt {
		a.excused = true
		a.notes.Rule("Information Bulletin #3 (March 2023), Avoiding Penalty for the Fourth Installment; Schedule IT-2210 (2025), Section B", fmt.Sprintf("the return was filed and paid on %s, by %s, so the fourth installment is excused", filed.Long(), fileBy.Long()))
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	exactly := taxKnown && !a.exempt && tax-credited == y.threshold
	divergences, err := r.divergences(exactly, zeroPrior)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

// priorShare is the IT-2210 line 7 percentage: 110% when last year's federal
// AGI was over the step, 100% otherwise, and 110% when it was not given. It
// says why in words for the rule it goes in.
func (r indianaEstimated) priorShare(f estimated.Facts, extras EstimatedFacts, pf estimated.Figures, notes *working.Notes) (money.Rate, string) {
	y := r.y
	last := y.year - 1
	agi, known := pf.FederalAGI()
	if !known {
		notes.Ask(working.PositionAdjustedGrossIncome, fmt.Sprintf("What was your %d federal AGI?", last),
			fmt.Sprintf("Above %s (%s married filing separately) last year's test is %v. It is %v otherwise, so until then the figures use %v.",
				y.step.Whole(), y.stepSeparate.Whole(), y.priorHigh, y.prior, y.priorHigh))
		return y.priorHigh, fmt.Sprintf("%d federal AGI was not given", last)
	}
	separate, answered := extras.PriorYearMarriedSeparately()
	if !answered {
		separate = f.FilingStatus() == filing.MarriedFilingSeparately
		if agi > y.stepSeparate && agi <= y.step {
			notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Was your %d return married filing separately?", last),
				fmt.Sprintf("If so, last year's test is %v above %s of federal AGI, not %s.", y.priorHigh, y.stepSeparate.Whole(), y.step.Whole()))
		}
	}
	step := y.step
	if separate {
		step = y.stepSeparate
	}
	if agi > step {
		return y.priorHigh, fmt.Sprintf("%d federal AGI of %v is more than %v", last, agi, step)
	}
	return y.prior, fmt.Sprintf("%d federal AGI of %v is not more than %v", last, agi, step)
}

// residenceShares reports which installment periods the filer lived in
// Indiana for, and how many. No residence given, or none in Indiana, is
// taken as all four: IT-2210 line 14 divides by fewer only for a part-year
// resident.
func (r indianaEstimated) residenceShares(f estimated.Facts) ([]bool, int64, error) {
	ends := r.y.periodEnds
	in := make([]bool, len(ends))
	given := false
	for _, p := range f.Residence() {
		if p.State() != string(jurisdiction) {
			continue
		}
		given = true
		start := estimated.MustDate(r.y.year, time.January, 1)
		for i, end := range ends {
			if !p.From().After(end) && !p.To().Before(start) {
				in[i] = true
			}
			start = end.AddDays(1)
		}
	}
	var n int64
	for i := range in {
		if !given {
			in[i] = true
		}
		if in[i] {
			n++
		}
	}
	if n == 0 {
		return nil, 0, errors.New("indianataxcalc: no installment period of Indiana residence")
	}
	return in, n, nil
}

// credits are withholding, the pass-through credit and payments, dated.
// Withholding is spread in four equal parts on the due dates unless the
// filer elects the dates it was withheld; the pass-through credit always is,
// as IT-2210 line 15 divides it by four.
func (r indianaEstimated) credits(f estimated.Facts, a assessment) ([]estimated.Payment, working.Notes, error) {
	var notes working.Notes
	var credits []estimated.Payment
	var err error
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
		notes.Rule("Information Bulletin #3 (March 2023), Treatment of Withholding Payments", fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		credits, err = estimated.Credits(f, a.dues, quarters)
		notes.Rule("Information Bulletin #3 (March 2023), Treatment of Withholding Payments; Schedule IT-2210 (2025), line 15",
			fmt.Sprintf("the %v withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld", f.WithheldTotal()))
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	if a.ptet > 0 {
		cum, err := estimated.CumulativeShares(a.ptet, quarters)
		if err != nil {
			return nil, working.Notes{}, err
		}
		for i, amt := range estimated.Increments(cum) {
			if amt == 0 {
				continue
			}
			p, err := estimated.NewPayment().On(a.dues[i]).Amount(amt).Build()
			if err != nil {
				return nil, working.Notes{}, err
			}
			credits = append(credits, p)
		}
		notes.Rule("Schedule IT-2210 (2025), lines 5 and 15", fmt.Sprintf("the %v pass-through entity tax credit is counted in four equal parts on the due dates", a.ptet))
	}
	sort.SliceStable(credits, func(i, j int) bool { return credits[i].On().Before(credits[j].On()) })
	return credits, notes, nil
}

// column is one IT-2210 Section E column.
type column struct {
	due        estimated.Date
	required   money.USCents // line 14
	neededBy   money.USCents
	creditedBy money.USCents // everything credited by the due date
	shortfall  money.USCents // line 19
}

// sectionE applies credits to the installments the way Schedule IT-2210
// Section E and Bulletin #3 do, for a penalty that is a flat share of each
// shortfall rather than interest on it.
//
// A credit counts in the column whose due date is the first on or after it:
// a payment after a due date is in the next period. An overpayment in a
// column first makes up earlier underpayments, which does not reduce the
// penalty on them, and only what is left carries to the next column. A column
// that is short carries nothing. Credits after the last due date count in no
// column.
//
// [estimated.Apply] is not used: it charges a payment made between two due
// dates against the older shortfall first and leaves the later installment
// short by as much, which is right for interest and charges a flat penalty
// twice on the same dollars. The return value late is the total of credits
// after the last due date.
func sectionE(dues []estimated.Date, required []money.USCents, credits []estimated.Payment) (cols []column, late money.USCents) {
	cols = make([]column, len(dues))
	var needed, creditedBy, carry, owed money.USCents
	next := 0
	for i, due := range dues {
		var paid money.USCents
		for next < len(credits) && !credits[next].On().After(due) {
			paid += credits[next].Amount()
			next++
		}
		creditedBy += paid
		needed += required[i]
		total := paid + carry
		c := column{due: due, required: required[i], neededBy: needed, creditedBy: creditedBy}
		if total >= required[i] {
			over := total - required[i]
			made := min(over, owed)
			owed -= made
			carry = over - made
		} else {
			c.shortfall = required[i] - total
			owed += c.shortfall
			carry = 0
		}
		cols[i] = c
	}
	for ; next < len(credits); next++ {
		late += credits[next].Amount()
	}
	return cols, late
}

func (r indianaEstimated) ledger(f estimated.Facts, a assessment) ([]column, working.Notes, error) {
	credits, notes, err := r.credits(f, a)
	if err != nil {
		return nil, working.Notes{}, err
	}
	required := append([]money.USCents(nil), a.installments...)
	if a.excused {
		required[3] = 0
	}
	cols, late := sectionE(a.dues, required, credits)
	notes.Rule("Information Bulletin #3 (March 2023), Underpayment; Schedule IT-2210 (2025), lines 16 to 19",
		"a payment after a due date counts in the next period, and an overpayment first makes up earlier underpayments, without reducing the penalty on them, before any of it carries forward")
	if late > 0 {
		notes.Rule("Information Bulletin #3 (March 2023), Methods for Payment of Estimated Tax", fmt.Sprintf("%v paid after %s is not a %d installment payment; the department returns it or credits it to the next year", late, a.dues[3].Long(), r.y.year))
	}
	if a.covered {
		for i := range cols {
			cols[i].shortfall = 0
		}
	}
	return cols, notes, nil
}

func (r indianaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r indianaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Indiana return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	cols, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := schedule(cols, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments the columns give. The fourth is excused for
// a return filed and paid in time.
func schedule(cols []column, a assessment) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(cols))
	var needed money.USCents
	for i, c := range cols {
		needed += a.installments[i]
		b := estimated.NewInstallment(i+1, c.due).NeededBy(needed).CreditedBy(c.creditedBy).Shortfall(c.shortfall)
		if a.excused && i == 3 {
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

// Penalty is 10% of the shortfall of each installment due on or before asOf.
// The charge arises on the due date and does not grow, so a return filed
// later does not stop it and there is no rate to run out.
func (r indianaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("indianataxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Indiana return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	waiver, waiverAnswered := f.Positions().Waiver()
	cols, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	sched, err := schedule(cols, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if a.exempt {
		p, err := estimated.NewPenalty(asOf).Waiver(waiver).Because(a.requirement, sched).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		return working.NewResult(p).With(a.notes).Build()
	}
	pb := estimated.NewPenalty(asOf).Waiver(waiver).Because(a.requirement, sched)
	for i, c := range cols {
		if c.shortfall == 0 || c.due.After(asOf) {
			continue
		}
		part, err := estimated.NewPenaltyPart(estimated.PartFlat, c.shortfall, y.rate).
			Charge(y.rate.Of(c.shortfall)).
			Source("IC 6-8.1-10-2.1(b); Schedule IT-2210 (2025), lines 20 and 21").Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		line, err := estimated.NewPenaltyLine(i+1, c.due, c.shortfall).Part(part).Build()
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
	pending := ""
	if asOf.Before(cols[len(cols)-1].due) {
		pending = "; installments due after that are not yet charged"
	}
	notes.Rule("IC 6-3-4-4.1(b); IC 6-8.1-10-2.1(b); Schedule IT-2210 (2025), lines 19 to 21", fmt.Sprintf("each installment's underpayment is charged a flat %v, however late it is paid; the penalty to %s is %v%s",
		y.rate, asOf.Long(), p.Total(), pending))
	if !a.taxKnown {
		notes.Rule("IC 6-3-4-4.1(a); Schedule IT-2210 (2025), line 8", fmt.Sprintf("this year's tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required annual payment is the lesser of that and 90%% of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, p.Total()))
	}
	switch {
	case p.Total() > 0 && !waiverAnswered:
		notes.Ask(working.PositionPenaltyWaiver, "Do you want a waiver for casualty, disaster, retirement after 62 or disability?",
			"Indiana hasn't said if the federal waivers or its reasonable-cause waiver apply. So the figures show the full penalty for an estimated tax shortfall.")
	case p.Total() > 0 && waiver != estimated.WaiverNone:
		// No Indiana document says whether IC 6-3-4-4.1(a) carries over the
		// waivers of 26 U.S.C. § 6654(e)(3), and IC 6-8.1-10-2.1(d) doesn't
		// name this penalty among those reasonable cause can waive.
		if p, err = p.ToBuilder().WaiverOutcome(estimated.WaiverUndecided("Indiana has no stated waiver for a casualty, disaster, retirement or disability.")).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Ask(working.PositionUnsettledRule, "",
			"The federal waivers cover a casualty, disaster, retirement or disability. Indiana hasn't said if they or its reasonable-cause waiver cover estimated tax.")
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r indianaEstimated) Explanation() estimated.Explanation { return indianaExplanation }

// indianaExplanation is written by hand and checked against
// research/estimated-payments/in.md.
var indianaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Indiana wants estimated tax if you expect $1,000 or more due after withholding. The $1,000 counts state and county income tax. Its penalty is a flat 10% of each quarter's shortfall, not interest.").
		Paragraph("You are clear of the penalty if you pay the smaller of two amounts. They are 90% of this year's state and county tax and 100% of last year's. Withholding, pass-through entity tax credits and estimated payments all count. Last year's figure becomes 110% if your federal AGI then was over $150,000. The limit is $75,000 if you were married filing separately. County income tax counts throughout. Indiana collects it on the same return and payment voucher.").
		Paragraph("The payments are a quarter each. They are due April 15, June 15 and September 15, 2026, and January 15, 2027. Withholding counts as paid evenly on the four dates. You can instead show the dates it was actually taken. Filing and paying in full by January 31 replaces the January payment. For 2026 the date is February 1, 2027, because January 31 is a Sunday. If your income came unevenly, use the annualised method on Schedule IT-2210A. It can lower the early payments.").
		Paragraph("The penalty is 10% of what each quarter was short. It does not grow with time. A payment one day late costs as much as one made months later. A late payment counts toward the next quarter. An overpayment first makes up earlier shortfalls, and the rest carries forward. Making up a shortfall does not take back the 10% already charged on it. A payment made after January 15, 2027 does not count toward 2026 at all. No Indiana document says whether the federal waivers apply. They cover a casualty, disaster, retirement or disability.").
		Paragraph("Subtract withholding and pass-through credits from this year's tax. There is no penalty if what is left is under $1,000. From 2026, last year's tax can count as zero. That needs no Indiana return filed last year and no Indiana tax for it. Then nothing is required.").
		VersusFederal("Indiana charges a flat 10% of each quarter's shortfall, not interest by the day. How late you pay doesn't matter, and catching up later doesn't reduce it. County income tax counts in the $1,000 threshold and both targets. Payments after January 15 don't count.").
		Source("IC 6-3-4-4.1; IC 6-8.1-10-2.1(b), (d)").
		Source("Form ES-40, Estimated Tax Payment Form, 2026 (R26 / 9-25)").
		Source("Schedule IT-2210, Underpayment of Estimated Tax by Individuals, and its instructions, 2025 (R27 / 9-25)").
		Source("Schedule IT-2210A, Annualized Income Schedule, 2025 (R25 / 9-25)").
		Source("Income Tax Information Bulletin #3, Payment of Indiana Estimated Tax by Individuals (March 2023)").
		Source("Indiana Department of Revenue, Legislative Synopsis 2026").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
