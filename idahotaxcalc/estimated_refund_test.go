package idahotaxcalc_test

import (
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/estimated/estimatedtest"
	"github.com/nexteggapp/taxcalcs/idahotaxcalc"
)

// Withholding over the tax prints no minus amount.
func TestEstimatedRefundReadsAsRefund(t *testing.T) {
	rules, err := idahotaxcalc.EstimatedTax(2026)
	if err != nil {
		t.Fatal(err)
	}
	var asked bool
	for _, a := range estimatedtest.Ask(rules, estimatedtest.Households(estimated.Idaho)) {
		if a.Household().Name() != "withholding over the tax" {
			continue
		}
		asked = true
		res, err := a.Requirement()
		if err != nil {
			t.Fatal(err)
		}
		if why := res.Value().Why(); !strings.Contains(why, "Idaho does not require individuals to make estimated tax payments") {
			t.Errorf("why %q, want %q in it", why, "Idaho does not require individuals to make estimated tax payments")
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
