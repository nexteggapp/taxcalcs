package northdakotataxcalc

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// North Dakota's estimated tax rules, N.D.C.C. §§ 57-38-62 and 57-38-63, with
// the interest under § 57-38-45, as the shared [estimated.EstimatedTax]
// interface. North Dakota calls the charge interest.
//
// Estimates are owed only by someone who must also pay federal estimated
// tax, and only when last year's North Dakota net tax liability was $1,000
// or more. The required payment is the lesser of 90% of this year's tax or
// 100% of last year's, with no 110% tier and no twelve-month test, in four
// 25% installments. The interest is a fixed 12% a year.
//
// The figure follows Schedule ND-1UT for 2025, the latest published, with
// the 2026 dates, and Form ND-1ES for 2026. ND-1UT differs from the federal
// form in two places that change the interest: a payment goes first to its
// own quarter, and the day fraction is rounded to three places. The result's
// Divergences say where the forms and the statute part ways. § 57-38-62 and
// § 57-38-63 were not amended in 2025 (research/estimated-payments/nd.md), so
// the forms do not predate the law.

// jurisdiction is North Dakota's postal code.
const jurisdiction = estimated.NorthDakota

// EstimatedFacts are the facts North Dakota's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	federal, federalAnswered       bool
	fourth, fourthAnswered         bool
	annualised, annualisedAnswered bool
	firstIncome                    estimated.Date
}

// FederalEstimatesRequired reports whether the filer must pay federal
// estimated tax for the year, under 26 U.S.C. § 6654, and whether the caller
// said. North Dakota requires estimates of no one else.
func (e EstimatedFacts) FederalEstimatesRequired() (required, answered bool) {
	return e.federal, e.federalAnswered
}

// FederalFourthQuarterExcused reports whether the federal January
// installment was not required because the federal return was filed and the
// federal tax paid by the federal date, and whether the caller said. This is
// Schedule ND-1UT Item C.
func (e EstimatedFacts) FederalFourthQuarterExcused() (excused, answered bool) {
	return e.fourth, e.fourthAnswered
}

// AnnualisedFederally reports whether the annualized income installment
// method was used on the federal Form 2210, and whether the caller said.
// North Dakota allows the method only then.
func (e EstimatedFacts) AnnualisedFederally() (used, answered bool) {
	return e.annualised, e.annualisedAnswered
}

// FirstNorthDakotaIncome is the day a nonresident, or a filer who moved into
// North Dakota this year, first received income North Dakota taxes, and false
// if the caller did not say. This is Schedule ND-1UT Item D.
func (e EstimatedFacts) FirstNorthDakotaIncome() (estimated.Date, bool) {
	return e.firstIncome, !e.firstIncome.IsZero()
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) FederalEstimatesRequired(required bool) *EstimatedFactsBuilder {
	b.e.federal, b.e.federalAnswered = required, true
	return b
}

func (b *EstimatedFactsBuilder) FederalFourthQuarterExcused(excused bool) *EstimatedFactsBuilder {
	b.e.fourth, b.e.fourthAnswered = excused, true
	return b
}

func (b *EstimatedFactsBuilder) AnnualisedFederally(used bool) *EstimatedFactsBuilder {
	b.e.annualised, b.e.annualisedAnswered = used, true
	return b
}

func (b *EstimatedFactsBuilder) FirstNorthDakotaIncome(d estimated.Date) *EstimatedFactsBuilder {
	b.e.firstIncome = d
	return b
}

func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is § 57-38-62(3) and ND-1ES line 10: no interest when this
	// year's tax less withholding is under it.
	threshold money.USCents

	// priorGate is § 57-38-62(1) and ND-1ES line 12: no estimates when last
	// year's net tax liability was under it.
	priorGate money.USCents

	thisYearShare, priorYearShare money.Rate
	shares                        []money.Rate
	annualised                    []money.Rate
	dues                          []estimated.Date
	calendar                      estimated.Calendar
	periodEnds                    []estimated.Date

	// itemD are the first days of the Schedule ND-1UT Item D windows. Income
	// first received on or after itemD[i] starts the installments at i+2.
	itemD []estimated.Date

	// fourthQuarterBy is the federal file-and-pay date for the January
	// installment: January 31, a Sunday in 2027, moved to the Monday.
	fourthQuarterBy estimated.Date

	stop estimated.Date

	// rate, dayBasis and places are ND-1UT lines 16c and 16d: the days
	// divided by dayBasis and rounded to places, then times rate and rounded
	// to places again.
	rate     money.Rate
	dayBasis int
	places   int

	// smallInterest is ND-1UT line 17: on a return with no overpayment and
	// no tax due, interest under it need not be sent with the return.
	smallInterest money.USCents
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns North Dakota's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return northDakotaEstimated{y}, nil
}

