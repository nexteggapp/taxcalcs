package marylandtaxcalc

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Maryland's estimated income tax rules, Md. Code, Tax-General §§ 10-815,
// 10-820, 10-902, 13-602 and 13-604, as the shared [estimated.EstimatedTax]
// interface.
//
// Maryland's rules are close to the federal ones in outline and differ in the
// numbers and in how the charge is figured. The prior-year test is 110% for
// everybody. Withholding counts on the dates it was withheld. Form 502UP's
// columns are cumulative, so an unpaid amount is charged again in every
// later column until it is paid. The charge is interest at the Comptroller's
// rate for the calendar year, which Form 502UP prints as one factor for each
// span between due dates.
//
// The figure follows Form 502UP for 2025, the latest published, and the 2026
// payment voucher worksheet. The result's Divergences say where the
// Tax-General Article reads otherwise. No act since 2024 changed these rules
// (research/estimated-payments/md.md), so no form here predates the law it
// describes. The 2026 Form 502UP is not published, so its factors are not
// known, and the interest is figured by the month as § 13-604(a) says; the
// penalty reports that in Required.

const jurisdiction = estimated.Maryland

// EstimatedFacts are the facts Maryland's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	otherState         bool
	otherStateAnswered bool
}

// TaxableByAnotherState reports whether at least 90% of the year's taxable
// income is also taxable by another state, and whether the caller said. Form
// 502UP then charges no interest when the underpayment is not greater than
// the local tax, [estimated.Figures.LocalTax] (code 302).
func (e EstimatedFacts) TaxableByAnotherState() (taxable, answered bool) {
	return e.otherState, e.otherStateAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

// TaxableByAnotherState answers whether at least 90% of taxable income is
// also taxable by another state. false is an answer.
func (b *EstimatedFactsBuilder) TaxableByAnotherState(taxable bool) *EstimatedFactsBuilder {
	b.e.otherState, b.e.otherStateAnswered = taxable, true
	return b
}

func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is the tax after withholding at or under which no interest
	// is due, Form 502UP exception A.
	threshold money.USCents

	// thisYearShare and priorYearShare are Form 502UP lines 8 and 9b.
	thisYearShare, priorYearShare money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// fileAndPayBy is the day by which a return filed and paid in full takes
	// the place of the January installment, before it is moved for a
	// weekend.
	fileAndPayBy estimated.Date

	// stop is the return's due date, where Form 502UP's last column ends.
	stop estimated.Date

	periodEnds []estimated.Date

	// rates has one period for each calendar year the interest can run in,
	// January 1 to December 31, at the rate the Comptroller sets for that
	// year under § 13-604(b).
	rates estimated.RateSeries
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are § 10-902(a)(1): at least 25% of the estimate with each
// declaration. Form 502UP line 11 takes one-fourth, one-half and
// three-fourths of the year's income by default, which is the same.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Maryland's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return marylandEstimated{y}, nil
}

type marylandEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// needed is Form 502UP line 13: the running total required by each due
	// date.
	needed []money.USCents

	excusedJanuary bool
	extras         EstimatedFacts
	notes          working.Notes
}

func (r marylandEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("marylandtaxcalc: the facts carry %T, not marylandtaxcalc.EstimatedFacts", s)
	}
}

