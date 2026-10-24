package ustaxcalc

import (
	"errors"
	"fmt"
	"time"

	"github.com/nexteggapp/taxcalcs/working"
)

// Capital gains and losses as Schedule D nets them, and the tax on taxable
// income with qualified dividends and net capital gain at their own rates, as
// the Qualified Dividends and Capital Gain Tax Worksheet computes it.
//
// The types here have unexported fields and accessors, and the facts a
// caller supplies are made by a builder whose Build validates.

// capitalLossLimit is § 1211(b)(1): a net capital loss reduces other income
// by at most $3,000, or $1,500 on a separate return. Statutory, and not
// adjusted for inflation.
const capitalLossLimit = 3_000 * Dollar

// CapitalGains are a year's capital gains and losses as Schedule D takes
// them.
type CapitalGains struct {
	shortTerm, longTerm           USCents
	shortCarryover, longCarryover USCents
	distributions                 USCents
}

// ShortTerm is the net short-term gain or loss from sales, Schedule D lines
// 1a to 5.
func (g CapitalGains) ShortTerm() USCents { return g.shortTerm }

// LongTerm is the net long-term gain or loss from sales, Schedule D lines 8a
// to 12.
func (g CapitalGains) LongTerm() USCents { return g.longTerm }

// ShortTermCarryover and LongTermCarryover are last year's losses carried
// forward, Schedule D lines 6 and 14, as positive amounts.
func (g CapitalGains) ShortTermCarryover() USCents { return g.shortCarryover }
func (g CapitalGains) LongTermCarryover() USCents  { return g.longCarryover }

// Distributions are capital gain distributions, Schedule D line 13, which
// are long-term whatever the holding period of the fund shares.
func (g CapitalGains) Distributions() USCents { return g.distributions }

// CapitalGainsBuilder builds [CapitalGains].
type CapitalGainsBuilder struct{ g CapitalGains }

func NewCapitalGains() *CapitalGainsBuilder { return &CapitalGainsBuilder{} }

// ShortTerm sets the net short-term gain, or a loss as a negative amount.
func (b *CapitalGainsBuilder) ShortTerm(net USCents) *CapitalGainsBuilder {
	b.g.shortTerm = net
	return b
}

// LongTerm sets the net long-term gain, or a loss as a negative amount.
func (b *CapitalGainsBuilder) LongTerm(net USCents) *CapitalGainsBuilder {
	b.g.longTerm = net
	return b
}

// ShortTermCarryover sets last year's short-term loss carried forward, as a
// positive amount.
func (b *CapitalGainsBuilder) ShortTermCarryover(loss USCents) *CapitalGainsBuilder {
	b.g.shortCarryover = loss
	return b
}

// LongTermCarryover sets last year's long-term loss carried forward, as a
// positive amount.
func (b *CapitalGainsBuilder) LongTermCarryover(loss USCents) *CapitalGainsBuilder {
	b.g.longCarryover = loss
	return b
}

func (b *CapitalGainsBuilder) Distributions(a USCents) *CapitalGainsBuilder {
	b.g.distributions = a
	return b
}

// Build refuses a negative carryover or distribution. A carryover is a loss
// written as a positive amount, as Schedule D prints it in parentheses, and
// a negative one would be a gain nobody can carry.
func (b *CapitalGainsBuilder) Build() (CapitalGains, error) {
	g := b.g
	switch {
	case g.shortCarryover < 0 || g.longCarryover < 0:
		return CapitalGains{}, errors.New("ustaxcalc: a loss carryover is written as a positive amount")
	case g.distributions < 0:
		return CapitalGains{}, errors.New("ustaxcalc: capital gain distributions cannot be negative")
	}
	return g, nil
}

// CapitalNet is Schedule D netted: Part III and line 7a of Form 1040.
type CapitalNet struct {
	shortTerm, longTerm, combined USCents
	inIncome, netCapitalGain      USCents
}

// ShortTerm is Schedule D line 7: short-term sales less last year's
// short-term carryover.
func (n CapitalNet) ShortTerm() USCents { return n.shortTerm }

// LongTerm is Schedule D line 15: long-term sales and distributions less
// last year's long-term carryover.
func (n CapitalNet) LongTerm() USCents { return n.longTerm }

