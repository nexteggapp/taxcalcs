package golden

import (
	form221 "github.com/nexteggapp/taxprotos/go/us/state/az/v1/form_221"
)

// form221Lines are Arizona's Form 221: the required annual payment on line 9,
// each column's required instalment on line 11 and underpayment on line 15a,
// and the total penalty in box 30a.
func form221Lines(form *form221.Form221) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetTotalPenalty()}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	return w
}
