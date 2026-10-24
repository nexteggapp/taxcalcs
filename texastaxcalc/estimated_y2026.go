package texastaxcalc

// Texas for tax year 2026.
//
// The finding is checked by estimated_test.go against the extracts in
// testdata/, listed in testdata/SOURCES.md:
//
//   - hjr-38-enrolled.txt: H.J.R. 38 (86th Legislature, 2019), enrolled, from
//     the Legislature's site.
//   - gov-tx-proclamation-2019.txt: the Governor's proclamation of the
//     November 5, 2019 election results.

func init() {
	registerEstimated(estimatedYear{
		year: 2026,
		why:  "Texas's constitution bars a tax on the net incomes of individuals. There is no Texas income tax to estimate for 2026. No estimated payments are required.",
		basis: []finding{
			{
				cite: "Tex. H.J.R. 38, 86th Leg. (2019), enrolled, adding Tex. Const. art. VIII, § 24-a",
				says: "Article VIII, Section 24-a says: \"The legislature may not impose a tax on the net incomes of individuals\", including an individual's share of partnership income. There is no Texas income tax to estimate.",
			},
			{
				cite: "Office of the Texas Governor, proclamation of the November 5, 2019 constitutional amendment election, December 4, 2019",
				says: "the voters adopted \"PROPOSITION No. 4, as submitted by House Joint Resolution No. 38, prohibiting the imposition of an individual income tax\", so Section 24-a is in force.",
			},
		},
	})
}