func (r marylandEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("marylandtaxcalc: these are Maryland's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("marylandtaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: a farmer or fisherman with two-thirds of gross income from farming or fishing pays once, by January 15, under Form 502UP's special calculation, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		periods := f.Periods()
		for i, p := range periods {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("marylandtaxcalc: Form 502UP line 11 period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
			if i > 0 && p.Income() < periods[i-1].Income() {
				return fmt.Errorf("marylandtaxcalc: Form 502UP line 11 is income received from January 1, so period %d cannot have less than period %d", i+1, i)
			}
		}
		if periods[3].Income() <= 0 {
			return errors.New("marylandtaxcalc: Form 502UP line 12 divides by the year's income, which must be more than zero")
		}
	}
	return nil
}

// divergences are where Maryland's forms and the Tax-General Article part
// ways on a rule the requirement uses, each marked with whether it changes
// anything for these facts. They are found in research/estimated-payments/md.md,
// Verification, and checked there against both. The rules the interest uses
// are in penaltyDivergences.
func (r marylandEstimated) divergences(atFiveHundred, priorChosen, incomeNotUp bool) ([]working.Divergence, error) {
	return buildAll(
		working.NewDivergence("No interest at $500").
			Form("No interest is due when your tax after Maryland withholding is $500 or less. Exactly $500 is free of interest. The 2025 booklet says less than $500.",
				"Form 502UP (2025), Exceptions, A; 2025 Maryland resident booklet, Underpayment of Estimated Tax").
			Statute("No interest may be charged on unpaid tax less than one-half the federal $1,000. Exactly $500 of unpaid tax can carry interest.",
				"Md. Code, Tax-Gen. § 13-602(c); 26 U.S.C. § 6654(e)(1)").
			Uses(working.ReadingForm).Affects(atFiveHundred),
		working.NewDivergence("Who may use 110% of last year's tax").
			Form("Form 502UP offers 110% of last year's tax to everyone. The 2026 payment worksheet has a condition. It needs 110% \"if you will receive more income than you did in 2025\".",
				"Form 502UP (2025), Exceptions, B, and lines 9b and 10; Payment Voucher Worksheet (2026), How to estimate your 2026 tax").
			Statute("An estimate below 90% of this year's tax and 110% of last year's draws interest. There's no condition on income.",
				"Md. Code, Tax-Gen. § 13-602(a)(2)").
			Uses(working.ReadingForm).Affects(priorChosen && incomeNotUp),
		working.NewDivergence("Last year's tax").
			Form("Last year's tax is Form 502 line 34, less the Part CC credits on Form 502CR.",
				"Form 502UP (2025), line 9a and its instructions").
			Statute("The test is 110% of the tax paid for last year. That tax is reduced by the credit for tax paid to another state.",
				"Md. Code, Tax-Gen. § 13-602(a)(2)(ii)").
			Uses(working.ReadingForm).Affects(priorChosen),
	)
}

// penaltyDivergences are the interest factors, a third-period payment made
// from December 15, and the penalty of up to 25%. The factors and the 25%
// affect any interest charged, and the December rule a third-period
// shortfall paid from December 15.
func (r marylandEstimated) penaltyDivergences(charged, december bool) ([]working.Divergence, error) {
	return buildAll(
		working.NewDivergence("The interest factors").
			Form("Each span between due dates is charged once at a printed factor. A factor is the span's days over 365 times the annual rate. For 2025 they are 0.0192, 0.0289, 0.0381 and 0.0266. The 2026 form isn't published yet.",
				"Form 502UP (2025), line 16 and its instructions").
			Statute("Interest is one-twelfth of the annual rate for each month or part of a month. For 2025's spans that is 0.0191, 0.0287, 0.0377 and 0.0270.",
				"Md. Code, Tax-Gen. § 13-604(a)").
			Uses(working.ReadingStatute).Affects(charged),
		working.NewDivergence("A third-period payment made from December 15").
			Form("A third-period payment before December 15 is charged for the months it was late. One made on or after December 15 is charged the whole printed factor. The next due date is still January 15.",
				"Form 502UP (2025), Line 16 instructions, 3rd Period").
			Statute("Interest runs by the month or part of one, from the due date to the day paid. The next installment is due January 15.",
				"Md. Code, Tax-Gen. §§ 10-820(b)(1), 13-602(a), 13-604(a)").
			Uses(working.ReadingStatute).Affects(december),
		working.NewDivergence("The penalty of up to 25%").
			Form("Only interest is figured.",
				"Form 502UP (2025), lines 16 to 18").
			Statute("A penalty of up to 25% of the amount underestimated is also to be charged.",
				"Md. Code, Tax-Gen. § 13-702(a)").
			Uses(working.ReadingForm).Affects(charged),
	)
}

func buildAll(bs ...*working.DivergenceBuilder) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range bs {
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
	return fmt.Sprintf("%s, %s", o.Label(), o.Amount().Whole())
}

func (r marylandEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	a := assessment{extras: extras}
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	last := y.year - 1
	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()

	// 90% of this year's tax, Form 502UP line 8.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("Md. Code, Tax-Gen. § 13-602(a)(2)(i); Form 502UP (2025), lines 7 and 8",
			fmt.Sprintf("%v of the %v of Maryland and local tax after credits is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// 110% of last year's tax, Form 502UP line 9b, at every income.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	// Questions whose answer can change the target, asked only when no
	// exception sets it.
	var targetAsks []func()
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Maryland return", last))
		targetAsks = append(targetAsks, func() {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Maryland return, and what tax did it show?", last),
				fmt.Sprintf("With it, your target could be %v of last year's tax, not %v of this year's.", y.priorYearShare, y.thisYearShare))
		})
	case prior.NotRequiredToFile():
		priorOption = unavailable(fmt.Sprintf("You didn't need to file a %d Maryland return", last))
		targetAsks = append(targetAsks, func() {
			a.notes.Ask(working.PositionUnsettledRule, "", fmt.Sprintf("Maryland's documents don't say if not needing a %d return means no %d tax. That would make your target zero, and the figures don't count it.", last, last))
		})
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Maryland return, so this year's tax sets your target", last))
		a.notes.Rule("Form 502UP (2025), line 10", fmt.Sprintf("no %d return was filed, so as a first-time filer only %v of this year's tax counts", last, y.thisYearShare))
	default:
		pf, _ := prior.Figures()
		priorTax, _ := pf.Tax()
		amount := y.priorYearShare.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, amount)
		a.notes.Rule("Md. Code, Tax-Gen. § 13-602(a)(2)(ii); Form 502UP (2025), lines 9a and 9b",
			fmt.Sprintf("%v of the %v of %d Maryland and local tax is %v; Maryland uses %v at every income", y.priorYearShare, priorTax, last, amount, y.priorYearShare))
		if prior.Months() != 12 {
			a.notes.Rule("Md. Code, Tax-Gen. § 13-602(a)(2)(ii); Form 502UP (2025), line 9a",
				fmt.Sprintf("neither sets a condition on how many months the %d return covered, so the tax on its %d months is used", last, prior.Months()))
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
	if !taxKnown {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Maryland and local tax to be?", y.year),
			fmt.Sprintf("With it, %v of this year's tax could set a lower target. Tax of %s or less after withholding needs no payments.", y.thisYearShare, y.threshold.Whole()))
	}

	chosenOpt := thisYear
	if chosen == estimated.BasisPriorYearTax {
		chosenOpt = priorOpt
	}
	switch {
	case taxKnown && tax-withheld <= y.threshold:
		a.exempt = true
		ex, err := estimated.ExceptionAtMost("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your Maryland and local tax is %s. Your Maryland withholding of %s %s. That is %s or less, so no estimated payments are needed. No interest is charged.",
			tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld), y.threshold.Whole())
		switch {
		case tax < withheld:
			why = fmt.Sprintf("Your Maryland and local tax is %s. Your Maryland withholding of %s %s. No estimated payments are needed, and no interest is charged.",
				tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
		case tax == withheld:
			why = fmt.Sprintf("Your Maryland withholding covers all %s of your Maryland and local tax. No estimated payments are needed, and no interest is charged.", tax.Whole())
		}
		rb.Required(false, why).Exempt(ex)
		a.notes.Rule("Form 502UP (2025), Exceptions, A; Payment Voucher Worksheet (2026), Who must file a declaration",
			fmt.Sprintf("%v of tax less %v withheld is %v, not more than %v, so no interest is due", tax, withheld, tax-withheld, y.threshold))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("There's no target until you have your %d Maryland tax or your %d return.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your target is %s. Your withholding of %s reaches it. No estimated payments are needed, whatever your %d tax turns out to be.",
			optionLabel(priorOpt), withheld.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %s. It keeps you clear of interest whatever your %d tax turns out to be. After %s of withholding, you need %s of estimated payments.",
			optionLabel(priorOpt), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case target == 0:
		rb.Required(false, fmt.Sprintf("Your target is %s. No estimated payments are needed.", optionLabel(chosenOpt))).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Maryland withholding of %s reaches your target of %s. No estimated payments are needed if enough is withheld by each date.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		lead := fmt.Sprintf("Your Maryland and local tax less withholding leaves %s, over %s.", (tax - withheld).Whole(), y.threshold.Whole())
		if (tax - withheld).Whole() == y.threshold.Whole() {
			lead = fmt.Sprintf("Your Maryland and local tax less withholding is more than %s.", y.threshold.Whole())
		}
		rb.Required(true, fmt.Sprintf("%s Your target is %s. You need %s of estimated payments.", lead, optionLabel(chosenOpt), (target-withheld).Whole())).Choose(chosen)
	}
	if !a.exempt {
		for _, ask := range targetAsks {
			ask()
		}
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	incomeNotUp := false
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			was, wasKnown := pf.StateAGI()
			now, nowKnown := f.ThisYear().StateAGI()
			incomeNotUp = wasKnown && nowKnown && now <= was
		}
	}
	priorChosen := !a.exempt && a.requirement.TargetKnown() && chosen == estimated.BasisPriorYearTax
	divergences, err := r.divergences(taxKnown && tax-withheld == y.threshold, priorChosen, incomeNotUp)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)

	if a.exempt {
		a.needed = make([]money.USCents, len(a.dues))
		return a, nil
	}

	// Form 502UP lines 11 to 13: the target times the share of the year's
	// income received by each period's end.
	if f.Elections().Annualised() {
		periods := f.Periods()
		year := periods[3].Income()
		a.needed = make([]money.USCents, len(periods))
		for i, p := range periods {
			a.needed[i] = shareOf(target, max(0, p.Income()), year)
		}
		a.notes.Rule("Form 502UP (2025), lines 11 to 13",
			fmt.Sprintf("by election, the %v required is taken in the shares of the year's %v of income received by March 31, May 31, August 31 and December 31: %v, %v, %v and %v by the four dates",
				target, year, a.needed[0], a.needed[1], a.needed[2], a.needed[3]))
	} else {
		if a.needed, err = estimated.CumulativeShares(target, quarters); err != nil {
			return assessment{}, err
		}
		a.notes.Rule("Md. Code, Tax-Gen. §§ 10-820(b)(1), 10-902(a)(1); Form 502UP (2025), lines 11 to 13",
			fmt.Sprintf("a quarter of the %v required is due by each of %s, %s, %s and %s: %v, %v, %v and %v in all",
				target, a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long(), a.needed[0], a.needed[1], a.needed[2], a.needed[3]))
	}

	// A return filed and paid in full by January 31 takes the place of the
	// January installment. January 31, 2027 is a Sunday, and § 1-201(b)
	// moves it to Monday, February 1.
	fileBy := y.calendar.NextBusinessDay(y.fileAndPayBy)
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(fileBy) {
		a.excusedJanuary = true
		a.needed[3] = a.needed[2]
		a.notes.Rule("Md. Code, Tax-Gen. §§ 1-201(b), 10-820(b)(3), 10-902(b); Payment Voucher Worksheet (2026), Filing a return instead of fourth payment",
			fmt.Sprintf("the return was filed and paid on %s, by %s (%s, moved past a weekend or holiday), so it takes the place of the %s installment",
				filed.Long(), y.fileAndPayBy.Long(), fileBy.Long(), a.dues[3].Long()))
	}
	return a, nil
}

