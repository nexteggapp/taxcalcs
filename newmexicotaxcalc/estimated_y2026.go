package newmexicotaxcalc

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
//   - the $1,000 threshold, the 90% and 100% tests, the dates, the late-start
//     schedule, the January 31 rule and the April 15 stop: NMSA 1978
//     § 7-2-12.2 as Laws 2011, HB 470 enacted it;
//   - the 25% installments, the line arithmetic, the weekend and holiday
//     rule and the first-quarter rule for April 1 to 15: Form RPD-41272 for
//     2025, whose 2026 edition is not published;
//   - the rates, 6% from April 1 to June 30, 2026 and 7% from July 1 to
//     December 31, 2026: the department's Penalty Interest Rates page.
//
// The rate from January 1, 2027 is not published. The series ends on
// December 31, 2026 and the penalty reports the gap.
//
// No New Mexico holiday list is checked in. The four 2026 due dates are
// weekdays, and the test checks the computed dates against the printed ones.

func init() {
	calendar, err := estimated.NewCalendar("weekends only; no New Mexico holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}
	rates, err := estimated.NewRateSeries().
		Period(date(2026, time.April, 1), date(2026, time.June, 30), money.Percent(6),
			"Taxation and Revenue Department, Penalty Interest Rates: April 1, 2026 – June 30, 2026, 6%").
		Period(date(2026, time.July, 1), date(2026, time.December, 31), money.Percent(7),
			"Taxation and Revenue Department, Penalty Interest Rates: July 1, 2026 – December 31, 2026, 7%").
		Build()
	if err != nil {
		panic(err)
	}
	registerEstimated(estimatedYear{
		year:           2026,
		threshold:      1_000 * money.Dollar,
		thisYearShare:  money.Percent(90),
		priorYearShare: money.Percent(100),
		shares:         []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		dues: []estimated.Date{
			date(2026, time.April, 15),
			date(2026, time.June, 15),
			date(2026, time.September, 15),
			date(2027, time.January, 15),
		},
		calendar: calendar,
		laterGroups: []estimated.Date{
			date(2026, time.April, 1),
			date(2026, time.June, 1),
			date(2026, time.September, 1),
		},
		fileAndPayBy: date(2027, time.January, 31),
		stop:         date(2027, time.April, 15),
		rates:        rates,
	})
}
