package golden

// Each answer's worksheet holds the figures its core came from, so the two
// can't disagree. docs/estimated-tax-design.md, "Verification", item 3.

import (
	"fmt"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	form221 "github.com/nexteggapp/taxprotos/go/us/state/az/v1/form_221"
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
	case *returnsv1.EstimatedAnswer_Az:
		return form221Problems(in, a, w.Az)
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

// form221Problems says where an answer's Arizona Form 221 disagrees with the
// answer's core: the required annual payment (line 9) with the target, each
// column's required instalment (line 11) with the instalment's own amount,
// its underpayment (line 15a) with the instalment's shortfall, and the total
// penalty (box 30a) with the penalty's total. A required instalment of
// nothing in the last column is the January instalment a return filed and
// paid early excuses.
func form221Problems(in *returnsv1.ReturnInputs, a *returnsv1.EstimatedAnswer, form *form221.Form221) []string {
	if a.GetRequirement().GetStatus() == returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN {
		return []string{"a worksheet without a target"}
	}
	var out []string
	differ := func(what string, got, want *commonv1.Money) {
		if got.GetMinor() != want.GetMinor() {
			out = append(out, fmt.Sprintf("%s is %d, want %d", what, got.GetMinor(), want.GetMinor()))
		}
	}
	differ("line 9", form.GetRequiredAnnualPayment(), a.GetRequirement().GetTarget())
	differ("box 30a", form.GetTotalPenalty(), a.GetPenalty().GetTotal())
	cols, insts := form.GetPaymentPeriods(), a.GetInstallments()
	if len(cols) != len(insts) {
		return append(out, fmt.Sprintf("%d columns for %d instalments", len(cols), len(insts)))
	}
	var before int64
	for i, inst := range insts {
		own := inst.GetNeededBy().GetMinor() - before
		before = inst.GetNeededBy().GetMinor()
		line11 := cols[i].GetRequiredInstallment().GetMinor()
		excused := i == len(insts)-1 && line11 == 0 && in.GetEstimatedTax().GetReturnFiled() != nil
		if line11 != own && !excused {
			out = append(out, fmt.Sprintf("column %d line 11 is %d, want %d", i+1, line11, own))
		}
		differ(fmt.Sprintf("column %d line 15a", i+1), cols[i].GetUnderpayment(), inst.GetShortfall())
	}
	return out
}
