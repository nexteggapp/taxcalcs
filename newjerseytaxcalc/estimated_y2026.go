package newjerseytaxcalc

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
//   - the $400 threshold, the filing thresholds, the due dates and the
//     February 15 rule: the 2026 Form NJ-1040-ES and N.J.S.A. 54A:8-4;
//   - the 80% and 100% tests, the quarters, the four exceptions and their
//     percentages, the periods, the April 15 stop and the month-or-part
//     rule: Form NJ-2210 for 2025, whose 2026 edition is not published;
//   - the 10.00% rate for 2026: TB-21(R).
//
// The NJ-2210 rate schedule runs a year's rate to January 15 of the next
// year, so 10.00% covers the months that start through January 15, 2027.
//
// No New Jersey holiday list is checked in. The four 2026 due dates are
// weekdays, and the test checks the computed dates against the printed
// ones.

func init() {
	calendar, err := estimated.NewCalendar("weekends only; no New Jersey holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}
	registerEstimated(estimatedYear{
		year:                  2026,
		threshold:             400 * money.Dollar,
		filingThreshold:       20_000 * money.Dollar,
		filingThresholdSingle: 10_000 * money.Dollar,
		thisYearShare:         money.Percent(80),
		priorYearShare:        money.Percent(100),
		annualisedShare:       money.Percent(80),
		incomeShare:           money.Percent(90),
		shares:                []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		dues: []estimated.Date{
			date(2026, time.April, 15),
			date(2026, time.June, 15),
			date(2026, time.September, 15),
			date(2027, time.January, 15),
		},
		calendar: calendar,
		periodEnds: []estimated.Date{
			date(2026, time.March, 31),
			date(2026, time.May, 31),
			date(2026, time.August, 31),
			date(2026, time.December, 31),
		},
		fileAndPayBy: date(2027, time.February, 15),
		stop:         date(2027, time.April, 15),
		rate:         money.Percent(10),
		rateTo:       date(2027, time.January, 15),
		rateSource:   "TB-21(R): 10.00% for January 1 – December 31, 2026; Form NJ-2210 (2025), Option 2 schedule",
		unrated:      "New Jersey sets each year's rate from prime on December 1 of the year before",
	})
}
