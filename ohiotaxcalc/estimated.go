package ohiotaxcalc

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Ohio's estimated income tax rules, Ohio Revised Code 5747.09, as the shared
// [estimated.EstimatedTax] interface.
//
// Ohio's rules look like the federal ones in outline: 90% of this year's tax
// or 100% of last year's, in four quarters. They differ in how the penalty is
// figured. The department's form, IT/SD 2210, first tests the whole year: if
// withholding and timely payments reach the target, there is no penalty
// however they were spread. Otherwise it compares cumulative amounts at each
// due date and charges each shortfall at one factor for the span to the next
// due date, the last span ending April 15. A payment made between two due
// dates counts only from the next one.
//
// The figure follows the 2025 IT/SD 2210, the latest published, and the 2026
// IT 1040ES. The result's Divergences say where ORC 5747.09 reads otherwise.
// HB 96 amended 5747.09 from tax year 2025, effective September 30, 2025; the
// 2025 IT/SD 2210 was published after it, and the amendment changed no figure
// the form computes, so the form is followed. The 2026 IT 1040ES worksheet
// prints 2025's tax brackets, which HB 96 replaced for 2026. These rules take
// this year's tax as given, so they use no bracket, and the divergence says
// which brackets to compute it with.
//
// Ohio's school district income tax and city income taxes take their own
// estimated payments, and none of them is covered here.

const jurisdiction = estimated.Ohio

// EstimatedFacts are the facts Ohio's rules read that no other jurisdiction
// does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorTimely         bool
	priorTimelyAnswered bool
}

// PriorYearReturnTimely reports whether last year's Ohio return was filed on
// time, and whether the caller said. IT/SD 2210 line 6 offers last year's tax
// as the target only after a timely return.
func (e EstimatedFacts) PriorYearReturnTimely() (timely, answered bool) {
	return e.priorTimely, e.priorTimelyAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

// PriorYearReturnTimely answers whether last year's Ohio return was filed on
// time. false is an answer.
func (b *EstimatedFactsBuilder) PriorYearReturnTimely(timely bool) *EstimatedFactsBuilder {
	b.e.priorTimely, b.e.priorTimelyAnswered = timely, true
	return b
}

func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is the tax after credits, less withholding, at or under
	// which no estimated payments are needed and no penalty arises.
	threshold money.USCents

	thisYearShare, priorYearShare money.Rate

	// shares are each column's step in IT/SD 2210 lines 10 and 11: 25% each,
	// so 25%, 50%, 75% and 100% by the four dates.
	shares []money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// stop is where the form's last span ends: the return's due date.
	stop estimated.Date

	rates estimated.RateSeries

	// dayCount is the days in the form's year, in hundredths: 365.25 is
	// 36525. places is how many decimal places the form prints a factor
	// to, and rounding is how a charge is rounded.
	dayCount int64
	places   int
	rounding estimated.Rounding
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Ohio's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return ohioEstimated{y}, nil
}

type ohioEstimated struct{ y estimatedYear }

// spanFactor is one IT/SD 2210 line 15 factor and what it was worked out
// from.
type spanFactor struct {
	factor  estimated.Factor
	charged bool           // false when no day of the span has a rate
	rate    money.Rate     // the rate on the span's first day
	ratedTo estimated.Date // the last day a published rate covers
	unrated estimated.Date // the first day none does, or the zero Date
	source  string
}

