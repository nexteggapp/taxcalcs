package louisianataxcalc

import (
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// Louisiana estimated tax for tax year 2026.
//
// Every figure here is checked by estimated_test.go against an extract in
// testdata/, listed in testdata/SOURCES.md:
//
//   - the 70% share, the May 15 stop and the repeal of the annualized method:
//     Act 307 of 2026, which applies to tax periods beginning on or after
//     January 1, 2026 and took effect May 22, 2026 (LDR's 2026 Legislative
//     Summaries);
//   - the $1,000 and $2,000 thresholds, the four due dates, the weekend rule
//     and the January 31 file-and-pay date: IT-540ESi (2026);
//   - the late-start groups, line 5's whole dollars, the carryforward, the
//     exceptions, the Exception 5 periods and the 0.000329 daily factor:
//     R-210R-i (1/26), the latest published, for 2025;
//   - the 12% rate, 100% of last year's tax and the equal part of
//     withholding: R.S. 47:118, unchanged by Act 307 on those points.
//
// No 2026 R-210R is published. The 2025 form's arithmetic is used, with Act
// 307's figures where the form predates it.
//
// The rate is fixed by R.S. 47:118(A) and is not reset, so one factor covers
// every day to the May 15, 2027 stop. May 15, 2027 is a Saturday; it ends the
// penalty period rather than being a due date, and nothing says it moves.
//
// No Louisiana holiday list is checked in. The four 2026 due dates IT-540ESi
// prints are weekdays, and the test checks the computed dates against them.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Louisiana holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	factor, err := estimated.ParseFactor("0.000329")
	if err != nil {
		panic(err)
	}
	daily, err := estimated.NewFactorSeries(estimated.RoundToCent).
		Period(d(2026, time.January, 1), d(2027, time.May, 15), factor, money.Percent(12),
			"R-210R-i (1/26), Line 18: 12 percent per year, 0.0329 percent a day; La. R.S. 47:118(A)").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		thisYearShare:  money.Percent(70),
		threshold:      1_000 * money.Dollar,
		thresholdJoint: 2_000 * money.Dollar,
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		laterGroups: []estimated.Date{
			d(2026, time.April, 2),
			d(2026, time.June, 2),
			d(2026, time.September, 2),
		},
		calendar:     calendar,
		fileAndPayBy: d(2027, time.January, 31),
		stop:         d(2027, time.May, 15),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		daily: daily,
	})
}
