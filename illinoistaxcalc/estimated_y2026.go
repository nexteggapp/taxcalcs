package illinoistaxcalc

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
//   - the $1,000 threshold, the 90% and 100% measures, the four equal
//     installments and their dates, and the farmer and nursing-home
//     exceptions: the 2026 IL-1040-ES (R-09/26);
//   - the line arithmetic, the 2% and 10% rates and the 30-day boundary, the
//     weekend and holiday rule, the withholding and pass-through rules, the
//     other exceptions, and the annualised periods and percentages: the 2025
//     IL-2210 and its instructions (R-12/25), which are the latest published;
//   - the 7% interest rate for 2025 and 2026, used only when the caller asks
//     for interest: the department's Interest Rates page.
//
// The 2026 IL-2210 is not published. The 2025 form's arithmetic is used with
// the 2026 figures from the IL-1040-ES. The statute side of each figure is
// research/estimated-payments/il.md, copied into testdata, because ilga.gov
// refused the statutes to a script.
//
// The interest rate from January 1, 2027 is not published. The series ends on
// December 31, 2026 and the penalty reports the gap when interest is asked
// for.
//
// No Illinois holiday list is checked in. The four 2026 due dates the
// IL-1040-ES prints are weekdays, and the test checks the computed dates
// against them.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Illinois holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	interest, err := estimated.NewRateSeries().
		Period(d(2025, time.January, 1), d(2026, time.December, 31), money.Percent(7), "IDOR, Interest Rates: 01/01/2025 - 12/31/2026, 7%").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:          2026,
		threshold:     1_000 * money.Dollar,
		ruleThreshold: 500 * money.Dollar,
		thisYear:      money.Percent(90),
		priorYear:     money.Percent(100),
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
		applicable: []money.Rate{2250, money.Percent(45), 6750, money.Percent(90)},
		lateDays:   30,
		low:        money.Percent(2),
		high:       money.Percent(10),
		interest:   interest,
	})
}
