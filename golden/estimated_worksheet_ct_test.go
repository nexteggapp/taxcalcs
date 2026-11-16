package golden

import (
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formct2210 "github.com/nexteggapp/taxprotos/go/us/state/ct/v1/form_ct_2210"
)

// ct2210Lines are Connecticut's Form CT-2210: the required annual payment on
// Part 2 line 6, the total interest on line 17, and of each column line 10, a
// running total, as the instalment's own amount, and line 16, the running
// underpayment.
func ct2210Lines(form *formct2210.FormCt2210) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetTotalInterest(), cumulative: true}
	var before int64
	for _, c := range form.GetPaymentPeriods() {
		own := c.GetRequiredInstallments().GetMinor() - before
		before = c.GetRequiredInstallments().GetMinor()
		w.columns = append(w.columns, worksheetColumn{&commonv1.Money{Minor: own}, c.GetUnderpayment()})
	}
	return w
}
