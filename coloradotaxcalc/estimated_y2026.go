package coloradotaxcalc

import (
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// Colorado estimated tax for tax year 2026.
//
// Every figure here is checked by estimated_test.go against an extract in
// testdata/, listed in testdata/SOURCES.md:
//
//   - the $1,000 threshold, the 70%, 100% and 110% tests, the $150,000 and
//     $75,000 steps, the due dates, the weekend and holiday rule and the 4.4%
//     rate: DR 0104EP for 2026 (06/10/25);
//   - the line arithmetic, the January 31 file-and-pay date, the April 15
//     stop and the annualised periods, factors and percentages: DR 0204 for
//     2025 (09/25/25), which is the latest published and whose rules C.R.S.
//     § 39-22-605 has not changed since;
//   - the 11% rate for 2026 and the 10% rate for 2027: CDOR's Tax Topics:
//     Penalties and Interest page, which publishes the rate fixed under
//     C.R.S. § 39-21-110.5 for each calendar year.
//
// The 2026 DR 0204 is not published. The 2025 form's arithmetic is used with
// the 2026 figures. The 2025 form prints 12% and 11% on lines 15 and 18,
// which are the rates for 2025 and 2026; a 2026 underpayment runs into 2027,
// so 2026's and 2027's rates are used and the divergence is reported.
//
// No Colorado holiday list is checked in. The four 2026 due dates DR 0104EP
// prints are weekdays, and the test checks the computed dates against them.
// The penalty stops by April 15, 2027, and the 2027 rate covers all of 2027,
// so no day of a 2026 penalty is without a published rate.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Colorado holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	// § 39-21-110.5(3): one rate for each calendar year, fixed on July 1 of
	// the year before.
	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.January, 1), d(2026, time.December, 31), money.Percent(11), "CDOR, Tax Topics: Penalties and Interest: 2026 regular rate, 11%").
		Period(d(2027, time.January, 1), d(2027, time.December, 31), money.Percent(10), "CDOR, Tax Topics: Penalties and Interest: 2027 regular rate, 10%").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:                  2026,
		threshold:             1_000 * money.Dollar,
		thisYearPercent:       money.Percent(70),
		priorPercent:          money.Percent(100),
		priorPercentHigh:      money.Percent(110),
		priorYearStep:         150_000 * money.Dollar,
		priorYearStepSeparate: 75_000 * money.Dollar,
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
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
		factors:               []money.Rate{40_000, 24_000, 15_000, 10_000},
		annualisedPercentages: []money.Rate{1_750, 3_500, 5_250, 7_000},
		taxRate:               440,
		rates:                 rates,
	})
}
