package main

import (
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The PolicyEngine-US version pinned in policyengine/requirements_lock.txt.
const policyEngineVersion = "2.154.1"

// pair is one figure compared: ours by name, theirs by PolicyEngine-US
// variable, federal or state.
type pair struct {
	ours, theirs string
	state        bool
}

// mapping pairs our figures with PolicyEngine-US variables. Federal tax is
// Form 1040 line 16, before credits; state tax is the return's total tax
// after nonrefundable credits.
var mapping = []pair{
	{"agi", "adjusted_gross_income", false},
	{"taxable_income", "taxable_income", false},
	{"tax_line_16", "income_tax_before_credits", false},
	{"agi", "state_agi", true},
	{"taxable_income", "state_taxable_income", true},
	{"total_tax", "state_income_tax_before_refundable_credits", true},
}

func dollars(m interface{ GetMinor() int64 }) float64 { return float64(m.GetMinor()) / 100 }

// ourFigures reads the compared figures from a prepared return, the state
// ones keyed as the mapping names them.
func ourFigures(out *returnsv1.ReturnOutputs) map[string]float64 {
	fed := out.GetFederal()
	f := map[string]float64{
		"agi":            dollars(fed.GetSummary().GetAdjustedGrossIncome()),
		"taxable_income": dollars(fed.GetSummary().GetTaxableIncome()),
		"tax_line_16":    dollars(fed.GetForms().GetForm_1040().GetTax()),
	}
	if len(out.GetStates()) == 1 {
		m := out.GetStates()[0].ProtoReflect()
		if fd := m.WhichOneof(m.Descriptor().Oneofs().ByName("return")); fd != nil {
			ret := m.Get(fd).Message()
			if sd := ret.Descriptor().Fields().ByName(protoreflect.Name("summary")); sd != nil {
				s := ret.Get(sd).Message().Interface().(*returnsv1.Summary)
				f["state/agi"] = dollars(s.GetAdjustedGrossIncome())
				f["state/taxable_income"] = dollars(s.GetTaxableIncome())
				f["state/total_tax"] = dollars(s.GetTotalTax())
			}
		}
	}
	return f
}
