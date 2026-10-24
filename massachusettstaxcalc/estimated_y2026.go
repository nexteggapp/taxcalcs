package massachusettstaxcalc

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
//   - the $400 threshold, the due dates, the weekend and holiday rule, the
//     late-start dates and the 2026 surtax threshold: the 2026 Form 1-ES;
//   - the 80% and 100% tests, the $400 exception, the line arithmetic, the
//     withholding rule, the April 15 stop and the annualised percentages and
//     periods: Form M-2210 for 2025, the latest final form, and the draft 2026
//     Form M-2210, which prints the same arithmetic with the 2026 dates;
//   - the late-start groups: AP 241 § 241.3, which agrees with the 1-ES;
//   - the rates, 7% for the second quarter of 2026 and 8% for the third and
//     fourth: TIR 26-5, which the draft 2026 M-2210 lines 20 to 22 repeat;
//   - the statute side of every rule: M.G.L. c. 62B §§ 13 and 14 and c. 62C
//     § 32(a);
//   - the holidays: M.G.L. c. 4 § 7, clause Eighteenth.
//
// The rate from January 1, 2027 is not published; the draft 2026 M-2210
// prints "X%". The rates end on December 31, 2026 and the penalty reports the
// gap.
//
// No 2026 due date falls on a weekend or a Massachusetts legal holiday, so
// none moves. Patriots' Day is April 20, 2026 and Bunker Hill Day June 17.
// DOR's estimated tax web page (updated 06/18/2026) gives the second date as
// June 16, 2026; the statute, the 1-ES and the draft M-2210 all say June 15,
// a Monday, which is used.
//
// No act in 2025 or 2026 amended c. 62B §§ 13 or 14 or c. 62C § 32
// (research/estimated-payments/ma.md), so no form here predates the law.

func init() {
	d := estimated.MustDate

	// Clause Eighteenth's legal holidays from the first due date to the last.
	// A holiday on a Sunday moves to the Monday; July 4, 2026 is a Saturday
	// and stays. Bunker Hill Day is a holiday in Suffolk county only.
	calendar, err := estimated.NewCalendar("M.G.L. c. 4 § 7, clause Eighteenth, April 2026 to January 2027").
		Holiday(d(2026, time.April, 20), "the third Monday in April (Patriots' Day)").
		Holiday(d(2026, time.May, 25), "the last Monday in May").
		Holiday(d(2026, time.June, 17), "Bunker Hill Day, June seventeenth, Suffolk county only").
		Holiday(d(2026, time.June, 19), "June nineteenth").
		Holiday(d(2026, time.July, 4), "July fourth").
		Holiday(d(2026, time.September, 7), "the first Monday in September").
		Holiday(d(2026, time.October, 12), "the second Monday in October").
		Holiday(d(2026, time.November, 11), "November eleventh").
		Holiday(d(2026, time.November, 26), "Thanksgiving Day").
		Holiday(d(2026, time.December, 25), "Christmas Day").
		Holiday(d(2027, time.January, 1), "January first").
		Holiday(d(2027, time.January, 18), "the third Monday in January").
		Build()
	if err != nil {
		panic(err)
	}

	rates, err := estimated.NewRateSeries().
		Period(d(2026, time.April, 1), d(2026, time.June, 30), money.Percent(7), "TIR 26-5 (09/01/2026): 7%, second quarter of 2026; draft 2026 Form M-2210 line 20").
		Period(d(2026, time.July, 1), d(2026, time.September, 30), money.Percent(8), "TIR 26-5 (09/01/2026): 8%, third quarter of 2026; draft 2026 Form M-2210 line 21").
		Period(d(2026, time.October, 1), d(2026, time.December, 31), money.Percent(8), "TIR 26-5 (09/01/2026): 8%, fourth quarter of 2026; draft 2026 Form M-2210 line 22").
		Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:           2026,
		floor:          400 * money.Dollar,
		thisYearShare:  money.Percent(80),
		priorYearShare: money.Percent(100),
		dues: []estimated.Date{
			d(2026, time.April, 15),
			d(2026, time.June, 15),
			d(2026, time.September, 15),
			d(2027, time.January, 15),
		},
		laterGroups: []estimated.Date{
			d(2026, time.April, 1),
			d(2026, time.June, 1),
			d(2026, time.September, 1),
		},
		calendar:    calendar,
		stop:        d(2027, time.April, 15),
		lastCharged: d(2027, time.April, 14),
		periodEnds: []estimated.Date{
			d(2026, time.March, 31),
			d(2026, time.May, 31),
			d(2026, time.August, 31),
			d(2026, time.December, 31),
		},
		applicable: []money.Rate{money.Percent(20), money.Percent(40), money.Percent(60), money.Percent(80)},
		rates:      rates,
	})
}
