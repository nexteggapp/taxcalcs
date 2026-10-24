package utahtaxcalc

import (
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// Utah's prepayment rules for tax year 2026.
//
// Every figure here is checked by estimated_test.go against an extract in
// testdata/, listed in testdata/SOURCES.md:
//
//   - the 90% and 100% shares: Utah Code § 59-10-516(2)(b) and the TC-546
//     worksheet (Rev. 10/26);
//   - the 2% a month, 24% a year by the day and the 12% cap: Publication 58
//     (Rev. 11/25) and Utah Code § 59-1-401(5)(b);
//   - the due date: Utah Code § 59-10-514(1)(a)(i) makes the Utah return due
//     when the federal one is, and the 2025 TC-40 instructions print April 15
//     for a calendar year, so the 2026 return is due April 15, 2027;
//   - the end of the extension: six months later, § 59-10-516(1)(b)(i), which
//     the 2025 instructions print as October 15 for the 2025 return.
//
// No 2026 TC-40 instructions are published. The rules did not change for
// 2026 (research/estimated-payments/ut.md, 2026 changes).
//
// No Utah holiday list is checked in. The TC-40 instructions move a due date
// on a Saturday, Sunday or legal holiday to the next business day. April 15
// and October 15, 2027 are a Thursday and a Friday, and the test checks
// neither moves.

func init() {
	d := estimated.MustDate

	calendar, err := estimated.NewCalendar("weekends only; no Utah holiday list is checked in").Build()
	if err != nil {
		panic(err)
	}

	registerEstimated(estimatedYear{
		year:          2026,
		thisYear:      money.Percent(90),
		priorYear:     money.Percent(100),
		due:           d(2027, time.April, 15),
		extensionEnds: d(2027, time.October, 15),
		calendar:      calendar,
		penalty:       money.Percent(24),
		penaltyCap:    money.Percent(12),
		daysInYear:    365,
	})
}
