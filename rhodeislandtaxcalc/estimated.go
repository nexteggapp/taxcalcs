package rhodeislandtaxcalc

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Rhode Island's estimated tax rules, R.I. Gen. Laws §§ 44-30-55, 44-30-56
// and 44-30-84, as the shared [estimated.EstimatedTax] interface.
//
// The shape is federal: the smaller of a share of this year's tax and all of
// last year's, four equal installments, simple interest on each shortfall.
// The numbers are not: 80% rather than 90%, 100% of last year's tax at every
// income, a $250 floor, and a rate set once for each calendar year, 12% for
// 2026. Due dates move for Rhode Island's own legal holidays.
//
// The figure follows the Division of Taxation's forms, the 2026 RI-1040ES and
// the 2025 RI-2210, the latest published. The result's Divergences say where
// the General Laws read otherwise. No law in 2024, 2025 or 2026 amended the
// sections these rules rest on (research/estimated-payments/ri.md), so no
// form here predates the law it describes.

// jurisdiction is Rhode Island's postal code.
const jurisdiction = estimated.RhodeIsland

// JanuaryReturn is the reading used for a return filed early in place of the
// January installment. The Division's two current forms give different
// dates, and no public source says which one its computation applies.
type JanuaryReturn int

const (
	// JanuaryReturnUnanswered is no answer. The rules then use
	// JanuaryReturnPenaltyForm and say so in Required when the answer would
	// change the figure.
	JanuaryReturnUnanswered JanuaryReturn = iota

	// JanuaryReturnPenaltyForm is the RI-2210 reading: a return filed with
	// the balance paid on or before January 31 counts in column D, as paid by
	// the January due date. RI-2210 (2025), line 16 instructions.
	JanuaryReturnPenaltyForm

	// JanuaryReturnCoupon is the RI-1040ES reading: the January payment is
	// not needed if the return is filed and paid by February 15. That is the
	// fifteenth day of the second month in § 44-30-55(e), and § 44-30-91(b)
	// moves it past a weekend or Rhode Island legal holiday.
	JanuaryReturnCoupon
)

func (j JanuaryReturn) String() string {
	switch j {
	case JanuaryReturnPenaltyForm:
		return "RI-2210, January 31"
	case JanuaryReturnCoupon:
		return "RI-1040ES, February 15"
	default:
		return "unanswered"
	}
}

// EstimatedFacts are the facts and readings Rhode Island's rules read that no
// other jurisdiction does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	noFederalLiability         bool
	noFederalLiabilityAnswered bool
	january                    JanuaryReturn
	minimum                    bool
	minimumAnswered            bool
}

// PriorYearNoFederalLiability reports whether last year's federal return, for
// a full twelve months, showed no federal tax liability, or would have had one
// been required, and whether the caller said. RI-2210 line 5 lets a filer who
// was also a Rhode Island resident all last year enter zero for last year's
// tax. [estimated.Facts.PriorYear] is last year's Rhode Island return, which
// does not carry it.
func (e EstimatedFacts) PriorYearNoFederalLiability() (none, answered bool) {
	return e.noFederalLiability, e.noFederalLiabilityAnswered
}

// JanuaryReturn is the reading to use for a return filed and paid between
// the two forms' dates.
func (e EstimatedFacts) JanuaryReturn() JanuaryReturn { return e.january }

// TwoDollarMinimum is the caller's reading of whether § 44-30-84(a)(1), which
// makes interest under $2.00 not payable, reaches the interest on estimates,
// and whether the caller gave one.
func (e EstimatedFacts) TwoDollarMinimum() (applies, answered bool) {
	return e.minimum, e.minimumAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorYearNoFederalLiability(none bool) *EstimatedFactsBuilder {
	b.e.noFederalLiability, b.e.noFederalLiabilityAnswered = none, true
	return b
}

func (b *EstimatedFactsBuilder) JanuaryReturn(j JanuaryReturn) *EstimatedFactsBuilder {
	b.e.january = j
	return b
}

func (b *EstimatedFactsBuilder) TwoDollarMinimum(applies bool) *EstimatedFactsBuilder {
	b.e.minimum, b.e.minimumAnswered = applies, true
	return b
}

// Build refuses a reading this package does not know.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) {
	if b.e.january < JanuaryReturnUnanswered || b.e.january > JanuaryReturnCoupon {
		return EstimatedFacts{}, fmt.Errorf("rhodeislandtaxcalc: January reading %d is not one this package knows", b.e.january)
	}
	return b.e, nil
}

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// floor is RI-2210 line 4: no charge when this year's tax less
	// withholding is this or less.
	floor money.USCents

	thisYearShare, priorYearShare money.Rate

	dues     []estimated.Date
	calendar estimated.Calendar

	// penaltyFormBy is the RI-2210 date for a return that counts in column
	// D. couponBy is the RI-1040ES date, before moving for a holiday.
	penaltyFormBy, couponBy estimated.Date

	// stop is the fifteenth day of the fourth month after the year.
	stop estimated.Date

	periodEnds []estimated.Date
	applicable []money.Rate
	rates      estimated.RateSeries

	// minimum is § 44-30-84(a)(1): interest under it is not payable.
	minimum money.USCents
}

