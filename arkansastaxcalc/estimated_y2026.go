package arkansastaxcalc

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
//   - the due dates, the 90% test and the 10% a year penalty: the 2026
//     AR1000ES;
//   - the $1,000 threshold, the 100% prior-year test, the four equal parts,
//     the April 15 stop and the 365-day year: the 2025 AR2210 and its
//     instructions, whose worked example also rounds each charge to the
//     whole dollar;
//   - the January 31 file-and-pay date: the AR2210 instructions, Exception 2,
//     and Subject 603;
//   - the annualisation periods, the 22.5%, 45%, 67.5% and 90% and the daily
//     factor of .00027397: the 2025 AR2210A.
//
// The 2026 AR2210 and AR2210A are not published. The 2025 forms' arithmetic
// is used with the 2026 dates, a year on from the dates they print. Their
// rules match the 2026 AR1000ES, and research/estimated-payments/ar.md found
// no act changing them for 2026. The 10% is fixed, not a rate reset each
// period, so the days in 2027 up to the April 15 stop are charged at it too.
//
// No Arkansas holiday list is checked in. The four 2026 due dates the AR1000ES
// prints are weekdays, and the test checks the computed dates against them.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Arkansas holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	factor, err := estimated.ParseFactor("0.00027397")
	if err != nil {
		panic(err)
	}
	annualisedFactor, err := estimated.NewFactorSeries(estimated.RoundToDollar).
		Period(d(2026, time.January, 1), d(2027, time.April, 15), factor, money.Percent(10),
			"AR2210A (2025), line 43: .00027397 a day").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:             2026,
		threshold:        1_000 * money.Dollar,
		thisYearPercent:  money.Percent(90),
		priorYearPercent: money.Percent(100),
		shares:           []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar:     calendar,
		fileAndPayBy: d(2027, time.January, 31),
		stop:         d(2027, time.April, 15),
		rate:         money.Percent(10),
		rateSource:   "AR1000ES (2026), Underestimate of Tax: ten percent (10%) per annum",
		yearDays:     365,
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		annualisedPercentages: []money.Rate{2250, 4500, 6750, 9000},
		annualisedFactor:      annualisedFactor,
	})
}
