package hawaiitaxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Hawaii's estimated tax rules, HRS § 235-97 and HAR § 18-235-97, as the
// shared [estimated.EstimatedTax] interface.
//
// HRS § 235-97 sets Hawaii's own rate, threshold, due dates and percentages,
// and § 235-97(f)(4) adopts subsections (d), (e)(2), (e)(3), (h), (i), (j),
// (k) and (l) of 26 U.S.C. § 6654 with the due dates moved to the 20th and the
// required annual payment set at 60% or 100%. Nothing here imports the
// federal package: what Hawaii takes from § 6654 is written out below and
// cited to § 235-97(f)(4).
//
// The figure follows the department's forms, Form N-210 and its instructions
// for 2025 and Tax Facts 2019-3 (revised February 2026), and the result's
// Divergences say where the statute or the rule reads otherwise. HRS § 235-97
// was last amended in 2003 and the department's summary of the 2026 session,
// Announcement No. 2026-06, lists no change to it, so no form here predates
// the law it describes.
//
// Where last year's return was joint and this year's is not, or the other way
// round, the caller passes last year's tax combined or split as federal
// Publication 505 directs, which Form N-210 line 7 adopts.

// jurisdiction is Hawaii's postal code.
const jurisdiction = estimated.Hawaii

// EstimatedFacts are the facts Hawaii's rules read that no other jurisdiction
// does. Carry them in [estimated.Facts.State].
type EstimatedFacts struct {
	priorFarmer, priorFarmerAnswered      bool
	nonresidentAlien, nonresidentAnswered bool
}

// PriorYearFarmerOrFisher reports whether last year's gross income was at
// least two-thirds from farming or fishing, which puts the filer under the
// farmer and fisherman rules this year (N-210 instructions; 26 U.S.C.
// § 6654(i)(2)(B), adopted by HRS § 235-97(f)(4)), and whether the caller
// said. This year's share is [estimated.Facts.FarmShare] and
// [estimated.Facts.FishShare].
func (e EstimatedFacts) PriorYearFarmerOrFisher() (farmer, answered bool) {
	return e.priorFarmer, e.priorFarmerAnswered
}

// NonresidentAlienWithoutWages reports whether the filer is a nonresident
// alien with no wages subject to Hawaii withholding, who pays on a schedule of
// their own (N-210 instructions, Part III; 26 U.S.C. § 6654(j)), and whether
// the caller said.
func (e EstimatedFacts) NonresidentAlienWithoutWages() (nonresident, answered bool) {
	return e.nonresidentAlien, e.nonresidentAnswered
}

// EstimatedFactsBuilder builds [EstimatedFacts].
type EstimatedFactsBuilder struct{ e EstimatedFacts }

func NewEstimatedFacts() *EstimatedFactsBuilder { return &EstimatedFactsBuilder{} }

func (b *EstimatedFactsBuilder) PriorYearFarmerOrFisher(farmer bool) *EstimatedFactsBuilder {
	b.e.priorFarmer, b.e.priorFarmerAnswered = farmer, true
	return b
}

func (b *EstimatedFactsBuilder) NonresidentAlienWithoutWages(nonresident bool) *EstimatedFactsBuilder {
	b.e.nonresidentAlien, b.e.nonresidentAnswered = nonresident, true
	return b
}

// Build has nothing to refuse: each fact is a yes or a no.
func (b *EstimatedFactsBuilder) Build() (EstimatedFacts, error) { return b.e, nil }

// estimatedYear is one tax year's published figures.
type estimatedYear struct {
	year int

	// threshold is N-210 Exception 1 and HRS § 235-97(a)(6): no penalty when
	// tax less withholding is under it.
	threshold money.USCents

	// thisYearPercent and priorPercent are the two tests of § 235-97(f)(4).
	thisYearPercent, priorPercent money.Rate

	// dues are the dates the statute and the forms print, before moving for
	// a weekend or holiday. The penalty's months are counted from them.
	dues     []estimated.Date
	calendar estimated.Calendar

	// fileAndPayBy is § 6654(h) and HAR § 18-235-97(h)(2): a return filed
	// and paid by it excuses the fourth installment.
	fileAndPayBy estimated.Date

	// stop is § 235-97(f)(2)(A): the twentieth day of the fourth month after
	// the year.
	stop estimated.Date

	// monthly is the factor N-210 line 20 multiplies by for each month or
	// part of a month; annualRate is the rate it stands for, twelve months
	// of § 235-97(f)'s two-thirds of one percent, for the working.
	monthly    estimated.Factor
	annualRate money.Rate
	rateSource string

	// periodEnds and annualisedPercentages are N-210 Schedule A's column
	// headings and line 17.
	periodEnds            []estimated.Date
	annualisedPercentages []money.Rate
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// quarters are § 6654(d)(1)(A), adopted by § 235-97(f)(4), and N-210 line 9:
// each installment is a quarter of the required annual payment. § 235-97(g)
// deems withholding paid in the same equal parts.
var quarters = []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)}