var estimatedYears = map[int]estimatedYear{}

// registerEstimated panics on a rate period that is not one calendar year.
// § 44-1-7(b) sets the rate each January 1 for that calendar year.
func registerEstimated(y estimatedYear) {
	for _, p := range y.rates.Periods() {
		jan1 := estimated.MustDate(p.From().Year(), time.January, 1)
		dec31 := estimated.MustDate(p.From().Year(), time.December, 31)
		if p.From() != jan1 || p.To() != dec31 {
			panic(fmt.Sprintf("rhodeislandtaxcalc: the rate from %v to %v is not one calendar year", p.From(), p.To()))
		}
	}
	estimatedYears[y.year] = y
}

// quarters are RI-2210 line 13, the required annual payment divided by four,
// and line 16, withholding deemed paid one-fourth on each due date.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Rhode Island's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return rhodeIslandEstimated{y}, nil
}

type rhodeIslandEstimated struct{ y estimatedYear }

type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool
	dues        []estimated.Date

	// installments are RI-2210 line 15: line 13, or line 14 under the
	// annualised method.
	installments []money.USCents

	// januaryMet is a return filed and paid in time to count in column D.
	januaryMet bool

	// affects says which divergences change these facts' figures.
	affects affects

	notes working.Notes
}

// affects records, for each divergence, whether its two readings give these
// facts different figures or answers.
type affects struct {
	priorOption, floor, noLiability, january, lateStart, minimum bool

	// minimumApplied is the statute's $2 minimum, applied by the reading
	// given.
	minimumApplied bool
}

func (r rhodeIslandEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("rhodeislandtaxcalc: the facts carry %T, not rhodeislandtaxcalc.EstimatedFacts", s)
	}
}

