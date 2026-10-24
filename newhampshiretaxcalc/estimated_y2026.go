package newhampshiretaxcalc

// New Hampshire for tax year 2026.
//
// The finding is checked by estimated_test.go against the extracts in
// testdata/, listed in testdata/SOURCES.md:
//
//   - rsa-77.txt: RSA chapter 77, Taxation of Incomes, from the General
//     Court's site.
//   - dra-nh-quoted-in-research.txt: the Department of Revenue
//     Administration's Interest & Dividends Tax page, as quoted in
//     research/estimated-payments/nh.md, because the site refuses scripted
//     downloads.

func init() {
	registerEstimated(estimatedYear{
		year: 2026,
		why:  "New Hampshire doesn't tax wages. Its interest and dividends tax was repealed from January 1, 2025. There is no New Hampshire individual income tax to estimate for 2026. No estimated payments are needed.",
		basis: []finding{
			{
				cite: "N.H. RSA ch. 77, Taxation of Incomes (repealed)",
				says: "RSA chapter 77, the interest and dividends tax, reads: \"Entire Chapter was repealed [Repealed by 2021, 91:99, II, eff. Jan. 1, 2025.]\" The tax does not apply to 2026.",
			},
			{
				cite: "New Hampshire Department of Revenue Administration, Interest & Dividends Tax, as quoted in research/estimated-payments/nh.md",
				says: "the Department of Revenue Administration says: \"The State of New Hampshire does not have an income tax on an individual's reported W-2 wages.\" There is no New Hampshire income tax on wages to estimate.",
			},
		},
	})
}