// EstimatedTax returns Hawaii's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return hawaiiEstimated{y}, nil
}

type hawaiiEstimated struct{ y estimatedYear }

// assessment is what the three questions share.
type assessment struct {
	requirement estimated.Requirement
	exempt      bool
	taxKnown    bool

	// dues are the due dates moved for weekends and holidays, as a schedule
	// shows them. The ledger and the penalty's months use the unmoved dates.
	dues []estimated.Date

	// installments are each installment's own amount: a quarter of the
	// target, or N-210 Schedule A line 24 under the annualised method.
	installments []money.USCents

	// filedEarly is a return filed and paid by January 31: the fourth
	// installment is excused and the earlier ones stop on January 20.
	filedEarly bool

	notes working.Notes
}

func (r hawaiiEstimated) extras(f estimated.Facts) (EstimatedFacts, error) {
	switch s := f.State().(type) {
	case nil:
		return EstimatedFacts{}, nil
	case EstimatedFacts:
		return s, nil
	default:
		return EstimatedFacts{}, fmt.Errorf("hawaiitaxcalc: the facts carry %T, not hawaiitaxcalc.EstimatedFacts", s)
	}
}

func (r hawaiiEstimated) check(f estimated.Facts, extras EstimatedFacts) error {
	farmerLastYear, _ := extras.PriorYearFarmerOrFisher()
	nonresident, _ := extras.NonresidentAlienWithoutWages()
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("hawaiitaxcalc: these are Hawaii's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("hawaiitaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.FarmerOrFisher(), farmerLastYear:
		return fmt.Errorf("%w: HRS § 235-97(f)(4) adopts 26 U.S.C. § 6654(i), and Form N-210 gives farmers and fishermen one installment by January 20, "+
			"or no penalty for a return filed and paid by March 1; that is not built, and whether § 6654(i)'s 66 2/3%% applies in place of the 60%% the form uses is not settled",
			estimated.ErrUnsupported)
	case nonresident:
		return fmt.Errorf("%w: Form N-210 and 26 U.S.C. § 6654(j) give a nonresident alien without Hawaii wage withholding one-half by June 20 and a quarter on each later date, which is not built",
			estimated.ErrUnsupported)
	}
	if f.Elections().Annualised() {
		for i, p := range f.Periods() {
			if p.End() != r.y.periodEnds[i] {
				return fmt.Errorf("hawaiitaxcalc: N-210 Schedule A period %d ends on %v, not %v", i+1, r.y.periodEnds[i], p.End())
			}
		}
	}
	return nil
}

