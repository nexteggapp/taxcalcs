package minnesotataxcalc

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
//   - the $500 threshold, the 90%, 100% and 110% tests, the $150,000 step,
//     the $1 minimum for nonresidents, the 25% installments and their dates,
//     the annualized periods and percentages, the January 31 and January 15
//     rule, the April 15, 2027 stop and the 7% rate: the 2026 Schedule M15
//     and its instructions, marked "FINAL DRAFT — 10/1/26";
//   - the $1 minimum and the dates again: the department's Estimated Tax
//     Payments for Individuals page;
//   - the 7% rate for 2026: the department's interest rate table, and
//     § 270C.40 subd. 4, which fixes one rate for the whole tax year.
//
// § 270C.40 subd. 4 takes the rate for January 1, 2026 for every day of a
// 2026 underpayment, including the days in 2027, so the series runs to April
// 15, 2027 and no later rate is needed.
//
// No Minnesota holiday list is checked in. The four 2026 due dates the M15
// prints are weekdays, and the test checks the computed dates against them.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Minnesota holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}
	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.January, 1), d(2027, time.April, 15), money.Percent(7),
			"Minn. Stat. § 270C.40 subd. 4; 2026 Schedule M15, line 20: 7% for tax year 2026").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:               2026,
		threshold:          500 * money.Dollar,
		thisYearShare:      money.Percent(90),
		priorYearShare:     money.Percent(100),
		priorYearHigh:      money.Percent(110),
		step:               150_000 * money.Dollar,
		nonresidentMinimum: 1 * money.Dollar,
		shares:             []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		annualised:         []money.Rate{2250, 4500, 6750, 9000},
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar: calendar,
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		fileAndPayBy: d(2027, time.January, 31),
		deemedPaid:   d(2027, time.January, 15),
		stop:         d(2027, time.April, 15),
		rates:        rates,
	})
}