type northDakotaEstimated struct{ y estimatedYear }

type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	dues         []estimated.Date
	installments []money.USCents
	fourthZero   bool
	notes        working.Notes
}

func (r northDakotaEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("northdakotataxcalc: the facts carry %T, not northdakotataxcalc.EstimatedFacts", s)
	}
}

func (r northDakotaEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("northdakotataxcalc: these are North Dakota's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("northdakotataxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: a qualified farmer uses 66 2/3%% and may pay once by January 15, and the March 1 waivers apply; none of that is built", estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("northdakotataxcalc: Form 2210 Schedule AI period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms and the statute part ways on
// a rule every North Dakota computation uses. The mover rule is conflict C1
// in research/estimated-payments/nd.md; the rest were found reading
// Schedule ND-1UT against §§ 57-38-62 and 57-38-63. Each is marked as
// affecting these facts only where the readings give them different figures.
func (r northDakotaEstimated) divergences(a assessment, moved bool, first int, cols []column) ([]working.Divergence, error) {
	// order: a column fell short and a later credit could pay it, where the
	// form's own-quarter rule and the statute's date-paid rule can differ.
	order, short := false, false
	for j, c := range cols {
		if c.shortfall <= 0 {
			continue
		}
		short = true
		for _, p := range c.pieces {
			order = order || !p.paidOn.IsZero()
		}
		order = order || cols[len(cols)-1].creditedBy > cols[j].creditedBy
	}
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("A filer who moved into North Dakota last year").
			Form("Form ND-1ES (2026) and the 2025 booklet keep the 100% prior-year option. They keep it for a mover who had North Dakota income before the move. Schedule ND-1UT (2025) line 6 does not.",
				"Form ND-1ES (2026), Who must pay estimated tax, 4b; Schedule ND-1UT (2025), line 6").
			Statute("No one who moved into North Dakota last year can use the prior-year option. There is no exception for earlier North Dakota income.",
				"N.D.C.C. § 57-38-62(1)(b)(1)").
			Uses(working.ReadingStatute).Affects(moved && !a.exempt),
		working.NewDivergence("The federal fourth-quarter exception").
			Form("You may not have to make the federal January payment. That happens if you filed and paid your federal return by the federal date. If so, the form enters zero for the fourth quarter.",
				"Schedule ND-1UT (2025), Part 1 Item C and line 7").
			Statute("At least a quarter of the estimated tax is due on each of the four dates. No exception is tied to the federal return.",
				"N.D.C.C. §§ 57-38-62(1), 57-38-63").
			Uses(working.ReadingForm).Affects(a.fourthZero),
		working.NewDivergence("A nonresident or new resident with no North Dakota income until after April 15").
			Form("ND-1UT sets the split by when the income started. Apr 16 to May 31 means thirds from June 15. June to August means halves from September 15. Later, it is all due in January. ND-1ES says equal payments after the income starts. That differs for June 1 to 15 or September 1 to 15.",
				"Schedule ND-1UT (2025), Part 1 Item D and line 7; Form ND-1ES (2026), Part-year requirement").
			Statute("At least a quarter of the estimated tax is due on each of the four dates. No rule covers a late start.",
				"N.D.C.C. § 57-38-63").
			Uses(working.ReadingForm).Affects(first > 0),
		working.NewDivergence("Which shortfall a payment pays").
			Form("A quarter's payment goes first to that quarter. Only the excess pays an earlier shortfall, as of that later due date. So a payment between due dates stops earlier interest only at the next due date.",
				"Schedule ND-1UT (2025), Instructions for Part 3 and line 16a").
			Statute("Interest runs from an installment's due date until that part of the tax is paid.",
				"N.D.C.C. § 57-38-62(3)").
			Uses(working.ReadingForm).Affects(order),
		working.NewDivergence("How the days are counted").
			Form("The days late are divided by 365 and rounded to three decimal places. The result is multiplied by 12% and rounded to three places again. That factor then multiplies the underpayment.",
				"Schedule ND-1UT (2025), lines 16b to 16e").
			Statute("Interest runs at a yearly rate, with no rounding of the fraction of the year.",
				"N.D.C.C. § 57-38-62(3)").
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
		return fmt.Sprintf("Your target is %s.", target.Whole())
	}
	return fmt.Sprintf("Your target is %s; after withholding that's %s.", target.Whole(), (target - withheld).Whole())
}

// leftWhy says what estimated payments the target leaves after
// withholding, for a Why.
func leftWhy(target, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("That's %s of estimated payments.", target.Whole())
	}
	return fmt.Sprintf("After %s of withholding, that's %s of estimated payments.", withheld.Whole(), (target - withheld).Whole())
}

