package newyorktaxcalc

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
//   - the $300 threshold, the 90%, 100% and 110% tests, the $150,000 and
//     $75,000 steps, the 25% installments, the dates, the January 31 rule,
//     the annualized percentages and the April 15 stop: Tax Law § 685(c) and
//     (d), the 2026 IT-2105-I and the 2025 IT-2105.9-I, whose 2026 edition
//     is not published;
//   - the rates, 8.5% for April to June 2026 and 9.5% for July to December:
//     the department's quarterly interest rate pages for § 697(j).
//
// The rate from January 1, 2027 is not published. The series ends on
// December 31, 2026 and the penalty reports the gap.
//
// No New York holiday list is checked in. The four 2026 due dates are
// weekdays, and the test checks the computed dates against the printed ones.

func init() {
	d := estimated.MustDate
	calendar, err := estimated.NewCalendar("weekends only; no New York holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}
	q2 := "DTF, Interest rates: 4/1/2026–6/30/2026: § 697(j) underpayment rate 8.5%"
	q3 := "DTF, Interest rates: 7/1/2026–9/30/2026: § 697(j) underpayment rate 9.5%"
	q4 := "DTF, Interest rates: 10/1/2026–12/31/2026: § 697(j) underpayment rate 9.5%"
	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.April, 1), d(2026, time.June, 15), 850, q2).
		Period(d(2026, time.June, 16), d(2026, time.June, 30), 850, q2).
		Period(d(2026, time.July, 1), d(2026, time.September, 15), 950, q3).
		Period(d(2026, time.September, 16), d(2026, time.September, 30), 950, q3).
		Period(d(2026, time.October, 1), d(2026, time.December, 31), 950, q4).
		Build()
	if err != nil {
		panic(err)
	}
	registerEstimated(estimatedYear{
		year:           2026,
		threshold:      300 * money.Dollar,
		thisYearShare:  money.Percent(90),
		priorYearShare: money.Percent(100),
		priorYearHigh:  money.Percent(110),
		step:           150_000 * money.Dollar,
		stepSeparate:   75_000 * money.Dollar,
		shares:         []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		annualised:     []money.Rate{2250, 4500, 6750, 9000},
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
		stop:         d(2027, time.April, 15),
		rates:        rates,
	})
}
