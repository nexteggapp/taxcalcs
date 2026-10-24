package ustaxcalc_test

import (
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/ustaxcalc"
)

// TestLadderAgainstPublishedBaseAmounts is the strongest available check on the
// bracket walk.
//
// Rev. Proc. 2025-32 states each band twice: as a rate on the excess over a
// threshold, and as a base amount that the bands below it come to. "Over
// $105,700 but not over $201,775: $17,966 plus 24% of the excess over $105,700"
// says that taxing exactly $105,700 gives $17,966. Those base amounts are an
// independent arithmetic result published by the IRS, so reproducing them
// exercises the ladder against something other than its own inputs.
func TestLadderAgainstPublishedBaseAmounts(t *testing.T) {
	// Threshold to the base amount the revenue procedure publishes for it.
	cases := map[ustaxcalc.FilingStatus][]struct {
		at   ustaxcalc.USCents
		base ustaxcalc.USCents
	}{
		ustaxcalc.Single: {
			{ustaxcalc.Dollars(12_400), ustaxcalc.Dollars(1_240)},
			{ustaxcalc.Dollars(50_400), ustaxcalc.Dollars(5_800)},
			{ustaxcalc.Dollars(105_700), ustaxcalc.Dollars(17_966)},
			{ustaxcalc.Dollars(201_775), ustaxcalc.Dollars(41_024)},
			{ustaxcalc.Dollars(256_225), ustaxcalc.Dollars(58_448)},
			{ustaxcalc.Dollars(640_600), ustaxcalc.USCents(19_297_925)}, // $192,979.25
		},
		ustaxcalc.MarriedFilingJointly: {
			{ustaxcalc.Dollars(24_800), ustaxcalc.Dollars(2_480)},
			{ustaxcalc.Dollars(100_800), ustaxcalc.Dollars(11_600)},
			{ustaxcalc.Dollars(211_400), ustaxcalc.Dollars(35_932)},
			{ustaxcalc.Dollars(403_550), ustaxcalc.Dollars(82_048)},
			{ustaxcalc.Dollars(512_450), ustaxcalc.Dollars(116_896)},
			{ustaxcalc.Dollars(768_700), ustaxcalc.USCents(20_658_350)}, // $206,583.50
		},
		ustaxcalc.HeadOfHousehold: {
			{ustaxcalc.Dollars(17_700), ustaxcalc.Dollars(1_770)},
			{ustaxcalc.Dollars(67_450), ustaxcalc.Dollars(7_740)},
			{ustaxcalc.Dollars(105_700), ustaxcalc.Dollars(16_155)},
			{ustaxcalc.Dollars(201_750), ustaxcalc.Dollars(39_207)},
			{ustaxcalc.Dollars(256_200), ustaxcalc.Dollars(56_631)},
			{ustaxcalc.Dollars(640_600), ustaxcalc.Dollars(191_171)},
		},
		ustaxcalc.MarriedFilingSeparately: {
			{ustaxcalc.Dollars(256_225), ustaxcalc.Dollars(58_448)},
			{ustaxcalc.Dollars(384_350), ustaxcalc.USCents(10_329_175)}, // $103,291.75
		},
	}

	for status, points := range cases {
		for _, p := range points {
			got, err := ustaxcalc.OrdinaryIncomeTax(2026, status, p.at)
			if err != nil {
				t.Fatalf("OrdinaryIncomeTax(2026, %s, %s): %v", status, p.at, err)
			}
			if got.Value().Total != p.base {
				t.Errorf("%s tax on %s is %s, the revenue procedure publishes %s",
					status, p.at, got.Value().Total, p.base)
			}
		}
	}
}

