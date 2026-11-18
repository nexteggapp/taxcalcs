package golden

import (
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formrev1630 "github.com/nexteggapp/taxprotos/go/us/state/pa/v1/form_rev_1630"
)

// rev1630Lines reads Pennsylvania's Form REV-1630: the interest on line 16,
// and of each column what is needed by its date and its underpayment.
//
// REV-1630 has no line for the required annual payment, so the target is
// not compared. Line 5 is net of withholding and credits, which Section I
// line 2 takes off and the Exception 1 Worksheet's line A puts back a
// quarter at a time; the core counts withholding as paid on the due dates
// and takes the credits off what is needed. So what the core needs by each
// date under the 90% test is lines 5 and A added, less the credits' quarter,
// summed to date. Lines 11 to 13 count the credits as line 11 does, so the
// credits' quarters to date come off them too. What is needed by a date is
// the smallest of the 90% test, Exception 1 (line 12) and Exception 2 (line
// 13), where each is figured; nothing before a late start the special exception covers (line B
// answered Yes, the X of lines 14a and 14b in the columns before the first
// without it). The underpayment is line 9, unless what was credited by the
// date reaches what is needed by it: then an exception takes it off.
func rev1630Lines(in *returnsv1.ReturnInputs, form *formrev1630.FormRev1630) *worksheetLines {
	if form == nil {
		return nil
	}
	var credits int64
	for _, s := range in.GetEstimatedTax().GetStateInputs() {
		if pa := s.GetPa(); pa != nil {
			credits = pa.GetCredits().GetMinor()
		}
	}
	w := &worksheetLines{penalty: form.GetTotalInterest()}
	special := form.GetPaymentsBeganWhenKnown() == formrev1630.FormRev1630_PAYMENTS_BEGAN_WHEN_KNOWN_YES
	var regular, before int64
	for i, c := range form.GetPaymentPeriods() {
		ccum := quarters(credits, i+1)
		cinc := ccum - before
		before = ccum
		regular += c.GetRequiredInstallment().GetMinor() + c.GetWithholdingAndCreditsShare().GetMinor() - cinc
		needed := regular
		if c.Exception_1 != nil {
			needed = min(needed, c.GetException_1().GetMinor()-ccum)
		}
		if c.Exception_2 != nil {
			needed = min(needed, max(0, c.GetException_2().GetMinor()-ccum))
		}
		if special && c.GetExceptionApplies() && prefixExcepted(form, i) {
			needed = 0
		}
		w.columns = append(w.columns, worksheetColumn{&commonv1.Money{Minor: needed}, nil})
		credited := c.GetCumulativeCredits().GetMinor() - ccum
		under := c.GetUnderpayment().GetMinor()
		if credited >= needed {
			under = 0
		}
		w.columns[i].underpayment = &commonv1.Money{Minor: under}
	}
	// The columns hold running totals so far; the checker reads each
	// column's own amount.
	var prev int64
	for i := range w.columns {
		total := w.columns[i].required.GetMinor()
		w.columns[i].required = &commonv1.Money{Minor: total - prev}
		prev = total
	}
	return w
}

// prefixExcepted reports whether every column up to and including i has
// the X of lines 14a and 14b, as the columns before a late start the
// special exception covers do.
func prefixExcepted(form *formrev1630.FormRev1630, i int) bool {
	for _, c := range form.GetPaymentPeriods()[:i+1] {
		if !c.GetExceptionApplies() {
			return false
		}
	}
	return true
}

// quarters is n quarters of a, to the nearest cent, halves away from zero.
func quarters(a int64, n int) int64 {
	product := a * int64(n)
	if product < 0 {
		return (product - 2) / 4
	}
	return (product + 2) / 4
}