func (r rhodeIslandEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("rhodeislandtaxcalc: these are Rhode Island's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("rhodeislandtaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher():
		return fmt.Errorf("%w: a farmer or fisherman uses 66.67%% and column D alone, and owes nothing after filing and paying by March 1 (RI-2210 instructions), which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("rhodeislandtaxcalc: RI-2210 Part 5 period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the Division's forms and the General Laws part ways
// on a rule every Rhode Island computation uses. Items 10 and "Conflicts
// resolved" in research/estimated-payments/ri.md cover all but the late
// start, which is § 44-30-56(a) read against RI-2210 line 13.
func (r rhodeIslandEstimated) divergences(af affects) ([]working.Divergence, error) {
	minimum := working.ReadingForm
	if af.minimumApplied {
		minimum = working.ReadingStatute
	}
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("The prior-year option").
			Form("Your target is the smaller of two figures. One is 80% of this year's tax; the other, 100% of last year's Rhode Island tax. There is no condition of a full year or of residence.",
				"RI-2210 (2025), Part 1, lines 2, 5 and 6; RI-1040ES (2026), worksheet lines 6 and 7").
			Statute("Interest is charged when payments fall short of 80% of each installment. The law adopts only the federal small-tax and no-liability exceptions. It creates no prior-year option.",
				"R.I. Gen. Laws § 44-30-84(b)").
			Uses(working.ReadingForm).Affects(af.priorOption),
		working.NewDivergence("The $250 floor").
			Form("This year's tax less Rhode Island withholding of $250 or less means no charge.",
				"RI-2210 (2025) and RI-2210A (2025), line 4").
			Statute("Estimated payments are required when the tax is $250 or more above credits. Exactly $250 therefore needs them. The law adopts the federal small-tax exception. It doesn't say whether that amount is $250 or the federal $1,000.",
				"R.I. Gen. Laws §§ 44-30-55(a), 44-30-84(b)").
			Uses(working.ReadingForm).Affects(af.floor),
		working.NewDivergence("No liability last year").
			Form("Last year's tax counts as zero if you had no federal tax liability for it. That needs a full-year federal return and Rhode Island residence all year.",
				"RI-2210 (2025) and RI-2210A (2025), line 5 instructions").
			Statute("The federal no-liability exception applies to Rhode Island amounts. The test is then no Rhode Island liability last year. The 100% option already sets a zero target when last year's tax was zero.",
				"R.I. Gen. Laws § 44-30-84(b)").
			Uses(working.ReadingForm).Affects(af.noLiability),
		working.NewDivergence("A return filed in place of the January installment").
			Form("A return filed and paid on or before January 31 counts as paid by January 15. January 31 doesn't move for a weekend. The payment coupon says February 15, moved for weekends and state holidays.",
				"RI-2210 (2025), line 16 instructions; RI-1040ES (2026), When and where to make estimates").
			Statute("The return counts as the declaration if filed and paid by a set date. That date is the fifteenth day of the second month after the year. It doesn't count if one was required earlier. Feb 15, 2027 is Washington's Birthday, so Feb 16 is on time. A missed January payment's interest isn't waived.",
				"R.I. Gen. Laws §§ 44-30-55(e), 44-30-91(b), 25-1-1").
			Uses(working.ReadingForm).Affects(af.january),
		working.NewDivergence("A requirement first met after April 15").
			Form("The required annual payment is divided by four. A quarter is due on each date, whenever the requirement arose.",
				"RI-2210 (2025), Part 3, line 13").
			Statute("Installments follow the date the declaration was due. If due June 15, pay equal installments on June 15, September 15 and January 15. If due September 15, pay on September 15 and January 15. If due January 15, pay all at once.",
				"R.I. Gen. Laws §§ 44-30-55(c), 44-30-56(a)").
			Uses(working.ReadingForm).Affects(af.lateStart),
		working.NewDivergence("Interest under $2").
			Form("Every charge is payable, however small. The penalty form has no minimum.",
				"RI-2210 (2025), Part 4, lines 18 to 22").
			Statute("Interest under $2 is not payable. That sentence is in the subsection on tax paid late. The subsection on estimates doesn't repeat it.",
				"R.I. Gen. Laws § 44-30-84(a)(1), (b)").
			Uses(minimum).Affects(af.minimum),
	} {
		d, err := b.Build()
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// date is d as a person reads it: "Dec 15" in the tax year, "Jan 15, 2027"
// outside it.
func (r rhodeIslandEstimated) date(d estimated.Date) string {
	if d.Year() == r.y.year {
		return d.Time().Format("Jan 2")
	}
	return d.Time().Format("Jan 2, 2006")
}

func (r rhodeIslandEstimated) assess(f estimated.Facts) (assessment, error) {
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
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			a.notes.Rule("R.I. Gen. Laws § 44-30-91(b)", fmt.Sprintf("the installment due %s falls on a weekend or Rhode Island legal holiday, so it is due %s", y.dues[i].Long(), a.dues[i].Long()))
		}
	}

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

	// 80% of this year's tax, RI-2210 line 2.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, y.thisYearShare.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearShare, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("RI-2210 (2025), Part 1, lines 1 and 2", fmt.Sprintf("%v of this year's Rhode Island tax after credits of %v is %v", y.thisYearShare, tax, thisYear.Amount()))
	}

	// 100% of last year's Rhode Island tax, RI-2210 line 5. The form sets no
	// condition on the return beyond its tax.
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Rhode Island return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Rhode Island return", last))
	default:
		amount := y.priorYearShare.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorYearShare, amount)
		a.notes.Rule("RI-2210 (2025), Part 1, line 5; RI-1040ES (2026), worksheet line 6", fmt.Sprintf("%v of the %v tax on the %d Rhode Island return is %v", y.priorYearShare, priorTax, last, amount))
		if prior.Months() != 12 {
			a.notes.Rule("RI-2210 (2025), Part 1, line 5", fmt.Sprintf("the %d return covered %d months; the form sets no condition of a twelve-month year on line 5, so the option stands", last, prior.Months()))
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

	// RI-2210 line 5: zero for last year's tax after a year with no federal
	// liability, as a Rhode Island resident, on a twelve-month federal return.
	noFederal, noFederalAnswered := extras.PriorYearNoFederalLiability()
	resident, residenceAnswered := prior.FullYearResident()
	noLiability := noFederal && hasPrior && residenceAnswered && resident
	if noFederal && (!hasPrior || !residenceAnswered) {
		a.notes.Ask(working.PositionResidency, fmt.Sprintf("Did you live in Rhode Island all of %d?", last),
			fmt.Sprintf("If so, no federal tax liability for %d means no estimated payments are needed.", last))
	}

	switch {
	case noLiability:
		a.exempt = true
		ex, err := estimated.NewException("No federal tax liability last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no federal tax liability for %d and lived in Rhode Island all of it. Last year's tax therefore counts as zero. No estimated payments are needed for %d.", last, y.year)).Exempt(ex)
		a.notes.Rule("RI-2210 (2025), Part 1, lines 5 and 6", fmt.Sprintf("no federal liability for %d on a twelve-month return, and a full year of Rhode Island residence, so line 5 is zero and there is no charge for %d", last, y.year))
	case taxKnown && tax-withheld <= y.floor:
		a.exempt = true
		ex, err := estimated.ExceptionAtMost("Tax less withholding", tax-withheld, y.floor)
		if err != nil {
			return assessment{}, err
		}
		why := fmt.Sprintf("Your Rhode Island tax after credits is %s, and %s is withheld. That leaves %s, which is %s or less. No estimated payments are needed.",
			tax.Whole(), withheld.Whole(), (tax - withheld).Whole(), y.floor.Whole())
		switch {
		case tax < withheld:
			why = fmt.Sprintf("Your Rhode Island tax after credits is %s, and %s is withheld. That %s. No estimated payments are needed.",
				tax.Whole(), withheld.Whole(), estimated.Leaves(tax-withheld))
		case tax-withheld == y.floor:
			why = fmt.Sprintf("Your Rhode Island tax after credits is %s, and %s is withheld. That leaves exactly %s. The law asks for estimated payments at %s or more. The penalty form charges nothing at %s or less.",
				tax.Whole(), withheld.Whole(), y.floor.Whole(), y.floor.Whole(), y.floor.Whole())
		}
		rb.Required(false, why).Exempt(ex)
		a.notes.Rule("RI-2210 (2025), Part 1, line 4", fmt.Sprintf("%v of tax less %v withheld is %v, not more than %v, so there is no charge", tax, withheld, max(0, tax-withheld), y.floor))
	case !taxKnown && !priorOpt.Available():
		why := fmt.Sprintf("Your target is the smaller of two figures. They are 80%% of your %d Rhode Island tax and 100%% of your %d tax.", y.year, last)
		if hasPrior && !prior.Filed() {
			why = fmt.Sprintf("Your target is 80%% of your %d Rhode Island tax. That is because you didn't file a %d return.", y.year, last)
		}
		rb.Required(false, why).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s covers 100%% of your %d tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is 100%% of your %d tax, %s. It keeps you clear of the charge whatever your %d tax turns out to be. After %s of withholding, that's %s of estimated payments. Pay a quarter by each date.",
			last, target.Whole(), y.year, withheld.Whole(), (target-withheld).Whole())).Choose(chosen)
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Rhode Island withholding is %s. That covers the %s needed for the year. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Rhode Island tax after credits is %s, and %s is withheld. That leaves %s due, and withholding falls short of your %s target. You need estimated payments of %s, a quarter by each date.",
			tax.Whole(), withheld.Whole(), (tax-withheld).Whole(), target.Whole(), (target-withheld).Whole())).Choose(chosen)
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	// An exception sets the target, so neither figure can change it.
	if !hasPrior && !a.exempt {
		a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Rhode Island return, and what was its tax?", last),
			"With it, 100% of last year's tax could set a lower target.")
	}
	if !taxKnown && !a.exempt {
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Rhode Island tax after credits to be?", y.year),
			fmt.Sprintf("With it, 80%% of this year's tax could be your target. A balance of %s or less would need no payments.", y.floor.Whole()))
	}
	if !a.exempt && a.requirement.TargetKnown() && !noFederalAnswered {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Did you have no federal tax liability for %d, on a full-year return?", last),
			"If so, you also need to have lived in Rhode Island all that year. Then no estimated payments are needed.")
	}

	// What each divergence turns on in these facts.
	balance := tax - withheld
	a.affects.priorOption = !a.exempt && a.requirement.TargetKnown() && chosen == estimated.BasisPriorYearTax
	a.affects.floor = taxKnown && !noLiability && balance >= y.floor && balance < 1_000*money.Dollar
	a.affects.noLiability = noLiability && priorTax > 0
	if met := f.RequirementFirstMet(); !a.exempt && !met.IsZero() && met.After(y.dues[0]) {
		a.affects.lateStart = true
	}

	if a.exempt || !a.requirement.TargetKnown() {
		a.installments = make([]money.USCents, len(a.dues))
		return a, nil
	}
	if a.installments, err = r.installments(f, &a, target); err != nil {
		return assessment{}, err
	}
	r.january(f, extras, &a)
	return a, nil
}

