package missouritaxcalc

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
//   - the $100 declaration threshold, the 90% and 100% tests, the due dates,
//     the weekend and holiday rule and equal installments: the 2026 Form
//     MO-1040ES and RSMo 143.521;
//   - the $500 stop, the exceptions and their percentages, the late-start
//     table, the January 31 rule, withholding in equal parts and the April 15
//     stop: Form MO-2210 for 2025, whose 2026 edition is not published, with
//     its dates moved a year;
//   - the 7% rate for 2026: 12 CSR 10-41.010, emergency amendment, and the
//     department's Statutory Interest Rates page, which also posts 7% for
//     2027.
//
// No Missouri holiday list is checked in. The four 2026 due dates are
// weekdays, and the test checks the computed dates against the printed ones.

func init() {
	calendar, err := estimated.NewCalendar("weekends only; no Missouri holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}
	rates, err := estimated.NewRateSeries().
		Period(date(2026, time.January, 1), date(2026, time.December, 31), money.Percent(7),
			"12 CSR 10-41.010 (emergency amendment effective January 1, 2026): 7% for 2026").
		Period(date(2027, time.January, 1), date(2027, time.December, 31), money.Percent(7),
			"Missouri DOR, Statutory Interest Rates: 7% for 2027").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:              2026,
		threshold:         500 * money.Dollar,
		declaration:       100 * money.Dollar,
		thisYearShare:     money.Percent(90),
		priorYearShare:    money.Percent(100),
		incomeShare:       money.Percent(90),
		withholdingShares: []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		dues: []estimated.Date{
			date(2026, time.April, 15),
			date(2026, time.June, 15),
			date(2026, time.September, 15),
			date(2027, time.January, 15),
		},
		calendar: calendar,
		laterGroups: []estimated.Date{
			date(2026, time.April, 2),
			date(2026, time.June, 2),
			date(2026, time.September, 2),
		},
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
