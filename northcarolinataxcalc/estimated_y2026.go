package northcarolinataxcalc

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
//   - the 90% and 100% tests, the 25% installments, the dates, the
//     annualized percentages, the January 31 rule and the April 15 stop:
//     N.C. Gen. Stat. § 105-163.15;
//   - the $1,000 threshold, the line arithmetic and the January column rule:
//     Form D-422 for 2025, whose 2026 edition is not published, and Form
//     NC-40 for 2026;
//   - the 7% rate for all of 2026: the department's Interest Rate page.
//
// § 105-163.15(f) takes the threshold from 26 U.S.C. § 6654(e); the figure
// here is the $1,000 the department's forms print.
//
// The rate from January 1, 2027 is not published. The series ends on
// December 31, 2026 and the penalty reports the gap.
//
// No North Carolina holiday list is checked in. The four 2026 due dates are
// weekdays, and the test checks the computed dates against the printed ones.

func init() {
	calendar, err := estimated.NewCalendar("weekends only; no North Carolina holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}
	rates, err := estimated.NewRateSeries().
		Period(date(2026, time.January, 1), date(2026, time.December, 31), money.Percent(7),
			"NCDOR, Interest Rate: 7% for January 1, 2025 through December 31, 2026, and for July 1 through December 31, 2026").
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
		annualised:     []money.Rate{2250, 4500, 6750, 9000},
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
		fileAndPayBy: date(2027, time.January, 31),
		deemedPaid:   date(2027, time.January, 15),
		stop:         date(2027, time.April, 15),
		rates:        rates,
	})
}
