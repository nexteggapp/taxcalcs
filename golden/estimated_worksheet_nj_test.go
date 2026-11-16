package golden

import (
	formnj2210 "github.com/nexteggapp/taxprotos/go/us/state/nj/v1/form_nj_2210"
)

// nj2210Lines are New Jersey's Form NJ-2210: the total interest on line 19,
// and of each column line 5, the quarter charged on its due date, and line
// 12, the quarter's own underpayment. The form has no line for the required
// annual amount: line 5 holds a quarter of it.
func nj2210Lines(form *formnj2210.FormNj2210) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{penalty: form.GetTotalInterest()}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	return w
}
