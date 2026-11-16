package golden

import (
	"fmt"

	formn210 "github.com/nexteggapp/taxprotos/go/us/state/hi/v1/form_n_210"
)

// n210Lines reads Hawaii's Form N-210: the required annual payment on line
// 8, each column's lines 9 and 16, and the penalty on line 21, which must
// also be line 20 of every column added.
func n210Lines(f *formn210.FormN210) *worksheetLines {
	if f == nil {
		return nil
	}
	t := &worksheetLines{target: f.GetRequiredAnnualPayment(), penalty: f.GetPenalty()}
	var columns int64
	for _, p := range f.GetPaymentPeriods() {
		t.columns = append(t.columns, worksheetColumn{required: p.GetRequiredInstallment(), underpayment: p.GetUnderpayment()})
		columns += p.GetPenalty().GetMinor()
	}
	if columns != f.GetPenalty().GetMinor() {
		t.problems = append(t.problems, fmt.Sprintf("N-210 line 21 is %d, and line 20 added is %d", f.GetPenalty().GetMinor(), columns))
	}
	return t
}