// shareOf is a × num ÷ den, rounded half up to the cent.
func shareOf(a, num, den money.USCents) money.USCents {
	n := new(big.Int).Mul(big.NewInt(int64(a)), big.NewInt(int64(num)))
	return estimated.RoundToCent.Div(n, big.NewInt(int64(den)))
}

// credits are what Form 502UP line 14 counts: withholding on the dates it was
// withheld, and estimated payments on theirs.
func (r marylandEstimated) credits(f estimated.Facts, a assessment) ([]estimated.Payment, working.Notes, error) {
	cs, err := estimated.CreditsAsWithheld(f, a.dues)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	notes.Rule("Form 502UP (2025), line 14", fmt.Sprintf("the %v withheld counts on the dates it was withheld, from the beginning of the year to each due date, "+
		"and estimated payments on the dates paid; Maryland does not spread withholding evenly", f.WithheldTotal()))
	return cs, notes, nil
}

// ledger applies the credits to the installments, each installment being
// what line 13 adds by its date.
func (r marylandEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, []estimated.Payment, working.Notes, error) {
	cs, notes, err := r.credits(f, a)
	if err != nil {
		return nil, nil, working.Notes{}, err
	}
	ledger, err := estimated.Apply(a.dues, estimated.Increments(a.needed), cs)
	if err != nil {
		return nil, nil, working.Notes{}, err
	}
	return ledger, cs, notes, nil
}

