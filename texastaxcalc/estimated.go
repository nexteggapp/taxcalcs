package texastaxcalc

import (
	"errors"
	"fmt"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/working"
)

// Texas levies no individual income tax, so its estimated tax rules are that
// there is nothing to estimate. They still answer the shared
// [estimated.EstimatedTax] questions, so that a caller asking about any state
// gets an answer rather than an error: no payments are required, there are no
// due dates, and the penalty is zero.
//
// Every state with no individual income tax is built in this same shape, each
// in its own package, because a jurisdiction package imports no other.

// jurisdiction is Texas's postal code.
const jurisdiction = estimated.Texas

// estimatedYear is one tax year's finding and the documents it rests on.
type estimatedYear struct {
	year int

	// why is what Requirement says, in plain words.
	why string

	// basis is each document the finding rests on: its citation, and what
	// it says, quoted.
	basis []finding
}

type finding struct {
	cite, says string
}

var estimatedYears = map[int]estimatedYear{}

func registerEstimated(y estimatedYear) { estimatedYears[y.year] = y }

// EstimatedTax returns Texas's estimated tax rules for a tax year, or
// [estimated.ErrNoRules] for a year this build does not carry.
func EstimatedTax(year int) (estimated.EstimatedTax, error) {
	y, ok := estimatedYears[year]
	if !ok {
		return nil, fmt.Errorf("%w: %s %d", estimated.ErrNoRules, jurisdiction, year)
	}
	return texasEstimated{y}, nil
}

type texasEstimated struct{ y estimatedYear }

func (r texasEstimated) check(f estimated.Facts) error {
	switch {
	case f.Jurisdiction() != jurisdiction:
		return fmt.Errorf("texastaxcalc: these are Texas's rules, and the facts are for %s", f.Jurisdiction())
	case f.TaxYear() != r.y.year:
		return fmt.Errorf("texastaxcalc: these are the %d rules, and the facts are for %d", r.y.year, f.TaxYear())
	case f.State() != nil:
		return fmt.Errorf("texastaxcalc: Texas's rules read no extras, and the facts carry %T", f.State())
	}
	return nil
}

// notes cite each document the finding rests on.
func (r texasEstimated) notes() working.Notes {
	var n working.Notes
	for _, b := range r.y.basis {
		n.Rule(b.cite, b.says)
	}
	return n
}

// Requirement is never required and always exempt: with no tax there is no
// target, and no penalty can arise.
func (r texasEstimated) Requirement(f estimated.Facts) (working.Result[estimated.Requirement], error) {
	if err := r.check(f); err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	req, err := r.requirement()
	if err != nil {
		return working.Result[estimated.Requirement]{}, err
	}
	return working.NewResult(req).With(r.notes()).Build()
}

// requirement is the exception that there is no income tax, as the target.
func (r texasEstimated) requirement() (estimated.Requirement, error) {
	none, err := estimated.NewException("No income tax").Build()
	if err != nil {
		return estimated.Requirement{}, err
	}
	return estimated.NewRequirement().Required(false, r.y.why).Exempt(none).Build()
}

// Schedule has no installments, because nothing is due.
func (r texasEstimated) Schedule(f estimated.Facts) (working.Result[[]estimated.Installment], error) {
	if err := r.check(f); err != nil {
		return working.Result[[]estimated.Installment]{}, err
	}
	return working.NewResult([]estimated.Installment{}).With(r.notes()).Build()
}

// Penalty is zero, figured to asOf.
func (r texasEstimated) Penalty(f estimated.Facts, asOf estimated.Date) (working.Result[estimated.Penalty], error) {
	if asOf.IsZero() {
		return working.Result[estimated.Penalty]{}, errors.New("texastaxcalc: a penalty needs the date to figure it to")
	}
	if err := r.check(f); err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	req, err := r.requirement()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	p, err := estimated.NewPenalty(asOf).Because(req, nil).Build()
	if err != nil {
		return working.Result[estimated.Penalty]{}, err
	}
	return working.NewResult(p).With(r.notes()).Build()
}

func (r texasEstimated) Explanation() estimated.Explanation { return texasExplanation }

// texasExplanation is written by hand and checked against
// research/estimated-payments/tx.md.
var texasExplanation = func() estimated.Explanation {
	e, err := estimated.NewExplanation(
		"Texas has no individual income tax. It therefore has no estimated payments and no underpayment penalty.").
		Paragraph("Texas does not tax the income of individuals. Texas voters adopted Proposition 4 in November 2019. Since then, the Texas constitution bars a tax on the net incomes of individuals. The bar covers an individual's share of partnership income. With nothing to pay the state during the year, nothing can be penalised as late.").
		VersusFederal("Federal estimated tax applies exactly as usual to Texas residents. Texas adds nothing to it.").
		Source("Tex. Const. art. VIII, § 24-a, added by H.J.R. 38, 86th Leg. (2019)").
		Source("Office of the Texas Governor, proclamation of the November 5, 2019 constitutional amendment election").
		Build()
	if err != nil {
		panic(err)
	}
	return e
}()