// factor works out the line 15 factor for the days after from up to and
// including to, by the formula on page 4 of the 2025 IT/SD 2210: the rate
// times the days late, divided by 365.25. A span that crosses a change of
// rate adds each part's rate times its days, as the 2025 form's third
// column does, and the sum is rounded once to the places the form prints.
func (y estimatedYear) factor(from, to estimated.Date) (spanFactor, error) {
	var out spanFactor
	var rateDays int64
	var sources []string
	for cur := from; cur.Before(to); {
		day := cur.AddDays(1)
		p, ok := y.rates.On(day)
		if !ok {
			out.unrated = day
			break
		}
		end := p.To()
		if to.Before(end) {
			end = to
		}
		if !out.charged {
			out.rate, out.charged = p.Rate(), true
		}
		if len(sources) == 0 || sources[len(sources)-1] != p.Source() {
			sources = append(sources, p.Source())
		}
		rateDays += int64(p.Rate()) * int64(end.DaysSince(cur))
		out.ratedTo = end
		cur = end
	}
	if !out.charged || rateDays == 0 {
		out.charged = false
		return out, nil
	}
	scale := int64(1)
	for i := 0; i < y.places; i++ {
		scale *= 10
	}
	// rate in basis points × days ÷ 10,000 ÷ (dayCount ÷ 100), in units
	// of 10^-places, rounded half up.
	d := 100 * y.dayCount
	units := (2*rateDays*scale + d) / (2 * d)
	if units == 0 || units >= scale {
		return spanFactor{}, fmt.Errorf("ohiotaxcalc: a factor of %d in %d places is not one the form prints", units, y.places)
	}
	f, err := estimated.ParseFactor(fmt.Sprintf("0.%0*d", y.places, units))
	if err != nil {
		return spanFactor{}, err
	}
	out.factor = f
	out.source = fmt.Sprintf("IT/SD 2210 (2025), page 4: rate × days late ÷ 365.25 = %v for %s to %s; %s",
		f, from.Long(), out.ratedTo.Long(), strings.Join(sources, "; "))
	return out, nil
}

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	target      money.USCents
	dues        []estimated.Date
	stop        estimated.Date

	// needed is IT/SD 2210 line 10: the target times 25%, 50%, 75% and 100%.
	needed []money.USCents

	// priorDiffers is whether the statute's test for last year's return
	// would give a different target from the form's.
	priorDiffers bool

	notes working.Notes
}

func (r ohioEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("ohiotaxcalc: the facts carry %T, not ohiotaxcalc.EstimatedFacts", s)
	}
}

func (r ohioEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("ohiotaxcalc: these are Ohio's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("ohiotaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: Ohio Adm. Code 5703-7-04 gives a filer with two-thirds of gross income from farming or fishing other ways to file and pay, which are not built",
			estimated.ErrUnsupported)
	}
	return nil
}

