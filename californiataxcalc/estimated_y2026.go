package californiataxcalc

import (
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// California estimated tax for tax year 2026.
//
// Every figure here is checked by estimated_test.go against an extract in
// testdata/, listed in testdata/SOURCES.md:
//
//   - the $500 and $250 thresholds, the 90%, 100% and 110% tests, the
//     $150,000 and $75,000 steps, the $1,000,000 and $500,000 limits, the
//     30/40/0/30 shares, the due dates and the January 31 file-and-pay date:
//     the 2026 Instructions for Form 540-ES;
//   - the annualised percentages and periods, withholding in the same shares,
//     and the penalty arithmetic: FTB 5805 (2025) and its instructions, which
//     are the latest published and whose rules are unchanged for 2026;
//   - the rates: the FTB's interest and estimate penalty rates page;
//   - the group-return exception: the FTB's analysis of AB 1518, with the
//     statute read in research/estimated-payments/ca.md.
//
// The FTB has not published the rate from January 1, 2027. The series ends on
// December 31, 2026 and the penalty reports the gap.
//
// No FTB holiday list is checked in. Every due date the 540-ES prints is a
// weekday, so they are moved for weekends only, and the test checks the
// computed dates against the printed ones. A later year whose printed date
// falls on a California holiday needs that list before it ships.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no FTB holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	// § 19521: the January rate applies July to December, the July rate
	// January to June. The FTB publishes each as a period on its rates page.
	rates, err := estimated.NewRateSeries().
		Period(d(2025, time.July, 1), d(2026, time.June, 30), money.Percent(7), "FTB, Interest and estimate penalty rates, 07/01/2025 – 06/30/2026").
		Period(d(2026, time.July, 1), d(2026, time.December, 31), money.Percent(7), "FTB, Interest and estimate penalty rates, 07/01/2026 – 12/31/2026").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:                  2026,
		threshold:             500 * money.Dollar,
		thresholdSeparate:     250 * money.Dollar,
		priorYearStep:         150_000 * money.Dollar,
		priorYearStepSeparate: 75_000 * money.Dollar,
		highIncome:            1_000_000 * money.Dollar,
		highIncomeSeparate:    500_000 * money.Dollar,
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
		rates:             rates,
		groupReturnExempt: true,
	})
}