// divergences are where the department's forms and HRS § 235-97, HAR
// § 18-235-97 or the federal text § 235-97(f)(4) adopts part ways on a rule
// every Hawaii computation uses. They are found in
// research/estimated-payments/hi.md and checked against the texts in
// testdata/. The flags say whether each changes anything for the facts.
func (r hawaiiEstimated) divergences(residence, rate, lateStart bool) ([]working.Divergence, error) {
	var out []working.Divergence
	for _, b := range []*working.DivergenceBuilder{
		working.NewDivergence("No liability last year").
			Form("There's no penalty if last year had no tax liability and was twelve months long. "+
				"You must also have been a full-year Hawaii resident that year. A nonresident or part-year resident doesn't qualify.",
				"Form N-210 (Rev. 2025), Part I box A and instructions, Exceptions to the Penalty 2; Tax Facts 2019-3 (rev. February 2026), questions 5 and 6").
			Statute("There's no penalty if last year had no tax liability and was twelve months long. "+
				"You must also have been a U.S. citizen or resident for all of it. The rule has no Hawaii residence test.",
				"HRS § 235-97(f)(4), adopting 26 U.S.C. § 6654(e)(2); HAR § 18-235-97(b)(2)(B)").
			Uses(working.ReadingForm).Affects(residence),
		working.NewDivergence("The monthly rate").
			Form("Each month or part of a month is charged at .00667 of the underpayment.",
				"Form N-210 (Rev. 2025), Part IV line 20 and instructions, Part IV").
			Statute("The charge is two-thirds of one per cent a month or fraction of a month. That is .006666…",
				"HRS § 235-97(f)").
			Uses(working.ReadingForm).Affects(rate),
		working.NewDivergence("A requirement first met after March 31").
			Form("Each installment is a quarter of the required annual payment. Under Schedule A, it is the annualised amount instead. "+
				"That holds whenever you first had to pay.",
				"Form N-210 (Rev. 2025), line 9 and instructions, Figure Your Underpayment").
			Statute("If you first had to pay in April or May, you pay three equal parts from June 20. "+
				"If it was June, July or August, you pay two equal parts from September 20. If it was after August 31, you pay it all by January 20.",
				"HAR § 18-235-97(e)(2)(A); HRS § 235-97(d)").
			Uses(working.ReadingForm).Affects(lateStart),
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

// shortDate is "Apr 20" inside the tax year and "Jan 31, 2027" outside it.
func shortDate(d estimated.Date, year int) string {
	m := d.Month().String()[:3]
	if d.Year() == year {
		return fmt.Sprintf("%s %d", m, d.Day())
	}
	return fmt.Sprintf("%s %d, %d", m, d.Day(), d.Year())
}

func (r hawaiiEstimated) assess(f estimated.Facts) (assessment, error) {
	extras, err := r.extras(f)
	if err != nil {
		return assessment{}, err
	}
	if err := r.check(f, extras); err != nil {
		return assessment{}, err
	}
	y := r.y
	var a assessment

	tax, taxKnown := f.ThisYear().Tax()
	a.taxKnown = taxKnown
	withheld := f.WithheldTotal()
	prior, hasPrior := f.PriorYear()
	last := y.year - 1

	// 60% of this year's tax. § 235-97(f)(4); N-210 line 6.
	thisYearOption := estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, y.thisYearPercent.Of(tax))
	if !taxKnown {
		thisYearOption = estimated.NewTargetOption(estimated.BasisThisYearTax, y.thisYearPercent, 0).Unavailable(estimated.NeedsThisYearTax)
	}
	thisYear, err := thisYearOption.Build()
	if err != nil {
		return assessment{}, err
	}
	if taxKnown {
		a.notes.Rule("HRS § 235-97(f)(4); Form N-210 (Rev. 2025), line 6", fmt.Sprintf("%v of this year's Hawaii tax of %v is %v", y.thisYearPercent, tax, thisYear.Amount()))
	}

	// 100% of last year's tax, for a filed twelve-month Hawaii return. There
	// is no higher percentage at any income.
	var priorTax money.USCents
	if hasPrior {
		if pf, ok := prior.Figures(); ok {
			priorTax, _ = pf.Tax()
		}
	}
	unavailable := func(why string) *estimated.TargetOptionBuilder {
		return estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorPercent, 0).Unavailable(why)
	}
	var priorOption *estimated.TargetOptionBuilder
	switch {
	case !hasPrior:
		priorOption = unavailable(fmt.Sprintf("Needs your %d Hawaii return", last))
	case prior.NotRequiredToFile():
		priorOption = unavailable(fmt.Sprintf("You didn't have to file a %d Hawaii return", last))
	case !prior.Filed():
		priorOption = unavailable(fmt.Sprintf("You didn't file a %d Hawaii return", last))
	case prior.Months() != 12:
		priorOption = unavailable(fmt.Sprintf("Your %d Hawaii return covered %d months, not 12", last, prior.Months()))
	default:
		amount := y.priorPercent.Of(priorTax)
		priorOption = estimated.NewTargetOption(estimated.BasisPriorYearTax, y.priorPercent, amount)
		a.notes.Rule("HRS § 235-97(f)(4); Form N-210 (Rev. 2025), line 7", fmt.Sprintf("%v of the %v tax on the %d Hawaii return is %v, at any income", y.priorPercent, priorTax, last, amount))
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
		a.notes.Ask(working.PositionThisYearTax, fmt.Sprintf("What do you expect your %d Hawaii tax to be?", y.year),
			fmt.Sprintf("It sets the %v option and decides whether you're under the %s threshold.", y.thisYearPercent, y.threshold.Whole()))
	}

	// No liability last year. The forms need a full-year Hawaii resident;
	// § 6654(e)(2), which § 235-97(f)(4) adopts, needs U.S. residence. The
	// form is followed, and the statute's reading is reported.
	noLiability := hasPrior && prior.Months() == 12 &&
		((prior.Filed() && priorTax == 0) || prior.NotRequiredToFile())
	resident, residenceAnswered := prior.FullYearResident()
	if noLiability {
		switch {
		case !residenceAnswered:
			a.notes.Ask(working.PositionResidency, fmt.Sprintf("Were you a Hawaii resident for all of %d?", last),
				fmt.Sprintf("If so, no Hawaii tax due for %d means there's no penalty for %d.", last, y.year))
		case !resident:
			a.notes.Rule("HRS § 235-97(f)(4); 26 U.S.C. § 6654(e)(2)", fmt.Sprintf("you had no Hawaii liability for %d, a twelve-month year, but were not a Hawaii resident all of it, so Form N-210's exception does not apply and the figures here are the form's; "+
				"the federal text Hawaii adopts asks only that you were a U.S. citizen or resident throughout %d, and if you were, the exception is yours to claim", last, last))
		}
	}

	spread := "They are due a quarter by each date."
	if f.Elections().Annualised() {
		spread = "They follow the annualised installments."
	}
	switch {
	case noLiability && residenceAnswered && resident:
		a.exempt = true
		none, err := estimated.NewException("No Hawaii tax due last year").Build()
		if err != nil {
			return assessment{}, err
		}
		rb.Required(false, fmt.Sprintf("You had no Hawaii tax due for %d, a full twelve-month year. You were a Hawaii resident all of it. No estimated payments are needed for %d, and no penalty can arise.", last, y.year)).Exempt(none)
		a.notes.Rule("Form N-210 (Rev. 2025), Exceptions to the Penalty 2; HRS § 235-97(f)(4); HAR § 18-235-97(b)(2)(B)",
			fmt.Sprintf("the %d return showed no liability for a full twelve months as a Hawaii resident, so there is no penalty for %d", last, y.year))
	case !taxKnown && !priorOpt.Available():
		rb.Required(false, fmt.Sprintf("Your target needs your %d Hawaii tax, because your %d return can't set one.", y.year, last)).NoTarget()
	case !taxKnown && withheld >= target:
		rb.Required(false, fmt.Sprintf("Your withholding of %s already reaches %v of your %d tax, %s. No estimated payments are needed, whatever your %d tax turns out to be.",
			withheld.Whole(), priorOpt.Percent(), last, target.Whole(), y.year)).Choose(chosen)
	case !taxKnown:
		rb.Required(true, fmt.Sprintf("Your target is %v of your %d tax, %s. After %s of withholding, that's %s of estimated payments. %s They keep you clear of a penalty whatever your %d tax turns out to be.",
			priorOpt.Percent(), last, target.Whole(), withheld.Whole(), (target-withheld).Whole(), spread, y.year)).Choose(chosen)
	case tax-withheld < y.threshold:
		a.exempt = true
		under, err := estimated.ExceptionBelow("Tax less withholding", tax-withheld, y.threshold)
		if err != nil {
			return assessment{}, err
		}
		leaves := "leaves " + near(tax-withheld, y.threshold) + ". That is under " + y.threshold.Whole()
		if tax < withheld {
			leaves = estimated.Leaves(tax - withheld)
		}
		rb.Required(false, fmt.Sprintf("Your Hawaii tax is %s, and %s was withheld. That %s. No estimated payments are needed, and no penalty can arise.",
			tax.Whole(), withheld.Whole(), leaves)).Exempt(under)
		a.notes.Rule("Form N-210 (Rev. 2025), line 5 and Exceptions to the Penalty 1; HRS § 235-97(a)(6); HAR § 18-235-97(b)(2)(A)",
			fmt.Sprintf("%v of tax less %v withheld is %v, under %v, so there is no penalty", tax, withheld, tax-withheld, y.threshold))
	case withheld >= target:
		rb.Required(false, fmt.Sprintf("Your Hawaii withholding of %s already reaches your target of %s. No estimated payments are needed.", withheld.Whole(), target.Whole())).Choose(chosen)
	default:
		rb.Required(true, fmt.Sprintf("Your Hawaii tax is %s, and %s was withheld. That leaves %s. That is %s or more, so estimated payments are needed. "+
			"You need %s of estimated payments to reach your target of %s. ",
			tax.Whole(), withheld.Whole(), near(tax-withheld, y.threshold), y.threshold.Whole(), (target-withheld).Whole(), target.Whole())+spread).Choose(chosen)
		if target-withheld < y.threshold {
			a.notes.Rule("Tax Facts 2019-3 (rev. February 2026), Estimated Tax Worksheet line 13; Form N-210 (Rev. 2025), lines 5 to 16",
				fmt.Sprintf("the worksheet says no estimated payments are required at this time when the required annual payment less withholding, here %v, is under %v; "+
					"Form N-210 tests the tax less withholding instead, %v here, and still charges on each quarter that is short, so the payments are shown as needed", target-withheld, y.threshold, tax-withheld))
		}
	}
	if a.requirement, err = rb.Build(); err != nil {
		return assessment{}, err
	}
	if !a.exempt {
		// Under an exception last year's return can't change the target.
		if !hasPrior {
			a.notes.Ask(working.PositionPriorYearReturn, fmt.Sprintf("Did you file a %d Hawaii return, and what was its tax?", last),
				fmt.Sprintf("With it, your target could be %v of last year's tax, not %v of this year's.", y.priorPercent, y.thisYearPercent))
		}
		if _, answered := extras.PriorYearFarmerOrFisher(); !answered {
			a.notes.Ask(working.PositionJurisdictionFact, fmt.Sprintf("Was at least two-thirds of your %d gross income from farming or fishing?", last),
				"If so, Hawaii's farmer and fisherman rules apply instead.")
		}
		if _, answered := extras.NonresidentAlienWithoutWages(); !answered {
			a.notes.Ask(working.PositionJurisdictionFact, "Are you a nonresident alien with no wages subject to Hawaii withholding?",
				"If so, your payments follow a schedule of their own.")
		}
	}

	// The due dates. § 235-97(a)(2) fixes them on the 20th, and HRS § 231-21
	// moves one that falls on a weekend or legal holiday.
	a.dues = estimated.MoveDates(y.calendar, y.dues)
	for i := range a.dues {
		if a.dues[i] != y.dues[i] {
			a.notes.Rule("HRS § 231-21; Tax Facts 2019-3 (rev. February 2026), question 8; Form N-210 (Rev. 2025) instructions, line 10 rule 6",
				fmt.Sprintf("the installment due %s falls on a %v, so a payment on %s is on time and counts as paid on %s", y.dues[i].Long(), y.dues[i].Weekday(), a.dues[i].Long(), y.dues[i].Long()))
		}
	}
	cumulative, err := estimated.CumulativeShares(a.requirement.Target(), quarters)
	if err != nil {
		return assessment{}, err
	}
	a.installments = estimated.Increments(cumulative)
	a.notes.Rule("HRS § 235-97(a)(2), (f)(4); Form N-210 (Rev. 2025), line 9", fmt.Sprintf("a quarter of %v, %v, is due on each of %s, %s, %s and %s", a.requirement.Target(), a.installments[0],
		a.dues[0].Long(), a.dues[1].Long(), a.dues[2].Long(), a.dues[3].Long()))

	if f.Elections().Annualised() && !a.exempt {
		if err := r.annualise(f, &a); err != nil {
			return assessment{}, err
		}
	}

	if met := f.RequirementFirstMet(); !met.IsZero() && met.After(estimated.MustDate(y.year, 3, 31)) && !a.exempt {
		a.notes.Rule("HAR § 18-235-97(e)(2)(A); Form N-210 (Rev. 2025), line 9", fmt.Sprintf("you first had to pay estimated tax on %s; the rule lets a late starter spread the year's estimate over the remaining dates, "+
			"but Form N-210 measures every installment as a quarter, or by Schedule A, so the figures here are the form's and the earlier installments are not excused", met.Long()))
	}

	// § 6654(h), adopted by § 235-97(f)(4), and HAR § 18-235-97(h)(2): a
	// return filed and paid by January 31 excuses the fourth installment.
	// January 31, 2027 is a Sunday. Neither the rule nor the forms say the
	// date moves, so it is not moved here.
	filed := f.ReturnFiled()
	if !filed.IsZero() && !filed.After(y.fileAndPayBy) && !a.exempt {
		a.filedEarly = true
		a.notes.Rule("HRS § 235-97(f)(4), adopting 26 U.S.C. § 6654(h); HAR § 18-235-97(h)(2); Form N-210 (Rev. 2025) instructions, Part IV",
			fmt.Sprintf("the return was filed and paid on %s, by %s, so there is no penalty on the fourth installment, and the earlier installments' penalty stops on %s", filed.Long(), y.fileAndPayBy.Long(), y.dues[3].Long()))
	}
	if moved := y.calendar.NextBusinessDay(y.fileAndPayBy); !a.exempt && filed.After(y.fileAndPayBy) && !filed.After(moved) {
		byDate := fmt.Sprintf("%v, %s %d", y.fileAndPayBy.Weekday(), y.fileAndPayBy.Month().String()[:3], y.fileAndPayBy.Day())
		a.notes.Ask(working.PositionUnsettledRule, "",
			fmt.Sprintf("Hawaii hasn't said if filing and paying on %s is by %s. "+
				"The figures treat it as late, so your January installment isn't excused.",
				shortDate(moved, y.year), byDate))
	}
	if a.exempt {
		a.installments = make([]money.USCents, len(a.dues))
	}

	// What each divergence turns on in these facts.
	residence := noLiability && residenceAnswered && !resident
	lateStart := !a.exempt && f.RequirementFirstMet().After(estimated.MustDate(y.year, 3, 31))
	rate := false
	if !a.exempt && a.requirement.TargetKnown() {
		if rate, err = r.rateAffects(f, a); err != nil {
			return assessment{}, err
		}
	}
	divergences, err := r.divergences(residence, rate, lateStart)
	if err != nil {
		return assessment{}, err
	}
	a.notes.Diverges(divergences...)
	return a, nil
}

