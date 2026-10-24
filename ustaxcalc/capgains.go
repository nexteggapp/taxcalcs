package ustaxcalc

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/working"
)

// GainsTax is net long-term capital gain split across the three rates.
type GainsTax struct {
	// AtZero, At15 and At20 are how much of the gain each rate reached.
	AtZero USCents
	At15   USCents
	At20   USCents

	// Total is the tax on the gain.
	Total USCents
}

// LongTermGainsTax taxes net long-term capital gain under § 1(h) and
// § 1(j)(5).
//
// taxableOrdinary is taxable income with the gain removed, and netLongTermGain
// is the gain itself. The two are separate arguments because the rate on a gain
// is set by where it lands when stacked on top of ordinary income, not by the
// size of the gain. A $5,000 gain is taxed at zero for one taxpayer and at 20%
// for another.
//
// This models the three headline rates. It does not model the 28% rate on
// collectibles under § 1(h)(4) or the 25% rate on unrecaptured § 1250 gain
// under § 1(h)(6), and a caller holding either must not fold them into
// netLongTermGain. It also does not model the § 1411 net investment income tax,
// which is a separate 3.8% charge rather than a capital gains rate.
func LongTermGainsTax(year int, status FilingStatus, taxableOrdinary, netLongTermGain USCents) (Result[GainsTax], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return Result[GainsTax]{}, err
	}
	return gainsFrom(table, taxableOrdinary, netLongTermGain)
}

func gainsFrom(table Table, taxableOrdinary, netLongTermGain USCents) (Result[GainsTax], error) {
	if taxableOrdinary < 0 {
		taxableOrdinary = 0
	}
	if netLongTermGain <= 0 {
		// A net loss is not taxed here. What a loss does — offsetting ordinary
		// income up to the § 1211(b) limit and carrying forward — belongs to
		// the caller working out taxable income, before this is reached.
		return working.NewResult(GainsTax{}).
			Rule("26 U.S.C. § 1(h)", "no net long-term gain, so no tax at the gains rates").
			Build()
	}

	total := taxableOrdinary + netLongTermGain

	// The gain is stacked above ordinary income. Only the part of the stack
	// that sits below a threshold gets that threshold's rate.
	atZero := table.MaxZeroRateAmount - taxableOrdinary
	if atZero < 0 {
		atZero = 0
	}
	if atZero > netLongTermGain {
		atZero = netLongTermGain
	}

	at20 := total - table.Max15RateAmount
	if at20 < 0 {
		at20 = 0
	}
	if at20 > netLongTermGain {
		at20 = netLongTermGain
	}

	at15 := netLongTermGain - atZero - at20
	if at15 < 0 {
		at15 = 0
	}

	gains := GainsTax{AtZero: atZero, At15: at15, At20: at20}
	gains.Total = Percent(15).Of(at15) + Percent(20).Of(at20)

	return working.NewResult(gains).
		Rule(fmt.Sprintf("26 U.S.C. § 1(h) and § 1(j)(5)(B), as adjusted for %d", table.Year),
			fmt.Sprintf(
				"%s of gain stacked on %s of ordinary taxable income: %s at 0%% up to %s, %s at 15%% up to %s, %s at 20%%",
				netLongTermGain, taxableOrdinary,
				atZero, table.MaxZeroRateAmount,
				at15, table.Max15RateAmount,
				at20)).
		Require(PositionLotSelection, "which lots a sale consumed decides its basis and whether the gain is "+
			"long-term at all. § 1.1012-1(c) takes first in, first out unless the seller "+
			"identified specific lots at the time of sale").
		Build()
}