// installments are RI-2210 line 15: line 13, the required annual payment
// divided by four, or line 14 under the annualised method.
func (r rhodeIslandEstimated) installments(f estimated.Facts, a *assessment, target money.USCents) ([]money.USCents, error) {
	y := r.y
	cum, err := estimated.CumulativeShares(target, quarters)
	if err != nil {
		return nil, err
	}
	regular := estimated.Increments(cum)
	a.notes.Rule("R.I. Gen. Laws § 44-30-56(a); RI-1040ES (2026); RI-2210 (2025), Part 3, line 13", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s",
		target, regular[0], a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))
	if met := f.RequirementFirstMet(); !met.IsZero() && met.After(y.dues[0]) {
		a.notes.Rule("RI-2210 (2025), Part 3, line 13", fmt.Sprintf("the requirement first arose on %s; the form still requires a quarter on each of the four dates, so the earlier ones are short until caught up (see Divergences for § 44-30-56(a))", met.Long()))
	}
	if !f.Elections().Annualised() {
		return regular, nil
	}
	periods := f.Periods()
	taxes := make([]money.USCents, len(periods))
	for i, p := range periods {
		taxes[i] = p.AnnualisedTax()
	}
	ann, err := estimated.Annualised(taxes, y.applicable, regular)
	if err != nil {
		return nil, err
	}
	a.notes.Rule("RI-2210 (2025), Part 5, lines 23 to 33", fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: 20%%, 40%%, 60%% and 80%% of the Rhode Island tax on annualised income, "+
		"less earlier installments, and never more than a quarter of %v plus any reduction carried forward", ann[0], ann[1], ann[2], ann[3], target))
	return ann, nil
}

