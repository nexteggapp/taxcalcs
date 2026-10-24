package tennesseetaxcalc

// Tennessee for tax year 2026.
//
// The finding is checked by estimated_test.go against the files in
// testdata/, listed in testdata/SOURCES.md:
//
//   - dor-tn-hall-income-tax.txt: the Department of Revenue's Hall Income Tax
//     page.
//   - research-tn.md: the research file, whose verified quote of the
//     Tennessee Constitution, Article II, Section 28 stands in for the
//     Secretary of State's Tennessee Blue Book 2025-2026, which could not be
//     downloaded.

func init() {
	registerEstimated(estimatedYear{
		year: 2026,
		why:  "Tennessee doesn't tax wages. Its Hall tax was on interest and dividends. It was repealed for tax periods beginning on or after January 1, 2021. There is no Tennessee income tax to estimate for 2026.",
		basis: []finding{
			{
				cite: "Tennessee Department of Revenue, Hall Income Tax",
				says: "the Department of Revenue says: \"The Hall Income tax was repealed for tax periods that begin on January 1, 2021, or later.\" It does not apply to 2026.",
			},
			{
				cite: "Tenn. Const. art. II, § 28 (Tennessee Blue Book 2025-2026)",
				says: "the constitution says \"the Legislature shall not levy, authorize or otherwise permit any state or local tax upon payroll or earned personal income\". There is no Tennessee tax on wages to estimate.",
			},
		},
	})
}
