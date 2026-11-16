package golden

import (
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formtc546 "github.com/nexteggapp/taxprotos/go/us/state/ut/v1/form_tc_546"
)

// tc546Lines reads Utah's TC-546 Payment Worksheet: the minimum on line 5,
// the extension penalty, and one column, the original due date, whose
// required amount is line 5 and whose underpayment is line 8 when it is
// more than nothing.
func tc546Lines(form *formtc546.FormTc546) *worksheetLines {
	if form == nil {
		return nil
	}
	under := &commonv1.Money{Minor: max(0, form.GetMinimumPaymentDue().GetMinor())}
	return &worksheetLines{
		target:  form.GetMinimumTaxDue(),
		penalty: form.GetExtensionPenalty(),
		columns: []worksheetColumn{{form.GetMinimumTaxDue(), under}},
	}
}
