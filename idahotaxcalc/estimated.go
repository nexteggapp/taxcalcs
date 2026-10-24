package idahotaxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Idaho's estimated tax rules for individuals, as the shared
// [estimated.EstimatedTax] interface.
//
// There are none. Idaho Code § 63-3036A requires estimated payments of
// corporations only, and the Form 40 instructions tell individuals that Idaho
// doesn't require them. So Requirement says no payments are required and no
// penalty can arise, Schedule has no installments, and Penalty is an exact
// zero.
//
// What an individual does meet is the extension prepayment test of Idaho Code
// § 63-3033: the automatic six-month extension to file holds only if enough
// was paid by the April due date. That is a condition of the extension, not
// an estimated payment, and it is described in the Explanation rather than
// computed here.

// jurisdiction is Idaho's postal code.
const jurisdiction = estimated.Idaho

// estimatedYear is one tax year's published figures. None of them is an
// estimated tax figure; they are the extension prepayment test and the
// interest the Explanation describes.
type estimatedYear struct {
	year int

	// extensionThisYear and extensionPriorYear are § 63-3033(a): the share of
	// this year's tax, or of the tax on last year's return, that must be paid
	// by the unextended due date for the automatic extension to hold.
	extensionThisYear, extensionPriorYear money.Rate

	// extensionMinimum is § 63-3033(b): a payment needed to meet the test of
	// this much or less is not required.
	extensionMinimum money.USCents

	// extensionPenalty is § 63-3033(f)(1): the penalty a month, from the
	// original due date, when the test is missed and the tax is paid by the
	// extended due date.
	extensionPenalty money.Rate

	// interest is the annual rate on tax unpaid after the return's due date
	// in the year after the tax year, set under § 63-3045.
	interest money.Rate
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Idaho's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return idahoEstimated{y}, nil
}

type idahoEstimated struct{ y estimatedYear }

func (r idahoEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("idahotaxcalc: these are Idaho's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("idahotaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.State() != nil:
		return fmt.Errorf("idahotaxcalc: Idaho's rules read no extras, and the facts carry %T", f.State())
	}
	return nil
}

// notes are the rule every answer rests on, and the divergence the
// Explanation's account of the extension test depends on.
func (r idahoEstimated) notes() (working.Notes, error) {
	var n working.Notes
	n.Rule("Idaho Code § 63-3036A(a); Form 40 instructions (2025), Filing Requirements",
		fmt.Sprintf("estimated tax payments are required of corporations only, and Idaho doesn't require them of individuals, so none are required for %d and no underpayment penalty arises", r.y.year))
	d, err := working.NewDivergence("What the extension's prior-year test measures").
		Form("The Form 40 instructions count 100% of state income taxes paid the year before. Form 51's worksheet takes the total tax on last year's return.",
			"Form 40 instructions (2025, EIN00046 03-02-2026), Extension of Time to File; Form 51 (EFO00092 09-18-2025), Tax Payment Worksheet, line 1").
		Statute("The extension holds if you've paid the total tax due on last year's return. That test applies only if you filed one.",
			"Idaho Code § 63-3033(a)").
		// The statute and Form 51 agree. No target, schedule or penalty
		// turns on the test, because Idaho has no estimated payments.
		Uses(working.ReadingStatute).
		Affects(false).
		Build()
	if err != nil {
		return working.Notes{}, err
	}
	n.Diverges(d)
	return n, nil
}

func (r idahoEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	if err := r.check(f); err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	req, err := r.requirement()
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	n, err := r.notes()
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(req).With(n).Build()
}

