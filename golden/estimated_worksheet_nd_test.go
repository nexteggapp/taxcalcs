package golden

import (
	formnd1ut "github.com/nexteggapp/taxprotos/go/us/state/nd/v1/form_nd_1ut"
)

// nd1utLines are North Dakota's Schedule ND-1UT: the required payment on
// line 6, the total interest on line 17, and of each quarter line 7, the
// instalment's own amount, and line 15, its own underpayment after its own
// payments and any overpayment carried in.
func nd1utLines(form *formnd1ut.FormNd1Ut) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredPayment(), penalty: form.GetTotalInterest()}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	return w
}
