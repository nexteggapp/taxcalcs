package ustaxcalc

// Tax year 2025.
//
// # Two sources, not one
//
// 2026 comes entirely from one revenue procedure. 2025 does not, and assuming
// it did would produce a standard deduction that is wrong by $750.
//
// The rate tables and the capital gains thresholds are Rev. Proc. 2024-40,
// which set the § 1(j)(2) and § 1(j)(5)(B) figures for taxable years beginning
// in 2025. Those still stand.
//
// The standard deduction and the § 63(f) addition do not. Legislation enacted
// during 2025 raised the § 63(c) amounts above what that revenue procedure
// published, so the figures below are the ones the IRS actually printed on the
// 2025 Form 1040 and in its instructions: $15,750, $31,500 and $23,625, against
// the $15,000 and $30,000 the revenue procedure had. The § 63(f) additions come
// from the same source, read as the difference between the "under 65" and
// "65 or older" rows of Chart A.
//
// Both extracts are in testdata. TestTables2025MatchSources checks the
// brackets and the capital gains thresholds against the revenue procedure line
// by line, and checks that every standard deduction figure appears in the
// printed instructions.
//
// The Social Security wage base is $176,100, read from line 7 of the printed
// 2025 Schedule SE rather than from either document above.
//
// The additional Medicare threshold and the prior-year AGI step are statutory
// and are not adjusted for inflation, so they appear in no year's revenue
// procedure.

func init() {
	const (
		wageBase = 176_100 * Dollar // 2025 Schedule SE line 7
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
		{Over: Dollars(23_850), Rate: Percent(12)},
		{Over: Dollars(96_950), Rate: Percent(22)},
		{Over: Dollars(206_700), Rate: Percent(24)},
		{Over: Dollars(394_600), Rate: Percent(32)},
		{Over: Dollars(501_050), Rate: Percent(35)},
		{Over: Dollars(751_600), Rate: Percent(37)},
	}

	// TABLE 3 and TABLE 4 agree up to the 35% band and part at the top: a
	// separate return reaches 37% at $375,800 rather than $626,350.
	unmarriedBands := []Bracket{
		{Over: 0, Rate: Percent(10)},
		{Over: Dollars(11_925), Rate: Percent(12)},
		{Over: Dollars(48_475), Rate: Percent(22)},
		{Over: Dollars(103_350), Rate: Percent(24)},
		{Over: Dollars(197_300), Rate: Percent(32)},
		{Over: Dollars(250_525), Rate: Percent(35)},
		{Over: Dollars(626_350), Rate: Percent(37)},
	}
	separateBands := []Bracket{
		{Over: 0, Rate: Percent(10)},
		{Over: Dollars(11_925), Rate: Percent(12)},
		{Over: Dollars(48_475), Rate: Percent(22)},
		{Over: Dollars(103_350), Rate: Percent(24)},
		{Over: Dollars(197_300), Rate: Percent(32)},
		{Over: Dollars(250_525), Rate: Percent(35)},
		{Over: Dollars(375_800), Rate: Percent(37)},
	}

	byStatus := map[FilingStatus]Table{
		MarriedFilingJointly: {
			Ordinary:            joint,
			StandardDeduction:   Dollars(31_500),
			AdditionalAgedBlind: Dollars(1_600),
			MaxZeroRateAmount:   Dollars(96_700),
			Max15RateAmount:     Dollars(600_050),
		},
		SurvivingSpouse: {
			Ordinary:            joint,
			StandardDeduction:   Dollars(31_500),
			AdditionalAgedBlind: Dollars(1_600),
			MaxZeroRateAmount:   Dollars(96_700),
			Max15RateAmount:     Dollars(600_050),
		},
		HeadOfHousehold: {
			Ordinary: []Bracket{
				{Over: 0, Rate: Percent(10)},
				{Over: Dollars(17_000), Rate: Percent(12)},
				{Over: Dollars(64_850), Rate: Percent(22)},
				{Over: Dollars(103_350), Rate: Percent(24)},
				{Over: Dollars(197_300), Rate: Percent(32)},
				{Over: Dollars(250_500), Rate: Percent(35)},
				{Over: Dollars(626_350), Rate: Percent(37)},
			},
			StandardDeduction: Dollars(23_625),
			// § 63(f)(3) gives an unmarried individual who is not a surviving
			// spouse the larger amount.
			AdditionalAgedBlind: Dollars(2_000),
			MaxZeroRateAmount:   Dollars(64_750),
			Max15RateAmount:     Dollars(566_700),
		},
		Single: {
			Ordinary:            unmarriedBands,
			StandardDeduction:   Dollars(15_750),
			AdditionalAgedBlind: Dollars(2_000),
			MaxZeroRateAmount:   Dollars(48_350),
			Max15RateAmount:     Dollars(533_400),
		},
		MarriedFilingSeparately: {
			Ordinary:            separateBands,
			StandardDeduction:   Dollars(15_750),
			AdditionalAgedBlind: Dollars(1_600),
			MaxZeroRateAmount:   Dollars(48_350),
			Max15RateAmount:     Dollars(300_000),
		},
	}

	for status, t := range byStatus {
		t.Year = 2025
		t.Status = status
		t.SocialSecurityWageBase = wageBase
		t.AdditionalMedicareThreshold = additionalMedicare(status)
		t.PriorYearAGIStep = agiStep(status)
		byStatus[status] = t
	}

	register(2025, byStatus)
}
