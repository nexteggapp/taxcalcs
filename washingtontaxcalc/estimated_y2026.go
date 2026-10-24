package washingtontaxcalc

import (
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// Capital gains tax payments for tax year 2026.
//
// Every figure here is checked by estimated_test.go against an extract in
// testdata/, listed in testdata/SOURCES.md:
//
//   - the April 15, 2027 due date and the October 15, 2026 first day for a
//     prepayment: the department's special notice of August 3, 2026, and
//     RCW 82.87.110(3) and 82.87.115(1);
//   - the 9%, 19% and 29% late payment penalty and its $5 minimum, and the
//     80%, $1,000 and 5% of the penalty on a low estimate: RCW 82.32.090(1)
//     and (10);
//   - interest from April 30, 2027: RCW 82.32.050(1)(c)(ii);
//   - the 6% rate for 2027: the department's interest rate tables;
//   - Memorial Day, May 31, 2027: RCW 1.16.050(1)(e).
//
// The rate for 2028 is set on January 1, 2028 and is not published. The
// rates end on December 31, 2027 and the penalty reports the gap.
//
// The due date is the federal return's due date (RCW 82.87.110(1)(a)), which
// moves for District of Columbia holidays too: WAC 458-20-300 Example 1 puts
// 2023's on April 18. April 15 and 16, 2027 are a Thursday and a Friday and
// no holiday falls on the 15th, so the 2027 date does not move. The calendar
// lists only Memorial Day: of the RCW 1.16.050 holidays for 2027 it is the
// one that falls on a date these rules use, May 31, the last day of the
// month after the due date.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("RCW 1.16.050(1) and (5), the 2027 state legal holidays that fall on a date these rules use").
		Holiday(d(2027, time.May, 31), "Memorial Day").
		Build()
	if err != nil {
		panic(err)
	}

	// The department publishes one rate per calendar year.
	calendarYear := func(y int) (estimated.Date, estimated.Date) {
		return d(y, time.January, 1), d(y, time.December, 31)
	}
	from2027, to2027 := calendarYear(2027)
	interest, err := estimated.NewRateSeries().
		Period(from2027, to2027, money.Percent(6),
			"Department of Revenue, Interest rate tables, Excise tax: 6% for 2027; RCW 82.32.050(2)").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		due:            d(2027, time.April, 15),
		prepayFrom:     d(2026, time.October, 15),
		calendar:       calendar,
		lateTotals:     []money.Rate{money.Percent(9), money.Percent(19), money.Percent(29)},
		lateMinimum:    5 * money.Dollar,
		underpaidShare: money.Percent(80),
		underpaidFloor: 1_000 * money.Dollar,
		underpaidRate:  money.Percent(5),
		interestFrom:   d(2027, time.April, 30),
		interest:       interest,
	})
}
