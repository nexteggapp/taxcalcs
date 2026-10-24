package ohiotaxcalc

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
//   - the $500 threshold and the four due dates: the 2026 IT 1040ES;
//   - 90% and 100%, the 25%, 50%, 75% and 100% columns, the weekend and
//     holiday rule, 365.25 days, factors printed to six places and the
//     7% rate for 2026: the 2025 IT/SD 2210 and its instructions, which are
//     the latest published;
//   - the April 15 end of the last span: ORC 5747.09(C), the return's due
//     date, which the 2025 form's last factor runs to;
//   - rounding each charge to the dollar: the 2025 IT 1040 booklet;
//   - the holidays: ORC 1.14.
//
// The 2026 IT/SD 2210 is not published. The 2025 form's arithmetic is used
// with the 2026 dates, and its factors are worked out by the formula the 2025
// instructions give, which the test checks gives every 2025 factor.
//
// ORC 5703.47(B) sets the rate for 2027 on October 15, 2026, and it is not
// published. The rates end on December 31, 2026 and the penalty reports the
// gap. That leaves 15 days of the third span and all of the fourth uncharged.

func init() {
	d := estimated.MustDate

	// ORC 1.14, January 2026 to April 2027. A day the governor or president
	// appoints is a holiday too and cannot be listed in advance. None of
	// these falls on a Sunday, so the Monday rule moves none. A Saturday is
	// no holiday under 1.14, which moves an act due on a day the office is
	// closed; the IT/SD 2210 instructions move a weekend date.
	calendar, err := estimated.NewCalendar("ORC 1.14 legal holidays; IT/SD 2210 (2025), page 4").
		Holiday(d(2026, time.January, 1), "New Year's day").
		Holiday(d(2026, time.January, 19), "Martin Luther King day").
		Holiday(d(2026, time.February, 16), "Washington-Lincoln day").
		Holiday(d(2026, time.May, 25), "Memorial day").
		Holiday(d(2026, time.June, 19), "Juneteenth day").
		Holiday(d(2026, time.July, 4), "Independence day").
		Holiday(d(2026, time.September, 7), "Labor day").
		Holiday(d(2026, time.October, 12), "Columbus day").
		Holiday(d(2026, time.November, 11), "Veterans' day").
		Holiday(d(2026, time.November, 26), "Thanksgiving day").
		Holiday(d(2026, time.December, 25), "Christmas day").
		Holiday(d(2027, time.January, 1), "New Year's day").
		Holiday(d(2027, time.January, 18), "Martin Luther King day").
		Holiday(d(2027, time.February, 15), "Washington-Lincoln day").
		Build()
	if err != nil {
		panic(err)
	}

	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.January, 1), d(2026, time.December, 31), money.Percent(7),
			"7% for calendar year 2026: IT/SD 2210 (2025), page 4; 2025 IT 1040 booklet; Administrative Journal Entry of October 15, 2025 under ORC 5703.47").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		threshold:      500 * money.Dollar,
		thisYearShare:  money.Percent(90),
		priorYearShare: money.Percent(100),
		shares:         []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar: calendar,
		stop:     d(2027, time.April, 15),
		rates:    rates,
		dayCount: 36525,
		places:   6,
		rounding: estimated.RoundToDollar,
	})
}
