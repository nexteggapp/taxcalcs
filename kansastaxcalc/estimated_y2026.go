package kansastaxcalc

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
//   - the $500 threshold, the 90% and 100% tests, the 12-month condition, the
//     due dates, the weekend rule, the January 31 file-and-pay date and the
//     annualised periods: the 2026 K-40ES;
//   - the line arithmetic, the 25%, 50%, 75% and 100% shares, the annualised
//     22.5%, 45%, 67.5% and 90%, the zero for no prior-year return, and the
//     8% rate from January 1, 2026: Schedule K-210 (2025) and its
//     instructions, which are the latest published;
//   - the 8% rate again, as interest on unpaid tax: the 2025 K-40 booklet,
//     line 29;
//   - the late-start groups, the April 15 stop, the $1 floor and the
//     calendar-year rate: K.S.A. 79-32,102, 79-32,107 and 79-2968.
//
// The 2026 Schedule K-210 is not published. The 2025 schedule's arithmetic
// is used with the 2026 dates.
//
// The rate for 2027 is the federal underpayment rate in effect on July 1,
// 2026, plus one point (K.S.A. 79-2968), and no Kansas document checked in
// publishes it. The rates end on December 31, 2026 and the penalty reports
// the gap.
//
// No Kansas holiday list is checked in. The four 2026 due dates the K-40ES
// prints are weekdays, and the test checks the computed dates against them.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Kansas holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	// K.S.A. 79-2968 fixes the rate for each calendar year.
	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.January, 1), d(2026, time.December, 31), money.Percent(8),
			"Schedule K-210 (2025), instructions for lines 15 and 16: 8% from 1/1/26; K-40 (2025), line 29; K.S.A. 79-2968").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		threshold:      500 * money.Dollar,
		thisYearShare:  money.Percent(90),
		priorYearShare: money.Percent(100),
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar: calendar,
		laterGroups: []estimated.Date{
			d(2026, time.April, 2),
			d(2026, time.June, 2),
			d(2026, time.September, 2),
		},
		fileAndPayBy: d(2027, time.January, 31),
		stop:         d(2027, time.April, 15),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		rates:     rates,
		deMinimis: 1 * money.Dollar,
	})
}
