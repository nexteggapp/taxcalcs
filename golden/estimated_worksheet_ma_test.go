package golden

import (
	"fmt"

	formm2210 "github.com/nexteggapp/taxprotos/go/us/state/ma/v1/form_m_2210"
)

// m2210Lines are Massachusetts's Form M-2210: the required annual payment on
// line 6, the penalty on line 24, and of each column line 8 and line 13, the
// installment's own underpayment. Line 24 must be lines 20 to 23 of every
// column added.
func m2210Lines(form *formm2210.FormM2210) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetPenalty()}
	var added int64
	for _, c := range form.GetInstallments() {
		added += c.GetPenaltySecondQuarter().GetMinor() + c.GetPenaltyThirdQuarter().GetMinor() +
			c.GetPenaltyFourthQuarter().GetMinor() + c.GetPenaltyNextYear().GetMinor()
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	if added != form.GetPenalty().GetMinor() {
		w.problems = append(w.problems, fmt.Sprintf("M-2210 line 24 is %d, and lines 20 to 23 added are %d", form.GetPenalty().GetMinor(), added))
	}
	return w
}
