package rhodeislandtaxcalc

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
//   - the due dates and the February 15 date for a return filed in place of
//     the January payment: the 2026 RI-1040ES;
//   - the 80% and 100% options, the $250 floor, the division by four, the
//     withholding rule, the January 31 date, the April 15 stop, and the
//     annualised periods and percentages: the 2025 RI-2210 and its
//     instructions, the latest published;
//   - the 12% rate for calendar year 2026: ADV 2025-23, which the 2025
//     RI-1040 instructions repeat;
//   - the $2 minimum and the April 15 stop: R.I. Gen. Laws § 44-30-84;
//   - the weekend and holiday rule: § 44-30-91(b); the holidays: § 25-1-1.
//
// The 2026 RI-2210 is not published. The 2025 form's arithmetic is used with
// the 2026 dates from the RI-1040ES. The 2025 form says to count days in
// column C to January 1 on line 19a and to December 31 on line 18; the days
// are counted through, as § 44-30-84(b) charges for the whole period.
//
// The rate for 2027 is set on January 1, 2027 under § 44-1-7(b) and is not
// published. The rates end on December 31, 2026 and the penalty reports the
// gap.
//
// The holidays are § 25-1-1's from the first due date to February 16, 2027,
// the last date these rules move. Two kinds are left out: election day,
// because whether 2026 holds a general election of state officers is not in
// the checked-in documents, and days a governor or president appoints, such
// as Thanksgiving, because no proclamation is checked in. Neither falls on a
// date these rules move. No 2026 due date is a weekend or a holiday, so none
// moves; February 15, 2027 is Washington's Birthday, so the RI-1040ES date
// moves to February 16.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("R.I. Gen. Laws § 25-1-1, April 2026 to February 2027").
		Holiday(d(2026, time.May, 4), "the fourth day of May (Rhode Island Independence Day)").
		Holiday(d(2026, time.May, 25), "the last Monday of May (Memorial Day)").
		Holiday(d(2026, time.June, 19), "the nineteenth day of June (Juneteenth National Freedom Day)").
		Holiday(d(2026, time.July, 6), "the fourth day of July, a Saturday, celebrated the following Monday (Independence Day)").
		Holiday(d(2026, time.August, 10), "the second Monday of August (Victory Day)").
		Holiday(d(2026, time.September, 7), "the first Monday of September (Labor Day)").
		Holiday(d(2026, time.October, 12), "the second Monday of October (Columbus Day)").
		Holiday(d(2026, time.November, 11), "the eleventh day of November (Veterans' Day)").
		Holiday(d(2026, time.December, 25), "the twenty-fifth day of December (Christmas Day)").
		Holiday(d(2027, time.January, 1), "the first day of January (New Year's Day)").
		Holiday(d(2027, time.January, 18), "the third Monday of January (Dr. Martin Luther King, Jr.'s Birthday)").
		Holiday(d(2027, time.February, 15), "the third Monday of February (Washington's Birthday)").
		Build()
	if err != nil {
		panic(err)
	}

	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.January, 1), d(2026, time.December, 31), money.Percent(12),
			"ADV 2025-23 (November 26, 2025): 12.00% for calendar year 2026, other tax types; RI-1040 instructions (2025): 12% (0.1200)").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		floor:          250 * money.Dollar,
		thisYearShare:  money.Percent(80),
		priorYearShare: money.Percent(100),
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar:      calendar,
		penaltyFormBy: d(2027, time.January, 31),
		couponBy:      d(2027, time.February, 15),
		stop:          d(2027, time.April, 15),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		applicable: []money.Rate{money.Percent(20), money.Percent(40), money.Percent(60), money.Percent(80)},
		rates:      rates,
		minimum:    2 * money.Dollar,
	})
}
