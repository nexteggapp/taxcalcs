package golden

// Each answer's worksheet holds the figures its core came from, so the two
// can't disagree. docs/estimated-tax-design.md, "Verification", item 3.

import (
	"fmt"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	formpitund "github.com/nexteggapp/taxprotos/go/us/state/de/v1/form_pit_und"
)

// worksheetColumn is one due date's column of a worksheet: what the form
// requires on that date, and the underpayment it finds.
type worksheetColumn struct {
	required, underpayment *commonv1.Money
}

// worksheetTotals are the figures every jurisdiction's worksheet shares with
// the answer's core, wherever its form prints them.
type worksheetTotals struct {
	// form names the worksheet, for a message.
	form string
	// required is the required annual payment, and penalty the penalty.
	required, penalty *commonv1.Money
	columns           []worksheetColumn
	// problems are where the form disagrees with itself.
	problems []string
}

// worksheetProblems says where an answer's worksheet disagrees with the
// answer's core: the required annual payment with the target, each
// column's required instalment with the instalment's own amount, its
// underpayment with the instalment's shortfall, and the penalty with the
// penalty's total. An answer with no target has no worksheet. A required
// instalment of nothing in the last column is the January instalment a
// return filed early excuses (26 U.S.C. § 6654(h)).
func worksheetProblems(in *returnsv1.ReturnInputs, a *returnsv1.EstimatedAnswer) []string {
	var totals *worksheetTotals
	switch w := a.GetWorksheet().(type) {
	case *returnsv1.EstimatedAnswer_Us:
		totals = form2210Totals(w.Us)
	case *returnsv1.EstimatedAnswer_Pattern:
		totals = form2210Totals(w.Pattern.GetForm_2210())
	case *returnsv1.EstimatedAnswer_De:
		totals = pitUndTotals(w.De)
	}
	computed := a.GetOutcome() == returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED &&
		a.GetRequirement().GetStatus() != returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN
	switch {
	case !computed && a.GetWorksheet() != nil:
		return []string{"a worksheet without a target"}
	case !computed:
		return nil
	case totals == nil:
		return []string{"no worksheet"}
	}
	out := append([]string(nil), totals.problems...)
	differ := func(what string, got, want *commonv1.Money) {
		if got.GetMinor() != want.GetMinor() {
			out = append(out, fmt.Sprintf("%s %s is %d, want %d", totals.form, what, got.GetMinor(), want.GetMinor()))
		}
	}
	differ("required annual payment", totals.required, a.GetRequirement().GetTarget())
	differ("penalty", totals.penalty, a.GetPenalty().GetTotal())
	cols, insts := totals.columns, a.GetInstallments()
	if len(cols) != len(insts) {
		return append(out, fmt.Sprintf("%s has %d columns for %d instalments", totals.form, len(cols), len(insts)))
	}
	var before int64
	for i, inst := range insts {
		own := inst.GetNeededBy().GetMinor() - before
		before = inst.GetNeededBy().GetMinor()
		required := cols[i].required.GetMinor()
		excused := i == len(insts)-1 && required == 0 && in.GetEstimatedTax().GetReturnFiled() != nil
		if required != own && !excused {
			out = append(out, fmt.Sprintf("%s column %d required instalment is %d, want %d", totals.form, i+1, required, own))
		}
		differ(fmt.Sprintf("column %d underpayment", i+1), cols[i].underpayment, inst.GetShortfall())
	}
	return out
}

// form2210Totals reads Form 2210, federal or a state's that follows it: the
// required annual payment on line 9, each column's lines 10 and 17, and the
// penalty on line 19.
func form2210Totals(f *form1040.Form2210) *worksheetTotals {
	if f == nil {
		return nil
	}
	t := &worksheetTotals{form: "Form 2210", required: f.GetRequiredAnnualPayment(), penalty: f.GetEstimatedTaxPenalty()}
	for _, p := range f.GetPaymentPeriods() {
		t.columns = append(t.columns, worksheetColumn{required: p.GetRequiredInstallment(), underpayment: p.GetUnderpayment()})
	}
	return t
}

// pitUndTotals reads Delaware's Form PIT-UND: the required annual amount on
// line C, each column's lines 28 and 37, and the total penalty on line 43,
// which must also be line 42 of every column added.
func pitUndTotals(f *formpitund.FormPitUnd) *worksheetTotals {
	if f == nil {
		return nil
	}
	t := &worksheetTotals{form: "PIT-UND", required: f.GetRequiredAnnualPayment(), penalty: f.GetTotalPenalty()}
	var columns int64
	for _, p := range f.GetPaymentPeriods() {
		t.columns = append(t.columns, worksheetColumn{required: p.GetRequiredInstallment(), underpayment: p.GetUnderpayment()})
		columns += p.GetPenalty().GetMinor()
	}
	if columns != f.GetTotalPenalty().GetMinor() {
		t.problems = append(t.problems, fmt.Sprintf("PIT-UND line 43 is %d, and line 42 added is %d", f.GetTotalPenalty().GetMinor(), columns))
	}
	return t
}
