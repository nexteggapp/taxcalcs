package golden

import (
	"fmt"

	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formr210r "github.com/nexteggapp/taxprotos/go/us/state/la/v1/form_r_210r"
)

// r210rLines are Louisiana's Form R-210R: the penalty on line 19, and of each
// column line 5 and line 9. Line 9 carries the signed balance from column to
// column, so less than zero it is the running underpayment: what was required
// by the due date less what was credited by it. Line 3 is the lesser of lines
// 1 and 2 only; the core's target can be Exception 3's lower figure, which no
// line holds, so each column's line 5 is what holds the form to the core.
// Line 19 must be line 18 added.
func r210rLines(form *formr210r.FormR210R) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{penalty: form.GetUnderpaymentPenalty(), cumulative: true}
	var added int64
	for _, c := range form.GetPeriods() {
		added += c.GetPenalty().GetMinor()
		short := max(0, -c.GetOverpaymentOrUnderpayment().GetMinor())
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredPayment(), &commonv1.Money{Minor: short}})
	}
	if added != form.GetUnderpaymentPenalty().GetMinor() {
		w.problems = append(w.problems, fmt.Sprintf("R-210R line 19 is %d, and line 18 added is %d", form.GetUnderpaymentPenalty().GetMinor(), added))
	}
	return w
}
