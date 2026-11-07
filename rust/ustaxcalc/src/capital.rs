//! Capital gains and losses as Schedule D nets them, and the tax on taxable
//! income with qualified dividends and net capital gain at their own rates,
//! as the Qualified Dividends and Capital Gain Tax Worksheet computes it.
//!
//! The types here have private fields and accessors, and the facts a caller
//! supplies are made by a builder whose `build` validates.

use estimated::{Date, Month};

use crate::brackets::ladder_from;
use crate::capgains::gains_from;
use crate::{
    DOLLAR, Error, FilingStatus, GainsTax, Ladder, MARRIED_FILING_SEPARATELY, USCents, invalid,
    table_for,
};

/// § 1211(b)(1): a net capital loss reduces other income by at most $3,000,
/// or $1,500 on a separate return. Statutory, and not adjusted for
/// inflation.
const CAPITAL_LOSS_LIMIT: USCents = USCents(3_000 * DOLLAR.0);

/// A year's capital gains and losses as Schedule D takes them.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct CapitalGains {
    short_term: USCents,
    long_term: USCents,
    short_carryover: USCents,
    long_carryover: USCents,
    distributions: USCents,
}

impl CapitalGains {
    /// The net short-term gain or loss from sales, Schedule D lines 1a to 5.
    pub fn short_term(&self) -> USCents {
        self.short_term
    }

    /// The net long-term gain or loss from sales, Schedule D lines 8a to 12.
    pub fn long_term(&self) -> USCents {
        self.long_term
    }

    /// Last year's short-term loss carried forward, Schedule D line 6, as a
    /// positive amount.
    pub fn short_term_carryover(&self) -> USCents {
        self.short_carryover
    }

    /// Last year's long-term loss carried forward, Schedule D line 14, as a
    /// positive amount.
    pub fn long_term_carryover(&self) -> USCents {
        self.long_carryover
    }

    /// Capital gain distributions, Schedule D line 13, which are long-term
    /// whatever the holding period of the fund shares.
    pub fn distributions(&self) -> USCents {
        self.distributions
    }
}

/// Builds [`CapitalGains`].
#[derive(Clone, Debug, Default)]
pub struct CapitalGainsBuilder {
    g: CapitalGains,
}

/// Starts a year's capital gains and losses.
pub fn new_capital_gains() -> CapitalGainsBuilder {
    CapitalGainsBuilder::default()
}

impl CapitalGainsBuilder {
    /// Sets the net short-term gain, or a loss as a negative amount.
    pub fn short_term(&mut self, net: USCents) -> &mut Self {
        self.g.short_term = net;
        self
    }

    /// Sets the net long-term gain, or a loss as a negative amount.
    pub fn long_term(&mut self, net: USCents) -> &mut Self {
        self.g.long_term = net;
        self
    }

    /// Sets last year's short-term loss carried forward, as a positive
    /// amount.
    pub fn short_term_carryover(&mut self, loss: USCents) -> &mut Self {
        self.g.short_carryover = loss;
        self
    }

    /// Sets last year's long-term loss carried forward, as a positive
    /// amount.
    pub fn long_term_carryover(&mut self, loss: USCents) -> &mut Self {
        self.g.long_carryover = loss;
        self
    }

    /// Sets the capital gain distributions.
    pub fn distributions(&mut self, a: USCents) -> &mut Self {
        self.g.distributions = a;
        self
    }

    /// Refuses a negative carryover or distribution. A carryover is a loss
    /// written as a positive amount, as Schedule D prints it in parentheses,
    /// and a negative one would be a gain nobody can carry.
    pub fn build(&self) -> Result<CapitalGains, Error> {
        let g = self.g;
        let zero = USCents(0);
        if g.short_carryover < zero || g.long_carryover < zero {
            return Err(invalid!(
                "ustaxcalc: a loss carryover is written as a positive amount"
            ));
        }
        if g.distributions < zero {
            return Err(invalid!(
                "ustaxcalc: capital gain distributions cannot be negative"
            ));
        }
        Ok(g)
    }
}

/// Schedule D netted: Part III and line 7a of Form 1040.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct CapitalNet {
    short_term: USCents,
    long_term: USCents,
    combined: USCents,
    in_income: USCents,
    net_capital_gain: USCents,
}

