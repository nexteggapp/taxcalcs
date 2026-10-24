package delawaretaxcalc

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
//   - the $800 threshold, the 90%, 100% and 110% tests, the $150,000 and
//     $75,000 steps, the four due dates and the late-start dates: PIT-EST
//     for 2026 (revised 11/10/25) and its voucher, and the 2026 payment
//     periods the 2025 PIT-RES instructions print;
//   - the line arithmetic, the $800 stop on line E, the 0.0005 daily factor,
//     the April 30 stop, the withholding periods, the annualised percentages
//     and the Short Method's 12%: PIT-UND for 2025 (revision 20250724) and
//     its instructions (revised 11/10/25), which are the latest published.
//
// The 2026 PIT-UND is not published. The 2025 form's arithmetic is used with
// the 2026 dates. Its line 39 prints 5/1/25 for the first installment, the
// day after April 30, and the due date itself for the other three; the 2026
// dates here follow the same pattern, so the first is May 1, 2026. Line 40
// stops at April 30, 2026, the 2025 return's due date; for 2026 that is
// April 30, 2027, the date 30 Del. C. § 1168 sets.
//
// The 0.0005 a day is fixed by the form, and the 1 1/2% a month behind it by
// 30 Del. C. § 535(b). Neither is set by a notice, so one period covers every
// day a 2026 penalty can run, to April 30, 2027.
//
// No Delaware holiday list is checked in. The four 2026 due dates the forms
// print and April 30, 2027 are weekdays, and the test checks the computed
// dates against the printed ones.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Delaware holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	// Line 40's stop: April 30 of the next year, the return's due date.
	stop := d(2027, time.April, 30)

	factor, err := estimated.ParseFactor("0.0005")
	if err != nil {
		panic(err)
	}
	// 1 1/2% a month is 18% over twelve months; the form's daily factor is
	// 1 1/2% over 30 days.
	daily, err := estimated.NewFactorSeries(estimated.RoundToCent).
		Period(d(2026, time.January, 1), stop, factor, money.Percent(18),
			"PIT-UND (2025), lines 40 to 42: 0.0005 a day; 30 Del. C. § 535(b): 1 1/2% a month").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:                  2026,
		threshold:             800 * money.Dollar,
		thisYearPercent:       money.Percent(90),
		priorPercent:          money.Percent(100),
		priorPercentHigh:      money.Percent(110),
		priorYearStep:         150_000 * money.Dollar,
		priorYearStepSeparate: 75_000 * money.Dollar,
		dues: []estimated.Date{
			d(2026, time.April, 30),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		line39: []estimated.Date{
			d(2026, time.May, 1),
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
		lateStarts: []estimated.Date{
			d(2026, time.April, 2),
			d(2026, time.June, 2),
			d(2026, time.September, 2),
		},
		stop:            stop,
		applicable:      []money.Rate{2_250, 4_500, 6_750, 9_000},
		daily:           daily,
		dailyFactor:     factor,
		shortMethodRate: money.Percent(12),
	})
}
