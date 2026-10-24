package alaskataxcalc

// Alaska for tax year 2026.
//
// The finding is checked by estimated_test.go against the extracts in
// testdata/, listed in testdata/SOURCES.md:
//
//   - dor-ak-personal-income.txt: the Department of Revenue's Personal Income
//     page.

func init() {
	registerEstimated(estimatedYear{
		year: 2026,
		why:  "Alaska has no individual income tax. There is no Alaska income tax to estimate for 2026. No estimated payments are required.",
		basis: []finding{
			{
				cite: "Alaska Department of Revenue, Tax Division, Personal Income",
				says: "the Department of Revenue says: \"The State of Alaska currently does not have an individual income tax, therefore no employee withholding for state income tax is required.\" There is no Alaska income tax to estimate.",
			},
		},
	})
}
