package southcarolinataxcalc

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
//   - the $100 threshold, the 100% and 110% tests, the $150,000 step, the due
//     dates, the late-start groups and the February 1 file-and-pay date: the
//     2026 SC1040ES;
//   - the 90% test, the $75,000 step for a separate return, the 25%
//     installments, the April 15 stop and the printed .07: the 2025 SC2210
//     and its instructions, the latest published;
//   - the annualisation periods and percentages, and the January 31 date
//     before moving: 26 U.S.C. § 6654, which S.C. Code § 12-54-55(1) adopts
//     and the SC2210 reaches through the federal Schedule AI;
//   - the rates for 2026: SC Information Letter #26-19;
//   - the legal holidays: SC Revenue Procedure #23-1.
//
// The 2026 SC2210 is not published. The 2025 form's arithmetic is used with
// the 2026 figures. Its rate periods are kept, one rate period for each, and
// each carries the rate Information Letter #26-19 publishes for it rather than
// the .07 the 2025 form prints; the result reports the divergence.
//
// The rate from January 1, 2027 is not published. The series ends on
// December 31, 2026 and the penalty reports the gap. No 2026 underpayment
// runs before April 16, so the series starts with the period that holds it.
//
// Revenue Procedure #23-1 gives each holiday as a rule, such as the third
// Monday in January, and moves one that falls on a Saturday to the Friday
// before and one on a Sunday to the Monday after. The dates below apply those
// rules to 2026 and to January to April 2027, the span the due dates and the
// file-and-pay date can reach. December 26, 2026 is a Saturday whose Friday
// is Christmas Day, so it adds no day.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("SC Revenue Procedure #23-1, legal holidays in South Carolina").
		Holiday(d(2026, time.January, 1), "New Year's Day").
		Holiday(d(2026, time.January, 19), "Martin Luther King, Jr. Day").
		Holiday(d(2026, time.February, 16), "George Washington's Birthday/President's Day").
		Holiday(d(2026, time.May, 11), "Confederate Memorial Day (observed)").
		Holiday(d(2026, time.May, 25), "National Memorial Day").
		Holiday(d(2026, time.June, 19), "Juneteenth National Independence Day").
		Holiday(d(2026, time.July, 3), "Independence Day (observed)").
		Holiday(d(2026, time.September, 7), "Labor Day").
		Holiday(d(2026, time.October, 12), "Columbus Day").
		Holiday(d(2026, time.November, 11), "Veteran's Day").
		Holiday(d(2026, time.November, 26), "Thanksgiving Day").
		Holiday(d(2026, time.November, 27), "Day after Thanksgiving").
		Holiday(d(2026, time.December, 24), "Christmas Eve").
		Holiday(d(2026, time.December, 25), "Christmas Day").
		Holiday(d(2027, time.January, 1), "New Year's Day").
		Holiday(d(2027, time.January, 18), "Martin Luther King, Jr. Day").
		Holiday(d(2027, time.February, 15), "George Washington's Birthday/President's Day").
		Build()
	if err != nil {
		panic(err)
	}

	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.April, 1), d(2026, time.June, 30), money.Percent(6), "SC Information Letter #26-19: 6%, 04/01/26 - 06/30/26").
		Period(d(2026, time.July, 1), d(2026, time.September, 30), money.Percent(7), "SC Information Letter #26-19: 7%, 07/01/26 - 12/31/26").
		Period(d(2026, time.October, 1), d(2026, time.December, 31), money.Percent(7), "SC Information Letter #26-19: 7%, 07/01/26 - 12/31/26").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:                  2026,
		threshold:             100 * money.Dollar,
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
