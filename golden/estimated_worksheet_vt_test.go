package golden

import (
	"fmt"

	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formin152 "github.com/nexteggapp/taxprotos/go/us/state/vt/v1/form_in_152"
	formin152a "github.com/nexteggapp/taxprotos/go/us/state/vt/v1/form_in_152a"
)

// in152Lines are Vermont's Worksheet IN-152: the required annual payment on
// line 3, the interest and penalty on line 15, and of each column line 4 and
// line 6, the quarter's own underpayment. Line 15 must be lines 11 and 14
// added, and those their columns' lines 10 and 13.
func in152Lines(form *formin152.FormIn152) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetTotalInterestAndPenalty()}
	var interest, penalty int64
	for _, c := range form.GetPaymentPeriods() {
		interest += c.GetInterest().GetMinor()
		penalty += c.GetPenalty().GetMinor()
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	w.problems = vtSumProblem(w.problems, "IN-152 line 11", form.GetTotalInterest(), interest)
	w.problems = vtSumProblem(w.problems, "IN-152 line 14", form.GetTotalPenalty(), penalty)
	w.problems = vtSumProblem(w.problems, "IN-152 line 15", form.GetTotalInterestAndPenalty(),
		form.GetTotalInterest().GetMinor()+form.GetTotalPenalty().GetMinor())
	return w
}

// in152aLines are Vermont's Worksheet IN-152A, for a filer who annualised
// federally: the required annual payment on line 3, the interest and penalty
// on line 17, and of each column line 6 and line 8. Line 17 must be lines 13
// and 16 added, and those their columns' lines 12 and 15.
func in152aLines(form *formin152a.FormIn152A) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetTotalInterestAndPenalty()}
	var interest, penalty int64
	for _, c := range form.GetPaymentPeriods() {
		interest += c.GetInterest().GetMinor()
		penalty += c.GetPenalty().GetMinor()
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	w.problems = vtSumProblem(w.problems, "IN-152A line 13", form.GetTotalInterest(), interest)
	w.problems = vtSumProblem(w.problems, "IN-152A line 16", form.GetTotalPenalty(), penalty)
	w.problems = vtSumProblem(w.problems, "IN-152A line 17", form.GetTotalInterestAndPenalty(),
		form.GetTotalInterest().GetMinor()+form.GetTotalPenalty().GetMinor())
	return w
}

// vtSumProblem adds a problem when a total line is not its parts added.
func vtSumProblem(problems []string, line string, total *commonv1.Money, added int64) []string {
	if total.GetMinor() != added {
		problems = append(problems, fmt.Sprintf("%s is %d, and its parts added are %d", line, total.GetMinor(), added))
	}
	return problems
}
