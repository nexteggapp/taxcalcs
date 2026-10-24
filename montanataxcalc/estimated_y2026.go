package montanataxcalc

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
//   - the $500 threshold, the 90% and 100% tests, the 25% installments, the
//     late-start shares, the dates, the January rule, the annualized
//     percentages and the April 15 stop: Mont. Code Ann. § 15-30-2512 and
//     the 2026 Publication 1;
//   - the line arithmetic, the exceptions, the periods and the 0.000192
//     daily factor for 7%: Form EST-I for 2025, whose 2026 edition is not
//     published;
//   - the holidays: Mont. Code Ann. § 1-1-216, with § 1-1-307 moving a day
//     that falls on one or on a Saturday;
//   - the rate: § 15-1-216(7)(a)(i) takes the federal underpayment rate for
//     the third quarter of the preceding year, which Rev. Rul. 2026-10 sets
//     at 7% for the quarter from July 1, 2026. Publication 1 says tax year
//     2026 uses the 2027 rate. The department has not yet set the 2027 rate
//     by rule, and the penalty says so.
//
// The calendar lists the § 1-1-216 holidays from January 2026 to April 2027
// except state general election day, whose date another title sets; no due
// date here is near November 3, 2026.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("Mont. Code Ann. § 1-1-216").
		Holiday(d(2026, time.January, 1), "New Year's Day").
		Holiday(d(2026, time.January, 19), "Martin Luther King Jr. Day").
		Holiday(d(2026, time.February, 16), "Presidents' Day").
		Holiday(d(2026, time.May, 25), "Memorial Day").
		Holiday(d(2026, time.July, 3), "Independence Day (Saturday, so the Friday before)").
		Holiday(d(2026, time.September, 7), "Labor Day").
		Holiday(d(2026, time.October, 12), "Indigenous Peoples' Day and Columbus Day").
		Holiday(d(2026, time.November, 11), "Veterans' Day").
		Holiday(d(2026, time.November, 26), "Thanksgiving Day").
		Holiday(d(2026, time.December, 25), "Christmas Day").
		Holiday(d(2027, time.January, 1), "New Year's Day").
		Holiday(d(2027, time.January, 18), "Martin Luther King Jr. Day").
		Holiday(d(2027, time.February, 15), "Presidents' Day").
		Build()
	if err != nil {
		panic(err)
	}

	factor, err := estimated.ParseFactor("0.000192")
	if err != nil {
		panic(err)
	}
	daily, err := estimated.NewFactorSeries(estimated.RoundToCent).
		Period(d(2026, time.January, 1), d(2027, time.April, 15), factor, money.Percent(7),
			"Form EST-I (2025): 0.000192 a day at 7%; Mont. Code Ann. § 15-1-216(7)(a)(i) and Rev. Rul. 2026-10: 7% for 2027").
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
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar: calendar,
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		fileAndPayBy: d(2027, time.January, 31),
		stop:         d(2027, time.April, 15),
		daily:        daily,
	})
}
