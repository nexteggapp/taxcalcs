package golden

// Each answer's worksheet holds the figures its core came from, so the two
// can't disagree. docs/estimated-tax-design.md, "Verification", item 3.

import (
	"fmt"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	form221 "github.com/nexteggapp/taxprotos/go/us/state/az/v1/form_221"
	formct2210 "github.com/nexteggapp/taxprotos/go/us/state/ct/v1/form_ct_2210"
	form500uet "github.com/nexteggapp/taxprotos/go/us/state/ga/v1/form_500_uet"
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
	case *returnsv1.EstimatedAnswer_Ct:
		return formCt2210Problems(in, a, w.Ct)
	case *returnsv1.EstimatedAnswer_Ga:
		return form500UetProblems(a, w.Ga)
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

// formCt2210Problems says where an answer's Connecticut Form CT-2210
// disagrees with the answer's core. Part 3 works in running totals, so each
// column's required installments (line 10) are checked against the
// installment's needed-by figure, its underpayment (line 16) against what is
// needed by the due date less what was credited by then, and the total
// interest (line 17) against the penalty's total; the required annual
// payment (Part 2 line 6) is checked against the target. A last column whose
// line 10 holds no more than the one before is the January installment a
// return filed and paid early excuses.
func formCt2210Problems(in *returnsv1.ReturnInputs, a *returnsv1.EstimatedAnswer, form *formct2210.FormCt2210) []string {
	if a.GetRequirement().GetStatus() == returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN {
		return []string{"a worksheet without a target"}
	}
	var out []string
	differ := func(what string, got, want int64) {
		if got != want {
			out = append(out, fmt.Sprintf("%s is %d, want %d", what, got, want))
		}
	}
	differ("line 6", form.GetRequiredAnnualPayment().GetMinor(), a.GetRequirement().GetTarget().GetMinor())
	differ("line 17 total", form.GetTotalInterest().GetMinor(), a.GetPenalty().GetTotal().GetMinor())
	cols, insts := form.GetPaymentPeriods(), a.GetInstallments()
	if len(cols) != len(insts) {
		return append(out, fmt.Sprintf("%d columns for %d instalments", len(cols), len(insts)))
	}
	for i, inst := range insts {
		excused := i == len(insts)-1 && i > 0 && in.GetEstimatedTax().GetReturnFiled() != nil &&
			cols[i].GetRequiredInstallments().GetMinor() == cols[i-1].GetRequiredInstallments().GetMinor()
		if excused {
			continue
		}
		differ(fmt.Sprintf("column %d line 10", i+1), cols[i].GetRequiredInstallments().GetMinor(), inst.GetNeededBy().GetMinor())
		differ(fmt.Sprintf("column %d line 16", i+1), cols[i].GetUnderpayment().GetMinor(),
			max(0, inst.GetNeededBy().GetMinor()-inst.GetCreditedBy().GetMinor()))
	}
	return out
}

// form500UetProblems says where an answer's Georgia Form 500 UET disagrees
// with the answer's core: the columns' required installments (line 8) added
// with the target, each column's line 8 with the installment's own amount,
// its underpayment (line 12) with the installment's shortfall, and the
// penalty (line 21) with the penalty's total.
func form500UetProblems(a *returnsv1.EstimatedAnswer, form *form500uet.Form500Uet) []string {
	if a.GetRequirement().GetStatus() == returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN {
		return []string{"a worksheet without a target"}
	}
	var out []string
	differ := func(what string, got, want int64) {
		if got != want {
			out = append(out, fmt.Sprintf("%s is %d, want %d", what, got, want))
		}
	}
	differ("line 21", form.GetTotalPenalty().GetMinor(), a.GetPenalty().GetTotal().GetMinor())
	cols, insts := form.GetPaymentPeriods(), a.GetInstallments()
	if len(cols) != len(insts) {
		return append(out, fmt.Sprintf("%d columns for %d instalments", len(cols), len(insts)))
	}
	var before, total int64
	for i, inst := range insts {
		own := inst.GetNeededBy().GetMinor() - before
		before = inst.GetNeededBy().GetMinor()
		total += cols[i].GetRequiredInstallment().GetMinor()
		differ(fmt.Sprintf("column %d line 8", i+1), cols[i].GetRequiredInstallment().GetMinor(), own)
		differ(fmt.Sprintf("column %d line 12", i+1), cols[i].GetUnderpayment().GetMinor(), inst.GetShortfall().GetMinor())
	}
	differ("line 8, every column", total, a.GetRequirement().GetTarget().GetMinor())
	return out
}
