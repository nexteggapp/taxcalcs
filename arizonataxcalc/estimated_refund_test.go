package arizonataxcalc_test

import (
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
)

// Withholding over the tax reads as a refund, never as a minus amount.
func TestEstimatedRefundReadsAsRefund(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 3_000 * money.Dollar, withheld: 3_600 * money.Dollar}.facts(t)))
	r := res.Value()
	const want = "Your Arizona tax after credits is $3,000, and $3,600 was withheld. That leaves a $600 refund. No estimated payments are needed."
	if r.Why() != want {
		t.Errorf("why %q, want %q", r.Why(), want)
	}
	texts := []string{r.Why()}
	for _, o := range r.Options() {
		texts = append(texts, o.Label(), o.WhyNot())
	}
	for _, q := range res.Required() {
		texts = append(texts, q.Because(), q.Question(), q.Effect())
	}
	for _, s := range texts {
		if strings.Contains(s, "−$") || strings.Contains(s, "-$") {
			t.Errorf("%q has a minus amount", s)
		}
	}
}
