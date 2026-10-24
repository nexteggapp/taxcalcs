package georgiataxcalc_test

import (
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/estimated/estimatedtest"
	"github.com/nexteggapp/taxcalcs/georgiataxcalc"
)

// Withholding over the tax says it reaches the target, never a minus amount.
func TestEstimatedRefundReadsAsRefund(t *testing.T) {
	rules, err := georgiataxcalc.EstimatedTax(2026)
	if err != nil {
		t.Fatal(err)
	}
	var asked bool
	for _, a := range estimatedtest.Ask(rules, estimatedtest.Households(estimated.Georgia)) {
		if a.Household().Name() != "withholding over the tax" {
			continue
		}
		asked = true
		res, err := a.Requirement()
		if err != nil {
			t.Fatal(err)
		}
		if why := res.Value().Why(); !strings.Contains(why, "$3,600 of Georgia withholding reaches the $2,100 needed") {
			t.Errorf("why %q, want %q in it", why, "$3,600 of Georgia withholding reaches the $2,100 needed")
		}
		for _, x := range estimatedtest.Texts(rules, []estimatedtest.Answers{a}) {
			if strings.Contains(x.Text(), "−$") || strings.Contains(x.Text(), "-$") {
				t.Errorf("%s: %q has a minus amount", x.Where(), x.Text())
			}
		}
	}
	if !asked {
		t.Fatal("no household withholding over the tax")
	}
}
