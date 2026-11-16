package golden

import (
	"fmt"

	form760c "github.com/nexteggapp/taxprotos/go/us/state/va/v1/form_760c"
)

// form760cLines reads Virginia's Form 760C: the addition to tax on line 25,
// which must also be line 24 added, and of each column line 7, the
// instalment's own amount, and line 14, its underpayment. Line 4 is what
// the instalments are measured against; the core's target can be Exception
// 2's lower figure, which is on line 17, so line 4 is not compared with it,
// and each column's line 7 holds the form to the core.
func form760cLines(form *form760c.Form760C) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{penalty: form.GetAdditionToTax()}
	var added int64
	for _, c := range form.GetInstallmentPeriods() {
		added += c.GetAddition().GetMinor()
		w.columns = append(w.columns, worksheetColumn{c.GetTaxLiability(), c.GetUnderpayment()})
	}
	if added != form.GetAdditionToTax().GetMinor() {
		w.problems = append(w.problems, fmt.Sprintf("760C line 25 is %d, and line 24 added is %d", form.GetAdditionToTax().GetMinor(), added))
	}
	return w
}
