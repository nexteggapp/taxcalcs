package mississippitaxcalc

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Mississippi's estimated tax rules, Miss. Code § 27-7-327 and 35 Miss.
// Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100, as the shared
// [estimated.EstimatedTax] interface.
//
// Mississippi differs from the federal rules in shape. The current-year test
// is 80%, and the prior-year test is 100% with no higher tier. All
// withholding counts toward the April installment. The charge is interest of
// ½% a month on the running underestimate, column by column, from each due
// date to the next or until paid, and the fourth column runs to April 15.
//
// The figure follows Form 80-320 (Rev. 09/25), the latest published; no 2026
// edition is posted. 2025 House Bill 1 changed the tax rates for 2026 and not
// the estimated tax sections, so the form does not predate the law it
// describes (research/estimated-payments/ms.md).
//
// Not reported in Required, because no 2026 figure turns on them: no source
// says whether a due date on a weekend or holiday moves, and every 2026 date
// and the April 15, 2027 end are weekdays; and no source states a January 31
// file-and-pay rule, so there is none here.

// jurisdiction is Mississippi's postal code.
const jurisdiction = estimated.Mississippi

// EstimatedFacts are the facts Mississippi's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	partMonthWhole    bool
	partMonthAnswered bool
	priorIncomeTax    optional
}

type optional struct {
	v   money.USCents
	set bool
}

// PartMonthCountsWhole reports how a span shorter than a whole number of
// months is charged, and whether the caller said: true counts the part month
// as a whole one, false counts only the months completed. § 27-7-327 charges
// "per month" and says neither.
func (e EstimatedFacts) PartMonthCountsWhole() (whole, answered bool) {
	return e.partMonthWhole, e.partMonthAnswered
}

// PriorYearIncomeAtThisYearsRates is the tax on last year's income at this
// year's Mississippi rates and exemptions, which Rule 100(3) uses as the
// prior-year test for a filer who was not required to file a resident return
// last year. Giving it applies that test.
func (e EstimatedFacts) PriorYearIncomeAtThisYearsRates() (money.USCents, bool) {
	return e.priorIncomeTax.v, e.priorIncomeTax.set
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

// PartMonthCountsWhole answers how a part month is charged.
func (b *EstimatedFactsBuilder) PartMonthCountsWhole(whole bool) *EstimatedFactsBuilder {
	b.e.partMonthWhole, b.e.partMonthAnswered = whole, true
	return b
}

func (b *EstimatedFactsBuilder) PriorYearIncomeAtThisYearsRates(a money.USCents) *EstimatedFactsBuilder {
	b.e.priorIncomeTax = optional{a, true}
	return b
}

// Build refuses a negative tax on last year's income.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.priorIncomeTax.v < 0 {
		return EstimatedFacts{}, errors.New("mississippitaxcalc: the tax on last year's income cannot be negative")
	}
	return b.e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is Form 80-320 line 1: no interest when the year's liability
	// is this or less.
	threshold money.USCents

	// thisYearShare and priorYearShare are Form 80-320 lines 2 and 4.
	thisYearShare, priorYearShare money.Rate

	// shares are Form 80-320 line 5, and withholdingShares line 6: all
	// withholding in column A.
	shares, withholdingShares []money.Rate

	dues []estimated.Date

	// stop is the end of column D.
	stop estimated.Date

	// monthly is the § 27-7-327(e) rate per month.
	monthly    money.Rate
	rateSource string
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Mississippi's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return mississippiEstimated{y}, nil
}

type mississippiEstimated struct{ y estimatedYear }

type assessment struct {
	requirement  estimated.Requirement
	exempt       bool
	taxKnown     bool
	installments []money.USCents
	extras       EstimatedFacts

	// zeroLine3 is Form 80-320 line 4 read with a zero line 3, and
	// priorIncome is Rule 100(3)'s tax on last year's income, as given.
	zeroLine3, priorIncome bool

	notes working.Notes
}

func (r mississippiEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("mississippitaxcalc: the facts carry %T, not mississippitaxcalc.EstimatedFacts", s)
	}
}

func (r mississippiEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("mississippitaxcalc: these are Mississippi's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("mississippitaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: farmers and fishermen owe no interest if 80%% is paid by January 15 or the return is filed and paid by March 1, which is not built",
			estimated.ErrUnsupported)
	}
	return nil
}

