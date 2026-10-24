package westvirginiataxcalc

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
//   - the $600 threshold, the 90% and 100% tests, the 12-month condition,
//     the $5,000 limit for a zero prior year, the quarters, the annualised
//     periods and percentages, the April 15 stop, February 1 and the 11.5%
//     rate with its 0.000315 daily factor: Form IT-210 and its instructions
//     in the 2025 IT-140 booklet, which are the latest published;
//   - the 10% test, the due dates and the weekend and holiday rule: the
//     IT-140ESI (Sept 2023);
//   - the 11.5% rate again: TSD-365;
//   - the late-start date, the last day of January and the holidays:
//     W. Va. Code §§ 11-21-55(d), 11-10-18a(i) and 2-2-1.
//
// No 2026 IT-140ES, IT-140ESI or IT-210 is published. The 2025 IT-210's
// arithmetic is used with the 2026 dates and the 2026 rate it prints.
//
// The rate for 2027 is fixed from the prime rate on the first business day
// of December 2026 (§ 11-10-17a(c)) and is not published. The factors end on
// December 31, 2026 and the penalty reports the gap.

func init() {
	d := estimated.MustDate

	// The § 2-2-1(a) holidays from the first due date to the last date these
	// rules move, each moved under § 2-2-1(b): West Virginia Day and
	// Independence Day fall on Saturdays in 2026, so the Fridays before are
	// the holidays. Election days and days the Governor proclaims are left
	// out, because no document checked in gives their dates. None of these
	// falls on a date the rules move.
	calendar, err := estimated.NewCalendar("W. Va. Code § 2-2-1(a) and (b), April 15, 2026 to February 1, 2027").
		Holiday(d(2026, time.May, 25), "Memorial Day").
		Holiday(d(2026, time.June, 19), "West Virginia Day, June 20 falling on a Saturday").
		Holiday(d(2026, time.July, 3), "Independence Day, July 4 falling on a Saturday").
		Holiday(d(2026, time.September, 7), "Labor Day").
		Holiday(d(2026, time.October, 12), "Columbus Day").
		Holiday(d(2026, time.November, 11), "Veterans' Day").
		Holiday(d(2026, time.November, 26), "Thanksgiving Day").
		Holiday(d(2026, time.November, 27), "Lincoln's Day").
		Holiday(d(2026, time.December, 25), "Christmas Day").
		Holiday(d(2027, time.January, 1), "New Year's Day").
		Holiday(d(2027, time.January, 18), "Martin Luther King's Birthday").
		Build()
	if err != nil {
		panic(err)
	}

	factor, err := estimated.ParseFactor("0.000315")
	if err != nil {
		panic(err)
	}
	daily, err := estimated.NewFactorSeries(estimated.RoundToDollar).
		Period(d(2026, time.January, 1), d(2026, time.December, 31), factor, 1150,
			"IT-210 (2025), Part IV Section B: 11.5%, 0.000315 a day, January 1 to December 31, 2026; TSD-365").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:             2026,
		threshold:        600 * money.Dollar,
		smallShare:       money.Percent(10),
		thisYearShare:    money.Percent(90),
		priorYearShare:   money.Percent(100),
		noLiabilityLimit: 5_000 * money.Dollar,
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar:       calendar,
		lateStartAfter: d(2026, time.April, 1),
		fileAndPayBy:   d(2027, time.January, 31),
		stop:           d(2027, time.April, 15),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		applicable: []money.Rate{2250, 4500, 6750, 9000},
		daily:      daily,
	})
}
