package mississippitaxcalc

import (
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// Estimated tax for tax year 2026.
//
// Every figure here is checked by estimated_test.go against an extract in
// testdata/, listed in testdata/SOURCES.md:
//
//   - the $200 threshold, the 80% and 100% tests, the quarter installments,
//     withholding in column A, the ½% a month and the months each column
//     runs: Form 80-320 (Rev. 09/25) and its worked example;
//   - the due dates: Rule 100(2) and the quarter table in Form 80-100 (2025);
//   - the ½% rate: Miss. Code § 27-7-327(e), as amended by Laws 2014, H.B. 799.
//
// No 2026 Form 80-320 is published. The 2025 form's arithmetic is used with
// the 2026 dates. April 15, 2027 ends column D, as April 15 ends it in the
// 2025 example's three months from January 15.
//
// The dates are not moved: no Mississippi source says whether a due date on
// a weekend or holiday moves, and every date here is a weekday.

func init() {
	d := estimated.MustDate
	registerEstimated(estimatedYear{
		year:              2026,
		threshold:         200 * money.Dollar,
		thisYearShare:     money.Percent(80),
		priorYearShare:    money.Percent(100),
		shares:            []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		withholdingShares: []money.Rate{money.Percent(100), 0, 0, 0},
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		stop:       d(2027, time.April, 15),
		monthly:    50,
		rateSource: "Miss. Code § 27-7-327(e); Form 80-320 (Rev. 09/25), line 9: ½% a month",
	})
}