// Combined is Schedule D line 16, lines 7 and 15 together.
func (n CapitalNet) Combined() USCents { return n.combined }

// InIncome is what goes on Form 1040 line 7a: the combined gain, or the loss
// limited by § 1211(b)(1), as a negative amount.
func (n CapitalNet) InIncome() USCents { return n.inIncome }

// NetCapitalGain is the smaller of Schedule D lines 15 and 16 when both are
// gains, and zero otherwise: the Qualified Dividends and Capital Gain Tax
// Worksheet, line 3, and the gain § 1(h) taxes at its own rates.
func (n CapitalNet) NetCapitalGain() USCents { return n.netCapitalGain }

// NetCapitalGains nets a year's gains and losses as Schedule D does.
//
// It does not compute next year's carryover, which the Capital Loss
// Carryover Worksheet figures from taxable income as well, and it does not
// separate the 28% and 25% gain Schedule D lines 18 and 19 report. A caller
// with either must not use this for them.
func NetCapitalGains(year int, status FilingStatus, g CapitalGains) (Result[CapitalNet], error) {
	if _, err := TableFor(year, status); err != nil {
		return Result[CapitalNet]{}, err
	}
	var n CapitalNet
	n.shortTerm = g.shortTerm - g.shortCarryover
	n.longTerm = g.longTerm + g.distributions - g.longCarryover
	n.combined = n.shortTerm + n.longTerm

	var b working.Notes
	b.Rule("Schedule D (Form 1040), lines 7, 15 and 16",
		fmt.Sprintf("%s of short-term gain less a %s carryover is %s; %s of long-term gain and %s of distributions less a %s carryover is %s; together %s",
			g.shortTerm, g.shortCarryover, n.shortTerm, g.longTerm, g.distributions, g.longCarryover, n.longTerm, n.combined))

	limit := capitalLossLimit
	if status == MarriedFilingSeparately {
		limit = capitalLossLimit / 2
	}
	switch {
	case n.combined >= 0:
		n.inIncome = n.combined
		if n.longTerm > 0 && n.combined > 0 {
			n.netCapitalGain = min(n.longTerm, n.combined)
			b.Rule("26 U.S.C. § 1222(11); Form 1040 instructions, Qualified Dividends and Capital Gain Tax Worksheet, line 3",
				fmt.Sprintf("the net capital gain taxed at the gains rates is the smaller of %s and %s, %s", n.longTerm, n.combined, n.netCapitalGain))
		}
	default:
		n.inIncome = max(n.combined, -limit)
		b.Rule("26 U.S.C. § 1211(b)(1); Schedule D (Form 1040), line 21",
			fmt.Sprintf("a net loss of %s reduces other income by at most %s, so %s is deducted", -n.combined, limit, -n.inIncome))
		if n.combined < -limit {
			b.Rule("26 U.S.C. § 1212(b)",
				fmt.Sprintf("the %s of loss beyond the limit is not lost; it carries forward to next year", -n.combined-limit))
		}
	}
	return working.NewResult(n).With(b).Build()
}

// TaxWithGains is the tax on taxable income with qualified dividends and net
// capital gain at the gains rates.
type TaxWithGains struct {
	ordinaryIncome     USCents
	preferentialIncome USCents
	ladder             Ladder
	gains              GainsTax
	allOrdinary        USCents
	total              USCents
}

// OrdinaryIncome is the taxable income the bands tax, worksheet line 5.
func (t TaxWithGains) OrdinaryIncome() USCents { return t.ordinaryIncome }

// PreferentialIncome is the qualified dividends and net capital gain the
// gains rates tax, worksheet line 10.
func (t TaxWithGains) PreferentialIncome() USCents { return t.preferentialIncome }

// Ladder is the bands on the ordinary income, every band included.
func (t TaxWithGains) Ladder() Ladder {
	l := t.ladder
	l.Bands = append([]Band(nil), l.Bands...)
	return l
}

// Gains is the preferential income across the 0%, 15% and 20% rates.
func (t TaxWithGains) Gains() GainsTax { return t.gains }

