package ustaxcalc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/nexteggapp/taxcalcs/working"
)

// ItemisedLimits applies the two limits Public Law 119-21 put on itemised
// deductions for taxable years beginning after December 31, 2025:
//   - 26 U.S.C. § 170(b)(1)(I), sec. 70425: charitable contributions count
//     only to the extent they exceed 0.5% of the contribution base, taken
//     here as adjusted gross income;
//   - 26 U.S.C. § 68, sec. 70111: what is left is reduced by 2/37 of the
//     lesser of itemised deductions and the amount by which taxable income,
//     with them added back, exceeds where the 37% bracket begins. § 68(b)
//     applies it after every other limit.
//
// itemised is Schedule A line 17 before these limits and after the others,
// such as the state and local tax limit, and charity is the charitable part
// of it. For an earlier year itemised is returned unchanged.
func ItemisedLimits(year int, status FilingStatus, itemised, charity, agi USCents) (Result[USCents], error) {
	if itemised < 0 || charity < 0 || charity > itemised {
		return Result[USCents]{}, errors.New("ustaxcalc: charity is part of itemised deductions, and neither can be negative")
	}
	table, err := TableFor(year, status)
	if err != nil {
		return Result[USCents]{}, err
	}
	var n working.Notes
	if year < 2026 {
		return working.NewResult(itemised).With(n).Build()
	}
	floor := Rate(50).Of(max(agi, 0))
	disallowed := min(charity, floor)
	after := itemised - disallowed
	if charity > 0 {
		n.Rule("26 U.S.C. § 170(b)(1)(I), added by Pub. L. 119-21, sec. 70425",
			fmt.Sprintf("charitable contributions count only above 0.5%% of %s, %s, so %s of the %s is not deducted", agi, floor, disallowed, charity))
	}
	var top USCents
	for _, b := range table.Ordinary {
		if b.Rate == Rate(3700) {
			top = b.Over
		}
	}
	if top == 0 {
		return Result[USCents]{}, fmt.Errorf("ustaxcalc: no 37%% bracket in the %d table", year)
	}
	// Taxable income without § 68, with the itemised deductions added back,
	// is adjusted gross income.
	over := max(agi-top, 0)
	base := min(after, over)
	reduction := USCents(new(big.Int).Quo(new(big.Int).Mul(big.NewInt(int64(base)), big.NewInt(2)), big.NewInt(37)).Int64())
	if over > 0 {
		n.Rule("26 U.S.C. § 68, as amended by Pub. L. 119-21, sec. 70111",
			fmt.Sprintf("2/37 of the lesser of %s of itemised deductions and the %s by which income exceeds the 37%% bracket's %s is %s off", after, over, top, reduction))
	}
	return working.NewResult(after - reduction).With(n).Build()
}
