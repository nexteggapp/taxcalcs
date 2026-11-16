package golden

import (
	form2210me "github.com/nexteggapp/taxprotos/go/us/state/me/v1/form_2210me"
)

// form2210meLines are Maine's Form 2210ME: the required annual payment on
// line 6, the penalty on line 16, and of each column the installment on line
// 9 and the underpayment on line 11a. Line 11a is the installment's own
// shortfall after later payments pay the oldest shortfall first, as the Go
// rules apply them; the form's own line 11 subtracts line 10d from line 9
// without that.
func form2210meLines(form *form2210me.Form2210Me) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetTotalPenalty()}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetEstimatedTaxDue(), c.GetUnderpayment()})
	}
	return w
}
