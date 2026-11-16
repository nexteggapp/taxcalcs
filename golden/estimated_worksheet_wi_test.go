package golden

import (
	"fmt"

	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	scheduleu "github.com/nexteggapp/taxprotos/go/us/state/wi/v1/schedule_u"
)

// scheduleULines are Wisconsin's Schedule U: the required annual payment on
// line 8, the interest on line 29, and of each Part III column line 16 and
// line 20 less line 23, what was unpaid on the due date after any
// overpayment carried forward. Line 24 must be line 20 less lines 22 and
// 23, line 22 its carrybacks added, and line 29 every line 27 and 28 added.
func scheduleULines(form *scheduleu.ScheduleU) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetUnderpaymentInterest()}
	var interest int64
	for _, c := range form.GetPaymentPeriods() {
		short := c.GetUnderpayment().GetMinor() - c.GetCarryforward().GetMinor()
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), &commonv1.Money{Minor: short}})
		var back int64
		for _, b := range c.GetCarrybacks() {
			back += b.GetAmount().GetMinor()
			interest += b.GetInterest().GetMinor()
		}
		interest += c.GetNetUnderpaymentInterest().GetMinor()
		if back != c.GetCarryback().GetMinor() {
			w.problems = append(w.problems, fmt.Sprintf("column %d line 22 is %d, and its carrybacks added are %d", c.GetPeriod(), c.GetCarryback().GetMinor(), back))
		}
		if net := short - back; net != c.GetNetUnderpayment().GetMinor() {
			w.problems = append(w.problems, fmt.Sprintf("column %d line 24 is %d, want %d", c.GetPeriod(), c.GetNetUnderpayment().GetMinor(), net))
		}
	}
	if interest != form.GetUnderpaymentInterest().GetMinor() {
		w.problems = append(w.problems, fmt.Sprintf("line 29 is %d, and lines 27 and 28 added are %d", form.GetUnderpaymentInterest().GetMinor(), interest))
	}
	return w
}
