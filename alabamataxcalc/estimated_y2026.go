package alabamataxcalc

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
//     $75,000 steps, the due dates and the late-start schedule: the 2026
//     Form 40ES instructions;
//   - the 25% installments, the Schedule AI periods and percentages, the
//     April 15 stop and the printed 7% rate: the 2025 Form 2210AL and its
//     instructions, the latest published;
//   - the quarterly rates for 2026: the Department of Revenue's Quarterly
//     Interest Rates page;
//   - the January 31 date of § 6654(h), which no Alabama form prints: 26
//     U.S.C. § 6654, which Code of Ala. § 40-18-80 adopts.
//
// The 2026 Form 2210AL is not published. The 2025 form's arithmetic is used
// with the 2026 figures from the 40ES. Its line 11 prints 7%, which was the
// rate for every quarter of 2025; for 2026 the department's published rates
// are used for each quarter, and the result reports the divergence.
//
// The rate from January 1, 2027 is not published. The series ends on
// December 31, 2026 and the penalty reports the gap. The first quarter of
// 2026 is left out because no 2026 underpayment runs before April 16.
//
// No State holiday is in the calendar. The four 2026 due dates the 40ES
// prints are weekdays and none is a State holiday under Code of Ala. § 1-3-8;
// the test checks both.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no 2026 due date falls on a Code of Ala. § 1-3-8 State holiday").Build()
	if err != nil {
		panic(err)
	}

	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.April, 1), d(2026, time.June, 30), money.Percent(6), "ADOR, Quarterly Interest Rates: 2026, 2nd quarter").
		Period(d(2026, time.July, 1), d(2026, time.September, 30), money.Percent(7), "ADOR, Quarterly Interest Rates: 2026, 3rd quarter").
		Period(d(2026, time.October, 1), d(2026, time.December, 31), money.Percent(7), "ADOR, Quarterly Interest Rates: 2026, 4th quarter").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:                  2026,
		threshold:             500 * money.Dollar,
		thisYearPercent:       money.Percent(90),
		priorYearPercent:      money.Percent(100),
		highIncomePercent:     money.Percent(110),
		priorYearStep:         150_000 * money.Dollar,
		priorYearStepSeparate: 75_000 * money.Dollar,
		shares:                []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		annualisedPercentages: []money.Rate{2250, 4500, 6750, 9000},
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar: calendar,
		lateGroups: []estimated.Date{
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
		formRate: money.Percent(7),
		rates:    rates,
	})
}
