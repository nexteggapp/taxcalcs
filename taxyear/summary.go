package taxyear

import (
	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/federal"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// dateKey is a date as a path key, YYYY-MM-DD.
func dateKey(d *commonv1.Date) string {
	c, err := civil.FromProto(d)
	if err != nil {
		return ""
	}
	return c.String()
}

// summarise fills the scenario's summary: the return's parts, the wages
// across employers, and each jurisdiction's tax paid against its tax.
func summarise(ctx *model.Context, in *scenariosv1.ScenarioInputs, ret *federal.Return, res *scenariosv1.ScenarioResult) {
	res.Summary = &scenariosv1.ScenarioSummary{}
	if ret != nil {
		res.Summary = ret.Summary()
	}
	var gross, pretax money.USCents
	for _, e := range res.GetProjection().GetEmployers() {
		y := e.GetYear().GetItems()
		gross += model.Value(y.GetGross())
		for _, p := range y.GetPretax() {
			pretax += model.Value(p.GetAmount())
		}
	}
	res.Summary.WagesGross, res.Summary.WagesPretax = model.Money(gross), model.Money(pretax)
	for _, e := range res.GetEstimated() {
		e.Summary = estimatedSummary(ctx, in, res, e)
	}
}

// estimatedSummary adds up one jurisdiction's tax paid against its tax.
func estimatedSummary(ctx *model.Context, in *scenariosv1.ScenarioInputs, res *scenariosv1.ScenarioResult, e *scenariosv1.Estimated) *scenariosv1.EstimatedSummary {
	j := e.GetJurisdiction()
	f := res.GetFederal().GetForm_1040()
	out := &scenariosv1.EstimatedSummary{}
	var withheld money.USCents
	if j == "US" {
		withheld = model.Value(f.GetTotalWithheld())
	} else {
		for _, emp := range res.GetProjection().GetEmployers() {
			for _, st := range emp.GetYear().GetBoxes().GetStates() {
				if st.GetState() == j {
					withheld += model.Value(st.GetWithheld())
				}
			}
		}
	}
	if j == "US" {
		// Social Security withheld over the limit is credited as withholding.
		over := model.Value(res.GetFederal().GetSchedule_3().GetExcessSocialSecurityWithheld())
		out.SocialSecurityOver = model.Money(over)
		withheld += over
	}
	out.Withholding = model.Money(withheld)
	paid, expected, balance := payments(ctx, in, res, j, out)
	// Payments to come are planned, not paid: the return credits the
	// payments made.
	out.Estimated, out.Paid, out.Expected = model.Money(paid), model.Money(paid), model.Money(expected)
	credited := withheld + paid
	out.Credited = model.Money(credited)
	// Set when nothing is applied too, as the amount with the return is
	// the tax less what is credited, plus it.
	applied := appliedToNext(ctx, j, f)
	out.AppliedToNextYear = model.Money(applied)
	tax, known := model.Cents(e.GetTaxForYear())
	if j == "US" && f != nil {
		tax, known = model.Value(f.GetTotalTax()), true
		excess := model.Value(f.GetTotalTax()) - model.Value(e.GetTaxForYear())
		out.EarlyDistributionTax = res.GetFederal().GetSchedule_2().GetTaxOnIrasAndTaxFavouredAccounts()
		out.ExcessContributionTax, out.PaymentsTax = model.Money(excess), e.GetTaxForYear()
	}
	if known {
		out.WithReturn = model.Money(tax - credited + applied)
		if balance != nil {
			out.LeftWithReturn = model.Money(tax - credited + applied - *balance)
		}
	}
	return out
}

// payments adds up a jurisdiction's payments that count for the year,
// made and still to come, lists those planned ahead, and returns the
// balance payments made toward the year's return, or nil.
func payments(ctx *model.Context, in *scenariosv1.ScenarioInputs, res *scenariosv1.ScenarioResult, j string, out *scenariosv1.EstimatedSummary) (paid, expected money.USCents, balance *money.USCents) {
	defaults := map[string]string{}
	for _, m := range in.GetModel().GetTaxPaid() {
		defaults[m.GetId()] = m.GetDefaultPart().GetId()
	}
	for _, t := range res.GetProjection().GetTaxPaid() {
		if t.GetJurisdiction() != j {
			continue
		}
		for _, it := range t.GetItems() {
			p := it.GetPayment()
			if p.GetPurpose().GetBalanceFor() == int32(ctx.Year()) && it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
				b := model.Value(p.GetAmount())
				if balance != nil {
					b += *balance
				}
				balance = &b
			}
			if !p.GetCounts() {
				continue
			}
			v := model.Value(p.GetAmount())
			if it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
				paid += v
				continue
			}
			expected += v
			out.Planned = append(out.Planned, &scenariosv1.PlannedPayment{Id: dateKey(it.GetDate()), Due: it.GetDate(), Amount: p.GetAmount(),
				Set: it.GetOrigin().GetPart() != defaults[t.GetId()]})
		}
	}
	return paid, expected, balance
}

// appliedToNext is the overpayment the year's filed return applied to next
// year, once the year is over.
func appliedToNext(ctx *model.Context, j string, f *form1040.Form1040) money.USCents {
	if !ctx.Finished() {
		return 0
	}
	if j == "US" {
		return model.Value(f.GetAppliedToNextYear())
	}
	for _, d := range ctx.Facts().GetUnassigned() {
		if !model.Usable(d.GetSource()) {
			continue
		}
		if r := d.GetCaliforniaReturn().GetForm_540(); j == "CA" && r != nil && int(r.GetTaxYear()) == ctx.Year() {
			return model.Value(r.GetAppliedToNextYearEstimatedTax())
		}
		if r := d.GetOregonReturn().GetFormOr40(); j == "OR" && r != nil && int(r.GetTaxYear()) == ctx.Year() {
			return model.Value(r.GetAppliedToNextYear())
		}
	}
	return 0
}