// TestLadderWorkedExample checks the ladder against a worked
// example: a single filer with $192,275 of ordinary taxable income, who owes
// $38,744 of tax at a 24% marginal rate with $9,500 of room before the 32%
// band.
func TestLadderWorkedExample(t *testing.T) {
	const ordinary = 192_275 * ustaxcalc.Dollar

	got, err := ustaxcalc.OrdinaryIncomeTax(2026, ustaxcalc.Single, ordinary)
	if err != nil {
		t.Fatalf("OrdinaryIncomeTax: %v", err)
	}
	if want := ustaxcalc.Dollars(38_744); got.Value().Total != want {
		t.Errorf("tax on %s is %s, want %s", ordinary, got.Value().Total, want)
	}
	if want := ustaxcalc.Percent(24); got.Value().Marginal != want {
		t.Errorf("marginal rate is %s, want %s", got.Value().Marginal, want)
	}
	if want := ustaxcalc.Dollars(9_500); got.Value().RoomInBracket != want {
		t.Errorf("room in the bracket is %s, want %s", got.Value().RoomInBracket, want)
	}

	// The band breakdown, top to bottom of what was reached.
	for _, want := range []struct {
		rate   ustaxcalc.Rate
		income ustaxcalc.USCents
		tax    ustaxcalc.USCents
	}{
		{ustaxcalc.Percent(10), ustaxcalc.Dollars(12_400), ustaxcalc.Dollars(1_240)},
		{ustaxcalc.Percent(12), ustaxcalc.Dollars(38_000), ustaxcalc.Dollars(4_560)},
		{ustaxcalc.Percent(22), ustaxcalc.Dollars(55_300), ustaxcalc.Dollars(12_166)},
		{ustaxcalc.Percent(24), ustaxcalc.Dollars(86_575), ustaxcalc.Dollars(20_778)},
	} {
		var found bool
		for _, band := range got.Value().Bands {
			if band.Rate != want.rate {
				continue
			}
			found = true
			if band.Income != want.income || band.Tax != want.tax {
				t.Errorf("the %s band took %s and taxed %s, want %s and %s",
					want.rate, band.Income, band.Tax, want.income, want.tax)
			}
		}
		if !found {
			t.Errorf("no %s band in the ladder", want.rate)
		}
	}

	// Every band's tax has to add up to the total.
	var summed ustaxcalc.USCents
	for _, band := range got.Value().Bands {
		summed += band.Tax
	}
	if summed != got.Value().Total {
		t.Errorf("the bands sum to %s, the total is %s", summed, got.Value().Total)
	}
}

func TestLadderEdges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		income ustaxcalc.USCents
		want   ustaxcalc.USCents
	}{
		{"no income", 0, 0},
		{"a deduction larger than income", ustaxcalc.Dollars(-5_000), 0},
		{"one cent", 1, 0}, // 10% of a cent rounds to nothing
		{"the first dollar", ustaxcalc.Dollars(1), ustaxcalc.USCents(10)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ustaxcalc.OrdinaryIncomeTax(2026, ustaxcalc.Single, tc.income)
			if err != nil {
				t.Fatalf("OrdinaryIncomeTax: %v", err)
			}
			if got.Value().Total != tc.want {
				t.Errorf("tax on %s is %s, want %s", tc.income, got.Value().Total, tc.want)
			}
		})
	}
}

// TestGainsStackOnOrdinaryIncome is the property that makes a gain's rate
// depend on the rest of the return rather than on the gain.
func TestGainsStackOnOrdinaryIncome(t *testing.T) {
	gain := ustaxcalc.Dollars(10_000)

	for _, tc := range []struct {
		name                     string
		ordinary                 ustaxcalc.USCents
		wantZero, want15, want20 ustaxcalc.USCents
	}{
		{
			// The whole gain sits below the $49,450 zero rate amount.
			name: "below the zero rate amount", ordinary: ustaxcalc.Dollars(20_000),
			wantZero: gain,
		},
		{
			// $39,450 of headroom takes part of the gain, the rest is at 15%.
			name: "straddling the zero rate amount", ordinary: ustaxcalc.Dollars(10_000),
			wantZero: gain,
		},
		{
			name: "split across the zero rate amount", ordinary: ustaxcalc.Dollars(45_000),
			wantZero: ustaxcalc.Dollars(4_450), want15: ustaxcalc.Dollars(5_550),
		},
		{
			name: "entirely at 15%", ordinary: ustaxcalc.Dollars(100_000),
			want15: gain,
		},
		{
			// Total lands above the $545,500 fifteen percent amount.
			name: "split across the 15% amount", ordinary: ustaxcalc.Dollars(540_000),
			want15: ustaxcalc.Dollars(5_500), want20: ustaxcalc.Dollars(4_500),
		},
		{
			name: "entirely at 20%", ordinary: ustaxcalc.Dollars(600_000),
			want20: gain,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ustaxcalc.LongTermGainsTax(2026, ustaxcalc.Single, tc.ordinary, gain)
			if err != nil {
				t.Fatalf("LongTermGainsTax: %v", err)
			}
			g := got.Value()
			if g.AtZero != tc.wantZero || g.At15 != tc.want15 || g.At20 != tc.want20 {
				t.Errorf("gain split 0%%=%s 15%%=%s 20%%=%s, want 0%%=%s 15%%=%s 20%%=%s",
					g.AtZero, g.At15, g.At20, tc.wantZero, tc.want15, tc.want20)
			}
			if sum := g.AtZero + g.At15 + g.At20; sum != gain {
				t.Errorf("the three parts come to %s, the gain is %s", sum, gain)
			}
			if want := ustaxcalc.Percent(15).Of(g.At15) + ustaxcalc.Percent(20).Of(g.At20); g.Total != want {
				t.Errorf("tax on the gain is %s, want %s", g.Total, want)
			}
		})
	}
}