// divergences are where the department's forms and the Revised Code part
// ways on a rule every Ohio computation uses. They are found in
// research/estimated-payments/oh.md, conflicts 3 to 6, 11 and 12, except the
// whole-year tests, which the research does not list. Each is marked as
// affecting these facts only where the readings give them different figures.
func (r ohioEstimated) divergences(f estimated.Facts, a assessment) ([]working.Divergence, error) {
	var wholeYear, column, stops bool
	active := !a.exempt && a.requirement.TargetKnown()
	if active {
		ledger, line14, yearMet, _, err := r.ledger(f, a)
		if err != nil {
			return nil, err
		}
		for i, short := range line14 {
			if short == 0 {
				continue
			}
			wholeYear = wholeYear || yearMet
			if yearMet {
				continue
			}
			column = column || ledger[len(ledger)-1].CreditedBy() > ledger[i].CreditedBy()
			end := a.stop
			if i+1 < len(a.dues) {
				end = a.dues[i+1]
			}
			for _, p := range f.Payments() {
				stops = stops || (p.On().After(a.dues[i]) && p.On().Before(end))
			}
		}
	}
	asWithheld := active && f.Elections().WithholdingAsWithheld() && f.WithheldTotal() > 0
	annualised := active && f.Elections().Annualised()
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("Last year's tax as the target").
			Form("Last year's tax counts only if you filed last year's Ohio return on time. The form asks nothing about how many months last year covered.",
				"IT/SD 2210 (2025), page 2, lines 6, 7 and 9; IT 1040ES (2026), worksheet line 11").
			Statute("Last year's tax counts if you filed a return for last year, on time or late. Last year must also have been twelve months long.",
				"ORC 5747.09(A)(4)(b) and (E)(2)").
			Uses(working.ReadingForm).Affects(a.priorDiffers),
		working.NewDivergence("The whole-year tests").
			Form("Take the year's withholding and timely estimated payments. There's no penalty if they reach 90% of this year's tax. Last year's tax after a timely return also works. That holds however they were spread across the year.",
				"IT/SD 2210 (2025), page 2, lines 1, 5 and 7; Ohio Tax Alert, Updates to Ohio Form IT/SD 2210 (2017)").
			Statute("Each installment is measured by the tax paid by its own date. Reasonable cause excuses it when the tax paid reaches one of two figures. One is 90% of the tax on income annualised to the month before. The other is 100% of last year's tax.",
				"ORC 5747.09(D)(1) to (4) and (E)").
			Uses(working.ReadingForm).Affects(wholeYear),
		working.NewDivergence("What each column is short").
			Form("Each column's shortfall is 25%, 50%, 75% or 100% of the target. Withholding and payments to that date are subtracted. An amount still unpaid therefore carries into the next column.",
				"IT/SD 2210 (2025), page 2, lines 10 to 14").
			Statute("Each shortfall is the installment, 25% of the target, less tax paid by its date. Payments reach old shortfalls only after the installment then due is covered.",
				"ORC 5747.09(A)(4) and (D)(1) to (4)").
			Uses(working.ReadingForm).Affects(column),
		working.NewDivergence("When the interest stops").
			Form("Each column's shortfall is charged at one factor. It runs from its due date to the next due date, and the last to April 15. That holds whenever within the span it was paid. The department's FAQ ends it at the later of payment and the return's due date.",
				"IT/SD 2210 (2025), page 2, line 15, and page 4, Fiscal Year Filers; FAQ question 6 (research/estimated-payments/oh.md)").
			Statute("The underpayment period runs from the due date to the date the payment is made.",
				"ORC 5747.09(D)").
			Uses(working.ReadingForm).Affects(stops),
		working.NewDivergence("Withholding on the dates withheld").
			Form("Withholding counts as 25%, 50%, 75% and 100% of the yearly total by each date. The form has no line for the dates it was actually withheld.",
				"IT/SD 2210 (2025), page 2, line 11").
			Statute("Withholding counts in equal amounts on each due date. You can instead establish the dates all of it was withheld. No form or instruction says how to claim that.",
				"ORC 5747.09(B)").
			Uses(working.ReadingForm).Affects(asWithheld),
		working.NewDivergence("The annualised income test").
			Form("There's no annualised calculation. The long form for it was withdrawn from tax year 2017.",
				"IT/SD 2210 (2025); Ohio Tax Alert, Updates to Ohio Form IT/SD 2210 (2017)").
			Statute("Reasonable cause, and no penalty, apply if the tax paid reaches 90% of a figure. It is the tax on income annualised to the end of the month before the due date. No form says how to claim it.",
				"ORC 5747.09(E)(1)").
			Uses(working.ReadingForm).Affects(annualised),
		working.NewDivergence("The 2026 worksheet's tax brackets").
			Form("The worksheet's tax table prints 2025's brackets and is out of date. They run from $342 plus 2.75% over $26,050 up to 3.125% over $100,000. HB 96 set one bracket for 2026.",
				"IT 1040ES (2026), worksheet line 6; 2025 IT 1040 booklet, tax table").
			Statute("From 2026, the tax is $332 plus 2.75% of income over $26,050. Work out this year's tax with it.",
				"ORC 5747.02(A)(3)(c), as amended by HB 96 (136th General Assembly), effective September 30, 2025").
			Uses(working.ReadingStatute).Affects(false),
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
		return fmt.Sprintf("Your target is %s, a quarter by each due date.", target.Whole())
	}
	return fmt.Sprintf("Your target is %s. After withholding that's %s, a quarter by each due date.", target.Whole(), (target - withheld).Whole())
}

// leftWhy says what estimated payments the target leaves after
// withholding, for a Why.
func leftWhy(target, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("That's %s of estimated payments, a quarter by each due date.", target.Whole())
	}
	return fmt.Sprintf("After %s of withholding, that's %s of estimated payments. Pay a quarter by each due date.", withheld.Whole(), (target - withheld).Whole())
}