// january decides whether the return was filed and paid in time to count in
// column D. Under RI-2210 that payment is treated as made by January 15, so
// the January installment is met and nothing runs after that day.
func (r rhodeIslandEstimated) january(f estimated.Facts, extras EstimatedFacts, a *assessment) {
	y := r.y
	filed := f.ReturnFiled()
	if filed.IsZero() {
		return
	}
	coupon := y.calendar.NextBusinessDay(y.couponBy)
	reading := extras.JanuaryReturn()
	by := y.penaltyFormBy
	if reading == JanuaryReturnCoupon {
		by = coupon
	}
	if reading == JanuaryReturnUnanswered && filed.After(y.penaltyFormBy) && !filed.After(coupon) {
		a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Will the Division count your return filed and paid %s toward January?", r.date(filed)),
			fmt.Sprintf("Until then, January counts as missed; the penalty form's date is %s. The coupon's %s moves to %s for %s.",
				r.date(y.penaltyFormBy), r.date(y.couponBy), r.date(coupon), holidayName(y.calendar, y.couponBy)))
	}
	a.affects.january = !filed.After(coupon)
	if filed.After(by) {
		return
	}
	a.januaryMet = true
	cite := "RI-2210 (2025), line 16 instructions"
	if reading == JanuaryReturnCoupon {
		cite = "RI-1040ES (2026), When and where to make estimates; R.I. Gen. Laws §§ 44-30-55(e), 44-30-91(b)"
	}
	a.notes.Rule(cite, fmt.Sprintf("the return was filed with the balance paid on %s, by %s, so the payment counts in column D as paid by %s: the January installment is met, and no shortfall runs after that day",
		filed.Long(), by.Long(), a.dues[3].Long()))
}