// TestGainsWorkedExample checks a worked example:
// $8,420 of long-term gain above $192,275 of ordinary income, taxed at 15%.
func TestGainsWorkedExample(t *testing.T) {
	got, err := ustaxcalc.LongTermGainsTax(2026, ustaxcalc.Single, 192_275*ustaxcalc.Dollar, ustaxcalc.Dollars(8_420))
	if err != nil {
		t.Fatalf("LongTermGainsTax: %v", err)
	}
	if want := ustaxcalc.Dollars(1_263); got.Value().Total != want {
		t.Errorf("tax on the gain is %s, want %s", got.Value().Total, want)
	}
	if got.Value().At15 != ustaxcalc.Dollars(8_420) {
		t.Errorf("%s of the gain was taxed at 15%%, want all of it", got.Value().At15)
	}
}

// TestGainsRequireALotPosition records that the calculator will not settle
// which lots a sale consumed.
func TestGainsRequireALotPosition(t *testing.T) {
	got, err := ustaxcalc.LongTermGainsTax(2026, ustaxcalc.Single, ustaxcalc.Dollars(50_000), ustaxcalc.Dollars(1_000))
	if err != nil {
		t.Fatalf("LongTermGainsTax: %v", err)
	}
	if !got.Needs() {
		t.Fatal("a gain was computed with no position required, want the lot selection")
	}
	if got.Required()[0].Position() != ustaxcalc.PositionLotSelection {
		t.Errorf("required position is %v, want PositionLotSelection", got.Required()[0].Position())
	}
}

// TestSelfEmploymentWageBaseInteraction covers self-employment tax that cannot
// be computed from net profit alone, because W-2 wages share the wage base.
func TestSelfEmploymentWageBaseInteraction(t *testing.T) {
	// $57,273 of net profit.
	netProfit := ustaxcalc.Dollars(57_273)

	t.Run("no W-2 wages", func(t *testing.T) {
		got, err := ustaxcalc.SelfEmploymentTax(2026, ustaxcalc.Single, netProfit, 0, 0)
		if err != nil {
			t.Fatalf("SelfEmploymentTax: %v", err)
		}
		// 92.35% of $57,273 is $52,891.6155, which rounds up to the cent. A
		// filed Schedule SE shows $52,892, because the form rounds to whole
		// dollars and this carries cents.
		if want := ustaxcalc.USCents(5_289_162); got.Value().NetEarnings != want {
			t.Errorf("net earnings are %s, want %s", got.Value().NetEarnings, want)
		}
		// The whole of it is under the wage base, so 12.4% applies to all.
		if want := ustaxcalc.Rate(1240).Of(got.Value().NetEarnings); got.Value().SocialSecurity != want {
			t.Errorf("Social Security part is %s, want %s", got.Value().SocialSecurity, want)
		}
		if got.Value().AdditionalMedicare != 0 {
			t.Errorf("additional Medicare of %s on earnings well under the threshold", got.Value().AdditionalMedicare)
		}
		// The deduction excludes the additional Medicare tax, which is zero
		// here, so it is exactly half.
		if want := ustaxcalc.Percent(50).Of(got.Value().Total); got.Value().AboveTheLineDeduction != want {
			t.Errorf("deduction is %s, want %s", got.Value().AboveTheLineDeduction, want)
		}
	})

	t.Run("W-2 wages already past the base", func(t *testing.T) {
		got, err := ustaxcalc.SelfEmploymentTax(2026, ustaxcalc.Single, netProfit, ustaxcalc.Dollars(200_000), ustaxcalc.Dollars(200_000))
		if err != nil {
			t.Fatalf("SelfEmploymentTax: %v", err)
		}
		if got.Value().SocialSecurity != 0 {
			t.Errorf("Social Security part is %s, want nothing once wages passed the base", got.Value().SocialSecurity)
		}
		// Medicare is uncapped, so it still applies in full.
		if want := ustaxcalc.Rate(290).Of(got.Value().NetEarnings); got.Value().Medicare != want {
			t.Errorf("Medicare part is %s, want %s", got.Value().Medicare, want)
		}
		// Wages alone passed $200,000, so every dollar of self-employment
		// earnings is over the additional Medicare threshold.
		if want := ustaxcalc.Rate(90).Of(got.Value().NetEarnings); got.Value().AdditionalMedicare != want {
			t.Errorf("additional Medicare is %s, want %s", got.Value().AdditionalMedicare, want)
		}
	})

	t.Run("W-2 wages partly consuming the base", func(t *testing.T) {
		// $150,000 of wages leaves $34,500 of the $184,500 base.
		got, err := ustaxcalc.SelfEmploymentTax(2026, ustaxcalc.Single, netProfit, ustaxcalc.Dollars(150_000), ustaxcalc.Dollars(150_000))
		if err != nil {
			t.Fatalf("SelfEmploymentTax: %v", err)
		}
		if want := ustaxcalc.Rate(1240).Of(ustaxcalc.Dollars(34_500)); got.Value().SocialSecurity != want {
			t.Errorf("Social Security part is %s, want %s on the remaining base", got.Value().SocialSecurity, want)
		}
	})

	t.Run("under the de minimis floor", func(t *testing.T) {
		got, err := ustaxcalc.SelfEmploymentTax(2026, ustaxcalc.Single, ustaxcalc.Dollars(400), 0, 0)
		if err != nil {
			t.Fatalf("SelfEmploymentTax: %v", err)
		}
		// 92.35% of $400 is $369.40, which is under the $400 floor.
		if got.Value().Total != 0 {
			t.Errorf("self-employment tax of %s on net earnings under $400", got.Value().Total)
		}
	})
}

