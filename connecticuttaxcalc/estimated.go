package connecticuttaxcalc

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Connecticut's estimated income tax rules, Conn. Gen. Stat. §§ 12-701(a)(12)
// and 12-722, as the shared [estimated.EstimatedTax] interface.
//
// The arithmetic is federal in shape: 90% of this year's tax or 100% of last
// year's, four equal quarters, nothing under $1,000, the annualised method at
// 22.5%, 45%, 67.5% and 90%. Connecticut reenacts those rules in its own
// statute rather than adopting 26 U.S.C. § 6654, and nothing here borrows from
// the federal package. What differs is the charge: 1% for each month or part
// of a month that an underpayment stays unpaid.
//
// The figure follows the department's forms, CT-1040ES (Rev. 01/26) for 2026
// and CT-2210 (Rev. 12/25) for 2025, and the result's Divergences say where
// § 12-722 reads otherwise. No act changed § 12-722 or § 12-701(a)(12) for
// 2026 (research/estimated-payments/ct.md, Verification, 2026 changes), so no
// form here predates the law it describes.

// jurisdiction is Connecticut's postal code.
const jurisdiction = estimated.Connecticut

// EstimatedFacts are the facts Connecticut's rules read that no other
// jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	peTaxCredit         optional
	priorSourceIncome   bool
	priorSourceAnswered bool
}

type optional struct {
	v   money.USCents
	set bool
}

// PETaxCredit is this year's Pass-Through Entity Tax Credit, Schedule CT-PE
// line 1, which CT-2210 Part 2 line 3a takes, and whether the caller gave it.
// The forms count it like withholding: against the $1,000 test, and a quarter
// of it by each due date. [estimated.Figures.Tax] is the tax before it.
func (e EstimatedFacts) PETaxCredit() (money.USCents, bool) {
	return e.peTaxCredit.v, e.peTaxCredit.set
}

// PriorYearConnecticutSourceIncome reports whether a filer who was not a
// full-year Connecticut resident last year had income from Connecticut
// sources then, and whether the caller said. The no-liability exception,
// § 12-722(j)(2), is open to such a filer only if they did (CT-2210 Box G);
// without it they must pay 90% of this year's tax (Box E).
func (e EstimatedFacts) PriorYearConnecticutSourceIncome() (had, answered bool) {
	return e.priorSourceIncome, e.priorSourceAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PETaxCredit(a money.USCents) *EstimatedFactsBuilder {
	b.e.peTaxCredit = optional{a, true}
	return b
}

func (b *EstimatedFactsBuilder) PriorYearConnecticutSourceIncome(had bool) *EstimatedFactsBuilder {
	b.e.priorSourceIncome, b.e.priorSourceAnswered = had, true
	return b
}

// Build refuses a negative credit.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.peTaxCredit.v < 0 {
		return EstimatedFacts{}, fmt.Errorf("connecticuttaxcalc: a PE Tax Credit of %v is negative", b.e.peTaxCredit.v)
	}
	return b.e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is § 12-722(j)(1): no interest when tax less withholding, and
	// on the forms less the PE Tax Credit, is under it.
	threshold money.USCents

	// thisYearShare and priorYearShare are § 12-701(a)(12): 90% of this
	// year's tax, or 100% of the tax on last year's 12-month return.
	thisYearShare, priorYearShare money.Rate

	// shares are § 12-722(d)(1), 25% each; withholding and the PE Tax Credit
	// are spread in the same parts.
	shares []money.Rate

	// annualised are the § 12-722(d)(2)(C) applicable percentages, CT-2210
	// Schedule A line 13.
	annualised []money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// fileAndPayBy is § 12-722(l): a return filed and paid in full by this
	// date takes the interest off the fourth installment. It is not moved for
	// a weekend; neither the statute nor the form says it moves.
	fileAndPayBy estimated.Date

	// stop is § 12-722(b): the interest runs no later than the fifteenth day
	// of the fourth month of the next year.
	stop estimated.Date

	// periodEnds are the CT-2210 Schedule A period ends for individuals.
	periodEnds []estimated.Date

	// monthlyRate is § 12-722(a): 1% a month or fraction of a month.
	monthlyRate money.Rate
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Connecticut's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return connecticutEstimated{y}, nil
}

type connecticutEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// installments are each installment's own amount, before excusing.
	installments []money.USCents

	// excused is the fourth installment, for a return filed and paid by
	// January 31.
	excused bool

	// pe is the PE Tax Credit, zero when not given.
	pe money.USCents

	notes working.Notes
}

func (r connecticutEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("connecticuttaxcalc: the facts carry %T, not connecticuttaxcalc.EstimatedFacts", s)
	}
}

func (r connecticutEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("connecticuttaxcalc: these are Connecticut's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("connecticuttaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: Conn. Gen. Stat. § 12-722(m) gives farmers and fishermen one installment, due January 15, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("connecticuttaxcalc: CT-2210 Schedule A period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms and § 12-722 part ways on a
// rule a Connecticut computation uses. They come from
// research/estimated-payments/ct.md, Verification, and from reading CT-2210's
// instructions against § 12-722(l). The figures follow the forms. Each
// affects flag says whether the statute's reading would change these facts'
// answer.
func (r connecticutEstimated) divergences(peTest, pePayment, early bool) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The PE Tax Credit in the $1,000 test").
			Form("The form subtracts withholding and the PE Tax Credit from your tax. It charges interest only if $1,000 or more is left.",
				"CT-2210 (Rev. 12/25), My Taxes Are Underpaid and Part 2 line 4; CT-1040ES (Rev. 01/26), Who Is Required to Make Estimated Payments and worksheet line 17").
			Statute("The law tests your tax reduced by the tax withheld. It doesn't name the PE Tax Credit. The law granting the credit doesn't make it withholding.",
				"Conn. Gen. Stat. § 12-722(j)(1); § 12-699(f)(1), as read in research/estimated-payments/ct.md").
			Uses(working.ReadingForm).Affects(peTest),
		working.NewDivergence("The PE Tax Credit as a payment by each due date").
			Form("The form counts a quarter of the PE Tax Credit as paid by each due date. It does this like withholding. That holds for the credit even if you show when your tax was actually withheld.",
				"CT-2210 (Rev. 12/25), Part 3 lines 12 to 13").
			Statute("The law treats only tax withheld as a payment of estimated tax. It counts it in equal parts on the due dates, or on the dates withheld. No law has been found that treats the PE Tax Credit as one.",
				"Conn. Gen. Stat. § 12-722(k); §§ 12-699, 12-699a, as read in research/estimated-payments/ct.md").
			Uses(working.ReadingForm).Affects(pePayment),
		working.NewDivergence("A return filed and paid by Jan 31, 2027").
			Form("Say you file and pay in full by Jan 31, 2027. The form then charges no interest after Jan 15, 2027 on any installment. That includes an earlier one still unpaid then.",
				"CT-2210 (Rev. 12/25), general instructions, \"Do not complete Schedule B, Worksheet D\"").
			Statute("The law excuses only the fourth installment's underpayment. An earlier installment's underpayment keeps bearing interest until it's paid.",
				"Conn. Gen. Stat. § 12-722(b), (l)").
			Uses(working.ReadingForm).Affects(early),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func (r connecticutEstimated) assess(f estimated.Facts) (assessment, error) {
	if err := r.check(f); err != nil {
		return assessment{}, err
	}
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment

	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	pe, peKnown := extras.PETaxCredit()
	a.pe = pe
	credited := withheld + pe
	prior, hasPrior := f.PriorYear()
	last := y.year - 1
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
	}

	// § 12-701(a)(12)(A): 90% of this year's tax.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("Conn. Gen. Stat. § 12-701(a)(12)(A); CT-1040ES (Rev. 01/26), worksheet line 13",
			fmt.Sprintf("%v of this year's Connecticut tax of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// § 12-701(a)(12)(B): 100% of last year's tax, for a filed 12-month
	// return. There is no higher percentage at any income.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Connecticut return", last))
	case prior.NotRequiredToFile() && noSourceIncome(prior, extras):
		priorOption = unavailable(fmt.Sprintf("You had no Connecticut income in %d and filed no Connecticut return", last))
	case prior.NotRequiredToFile():
		priorOption = unavailable(fmt.Sprintf("You didn't need to file a %d Connecticut return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Connecticut return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d return covered %d months, not 12", last, prior.Months()))
	default:
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, y.priorYearShare.Of(priorTax))
		a.notes.Rule("Conn. Gen. Stat. § 12-701(a)(12)(B); CT-1040ES (Rev. 01/26), worksheet line 14",
			fmt.Sprintf("%v of the %v tax on the 12-month %d return is %v; Connecticut has no higher percentage at higher income", y.priorYearShare, priorTax, last, y.priorYearShare.Of(priorTax)))
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
	// § 12-722(j)(2): no liability last year, a 12-month year, as a resident
	// or with Connecticut-source income. CT-2210 Boxes F and G for a filer who
	// filed nothing; a filed return showing no tax gives a prior-year target
	// of nothing, which comes to the same.
	noLiability := hasPrior && prior.Months() == 12 &&
		(prior.NotRequiredToFile() || (prior.Filed() && priorTax == 0))
	exception, box := false, ""
	if noLiability {
		resident, residenceAnswered := prior.FullYearResident()
		source, sourceAnswered := extras.PriorYearConnecticutSourceIncome()
		switch {
		case !residenceAnswered && !prior.NotRequiredToFile():
			// A filed return showing no tax already gives a target of nothing.
		case !residenceAnswered:
			a.notes.Ask(working.PositionResidency, fmt.Sprintf("Did you live in Connecticut all of %d, or have Connecticut income that year?", last),
				fmt.Sprintf("If either is true, no Connecticut tax for %d means no interest for %d.", last, y.year))
		case resident:
			exception, box = true, "Box F"
		case !sourceAnswered && !prior.NotRequiredToFile():
		case !sourceAnswered:
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did you have any Connecticut income in %d?", last),
				fmt.Sprintf("If you did, having no Connecticut tax for %d means no interest for %d.", last, y.year))
		case source:
			exception, box = true, "Box G"
		case prior.NotRequiredToFile():
			a.notes.Rule("Conn. Gen. Stat. § 12-722(j)(2); CT-2210 (Rev. 12/25), Part 1, Box E",
				fmt.Sprintf("a %d nonresident or part-year resident with no Connecticut-source income gets no no-liability exception, and must pay %v of this year's tax", last, y.thisYearShare))
		}
	}

	creditWords, exceptionWhat := "withholding", "Tax less withholding"
	if pe > 0 {
		creditWords, exceptionWhat = "withholding and PE Tax Credit", "Tax less withholding and PE Tax Credit"
	}
	// taxLeaves is this year's tax less what is credited against it. With a PE
	// Tax Credit it takes two sentences, to keep each under 80 characters.
	taxLeaves := func() string {
		return fmt.Sprintf("Your Connecticut tax is %s. After %s of %s, it %s.", tax.Whole(), credited.Whole(), creditWords, estimated.Leaves(tax-credited))
	}
	switch {
	case exception:
		a.exempt = true
		none, err := estimated.NewException("No Connecticut tax last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no Connecticut tax for %d, a full 12-month year. That means no estimated payments are needed for %d.", last, y.year)).Exempt(none)
		a.notes.Rule("Conn. Gen. Stat. § 12-722(j)(2); CT-2210 (Rev. 12/25), Part 1, "+box,
			fmt.Sprintf("no Connecticut liability for the 12 months of %d, so there is no interest for %d", last, y.year))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("With no %d Connecticut return, your %d tax must be figured to set a target.", last, y.year)).NoTarget()
	case !taxKnown && credited >= target:
		rb.Required(false, fmt.Sprintf("Your target is %v of your %d Connecticut tax, %s. Your %s of %s already reaches it. No estimated payments are needed, whatever your %d tax turns out to be.",
			priorOpt.Percent(), last, target.Whole(), creditWords, credited.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d Connecticut tax, %s. Reaching it keeps you clear of interest, whatever your %d tax is. After %s of %s, %s is left. Pay it in estimated payments, a quarter by each date.",
			priorOpt.Percent(), last, target.Whole(), y.year, credited.Whole(), creditWords, (target-credited).Whole())).Choose(chosen)
	case tax-credited < y.threshold:
		a.exempt = true
		below, err := estimated.ExceptionBelow(exceptionWhat, tax-credited, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		under := fmt.Sprintf(" That is under %s, so no estimated payments are needed.", y.threshold.Whole())
		if tax < credited {
			under = " No estimated payments are needed."
		}
		rb.Required(false, taxLeaves()+under).Exempt(below)
		a.notes.Rule("Conn. Gen. Stat. § 12-722(j)(1); CT-2210 (Rev. 12/25), Part 2 line 4",
			fmt.Sprintf("%v of tax less %v withheld and %v of PE Tax Credit is %v, under %v, so there is no interest", tax, withheld, pe, tax-credited, y.threshold))
		if tax-withheld >= y.threshold {
			a.notes.Ask(working.PositionUnsettledRule, "",
				fmt.Sprintf("The forms count your %s PE Tax Credit in the %s test. The law may not, and the figures follow the forms.", pe.Whole(), y.threshold.Whole()))
		}
	case credited >= target:
		rb.Required(false, fmt.Sprintf("You have %s of Connecticut %s. That already reaches the %s needed for the year. No estimated payments are needed.", credited.Whole(), creditWords, target.Whole())).Choose(chosen)
	default:
		rb.Required(true, taxLeaves()+fmt.Sprintf(" That is %s or more, so estimated payments are needed. To reach your %s target, pay %s, a quarter by each date.",
			y.threshold.Whole(), target.Whole(), (target-credited).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}

	// These answers change the target. An exception sets the target, so they
	// are asked only when none applies.
	if !a.exempt && !hasPrior {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Connecticut return, and what was its tax?", last),
			fmt.Sprintf("With it, %v of last year's tax can be the target if under %v of this year's.", y.priorYearShare, y.thisYearShare))
	}
	if !a.exempt && !taxKnown {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Connecticut tax to be?", y.year),
			fmt.Sprintf("For now, %s. The %s exception is open.", map[bool]string{true: "only last year's tax sets the target", false: "there's no target"}[a.requirement.TargetKnown()], y.threshold.Whole()))
	}
	if !a.exempt {
		if !peKnown {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Do you have a %d Pass-Through Entity Tax Credit, and how much is it?", y.year),
				"With it, the credit counts like withholding and can lower what you need to pay.")
		} else if pe > 0 {
			a.notes.Ask(working.PositionUnsettledRule, "",
				fmt.Sprintf("The forms count your %s PE Tax Credit as a payment on each due date. The law may not, and the figures follow the forms.", pe.Whole()))
		}
		for _, p := range f.Payments() {
			if p.FromPriorYear() {
				a.notes.Ask(working.PositionUnsettledRule, "",
					fmt.Sprintf("Neither Connecticut form says when your %s overpayment from %d counts. The figures follow the law, counting it on the first due date.", p.Amount().Whole(), last))
			}
		}
	}

	// The due dates. § 12-722(c) fixes them; CT-1040ES moves one that falls
	// on a Saturday, Sunday or legal holiday to the next business day.
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			a.notes.Rule("CT-1040ES (Rev. 01/26), When to File", fmt.Sprintf("the installment due %s falls on a weekend or legal holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
		}
	}
	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), y.shares)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("Conn. Gen. Stat. § 12-722(c), (d)(1); CT-1040ES (Rev. 01/26), When to File and worksheet line 18",
		fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", a.requirement.Target(), a.installments[0],
			a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() && !a.exempt {
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
		a.notes.Rule("Conn. Gen. Stat. § 12-722(d)(2); CT-2210 (Rev. 12/25), Schedule A", fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: %v, %v, %v and %v of the annualised tax, "+
			"less earlier installments, and never more than the regular quarter plus any reduction not yet recaptured", ann[0], ann[1], ann[2], ann[3],
			y.annualised[0], y.annualised[1], y.annualised[2], y.annualised[3]))
	}

	// § 12-722(l): filing and paying in full by January 31 takes the
	// interest off the fourth installment. January 31, 2027 is a Sunday; the
	// statute and CT-1040ES print January 31 and say nothing of moving it, so
	// it is not moved.
	if filed := f.ReturnFiled(); !filed.IsZero() && !filed.After(y.fileAndPayBy) && !a.exempt {
		a.excused = true
		a.notes.Rule("Conn. Gen. Stat. § 12-722(l); CT-1040ES (Rev. 01/26), Interest", fmt.Sprintf("the return was filed and paid in full on %s, by %s, so the January installment bears no interest", filed.Long(), y.fileAndPayBy.Long()))
	}

	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	// The statute's reading of the PE Tax Credit changes the $1,000 test only
	// when the credit is what brings the balance under it, and the payments
	// by each date whenever there is a credit and a schedule. The January 31
	// rule matters only to a return filed and paid by then.
	peTest := pe > 0 && taxKnown && !exception && (tax-credited < y.threshold) != (tax-withheld < y.threshold)
	divergences, err := r.divergences(peTest, pe > 0 && !exception, a.excused)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

