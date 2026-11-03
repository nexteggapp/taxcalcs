package taxyear

import (
	"strings"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// negated is a new amount of the opposite sign, or nil.
func negated(m *commonv1.Money) *commonv1.Money {
	if m == nil {
		return nil
	}
	return &commonv1.Money{Minor: -m.GetMinor(), Currency: m.GetCurrency()}
}

// modelSummary is one line on how a model's figure is made, for the row
// while it is closed, or "" when the figure says it all.
func (v *viewer) modelSummary(s sourced, proj string) string {
	m := func(p string) string { return moneyText(moneyAt(v.res, proj+"."+p)) }
	switch s.list {
	case "businesses":
		return v.businessSummary(s, proj)
	case "accounts":
		var parts []string
		for _, p := range []struct {
			label string
			paths []string
		}{
			{"interest", []string{"year.items.interest", "year.items.treasury_interest"}},
			{"dividends", []string{"year.items.ordinary_dividends"}},
			{"fund gains paid out", []string{"year.items.capital_gain_distributions"}},
		} {
			var full []string
			for _, x := range p.paths {
				full = append(full, proj+"."+x)
			}
			if a := sumAt(v.res, full...); a.GetMinor() != 0 {
				parts = append(parts, p.label+" "+moneyText(a))
			}
		}
		out := upperFirst(joinAnd(parts))
		if q := moneyAt(v.res, proj+".year.boxes.qualified_dividends"); q.GetMinor() != 0 {
			out += ". " + moneyText(q) + " of the dividends are qualified, taxed at the lower rate"
		}
		return out
	case "brokers":
		return "Sold for " + m("year.items.proceeds") + "; bought for " + m("year.items.basis")
	case "retirement":
		put := sumAt(v.res, proj+".year.items.traditional_ira", proj+".year.items.roth_ira", proj+".year.items.hsa")
		if put.GetMinor() == 0 {
			return "Nothing put in outside payroll"
		}
		person := s.top.Get(s.top.Descriptor().Fields().ByName("person")).String()
		return "Put in " + moneyText(put) + "; " + moneyText(moneyAt(v.res, "summary.retirement["+person+"].deducted")) + " comes off income"
	case "itemised":
		return v.itemisedSummary(s, proj)
	}
	return ""
}

// itemisedSummary says how much of an itemised deduction counts, and its
// limit in words, or how much is paid so far and still to come.
func (v *viewer) itemisedSummary(s sourced, proj string) string {
	year := moneyAt(v.res, proj+".year.items")
	floor := moneyAt(v.res, "summary.itemised["+s.id+"].floor")
	agi := moneyAt(v.res, form1040Prefix+"adjusted_gross_income").GetMinor()
	switch waysKey(s) {
	case "charity":
		if floor.GetMinor() == 0 {
			break
		}
		return moneyText(year) + " given this year. " + moneyText(floor) + " doesn't count: from 2026 only gifts above 0.5% of income count"
	case "medical":
		limit := &commonv1.Money{Minor: (agi*75/1000 + 50) / 100 * 100, Currency: "USD"}
		out := moneyText(year) + " this year. Only costs above 7.5% of income, " + moneyText(limit) + ", count"
		if moneyAt(v.res, "summary.itemised["+s.id+"].counted").GetMinor() == 0 {
			out += ", so none does"
		}
		return out
	}
	soFar, rest := moneyAt(v.res, proj+".so_far.items"), moneyAt(v.res, proj+".rest.items")
	if rest.GetMinor() == 0 {
		return moneyText(soFar) + " paid so far"
	}
	return moneyText(soFar) + " paid so far; " + moneyText(rest) + " still to come"
}

// businessSummary is a business's heading: "Sales $31,350 less costs
// $5,349. Adds $3,673 in self-employment tax." The tax is named only when
// the person has one business, as it is worked out per person.
func (v *viewer) businessSummary(s sourced, proj string) string {
	out := "Sales " + moneyText(moneyAt(v.res, proj+".year.boxes.gross_receipts")) + " less costs " + moneyText(moneyAt(v.res, proj+".year.boxes.total_expenses")) + "."
	person := s.top.Get(s.top.Descriptor().Fields().ByName("person")).String()
	n := 0
	for _, b := range v.in.GetModel().GetBusinesses() {
		if b.GetPerson() == person {
			n++
		}
	}
	if se := moneyAt(v.res, "federal.schedule_2.self_employment_tax"); n == 1 && se.GetMinor() != 0 {
		out += " Adds " + moneyText(se) + " in self-employment tax."
	}
	return out
}

// upperFirst raises a sentence's first letter.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// taxPaidSummary says what a jurisdiction's payments are: "Withheld from
// pay $18,637 and estimated payments $4,205".
func (v *viewer) taxPaidSummary(at string) string {
	var parts []string
	if w := moneyAt(v.res, at+".summary.withholding"); w.GetMinor() != 0 {
		parts = append(parts, "withheld from pay "+moneyText(w))
	}
	if e := moneyAt(v.res, at+".summary.estimated"); e.GetMinor() != 0 {
		parts = append(parts, "estimated payments "+moneyText(e))
	}
	if len(parts) == 0 {
		return "Nothing paid yet"
	}
	return upperFirst(joinAnd(parts))
}

// taxPaidLines are the figures shown in a jurisdiction's open row, with
// plain labels, in order.
var taxPaidLines = []struct{ field, label string }{
	{"withholding", "Withheld from pay"},
	{"estimated", "Estimated payments counted"},
	{"expected", "Payments still to come"},
	{"applied_to_next_year", "Kept for next year"},
	{"left_with_return", "Left after payments toward the balance"},
}

// taxPaidDisplays are a jurisdiction's tax for the year, its payments and
// what is left to pay or refunded with the return.
func (v *viewer) taxPaidDisplays(at string) []*scenariosv1.Display {
	var lines []*scenariosv1.FormLine
	lines = append(lines, v.line(at+".tax_for_year", "Tax for the year"))
	for _, f := range taxPaidLines {
		lines = append(lines, v.line(at+".summary."+f.field, f.label))
	}
	if l := v.line(at+".summary.with_return", "Left to pay with the return"); l != nil {
		if l.GetThisScenario().GetMinor() < 0 {
			l.Label = "Refund"
			l.ThisScenario, l.YourPlan = negated(l.GetThisScenario()), negated(l.GetYourPlan())
		}
		lines = append(lines, l)
	}
	var out []*scenariosv1.Display
	for _, l := range lines {
		if l != nil && (l.GetThisScenario().GetMinor() != 0 || l.GetYourPlan().GetMinor() != 0) {
			out = append(out, &scenariosv1.Display{Kind: &scenariosv1.Display_Figure{Figure: l}})
		}
	}
	return out
}
