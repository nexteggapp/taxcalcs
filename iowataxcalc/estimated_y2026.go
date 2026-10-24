package iowataxcalc

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
//   - the $1,000 threshold: Iowa Code § 422.16(12)(a)(1) (2026), as amended by
//     2025 Iowa Acts ch. 148, § 59, and the 2026 IA 1040ES;
//   - the 25% installments, the due dates and the weekend rule: the 2026 IA
//     1040ES and Iowa Code §§ 422.16(12)(b)(1), 421.9A;
//   - the 90%, 100% and 110% tests, the $150,000 and $75,000 steps, the
//     late-start table, the equal withholding credit, the April 30 stop and the
//     .00027397 daily factor: the 2025 IA 2210 and its instructions, which are
//     the latest published;
//   - the 10% rate for 2026: the Department of Revenue's press release of
//     October 21, 2025.
//
// No 2026 IA 2210 is published. The 2025 form's arithmetic is used with the
// 2026 dates. Its $200 small-balance test predates the act that raised the
// threshold to $1,000 for 2026, so the act's figure is used and the result
// reports the form as out of date.
//
// The rate from January 1, 2027 is set under Iowa Code § 421.7 and is not
// published. The factors end on December 31, 2026 and the penalty reports the
// gap.
//
// No Iowa holiday list is checked in. § 421.9A names its holidays by category.
// The first three 2026 dates are weekdays, and January 31, 2027 is a Sunday,
// so the fourth installment moves to Monday, February 1, 2027.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; Iowa Code § 421.9A names its holidays by category, and no list of dates is checked in").Build()
	if err != nil {
		panic(err)
	}

	factor, err := estimated.ParseFactor("0.00027397")
	if err != nil {
		panic(err)
	}
	daily, err := estimated.NewFactorSeries(estimated.RoundToCent).
		Period(d(2026, time.January, 1), d(2026, time.December, 31), factor, money.Percent(10),
			"2025 IA 2210, line 24b: 10% per annum, .00027397 per day (01/01/26 - 12/31/26); IDR press release, October 21, 2025").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:                  2026,
		threshold:             1_000 * money.Dollar,
		formThreshold:         200 * money.Dollar,
		thisYearShare:         money.Percent(90),
		priorYearShare:        money.Percent(100),
		priorYearShareHigh:    money.Percent(110),
		priorYearStep:         150_000 * money.Dollar,
		priorYearStepSeparate: 75_000 * money.Dollar,
		dues: []estimated.Date{
			d(2026, time.April, 30),
			d(2026, time.June, 30),
			d(2026, time.September, 30),
			d(2027, time.January, 31),
		},
		laterGroups: []estimated.Date{
			d(2026, time.April, 2),
			d(2026, time.June, 2),
			d(2026, time.September, 2),
		},
		calendar: calendar,
		stop:     d(2027, time.April, 30),
		daily:    daily,
	})
}
