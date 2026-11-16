package golden

import (
	formit21059 "github.com/nexteggapp/taxprotos/go/us/state/ny/v1/form_it_2105_9"
)

// it21059Lines are New York's Form IT-2105.9: the required annual payment on
// Part 1 line 17, the penalty on line 39, and of each column line 25, the
// instalment's own amount, and line 30, the running underpayment.
func it21059Lines(form *formit21059.FormIt21059) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetTotalPenalty(), cumulative: true}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetPenaltyUnderpayment()})
	}
	return w
}
