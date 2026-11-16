package golden

// Each answer's worksheet holds the figures its core came from, so the two
// can't disagree. docs/estimated-tax-design.md, "Verification", item 3.

import (
	"fmt"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// worksheetLines are the lines every jurisdiction's underpayment form has,
// whatever it numbers them: the required annual payment, each due date's
// required instalment and underpayment, and the penalty.
type worksheetLines struct {
	target, penalty *commonv1.Money
	columns         []worksheetColumn
}

// worksheetColumn is one due date's column.
type worksheetColumn struct {
	required, underpayment *commonv1.Money
}

// form2210Lines are Form 2210's, federal or a state's that follows it: lines
// 9 and 19, and lines 10 and 17 of each column.
func form2210Lines(form *form1040.Form2210) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetEstimatedTaxPenalty()}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	return w
}

// worksheetOf is the answer's worksheet as the lines every form has, or nil
// for none. Each jurisdiction's own form is one case.
func worksheetOf(a *returnsv1.EstimatedAnswer) *worksheetLines {
	switch w := a.GetWorksheet().(type) {
	case *returnsv1.EstimatedAnswer_Us:
		return form2210Lines(w.Us)
	case *returnsv1.EstimatedAnswer_Pattern:
		return form2210Lines(w.Pattern.GetForm_2210())
	case *returnsv1.EstimatedAnswer_Ca:
		return ftb5805Lines(w.Ca)
	}
	return nil
}

// worksheetProblems says where an answer's worksheet disagrees with the
// answer's core: the required annual payment with the target, each column's
// required instalment with the instalment's own amount, its underpayment
// with the instalment's shortfall, and the penalty with the penalty's total.
// An answer with no target has no worksheet. A required instalment of
// nothing in the last column is the January instalment a return filed early
// excuses (26 U.S.C. § 6654(h)).
func worksheetProblems(in *returnsv1.ReturnInputs, a *returnsv1.EstimatedAnswer) []string {
	form := worksheetOf(a)
	computed := a.GetOutcome() == returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED &&
		a.GetRequirement().GetStatus() != returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN
	switch {
	case !computed && a.GetWorksheet() != nil:
		return []string{"a worksheet without a target"}
	case !computed:
		return nil
	case form == nil:
		return []string{"no worksheet"}
	}
	var out []string
	differ := func(what string, got, want *commonv1.Money) {
		if got.GetMinor() != want.GetMinor() {
			out = append(out, fmt.Sprintf("%s is %d, want %d", what, got.GetMinor(), want.GetMinor()))
		}
	}
	differ("required annual payment", form.target, a.GetRequirement().GetTarget())
	differ("penalty", form.penalty, a.GetPenalty().GetTotal())
	cols, insts := form.columns, a.GetInstallments()
	if len(cols) != len(insts) {
		return append(out, fmt.Sprintf("%d columns for %d instalments", len(cols), len(insts)))
	}
	var before int64
	for i, inst := range insts {
		own := inst.GetNeededBy().GetMinor() - before
		before = inst.GetNeededBy().GetMinor()
		required := cols[i].required.GetMinor()
		excused := i == len(insts)-1 && required == 0 && in.GetEstimatedTax().GetReturnFiled() != nil
		if required != own && !excused {
			out = append(out, fmt.Sprintf("column %d required instalment is %d, want %d", i+1, required, own))
		}
		differ(fmt.Sprintf("column %d underpayment", i+1), cols[i].underpayment, inst.GetShortfall())
	}
	return out
}