func (r connecticutEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// credits are withholding, the PE Tax Credit and payments, dated as CT-2210
// Part 3 counts them: withholding in equal parts on the due dates unless the
// filer elects the dates withheld (Box C), and the PE Tax Credit in equal parts
// either way.
func (r connecticutEstimated) credits(f estimated.Facts, a assessment) ([]estimated.Payment, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, r.y.shares)
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("Conn. Gen. Stat. § 12-722(k); CT-2210 (Rev. 12/25), Part 1 Box C and Part 3 line 11b",
			fmt.Sprintf("by election, the %v withheld is credited on the dates it was withheld", f.WithheldTotal()))
	} else {
		notes.Rule("Conn. Gen. Stat. § 12-722(k); CT-2210 (Rev. 12/25), Part 3 lines 11 to 11b",
			fmt.Sprintf("the %v withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld", f.WithheldTotal()))
	}
	if a.pe > 0 {
		cum, err := estimated.CumulativeShares(a.pe, r.y.shares)
		if err != nil {
			return nil, working.Notes{}, err
		}
		for i, part := range estimated.Increments(cum) {
			if part == 0 {
				continue
			}
			p, err := estimated.NewPayment().On(a.dues[i]).Amount(part).Build()
			if err != nil {
				return nil, working.Notes{}, err
			}
			credits = append(credits, p)
		}
		notes.Rule("CT-2210 (Rev. 12/25), Part 3 lines 12 to 13", fmt.Sprintf("the %v PE Tax Credit is treated as paid in four equal parts on the due dates", a.pe))
	}
	sort.SliceStable(credits, func(i, j int) bool { return credits[i].On().Before(credits[j].On()) })
	return credits, notes, nil
}