// holidayName is the holiday's common name, the part of the calendar's name
// in parentheses.
func holidayName(c estimated.Calendar, d estimated.Date) string {
	if name, ok := c.IsHoliday(d); ok {
		if i, j := strings.LastIndex(name, "("), strings.LastIndex(name, ")"); i >= 0 && j > i {
			return name[i+1 : j]
		}
		return name
	}
	return "a weekend"
}

func (r rhodeIslandEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	ds, err := r.divergences(a.affects)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	var notes working.Notes
	notes.Diverges(ds...)
	return working.NewResult(a.requirement).With(a.notes).With(notes).Build()
}

// ledger applies the credits to the required installments.
func (r rhodeIslandEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, a.dues, quarters)
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, a.dues)
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	required := append([]money.USCents(nil), a.installments...)
	if a.januaryMet {
		required[3] = 0
	}
	ledger, err := estimated.Apply(a.dues, required, credits)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("RI-2210 (2025), line 16 instructions", fmt.Sprintf("the %v withheld is credited on the dates it was withheld, as the filer can show", f.WithheldTotal()))
	} else {
		notes.Rule("RI-2210 (2025), line 16 instructions", fmt.Sprintf("the %v withheld is treated as paid one-fourth on each due date, whenever it was actually withheld", f.WithheldTotal()))
	}
	notes.Rule("RI-2210 (2025), line 17 instructions", "an underpayment carries forward and is paid by the next payments first, and an overpayment carries forward to the next installment")
	return ledger, notes, nil
}

