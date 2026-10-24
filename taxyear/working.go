package taxyear

import (
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// term is one signed figure a line adds up, by path.
type term struct {
	sign int32
	path string
}

func plus(paths ...string) []term {
	var out []term
	for _, p := range paths {
		out = append(out, term{1, p})
	}
	return out
}

func minus(paths ...string) []term {
	var out []term
	for _, p := range paths {
		out = append(out, term{-1, p})
	}
	return out
}

// formLines are the federal lines in the result that the forms define as
// sums of other lines, each with its definitions in order: the first that
// adds up to the line is its working. A line held to a floor or a limit
// adds up under none, and keeps only the rules that made it.
var formLines = map[string][][]term{
	"form_1040.total_wages": {plus("form_1040.w2_wages", "form_1040.household_employee_wages", "form_1040.unreported_tip_income",
		"form_1040.medicaid_waiver_payments", "form_1040.taxable_dependent_care_benefits", "form_1040.employer_provided_adoption_benefits",
		"form_1040.wages_from_form_8919", "form_1040.other_earned_income")},
	"form_1040.total_income": {plus("form_1040.total_wages", "form_1040.taxable_interest", "form_1040.ordinary_dividends",
		"form_1040.ira_distributions_taxable", "form_1040.pensions_and_annuities_taxable", "form_1040.social_security_benefits_taxable",
		"form_1040.capital_gain_or_loss", "form_1040.additional_income")},
	"form_1040.additional_income":     {plus("schedule_1.total_additional_income")},
	"form_1040.adjustments_to_income": {plus("schedule_1.total_adjustments")},
	"form_1040.deduction":             {plus("schedule_a.total_itemized_deductions")},
	"form_1040.total_deductions":      {plus("form_1040.deduction", "form_1040.qualified_business_income_deduction", "form_1040.additional_deductions")},
	"form_1040.taxable_income":        {append(plus("form_1040.adjusted_gross_income"), minus("form_1040.total_deductions")...)},
	"form_1040.capital_gain_or_loss":  {plus("schedule_d.net_gain_or_loss")},
	"form_1040.total_withheld":        {plus("form_1040.withheld_from_w2", "form_1040.withheld_from_1099", "form_1040.withheld_from_other_forms")},
	"form_1040.other_taxes":           {plus("schedule_2.total_part_two")},
	"form_1040.total_tax": {
		plus("form_1040.tax_after_credits", "form_1040.other_taxes"),
		plus("form_1040.tax", "form_1040.other_taxes"),
	},
	"form_1040.total_payments": {
		plus("form_1040.total_withheld", "form_1040.estimated_tax_payments", "form_1040.total_other_payments"),
		plus("form_1040.total_withheld", "form_1040.estimated_tax_payments", "schedule_3.excess_social_security_withheld"),
	},
	"form_1040.amount_owed": {append(plus("form_1040.total_tax"), minus("form_1040.total_payments")...)},
	"form_1040.overpaid":    {append(plus("form_1040.total_payments"), minus("form_1040.total_tax")...)},
	"schedule_1.total_additional_income": {plus("schedule_1.taxable_state_local_refunds", "schedule_1.alimony_received",
		"schedule_1.business_income_or_loss", "schedule_1.other_gains_or_losses", "schedule_1.rental_royalty_partnership_s_corp_trust",
		"schedule_1.farm_income_or_loss", "schedule_1.unemployment_compensation", "schedule_1.total_other_income")},
	"schedule_1.total_adjustments": {plus("schedule_1.educator_expenses", "schedule_1.reservist_performing_artist_official_expenses",
		"schedule_1.health_savings_account_deduction", "schedule_1.armed_forces_moving_expenses", "schedule_1.deductible_part_of_self_employment_tax",
		"schedule_1.self_employed_retirement_plans", "schedule_1.self_employed_health_insurance", "schedule_1.early_withdrawal_penalty",
		"schedule_1.alimony_paid", "schedule_1.ira_deduction", "schedule_1.student_loan_interest_deduction", "schedule_1.archer_msa_deduction",
		"schedule_1.total_other_adjustments")},
	"schedule_a.total_taxes_paid": {plus("schedule_a.state_and_local_taxes_after_cap", "schedule_a.other_taxes")},
	"schedule_a.total_itemized_deductions": {plus("schedule_a.deductible_medical_and_dental", "schedule_a.total_taxes_paid",
		"schedule_a.total_interest_paid", "schedule_a.total_gifts_to_charity", "schedule_a.casualty_and_theft_losses")},
}

// projectionLines are federal lines that add up one year figure from each
// model of a list, or for tax paid, from the US model: the payments made,
// so far's, as the return counts only those.
var projectionLines = map[string]struct {
	list    string
	figures []string
}{
	"form_1040.taxable_interest":       {"accounts", []string{"boxes.interest", "boxes.treasury_interest"}},
	"form_1040.ordinary_dividends":     {"accounts", []string{"boxes.ordinary_dividends"}},
	"form_1040.qualified_dividends":    {"accounts", []string{"boxes.qualified_dividends"}},
	"form_1040.tax_exempt_interest":    {"accounts", []string{"boxes.tax_exempt_interest"}},
	"form_1040.withheld_from_w2":       {"employers", []string{"boxes.federal_withheld"}},
	"form_1040.withheld_from_1099":     {"other_income", []string{"boxes.federal_withheld"}},
	"form_1040.estimated_tax_payments": {"tax_paid", []string{"so_far.counted"}},
}

// addWorkings gives every figure in the result a working: each federal line
// the forms define as a sum, and every figure of every model's projection,
// whose year is its so far and its rest, and whose so far and rest add up
// their items. A working is kept only when its terms add up to the figure;
// the figures that already have one keep it. kept holds the workings of the
// models the cache finished, by projection path; the jobs' and tax paid's are
// worked out here.
func addWorkings(res *scenariosv1.ScenarioResult, kept map[string][]*scenariosv1.Working) {
	w := newScenarioWorkings(res)
	for _, line := range sortedKeys(formLines) {
		for _, def := range formLines[line] {
			if w.add("federal."+line, prefixed("federal.", def), nil) {
				break
			}
		}
	}
	for _, line := range sortedKeys(projectionLines) {
		l := projectionLines[line]
		var ts []term
		for _, id := range projectionIDs(res, l.list, "US") {
			for _, f := range l.figures {
				// A figure names its period when it isn't the year's.
				if !strings.Contains(f, "so_far.") {
					f = "year." + f
				}
				ts = append(ts, term{1, fmt.Sprintf("projection.%s[%s].%s", l.list, id, f)})
			}
		}
		w.add("federal."+line, ts, nil)
	}
	w.projections(kept)
	res.Working = w.list
}

// modelWorkings is the workings of one model's figures, at its path.
func modelWorkings(at string, p proto.Message) []*scenariosv1.Working {
	w := &workings{byPath: map[string]*scenariosv1.Working{}, amounts: map[string]money.USCents{}}
	w.indexPeriods(at, p.ProtoReflect(), true)
	w.periods(at, p.ProtoReflect())
	return w.list
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func prefixed(prefix string, ts []term) []term {
	out := make([]term, len(ts))
	for i, t := range ts {
		out[i] = term{t.sign, prefix + t.path}
	}
	return out
}

// workings is a result's workings being added to, by path, with every
// money figure the workings read, by path.
type workings struct {
	res     *scenariosv1.ScenarioResult
	list    []*scenariosv1.Working
	byPath  map[string]*scenariosv1.Working
	amounts map[string]money.USCents
}

// newWorkings indexes every money figure of the result's return and
// projections.
func newWorkings(res *scenariosv1.ScenarioResult) *workings {
	return indexed(res, func(protoreflect.Name) bool { return true })
}

// newScenarioWorkings indexes what addWorkings reads: the return, every
// projection's year, and every period of the jobs and tax paid, whose
// workings it works out.
func newScenarioWorkings(res *scenariosv1.ScenarioResult) *workings {
	return indexed(res, worksOut)
}

// worksOut reports whether addWorkings works out a list's model workings,
// rather than the cache's finished models holding them.
func worksOut(list protoreflect.Name) bool { return list == "employers" || list == "tax_paid" }

func indexed(res *scenariosv1.ScenarioResult, allPeriods func(protoreflect.Name) bool) *workings {
	w := &workings{res: res, list: res.GetWorking(), byPath: map[string]*scenariosv1.Working{}, amounts: map[string]money.USCents{}}
	for _, x := range w.list {
		w.byPath[x.GetPath()] = x
	}
	w.index("federal", res.GetFederal().ProtoReflect(), 2)
	// The summary's own figures, such as the itemised limits, and each
	// entry of its lists by id or person.
	sum := res.GetSummary().ProtoReflect()
	w.index("summary", sum, 1)
	sum.Range(func(list protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if !list.IsList() || list.Kind() != protoreflect.MessageKind {
			return true
		}
		for i := 0; i < v.List().Len(); i++ {
			e := v.List().Get(i).Message()
			for _, k := range []protoreflect.Name{"id", "person"} {
				if kf := e.Descriptor().Fields().ByName(k); kf != nil {
					w.index(fmt.Sprintf("summary.%s[%s]", list.Name(), e.Get(kf).String()), e, 1)
				}
			}
		}
		return true
	})
	proj := res.GetProjection().ProtoReflect()
	proj.Range(func(list protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		for i := 0; i < v.List().Len(); i++ {
			p := v.List().Get(i).Message()
			at := fmt.Sprintf("projection.%s[%s]", list.Name(), p.Get(p.Descriptor().Fields().ByName("id")).String())
			w.indexPeriods(at, p, allPeriods(list.Name()))
			// A projection's own figures, such as a job's hsa_over.
			w.index(at, p, 1)
		}
		return true
	})
	return w
}

// indexPeriods indexes one projection's year, and with all, its so far and
// rest.
func (w *workings) indexPeriods(at string, p protoreflect.Message, all bool) {
	for _, period := range []protoreflect.Name{"so_far", "rest", "year"} {
		if !all && period != "year" {
			continue
		}
		if fd := p.Descriptor().Fields().ByName(period); fd != nil && p.Has(fd) {
			w.index(at+"."+string(period), p.Get(fd).Message(), 2)
		}
	}
}

// index records the money figures in m, depth messages deep, by path. A
// path into a list is not recorded.
func (w *workings) index(at string, m protoreflect.Message, depth int) {
	if !m.IsValid() {
		return
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() {
			return true
		}
		path := at + "." + string(fd.Name())
		if money, ok := v.Message().Interface().(*commonv1.Money); ok {
			w.amounts[path] = moneyOf(money)
		} else if depth > 1 {
			w.index(path, v.Message(), depth-1)
		}
		return true
	})
}

func moneyOf(m *commonv1.Money) money.USCents { return money.USCents(m.GetMinor()) }

// amount is the money figure at a path, and false when it is unset.
func (w *workings) amount(path string) (money.USCents, bool) {
	v, ok := w.amounts[path]
	return v, ok
}

// add records a figure's terms, the set ones of ts by path and items by
// id, when the figure is set, has no terms yet, and the path terms add up
// to it. It reports whether the figure has its terms now.
func (w *workings) add(path string, ts []term, items []string) bool {
	total, ok := w.amount(path)
	if !ok {
		return false
	}
	if x := w.byPath[path]; x != nil && len(x.GetTerms()) > 0 {
		return true
	}
	var sum money.USCents
	var terms []*scenariosv1.Term
	for _, t := range ts {
		v, ok := w.amount(t.path)
		if !ok {
			continue
		}
		sum += money.USCents(t.sign) * v
		terms = append(terms, &scenariosv1.Term{Of: &scenariosv1.Term_Path{Path: t.path}, Sign: t.sign})
	}
	if len(terms) == 0 && len(items) == 0 || len(terms) > 0 && sum != total {
		return false
	}
	for _, it := range items {
		terms = append(terms, &scenariosv1.Term{Of: &scenariosv1.Term_Item{Item: it}, Sign: 1})
	}
	x := w.byPath[path]
	if x == nil {
		x = &scenariosv1.Working{Path: path}
		w.byPath[path] = x
		w.list = append(w.list, x)
	}
	x.Terms = terms
	return true
}

// projectionIDs lists a projection list's model ids, in order: of a list
// whose projections name a jurisdiction, only that jurisdiction's.
func projectionIDs(res *scenariosv1.ScenarioResult, list, jurisdiction string) []string {
	proj := res.GetProjection().ProtoReflect()
	fd := proj.Descriptor().Fields().ByName(protoreflect.Name(list))
	if fd == nil {
		return nil
	}
	var out []string
	l := proj.Get(fd).List()
	for i := 0; i < l.Len(); i++ {
		m := l.Get(i).Message()
		if j := m.Descriptor().Fields().ByName("jurisdiction"); j != nil && m.Get(j).String() != jurisdiction {
			continue
		}
		out = append(out, m.Get(m.Descriptor().Fields().ByName("id")).String())
	}
	return out
}

// projections adds the workings of every model's figures, in the
// projection's order: the jobs' and tax paid's worked out here, each other
// model's as the cache finished it.
func (w *workings) projections(kept map[string][]*scenariosv1.Working) {
	proj := w.res.GetProjection().ProtoReflect()
	proj.Range(func(list protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		for i := 0; i < v.List().Len(); i++ {
			p := v.List().Get(i).Message()
			id := p.Get(p.Descriptor().Fields().ByName("id")).String()
			at := fmt.Sprintf("projection.%s[%s]", list.Name(), id)
			if worksOut(list.Name()) {
				w.periods(at, p)
				continue
			}
			w.list = append(w.list, kept[at]...)
		}
		return true
	})
}

// periods adds one model's workings.
func (w *workings) periods(at string, p protoreflect.Message) {
	year := p.Descriptor().Fields().ByName("year")
	if year == nil || !p.Has(year) {
		return
	}
	for _, f := range moneyFields(p.Get(year).Message(), "") {
		w.add(at+".year."+f, plus(at+".so_far."+f, at+".rest."+f), nil)
	}
	carried := carrying(p.Get(p.Descriptor().Fields().ByName("items")).List())
	for _, period := range []string{"so_far", "rest"} {
		fd := p.Descriptor().Fields().ByName(protoreflect.Name(period))
		if !p.Has(fd) {
			continue
		}
		for _, f := range moneyFields(p.Get(fd).Message(), "") {
			if ids := carried[period+"."+f]; len(ids) > 0 {
				w.add(at+"."+period+"."+f, nil, ids)
			}
		}
	}
}

// moneyFields lists the paths of the set money fields in a totals message,
// one message deep, such as "items.gross" or "counted".
func moneyFields(m protoreflect.Message, prefix string) []string {
	var out []string
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsList() || fd.IsMap() {
			return true
		}
		name := prefix + string(fd.Name())
		switch {
		case fd.Message().FullName() == "nexteggapp.taxprotos.common.v1.Money":
			out = append(out, name)
		case prefix == "":
			out = append(out, moneyFields(v.Message(), name+".")...)
		}
		return true
	})
	return out
}

// carrying maps each so far or rest item total, such as "so_far.items.gross",
// to the ids of the items that add to it: the made items for so far and
// the expected for the rest, each with the figure set. A total that is
// itself money, as an itemised model's "items", adds every item, and tax
// paid's "counted" every payment that counts. Box figures are worked out
// from the item totals, so no item carries them.
func carrying(items protoreflect.List) map[string][]string {
	out := map[string][]string{}
	for i := 0; i < items.Len(); i++ {
		it := items.Get(i).Message().Interface().(*scenariosv1.Item)
		period := "rest."
		if it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
			period = "so_far."
		}
		out[period+"items"] = append(out[period+"items"], it.GetId())
		if it.GetPayment().GetCounts() {
			out[period+"counted"] = append(out[period+"counted"], it.GetId())
		}
		m := it.ProtoReflect()
		which := m.WhichOneof(m.Descriptor().Oneofs().ByName("figures"))
		if which == nil {
			continue
		}
		m.Get(which).Message().Range(func(fd protoreflect.FieldDescriptor, _ protoreflect.Value) bool {
			key := period + "items." + string(fd.Name())
			out[key] = append(out[key], it.GetId())
			return true
		})
	}
	return out
}
