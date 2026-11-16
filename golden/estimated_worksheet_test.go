package golden

// Each answer's worksheet holds the figures its core came from, so the two
// can't disagree. docs/estimated-tax-design.md, "Verification", item 3.

import (
	"fmt"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// worksheetProblems says where an answer's Form 2210 worksheet, federal or a
// state's that follows it, disagrees with the answer's core: the required
// annual payment (line 9) with the target, each column's required
// instalment (line 10) with the instalment's own amount, its underpayment
// (line 17) with the instalment's shortfall, and the penalty (line 19) with
// the penalty's total. An answer with no target has no worksheet. A required
// instalment of nothing in the last column is the January instalment a
// return filed early excuses (26 U.S.C. § 6654(h)).
func worksheetProblems(in *returnsv1.ReturnInputs, a *returnsv1.EstimatedAnswer) []string {
	var form *form1040.Form2210
	switch w := a.GetWorksheet().(type) {
	case *returnsv1.EstimatedAnswer_Us:
		form = w.Us
	case *returnsv1.EstimatedAnswer_Pattern:
		form = w.Pattern.GetForm_2210()
	}
	computed := a.GetOutcome() == returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED &&
		a.GetRequirement().GetStatus() != returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN
	switch {
	case !computed && a.GetWorksheet() != nil:
		return []string{"a worksheet without a target"}
	case !computed:
		return nil
	case form == nil:
		return []string{"no Form 2210 worksheet"}
	}
	var out []string
	differ := func(what string, got, want *commonv1.Money) {
		if got.GetMinor() != want.GetMinor() {
			out = append(out, fmt.Sprintf("%s is %d, want %d", what, got.GetMinor(), want.GetMinor()))
		}
	}
	differ("line 9", form.GetRequiredAnnualPayment(), a.GetRequirement().GetTarget())
	differ("line 19", form.GetEstimatedTaxPenalty(), a.GetPenalty().GetTotal())
	cols, insts := form.GetPaymentPeriods(), a.GetInstallments()
	if len(cols) != len(insts) {
		return append(out, fmt.Sprintf("%d columns for %d instalments", len(cols), len(insts)))
	}
	var before int64
	for i, inst := range insts {
		own := inst.GetNeededBy().GetMinor() - before
		before = inst.GetNeededBy().GetMinor()
		line10 := cols[i].GetRequiredInstallment().GetMinor()
		excused := i == len(insts)-1 && line10 == 0 && in.GetEstimatedTax().GetReturnFiled() != nil
		if line10 != own && !excused {
			out = append(out, fmt.Sprintf("column %d line 10 is %d, want %d", i+1, line10, own))
		}
		differ(fmt.Sprintf("column %d line 17", i+1), cols[i].GetUnderpayment(), inst.GetShortfall())
	}
	return out
}