impl CapitalNet {
    /// Schedule D line 7: short-term sales less last year's short-term
    /// carryover.
    pub fn short_term(&self) -> USCents {
        self.short_term
    }

    /// Schedule D line 15: long-term sales and distributions less last
    /// year's long-term carryover.
    pub fn long_term(&self) -> USCents {
        self.long_term
    }

    /// Schedule D line 16, lines 7 and 15 together.
    pub fn combined(&self) -> USCents {
        self.combined
    }

    /// What goes on Form 1040 line 7a: the combined gain, or the loss
    /// limited by § 1211(b)(1), as a negative amount.
    pub fn in_income(&self) -> USCents {
        self.in_income
    }

    /// The smaller of Schedule D lines 15 and 16 when both are gains, and
    /// zero otherwise: the Qualified Dividends and Capital Gain Tax
    /// Worksheet, line 3, and the gain § 1(h) taxes at its own rates.
    pub fn net_capital_gain(&self) -> USCents {
        self.net_capital_gain
    }
}

/// Nets a year's gains and losses as Schedule D does.
///
/// It does not compute next year's carryover, which the Capital Loss
/// Carryover Worksheet figures from taxable income as well, and it does not
/// separate the 28% and 25% gain Schedule D lines 18 and 19 report. A caller
/// with either must not use this for them.
pub fn net_capital_gains(
    year: i64,
    status: FilingStatus,
    g: CapitalGains,
) -> Result<working::Result<CapitalNet>, Error> {
    table_for(year, status)?;
    let mut n = CapitalNet {
        short_term: g.short_term - g.short_carryover,
        long_term: g.long_term + g.distributions - g.long_carryover,
        ..CapitalNet::default()
    };
    n.combined = n.short_term + n.long_term;

    let mut b = working::Notes::default();
    b.rule(
        "Schedule D (Form 1040), lines 7, 15 and 16",
        format!(
            "{} of short-term gain less a {} carryover is {}; {} of long-term gain and {} of distributions less a {} carryover is {}; together {}",
            g.short_term,
            g.short_carryover,
            n.short_term,
            g.long_term,
            g.distributions,
            g.long_carryover,
            n.long_term,
            n.combined
        ),
    );

    let mut limit = CAPITAL_LOSS_LIMIT;
    if status == MARRIED_FILING_SEPARATELY {
        limit = CAPITAL_LOSS_LIMIT / 2;
    }
    let zero = USCents(0);
    if n.combined >= zero {
        n.in_income = n.combined;
        if n.long_term > zero && n.combined > zero {
            n.net_capital_gain = n.long_term.min(n.combined);
            b.rule(
                "26 U.S.C. § 1222(11); Form 1040 instructions, Qualified Dividends and Capital Gain Tax Worksheet, line 3",
                format!(
                    "the net capital gain taxed at the gains rates is the smaller of {} and {}, {}",
                    n.long_term, n.combined, n.net_capital_gain
                ),
            );
        }
    } else {
        n.in_income = n.combined.max(-limit);
        b.rule(
            "26 U.S.C. § 1211(b)(1); Schedule D (Form 1040), line 21",
            format!(
                "a net loss of {} reduces other income by at most {}, so {} is deducted",
                -n.combined, limit, -n.in_income
            ),
        );
        if n.combined < -limit {
            b.rule(
                "26 U.S.C. § 1212(b)",
                format!(
                    "the {} of loss beyond the limit is not lost; it carries forward to next year",
                    -n.combined - limit
                ),
            );
        }
    }
    Ok(working::new_result(n).with(&b).build()?)
}

/// The tax on taxable income with qualified dividends and net capital gain
/// at the gains rates.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct TaxWithGains {
    ordinary_income: USCents,
    preferential_income: USCents,
    ladder: Ladder,
    gains: GainsTax,
    all_ordinary: USCents,
    total: USCents,
}

impl TaxWithGains {
    /// The taxable income the bands tax, worksheet line 5.
    pub fn ordinary_income(&self) -> USCents {
        self.ordinary_income
    }

    /// The qualified dividends and net capital gain the gains rates tax,
    /// worksheet line 10.
    pub fn preferential_income(&self) -> USCents {
        self.preferential_income
    }

