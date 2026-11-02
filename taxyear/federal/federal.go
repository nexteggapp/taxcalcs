// Package federal computes a scenario's federal return with the rules
// engine, returns/rules/federal: [Inputs] turns the projection into the
// engine's ReturnInputs, and [Compute] runs the engine and reads what the
// taxyear layer needs from its ReturnOutputs. See docs/returns.md.
package federal

import (
	"context"
	"fmt"
	"sync"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/preparer"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	"google.golang.org/protobuf/proto"
)

// ErrNoRules is returned for a tax year the rules engine has no parameters
// for.
var ErrNoRules = fmt.Errorf("federal: no rules for this tax year")

// HasRules reports whether the rules engine has parameters for a year.
func HasRules(year int) bool {
	s, err := server()
	if err != nil {
		return false
	}
	resp, err := s.ListTaxYears(context.Background(), &returnsv1.ListTaxYearsRequest{})
	if err != nil {
		return false
	}
	for _, y := range resp.GetTaxYears() {
		if int(y.GetTaxYear()) == year {
			return true
		}
	}
	return false
}

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
func (r *Return) AGI() money.USCents {
	return model.Value(r.form.GetForm_1040().GetAdjustedGrossIncome())
}

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
	return max(0, tax-refundable-ExcessContributionTax(r.form))
}

// ExcessContributionTax is the § 4973 tax on excess contributions, Form
// 5329 lines 25, 33, 41, 49 and 51, across the return's Forms 5329.
func ExcessContributionTax(f *factsv1.FederalReturn) money.USCents {
	var t money.USCents
	for _, x := range f.GetForm_5329() {
		t += model.Value(x.GetAdditionalTaxOnTraditionalIraExcess()) + model.Value(x.GetAdditionalTaxOnRothIraExcess()) +
			model.Value(x.GetAdditionalTaxOnCoverdellEsaExcess()) + model.Value(x.GetAdditionalTaxOnArcherMsaExcess()) +
			model.Value(x.GetAdditionalTaxOnHsaExcess()) + model.Value(x.GetAdditionalTaxOnAbleExcess())
	}
	return t
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

var (
	serverOnce sync.Once
	srv        *preparer.Server
	srvErr     error
)

// server is the ReturnPreparer over the shared rules engine, which is
// loaded and typechecked once.
func server() (*preparer.Server, error) {
	serverOnce.Do(func() { srv, srvErr = preparer.NewServer() })
	return srv, srvErr
}

// Load loads and typechecks the rules engine, which Compute otherwise does
// the first time it runs.
func Load() error {
	_, err := server()
	return err
}

// Compute runs the rules engine on a scenario's projection, with the
// engine's line-by-line working when withWorking is set. It returns
// ErrNoRules for a year the engine has no parameters for.
func Compute(ctx *model.Context, proj *scenariosv1.YearProjection, in *scenariosv1.ScenarioInputs, withWorking bool) (*Return, error) {
	if !HasRules(ctx.Year()) {
		return nil, ErrNoRules
	}
	s, err := server()
	if err != nil {
		return nil, err
	}
	status, asked := FilingStatus(in.GetFacts())
	inputs, notes := Inputs(ctx, proj, in)
	resp, err := s.Prepare(context.Background(), &returnsv1.PrepareRequest{Inputs: inputs, IncludeWorking: withWorking})
	if err != nil {
		return nil, fmt.Errorf("rules engine: %w", err)
	}
	out := resp.GetOutputs()
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

// priorStandardDeduction is last year's standard deduction for last
// year's return's filing status and the household's ages and blindness,
// from the rules engine. It reports false for a year without rules.
func priorStandardDeduction(ctx *model.Context, f *form1040.Form1040) (*commonv1.Money, bool) {
	year := ctx.Year() - 1
	if !HasRules(year) {
		return nil, false
	}
	s, err := server()
	if err != nil {
		return nil, false
	}
	status := statusOf(f.GetFilingStatus())
	hh := &returnsv1.Household{MaritalStatus: maritalOf(status), Taxpayer: &returnsv1.Person{Id: youID, Blind: proto.Bool(f.GetFilerBlind())}}
	if status == filing.MarriedFilingJointly || status == filing.MarriedFilingSeparately {
		hh.Spouse = &returnsv1.Person{Id: spouseID, Blind: proto.Bool(f.GetSpouseBlind())}
	}
	for _, p := range ctx.Facts().GetPeople() {
		switch {
		case p.GetRelationship() == factsv1.Relationship_RELATIONSHIP_YOU && p.GetDateOfBirth().GetYear() != 0:
			hh.Taxpayer.DateOfBirth = p.GetDateOfBirth()
		case p.GetRelationship() == factsv1.Relationship_RELATIONSHIP_SPOUSE && hh.Spouse != nil && p.GetDateOfBirth().GetYear() != 0:
			hh.Spouse.DateOfBirth = p.GetDateOfBirth()
		}
	}
	resp, err := s.Prepare(context.Background(), &returnsv1.PrepareRequest{Inputs: &returnsv1.ReturnInputs{TaxYear: int32(year),
		Federal: &returnsv1.Federal1040Inputs{Household: hh, Elections: &returnsv1.Elections{FilingStatus: electionOf(status)}}}})
	if err != nil {
		return nil, false
	}
	return resp.GetOutputs().GetFederal().GetWorkpapers().GetStandardDeduction().GetEffectiveStandardDeduction(), true
}