// owedWhy says what tax less withholding leaves, as a sentence for a Why.
func owedWhy(tax, withheld money.USCents) string {
	if withheld == 0 {
		return fmt.Sprintf("Your North Dakota tax is %s, with no withholding.", tax.Whole())
	}
	return fmt.Sprintf("Your North Dakota tax is %s, and %s was withheld. That %s.", tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
}

// movedIn reports whether periods show the filer arriving in North Dakota
// during year, rather than living there from January 1 or never.
func movedIn(periods []estimated.ResidencePeriod, year int) bool {
	jan1 := estimated.MustDate(year, time.January, 1)
	arrived, fromStart := false, false
	for _, p := range periods {
		if p.State() != string(jurisdiction) {
			continue
		}
		switch {
		case p.From() == jan1:
			fromStart = true
		case p.From().Year() == year:
			arrived = true
		}
	}
	return arrived && !fromStart
}

func (r northDakotaEstimated) assess(f estimated.Facts) (assessment, error) {
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
	last := y.year - 1

	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	var priorTax money.USCents
	priorKnown := false
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, priorKnown = pf.Tax()
		}
		if prior.NotRequiredToFile() {
			priorTax, priorKnown = 0, true
		}
	}

	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("N.D.C.C. § 57-38-62(1)(a); Form ND-1ES (2026), line 11", fmt.Sprintf("%v of this year's North Dakota net tax liability of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// A mover's residence last year: from the dated periods, or unknown when
	// only "not a full-year resident" was said.
	moved := movedIn(f.PriorResidence(), last)
	// Questions about the target, asked only when no exception sets it.
	var unlessExempt []func()
	if len(f.PriorResidence()) == 0 && hasPrior {
		if resident, answered := prior.FullYearResident(); answered && !resident {
			unlessExempt = append(unlessExempt, func() {
				a.notes.Ask(working.PositionResidency, fmt.Sprintf("When did you live in North Dakota during %d?", last),
					fmt.Sprintf("If you moved in that year, 100%% of your %d tax can't be your target.", last))
			})
		}
	}

	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	priorEffect := fmt.Sprintf("Under %s, no estimates are needed; otherwise 100%% of it can be the target.", y.priorGate.Whole())
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("No %d North Dakota return on record", last))
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d North Dakota return, and what was its net tax liability?", last), priorEffect)
		})
	case prior.NotRequiredToFile():
		priorOption = unavailable(fmt.Sprintf("You weren't required to file a %d North Dakota return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d North Dakota return", last))
	case !priorKnown:
		priorOption = unavailable(fmt.Sprintf("Needs your %d North Dakota net tax liability", last))
		unlessExempt = append(unlessExempt, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("What was the net tax liability on your %d North Dakota return?", last), priorEffect)
		})
	case moved:
		priorOption = unavailable(fmt.Sprintf("You moved into North Dakota during %d", last))
		a.notes.Rule("N.D.C.C. § 57-38-62(1)(b)(1); Schedule ND-1UT (2025), line 6", fmt.Sprintf("a filer who moved into North Dakota during %d must meet the 90%% test", last))
	default:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, y.priorYearShare.Of(priorTax))
		a.notes.Rule("N.D.C.C. § 57-38-62(1)(b); Form ND-1ES (2026), line 12", fmt.Sprintf("the prior-year test is %v of the %v net tax liability on the %d return", y.priorYearShare, priorTax, last))
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
			a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d North Dakota net tax liability to be?", y.year),
				estimated.WithoutThisYearTax(priorOpt.Available()))
		})
	}

	federal, federalAnswered := extras.FederalEstimatesRequired()
	exempt := func(why string, exception estimated.TargetOption, cite, applied string) {
		a.exempt = true
		rb.Required(false, why).Exempt(exception)
		a.notes.Rule(cite, applied)
	}
	switch {
	case federalAnswered && !federal:
		none, err := estimated.NewException("No federal estimated tax required").Build()
		if err != nil {
			return assessment{}, err
		}
		exempt(fmt.Sprintf("You don't have to pay federal estimated tax for %d. North Dakota doesn't require estimated payments either.", y.year), none,
			"N.D.C.C. § 57-38-62(1); Form ND-1ES (2026), Who must pay estimated tax, 1", "only a filer subject to 26 U.S.C. § 6654 owes North Dakota estimated tax")
	case priorKnown && priorTax < y.priorGate:
		below, err := estimated.ExceptionBelow("Last year's North Dakota tax", priorTax, y.priorGate)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your %d North Dakota net tax liability was %s, under %s. No estimated payments are needed for %d.", last, priorTax.Whole(), y.priorGate.Whole(), y.year)
		if prior.NotRequiredToFile() {
			why = fmt.Sprintf("You weren't required to file a %d North Dakota return. No estimated payments are needed for %d.", last, y.year)
		}
		exempt(why, below,
			"N.D.C.C. § 57-38-62(1); Form ND-1ES (2026), line 12", fmt.Sprintf("a %d net tax liability of %v is under %v, and a filer not required to file counts as zero, so no estimates are required", last, priorTax, y.priorGate))
	case taxKnown && tax-withheld < y.threshold:
		below, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("%s That's under %s, so no estimated payments are needed.", owedWhy(tax, withheld), y.threshold.Whole())
		switch {
		case withheld > tax:
			why = fmt.Sprintf("%s No estimated payments are needed.", owedWhy(tax, withheld))
		case withheld == tax:
			why = fmt.Sprintf("Your withholding of %s equals your North Dakota tax. No estimated payments are needed.", withheld.Whole())
		}
		exempt(why, below,
			"N.D.C.C. § 57-38-62(3); Form ND-1ES (2026), line 10", fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no interest", tax, withheld, tax-withheld, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("With no usable %d liability, your target is 90%% of your %d North Dakota tax.", last, y.year)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s reaches 100%% of your %d liability, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is 100%% of your %d liability, %s. That keeps you clear of interest whatever your %d tax turns out to be. %s",
			last, target.Whole(), y.year, leftWhy(target, withheld))).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your North Dakota withholding of %s covers the %s needed for %d. No estimated payments are needed.", withheld.Whole(), target.Whole(), y.year)).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("%s That's %s or more, so estimated payments are needed. %s",
			owedWhy(tax, withheld), y.threshold.Whole(), targetWhy(target, withheld))).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	if !a.exempt {
		for _, ask := range unlessExempt {
			ask()
		}
	}
	if !a.exempt && !federalAnswered {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Do you have to pay federal estimated tax for %d?", y.year),
			"If you don't, North Dakota doesn't require estimated payments either.")
	}

	// The installments: four quarters, or Item D's late start.
	first := 0
	if !a.exempt {
		thisRes := f.Residence()
		eligible := len(thisRes) > 0 && !estimated.ResidentAllYear(thisRes, jurisdiction, y.year)
		for _, p := range thisRes {
			if p.State() == string(jurisdiction) && p.From() == estimated.MustDate(y.year, time.January, 1) {
				eligible = false
			}
		}
		income, incomeKnown := extras.FirstNorthDakotaIncome()
		switch {
		case eligible && incomeKnown:
			first = estimated.LateStart(income, y.itemD)
			if first > 0 {
				a.notes.Rule("Schedule ND-1UT (2025), Part 1 Item D and line 7", fmt.Sprintf("North Dakota income was first received on %s, after %s, so the required payment is spread equally over the %d installments from %s",
					income.Long(), y.dues[0].Long(), len(a.dues)-first, a.dues[first].Long()))
			}
		case eligible:
			a.notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first receive North Dakota income in %d?", y.year),
				fmt.Sprintf("If it was after %s, you pay in fewer, larger installments.", shortDate(y.dues[0], y.year)))
		case incomeKnown && len(thisRes) == 0:
			a.notes.Ask(working.PositionResidency, fmt.Sprintf("Where did you live during %d?", y.year),
				fmt.Sprintf("North Dakota income begun after %s means fewer, larger installments. That holds if you weren't a North Dakota resident all year.", shortDate(y.dues[0], y.year)))
		}
	}
	cumulative, err := estimated.EqualShares(a.requirement.Target(), len(a.dues), first)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("N.D.C.C. § 57-38-63; Schedule ND-1UT (2025), line 7", fmt.Sprintf("of %v, %v, %v, %v and %v are due on %s, %s, %s and %s", a.requirement.Target(),
		a.installments[0], a.installments[1], a.installments[2], a.installments[3], a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() && !a.exempt {
		used, answered := extras.AnnualisedFederally()
		switch {
		case answered && !used:
			a.notes.Rule("Schedule ND-1UT (2025), Part 1 Item B; N.D. Admin. Code § 81-03-04-02(3)(c)", "the annualized method is allowed only if it was used on the federal Form 2210, and it was not, so the regular installments are used")
		default:
			if !answered {
				a.notes.Ask(working.PositionJurisdictionFact, "Did you use the annualized method on your federal Form 2210?",
					"Until then, the figures use it, though North Dakota allows it only if you did.")
			}
			periods := f.Periods()
			annual := make([]money.USCents, len(periods))
			for i, p := range periods {
				annual[i] = p.AnnualisedTax()
			}
			ann, err := estimated.Annualised(annual, y.annualised, a.installments)
			if err != nil {
				return assessment{}, err
			}
			a.installments = ann
			a.notes.Rule("Schedule ND-1UT (2025), Part 1 Item B and line 7; Form 2210 (2025), Schedule AI line 20", fmt.Sprintf("under the annualized method the installments are %v, %v, %v and %v: 22.5%%, 45%%, 67.5%% and 90%% of the annualized North Dakota tax, "+
				"less earlier installments, and never more than the regular installment plus any reduction carried forward", ann[0], ann[1], ann[2], ann[3]))
		}
	}

	// Item C: the federal fourth-quarter exception zeroes the January column.
	if !a.exempt {
		excused, answered := extras.FederalFourthQuarterExcused()
		switch {
		case excused:
			a.fourthZero = true
			a.installments[len(a.installments)-1] = 0
			a.notes.Rule("Schedule ND-1UT (2025), Part 1 Item C and line 7", fmt.Sprintf("the federal January installment was not required, the federal return having been filed and paid by %s, so nothing is due on %s",
				y.fourthQuarterBy.Long(), a.dues[len(a.dues)-1].Long()))
		case !answered:
			if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(y.fourthQuarterBy) {
				a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did you file your federal return and pay the federal tax by %s?", shortDate(y.fourthQuarterBy, y.year)),
					"If you did, nothing is due on your North Dakota January payment.")
			}
		}
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}
	cols, _, err := r.ledger(f, a)
	if err != nil {
		return assessment{}, err
	}
	divergences, err := r.divergences(a, moved, first, cols)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

func (r northDakotaEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// piece is part of a column's underpayment and the day it was paid or
// offset; the zero Date if it never was.
type piece struct {
	amount money.USCents
	paidOn estimated.Date
}

// column is one Schedule ND-1UT column after the credits are applied.
type column struct {
	due        estimated.Date
	required   money.USCents // line 7
	neededBy   money.USCents
	creditedBy money.USCents
	shortfall  money.USCents // line 15
	pieces     []piece
}

func (c column) outstanding() money.USCents {
	left := c.shortfall
	for _, p := range c.pieces {
		if !p.paidOn.IsZero() {
			left -= p.amount
		}
	}
	return left
}

// applyByQuarter is Schedule ND-1UT lines 8 to 15 and the Part 3
// application rule. A credit counts in the column whose due date is the first
// on or after it. A column's credits, with any overpayment carried in, pay
// that column first; the excess pays earlier underpayments, earliest first,
// as of this column's due date, and what is left carries on. A credit after
// the last due date pays the earliest underpayment on its own date.
//
// between reports a credit dated strictly between due dates that arrived
// while an earlier column was still short, the one case where the form's
// date-by-column reading and treating it as a late payment of the earlier
// quarter give different interest.
func applyByQuarter(dues []estimated.Date, required []money.USCents, credits []estimated.Payment) (cols []column, between bool) {
	cs := append([]estimated.Payment(nil), credits...)
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].On().Before(cs[j].On()) })
	cols = make([]column, len(dues))
	var carry, credited, needed money.USCents
	next := 0
	for i, due := range dues {
		var inColumn money.USCents
		for next < len(cs) && !cs[next].On().After(due) {
			if i > 0 && cs[next].On().After(dues[i-1]) && cs[next].On().Before(due) {
				for j := 0; j < i; j++ {
					if cols[j].outstanding() > 0 {
						between = true
					}
				}
			}
			inColumn += cs[next].Amount()
			credited += cs[next].Amount()
			next++
		}
		needed += required[i]
		total := inColumn + carry // line 10
		cols[i] = column{due: due, required: required[i], neededBy: needed, creditedBy: credited}
		if total < required[i] {
			cols[i].shortfall = required[i] - total
			carry = 0
			continue
		}
		over := total - required[i] // line 12
		for j := 0; j < i && over > 0; j++ {
			owed := cols[j].outstanding()
			if owed <= 0 {
				continue
			}
			paid := min(owed, over)
			cols[j].pieces = append(cols[j].pieces, piece{amount: paid, paidOn: due})
			over -= paid
		}
		carry = over // line 13
	}
	for ; next < len(cs); next++ {
		left := cs[next].Amount()
		for j := range cols {
			owed := cols[j].outstanding()
			if owed <= 0 || left <= 0 {
				continue
			}
			paid := min(owed, left)
			cols[j].pieces = append(cols[j].pieces, piece{amount: paid, paidOn: cs[next].On()})
			left -= paid
		}
	}
	for j := range cols {
		if owed := cols[j].outstanding(); owed > 0 {
			cols[j].pieces = append(cols[j].pieces, piece{amount: owed})
		}
	}
	return cols, between
}

