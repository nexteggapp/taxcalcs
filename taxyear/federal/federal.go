// Package federal fills a scenario's federal return from its projection's
// year totals, through ustaxcalc, in the order Form 1040 takes them:
// self-employment tax, Schedule D, adjusted gross income, the deduction,
// taxable income and the tax.
//
// It fills the return lines a caller displays, as docs/results.md describes.
// The rules are ustaxcalc's. The few this package states itself, the medical
// floor and the state and local tax limit, name their authority and belong in
// ustaxcalc.
package federal

import (
	"fmt"
	"strings"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// Return is a scenario's federal return and what went into it. It is made
// by [Compute] and finished by [Return.Pay].
type Return struct {
	form          *factsv1.FederalReturn
	summary       *scenariosv1.ScenarioSummary
	work          []*scenariosv1.Working
	notes         model.Notes
	status        filing.Status
	agi           money.USCents
	tax2210       money.USCents
	total         money.USCents
	excess        money.USCents
	stateWithheld map[string]money.USCents
}

// Form returns the federal return.
func (r *Return) Form() *factsv1.FederalReturn { return r.form }

// Working returns each line's working, by path.
func (r *Return) Working() []*scenariosv1.Working { return r.work }

// Notes returns the questions the return raised and what it leaves out.
func (r *Return) Notes() *model.Notes { return &r.notes }

// Status is the filing status the return uses.
func (r *Return) Status() filing.Status { return r.status }

// AGI is adjusted gross income, Form 1040 line 11.
func (r *Return) AGI() money.USCents { return r.agi }

// Tax2210 is the tax the estimated-payment rules measure: Form 2210 line 2,
// which leaves out the § 4973 excess contributions tax.
func (r *Return) Tax2210() money.USCents { return r.tax2210 }

// TotalTax is Form 1040 line 24.
func (r *Return) TotalTax() money.USCents { return r.total }

// ExcessSocialSecurity is Schedule 3 line 11: Social Security tax withheld
// across a person's jobs over the year's maximum.
func (r *Return) ExcessSocialSecurity() money.USCents { return r.excess }

// Pay fills the payments lines: withholding is already in; estimated adds
// line 26, the year's estimated payments, and the return is balanced into
// overpaid or owed.
func (r *Return) Pay(estimated money.USCents) {
	f := r.form.GetForm_1040()
	f.EstimatedTaxPayments = model.Money(estimated)
	payments := model.Value(f.GetTotalWithheld()) + estimated + r.excess
	f.TotalOtherPayments = model.Money(r.excess)
	f.TotalPayments = model.Money(payments)
	f.Overpaid = model.Money(max(0, payments-r.total))
	f.AmountOwed = model.Money(max(0, r.total-payments))
	r.term("federal.form_1040.total_payments", nil, "federal.form_1040.total_withheld", "federal.form_1040.estimated_tax_payments", "federal.schedule_3.excess_social_security_withheld")
}

// SplitOverpayment divides line 34, the overpayment, into line 35a, the
// refund, and line 36, applied to next year's estimated tax, as the filed
// return divides it: the filed return's line 36, held to this overpayment,
// and the rest refunded. Without a filed return the overpayment is left
// whole, since dividing it is the household's choice.
func (r *Return) SplitOverpayment(filed *form1040.Form1040) {
	f := r.form.GetForm_1040()
	over := model.Value(f.GetOverpaid())
	if filed == nil || over <= 0 {
		return
	}
	applied := min(over, max(0, model.Value(filed.GetAppliedToNextYear())))
	f.AppliedToNextYear, f.Refunded = model.Money(applied), model.Money(over-applied)
}

// FilingStatus returns the household's elected filing status, or single
// and true when none is elected.
func FilingStatus(facts *factsv1.TaxYearFacts) (filing.Status, bool) {
	for _, e := range facts.GetElections() {
		if s := e.GetFilingStatus(); s != form1040.FilingStatus_FILING_STATUS_UNSPECIFIED {
			return statusOf(s), false
		}
	}
	return filing.Single, true
}

func statusOf(s form1040.FilingStatus) filing.Status {
	switch s {
	case form1040.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY:
		return filing.MarriedFilingJointly
	case form1040.FilingStatus_FILING_STATUS_MARRIED_FILING_SEPARATELY:
		return filing.MarriedFilingSeparately
	case form1040.FilingStatus_FILING_STATUS_HEAD_OF_HOUSEHOLD:
		return filing.HeadOfHousehold
	case form1040.FilingStatus_FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE:
		return filing.SurvivingSpouse
	}
	return filing.Single
}

func protoStatus(s filing.Status) form1040.FilingStatus {
	switch s {
	case filing.MarriedFilingJointly:
		return form1040.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY
	case filing.MarriedFilingSeparately:
		return form1040.FilingStatus_FILING_STATUS_MARRIED_FILING_SEPARATELY
	case filing.HeadOfHousehold:
		return form1040.FilingStatus_FILING_STATUS_HEAD_OF_HOUSEHOLD
	case filing.SurvivingSpouse:
		return form1040.FilingStatus_FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE
	}
	return form1040.FilingStatus_FILING_STATUS_SINGLE
}

// Compute fills the federal return from a scenario's projection. It returns
// ustaxcalc.ErrNoTable for a year this build has no tables for.
func Compute(ctx *model.Context, proj *scenariosv1.YearProjection, inputs *scenariosv1.ScenarioInputs) (*Return, error) {
	facts := inputs.GetFacts()
	if facts == nil {
		facts = &factsv1.TaxYearFacts{}
	}
	status, asked := FilingStatus(facts)
	if _, err := ustaxcalc.TableFor(ctx.Year(), status); err != nil {
		return nil, err
	}
	r := &Return{status: status, summary: &scenariosv1.ScenarioSummary{}, form: &factsv1.FederalReturn{
		Form_1040:  &form1040.Form1040{TaxYear: int32(ctx.Year()), FilingStatus: protoStatus(status)},
		Schedule_1: &form1040.Schedule1{TaxYear: int32(ctx.Year())},
		Schedule_2: &form1040.Schedule2{TaxYear: int32(ctx.Year())},
		Schedule_3: &form1040.Schedule3{TaxYear: int32(ctx.Year())},
		ScheduleA:  &form1040.ScheduleA{TaxYear: int32(ctx.Year())},
		ScheduleD:  &form1040.ScheduleD{TaxYear: int32(ctx.Year())},
	}, stateWithheld: map[string]money.USCents{}}
	if asked {
		r.notes.Ask(model.NewQuestion("federal/filing-status", "federal.form_1040.filing_status",
			"What is your filing status?", "Single is used until you say.", "facts.elections"))
	}
	c := &calc{ctx: ctx, proj: proj, inputs: inputs, facts: facts, r: r, hsaLimits: map[string]money.USCents{}}
	if err := c.run(); err != nil {
		return nil, err
	}
	return r, nil
}

// calc holds one computation's running figures.
type calc struct {
	ctx    *model.Context
	proj   *scenariosv1.YearProjection
	inputs *scenariosv1.ScenarioInputs
	facts  *factsv1.TaxYearFacts
	r      *Return

	// people is each person's payroll figures across their jobs.
	people map[string]*payrollPerson
	// seTax and seHalf are self-employment tax and its deductible half.
	seTax, seHalf money.USCents
	// income is total income, Form 1040 line 9, as it is built.
	income money.USCents
	// gains are Schedule D's net figures.
	gains ustaxcalc.CapitalNet
	// qualified is qualified dividends, line 3a.
	qualified money.USCents
	// early is the 10% additional tax on early distributions.
	early money.USCents
	// excessIRA is the § 4973 tax on excess IRA contributions.
	excessIRA money.USCents
	// magiBase is income less the adjustments other than the IRA
	// deduction, which § 219 measures against.
	hsa money.USCents
	// charity is the charitable part of itemised deductions.
	charity  money.USCents
	itemised money.USCents
	taken    ustaxcalc.Deduction
	// refundsAll is the state refunds whose year is unsaid, and
	// refundsTaxable what of them the return counts.
	refundsAll, refundsTaxable money.USCents
	// itemisedAll is the state refunds whose own model says their year
	// itemised, and itemisedTaxable what of them the return counts.
	itemisedAll, itemisedTaxable money.USCents
	// hsaLimits is each person's HSA limit, where it is known.
	hsaLimits map[string]money.USCents
}

// payrollPerson is one person's W-2 figures across their jobs.
type payrollPerson struct {
	ssWages, medicareWages money.USCents
	ssWithheld             []money.USCents
	covered                bool
}

func (c *calc) run() error {
	steps := []func() error{c.wages, c.selfEmployment, c.investments, c.other, c.adjustments, c.deduction, c.tax, c.summarise}
	for _, s := range steps {
		if err := s(); err != nil {
			return err
		}
	}
	return nil
}

// add records a calculator's rules on a line's working, and its questions.
func (c *calc) add(path string, applied []working.Rule, required []working.Required) {
	w := c.r.working(path)
	for _, a := range applied {
		w.Rules = append(w.Rules, rule(a.Cite(), a.Applied()))
	}
	for i, q := range required {
		text := q.Question()
		if text == "" {
			text = q.Because()
		}
		effect := q.Effect()
		if effect == "" {
			effect = q.Because()
		}
		c.r.notes.Ask(model.NewQuestion(fmt.Sprintf("federal/%s/%s/%d", path, strings.ReplaceAll(q.Position().String(), " ", "-"), i), path, text, effect, "facts.positions"))
	}
}

func (r *Return) working(path string) *scenariosv1.Working {
	for _, w := range r.work {
		if w.GetPath() == path {
			return w
		}
	}
	w := &scenariosv1.Working{Path: path}
	r.work = append(r.work, w)
	return w
}

// rule is an applied rule whose sentence states the figures at paths.
func rule(cite, applied string, states ...string) *scenariosv1.AppliedRule {
	return &scenariosv1.AppliedRule{Cite: cite, Applied: &scenariosv1.Statement{Text: applied, States: states}}
}

// minus records that a line takes away other figures, by path.
func (r *Return) minus(path string, paths ...string) {
	w := r.working(path)
	for _, p := range paths {
		w.Terms = append(w.Terms, &scenariosv1.Term{Of: &scenariosv1.Term_Path{Path: p}, Sign: -1})
	}
}

// term records that a line adds up other figures, by path.
func (r *Return) term(path string, items []string, paths ...string) {
	w := r.working(path)
	for _, p := range paths {
		w.Terms = append(w.Terms, &scenariosv1.Term{Of: &scenariosv1.Term_Path{Path: p}, Sign: 1})
	}
	for _, it := range items {
		w.Terms = append(w.Terms, &scenariosv1.Term{Of: &scenariosv1.Term_Item{Item: it}, Sign: 1})
	}
}
