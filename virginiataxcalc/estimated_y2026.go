package virginiataxcalc

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
//   - the $1,000 filing test, the $11,950, $23,900 and $11,950 VAGI floors,
//     the due dates, the late-start groups, the weekend and holiday rule, the
//     March 1 rule and the $1,000 small-underpayment exception: the 2026 Form
//     760ES (Rev. 09/25);
//   - the $1,000 exception again, the 90% and 100% shares and the May 1 stop:
//     Code of Virginia § 58.1-492, as amended by 2026 Acts c. 7, and the VAGI
//     floors again: § 58.1-321;
//   - the line arithmetic, the exceptions' percentages, the period ends, the
//     90% of Exception 4 and the .00025 daily rate for 9%: Form 760C for 2025
//     and its instructions, which are the latest published;
//   - the rates, 8% from April 1 to June 30, 2026 and 9% from July 1 to
//     December 31, 2026: Tax Bulletins 26-2, 26-3 and 26-4;
//   - the holidays: § 2.2-3300.
//
// The 2026 Form 760C is not published. The 2025 form's arithmetic is used with
// the 2026 dates, and with the $1,000 the 2026 law sets in place of the 2025
// form's $150.
//
// The 2025 form prints .00025 a day for 9% and no factor for 8%. Days at 9%
// are charged at .00025, as the form does. Days at 8% are charged at 8% times
// the days over 365, as 23VAC10-112-42 writes the computation, and the result
// reports it.
//
// Under § 58.1-15(B), days from January 1 to May 1, 2027 take the rate for
// March 2027, which is not published. The rates end on December 31, 2026 and
// the penalty reports the gap.
//
// No 2026 due date moves. May 1, June 15 and September 15, 2026 and January
// 15, 2027 are weekdays and none is a Virginia legal holiday. March 1, 2027,
// the last day of the March 1 rule, is a Monday. May 1, 2027 is a Saturday,
// but it ends the period of the underpayment under § 58.1-492(B)(1) rather
// than being a day to file or pay, so it is not moved.

func init() {
	d := estimated.MustDate

	// § 2.2-3300, January 2026 to April 2027. July 4, 2026 is a Saturday, so
	// the Friday before is the holiday. Days the Governor or the President
	// appoints are not known in advance and are not listed.
	calendar, err := estimated.NewCalendar("Code of Virginia § 2.2-3300, applied by Form 760ES (2026), Section II").
		Holiday(d(2026, time.January, 1), "New Year's Day").
		Holiday(d(2026, time.January, 19), "Martin Luther King, Jr., Day").
		Holiday(d(2026, time.February, 16), "George Washington Day").
		Holiday(d(2026, time.May, 25), "Memorial Day").
		Holiday(d(2026, time.June, 19), "Juneteenth").
		Holiday(d(2026, time.July, 3), "Independence Day, observed").
		Holiday(d(2026, time.September, 7), "Labor Day").
		Holiday(d(2026, time.October, 12), "Columbus Day and Yorktown Victory Day").
		Holiday(d(2026, time.November, 3), "Election Day").
		Holiday(d(2026, time.November, 11), "Veterans Day").
		Holiday(d(2026, time.November, 26), "Thanksgiving Day").
		Holiday(d(2026, time.November, 27), "the Friday after Thanksgiving").
		Holiday(d(2026, time.December, 25), "Christmas Day").
		Holiday(d(2027, time.January, 1), "New Year's Day").
		Holiday(d(2027, time.January, 18), "Martin Luther King, Jr., Day").
		Holiday(d(2027, time.February, 15), "George Washington Day").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:              2026,
		thisYearShare:     money.Percent(90),
		priorYearShare:    money.Percent(100),
		filingThreshold:   1_000 * money.Dollar,
		smallUnderpayment: 1_000 * money.Dollar,
		agiSingle:         11_950 * money.Dollar,
		agiJoint:          23_900 * money.Dollar,
		agiSeparate:       11_950 * money.Dollar,
		dues: []estimated.Date{
			d(2026, time.May, 1),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		laterGroups: []estimated.Date{
			d(2026, time.April, 16),
			d(2026, time.June, 2),
			d(2026, time.September, 2),
		},
		calendar:     calendar,
		fileAndPayBy: d(2027, time.March, 1),
		stop:         d(2027, time.May, 1),
		periodEnds: []estimated.Date{
			d(2026, time.April, 30),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		exceptionShares: []money.Rate{money.Percent(25), money.Percent(50), money.Percent(75), money.Percent(100)},
		annualised:      []money.Rate{2250, 4500, 6750},
		incomeToDate:    money.Percent(90),
		rates: []ratePeriod{
			newRatePeriod(d(2026, time.April, 1), d(2026, time.June, 30), money.Percent(8), "",
				"Tax Bulletin 26-2 (March 10, 2026): 8% for the second quarter of 2026"),
			newRatePeriod(d(2026, time.July, 1), d(2026, time.December, 31), money.Percent(9), "0.00025",
				"Tax Bulletins 26-3 and 26-4: 9% for the third and fourth quarters of 2026; Form 760C (2025), line 23: .00025 a day for 9%"),
		},
	})
}
