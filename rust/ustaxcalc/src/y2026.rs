//! Tax year 2026.
//!
//! Rate tables, standard deduction, the additional deduction for age or
//! blindness and the capital gains thresholds are Rev. Proc. 2025-32, which
//! sets the § 1(j)(2), § 63(c)(2), § 63(f) and § 1(j)(5)(B) figures for
//! taxable years beginning in 2026. The extract those were read from is
//! testdata/rp-2025-32-extract.txt in the Go package, whose tests check
//! every figure below against it.
//!
//! The Social Security wage base is the Social Security Administration's
//! 2026 figure of $184,500, which is published separately from the revenue
//! procedure and has no entry in that extract.
//!
//! The additional Medicare threshold and the prior-year AGI step are
//! statutory and are not adjusted for inflation, so they do not appear in
//! any year's revenue procedure.

use std::collections::BTreeMap;

use crate::tables::{Tables, register as register_year};
use crate::{
    Bracket, DOLLAR, FilingStatus, HEAD_OF_HOUSEHOLD, MARRIED_FILING_JOINTLY,
    MARRIED_FILING_SEPARATELY, SINGLE, SURVIVING_SPOUSE, Table, USCents, dollars, percent,
};

/// § 1401(b)(2)(A). Not inflation adjusted.
fn additional_medicare(s: FilingStatus) -> USCents {
    match s {
        MARRIED_FILING_JOINTLY | SURVIVING_SPOUSE => dollars(250_000),
        MARRIED_FILING_SEPARATELY => dollars(125_000),
        _ => dollars(200_000),
    }
}

/// § 6654(d)(1)(C)(i). Not inflation adjusted, and halved for a separate
/// return.
fn agi_step(s: FilingStatus) -> USCents {
    if s == MARRIED_FILING_SEPARATELY {
        return dollars(75_000);
    }
    dollars(150_000)
}

/// Registers 2026's tables.
pub(crate) fn register(tables: &mut Tables) {
    let wage_base = DOLLAR * 184_500; // SSA 2026

    // Married filing jointly and surviving spouse share TABLE 1 and the
    // § 63(c)(2) amount. They are registered separately because § 63(f)(3)
    // treats only one of them as unmarried.
    let joint = vec![
        Bracket {
            over: USCents(0),
            rate: percent(10),
        },
        Bracket {
            over: dollars(24_800),
            rate: percent(12),
        },
        Bracket {
            over: dollars(100_800),
            rate: percent(22),
        },
        Bracket {
            over: dollars(211_400),
            rate: percent(24),
        },
        Bracket {
            over: dollars(403_550),
            rate: percent(32),
        },
        Bracket {
            over: dollars(512_450),
            rate: percent(35),
        },
        Bracket {
            over: dollars(768_700),
            rate: percent(37),
        },
    ];

    // TABLE 3 and TABLE 4 agree up to the 35% band and part at the top: a
    // separate return reaches 37% at $384,350 rather than $640,600.
    let unmarried_bands = vec![
        Bracket {
            over: USCents(0),
            rate: percent(10),
        },
        Bracket {
            over: dollars(12_400),
            rate: percent(12),
        },
        Bracket {
            over: dollars(50_400),
            rate: percent(22),
        },
        Bracket {
            over: dollars(105_700),
            rate: percent(24),
        },
        Bracket {
            over: dollars(201_775),
            rate: percent(32),
        },
        Bracket {
            over: dollars(256_225),
            rate: percent(35),
        },
        Bracket {
            over: dollars(640_600),
            rate: percent(37),
        },
    ];
    let separate_bands = vec![
        Bracket {
            over: USCents(0),
            rate: percent(10),
        },
        Bracket {
            over: dollars(12_400),
            rate: percent(12),
        },
        Bracket {
            over: dollars(50_400),
            rate: percent(22),
        },
        Bracket {
            over: dollars(105_700),
            rate: percent(24),
        },
        Bracket {
            over: dollars(201_775),
            rate: percent(32),
        },
        Bracket {
            over: dollars(256_225),
            rate: percent(35),
        },
        Bracket {
            over: dollars(384_350),
            rate: percent(37),
        },
    ];

    let table = |ordinary: &[Bracket], sd: i64, aab: i64, zero: i64, fifteen: i64| Table {
        ordinary: ordinary.to_vec(),
        standard_deduction: dollars(sd),
        additional_aged_blind: dollars(aab),
        max_zero_rate_amount: dollars(zero),
        max_15_rate_amount: dollars(fifteen),
        ..Table::default()
    };
    let hoh = vec![
        Bracket {
            over: USCents(0),
            rate: percent(10),
        },
        Bracket {
            over: dollars(17_700),
            rate: percent(12),
        },
        Bracket {
            over: dollars(67_450),
            rate: percent(22),
        },
        Bracket {
            over: dollars(105_700),
            rate: percent(24),
        },
        Bracket {
            over: dollars(201_750),
            rate: percent(32),
        },
        Bracket {
            over: dollars(256_200),
            rate: percent(35),
        },
        Bracket {
            over: dollars(640_600),
            rate: percent(37),
        },
    ];

    let mut by_status = BTreeMap::from([
        (
            MARRIED_FILING_JOINTLY,
            table(&joint, 32_200, 1_650, 98_900, 613_700),
        ),
        (
            SURVIVING_SPOUSE,
            table(&joint, 32_200, 1_650, 98_900, 613_700),
        ),
        // § 63(f)(3) gives an unmarried individual who is not a surviving
        // spouse the larger amount.
        (
            HEAD_OF_HOUSEHOLD,
            table(&hoh, 24_150, 2_050, 66_200, 579_600),
        ),
        (
            SINGLE,
            table(&unmarried_bands, 16_100, 2_050, 49_450, 545_500),
        ),
        (
            MARRIED_FILING_SEPARATELY,
            table(&separate_bands, 16_100, 1_650, 49_450, 306_850),
        ),
    ]);

    for (status, t) in by_status.iter_mut() {
        t.year = 2026;
        t.status = *status;
        t.social_security_wage_base = wage_base;
        t.additional_medicare_threshold = additional_medicare(*status);
        t.prior_year_agi_step = agi_step(*status);
    }

    register_year(tables, 2026, by_status);
}
