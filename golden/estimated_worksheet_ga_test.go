package golden

import (
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form500uet "github.com/nexteggapp/taxprotos/go/us/state/ga/v1/form_500_uet"
)

// uetLines are Georgia's Form 500 UET: each column's required instalment on
// line 8 and underpayment on line 12, and the penalty on line 21. The form
// has no line for the required annual payment, so the columns' line 8 added
// stands for it.
func uetLines(form *form500uet.Form500Uet) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{penalty: form.GetTotalPenalty()}
	var total int64
	for _, c := range form.GetPaymentPeriods() {
		total += c.GetRequiredInstallment().GetMinor()
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	w.target = &commonv1.Money{Minor: total}
	return w
}