// rateAffects reports whether the printed .00667 and the statute's
// two-thirds of one percent give a different penalty, figured to the latest
// day the penalty can run to.
func (r hawaiiEstimated) rateAffects(f estimated.Facts, a assessment) (bool, error) {
	y := r.y
	ledger, _, err := r.ledger(f, a)
	if err != nil {
		return false, err
	}
	stop := y.stop
	switch filed := f.ReturnFiled(); {
	case a.filedEarly:
		stop = y.dues[3]
	case !filed.IsZero() && filed.Before(stop):
		stop = filed
	}
	for i, l := range ledger {
		if l.Shortfall() <= 0 {
			continue
		}
		for _, piece := range l.Pieces() {
			to := stop
			if on, paid := piece.PaidOn(); paid {
				to = earliest(on, stop)
			}
			if !to.After(y.dues[i]) {
				continue
			}
			months := estimated.MonthsOrPart(y.dues[i], to)
			// Two-thirds of one percent is 2 / 300, rounded to the cent.
			n := int64(piece.Amount()) * int64(months) * 2
			exact := money.USCents((n + 150) / 300)
			if exact != y.monthly.Of(piece.Amount(), months, estimated.RoundToCent) {
				return true, nil
			}
		}
	}
	return false, nil
}

// annualise replaces the installments with N-210 Schedule A line 24: the
// applicable percentage of the period's tax after credits on annualised
// income, less earlier installments, and never more than the regular
// installment plus what the method lowered earlier. That is § 6654(d)(2), as
// § 235-97(f)(4) adopts it, with Hawaii's 15%, 30%, 45% and 60%.
func (r hawaiiEstimated) annualise(f estimated.Facts, a *assessment) error {
	periods := f.Periods()
	taxes := make([]money.USCents, len(periods))
	for i, p := range periods {
		taxes[i] = p.AnnualisedTax()
	}
	ann, err := estimated.Annualised(taxes, r.y.annualisedPercentages, a.installments)
	if err != nil {
		return err
	}
	a.installments = ann
	p := r.y.annualisedPercentages
	a.notes.Rule("HRS § 235-97(f)(4), adopting 26 U.S.C. § 6654(d)(2); Form N-210 (Rev. 2025), Schedule A, lines 17 to 24",
		fmt.Sprintf("under the annualised method the installments are %v, %v, %v and %v: %v, %v, %v and %v of the tax after credits on annualised income, "+
			"less earlier installments, and never more than the regular installment plus what the method lowered earlier", ann[0], ann[1], ann[2], ann[3], p[0], p[1], p[2], p[3]))
	return nil
}

