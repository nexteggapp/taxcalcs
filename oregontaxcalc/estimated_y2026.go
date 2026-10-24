package oregontaxcalc

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
//   - the $1,000 threshold, the 90% and 100% tests, the due dates, the
//     late-start chart and the next-business-day rule: Publication
//     OR-ESTIMATE (2026), with the due dates also in the Form OR-40-V
//     instructions;
//   - the worksheet arithmetic, the exceptions, the annualised periods and
//     percentages, the April 15 stop and the 0.000219 daily rate for 2026: the
//     Form OR-10 instructions (2025), which are the latest published;
//   - the 8% rate from January 1, 2026: OAR 150-305-0140;
//   - the Oregon legal holidays: ORS 187.010;
//   - the $20,000 gross income figure: ORS 316.563(1).
//
// The 2026 Form OR-10 is not published. The 2025 instructions' worksheets are
// used with the 2026 dates and rate, which those instructions already print.
//
// The rate from January 1, 2027 is not published. OAR 150-305-0140 lists rates
// only through January 1, 2026, so the factors end on December 31, 2026 and the
// penalty reports the gap.
//
// The calendar holds the ORS 187.010 holidays from January 1, 2026 to April 15,
// 2027. ORS 187.020 adds any day the Governor appoints; none can be listed in
// advance. None of the four 2026 due dates falls on a weekend or holiday.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("ORS 187.010 (2025 edition), legal holidays; ORS 187.020 days appointed by the Governor are not listed").
		Holiday(d(2026, time.January, 1), "New Year's Day").
		Holiday(d(2026, time.January, 19), "Martin Luther King, Jr.'s Birthday").
		Holiday(d(2026, time.February, 16), "Presidents Day").
		Holiday(d(2026, time.May, 25), "Memorial Day").
		Holiday(d(2026, time.June, 19), "Juneteenth").
		Holiday(d(2026, time.July, 3), "Independence Day, observed on the Friday before").
		Holiday(d(2026, time.July, 4), "Independence Day").
		Holiday(d(2026, time.September, 7), "Labor Day").
		Holiday(d(2026, time.November, 11), "Veterans Day").
		Holiday(d(2026, time.November, 26), "Thanksgiving Day").
		Holiday(d(2026, time.December, 25), "Christmas Day").
		Holiday(d(2027, time.January, 1), "New Year's Day").
		Holiday(d(2027, time.January, 18), "Martin Luther King, Jr.'s Birthday").
		Holiday(d(2027, time.February, 15), "Presidents Day").
		Build()
	if err != nil {
		panic(err)
	}

	factor, err := estimated.ParseFactor("0.000219")
	if err != nil {
		panic(err)
	}
	daily, err := estimated.NewFactorSeries(estimated.RoundToDollar).
		Period(d(2026, time.January, 1), d(2026, time.December, 31), factor, money.Percent(8),
			"Form OR-10 instructions (2025), underpayment interest worksheet: 0.000219 a day from 01/01/26; OAR 150-305-0140(1): 8% from January 1, 2026").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		threshold:      1_000 * money.Dollar,
		thisYearShare:  money.Percent(90),
		priorYearShare: money.Percent(100),
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		lateStarts: []estimated.Date{
			d(2026, time.April, 2),
			d(2026, time.June, 2),
			d(2026, time.September, 2),
		},
		calendar: calendar,
		stop:     d(2027, time.April, 15),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		annualised:      []money.Rate{2_250, 4_500, 6_750, 9_000},
		grossIncomeTest: 20_000 * money.Dollar,
		daily:           daily,
	})
}