func (r northDakotaEstimated) ledger(f estimated.Facts, a assessment) ([]column, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, r.y.shares)
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	cols, between := applyByQuarter(a.dues, a.installments, credits)
	var notes working.Notes
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("Schedule ND-1UT (2025), line 8 instructions", fmt.Sprintf("as shown, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("Schedule ND-1UT (2025), line 8 instructions", fmt.Sprintf("one-fourth of the %v withheld is treated as withheld by each due date", f.WithheldTotal()))
	}
	notes.Rule("Schedule ND-1UT (2025), Instructions for Part 3", "a payment counts in the quarter whose due date it falls before and pays that quarter first; only the excess pays an earlier underpayment, as of that quarter's due date")
	if between {
		notes.Ask(working.PositionUnsettledRule, "", "North Dakota doesn't say if a payment between due dates stops interest that day. The figures stop an earlier quarter's interest at the next due date.")
	}
	return cols, notes, nil
}

// installments is the schedule from the columns. The January installment is
// excused under the federal fourth-quarter exception.
func (r northDakotaEstimated) installments(a assessment, cols []column) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(cols))
	for i, c := range cols {
		b := estimated.NewInstallment(i+1, c.due).NeededBy(c.neededBy).CreditedBy(c.creditedBy).Shortfall(c.shortfall)
		if a.fourthZero && i == len(cols)-1 {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r northDakotaEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d North Dakota return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	cols, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := r.installments(a, cols)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// pow10 is 10 to the n.
func pow10(n int) int64 {
	p := int64(1)
	for range n {
		p *= 10
	}
	return p
}

// printed writes units at places decimal places, as "0.020".
func printed(units int64, places int) string {
	return fmt.Sprintf("%d.%0*d", units/pow10(places), places, units%pow10(places))
}

// factor is Schedule ND-1UT lines 16c and 16d for a span of days: the
// fraction of the year and the interest factor, each in units of the last
// decimal place, both rounded half up.
func (y estimatedYear) factor(days int) (fraction, factor int64) {
	unit := pow10(y.places)
	basis := int64(y.dayBasis)
	fraction = (int64(days)*unit*2 + basis) / (2 * basis)
	factor = (fraction*int64(y.rate)*2 + int64(money.Percent(100))) / (2 * int64(money.Percent(100)))
	return fraction, factor
}

// charge is ND-1UT line 16e for one piece: the amount times the factor,
// rounded to the cent.
func (y estimatedYear) charge(amount money.USCents, from, to estimated.Date) (estimated.PenaltyPart, error) {
	days := to.DaysSince(from)
	fraction, factor := y.factor(days)
	source := fmt.Sprintf("Schedule ND-1UT (2025), lines 16b to 16e: %d days ÷ %d = %s, × %v = %s", days, y.dayBasis, printed(fraction, y.places), y.rate, printed(factor, y.places))
	var c money.USCents
	if factor > 0 {
		fac, err := estimated.ParseFactor(printed(factor, y.places))
		if err != nil {
			return estimated.PenaltyPart{}, err
		}
		c = fac.Of(amount, 1, estimated.RoundToCent)
	}
	return estimated.NewPenaltyPart(estimated.PartInterest, amount, y.rate).Span(from, to).Days(days).Charge(c).Source(source).Build()
}

func (r northDakotaEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("northdakotataxcalc: interest needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d North Dakota return to measure against, there is no target to figure the interest from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := asOf
	if y.stop.Before(stop) {
		stop = y.stop
	}
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop = filed
	}
	waiver, waiverAsked := f.Positions().Waiver()
	cols, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule, err := r.installments(a, cols)
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

	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	for i, c := range cols {
		if c.shortfall <= 0 || !c.due.Before(stop) {
			continue
		}
		lb := estimated.NewPenaltyLine(i+1, c.due, c.shortfall)
		lastDay := c.due
		for _, p := range c.pieces {
			to := stop
			if !p.paidOn.IsZero() && p.paidOn.Before(stop) {
				to = p.paidOn
			}
			if !to.After(c.due) {
				continue
			}
			part, err := y.charge(p.amount, c.due, to)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			lb.Part(part)
			if to.After(lastDay) {
				lastDay = to
			}
		}
		line, err := lb.Days(lastDay.DaysSince(c.due)).Build()
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
		notes.Rule("N.D.C.C. § 57-38-62(1)", fmt.Sprintf("this year's North Dakota tax was not given, so the interest is figured against the %d liability alone; "+
			"the required payment is the lesser of that and 90%% of this year's tax, so %v is the most the interest can be, and it is less if this year's tax turns out lower", y.year-1, p.Total()))
	}
	notes.Rule("N.D.C.C. §§ 57-38-62(3), 57-38-45; Schedule ND-1UT (2025), lines 16a to 17", fmt.Sprintf("each quarter's underpayment is charged %v a year: the days late ÷ %d, rounded to %d places, × %v, rounded again, × the underpayment, from its due date until paid or %s; "+
		"the interest to %s is %v", y.rate, y.dayBasis, y.places, y.rate, y.stop.Long(), stop.Long(), p.Total()))
	if p.Total() > 0 && p.Total() < y.smallInterest {
		notes.Rule("Schedule ND-1UT (2025), line 17 instructions", fmt.Sprintf("if the return shows no overpayment and no tax due, interest under %v need not be sent with it; the interest is shown in full", y.smallInterest))
	}
	switch {
	case p.Total() > 0 && !waiverAsked:
		notes.Ask(working.PositionPenaltyWaiver, "Do you have good cause for a waiver of interest, or an IRS disaster extension?",
			"Either one can remove the interest, which is shown in full until then. The Tax Commissioner decides a waiver for good cause.")
	case waiverAsked && waiver != estimated.WaiverNone:
		notes.Rule("N.D.C.C. § 57-38-45(5); Office of State Tax Commissioner, Disaster Information and Tax Implications", fmt.Sprintf("North Dakota has no waiver for %s as such; the Tax Commissioner may waive interest for good cause shown, and an IRS disaster extension carries over automatically; the interest is shown before any waiver", waiver))
	}
	if o, ok := northDakotaWaiver(f); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Ask(working.PositionUnsettledRule, "", "North Dakota lets the Tax Commissioner waive interest for good cause shown. It doesn't say if a casualty, disaster, retirement or disability is good cause.")
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// northDakotaWaiver decides a waiver asked for. North Dakota has no
// casualty, retirement or disability waiver; N.D.C.C. § 57-38-45(5) lets the
// Tax Commissioner waive interest for good cause shown, and names no causes,
// so every request is undecided.
func northDakotaWaiver(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	p := f.Positions()
	k, answered := p.Waiver()
	if !answered || k == estimated.WaiverNone {
		return estimated.WaiverOutcome{}, false
	}
	switch {
	case p.WaiverCause() == estimated.CauseDisaster:
		return estimated.WaiverUndecided("North Dakota has no disaster waiver as such but follows IRS disaster extensions. The Tax Commissioner can waive all or part of the interest for good cause shown."), true
	case k == estimated.WaiverCasualty:
		return estimated.WaiverUndecided("North Dakota has no casualty waiver as such. The Tax Commissioner can waive all or part of the interest for good cause shown."), true
	default:
		return estimated.WaiverUndecided("North Dakota has no waiver for retirement or disability. The Tax Commissioner can waive all or part of the interest for good cause shown."), true
	}
}

func (r northDakotaEstimated) Explanation() estimated.Explanation { return northDakotaExplanation }

// northDakotaExplanation is written by hand and checked against
// research/estimated-payments/nd.md.
var northDakotaExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"North Dakota expects estimates only if you must also pay federal estimated tax. Your North Dakota tax last year must also have been $1,000 or more. This year's tax less withholding must also be $1,000 or more. Falling short costs interest at 12% a year.").
		Paragraph("Your target is the smaller of 90% of this year's tax or 100% of last year's. You're clear if withholding and estimated payments reach it. A quarter is due by April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. Last year's return need not cover a full year. If you weren't required to file last year, no estimates are due this year. Withholding counts as paid evenly unless you show when it was taken.").
		Paragraph("An early federal return and payment can excuse the federal January payment. If it does, nothing is due for North Dakota's January payment either. The annualized method is allowed only if you used it on your federal Form 2210.").
		Paragraph("Interest runs from each due date until the shortfall is paid. It stops on April 15 of the next year. A payment goes first to its own quarter. Only the excess pays an earlier shortfall, as of that later due date. The form rounds the fraction of the year to three decimal places. The Tax Commissioner can waive interest for good cause. North Dakota follows an IRS disaster extension automatically.").
		VersusFederal("Unlike federal, North Dakota adds a $1,000 test on last year's tax. North Dakota has no 110% tier or twelve-month test. The charge is a fixed 12% a year. A payment pays its own quarter first. The day count is rounded to three places.").
		Source("N.D.C.C. §§ 57-38-45, 57-38-62, 57-38-63, 1-03-05").
		Source("N.D. Admin. Code § 81-03-04-02").
		Source("Schedule ND-1UT, 2025 Underpayment or Late Payment of Estimated Income Tax for Individuals, and its instructions").
		Source("Form ND-1ES, Estimated Income Tax - Individuals, 2026").
		Source("Office of State Tax Commissioner, Disaster Information and Tax Implications").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()

// date is a shorthand for the year files.
func date(y int, m time.Month, d int) estimated.Date { return estimated.MustDate(y, m, d) }