// required is each installment's amount, with the fourth set to nothing when
// filing early excused it.
func (a assessment) required() []money.USCents {
	out := append([]money.USCents(nil), a.installments...)
	if a.excused {
		out[3] = 0
	}
	return out
}

func (r connecticutEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Connecticut return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	out, notes, err := r.schedule(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments and the notes on how they were credited.
func (r connecticutEstimated) schedule(f estimated.Facts, a assessment) ([]estimated.Installment, working.Notes, error) {
	credits, notes, err := r.credits(f, a)
	if err != nil {
		return nil, working.Notes{}, err
	}
	ledger, err := estimated.Apply(a.dues, a.required(), credits)
	if err != nil {
		return nil, working.Notes{}, err
	}
	out := make([]estimated.Installment, len(ledger))
	var needed money.USCents
	for i, l := range ledger {
		needed += a.installments[i]
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(needed).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if a.excused && i == 3 {
			b.Excused()
		}
		if out[i], err = b.Build(); err != nil {
			return nil, working.Notes{}, err
		}
	}
	return out, notes, nil
}

// Penalty figures the interest as CT-2210 does, one line per column of Part 3.
//
// A column's shortfall is its Line 16: everything required by its due date
// less everything credited by then, so it carries any earlier installment
// still unpaid. Schedule B charges it 1% for each month from the due date to
// the next one, or to April 15 for the last, a month running from the 16th
// to the 15th. A month in which any part is still unpaid is charged in full,
// on what was unpaid at its start; a payment during a month lowers the next
// month's charge. That is § 12-722(a) and (b) installment by installment, with
// payments applied oldest first, because every 2026 due date falls on a 15th.
// The rate is set by statute, so no month lacks one.
func (r connecticutEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("connecticuttaxcalc: interest needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Connecticut return to measure against, there is no target to figure interest from", estimated.ErrNeedsThisYearTax, r.y.year-1)
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
	schedule, notes, err := r.schedule(f, a)
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

	credits, _, err := r.credits(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	required := a.required()
	source := "Conn. Gen. Stat. § 12-722(a): 1% per month or fraction thereof"
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
	var needed money.USCents
	carried := false
	for k, due := range a.dues {
		needed += required[k]
		if a.excused && k == 3 {
			// Worksheet D is not completed.
			continue
		}
		var paid money.USCents
		for _, c := range credits {
			if !c.On().After(due) {
				paid += c.Amount()
			}
		}
		under := needed - paid
		if under <= 0 {
			continue
		}
		end := y.stop
		if k+1 < len(a.dues) {
			end = a.dues[k+1]
		}
		lb := estimated.NewPenaltyLine(k+1, due, under)
		base, lastDay := under, due
		for j := 0; base > 0; j++ {
			from, to := due.AddMonths(j), due.AddMonths(j+1)
			if !from.Before(end) || !from.Before(stop) {
				break
			}
			part, err := estimated.Monthly(base, from, to, y.monthlyRate, source)
			if err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
			lb.Part(part)
			lastDay = earlier(to, stop)
			for _, c := range credits {
				if c.On().After(from) && !c.On().After(to) && base > 0 {
					base -= c.Amount()
					if base <= 0 {
						base, lastDay = 0, earlier(c.On(), stop)
					}
				}
			}
		}
		if a.excused && k == 2 && base > 0 {
			carried = true
		}
		if lastDay == due {
			continue
		}
		line, err := lb.Days(lastDay.DaysSince(due)).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		pb.Line(line)
	}
	p, err := pb.Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("Conn. Gen. Stat. § 12-701(a)(12)", fmt.Sprintf("this year's tax was not given, so the interest is figured against %v of the %d tax alone; "+
			"the required annual payment is the lesser of that and %v of this year's tax, so %v is the most the interest can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, y.thisYearShare, p.Total()))
	}
	if paidInFull {
		notes.Rule("Conn. Gen. Stat. § 12-722(b); CT-2210 (Rev. 12/25), Form CT-2210 Instructions", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every underpayment still outstanding was paid that day", stop.Long()))
	}
	if carried {
		notes.Rule("CT-2210 (Rev. 12/25), general instructions", "the return was filed and paid by January 31, so Schedule B Worksheet D is not completed and nothing is charged after January 15, on any installment")
	}
	notes.Rule("Conn. Gen. Stat. § 12-722(a), (b); CT-2210 (Rev. 12/25), Part 3 lines 8 to 17 and Schedule B", fmt.Sprintf("each due date's underpayment, after withholding, the PE Tax Credit and payments made by then, is charged %v for each month or part of a month, "+
		"a month running from the 16th to the 15th, until paid or %s; a payment during a month lowers the next month's charge; the interest to %s is %v",
		y.monthlyRate, y.stop.Long(), stop.Long(), p.Total()))
	switch {
	case waiverAnswered && waiver != estimated.WaiverNone:
		notes.Rule("Conn. Gen. Stat. § 12-722; CT-2210 (Rev. 12/25)", fmt.Sprintf("neither § 12-722 nor CT-2210 provides a waiver for %v; the interest shown is not reduced", waiver))
		if p.Total() > 0 {
			if p, err = p.ToBuilder().WaiverOutcome(connecticutWaiver(waiver)).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	case !waiverAnswered && p.Total() > 0:
		notes.Ask(working.PositionPenaltyWaiver, "Do you want interest waived for a casualty, disaster, retirement or disability?",
			"Connecticut's estimated tax rules give no waiver for these. The interest shown stands unless another law lets the department waive it.")
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// connecticutWaiver refuses the waiver asked for: neither § 12-722 nor the
// CT-2210 instructions provide one for a casualty, disaster, retirement or
// disability.
func connecticutWaiver(k estimated.WaiverKind) estimated.WaiverOutcome {
	if k == estimated.WaiverCasualty {
		return estimated.WaiverRefused("There is no waiver of the interest for a casualty or disaster. Neither Connecticut's estimated tax law nor Form CT-2210 gives one.")
	}
	return estimated.WaiverRefused("There is no waiver of the interest for retirement or disability. Neither Connecticut's estimated tax law nor Form CT-2210 gives one.")
}

// noSourceIncome reports a prior year spent outside Connecticut, or partly
// outside it, with no Connecticut income: CT-2210 Box E.
func noSourceIncome(prior estimated.PriorReturn, extras EstimatedFacts) bool {
	resident, residenceAnswered := prior.FullYearResident()
	source, sourceAnswered := extras.PriorYearConnecticutSourceIncome()
	return prior.Months() == 12 && residenceAnswered && !resident && sourceAnswered && !source
}

func earlier(a, b estimated.Date) estimated.Date {
	if a.Before(b) {
		return a
	}
	return b
}

func (r connecticutEstimated) Explanation() estimated.Explanation { return connecticutExplanation }

// connecticutExplanation is written by hand and checked against
// research/estimated-payments/ct.md.
var connecticutExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Connecticut expects estimated payments if your tax will be $1,000 or more. The tax is counted after withholding and any Pass-Through Entity Tax Credit. Paying too little by a due date costs interest of 1% a month on the shortfall.").
		Paragraph("There is no interest if your payments together reach the smaller of two figures. One is 90% of this year's Connecticut tax. The other is 100% of the tax on last year's Connecticut return. Withholding and the Pass-Through Entity Tax Credit count as payments. Last year's figure needs a return that covered twelve months. Unlike the federal rules, there is no 110% figure at higher incomes.").
		Paragraph("Pay a quarter by each of Apr 15, Jun 15, Sep 15 and Jan 15 of the next year. A date on a weekend or legal holiday becomes the next business day. Withholding counts as paid evenly across the four dates. You can instead show the dates it was actually withheld. The Pass-Through Entity Tax Credit is always spread evenly. Filing and paying in full by January 31 clears interest on the January payment.").
		Paragraph("Withholding and the credit are taken off this year's tax. There is no interest if under $1,000 is left. Nor is there any if you lived in Connecticut all last year and had no tax there.").
		Paragraph("Connecticut calls the charge interest rather than a penalty. It is 1% for each month or part of a month that a shortfall stays unpaid. Months run from the 16th to the 15th, so a day late is charged a whole month. The rate is fixed by law and doesn't follow market rates. It stops when the shortfall is paid, and no later than April 15 of next year. With uneven income, Form CT-2210's annualised method can lower early payments. Connecticut's rules have no waiver for a casualty or disaster. Nor do they have one for retirement or disability.").
		VersusFederal("Connecticut charges 1% for each month or part of one, about 12% a year. Federal interest is a quarterly rate counted by the day. There is no 110% figure at higher income. The Pass-Through Entity Tax Credit counts like withholding. There is no waiver for a casualty, disaster, retirement or disability. The $1,000 threshold, the 90% and 100% tests and four quarters match federal.").
		Source("Conn. Gen. Stat. §§ 12-701(a)(12), 12-722").
		Source("Form CT-1040ES, 2026 Estimated Connecticut Income Tax Payment Coupon for Individuals (Rev. 01/26)").
		Source("Form CT-2210, Underpayment of Estimated Income Tax by Individuals, Trusts, and Estates, and its instructions (Rev. 12/25)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
