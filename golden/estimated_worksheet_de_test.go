package golden

import (
	"fmt"

	formpitund "github.com/nexteggapp/taxprotos/go/us/state/de/v1/form_pit_und"
)

// pitUndLines reads Delaware's Form PIT-UND: the required annual amount on
// line C, each column's lines 28 and 37, and the total penalty on line 43,
// which must also be line 42 of every column added.
func pitUndLines(f *formpitund.FormPitUnd) *worksheetLines {
	if f == nil {
		return nil
	}
	t := &worksheetLines{target: f.GetRequiredAnnualPayment(), penalty: f.GetTotalPenalty()}
	var columns int64
	for _, p := range f.GetPaymentPeriods() {
		t.columns = append(t.columns, worksheetColumn{required: p.GetRequiredInstallment(), underpayment: p.GetUnderpayment()})
		columns += p.GetPenalty().GetMinor()
	}
	if columns != f.GetTotalPenalty().GetMinor() {
		t.problems = append(t.problems, fmt.Sprintf("PIT-UND line 43 is %d, and line 42 added is %d", f.GetTotalPenalty().GetMinor(), columns))
	}
	return t
}