// owedWhy says what tax less withholding leaves, for a Why.
func owedWhy(tax, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("Your Ohio tax is %s, with no withholding.", tax.Whole())
	}
	return fmt.Sprintf("Your Ohio tax of %s less %s of withholding %s.", tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
}

// assess is the requirement and installments, with the divergences for
// these facts.
func (r ohioEstimated) assess(f estimated.Facts) (assessment, error) {
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

func (r ohioEstimated) measure(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	a.stop = y.calendar.NextBusinessDay(y.stop)
	last := y.year - 1
	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()

	// 90% of this year's tax, IT/SD 2210 line 4.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("ORC 5747.09(A)(4)(a); IT/SD 2210 (2025), page 2, lines 2 and 4",
			fmt.Sprintf("%v of the %v of %d Ohio tax after credits is %v", y.thisYearShare, tax, y.year, thisYear.Amount()))
	}

	// Last year's tax, lines 3, 6 and 9.
	prior, hasPrior := f.PriorYear()
	timely, timelyAnswered := extras.PriorYearReturnTimely()
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	// Questions about the target, asked only when no exception sets it.
	var unlessExempt []func()
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("No %d Ohio return on record", last))
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Ohio return on time, and what was its tax?", last),
				"With it, your target could be 100% of last year's tax, not 90% of this year's.")
		})
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Ohio return", last))
	case !timelyAnswered:
		priorOption = unavailable(fmt.Sprintf("Depends on whether you filed your %d Ohio return on time", last))
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did you file your %d Ohio return on time?", last),
				"Until then, last year's tax can't be your target. Ohio's form counts it only after a timely return.")
		})
	case !timely:
		priorOption = unavailable(fmt.Sprintf("Your %d Ohio return wasn't filed on time", last))
	default:
		pf, _ := prior.Figures()
		priorTax, _ := pf.Tax()
		amount := y.priorYearShare.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, amount)
		a.notes.Rule("ORC 5747.09(A)(4)(b); IT/SD 2210 (2025), page 2, lines 3, 6 and 9",
			fmt.Sprintf("the %d Ohio return was filed on time and showed %v of tax after credits; %v of it is %v", last, priorTax, y.priorYearShare, amount))
	}
	priorOpt, err := priorOption.Build()
	if err != nil {
		return assessment{}, err
	}
	// The statute's test: any filed return for a twelve-month year. A
	// return that does not say its months is taken as twelve.
	if hasPrior && (prior.Filed() && (prior.Months() == 0 || prior.Months() == 12)) != priorOpt.Available() {
		var priorTax money.USCents
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
		a.priorDiffers = !taxKnown || y.priorYearShare.Of(priorTax) < thisYear.Amount()
	}

	rb := estimated.NewRequirement().Option(thisYear).Option(priorOpt)
	chosen, target := estimated.BasisThisYearTax, thisYear.Amount()
	if priorOpt.Available() && (!taxKnown || priorOpt.Amount() < target) {
		chosen, target = estimated.BasisPriorYearTax, priorOpt.Amount()
	}
	if !taxKnown {
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Ohio tax after credits to be?", y.year),
				estimated.WithoutThisYearTax(priorOpt.Available()))
		})
	}

	switch {
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target is 90%% of your %d Ohio tax. You have no timely %d Ohio return to measure against.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s covers last year's tax of %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is last year's tax of %s. It keeps you clear of a penalty whatever your %d tax turns out to be. %s",
			target.Whole(), y.year, leftWhy(target, withheld))).Choose(chosen)
	case tax-withheld <= y.threshold:
		a.exempt = true
		atMost, err := estimated.ExceptionAtMost("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("%s That is %s or less, so no estimated payments are needed.", owedWhy(tax, withheld), y.threshold.Whole())
		switch {
		case withheld > tax:
			why = fmt.Sprintf("Your Ohio tax of %s less %s of withholding %s. No estimated payments are needed.", tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
		case withheld == tax:
			why = fmt.Sprintf("Your Ohio withholding of %s covers your tax of %s. No estimated payments are needed.", withheld.Whole(), tax.Whole())
		}
		rb.Required(false, why).Exempt(atMost)
		a.notes.Rule("ORC 5747.09(B); IT 1040ES (2026); IT/SD 2210 (2025), page 2, line 8",
			fmt.Sprintf("%v of tax less %v of withholding is %v, not more than %v, so there is no penalty", tax, withheld, tax-withheld, y.threshold))
	case withheld >= target:
		why := fmt.Sprintf("Your Ohio withholding of %s covers the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())
		if target == 0 {
			why = fmt.Sprintf("Your %d Ohio tax was $0, so your target is $0. No estimated payments are needed.", last)
		}
		rb.Required(false, why).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("%s That is more than %s, so estimated payments are needed. %s",
			owedWhy(tax, withheld), y.threshold.Whole(), targetWhy(target, withheld))).Choose(chosen)
	}
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
		a.needed = make([]money.USCents, len(a.dues))
		return a, nil
	}

	a.target = target
	if a.needed, err = estimated.CumulativeShares(target, y.shares); err != nil {
		return assessment{}, err
	}
	a.notes.Rule("ORC 5747.09(A)(4), (C); IT/SD 2210 (2025), page 2, line 10; IT 1040ES (2026), worksheet line 14",
		fmt.Sprintf("the target is %v, so %v, %v, %v and %v are needed by %s, %s, %s and %s", target,
			a.needed[0], a.needed[1], a.needed[2], a.needed[3], a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
	if !taxKnown {
		a.notes.Rule("ORC 5747.09(A)(4); IT/SD 2210 (2025), page 2, line 9",
			fmt.Sprintf("this year's tax was not given, so the target is the %d tax of %v; line 9 is the lesser of that and 90%% of this year's tax, so the target can only be lower once this year's tax is known", last, target))
	}
	if met := f.RequirementFirstMet(); !met.IsZero() && met.After(a.dues[0]) {
		a.notes.Rule("ORC 5747.09(B); IT/SD 2210 (2025), page 2, line 10; FAQ question 7 (research/estimated-payments/oh.md)",
			fmt.Sprintf("the requirement was first met on %s, but the form measures every column by its share of the year, and Ohio has no exception for a late start; the statute moves only the first declaration, and the department asks new residents to pay from the next due date", met.Long()))
	}
	return a, nil
}

// ledger applies the credits to the installments. It returns, with the
// ledger, each column's IT/SD 2210 line 14 and whether the whole-year tests
// on lines 5 and 7 are met.
func (r ohioEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, []money.USCents, bool, working.Notes, error) {
	var notes working.Notes
	credits, err := estimated.Credits(f, a.dues, r.y.shares)
	if err != nil {
		return nil, nil, false, notes, err
	}
	ledger, err := estimated.Apply(a.dues, estimated.Increments(a.needed), credits)
	if err != nil {
		return nil, nil, false, notes, err
	}
	line14 := make([]money.USCents, len(ledger))
	for i, l := range ledger {
		line14[i] = max(0, a.needed[i]-l.CreditedBy())
	}
	notes.Rule("ORC 5747.09(B); IT/SD 2210 (2025), page 2, lines 11 to 14",
		fmt.Sprintf("the %v withheld is treated as paid 25%% by each due date, whenever it was withheld; estimated payments count from the day paid, and last year's overpayment applied forward from %s; "+
			"each column is short by what is needed to its date less what was credited to it", f.WithheldTotal(), a.dues[0].Long()))
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("IT/SD 2210 (2025), page 2, line 11; ORC 5747.09(B)", "the form has no line for crediting withholding on the dates it was withheld, so it is spread evenly; the statute allows the dates withheld, and Divergences reports it")
	}
	if f.Elections().Annualised() {
		notes.Rule("Ohio Tax Alert, Updates to Ohio Form IT/SD 2210 (2017); ORC 5747.09(E)(1)", "the form has no annualised calculation, so the annualised election is not applied; the statute allows it, and Divergences reports it")
	}

	// Lines 1, 5 and 7: withholding, timely estimated payments and last
	// year's overpayment against the target. The form does not define
	// "timely paid"; a payment counts if made by the last due date.
	lastDue := a.dues[len(a.dues)-1]
	paid := f.WithheldTotal()
	for _, p := range f.Payments() {
		if p.FromPriorYear() || !p.On().After(lastDue) {
			paid += p.Amount()
		}
	}
	if a.exempt {
		return ledger, line14, false, notes, nil
	}
	met := paid >= a.target
	verdict := "short of"
	if met {
		verdict = "reaching"
	}
	notes.Rule("IT/SD 2210 (2025), page 2, lines 1, 5, 7 and 9",
		fmt.Sprintf("withholding, estimated payments made by %s and last year's overpayment applied forward come to %v, %s the target of %v; when they reach it there is no penalty", lastDue.Long(), paid, verdict, a.target))
	return ledger, line14, met, notes, nil
}