// divergences are where the department's form, its rule and the statute part
// ways on a rule every Mississippi computation uses. They are in the research
// file, checked there against all three. Each says whether it changes
// anything for f.
func (r mississippiEstimated) divergences(f estimated.Facts, a assessment) ([]working.Divergence, error) {
	short, priorChosen := false, false
	if !a.exempt && a.requirement.TargetKnown() {
		credits, _, err := r.credits(f)
		if err != nil {
			return nil, err
		}
		ledger, err := estimated.Apply(r.y.dues, a.installments, credits)
		if err != nil {
			return nil, err
		}
		for _, l := range ledger {
			short = short || l.Shortfall() > 0
		}
		c, _ := a.requirement.Chosen()
		priorChosen = c.Basis() != estimated.BasisThisYearTax
	}
	zeroReading := working.ReadingForm
	if a.priorIncome {
		zeroReading = working.ReadingStatute
	}
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The interest rate").
			Form("Interest is ½% a month.",
				"Form 80-320 (Rev. 09/25), line 9; Form 80-100 (2025), Interest and Penalty Provisions").
			Statute("The statute sets ½% a month for taxes assessed from Jan 1, 2019. The department's rule still prints the earlier 1% a month.",
				"Miss. Code § 27-7-327(e), as amended by Laws 2014, H.B. 799 § 7; 35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(1)").
			Uses(working.ReadingForm).Affects(short),
		working.NewDivergence("The prior-year test").
			Form("You need the lesser of 80% of this year's liability and 100% of last year's. A quarter is due by each date.",
				"Form 80-320 (Rev. 09/25), lines 1 to 5").
			Statute("The statute requires estimates of at least 80% of the tax actually due. It has no prior-year test. The department's rule instead has a prior-year test. It charges no interest when the year's payments reach last year's liability. That liability must be on a filed twelve-month return.",
				"Miss. Code § 27-7-327; 35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(3)").
			Uses(working.ReadingForm).Affects(priorChosen),
		working.NewDivergence("Last year's liability was zero").
			Form("If last year's liability is zero, line 4 depends on your estimated payments. With none, it takes 80% of this year's liability. With any, it takes the lesser of the two, which is zero.",
				"Form 80-320 (Rev. 09/25), Line 4 instructions").
			Statute("If no resident return was required last year, the department's rule differs. It measures payments against the tax on last year's income. That tax uses this year's rates and exemptions. The statute has no prior-year test.",
				"35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(3); Miss. Code § 27-7-327").
			Uses(zeroReading).Affects(!a.exempt && (a.zeroLine3 || a.priorIncome)),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// assess is the requirement and installments, with the divergences.
func (r mississippiEstimated) assess(f estimated.Facts) (assessment, error) {
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

func (r mississippiEstimated) measure(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	a := assessment{extras: extras}
	last := y.year - 1

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
	paidEstimates := false
	for _, p := range f.Payments() {
		if p.Amount() > 0 {
			paidEstimates = true
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
		a.notes.Rule("Miss. Code § 27-7-327; Form 80-320 (Rev. 09/25), line 2", fmt.Sprintf("%v of this year's Mississippi liability of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	unavailable := func(basis estimated.Basis, why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(basis, y.priorYearShare, 0).Unavailable(why)
	}
	// zeroRule is Form 80-320 line 4 when line 3 is zero.
	// whyNot says why last year is zero, for a person; why says it for the
	// working.
	zeroRule := func(whyNot, why string) *estimated.TargetOptionBuilder {
		a.zeroLine3 = true
		if !paidEstimates {
			return unavailable(estimated.BasisPriorYearTax, whyNot+". With no estimated payments, this year's figure is used")
		}
		a.notes.Rule("Form 80-320 (Rev. 09/25), Line 4 instructions", fmt.Sprintf("%s and estimated payments were made, so line 4 is the lesser of line 2 and zero, which is zero", why))
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(estimated.BasisPriorYearTax, fmt.Sprintf("Needs your %d Mississippi return", last))
	case prior.NotRequiredToFile():
		if amount, ok := extras.PriorYearIncomeAtThisYearsRates(); ok {
			a.priorIncome = true
			priorOption = estimated.NewTargetOption(estimated.BasisPriorYearIncome, y.priorYearShare, amount)
			a.notes.Rule("35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(3)", fmt.Sprintf("no %d resident return was required, so the prior-year test is the tax on %d income at %d rates and exemptions, %v, as given", last, last, y.year, amount))
		} else {
			priorOption = zeroRule(fmt.Sprintf("You didn't have to file a %d Mississippi return", last), fmt.Sprintf("no %d return was required, so line 3 is zero", last))
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What is the tax on your %d income at %d Mississippi rates and exemptions?", last, y.year),
				fmt.Sprintf("You didn't have to file a %d resident return. So the department uses that tax as the last-year test.", last))
		}
	case !prior.Filed():
		priorOption = unavailable(estimated.BasisPriorYearTax, fmt.Sprintf("You didn't file a %d Mississippi return", last))
	case prior.Months() != 12:
		priorOption = unavailable(estimated.BasisPriorYearTax, fmt.Sprintf("Your %d Mississippi return covered %d months, not twelve", last, prior.Months()))
	case priorTax == 0:
		priorOption = zeroRule(fmt.Sprintf("Your %d Mississippi return showed no liability", last), fmt.Sprintf("the %d return showed no liability, so line 3 is zero", last))
	default:
		amount := y.priorYearShare.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, amount)
		a.notes.Rule("Form 80-320 (Rev. 09/25), lines 3 and 4; 35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(3)", fmt.Sprintf("the prior-year test is %v of the %v liability on the %d return, or %v", y.priorYearShare, priorTax, last, amount))
	}
	priorOpt, err := priorOption.Build()
	if err != nil {
		return assessment{}, err
	}

	rb := estimated.NewRequirement().Option(thisYear).Option(priorOpt)
	chosen, target := estimated.BasisThisYearTax, thisYear.Amount()
	if priorOpt.Available() && (!taxKnown || priorOpt.Amount() < target) {
		chosen, target = priorOpt.Basis(), priorOpt.Amount()
	}
	if !taxKnown {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Mississippi liability to be?", y.year),
			fmt.Sprintf("It sets the %v option and the %s test.", y.thisYearShare, y.threshold.Whole()))
	}

	switch {
	case taxKnown && tax <= y.threshold:
		a.exempt = true
		small, err := estimated.ExceptionAtMost("Mississippi liability", tax, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("Your Mississippi liability of %s is %s or less. So no estimated payments are needed.", tax.Whole(), y.threshold.Whole())).Exempt(small)
		a.notes.Rule("Form 80-320 (Rev. 09/25), Line 1 instructions; Form 80-106 (Rev. 09/25)", fmt.Sprintf("the liability of %v is %v or less, so no interest is due", tax, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("A target needs your %d Mississippi liability or a full-year %d return. There is no target yet.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches your %d figure, %s. All of it counts toward Apr 15. No estimated payments are needed, whatever your %d liability turns out to be.",
			withheld.Whole(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is your %d figure, %s. All %s of withholding counts toward Apr 15. That leaves %s of estimated payments, whatever your %d liability is.",
			last, target.Whole(), withheld.Whole(), (target-withheld).Whole(), y.year)).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Mississippi withholding of %s already reaches the %s target. All of it counts toward Apr 15, so no estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Mississippi liability of %s is over %s. Withholding of %s falls short of the %s target. You need %s of estimated payments. All withholding counts toward the Apr 15 installment, a quarter of the target.",
			tax.Whole(), y.threshold.Whole(), withheld.Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// Under an exception last year's return can't change the target.
	if !hasPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Mississippi return, and what was its liability?", last),
			fmt.Sprintf("With it, last year's liability could be a lower target than %v of this year's.", y.thisYearShare))
	}

	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(2); Form 80-320 (Rev. 09/25), line 5", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s",
		a.requirement.Target(), a.installments[0], y.dues[0].Long(), y.dues[1].Long(), y.dues[2].Long(), y.dues[3].Long()))
	if f.Elections().Annualised() {
		a.notes.Rule("35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100; Form 80-320 (Rev. 09/25)", "Mississippi has no annualized income method for individuals, so the four equal installments are used")
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(y.dues))
	}
	return a, nil
}

