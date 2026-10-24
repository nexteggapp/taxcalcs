package wisconsintaxcalc

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
//   - the $500 threshold, the 90% and 100% tests and the four due dates: the
//     2026 Form 1-ES instructions, and Wis. Stat. § 71.09(11) to (13);
//   - the quarters, the withholding shares, the annualised periods and
//     percentages, the line arithmetic and the 12% ÷ 365: the 2025 Schedule U
//     and its instructions, which are the latest published, and §§ 71.09(13)
//     and 71.84(1);
//   - the January 31 file-and-pay date: § 71.09(14) and the 1-ES; the April
//     15, 2027 stop: § 71.84(1);
//   - the holidays: Wis. Stat. § 995.20, which § 990.001(4)(e) makes the
//     legal holidays a due date moves for, with the two election days it
//     names dated by § 5.02(5) and (12s).
//
// The 2026 Schedule U is not published. The 2025 form's arithmetic is used
// with the 2026 dates.
//
// The rate is set by statute, not by notice. The series runs from April 16,
// 2026, the first day interest can accrue, to the April 15, 2027 stop, and
// nothing is reported as unpublished.
//
// The holidays are those § 995.20 sets from January 1, 2026 to the stop.
// None falls on a Sunday in that span, so none moves to a Monday.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("Wis. Stat. § 995.20, with § 5.02(5) and (12s), applied by § 990.001(4)(b), (c) and (e)").
		Holiday(d(2026, time.January, 1), "January 1").
		Holiday(d(2026, time.January, 19), "the 3rd Monday in January").
		Holiday(d(2026, time.February, 16), "the 3rd Monday in February").
		Holiday(d(2026, time.May, 25), "the last Monday in May").
		Holiday(d(2026, time.June, 19), "June 19, Juneteenth Day").
		Holiday(d(2026, time.July, 4), "July 4").
		Holiday(d(2026, time.August, 11), "the partisan primary, the 2nd Tuesday in August").
		Holiday(d(2026, time.September, 7), "Labor day").
		Holiday(d(2026, time.October, 12), "the 2nd Monday in October").
		Holiday(d(2026, time.November, 3), "the general election, the Tuesday after the first Monday in November").
		Holiday(d(2026, time.November, 11), "November 11").
		Holiday(d(2026, time.November, 26), "the 4th Thursday in November, Thanksgiving").
		Holiday(d(2026, time.December, 25), "December 25").
		Holiday(d(2027, time.January, 1), "January 1").
		Holiday(d(2027, time.January, 18), "the 3rd Monday in January").
		Holiday(d(2027, time.February, 15), "the 3rd Monday in February").
		Build()
	if err != nil {
		panic(err)
	}

	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.April, 16), d(2027, time.April, 15), money.Percent(12),
			"Wis. Stat. § 71.84(1): 12 percent per year; Schedule U (2025), lines 27 and 28: days ÷ 365 × .12").
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
		annualised:     []money.Rate{2250, 4500, 6750, 9000},
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
	})
}
