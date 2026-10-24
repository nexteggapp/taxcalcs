package ustaxcalc

// Tax year 2026.
//
// Rate tables, standard deduction, the additional deduction for age or
// blindness and the capital gains thresholds are Rev. Proc. 2025-32, which sets
// the § 1(j)(2), § 63(c)(2), § 63(f) and § 1(j)(5)(B) figures for taxable years
// beginning in 2026. The extract those were read from is in
// testdata/rp-2025-32-extract.txt and TestTablesMatchRevenueProcedure checks
// every figure below against it.
//
// The Social Security wage base is the Social Security Administration's 2026
// figure of $184,500, which is published separately from the revenue procedure
// and has no entry in that extract.
//
// The additional Medicare threshold and the prior-year AGI step are statutory
// and are not adjusted for inflation, so they do not appear in any year's
// revenue procedure.

func init() {
	const (
		wageBase = 184_500 * Dollar // SSA 2026
	)

	// § 1401(b)(2)(A). Not inflation adjusted.
	additionalMedicare := func(s FilingStatus) USCents {
		switch s {
		case MarriedFilingJointly, SurvivingSpouse:
			return Dollars(250_000)
		case MarriedFilingSeparately:
			return Dollars(125_000)
		default:
			return Dollars(200_000)
		}
	}

	// § 6654(d)(1)(C)(i). Not inflation adjusted, and halved for a separate
	// return.
	agiStep := func(s FilingStatus) USCents {
		if s == MarriedFilingSeparately {
			return Dollars(75_000)
		}
		return Dollars(150_000)
	}

	// Married filing jointly and surviving spouse share TABLE 1 and the
	// § 63(c)(2) amount. They are registered separately because § 63(f)(3)
	// treats only one of them as unmarried.
	joint := []Bracket{
		{Over: 0, Rate: Percent(10)},
		{Over: Dollars(24_800), Rate: Percent(12)},
		{Over: Dollars(100_800), Rate: Percent(22)},
		{Over: Dollars(211_400), Rate: Percent(24)},
		{Over: Dollars(403_550), Rate: Percent(32)},
		{Over: Dollars(512_450), Rate: Percent(35)},
		{Over: Dollars(768_700), Rate: Percent(37)},
	}

	// TABLE 3 and TABLE 4 agree up to the 35% band and part at the top: a
	// separate return reaches 37% at $384,350 rather than $640,600.
	unmarriedBands := []Bracket{
		{Over: 0, Rate: Percent(10)},
		{Over: Dollars(12_400), Rate: Percent(12)},
		{Over: Dollars(50_400), Rate: Percent(22)},
		{Over: Dollars(105_700), Rate: Percent(24)},
		{Over: Dollars(201_775), Rate: Percent(32)},
		{Over: Dollars(256_225), Rate: Percent(35)},
		{Over: Dollars(640_600), Rate: Percent(37)},
	}
	separateBands := []Bracket{
		{Over: 0, Rate: Percent(10)},
		{Over: Dollars(12_400), Rate: Percent(12)},
		{Over: Dollars(50_400), Rate: Percent(22)},
		{Over: Dollars(105_700), Rate: Percent(24)},
		{Over: Dollars(201_775), Rate: Percent(32)},
		{Over: Dollars(256_225), Rate: Percent(35)},
		{Over: Dollars(384_350), Rate: Percent(37)},
	}

	byStatus := map[FilingStatus]Table{
		MarriedFilingJointly: {
			Ordinary:            joint,
			StandardDeduction:   Dollars(32_200),
			AdditionalAgedBlind: Dollars(1_650),
			MaxZeroRateAmount:   Dollars(98_900),
			Max15RateAmount:     Dollars(613_700),
		},
		SurvivingSpouse: {
			Ordinary:            joint,
			StandardDeduction:   Dollars(32_200),
			AdditionalAgedBlind: Dollars(1_650),
			MaxZeroRateAmount:   Dollars(98_900),
			Max15RateAmount:     Dollars(613_700),
		},
		HeadOfHousehold: {
			Ordinary: []Bracket{
				{Over: 0, Rate: Percent(10)},
				{Over: Dollars(17_700), Rate: Percent(12)},
				{Over: Dollars(67_450), Rate: Percent(22)},
				{Over: Dollars(105_700), Rate: Percent(24)},
				{Over: Dollars(201_750), Rate: Percent(32)},
				{Over: Dollars(256_200), Rate: Percent(35)},
				{Over: Dollars(640_600), Rate: Percent(37)},
			},
			StandardDeduction: Dollars(24_150),
			// § 63(f)(3) gives an unmarried individual who is not a surviving
			// spouse the larger amount.
			AdditionalAgedBlind: Dollars(2_050),
			MaxZeroRateAmount:   Dollars(66_200),
			Max15RateAmount:     Dollars(579_600),
		},
		Single: {
			Ordinary:            unmarriedBands,
			StandardDeduction:   Dollars(16_100),
			AdditionalAgedBlind: Dollars(2_050),
			MaxZeroRateAmount:   Dollars(49_450),
			Max15RateAmount:     Dollars(545_500),
		},
		MarriedFilingSeparately: {
			Ordinary:            separateBands,
			StandardDeduction:   Dollars(16_100),
			AdditionalAgedBlind: Dollars(1_650),
			MaxZeroRateAmount:   Dollars(49_450),
			Max15RateAmount:     Dollars(306_850),
		},
	}

	for status, t := range byStatus {
		t.Year = 2026
		t.Status = status
		t.SocialSecurityWageBase = wageBase
		t.AdditionalMedicareThreshold = additionalMedicare(status)
		t.PriorYearAGIStep = agiStep(status)
		byStatus[status] = t
	}

	register(2026, byStatus)
}