func (r mississippiEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// credits are withholding and payments, dated as Form 80-320 line 6 counts
// them, earliest first.
func (r mississippiEstimated) credits(f estimated.Facts) ([]estimated.Payment, working.Notes, error) {
	var notes working.Notes
	var credits []estimated.Payment
	var err error
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, r.y.dues)
		notes.Rule("Form 80-320 (Rev. 09/25), Line 6 instructions", fmt.Sprintf("as the caller answered, the %v withheld is counted on the dates it was withheld", f.WithheldTotal()))
	} else {
		credits, err = estimated.Credits(f, r.y.dues, r.y.withholdingShares)
		notes.Rule("Form 80-320 (Rev. 09/25), line 6", fmt.Sprintf("the %v withheld is all counted in column A, on %s, whenever it was withheld", f.WithheldTotal(), r.y.dues[0].Long()))
		for _, w := range f.Withholding() {
			if w.On().After(r.y.dues[0]) && w.Amount() > 0 {
				notes.Ask(working.PositionJurisdictionFact, "Do you want your withholding after Apr 15 counted on the dates it was withheld?",
					"Only Form 80-320 covers it, so the figures count all withholding toward Apr 15.")
				break
			}
		}
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	sort.SliceStable(credits, func(i, j int) bool { return credits[i].On().Before(credits[j].On()) })
	return credits, notes, nil
}

