package mainetaxcalc

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
//   - the $1,000 tests, the 90% and 100% shares, the 12-month condition, the
//     $500,000 unusual event, four equal installments, the due months, the
//     January 31 relief and the end of the penalty period: 36 M.R.S. § 5228;
//   - the end of the penalty period as the return's due date: § 5227;
//   - moving a date for a weekend or Maine legal holiday: § 153(2), with the
//     holidays from 4 M.R.S. § 1051;
//   - monthly compounding: § 186;
//   - the printed 2026 dates: Form 1040ES-ME (2026);
//   - 25% of withholding a quarter, the .75% monthly rate for 2026, a part of
//     a month as a whole one, and the annualized periods and percentages:
//     Form 2210ME (2025) and the 2025 Annualized Income Installment Worksheet.
//
// www.maine.gov refused every download of the Maine Revenue Services PDFs on
// 2026-10-20, so the figures in the last two items are checked against the
// verified quotes in research/estimated-payments/me.md, copied into
// testdata/research-me-quotes.txt. The statutes come from the Revisor's site.
//
// No 2026 date moves. April 15, June 15 and September 15, 2026 and January
// 15, 2027 are weekdays and none is a Maine legal holiday; Patriot's Day is
// April 20, 2026 and April 19, 2027. January 31, 2027, the last day for the
// file-and-pay relief, is a Sunday and moves to Monday, February 1.
//
// The § 186 rate for 2027 is not carried. The rates end on December 31, 2026
// and the penalty reports the months after.

func init() {
	d := estimated.MustDate

	// 4 M.R.S. § 1051, January 2026 to April 2027. The annual Thanksgiving is
	// "any day designated", not a date the statute fixes, and is left out; no
	// date this package moves is in November. None of these falls on a
	// Sunday, so the Monday rule moves none.
	calendar, err := estimated.NewCalendar("4 M.R.S. § 1051, applied by 36 M.R.S. § 153(2)").
		Holiday(d(2026, time.January, 1), "New Year's Day").
		Holiday(d(2026, time.January, 19), "Martin Luther King, Jr., Day").
		Holiday(d(2026, time.February, 16), "Washington's Birthday").
		Holiday(d(2026, time.April, 20), "Patriot's Day").
		Holiday(d(2026, time.May, 25), "Memorial Day").
		Holiday(d(2026, time.June, 19), "Juneteenth").
		Holiday(d(2026, time.July, 4), "the 4th of July").
		Holiday(d(2026, time.September, 7), "Labor Day").
		Holiday(d(2026, time.October, 12), "Indigenous Peoples Day").
		Holiday(d(2026, time.November, 11), "Veterans Day").
		Holiday(d(2026, time.December, 25), "Christmas Day").
		Holiday(d(2027, time.January, 1), "New Year's Day").
		Holiday(d(2027, time.January, 18), "Martin Luther King, Jr., Day").
		Holiday(d(2027, time.February, 15), "Washington's Birthday").
		Holiday(d(2027, time.April, 19), "Patriot's Day").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		threshold:      1_000 * money.Dollar,
		thisYearShare:  money.Percent(90),
		priorYearShare: money.Percent(100),
		unusualEvent:   500_000 * money.Dollar,
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		fileAndPayBy: d(2027, time.January, 31),
		stop:         d(2027, time.April, 15),
		calendar:     calendar,
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		annualised: []money.Rate{2250, 4500, 6750, 9000},
		rates: []monthlyRate{
			{from: d(2026, time.January, 1), to: d(2026, time.December, 31), monthly: 75,
				source: "Form 2210ME (2025) instructions: .75% per month or fraction thereof, compounded monthly, for calendar year 2026"},
		},
	})
}
