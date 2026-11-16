package golden

import (
	"fmt"

	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formit2210 "github.com/nexteggapp/taxprotos/go/us/state/in/v1/form_it_2210"
	formit2210a "github.com/nexteggapp/taxprotos/go/us/state/in/v1/form_it_2210a"
)

// it2210Lines are Indiana's Schedule IT-2210: the required annual payment on
// line 8, the penalty on line 21, and of each Section E column line 14 and
// line 19, the column's own underpayment. Line 20 must be line 19 added.
func it2210Lines(form *formit2210.FormIt2210) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetUnderpaymentPenalty()}
	var added int64
	for _, c := range form.GetInstallmentPeriods() {
		added += c.GetUnderpayment().GetMinor()
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	w.problems = inSumProblem(w.problems, "IT-2210 line 20", form.GetTotalUnderpayment(), added)
	return w
}

// it2210aLines are Indiana's Schedule IT-2210A, for the annualized method:
// the required annual payment on line H, the penalty on line 27, and of each
// Section 3 column line 19 and line 25. Line 26 must be line 25 added.
func it2210aLines(form *formit2210a.FormIt2210A) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetUnderpaymentPenalty()}
	var added int64
	for _, c := range form.GetPeriods() {
		added += c.GetUnderpayment().GetMinor()
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	w.problems = inSumProblem(w.problems, "IT-2210A line 26", form.GetTotalUnderpayment(), added)
	return w
}

// inSumProblem adds a problem when a total line is not its column lines added.
func inSumProblem(problems []string, line string, total *commonv1.Money, added int64) []string {
	if total.GetMinor() != added {
		problems = append(problems, fmt.Sprintf("%s is %d, and its columns added are %d", line, total.GetMinor(), added))
	}
	return problems
}
