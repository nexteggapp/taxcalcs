package californiataxcalc

// Tax year 2025.
//
// The rate schedules, the standard deduction, the exemption credits and the
// adjusted gross income limitation are the 2025 Personal Income Tax Booklet,
// which is the Form 540 instructions the Franchise Tax Board publishes. The
// extracts those were read from are in testdata and the tests in
// tables_test.go check every figure below against them.
//
// The base tax on each bracket is written as the booklet prints it, in dollars
// and cents, because it is a published figure rather than one this library
// derives. TestLadderReproducesEveryPublishedBaseAmount checks that the ladder
// reproduces it.

import (
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
)

func init() {
	const (
		// Rev. & Tax. Code § 17041(a). The three schedules share these rates
		// and differ only in where the bands start.
		r1     = money.Rate(100)  // 1%
		r2     = money.Rate(200)  // 2%
		r4     = money.Rate(400)  // 4%
		r6     = money.Rate(600)  // 6%
		r8     = money.Rate(800)  // 8%
		r93    = money.Rate(930)  // 9.3%
		r103   = money.Rate(1030) // 10.3%
		r113   = money.Rate(1130) // 11.3%
		r123   = money.Rate(1230) // 12.3%
		dollar = money.Dollar
	)

	// Schedule X. Single, or married/RDP filing separately.
	scheduleX := []Bracket{
		{Over: 0, Rate: r1},
		{Over: 11_079 * dollar, Rate: r2, BaseTax: 110*dollar + 79},
		{Over: 26_264 * dollar, Rate: r4, BaseTax: 414*dollar + 49},
		{Over: 41_452 * dollar, Rate: r6, BaseTax: 1_022*dollar + 1},
		{Over: 57_542 * dollar, Rate: r8, BaseTax: 1_987*dollar + 41},
		{Over: 72_724 * dollar, Rate: r93, BaseTax: 3_201*dollar + 97},
		{Over: 371_479 * dollar, Rate: r103, BaseTax: 30_986*dollar + 19},
		{Over: 445_771 * dollar, Rate: r113, BaseTax: 38_638*dollar + 27},
		{Over: 742_953 * dollar, Rate: r123, BaseTax: 72_219*dollar + 84},
	}

	// Schedule Y. Married/RDP filing jointly, or qualifying surviving
	// spouse/RDP.
	scheduleY := []Bracket{
		{Over: 0, Rate: r1},
		{Over: 22_158 * dollar, Rate: r2, BaseTax: 221*dollar + 58},
		{Over: 52_528 * dollar, Rate: r4, BaseTax: 828*dollar + 98},
		{Over: 82_904 * dollar, Rate: r6, BaseTax: 2_044*dollar + 2},
		{Over: 115_084 * dollar, Rate: r8, BaseTax: 3_974*dollar + 82},
		{Over: 145_448 * dollar, Rate: r93, BaseTax: 6_403*dollar + 94},
		{Over: 742_958 * dollar, Rate: r103, BaseTax: 61_972*dollar + 37},
		{Over: 891_542 * dollar, Rate: r113, BaseTax: 77_276*dollar + 52},
		{Over: 1_485_906 * dollar, Rate: r123, BaseTax: 144_439*dollar + 65},
	}

	// Schedule Z. Head of household.
	scheduleZ := []Bracket{
		{Over: 0, Rate: r1},
		{Over: 22_173 * dollar, Rate: r2, BaseTax: 221*dollar + 73},
		{Over: 52_530 * dollar, Rate: r4, BaseTax: 828*dollar + 87},
		{Over: 67_716 * dollar, Rate: r6, BaseTax: 1_436*dollar + 31},
		{Over: 83_805 * dollar, Rate: r8, BaseTax: 2_401*dollar + 65},
		{Over: 98_990 * dollar, Rate: r93, BaseTax: 3_616*dollar + 45},
		{Over: 505_208 * dollar, Rate: r103, BaseTax: 41_394*dollar + 72},
		{Over: 606_251 * dollar, Rate: r113, BaseTax: 51_802*dollar + 15},
		{Over: 1_010_417 * dollar, Rate: r123, BaseTax: 97_472*dollar + 91},
	}

	// The AGI Limitation Worksheet in the booklet. A separate return counts
	// half-sized steps, so the reduction bites twice as fast.
	phaseOutAGI := func(s filing.Status) money.USCents {
		switch s {
		case filing.MarriedFilingJointly, filing.SurvivingSpouse:
			return 504_411 * dollar
		case filing.HeadOfHousehold:
			return 378_310 * dollar
		default:
			return 252_203 * dollar
		}
	}
	phaseOutStep := func(s filing.Status) money.USCents {
		if s == filing.MarriedFilingSeparately {
			return 1_250 * dollar
		}
		return 2_500 * dollar
	}

	byStatus := map[filing.Status]Table{
		filing.Single: {
			Ordinary:          scheduleX,
			StandardDeduction: 5_706 * dollar,
		},
		filing.MarriedFilingSeparately: {
			Ordinary:          scheduleX,
			StandardDeduction: 5_706 * dollar,
		},
		filing.MarriedFilingJointly: {
			Ordinary:          scheduleY,
			StandardDeduction: 11_412 * dollar,
		},
		filing.SurvivingSpouse: {
			Ordinary:          scheduleY,
			StandardDeduction: 11_412 * dollar,
		},
		filing.HeadOfHousehold: {
			// Head of household takes Schedule Z but the joint standard
			// deduction, which is the one place California's chart and its rate
			// schedules disagree about which filers go together.
			Ordinary:          scheduleZ,
			StandardDeduction: 11_412 * dollar,
		},
	}

	for status, t := range byStatus {
		t.Year = 2025
		t.Status = status
		t.PersonalExemptionCredit = 153 * dollar
		t.DependentExemptionCredit = 475 * dollar
		t.ExemptionPhaseOutAGI = phaseOutAGI(status)
		t.ExemptionPhaseOutStep = phaseOutStep(status)
		t.ExemptionPhaseOutPerStep = 6 * dollar
		t.BehavioralHealthThreshold = 1_000_000 * dollar
		t.BehavioralHealthRate = money.Percent(1)
		byStatus[status] = t
	}

	register(2025, byStatus)
}
