package golden

import (
	formar2210 "github.com/nexteggapp/taxprotos/go/us/state/ar/v1/form_ar2210"
	formar2210a "github.com/nexteggapp/taxprotos/go/us/state/ar/v1/form_ar2210a"
)

// ar2210Lines are Arkansas's Form AR2210: lines 6 and 17, and lines 7 and 13
// of each column. Line 13 is the running underpayment.
func ar2210Lines(form *formar2210.FormAr2210) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetEstimatedTaxPenalty(), cumulative: true}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	return w
}

// ar2210aLines are Arkansas's Form AR2210A, for the annualized method: line
// 46, and lines 31 and 38 of each column. It has no line for the required
// annual payment, which is Form AR2210's, so the target is taken as the
// answer's: each column's line 31 is what holds it to the core.
func ar2210aLines(form *formar2210a.FormAr2210A) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{penalty: form.GetEstimatedTaxPenalty()}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	return w
}