// AllOrdinary is the tax on all taxable income through the bands, worksheet
// line 24.
func (t TaxWithGains) AllOrdinary() USCents { return t.allOrdinary }

// Total is the smaller of the bands plus the gains rates and the tax on all
// of it through the bands, worksheet line 25.
func (t TaxWithGains) Total() USCents { return t.total }

// TaxOnIncome taxes taxable income with qualified dividends and net capital
// gain at their own rates, as the Qualified Dividends and Capital Gain Tax
// Worksheet does.
//
// taxable is Form 1040 line 15. qualifiedDividends is line 3a, and
// netCapitalGain is [CapitalNet.NetCapitalGain]. Their sum is limited to
// taxable income (line 10); the rest is ordinary (line 5) and goes through
// the bands. The 0%, 15% and 20% split is [LongTermGainsTax]. A negative
// taxable income is treated as zero.
//
// The bands are figured exactly rather than from the Tax Table, which taxes
// income under $100,000 at the middle of its $50 row, so a figure under
// $100,000 can differ from the table's by a few dollars.
func TaxOnIncome(year int, status FilingStatus, taxable, qualifiedDividends, netCapitalGain USCents) (Result[TaxWithGains], error) {
	table, err := TableFor(year, status)
	if err != nil {
		return Result[TaxWithGains]{}, err
	}
	if qualifiedDividends < 0 || netCapitalGain < 0 {
		return Result[TaxWithGains]{}, errors.New("ustaxcalc: qualified dividends and net capital gain cannot be negative")
	}
	taxable = max(taxable, 0)

	var t TaxWithGains
	t.preferentialIncome = min(taxable, qualifiedDividends+netCapitalGain)
	t.ordinaryIncome = taxable - t.preferentialIncome

	ladder, err := ladderFrom(table, t.ordinaryIncome)
	if err != nil {
		return Result[TaxWithGains]{}, err
	}
	gains, err := gainsFrom(table, t.ordinaryIncome, t.preferentialIncome)
	if err != nil {
		return Result[TaxWithGains]{}, err
	}
	whole, err := ladderFrom(table, taxable)
	if err != nil {
		return Result[TaxWithGains]{}, err
	}
	t.ladder, t.gains = ladder.Value(), gains.Value()
	t.allOrdinary = whole.Value().Total
	t.total = min(t.ladder.Total+t.gains.Total, t.allOrdinary)

	b := working.NewResult(t)
	if t.preferentialIncome > 0 {
		b.Rule("Form 1040 instructions, Qualified Dividends and Capital Gain Tax Worksheet, lines 1 to 5 and 10",
			fmt.Sprintf("%s of qualified dividends and %s of net capital gain, limited to %s of taxable income, leave %s to tax at the ordinary rates",
				qualifiedDividends, netCapitalGain, taxable, t.ordinaryIncome))
	}
	b.Applied(ladder.Applied()...)
	if t.preferentialIncome > 0 {
		b.Applied(gains.Applied()...)
		if netCapitalGain > 0 {
			b.Required(gains.Required()...)
		}
		if t.total == t.allOrdinary && t.allOrdinary < t.ladder.Total+t.gains.Total {
			b.Rule("Form 1040 instructions, Qualified Dividends and Capital Gain Tax Worksheet, lines 24 and 25",
				fmt.Sprintf("taxing all %s at the ordinary rates gives %s, less than %s, so that is the tax", taxable, t.allOrdinary, t.ladder.Total+t.gains.Total))
		}
	}
	return b.Build()
}

// Aged65OrOlder reports whether a person born on born counts as 65 or older
// at the end of year for the § 63(f) addition to the standard deduction.
//
// A person attains an age on the day before the anniversary of their birth,
// so someone born on January 1 is 65 on the last day of the year before
// their 65th birthday. The Form 1040 instructions write it as born before
// January 2 of the year 64 years earlier: for 2025, before January 2, 1961.
// Only born's year, month and day are read.
func Aged65OrOlder(year int, born time.Time) bool {
	cutoff := time.Date(year-64, time.January, 2, 0, 0, 0, 0, time.UTC)
	return time.Date(born.Year(), born.Month(), born.Day(), 0, 0, 0, 0, time.UTC).Before(cutoff)
}
