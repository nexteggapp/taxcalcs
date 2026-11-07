//! Self-employment tax under § 1401.

use crate::{Error, FilingStatus, PositionKind, Rate, USCents, percent, table_for};

/// Self-employment tax, split into the parts that are computed differently.
/// Go's `SECA`.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Seca {
    /// Net profit reduced under § 1402(a)(12), which is the base the rates
    /// below apply to.
    pub net_earnings: USCents,

    /// The § 1401(a) old age, survivors and disability insurance part,
    /// capped by the wage base.
    pub social_security: USCents,

    /// The § 1401(b)(1) hospital insurance part, which has no cap.
    pub medicare: USCents,

    /// The § 1401(b)(2) extra 0.9% on combined wages and self-employment
    /// earnings above the threshold.
    pub additional_medicare: USCents,

    /// The sum of the three.
    pub total: USCents,

    /// The § 164(f) deduction of half the tax. It excludes
    /// `additional_medicare`, which § 164(f)(1) does not reach.
    pub above_the_line_deduction: USCents,
}

// Rates fixed by § 1401 rather than adjusted for inflation.

/// § 1402(a)(12) reduces net earnings to 92.35% before the rates apply.
const NET_EARNINGS_FACTOR: Rate = Rate(9235);

const SOCIAL_SECURITY_RATE: Rate = Rate(1240); // § 1401(a), 12.4%
const MEDICARE_RATE: Rate = Rate(290); // § 1401(b)(1), 2.9%
const ADDITIONAL_MEDICARE_RATE: Rate = Rate(90); // § 1401(b)(2), 0.9%

/// § 1402(b)(2). Below this, there is no self-employment tax at all.
const MINIMUM_NET_EARNINGS: USCents = USCents(400 * 100);

const TRADE_OR_BUSINESS: &str = "self-employment tax reaches a trade or business under § 162 and not a \
     hobby under § 183, and the figures alone do not say which this is";

/// Computes the tax on net earnings from self-employment.
///
/// `net_profit` is profit from the trade or business before this tax.
/// `w2_social_security_wages` is the Social Security wages already reported
/// on the taxpayer's Forms W-2, which matters because those wages consume
/// the wage base first: someone whose salary already exceeded it owes no
/// Social Security part on their self-employment earnings at all. Passing
/// zero for a taxpayer who does have W-2 wages overstates the tax, and that
/// interaction is the reason this cannot be estimated from net profit alone.
///
/// `other_medicare_wages` is wages counting toward the additional Medicare
/// threshold, which for most taxpayers is the same figure as
/// `w2_social_security_wages` but diverges once the wage base is passed,
/// because Medicare wages are uncapped.
pub fn self_employment_tax(
    year: i64,
    status: FilingStatus,
    net_profit: USCents,
    w2_social_security_wages: USCents,
    other_medicare_wages: USCents,
) -> Result<working::Result<Seca>, Error> {
    let table = table_for(year, status)?;
    let zero = USCents(0);

    let mut seca = Seca {
        net_earnings: NET_EARNINGS_FACTOR.of(net_profit),
        ..Seca::default()
    };
    if seca.net_earnings < MINIMUM_NET_EARNINGS {
        return Ok(working::new_result(Seca {
            net_earnings: seca.net_earnings,
            ..Seca::default()
        })
        .rule(
            "26 U.S.C. § 1402(b)(2)",
            format!(
                "net earnings of {} are under $400, so no self-employment tax is due",
                seca.net_earnings
            ),
        )
        .require(PositionKind::TradeOrBusiness, TRADE_OR_BUSINESS)
        .build()?);
    }

    // The wage base is consumed by W-2 wages first. § 1402(b)(1).
    let mut room = table.social_security_wage_base - w2_social_security_wages;
    if room < zero {
        room = zero;
    }
    let mut ss_base = seca.net_earnings;
    if ss_base > room {
        ss_base = room;
    }
    seca.social_security = SOCIAL_SECURITY_RATE.of(ss_base);
    seca.medicare = MEDICARE_RATE.of(seca.net_earnings);

    // The additional Medicare tax is on the combined figure over the
    // threshold, and wages are counted first.
    let combined = seca.net_earnings + other_medicare_wages;
    let mut over = combined - table.additional_medicare_threshold;
    if over > zero {
        if over > seca.net_earnings {
            // The wages alone already passed the threshold, so all of the
            // self-employment earnings are over it. What the wages owe is
            // withheld by the employer and is not this calculator's to
            // report.
            over = seca.net_earnings;
        }
        seca.additional_medicare = ADDITIONAL_MEDICARE_RATE.of(over);
    }

    seca.total = seca.social_security + seca.medicare + seca.additional_medicare;
    seca.above_the_line_deduction = percent(50).of(seca.social_security + seca.medicare);

    Ok(working::new_result(seca)
        .rule(
            "26 U.S.C. § 1402(a)(12)",
            format!(
                "{} of net profit reduced to {} of net earnings",
                net_profit, seca.net_earnings
            ),
        )
        .rule(
            "26 U.S.C. § 1401(a)",
            format!(
                "12.4% on {}, being the {} wage base less {} of W-2 Social Security wages, gives {}",
                ss_base, table.social_security_wage_base, w2_social_security_wages, seca.social_security
            ),
        )
        .rule(
            "26 U.S.C. § 1401(b)(1)",
            format!(
                "2.9% on all {} of net earnings gives {}",
                seca.net_earnings, seca.medicare
            ),
        )
        .rule(
            "26 U.S.C. § 164(f)",
            format!(
                "half of {} is deductible above the line",
                seca.social_security + seca.medicare
            ),
        )
        .require(PositionKind::TradeOrBusiness, TRADE_OR_BUSINESS)
        .build()?)
}