// TestSafeHarbourPicksTheLesserTest covers both sides of the § 6654(d)(1)(C)
// step and the case where no prior return exists.
func TestSafeHarbourPicksTheLesserTest(t *testing.T) {
	currentYear := ustaxcalc.Dollars(48_169) // the year's estimated liability

	t.Run("prior year AGI over the step", func(t *testing.T) {
		// $39,400 of prior year tax on $172,800 of AGI.
		got, err := ustaxcalc.RequiredAnnualPayment(2026, ustaxcalc.Single,
			currentYear, ustaxcalc.Dollars(39_400), ustaxcalc.Dollars(172_800), true)
		if err != nil {
			t.Fatalf("RequiredAnnualPayment: %v", err)
		}
		if got.Value().PriorYearRate != ustaxcalc.Percent(110) {
			t.Errorf("prior year rate is %s, want 110%% above the step", got.Value().PriorYearRate)
		}
		if want := ustaxcalc.Dollars(43_340); got.Value().PriorYearTest != want {
			t.Errorf("prior year test is %s, want %s", got.Value().PriorYearTest, want)
		}
		if got.Value().Required != got.Value().PriorYearTest {
			t.Errorf("required payment is %s, want the lesser prior year test %s",
				got.Value().Required, got.Value().PriorYearTest)
		}
	})

	t.Run("prior year AGI under the step", func(t *testing.T) {
		got, err := ustaxcalc.RequiredAnnualPayment(2026, ustaxcalc.Single,
			currentYear, ustaxcalc.Dollars(39_400), ustaxcalc.Dollars(140_000), true)
		if err != nil {
			t.Fatalf("RequiredAnnualPayment: %v", err)
		}
		if got.Value().PriorYearRate != ustaxcalc.Percent(100) {
			t.Errorf("prior year rate is %s, want 100%% below the step", got.Value().PriorYearRate)
		}
		if want := ustaxcalc.Dollars(39_400); got.Value().Required != want {
			t.Errorf("required payment is %s, want %s", got.Value().Required, want)
		}
	})

	t.Run("a separate return halves the step", func(t *testing.T) {
		got, err := ustaxcalc.RequiredAnnualPayment(2026, ustaxcalc.MarriedFilingSeparately,
			currentYear, ustaxcalc.Dollars(39_400), ustaxcalc.Dollars(80_000), true)
		if err != nil {
			t.Fatalf("RequiredAnnualPayment: %v", err)
		}
		if got.Value().PriorYearRate != ustaxcalc.Percent(110) {
			t.Errorf("prior year rate is %s, want 110%% above the $75,000 step", got.Value().PriorYearRate)
		}
	})

	t.Run("a small prior year tax wins", func(t *testing.T) {
		got, err := ustaxcalc.RequiredAnnualPayment(2026, ustaxcalc.Single,
			currentYear, ustaxcalc.Dollars(2_000), ustaxcalc.Dollars(30_000), true)
		if err != nil {
			t.Fatalf("RequiredAnnualPayment: %v", err)
		}
		if want := ustaxcalc.Dollars(2_000); got.Value().Required != want {
			t.Errorf("required payment is %s, want %s", got.Value().Required, want)
		}
	})

	t.Run("no prior return", func(t *testing.T) {
		got, err := ustaxcalc.RequiredAnnualPayment(2026, ustaxcalc.Single, currentYear, 0, 0, false)
		if err != nil {
			t.Fatalf("RequiredAnnualPayment: %v", err)
		}
		if got.Value().Required != got.Value().CurrentYearTest {
			t.Errorf("required payment is %s, want the current year test %s",
				got.Value().Required, got.Value().CurrentYearTest)
		}
	})
}

