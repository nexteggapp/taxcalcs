package golden

import (
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form502up "github.com/nexteggapp/taxprotos/go/us/state/md/v1/form_502up"
)

// form502upLines are Maryland's Form 502UP: the required payment on line 10,
// the interest on line 18, and of each column line 13, a running total, as
// the instalment's own amount, and line 15, the running underpayment.
func form502upLines(form *form502up.Form502Up) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetTotalInterest(), cumulative: true}
	var before int64
	for _, c := range form.GetPaymentPeriods() {
		own := c.GetRequiredToDate().GetMinor() - before
		before = c.GetRequiredToDate().GetMinor()
		w.columns = append(w.columns, worksheetColumn{&commonv1.Money{Minor: own}, c.GetUnderpayment()})
	}
	return w
}
