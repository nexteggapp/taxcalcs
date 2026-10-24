package wyomingtaxcalc

// Wyoming for tax year 2026.
//
// The finding is checked by estimated_test.go against the extracts in
// testdata/, listed in testdata/SOURCES.md:
//
//   - ws-title39-ch7.txt: Wyoming Statutes title 39, chapter 7, from the
//     Legislature's compiled title.
//   - wyo-gov-about.txt: the State of Wyoming's About Wyoming page.

func init() {
	registerEstimated(estimatedYear{
		year: 2026,
		why:  "Wyoming has no individual income tax. There is no Wyoming income tax to estimate for 2026. No estimated payments are required.",
		basis: []finding{
			{
				cite: "Wyo. Stat. tit. 39, ch. 7, Income Taxes",
				says: "title 39 reads: \"CHAPTER 7 - INCOME TAXES 39-7-101. Repealed By Laws 1998, ch. 5, § 4.\" Wyoming levies no income tax.",
			},
			{
				cite: "State of Wyoming, About Wyoming",
				says: "the state says: \"Wyoming does not possess an individual or corporate income tax.\" There is no Wyoming income tax to estimate.",
			},
		},
	})
}
