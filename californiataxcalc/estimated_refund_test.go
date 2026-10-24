package californiataxcalc_test

import (
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/californiataxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/estimated/estimatedtest"
)

// Withholding over last year's tax reads as a refund, never as a minus amount.
func TestEstimatedRefundReadsAsRefund(t *testing.T) {
	rules, err := californiataxcalc.EstimatedTax(2026)
	if err != nil {
		t.Fatal(err)
	}
	var asked bool
	for _, a := range estimatedtest.Ask(rules, estimatedtest.Households(estimated.California)) {
		if a.Household().Name() != "withholding over last year's tax" {
			continue
		}
		asked = true
		res, err := a.Requirement()
		if err != nil {
			t.Fatal(err)
		}
		if why := res.Value().Why(); !strings.Contains(why, "and $2,136 was withheld. That left a $136 refund. California") {
			t.Errorf("why %q, want %q in it", why, "and $2,136 was withheld. That left a $136 refund. California")
		}
		for _, x := range estimatedtest.Texts(rules, []estimatedtest.Answers{a}) {
			if strings.Contains(x.Text(), "−$") || strings.Contains(x.Text(), "-$") {
				t.Errorf("%s: %q has a minus amount", x.Where(), x.Text())
			}
		}
	}
	if !asked {
		t.Fatal("no household withholding over last year's tax")
	}
}