func (r hawaiiEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(a.requirement).With(a.notes).Build()
}

// onTheDueDate moves a credit made after an unmoved due date, up to the day
// the date moved to, back onto the due date. N-210 instructions, line 10
// rule 6: a payment made on the next working day "will be considered paid on
// the payment due date".
func (r hawaiiEstimated) onTheDueDate(credits []estimated.Payment, moved []estimated.Date) ([]estimated.Payment, error) {
	out := make([]estimated.Payment, len(credits))
	for i, c := range credits {
		on := c.On()
		for j, due := range r.y.dues {
			if on.After(due) && !on.After(moved[j]) {
				on = due
			}
		}
		p, err := estimated.NewPayment().On(on).Amount(c.Amount()).Build()
		if err != nil {
			return nil, err
		}
		out[i] = p
	}
	return out, nil
}

// ledger applies the credits to the installments on the unmoved due dates,
// with the fourth set to nothing when filing early excused it.
func (r hawaiiEstimated) ledger(f estimated.Facts, a assessment) ([]estimated.LedgerEntry, working.Notes, error) {
	credits, err := estimated.Credits(f, r.y.dues, quarters)
	if f.Elections().WithholdingAsWithheld() {
		credits, err = estimated.CreditsAsWithheld(f, r.y.dues)
	}
	if err != nil {
		return nil, working.Notes{}, err
	}
	if credits, err = r.onTheDueDate(credits, a.dues); err != nil {
		return nil, working.Notes{}, err
	}
	required := append([]money.USCents(nil), a.installments...)
	if a.filedEarly {
		required[3] = 0
	}
	ledger, err := estimated.Apply(r.y.dues, required, credits)
	if err != nil {
		return nil, working.Notes{}, err
	}
	var notes working.Notes
	if f.Elections().WithholdingAsWithheld() {
		notes.Rule("HRS § 235-97(g); Form N-210 (Rev. 2025), Part I box D", fmt.Sprintf("the %v withheld is credited on the dates it was actually withheld, which the filer must show", f.WithheldTotal()))
	} else {
		notes.Rule("HRS § 235-97(g); Form N-210 (Rev. 2025) instructions, line 10 rule 1", fmt.Sprintf("the %v withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld", f.WithheldTotal()))
	}
	notes.Rule("HRS § 235-97(f)(2)(B); Form N-210 (Rev. 2025) instructions, line 10 rule 2 and line 19",
		fmt.Sprintf("a payment is credited against the unpaid installments in the order they fall due, and last year's overpayment applied forward counts as paid on %s, as the form treats it when last year's return was paid in full by its due date", r.y.dues[0].Long()))
	return ledger, notes, nil
}

