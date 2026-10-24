package ustaxcalc_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/ustaxcalc"
)

func TestItemisedLimitsAreInTheirSources(t *testing.T) {
	mustContain(t, "plaw-119-21-sec-70111.txt",
		"shall be reduced by \\2/37\\ of the lesser of--",
		"exceeds the dollar amount at which the 37 percent rate bracket under section 1 begins with respect to the taxpayer.",
		"This section shall be applied after the application of any other limitation on the allowance of any itemized deduction.",
		"shall apply to taxable years beginning after December 31, 2025.")
	mustContain(t, "plaw-119-21-sec-70425.txt",
		"shall be allowed only to the extent that the aggregate of such contributions exceeds 0.5 percent of the taxpayer's contribution base",
		"shall apply to taxable years beginning after December 31, 2025.")
}

// A single filer in 2026 with $800,000 of AGI and $60,000 itemised, $20,000
// of it to charity: 0.5% of AGI, $4,000, of the charity is lost, leaving
// $56,000; income is $159,400 over the 37% bracket's $640,600, so 2/37 of
// the lesser, $56,000, comes off: $3,027.02, leaving $52,972.98.
func TestItemisedLimits(t *testing.T) {
	const d = ustaxcalc.Dollar
	r, err := ustaxcalc.ItemisedLimits(2026, ustaxcalc.Single, 60_000*d, 20_000*d, 800_000*d)
	if err != nil || r.Value() != 5_297_298 {
		t.Errorf("%v, %v; want $52,972.98", r.Value(), err)
	}
	// Below the 37% bracket only the charity floor applies.
	if r, _ := ustaxcalc.ItemisedLimits(2026, ustaxcalc.Single, 60_000*d, 20_000*d, 300_000*d); r.Value() != 58_500*d {
		t.Errorf("%v; want $58,500", r.Value())
	}
	// A charity under the floor is lost whole.
	if r, _ := ustaxcalc.ItemisedLimits(2026, ustaxcalc.Single, 30_000*d, 1_000*d, 300_000*d); r.Value() != 29_000*d {
		t.Errorf("%v; want $29,000", r.Value())
	}
	// 2025 is before both.
	if r, _ := ustaxcalc.ItemisedLimits(2025, ustaxcalc.Single, 60_000*d, 20_000*d, 800_000*d); r.Value() != 60_000*d {
		t.Errorf("2025: %v; want $60,000", r.Value())
	}
	if _, err := ustaxcalc.ItemisedLimits(2026, ustaxcalc.Single, 1, 2, 0); err == nil {
		t.Error("charity over the itemised total was accepted")
	}
}