// requirement is the exception that Idaho requires no estimated payments of
// individuals, as the target.
func (r idahoEstimated) requirement() (estimated.Requirement, error) {
	y := r.y
	none, err := estimated.NewException("Idaho doesn't require estimated payments").Build()
	if err != nil {
		return estimated.Requirement{}, err
	}
	return estimated.NewRequirement().
		Required(false, fmt.Sprintf("Idaho does not require individuals to make estimated tax payments for %d. There is no underpayment penalty. "+
			"To get the automatic extension to file, pay enough by the April due date. That is at least %v of the %d tax. Or it is %v of the tax on your %d Idaho return.",
			y.year, y.extensionThisYear, y.year, y.extensionPriorYear, y.year-1)).
		Exempt(none).
		Build()
}

func (r idahoEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	if err := r.check(f); err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	n, err := r.notes()
	if err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	n.Rule("Idaho Code § 63-3036A", fmt.Sprintf("an individual has no estimated tax installments, so there are no due dates for %d", r.y.year))
	return working.NewResult([]estimated.Installment{}).With(n).Build()
}

func (r idahoEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("idahotaxcalc: a penalty needs the date to figure it to")
	}
	if err := r.check(f); err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	stop := asOf
	if filed := f.ReturnFiled(); !filed.IsZero() && filed.Before(stop) {
		stop = filed
	}
	waiver, _ := f.Positions().Waiver()
	req, err := r.requirement()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	p, err := estimated.NewPenalty(stop).Waiver(waiver).Because(req, nil).Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	n, err := r.notes()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	n.Rule("Idaho Code §§ 63-3036A, 63-3046A", fmt.Sprintf("interest on underpaid estimated tax is charged to corporations only, so an individual's penalty for %d is zero", r.y.year))
	if waiver != estimated.WaiverNone {
		n.Rule("Idaho Code § 63-3036A", "there is no individual estimated tax penalty, so there is nothing to waive")
	}
	return working.NewResult(p).With(n).Build()
}

func (r idahoEstimated) Explanation() estimated.Explanation { return explanationFor(r.y) }

// explanationFor is written by hand and checked against
// research/estimated-payments/id.md. Its figures come from the year, so a
// changed figure changes the text.
func explanationFor(y estimatedYear) estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Idaho does not require individuals to make estimated tax payments. It has no underpayment penalty for them. You can still prepay at any time with Form 51.").
		Paragraph("Idaho's estimated tax law applies only to corporations. If your Idaho withholding falls short, you pay the balance with your return. Nothing is charged for the quarters along the way.").
		Paragraph(fmt.Sprintf("What does matter is the April payment if you want more time to file. Idaho gives an automatic six-month extension to file if you pay enough in time. Your withholding and payments by April's due date must be at least one of these. One is %v of this year's tax. The other is %v of the tax on last year's return. The second option is open only if you filed an Idaho return last year. If you are %s or less short of qualifying, you qualify without paying it.",
			y.extensionThisYear, y.extensionPriorYear, y.extensionMinimum.Whole())).
		Paragraph(fmt.Sprintf("If you miss that test, the extension does not protect you. A penalty of %v a month then runs on the whole balance until you pay. It counts from the original due date, if you pay by the extended due date. If you pay later, Idaho's late-filing and late-payment penalties apply instead. They also count from the original due date. Interest runs on tax unpaid after the original due date in every case. The rate is %v a year for %d.",
			y.extensionPenalty, y.interest, y.year+1)).
		VersusFederal(fmt.Sprintf("Federally, you pay quarterly if you expect $1,000 or more due after withholding. Each short quarter is charged interest. Idaho has neither: no quarterly payments and no penalty for the year. Its %v-or-%v test looks like the federal safe harbour. But it decides the extension to file, and is judged once at the April due date. It has no 110%% tier for higher incomes.",
			y.extensionThisYear, y.extensionPriorYear)).
		Source("Idaho Code §§ 63-3033, 63-3036A").
		Source("Form 40 instructions, tax year 2025 (EIN00046, 03-02-2026)").
		Source("Form 51, Estimated Payment of Individual Income Tax (EFO00092, 09-18-2025)").
		Source("Idaho State Tax Commission, Interest Rates").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}