// underpayments are Form 502UP line 15: line 13 less line 14 by each due
// date, and never below zero. Each includes any earlier underpayment still
// unpaid.
func underpayments(a assessment, ledger []estimated.LedgerEntry) []money.USCents {
	out := make([]money.USCents, len(ledger))
	for i, l := range ledger {
		out[i] = max(0, a.needed[i]-l.CreditedBy())
	}
	return out
}

// otherState applies the exception for income taxable by another state: no
// interest when at least 90% of taxable income is also taxable by another
// state and the underpayment is not greater than the local tax. The
// underpayment is taken as the largest on line 15.
func (r marylandEstimated) otherState(f estimated.Facts, a assessment, line15 []money.USCents) (bool, working.Notes) {
	var notes working.Notes
	var largest money.USCents
	for _, u := range line15 {
		largest = max(largest, u)
	}
	if largest == 0 {
		return false, notes
	}
	const cite = "Md. Code, Tax-Gen. § 13-602(b); Form 502UP (2025), Income Taxable by Another State"
	taxable, answered := a.extras.TaxableByAnotherState()
	local, localKnown := f.ThisYear().LocalTax()
	switch {
	case !answered:
		notes.Ask(working.PositionJurisdictionFact, "Is at least 90% of your income also taxable by another state?",
			"If so, no interest is charged on a shortfall no larger than your county tax.")
	case !taxable:
	case !localKnown:
		notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("What is your %d county income tax?", r.y.year),
			"No interest is charged on a shortfall no larger than your county tax. This applies when at least 90% of your income is taxable by another state.")
	case largest <= local:
		notes.Rule(cite, fmt.Sprintf("at least 90%% of taxable income is also taxable by another state, and the largest underpayment on line 15, %v, is not greater than the %v of local tax, so no interest is due", largest, local))
		return true, notes
	default:
		notes.Rule(cite, fmt.Sprintf("at least 90%% of taxable income is also taxable by another state, but the largest underpayment on line 15, %v, is more than the %v of local tax, so the exception does not apply", largest, local))
	}
	return false, notes
}

