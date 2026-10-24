package oklahomataxcalc

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
//   - the $500 threshold, the 70% and prior-year tests, the due dates, the
//     weekend and holiday rule, the $1,000 rule and the 20% rate: Form
//     OW-8-ES (revised 6-2025), for 2026;
//   - the line arithmetic, the $1,000 and $500 stops, the prior-year stop,
//     the even withholding, the earliest-first payment order and the
//     20% ÷ 365 formula: Form OW-8-P, for 2025, which is the latest
//     published;
//   - the annualised periods, factors and percentages: Form OW-8-ES-SUP for
//     2026 and Form OW-8-P-SUP-I for 2025;
//   - the same rules, the January 31 date, the April 15 stop and the farmer
//     share: 68 O.S. §§ 2385.7, 2385.9 and 2385.13, in the Legislature's
//     Title 68 compilation;
//   - the holidays: 25 O.S. § 82.1, in the Legislature's Title 25
//     compilation.
//
// The 2026 OW-8-P is not published. The 2025 form's arithmetic is used with
// the 2026 dates from OW-8-ES.
//
// The rate is set by statute, not by notice, so every day to the April 15,
// 2027 stop has one. HB 2730 (2026) would replace it from November 1, 2026;
// it passed the House and has not been enacted, so it is not used.
//
// The holidays are those § 82.1(A) fixes from April 2026 to February 2027.
// The extra Christmas day for 2026, December 24 or December 28, is chosen by
// the Governor's Executive Order, which is not checked in, so it is left
// out; no due date is near it.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("25 O.S. § 82.1(A), (C); OW-8-ES (revised 6-2025), Due Dates").
		Holiday(d(2026, time.January, 1), "New Year's Day").
		Holiday(d(2026, time.January, 19), "Martin Luther King, Jr.'s Birthday").
		Holiday(d(2026, time.February, 16), "Presidents' Day").
		Holiday(d(2026, time.May, 25), "Memorial Day").
		Holiday(d(2026, time.July, 3), "Independence Day, observed").
		Holiday(d(2026, time.September, 7), "Labor Day").
		Holiday(d(2026, time.November, 11), "Veterans' Day").
		Holiday(d(2026, time.November, 26), "Thanksgiving Day").
		Holiday(d(2026, time.November, 27), "the day after Thanksgiving Day").
		Holiday(d(2026, time.December, 25), "Christmas").
		Holiday(d(2027, time.January, 1), "New Year's Day").
		Holiday(d(2027, time.January, 18), "Martin Luther King, Jr.'s Birthday").
		Holiday(d(2027, time.February, 15), "Presidents' Day").
		Build()
	if err != nil {
		panic(err)
	}

	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.January, 1), d(2027, time.April, 15), money.Percent(20),
			"68 O.S. § 2385.13(A): 20% a year, fixed by statute").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		threshold:      500 * money.Dollar,
		smallTax:       1_000 * money.Dollar,
		thisYearShare:  money.Percent(70),
		priorYearShare: money.Percent(100),
		shares:         []money.Rate{money.Percent(25), money.Percent(25), money.Percent(25), money.Percent(25)},
		annualised:     []money.Rate{1750, 3500, 5250, 7000},
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar:     calendar,
		fileAndPayBy: d(2027, time.January, 31),
		stop:         d(2027, time.April, 15),
		rates:        rates,
		farmShare:    fraction{2, 3},
	})
}
