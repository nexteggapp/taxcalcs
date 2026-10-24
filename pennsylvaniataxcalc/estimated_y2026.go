package pennsylvaniataxcalc

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
//   - the $14,000 income threshold, the $430 tax threshold, the 3.07% rate,
//     the due dates, the late-start table, the 90% test and the prior-year
//     income test: the 2026 REV-413 (I);
//   - the line arithmetic, the exceptions, the special exception, the January
//     31 rule, the April 15 stop and the daily factor: the 2025 REV-1630 and
//     its instructions, which are the latest published;
//   - the 7% rate and its daily factor for 2026: REV-1611 (11-25).
//
// The 2026 REV-1630 is not published. The 2025 form's arithmetic is used with
// the 2026 figures from the REV-413 (I), and the special exception's tax
// figure is the year's own threshold, as the 2025 form uses 2025's $338.
//
// The rate for 2027 is set under Fiscal Code § 806 from the federal rate on
// January 1, 2027, and it is not published. The rates end on December 31,
// 2026 and the penalty reports the gap.
//
// No Pennsylvania holiday list is checked in. The four 2026 due dates the
// REV-413 (I) prints are weekdays, and the test checks the computed dates
// against them.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Pennsylvania holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	factor, err := estimated.ParseFactor("0.000192")
	if err != nil {
		panic(err)
	}
	daily, err := estimated.NewFactorSeries(estimated.RoundToDollar).
		Period(d(2026, time.January, 1), d(2026, time.December, 31), factor, money.Percent(7),
			"REV-1611 (11-25): 7%, 0.000192 a day, 1/1/25 - 12/31/26").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:            2026,
		rate:            307,
		incomeThreshold: 14_000 * money.Dollar,
		taxThreshold:    430 * money.Dollar,
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		laterGroups: []estimated.Date{
			d(2026, time.April, 1),
			d(2026, time.June, 1),
			d(2026, time.September, 1),
		},
		calendar:     calendar,
		fileAndPayBy: d(2027, time.January, 31),
		stop:         d(2027, time.April, 15),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		daily: daily,
	})
}
