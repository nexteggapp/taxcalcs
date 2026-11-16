package golden

import (
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form80320 "github.com/nexteggapp/taxprotos/go/us/state/ms/v1/form_80_320"
)

// ms80320Lines are Mississippi's Form 80-320: the required amount on line 4,
// the total interest on line 11, and of each column line 5, the instalment's
// own amount, and line 8, the running underestimate, an overpayment negative
// and so nothing underpaid.
func ms80320Lines(form *form80320.Form80320) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetTotalInterest(), cumulative: true}
	for _, c := range form.GetPaymentPeriods() {
		under := &commonv1.Money{Minor: max(0, c.GetUnderestimate().GetMinor())}
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), under})
	}
	return w
}
