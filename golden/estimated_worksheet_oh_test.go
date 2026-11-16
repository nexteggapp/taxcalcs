package golden

import (
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formitsd2210 "github.com/nexteggapp/taxprotos/go/us/state/oh/v1/form_it_sd_2210"
)

// itsd2210Lines are Ohio's IT/SD 2210: the required annual payment on page
// 2 line 9, the penalty on line 17, and of each column line 10, a running
// total, as the instalment's own amount, and line 14, the running
// underpayment.
func itsd2210Lines(form *formitsd2210.FormItSd2210) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetTotalPenalty(), cumulative: true}
	var before int64
	for _, c := range form.GetPaymentPeriods() {
		own := c.GetRequiredToDate().GetMinor() - before
		before = c.GetRequiredToDate().GetMinor()
		w.columns = append(w.columns, worksheetColumn{&commonv1.Money{Minor: own}, c.GetUnderpayment()})
	}
	return w
}