func (r hawaiiEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	a, err := r.assess(f)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[[]estimated.Installment]{}, fmt.Errorf("%w: with no %d Hawaii return to measure against, there is no target to schedule", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	out, err := schedule(ledger, a)
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult(out).With(a.notes).With(notes).Build()
}

// schedule is the installments the ledger gives, on the moved dates. The
// fourth is excused for a return filed and paid by January 31.
func schedule(ledger []estimated.LedgerEntry, a assessment) ([]estimated.Installment, error) {
	out := make([]estimated.Installment, len(ledger))
	var needed money.USCents
	for i, l := range ledger {
		needed += a.installments[i]
		b := estimated.NewInstallment(i+1, a.dues[i]).NeededBy(needed).CreditedBy(l.CreditedBy()).Shortfall(l.Shortfall())
		if a.filedEarly && i == 3 {
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

func (r hawaiiEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("hawaiitaxcalc: a penalty needs the date to figure it to")
	}
	a, err := r.assess(f)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.requirement.TargetKnown() {
		return working.Result[estimated.Penalty]{}, fmt.Errorf("%w: with no %d Hawaii return to measure against, there is no target to figure a penalty from", estimated.ErrNeedsThisYearTax, r.y.year-1)
	}
	y := r.y
	stop := earliest(asOf, y.stop)
	paidInFull := false
	switch filed := f.ReturnFiled(); {
	case a.filedEarly:
		stop = earliest(stop, y.dues[3])
	case !filed.IsZero() && filed.Before(stop):
		stop, paidInFull = filed, true
	}
	waiver, waiverAnswered := f.Positions().Waiver()
	ledger, notes, err := r.ledger(f, a)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	sched, err := schedule(ledger, a)
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

	p, err := r.charge(ledger, a.dues, stop, waiver, a.requirement, sched)
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	if !a.taxKnown {
		if p, err = p.ToBuilder().UpperBound().Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
		notes.Rule("HRS § 235-97(f)(4)", fmt.Sprintf("this year's Hawaii tax was not given, so the penalty is figured against %v of the %d tax alone; "+
			"the required payment is the lesser of that and %v of this year's tax, so %v is the most the penalty can be, and it is less if this year's tax turns out lower",
			a.requirement.Options()[1].Percent(), y.year-1, y.thisYearPercent, p.Total()))
	}
	if o, asked := estimated.StandardWaiver(f, "the Hawaii Department of Taxation", 62); asked && p.Total() > 0 {
		// HRS § 235-97(f)(4) adopts the waivers of 26 U.S.C. § 6654(e)(3),
		// as Form N-210's instructions state them.
		if p, err = p.ToBuilder().WaiverOutcome(o).Build(); err != nil {
			return working.Result[estimated.Penalty]{}, err
		}
	}
	if paidInFull {
		notes.Rule("HRS § 235-97(f)(2)(B)", fmt.Sprintf("the return was filed with the whole balance paid on %s, so every shortfall still outstanding was paid that day", stop.Long()))
	}
	notes.Rule("HRS § 235-97(f), (f)(2); Form N-210 (Rev. 2025), Part IV, lines 18 to 20",
		fmt.Sprintf("each shortfall is charged %v of itself for each month or part of a month from its due date until paid or %s, a month running from the 21st to the 20th, "+
			"with each payment applied to the oldest shortfall first; the penalty to %s is %v", y.monthly, y.stop.Long(), stop.Long(), p.Total()))
	notes.Rule("Form N-11 (Rev. 2025) instructions, Rounding Off to Whole Dollars", "each charge here is rounded to the cent; the return rounds its entries to whole dollars, "+
		"either each entry or only the total as the filer chooses, so the figure entered can differ by less than a dollar a line")
	if p.Total() > 0 && !waiverAnswered {
		notes.Ask(working.PositionPenaltyWaiver, "Do you want a waiver for a casualty, disaster, retiring after 62 or disability?",
			"Unusual circumstances count too; retiring or disability needs reasonable cause. "+
				"Until Hawaii grants a waiver, the figures include the full penalty.")
	}
	return working.NewResult(p).With(a.notes).With(notes).Build()
}

// charge is N-210 Part IV: for each piece of each shortfall, the months from
// the unmoved due date to the day it was paid or stop, any part of a month
// counted whole, times the printed factor, times the piece. The 20th-to-20th
// count of [estimated.MonthsOrPart] is the form's month from the 21st to the
// 20th.
func (r hawaiiEstimated) charge(ledger []estimated.LedgerEntry, moved []estimated.Date, stop estimated.Date, waiver estimated.WaiverKind,
	req estimated.Requirement, sched []estimated.Installment) (estimated.Penalty, error) {
	pb := estimated.NewPenalty(stop).Waiver(waiver).Because(req, sched)
	for i, l := range ledger {
		if l.Shortfall() <= 0 {
			continue
		}
		from := r.y.dues[i]
		lb := estimated.NewPenaltyLine(l.Number(), moved[i], l.Shortfall())
		last := from
		for _, piece := range l.Pieces() {
			to := stop
			if on, paid := piece.PaidOn(); paid {
				to = earliest(on, stop)
			}
			if !to.After(from) {
				continue
			}
			if to.After(last) {
				last = to
			}
			months := estimated.MonthsOrPart(from, to)
			part, err := estimated.NewPenaltyPart(estimated.PartInterest, piece.Amount(), r.y.annualRate).
				Span(from, to).Days(to.DaysSince(from)).Months(months).
				Charge(r.y.monthly.Of(piece.Amount(), months, estimated.RoundToCent)).Source(r.y.rateSource).Build()
			if err != nil {
				return estimated.Penalty{}, err
			}
			lb.Part(part)
		}
		line, err := lb.Days(last.DaysSince(from)).Build()
		if err != nil {
			return estimated.Penalty{}, err
		}
		pb.Line(line)
	}
	return pb.Build()
}

func earliest(a, b estimated.Date) estimated.Date {
	if b.Before(a) {
		return b
	}
	return a
}

func (r hawaiiEstimated) Explanation() estimated.Explanation { return hawaiiExplanation }

// hawaiiExplanation is written by hand and checked against
// research/estimated-payments/hi.md.
var hawaiiExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Hawaii expects estimated payments if you expect $500 or more of Hawaii tax due. That is the tax left after withholding and credits. " +
			"Each quarter's shortfall is charged 2/3 of 1% for each month it stays unpaid. Part of a month counts as a full month.").
		Paragraph("Your target is 60% of this year's Hawaii tax or 100% of last year's, if smaller. Withholding and estimated payments that reach it keep you clear of the penalty. " +
			"There is no 110% figure for higher incomes. Last year's figure needs a Hawaii return covering a full twelve months. " +
			"Someone who filed no Hawaii return last year has only the 60% figure.").
		Paragraph("Each payment is a quarter, due April 20, June 20, September 20 and January 20. January 20 is in the next year. " +
			"A date on a weekend or holiday shifts to the next business day. In 2026 the June date shifts to June 22 and the September date to September 21. " +
			"Withholding counts as paid evenly over the four dates. That holds unless you show when it was withheld. " +
			"Filing and paying in full by January 31 removes the January payment's penalty. The charge on earlier shortfalls then stops at January 20.").
		Paragraph("There is no penalty if this year's tax less withholding is under $500. Nor is there one if you had no Hawaii tax due last year. " +
			"That year must be twelve months, all as a Hawaii resident. The department can waive the penalty for a casualty or disaster. " +
			"The same goes if you retired after 62 or became disabled with reasonable cause.").
		Paragraph("The charge is two-thirds of one percent of the shortfall, 8% a year. " +
			"It applies for each month or part of a month the shortfall stays unpaid. A month runs from the 21st to the 20th, so one day late costs a full month. " +
			"Form N-210 multiplies by .00667 a month. The charge stops when the shortfall is paid, or on April 20 of the next year. That is when the return is due. " +
			"With uneven income, Schedule A of Form N-210 can lower the early payments. It uses 15%, 30%, 45% and 60% of the tax on annualised income.").
		VersusFederal("Hawaii's test on this year's tax is 60%, not 90%. Last year's test is 100% at every income, with no 110% step. " +
			"The threshold is $500, not $1,000, and payments fall on the 20th, not the 15th. " +
			"The charge is 2/3 of 1% for each month or part of one, not a daily rate. One day late costs a full month.").
		Source("HRS §§ 235-97, 231-21").
		Source("HAR § 18-235-97").
		Source("Tax Facts 2019-3, Estimated Income Tax for Individuals (rev. February 2026)").
		Source("Form N-210, Underpayment of Estimated Tax by Individuals, Estates, and Trusts, and its instructions (Rev. 2025)").
		Source("26 U.S.C. § 6654(e), (h), as adopted by HRS § 235-97(f)(4)").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