func (r marylandEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

func (r marylandEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Maryland return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, _, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	excused := false
	var more working.Notes
	if !a.exempt {
		excused, more = r.otherState(f, a, underpayments(a, ledger))
	}
	out, err := installments(a, ledger, excused)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).With(more).Build()
}

// installments are the due dates with what was needed and credited by each.
// excused is the exception for income taxable by another state, which covers
// every date.
func installments(a assessment, ledger []estimated.LedgerEntry, excused bool) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	for i, l := range ledger {
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(a.needed[i]).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if excused || (a.excusedJanuary && i == 3) {
			b.Excused()
		}
		var err error
		if out[i], err = b.Build(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r marylandEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("marylandtaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Maryland return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	ledger, credits, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	line15 := underpayments(a, ledger)
	// A third-period underpayment paid from December 15, before the January
	// due date.
	december := false
	if !a.exempt && line15[2] > 0 {
		from := estimated.MustDate(y.year, time.December, 15)
		for _, c := range credits {
			if !c.On().Before(from) && c.On().Before(a.dues[3]) && c.Amount() > 0 {
				december = true
			}
		}
	}
	none := func(schedule []estimated.Installment, ns ...working.Notes) (working.Result[estimated.Penalty], error) {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		ds, err := r.penaltyDivergences(false, false)
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		var dn working.Notes
		dn.Diverges(ds...)
		b := working.NewResult(p).With(a.notes)
		for _, n := range ns {
			b.With(n)
		}
		return b.With(dn).Build()
	}
	if a.exempt {
		return none(nil)
	}
	excused, more := r.otherState(f, a, line15)
	schedule, err := installments(a, ledger, excused)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if excused {
		return none(schedule, notes, more)
	}

	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	var unrated estimated.Date
	charged := false
	for i, short := range line15 {
		due := a.dues[i]
		if short == 0 || !stop.After(due) {
			continue
		}
		end := y.stop
		if i+1 < len(a.dues) {
			end = a.dues[i+1]
		}
		if stop.Before(end) {
			end = stop
		}
		lb := estimated.NewPenaltyLine(i+1, due, short)
		lastDay := due
		charge := func(amount money.USCents, to estimated.Date) error {
			parts, gap, err := monthlyInterest(amount, due, to, y.rates)
			if err != nil {
				return err
			}
			if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
				unrated = gap
			}
			for _, p := range parts {
				lb.Part(p)
			}
			if to.After(lastDay) {
				lastDay = to
			}
			return nil
		}
		// A payment made before the end of the span pays part of the
		// underpayment, which is charged only the months it was late.
		left := short
		for _, c := range credits {
			if left == 0 || !c.On().After(due) || !c.On().Before(end) || c.Amount() == 0 {
				continue
			}
			piece := min(left, c.Amount())
			if err := charge(piece, c.On()); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			left -= piece
		}
		if left > 0 {
			if err := charge(left, end); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
		line, err := lb.Days(lastDay.DaysSince(due)).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		pb.Line(line)
		charged = true
	}
	if !a.taxKnown {
		pb.UpperBound()
	}
	p, err := pb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}

	if paidInFull {
		notes.Rule("Md. Code, Tax-Gen. § 13-602(a); Form 502UP (2025), Line 16 instructions", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every underpayment still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("Md. Code, Tax-Gen. §§ 13-602(a), 13-604(a); Form 502UP (2025), lines 15 to 18", fmt.Sprintf("each column's underpayment, which includes any earlier one still unpaid, is charged from its due date to the next due date, or %s for the last, "+
		"or to the day a later payment paid it; each month or fraction of a month is one-twelfth of the annual rate for the year it ends in; the interest to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !a.taxKnown {
		notes.Rule("Md. Code, Tax-Gen. § 13-602(a)(2); Form 502UP (2025), line 10", fmt.Sprintf("this year's Maryland tax was not given, so the interest is figured against %v of the %d tax alone; "+
			"line 10 is the lesser of that and %v of this year's tax, so %v is the most the interest can be, and it is less if this year's tax turns out lower",
			y.priorYearShare, y.year-1, y.thisYearShare, p.Total()))
	}
	ds, err := r.penaltyDivergences(p.Total() > 0, december)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	notes.Diverges(ds...)
	if charged {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("Until Maryland publishes %d interest factors, the law's monthly rate is used. For 2025 it differed from the printed factors by up to 0.0004 of the shortfall.", y.year))
	}
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The Maryland rate for the days after %s isn't published yet.", unrated.AddDays(-1).Long()))
	}
	if p.Total() > 0 {
		notes.Ask(working.PositionUnpublishedRate, "", "The law allows a separate penalty of up to 25% of the shortfall. No published rule sets the percentage, so the figures show interest only.")
		if !waiverAnswered {
			notes.Ask(working.PositionPenaltyWaiver, "Are you asking Maryland to waive the interest or penalty for reasonable cause?",
				"The Comptroller decides reasonable cause; interest is shown before any waiver.")
		}
	}
	if waiverAnswered && waiver != estimated.WaiverNone {
		notes.Rule("Md. Code, Tax-Gen. §§ 13-606(a), 13-714(a)", fmt.Sprintf("a waiver for %v is asked for; Maryland waives for reasonable cause, which the Comptroller decides, so the interest shown is before it", waiver))
	}
	if o, ok := marylandWaiver(f); ok && p.Total() > 0 {
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		if o.Undecided() {
			notes.Ask(working.PositionUnsettledRule, "", "Maryland's law allows a waiver for reasonable cause without listing the causes. It doesn't say whether a casualty or disaster is one.")
		}
	}
	return working.NewResult(p).With(a.notes).With(more).With(notes).Build()
}

// marylandWaiver decides a waiver asked for. Tax-General §§ 13-606(a) and
// 13-714(a) let the tax collector waive interest and penalty for reasonable
// cause, without listing causes. Maryland has no retirement or disability
// waiver.
func marylandWaiver(f estimated.Facts) (estimated.WaiverOutcome, bool) {
	k, answered := f.Positions().Waiver()
	switch {
	case !answered || k == estimated.WaiverNone:
		return estimated.WaiverOutcome{}, false
	case k == estimated.WaiverCasualty:
		return estimated.WaiverUndecided("Maryland can waive interest for reasonable cause, but its law lists no causes. The Comptroller decides if a casualty or disaster counts."), true
	default:
		return estimated.WaiverRefused("Maryland waives only for reasonable cause, not retirement or disability as such. The Comptroller decides reasonable cause."), true
	}
}

func rateList(rates estimated.RateSeries) string {
	s := ""
	for i, p := range rates.Periods() {
		if i > 0 {
			s += " and "
		}
		s += fmt.Sprintf("%v for %d", p.FineRate(), p.From().Year())
	}
	return s
}

// monthlyInterest charges principal for each month or fraction of a month
// after from up to to, at one-twelfth of the annual rate for the calendar
// year in which that month ends, as § 13-604(a) charges interest. Months in
// a row at one rate are one part, rounded to the cent.
//
// unrated is the first day not charged because a month ends in a year with
// no published rate, or the zero Date.
func monthlyInterest(principal money.USCents, from, to estimated.Date, rates estimated.RateSeries) ([]estimated.PenaltyPart, estimated.Date, error) {
	var parts []estimated.PenaltyPart
	start, prevEnd := from, from
	var cur estimated.RatePeriod
	n := 0
	flush := func() error {
		if n == 0 {
			return nil
		}
		num := new(big.Int).Mul(big.NewInt(int64(principal)), big.NewInt(int64(n)))
		num.Mul(num, big.NewInt(int64(cur.FineRate())))
		p, err := estimated.NewPenaltyPart(estimated.PartInterest, principal, 0).FineRate(cur.FineRate()).
			Span(start, prevEnd).Days(prevEnd.DaysSince(start)).Months(n).
			Charge(estimated.RoundToCent.Div(num, big.NewInt(12_000_000))).Source(cur.Source()).Build()
		if err != nil {
			return err
		}
		parts = append(parts, p)
		start, n = prevEnd, 0
		return nil
	}
	months := estimated.MonthsOrPart(from, to)
	for i := 1; i <= months; i++ {
		end := from.AddMonths(i)
		if end.After(to) {
			end = to
		}
		rate, ok := rates.On(end)
		if !ok {
			if err := flush(); err != nil {
				return nil, estimated.Date{}, err
			}
			return parts, prevEnd.AddDays(1), nil
		}
		if n > 0 && rate.From() != cur.From() {
			if err := flush(); err != nil {
				return nil, estimated.Date{}, err
			}
		}
		cur, prevEnd = rate, end
		n++
	}
	if err := flush(); err != nil {
		return nil, estimated.Date{}, err
	}
	return parts, estimated.Date{}, nil
}

func (r marylandEstimated) Explanation() estimated.Explanation { return marylandExplanation }

// marylandExplanation is written by hand and checked against
// research/estimated-payments/md.md.
var marylandExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Maryland expects estimated payments when your tax will be more than $500. That tax is Maryland and county tax less Maryland withholding. The charge for paying too little is interest on each quarter's shortfall.").
		Paragraph("Maryland charges nothing when your tax after withholding is $500 or less. Otherwise, withholding and estimated payments must reach a target. Short of it, interest is due. The target is 90% of this year's Maryland and local tax. If 110% of last year's tax is less, that is the target. The 110% applies at every income; there's no 100% figure as there is federally. County income tax is paid with the state's, so the payments have to cover both.").
		Paragraph("A quarter of the target is due by April 15, June 15 and September 15. The last quarter is due by January 15 of the next year. A date on a weekend or holiday shifts to the next business day. Withholding counts on the dates it was taken, not spread evenly over the year. Late-year withholding doesn't cover an early quarter. Filing your return and paying in full by January 31 replaces January's payment. If your income came unevenly, there's another way to measure each quarter. Use the share of the year's income you'd received by then.").
		Paragraph("Interest is figured on what was short at each due date. An earlier shortfall still unpaid counts again at every later date. Interest runs until the shortfall is paid or the return is due on April 15. The rate is set for each calendar year: 10.8133% for 2026 and 9.995% for 2027. Interest is charged by the month, and part of a month counts as a whole one. The Comptroller may waive the interest for reasonable cause. The law also allows a separate penalty of up to 25% of the shortfall. The underpayment form doesn't figure that penalty.").
		VersusFederal("Maryland differs from federal rules in six ways. Its $500 threshold counts county tax. Everyone uses 110% of last year's tax. Withholding counts on its dates. Uneven income uses the share of the year's income received, not annualizing. A shortfall is charged again at every later quarter. The yearly rate has a 9% floor and is charged by the month.").
		Source("Md. Code, Tax-General §§ 1-201, 10-815, 10-820, 10-902, 13-602, 13-604, 13-606, 13-702, 13-714").
		Source("Payment Voucher Worksheet for Estimated Tax and Extension Payments (PVW), tax year 2026").
		Source("Form 502UP, Underpayment of Estimated Income Tax by Individuals, and its instructions, 2025").
		Source("2025 Maryland State and Local Tax Forms and Instructions (resident booklet)").
		Source("Maryland Register 53:18 (September 4, 2026), Notice of Interest Rate on Refunds and Moneys Owed to the State").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
