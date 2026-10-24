package federal

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/taxyear/model"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// The overpayment, line 34, divides as the filed return divides it: line
// 36 applied to next year, held to this overpayment, and the rest refunded
// on line 35a. Without a filed return it stays whole.
func TestSplitOverpayment(t *testing.T) {
	for _, c := range []struct {
		filed             *form1040.Form1040
		refunded, applied int64
	}{
		{&form1040.Form1040{AppliedToNextYear: model.Money(20_500)}, 0, 20_500},
		{&form1040.Form1040{AppliedToNextYear: model.Money(10_000)}, 10_500, 10_000},
		{&form1040.Form1040{AppliedToNextYear: model.Money(90_000)}, 0, 20_500},
		{nil, 0, 0},
	} {
		r := &Return{form: &factsv1.FederalReturn{Form_1040: &form1040.Form1040{Overpaid: model.Money(20_500)}}}
		r.SplitOverpayment(c.filed)
		f := r.form.GetForm_1040()
		if f.GetRefunded().GetMinor() != c.refunded || f.GetAppliedToNextYear().GetMinor() != c.applied {
			t.Errorf("filed %v: refunded %v and applied %v, want %d and %d", c.filed, f.GetRefunded(), f.GetAppliedToNextYear(), c.refunded, c.applied)
		}
	}
}
