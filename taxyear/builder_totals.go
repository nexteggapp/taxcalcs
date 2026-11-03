package taxyear

import (
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// The Income and Deductions groups are built so their rows add up to the
// group's total: each row shows what the return counts, and what the
// return adds that no model holds, such as last year's capital losses or
// half the self-employment tax, is a row of its own. Anything left over
// is one last row, so the rows always add up.

// derive is a figure worked out from a scenario's result, or nil.
type derive func(res *scenariosv1.ScenarioResult) *commonv1.Money

// derived is a row with no fields whose figure is worked out from the
// result, in this scenario and Your plan. It is nil when the figure is
// zero or unset in both.
func (v *viewer) derived(id, label, summary string, f derive) *scenariosv1.BuilderItem {
	this, plan := f(v.res), f(v.planRes)
	if this.GetMinor() == 0 && plan.GetMinor() == 0 {
		return nil
	}
	item := &scenariosv1.BuilderItem{Id: id, Label: label, Summary: summary}
	if this != nil {
		item.ThisScenario = &scenariosv1.Figure{Amount: this}
	}
	if plan != nil && v.planRes != nil {
		item.YourPlan = &scenariosv1.Figure{Amount: plan}
	}
	return item
}

// at derives the money at a path, times sign.
func at(sign int64, paths ...string) derive {
	return func(res *scenariosv1.ScenarioResult) *commonv1.Money {
		m := sumAt(res, paths...)
		if m == nil {
			return nil
		}
		return &commonv1.Money{Minor: sign * m.GetMinor(), Currency: m.GetCurrency()}
	}
}

// add appends the items that are not nil.
func add(out []*scenariosv1.BuilderItem, items ...*scenariosv1.BuilderItem) []*scenariosv1.BuilderItem {
	for _, it := range items {
		if it != nil {
			out = append(out, it)
		}
	}
	return out
}

// sumOf adds the counted figures of items, in this scenario or, with plan,
// in Your plan.
func sumOf(items []*scenariosv1.BuilderItem, plan bool) int64 {
	var n int64
	for _, it := range items {
		if it.GetNotCounted() {
			continue
		}
		if plan {
			n += it.GetYourPlan().GetAmount().GetMinor()
		} else {
			n += it.GetThisScenario().GetAmount().GetMinor()
		}
	}
	return n
}

// rest is the row that makes items add up to total, or nil when they do.
func (v *viewer) rest(id, label string, total derive, items []*scenariosv1.BuilderItem) *scenariosv1.BuilderItem {
	diff := func(res *scenariosv1.ScenarioResult, plan bool) *commonv1.Money {
		t := total(res)
		if t == nil {
			return nil
		}
		return &commonv1.Money{Minor: t.GetMinor() - sumOf(items, plan), Currency: t.GetCurrency()}
	}
	return v.derived(id, label, "Worked out on the return from the rows above", func(res *scenariosv1.ScenarioResult) *commonv1.Money {
		return diff(res, res == v.planRes && v.planRes != nil)
	})
}

// income is the Income group's rows: each model's figure as the return
// counts it, last year's capital losses, and a capital loss over the
// year's limit.
func (v *viewer) income() []*scenariosv1.BuilderItem {
	var out []*scenariosv1.BuilderItem
	for _, s := range models(v.in.GetModel()) {
		if s.list == "employers" {
			out = append(out, v.employerItem(s))
		}
	}
	out = append(out, v.modelItems("businesses", "accounts", "brokers", "other_income")...)
	out = add(out,
		v.derived("income.capital_loss_carryover", "Losses carried from last year", "Investment losses earlier years didn't use up",
			at(-1, "federal.schedule_d.short_term_capital_loss_carryover", "federal.schedule_d.long_term_capital_loss_carryover")),
		v.derived("income.capital_loss_limit", "Loss over this year's limit", "Moved to next year",
			at(-1, "summary.capital_loss_carried")))
	return add(out, v.rest("income.rest", "Other amounts on the return", at(1, form1040Prefix+"total_income"), out))
}

// takenOff is the Deductions group's total: the adjustments to income
// and the deductions, Form 1040 lines 10 and 14.
func (v *viewer) takenOff() *scenariosv1.FormLine {
	paths := []string{form1040Prefix + "adjustments_to_income", form1040Prefix + "total_deductions"}
	l := &scenariosv1.FormLine{Label: "Total taken off income", ThisScenario: sumAt(v.res, paths...), YourPlan: sumAt(v.planRes, paths...)}
	if l.ThisScenario == nil {
		return nil
	}
	l.Working = &scenariosv1.Working{Terms: []*scenariosv1.Term{
		{Of: &scenariosv1.Term_Path{Path: paths[0]}, Sign: 1},
		{Of: &scenariosv1.Term_Path{Path: paths[1]}, Sign: 1},
	}}
	return l
}

// deductions is the Deductions group's rows: what is taken off income
// before the deduction, the itemised and standard deductions with the one
// not used marked, and the deductions after them.
func (v *viewer) deductions() []*scenariosv1.BuilderItem {
	out := v.modelItems("retirement")
	out = add(out, v.derived("deductions.self_employment_tax", "Half of self-employment tax", "Taken off income because you pay both halves",
		at(1, "federal.schedule_1.deductible_part_of_self_employment_tax")))
	out = add(out, v.rest("deductions.other_adjustments", "Other amounts taken off income", at(1, form1040Prefix+"adjustments_to_income"), out))
	out = append(out, v.deductionChoice()...)
	out = add(out,
		v.derived("deductions.qbi", "Self-employment profit deduction", "Up to 20% of self-employment profit",
			at(1, form1040Prefix+"qualified_business_income_deduction")),
		v.derived("deductions.additional", "Other deductions", "", at(1, form1040Prefix+"additional_deductions")))
	return add(out, v.rest("deductions.rest", "Other amounts on the return", at(1, form1040Prefix+"adjustments_to_income", form1040Prefix+"total_deductions"), out))
}

// deductionChoice is itemised deductions, with the itemised models as its
// parts, and the standard deduction. The return takes one; the other is
// not counted, and each says why.
func (v *viewer) deductionChoice() []*scenariosv1.BuilderItem {
	itemized := v.res.GetFederal().GetForm_1040().GetItemized()
	std := &scenariosv1.BuilderItem{Id: "deductions.standard", Label: "Standard deduction", NotCounted: itemized}
	if itemized {
		v.figures(std, "summary.standard_deduction")
	} else {
		v.figures(std, form1040Prefix+"deduction")
	}
	parent := v.itemisedParent()
	if parent == nil {
		return []*scenariosv1.BuilderItem{std}
	}
	parent.NotCounted, parent.StartOpen = !itemized, itemized
	larger := parent.GetThisScenario().GetAmount().GetMinor() > std.GetThisScenario().GetAmount().GetMinor()
	switch {
	case itemized && larger:
		parent.Summary = "Used: more than the " + moneyText(std.GetThisScenario().GetAmount()) + " standard deduction"
		std.Summary = "Not used: itemised deductions are larger"
	case itemized:
		parent.Summary = "Used, as you chose"
		std.Summary = "Not used, as you chose"
	case larger:
		parent.Summary = "Not used, as you chose"
		std.Summary = "Used, as you chose"
	default:
		parent.Summary = "Not used: the standard deduction is larger"
		std.Summary = "Used: more than the " + moneyText(parent.GetThisScenario().GetAmount()) + " itemised"
	}
	return []*scenariosv1.BuilderItem{parent, std}
}

// itemisedParent is itemised deductions: each itemised model, state
// income tax, and what the year's limits take off, adding up to Schedule
// A's total. Nil when the household has no itemised model.
func (v *viewer) itemisedParent() *scenariosv1.BuilderItem {
	children := v.modelItems("itemised")
	if len(children) == 0 {
		return nil
	}
	children = add(children,
		v.derived("deductions.state_income_tax", "State income tax", "Withheld and paid this year",
			at(1, "summary.salt.withholding", "summary.salt.estimates", "summary.salt.last_year_balance")),
		v.derived("deductions.salt_limit", "Over the state and local tax limit", "",
			func(res *scenariosv1.ScenarioResult) *commonv1.Money {
				after := moneyAt(res, "federal.schedule_a.state_and_local_taxes_after_cap")
				before := moneyAt(res, "federal.schedule_a.total_state_and_local_taxes")
				if after == nil || before == nil {
					return nil
				}
				return &commonv1.Money{Minor: after.GetMinor() - before.GetMinor(), Currency: after.GetCurrency()}
			}),
		v.derived("deductions.bracket_limit", "Limit in the top tax bracket", "", at(-1, "summary.itemised_bracket_limit")))
	parent := &scenariosv1.BuilderItem{Id: "deductions.itemised", Label: "Itemised deductions"}
	schA := at(1, "federal.schedule_a.total_itemized_deductions")
	if schA(v.res).GetMinor() != 0 {
		children = add(children, v.rest("deductions.itemised_rest", "Other limits", schA, children))
		v.figures(parent, "federal.schedule_a.total_itemized_deductions")
	} else {
		usd := func(n int64) *scenariosv1.Figure {
			return &scenariosv1.Figure{Amount: &commonv1.Money{Minor: n, Currency: "USD"}}
		}
		parent.ThisScenario = usd(sumOf(children, false))
		if v.planRes != nil {
			parent.YourPlan = usd(sumOf(children, true))
		}
	}
	parent.Children = children
	return parent
}

// headline is the federal balance and total tax, for the top of the page.
func (v *viewer) headline() *scenariosv1.Headline {
	total := v.line(form1040Prefix+"total_tax", "Total federal tax")
	if total == nil {
		return nil
	}
	h := &scenariosv1.Headline{TotalTax: total}
	h.Balance = v.balance(v.res)
	h.Balance.ThisScenario, h.Balance.YourPlan = h.Balance.YourPlan, nil
	if v.planRes != nil {
		h.PlanBalance = v.balance(v.planRes)
	}
	return h
}

// balance is what a result owes or gets back with the federal return, its
// amount in your_plan.
func (v *viewer) balance(res *scenariosv1.ScenarioResult) *scenariosv1.FormLine {
	owed, over := moneyAt(res, form1040Prefix+"amount_owed"), moneyAt(res, form1040Prefix+"overpaid")
	l := &scenariosv1.FormLine{Path: form1040Prefix + "amount_owed", Label: "Nothing to pay", YourPlan: &commonv1.Money{Currency: "USD"}}
	switch {
	case owed.GetMinor() > 0:
		l.Label, l.YourPlan = "You owe", owed
	case over.GetMinor() > 0:
		l.Path, l.Label, l.YourPlan = form1040Prefix+"overpaid", "Your refund", over
	}
	if res == v.res {
		l.Working = v.working[l.Path]
	}
	return l
}