// TestInstalmentsWorkedExample checks the four cumulative
// targets against a $43,340 safe harbour.
func TestInstalmentsWorkedExample(t *testing.T) {
	got := ustaxcalc.Instalments(2026, ustaxcalc.Dollars(43_340))

	want := []struct {
		due    time.Time
		target ustaxcalc.USCents
	}{
		{time.Date(2026, time.April, 15, 0, 0, 0, 0, time.UTC), ustaxcalc.Dollars(10_835)},
		{time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC), ustaxcalc.Dollars(21_670)},
		{time.Date(2026, time.September, 15, 0, 0, 0, 0, time.UTC), ustaxcalc.Dollars(32_505)},
		{time.Date(2027, time.January, 15, 0, 0, 0, 0, time.UTC), ustaxcalc.Dollars(43_340)},
	}

	if len(got) != len(want) {
		t.Fatalf("%d instalments, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Due.Equal(want[i].due) {
			t.Errorf("instalment %d is due %s, want %s", i+1, got[i].Due, want[i].due)
		}
		if got[i].CumulativeTarget != want[i].target {
			t.Errorf("instalment %d target is %s, want %s",
				i+1, got[i].CumulativeTarget, want[i].target)
		}
	}

	// The last cumulative target has to be the whole thing, or a taxpayer who
	// met every instalment still owes a penalty.
	if got[len(got)-1].CumulativeTarget != ustaxcalc.Dollars(43_340) {
		t.Errorf("the final target is %s, want the full required payment", got[len(got)-1].CumulativeTarget)
	}
}

// TestWithholdingIsCreditedEvenly is the § 6654(g) rule that makes withholding
// and estimated payments count differently.
func TestWithholdingIsCreditedEvenly(t *testing.T) {
	withheld := ustaxcalc.Dollars(41_808) // the year's projected withholding

	for quarter, want := range map[int]ustaxcalc.USCents{
		1: ustaxcalc.Dollars(10_452),
		2: ustaxcalc.Dollars(20_904),
		3: ustaxcalc.Dollars(31_356),
		4: ustaxcalc.Dollars(41_808),
	} {
		if got := ustaxcalc.WithholdingCreditedBy(withheld, quarter); got != want {
			t.Errorf("quarter %d credits %s, want %s", quarter, got, want)
		}
	}
	if got := ustaxcalc.WithholdingCreditedBy(withheld, 5); got != 0 {
		t.Errorf("quarter 5 credits %s, want nothing", got)
	}
}

