package golden

import (
	"fmt"

	capitalgainspayment "github.com/nexteggapp/taxprotos/go/us/state/wa/v1/capital_gains_payment"
)

// capitalGainsPaymentLines are Washington's capital gains tax payment, which
// has no printed form: the tax as the required annual payment and the one
// installment, the tax unpaid at the due date as its underpayment, and the
// penalties and interest added as the penalty. That total must be the
// low-estimate penalty, the late payment penalty and the interest added, and
// each of those its parts added.
func capitalGainsPaymentLines(p *capitalgainspayment.CapitalGainsPayment) *worksheetLines {
	if p == nil {
		return nil
	}
	w := &worksheetLines{target: p.GetTax(), penalty: p.GetTotalPenaltyAndInterest(),
		columns: []worksheetColumn{{p.GetTax(), p.GetUnpaidAtDueDate()}}}
	var late, interest int64
	for _, s := range p.GetLatePaymentSteps() {
		late += s.GetPenalty().GetMinor()
	}
	for _, i := range p.GetInterestPeriods() {
		interest += i.GetInterest().GetMinor()
	}
	sum := func(what string, got, want int64) {
		if got != want {
			w.problems = append(w.problems, fmt.Sprintf("%s is %d, and its parts added are %d", what, got, want))
		}
	}
	sum("the late payment penalty", p.GetLatePaymentPenalty().GetMinor(), late)
	sum("the interest", p.GetInterest().GetMinor(), interest)
	sum("the penalties and interest", p.GetTotalPenaltyAndInterest().GetMinor(),
		p.GetLowEstimatePenalty().GetMinor()+p.GetLatePaymentPenalty().GetMinor()+p.GetInterest().GetMinor())
	return w
}