func (r rhodeIslandEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Rhode Island return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := r.schedule(a, ledger)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	ds, err := r.divergences(a.affects)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	notes.Diverges(ds...)
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments from the ledger. A January installment met by
// the return is excused.
func (r rhodeIslandEstimated) schedule(a assessment, ledger []estimated.LedgerEntry) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	var needed money.USCents
	for i, l := range ledger {
		needed += a.installments[i]
		b := estimated.NewInstallment(i+1, l.Due()).NeededBy(needed).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if a.januaryMet && i == 3 {
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

func (r rhodeIslandEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("rhodeislandtaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Rhode Island return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	extras, err := r.extras(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
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
	if a.januaryMet && a.dues[3].Before(stop) {
		stop = a.dues[3]
	}
	waiver, waiverAnswered := f.Positions().Waiver()
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	schedule, err := r.schedule(a, ledger)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if a.exempt {
		p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule).Build()
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		ds, err := r.divergences(a.affects)
		if err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		var dn working.Notes
		dn.Diverges(ds...)
		return working.NewResult(p).With(a.notes).With(dn).Build()
	}

	p, unrated, err := estimated.SimplePenalty(ledger, stop, y.rates, waiver, a.requirement, schedule)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("RI-2210 (2025), Part 1, line 6", fmt.Sprintf("this year's Rhode Island tax was not given, so the charge is figured against 100%% of the %d tax alone; "+
			"the required annual payment is the smaller of that and 80%% of this year's tax, so %v is the most the charge can be, and it is less if this year's tax turns out lower",
			y.year-1, p.Total()))
	}
	if paidInFull && !a.januaryMet {
		notes.Rule("R.I. Gen. Laws § 44-30-84(b); RI-2210 (2025), line 18 instructions", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("R.I. Gen. Laws §§ 44-30-84(b), 44-1-7; RI-2210 (2025), Part 4, lines 18 to 22", fmt.Sprintf("each shortfall is charged simple interest by the day, at the rate for each calendar year over 365 days, from its due date until paid or %s, "+
		"with each payment going to the oldest shortfall first; the charge to %s is %v", y.stop.Long(), stop.Long(), p.Total()))
	if !unrated.IsZero() {
		if p, err = p.ToBuilder().RatedThrough(unrated.AddDays(-1)).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if !unrated.IsZero() {
		notes.Ask(working.PositionUnpublishedRate, "", fmt.Sprintf("The rate for the days after %s isn't published yet.", y.rates.Ends().Time().Format("Jan 2, 2006")))
	}
	if p.Total() > 0 && p.Total() < y.minimum {
		applies, answered := extras.TwoDollarMinimum()
		a.affects.minimum = true
		a.affects.minimumApplied = answered && applies
		switch {
		case !answered:
			notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Does the Division apply its under-%s rule to your estimated tax charge?", y.minimum.Whole()),
				fmt.Sprintf("Only the law on tax paid late says interest under %s isn't payable. Until then, the charge counts in full.", y.minimum.Whole()))
		case applies:
			notes.Rule("R.I. Gen. Laws § 44-30-84(a)(1)", fmt.Sprintf("by the reading given, the %v charge is under %v and is not payable", p.Total(), y.minimum))
			pb := estimated.NewPenalty(stop).Waiver(waiver).Because(a.requirement, schedule)
			if !a.taxKnown {
				pb.UpperBound()
			}
			if p, err = pb.Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	switch {
	case p.Total() > 0 && !waiverAnswered:
		notes.Ask(working.PositionPenaltyWaiver, "Will you ask the Division to waive the charge?",
			"The federal waivers cover a casualty, disaster, retiring after 62 or disability. The charge shown is before any waiver; Rhode Island hasn't said it grants them.")
	case waiverAnswered && waiver != estimated.WaiverNone:
		notes.Rule("280-RICR-20-55-1 § 1.6(A)(1)", fmt.Sprintf("a waiver for %v is asked for; the Division decides it, and no Rhode Island source says it grants one, so the charge shown is before it", waiver))
		if p.Total() > 0 {
			// § 44-30-84(b) adopts only § 6654(e)(1) and (2); the regulation
			// says federal exceptions "may be followed". Facts the federal
			// waiver refuses are refused; any other is the Division's call.
			o, _ := estimated.StandardWaiver(f, "the Division", 62)
			if !o.Refused() {
				o = estimated.WaiverUndecided("Rhode Island law doesn't adopt the federal waivers. Its rules only say federal exceptions may be followed, so the Division decides.")
				notes.Ask(working.PositionUnsettledRule, "", "Rhode Island hasn't said whether the Division grants the federal waivers. They cover a casualty, disaster, retirement or disability.")
			}
			if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
				return working.Result[estimated.Penalty]{}, err
			}
		}
	}
	ds, err := r.divergences(a.affects)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	notes.Diverges(ds...)
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

func (r rhodeIslandEstimated) Explanation() estimated.Explanation { return rhodeIslandExplanation }

// rhodeIslandExplanation is written by hand and checked against
// research/estimated-payments/ri.md.
var rhodeIslandExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Take your Rhode Island tax after credits and withholding. If it comes to $250 or more, Rhode Island expects estimated payments. The charge for paying too little is interest on each quarter's shortfall. It is 12% a year for 2026.").
		Paragraph("There is no charge if withholding and estimated payments reach a target. The target is the smaller of 80% of this year's tax or 100% of last year's. Last year's figure stays at 100% however high your income was. There is also no charge if this year's tax less withholding is $250 or less. There is none either if you had no federal tax liability last year. You must also have lived in Rhode Island all that year.").
		Paragraph("Pay four equal quarters, by April 15, June 15, September 15 and January 15. The January date is in the next year. A weekend or Rhode Island legal holiday shifts a date to the next business day. None of the 2026 dates falls on one. Withholding counts as paid evenly on the four dates. You can instead show when it was actually withheld. A payment goes first to the oldest shortfall.").
		Paragraph("The interest runs by the day from each due date until the shortfall is paid. It stops on April 15 of the next year. The rate is set once a year, at the prime rate plus 2%, but never below 12%. It was 12% for 2025 and is 12% for 2026. The annualised method on Form RI-2210 can lower early quarters. It helps when income arrives late in the year.").
		Paragraph("Filing your return early can stand in for the January payment. The penalty form counts a return filed and paid by January 31. The payment coupon says February 15. For 2026 the later date is February 16, 2027. February 15 is Washington's Birthday. The Division hasn't said which date it applies.").
		VersusFederal("Rhode Island uses 80% of this year's tax rather than 90%. It uses 100% of last year's at every income, with no 110% tier. Its floor is $250 rather than $1,000. Its rate is fixed for each calendar year rather than reset each quarter. Due dates move for Rhode Island's own holidays. The law adopts no waiver for a casualty, retirement or disability.").
		Source("R.I. Gen. Laws §§ 44-30-55, 44-30-56, 44-30-84, 44-30-91, 44-1-7 and 25-1-1").
		Source("280-RICR-20-55-1, Estimated Payments").
		Source("2026 RI-1040ES, Rhode Island Resident and Nonresident Estimated Payment Coupons").
		Source("2025 Form RI-2210 and its instructions, and 2025 Form RI-2210A").
		Source("ADV 2025-23, Interest rates posted for 2026 (November 26, 2025)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
