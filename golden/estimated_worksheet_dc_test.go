package golden

import (
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formd2210 "github.com/nexteggapp/taxprotos/go/us/state/dc/v1/form_d_2210"
)

// d2210Lines are the District of Columbia's Form D-2210: lines 5 and 12, and
// of each column line 7, a running total, as the instalment's own amount,
// and line 9, the running underpayment.
func d2210Lines(form *formd2210.FormD2210) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetEstimatedTaxPenalty(), cumulative: true}
	var before int64
	for _, c := range form.GetPaymentPeriods() {
		own := c.GetRequiredToDate().GetMinor() - before
		before = c.GetRequiredToDate().GetMinor()
		w.columns = append(w.columns, worksheetColumn{&commonv1.Money{Minor: own}, c.GetUnderpayment()})
	}
	return w
}
