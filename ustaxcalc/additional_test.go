package ustaxcalc_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/ustaxcalc"
)

func TestAdditionalTaxesAreInTheirSources(t *testing.T) {
	mustContain(t, "i2210-2025-line-2.txt", "Line 8 (additional tax on distributions only),")
	mustContain(t, "i5329-2025-code-1.txt",
		"if distribution code 1 is correctly shown in box 7 of all your Forms 1099-R and you owe the additional tax on the full amount shown on each Form 1099-R, you don’t have to file Form 5329.")
	mustContain(t, "f5329-2025-part-i.txt", "Additional tax. Enter 10% (0.10) of line 3. Include this amount on Schedule 2 (Form 1040), line 8")
	mustContain(t, "i1040-2025-state-refund-worksheet.txt",
		"Enter the income tax refund from Form(s) 1099-G (or similar statement). But don’t enter more than the amount of your state and local income taxes shown on your 2024 Schedule A, line 5d",
		"Subtract the amount on your 2024 Schedule A, line 5e,",
		"Enter your total itemized deductions from your 2024 Schedule A, line 17.",
		"Taxable part of your refund. Enter the smaller of line 3 or line 8 here and on Schedule 1, line 1",
	)
}

func dist(t *testing.T, taxable ustaxcalc.USCents, code string) ustaxcalc.RetirementDistribution {
	t.Helper()
	d, err := ustaxcalc.NewRetirementDistribution(taxable, code)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// $8,000 with code 1 owes $800; a normal distribution, code 7, owes
// nothing.
func TestEarlyDistributionTax(t *testing.T) {
	const d = ustaxcalc.Dollar
	r, err := ustaxcalc.EarlyDistributionTax(2026, []ustaxcalc.RetirementDistribution{dist(t, 8_000*d, "1"), dist(t, 5_000*d, "7")})
	if err != nil || r.Value() != 800*d {
		t.Errorf("%v, %v; want $800", r.Value(), err)
	}
	if ustaxcalc.EarlyDistributionTaxOther([]ustaxcalc.RetirementDistribution{dist(t, 1, "1")}) ||
		!ustaxcalc.EarlyDistributionTaxOther([]ustaxcalc.RetirementDistribution{dist(t, 1, "S")}) {
		t.Error("code S is not reported as uncomputed, or code 1 is")
	}
	if _, err := ustaxcalc.NewRetirementDistribution(1, ""); err == nil {
		t.Error("a distribution with no code was accepted")
	}
}

// A $10,000 refund of 2025 state income tax for a single filer who
// itemized $23,000 in 2025, $6,513 of it state income tax, all deducted:
//   - line 1 is the refund up to the $6,513 deducted;
//   - nothing was kept back by the limit, so line 3 is $6,513;
//   - line 8 is $23,000 less 2025's $15,750 standard deduction, $7,250;
//   - the taxable part is the smaller, $6,513.
func TestStateRefundWorksheet(t *testing.T) {
	const d = ustaxcalc.Dollar
	taxable := func(b *ustaxcalc.StateRefundBuilder) ustaxcalc.USCents {
		t.Helper()
		r, err := b.Build()
		if err != nil {
			t.Fatal(err)
		}
		res, err := ustaxcalc.StateRefundTaxable(2026, r)
		if err != nil {
			t.Fatal(err)
		}
		return res.Value()
	}
	base := func() *ustaxcalc.StateRefundBuilder {
		return ustaxcalc.NewStateRefund(10_000*d, ustaxcalc.Single).PriorIncomeTax(6_513*d).PriorSALT(9_000*d, 9_000*d).PriorItemized(23_000 * d)
	}
	if got := taxable(base()); got != 6_513*d {
		t.Errorf("taxable %v, want $6,513", got)
	}
	// Itemizing only $2,000 more than the standard deduction caps it there.
	if got := taxable(base().PriorItemized(17_750 * d)); got != 2_000*d {
		t.Errorf("taxable %v, want $2,000", got)
	}
	// One box for age adds 2025's $2,000 to the standard deduction.
	if got := taxable(base().PriorItemized(17_750 * d).PriorBoxes(1)); got != 0 {
		t.Errorf("taxable %v, want none", got)
	}
	// $12,000 paid and $10,000 deducted after the limit keeps $2,000 back.
	if got := taxable(base().PriorSALT(12_000*d, 10_000*d)); got != 4_513*d {
		t.Errorf("taxable %v, want $4,513", got)
	}
	// The standard deduction last year: none of it is taxable.
	if got := taxable(base().PriorItemized(0)); got != 0 {
		t.Errorf("taxable %v, want none", got)
	}
	// Married separately with a spouse who itemized: no standard deduction.
	sep := ustaxcalc.NewStateRefund(10_000*d, ustaxcalc.MarriedFilingSeparately).PriorIncomeTax(6_513 * d).PriorItemized(3_000 * d).SpouseItemizedSeparately()
	if got := taxable(sep); got != 3_000*d {
		t.Errorf("taxable %v, want $3,000", got)
	}
}

func TestDistributionCodesAreInTheirSources(t *testing.T) {
	mustContain(t, "i1099r-2026-codes.txt",
		"1—Early distribution, no known exception.",
		"2—Early distribution, exception applies.",
		"7—Normal distribution.",
		"G—Direct rollover and direct payment.",
		"H—Direct rollover of a designated Roth account",
		"P—Excess contributions plus earnings/excess",
		"deferrals taxable in 2025 or a previous year.",
		"A—May be eligible for 10-year tax option.")
	mustContain(t, "i1099r-2026-direct-rollover.txt", "Report a direct rollover in box 1 and a -0- (zero) in box 2a, unless the rollover is a direct rollover of a qualified rollover contribution")
	mustContain(t, "i1040-2025-rollovers.txt", "Use lines 5a and 5b to report a rollover, including a direct rollover")
	mustContain(t, "usc26-469i-2024.txt",
		"shall not exceed $25,000.",
		"shall be reduced (but not below zero) by 50 percent of the amount by which the adjusted gross income of the taxpayer for the taxable year exceeds $100,000.",
		`(i) "$12,500" for "$25,000" each place it appears,`,
		"(ii) does not live apart from his spouse at all times during such taxable year.")
}

// The codes: 1 owes 10%; 2, 3, 4 and 7 owe none; G and H count their box 2a
// and owe none; P is not 2026 income. A, J and S are not worked out.
func TestDistributionCodes(t *testing.T) {
	const d = ustaxcalc.Dollar
	ds := []ustaxcalc.RetirementDistribution{dist(t, 1_000*d, "1"), dist(t, 2_000*d, "2"), dist(t, 0, "G"), dist(t, 3_000*d, "P"), dist(t, 4_000*d, "4G"), dist(t, 500*d, "1P")}
	if got := ustaxcalc.DistributionIncome(ds); got != 7_000*d {
		t.Errorf("income %v, want $7,000: everything but the two coded P", got)
	}
	r, _ := ustaxcalc.EarlyDistributionTax(2026, ds)
	if r.Value() != 100*d {
		t.Errorf("additional tax %v, want $100 on the $1,000 coded 1 alone", r.Value())
	}
	if got := ustaxcalc.UnhandledDistributionCodes(append(ds, dist(t, 1, "A"), dist(t, 1, "J7"), dist(t, 1, "A"))); len(got) != 2 || got[0] != "A" || got[1] != "J" {
		t.Errorf("unhandled %v, want A and J", got)
	}
}

// § 469(i): $25,000 less half of modified AGI over $100,000.
func TestRentalLossAllowed(t *testing.T) {
	const d = ustaxcalc.Dollar
	for _, tc := range []struct {
		status     ustaxcalc.FilingStatus
		loss, magi ustaxcalc.USCents
		apart      bool
		want       ustaxcalc.USCents
	}{
		{ustaxcalc.Single, 30_000 * d, 90_000 * d, false, 25_000 * d},
		{ustaxcalc.Single, 30_000 * d, 120_000 * d, false, 15_000 * d},
		{ustaxcalc.Single, 10_000 * d, 120_000 * d, false, 10_000 * d},
		{ustaxcalc.MarriedFilingJointly, 30_000 * d, 150_000 * d, false, 0},
		{ustaxcalc.MarriedFilingSeparately, 30_000 * d, 60_000 * d, true, 7_500 * d},
		{ustaxcalc.MarriedFilingSeparately, 30_000 * d, 20_000 * d, false, 0},
	} {
		r, err := ustaxcalc.RentalLossAllowed(tc.status, tc.loss, tc.magi, tc.apart)
		if err != nil || r.Value() != tc.want {
			t.Errorf("%v, %v loss at %v, apart %v: %v, %v; want %v", tc.status, tc.loss, tc.magi, tc.apart, r.Value(), err, tc.want)
		}
	}
}

// Form 2210 line 6 withholding includes Schedule 3 line 11, which is excess
// Social Security tax withheld, deemed paid a quarter on each due date.
func TestExcessSocialSecurityIsWithholding(t *testing.T) {
	mustContain(t, "i2210-2025-line-6.txt",
		"Include the taxes withheld shown on the following lines.",
		"Also, Schedule 3 (Form 1040), line 11, if you filed the above forms.",
		"For withheld federal income tax and excess social security or tier 1 RRTA, you are considered to have paid one-fourth of these amounts on each payment due date unless you can show otherwise.")
	mustContain(t, "f1040s3-2025-line-11.txt", "11 Excess social security and tier 1 RRTA tax withheld")
}
