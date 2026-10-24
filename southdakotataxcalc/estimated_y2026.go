package southdakotataxcalc

// South Dakota for tax year 2026.
//
// The finding is checked by estimated_test.go against the extracts in
// testdata/, listed in testdata/SOURCES.md:
//
//   - dor-sd-taxes.txt: the Department of Revenue's Taxes page for
//     individuals.

func init() {
	registerEstimated(estimatedYear{
		year: 2026,
		why:  "South Dakota has no state income tax. There is no South Dakota income tax to estimate for 2026. No estimated payments are required.",
		basis: []finding{
			{
				cite: "South Dakota Department of Revenue, Taxes (individuals)",
				says: "the Department of Revenue says: \"South Dakota is one of seven states that does not impose a state income tax.\" There is no South Dakota income tax to estimate.",
			},
		},
	})
}
