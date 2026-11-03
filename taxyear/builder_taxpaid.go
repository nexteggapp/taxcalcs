package taxyear

import (
	"fmt"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
)

// taxPaid lists each jurisdiction's payments: a sentence on what is still
// owed with its return, the withholding from each job, each payment made
// or planned, and a button that plans one.
func (v *viewer) taxPaid() []*scenariosv1.BuilderItem {
	var out []*scenariosv1.BuilderItem
	for _, e := range v.res.GetEstimated() {
		j := e.GetJurisdiction()
		at := "estimated[" + j + "]"
		item := &scenariosv1.BuilderItem{Id: at, Label: jurisdictionLabel(j) + " tax paid", Summary: v.taxPaidSummary(at) + ". " + v.stillOwed(j, at)}
		v.figures(item, at+".summary.credited")
		item.Displays = v.taxPaidDisplays(at)
		if j != "US" {
			item.Fields = append(item.Fields, v.override(at+".tax_for_year", "Tax for the year", nil))
		}
		item.Children = append(item.Children, v.withholdingRows(j)...)
		for _, s := range models(v.in.GetModel()) {
			if s.list == "tax_paid" && s.top.Get(s.top.Descriptor().Fields().ByName("jurisdiction")).String() == j {
				item.Children = append(item.Children, v.paymentRows(s)...)
				item.Actions = append(item.Actions, v.planPayment(s)...)
			}
		}
		for _, c := range append(item.GetChildren(), &scenariosv1.BuilderItem{Status: v.overrideStatus(at + ".tax_for_year")}) {
			if c.GetStatus() != nil {
				item.Status = c.GetStatus()
				break
			}
		}
		item.Questions = v.questions(at)
		out = append(out, item)
	}
	return out
}

// stillOwed says what is left to pay with a jurisdiction's return, or
// that it isn't known.
func (v *viewer) stillOwed(j, at string) string {
	ret := fmt.Sprintf("the %d %s return", v.year, map[bool]string{true: "federal", false: stateName(j)}[j == "US"])
	w := moneyAt(v.res, at+".summary.with_return")
	switch {
	case w == nil:
		return upperFirst(jurisdictionLabel(j)) + "'s tax isn't worked out, so what is left to pay with " + ret + " isn't known."
	case w.GetMinor() > 0:
		return "Still to pay with " + ret + ": " + moneyText(w) + "."
	case w.GetMinor() < 0:
		return "Refund expected with " + ret + ": " + moneyText(negated(w)) + "."
	}
	return "Nothing left to pay with " + ret + "."
}

// withholdingRows are the tax each job withholds for a jurisdiction, named
// by the job, then what other forms withhold.
func (v *viewer) withholdingRows(j string) []*scenariosv1.BuilderItem {
	var out []*scenariosv1.BuilderItem
	for _, e := range v.in.GetModel().GetEmployers() {
		path := "projection.employers[" + e.GetId() + "].year.boxes."
		if j == "US" {
			path += "federal_withheld"
		} else {
			path += "states[" + j + "].withheld"
		}
		row := v.derived("withheld["+j+"]["+e.GetId()+"]", "Withheld from "+e.GetName()+" pay", "From the job under Income", at(1, path))
		out = add(out, row)
	}
	out = add(out, v.rest("withheld["+j+"].other", "Withheld on other forms", at(1, "estimated["+j+"].summary.withholding"), out))
	return out
}

// paymentRows are a tax paid model's payments: each one made, by date,
// and each planned, which can be changed or removed. A payment the return
// doesn't count for this year is shown and not counted.
func (v *viewer) paymentRows(s sourced) []*scenariosv1.BuilderItem {
	planned := map[string]bool{}
	var out []*scenariosv1.BuilderItem
	for i := 0; s.parts != nil && i < s.parts.Len(); i++ {
		if id, k, _ := partCase(s.parts.Get(i).Message()); k == "planned_payment" {
			planned[id] = true
		}
	}
	for _, p := range v.res.GetProjection().GetTaxPaid() {
		if p.GetId() != s.id {
			continue
		}
		for i, it := range p.GetItems() {
			if planned[it.GetOrigin().GetPart()] {
				continue
			}
			out = append(out, v.paymentRow(s, i, it))
		}
	}
	for i := 0; s.parts != nil && i < s.parts.Len(); i++ {
		id, k, body := partCase(s.parts.Get(i).Message())
		if k != "planned_payment" {
			continue
		}
		pp, _ := body.Interface().(*scenariosv1.TaxPaidPart_PlannedPayment)
		path := s.path + ".parts[" + id + "]"
		out = append(out, &scenariosv1.BuilderItem{Id: path, Label: "Planned " + shortDate(pp.GetDue()), Status: v.status(path), NotCounted: true,
			ThisScenario: &scenariosv1.Figure{Amount: pp.GetAmount()}, Summary: "Not counted until it is paid",
			Fields:  v.pick(v.fields(path+".planned_payment", body, ""), map[string]string{path + ".planned_payment.due": "Due", path + ".planned_payment.amount": "Amount"}),
			Actions: []*scenariosv1.Action{{Label: "Remove", Edit: v.edit(path, &scenariosv1.Value{Kind: &scenariosv1.Value_Remove_{Remove: &scenariosv1.Value_Remove{}}})}}})
	}
	return out
}

// paymentRow is one payment made or expected, as the projection has it.
func (v *viewer) paymentRow(s sourced, i int, it *scenariosv1.Item) *scenariosv1.BuilderItem {
	f := it.GetPayment()
	row := &scenariosv1.BuilderItem{Id: fmt.Sprintf("%s#payment-%d", s.path, i), ThisScenario: &scenariosv1.Figure{Amount: f.GetAmount()}, NotCounted: !f.GetCounts()}
	switch {
	case it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED:
		row.Label, row.Summary = "Suggested "+shortDate(it.GetDate()), "The rest of the year's target, shared over the due dates. Not counted until it is paid"
	case f.GetFromLastYear():
		row.Label, row.Summary = "Last year's overpayment, "+shortDate(it.GetDate()), "Kept from last year's return for this year"
	default:
		row.Label = "Paid " + shortDate(it.GetDate())
	}
	switch p := f.GetPurpose(); {
	case p.GetEstimatedFor() != 0 && int(p.GetEstimatedFor()) != v.year:
		row.Summary = fmt.Sprintf("An estimated payment for %d, so it doesn't count here", p.GetEstimatedFor())
	case p.GetBalanceFor() != 0:
		row.Summary = fmt.Sprintf("The balance of the %d return, so it doesn't count here", p.GetBalanceFor())
	case p.GetNotTax() != nil:
		row.Summary = "Not tax, such as a fee, so it doesn't count"
	}
	return row
}

// planPayment is the button that plans a payment for a tax paid model.
func (v *viewer) planPayment(s sourced) []*scenariosv1.Action {
	var out []*scenariosv1.Action
	for _, a := range v.canAdd(s, s.path, "projection.tax_paid["+s.id+"]") {
		a.Label = "+ A payment"
		out = append(out, a)
	}
	return out
}
