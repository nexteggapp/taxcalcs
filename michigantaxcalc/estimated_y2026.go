package michigantaxcalc

import (
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// Estimated tax for tax year 2026.
//
// The figures come from the documents listed below. estimated_test.go checks the
// statutes' figures against their extracts in testdata/, and Treasury's
// against the verified quotes in testdata/research-mi.md, because
// www.michigan.gov refused the downloads. testdata/SOURCES.md lists the
// figures no quote covers.
//
//   - the $500 threshold and the due dates: MCL 206.301(1) and the 2026
//     MI-1040ES (Rev. 04-25);
//   - the 90%, 100% and 110% tests and the $150,000 and $75,000 steps: the
//     2026 MI-1040ES, the 2025 MI-2210 (Rev. 10-25) and RAB 2021-17;
//   - the line arithmetic, the withholding quarters, the annualised periods
//     and percentages, the 25% and 10% penalties with their $25 and $10
//     minimums, the exceptions and the April 15 stop: the 2025 MI-2210 and
//     its instructions, which are the latest published, and RAB 2021-17;
//   - the January 31 rule: RAB 2021-17 and the 2026 MI-1040ES, with MCL 8.6
//     moving January 31, 2027, a Sunday, to Monday, February 1;
//   - the daily interest factors: RAB 2026-5 for 2026 and RAB 2026-10 for
//     January 1 to June 30, 2027.
//
// The 2026 MI-2210 is not published. The 2025 form's arithmetic is used with
// the 2026 dates and rates. No statute behind these rules was amended in 2024,
// 2025 or 2026 (research/estimated-payments/mi.md), so no form here predates
// the law it describes.
//
// The published factors run to June 30, 2027, past the April 15, 2027 stop,
// so no 2026 penalty reaches a day without a rate.
//
// No Michigan holiday list is checked in. The four 2026 due dates the
// MI-1040ES prints are weekdays, and the test checks the computed dates
// against them.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("MCL 8.6, weekends only; no Michigan holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	factor := func(printed string) estimated.Factor {
		f, err := estimated.ParseFactor(printed)
		if err != nil {
			panic(err)
		}
		return f
	}
	daily, err := estimated.NewFactorSeries(estimated.RoundToDollar).
		Period(d(2026, time.January, 1), d(2026, time.June, 30), factor("0.0002324"), 848,
			"RAB 2026-5: January 1, 2026 to June 30, 2026, 8.48%, 0.0002324 a day").
		Period(d(2026, time.July, 1), d(2026, time.December, 31), factor("0.0002150"), 785,
			"RAB 2026-5: July 1, 2026 to December 31, 2026, 7.85%, 0.0002150 a day").
		Period(d(2027, time.January, 1), d(2027, time.June, 30), factor("0.0002129"), 777,
			"RAB 2026-10: January 1, 2027 to June 30, 2027, 7.77%, 0.0002129 a day").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:                  2026,
		threshold:             500 * money.Dollar,
		priorYearStep:         150_000 * money.Dollar,
		priorYearStepSeparate: 75_000 * money.Dollar,
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		calendar:     calendar,
		fileAndPayBy: d(2027, time.January, 31),
		stop:         d(2027, time.April, 15),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		lateGroups: []estimated.Date{
			d(2026, time.April, 1),
			d(2026, time.June, 1),
			d(2026, time.September, 1),
		},
		daily:          daily,
		notPaidRate:    money.Percent(25),
		notPaidMinimum: 25 * money.Dollar,
		shortRate:      money.Percent(10),
		shortMinimum:   10 * money.Dollar,
	})
}