    /// The bands on the ordinary income, every band included.
    pub fn ladder(&self) -> Ladder {
        self.ladder.clone()
    }

    /// The preferential income across the 0%, 15% and 20% rates.
    pub fn gains(&self) -> GainsTax {
        self.gains
    }

    /// The tax on all taxable income through the bands, worksheet line 24.
    pub fn all_ordinary(&self) -> USCents {
        self.all_ordinary
    }

    /// The smaller of the bands plus the gains rates and the tax on all of
    /// it through the bands, worksheet line 25.
    pub fn total(&self) -> USCents {
        self.total
    }
}

/// Taxes taxable income with qualified dividends and net capital gain at
/// their own rates, as the Qualified Dividends and Capital Gain Tax
/// Worksheet does.
///
/// `taxable` is Form 1040 line 15. `qualified_dividends` is line 3a, and
/// `net_capital_gain` is [`CapitalNet::net_capital_gain`]. Their sum is
/// limited to taxable income (line 10); the rest is ordinary (line 5) and
/// goes through the bands. The 0%, 15% and 20% split is
/// [`crate::long_term_gains_tax`]. A negative taxable income is treated as
/// zero.
///
/// The bands are figured exactly rather than from the Tax Table, which taxes
/// income under $100,000 at the middle of its $50 row, so a figure under
/// $100,000 can differ from the table's by a few dollars.
pub fn tax_on_income(
    year: i64,
    status: FilingStatus,
    taxable: USCents,
    qualified_dividends: USCents,
    net_capital_gain: USCents,
) -> Result<working::Result<TaxWithGains>, Error> {
    let table = table_for(year, status)?;
    let zero = USCents(0);
    if qualified_dividends < zero || net_capital_gain < zero {
        return Err(invalid!(
            "ustaxcalc: qualified dividends and net capital gain cannot be negative"
        ));
    }
    let taxable = taxable.max(zero);

    let mut t = TaxWithGains {
        preferential_income: taxable.min(qualified_dividends + net_capital_gain),
        ..TaxWithGains::default()
    };
    t.ordinary_income = taxable - t.preferential_income;

    let ladder = ladder_from(&table, t.ordinary_income)?;
    let gains = gains_from(&table, t.ordinary_income, t.preferential_income)?;
    let whole = ladder_from(&table, taxable)?;
    t.ladder = ladder.value();
    t.gains = gains.value();
    t.all_ordinary = whole.value().total;
    t.total = (t.ladder.total + t.gains.total).min(t.all_ordinary);

    let mut b = working::Notes::default();
    if t.preferential_income > zero {
        b.rule(
            "Form 1040 instructions, Qualified Dividends and Capital Gain Tax Worksheet, lines 1 to 5 and 10",
            format!(
                "{} of qualified dividends and {} of net capital gain, limited to {} of taxable income, leave {} to tax at the ordinary rates",
                qualified_dividends, net_capital_gain, taxable, t.ordinary_income
            ),
        );
    }
    b.applied(&ladder.applied());
    if t.preferential_income > zero {
        b.applied(&gains.applied());
        if net_capital_gain > zero {
            b.required(&gains.required());
        }
        if t.total == t.all_ordinary && t.all_ordinary < t.ladder.total + t.gains.total {
            b.rule(
                "Form 1040 instructions, Qualified Dividends and Capital Gain Tax Worksheet, lines 24 and 25",
                format!(
                    "taxing all {} at the ordinary rates gives {}, less than {}, so that is the tax",
                    taxable,
                    t.all_ordinary,
                    t.ladder.total + t.gains.total
                ),
            );
        }
    }
    Ok(working::new_result(t).with(&b).build()?)
}

/// Reports whether a person born on `born` counts as 65 or older at the end
/// of `year` for the § 63(f) addition to the standard deduction.
///
/// A person attains an age on the day before the anniversary of their
/// birth, so someone born on January 1 is 65 on the last day of the year
/// before their 65th birthday. The Form 1040 instructions write it as born
/// before January 2 of the year 64 years earlier: for 2025, before January
/// 2, 1961.
pub fn aged_65_or_older(year: i64, born: Date) -> bool {
    let cutoff = estimated::date_of(year - 64, Month::JANUARY, 2);
    born.before(cutoff)
}