// TestStandardDeductionAgedAndBlind covers the § 63(f) additions, including the
// one place a surviving spouse differs from a joint filer.
func TestStandardDeductionAgedAndBlind(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status ustaxcalc.FilingStatus
		cond   ustaxcalc.Conditions
		want   ustaxcalc.USCents
	}{
		{"single, neither", ustaxcalc.Single, ustaxcalc.Conditions{}, ustaxcalc.Dollars(16_100)},
		{"single, aged", ustaxcalc.Single, ustaxcalc.Conditions{Aged65OrOlder: true}, ustaxcalc.Dollars(18_150)},
		{"single, aged and blind", ustaxcalc.Single, ustaxcalc.Conditions{Aged65OrOlder: true, Blind: true}, ustaxcalc.Dollars(20_200)},
		{"joint, one aged", ustaxcalc.MarriedFilingJointly, ustaxcalc.Conditions{Aged65OrOlder: true}, ustaxcalc.Dollars(33_850)},
		{"joint, both aged", ustaxcalc.MarriedFilingJointly, ustaxcalc.Conditions{Aged65OrOlder: true, SpouseAged65OrOlder: true}, ustaxcalc.Dollars(35_500)},
		// A surviving spouse takes the joint standard deduction and the smaller
		// § 63(f) addition, because the statute excludes them from the
		// unmarried amount by name.
		{"surviving spouse, aged", ustaxcalc.SurvivingSpouse, ustaxcalc.Conditions{Aged65OrOlder: true}, ustaxcalc.Dollars(33_850)},
		{"head of household, aged", ustaxcalc.HeadOfHousehold, ustaxcalc.Conditions{Aged65OrOlder: true}, ustaxcalc.Dollars(26_200)},
		// A spouse's condition is not counted on a separate return.
		{"separate, spouse aged", ustaxcalc.MarriedFilingSeparately, ustaxcalc.Conditions{SpouseAged65OrOlder: true}, ustaxcalc.Dollars(16_100)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ustaxcalc.StandardDeduction(2026, tc.status, tc.cond)
			if err != nil {
				t.Fatalf("StandardDeduction: %v", err)
			}
			if got.Value() != tc.want {
				t.Errorf("standard deduction is %s, want %s", got.Value(), tc.want)
			}
		})
	}
}

// TestDeductionTakenAlwaysAsksForTheElection records that the larger figure
// winning is the calculator's default and not the taxpayer's only option.
func TestDeductionTakenAlwaysAsksForTheElection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		itemised   ustaxcalc.USCents
		wantAmount ustaxcalc.USCents
		wantItem   bool
	}{
		{"nothing itemised", 0, ustaxcalc.Dollars(16_100), false},
		{"itemised smaller", ustaxcalc.Dollars(10_000), ustaxcalc.Dollars(16_100), false},
		{"itemised larger", ustaxcalc.Dollars(20_000), ustaxcalc.Dollars(20_000), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ustaxcalc.DeductionTaken(2026, ustaxcalc.Single, ustaxcalc.Conditions{}, tc.itemised, ustaxcalc.ElectLarger)
			if err != nil {
				t.Fatalf("DeductionTaken: %v", err)
			}
			if got.Value().Amount != tc.wantAmount || got.Value().Itemised != tc.wantItem {
				t.Errorf("took %s (itemised %t), want %s (itemised %t)",
					got.Value().Amount, got.Value().Itemised, tc.wantAmount, tc.wantItem)
			}
			if got.Value().Standard != ustaxcalc.Dollars(16_100) {
				t.Errorf("standard deduction reported as %s, want $16,100 whichever was taken", got.Value().Standard)
			}
			if !got.Needs() {
				t.Error("no position required, want the deduction election")
			}
		})
	}
}

// TestEveryResultCitesItsRule is what makes a figure checkable. A calculator
// returning a number with no authority behind it is one a user has to trust.
func TestEveryResultCitesItsRule(t *testing.T) {
	ladder, err := ustaxcalc.OrdinaryIncomeTax(2026, ustaxcalc.Single, ustaxcalc.Dollars(80_000))
	if err != nil {
		t.Fatalf("OrdinaryIncomeTax: %v", err)
	}
	seca, err := ustaxcalc.SelfEmploymentTax(2026, ustaxcalc.Single, ustaxcalc.Dollars(50_000), 0, 0)
	if err != nil {
		t.Fatalf("SelfEmploymentTax: %v", err)
	}
	harbour, err := ustaxcalc.RequiredAnnualPayment(2026, ustaxcalc.Single,
		ustaxcalc.Dollars(20_000), ustaxcalc.Dollars(18_000), ustaxcalc.Dollars(90_000), true)
	if err != nil {
		t.Fatalf("RequiredAnnualPayment: %v", err)
	}

	for name, applied := range map[string][]ustaxcalc.Rule{
		"OrdinaryIncomeTax":     ladder.Applied(),
		"SelfEmploymentTax":     seca.Applied(),
		"RequiredAnnualPayment": harbour.Applied(),
	} {
		if len(applied) == 0 {
			t.Errorf("%s cited no rule", name)
			continue
		}
		for i, rule := range applied {
			if rule.Cite() == "" {
				t.Errorf("%s rule %d has no citation", name, i)
			}
			if rule.Applied() == "" {
				t.Errorf("%s rule %d does not say what it did", name, i)
			}
		}
	}
}
