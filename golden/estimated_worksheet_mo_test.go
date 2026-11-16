package golden

import (
	formmo2210 "github.com/nexteggapp/taxprotos/go/us/state/mo/v1/form_mo_2210"
)

// mo2210Lines are Missouri's Form MO-2210: the required annual payment on
// Part I line 6, the total penalty on line 29, and of each column line 15,
// the instalment's own amount, and line 19d, the running underpayment.
func mo2210Lines(form *formmo2210.FormMo2210) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetTotalPenalty(), cumulative: true}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetTotalUnderpayment()})
	}
	return w
}