func (r ohioEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// installments is the schedule from the ledger. Every short column is
// excused when the whole-year test is met.
func (r ohioEstimated) installments(a assessment, ledger []estimated.LedgerEntry, yearMet bool) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(a.needed[i]).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if yearMet {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r ohioEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Ohio return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, _, yearMet, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := r.installments(a, ledger, yearMet)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

func (r ohioEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("ohiotaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Ohio return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := asOf
	if a.stop.Before(stop) {
		stop = a.stop
	}
	waiver, _ := f.Positions().Waiver()
	var more working.Notes
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(a.stop) {
		more.Rule("IT/SD 2210 (2025), page 2, line 15; FAQ question 6 (research/estimated-payments/oh.md)",
			fmt.Sprintf("the return was filed on %s, but the form charges the last column to %s whenever the balance was paid, so filing early does not shorten the penalty", filed.Long(), a.stop.Long()))
	}
	if waiver != estimated.WaiverNone {
		more.Rule("ORC 5747.09(D), as amended by HB 96 (136th General Assembly)",
			"Ohio has no casualty, retirement or disability waiver; from tax year 2025 the Tax Commissioner may abate the penalty in whole or in part, so the request goes to the commissioner, and the penalty is shown before any abatement")
	}
	ledger, line14, yearMet, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule, err := r.installments(a, ledger, yearMet)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	none := func(ns ...working.Notes) (working.Result[estimated.Penalty], error) {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		b := working.NewResult(p).With(a.notes)
		for _, n := range ns {
			b.With(n)
		}
		return b.With(more).Build()
	}
	if a.exempt {
		return none()
	}
	if yearMet {
		return none(notes)
	}

	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	var unrated estimated.Date
	var factors []string
	for i, short := range line14 {
		due := a.dues[i]
		if short == 0 || !stop.After(due) {
			continue
		}
		end := a.stop
		if i+1 < len(a.dues) {
			end = a.dues[i+1]
		}
		if stop.Before(end) {
			end = stop
		}
		sf, err := y.factor(due, end)
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		if !sf.unrated.IsZero() && (unrated.IsZero() || sf.unrated.Before(unrated)) {
			unrated = sf.unrated
		}
		lb := estimated.NewPenaltyLine(ledger[i].Number(), due, short)
		if sf.charged {
			part, err := estimated.SpanFactor(short, due, sf.ratedTo, sf.factor, sf.rate, y.rounding, sf.source)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			lb.Part(part).Days(sf.ratedTo.DaysSince(due))
			factors = append(factors, fmt.Sprintf("%v from %s", sf.factor, due.Long()))
		}
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
	notes.Rule("ORC 5747.09(D); IT/SD 2210 (2025), page 2, lines 14 to 17, and page 4",
		fmt.Sprintf("each column's underpayment is charged once, at a factor for the span from its due date to the next due date, or %s for the last: the rate times the days divided by 365.25, rounded to %d places; "+
			"a payment made within a span counts from the next due date; each charge is rounded %v; the penalty to %s is %v", a.stop.Long(), y.places, y.rounding, stop.Long(), p.Total()))
	if !a.taxKnown {
		notes.Rule("ORC 5747.09(A)(4); IT/SD 2210 (2025), page 2, line 9",
			fmt.Sprintf("this year's Ohio tax was not given, so the penalty is figured against the %d tax alone; %v is the most it can be, and it is less if 90%% of this year's tax turns out lower", y.year-1, p.Total()))
	}
	if len(factors) > 0 {
		notes.Rule("IT/SD 2210 (2025), page 4", fmt.Sprintf("the %d factors are worked out by the 2025 form's formula, rate × days late ÷ 365.25, which gives every 2025 factor exactly: %s", y.year, strings.Join(factors, ", ")))
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("Ohio hasn't published its %d penalty factors yet. Until then, the figures use the %d form: the rate times days late over 365.25.", y.year, y.year-1))
	}
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", shortDate(unrated.AddDays(-1), 0)))
	}
	if o, ok := ohioWaiver(f); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Ask(working.PositionUnsettledRule, "", "Ohio's law lets the Tax Commissioner abate all or part of the penalty from 2025. It doesn't say whether a casualty, disaster, retirement or disability qualifies.")
	}
	return working.NewResult(p).With(a.notes).With(notes).With(more).Build()
}

// ohioWaiver decides a waiver asked for. Ohio has no casualty, retirement or
// disability waiver; ORC 5747.09(D), as amended by HB 96, lets the Tax
// Commissioner abate the penalty from tax year 2025 and names no grounds,
// so every request is undecided.
func ohioWaiver(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	k, answered := f.Positions().Waiver()
	switch {
	case !answered || k == estimated.WaiverNone:
		return estimated.WaiverOutcome{}, false
	case k == estimated.WaiverCasualty:
		return estimated.WaiverUndecided("Ohio has no waiver for a casualty or disaster as such. From 2025 the Tax Commissioner can abate all or part of the penalty."), true
	default:
		return estimated.WaiverUndecided("Ohio has no waiver for retirement or disability. From 2025 the Tax Commissioner can abate all or part of the penalty."), true
	}
}

func (r ohioEstimated) Explanation() estimated.Explanation { return ohioExplanation }

// ohioExplanation is written by hand and checked against
// research/estimated-payments/oh.md.
var ohioExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Take this year's Ohio tax after credits, less Ohio withholding. If that is more than $500, Ohio expects estimated payments. The penalty is interest on what was short at each due date.").
		Paragraph("There is no penalty if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's Ohio tax or 100% of last year's. Last year's figure counts only if you filed last year's Ohio return on time. So if you filed no Ohio return last year, only the 90% figure is open to you. There is no 110% tier for higher incomes.").
		Paragraph("Pay a quarter each by April 15, June 15, September 15 and the next January 15. Withholding counts as paid evenly on the four dates, whenever it was withheld. The department's form looks at the whole year first. Withholding and payments made by the January date may reach the target. If so, there is no penalty, even when the earlier quarters were short.").
		Paragraph("Otherwise the form compares each date's need with what had been paid by then. Each shortfall is charged interest from that date to the next one. The last is charged to April 15. A payment made between two dates counts only from the next date. The rate is set once for each calendar year, 7% for 2026. The form divides the year into 365.25 days.").
		Paragraph("Since tax year 2025 the law says the penalty may be added. The Tax Commissioner may reduce or remove it. Ohio's school district and city income taxes take their own estimated payments. Those are separate from these.").
		VersusFederal("Unlike federal, Ohio's threshold is more than $500 after withholding. The federal one is $1,000. There is no 110% tier. Last year's tax counts only after a timely Ohio return. Meeting the whole year's target means no penalty however payments were spread. The interest runs to April 15 at a yearly rate. There's no casualty or retirement waiver.").
		Source("Ohio Revised Code 5747.09, as amended by HB 96 (136th General Assembly); 5703.47; 1.14").
		Source("IT/SD 2210, Interest Penalty on Underpayment of Tax, and its instructions, 2025").
		Source("IT 1040ES, Ohio Estimated Income Tax Payment Worksheet and Instructions, 2026").
		Source("Ohio Tax Alert, Updates to Ohio Form IT/SD 2210 (tax year 2017)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
