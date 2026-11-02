// Package federal computes a scenario's federal return with the rules
// engine, returns/rules/federal: [Inputs] turns the projection into the
// engine's ReturnInputs, and [Compute] runs the engine and reads what the
// taxyear layer needs from its ReturnOutputs. See docs/returns.md.
package federal

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	rules "github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// ErrNoRules is returned for a tax year the rules engine has no parameters
// for.
var ErrNoRules = fmt.Errorf("federal: no rules for this tax year")

// years are the tax years the rules engine has parameters for.
var years = map[int]bool{2025: true, 2026: true}

// HasRules reports whether the rules engine has parameters for a year.
func HasRules(year int) bool { return years[year] }

// Return is a scenario's federal return and what went into it. It is made
// by [Compute].
type Return struct {
	inputs  *returnsv1.ReturnInputs
	out     *returnsv1.ReturnOutputs
	form    *factsv1.FederalReturn
	summary *scenariosv1.ScenarioSummary
	work    []*scenariosv1.Working
	notes   model.Notes
	status  filing.Status
}

// Form returns the federal return.
func (r *Return) Form() *factsv1.FederalReturn { return r.form }

// Inputs returns what the rules engine was given.
func (r *Return) Inputs() *returnsv1.ReturnInputs { return r.inputs }

// Outputs returns what the rules engine gave back.
func (r *Return) Outputs() *returnsv1.ReturnOutputs { return r.out }

// Working returns each line's working, by path.
func (r *Return) Working() []*scenariosv1.Working { return r.work }

// Notes returns the questions the return raised and what it leaves out.
func (r *Return) Notes() *model.Notes { return &r.notes }

// Summary returns the totals and limits no form line holds. It is shared
// with the Return.
func (r *Return) Summary() *scenariosv1.ScenarioSummary { return r.summary }

// Status is the filing status the return uses.
func (r *Return) Status() filing.Status { return r.status }

// AGI is adjusted gross income, Form 1040 line 11.
func (r *Return) AGI() money.USCents { return model.Value(r.form.GetForm_1040().GetAdjustedGrossIncome()) }

// TotalTax is Form 1040 line 24.
func (r *Return) TotalTax() money.USCents { return model.Value(r.form.GetForm_1040().GetTotalTax()) }

// ExcessSocialSecurity is Schedule 3 line 11: Social Security tax withheld
// across a person's jobs over the year's maximum.
func (r *Return) ExcessSocialSecurity() money.USCents {
	return model.Value(r.form.GetSchedule_3().GetExcessSocialSecurityWithheld())
}

// Tax2210 is the tax the estimated-payment rules measure, Form 2210 line 4:
// lines 22 and 23 less the refundable credits, leaving out the § 4973
// taxes on excess contributions, which § 6654(f) doesn't count.
func (r *Return) Tax2210() money.USCents {
	f := r.form.GetForm_1040()
	tax := model.Value(f.GetTaxAfterCredits()) + model.Value(f.GetOtherTaxes())
	refundable := model.Value(f.GetEarnedIncomeCredit()) + model.Value(f.GetAdditionalChildTaxCredit()) +
		model.Value(f.GetAmericanOpportunityCredit()) + model.Value(r.form.GetSchedule_3().GetNetPremiumTaxCredit()) +
		model.Value(r.form.GetSchedule_3().GetCreditForFederalTaxOnFuels())
	var excess money.USCents
	for _, x := range r.form.GetForm_5329() {
		excess += model.Value(x.GetAdditionalTaxOnTraditionalIraExcess()) + model.Value(x.GetAdditionalTaxOnRothIraExcess()) +
			model.Value(x.GetAdditionalTaxOnCoverdellEsaExcess()) + model.Value(x.GetAdditionalTaxOnArcherMsaExcess()) +
			model.Value(x.GetAdditionalTaxOnHsaExcess()) + model.Value(x.GetAdditionalTaxOnAbleExcess())
	}
	return max(0, tax-refundable-excess)
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
		f.Refunded, f.AppliedToNextYear = nil, nil
		return
	}
	applied := min(over, max(0, model.Value(filed.GetAppliedToNextYear())))
	f.AppliedToNextYear, f.Refunded = model.Money(applied), model.Money(over-applied)
}

// Compute runs the rules engine on a scenario's projection. It returns
// ErrNoRules for a year the engine has no parameters for.
func Compute(ctx *model.Context, proj *scenariosv1.YearProjection, in *scenariosv1.ScenarioInputs) (*Return, error) {
	if !HasRules(ctx.Year()) {
		return nil, ErrNoRules
	}
	eng, err := rules.DefaultEngine()
	if err != nil {
		return nil, err
	}
	status, asked := FilingStatus(in.GetFacts())
	inputs, notes := Inputs(ctx, proj, in)
	out, err := eng.Prepare(inputs)
	if err != nil {
		return nil, fmt.Errorf("rules engine: %w", err)
	}
	r := &Return{inputs: inputs, out: out, form: out.GetFederal().GetForms(), status: status}
	if r.form == nil {
		r.form = &factsv1.FederalReturn{}
	}
	if r.form.Form_1040 == nil {
		r.form.Form_1040 = &form1040.Form1040{TaxYear: int32(ctx.Year())}
	}
	if asked {
		r.notes.Ask(model.NewQuestion("federal/filing-status", "federal.form_1040.filing_status",
			"What is your filing status?", "Single is used until you say.", "facts.elections"))
	}
	r.notes.Merge(notes)
	r.work = working(out)
	r.diagnostics()
	r.summary = summarise(ctx, proj, in, r)
	return r, nil
}

