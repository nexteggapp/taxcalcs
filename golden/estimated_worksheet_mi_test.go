package golden

import (
	formmi2210 "github.com/nexteggapp/taxprotos/go/us/state/mi/v1/form_mi_2210"
)

// mi2210Lines are Michigan's Form MI-2210: the required annual payment on
// line 7, the interest and penalty together on line 27, and of each column
// the installment on line 9 and the interest underpayment on line 16. The
// flat penalty's own underpayment, line 23, counts a payment only in its
// quarter and is not the answer's shortfall.
func mi2210Lines(form *formmi2210.FormMi2210) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetTotalPenaltyAndInterest()}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	return w
}
