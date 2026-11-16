package golden

import (
	"fmt"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formil2210 "github.com/nexteggapp/taxprotos/go/us/state/il/v1/form_il_2210"
)

// il2210Lines reads Illinois's Form IL-2210: the required annual payment on
// line 7, each quarter's line 9b, and the penalty on line 22, which must
// also be column I of Penalty Worksheet 1 added. A quarter's underpayment is
// its unpaid amount, line 13 when positive, less what Penalty Worksheet 1
// applies to it from payments made by its due date. Line 22 is the form's
// flat penalty alone, so it is the penalty's total only when no interest is
// asked for on top of it.
func il2210Lines(in *returnsv1.ReturnInputs, f *formil2210.FormIl2210) *worksheetLines {
	if f == nil {
		return nil
	}
	t := &worksheetLines{target: f.GetRequiredAnnualPayment(), penalty: f.GetUnderpaymentPenalty()}
	for _, s := range in.GetEstimatedTax().GetStateInputs() {
		if s.GetIl().GetLateInstallmentInterest() == returnsv1.IllinoisEstimatedInputs_LATE_INSTALLMENT_INTEREST_CHARGED {
			t.penaltyApart = true
		}
	}
	paid := map[int32]int64{}
	var columnI int64
	for _, r := range f.GetUnderpaymentRows() {
		columnI += r.GetPenalty().GetMinor()
		if dateKey(r.GetPaymentDate()) <= dateKey(r.GetDueDate()) {
			paid[r.GetPeriod()] += r.GetPaymentApplied().GetMinor()
		}
	}
	if columnI != f.GetUnderpaymentPenalty().GetMinor() {
		t.problems = append(t.problems, fmt.Sprintf("IL-2210 line 22 is %d, and column I added is %d", f.GetUnderpaymentPenalty().GetMinor(), columnI))
	}
	for _, q := range f.GetQuarters() {
		unpaid := max(q.GetUnpaidInstallment().GetMinor(), 0)
		t.columns = append(t.columns, worksheetColumn{
			required:     q.GetRequiredInstallment(),
			underpayment: &commonv1.Money{Minor: unpaid - paid[q.GetQuarter()]},
		})
	}
	return t
}

// dateKey orders a date as a number, yyyymmdd.
func dateKey(d *commonv1.Date) int64 {
	return int64(d.GetYear())*10000 + int64(d.GetMonth())*100 + int64(d.GetDay())
}
