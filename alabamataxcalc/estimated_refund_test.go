package alabamataxcalc_test

import (
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/alabamataxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/estimated/estimatedtest"
)

// Withholding over the tax reads as a refund, never as a minus amount.
func TestEstimatedRefundReadsAsRefund(t *testing.T) {
	rules, err := alabamataxcalc.EstimatedTax(2026)
	if err != nil {
		t.Fatal(err)
	}
	var asked bool
	for _, a := range estimatedtest.Ask(rules, estimatedtest.Households(estimated.Alabama)) {
		if a.Household().Name() != "withholding over the tax" {
			continue
		}
		asked = true
		res, err := a.Requirement()
		if err != nil {
			t.Fatal(err)
		}
		if why := res.Value().Why(); !strings.Contains(why, "leaves a $600 refund. No estimated payments are needed") {
			t.Errorf("why %q, want %q in it", why, "leaves a $600 refund. No estimated payments are needed")
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