func (r mississippiEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Mississippi return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	out, _, notes, err := r.schedule(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments after the credits, with the credits they come
// from. A due date before the requirement was met is excused.
func (r mississippiEstimated) schedule(f estimated.Facts, a assessment) ([]estimated.Installment, []estimated.Payment, working.Notes, error) {
	credits, notes, err := r.credits(f)
	if err != nil {
		return nil, nil, working.Notes{}, err
	}
	ledger, err := estimated.Apply(r.y.dues, a.installments, credits)
	if err != nil {
		return nil, nil, working.Notes{}, err
	}
	firstMet := f.RequirementFirstMet()
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(l.NeededBy()).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if !firstMet.IsZero() && firstMet.After(l.Due()) {
			b.Excused()
		}
		if out[i], err = b.Build(); err != nil {
			return nil, nil, working.Notes{}, err
		}
	}
	return out, credits, notes, nil
}

// completedMonths counts the whole months from from to to, leaving out a part
// month at the end.
func completedMonths(from, to estimated.Date) int {
	k := 0
	for !from.AddMonths(k + 1).After(to) {
		k++
	}
	return k
}

func (r mississippiEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("mississippitaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Mississippi return to measure against, there is no target to figure the interest from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	sched, credits, notes, err := r.schedule(f, a)
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

	whole, answered := a.extras.PartMonthCountsWhole()
	if !answered {
		whole = true
	}
	partMonth := false
	charge := func(principal money.USCents, from, to estimated.Date) (estimated.PenaltyPart, error) {
		months := completedMonths(from, to)
		if estimated.MonthsOrPart(from, to) != months {
			partMonth = true
			if whole {
				months = estimated.MonthsOrPart(from, to)
			}
		}
		n := int64(principal) * int64(y.monthly) * int64(months)
		c := money.USCents((n + 5_000) / 10_000)
		return estimated.NewPenaltyPart(estimated.PartMonthly, principal, y.monthly).
			Span(from, to).Months(months).Charge(c).Source(y.rateSource).Build()
	}

	// Form 80-320 lines 5 to 10: the running underestimate on each due date,
	// charged until paid or the next due date, whichever is earlier.
	firstMet := f.RequirementFirstMet()
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, sched)
	required := make([]money.USCents, len(y.dues))
	var sum money.USCents
	for k := range y.dues {
		sum += a.installments[k]
		required[k] = sum
	}
	cols, err := estimated.Columns(y.dues, required, credits, y.stop, stop)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	early := false
	for _, col := range cols {
		due := col.Due()
		if col.Under() <= 0 {
			continue
		}
		if !firstMet.IsZero() && firstMet.After(due) {
			notes.Rule("Form 80-320 (Rev. 09/25), Exceptions; Form 80-100 (2025)", fmt.Sprintf("the filing requirement was met on %s, after the %s due date, so no interest is computed in that column", firstMet.Long(), due.Long()))
			continue
		}
		lb := estimated.NewPenaltyLine(col.Number(), due, col.Under())
		for _, piece := range col.Pieces() {
			to, paid := piece.PaidOn()
			if !paid {
				to = col.End()
			}
			part, err := charge(piece.Amount(), due, to)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			lb.Part(part)
		}
		line, err := lb.Days(col.Through().DaysSince(due)).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		if line.Total() > 0 && col.Number() < len(y.dues) {
			early = true
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
		notes.Rule("Form 80-320 (Rev. 09/25), line 4", fmt.Sprintf("this year's Mississippi liability was not given, so the interest is figured against the %d figure alone; "+
			"the required amount is the lesser of that and 80%% of this year's liability, so %v is the most the interest can be, and it is less if this year's liability turns out lower", y.year-1, p.Total()))
	}
	notes.Rule("Miss. Code § 27-7-327(e); Form 80-320 (Rev. 09/25), lines 5 to 11", fmt.Sprintf("each column's running underestimate is charged %v a month from its due date until paid or the next due date, whichever is earlier, and column D runs to %s; "+
		"the interest to %s is %v", y.monthly, y.stop.Long(), stop.Long(), p.Total()))
	if partMonth && !answered {
		notes.Ask(working.PositionJurisdictionFact, "Should interest for part of a month be charged as a whole month?",
			"The law charges per month and says nothing about part months. The figures count a part month as whole, as other Mississippi interest rules do.")
	}
	if early && firstMet.IsZero() {
		notes.Ask(working.PositionRequirementFirstMet, fmt.Sprintf("When did you first have to make estimated payments for %d?", y.year),
			"Interest runs only from the due dates after that.")
	}
	if waiverAsked && waiver != estimated.WaiverNone {
		notes.Rule("Form 80-320 (Rev. 09/25); Form 80-100 (2025)", "Mississippi's published estimated tax rules have no waiver for casualty, retirement or disability; the interest is shown in full")
		// The research found no waiver and no rule refusing one.
		if p.Total() > 0 {
			o := estimated.WaiverUndecided("Mississippi publishes no retirement, disability, casualty or disaster waiver. Only the Department of Revenue can say whether it will waive the interest.")
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			notes.Ask(working.PositionUnsettledRule, "", "Interest might be waived for retirement, disability, casualty or disaster. No Mississippi statute, rule or form says, so it is shown in full.")
		}
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r mississippiEstimated) Explanation() estimated.Explanation { return mississippiExplanation }

// mississippiExplanation is written by hand and checked against
// research/estimated-payments/ms.md.
var mississippiExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Mississippi expects estimated payments when your year's liability is over $200. None are expected if withholding covers 80% or more of that liability. Falling short costs interest of ½% a month.").
		Paragraph("The target is 80% of this year's Mississippi liability or 100% of last year's. It is whichever is lower. You are clear if withholding and estimated payments reach it. A quarter is due by each of April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. If last year's liability was zero, the form uses 80% of this year's instead. That holds only if you made no estimated payments.").
		Paragraph("All your withholding counts toward the April payment, whenever it was taken. So if withholding covers the year's requirement, nothing is due. Withholding taken late in the year is credited early.").
		Paragraph("The interest is figured column by column on Form 80-320. On each due date, any amount still short is charged ½% a month. That includes any shortfall carried from earlier dates. The charge runs until the next due date or until you pay it. The last column runs from January 15 to April 15. A liability of $200 or less has no interest due.").
		Paragraph("Payments may first be required partway through the year. Then interest is figured only from the due dates after that. There is no annualized method and no relief for filing by January 31. There is no published waiver for retirement, disability or a casualty.").
		VersusFederal("Mississippi's current-year test is 80%, not 90%. The prior-year test is always 100%. The threshold is $200 of liability, not $1,000 after withholding. All withholding counts toward the first installment. The charge is a flat ½% a month on the running shortfall. The federal charge is a daily rate on each installment.").
		Source("Miss. Code § 27-7-327, as amended by Laws 2014, H.B. 799 § 7").
		Source("35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100").
		Source("Form 80-320, Interest and Penalty Worksheet, and its instructions and example (Rev. 09/25)").
		Source("Form 80-106 (Rev. 09/25) and Form 80-100, 2025 Resident Individual Income Tax Instructions").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
