package nebraskataxcalc

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
//   - the $500 threshold, the 90%, 100% and 110% tests, the $150,000 and
//     $75,000 steps, the 25% columns, the dates, the January 31 rule, the
//     April 15 stop and the 8% rate: the 2026 Form 2210N and its
//     instructions;
//   - the dates, the weekend and holiday rule and the annualized method's
//     federal pattern: the 2026 Form 1040N-ES and Form 2210N;
//   - the rate's source: REG-20-007.03 ties it to § 45-104.02, and Revenue
//     Ruling 99-26-2 sets 8% from January 1, 2025 and for 2027 and 2028.
//
// No Nebraska holiday list is checked in. The four 2026 due dates are
// weekdays, and the test checks the computed dates against the printed ones.

func init() {
	calendar, err := estimated.NewCalendar("weekends only; no Nebraska holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}
	rates, err := estimated.NewRateSeries().
		Period(date(2026, time.January, 1), date(2026, time.December, 31), money.Percent(8),
			"Revenue Ruling 99-26-2: 8% from January 1, 2025; Form 2210N (2026), line 22a").
		Period(date(2027, time.January, 1), date(2028, time.December, 31), money.Percent(8),
			"Revenue Ruling 99-26-2: 8% from January 1, 2027 through December 31, 2028").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		threshold:      500 * money.Dollar,
		thisYearShare:  money.Percent(90),
		priorYearShare: money.Percent(100),
		priorYearHigh:  money.Percent(110),
		step:           150_000 * money.Dollar,
		stepSeparate:   75_000 * money.Dollar,
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
